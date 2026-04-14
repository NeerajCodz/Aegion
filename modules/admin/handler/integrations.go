package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/aegion/aegion/modules/admin/service"
	adminstore "github.com/aegion/aegion/modules/admin/store"
	"github.com/aegion/aegion/modules/social/providers/catalog"
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

type SetupStatusResponse struct {
	Operators         int64 `json:"operators"`
	Roles             int64 `json:"roles"`
	APIKeys           int64 `json:"api_keys"`
	SocialProviders   int64 `json:"social_providers"`
	SocialEnabled     int64 `json:"social_enabled"`
	SocialLinks       int64 `json:"social_links"`
	SSOConnections    int64 `json:"sso_connections"`
	SSOEnabled        int64 `json:"sso_enabled"`
	ProxyUpstreams    int64 `json:"proxy_upstreams"`
	ProxyRoutes       int64 `json:"proxy_routes"`
	ProxyEnabled      int64 `json:"proxy_enabled"`
	SCIMTokens        int64 `json:"scim_tokens"`
	AuditEvents24h    int64 `json:"audit_events_24h"`
	AdminOperators    int64 `json:"admin_operators"`
	HasAdminOperator  bool  `json:"has_admin_operator"`
	HasSocialProvider bool  `json:"has_social_provider"`
	HasSSOConnection  bool  `json:"has_sso_connection"`
	HasProxyRoute     bool  `json:"has_proxy_route"`
	HasSCIMToken      bool  `json:"has_scim_token"`
}

type SocialPresetView struct {
	Slug               string                   `json:"slug"`
	DisplayName        string                   `json:"display_name"`
	Preset             string                   `json:"preset"`
	Protocol           socialstore.Protocol     `json:"protocol"`
	Issuer             string                   `json:"issuer,omitempty"`
	DiscoveryURL       string                   `json:"discovery_url,omitempty"`
	AuthorizeEndpoint  string                   `json:"authorize_endpoint,omitempty"`
	TokenEndpoint      string                   `json:"token_endpoint,omitempty"`
	UserInfoEndpoint   string                   `json:"userinfo_endpoint,omitempty"`
	JWKSURI            string                   `json:"jwks_uri,omitempty"`
	Scopes             []string                 `json:"scopes,omitempty"`
	ClaimMapping       socialstore.ClaimMapping `json:"claim_mapping"`
	ExtraAuthParams    map[string]string        `json:"extra_auth_params,omitempty"`
	PKCEMethod         socialstore.PKCEMethod   `json:"pkce_method"`
	AuthStyle          socialstore.AuthStyle    `json:"auth_style"`
	ClaimSource        socialstore.ClaimSource  `json:"claim_source"`
	TrustEmailVerified bool                     `json:"trust_email_verified"`
}

type RoleSummary struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Permissions []string `json:"permissions"`
	IsSystem    bool     `json:"is_system"`
	OperatorIDs int64    `json:"operator_ids"`
}

type ActivityItem struct {
	ID           string                 `json:"id"`
	OperatorID   *string                `json:"operator_id,omitempty"`
	Action       string                 `json:"action"`
	ResourceType string                 `json:"resource_type"`
	ResourceID   string                 `json:"resource_id"`
	Details      map[string]interface{} `json:"details,omitempty"`
	IPAddress    string                 `json:"ip_address"`
	CreatedAt    time.Time              `json:"created_at"`
}

func (h *Handler) IntegrationOverview(w http.ResponseWriter, r *http.Request) {
	if OperatorFromContext(r.Context()) == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required")
		return
	}

	var resp IntegrationOverviewResponse
	if err := h.countValue(r, `SELECT COUNT(*) FROM soc_providers`, &resp.SocialProviders); err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Failed to load social provider count")
		return
	}
	if err := h.countValue(r, `SELECT COUNT(*) FROM soc_identity_links`, &resp.SocialLinks); err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Failed to load social link count")
		return
	}
	if err := h.countValue(r, `SELECT COUNT(*) FROM sso_connections`, &resp.SSOConnections); err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Failed to load SSO connection count")
		return
	}
	if err := h.countValue(r, `SELECT COUNT(*) FROM proxy_upstreams`, &resp.ProxyUpstreams); err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Failed to load proxy upstream count")
		return
	}
	if err := h.countValue(r, `SELECT COUNT(*) FROM proxy_routes`, &resp.ProxyRoutes); err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Failed to load proxy route count")
		return
	}
	if err := h.countValue(r, `SELECT COUNT(*) FROM adm_scim_tokens`, &resp.SCIMTokens); err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Failed to load SCIM token count")
		return
	}

	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) SetupStatus(w http.ResponseWriter, r *http.Request) {
	if OperatorFromContext(r.Context()) == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required")
		return
	}

	var resp SetupStatusResponse
	queries := []struct {
		sql    string
		target *int64
	}{
		{`SELECT COUNT(*) FROM adm_operators`, &resp.Operators},
		{`SELECT COUNT(*) FROM adm_roles`, &resp.Roles},
		{`SELECT COUNT(*) FROM adm_api_keys`, &resp.APIKeys},
		{`SELECT COUNT(*) FROM soc_providers`, &resp.SocialProviders},
		{`SELECT COUNT(*) FROM soc_providers WHERE enabled = true`, &resp.SocialEnabled},
		{`SELECT COUNT(*) FROM soc_identity_links`, &resp.SocialLinks},
		{`SELECT COUNT(*) FROM sso_connections`, &resp.SSOConnections},
		{`SELECT COUNT(*) FROM sso_connections WHERE enabled = true`, &resp.SSOEnabled},
		{`SELECT COUNT(*) FROM proxy_upstreams`, &resp.ProxyUpstreams},
		{`SELECT COUNT(*) FROM proxy_routes`, &resp.ProxyRoutes},
		{`SELECT COUNT(*) FROM proxy_routes WHERE enabled = true`, &resp.ProxyEnabled},
		{`SELECT COUNT(*) FROM adm_scim_tokens`, &resp.SCIMTokens},
		{`SELECT COUNT(*) FROM adm_audit_logs WHERE created_at >= NOW() - INTERVAL '24 hours'`, &resp.AuditEvents24h},
	}
	for _, query := range queries {
		if err := h.countValue(r, query.sql, query.target); err != nil {
			writeError(w, http.StatusInternalServerError, "internal_error", "Failed to load setup status")
			return
		}
	}
	if err := h.countValue(r, `SELECT COUNT(*) FROM adm_operators WHERE role IN ('super_admin', 'admin')`, &resp.AdminOperators); err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Failed to load admin operator status")
		return
	}

	resp.HasAdminOperator = resp.AdminOperators > 0
	resp.HasSocialProvider = resp.SocialProviders > 0
	resp.HasSSOConnection = resp.SSOConnections > 0
	resp.HasProxyRoute = resp.ProxyRoutes > 0
	resp.HasSCIMToken = resp.SCIMTokens > 0

	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) ListSocialPresets(w http.ResponseWriter, r *http.Request) {
	if OperatorFromContext(r.Context()) == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required")
		return
	}

	presets := catalog.All()
	items := make([]SocialPresetView, 0, len(presets))
	for _, provider := range presets {
		items = append(items, SocialPresetView{
			Slug:               provider.Slug,
			DisplayName:        provider.DisplayName,
			Preset:             provider.Preset,
			Protocol:           provider.Protocol,
			Issuer:             provider.Issuer,
			DiscoveryURL:       provider.DiscoveryURL,
			AuthorizeEndpoint:  provider.AuthorizeEndpoint,
			TokenEndpoint:      provider.TokenEndpoint,
			UserInfoEndpoint:   provider.UserInfoEndpoint,
			JWKSURI:            provider.JWKSURI,
			Scopes:             append([]string(nil), provider.Scopes...),
			ClaimMapping:       provider.ClaimMapping,
			ExtraAuthParams:    provider.ExtraAuthParams,
			PKCEMethod:         provider.PKCEMethod,
			AuthStyle:          provider.AuthStyle,
			ClaimSource:        provider.ClaimSource,
			TrustEmailVerified: provider.TrustEmailVerified,
		})
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"presets": items,
		"count":   len(items),
	})
}

func (h *Handler) RBACSummary(w http.ResponseWriter, r *http.Request) {
	operator := OperatorFromContext(r.Context())
	if operator == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required")
		return
	}

	roles, _, err := h.service.ListRoles(r.Context(), operator.ID, h.config.MaxPageSize, 0)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Failed to load RBAC summary")
		return
	}

	available := h.service.AvailablePermissions()
	sort.Strings(available)
	roleItems := make([]RoleSummary, 0, len(roles))
	for _, role := range roles {
		var count int64
		if err := h.countValue(r, `SELECT COUNT(*) FROM adm_operators WHERE role = $1`, &count, role.Name); err != nil {
			writeError(w, http.StatusInternalServerError, "internal_error", "Failed to load RBAC summary")
			return
		}
		perms := append([]string(nil), role.Permissions...)
		sort.Strings(perms)
		roleItems = append(roleItems, RoleSummary{
			ID:          role.ID.String(),
			Name:        role.Name,
			Description: role.Description,
			Permissions: perms,
			IsSystem:    role.IsSystem,
			OperatorIDs: count,
		})
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"available_permissions": available,
		"default_roles":         service.DefaultRolePermissions,
		"roles":                 roleItems,
		"count":                 len(roleItems),
	})
}

func (h *Handler) ActivityFeed(w http.ResponseWriter, r *http.Request) {
	operator := OperatorFromContext(r.Context())
	if operator == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required")
		return
	}

	page, perPage, offset := h.parsePagination(r)
	if perPage > 50 {
		perPage = 50
	}

	entries, total, err := h.service.ListAuditLogs(r.Context(), operator.ID, adminstore.AuditFilter{}, perPage, offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Failed to load activity feed")
		return
	}

	items := make([]ActivityItem, 0, len(entries))
	for _, entry := range entries {
		var operatorID *string
		if entry.OperatorID != nil {
			id := entry.OperatorID.String()
			operatorID = &id
		}
		items = append(items, ActivityItem{
			ID:           entry.ID.String(),
			OperatorID:   operatorID,
			Action:       entry.Action,
			ResourceType: entry.ResourceType,
			ResourceID:   entry.ResourceID,
			Details:      entry.Details,
			IPAddress:    entry.IPAddress,
			CreatedAt:    entry.CreatedAt,
		})
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"items":       items,
		"count":       len(items),
		"pagination":  buildPaginationMeta(page, perPage, total),
		"total_items": total,
	})
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

func (h *Handler) countValue(r *http.Request, sql string, target *int64, args ...interface{}) error {
	return h.dbConn().QueryRow(r.Context(), sql, args...).Scan(target)
}
