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
	"github.com/aegion/aegion/modules/sso/service"
	"github.com/aegion/aegion/modules/sso/store"
)

const maxJSONBodyBytes int64 = 1 << 20

type Config struct {
	ManagementToken string
}

type SSOService interface {
	ListConnections(ctx context.Context) ([]store.Connection, error)
	ListConfiguredConnections(ctx context.Context, includeDisabled bool) ([]store.Connection, error)
	GetConnection(ctx context.Context, slug string) (*store.Connection, error)
	GetConnectionForDomain(ctx context.Context, domain string) (*store.Connection, error)
	UpsertConnection(ctx context.Context, req service.ConnectionUpsertRequest) (*store.Connection, error)
	DeleteConnection(ctx context.Context, slug string) error
	StartAuth(ctx context.Context, slug, redirectTo string) (*service.StartResponse, error)
	CompleteAuth(ctx context.Context, slug, relayState, subject, email, displayName string, attributes map[string]interface{}) (*service.CallbackResult, error)
}

type Handler struct {
	svc             SSOService
	managementToken string
}

func New(svc SSOService, cfgOverride ...Config) *Handler {
	cfg := Config{}
	if len(cfgOverride) > 0 {
		cfg = cfgOverride[0]
	}
	return &Handler{svc: svc, managementToken: strings.TrimSpace(cfg.ManagementToken)}
}

func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	if mux == nil {
		return
	}
	mux.HandleFunc("/api/v1/sso/connections", h.handleConnections)
	mux.HandleFunc("/api/v1/sso/admin/connections", h.handleAdminConnections)
	mux.HandleFunc("/api/v1/sso/admin/connections/", h.handleAdminConnection)
	mux.HandleFunc("/api/v1/sso/resolve-domain", h.handleResolveDomain)
	mux.HandleFunc("/api/v1/sso/", h.handleSSOPath)
	mux.HandleFunc("/self-service/sso/", h.handleSSOPath)
}

func (h *Handler) handleConnections(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	connections, err := h.svc.ListConnections(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list connections")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"connections": connections})
}

func (h *Handler) handleResolveDomain(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	domain := strings.TrimSpace(r.URL.Query().Get("domain"))
	if domain == "" {
		writeError(w, http.StatusBadRequest, "domain is required")
		return
	}
	connection, err := h.svc.GetConnectionForDomain(r.Context(), domain)
	if err != nil {
		writeError(w, http.StatusNotFound, "connection not found")
		return
	}
	writeJSON(w, http.StatusOK, connection)
}

func (h *Handler) handleSSOPath(w http.ResponseWriter, r *http.Request) {
	path := strings.Trim(r.URL.Path, "/")
	segments := strings.Split(path, "/")
	if len(segments) < 4 {
		writeError(w, http.StatusNotFound, "route not found")
		return
	}
	connection := segments[len(segments)-2]
	action := segments[len(segments)-1]
	switch action {
	case "start":
		h.handleStart(w, r, connection)
	case "callback":
		h.handleCallback(w, r, connection)
	default:
		writeError(w, http.StatusNotFound, "route not found")
	}
}

func (h *Handler) handleStart(w http.ResponseWriter, r *http.Request, connection string) {
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
	resp, err := h.svc.StartAuth(r.Context(), connection, req.RedirectTo)
	if err != nil {
		writeError(w, http.StatusBadRequest, "failed to start sso auth")
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) handleCallback(w http.ResponseWriter, r *http.Request, connection string) {
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
	relayState := strings.TrimSpace(firstNonEmpty(r.URL.Query().Get("RelayState"), r.FormValue("RelayState"), r.URL.Query().Get("state")))
	if hasUntrustedIdentityInputs(r) {
		writeError(w, http.StatusBadRequest, "identity attributes must not be supplied by callback caller")
		return
	}
	attrs := map[string]interface{}{
		"_expected_recipients": expectedRecipients(r),
	}
	if samlResponse := strings.TrimSpace(firstNonEmpty(r.URL.Query().Get("SAMLResponse"), r.FormValue("SAMLResponse"))); samlResponse != "" {
		attrs["_saml_response"] = samlResponse
	}
	resp, err := h.svc.CompleteAuth(r.Context(), connection, relayState, "", "", "", attrs)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid sso callback")
		return
	}
	if !acceptsJSON(r) && strings.TrimSpace(resp.RedirectTo) != "" {
		redirectTo := withQuery(resp.RedirectTo, map[string]string{
			"sso_connection": resp.Connection,
			"sso_status":     "authenticated",
			"email":          resp.Email,
		})
		http.Redirect(w, r, redirectTo, http.StatusSeeOther)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func hasUntrustedIdentityInputs(r *http.Request) bool {
	for _, key := range []string{"subject", "name_id", "email", "display_name", "attributes"} {
		if strings.TrimSpace(firstNonEmpty(r.URL.Query().Get(key), r.FormValue(key))) != "" {
			return true
		}
	}
	return false
}

func (h *Handler) handleAdminConnections(w http.ResponseWriter, r *http.Request) {
	if !h.requireManagementAuth(w, r) {
		return
	}
	switch r.Method {
	case http.MethodGet:
		connections, err := h.svc.ListConfiguredConnections(r.Context(), true)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to list connections")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"connections": connections})
	case http.MethodPost:
		var req service.ConnectionUpsertRequest
		if err := decodeJSONBody(w, r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		connection, err := h.svc.UpsertConnection(r.Context(), req)
		if err != nil {
			writeError(w, http.StatusBadRequest, "failed to save connection")
			return
		}
		writeJSON(w, http.StatusOK, connection)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (h *Handler) handleAdminConnection(w http.ResponseWriter, r *http.Request) {
	if !h.requireManagementAuth(w, r) {
		return
	}
	slug := strings.TrimSpace(strings.TrimPrefix(r.URL.Path, "/api/v1/sso/admin/connections/"))
	if slug == "" {
		writeError(w, http.StatusNotFound, "route not found")
		return
	}
	switch r.Method {
	case http.MethodGet:
		connection, err := h.svc.GetConnection(r.Context(), slug)
		if err != nil {
			writeError(w, http.StatusNotFound, "connection not found")
			return
		}
		writeJSON(w, http.StatusOK, connection)
	case http.MethodDelete:
		if err := h.svc.DeleteConnection(r.Context(), slug); err != nil {
			writeError(w, http.StatusNotFound, "connection not found")
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (h *Handler) requireManagementAuth(w http.ResponseWriter, r *http.Request) bool {
	if strings.TrimSpace(h.managementToken) == "" {
		writeError(w, http.StatusServiceUnavailable, "sso management is disabled")
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

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func expectedRecipients(r *http.Request) []string {
	if r == nil || r.URL == nil {
		return nil
	}
	pathOnly := strings.TrimSpace(r.URL.Path)
	proto := firstForwardedValue(r.Header.Get("X-Forwarded-Proto"))
	if proto == "" {
		if r.TLS != nil {
			proto = "https"
		} else {
			proto = "http"
		}
	}
	host := firstForwardedValue(r.Header.Get("X-Forwarded-Host"))
	if host == "" {
		host = strings.TrimSpace(r.Host)
	}

	out := make([]string, 0, 2)
	seen := map[string]struct{}{}
	add := func(value string) {
		value = strings.TrimSpace(value)
		if value == "" {
			return
		}
		if _, ok := seen[value]; ok {
			return
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}

	if host != "" {
		add(strings.ToLower(strings.TrimSpace(proto)) + "://" + host + pathOnly)
	}
	add(pathOnly)
	return out
}

func firstForwardedValue(raw string) string {
	parts := strings.Split(raw, ",")
	for _, part := range parts {
		if value := strings.TrimSpace(part); value != "" {
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
