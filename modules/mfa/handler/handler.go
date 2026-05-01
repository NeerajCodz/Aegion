package handler

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/aegion/aegion/modules/mfa/service"
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

// Handler exposes HTTP routes for the MFA module.
type Handler struct {
	svc MFAService
}

// New creates a new MFA handler.
func New(svc MFAService) *Handler {
	return &Handler{svc: svc}
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
		IdentityID  string `json:"identity_id"`
		AccountName string `json:"account_name"`
	}
	if err := decodeJSONBody(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	resp, err := h.svc.StartTOTPEnrollment(r.Context(), req.IdentityID, req.AccountName)
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
	var req service.TOTPEnrollmentFinishRequest
	if err := decodeJSONBody(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	resp, err := h.svc.CompleteTOTPEnrollment(r.Context(), &req)
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
		IdentityID string `json:"identity_id"`
		Code       string `json:"code"`
	}
	if err := decodeJSONBody(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := h.svc.VerifyTOTP(r.Context(), req.IdentityID, req.Code); err != nil {
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
		IdentityID string `json:"identity_id"`
		Code       string `json:"code"`
	}
	if err := decodeJSONBody(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := h.svc.VerifyBackupCode(r.Context(), req.IdentityID, req.Code); err != nil {
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
	var req struct {
		IdentityID string `json:"identity_id"`
	}
	if err := decodeJSONBody(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	codes, err := h.svc.RegenerateBackupCodes(r.Context(), req.IdentityID)
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
			IdentityID string `json:"identity_id"`
			Label      string `json:"label"`
		}
		if err := decodeJSONBody(w, r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		token, expiresAt, err := h.svc.RememberTrustedDevice(r.Context(), req.IdentityID, req.Label)
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
			IdentityID string `json:"identity_id"`
			Token      string `json:"token"`
		}
		if err := decodeJSONBody(w, r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		if err := h.svc.RevokeTrustedDevice(r.Context(), req.IdentityID, req.Token); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"status": "revoked"})
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
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
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]any{
		"error": map[string]any{
			"code":    status,
			"message": strings.TrimSpace(message),
		},
	})
}
