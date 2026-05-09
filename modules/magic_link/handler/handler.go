// Package handler provides HTTP handlers for magic link authentication.
package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"

	coresession "github.com/aegion/aegion/core/session"
	"github.com/google/uuid"

	"github.com/aegion/aegion/modules/magic_link/service"
	"github.com/aegion/aegion/modules/magic_link/store"
)

const maxJSONBodyBytes int64 = 1 << 20

// Service defines the magic link service behavior needed by handlers.
type Service interface {
	SendLoginCode(ctx context.Context, email string) error
	VerifyCode(ctx context.Context, email, otpCode string) (string, *uuid.UUID, error)
	VerifyMagicLink(ctx context.Context, token string) (string, *uuid.UUID, error)
	VerifyMagicLinkForType(ctx context.Context, token string, expectedType store.CodeType) (string, *uuid.UUID, error)
	SendVerificationCode(ctx context.Context, email string, identityID uuid.UUID) error
	VerifyVerificationCode(ctx context.Context, email, otpCode string) (*uuid.UUID, error)
	SendRecoveryCodeIfIdentityExists(ctx context.Context, email string, identityID *uuid.UUID) error
	VerifyRecoveryCode(ctx context.Context, email, otpCode string) (*uuid.UUID, error)
}

// IdentityStore resolves identities from core identity data.
type IdentityStore interface {
	GetIdentityByEmail(ctx context.Context, email string) (*uuid.UUID, error)
	MarkEmailVerified(ctx context.Context, identityID uuid.UUID, email string) error
}

// SessionManager creates and manages core sessions.
type SessionManager interface {
	Create(ctx context.Context, identityID uuid.UUID, method coresession.AuthMethod, device coresession.DeviceInfo) (*coresession.Session, error)
	SetCookie(w http.ResponseWriter, session *coresession.Session)
}

// Option configures handler integrations.
type Option func(*Handler)

// WithIdentityStore configures identity resolution/verification integration.
func WithIdentityStore(identityStore IdentityStore) Option {
	return func(h *Handler) {
		h.identityStore = identityStore
	}
}

// WithSessionManager configures core session integration.
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

// Handler handles magic link HTTP requests.
type Handler struct {
	service                       Service
	identityStore                 IdentityStore
	sessionManager                SessionManager
	sessionHeaderSecret           []byte
	allowLegacyIdentityHeaderAuth bool
}

// New creates a new magic link handler.
func New(svc Service, opts ...Option) *Handler {
	h := &Handler{service: svc}
	for _, opt := range opts {
		if opt != nil {
			opt(h)
		}
	}
	return h
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
	if err := decodeJSONBody(w, r, &req); err != nil {
		h.writeError(w, http.StatusBadRequest, "invalid_request", "Invalid request body")
		return
	}

	req.Email = strings.TrimSpace(req.Email)
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
	if err := decodeJSONBody(w, r, &req); err != nil {
		h.writeError(w, http.StatusBadRequest, "invalid_request", "Invalid request body")
		return
	}

	req.Email = strings.TrimSpace(req.Email)
	req.Code = strings.TrimSpace(req.Code)
	if req.Email == "" || req.Code == "" {
		h.writeError(w, http.StatusBadRequest, "missing_fields", "Email and code are required")
		return
	}

	switch h.codeTypeFromRequest(r) {
	case store.CodeTypeVerification:
		identityID, err := h.service.VerifyVerificationCode(r.Context(), req.Email, req.Code)
		if err != nil {
			h.handleServiceError(w, err)
			return
		}
		h.handleVerificationSuccess(r.Context(), w, req.Email, identityID)
	case store.CodeTypeRecovery:
		identityID, err := h.service.VerifyRecoveryCode(r.Context(), req.Email, req.Code)
		if err != nil {
			h.handleServiceError(w, err)
			return
		}
		h.handleSessionVerificationSuccess(w, r, req.Email, identityID)
	default:
		recipient, identityID, err := h.service.VerifyCode(r.Context(), req.Email, req.Code)
		if err != nil {
			h.handleServiceError(w, err)
			return
		}
		h.handleSessionVerificationSuccess(w, r, recipient, identityID)
	}
}

// HandleVerifyMagicLink handles verification of a magic link token.
func (h *Handler) HandleVerifyMagicLink(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	if token == "" {
		h.writeError(w, http.StatusBadRequest, "missing_token", "Token is required")
		return
	}

	codeType := h.codeTypeFromRequest(r)
	var (
		recipient  string
		identityID *uuid.UUID
		err        error
	)
	if codeType == store.CodeTypeLogin {
		recipient, identityID, err = h.service.VerifyMagicLink(r.Context(), token)
	} else {
		recipient, identityID, err = h.service.VerifyMagicLinkForType(r.Context(), token, codeType)
	}
	if err != nil {
		h.handleServiceError(w, err)
		return
	}

	switch codeType {
	case store.CodeTypeVerification:
		h.handleVerificationSuccess(r.Context(), w, recipient, identityID)
	case store.CodeTypeRecovery:
		h.handleSessionVerificationSuccess(w, r, recipient, identityID)
	default:
		h.handleSessionVerificationSuccess(w, r, recipient, identityID)
	}
}

// HandleSendVerificationCode handles requests to send a verification code.
func (h *Handler) HandleSendVerificationCode(w http.ResponseWriter, r *http.Request) {
	var req SendCodeRequest
	if err := decodeJSONBody(w, r, &req); err != nil {
		h.writeError(w, http.StatusBadRequest, "invalid_request", "Invalid request body")
		return
	}

	req.Email = strings.TrimSpace(req.Email)
	if req.Email == "" {
		h.writeError(w, http.StatusBadRequest, "missing_email", "Email is required")
		return
	}

	identityID, err := h.identityIDFromRequest(r)
	if err != nil {
		h.writeError(w, http.StatusUnauthorized, "unauthorized", "Active session required")
		return
	}

	if err := h.service.SendVerificationCode(r.Context(), req.Email, identityID); err != nil {
		h.handleServiceError(w, err)
		return
	}

	h.writeSuccess(w, http.StatusOK, "If verification is required for this address, you will receive a verification link.")
}

// HandleSendRecoveryCode handles requests to send a recovery code.
func (h *Handler) HandleSendRecoveryCode(w http.ResponseWriter, r *http.Request) {
	var req SendCodeRequest
	if err := decodeJSONBody(w, r, &req); err != nil {
		h.writeError(w, http.StatusBadRequest, "invalid_request", "Invalid request body")
		return
	}

	req.Email = strings.TrimSpace(req.Email)
	if req.Email == "" {
		h.writeError(w, http.StatusBadRequest, "missing_email", "Email is required")
		return
	}

	var identityID *uuid.UUID
	if h.identityStore != nil {
		resolved, err := h.identityStore.GetIdentityByEmail(r.Context(), req.Email)
		if err != nil {
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}
		identityID = resolved
	}

	if err := h.service.SendRecoveryCodeIfIdentityExists(r.Context(), req.Email, identityID); err != nil {
		h.handleServiceError(w, err)
		return
	}

	h.writeSuccess(w, http.StatusOK, "If an account exists with this email, you will receive a recovery link.")
}

func (h *Handler) codeTypeFromRequest(r *http.Request) store.CodeType {
	path := strings.ToLower(r.URL.Path)
	switch {
	case strings.Contains(path, "/verification/"):
		return store.CodeTypeVerification
	case strings.Contains(path, "/recovery/"):
		return store.CodeTypeRecovery
	default:
		return store.CodeTypeLogin
	}
}

func (h *Handler) handleSessionVerificationSuccess(w http.ResponseWriter, r *http.Request, recipient string, directIdentityID *uuid.UUID) {
	identityID, err := h.resolveIdentityID(r.Context(), recipient, directIdentityID)
	if err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	resp := SuccessResponse{}
	if identityID == nil {
		resp.Message = "Link verified for: " + recipient
		h.writeJSONSuccess(w, resp)
		return
	}

	resp.Session.IdentityID = identityID.String()
	resp.Session.AAL = string(coresession.AAL1)

	if h.sessionManager != nil {
		session, createErr := h.sessionManager.Create(
			r.Context(),
			*identityID,
			coresession.AuthMethodMagicLink,
			coresession.DeviceInfo{
				UserAgent: r.UserAgent(),
				IPAddress: requestIP(r),
			},
		)
		if createErr != nil {
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}

		if session != nil {
			h.sessionManager.SetCookie(w, session)
			resp.Session.ID = session.ID.String()
			resp.Session.IdentityID = session.IdentityID.String()
			resp.Session.AAL = string(session.AAL)
		}
	}

	h.writeJSONSuccess(w, resp)
}

func (h *Handler) handleVerificationSuccess(ctx context.Context, w http.ResponseWriter, email string, identityID *uuid.UUID) {
	if identityID == nil {
		h.writeError(w, http.StatusBadRequest, "invalid_code", "The code is invalid or has expired")
		return
	}

	if h.identityStore != nil {
		if err := h.identityStore.MarkEmailVerified(ctx, *identityID, email); err != nil {
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}
	}

	h.writeSuccess(w, http.StatusOK, "Verification complete.")
}

func (h *Handler) resolveIdentityID(ctx context.Context, recipient string, directIdentityID *uuid.UUID) (*uuid.UUID, error) {
	if directIdentityID != nil {
		return directIdentityID, nil
	}
	if h.identityStore == nil || recipient == "" {
		return nil, nil
	}
	return h.identityStore.GetIdentityByEmail(ctx, recipient)
}

func (h *Handler) writeJSONSuccess(w http.ResponseWriter, resp SuccessResponse) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(resp)
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
		id, err := uuid.Parse(raw)
		if err != nil {
			return uuid.Nil, err
		}
		return id, nil
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
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}

// writeError writes an error response.
func (h *Handler) writeError(w http.ResponseWriter, status int, code, message string) {
	resp := ErrorResponse{}
	resp.Error.Code = status
	resp.Error.Status = code
	resp.Error.Message = message

	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(resp); err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(buf.Bytes())
}

// writeSuccess writes a success response with a message.
func (h *Handler) writeSuccess(w http.ResponseWriter, status int, message string) {
	resp := SuccessResponse{Message: message}

	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(resp); err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(buf.Bytes())
}
