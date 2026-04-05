// Package handler provides HTTP handlers for OAuth2 endpoints.
package handler

import (
	"encoding/json"
	"net/http"

	"github.com/aegion/aegion/modules/oauth2/service/authorization"
	"github.com/aegion/aegion/modules/oauth2/service/device"
	"github.com/aegion/aegion/modules/oauth2/service/grants"
	"github.com/aegion/aegion/modules/oauth2/service/oidc"
	"github.com/aegion/aegion/modules/oauth2/service/revocation"
	"github.com/aegion/aegion/modules/oauth2/service/token"
)

// OAuth2Handler handles all OAuth2 HTTP endpoints.
type OAuth2Handler struct {
	authzSvc       *authorization.AuthorizationService
	tokenSvc       *token.TokenService
	revocationSvc  *revocation.RevocationService
	deviceSvc      *device.DeviceService
	clientCredsSvc *grants.ClientCredentialsService
	jwtBearerSvc   *grants.JWTBearerService
	discoverySvc   *oidc.DiscoveryService
	jwksSvc        *oidc.JWKSService
	userInfoSvc    *oidc.UserInfoService
}

// NewOAuth2Handler creates a new OAuth2 handler.
func NewOAuth2Handler(
	authzSvc *authorization.AuthorizationService,
	tokenSvc *token.TokenService,
	revocationSvc *revocation.RevocationService,
	deviceSvc *device.DeviceService,
	clientCredsSvc *grants.ClientCredentialsService,
	jwtBearerSvc *grants.JWTBearerService,
	discoverySvc *oidc.DiscoveryService,
	jwksSvc *oidc.JWKSService,
	userInfoSvc *oidc.UserInfoService,
) *OAuth2Handler {
	return &OAuth2Handler{
		authzSvc:       authzSvc,
		tokenSvc:       tokenSvc,
		revocationSvc:  revocationSvc,
		deviceSvc:      deviceSvc,
		clientCredsSvc: clientCredsSvc,
		jwtBearerSvc:   jwtBearerSvc,
		discoverySvc:   discoverySvc,
		jwksSvc:        jwksSvc,
		userInfoSvc:    userInfoSvc,
	}
}

// HandleAuthorize handles GET /oauth2/authorize
func (h *OAuth2Handler) HandleAuthorize(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, "invalid_request", "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	req := &authorization.AuthorizeRequest{
		ResponseType:        r.URL.Query().Get("response_type"),
		ClientID:            r.URL.Query().Get("client_id"),
		RedirectURI:         r.URL.Query().Get("redirect_uri"),
		Scope:               r.URL.Query().Get("scope"),
		State:               r.URL.Query().Get("state"),
		Nonce:               r.URL.Query().Get("nonce"),
		CodeChallenge:       r.URL.Query().Get("code_challenge"),
		CodeChallengeMethod: r.URL.Query().Get("code_challenge_method"),
	}

	challenge, err := h.authzSvc.StartAuthorization(r.Context(), req)
	if err != nil {
		writeError(w, "invalid_request", err.Error(), http.StatusBadRequest)
		return
	}

	writeJSON(w, http.StatusOK, challenge)
}

// HandleToken handles POST /oauth2/token
func (h *OAuth2Handler) HandleToken(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, "invalid_request", "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if err := r.ParseForm(); err != nil {
		writeError(w, "invalid_request", "Failed to parse form", http.StatusBadRequest)
		return
	}

	grantType := r.FormValue("grant_type")

	switch grantType {
	case "authorization_code":
		h.handleAuthorizationCodeGrant(w, r)
	case "refresh_token":
		h.handleRefreshTokenGrant(w, r)
	case "client_credentials":
		h.handleClientCredentialsGrant(w, r)
	case "urn:ietf:params:oauth:grant-type:device_code":
		h.handleDeviceCodeGrant(w, r)
	case "urn:ietf:params:oauth:grant-type:jwt-bearer":
		h.handleJWTBearerGrant(w, r)
	default:
		writeError(w, "unsupported_grant_type", "Grant type not supported", http.StatusBadRequest)
	}
}

func (h *OAuth2Handler) handleAuthorizationCodeGrant(w http.ResponseWriter, r *http.Request) {
	clientID, clientSecret := extractClientCredentials(r)

	req := &token.TokenRequest{
		GrantType:    "authorization_code",
		Code:         r.FormValue("code"),
		RedirectURI:  r.FormValue("redirect_uri"),
		ClientID:     clientID,
		ClientSecret: clientSecret,
		CodeVerifier: r.FormValue("code_verifier"),
	}

	resp, err := h.tokenSvc.ExchangeAuthorizationCode(r.Context(), req)
	if err != nil {
		writeError(w, "invalid_grant", err.Error(), http.StatusBadRequest)
		return
	}

	writeJSON(w, http.StatusOK, resp)
}

func (h *OAuth2Handler) handleRefreshTokenGrant(w http.ResponseWriter, r *http.Request) {
	clientID, clientSecret := extractClientCredentials(r)

	req := &token.TokenRequest{
		GrantType:    "refresh_token",
		ClientID:     clientID,
		ClientSecret: clientSecret,
		RefreshToken: r.FormValue("refresh_token"),
		Scope:        r.FormValue("scope"),
	}

	resp, err := h.tokenSvc.RefreshAccessToken(r.Context(), req)
	if err != nil {
		writeError(w, "invalid_grant", err.Error(), http.StatusBadRequest)
		return
	}

	writeJSON(w, http.StatusOK, resp)
}

func (h *OAuth2Handler) handleClientCredentialsGrant(w http.ResponseWriter, r *http.Request) {
	clientID, clientSecret := extractClientCredentials(r)

	req := &grants.ClientCredentialsRequest{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		Scope:        r.FormValue("scope"),
	}

	resp, err := h.clientCredsSvc.IssueClientCredentials(r.Context(), req)
	if err != nil {
		writeError(w, "invalid_client", err.Error(), http.StatusUnauthorized)
		return
	}

	writeJSON(w, http.StatusOK, resp)
}

func (h *OAuth2Handler) handleDeviceCodeGrant(w http.ResponseWriter, r *http.Request) {
	clientID, _ := extractClientCredentials(r)

	req := &device.DeviceTokenRequest{
		GrantType:  "urn:ietf:params:oauth:grant-type:device_code",
		DeviceCode: r.FormValue("device_code"),
		ClientID:   clientID,
	}

	dc, err := h.deviceSvc.PollDeviceToken(r.Context(), req)
	if err != nil {
		switch err {
		case device.ErrAuthorizationPending:
			writeError(w, "authorization_pending", "User has not yet authorized", http.StatusBadRequest)
		case device.ErrSlowDown:
			writeError(w, "slow_down", "Polling too fast", http.StatusBadRequest)
		case device.ErrExpiredToken:
			writeError(w, "expired_token", "Device code expired", http.StatusBadRequest)
		case device.ErrAccessDenied:
			writeError(w, "access_denied", "User denied authorization", http.StatusBadRequest)
		default:
			writeError(w, "invalid_grant", err.Error(), http.StatusBadRequest)
		}
		return
	}

	// Issue tokens (TODO: integrate with token service)
	_ = dc
	writeError(w, "server_error", "Token issuance not yet implemented", http.StatusInternalServerError)
}

func (h *OAuth2Handler) handleJWTBearerGrant(w http.ResponseWriter, r *http.Request) {
	clientID, _ := extractClientCredentials(r)

	req := &grants.JWTBearerRequest{
		GrantType: "urn:ietf:params:oauth:grant-type:jwt-bearer",
		Assertion: r.FormValue("assertion"),
		Scope:     r.FormValue("scope"),
		ClientID:  clientID,
	}

	resp, err := h.jwtBearerSvc.IssueJWTBearer(r.Context(), req)
	if err != nil {
		writeError(w, "invalid_grant", err.Error(), http.StatusBadRequest)
		return
	}

	writeJSON(w, http.StatusOK, resp)
}

// HandleRevoke handles POST /oauth2/revoke
func (h *OAuth2Handler) HandleRevoke(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, "invalid_request", "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if err := r.ParseForm(); err != nil {
		writeError(w, "invalid_request", "Failed to parse form", http.StatusBadRequest)
		return
	}

	clientID, clientSecret := extractClientCredentials(r)

	req := &revocation.RevocationRequest{
		Token:         r.FormValue("token"),
		TokenTypeHint: r.FormValue("token_type_hint"),
		ClientID:      clientID,
		ClientSecret:  clientSecret,
	}

	if err := h.revocationSvc.RevokeToken(r.Context(), req); err != nil {
		writeError(w, "invalid_client", err.Error(), http.StatusUnauthorized)
		return
	}

	w.WriteHeader(http.StatusOK)
}

// HandleDeviceAuthorization handles POST /oauth2/device/authorize
func (h *OAuth2Handler) HandleDeviceAuthorization(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, "invalid_request", "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if err := r.ParseForm(); err != nil {
		writeError(w, "invalid_request", "Failed to parse form", http.StatusBadRequest)
		return
	}

	req := &device.DeviceAuthorizationRequest{
		ClientID: r.FormValue("client_id"),
		Scope:    r.FormValue("scope"),
	}

	resp, err := h.deviceSvc.RequestDeviceAuthorization(r.Context(), req)
	if err != nil {
		writeError(w, "invalid_client", err.Error(), http.StatusBadRequest)
		return
	}

	writeJSON(w, http.StatusOK, resp)
}

// HandleDiscovery handles GET /.well-known/openid-configuration
func (h *OAuth2Handler) HandleDiscovery(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, "invalid_request", "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	data, err := h.discoverySvc.MarshalDiscoveryDocument(r.Context())
	if err != nil {
		writeError(w, "server_error", err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

// HandleJWKS handles GET /.well-known/jwks.json
func (h *OAuth2Handler) HandleJWKS(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, "invalid_request", "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	data, err := h.jwksSvc.MarshalJWKS(r.Context())
	if err != nil {
		writeError(w, "server_error", err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

// HandleUserInfo handles GET/POST /oidc/userinfo
func (h *OAuth2Handler) HandleUserInfo(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		writeError(w, "invalid_request", "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract bearer token
	authHeader := r.Header.Get("Authorization")
	accessToken, err := revocation.ExtractTokenFromHeader(authHeader)
	if err != nil {
		writeError(w, "invalid_token", "Missing or invalid Authorization header", http.StatusUnauthorized)
		return
	}

	claims, err := h.userInfoSvc.GetUserInfo(r.Context(), accessToken)
	if err != nil {
		switch err {
		case oidc.ErrInvalidToken:
			writeError(w, "invalid_token", "Invalid access token", http.StatusUnauthorized)
		case oidc.ErrInsufficientScope:
			writeError(w, "insufficient_scope", "openid scope required", http.StatusForbidden)
		default:
			writeError(w, "server_error", err.Error(), http.StatusInternalServerError)
		}
		return
	}

	writeJSON(w, http.StatusOK, claims)
}

// Helper functions

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, code, description string, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"error":             code,
		"error_description": description,
	})
}

func ptrIfNotEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func extractClientCredentials(r *http.Request) (clientID, clientSecret string) {
	// Try Authorization header first (Basic auth)
	authHeader := r.Header.Get("Authorization")
	if authHeader != "" {
		id, secret, err := revocation.ExtractClientCredentials(authHeader)
		if err == nil {
			return id, secret
		}
	}

	// Fall back to form parameters
	return r.FormValue("client_id"), r.FormValue("client_secret")
}
