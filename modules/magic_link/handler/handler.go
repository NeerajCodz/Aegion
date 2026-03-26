// Package handler provides HTTP handlers for magic link authentication.
package handler

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/aegion/aegion/modules/magic_link/service"
)

// Handler handles magic link HTTP requests.
type Handler struct {
	service *service.Service
}

// New creates a new magic link handler.
func New(svc *service.Service) *Handler {
	return &Handler{service: svc}
}

// SendCodeRequest is the request body for sending a magic link/code.
type SendCodeRequest struct {
	Email string `json:"email"`
}

// VerifyCodeRequest is the request body for verifying an OTP code.
type VerifyCodeRequest struct {
	Email string `json:"email"`
	Code  string `json:"code"`
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
	Message string `json:"message,omitempty"`
}

// HandleSendLoginCode handles requests to send a magic link/OTP for login.
func (h *Handler) HandleSendLoginCode(w http.ResponseWriter, r *http.Request) {
	var req SendCodeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeError(w, http.StatusBadRequest, "invalid_request", "Invalid request body")
		return
	}

	if req.Email == "" {
		h.writeError(w, http.StatusBadRequest, "missing_email", "Email is required")
		return
	}

	err := h.service.SendLoginCode(r.Context(), req.Email)
	if err != nil {
		h.handleServiceError(w, err)
		return
	}

	// Always return success to prevent account enumeration
	h.writeSuccess(w, http.StatusOK, "If an account exists with this email, you will receive a login link.")
}

// HandleVerifyCode handles verification of an OTP code.
func (h *Handler) HandleVerifyCode(w http.ResponseWriter, r *http.Request) {
	var req VerifyCodeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeError(w, http.StatusBadRequest, "invalid_request", "Invalid request body")
		return
	}

	if req.Email == "" || req.Code == "" {
		h.writeError(w, http.StatusBadRequest, "missing_fields", "Email and code are required")
		return
	}

	recipient, identityID, err := h.service.VerifyCode(r.Context(), req.Email, req.Code)
	if err != nil {
		h.handleServiceError(w, err)
		return
	}

	// TODO: Create session via core session manager
	resp := SuccessResponse{}
	if identityID != nil {
		resp.Session.IdentityID = identityID.String()
	} else {
		// New user - would need to create identity
		resp.Message = "Code verified for: " + recipient
	}
	resp.Session.AAL = "aal1"

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(resp)
}

// HandleVerifyMagicLink handles verification of a magic link token.
func (h *Handler) HandleVerifyMagicLink(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	if token == "" {
		h.writeError(w, http.StatusBadRequest, "missing_token", "Token is required")
		return
	}

	recipient, identityID, err := h.service.VerifyMagicLink(r.Context(), token)
	if err != nil {
		h.handleServiceError(w, err)
		return
	}

	// TODO: Create session and redirect
	resp := SuccessResponse{}
	if identityID != nil {
		resp.Session.IdentityID = identityID.String()
	} else {
		resp.Message = "Link verified for: " + recipient
	}
	resp.Session.AAL = "aal1"

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(resp)
}

// HandleSendVerificationCode handles requests to send a verification code.
func (h *Handler) HandleSendVerificationCode(w http.ResponseWriter, r *http.Request) {
	// Get identity from session
	// TODO: Extract from session context
	h.writeError(w, http.StatusNotImplemented, "not_implemented", "Verification flow not implemented")
}

// HandleSendRecoveryCode handles requests to send a recovery code.
func (h *Handler) HandleSendRecoveryCode(w http.ResponseWriter, r *http.Request) {
	var req SendCodeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeError(w, http.StatusBadRequest, "invalid_request", "Invalid request body")
		return
	}

	if req.Email == "" {
		h.writeError(w, http.StatusBadRequest, "missing_email", "Email is required")
		return
	}

	// TODO: Look up identity by email, then send recovery code
	// For now, this is a placeholder

	h.writeSuccess(w, http.StatusOK, "If an account exists with this email, you will receive a recovery link.")
}

// handleServiceError converts service errors to HTTP responses.
func (h *Handler) handleServiceError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, service.ErrInvalidCode):
		h.writeError(w, http.StatusBadRequest, "invalid_code", "The code is invalid or has expired")
	case errors.Is(err, service.ErrRateLimited):
		h.writeError(w, http.StatusTooManyRequests, "rate_limited", "Too many requests. Please wait before trying again.")
	case errors.Is(err, service.ErrRecipientEmpty):
		h.writeError(w, http.StatusBadRequest, "missing_recipient", "Email or phone number is required")
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

// writeSuccess writes a success response with a message.
func (h *Handler) writeSuccess(w http.ResponseWriter, status int, message string) {
	resp := SuccessResponse{Message: message}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(resp)
}
