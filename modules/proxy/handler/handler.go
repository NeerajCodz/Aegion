package handler

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	platformcrypto "github.com/aegion/aegion/internal/platform/crypto"
	"github.com/aegion/aegion/modules/proxy/service"
	"github.com/aegion/aegion/modules/proxy/store"
)

const maxJSONBodyBytes int64 = 1 << 20

type Config struct {
	ManagementToken string
}

type ProxyService interface {
	EffectiveConfig(ctx context.Context) (*service.EffectiveConfig, error)
	ListUpstreams(ctx context.Context) ([]store.Upstream, error)
	GetUpstream(ctx context.Context, name string) (*store.Upstream, error)
	UpsertUpstream(ctx context.Context, upstream store.Upstream) (*store.Upstream, error)
	DeleteUpstream(ctx context.Context, name string) error
	ListRoutes(ctx context.Context) ([]store.Route, error)
	GetRoute(ctx context.Context, id string) (*store.Route, error)
	UpsertRoute(ctx context.Context, route store.Route) (*store.Route, error)
	DeleteRoute(ctx context.Context, id string) error
	Simulate(ctx context.Context, req service.SimulateRequest) (*service.SimulateResponse, error)
}

type Handler struct {
	svc             ProxyService
	managementToken string
}

func New(svc ProxyService, cfgOverride ...Config) *Handler {
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
	for _, prefix := range []string{"/api/v1/proxy", "/proxy"} {
		mux.HandleFunc(prefix+"/config", h.handleConfig)
		mux.HandleFunc(prefix+"/upstreams", h.handleUpstreams)
		mux.HandleFunc(prefix+"/upstreams/", h.handleUpstream)
		mux.HandleFunc(prefix+"/routes", h.handleRoutes)
		mux.HandleFunc(prefix+"/routes/", h.handleRoute)
		mux.HandleFunc(prefix+"/simulate", h.handleSimulate)
	}
}

func (h *Handler) handleConfig(w http.ResponseWriter, r *http.Request) {
	if !h.requireManagementAuth(w, r) {
		return
	}
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	config, err := h.svc.EffectiveConfig(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load proxy config")
		return
	}
	writeJSON(w, http.StatusOK, config)
}

func (h *Handler) handleUpstreams(w http.ResponseWriter, r *http.Request) {
	if !h.requireManagementAuth(w, r) {
		return
	}
	switch r.Method {
	case http.MethodGet:
		upstreams, err := h.svc.ListUpstreams(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to list upstreams")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"upstreams": upstreams})
	case http.MethodPost:
		var upstream store.Upstream
		if err := decodeJSONBody(w, r, &upstream); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		saved, err := h.svc.UpsertUpstream(r.Context(), upstream)
		if err != nil {
			status := http.StatusBadRequest
			if errors.Is(err, service.ErrInvalidProxyConfig) {
				status = http.StatusBadRequest
			}
			writeError(w, status, "failed to save upstream")
			return
		}
		writeJSON(w, http.StatusOK, saved)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (h *Handler) handleUpstream(w http.ResponseWriter, r *http.Request) {
	if !h.requireManagementAuth(w, r) {
		return
	}
	name := strings.TrimSpace(lastPathSegment(r.URL.Path))
	if name == "" {
		writeError(w, http.StatusNotFound, "route not found")
		return
	}

	switch r.Method {
	case http.MethodGet:
		upstream, err := h.svc.GetUpstream(r.Context(), name)
		if err != nil {
			writeError(w, http.StatusNotFound, "upstream not found")
			return
		}
		writeJSON(w, http.StatusOK, upstream)
	case http.MethodDelete:
		err := h.svc.DeleteUpstream(r.Context(), name)
		if err != nil {
			switch {
			case errors.Is(err, store.ErrUpstreamInUse):
				writeError(w, http.StatusConflict, "upstream is referenced by routes")
			default:
				writeError(w, http.StatusNotFound, "upstream not found")
			}
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (h *Handler) handleRoutes(w http.ResponseWriter, r *http.Request) {
	if !h.requireManagementAuth(w, r) {
		return
	}
	switch r.Method {
	case http.MethodGet:
		routes, err := h.svc.ListRoutes(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to list routes")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"routes": routes})
	case http.MethodPost:
		var route store.Route
		if err := decodeJSONBody(w, r, &route); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		saved, err := h.svc.UpsertRoute(r.Context(), route)
		if err != nil {
			status := http.StatusBadRequest
			if errors.Is(err, store.ErrUpstreamNotFound) {
				status = http.StatusBadRequest
			}
			writeError(w, status, "failed to save route")
			return
		}
		writeJSON(w, http.StatusOK, saved)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (h *Handler) handleRoute(w http.ResponseWriter, r *http.Request) {
	if !h.requireManagementAuth(w, r) {
		return
	}
	id := strings.TrimSpace(lastPathSegment(r.URL.Path))
	if id == "" {
		writeError(w, http.StatusNotFound, "route not found")
		return
	}

	switch r.Method {
	case http.MethodGet:
		route, err := h.svc.GetRoute(r.Context(), id)
		if err != nil {
			writeError(w, http.StatusNotFound, "route not found")
			return
		}
		writeJSON(w, http.StatusOK, route)
	case http.MethodDelete:
		if err := h.svc.DeleteRoute(r.Context(), id); err != nil {
			writeError(w, http.StatusNotFound, "route not found")
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (h *Handler) handleSimulate(w http.ResponseWriter, r *http.Request) {
	if !h.requireManagementAuth(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req service.SimulateRequest
	if err := decodeJSONBody(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	resp, err := h.svc.Simulate(r.Context(), req)
	if err != nil {
		writeError(w, http.StatusBadRequest, "failed to simulate proxy route")
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) requireManagementAuth(w http.ResponseWriter, r *http.Request) bool {
	if strings.TrimSpace(h.managementToken) == "" {
		writeError(w, http.StatusServiceUnavailable, "proxy management is disabled")
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

func lastPathSegment(path string) string {
	trimmed := strings.Trim(strings.TrimSpace(path), "/")
	if trimmed == "" {
		return ""
	}
	parts := strings.Split(trimmed, "/")
	return parts[len(parts)-1]
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
	if err := json.NewEncoder(w).Encode(v); err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]any{
		"error": map[string]any{
			"code":    status,
			"message": message,
		},
	})
}
