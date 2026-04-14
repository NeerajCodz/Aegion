package handler

import "net/http"

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

func (h *Handler) ListSSOConnections(w http.ResponseWriter, r *http.Request) {
	h.listGenericRows(w, r, `
		SELECT slug, display_name, entity_id, metadata_url, enabled, jit_provisioning, created_at, updated_at
		FROM sso_connections
		ORDER BY display_name ASC, slug ASC
	`, []string{"slug", "display_name", "entity_id", "metadata_url", "enabled", "jit_provisioning", "created_at", "updated_at"}, "connections")
}

func (h *Handler) ListProxyUpstreams(w http.ResponseWriter, r *http.Request) {
	h.listGenericRows(w, r, `
		SELECT name, url, health_check, timeout, max_connections, enabled, created_at, updated_at
		FROM proxy_upstreams
		ORDER BY name ASC
	`, []string{"name", "url", "health_check", "timeout", "max_connections", "enabled", "created_at", "updated_at"}, "upstreams")
}

func (h *Handler) ListProxyRoutes(w http.ResponseWriter, r *http.Request) {
	h.listGenericRows(w, r, `
		SELECT id, path, target, require_auth, required_aal, priority, enabled, description, created_at, updated_at
		FROM proxy_routes
		ORDER BY priority DESC, id ASC
	`, []string{"id", "path", "target", "require_auth", "required_aal", "priority", "enabled", "description", "created_at", "updated_at"}, "routes")
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
