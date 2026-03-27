// Package handler provides identity management handlers for the admin module.
package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"

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

	page, perPage, offset := parsePagination(r)
	sort := r.URL.Query().Get("sort")
	filter := r.URL.Query().Get("filter")

	// Query identities from the identity store
	// Note: This would typically call an identity service
	// For now, we simulate with a placeholder
	identities, total, err := h.listIdentitiesFromStore(r.Context(), perPage, offset, sort, filter)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Failed to list identities")
		return
	}

	// Log the action
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

	// Get identity with sessions
	identity, err := h.getIdentityWithSessions(r.Context(), identityID)
	if err != nil {
		if errors.Is(err, errIdentityNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "Identity not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "Failed to get identity")
		return
	}

	// Log the action
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

	// Validate state if provided
	if req.State != nil {
		switch *req.State {
		case IdentityStateActive, IdentityStateInactive, IdentityStateBlocked:
			// Valid states
		default:
			writeError(w, http.StatusBadRequest, "invalid_state", "Invalid identity state")
			return
		}
	}

	// Update identity
	identity, err := h.updateIdentityInStore(r.Context(), identityID, req)
	if err != nil {
		if errors.Is(err, errIdentityNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "Identity not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "Failed to update identity")
		return
	}

	// Log the action
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

	// Soft delete identity (set state to deleted)
	if err := h.softDeleteIdentity(r.Context(), identityID); err != nil {
		if errors.Is(err, errIdentityNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "Identity not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "Failed to delete identity")
		return
	}

	// Revoke all sessions for this identity
	_ = h.revokeAllSessionsForIdentity(r.Context(), identityID)

	// Log the action
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

	page, perPage, offset := parsePagination(r)

	// Search identities
	identities, total, err := h.searchIdentitiesInStore(r.Context(), req, perPage, offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Failed to search identities")
		return
	}

	// Log the action
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

// Internal error for identity not found
var errIdentityNotFound = errors.New("identity not found")

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

	// Best-effort logging
	_ = h.service.Store().LogAction(ctx, entry)
}

// Placeholder functions for identity operations
// These would integrate with the core identity module

func (h *Handler) listIdentitiesFromStore(ctx context.Context, limit, offset int, sort, filter string) ([]IdentityResponse, int64, error) {
	// TODO: Integrate with core identity module
	// This is a placeholder that would query the identities table
	return []IdentityResponse{}, 0, nil
}

func (h *Handler) getIdentityWithSessions(ctx context.Context, identityID uuid.UUID) (*IdentityResponse, error) {
	// TODO: Integrate with core identity module
	// This would fetch the identity and its sessions
	return nil, errIdentityNotFound
}

func (h *Handler) updateIdentityInStore(ctx context.Context, identityID uuid.UUID, req UpdateIdentityRequest) (*IdentityResponse, error) {
	// TODO: Integrate with core identity module
	return nil, errIdentityNotFound
}

func (h *Handler) softDeleteIdentity(ctx context.Context, identityID uuid.UUID) error {
	// TODO: Integrate with core identity module
	return errIdentityNotFound
}

func (h *Handler) revokeAllSessionsForIdentity(ctx context.Context, identityID uuid.UUID) error {
	// TODO: Integrate with core session module
	return nil
}

func (h *Handler) searchIdentitiesInStore(ctx context.Context, req SearchIdentitiesRequest, limit, offset int) ([]IdentityResponse, int64, error) {
	// TODO: Integrate with core identity module
	return []IdentityResponse{}, 0, nil
}
