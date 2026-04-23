package handler

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"

	platformcrypto "github.com/aegion/aegion/internal/platform/crypto"
	"github.com/aegion/aegion/modules/social/service"
	"github.com/aegion/aegion/modules/social/store"
)

const maxJSONBodyBytes int64 = 1 << 20

type Config struct {
	ManagementToken string
}

type SocialService interface {
	ListProviders(ctx context.Context) ([]store.Provider, error)
	StartAuth(ctx context.Context, provider, redirectTo string) (*service.StartAuthResponse, error)
	CompleteAuth(ctx context.Context, provider, stateID, code string) (*service.CallbackResult, error)
	ListConfiguredProviders(ctx context.Context, includeDisabled bool) ([]store.Provider, error)
	GetProvider(ctx context.Context, slug string) (*store.Provider, error)
	UpsertProvider(ctx context.Context, req service.ProviderUpsertRequest) (*store.Provider, error)
	DeleteProvider(ctx context.Context, slug string) error
}

type Handler struct {
	svc             SocialService
	managementToken string
}

func New(svc SocialService, cfgOverride ...Config) *Handler {
	cfg := Config{}
	if len(cfgOverride) > 0 {
		cfg = cfgOverride[0]
	}
	return &Handler{
		svc:             svc,
		managementToken: strings.TrimSpace(cfg.ManagementToken),
	}
}

func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	if mux == nil {
		return
	}
	mux.HandleFunc("/api/v1/social/providers", h.handleProviders)
	mux.HandleFunc("/api/v1/social/admin/providers", h.handleAdminProviders)
	mux.HandleFunc("/api/v1/social/admin/providers/", h.handleAdminProvider)
	mux.HandleFunc("/api/v1/social/", h.handleSocialPath)
	mux.HandleFunc("/self-service/social/", h.handleSocialPath)
}

func (h *Handler) handleProviders(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	providers, err := h.svc.ListProviders(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list providers")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"providers": providers})
}

func (h *Handler) handleSocialPath(w http.ResponseWriter, r *http.Request) {
	if h == nil || h.svc == nil {
		writeError(w, http.StatusInternalServerError, "social service unavailable")
		return
	}
	path := strings.Trim(r.URL.Path, "/")
	segments := strings.Split(path, "/")
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
		case errors.Is(err, service.ErrProviderUnsupported), errors.Is(err, service.ErrProviderMisconfig):
			writeError(w, http.StatusBadRequest, "provider is not configured")
		default:
			writeError(w, http.StatusInternalServerError, "failed to start social auth")
		}
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) handleCallback(w http.ResponseWriter, r *http.Request, provider string) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if r.Method == http.MethodPost {
		if err := r.ParseForm(); err != nil {
			writeError(w, http.StatusBadRequest, "invalid callback payload")
			return
		}
	}

	stateID := strings.TrimSpace(firstNonEmpty(
		r.URL.Query().Get("state"),
		r.FormValue("state"),
	))
	code := strings.TrimSpace(firstNonEmpty(
		r.URL.Query().Get("code"),
		r.FormValue("code"),
	))

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

	redirectTarget := safeBrowserRedirectTarget(resp.RedirectTo)
	if !acceptsJSON(r) && redirectTarget != "" {
		redirectTo := withQuery(redirectTarget, map[string]string{
			"social_provider": resp.Provider,
			"identity_id":     resp.IdentityID,
			"social_status":   "authenticated",
		})
		http.Redirect(w, r, redirectTo, http.StatusSeeOther)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) handleAdminProviders(w http.ResponseWriter, r *http.Request) {
	if !h.requireManagementAuth(w, r) {
		return
	}

	switch r.Method {
	case http.MethodGet:
		providers, err := h.svc.ListConfiguredProviders(r.Context(), true)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to list providers")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"providers": providers})
	case http.MethodPost:
		var req service.ProviderUpsertRequest
		if err := decodeJSONBody(w, r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		provider, err := h.svc.UpsertProvider(r.Context(), req)
		if err != nil {
			writeError(w, http.StatusBadRequest, "failed to save provider")
			return
		}
		writeJSON(w, http.StatusOK, provider)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (h *Handler) handleAdminProvider(w http.ResponseWriter, r *http.Request) {
	if !h.requireManagementAuth(w, r) {
		return
	}

	slug := strings.TrimSpace(strings.TrimPrefix(r.URL.Path, "/api/v1/social/admin/providers/"))
	if slug == "" {
		writeError(w, http.StatusNotFound, "route not found")
		return
	}

	switch r.Method {
	case http.MethodGet:
		provider, err := h.svc.GetProvider(r.Context(), slug)
		if err != nil {
			writeError(w, http.StatusNotFound, "provider not found")
			return
		}
		writeJSON(w, http.StatusOK, provider)
	case http.MethodDelete:
		if err := h.svc.DeleteProvider(r.Context(), slug); err != nil {
			writeError(w, http.StatusNotFound, "provider not found")
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (h *Handler) requireManagementAuth(w http.ResponseWriter, r *http.Request) bool {
	if strings.TrimSpace(h.managementToken) == "" {
		writeError(w, http.StatusServiceUnavailable, "provider management is disabled")
		return false
	}
	token := strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
	if token == "" {
		writeError(w, http.StatusUnauthorized, "missing management token")
		return false
	}
	if !platformcrypto.ConstantTimeCompare([]byte(token), []byte(h.managementToken)) {
		writeError(w, http.StatusUnauthorized, "invalid management token")
		return false
	}
	return true
}

func acceptsJSON(r *http.Request) bool {
	return strings.Contains(strings.ToLower(strings.TrimSpace(r.Header.Get("Accept"))), "application/json")
}

func withQuery(target string, additions map[string]string) string {
	parsed, err := url.Parse(target)
	if err != nil {
		return target
	}
	query := parsed.Query()
	for key, value := range additions {
		if strings.TrimSpace(value) == "" {
			continue
		}
		query.Set(key, value)
	}
	parsed.RawQuery = query.Encode()
	return parsed.String()
}

func safeBrowserRedirectTarget(target string) string {
	target = strings.TrimSpace(target)
	if target == "" {
		return ""
	}
	if strings.ContainsAny(target, "\r\n") {
		return "/"
	}
	if !strings.HasPrefix(target, "/") || strings.HasPrefix(target, "//") {
		return "/"
	}
	return target
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
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
