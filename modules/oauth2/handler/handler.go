// Package handler provides HTTP handlers for OAuth2 endpoints.
package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/aegion/aegion/modules/oauth2/service/authorization"
	"github.com/aegion/aegion/modules/oauth2/service/device"
	"github.com/aegion/aegion/modules/oauth2/service/grants"
	"github.com/aegion/aegion/modules/oauth2/service/oidc"
	"github.com/aegion/aegion/modules/oauth2/service/revocation"
	"github.com/aegion/aegion/modules/oauth2/service/token"
	"github.com/aegion/aegion/modules/oauth2/store"
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
		code, description, status := mapAuthorizationError(err)
		writeError(w, code, description, status)
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
	setNoStoreHeaders(w)

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
		code, description, status := mapTokenGrantError(err)
		writeTokenError(w, code, description, status)
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
		code, description, status := mapTokenGrantError(err)
		writeTokenError(w, code, description, status)
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
		code, description, status := mapClientCredentialsError(err)
		writeTokenError(w, code, description, status)
		return
	}

	writeJSON(w, http.StatusOK, resp)
}

func (h *OAuth2Handler) handleDeviceCodeGrant(w http.ResponseWriter, r *http.Request) {
	clientID, clientSecret := extractClientCredentials(r)

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
	if h.tokenSvc == nil {
		writeError(w, "server_error", "Token service unavailable", http.StatusInternalServerError)
		return
	}

	if err := h.deviceSvc.ConsumeDeviceCode(r.Context(), dc.DeviceCode); err != nil {
		writeError(w, "server_error", "Failed to finalize device code", http.StatusInternalServerError)
		return
	}

	issueReq := &token.DeviceCodeTokenRequest{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		IdentityID:   *dc.IdentityID,
		Scopes:       dc.Scopes,
		Audience:     dc.Audience,
		AuthTime:     ptrTimeValue(dc.ApprovedAt),
	}
	if dc.SessionID != nil {
		issueReq.SessionID = *dc.SessionID
	}

	resp, err := h.tokenSvc.ExchangeDeviceCode(r.Context(), issueReq)
	if err != nil {
		code, description, status := mapTokenGrantError(err)
		writeTokenError(w, code, description, status)
		return
	}

	writeJSON(w, http.StatusOK, resp)
}

func (h *OAuth2Handler) handleJWTBearerGrant(w http.ResponseWriter, r *http.Request) {
	clientID, clientSecret := extractClientCredentials(r)

	req := &grants.JWTBearerRequest{
		GrantType:    "urn:ietf:params:oauth:grant-type:jwt-bearer",
		Assertion:    r.FormValue("assertion"),
		Scope:        r.FormValue("scope"),
		ClientID:     clientID,
		ClientSecret: clientSecret,
	}

	resp, err := h.jwtBearerSvc.IssueJWTBearer(r.Context(), req)
	if err != nil {
		code, description, status := mapJWTBearerError(err)
		writeTokenError(w, code, description, status)
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
	setNoStoreHeaders(w)

	if r.FormValue("token") == "" {
		writeError(w, "invalid_request", "token is required", http.StatusBadRequest)
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
		if errors.Is(err, revocation.ErrInvalidClient) {
			w.Header().Set("WWW-Authenticate", `Basic realm="oauth2", error="invalid_client"`)
			writeError(w, "invalid_client", "Client authentication failed", http.StatusUnauthorized)
			return
		}
		writeError(w, "server_error", "Internal server error", http.StatusInternalServerError)
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
		if errors.Is(err, device.ErrInvalidClient) {
			writeError(w, "invalid_client", "Client authentication failed", http.StatusUnauthorized)
			return
		}
		writeError(w, "invalid_request", "Invalid device authorization request", http.StatusBadRequest)
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
		writeError(w, "server_error", "Internal server error", http.StatusInternalServerError)
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
		writeError(w, "server_error", "Internal server error", http.StatusInternalServerError)
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
			writeError(w, "server_error", "Internal server error", http.StatusInternalServerError)
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

func writeTokenError(w http.ResponseWriter, code, description string, status int) {
	setNoStoreHeaders(w)
	if code == "invalid_client" {
		w.Header().Set("WWW-Authenticate", `Basic realm="oauth2", error="invalid_client"`)
	}
	writeError(w, code, description, status)
}

func setNoStoreHeaders(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
}

func ptrIfNotEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func ptrTimeValue(v *time.Time) time.Time {
	if v == nil {
		return time.Time{}
	}
	return *v
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

func mapAuthorizationError(err error) (code, description string, status int) {
	switch {
	case errors.Is(err, authorization.ErrUnauthorizedClient):
		return "unauthorized_client", "Client is not authorized for this flow", http.StatusBadRequest
	case errors.Is(err, authorization.ErrUnsupportedResponseType):
		return "unsupported_response_type", "Response type is not supported", http.StatusBadRequest
	case errors.Is(err, authorization.ErrInvalidScope):
		return "invalid_scope", "Requested scope is invalid", http.StatusBadRequest
	case errors.Is(err, authorization.ErrInvalidRequest), errors.Is(err, authorization.ErrInvalidPKCE), errors.Is(err, authorization.ErrPKCERequired):
		return "invalid_request", "Invalid authorization request", http.StatusBadRequest
	default:
		return "server_error", "Internal server error", http.StatusInternalServerError
	}
}

func mapTokenGrantError(err error) (code, description string, status int) {
	switch {
	case errors.Is(err, token.ErrInvalidClient):
		return "invalid_client", "Client authentication failed", http.StatusUnauthorized
	case errors.Is(err, token.ErrUnauthorizedClient):
		return "unauthorized_client", "Client is not authorized for this grant type", http.StatusBadRequest
	case errors.Is(err, token.ErrInvalidScope):
		return "invalid_scope", "Requested scope is invalid", http.StatusBadRequest
	case errors.Is(err, token.ErrInvalidRequest):
		return "invalid_request", "Invalid token request", http.StatusBadRequest
	case errors.Is(err, token.ErrUnsupportedGrantType):
		return "unsupported_grant_type", "Grant type not supported", http.StatusBadRequest
	case errors.Is(err, token.ErrInvalidGrant), errors.Is(err, store.ErrPKCERequired), errors.Is(err, store.ErrPKCEMismatch), errors.Is(err, store.ErrFamilyInvalidated):
		return "invalid_grant", "Invalid grant", http.StatusBadRequest
	default:
		return "server_error", "Internal server error", http.StatusInternalServerError
	}
}

func mapClientCredentialsError(err error) (code, description string, status int) {
	switch {
	case errors.Is(err, grants.ErrInvalidClient):
		return "invalid_client", "Client authentication failed", http.StatusUnauthorized
	case errors.Is(err, grants.ErrUnauthorizedClient):
		return "unauthorized_client", "Client is not authorized for this grant type", http.StatusBadRequest
	case errors.Is(err, grants.ErrInvalidScope):
		return "invalid_scope", "Requested scope is invalid", http.StatusBadRequest
	default:
		return "server_error", "Internal server error", http.StatusInternalServerError
	}
}

func mapJWTBearerError(err error) (code, description string, status int) {
	switch {
	case errors.Is(err, grants.ErrInvalidClient):
		return "invalid_client", "Client authentication failed", http.StatusUnauthorized
	case errors.Is(err, grants.ErrUnauthorizedClient):
		return "unauthorized_client", "Client is not authorized for this grant type", http.StatusBadRequest
	case errors.Is(err, grants.ErrInvalidScope):
		return "invalid_scope", "Requested scope is invalid", http.StatusBadRequest
	default:
		return "invalid_grant", "Invalid JWT bearer assertion", http.StatusBadRequest
	}
}
