package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	platformcrypto "github.com/aegion/aegion/internal/platform/crypto"
	"github.com/aegion/aegion/modules/mfa/service"
	"github.com/google/uuid"
)

const maxJSONBodyBytes int64 = 1 << 20

type MFAService interface {
	StartTOTPEnrollment(ctx context.Context, identityID, accountName string) (*service.TOTPEnrollmentStartResponse, error)
	CompleteTOTPEnrollment(ctx context.Context, req *service.TOTPEnrollmentFinishRequest) (*service.TOTPEnrollmentFinishResponse, error)
	VerifyTOTP(ctx context.Context, identityID, code string) error
	VerifyBackupCode(ctx context.Context, identityID, code string) error
	RegenerateBackupCodes(ctx context.Context, identityID string) ([]string, error)
	RememberTrustedDevice(ctx context.Context, identityID, label string) (string, time.Time, error)
	RevokeTrustedDevice(ctx context.Context, identityID, token string) error
}

// Option configures trusted request identity handling.
type Option func(*Handler)

// WithIdentitySigningSecret enables validation of identity context injected by
// the core module proxy.
func WithIdentitySigningSecret(secret []byte) Option {
	return func(h *Handler) {
		h.identitySigningSecret = append(h.identitySigningSecret[:0], secret...)
	}
}

// Handler exposes HTTP routes for the MFA module.
type Handler struct {
	svc                   MFAService
	identitySigningSecret []byte
}

// New creates a new MFA handler.
func New(svc MFAService, opts ...Option) *Handler {
	h := &Handler{svc: svc}
	for _, opt := range opts {
		if opt != nil {
			opt(h)
		}
	}
	return h
}

// RegisterRoutes registers module routes on the provided mux.
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	if mux == nil {
		return
	}
	mux.HandleFunc("/api/v1/mfa/totp/start", h.handleStartTOTPEnrollment)
	mux.HandleFunc("/api/v1/mfa/totp/finish", h.handleFinishTOTPEnrollment)
	mux.HandleFunc("/api/v1/mfa/totp/verify", h.handleVerifyTOTP)
	mux.HandleFunc("/api/v1/mfa/backup/verify", h.handleVerifyBackupCode)
	mux.HandleFunc("/api/v1/mfa/backup/regenerate", h.handleRegenerateBackupCodes)
	mux.HandleFunc("/api/v1/mfa/trusted-device", h.handleTrustedDevice)
}

func (h *Handler) handleStartTOTPEnrollment(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req struct {
		AccountName string `json:"account_name"`
	}
	if err := decodeJSONBody(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	identityID, err := h.identityIDFromRequest(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	resp, err := h.svc.StartTOTPEnrollment(r.Context(), identityID, req.AccountName)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) handleFinishTOTPEnrollment(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req struct {
		EnrollmentID string `json:"enrollment_id"`
		Code         string `json:"code"`
	}
	if err := decodeJSONBody(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	identityID, err := h.identityIDFromRequest(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	resp, err := h.svc.CompleteTOTPEnrollment(r.Context(), &service.TOTPEnrollmentFinishRequest{
		IdentityID:   identityID,
		EnrollmentID: req.EnrollmentID,
		Code:         req.Code,
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) handleVerifyTOTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req struct {
		Code string `json:"code"`
	}
	if err := decodeJSONBody(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	identityID, err := h.identityIDFromRequest(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	if err := h.svc.VerifyTOTP(r.Context(), identityID, req.Code); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "verified"})
}

func (h *Handler) handleVerifyBackupCode(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req struct {
		Code string `json:"code"`
	}
	if err := decodeJSONBody(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	identityID, err := h.identityIDFromRequest(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	if err := h.svc.VerifyBackupCode(r.Context(), identityID, req.Code); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "verified"})
}

func (h *Handler) handleRegenerateBackupCodes(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req struct{}
	if err := decodeJSONBody(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	identityID, err := h.identityIDFromRequest(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	codes, err := h.svc.RegenerateBackupCodes(r.Context(), identityID)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status":       "regenerated",
		"backup_codes": codes,
	})
}

func (h *Handler) handleTrustedDevice(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		var req struct {
			Label string `json:"label"`
		}
		if err := decodeJSONBody(w, r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		identityID, err := h.identityIDFromRequest(r)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "authentication required")
			return
		}
		token, expiresAt, err := h.svc.RememberTrustedDevice(r.Context(), identityID, req.Label)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"status":     "trusted",
			"token":      token,
			"expires_at": expiresAt.UTC(),
		})
	case http.MethodDelete:
		var req struct {
			Token string `json:"token"`
		}
		if err := decodeJSONBody(w, r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		identityID, err := h.identityIDFromRequest(r)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "authentication required")
			return
		}
		if err := h.svc.RevokeTrustedDevice(r.Context(), identityID, req.Token); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"status": "revoked"})
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

const (
	identityIDHeader        = "X-User-ID"
	identitySessionIDHeader = "X-User-Session-ID"
	identityAALHeader       = "X-User-AAL"
	identitySignatureHeader = "X-Aegion-Signature"
	identityContextMaxAge   = time.Minute
)

var signedIdentityHeaders = []string{
	identityIDHeader,
	identitySessionIDHeader,
	identityAALHeader,
}

func (h *Handler) identityIDFromRequest(r *http.Request) (string, error) {
	if len(h.identitySigningSecret) == 0 {
		return "", errors.New("identity context verifier is not configured")
	}
	if !platformcrypto.VerifyIdentityHeaders(
		h.identitySigningSecret,
		r.Header,
		signedIdentityHeaders,
		r.Header.Get(identitySignatureHeader),
		identityContextMaxAge,
		time.Now().UTC(),
	) {
		return "", errors.New("invalid signed identity context")
	}
	identityID, err := uuid.Parse(strings.TrimSpace(r.Header.Get(identityIDHeader)))
	if err != nil || identityID == uuid.Nil {
		return "", errors.New("invalid signed identity")
	}
	if _, err := uuid.Parse(strings.TrimSpace(r.Header.Get(identitySessionIDHeader))); err != nil {
		return "", errors.New("invalid signed session")
	}
	if strings.TrimSpace(r.Header.Get(identityAALHeader)) == "" {
		return "", errors.New("invalid signed authentication assurance level")
	}
	return identityID.String(), nil
}

func decodeJSONBody(w http.ResponseWriter, r *http.Request, dst interface{}) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBodyBytes)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		return err
	}
	var extra struct{}
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return errors.New("invalid request body")
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(v); err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(buf.Bytes())
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]any{
		"error": map[string]any{
			"code":    status,
			"message": strings.TrimSpace(message),
		},
	})
}
