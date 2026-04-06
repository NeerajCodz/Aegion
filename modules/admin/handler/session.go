// Package handler provides session management handlers for the admin module.
package handler

import (
	"context"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// ListIdentitySessions handles GET /admin/identities/{id}/sessions
func (h *Handler) ListIdentitySessions(w http.ResponseWriter, r *http.Request) {
	operator := OperatorFromContext(r.Context())
	if operator == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required")
		return
	}

	idStr := chi.URLParam(r, "id")
	identityID, err := uuid.Parse(idStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id", "Invalid identity ID format")
		return
	}

	page, perPage, offset := h.parsePagination(r)

	sessions, total, err := h.listSessionsForIdentity(r.Context(), identityID, perPage, offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Failed to list sessions")
		return
	}

	ipAddress := IPAddressFromContext(r.Context())
	h.logAction(r.Context(), &operator.ID, "list", "session", "", map[string]interface{}{
		"identity_id": identityID.String(),
		"page":        page,
		"per_page":    perPage,
	}, ipAddress)

	resp := ListResponse{
		Items:      sessions,
		Pagination: buildPaginationMeta(page, perPage, total),
	}
	writeJSON(w, http.StatusOK, resp)
}

// RevokeAllIdentitySessions handles DELETE /admin/identities/{id}/sessions
func (h *Handler) RevokeAllIdentitySessions(w http.ResponseWriter, r *http.Request) {
	operator := OperatorFromContext(r.Context())
	if operator == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required")
		return
	}

	idStr := chi.URLParam(r, "id")
	identityID, err := uuid.Parse(idStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id", "Invalid identity ID format")
		return
	}

	count, err := h.revokeSessionsForIdentity(r.Context(), identityID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Failed to revoke sessions")
		return
	}

	ipAddress := IPAddressFromContext(r.Context())
	h.logAction(r.Context(), &operator.ID, "revoke_all", "session", "", map[string]interface{}{
		"identity_id":      identityID.String(),
		"sessions_revoked": count,
	}, ipAddress)

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success":          true,
		"sessions_revoked": count,
	})
}

// RevokeSession handles DELETE /admin/sessions/{session_id}
func (h *Handler) RevokeSession(w http.ResponseWriter, r *http.Request) {
	operator := OperatorFromContext(r.Context())
	if operator == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required")
		return
	}

	sessionIDStr := chi.URLParam(r, "session_id")
	sessionID, err := uuid.Parse(sessionIDStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id", "Invalid session ID format")
		return
	}

	if err := h.revokeSession(r.Context(), sessionID); err != nil {
		if err == errSessionNotFound {
			writeError(w, http.StatusNotFound, "not_found", "Session not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "Failed to revoke session")
		return
	}

	ipAddress := IPAddressFromContext(r.Context())
	h.logAction(r.Context(), &operator.ID, "revoke", "session", sessionID.String(), nil, ipAddress)

	w.WriteHeader(http.StatusNoContent)
}

var errSessionNotFound = errors.New("session not found")

func (h *Handler) listSessionsForIdentity(ctx context.Context, identityID uuid.UUID, limit, offset int) ([]SessionResponse, int64, error) {
	var total int64
	if err := h.dbConn().QueryRow(ctx, `
		SELECT COUNT(*)
		FROM core_sessions
		WHERE identity_id = $1
		  AND active = TRUE
		  AND expires_at > NOW()
	`, identityID).Scan(&total); err != nil {
		return nil, 0, err
	}

	rows, err := h.dbConn().Query(ctx, `
		SELECT id, aal, active, expires_at, authenticated_at,
		       COALESCE(NULLIF((devices->0->>'ip_address'), ''), ''),
		       COALESCE(NULLIF((devices->0->>'user_agent'), ''), '')
		FROM core_sessions
		WHERE identity_id = $1
		  AND active = TRUE
		  AND expires_at > NOW()
		ORDER BY created_at DESC
		LIMIT $2 OFFSET $3
	`, identityID, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	out := make([]SessionResponse, 0, limit)
	for rows.Next() {
		var item SessionResponse
		if err := rows.Scan(
			&item.ID,
			&item.AAL,
			&item.Active,
			&item.ExpiresAt,
			&item.AuthenticatedAt,
			&item.IPAddress,
			&item.UserAgent,
		); err != nil {
			return nil, 0, err
		}
		out = append(out, item)
	}

	return out, total, rows.Err()
}

func (h *Handler) revokeSessionsForIdentity(ctx context.Context, identityID uuid.UUID) (int64, error) {
	res, err := h.dbConn().Exec(ctx, `
		UPDATE core_sessions
		SET active = FALSE, updated_at = NOW()
		WHERE identity_id = $1
		  AND active = TRUE
		  AND expires_at > NOW()
	`, identityID)
	if err != nil {
		return 0, err
	}

	return res.RowsAffected(), nil
}

func (h *Handler) revokeSession(ctx context.Context, sessionID uuid.UUID) error {
	res, err := h.dbConn().Exec(ctx, `
		UPDATE core_sessions
		SET active = FALSE, updated_at = NOW()
		WHERE id = $1
		  AND active = TRUE
		  AND expires_at > NOW()
	`, sessionID)
	if err != nil {
		return err
	}
	if res.RowsAffected() == 0 {
		var exists bool
		err := h.dbConn().QueryRow(ctx, `
			SELECT EXISTS(
				SELECT 1
				FROM core_sessions
				WHERE id = $1
			)
		`, sessionID).Scan(&exists)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		if !exists {
			return errSessionNotFound
		}
	}

	return nil
}
