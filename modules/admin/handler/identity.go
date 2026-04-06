// Package handler provides identity management handlers for the admin module.
package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/aegion/aegion/modules/admin/store"
)

// IdentityState represents the state of an identity.
type IdentityState string

const (
	IdentityStateActive   IdentityState = "active"
	IdentityStateInactive IdentityState = "inactive"
	IdentityStateBlocked  IdentityState = "blocked"
	IdentityStateDeleted  IdentityState = "deleted"
)

// IdentityResponse represents an identity in API responses.
type IdentityResponse struct {
	ID        string                 `json:"id"`
	SchemaID  string                 `json:"schema_id,omitempty"`
	Traits    map[string]interface{} `json:"traits"`
	State     IdentityState          `json:"state"`
	CreatedAt time.Time              `json:"created_at"`
	UpdatedAt time.Time              `json:"updated_at"`
	Sessions  []SessionResponse      `json:"sessions,omitempty"`
}

// SessionResponse represents a session in API responses.
type SessionResponse struct {
	ID              string    `json:"id"`
	AAL             string    `json:"aal"`
	Active          bool      `json:"active"`
	ExpiresAt       time.Time `json:"expires_at"`
	AuthenticatedAt time.Time `json:"authenticated_at"`
	IPAddress       string    `json:"ip_address,omitempty"`
	UserAgent       string    `json:"user_agent,omitempty"`
}

// UpdateIdentityRequest is the request body for updating an identity.
type UpdateIdentityRequest struct {
	Traits map[string]interface{} `json:"traits,omitempty"`
	State  *IdentityState         `json:"state,omitempty"`
}

// SearchIdentitiesRequest is the request body for searching identities.
type SearchIdentitiesRequest struct {
	Email  string                 `json:"email,omitempty"`
	Traits map[string]interface{} `json:"traits,omitempty"`
	State  *IdentityState         `json:"state,omitempty"`
}

// ListIdentities handles GET /admin/identities
func (h *Handler) ListIdentities(w http.ResponseWriter, r *http.Request) {
	operator := OperatorFromContext(r.Context())
	if operator == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required")
		return
	}

	page, perPage, offset := h.parsePagination(r)
	sort := r.URL.Query().Get("sort")
	filter := r.URL.Query().Get("filter")

	identities, total, err := h.listIdentitiesFromStore(r.Context(), perPage, offset, sort, filter)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Failed to list identities")
		return
	}

	ipAddress := IPAddressFromContext(r.Context())
	h.logAction(r.Context(), &operator.ID, "list", "identity", "", map[string]interface{}{
		"page":     page,
		"per_page": perPage,
		"sort":     sort,
		"filter":   filter,
	}, ipAddress)

	resp := ListResponse{
		Items:      identities,
		Pagination: buildPaginationMeta(page, perPage, total),
	}
	writeJSON(w, http.StatusOK, resp)
}

// GetIdentity handles GET /admin/identities/{id}
func (h *Handler) GetIdentity(w http.ResponseWriter, r *http.Request) {
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

	identity, err := h.getIdentityWithSessions(r.Context(), identityID)
	if err != nil {
		if errors.Is(err, errIdentityNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "Identity not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "Failed to get identity")
		return
	}

	ipAddress := IPAddressFromContext(r.Context())
	h.logAction(r.Context(), &operator.ID, "read", "identity", identityID.String(), nil, ipAddress)

	writeJSON(w, http.StatusOK, identity)
}

// UpdateIdentity handles PATCH /admin/identities/{id}
func (h *Handler) UpdateIdentity(w http.ResponseWriter, r *http.Request) {
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

	var req UpdateIdentityRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "Invalid request body")
		return
	}

	if req.State != nil {
		switch *req.State {
		case IdentityStateActive, IdentityStateInactive, IdentityStateBlocked:
		default:
			writeError(w, http.StatusBadRequest, "invalid_state", "Invalid identity state")
			return
		}
	}

	identity, err := h.updateIdentityInStore(r.Context(), identityID, req)
	if err != nil {
		if errors.Is(err, errIdentityNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "Identity not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "Failed to update identity")
		return
	}

	ipAddress := IPAddressFromContext(r.Context())
	details := map[string]interface{}{}
	if req.Traits != nil {
		details["traits_updated"] = true
	}
	if req.State != nil {
		details["new_state"] = *req.State
	}
	h.logAction(r.Context(), &operator.ID, "update", "identity", identityID.String(), details, ipAddress)

	writeJSON(w, http.StatusOK, identity)
}

// DeleteIdentity handles DELETE /admin/identities/{id}
func (h *Handler) DeleteIdentity(w http.ResponseWriter, r *http.Request) {
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

	if err := h.softDeleteIdentity(r.Context(), identityID); err != nil {
		if errors.Is(err, errIdentityNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "Identity not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "Failed to delete identity")
		return
	}

	_, _ = h.revokeSessionsForIdentity(r.Context(), identityID)

	ipAddress := IPAddressFromContext(r.Context())
	h.logAction(r.Context(), &operator.ID, "delete", "identity", identityID.String(), map[string]interface{}{
		"soft_delete": true,
	}, ipAddress)

	w.WriteHeader(http.StatusNoContent)
}

// SearchIdentities handles POST /admin/identities/search
func (h *Handler) SearchIdentities(w http.ResponseWriter, r *http.Request) {
	operator := OperatorFromContext(r.Context())
	if operator == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required")
		return
	}

	var req SearchIdentitiesRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "Invalid request body")
		return
	}

	page, perPage, offset := h.parsePagination(r)

	identities, total, err := h.searchIdentitiesInStore(r.Context(), req, perPage, offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Failed to search identities")
		return
	}

	ipAddress := IPAddressFromContext(r.Context())
	h.logAction(r.Context(), &operator.ID, "search", "identity", "", map[string]interface{}{
		"email":  req.Email,
		"traits": req.Traits != nil,
		"state":  req.State,
	}, ipAddress)

	resp := ListResponse{
		Items:      identities,
		Pagination: buildPaginationMeta(page, perPage, total),
	}
	writeJSON(w, http.StatusOK, resp)
}

// SuspendIdentity handles POST /admin/identities/{id}/suspend.
func (h *Handler) SuspendIdentity(w http.ResponseWriter, r *http.Request) {
	h.patchIdentityState(w, r, IdentityStateBlocked, "suspend")
}

// ActivateIdentity handles POST /admin/identities/{id}/activate.
func (h *Handler) ActivateIdentity(w http.ResponseWriter, r *http.Request) {
	h.patchIdentityState(w, r, IdentityStateActive, "activate")
}

// ResetIdentityMFA handles POST /admin/identities/{id}/reset-mfa.
func (h *Handler) ResetIdentityMFA(w http.ResponseWriter, r *http.Request) {
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

	_, err = h.dbConn().Exec(r.Context(), `
		UPDATE core_sessions
		SET aal = 'aal1', updated_at = NOW()
		WHERE identity_id = $1
		  AND active = TRUE
		  AND expires_at > NOW()
		  AND aal = 'aal2'
	`, identityID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Failed to reset MFA state")
		return
	}

	ipAddress := IPAddressFromContext(r.Context())
	h.logAction(r.Context(), &operator.ID, "update", "identity_mfa", identityID.String(), map[string]interface{}{
		"action": "reset_mfa",
	}, ipAddress)

	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) patchIdentityState(w http.ResponseWriter, r *http.Request, state IdentityState, action string) {
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

	_, err = h.updateIdentityInStore(r.Context(), identityID, UpdateIdentityRequest{State: &state})
	if err != nil {
		if errors.Is(err, errIdentityNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "Identity not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "Failed to update identity")
		return
	}

	ipAddress := IPAddressFromContext(r.Context())
	h.logAction(r.Context(), &operator.ID, action, "identity", identityID.String(), map[string]interface{}{
		"new_state": state,
	}, ipAddress)
	w.WriteHeader(http.StatusNoContent)
}

// Internal error for identity not found.
var errIdentityNotFound = errors.New("identity not found")

func (h *Handler) listIdentitiesFromStore(ctx context.Context, limit, offset int, sort, filter string) ([]IdentityResponse, int64, error) {
	sortExpr := identitySortExpr(sort)
	where := "WHERE ci.deleted_at IS NULL"
	args := []interface{}{}
	argPos := 1

	filter = strings.TrimSpace(filter)
	if filter != "" {
		where += `
		 AND (
			LOWER(COALESCE(
				(SELECT value FROM core_identity_addresses a
				 WHERE a.identity_id = ci.id AND a.type = 'email'
				 ORDER BY a.is_primary DESC, a.created_at ASC LIMIT 1),
				ci.traits->>'email',
				''
			)) LIKE LOWER($` + strconv.Itoa(argPos) + `)
			OR LOWER(COALESCE(ci.traits->>'display_name', ci.traits->>'name', '')) LIKE LOWER($` + strconv.Itoa(argPos) + `)
		 )`
		args = append(args, "%"+filter+"%")
		argPos++
	}

	var total int64
	if err := h.dbConn().QueryRow(ctx, `
		SELECT COUNT(*)
		FROM core_identities ci
	`+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	query := `
		SELECT ci.id, ci.schema_id, ci.traits, ci.state, ci.created_at, ci.updated_at
		FROM core_identities ci
	` + where + `
		ORDER BY ` + sortExpr + `
		LIMIT $` + strconv.Itoa(argPos) + ` OFFSET $` + strconv.Itoa(argPos+1)
	args = append(args, limit, offset)

	rows, err := h.dbConn().Query(ctx, query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	items := make([]IdentityResponse, 0, limit)
	for rows.Next() {
		identity, err := scanIdentityRow(rows)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, *identity)
	}

	return items, total, rows.Err()
}

func (h *Handler) getIdentityWithSessions(ctx context.Context, identityID uuid.UUID) (*IdentityResponse, error) {
	var (
		rawTraits []byte
		state     string
	)
	resp := &IdentityResponse{}
	schemaID := uuid.Nil

	err := h.dbConn().QueryRow(ctx, `
		SELECT id, schema_id, traits, state, created_at, updated_at
		FROM core_identities
		WHERE id = $1
		  AND deleted_at IS NULL
	`, identityID).Scan(&resp.ID, &schemaID, &rawTraits, &state, &resp.CreatedAt, &resp.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, errIdentityNotFound
		}
		return nil, err
	}

	resp.SchemaID = schemaID.String()
	resp.State = mapDBStateToAPI(state)
	if err := json.Unmarshal(rawTraits, &resp.Traits); err != nil {
		resp.Traits = map[string]interface{}{}
	}

	sessions, _, err := h.listSessionsForIdentity(ctx, identityID, 50, 0)
	if err == nil {
		resp.Sessions = sessions
	}

	return resp, nil
}

func (h *Handler) updateIdentityInStore(ctx context.Context, identityID uuid.UUID, req UpdateIdentityRequest) (*IdentityResponse, error) {
	if req.Traits == nil && req.State == nil {
		return h.getIdentityWithSessions(ctx, identityID)
	}

	setClauses := []string{"updated_at = NOW()"}
	args := []interface{}{}
	argPos := 1

	if req.Traits != nil {
		rawTraits, err := json.Marshal(req.Traits)
		if err != nil {
			return nil, err
		}
		setClauses = append(setClauses, "traits = COALESCE(traits, '{}'::jsonb) || $"+strconv.Itoa(argPos)+"::jsonb")
		args = append(args, string(rawTraits))
		argPos++
	}

	if req.State != nil {
		normalizedState := normalizeDBState(*req.State)
		setClauses = append(setClauses, "state = $"+strconv.Itoa(argPos))
		args = append(args, normalizedState)
		argPos++
	}

	query := `
		UPDATE core_identities
		SET ` + strings.Join(setClauses, ", ") + `
		WHERE id = $` + strconv.Itoa(argPos) + `
		  AND deleted_at IS NULL
	`
	args = append(args, identityID)

	res, err := h.dbConn().Exec(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	if res.RowsAffected() == 0 {
		return nil, errIdentityNotFound
	}

	return h.getIdentityWithSessions(ctx, identityID)
}

func (h *Handler) softDeleteIdentity(ctx context.Context, identityID uuid.UUID) error {
	res, err := h.dbConn().Exec(ctx, `
		UPDATE core_identities
		SET state = 'inactive', deleted_at = NOW(), updated_at = NOW()
		WHERE id = $1
		  AND deleted_at IS NULL
	`, identityID)
	if err != nil {
		return err
	}
	if res.RowsAffected() == 0 {
		return errIdentityNotFound
	}

	return nil
}

func (h *Handler) searchIdentitiesInStore(ctx context.Context, req SearchIdentitiesRequest, limit, offset int) ([]IdentityResponse, int64, error) {
	where := "WHERE ci.deleted_at IS NULL"
	args := []interface{}{}
	argPos := 1

	if email := strings.TrimSpace(req.Email); email != "" {
		where += `
		 AND LOWER(COALESCE(
			(SELECT value FROM core_identity_addresses a
			 WHERE a.identity_id = ci.id AND a.type = 'email'
			 ORDER BY a.is_primary DESC, a.created_at ASC LIMIT 1),
			ci.traits->>'email',
			''
		 )) LIKE LOWER($` + strconv.Itoa(argPos) + `)
		`
		args = append(args, "%"+email+"%")
		argPos++
	}

	if req.State != nil {
		where += " AND ci.state = $" + strconv.Itoa(argPos)
		args = append(args, normalizeDBState(*req.State))
		argPos++
	}

	if len(req.Traits) > 0 {
		rawTraits, err := json.Marshal(req.Traits)
		if err != nil {
			return nil, 0, err
		}
		where += " AND ci.traits @> $" + strconv.Itoa(argPos) + "::jsonb"
		args = append(args, string(rawTraits))
		argPos++
	}

	var total int64
	if err := h.dbConn().QueryRow(ctx, `
		SELECT COUNT(*)
		FROM core_identities ci
	`+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	query := `
		SELECT ci.id, ci.schema_id, ci.traits, ci.state, ci.created_at, ci.updated_at
		FROM core_identities ci
	` + where + `
		ORDER BY ci.created_at DESC
		LIMIT $` + strconv.Itoa(argPos) + ` OFFSET $` + strconv.Itoa(argPos+1)
	args = append(args, limit, offset)

	rows, err := h.dbConn().Query(ctx, query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	items := make([]IdentityResponse, 0, limit)
	for rows.Next() {
		identity, err := scanIdentityRow(rows)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, *identity)
	}

	return items, total, rows.Err()
}

func identitySortExpr(sort string) string {
	switch strings.TrimSpace(sort) {
	case "created_at":
		return "ci.created_at ASC"
	case "-created_at":
		return "ci.created_at DESC"
	case "updated_at":
		return "ci.updated_at ASC"
	case "-updated_at":
		return "ci.updated_at DESC"
	case "state":
		return "ci.state ASC, ci.created_at DESC"
	case "-state":
		return "ci.state DESC, ci.created_at DESC"
	default:
		return "ci.created_at DESC"
	}
}

func scanIdentityRow(scanner interface {
	Scan(dest ...interface{}) error
}) (*IdentityResponse, error) {
	resp := &IdentityResponse{}
	schemaID := uuid.Nil
	rawTraits := []byte{}
	state := ""

	if err := scanner.Scan(&resp.ID, &schemaID, &rawTraits, &state, &resp.CreatedAt, &resp.UpdatedAt); err != nil {
		return nil, err
	}

	resp.SchemaID = schemaID.String()
	resp.State = mapDBStateToAPI(state)
	if err := json.Unmarshal(rawTraits, &resp.Traits); err != nil {
		resp.Traits = map[string]interface{}{}
	}

	return resp, nil
}

func normalizeDBState(state IdentityState) string {
	switch state {
	case IdentityStateBlocked:
		return "banned"
	case IdentityStateDeleted:
		return "inactive"
	default:
		return string(state)
	}
}

func mapDBStateToAPI(state string) IdentityState {
	switch strings.ToLower(strings.TrimSpace(state)) {
	case "banned":
		return IdentityStateBlocked
	case "inactive":
		return IdentityStateInactive
	default:
		return IdentityStateActive
	}
}

// logAction is a helper to log admin actions.
func (h *Handler) logAction(ctx context.Context, operatorID *uuid.UUID, action, resourceType, resourceID string, details map[string]interface{}, ipAddress string) {
	if details == nil {
		details = make(map[string]interface{})
	}

	if requestID := middleware.GetReqID(ctx); requestID != "" {
		details["request_id"] = requestID
	}

	entry := &store.AuditLogEntry{
		ID:           uuid.New(),
		OperatorID:   operatorID,
		Action:       action,
		ResourceType: resourceType,
		ResourceID:   resourceID,
		Details:      details,
		IPAddress:    ipAddress,
		CreatedAt:    time.Now().UTC(),
	}

	_ = h.service.Store().LogAction(ctx, entry)
}
