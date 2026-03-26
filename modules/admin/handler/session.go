// Package handler provides session management handlers for the admin module.
package handler

import (
	"context"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
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

	page, perPage, offset := parsePagination(r)

	// Get sessions for identity
	sessions, total, err := h.listSessionsForIdentity(r.Context(), identityID, perPage, offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Failed to list sessions")
		return
	}

	// Log the action
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

	// Revoke all sessions for identity
	count, err := h.revokeSessionsForIdentity(r.Context(), identityID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Failed to revoke sessions")
		return
	}

	// Log the action
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

	// Revoke the session
	if err := h.revokeSession(r.Context(), sessionID); err != nil {
		if err == errSessionNotFound {
			writeError(w, http.StatusNotFound, "not_found", "Session not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "Failed to revoke session")
		return
	}

	// Log the action
	ipAddress := IPAddressFromContext(r.Context())
	h.logAction(r.Context(), &operator.ID, "revoke", "session", sessionID.String(), nil, ipAddress)

	w.WriteHeader(http.StatusNoContent)
}

// Internal error for session not found
var errSessionNotFound = errors.New("session not found")

// Placeholder functions for session operations
// These would integrate with the core session module

func (h *Handler) listSessionsForIdentity(ctx context.Context, identityID uuid.UUID, limit, offset int) ([]SessionResponse, int64, error) {
	// TODO: Integrate with core session module
	// This would query the core_sessions table for the identity
	return []SessionResponse{}, 0, nil
}

func (h *Handler) revokeSessionsForIdentity(ctx context.Context, identityID uuid.UUID) (int64, error) {
	// TODO: Integrate with core session module
	// This would update core_sessions set active = FALSE for the identity
	return 0, nil
}

func (h *Handler) revokeSession(ctx context.Context, sessionID uuid.UUID) error {
	// TODO: Integrate with core session module
	// This would update core_sessions set active = FALSE for the session
	return nil
}
