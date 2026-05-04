package handler

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	bcrypt "github.com/aegion/aegion/internal/platform/bcryptcompat"
	"github.com/aegion/aegion/modules/oauth2/service/authorization"
	"github.com/aegion/aegion/modules/oauth2/service/device"
	"github.com/aegion/aegion/modules/oauth2/service/grants"
	"github.com/aegion/aegion/modules/oauth2/service/introspection"
	"github.com/aegion/aegion/modules/oauth2/service/oidc"
	"github.com/aegion/aegion/modules/oauth2/service/revocation"
	tokenSvc "github.com/aegion/aegion/modules/oauth2/service/token"
	"github.com/aegion/aegion/modules/oauth2/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOAuth2Handler_MethodGuards(t *testing.T) {
	h := &OAuth2Handler{}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/oauth2/authorize", nil)
	h.HandleAuthorize(rec, req)
	assert.Equal(t, http.StatusMethodNotAllowed, rec.Code)

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/oauth2/token", nil)
	h.HandleToken(rec, req)
	assert.Equal(t, http.StatusMethodNotAllowed, rec.Code)

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/oauth2/revoke", nil)
	h.HandleRevoke(rec, req)
	assert.Equal(t, http.StatusMethodNotAllowed, rec.Code)

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/oauth2/device/authorize", nil)
	h.HandleDeviceAuthorization(rec, req)
	assert.Equal(t, http.StatusMethodNotAllowed, rec.Code)

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/.well-known/openid-configuration", nil)
	h.HandleDiscovery(rec, req)
	assert.Equal(t, http.StatusMethodNotAllowed, rec.Code)

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/.well-known/jwks.json", nil)
	h.HandleJWKS(rec, req)
	assert.Equal(t, http.StatusMethodNotAllowed, rec.Code)

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPut, "/oidc/userinfo", nil)
	h.HandleUserInfo(rec, req)
	assert.Equal(t, http.StatusMethodNotAllowed, rec.Code)

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/oauth2/introspect", nil)
	h.HandleIntrospect(rec, req)
	assert.Equal(t, http.StatusMethodNotAllowed, rec.Code)
}

func TestOAuth2Handler_HandleToken_UnsupportedGrant(t *testing.T) {
	h := &OAuth2Handler{}
	form := url.Values{}
	form.Set("grant_type", "unknown")
	req := httptest.NewRequest(http.MethodPost, "/oauth2/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()

	h.HandleToken(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)

	var body map[string]string
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, "unsupported_grant_type", body["error"])
}

func TestOAuth2Handler_DiscoveryAndJWKS_Success(t *testing.T) {
	h := &OAuth2Handler{
		discoverySvc: oidc.NewDiscoveryService("https://issuer.example.com/", "https://auth.example.com"),
		jwksSvc:      oidc.NewJWKSService(&oidc.MockJWKSProvider{}),
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/.well-known/openid-configuration", nil)
	h.HandleDiscovery(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "\"issuer\":\"https://issuer.example.com\"")

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/.well-known/jwks.json", nil)
	h.HandleJWKS(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "\"keys\"")
}

func TestOAuth2Handler_UserInfo(t *testing.T) {
	h := &OAuth2Handler{
		userInfoSvc: oidc.NewUserInfoService(
			&oidc.MockTokenValidator{},
			&oidc.MockUserInfoProvider{},
		),
	}

	t.Run("missing auth header", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/oidc/userinfo", nil)
		h.HandleUserInfo(rec, req)
		assert.Equal(t, http.StatusUnauthorized, rec.Code)
	})

	t.Run("success", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/oidc/userinfo", nil)
		req.Header.Set("Authorization", "Bearer valid-token")
		h.HandleUserInfo(rec, req)
		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Contains(t, rec.Body.String(), "\"sub\"")
	})
}

func TestOAuth2Handler_HandleIntrospect(t *testing.T) {
	hash, err := bcrypt.GenerateFromPassword([]byte("secret"), bcrypt.DefaultCost)
	require.NoError(t, err)

	storeMock := &handlerTokenStore{
		client: &store.Client{
			ID:                      "client-1",
			TokenEndpointAuthMethod: "client_secret_post",
			SecretHash:              ptrString(string(hash)),
		},
		accessToken: &store.AccessToken{
			JTI:       "at-jti-1",
			ClientID:  "client-1",
			Subject:   "identity-1",
			Scopes:    []string{"openid", "profile"},
			Issuer:    "https://issuer.example.com",
			Audience:  []string{"api"},
			ExpiresAt: time.Now().UTC().Add(time.Minute),
		},
	}

	introspectSvc := introspection.NewService(tokenSvc.NewTokenService(storeMock, &tokenSvc.MockJWTSigner{}, "https://issuer.example.com"))
	h := (&OAuth2Handler{}).WithIntrospectionService(introspectSvc)

	form := url.Values{}
	form.Set("token", "at-jti-1")
	form.Set("client_id", "client-1")
	form.Set("client_secret", "secret")
	req := httptest.NewRequest(http.MethodPost, "/oauth2/introspect", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()

	h.HandleIntrospect(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"active":true`)
	assert.Contains(t, rec.Body.String(), `"client_id":"client-1"`)

	bad := httptest.NewRecorder()
	badReq := httptest.NewRequest(http.MethodPost, "/oauth2/introspect", strings.NewReader(url.Values{
		"token":         []string{"at-jti-1"},
		"client_id":     []string{"client-1"},
		"client_secret": []string{"wrong"},
	}.Encode()))
	badReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	h.HandleIntrospect(bad, badReq)
	assert.Equal(t, http.StatusUnauthorized, bad.Code)
}

func TestHandlerHelpers(t *testing.T) {
	rec := httptest.NewRecorder()
	writeJSON(rec, http.StatusCreated, map[string]string{"ok": "true"})
	assert.Equal(t, http.StatusCreated, rec.Code)
	assert.Contains(t, rec.Header().Get("Content-Type"), "application/json")

	rec = httptest.NewRecorder()
	writeError(rec, "invalid_request", "bad", http.StatusBadRequest)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "\"error\":\"invalid_request\"")

	assert.Nil(t, ptrIfNotEmpty(""))
	v := ptrIfNotEmpty("x")
	require.NotNil(t, v)
	assert.Equal(t, "x", *v)
}

func TestExtractClientCredentials(t *testing.T) {
	encoded := base64.StdEncoding.EncodeToString([]byte("client-id:client-secret"))
	req := httptest.NewRequest(http.MethodPost, "/oauth2/token", nil)
	req.Header.Set("Authorization", "Basic "+encoded)
	clientID, secret := extractClientCredentials(req)
	assert.Equal(t, "client-id", clientID)
	assert.Equal(t, "client-secret", secret)

	form := url.Values{}
	form.Set("client_id", "form-client")
	form.Set("client_secret", "form-secret")
	req = httptest.NewRequest(http.MethodPost, "/oauth2/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	clientID, secret = extractClientCredentials(req)
	assert.Equal(t, "form-client", clientID)
	assert.Equal(t, "form-secret", secret)
}

func TestOAuth2Handler_TokenEndpointSecurityHeaders(t *testing.T) {
	hash, err := bcrypt.GenerateFromPassword([]byte("secret"), bcrypt.DefaultCost)
	require.NoError(t, err)

	client := &store.Client{
		ID:                      "client-1",
		RedirectURIs:            []string{"https://app.example.com/callback"},
		ResponseTypes:           []string{"code"},
		Scopes:                  []string{"openid"},
		GrantTypes:              []string{"authorization_code"},
		TokenEndpointAuthMethod: "client_secret_post",
		SecretHash:              ptrString(string(hash)),
	}
	h := &OAuth2Handler{
		tokenSvc: tokenSvc.NewTokenService(&handlerTokenStore{
			client: client,
			authCode: &store.AuthCode{
				Code:        "ac-1",
				ClientID:    "client-1",
				IdentityID:  "identity-1",
				SessionID:   "session-1",
				RedirectURI: "https://app.example.com/callback",
				Scopes:      []string{"openid"},
				ExpiresAt:   time.Now().UTC().Add(time.Minute),
			},
		}, &tokenSvc.MockJWTSigner{}, "https://issuer"),
	}

	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", "ac-1")
	form.Set("redirect_uri", "https://app.example.com/callback")
	form.Set("client_id", "client-1")
	req := httptest.NewRequest(http.MethodPost, "/oauth2/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	h.HandleToken(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
	assert.Equal(t, "no-cache", rec.Header().Get("Pragma"))
	assert.Contains(t, rec.Header().Get("WWW-Authenticate"), "invalid_client")
	assert.Contains(t, rec.Body.String(), "\"error\":\"invalid_client\"")
	assert.NotContains(t, rec.Body.String(), "crypto/bcrypt")
}

type handlerAuthzStore struct {
	client *store.Client
}

func (s *handlerAuthzStore) GetClient(ctx context.Context, id string) (*store.Client, error) {
	if s.client != nil {
		return s.client, nil
	}
	return nil, store.ErrNotFound
}
func (s *handlerAuthzStore) CreateAuthCode(ctx context.Context, code *store.AuthCode) error {
	return nil
}
func (s *handlerAuthzStore) GetAuthCode(ctx context.Context, code string) (*store.AuthCode, error) {
	return nil, store.ErrNotFound
}
func (s *handlerAuthzStore) MarkAuthCodeUsed(ctx context.Context, code string) error {
	return nil
}
func (s *handlerAuthzStore) GetSessionAuthContext(ctx context.Context, sessionID string) (*store.SessionAuthContext, error) {
	return nil, store.ErrNotFound
}
func (s *handlerAuthzStore) CreateLoginChallenge(ctx context.Context, challenge *store.LoginChallenge) error {
	return nil
}
func (s *handlerAuthzStore) GetLoginChallenge(ctx context.Context, id string) (*store.LoginChallenge, error) {
	return nil, store.ErrNotFound
}
func (s *handlerAuthzStore) AcceptLoginChallenge(ctx context.Context, id, identityID, sessionID string) error {
	return nil
}
func (s *handlerAuthzStore) CreateConsentChallenge(ctx context.Context, challenge *store.ConsentChallenge) error {
	return nil
}
func (s *handlerAuthzStore) GetConsentChallenge(ctx context.Context, id string) (*store.ConsentChallenge, error) {
	return nil, store.ErrNotFound
}
func (s *handlerAuthzStore) AcceptConsentChallenge(ctx context.Context, id string, grantedScopes, grantedAudience []string, remember bool, rememberFor *int) error {
	return nil
}
func (s *handlerAuthzStore) RejectConsentChallenge(ctx context.Context, id, errorCode, errorDesc string) error {
	return nil
}
func (s *handlerAuthzStore) GetConsentSession(ctx context.Context, clientID, identityID string) (*store.ConsentSession, error) {
	return nil, store.ErrNotFound
}
func (s *handlerAuthzStore) CreateConsentSession(ctx context.Context, consent *store.ConsentSession) error {
	return nil
}

type handlerTokenStore struct {
	client       *store.Client
	authCode     *store.AuthCode
	refreshToken *store.RefreshToken
	accessToken  *store.AccessToken
}

func (s *handlerTokenStore) GetClient(ctx context.Context, id string) (*store.Client, error) {
	if s.client != nil {
		return s.client, nil
	}
	return nil, store.ErrNotFound
}
func (s *handlerTokenStore) GetAuthCode(ctx context.Context, code string) (*store.AuthCode, error) {
	if s.authCode != nil && s.authCode.Code == code {
		return s.authCode, nil
	}
	return nil, store.ErrNotFound
}
func (s *handlerTokenStore) MarkAuthCodeUsed(ctx context.Context, code string) error {
	return nil
}
func (s *handlerTokenStore) CreateAccessToken(ctx context.Context, token *store.AccessToken) error {
	return nil
}
func (s *handlerTokenStore) CreateRefreshToken(ctx context.Context, token *store.RefreshToken) error {
	return nil
}
func (s *handlerTokenStore) CreateIDToken(ctx context.Context, token *store.IDToken) error {
	return nil
}
func (s *handlerTokenStore) GetRefreshToken(ctx context.Context, id string) (*store.RefreshToken, error) {
	if s.refreshToken != nil && s.refreshToken.ID == id {
		return s.refreshToken, nil
	}
	return nil, store.ErrNotFound
}
func (s *handlerTokenStore) MarkRefreshTokenUsed(ctx context.Context, id, successorID string, gracePeriod time.Duration) error {
	return nil
}
func (s *handlerTokenStore) InvalidateRefreshTokenFamily(ctx context.Context, familyID string) (int64, error) {
	return 1, nil
}
func (s *handlerTokenStore) RevokeAccessToken(ctx context.Context, jti string) error {
	return nil
}
func (s *handlerTokenStore) RevokeRefreshTokensBySession(ctx context.Context, sessionID string) (int64, error) {
	return 1, nil
}
func (s *handlerTokenStore) RevokeAccessTokensBySession(ctx context.Context, sessionID string) (int64, error) {
	return 1, nil
}
func (s *handlerTokenStore) GetAccessToken(ctx context.Context, jti string) (*store.AccessToken, error) {
	if s.accessToken != nil && s.accessToken.JTI == jti {
		return s.accessToken, nil
	}
	return nil, store.ErrNotFound
}
func (s *handlerTokenStore) GetAccessTokenBySignature(ctx context.Context, signature string) (*store.AccessToken, error) {
	if s.accessToken != nil && s.accessToken.Signature != nil && *s.accessToken.Signature == signature {
		return s.accessToken, nil
	}
	return nil, store.ErrNotFound
}

type handlerGrantStore struct {
	client *store.Client
}

func (s *handlerGrantStore) GetClient(ctx context.Context, id string) (*store.Client, error) {
	if s.client != nil {
		return s.client, nil
	}
	return nil, store.ErrNotFound
}
func (s *handlerGrantStore) CreateAccessToken(ctx context.Context, token *store.AccessToken) error {
	return nil
}

type handlerMultiClientGrantStore struct {
	clients map[string]*store.Client
}

func (s *handlerMultiClientGrantStore) GetClient(ctx context.Context, id string) (*store.Client, error) {
	client, ok := s.clients[id]
	if !ok {
		return nil, store.ErrNotFound
	}
	return client, nil
}

func (s *handlerMultiClientGrantStore) CreateAccessToken(ctx context.Context, token *store.AccessToken) error {
	return nil
}

type handlerRevocationStore struct {
	client       *store.Client
	refreshToken *store.RefreshToken
}

func (s *handlerRevocationStore) GetClient(ctx context.Context, id string) (*store.Client, error) {
	if s.client != nil {
		return s.client, nil
	}
	return nil, store.ErrNotFound
}
func (s *handlerRevocationStore) RevokeAccessToken(ctx context.Context, jti string) error {
	return nil
}
func (s *handlerRevocationStore) GetRefreshToken(ctx context.Context, id string) (*store.RefreshToken, error) {
	if s.refreshToken != nil {
		return s.refreshToken, nil
	}
	return nil, store.ErrNotFound
}
func (s *handlerRevocationStore) InvalidateRefreshTokenFamily(ctx context.Context, familyID string) (int64, error) {
	return 1, nil
}

type handlerDeviceStore struct {
	client       *store.Client
	deviceCode   *store.DeviceCode
	deviceByUser *store.DeviceCode
	lastUsedCode string
	markUsedErr  error
	createErr    error
}

func (s *handlerDeviceStore) GetClient(ctx context.Context, id string) (*store.Client, error) {
	if s.client != nil {
		return s.client, nil
	}
	return nil, store.ErrNotFound
}
func (s *handlerDeviceStore) CreateDeviceCode(ctx context.Context, dc *store.DeviceCode) error {
	s.deviceCode = dc
	return s.createErr
}
func (s *handlerDeviceStore) GetDeviceCode(ctx context.Context, deviceCode string) (*store.DeviceCode, error) {
	if s.deviceCode != nil {
		return s.deviceCode, nil
	}
	return nil, store.ErrNotFound
}
func (s *handlerDeviceStore) GetDeviceCodeByUserCode(ctx context.Context, userCode string) (*store.DeviceCode, error) {
	if s.deviceByUser != nil {
		return s.deviceByUser, nil
	}
	return nil, store.ErrNotFound
}
func (s *handlerDeviceStore) MarkDeviceCodeApproved(ctx context.Context, deviceCode, identityID string, scopes []string) error {
	return nil
}
func (s *handlerDeviceStore) MarkDeviceCodeDenied(ctx context.Context, deviceCode string) error {
	return nil
}
func (s *handlerDeviceStore) MarkDeviceCodeUsed(ctx context.Context, deviceCode string) error {
	s.lastUsedCode = deviceCode
	return s.markUsedErr
}

func TestOAuth2Handler_GrantHandlers(t *testing.T) {
	now := time.Now().UTC()

	// Generate a valid secret hash for the confidential client
	secretHash, err := bcrypt.GenerateFromPassword([]byte("secret"), bcrypt.DefaultCost)
	require.NoError(t, err)
	secretHashStr := string(secretHash)

	authCode := &store.AuthCode{
		Code:        "ac-1",
		ClientID:    "client-1",
		IdentityID:  "identity-1",
		SessionID:   "session-1",
		RedirectURI: "https://app.example.com/callback",
		Scopes:      []string{"openid", "offline_access"},
		ExpiresAt:   now.Add(time.Minute),
	}
	refreshToken := &store.RefreshToken{
		ID:         "rt-1",
		FamilyID:   "family-1",
		ClientID:   "client-1",
		IdentityID: "identity-1",
		SessionID:  "session-1",
		Scopes:     []string{"openid", "offline_access"},
		Active:     true,
		ExpiresAt:  now.Add(time.Hour),
	}
	client := &store.Client{
		ID:                      "client-1",
		RedirectURIs:            []string{"https://app.example.com/callback"},
		ResponseTypes:           []string{"code"},
		Scopes:                  []string{"openid", "offline_access", "read", "write"},
		AccessTokenTTL:          900,
		RefreshTokenTTL:         2592000,
		IDTokenTTL:              3600,
		AllowOfflineAccess:      true,
		TokenEndpointAuthMethod: "none",
		GrantTypes: []string{
			"authorization_code",
			"refresh_token",
			"urn:ietf:params:oauth:grant-type:device_code",
		},
	}

	// Confidential client for client_credentials and jwt-bearer grants
	confClient := &store.Client{
		ID:                      "client-confidential",
		Scopes:                  []string{"read", "write"},
		AccessTokenTTL:          900,
		TokenEndpointAuthMethod: "client_secret_basic",
		SecretHash:              &secretHashStr,
		GrantTypes: []string{
			"client_credentials",
			"urn:ietf:params:oauth:grant-type:jwt-bearer",
		},
	}

	authzSvc := authorization.NewAuthorizationService(&handlerAuthzStore{client: client})
	tokenService := tokenSvc.NewTokenService(&handlerTokenStore{
		client:       client,
		authCode:     authCode,
		refreshToken: refreshToken,
	}, &tokenSvc.MockJWTSigner{}, "https://issuer.example.com")

	// Multi-client grant store for testing both public and confidential clients
	multiClientGrantStore := &handlerMultiClientGrantStore{
		clients: map[string]*store.Client{
			"client-1":            client,
			"client-confidential": confClient,
		},
	}
	clientCredsSvc := grants.NewClientCredentialsService(
		multiClientGrantStore,
		&tokenSvc.MockJWTSigner{},
		"https://issuer.example.com",
	)
	jwtBearerSvc := grants.NewJWTBearerService(
		multiClientGrantStore,
		&tokenSvc.MockJWTSigner{},
		"https://issuer.example.com",
		&grants.MockJWTValidator{},
	)
	revSvc := revocation.NewRevocationService(&handlerRevocationStore{
		client:       client,
		refreshToken: refreshToken,
	})
	deviceStore := &handlerDeviceStore{
		client: client,
		deviceCode: &store.DeviceCode{
			DeviceCode: "dc-1",
			ClientID:   "client-1",
			Status:     "approved",
			IdentityID: ptrString("identity-1"),
			Scopes:     []string{"openid"},
			ExpiresAt:  now.Add(time.Hour),
		},
	}
	deviceSvc := device.NewDeviceService(deviceStore, 10*time.Minute, 5, "https://issuer.example.com/device")

	h := NewOAuth2Handler(
		authzSvc,
		tokenService,
		revSvc,
		deviceSvc,
		clientCredsSvc,
		jwtBearerSvc,
		oidc.NewDiscoveryService("https://issuer.example.com", "https://issuer.example.com"),
		oidc.NewJWKSService(&oidc.MockJWKSProvider{}),
		oidc.NewUserInfoService(&oidc.MockTokenValidator{}, &oidc.MockUserInfoProvider{}),
	)

	t.Run("authorize success", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/oauth2/authorize?response_type=code&client_id=client-1&redirect_uri=https://app.example.com/callback&scope=openid", nil)
		rec := httptest.NewRecorder()
		h.HandleAuthorize(rec, req)
		assert.Equal(t, http.StatusOK, rec.Code)
	})

	t.Run("authorization_code grant", func(t *testing.T) {
		form := url.Values{}
		form.Set("grant_type", "authorization_code")
		form.Set("code", "ac-1")
		form.Set("redirect_uri", "https://app.example.com/callback")
		form.Set("client_id", "client-1")
		req := httptest.NewRequest(http.MethodPost, "/oauth2/token", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rec := httptest.NewRecorder()
		h.HandleToken(rec, req)
		assert.Equal(t, http.StatusOK, rec.Code)
	})

	t.Run("refresh_token grant", func(t *testing.T) {
		form := url.Values{}
		form.Set("grant_type", "refresh_token")
		form.Set("refresh_token", "rt-1")
		form.Set("client_id", "client-1")
		req := httptest.NewRequest(http.MethodPost, "/oauth2/token", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rec := httptest.NewRecorder()
		h.HandleToken(rec, req)
		assert.Equal(t, http.StatusOK, rec.Code)
	})

	t.Run("client_credentials grant", func(t *testing.T) {
		form := url.Values{}
		form.Set("grant_type", "client_credentials")
		req := httptest.NewRequest(http.MethodPost, "/oauth2/token", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		// Use Basic auth header for client authentication
		req.SetBasicAuth("client-confidential", "secret")
		rec := httptest.NewRecorder()
		h.HandleToken(rec, req)
		assert.Equal(t, http.StatusOK, rec.Code)
	})

	t.Run("jwt bearer grant", func(t *testing.T) {
		form := url.Values{}
		form.Set("grant_type", "urn:ietf:params:oauth:grant-type:jwt-bearer")
		form.Set("assertion", "assert")
		req := httptest.NewRequest(http.MethodPost, "/oauth2/token", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		// Use Basic auth header for client authentication
		req.SetBasicAuth("client-confidential", "secret")
		rec := httptest.NewRecorder()
		h.HandleToken(rec, req)
		assert.Equal(t, http.StatusOK, rec.Code)
	})

	t.Run("device code grant", func(t *testing.T) {
		form := url.Values{}
		form.Set("grant_type", "urn:ietf:params:oauth:grant-type:device_code")
		form.Set("device_code", "dc-1")
		form.Set("client_id", "client-1")
		req := httptest.NewRequest(http.MethodPost, "/oauth2/token", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rec := httptest.NewRecorder()
		h.HandleToken(rec, req)
		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Contains(t, rec.Body.String(), "\"access_token\"")
		assert.Equal(t, "dc-1", deviceStore.lastUsedCode)
	})

	t.Run("revoke endpoint success", func(t *testing.T) {
		form := url.Values{}
		form.Set("token", "rt-1")
		form.Set("client_id", "client-1")
		req := httptest.NewRequest(http.MethodPost, "/oauth2/revoke", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rec := httptest.NewRecorder()
		h.HandleRevoke(rec, req)
		assert.Equal(t, http.StatusOK, rec.Code)
	})

	t.Run("device authorization endpoint success", func(t *testing.T) {
		form := url.Values{}
		form.Set("client_id", "client-1")
		form.Set("scope", "openid")
		req := httptest.NewRequest(http.MethodPost, "/oauth2/device/authorize", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rec := httptest.NewRecorder()
		h.HandleDeviceAuthorization(rec, req)
		assert.Equal(t, http.StatusOK, rec.Code)
	})
}

func TestOAuth2Handler_DeviceGrantErrors(t *testing.T) {
	client := &store.Client{ID: "client-1", TokenEndpointAuthMethod: "none"}
	deviceStore := &handlerDeviceStore{
		client: client,
		deviceCode: &store.DeviceCode{
			DeviceCode: "dc-1",
			ClientID:   "client-1",
			Status:     "pending",
			ExpiresAt:  time.Now().UTC().Add(time.Hour),
			LastPollAt: ptrTime(time.Now().UTC()),
		},
	}
	h := &OAuth2Handler{
		deviceSvc: device.NewDeviceService(deviceStore, 10*time.Minute, 5, "https://issuer/device"),
	}

	form := url.Values{}
	form.Set("grant_type", "urn:ietf:params:oauth:grant-type:device_code")
	form.Set("device_code", "dc-1")
	form.Set("client_id", "client-1")
	req := httptest.NewRequest(http.MethodPost, "/oauth2/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	h.HandleToken(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "slow_down")

	deviceStore.deviceCode.LastPollAt = nil
	rec = httptest.NewRecorder()
	h.HandleToken(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "authorization_pending")
}

func TestOAuth2Handler_DeviceGrantTokenIssuanceErrors(t *testing.T) {
	now := time.Now().UTC()
	client := &store.Client{
		ID:                      "client-1",
		TokenEndpointAuthMethod: "none",
		GrantTypes:              []string{"urn:ietf:params:oauth:grant-type:device_code"},
		AccessTokenTTL:          900,
		IDTokenTTL:              3600,
	}
	deviceStore := &handlerDeviceStore{
		client: client,
		deviceCode: &store.DeviceCode{
			DeviceCode: "dc-1",
			ClientID:   "client-1",
			Status:     "approved",
			IdentityID: ptrString("identity-1"),
			ExpiresAt:  now.Add(time.Hour),
		},
	}
	deviceSvc := device.NewDeviceService(deviceStore, 10*time.Minute, 5, "https://issuer/device")

	form := url.Values{}
	form.Set("grant_type", "urn:ietf:params:oauth:grant-type:device_code")
	form.Set("device_code", "dc-1")
	form.Set("client_id", "client-1")
	newReq := func() *http.Request {
		req := httptest.NewRequest(http.MethodPost, "/oauth2/token", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		return req
	}

	t.Run("token service unavailable", func(t *testing.T) {
		h := &OAuth2Handler{
			deviceSvc: deviceSvc,
		}
		rec := httptest.NewRecorder()
		h.HandleToken(rec, newReq())
		assert.Equal(t, http.StatusInternalServerError, rec.Code)
		assert.Contains(t, rec.Body.String(), "Token service unavailable")
	})

	t.Run("consume device code failure", func(t *testing.T) {
		deviceStore.markUsedErr = errors.New("consume failed")
		defer func() {
			deviceStore.markUsedErr = nil
		}()

		h := &OAuth2Handler{
			deviceSvc: deviceSvc,
			tokenSvc: tokenSvc.NewTokenService(&handlerTokenStore{
				client: client,
			}, &tokenSvc.MockJWTSigner{}, "https://issuer"),
		}
		rec := httptest.NewRecorder()
		h.HandleToken(rec, newReq())
		assert.Equal(t, http.StatusInternalServerError, rec.Code)
		assert.Contains(t, rec.Body.String(), "Failed to finalize device code")
	})
}

type handlerErrJWKSProvider struct{}

func (p *handlerErrJWKSProvider) GetPublicKeys(ctx context.Context) ([]oidc.JWK, error) {
	return nil, errors.New("jwks unavailable")
}

func TestOAuth2Handler_AdditionalErrorBranches(t *testing.T) {
	t.Run("grant handlers error responses", func(t *testing.T) {
		client := &store.Client{
			ID:                      "client-1",
			RedirectURIs:            []string{"https://app.example.com/callback"},
			ResponseTypes:           []string{"code"},
			Scopes:                  []string{"openid"},
			AccessTokenTTL:          900,
			RefreshTokenTTL:         2592000,
			IDTokenTTL:              3600,
			TokenEndpointAuthMethod: "none",
			GrantTypes:              []string{"client_credentials"},
		}
		h := &OAuth2Handler{
			tokenSvc: tokenSvc.NewTokenService(&handlerTokenStore{
				client: client,
				authCode: &store.AuthCode{
					Code:        "ac-valid",
					ClientID:    "client-1",
					IdentityID:  "identity-1",
					SessionID:   "session-1",
					RedirectURI: "https://app.example.com/callback",
					Scopes:      []string{"openid"},
					ExpiresAt:   time.Now().UTC().Add(time.Minute),
				},
			}, &tokenSvc.MockJWTSigner{}, "https://issuer"),
			clientCredsSvc: grants.NewClientCredentialsService(
				&handlerGrantStore{client: &store.Client{
					ID:             "client-1",
					GrantTypes:     []string{"authorization_code"},
					Scopes:         []string{"openid"},
					AccessTokenTTL: 900,
				}},
				&tokenSvc.MockJWTSigner{},
				"https://issuer",
			),
			jwtBearerSvc: grants.NewJWTBearerService(
				&handlerGrantStore{client: &store.Client{
					ID:             "client-1",
					GrantTypes:     []string{"urn:ietf:params:oauth:grant-type:jwt-bearer"},
					AccessTokenTTL: 900,
				}},
				&tokenSvc.MockJWTSigner{},
				"https://issuer",
				&grants.MockJWTValidator{Err: errors.New("invalid assertion")},
			),
		}

		form := url.Values{}
		form.Set("grant_type", "authorization_code")
		form.Set("code", "wrong-code")
		form.Set("redirect_uri", "https://app.example.com/callback")
		form.Set("client_id", "client-1")
		req := httptest.NewRequest(http.MethodPost, "/oauth2/token", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rec := httptest.NewRecorder()
		h.HandleToken(rec, req)
		assert.Equal(t, http.StatusBadRequest, rec.Code)

		form = url.Values{}
		form.Set("grant_type", "refresh_token")
		form.Set("refresh_token", "missing")
		form.Set("client_id", "client-1")
		req = httptest.NewRequest(http.MethodPost, "/oauth2/token", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rec = httptest.NewRecorder()
		h.HandleToken(rec, req)
		assert.Equal(t, http.StatusBadRequest, rec.Code)

		form = url.Values{}
		form.Set("grant_type", "client_credentials")
		form.Set("client_id", "client-1")
		req = httptest.NewRequest(http.MethodPost, "/oauth2/token", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rec = httptest.NewRecorder()
		h.HandleToken(rec, req)
		assert.Equal(t, http.StatusUnauthorized, rec.Code)

		form = url.Values{}
		form.Set("grant_type", "urn:ietf:params:oauth:grant-type:jwt-bearer")
		form.Set("assertion", "bad")
		form.Set("client_id", "client-1")
		req = httptest.NewRequest(http.MethodPost, "/oauth2/token", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rec = httptest.NewRecorder()
		h.HandleToken(rec, req)
		assert.Equal(t, http.StatusUnauthorized, rec.Code)
	})

	t.Run("device grant denied and expired mapping", func(t *testing.T) {
		client := &store.Client{ID: "client-1", TokenEndpointAuthMethod: "none"}
		storeDenied := &handlerDeviceStore{
			client: client,
			deviceCode: &store.DeviceCode{
				ClientID:  "client-1",
				Status:    "denied",
				ExpiresAt: time.Now().UTC().Add(time.Hour),
			},
		}
		h := &OAuth2Handler{
			deviceSvc: device.NewDeviceService(storeDenied, 10*time.Minute, 5, "https://issuer/device"),
		}

		form := url.Values{}
		form.Set("grant_type", "urn:ietf:params:oauth:grant-type:device_code")
		form.Set("device_code", "dc")
		form.Set("client_id", "client-1")
		req := httptest.NewRequest(http.MethodPost, "/oauth2/token", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rec := httptest.NewRecorder()
		h.HandleToken(rec, req)
		assert.Equal(t, http.StatusBadRequest, rec.Code)
		assert.Contains(t, rec.Body.String(), "access_denied")

		storeExpired := &handlerDeviceStore{
			client: client,
			deviceCode: &store.DeviceCode{
				ClientID:  "client-1",
				Status:    "pending",
				ExpiresAt: time.Now().UTC().Add(-time.Minute),
			},
		}
		h.deviceSvc = device.NewDeviceService(storeExpired, 10*time.Minute, 5, "https://issuer/device")
		rec = httptest.NewRecorder()
		h.HandleToken(rec, req)
		assert.Equal(t, http.StatusBadRequest, rec.Code)
		assert.Contains(t, rec.Body.String(), "expired_token")
	})

	t.Run("revoke and device auth endpoint errors", func(t *testing.T) {
		revClient := &store.Client{
			ID:                      "client-1",
			TokenEndpointAuthMethod: "client_secret_post",
			SecretHash:              ptrString("expected"),
		}
		h := &OAuth2Handler{
			revocationSvc: revocation.NewRevocationService(&handlerRevocationStore{client: revClient}),
			deviceSvc:     device.NewDeviceService(&handlerDeviceStore{}, 10*time.Minute, 5, "https://issuer/device"),
		}

		form := url.Values{}
		form.Set("token", "abc")
		form.Set("client_id", "client-1")
		req := httptest.NewRequest(http.MethodPost, "/oauth2/revoke", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rec := httptest.NewRecorder()
		h.HandleRevoke(rec, req)
		assert.Equal(t, http.StatusUnauthorized, rec.Code)

		form = url.Values{}
		form.Set("client_id", "missing-client")
		req = httptest.NewRequest(http.MethodPost, "/oauth2/device/authorize", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rec = httptest.NewRecorder()
		h.HandleDeviceAuthorization(rec, req)
		assert.Equal(t, http.StatusUnauthorized, rec.Code)
	})

	t.Run("jwks and userinfo error mapping", func(t *testing.T) {
		h := &OAuth2Handler{
			jwksSvc: oidc.NewJWKSService(&handlerErrJWKSProvider{}),
		}
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/.well-known/jwks.json", nil)
		h.HandleJWKS(rec, req)
		assert.Equal(t, http.StatusInternalServerError, rec.Code)

		h.userInfoSvc = oidc.NewUserInfoService(
			&oidc.MockTokenValidator{Err: errors.New("invalid token")},
			&oidc.MockUserInfoProvider{},
		)
		rec = httptest.NewRecorder()
		req = httptest.NewRequest(http.MethodGet, "/oidc/userinfo", nil)
		req.Header.Set("Authorization", "Bearer x")
		h.HandleUserInfo(rec, req)
		assert.Equal(t, http.StatusUnauthorized, rec.Code)

		h.userInfoSvc = oidc.NewUserInfoService(
			&oidc.MockTokenValidator{
				Token: &oidc.AccessToken{
					IdentityID: "identity-1",
					Scopes:     []string{"profile"},
					ExpiresAt:  time.Now().Add(time.Minute),
				},
			},
			&oidc.MockUserInfoProvider{},
		)
		rec = httptest.NewRecorder()
		req = httptest.NewRequest(http.MethodGet, "/oidc/userinfo", nil)
		req.Header.Set("Authorization", "Bearer x")
		h.HandleUserInfo(rec, req)
		assert.Equal(t, http.StatusForbidden, rec.Code)

		h.userInfoSvc = oidc.NewUserInfoService(
			&oidc.MockTokenValidator{
				Token: &oidc.AccessToken{
					IdentityID: "identity-1",
					Scopes:     []string{"openid"},
					ExpiresAt:  time.Now().Add(time.Minute),
				},
			},
			&oidc.MockUserInfoProvider{Err: errors.New("provider down")},
		)
		rec = httptest.NewRecorder()
		req = httptest.NewRequest(http.MethodGet, "/oidc/userinfo", nil)
		req.Header.Set("Authorization", "Bearer x")
		h.HandleUserInfo(rec, req)
		assert.Equal(t, http.StatusInternalServerError, rec.Code)
	})
}

func TestOAuth2Handler_TargetedUncoveredBranches(t *testing.T) {
	t.Run("authorize service error path", func(t *testing.T) {
		h := &OAuth2Handler{
			authzSvc: authorization.NewAuthorizationService(&handlerAuthzStore{}),
		}
		req := httptest.NewRequest(http.MethodGet, "/oauth2/authorize?response_type=code&client_id=missing&redirect_uri=https://app.example.com/callback", nil)
		rec := httptest.NewRecorder()
		h.HandleAuthorize(rec, req)
		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})

	t.Run("token parse form failure", func(t *testing.T) {
		h := &OAuth2Handler{}
		req := httptest.NewRequest(http.MethodPost, "/oauth2/token", strings.NewReader("%"))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rec := httptest.NewRecorder()
		h.HandleToken(rec, req)
		assert.Equal(t, http.StatusBadRequest, rec.Code)
		assert.Contains(t, rec.Body.String(), "Failed to parse form")
	})

	t.Run("device grant default error mapping", func(t *testing.T) {
		deviceStore := &handlerDeviceStore{
			client: &store.Client{ID: "client-1", TokenEndpointAuthMethod: "none"},
			deviceCode: &store.DeviceCode{
				DeviceCode: "dc-default",
				ClientID:   "other-client",
				Status:     "approved",
				IdentityID: ptrString("identity-1"),
				ExpiresAt:  time.Now().UTC().Add(time.Hour),
			},
		}
		h := &OAuth2Handler{
			deviceSvc: device.NewDeviceService(deviceStore, 10*time.Minute, 5, "https://issuer/device"),
		}

		form := url.Values{}
		form.Set("grant_type", "urn:ietf:params:oauth:grant-type:device_code")
		form.Set("device_code", "dc-default")
		form.Set("client_id", "client-1")
		req := httptest.NewRequest(http.MethodPost, "/oauth2/token", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rec := httptest.NewRecorder()
		h.HandleToken(rec, req)
		assert.Equal(t, http.StatusBadRequest, rec.Code)
		assert.Contains(t, rec.Body.String(), "\"error\":\"invalid_grant\"")
	})

	t.Run("device grant session id branch and token issuance error", func(t *testing.T) {
		deviceStore := &handlerDeviceStore{
			client: &store.Client{ID: "client-1", TokenEndpointAuthMethod: "none"},
			deviceCode: &store.DeviceCode{
				DeviceCode: "dc-issue",
				ClientID:   "client-1",
				Status:     "approved",
				IdentityID: ptrString("identity-1"),
				SessionID:  ptrString("session-1"),
				ExpiresAt:  time.Now().UTC().Add(time.Hour),
			},
		}
		tokenClient := &store.Client{
			ID:                      "client-1",
			TokenEndpointAuthMethod: "none",
			GrantTypes:              []string{"authorization_code"},
		}
		h := &OAuth2Handler{
			deviceSvc: device.NewDeviceService(deviceStore, 10*time.Minute, 5, "https://issuer/device"),
			tokenSvc:  tokenSvc.NewTokenService(&handlerTokenStore{client: tokenClient}, &tokenSvc.MockJWTSigner{}, "https://issuer"),
		}

		form := url.Values{}
		form.Set("grant_type", "urn:ietf:params:oauth:grant-type:device_code")
		form.Set("device_code", "dc-issue")
		form.Set("client_id", "client-1")
		req := httptest.NewRequest(http.MethodPost, "/oauth2/token", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rec := httptest.NewRecorder()
		h.HandleToken(rec, req)
		assert.Equal(t, http.StatusBadRequest, rec.Code)
		assert.Equal(t, "dc-issue", deviceStore.lastUsedCode)
		assert.Contains(t, rec.Body.String(), "\"error\":\"unauthorized_client\"")
	})

	t.Run("revoke parse form and missing token", func(t *testing.T) {
		h := &OAuth2Handler{}

		req := httptest.NewRequest(http.MethodPost, "/oauth2/revoke", strings.NewReader("%"))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rec := httptest.NewRecorder()
		h.HandleRevoke(rec, req)
		assert.Equal(t, http.StatusBadRequest, rec.Code)
		assert.Contains(t, rec.Body.String(), "Failed to parse form")

		form := url.Values{}
		form.Set("client_id", "client-1")
		req = httptest.NewRequest(http.MethodPost, "/oauth2/revoke", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rec = httptest.NewRecorder()
		h.HandleRevoke(rec, req)
		assert.Equal(t, http.StatusBadRequest, rec.Code)
		assert.Contains(t, rec.Body.String(), "token is required")
	})

	t.Run("device authorization parse form and generic error", func(t *testing.T) {
		h := &OAuth2Handler{}
		req := httptest.NewRequest(http.MethodPost, "/oauth2/device/authorize", strings.NewReader("%"))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rec := httptest.NewRecorder()
		h.HandleDeviceAuthorization(rec, req)
		assert.Equal(t, http.StatusBadRequest, rec.Code)
		assert.Contains(t, rec.Body.String(), "Failed to parse form")

		deviceStore := &handlerDeviceStore{
			client:    &store.Client{ID: "client-1", TokenEndpointAuthMethod: "none"},
			createErr: errors.New("device create failed"),
		}
		h.deviceSvc = device.NewDeviceService(deviceStore, 10*time.Minute, 5, "https://issuer/device")

		form := url.Values{}
		form.Set("client_id", "client-1")
		req = httptest.NewRequest(http.MethodPost, "/oauth2/device/authorize", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rec = httptest.NewRecorder()
		h.HandleDeviceAuthorization(rec, req)
		assert.Equal(t, http.StatusBadRequest, rec.Code)
		assert.Contains(t, rec.Body.String(), "Invalid device authorization request")

		deviceStore = &handlerDeviceStore{
			client: &store.Client{
				ID:                      "client-1",
				TokenEndpointAuthMethod: "none",
				Scopes:                  []string{"openid"},
			},
		}
		h.deviceSvc = device.NewDeviceService(deviceStore, 10*time.Minute, 5, "https://issuer/device")

		form = url.Values{}
		form.Set("client_id", "client-1")
		form.Set("scope", "openid admin")
		req = httptest.NewRequest(http.MethodPost, "/oauth2/device/authorize", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rec = httptest.NewRecorder()
		h.HandleDeviceAuthorization(rec, req)
		assert.Equal(t, http.StatusBadRequest, rec.Code)
		assert.Contains(t, rec.Body.String(), "\"error\":\"invalid_scope\"")
	})
}

func TestOAuth2Handler_ErrorMappers(t *testing.T) {
	t.Run("authorization mapper", func(t *testing.T) {
		cases := []struct {
			name   string
			err    error
			code   string
			status int
		}{
			{name: "unauthorized client", err: authorization.ErrUnauthorizedClient, code: "unauthorized_client", status: http.StatusBadRequest},
			{name: "unsupported response type", err: authorization.ErrUnsupportedResponseType, code: "unsupported_response_type", status: http.StatusBadRequest},
			{name: "invalid scope", err: authorization.ErrInvalidScope, code: "invalid_scope", status: http.StatusBadRequest},
			{name: "invalid request", err: authorization.ErrInvalidRequest, code: "invalid_request", status: http.StatusBadRequest},
			{name: "invalid pkce", err: authorization.ErrInvalidPKCE, code: "invalid_request", status: http.StatusBadRequest},
			{name: "pkce required", err: authorization.ErrPKCERequired, code: "invalid_request", status: http.StatusBadRequest},
			{name: "default", err: errors.New("boom"), code: "server_error", status: http.StatusInternalServerError},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				code, desc, status := mapAuthorizationError(tc.err)
				assert.Equal(t, tc.code, code)
				assert.Equal(t, tc.status, status)
				assert.NotEmpty(t, desc)
			})
		}
	})

	t.Run("token mapper", func(t *testing.T) {
		cases := []struct {
			name   string
			err    error
			code   string
			status int
		}{
			{name: "invalid client", err: tokenSvc.ErrInvalidClient, code: "invalid_client", status: http.StatusUnauthorized},
			{name: "unauthorized client", err: tokenSvc.ErrUnauthorizedClient, code: "unauthorized_client", status: http.StatusBadRequest},
			{name: "invalid scope", err: tokenSvc.ErrInvalidScope, code: "invalid_scope", status: http.StatusBadRequest},
			{name: "invalid request", err: tokenSvc.ErrInvalidRequest, code: "invalid_request", status: http.StatusBadRequest},
			{name: "unsupported grant", err: tokenSvc.ErrUnsupportedGrantType, code: "unsupported_grant_type", status: http.StatusBadRequest},
			{name: "invalid grant", err: tokenSvc.ErrInvalidGrant, code: "invalid_grant", status: http.StatusBadRequest},
			{name: "pkce required", err: store.ErrPKCERequired, code: "invalid_grant", status: http.StatusBadRequest},
			{name: "pkce mismatch", err: store.ErrPKCEMismatch, code: "invalid_grant", status: http.StatusBadRequest},
			{name: "family invalidated", err: store.ErrFamilyInvalidated, code: "invalid_grant", status: http.StatusBadRequest},
			{name: "default", err: errors.New("other"), code: "server_error", status: http.StatusInternalServerError},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				code, desc, status := mapTokenGrantError(tc.err)
				assert.Equal(t, tc.code, code)
				assert.Equal(t, tc.status, status)
				assert.NotEmpty(t, desc)
			})
		}
	})

	t.Run("client credentials mapper", func(t *testing.T) {
		cases := []struct {
			name   string
			err    error
			code   string
			status int
		}{
			{name: "invalid client", err: grants.ErrInvalidClient, code: "invalid_client", status: http.StatusUnauthorized},
			{name: "unauthorized client", err: grants.ErrUnauthorizedClient, code: "unauthorized_client", status: http.StatusBadRequest},
			{name: "invalid scope", err: grants.ErrInvalidScope, code: "invalid_scope", status: http.StatusBadRequest},
			{name: "default", err: errors.New("other"), code: "server_error", status: http.StatusInternalServerError},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				code, desc, status := mapClientCredentialsError(tc.err)
				assert.Equal(t, tc.code, code)
				assert.Equal(t, tc.status, status)
				assert.NotEmpty(t, desc)
			})
		}
	})

	t.Run("jwt bearer mapper", func(t *testing.T) {
		cases := []struct {
			name   string
			err    error
			code   string
			status int
		}{
			{name: "invalid client", err: grants.ErrInvalidClient, code: "invalid_client", status: http.StatusUnauthorized},
			{name: "unauthorized client", err: grants.ErrUnauthorizedClient, code: "unauthorized_client", status: http.StatusBadRequest},
			{name: "invalid scope", err: grants.ErrInvalidScope, code: "invalid_scope", status: http.StatusBadRequest},
			{name: "default", err: errors.New("other"), code: "invalid_grant", status: http.StatusBadRequest},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				code, desc, status := mapJWTBearerError(tc.err)
				assert.Equal(t, tc.code, code)
				assert.Equal(t, tc.status, status)
				assert.NotEmpty(t, desc)
			})
		}
	})
}

func TestExtractClientCredentials_InvalidHeaderFallsBackToForm(t *testing.T) {
	form := url.Values{}
	form.Set("client_id", "fallback-client")
	form.Set("client_secret", "fallback-secret")
	req := httptest.NewRequest(http.MethodPost, "/oauth2/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Authorization", "Basic !!!not-base64!!!")

	clientID, secret := extractClientCredentials(req)
	assert.Equal(t, "fallback-client", clientID)
	assert.Equal(t, "fallback-secret", secret)
}

func TestOAuth2Handler_Construction(t *testing.T) {
	h := NewOAuth2Handler(nil, nil, nil, nil, nil, nil, nil, nil, nil)
	require.NotNil(t, h)
}

func TestPtrTimeValue(t *testing.T) {
	// Test nil pointer returns zero time
	var nilPtr *time.Time
	zeroTime := ptrTimeValue(nilPtr)
	if !zeroTime.IsZero() {
		t.Errorf("expected zero time for nil pointer, got %v", zeroTime)
	}

	// Test non-nil pointer returns dereferenced value
	now := time.Now().UTC()
	result := ptrTimeValue(&now)
	if result != now {
		t.Errorf("expected %v, got %v", now, result)
	}
}

func ptrTime(v time.Time) *time.Time {
	return &v
}

func ptrString(v string) *string {
	return &v
}
