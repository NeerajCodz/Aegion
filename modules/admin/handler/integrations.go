package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	socialservice "github.com/aegion/aegion/modules/social/service"
	socialstore "github.com/aegion/aegion/modules/social/store"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

type IntegrationOverviewResponse struct {
	SocialProviders int64 `json:"social_providers"`
	SocialLinks     int64 `json:"social_links"`
	SSOConnections  int64 `json:"sso_connections"`
	ProxyUpstreams  int64 `json:"proxy_upstreams"`
	ProxyRoutes     int64 `json:"proxy_routes"`
	SCIMTokens      int64 `json:"scim_tokens"`
}

func (h *Handler) IntegrationOverview(w http.ResponseWriter, r *http.Request) {
	if OperatorFromContext(r.Context()) == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required")
		return
	}

	var resp IntegrationOverviewResponse
	if err := h.dbConn().QueryRow(r.Context(), `SELECT COUNT(*) FROM soc_providers`).Scan(&resp.SocialProviders); err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Failed to load social provider count")
		return
	}
	if err := h.dbConn().QueryRow(r.Context(), `SELECT COUNT(*) FROM soc_identity_links`).Scan(&resp.SocialLinks); err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Failed to load social link count")
		return
	}
	if err := h.dbConn().QueryRow(r.Context(), `SELECT COUNT(*) FROM sso_connections`).Scan(&resp.SSOConnections); err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Failed to load SSO connection count")
		return
	}
	if err := h.dbConn().QueryRow(r.Context(), `SELECT COUNT(*) FROM proxy_upstreams`).Scan(&resp.ProxyUpstreams); err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Failed to load proxy upstream count")
		return
	}
	if err := h.dbConn().QueryRow(r.Context(), `SELECT COUNT(*) FROM proxy_routes`).Scan(&resp.ProxyRoutes); err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Failed to load proxy route count")
		return
	}
	if err := h.dbConn().QueryRow(r.Context(), `SELECT COUNT(*) FROM adm_scim_tokens`).Scan(&resp.SCIMTokens); err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Failed to load SCIM token count")
		return
	}

	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) ListSocialProviders(w http.ResponseWriter, r *http.Request) {
	h.listGenericRows(w, r, `
		SELECT slug, display_name, preset, protocol, enabled, redirect_uri, created_at, updated_at
		FROM soc_providers
		ORDER BY display_name ASC, slug ASC
	`, []string{"slug", "display_name", "preset", "protocol", "enabled", "redirect_uri", "created_at", "updated_at"}, "providers")
}

type SocialProviderRequest struct {
	Slug               string                   `json:"slug"`
	DisplayName        string                   `json:"display_name"`
	Preset             string                   `json:"preset"`
	Protocol           socialstore.Protocol     `json:"protocol"`
	Issuer             string                   `json:"issuer"`
	DiscoveryURL       string                   `json:"discovery_url"`
	AuthorizeEndpoint  string                   `json:"authorize_endpoint"`
	TokenEndpoint      string                   `json:"token_endpoint"`
	UserInfoEndpoint   string                   `json:"userinfo_endpoint"`
	JWKSURI            string                   `json:"jwks_uri"`
	Scopes             []string                 `json:"scopes"`
	ClaimMapping       socialstore.ClaimMapping `json:"claim_mapping"`
	ExtraAuthParams    map[string]string        `json:"extra_auth_params"`
	PKCEMethod         socialstore.PKCEMethod   `json:"pkce_method"`
	AuthStyle          socialstore.AuthStyle    `json:"auth_style"`
	ClaimSource        socialstore.ClaimSource  `json:"claim_source"`
	Enabled            bool                     `json:"enabled"`
	TrustEmailVerified bool                     `json:"trust_email_verified"`
	RedirectURI        string                   `json:"redirect_uri"`
	ClientID           string                   `json:"client_id"`
	ClientSecret       string                   `json:"client_secret,omitempty"`
}

func (h *Handler) GetSocialProvider(w http.ResponseWriter, r *http.Request) {
	if OperatorFromContext(r.Context()) == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required")
		return
	}
	if h.socialProviders == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "Social provider management is not configured")
		return
	}
	slug := strings.ToLower(strings.TrimSpace(chi.URLParam(r, "slug")))
	if slug == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "slug is required")
		return
	}
	provider, err := h.socialProviders.GetProvider(r.Context(), slug)
	if err != nil {
		if errors.Is(err, socialstore.ErrProviderNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "Social provider not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "Failed to load social provider")
		return
	}
	writeJSON(w, http.StatusOK, provider)
}

func (h *Handler) UpsertSocialProvider(w http.ResponseWriter, r *http.Request) {
	if OperatorFromContext(r.Context()) == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required")
		return
	}
	if h.socialProviders == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "Social provider management is not configured")
		return
	}
	var req SocialProviderRequest
	if err := decodeJSONBody(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "Invalid request body")
		return
	}
	req.Slug = strings.ToLower(strings.TrimSpace(req.Slug))
	req.DisplayName = strings.TrimSpace(req.DisplayName)
	req.Preset = strings.ToLower(strings.TrimSpace(req.Preset))
	req.Issuer = strings.TrimSpace(req.Issuer)
	req.DiscoveryURL = strings.TrimSpace(req.DiscoveryURL)
	req.AuthorizeEndpoint = strings.TrimSpace(req.AuthorizeEndpoint)
	req.TokenEndpoint = strings.TrimSpace(req.TokenEndpoint)
	req.UserInfoEndpoint = strings.TrimSpace(req.UserInfoEndpoint)
	req.JWKSURI = strings.TrimSpace(req.JWKSURI)
	req.RedirectURI = strings.TrimSpace(req.RedirectURI)
	req.ClientID = strings.TrimSpace(req.ClientID)
	req.ClientSecret = strings.TrimSpace(req.ClientSecret)

	provider, err := h.socialProviders.UpsertProvider(r.Context(), socialservice.ProviderUpsertRequest{
		Slug:               req.Slug,
		DisplayName:        req.DisplayName,
		Preset:             req.Preset,
		Protocol:           req.Protocol,
		Issuer:             req.Issuer,
		DiscoveryURL:       req.DiscoveryURL,
		AuthorizeEndpoint:  req.AuthorizeEndpoint,
		TokenEndpoint:      req.TokenEndpoint,
		UserInfoEndpoint:   req.UserInfoEndpoint,
		JWKSURI:            req.JWKSURI,
		Scopes:             req.Scopes,
		ClaimMapping:       req.ClaimMapping,
		ExtraAuthParams:    req.ExtraAuthParams,
		PKCEMethod:         req.PKCEMethod,
		AuthStyle:          req.AuthStyle,
		ClaimSource:        req.ClaimSource,
		Enabled:            req.Enabled,
		TrustEmailVerified: req.TrustEmailVerified,
		RedirectURI:        req.RedirectURI,
		ClientID:           req.ClientID,
		ClientSecret:       req.ClientSecret,
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "Failed to save social provider")
		return
	}
	writeJSON(w, http.StatusOK, provider)
}

func (h *Handler) DeleteSocialProvider(w http.ResponseWriter, r *http.Request) {
	if OperatorFromContext(r.Context()) == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required")
		return
	}
	if h.socialProviders == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "Social provider management is not configured")
		return
	}
	slug := strings.ToLower(strings.TrimSpace(chi.URLParam(r, "slug")))
	if slug == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "slug is required")
		return
	}
	if err := h.socialProviders.DeleteProvider(r.Context(), slug); err != nil {
		if errors.Is(err, socialstore.ErrProviderNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "Social provider not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "Failed to delete social provider")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) ListSSOConnections(w http.ResponseWriter, r *http.Request) {
	h.listGenericRows(w, r, `
		SELECT slug, display_name, entity_id, metadata_url, enabled, jit_provisioning, created_at, updated_at
		FROM sso_connections
		ORDER BY display_name ASC, slug ASC
	`, []string{"slug", "display_name", "entity_id", "metadata_url", "enabled", "jit_provisioning", "created_at", "updated_at"}, "connections")
}

type SSOConnectionRequest struct {
	Slug              string            `json:"slug"`
	DisplayName       string            `json:"display_name"`
	EntityID          string            `json:"entity_id"`
	SSOURL            string            `json:"sso_url"`
	CertificatePEM    string            `json:"certificate_pem"`
	MetadataURL       string            `json:"metadata_url"`
	Domains           []string          `json:"domains"`
	AttributeMapping  map[string]string `json:"attribute_mapping"`
	JITProvisioning   bool              `json:"jit_provisioning"`
	DefaultRedirectTo string            `json:"default_redirect_to"`
	ExtraAuthnContext map[string]string `json:"extra_authn_context"`
	Enabled           bool              `json:"enabled"`
}

func (h *Handler) UpsertSSOConnection(w http.ResponseWriter, r *http.Request) {
	if OperatorFromContext(r.Context()) == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required")
		return
	}
	var req SSOConnectionRequest
	if err := decodeJSONBody(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "Invalid request body")
		return
	}
	req.Slug = strings.ToLower(strings.TrimSpace(req.Slug))
	req.DisplayName = strings.TrimSpace(req.DisplayName)
	req.EntityID = strings.TrimSpace(req.EntityID)
	req.SSOURL = strings.TrimSpace(req.SSOURL)
	req.MetadataURL = strings.TrimSpace(req.MetadataURL)
	req.CertificatePEM = strings.TrimSpace(req.CertificatePEM)
	req.DefaultRedirectTo = strings.TrimSpace(req.DefaultRedirectTo)
	if req.Slug == "" || req.DisplayName == "" || req.EntityID == "" || req.SSOURL == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "slug, display_name, entity_id, and sso_url are required")
		return
	}

	domainsRaw, err := json.Marshal(req.Domains)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "Invalid domains")
		return
	}
	mappingRaw, err := json.Marshal(req.AttributeMapping)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "Invalid attribute mapping")
		return
	}
	extraRaw, err := json.Marshal(req.ExtraAuthnContext)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "Invalid extra authn context")
		return
	}

	now := time.Now().UTC()
	var (
		id        uuid.UUID
		createdAt time.Time
		updatedAt time.Time
	)
	err = h.dbConn().QueryRow(r.Context(), `
		INSERT INTO sso_connections (
			id, slug, display_name, entity_id, sso_url, certificate_pem, metadata_url, domains, attribute_mapping,
			jit_provisioning, default_redirect_to, extra_authn_context, enabled, created_at, updated_at
		) VALUES (
			gen_random_uuid(), $1, $2, $3, $4, $5, $6, $7::jsonb, $8::jsonb, $9, $10, $11::jsonb, $12, $13, $14
		)
		ON CONFLICT (slug) DO UPDATE SET
			display_name = EXCLUDED.display_name,
			entity_id = EXCLUDED.entity_id,
			sso_url = EXCLUDED.sso_url,
			certificate_pem = EXCLUDED.certificate_pem,
			metadata_url = EXCLUDED.metadata_url,
			domains = EXCLUDED.domains,
			attribute_mapping = EXCLUDED.attribute_mapping,
			jit_provisioning = EXCLUDED.jit_provisioning,
			default_redirect_to = EXCLUDED.default_redirect_to,
			extra_authn_context = EXCLUDED.extra_authn_context,
			enabled = EXCLUDED.enabled,
			updated_at = EXCLUDED.updated_at
		RETURNING id, created_at, updated_at
	`, req.Slug, req.DisplayName, req.EntityID, req.SSOURL, req.CertificatePEM, req.MetadataURL, string(domainsRaw), string(mappingRaw), req.JITProvisioning, req.DefaultRedirectTo, string(extraRaw), req.Enabled, now, now).Scan(&id, &createdAt, &updatedAt)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Failed to save SSO connection")
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"id":                  id.String(),
		"slug":                req.Slug,
		"display_name":        req.DisplayName,
		"entity_id":           req.EntityID,
		"sso_url":             req.SSOURL,
		"certificate_pem":     req.CertificatePEM,
		"metadata_url":        req.MetadataURL,
		"domains":             req.Domains,
		"attribute_mapping":   req.AttributeMapping,
		"jit_provisioning":    req.JITProvisioning,
		"default_redirect_to": req.DefaultRedirectTo,
		"extra_authn_context": req.ExtraAuthnContext,
		"enabled":             req.Enabled,
		"created_at":          createdAt,
		"updated_at":          updatedAt,
	})
}

func (h *Handler) DeleteSSOConnection(w http.ResponseWriter, r *http.Request) {
	if OperatorFromContext(r.Context()) == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required")
		return
	}
	slug := strings.ToLower(strings.TrimSpace(chi.URLParam(r, "slug")))
	if slug == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "slug is required")
		return
	}
	result, err := h.dbConn().Exec(r.Context(), `DELETE FROM sso_connections WHERE slug = $1`, slug)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Failed to delete SSO connection")
		return
	}
	if result.RowsAffected() == 0 {
		writeError(w, http.StatusNotFound, "not_found", "SSO connection not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) ListProxyUpstreams(w http.ResponseWriter, r *http.Request) {
	h.listGenericRows(w, r, `
		SELECT name, url, health_check, timeout, max_connections, enabled, created_at, updated_at
		FROM proxy_upstreams
		ORDER BY name ASC
	`, []string{"name", "url", "health_check", "timeout", "max_connections", "enabled", "created_at", "updated_at"}, "upstreams")
}

type ProxyUpstreamRequest struct {
	Name           string                 `json:"name"`
	URL            string                 `json:"url"`
	HealthCheck    string                 `json:"health_check"`
	Timeout        string                 `json:"timeout"`
	MaxConnections int                    `json:"max_connections"`
	Headers        map[string]string      `json:"headers"`
	CircuitBreaker map[string]interface{} `json:"circuit_breaker"`
	Enabled        bool                   `json:"enabled"`
}

func (h *Handler) UpsertProxyUpstream(w http.ResponseWriter, r *http.Request) {
	if OperatorFromContext(r.Context()) == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required")
		return
	}
	var req ProxyUpstreamRequest
	if err := decodeJSONBody(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "Invalid request body")
		return
	}
	req.Name = strings.ToLower(strings.TrimSpace(req.Name))
	req.URL = strings.TrimSpace(req.URL)
	req.HealthCheck = strings.TrimSpace(req.HealthCheck)
	req.Timeout = strings.TrimSpace(req.Timeout)
	if req.Name == "" || req.URL == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "name and url are required")
		return
	}
	headersRaw, err := json.Marshal(req.Headers)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "Invalid headers")
		return
	}
	cbRaw, err := json.Marshal(req.CircuitBreaker)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "Invalid circuit breaker")
		return
	}
	now := time.Now().UTC()
	var createdAt, updatedAt time.Time
	var id uuid.UUID
	err = h.dbConn().QueryRow(r.Context(), `
		INSERT INTO proxy_upstreams (
			id, name, url, health_check, timeout, max_connections, headers, circuit_breaker, enabled, created_at, updated_at
		) VALUES (
			gen_random_uuid(), $1, $2, $3, $4, $5, $6::jsonb, $7::jsonb, $8, $9, $10
		)
		ON CONFLICT (name) DO UPDATE SET
			url = EXCLUDED.url,
			health_check = EXCLUDED.health_check,
			timeout = EXCLUDED.timeout,
			max_connections = EXCLUDED.max_connections,
			headers = EXCLUDED.headers,
			circuit_breaker = EXCLUDED.circuit_breaker,
			enabled = EXCLUDED.enabled,
			updated_at = EXCLUDED.updated_at
		RETURNING id, created_at, updated_at
	`, req.Name, req.URL, req.HealthCheck, req.Timeout, req.MaxConnections, string(headersRaw), string(cbRaw), req.Enabled, now, now).Scan(&id, &createdAt, &updatedAt)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Failed to save proxy upstream")
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"id": id.String(), "name": req.Name, "url": req.URL, "health_check": req.HealthCheck, "timeout": req.Timeout,
		"max_connections": req.MaxConnections, "headers": req.Headers, "circuit_breaker": req.CircuitBreaker,
		"enabled": req.Enabled, "created_at": createdAt, "updated_at": updatedAt,
	})
}

func (h *Handler) DeleteProxyUpstream(w http.ResponseWriter, r *http.Request) {
	if OperatorFromContext(r.Context()) == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required")
		return
	}
	name := strings.ToLower(strings.TrimSpace(chi.URLParam(r, "name")))
	if name == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "name is required")
		return
	}
	result, err := h.dbConn().Exec(r.Context(), `DELETE FROM proxy_upstreams WHERE name = $1`, name)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Failed to delete proxy upstream")
		return
	}
	if result.RowsAffected() == 0 {
		writeError(w, http.StatusNotFound, "not_found", "Proxy upstream not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) ListProxyRoutes(w http.ResponseWriter, r *http.Request) {
	h.listGenericRows(w, r, `
		SELECT id, path, target, require_auth, required_aal, priority, enabled, description, created_at, updated_at
		FROM proxy_routes
		ORDER BY priority DESC, id ASC
	`, []string{"id", "path", "target", "require_auth", "required_aal", "priority", "enabled", "description", "created_at", "updated_at"}, "routes")
}

type ProxyRouteRequest struct {
	ID           string                 `json:"id"`
	Path         string                 `json:"path"`
	Methods      []string               `json:"methods"`
	RequireAuth  bool                   `json:"require_auth"`
	RequiredAAL  string                 `json:"required_aal"`
	Capabilities []string               `json:"capabilities"`
	RateLimit    map[string]interface{} `json:"rate_limit"`
	Target       string                 `json:"target"`
	Priority     int                    `json:"priority"`
	Headers      map[string]string      `json:"headers"`
	Rewrite      map[string]interface{} `json:"rewrite"`
	Enabled      bool                   `json:"enabled"`
	Description  string                 `json:"description"`
}

func (h *Handler) UpsertProxyRoute(w http.ResponseWriter, r *http.Request) {
	if OperatorFromContext(r.Context()) == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required")
		return
	}
	var req ProxyRouteRequest
	if err := decodeJSONBody(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "Invalid request body")
		return
	}
	req.ID = strings.TrimSpace(req.ID)
	req.Path = strings.TrimSpace(req.Path)
	req.Target = strings.ToLower(strings.TrimSpace(req.Target))
	req.RequiredAAL = strings.TrimSpace(req.RequiredAAL)
	req.Description = strings.TrimSpace(req.Description)
	if req.Path == "" || req.Target == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "path and target are required")
		return
	}
	if req.ID == "" {
		req.ID = uuid.NewString()
	}
	methodsRaw, err := json.Marshal(req.Methods)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "Invalid methods")
		return
	}
	capsRaw, err := json.Marshal(req.Capabilities)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "Invalid capabilities")
		return
	}
	rateRaw, err := json.Marshal(req.RateLimit)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "Invalid rate limit")
		return
	}
	headersRaw, err := json.Marshal(req.Headers)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "Invalid headers")
		return
	}
	rewriteRaw, err := json.Marshal(req.Rewrite)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "Invalid rewrite")
		return
	}
	now := time.Now().UTC()
	var createdAt, updatedAt time.Time
	err = h.dbConn().QueryRow(r.Context(), `
		INSERT INTO proxy_routes (
			id, path, methods, require_auth, required_aal, capabilities, rate_limit, target, priority, headers, rewrite, enabled, description, created_at, updated_at
		) VALUES (
			$1, $2, $3::jsonb, $4, $5, $6::jsonb, $7::jsonb, $8, $9, $10::jsonb, $11::jsonb, $12, $13, $14, $15
		)
		ON CONFLICT (id) DO UPDATE SET
			path = EXCLUDED.path,
			methods = EXCLUDED.methods,
			require_auth = EXCLUDED.require_auth,
			required_aal = EXCLUDED.required_aal,
			capabilities = EXCLUDED.capabilities,
			rate_limit = EXCLUDED.rate_limit,
			target = EXCLUDED.target,
			priority = EXCLUDED.priority,
			headers = EXCLUDED.headers,
			rewrite = EXCLUDED.rewrite,
			enabled = EXCLUDED.enabled,
			description = EXCLUDED.description,
			updated_at = EXCLUDED.updated_at
		RETURNING created_at, updated_at
	`, req.ID, req.Path, string(methodsRaw), req.RequireAuth, req.RequiredAAL, string(capsRaw), string(rateRaw), req.Target, req.Priority, string(headersRaw), string(rewriteRaw), req.Enabled, req.Description, now, now).Scan(&createdAt, &updatedAt)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Failed to save proxy route")
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"id": req.ID, "path": req.Path, "methods": req.Methods, "require_auth": req.RequireAuth, "required_aal": req.RequiredAAL,
		"capabilities": req.Capabilities, "rate_limit": req.RateLimit, "target": req.Target, "priority": req.Priority,
		"headers": req.Headers, "rewrite": req.Rewrite, "enabled": req.Enabled, "description": req.Description,
		"created_at": createdAt, "updated_at": updatedAt,
	})
}

func (h *Handler) DeleteProxyRoute(w http.ResponseWriter, r *http.Request) {
	if OperatorFromContext(r.Context()) == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required")
		return
	}
	id := strings.TrimSpace(chi.URLParam(r, "id"))
	if id == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "id is required")
		return
	}
	result, err := h.dbConn().Exec(r.Context(), `DELETE FROM proxy_routes WHERE id = $1`, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Failed to delete proxy route")
		return
	}
	if result.RowsAffected() == 0 {
		writeError(w, http.StatusNotFound, "not_found", "Proxy route not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) listGenericRows(w http.ResponseWriter, r *http.Request, query string, columns []string, envelope string) {
	if OperatorFromContext(r.Context()) == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required")
		return
	}
	rows, err := h.dbConn().Query(r.Context(), query)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Failed to load integration data")
		return
	}
	defer rows.Close()

	items := make([]map[string]interface{}, 0)
	for rows.Next() {
		values, err := rows.Values()
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal_error", "Failed to read integration data")
			return
		}
		item := make(map[string]interface{}, len(columns))
		for i, column := range columns {
			if i < len(values) {
				item[column] = values[i]
			}
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Failed to load integration data")
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		envelope: items,
		"count":  len(items),
	})
}
