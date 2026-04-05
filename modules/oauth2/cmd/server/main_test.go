package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/aegion/aegion/modules/oauth2/handler"
	"github.com/aegion/aegion/modules/oauth2/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestApplyDefaults(t *testing.T) {
	cfg := &Config{}
	applyDefaults(cfg)

	assert.Equal(t, "0.0.0.0", cfg.Server.Address)
	assert.Equal(t, 8083, cfg.Server.Port)
	assert.Equal(t, int32(20), cfg.Database.MaxConns)
	assert.Equal(t, int32(2), cfg.Database.MinConns)
	assert.Equal(t, "http://localhost:8083", cfg.OAuth2.Issuer)
	assert.Equal(t, "http://localhost:8083", cfg.OAuth2.BaseURL)
	assert.Equal(t, 10*time.Minute, cfg.OAuth2.DeviceCodeTTL)
	assert.Equal(t, 5, cfg.OAuth2.DevicePollInterval)
	assert.Equal(t, "http://localhost:8083/oauth2/device/verify", cfg.OAuth2.DeviceVerificationURI)
}

func TestApplyDefaults_TrimBaseURL(t *testing.T) {
	cfg := &Config{}
	cfg.OAuth2.Issuer = "https://issuer.example.com"
	cfg.OAuth2.BaseURL = "https://issuer.example.com/"
	applyDefaults(cfg)
	assert.Equal(t, "https://issuer.example.com", cfg.OAuth2.BaseURL)
}

type mockLookupStore struct {
	token *store.AccessToken
	err   error
}

func (m *mockLookupStore) GetAccessToken(ctx context.Context, jti string) (*store.AccessToken, error) {
	if m.err != nil {
		return nil, m.err
	}
	if m.token != nil {
		return m.token, nil
	}
	return nil, store.ErrNotFound
}

func TestAccessTokenValidator(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		v := &accessTokenValidator{
			store: &mockLookupStore{
				token: &store.AccessToken{
					JTI:        "jti-1",
					ClientID:   "client-1",
					IdentityID: "identity-1",
					Scopes:     []string{"openid"},
					ExpiresAt:  time.Now().UTC().Add(time.Minute),
				},
			},
		}
		got, err := v.ValidateAccessToken(context.Background(), "jti-1")
		require.NoError(t, err)
		assert.Equal(t, "identity-1", got.IdentityID)
	})

	t.Run("store and inactive errors", func(t *testing.T) {
		v := &accessTokenValidator{store: &mockLookupStore{err: errors.New("db down")}}
		_, err := v.ValidateAccessToken(context.Background(), "jti-1")
		assert.ErrorContains(t, err, "db down")

		v = &accessTokenValidator{
			store: &mockLookupStore{
				token: &store.AccessToken{
					JTI:       "jti-1",
					Revoked:   true,
					ExpiresAt: time.Now().UTC().Add(time.Minute),
				},
			},
		}
		_, err = v.ValidateAccessToken(context.Background(), "jti-1")
		assert.ErrorContains(t, err, "inactive")

		v = &accessTokenValidator{
			store: &mockLookupStore{
				token: &store.AccessToken{
					JTI:       "jti-1",
					ExpiresAt: time.Now().UTC().Add(-time.Second),
				},
			},
		}
		_, err = v.ValidateAccessToken(context.Background(), "jti-1")
		assert.ErrorContains(t, err, "inactive")
	})
}

func TestUserInfoProviderAndHelpers(t *testing.T) {
	p := &userInfoProvider{}
	info, err := p.GetUserInfo(context.Background(), "identity-1", []string{"openid", "profile", "email"})
	require.NoError(t, err)
	assert.Equal(t, "identity-1", info.Sub)
	require.NotNil(t, info.Name)
	require.NotNil(t, info.Email)

	assert.True(t, containsScope([]string{"a", "b"}, "b"))
	assert.False(t, containsScope([]string{"a", "b"}, "c"))
}

func TestRegisterRoutesAndServer(t *testing.T) {
	cfg := &Config{}
	applyDefaults(cfg)
	h := &handler.OAuth2Handler{}

	srv := newHTTPServer(cfg, h)
	require.NotNil(t, srv)
	assert.Contains(t, srv.Addr, ":8083")

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	srv.Handler.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "\"status\":\"ok\"")
}

func TestGetEnv(t *testing.T) {
	const key = "AEGION_OAUTH2_TEST_ENV"
	_ = os.Unsetenv(key)
	assert.Equal(t, "fallback", getEnv(key, "fallback"))

	require.NoError(t, os.Setenv(key, "value"))
	defer os.Unsetenv(key)
	assert.Equal(t, "value", getEnv(key, "fallback"))
}
