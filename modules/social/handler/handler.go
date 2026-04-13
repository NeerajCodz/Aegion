package handler

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/aegion/aegion/modules/social/service"
)

const maxJSONBodyBytes int64 = 1 << 20

type SocialService interface {
	ListProviders() []string
	StartAuth(ctx context.Context, provider, redirectTo string) (*service.StartAuthResponse, error)
	CompleteAuth(ctx context.Context, provider, stateID, code string) (*service.CallbackResult, error)
}

// Handler exposes HTTP routes for the social login module.
type Handler struct {
	svc SocialService
}

// New creates a new social handler.
func New(svc SocialService) *Handler {
	return &Handler{svc: svc}
}

// RegisterRoutes registers module routes on the provided mux.
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	if mux == nil {
		return
	}
	mux.HandleFunc("/api/v1/social/providers", h.handleProviders)
	mux.HandleFunc("/api/v1/social/", h.handleSocialPath)
	mux.HandleFunc("/self-service/social/", h.handleSocialPath)
}

func (h *Handler) handleProviders(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"providers": h.svc.ListProviders()})
}

func (h *Handler) handleSocialPath(w http.ResponseWriter, r *http.Request) {
	if h == nil || h.svc == nil {
		writeError(w, http.StatusInternalServerError, "social service unavailable")
		return
	}
	path := strings.Trim(r.URL.Path, "/")
	segments := strings.Split(path, "/")
	// /api/v1/social/{provider}/{action} OR /self-service/social/{provider}/{action}
	if len(segments) < 4 {
		writeError(w, http.StatusNotFound, "route not found")
		return
	}
	providerIndex := len(segments) - 2
	actionIndex := len(segments) - 1
	provider := segments[providerIndex]
	action := segments[actionIndex]

	switch action {
	case "start":
		h.handleStart(w, r, provider)
	case "callback":
		h.handleCallback(w, r, provider)
	default:
		writeError(w, http.StatusNotFound, "route not found")
	}
}

func (h *Handler) handleStart(w http.ResponseWriter, r *http.Request, provider string) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req struct {
		RedirectTo string `json:"redirect_to"`
	}
	if err := decodeJSONBody(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	resp, err := h.svc.StartAuth(r.Context(), provider, req.RedirectTo)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrProviderUnsupported):
			writeError(w, http.StatusBadRequest, "provider is not configured")
		default:
			writeError(w, http.StatusInternalServerError, "failed to start social auth")
		}
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) handleCallback(w http.ResponseWriter, r *http.Request, provider string) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	stateID := strings.TrimSpace(r.URL.Query().Get("state"))
	code := strings.TrimSpace(r.URL.Query().Get("code"))
	resp, err := h.svc.CompleteAuth(r.Context(), provider, stateID, code)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrProviderUnsupported), errors.Is(err, service.ErrInvalidState), errors.Is(err, service.ErrInvalidCallback):
			writeError(w, http.StatusBadRequest, "invalid social callback")
		default:
			writeError(w, http.StatusBadGateway, "provider callback failed")
		}
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func decodeJSONBody(w http.ResponseWriter, r *http.Request, dst interface{}) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBodyBytes)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		return err
	}
	var extra struct{}
	if err := decoder.Decode(&extra); err != io.EOF {
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
			"message": message,
		},
	})
}
