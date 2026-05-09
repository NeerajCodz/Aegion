// Package handler provides HTTP handlers for password authentication.
package handler

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"

	coresession "github.com/aegion/aegion/core/session"
	"github.com/google/uuid"

	"github.com/aegion/aegion/modules/password/service"
)

const maxJSONBodyBytes int64 = 1 << 20

// Service defines the password service behavior needed by handlers.
type Service interface {
	ValidatePassword(ctx context.Context, password, identifier string) error
	Register(ctx context.Context, identityID uuid.UUID, identifier, password string) error
	Verify(ctx context.Context, identifier, password string) (uuid.UUID, error)
	ChangePassword(ctx context.Context, identityID uuid.UUID, oldPassword, newPassword string) error
}

// IdentityStore provisions identities in core.
type IdentityStore interface {
	CreateIdentity(ctx context.Context, traits map[string]interface{}) (uuid.UUID, error)
}

// SessionManager creates and manages core sessions.
type SessionManager interface {
	Create(ctx context.Context, identityID uuid.UUID, method coresession.AuthMethod, device coresession.DeviceInfo) (*coresession.Session, error)
	SetCookie(w http.ResponseWriter, session *coresession.Session)
}

// Option configures handler integrations.
type Option func(*Handler)

// WithIdentityStore configures identity provisioning integration.
func WithIdentityStore(identityStore IdentityStore) Option {
	return func(h *Handler) {
		h.identityStore = identityStore
	}
}

// WithSessionManager configures session creation integration.
func WithSessionManager(sessionManager SessionManager) Option {
	return func(h *Handler) {
		h.sessionManager = sessionManager
	}
}

// WithSessionHeaderSecret enables verification of signed session context headers.
func WithSessionHeaderSecret(secret []byte) Option {
	return func(h *Handler) {
		if len(secret) == 0 {
			h.sessionHeaderSecret = nil
			return
		}
		h.sessionHeaderSecret = append([]byte(nil), secret...)
	}
}

// WithLegacyIdentityHeaderAuth controls whether unsigned identity header fallback is allowed.
func WithLegacyIdentityHeaderAuth(enabled bool) Option {
	return func(h *Handler) {
		h.allowLegacyIdentityHeaderAuth = enabled
	}
}

// Handler handles password authentication HTTP requests.
type Handler struct {
	service                       Service
	identityStore                 IdentityStore
	sessionManager                SessionManager
	sessionHeaderSecret           []byte
	allowLegacyIdentityHeaderAuth bool
}

// New creates a new password handler.
func New(svc Service, opts ...Option) *Handler {
	h := &Handler{service: svc}
	for _, opt := range opts {
		if opt != nil {
			opt(h)
		}
	}
	return h
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
	if err := decodeJSONBody(w, r, &req); err != nil {
		h.writeError(w, http.StatusBadRequest, "invalid_request", "Invalid request body")
		return
	}

	email := strings.ToLower(strings.TrimSpace(req.Traits.Email))
	if email == "" {
		h.writeError(w, http.StatusBadRequest, "missing_email", "Email is required")
		return
	}

	if req.Password == "" {
		h.writeError(w, http.StatusBadRequest, "missing_password", "Password is required")
		return
	}

	if err := h.service.ValidatePassword(r.Context(), req.Password, email); err != nil {
		h.handleServiceError(w, err)
		return
	}

	identityID, err := h.resolveRegistrationIdentityID(r.Context(), email)
	if err != nil {
		h.writeError(w, http.StatusInternalServerError, "internal_error", "An internal error occurred")
		return
	}

	err = h.service.Register(r.Context(), identityID, email, req.Password)
	if err != nil {
		h.handleServiceError(w, err)
		return
	}

	resp := SuccessResponse{}
	resp.Identity.ID = identityID.String()
	resp.Identity.Traits = map[string]interface{}{
		"email": email,
	}

	if err := h.createSession(r.Context(), w, r, identityID, &resp); err != nil {
		h.writeError(w, http.StatusInternalServerError, "internal_error", "An internal error occurred")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(resp)
}

// HandleLogin handles password login.
func (h *Handler) HandleLogin(w http.ResponseWriter, r *http.Request) {
	var req LoginRequest
	if err := decodeJSONBody(w, r, &req); err != nil {
		h.writeError(w, http.StatusBadRequest, "invalid_request", "Invalid request body")
		return
	}

	identifier := strings.TrimSpace(req.Identifier)
	if identifier == "" || req.Password == "" {
		h.writeError(w, http.StatusBadRequest, "missing_credentials", "Identifier and password are required")
		return
	}

	identityID, err := h.service.Verify(r.Context(), identifier, req.Password)
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

	resp := SuccessResponse{}
	if err := h.createSession(r.Context(), w, r, identityID, &resp); err != nil {
		h.writeError(w, http.StatusInternalServerError, "internal_error", "An internal error occurred")
		return
	}

	payload, err := json.Marshal(resp)
	if err != nil {
		h.writeError(w, http.StatusInternalServerError, "internal_error", "An internal error occurred")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(append(payload, '\n'))
}

// HandleChangePassword handles password change.
func (h *Handler) HandleChangePassword(w http.ResponseWriter, r *http.Request) {
	identityID, err := h.identityIDFromRequest(r)
	if err != nil {
		h.writeError(w, http.StatusUnauthorized, "unauthorized", "Session required")
		return
	}

	var req ChangePasswordRequest
	if err := decodeJSONBody(w, r, &req); err != nil {
		h.writeError(w, http.StatusBadRequest, "invalid_request", "Invalid request body")
		return
	}

	err = h.service.ChangePassword(r.Context(), identityID, req.OldPassword, req.NewPassword)
	if err != nil {
		h.handleServiceError(w, err)
		return
	}

	payload, err := json.Marshal(map[string]interface{}{
		"success": true,
	})
	if err != nil {
		h.writeError(w, http.StatusInternalServerError, "internal_error", "An internal error occurred")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(append(payload, '\n'))
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

	payload, err := json.Marshal(resp)
	if err != nil {
		http.Error(w, message, status)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(append(payload, '\n'))
}

func (h *Handler) resolveRegistrationIdentityID(ctx context.Context, email string) (uuid.UUID, error) {
	if h.identityStore == nil {
		return uuid.Nil, errors.New("identity store unavailable")
	}
	return h.identityStore.CreateIdentity(ctx, map[string]interface{}{"email": email})
}

func (h *Handler) createSession(ctx context.Context, w http.ResponseWriter, r *http.Request, identityID uuid.UUID, resp *SuccessResponse) error {
	if h.sessionManager == nil {
		return errors.New("session manager unavailable")
	}

	session, err := h.sessionManager.Create(
		ctx,
		identityID,
		coresession.AuthMethodPassword,
		coresession.DeviceInfo{
			UserAgent: r.UserAgent(),
			IPAddress: requestIP(r),
		},
	)
	if err != nil {
		return err
	}

	if session != nil {
		h.sessionManager.SetCookie(w, session)
		resp.Session.ID = session.ID.String()
		resp.Session.IdentityID = session.IdentityID.String()
		resp.Session.AAL = string(session.AAL)
		return nil
	}

	resp.Session.IdentityID = identityID.String()
	resp.Session.AAL = string(coresession.AAL1)
	return nil
}

func (h *Handler) identityIDFromRequest(r *http.Request) (uuid.UUID, error) {
	if sessionCtx := coresession.GetContext(r.Context()); sessionCtx != nil && sessionCtx.IdentityID != uuid.Nil {
		return sessionCtx.IdentityID, nil
	}

	if len(h.sessionHeaderSecret) > 0 && hasSignedSessionContextHeaders(r) {
		sessionCtx, err := coresession.VerifyHeaders(r, h.sessionHeaderSecret)
		if err != nil || sessionCtx == nil || sessionCtx.IdentityID == uuid.Nil {
			return uuid.Nil, errors.New("invalid signed session context")
		}
		return sessionCtx.IdentityID, nil
	}

	if !h.allowLegacyIdentityHeaderAuth {
		return uuid.Nil, errors.New("identity context missing")
	}

	for _, header := range []string{"X-Aegion-Session-Identity-ID", "X-Aegion-Identity-ID", "X-User-ID"} {
		raw := strings.TrimSpace(r.Header.Get(header))
		if raw == "" {
			continue
		}
		identityID, err := uuid.Parse(raw)
		if err != nil {
			return uuid.Nil, err
		}
		return identityID, nil
	}

	return uuid.Nil, errors.New("identity header missing")
}

func hasSignedSessionContextHeaders(r *http.Request) bool {
	for _, suffix := range []string{"Session-ID", "Identity-ID", "AAL", "Signature"} {
		if strings.TrimSpace(r.Header.Get(coresession.HeaderPrefix+suffix)) != "" {
			return true
		}
	}
	return false
}

func requestIP(r *http.Request) string {
	if xff := strings.TrimSpace(r.Header.Get("X-Forwarded-For")); xff != "" {
		parts := strings.Split(xff, ",")
		if len(parts) > 0 {
			return strings.TrimSpace(parts[0])
		}
	}
	if xri := strings.TrimSpace(r.Header.Get("X-Real-IP")); xri != "" {
		return xri
	}
	host, _, err := net.SplitHostPort(strings.TrimSpace(r.RemoteAddr))
	if err == nil {
		return strings.Trim(host, "[]")
	}
	return strings.TrimSpace(r.RemoteAddr)
}

func decodeJSONBody(w http.ResponseWriter, r *http.Request, dst interface{}) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBodyBytes)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return err
	}
	var extra struct{}
	if err := dec.Decode(&extra); err != io.EOF {
		return errors.New("invalid request body")
	}
	return nil
}
