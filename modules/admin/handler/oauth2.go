package handler

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	platformcrypto "github.com/aegion/aegion/internal/platform/crypto"
	adminstore "github.com/aegion/aegion/modules/admin/store"
	oauth2store "github.com/aegion/aegion/modules/oauth2/store"
)

var (
	allowedOAuth2GrantTypes = map[string]struct{}{
		"authorization_code": {},
		"refresh_token":      {},
		"client_credentials": {},
		"urn:ietf:params:oauth:grant-type:device_code": {},
	}
	allowedOAuth2ResponseTypes = map[string]struct{}{
		"code": {},
	}
	allowedOAuth2AuthMethods = map[string]struct{}{
		"none":                {},
		"client_secret_basic": {},
		"client_secret_post":  {},
		"client_secret_jwt":   {},
		"private_key_jwt":     {},
	}
	allowedOAuth2SubjectTypes = map[string]struct{}{
		"public":   {},
		"pairwise": {},
	}
	allowedOAuth2AccessTokenStrategies = map[string]struct{}{
		"jwt":    {},
		"opaque": {},
	}
)

type OAuth2ClientRequest struct {
	ID                       string            `json:"id,omitempty"`
	Name                     string            `json:"name"`
	Description              *string           `json:"description,omitempty"`
	LogoURI                  *string           `json:"logo_uri,omitempty"`
	ClientURI                *string           `json:"client_uri,omitempty"`
	PolicyURI                *string           `json:"policy_uri,omitempty"`
	TOSURI                   *string           `json:"tos_uri,omitempty"`
	RedirectURIs             []string          `json:"redirect_uris"`
	PostLogoutRedirectURIs   []string          `json:"post_logout_redirect_uris,omitempty"`
	GrantTypes               []string          `json:"grant_types,omitempty"`
	ResponseTypes            []string          `json:"response_types,omitempty"`
	Scopes                   []string          `json:"scopes,omitempty"`
	Audience                 []string          `json:"audience,omitempty"`
	TokenEndpointAuthMethod  string            `json:"token_endpoint_auth_method,omitempty"`
	JWKSURI                  *string           `json:"jwks_uri,omitempty"`
	JWKS                     json.RawMessage   `json:"jwks,omitempty"`
	SectorIdentifierURI      *string           `json:"sector_identifier_uri,omitempty"`
	SubjectType              string            `json:"subject_type,omitempty"`
	IDTokenSignedResponseAlg string            `json:"id_token_signed_response_alg,omitempty"`
	AccessTokenStrategy      string            `json:"access_token_strategy,omitempty"`
	AccessTokenTTL           *int              `json:"access_token_ttl,omitempty"`
	RefreshTokenTTL          *int              `json:"refresh_token_ttl,omitempty"`
	IDTokenTTL               *int              `json:"id_token_ttl,omitempty"`
	AuthCodeTTL              *int              `json:"auth_code_ttl,omitempty"`
	RequirePKCE              *bool             `json:"require_pkce,omitempty"`
	RequireConsent           *bool             `json:"require_consent,omitempty"`
	AllowOfflineAccess       *bool             `json:"allow_offline_access,omitempty"`
	Metadata                 map[string]string `json:"metadata,omitempty"`
	OwnerID                  *string           `json:"owner_id,omitempty"`
	ClientSecret             string            `json:"client_secret,omitempty"`
}

type OAuth2ClientRotateSecretResponse struct {
	ID           string `json:"id"`
	ClientSecret string `json:"client_secret"`
	RotatedAt    string `json:"rotated_at"`
}

type OAuth2TokenView struct {
	TokenType  string         `json:"token_type"`
	ID         string         `json:"id"`
	ClientID   string         `json:"client_id"`
	IdentityID string         `json:"identity_id"`
	SessionID  string         `json:"session_id"`
	Scopes     []string       `json:"scopes,omitempty"`
	Audience   []string       `json:"audience,omitempty"`
	Status     string         `json:"status"`
	ExpiresAt  time.Time      `json:"expires_at"`
	CreatedAt  time.Time      `json:"created_at"`
	Metadata   map[string]any `json:"metadata,omitempty"`
}

type OAuth2TokenRevokeRequest struct {
	TokenType string `json:"token_type"`
	ID        string `json:"id"`
	Reason    string `json:"reason,omitempty"`
}

func (h *Handler) oauth2Store() *oauth2store.Store {
	return oauth2store.NewWithDB(h.dbConn())
}

func (h *Handler) ListOAuth2Clients(w http.ResponseWriter, r *http.Request) {
	operator := OperatorFromContext(r.Context())
	if operator == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required")
		return
	}

	page, perPage, offset := h.parsePagination(r)
	ownerID := strings.TrimSpace(r.URL.Query().Get("owner_id"))

	var (
		rows pgx.Rows
		err  error
	)
	if ownerID != "" {
		rows, err = h.dbConn().Query(r.Context(), `
			SELECT id, name, description, redirect_uris, grant_types, response_types, scopes,
				token_endpoint_auth_method, require_pkce, require_consent, allow_offline_access, created_at, updated_at
			FROM oa2_clients
			WHERE owner_id = $1
			ORDER BY created_at DESC
			LIMIT $2 OFFSET $3
		`, ownerID, perPage, offset)
	} else {
		rows, err = h.dbConn().Query(r.Context(), `
			SELECT id, name, description, redirect_uris, grant_types, response_types, scopes,
				token_endpoint_auth_method, require_pkce, require_consent, allow_offline_access, created_at, updated_at
			FROM oa2_clients
			ORDER BY created_at DESC
			LIMIT $1 OFFSET $2
		`, perPage, offset)
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Failed to load OAuth2 clients")
		return
	}
	defer rows.Close()

	items := make([]map[string]any, 0)
	for rows.Next() {
		var (
			id, name, tokenEndpointAuthMethod string
			description                       *string
			redirectURIs, grantTypes          []string
			responseTypes, scopes             []string
			requirePKCE, requireConsent       bool
			allowOfflineAccess                bool
			createdAt, updatedAt              time.Time
		)
		if err := rows.Scan(
			&id, &name, &description, &redirectURIs, &grantTypes, &responseTypes, &scopes,
			&tokenEndpointAuthMethod, &requirePKCE, &requireConsent, &allowOfflineAccess, &createdAt, &updatedAt,
		); err != nil {
			writeError(w, http.StatusInternalServerError, "internal_error", "Failed to read OAuth2 client")
			return
		}
		items = append(items, map[string]any{
			"id":                         id,
			"name":                       name,
			"description":                description,
			"redirect_uris":              redirectURIs,
			"grant_types":                grantTypes,
			"response_types":             responseTypes,
			"scopes":                     scopes,
			"token_endpoint_auth_method": tokenEndpointAuthMethod,
			"require_pkce":               requirePKCE,
			"require_consent":            requireConsent,
			"allow_offline_access":       allowOfflineAccess,
			"created_at":                 createdAt,
			"updated_at":                 updatedAt,
		})
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Failed to load OAuth2 clients")
		return
	}

	var total int64
	if ownerID != "" {
		err = h.countValue(r, `SELECT COUNT(*) FROM oa2_clients WHERE owner_id = $1`, &total, ownerID)
	} else {
		err = h.countValue(r, `SELECT COUNT(*) FROM oa2_clients`, &total)
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Failed to count OAuth2 clients")
		return
	}

	h.logAction(r.Context(), &operator.ID, "list", "oauth2_client", "", map[string]any{
		"owner_id": ownerID,
		"count":    len(items),
	}, IPAddressFromContext(r.Context()))

	writeJSON(w, http.StatusOK, map[string]any{
		"items":      items,
		"count":      len(items),
		"pagination": buildPaginationMeta(page, perPage, total),
	})
}

func (h *Handler) GetOAuth2Client(w http.ResponseWriter, r *http.Request) {
	operator := OperatorFromContext(r.Context())
	if operator == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required")
		return
	}

	clientID := strings.TrimSpace(chi.URLParam(r, "id"))
	if clientID == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "client id is required")
		return
	}

	client, err := h.oauth2Store().GetClient(r.Context(), clientID)
	if err != nil {
		if errors.Is(err, oauth2store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "OAuth2 client not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "Failed to load OAuth2 client")
		return
	}

	h.logAction(r.Context(), &operator.ID, "read", "oauth2_client", clientID, nil, IPAddressFromContext(r.Context()))
	writeJSON(w, http.StatusOK, client)
}

func (h *Handler) CreateOAuth2Client(w http.ResponseWriter, r *http.Request) {
	operator := OperatorFromContext(r.Context())
	if operator == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required")
		return
	}

	var req OAuth2ClientRequest
	if err := decodeJSONBody(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "Invalid request body")
		return
	}

	client, plainSecret, err := buildOAuth2ClientFromRequest(nil, req)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	if plainSecret != nil {
		hash, err := hashOAuth2ClientSecret(*plainSecret)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal_error", "Failed to store OAuth2 client secret")
			return
		}
		client.SecretHash = &hash
	}
	if client.OwnerID == nil {
		id := operator.IdentityID.String()
		client.OwnerID = &id
	}

	if err := h.oauth2Store().CreateClient(r.Context(), client); err != nil {
		if errors.Is(err, oauth2store.ErrAlreadyExists) {
			writeError(w, http.StatusConflict, "already_exists", "OAuth2 client already exists")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "Failed to create OAuth2 client")
		return
	}

	h.logAction(r.Context(), &operator.ID, "create", "oauth2_client", client.ID, map[string]any{
		"name":        client.Name,
		"grant_types": client.GrantTypes,
		"scopes":      client.Scopes,
	}, IPAddressFromContext(r.Context()))

	resp := map[string]any{"client": client}
	if plainSecret != nil {
		resp["client_secret"] = *plainSecret
	}
	writeJSON(w, http.StatusCreated, resp)
}

func (h *Handler) UpdateOAuth2Client(w http.ResponseWriter, r *http.Request) {
	operator := OperatorFromContext(r.Context())
	if operator == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required")
		return
	}

	clientID := strings.TrimSpace(chi.URLParam(r, "id"))
	if clientID == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "client id is required")
		return
	}

	current, err := h.oauth2Store().GetClient(r.Context(), clientID)
	if err != nil {
		if errors.Is(err, oauth2store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "OAuth2 client not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "Failed to load OAuth2 client")
		return
	}

	var req OAuth2ClientRequest
	if err := decodeJSONBody(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "Invalid request body")
		return
	}

	updated, plainSecret, err := buildOAuth2ClientFromRequest(current, req)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	updated.ID = current.ID
	updated.SecretHash = current.SecretHash
	if plainSecret != nil {
		hash, err := hashOAuth2ClientSecret(*plainSecret)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal_error", "Failed to update client secret")
			return
		}
		updated.SecretHash = &hash
	}
	if updated.TokenEndpointAuthMethod == "none" {
		updated.SecretHash = nil
	}

	if err := h.oauth2Store().UpdateClient(r.Context(), updated); err != nil {
		if errors.Is(err, oauth2store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "OAuth2 client not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "Failed to update OAuth2 client")
		return
	}
	if plainSecret != nil && updated.TokenEndpointAuthMethod != "none" && updated.SecretHash != nil {
		if err := h.oauth2Store().UpdateClientSecret(r.Context(), updated.ID, *updated.SecretHash); err != nil {
			writeError(w, http.StatusInternalServerError, "internal_error", "Failed to rotate OAuth2 client secret")
			return
		}
	}

	h.logAction(r.Context(), &operator.ID, "update", "oauth2_client", updated.ID, map[string]any{
		"name":        updated.Name,
		"grant_types": updated.GrantTypes,
		"scopes":      updated.Scopes,
	}, IPAddressFromContext(r.Context()))

	resp := map[string]any{"client": updated}
	if plainSecret != nil {
		resp["client_secret"] = *plainSecret
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) DeleteOAuth2Client(w http.ResponseWriter, r *http.Request) {
	operator := OperatorFromContext(r.Context())
	if operator == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required")
		return
	}

	clientID := strings.TrimSpace(chi.URLParam(r, "id"))
	if clientID == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "client id is required")
		return
	}

	client, err := h.oauth2Store().GetClient(r.Context(), clientID)
	if err != nil {
		if errors.Is(err, oauth2store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "OAuth2 client not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "Failed to load OAuth2 client")
		return
	}
	if err := h.oauth2Store().DeleteClient(r.Context(), clientID); err != nil {
		if errors.Is(err, oauth2store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "OAuth2 client not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "Failed to delete OAuth2 client")
		return
	}

	h.logAction(r.Context(), &operator.ID, "delete", "oauth2_client", clientID, map[string]any{
		"name": client.Name,
	}, IPAddressFromContext(r.Context()))
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) RotateOAuth2ClientSecret(w http.ResponseWriter, r *http.Request) {
	operator := OperatorFromContext(r.Context())
	if operator == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required")
		return
	}

	clientID := strings.TrimSpace(chi.URLParam(r, "id"))
	if clientID == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "client id is required")
		return
	}

	client, err := h.oauth2Store().GetClient(r.Context(), clientID)
	if err != nil {
		if errors.Is(err, oauth2store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "OAuth2 client not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "Failed to load OAuth2 client")
		return
	}
	if client.TokenEndpointAuthMethod == "none" {
		writeError(w, http.StatusBadRequest, "invalid_request", "Public clients do not use a client secret")
		return
	}

	secret, err := generateOAuth2ClientSecret()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Failed to generate OAuth2 client secret")
		return
	}
	hash, err := hashOAuth2ClientSecret(secret)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Failed to store OAuth2 client secret")
		return
	}
	if err := h.oauth2Store().UpdateClientSecret(r.Context(), clientID, hash); err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Failed to rotate OAuth2 client secret")
		return
	}

	h.logAction(r.Context(), &operator.ID, "rotate_secret", "oauth2_client", clientID, nil, IPAddressFromContext(r.Context()))
	writeJSON(w, http.StatusOK, OAuth2ClientRotateSecretResponse{
		ID:           clientID,
		ClientSecret: secret,
		RotatedAt:    time.Now().UTC().Format(time.RFC3339),
	})
}

func (h *Handler) ListOAuth2Tokens(w http.ResponseWriter, r *http.Request) {
	operator := OperatorFromContext(r.Context())
	if operator == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required")
		return
	}

	page, perPage, offset := h.parsePagination(r)
	tokenType := strings.TrimSpace(r.URL.Query().Get("token_type"))
	clientID := strings.TrimSpace(r.URL.Query().Get("client_id"))
	identityID := strings.TrimSpace(r.URL.Query().Get("identity_id"))

	whereAccess := make([]string, 0)
	whereRefresh := make([]string, 0)
	whereID := make([]string, 0)
	args := make([]any, 0)
	argPos := 1

	if clientID != "" {
		whereAccess = append(whereAccess, "client_id = $"+strconv.Itoa(argPos))
		whereRefresh = append(whereRefresh, "client_id = $"+strconv.Itoa(argPos))
		whereID = append(whereID, "client_id = $"+strconv.Itoa(argPos))
		args = append(args, clientID)
		argPos++
	}
	if identityID != "" {
		whereAccess = append(whereAccess, "identity_id = $"+strconv.Itoa(argPos))
		whereRefresh = append(whereRefresh, "identity_id = $"+strconv.Itoa(argPos))
		whereID = append(whereID, "identity_id = $"+strconv.Itoa(argPos))
		args = append(args, identityID)
		argPos++
	}

	unionQueries := make([]string, 0, 3)
	if tokenType == "" || tokenType == "access_token" {
		unionQueries = append(unionQueries, `
			SELECT 'access_token' AS token_type, jti AS id, client_id, identity_id, session_id, scopes, audience,
				CASE WHEN revoked THEN 'revoked' WHEN expires_at <= NOW() THEN 'expired' ELSE 'active' END AS status,
				expires_at, created_at, extra_claims
			FROM oa2_access_tokens`+buildWhereClause(whereAccess))
	}
	if tokenType == "" || tokenType == "refresh_token" {
		unionQueries = append(unionQueries, `
			SELECT 'refresh_token' AS token_type, id, client_id, identity_id, session_id, scopes, audience,
				CASE WHEN active = false THEN 'revoked' WHEN expires_at <= NOW() THEN 'expired' WHEN used THEN 'used' ELSE 'active' END AS status,
				expires_at, created_at, extra_claims
			FROM oa2_refresh_tokens`+buildWhereClause(whereRefresh))
	}
	if tokenType == "" || tokenType == "id_token" {
		unionQueries = append(unionQueries, `
			SELECT 'id_token' AS token_type, jti AS id, client_id, identity_id, session_id, ARRAY[]::text[] AS scopes, ARRAY[]::text[] AS audience,
				CASE WHEN revoked THEN 'revoked' WHEN expires_at <= NOW() THEN 'expired' ELSE 'active' END AS status,
				expires_at, created_at, extra_claims
			FROM oa2_id_tokens`+buildWhereClause(whereID))
	}
	if len(unionQueries) == 0 {
		writeError(w, http.StatusBadRequest, "invalid_request", "Unsupported token_type")
		return
	}

	countQuery := `SELECT COUNT(*) FROM (` + strings.Join(unionQueries, ` UNION ALL `) + `) t`
	var total int64
	if err := h.dbConn().QueryRow(r.Context(), countQuery, args...).Scan(&total); err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Failed to count OAuth2 tokens")
		return
	}

	argsWithPaging := append(append([]any{}, args...), perPage, offset)
	query := `SELECT token_type, id, client_id, identity_id, session_id, scopes, audience, status, expires_at, created_at, extra_claims
		FROM (` + strings.Join(unionQueries, ` UNION ALL `) + `) t
		ORDER BY created_at DESC
		LIMIT $` + strconv.Itoa(argPos) + ` OFFSET $` + strconv.Itoa(argPos+1)

	rows, err := h.dbConn().Query(r.Context(), query, argsWithPaging...)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Failed to load OAuth2 tokens")
		return
	}
	defer rows.Close()

	items := make([]OAuth2TokenView, 0)
	for rows.Next() {
		var (
			item      OAuth2TokenView
			rawClaims []byte
		)
		if err := rows.Scan(
			&item.TokenType, &item.ID, &item.ClientID, &item.IdentityID, &item.SessionID,
			&item.Scopes, &item.Audience, &item.Status, &item.ExpiresAt, &item.CreatedAt, &rawClaims,
		); err != nil {
			writeError(w, http.StatusInternalServerError, "internal_error", "Failed to read OAuth2 tokens")
			return
		}
		if len(rawClaims) > 0 {
			_ = json.Unmarshal(rawClaims, &item.Metadata)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Failed to load OAuth2 tokens")
		return
	}

	h.logAction(r.Context(), &operator.ID, "list", "oauth2_token", "", map[string]any{
		"token_type":  tokenType,
		"client_id":   clientID,
		"identity_id": identityID,
		"count":       len(items),
	}, IPAddressFromContext(r.Context()))

	writeJSON(w, http.StatusOK, map[string]any{
		"items":      items,
		"count":      len(items),
		"pagination": buildPaginationMeta(page, perPage, total),
	})
}

func (h *Handler) RevokeOAuth2Token(w http.ResponseWriter, r *http.Request) {
	operator := OperatorFromContext(r.Context())
	if operator == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required")
		return
	}

	var req OAuth2TokenRevokeRequest
	if err := decodeJSONBody(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "Invalid request body")
		return
	}
	req.TokenType = strings.TrimSpace(req.TokenType)
	req.ID = strings.TrimSpace(req.ID)
	req.Reason = strings.TrimSpace(req.Reason)
	if req.TokenType == "" || req.ID == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "token_type and id are required")
		return
	}

	switch req.TokenType {
	case "access_token":
		token, err := h.oauth2Store().GetAccessToken(r.Context(), req.ID)
		if err != nil {
			if errors.Is(err, oauth2store.ErrNotFound) {
				writeError(w, http.StatusNotFound, "not_found", "OAuth2 token not found")
				return
			}
			writeError(w, http.StatusInternalServerError, "internal_error", "Failed to load OAuth2 token")
			return
		}
		if err := h.oauth2Store().RevokeAccessToken(r.Context(), req.ID); err != nil && !errors.Is(err, oauth2store.ErrNotFound) {
			writeError(w, http.StatusInternalServerError, "internal_error", "Failed to revoke OAuth2 token")
			return
		}
		_ = h.recordOAuth2TokenRevocation(r, req.TokenType, req.ID, token.ClientID, token.IdentityID, token.ExpiresAt, req.Reason, operator)
	case "refresh_token":
		token, err := h.oauth2Store().GetRefreshToken(r.Context(), req.ID)
		if err != nil {
			if errors.Is(err, oauth2store.ErrNotFound) {
				writeError(w, http.StatusNotFound, "not_found", "OAuth2 token not found")
				return
			}
			writeError(w, http.StatusInternalServerError, "internal_error", "Failed to load OAuth2 token")
			return
		}
		if _, err := h.dbConn().Exec(r.Context(), `UPDATE oa2_refresh_tokens SET active = false WHERE id = $1`, req.ID); err != nil {
			writeError(w, http.StatusInternalServerError, "internal_error", "Failed to revoke OAuth2 token")
			return
		}
		_ = h.recordOAuth2TokenRevocation(r, req.TokenType, req.ID, token.ClientID, token.IdentityID, token.ExpiresAt, req.Reason, operator)
	case "id_token":
		var (
			clientID   string
			identityID string
			expiresAt  time.Time
		)
		err := h.dbConn().QueryRow(r.Context(), `
			SELECT client_id, identity_id, expires_at
			FROM oa2_id_tokens
			WHERE jti = $1
		`, req.ID).Scan(&clientID, &identityID, &expiresAt)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				writeError(w, http.StatusNotFound, "not_found", "OAuth2 token not found")
				return
			}
			writeError(w, http.StatusInternalServerError, "internal_error", "Failed to load OAuth2 token")
			return
		}
		if _, err := h.dbConn().Exec(r.Context(), `UPDATE oa2_id_tokens SET revoked = true WHERE jti = $1`, req.ID); err != nil {
			writeError(w, http.StatusInternalServerError, "internal_error", "Failed to revoke OAuth2 token")
			return
		}
		_ = h.recordOAuth2TokenRevocation(r, req.TokenType, req.ID, clientID, identityID, expiresAt, req.Reason, operator)
	default:
		writeError(w, http.StatusBadRequest, "invalid_request", "Unsupported token_type")
		return
	}

	h.logAction(r.Context(), &operator.ID, "revoke", "oauth2_token", req.ID, map[string]any{
		"token_type": req.TokenType,
		"reason":     req.Reason,
	}, IPAddressFromContext(r.Context()))

	writeJSON(w, http.StatusOK, map[string]any{
		"id":         req.ID,
		"token_type": req.TokenType,
		"revoked":    true,
	})
}

func buildOAuth2ClientFromRequest(existing *oauth2store.Client, req OAuth2ClientRequest) (*oauth2store.Client, *string, error) {
	client := &oauth2store.Client{}
	if existing != nil {
		*client = *existing
	}

	name := strings.TrimSpace(req.Name)
	if existing == nil || name != "" {
		client.Name = name
	}
	if strings.TrimSpace(client.Name) == "" {
		return nil, nil, errors.New("name is required")
	}

	if req.Description != nil || existing == nil {
		client.Description = trimOptionalString(req.Description)
	}
	if req.LogoURI != nil || existing == nil {
		client.LogoURI = trimOptionalString(req.LogoURI)
	}
	if req.ClientURI != nil || existing == nil {
		client.ClientURI = trimOptionalString(req.ClientURI)
	}
	if req.PolicyURI != nil || existing == nil {
		client.PolicyURI = trimOptionalString(req.PolicyURI)
	}
	if req.TOSURI != nil || existing == nil {
		client.TOSURI = trimOptionalString(req.TOSURI)
	}
	if req.JWKSURI != nil || existing == nil {
		client.JWKSURI = trimOptionalString(req.JWKSURI)
	}
	if req.SectorIdentifierURI != nil || existing == nil {
		client.SectorIdentifierURI = trimOptionalString(req.SectorIdentifierURI)
	}
	if req.OwnerID != nil || existing == nil {
		client.OwnerID = trimOptionalString(req.OwnerID)
	}

	if req.RedirectURIs != nil || existing == nil {
		redirectURIs, err := normalizeRedirectURIs(req.RedirectURIs)
		if err != nil {
			return nil, nil, err
		}
		client.RedirectURIs = redirectURIs
	}
	if len(client.RedirectURIs) == 0 {
		return nil, nil, errors.New("at least one redirect URI is required")
	}
	if req.PostLogoutRedirectURIs != nil || existing == nil {
		postLogout, err := normalizeRedirectURIs(req.PostLogoutRedirectURIs)
		if err != nil {
			return nil, nil, err
		}
		client.PostLogoutRedirectURIs = postLogout
	}
	if req.GrantTypes != nil || existing == nil {
		grantTypes, err := normalizeStringSet(req.GrantTypes, allowedOAuth2GrantTypes, []string{"authorization_code"}, true)
		if err != nil {
			return nil, nil, err
		}
		client.GrantTypes = grantTypes
	}
	if req.ResponseTypes != nil || existing == nil {
		responseTypes, err := normalizeStringSet(req.ResponseTypes, allowedOAuth2ResponseTypes, []string{"code"}, true)
		if err != nil {
			return nil, nil, err
		}
		client.ResponseTypes = responseTypes
	}
	if req.Scopes != nil || existing == nil {
		client.Scopes = normalizeGenericStringSet(req.Scopes, []string{"openid"})
	}
	if req.Audience != nil || existing == nil {
		client.Audience = normalizeGenericStringSet(req.Audience, nil)
	}

	if method := strings.TrimSpace(req.TokenEndpointAuthMethod); method != "" || existing == nil {
		if method == "" {
			method = "client_secret_basic"
		}
		if _, ok := allowedOAuth2AuthMethods[method]; !ok {
			return nil, nil, errors.New("unsupported token_endpoint_auth_method")
		}
		client.TokenEndpointAuthMethod = method
	}
	if req.JWKS != nil || existing == nil {
		if len(req.JWKS) == 0 {
			client.JWKS = nil
		} else {
			client.JWKS = append([]byte(nil), req.JWKS...)
		}
	}
	if subjectType := strings.TrimSpace(req.SubjectType); subjectType != "" || existing == nil {
		if subjectType == "" {
			subjectType = "public"
		}
		if _, ok := allowedOAuth2SubjectTypes[subjectType]; !ok {
			return nil, nil, errors.New("unsupported subject_type")
		}
		client.SubjectType = subjectType
	}
	if alg := strings.TrimSpace(req.IDTokenSignedResponseAlg); alg != "" || existing == nil {
		if alg == "" {
			alg = "RS256"
		}
		client.IDTokenSignedResponseAlg = alg
	}
	if strategy := strings.TrimSpace(req.AccessTokenStrategy); strategy != "" || existing == nil {
		if strategy == "" {
			strategy = "jwt"
		}
		if _, ok := allowedOAuth2AccessTokenStrategies[strategy]; !ok {
			return nil, nil, errors.New("unsupported access_token_strategy")
		}
		client.AccessTokenStrategy = strategy
	}
	if req.AccessTokenTTL != nil || existing == nil {
		client.AccessTokenTTL = normalizedTTL(req.AccessTokenTTL, 900)
	}
	if req.RefreshTokenTTL != nil || existing == nil {
		client.RefreshTokenTTL = normalizedTTL(req.RefreshTokenTTL, 2592000)
	}
	if req.IDTokenTTL != nil || existing == nil {
		client.IDTokenTTL = normalizedTTL(req.IDTokenTTL, 3600)
	}
	if req.AuthCodeTTL != nil || existing == nil {
		client.AuthCodeTTL = normalizedTTL(req.AuthCodeTTL, 600)
	}
	if client.AccessTokenTTL <= 0 || client.RefreshTokenTTL <= 0 || client.IDTokenTTL <= 0 || client.AuthCodeTTL <= 0 {
		return nil, nil, errors.New("ttl values must be greater than zero")
	}
	if req.RequirePKCE != nil || existing == nil {
		client.RequirePKCE = normalizedBool(req.RequirePKCE, true)
	}
	if req.RequireConsent != nil || existing == nil {
		client.RequireConsent = normalizedBool(req.RequireConsent, true)
	}
	if req.AllowOfflineAccess != nil || existing == nil {
		client.AllowOfflineAccess = normalizedBool(req.AllowOfflineAccess, true)
	}
	if req.Metadata != nil || existing == nil {
		client.Metadata = normalizeMetadata(req.Metadata)
	}
	if client.TokenEndpointAuthMethod == "none" {
		client.RequirePKCE = true
	}
	if client.TokenEndpointAuthMethod == "private_key_jwt" && client.JWKSURI == nil && len(client.JWKS) == 0 {
		return nil, nil, errors.New("private_key_jwt clients require jwks_uri or jwks")
	}

	var plainSecret *string
	if req.ClientSecret != "" {
		if client.TokenEndpointAuthMethod == "none" {
			return nil, nil, errors.New("public clients cannot set a client_secret")
		}
		secret := strings.TrimSpace(req.ClientSecret)
		plainSecret = &secret
	} else if existing == nil && client.TokenEndpointAuthMethod != "none" {
		secret, err := generateOAuth2ClientSecret()
		if err != nil {
			return nil, nil, err
		}
		plainSecret = &secret
	}

	if client.ID == "" {
		client.ID = strings.TrimSpace(req.ID)
	}
	return client, plainSecret, nil
}

func normalizeRedirectURIs(values []string) ([]string, error) {
	out := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		parsed, err := url.Parse(trimmed)
		if err != nil || parsed.Scheme == "" || parsed.Host == "" {
			return nil, errors.New("redirect URIs must be absolute")
		}
		if strings.Contains(trimmed, "*") {
			return nil, errors.New("wildcards are not allowed in redirect URIs")
		}
		if parsed.Fragment != "" {
			return nil, errors.New("redirect URIs cannot include fragments")
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
	}
	sort.Strings(out)
	return out, nil
}

func normalizeStringSet(values []string, allowed map[string]struct{}, defaults []string, lowercase bool) ([]string, error) {
	if len(values) == 0 {
		return append([]string(nil), defaults...), nil
	}
	out := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if lowercase {
			trimmed = strings.ToLower(trimmed)
		}
		if trimmed == "" {
			continue
		}
		if _, ok := allowed[trimmed]; !ok {
			return nil, errors.New("unsupported value: " + trimmed)
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
	}
	sort.Strings(out)
	if len(out) == 0 {
		return append([]string(nil), defaults...), nil
	}
	return out, nil
}

func normalizeGenericStringSet(values []string, defaults []string) []string {
	if len(values) == 0 {
		return append([]string(nil), defaults...)
	}
	out := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
	}
	sort.Strings(out)
	return out
}

func normalizeMetadata(input map[string]string) map[string]string {
	if len(input) == 0 {
		return nil
	}
	out := make(map[string]string, len(input))
	for key, value := range input {
		trimmedKey := strings.TrimSpace(key)
		if trimmedKey == "" {
			continue
		}
		out[trimmedKey] = strings.TrimSpace(value)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func normalizedTTL(value *int, fallback int) int {
	if value == nil {
		return fallback
	}
	return *value
}

func normalizedBool(value *bool, fallback bool) bool {
	if value == nil {
		return fallback
	}
	return *value
}

func trimOptionalString(value *string) *string {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func generateOAuth2ClientSecret() (string, error) {
	b, err := platformcrypto.RandomBytes(32)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func hashOAuth2ClientSecret(secret string) (string, error) {
	return platformcrypto.HashPassword(secret)
}

func (h *Handler) recordOAuth2TokenRevocation(r *http.Request, tokenType, tokenID, clientID, identityID string, expiresAt time.Time, reason string, operator *adminstore.Operator) error {
	_, err := h.dbConn().Exec(r.Context(), `
		INSERT INTO oa2_token_revocations (jti, token_type, client_id, identity_id, reason, revoked_by, expires_at, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, NOW())
		ON CONFLICT (jti) DO UPDATE SET
			token_type = EXCLUDED.token_type,
			client_id = EXCLUDED.client_id,
			identity_id = EXCLUDED.identity_id,
			reason = EXCLUDED.reason,
			revoked_by = EXCLUDED.revoked_by,
			expires_at = EXCLUDED.expires_at
	`, tokenID, tokenType, clientID, identityID, reason, operator.IdentityID.String(), expiresAt)
	return err
}

func buildWhereClause(parts []string) string {
	if len(parts) == 0 {
		return ""
	}
	return " WHERE " + strings.Join(parts, " AND ")
}
