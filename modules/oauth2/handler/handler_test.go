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

	"github.com/aegion/aegion/modules/oauth2/service/authorization"
	"github.com/aegion/aegion/modules/oauth2/service/device"
	"github.com/aegion/aegion/modules/oauth2/service/grants"
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
}

func (s *handlerDeviceStore) GetClient(ctx context.Context, id string) (*store.Client, error) {
	if s.client != nil {
		return s.client, nil
	}
	return nil, store.ErrNotFound
}
func (s *handlerDeviceStore) CreateDeviceCode(ctx context.Context, dc *store.DeviceCode) error {
	s.deviceCode = dc
	return nil
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
	return nil
}

func TestOAuth2Handler_GrantHandlers(t *testing.T) {
	now := time.Now().UTC()
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
		Scopes:                  []string{"openid", "offline_access"},
		AccessTokenTTL:          900,
		RefreshTokenTTL:         2592000,
		IDTokenTTL:              3600,
		AllowOfflineAccess:      true,
		TokenEndpointAuthMethod: "none",
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
	clientCredsSvc := grants.NewClientCredentialsService(
		&handlerGrantStore{client: client},
		&tokenSvc.MockJWTSigner{},
		"https://issuer.example.com",
	)
	jwtBearerSvc := grants.NewJWTBearerService(
		&handlerGrantStore{client: client},
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
		form.Set("client_id", "client-1")
		req := httptest.NewRequest(http.MethodPost, "/oauth2/token", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rec := httptest.NewRecorder()
		h.HandleToken(rec, req)
		assert.Equal(t, http.StatusOK, rec.Code)
	})

	t.Run("jwt bearer grant", func(t *testing.T) {
		form := url.Values{}
		form.Set("grant_type", "urn:ietf:params:oauth:grant-type:jwt-bearer")
		form.Set("assertion", "assert")
		form.Set("client_id", "client-1")
		req := httptest.NewRequest(http.MethodPost, "/oauth2/token", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rec := httptest.NewRecorder()
		h.HandleToken(rec, req)
		assert.Equal(t, http.StatusOK, rec.Code)
	})

	t.Run("device code grant currently not implemented", func(t *testing.T) {
		form := url.Values{}
		form.Set("grant_type", "urn:ietf:params:oauth:grant-type:device_code")
		form.Set("device_code", "dc-1")
		form.Set("client_id", "client-1")
		req := httptest.NewRequest(http.MethodPost, "/oauth2/token", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rec := httptest.NewRecorder()
		h.HandleToken(rec, req)
		assert.Equal(t, http.StatusInternalServerError, rec.Code)
		assert.Contains(t, rec.Body.String(), "Token issuance not yet implemented")
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
		assert.Equal(t, http.StatusBadRequest, rec.Code)
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
		assert.Equal(t, http.StatusBadRequest, rec.Code)
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

func TestOAuth2Handler_Construction(t *testing.T) {
	h := NewOAuth2Handler(nil, nil, nil, nil, nil, nil, nil, nil, nil)
	require.NotNil(t, h)
}

func ptrTime(v time.Time) *time.Time {
	return &v
}

func ptrString(v string) *string {
	return &v
}
