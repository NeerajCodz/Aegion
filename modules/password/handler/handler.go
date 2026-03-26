// Package handler provides HTTP handlers for password authentication.
package handler

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/google/uuid"

	"github.com/aegion/aegion/modules/password/service"
)

// Handler handles password authentication HTTP requests.
type Handler struct {
	service *service.Service
}

// New creates a new password handler.
func New(svc *service.Service) *Handler {
	return &Handler{service: svc}
}

// RegisterRequest is the request body for registration.
type RegisterRequest struct {
	Traits struct {
		Email string `json:"email"`
	} `json:"traits"`
	Password string `json:"password"`
}

// LoginRequest is the request body for login.
type LoginRequest struct {
	Identifier string `json:"identifier"`
	Password   string `json:"password"`
}

// ChangePasswordRequest is the request body for password change.
type ChangePasswordRequest struct {
	OldPassword string `json:"old_password"`
	NewPassword string `json:"new_password"`
}

// ErrorResponse is the error response format.
type ErrorResponse struct {
	Error struct {
		Code    int    `json:"code"`
		Status  string `json:"status"`
		Message string `json:"message"`
	} `json:"error"`
}

// SuccessResponse is the success response format.
type SuccessResponse struct {
	Session struct {
		ID         string `json:"id"`
		IdentityID string `json:"identity_id"`
		AAL        string `json:"aal"`
	} `json:"session,omitempty"`
	Identity struct {
		ID     string                 `json:"id"`
		Traits map[string]interface{} `json:"traits"`
	} `json:"identity,omitempty"`
}

// HandleRegistration handles password registration.
func (h *Handler) HandleRegistration(w http.ResponseWriter, r *http.Request) {
	var req RegisterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeError(w, http.StatusBadRequest, "invalid_request", "Invalid request body")
		return
	}

	if req.Traits.Email == "" {
		h.writeError(w, http.StatusBadRequest, "missing_email", "Email is required")
		return
	}

	if req.Password == "" {
		h.writeError(w, http.StatusBadRequest, "missing_password", "Password is required")
		return
	}

	// TODO: Create identity first, then register password
	// For now, this is a placeholder that would integrate with core identity creation

	// Generate identity ID (in real implementation, this comes from core)
	identityID := uuid.New()

	err := h.service.Register(r.Context(), identityID, req.Traits.Email, req.Password)
	if err != nil {
		h.handleServiceError(w, err)
		return
	}

	// Return success (in real implementation, would create session)
	resp := SuccessResponse{}
	resp.Identity.ID = identityID.String()
	resp.Identity.Traits = map[string]interface{}{
		"email": req.Traits.Email,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(resp)
}

// HandleLogin handles password login.
func (h *Handler) HandleLogin(w http.ResponseWriter, r *http.Request) {
	var req LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeError(w, http.StatusBadRequest, "invalid_request", "Invalid request body")
		return
	}

	if req.Identifier == "" || req.Password == "" {
		h.writeError(w, http.StatusBadRequest, "missing_credentials", "Identifier and password are required")
		return
	}

	identityID, err := h.service.Verify(r.Context(), req.Identifier, req.Password)
	if err != nil {
		if errors.Is(err, service.ErrInvalidCredentials) {
			// Use generic error to prevent account enumeration
			h.writeError(w, http.StatusUnauthorized, "invalid_credentials",
				"The provided credentials are invalid. Check for spelling mistakes or use another login method.")
			return
		}
		h.handleServiceError(w, err)
		return
	}

	// TODO: Create session via core session manager
	// For now, return identity ID
	resp := SuccessResponse{}
	resp.Session.IdentityID = identityID.String()
	resp.Session.AAL = "aal1"

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(resp)
}

// HandleChangePassword handles password change.
func (h *Handler) HandleChangePassword(w http.ResponseWriter, r *http.Request) {
	// Get identity from session context
	// TODO: Extract from session context
	identityIDStr := r.Header.Get("X-Aegion-Session-Identity-ID")
	if identityIDStr == "" {
		h.writeError(w, http.StatusUnauthorized, "unauthorized", "Session required")
		return
	}

	identityID, err := uuid.Parse(identityIDStr)
	if err != nil {
		h.writeError(w, http.StatusUnauthorized, "unauthorized", "Invalid session")
		return
	}

	var req ChangePasswordRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeError(w, http.StatusBadRequest, "invalid_request", "Invalid request body")
		return
	}

	err = h.service.ChangePassword(r.Context(), identityID, req.OldPassword, req.NewPassword)
	if err != nil {
		h.handleServiceError(w, err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
	})
}

// handleServiceError converts service errors to HTTP responses.
func (h *Handler) handleServiceError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, service.ErrPasswordTooShort):
		h.writeError(w, http.StatusBadRequest, "password_too_short", "Password must be at least 8 characters")
	case errors.Is(err, service.ErrPasswordTooWeak):
		h.writeError(w, http.StatusBadRequest, "password_too_weak", "Password does not meet complexity requirements")
	case errors.Is(err, service.ErrPasswordBreached):
		h.writeError(w, http.StatusBadRequest, "password_breached", "This password has been found in a data breach. Please choose a different password.")
	case errors.Is(err, service.ErrPasswordReused):
		h.writeError(w, http.StatusBadRequest, "password_reused", "This password was used recently. Please choose a different password.")
	case errors.Is(err, service.ErrPasswordSimilar):
		h.writeError(w, http.StatusBadRequest, "password_similar", "Password is too similar to your email or username")
	case errors.Is(err, service.ErrInvalidCredentials):
		h.writeError(w, http.StatusUnauthorized, "invalid_credentials", "The provided credentials are invalid")
	case errors.Is(err, service.ErrIdentityNotFound):
		h.writeError(w, http.StatusNotFound, "identity_not_found", "Identity not found")
	default:
		h.writeError(w, http.StatusInternalServerError, "internal_error", "An internal error occurred")
	}
}

// writeError writes an error response.
func (h *Handler) writeError(w http.ResponseWriter, status int, code, message string) {
	resp := ErrorResponse{}
	resp.Error.Code = status
	resp.Error.Status = code
	resp.Error.Message = message

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(resp)
}
