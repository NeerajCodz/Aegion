package handler

import (
	"errors"
	"net/http"
	"net/netip"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

type IPBanRequest struct {
	CIDR      string     `json:"cidr"`
	Reason    string     `json:"reason"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
}

type IPBanResponse struct {
	ID        string     `json:"id"`
	CIDR      string     `json:"cidr"`
	Reason    string     `json:"reason"`
	CreatedBy *string    `json:"created_by,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
	Active    bool       `json:"active"`
}

func (h *Handler) ListIPBans(w http.ResponseWriter, r *http.Request) {
	if OperatorFromContext(r.Context()) == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required")
		return
	}

	rows, err := h.dbConn().Query(r.Context(), `
		SELECT id, cidr, reason, COALESCE(created_by::text, ''), created_at, updated_at, expires_at
		FROM adm_ip_bans
		ORDER BY created_at DESC
	`)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Failed to list IP bans")
		return
	}
	defer rows.Close()

	items := make([]IPBanResponse, 0)
	now := time.Now().UTC()
	for rows.Next() {
		var (
			item           IPBanResponse
			id             uuid.UUID
			createdByValue string
		)
		if err := rows.Scan(&id, &item.CIDR, &item.Reason, &createdByValue, &item.CreatedAt, &item.UpdatedAt, &item.ExpiresAt); err != nil {
			writeError(w, http.StatusInternalServerError, "internal_error", "Failed to read IP bans")
			return
		}
		item.ID = id.String()
		if createdByValue != "" {
			item.CreatedBy = &createdByValue
		}
		item.Active = item.ExpiresAt == nil || item.ExpiresAt.After(now)
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Failed to list IP bans")
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"items": items,
		"count": len(items),
	})
}

func (h *Handler) UpsertIPBan(w http.ResponseWriter, r *http.Request) {
	operator := OperatorFromContext(r.Context())
	if operator == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required")
		return
	}

	var req IPBanRequest
	if err := decodeJSONBody(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "Invalid request body")
		return
	}

	cidr, err := normalizeCIDR(strings.TrimSpace(req.CIDR))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_cidr", "Invalid IP or CIDR")
		return
	}
	req.Reason = strings.TrimSpace(req.Reason)
	if req.Reason == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "reason is required")
		return
	}
	if req.ExpiresAt != nil {
		expiresAt := req.ExpiresAt.UTC()
		req.ExpiresAt = &expiresAt
	}

	now := time.Now().UTC()
	var (
		id             uuid.UUID
		createdByValue string
		createdAt      time.Time
		updatedAt      time.Time
		expiresAt      *time.Time
	)
	err = h.dbConn().QueryRow(r.Context(), `
		INSERT INTO adm_ip_bans (id, cidr, reason, created_by, created_at, updated_at, expires_at)
		VALUES (gen_random_uuid(), $1, $2, $3, $4, $5, $6)
		ON CONFLICT (cidr) DO UPDATE SET
			reason = EXCLUDED.reason,
			created_by = EXCLUDED.created_by,
			updated_at = EXCLUDED.updated_at,
			expires_at = EXCLUDED.expires_at
		RETURNING id, COALESCE(created_by::text, ''), created_at, updated_at, expires_at
	`, cidr, req.Reason, operator.ID, now, now, req.ExpiresAt).Scan(&id, &createdByValue, &createdAt, &updatedAt, &expiresAt)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Failed to save IP ban")
		return
	}

	createdByString := operator.ID.String()
	if createdByValue != "" {
		createdByString = createdByValue
	}
	h.logAction(r.Context(), &operator.ID, "upsert", "ip_ban", id.String(), map[string]interface{}{
		"cidr":       cidr,
		"expires_at": expiresAt,
	}, IPAddressFromContext(r.Context()))

	writeJSON(w, http.StatusOK, IPBanResponse{
		ID:        id.String(),
		CIDR:      cidr,
		Reason:    req.Reason,
		CreatedBy: &createdByString,
		CreatedAt: createdAt,
		UpdatedAt: updatedAt,
		ExpiresAt: expiresAt,
		Active:    expiresAt == nil || expiresAt.After(now),
	})
}

func (h *Handler) DeleteIPBan(w http.ResponseWriter, r *http.Request) {
	operator := OperatorFromContext(r.Context())
	if operator == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required")
		return
	}

	id := strings.TrimSpace(chi.URLParam(r, "id"))
	parsedID, err := uuid.Parse(id)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id", "Invalid IP ban ID")
		return
	}

	result, err := h.dbConn().Exec(r.Context(), `DELETE FROM adm_ip_bans WHERE id = $1`, parsedID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Failed to delete IP ban")
		return
	}
	if result.RowsAffected() == 0 {
		writeError(w, http.StatusNotFound, "not_found", "IP ban not found")
		return
	}

	h.logAction(r.Context(), &operator.ID, "delete", "ip_ban", parsedID.String(), nil, IPAddressFromContext(r.Context()))
	w.WriteHeader(http.StatusNoContent)
}

func normalizeCIDR(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", errors.New("empty cidr")
	}
	if prefix, err := netip.ParsePrefix(raw); err == nil {
		return prefix.Masked().String(), nil
	}
	if addr, err := netip.ParseAddr(raw); err == nil {
		if addr.Is4() {
			return netip.PrefixFrom(addr, 32).Masked().String(), nil
		}
		return netip.PrefixFrom(addr, 128).Masked().String(), nil
	}
	return "", errors.New("invalid cidr")
}
