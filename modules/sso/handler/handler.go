package handler

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"time"

	platformcrypto "github.com/aegion/aegion/internal/platform/crypto"
	"github.com/aegion/aegion/internal/platform/trustedproxy"
	"github.com/aegion/aegion/modules/sso/service"
	"github.com/aegion/aegion/modules/sso/store"
)

const (
	maxJSONBodyBytes      int64 = 1 << 20
	maxSAMLFormBodyBytes  int64 = 1 << 20
	identityContextMaxAge       = time.Minute
)

var signedIdentityHeaders = []string{
	"X-User-ID",
	"X-User-Session-ID",
	"X-User-AAL",
}

type Config struct {
	IdentityContextSecret []byte
	IdentityContextMaxAge time.Duration
	TrustForwardedHeaders bool
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
	svc                   SSOService
	identityContextSecret []byte
	identityContextMaxAge time.Duration
	trustForwardedHeaders bool
	now                   func() time.Time
}

func New(svc SSOService, cfgOverride ...Config) *Handler {
	cfg := Config{}
	if len(cfgOverride) > 0 {
		cfg = cfgOverride[0]
	}
	maxAge := cfg.IdentityContextMaxAge
	if maxAge <= 0 || maxAge > identityContextMaxAge {
		maxAge = identityContextMaxAge
	}
	return &Handler{
		svc:                   svc,
		identityContextSecret: append([]byte(nil), cfg.IdentityContextSecret...),
		identityContextMaxAge: maxAge,
		trustForwardedHeaders: cfg.TrustForwardedHeaders,
		now: func() time.Time {
			return time.Now().UTC()
		},
	}
}

func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	if mux == nil {
		return
	}
	mux.HandleFunc("/api/v1/sso/connections", h.handleConnections)
	mux.HandleFunc("/api/v1/sso/admin/connections", h.handleAdminConnections)
	mux.HandleFunc("/api/v1/sso/admin/connections/", h.handleAdminConnection)
	mux.HandleFunc("/api/v1/sso/resolve-domain", h.handleResolveDomain)
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
	segments := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(segments) != 4 || segments[0] != "self-service" || segments[1] != "sso" || strings.TrimSpace(segments[2]) == "" {
		writeError(w, http.StatusNotFound, "route not found")
		return
	}
	connection := segments[2]
	action := segments[3]
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
	if !isJSONContentType(r) {
		writeError(w, http.StatusUnsupportedMediaType, "application/json content type is required")
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
		if !isFormContentType(r) {
			writeError(w, http.StatusUnsupportedMediaType, "form content type is required")
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxSAMLFormBodyBytes)
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
		"_expected_recipients": expectedRecipients(r, h.trustForwardedHeaders),
	}
	if samlResponse := strings.TrimSpace(firstNonEmpty(r.URL.Query().Get("SAMLResponse"), r.FormValue("SAMLResponse"))); samlResponse != "" {
		attrs["_saml_response"] = samlResponse
	}
	resp, err := h.svc.CompleteAuth(r.Context(), connection, relayState, "", "", "", attrs)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid sso callback")
		return
	}
	redirectTarget := safeBrowserRedirectTarget(resp.RedirectTo)
	if !acceptsJSON(r) && redirectTarget != "" {
		redirectTo := withQuery(redirectTarget, map[string]string{
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
		if !isJSONContentType(r) {
			writeError(w, http.StatusUnsupportedMediaType, "application/json content type is required")
			return
		}
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
	if len(h.identityContextSecret) == 0 {
		writeError(w, http.StatusServiceUnavailable, "sso management is disabled")
		return false
	}
	for _, header := range signedIdentityHeaders {
		if strings.TrimSpace(r.Header.Get(header)) == "" {
			writeError(w, http.StatusUnauthorized, "missing authenticated identity context")
			return false
		}
	}
	if !platformcrypto.VerifyIdentityHeaders(
		h.identityContextSecret,
		r.Header,
		signedIdentityHeaders,
		r.Header.Get("X-Aegion-Signature"),
		h.identityContextMaxAge,
		h.now(),
	) {
		writeError(w, http.StatusUnauthorized, "invalid authenticated identity context")
		return false
	}
	return true
}

func acceptsJSON(r *http.Request) bool {
	return strings.Contains(strings.ToLower(strings.TrimSpace(r.Header.Get("Accept"))), "application/json")
}

func isJSONContentType(r *http.Request) bool {
	return hasContentType(r, "application/json")
}

func isFormContentType(r *http.Request) bool {
	return hasContentType(r, "application/x-www-form-urlencoded")
}

func hasContentType(r *http.Request, expected string) bool {
	if r == nil {
		return false
	}
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	return err == nil && strings.EqualFold(mediaType, expected)
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

func expectedRecipients(r *http.Request, trustForwardedHeaders bool) []string {
	if r == nil || r.URL == nil {
		return nil
	}
	pathOnly := strings.TrimSpace(r.URL.Path)
	proto := firstForwardedValue(trustedproxy.ForwardedProto(r, trustForwardedHeaders, "AEGION_TRUSTED_PROXY_CIDRS"))
	proto = strings.ToLower(strings.TrimSpace(proto))
	if proto == "" {
		proto = "http"
	}
	host := firstForwardedValue(trustedproxy.ForwardedHost(r, trustForwardedHeaders, "AEGION_TRUSTED_PROXY_CIDRS"))
	host = strings.TrimSpace(host)

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
