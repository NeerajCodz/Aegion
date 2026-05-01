// Package token tests
package token

import (
	"context"
	"errors"
	"testing"
	"time"

	bcrypt "github.com/aegion/aegion/internal/platform/bcryptcompat"
	"github.com/aegion/aegion/modules/oauth2/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Mock store for testing
type mockTokenStore struct {
	client                     *store.Client
	authCode                   *store.AuthCode
	refreshToken               *store.RefreshToken
	refreshTokenByID           map[string]*store.RefreshToken
	accessTokens               []*store.AccessToken
	accessTokenByJTI           map[string]*store.AccessToken
	accessTokenBySignature     map[string]*store.AccessToken
	refreshTokens              []*store.RefreshToken
	idTokens                   []*store.IDToken
	getClientErr               error
	getAuthCodeErr             error
	markAuthCodeUsedErr        error
	createAccessErr            error
	createRefreshErr           error
	createIDErr                error
	getRefreshErr              error
	markRefreshUsedErr         error
	markRefreshUsedGracePeriod time.Duration
	invalidateRefreshFamilyErr error
	revokeAccessErr            error
	revokeRefreshBySessionErr  error
	revokeAccessBySessionErr   error
	invalidatedFamilyID        string
}

type failingJWTSigner struct {
	accessErr error
	idErr     error
}

func (s *failingJWTSigner) SignAccessToken(claims map[string]interface{}) (string, error) {
	if s.accessErr != nil {
		return "", s.accessErr
	}
	return (&MockJWTSigner{}).SignAccessToken(claims)
}

func (s *failingJWTSigner) SignIDToken(claims map[string]interface{}) (string, error) {
	if s.idErr != nil {
		return "", s.idErr
	}
	return (&MockJWTSigner{}).SignIDToken(claims)
}

func (m *mockTokenStore) GetClient(ctx context.Context, id string) (*store.Client, error) {
	if m.getClientErr != nil {
		return nil, m.getClientErr
	}
	if m.client != nil && m.client.ID == id {
		return m.client, nil
	}
	return nil, store.ErrNotFound
}

func (m *mockTokenStore) GetAuthCode(ctx context.Context, code string) (*store.AuthCode, error) {
	if m.getAuthCodeErr != nil {
		return nil, m.getAuthCodeErr
	}
	if m.authCode != nil && m.authCode.Code == code {
		return m.authCode, nil
	}
	return nil, store.ErrNotFound
}

func (m *mockTokenStore) MarkAuthCodeUsed(ctx context.Context, code string) error {
	if m.markAuthCodeUsedErr != nil {
		return m.markAuthCodeUsedErr
	}
	if m.authCode != nil && m.authCode.Code == code {
		m.authCode.Used = true
		return nil
	}
	return store.ErrNotFound
}

func (m *mockTokenStore) CreateAccessToken(ctx context.Context, token *store.AccessToken) error {
	m.accessTokens = append(m.accessTokens, token)
	return m.createAccessErr
}

func (m *mockTokenStore) CreateRefreshToken(ctx context.Context, token *store.RefreshToken) error {
	m.refreshTokens = append(m.refreshTokens, token)
	return m.createRefreshErr
}

func (m *mockTokenStore) CreateIDToken(ctx context.Context, token *store.IDToken) error {
	m.idTokens = append(m.idTokens, token)
	return m.createIDErr
}

func (m *mockTokenStore) GetRefreshToken(ctx context.Context, id string) (*store.RefreshToken, error) {
	if m.getRefreshErr != nil {
		return nil, m.getRefreshErr
	}
	if m.refreshTokenByID != nil {
		if rt, ok := m.refreshTokenByID[id]; ok {
			return rt, nil
		}
	}
	if m.refreshToken != nil && m.refreshToken.ID == id {
		return m.refreshToken, nil
	}
	return nil, store.ErrNotFound
}

func (m *mockTokenStore) MarkRefreshTokenUsed(ctx context.Context, id, successorID string, gracePeriod time.Duration) error {
	if m.markRefreshUsedErr != nil {
		return m.markRefreshUsedErr
	}
	m.markRefreshUsedGracePeriod = gracePeriod
	if m.refreshTokenByID != nil {
		if rt, ok := m.refreshTokenByID[id]; ok {
			rt.Used = true
			rt.SuccessorID = &successorID
			return nil
		}
	}
	if m.refreshToken != nil && m.refreshToken.ID == id {
		m.refreshToken.Used = true
		m.refreshToken.SuccessorID = &successorID
		return nil
	}
	return store.ErrNotFound
}

func (m *mockTokenStore) InvalidateRefreshTokenFamily(ctx context.Context, familyID string) (int64, error) {
	m.invalidatedFamilyID = familyID
	if m.invalidateRefreshFamilyErr != nil {
		return 0, m.invalidateRefreshFamilyErr
	}
	return 1, nil
}

func (m *mockTokenStore) RevokeAccessToken(ctx context.Context, jti string) error {
	return m.revokeAccessErr
}

func (m *mockTokenStore) GetAccessToken(ctx context.Context, jti string) (*store.AccessToken, error) {
	if m.accessTokenByJTI != nil {
		if token, ok := m.accessTokenByJTI[jti]; ok {
			return token, nil
		}
	}
	return nil, store.ErrNotFound
}

func (m *mockTokenStore) GetAccessTokenBySignature(ctx context.Context, signature string) (*store.AccessToken, error) {
	if m.accessTokenBySignature != nil {
		if token, ok := m.accessTokenBySignature[signature]; ok {
			return token, nil
		}
	}
	return nil, store.ErrNotFound
}

func (m *mockTokenStore) RevokeRefreshTokensBySession(ctx context.Context, sessionID string) (int64, error) {
	if m.revokeRefreshBySessionErr != nil {
		return 0, m.revokeRefreshBySessionErr
	}
	return 1, nil
}

func (m *mockTokenStore) RevokeAccessTokensBySession(ctx context.Context, sessionID string) (int64, error) {
	if m.revokeAccessBySessionErr != nil {
		return 0, m.revokeAccessBySessionErr
	}
	return 1, nil
}

func TestExchangeAuthorizationCode(t *testing.T) {
	ctx := context.Background()

	t.Run("Success", func(t *testing.T) {
		mockStore := &mockTokenStore{
			client: &store.Client{
				ID:                 "client-123",
				AccessTokenTTL:     900,
				RefreshTokenTTL:    2592000,
				IDTokenTTL:         3600,
				AllowOfflineAccess: true,
			},
			authCode: &store.AuthCode{
				Code:        "ac_test123",
				ClientID:    "client-123",
				IdentityID:  "identity-456",
				SessionID:   "session-789",
				RedirectURI: "https://app.example.com/callback",
				Scopes:      []string{"openid", "offline_access"},
				Audience:    []string{"api.example.com"},
				ACR:         "aal1",
				AMR:         []string{"pwd"},
				AuthTime:    time.Now().UTC(),
				ExpiresAt:   time.Now().UTC().Add(10 * time.Minute),
			},
		}

		svc := NewTokenService(mockStore, &MockJWTSigner{}, "https://auth.example.com")

		req := &TokenRequest{
			GrantType:   "authorization_code",
			Code:        "ac_test123",
			RedirectURI: "https://app.example.com/callback",
			ClientID:    "client-123",
		}

		resp, err := svc.ExchangeAuthorizationCode(ctx, req)
		require.NoError(t, err)
		assert.NotEmpty(t, resp.AccessToken)
		assert.NotNil(t, resp.RefreshToken)
		assert.NotNil(t, resp.IDToken)
		assert.Equal(t, "Bearer", resp.TokenType)
		assert.True(t, mockStore.authCode.Used)
	})

	t.Run("InvalidCode", func(t *testing.T) {
		mockStore := &mockTokenStore{
			client: &store.Client{ID: "client-123"},
		}
		svc := NewTokenService(mockStore, &MockJWTSigner{}, "https://auth.example.com")

		req := &TokenRequest{
			GrantType:   "authorization_code",
			Code:        "invalid",
			RedirectURI: "https://app.example.com/callback",
			ClientID:    "client-123",
		}

		_, err := svc.ExchangeAuthorizationCode(ctx, req)
		assert.ErrorIs(t, err, ErrInvalidGrant)
	})

	t.Run("MismatchedClient", func(t *testing.T) {
		mockStore := &mockTokenStore{
			client: &store.Client{ID: "client-456"},
			authCode: &store.AuthCode{
				Code:      "ac_test123",
				ClientID:  "client-123",
				ExpiresAt: time.Now().UTC().Add(10 * time.Minute),
			},
		}
		svc := NewTokenService(mockStore, &MockJWTSigner{}, "https://auth.example.com")

		req := &TokenRequest{
			GrantType:   "authorization_code",
			Code:        "ac_test123",
			ClientID:    "client-456",
			RedirectURI: "https://app.example.com/callback",
		}

		_, err := svc.ExchangeAuthorizationCode(ctx, req)
		assert.ErrorIs(t, err, ErrInvalidGrant)
	})
}

func TestIntrospectToken(t *testing.T) {
	ctx := context.Background()

	hash, err := bcrypt.GenerateFromPassword([]byte("secret"), bcrypt.DefaultCost)
	require.NoError(t, err)

	t.Run("active token by jti", func(t *testing.T) {
		mockStore := &mockTokenStore{
			client: &store.Client{
				ID:                      "client-1",
				TokenEndpointAuthMethod: "client_secret_post",
				SecretHash:              ptrString(string(hash)),
			},
			accessTokenByJTI: map[string]*store.AccessToken{
				"at-jti-1": {
					JTI:       "at-jti-1",
					ClientID:  "client-1",
					Subject:   "identity-1",
					Scopes:    []string{"openid", "profile"},
					Audience:  []string{"api"},
					Issuer:    "https://issuer.example.com",
					ExpiresAt: time.Now().UTC().Add(time.Minute),
				},
			},
		}
		svc := NewTokenService(mockStore, &MockJWTSigner{}, "https://issuer.example.com")

		resp, err := svc.IntrospectToken(ctx, &IntrospectionRequest{
			Token:        "at-jti-1",
			ClientID:     "client-1",
			ClientSecret: "secret",
		})
		require.NoError(t, err)
		require.NotNil(t, resp)
		assert.True(t, resp.Active)
		assert.Equal(t, "client-1", resp.ClientID)
		assert.Equal(t, "openid profile", resp.Scope)
	})

	t.Run("inactive token by signature fingerprint", func(t *testing.T) {
		token := "signed.jwt.token"
		mockStore := &mockTokenStore{
			client: &store.Client{
				ID:                      "client-1",
				TokenEndpointAuthMethod: "client_secret_post",
				SecretHash:              ptrString(string(hash)),
			},
			accessTokenBySignature: map[string]*store.AccessToken{
				accessTokenSignature(token): {
					JTI:       "at-jti-2",
					ClientID:  "client-1",
					Subject:   "identity-1",
					Scopes:    []string{"openid"},
					Issuer:    "https://issuer.example.com",
					Revoked:   true,
					ExpiresAt: time.Now().UTC().Add(time.Minute),
				},
			},
		}
		svc := NewTokenService(mockStore, &MockJWTSigner{}, "https://issuer.example.com")

		resp, err := svc.IntrospectToken(ctx, &IntrospectionRequest{
			Token:        token,
			ClientID:     "client-1",
			ClientSecret: "secret",
		})
		require.NoError(t, err)
		require.NotNil(t, resp)
		assert.False(t, resp.Active)
	})
}

func TestRefreshAccessToken(t *testing.T) {
	ctx := context.Background()

	t.Run("Success", func(t *testing.T) {
		mockStore := &mockTokenStore{
			client: &store.Client{
				ID:                 "client-123",
				AccessTokenTTL:     900,
				RefreshTokenTTL:    2592000,
				IDTokenTTL:         3600,
				AllowOfflineAccess: true,
			},
			refreshToken: &store.RefreshToken{
				ID:         "rt_test123",
				FamilyID:   "rtf_family",
				ClientID:   "client-123",
				IdentityID: "identity-456",
				SessionID:  "session-789",
				Scopes:     []string{"openid", "offline_access"},
				Audience:   []string{"api.example.com"},
				Active:     true,
				ExpiresAt:  time.Now().UTC().Add(30 * 24 * time.Hour),
			},
		}

		svc := NewTokenService(mockStore, &MockJWTSigner{}, "https://auth.example.com")

		req := &TokenRequest{
			GrantType:    "refresh_token",
			RefreshToken: "rt_test123",
			ClientID:     "client-123",
		}

		resp, err := svc.RefreshAccessToken(ctx, req)
		require.NoError(t, err)
		assert.NotEmpty(t, resp.AccessToken)
		assert.NotNil(t, resp.RefreshToken)
		assert.True(t, mockStore.refreshToken.Used)
		assert.Equal(t, 0*time.Second, mockStore.markRefreshUsedGracePeriod)
	})

	t.Run("ConfigurableGracePeriodFromMetadata", func(t *testing.T) {
		mockStore := &mockTokenStore{
			client: &store.Client{
				ID:                 "client-123",
				AccessTokenTTL:     900,
				RefreshTokenTTL:    2592000,
				IDTokenTTL:         3600,
				AllowOfflineAccess: true,
				Metadata: map[string]string{
					"refresh_token_grace_seconds": "45",
				},
			},
			refreshToken: &store.RefreshToken{
				ID:         "rt_test123",
				FamilyID:   "rtf_family",
				ClientID:   "client-123",
				IdentityID: "identity-456",
				SessionID:  "session-789",
				Scopes:     []string{"openid", "offline_access"},
				Audience:   []string{"api.example.com"},
				Active:     true,
				ExpiresAt:  time.Now().UTC().Add(30 * 24 * time.Hour),
			},
		}

		svc := NewTokenService(mockStore, &MockJWTSigner{}, "https://auth.example.com")
		_, err := svc.RefreshAccessToken(ctx, &TokenRequest{
			GrantType:    "refresh_token",
			RefreshToken: "rt_test123",
			ClientID:     "client-123",
		})
		require.NoError(t, err)
		assert.Equal(t, 45*time.Second, mockStore.markRefreshUsedGracePeriod)
	})

	t.Run("InvalidGracePeriodConfig", func(t *testing.T) {
		mockStore := &mockTokenStore{
			client: &store.Client{
				ID:                 "client-123",
				AccessTokenTTL:     900,
				RefreshTokenTTL:    2592000,
				IDTokenTTL:         3600,
				AllowOfflineAccess: true,
				Metadata: map[string]string{
					"refresh_token_grace_seconds": "not-a-number",
				},
			},
			refreshToken: &store.RefreshToken{
				ID:         "rt_test123",
				FamilyID:   "rtf_family",
				ClientID:   "client-123",
				IdentityID: "identity-456",
				SessionID:  "session-789",
				Scopes:     []string{"openid", "offline_access"},
				Audience:   []string{"api.example.com"},
				Active:     true,
				ExpiresAt:  time.Now().UTC().Add(30 * 24 * time.Hour),
			},
		}

		svc := NewTokenService(mockStore, &MockJWTSigner{}, "https://auth.example.com")
		_, err := svc.RefreshAccessToken(ctx, &TokenRequest{
			GrantType:    "refresh_token",
			RefreshToken: "rt_test123",
			ClientID:     "client-123",
		})
		assert.ErrorIs(t, err, ErrInvalidClient)
	})

	t.Run("ReplayDetection", func(t *testing.T) {
		mockStore := &mockTokenStore{
			client: &store.Client{ID: "client-123"},
			refreshToken: &store.RefreshToken{
				ID:        "rt_test123",
				FamilyID:  "rtf_family",
				ClientID:  "client-123",
				Active:    true,
				Used:      true,
				ExpiresAt: time.Now().UTC().Add(30 * 24 * time.Hour),
			},
		}

		svc := NewTokenService(mockStore, &MockJWTSigner{}, "https://auth.example.com")

		req := &TokenRequest{
			GrantType:    "refresh_token",
			RefreshToken: "rt_test123",
			ClientID:     "client-123",
		}

		_, err := svc.RefreshAccessToken(ctx, req)
		assert.Error(t, err)
	})
}

func TestExchangeAuthorizationCode_ErrorPaths(t *testing.T) {
	ctx := context.Background()
	baseClient := &store.Client{
		ID:                 "client-123",
		AccessTokenTTL:     900,
		RefreshTokenTTL:    2592000,
		IDTokenTTL:         3600,
		AllowOfflineAccess: true,
	}

	t.Run("invalid client and invalid grant", func(t *testing.T) {
		st := &mockTokenStore{getClientErr: errors.New("missing")}
		svc := NewTokenService(st, &MockJWTSigner{}, "https://issuer")
		_, err := svc.ExchangeAuthorizationCode(ctx, &TokenRequest{
			ClientID:    "client-123",
			Code:        "ac_ok",
			RedirectURI: "https://app.example.com/callback",
		})
		assert.ErrorIs(t, err, ErrInvalidClient)

		st = &mockTokenStore{
			client:         baseClient,
			getAuthCodeErr: errors.New("missing code"),
		}
		svc = NewTokenService(st, &MockJWTSigner{}, "https://issuer")
		_, err = svc.ExchangeAuthorizationCode(ctx, &TokenRequest{
			ClientID:    "client-123",
			Code:        "ac_ok",
			RedirectURI: "https://app.example.com/callback",
		})
		assert.ErrorIs(t, err, ErrInvalidGrant)
	})

	t.Run("invalid request and unauthorized grant", func(t *testing.T) {
		svc := NewTokenService(&mockTokenStore{client: baseClient}, &MockJWTSigner{}, "https://issuer")
		_, err := svc.ExchangeAuthorizationCode(ctx, &TokenRequest{
			ClientID: "client-123",
			Code:     "ac_ok",
		})
		assert.ErrorIs(t, err, ErrInvalidRequest)

		st := &mockTokenStore{
			client: &store.Client{
				ID:         "client-123",
				GrantTypes: []string{"client_credentials"},
			},
		}
		svc = NewTokenService(st, &MockJWTSigner{}, "https://issuer")
		_, err = svc.ExchangeAuthorizationCode(ctx, &TokenRequest{
			ClientID:    "client-123",
			Code:        "ac_ok",
			RedirectURI: "https://app.example.com/callback",
		})
		assert.ErrorIs(t, err, ErrUnauthorizedClient)
	})

	t.Run("confidential client secret validation", func(t *testing.T) {
		hash, err := bcrypt.GenerateFromPassword([]byte("top-secret"), bcrypt.DefaultCost)
		require.NoError(t, err)

		st := &mockTokenStore{
			client: &store.Client{
				ID:                      "client-123",
				TokenEndpointAuthMethod: "client_secret_post",
				SecretHash:              ptrString(string(hash)),
				GrantTypes:              []string{"authorization_code"},
			},
		}
		svc := NewTokenService(st, &MockJWTSigner{}, "https://issuer")
		_, err = svc.ExchangeAuthorizationCode(ctx, &TokenRequest{
			ClientID:    "client-123",
			Code:        "ac_ok",
			RedirectURI: "https://app.example.com/callback",
		})
		assert.ErrorIs(t, err, ErrInvalidClient)

		_, err = svc.ExchangeAuthorizationCode(ctx, &TokenRequest{
			ClientID:     "client-123",
			ClientSecret: "wrong",
			Code:         "ac_ok",
			RedirectURI:  "https://app.example.com/callback",
		})
		assert.ErrorIs(t, err, ErrInvalidClient)
	})

	t.Run("validation failures", func(t *testing.T) {
		st := &mockTokenStore{
			client: baseClient,
			authCode: &store.AuthCode{
				Code:      "ac_used",
				ClientID:  "client-123",
				Used:      true,
				ExpiresAt: time.Now().UTC().Add(time.Minute),
			},
		}
		svc := NewTokenService(st, &MockJWTSigner{}, "https://issuer")
		_, err := svc.ExchangeAuthorizationCode(ctx, &TokenRequest{
			ClientID:    "client-123",
			Code:        "ac_used",
			RedirectURI: "https://app.example.com/callback",
		})
		assert.ErrorIs(t, err, ErrInvalidGrant)

		st.authCode = &store.AuthCode{
			Code:        "ac_mismatch",
			ClientID:    "other-client",
			RedirectURI: "https://app.example.com/callback",
			ExpiresAt:   time.Now().UTC().Add(time.Minute),
		}
		_, err = svc.ExchangeAuthorizationCode(ctx, &TokenRequest{
			ClientID:    "client-123",
			Code:        "ac_mismatch",
			RedirectURI: "https://app.example.com/callback",
		})
		assert.ErrorIs(t, err, ErrInvalidGrant)

		st.authCode = &store.AuthCode{
			Code:        "ac_bad_redirect",
			ClientID:    "client-123",
			RedirectURI: "https://app.example.com/callback",
			ExpiresAt:   time.Now().UTC().Add(time.Minute),
		}
		_, err = svc.ExchangeAuthorizationCode(ctx, &TokenRequest{
			ClientID:    "client-123",
			Code:        "ac_bad_redirect",
			RedirectURI: "https://evil.example.com/callback",
		})
		assert.ErrorIs(t, err, ErrInvalidGrant)
	})

	t.Run("pkce and marking errors", func(t *testing.T) {
		challenge := "abc"
		method := "plain"
		st := &mockTokenStore{
			client: baseClient,
			authCode: &store.AuthCode{
				Code:                "ac_pkce",
				ClientID:            "client-123",
				RedirectURI:         "https://app.example.com/callback",
				IdentityID:          "identity-1",
				SessionID:           "session-1",
				CodeChallenge:       &challenge,
				CodeChallengeMethod: &method,
				ExpiresAt:           time.Now().UTC().Add(time.Minute),
			},
		}
		svc := NewTokenService(st, &MockJWTSigner{}, "https://issuer")
		_, err := svc.ExchangeAuthorizationCode(ctx, &TokenRequest{
			ClientID:    "client-123",
			Code:        "ac_pkce",
			RedirectURI: "https://app.example.com/callback",
		})
		assert.ErrorIs(t, err, store.ErrPKCERequired)

		_, err = svc.ExchangeAuthorizationCode(ctx, &TokenRequest{
			ClientID:     "client-123",
			Code:         "ac_pkce",
			RedirectURI:  "https://app.example.com/callback",
			CodeVerifier: "wrong",
		})
		assert.ErrorIs(t, err, store.ErrPKCEMismatch)

		st.authCode.CodeChallenge = nil
		st.markAuthCodeUsedErr = errors.New("cannot mark used")
		_, err = svc.ExchangeAuthorizationCode(ctx, &TokenRequest{
			ClientID:    "client-123",
			Code:        "ac_pkce",
			RedirectURI: "https://app.example.com/callback",
		})
		assert.ErrorIs(t, err, ErrInvalidGrant)
	})

	t.Run("issueTokens downstream errors", func(t *testing.T) {
		newAuthCode := func() *store.AuthCode {
			return &store.AuthCode{
				Code:        "ac_ok",
				ClientID:    "client-123",
				IdentityID:  "identity-1",
				SessionID:   "session-1",
				RedirectURI: "https://app.example.com/callback",
				Scopes:      []string{"openid", "offline_access"},
				ExpiresAt:   time.Now().UTC().Add(10 * time.Minute),
			}
		}

		st := &mockTokenStore{
			client:   baseClient,
			authCode: newAuthCode(),
		}
		svc := NewTokenService(st, &failingJWTSigner{accessErr: errors.New("sign access")}, "https://issuer")
		_, err := svc.ExchangeAuthorizationCode(ctx, &TokenRequest{
			ClientID:    "client-123",
			Code:        "ac_ok",
			RedirectURI: "https://app.example.com/callback",
		})
		assert.ErrorContains(t, err, "sign access")

		st = &mockTokenStore{
			client:          baseClient,
			authCode:        newAuthCode(),
			createAccessErr: errors.New("store access"),
		}
		svc = NewTokenService(st, &MockJWTSigner{}, "https://issuer")
		_, err = svc.ExchangeAuthorizationCode(ctx, &TokenRequest{
			ClientID:    "client-123",
			Code:        "ac_ok",
			RedirectURI: "https://app.example.com/callback",
		})
		assert.ErrorIs(t, err, st.createAccessErr)

		st = &mockTokenStore{
			client:           baseClient,
			authCode:         newAuthCode(),
			createRefreshErr: errors.New("store refresh"),
		}
		svc = NewTokenService(st, &MockJWTSigner{}, "https://issuer")
		_, err = svc.ExchangeAuthorizationCode(ctx, &TokenRequest{
			ClientID:    "client-123",
			Code:        "ac_ok",
			RedirectURI: "https://app.example.com/callback",
		})
		assert.ErrorIs(t, err, st.createRefreshErr)

		st = &mockTokenStore{
			client:      baseClient,
			authCode:    newAuthCode(),
			createIDErr: errors.New("store id"),
		}
		svc = NewTokenService(st, &MockJWTSigner{}, "https://issuer")
		_, err = svc.ExchangeAuthorizationCode(ctx, &TokenRequest{
			ClientID:    "client-123",
			Code:        "ac_ok",
			RedirectURI: "https://app.example.com/callback",
		})
		assert.ErrorIs(t, err, st.createIDErr)
	})
}

func TestRefreshAccessToken_ErrorPaths(t *testing.T) {
	ctx := context.Background()
	baseClient := &store.Client{
		ID:                 "client-123",
		AccessTokenTTL:     900,
		RefreshTokenTTL:    2592000,
		IDTokenTTL:         3600,
		AllowOfflineAccess: true,
	}

	t.Run("invalid client and token", func(t *testing.T) {
		st := &mockTokenStore{getClientErr: errors.New("missing")}
		svc := NewTokenService(st, &MockJWTSigner{}, "https://issuer")
		_, err := svc.RefreshAccessToken(ctx, &TokenRequest{ClientID: "client-123", RefreshToken: "rt"})
		assert.ErrorIs(t, err, ErrInvalidClient)

		st = &mockTokenStore{client: baseClient, getRefreshErr: errors.New("missing refresh")}
		svc = NewTokenService(st, &MockJWTSigner{}, "https://issuer")
		_, err = svc.RefreshAccessToken(ctx, &TokenRequest{ClientID: "client-123", RefreshToken: "rt"})
		assert.ErrorIs(t, err, ErrInvalidGrant)
	})

	t.Run("invalid request and unauthorized grant", func(t *testing.T) {
		svc := NewTokenService(&mockTokenStore{client: baseClient}, &MockJWTSigner{}, "https://issuer")
		_, err := svc.RefreshAccessToken(ctx, &TokenRequest{
			ClientID: "client-123",
		})
		assert.ErrorIs(t, err, ErrInvalidRequest)

		st := &mockTokenStore{
			client: &store.Client{
				ID:         "client-123",
				GrantTypes: []string{"authorization_code"},
			},
		}
		svc = NewTokenService(st, &MockJWTSigner{}, "https://issuer")
		_, err = svc.RefreshAccessToken(ctx, &TokenRequest{
			ClientID:     "client-123",
			RefreshToken: "rt",
		})
		assert.ErrorIs(t, err, ErrUnauthorizedClient)
	})

	t.Run("confidential refresh requires secret", func(t *testing.T) {
		hash, err := bcrypt.GenerateFromPassword([]byte("secret"), bcrypt.DefaultCost)
		require.NoError(t, err)
		st := &mockTokenStore{
			client: &store.Client{
				ID:                      "client-123",
				TokenEndpointAuthMethod: "client_secret_post",
				SecretHash:              ptrString(string(hash)),
				GrantTypes:              []string{"refresh_token"},
			},
			refreshToken: &store.RefreshToken{
				ID:        "rt-1",
				ClientID:  "client-123",
				Active:    true,
				ExpiresAt: time.Now().UTC().Add(time.Minute),
			},
		}
		svc := NewTokenService(st, &MockJWTSigner{}, "https://issuer")
		_, err = svc.RefreshAccessToken(ctx, &TokenRequest{
			ClientID:     "client-123",
			RefreshToken: "rt-1",
		})
		assert.ErrorIs(t, err, ErrInvalidClient)
	})

	t.Run("invalid refresh token state and client mismatch", func(t *testing.T) {
		st := &mockTokenStore{
			client: baseClient,
			refreshToken: &store.RefreshToken{
				ID:        "rt-inactive",
				ClientID:  "client-123",
				Active:    false,
				ExpiresAt: time.Now().UTC().Add(time.Minute),
			},
		}
		svc := NewTokenService(st, &MockJWTSigner{}, "https://issuer")
		_, err := svc.RefreshAccessToken(ctx, &TokenRequest{ClientID: "client-123", RefreshToken: "rt-inactive"})
		assert.ErrorIs(t, err, ErrInvalidGrant)

		st.refreshToken = &store.RefreshToken{
			ID:        "rt-mismatch",
			ClientID:  "other-client",
			Active:    true,
			ExpiresAt: time.Now().UTC().Add(time.Minute),
		}
		_, err = svc.RefreshAccessToken(ctx, &TokenRequest{ClientID: "client-123", RefreshToken: "rt-mismatch"})
		assert.ErrorIs(t, err, ErrInvalidGrant)
	})

	t.Run("replay detection and grace successor", func(t *testing.T) {
		successorID := "rt-successor"
		grace := time.Now().UTC().Add(10 * time.Minute)
		st := &mockTokenStore{
			client: baseClient,
			refreshTokenByID: map[string]*store.RefreshToken{
				"rt-old": {
					ID:                   "rt-old",
					FamilyID:             "family-1",
					ClientID:             "client-123",
					IdentityID:           "identity-1",
					SessionID:            "session-1",
					Scopes:               []string{"openid", "offline_access"},
					Active:               true,
					Used:                 true,
					SuccessorID:          &successorID,
					GracePeriodExpiresAt: &grace,
					ExpiresAt:            time.Now().UTC().Add(time.Hour),
				},
				"rt-successor": {
					ID:         "rt-successor",
					FamilyID:   "family-1",
					ClientID:   "client-123",
					IdentityID: "identity-1",
					SessionID:  "session-1",
					Scopes:     []string{"openid", "offline_access"},
					Active:     true,
					ExpiresAt:  time.Now().UTC().Add(time.Hour),
				},
			},
		}
		svc := NewTokenService(st, &MockJWTSigner{}, "https://issuer")
		resp, err := svc.RefreshAccessToken(ctx, &TokenRequest{ClientID: "client-123", RefreshToken: "rt-old"})
		require.NoError(t, err)
		assert.NotEmpty(t, resp.AccessToken)
		assert.NotNil(t, resp.RefreshToken)

		st = &mockTokenStore{
			client: baseClient,
			refreshToken: &store.RefreshToken{
				ID:        "rt-replay",
				FamilyID:  "family-2",
				ClientID:  "client-123",
				Active:    true,
				Used:      true,
				ExpiresAt: time.Now().UTC().Add(time.Hour),
			},
		}
		svc = NewTokenService(st, &MockJWTSigner{}, "https://issuer")
		_, err = svc.RefreshAccessToken(ctx, &TokenRequest{ClientID: "client-123", RefreshToken: "rt-replay"})
		assert.ErrorIs(t, err, store.ErrFamilyInvalidated)
		assert.Equal(t, "family-2", st.invalidatedFamilyID)
	})

	t.Run("scope restriction and mark used error", func(t *testing.T) {
		st := &mockTokenStore{
			client: baseClient,
			refreshToken: &store.RefreshToken{
				ID:         "rt-1",
				FamilyID:   "family-1",
				ClientID:   "client-123",
				IdentityID: "identity-1",
				SessionID:  "session-1",
				Scopes:     []string{"openid", "profile"},
				Active:     true,
				ExpiresAt:  time.Now().UTC().Add(time.Hour),
			},
		}
		svc := NewTokenService(st, &MockJWTSigner{}, "https://issuer")
		_, err := svc.RefreshAccessToken(ctx, &TokenRequest{
			ClientID:     "client-123",
			RefreshToken: "rt-1",
			Scope:        "admin",
		})
		assert.ErrorIs(t, err, ErrInvalidScope)

		st.refreshToken.Scopes = []string{"openid", "offline_access"}
		st.markRefreshUsedErr = errors.New("mark failed")
		_, err = svc.RefreshAccessToken(ctx, &TokenRequest{
			ClientID:     "client-123",
			RefreshToken: "rt-1",
		})
		assert.ErrorIs(t, err, st.markRefreshUsedErr)
	})
}

func TestExchangeDeviceCode(t *testing.T) {
	ctx := context.Background()

	t.Run("success", func(t *testing.T) {
		st := &mockTokenStore{
			client: &store.Client{
				ID:             "client-1",
				GrantTypes:     []string{"urn:ietf:params:oauth:grant-type:device_code"},
				Scopes:         []string{"openid", "profile"},
				AccessTokenTTL: 900,
				IDTokenTTL:     3600,
			},
		}
		svc := NewTokenService(st, &MockJWTSigner{}, "https://issuer")
		resp, err := svc.ExchangeDeviceCode(ctx, &DeviceCodeTokenRequest{
			ClientID:   "client-1",
			IdentityID: "identity-1",
			SessionID:  "session-1",
			Scopes:     []string{"openid"},
			AuthTime:   time.Now().UTC().Add(-time.Minute),
		})
		require.NoError(t, err)
		require.NotNil(t, resp)
		assert.NotEmpty(t, resp.AccessToken)
		assert.NotNil(t, resp.IDToken)
	})

	t.Run("invalid request and client lookup", func(t *testing.T) {
		svc := NewTokenService(&mockTokenStore{}, &MockJWTSigner{}, "https://issuer")
		_, err := svc.ExchangeDeviceCode(ctx, nil)
		assert.ErrorIs(t, err, ErrInvalidRequest)

		_, err = svc.ExchangeDeviceCode(ctx, &DeviceCodeTokenRequest{
			ClientID: "client-1",
		})
		assert.ErrorIs(t, err, ErrInvalidRequest)

		st := &mockTokenStore{getClientErr: errors.New("missing")}
		svc = NewTokenService(st, &MockJWTSigner{}, "https://issuer")
		_, err = svc.ExchangeDeviceCode(ctx, &DeviceCodeTokenRequest{
			ClientID:   "client-1",
			IdentityID: "identity-1",
		})
		assert.ErrorIs(t, err, ErrInvalidClient)
	})

	t.Run("grant and client auth errors", func(t *testing.T) {
		st := &mockTokenStore{
			client: &store.Client{
				ID:         "client-1",
				GrantTypes: []string{"authorization_code"},
			},
		}
		svc := NewTokenService(st, &MockJWTSigner{}, "https://issuer")
		_, err := svc.ExchangeDeviceCode(ctx, &DeviceCodeTokenRequest{
			ClientID:   "client-1",
			IdentityID: "identity-1",
		})
		assert.ErrorIs(t, err, ErrUnauthorizedClient)

		hash, hashErr := bcrypt.GenerateFromPassword([]byte("secret"), bcrypt.DefaultCost)
		require.NoError(t, hashErr)
		st = &mockTokenStore{
			client: &store.Client{
				ID:                      "client-1",
				TokenEndpointAuthMethod: "client_secret_post",
				SecretHash:              ptrString(string(hash)),
				GrantTypes:              []string{"urn:ietf:params:oauth:grant-type:device_code"},
			},
		}
		svc = NewTokenService(st, &MockJWTSigner{}, "https://issuer")
		_, err = svc.ExchangeDeviceCode(ctx, &DeviceCodeTokenRequest{
			ClientID:   "client-1",
			IdentityID: "identity-1",
		})
		assert.ErrorIs(t, err, ErrInvalidClient)
	})

	t.Run("invalid scope", func(t *testing.T) {
		st := &mockTokenStore{
			client: &store.Client{
				ID:         "client-1",
				GrantTypes: []string{"urn:ietf:params:oauth:grant-type:device_code"},
				Scopes:     []string{"openid", "profile"},
			},
		}
		svc := NewTokenService(st, &MockJWTSigner{}, "https://issuer")
		_, err := svc.ExchangeDeviceCode(ctx, &DeviceCodeTokenRequest{
			ClientID:   "client-1",
			IdentityID: "identity-1",
			Scopes:     []string{"admin"},
		})
		assert.ErrorIs(t, err, ErrInvalidScope)
	})
}

func TestRevokeToken(t *testing.T) {
	ctx := context.Background()

	t.Run("RevokeAccessToken", func(t *testing.T) {
		mockStore := &mockTokenStore{}
		svc := NewTokenService(mockStore, &MockJWTSigner{}, "https://auth.example.com")

		err := svc.RevokeToken(ctx, "at_test123", "access_token")
		assert.NoError(t, err)
	})

	t.Run("RevokeRefreshToken", func(t *testing.T) {
		mockStore := &mockTokenStore{
			refreshToken: &store.RefreshToken{
				ID:       "rt_test123",
				FamilyID: "rtf_family",
			},
		}
		svc := NewTokenService(mockStore, &MockJWTSigner{}, "https://auth.example.com")

		err := svc.RevokeToken(ctx, "rt_test123", "refresh_token")
		assert.NoError(t, err)
	})

	t.Run("UnknownTokenAndHintsReturnSuccess", func(t *testing.T) {
		mockStore := &mockTokenStore{
			revokeAccessErr: errors.New("not access"),
			getRefreshErr:   store.ErrNotFound,
		}
		svc := NewTokenService(mockStore, &MockJWTSigner{}, "https://auth.example.com")
		err := svc.RevokeToken(ctx, "unknown", "")
		assert.NoError(t, err)

		err = svc.RevokeToken(ctx, "unknown", "access_token")
		assert.NoError(t, err)

		err = svc.RevokeToken(ctx, "unknown", "refresh_token")
		assert.NoError(t, err)
	})
}

func TestComputeATHash(t *testing.T) {
	accessToken := "test-access-token"
	hash := computeATHash(accessToken)
	assert.NotEmpty(t, hash)
	// Hash is base64 URL encoded SHA256 hash truncated to half
	assert.Greater(t, len(hash), 10)
}

func TestHasScope(t *testing.T) {
	scopes := []string{"openid", "profile", "email"}

	assert.True(t, hasScope(scopes, "openid"))
	assert.True(t, hasScope(scopes, "profile"))
	assert.False(t, hasScope(scopes, "offline_access"))
}

func TestParseScopes(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []string
	}{
		{"Single", "openid", []string{"openid"}},
		{"Multiple", "openid profile email", []string{"openid", "profile", "email"}},
		{"Empty", "", []string{}},
		{"Spaces", "  openid   profile  ", []string{"openid", "profile"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseScopes(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestTokenService_AdditionalCoverageBranches(t *testing.T) {
	ctx := context.Background()

	t.Run("nil token requests return invalid request", func(t *testing.T) {
		svc := NewTokenService(&mockTokenStore{}, &MockJWTSigner{}, "https://issuer")

		_, err := svc.ExchangeAuthorizationCode(ctx, nil)
		assert.ErrorIs(t, err, ErrInvalidRequest)

		_, err = svc.RefreshAccessToken(ctx, nil)
		assert.ErrorIs(t, err, ErrInvalidRequest)
	})

	t.Run("refresh token accepts subset scope request with offline access", func(t *testing.T) {
		st := &mockTokenStore{
			client: &store.Client{
				ID:                 "client-subset",
				AccessTokenTTL:     900,
				RefreshTokenTTL:    2592000,
				IDTokenTTL:         3600,
				AllowOfflineAccess: true,
				GrantTypes:         []string{"refresh_token"},
			},
			refreshToken: &store.RefreshToken{
				ID:         "rt-subset",
				FamilyID:   "family-subset",
				ClientID:   "client-subset",
				IdentityID: "identity-subset",
				SessionID:  "session-subset",
				Scopes:     []string{"openid", "profile", "offline_access"},
				Active:     true,
				ExpiresAt:  time.Now().UTC().Add(time.Hour),
			},
		}
		svc := NewTokenService(st, &MockJWTSigner{}, "https://issuer")

		resp, err := svc.RefreshAccessToken(ctx, &TokenRequest{
			ClientID:     "client-subset",
			RefreshToken: "rt-subset",
			Scope:        "openid offline_access",
		})
		require.NoError(t, err)
		require.NotNil(t, resp)
		assert.Equal(t, "openid offline_access", resp.Scope)
	})

	t.Run("refresh token without offline_access returns invalid scope", func(t *testing.T) {
		st := &mockTokenStore{
			client: &store.Client{
				ID:                 "client-no-offline",
				AccessTokenTTL:     900,
				RefreshTokenTTL:    2592000,
				IDTokenTTL:         3600,
				AllowOfflineAccess: true,
				GrantTypes:         []string{"refresh_token"},
			},
			refreshToken: &store.RefreshToken{
				ID:         "rt-no-offline",
				FamilyID:   "family-no-offline",
				ClientID:   "client-no-offline",
				IdentityID: "identity-no-offline",
				SessionID:  "session-no-offline",
				Scopes:     []string{"openid", "offline_access"},
				Active:     true,
				ExpiresAt:  time.Now().UTC().Add(time.Hour),
			},
		}
		svc := NewTokenService(st, &MockJWTSigner{}, "https://issuer")

		_, err := svc.RefreshAccessToken(ctx, &TokenRequest{
			ClientID:     "client-no-offline",
			RefreshToken: "rt-no-offline",
			Scope:        "openid",
		})
		assert.ErrorIs(t, err, ErrInvalidScope)
	})

	t.Run("refresh token propagates issueTokens error", func(t *testing.T) {
		st := &mockTokenStore{
			client: &store.Client{
				ID:                 "client-fail",
				AccessTokenTTL:     900,
				RefreshTokenTTL:    2592000,
				IDTokenTTL:         3600,
				AllowOfflineAccess: true,
				GrantTypes:         []string{"refresh_token"},
			},
			refreshToken: &store.RefreshToken{
				ID:         "rt-fail",
				FamilyID:   "family-fail",
				ClientID:   "client-fail",
				IdentityID: "identity-fail",
				SessionID:  "session-fail",
				Scopes:     []string{"openid"},
				Active:     true,
				ExpiresAt:  time.Now().UTC().Add(time.Hour),
			},
		}
		svc := NewTokenService(st, &failingJWTSigner{accessErr: errors.New("access-sign-fail")}, "https://issuer")

		_, err := svc.RefreshAccessToken(ctx, &TokenRequest{
			ClientID:     "client-fail",
			RefreshToken: "rt-fail",
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to sign access token")
	})

	t.Run("device exchange uses fallback auth time when zero", func(t *testing.T) {
		st := &mockTokenStore{
			client: &store.Client{
				ID:             "device-client",
				GrantTypes:     []string{"device_code"},
				Scopes:         []string{"openid"},
				AccessTokenTTL: 900,
				IDTokenTTL:     3600,
			},
		}
		svc := NewTokenService(st, &MockJWTSigner{}, "https://issuer")

		resp, err := svc.ExchangeDeviceCode(ctx, &DeviceCodeTokenRequest{
			ClientID:   "device-client",
			IdentityID: "identity-device",
			Scopes:     []string{"openid"},
			AuthTime:   time.Time{},
		})
		require.NoError(t, err)
		require.NotNil(t, resp)
		assert.NotEmpty(t, resp.AccessToken)
	})

	t.Run("authorization exchange includes nonce and returns id signing error", func(t *testing.T) {
		nonce := "nonce-value"
		st := &mockTokenStore{
			client: &store.Client{
				ID:                 "client-nonce",
				AccessTokenTTL:     900,
				RefreshTokenTTL:    2592000,
				IDTokenTTL:         3600,
				AllowOfflineAccess: true,
				GrantTypes:         []string{"authorization_code"},
			},
			authCode: &store.AuthCode{
				Code:        "ac-nonce",
				ClientID:    "client-nonce",
				IdentityID:  "identity-nonce",
				SessionID:   "session-nonce",
				RedirectURI: "https://app.example.com/callback",
				Scopes:      []string{"openid"},
				Nonce:       &nonce,
				ACR:         "aal1",
				AMR:         []string{"pwd"},
				AuthTime:    time.Now().UTC().Add(-time.Minute),
				ExpiresAt:   time.Now().UTC().Add(time.Minute),
			},
		}
		svc := NewTokenService(st, &failingJWTSigner{idErr: errors.New("id-sign-fail")}, "https://issuer")

		_, err := svc.ExchangeAuthorizationCode(ctx, &TokenRequest{
			ClientID:    "client-nonce",
			Code:        "ac-nonce",
			RedirectURI: "https://app.example.com/callback",
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to sign ID token")
	})

	t.Run("authenticateClient helper edge branches", func(t *testing.T) {
		assert.ErrorIs(t, authenticateClient(nil, "secret"), ErrInvalidClient)

		err := authenticateClient(&store.Client{
			ID:                      "confidential-nohash",
			TokenEndpointAuthMethod: "client_secret_post",
			SecretHash:              nil,
		}, "secret")
		assert.ErrorIs(t, err, ErrInvalidClient)

		hash, hashErr := bcrypt.GenerateFromPassword([]byte("valid-secret"), bcrypt.DefaultCost)
		require.NoError(t, hashErr)
		err = authenticateClient(&store.Client{
			ID:                      "confidential-valid",
			TokenEndpointAuthMethod: "client_secret_post",
			SecretHash:              ptrString(string(hash)),
		}, "valid-secret")
		assert.NoError(t, err)

		err = authenticateClient(&store.Client{
			ID:                      "unsupported-auth",
			TokenEndpointAuthMethod: "private_key_jwt",
			SecretHash:              ptrString(string(hash)),
		}, "valid-secret")
		assert.ErrorIs(t, err, ErrInvalidClient)
	})
}

func TestIssueTokens_PairwiseSubject(t *testing.T) {
	ctx := context.Background()
	sectorIdentifier := "https://sector.example.com/clients.json"
	client := &store.Client{
		ID:                  "client-pairwise",
		SubjectType:         "pairwise",
		SectorIdentifierURI: &sectorIdentifier,
		AccessTokenTTL:      900,
		RefreshTokenTTL:     3600,
		IDTokenTTL:          3600,
	}
	mockStore := &mockTokenStore{client: client}
	svc := NewTokenService(mockStore, &MockJWTSigner{}, "https://issuer.example.com")

	resp, err := svc.issueTokens(ctx, client, "identity-1", "session-1", []string{"openid"}, []string{"api"}, nil, "aal1", []string{"pwd"}, time.Now().UTC())
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.NotNil(t, resp.IDToken)
	require.Len(t, mockStore.accessTokens, 1)

	subject := mockStore.accessTokens[0].Subject
	assert.NotEqual(t, "identity-1", subject)

	expected, err := resolveTokenSubject(client, "identity-1", "https://issuer.example.com")
	require.NoError(t, err)
	assert.Equal(t, expected, subject)
}

func TestResolveTokenSubject_PairwiseFallbackAndValidation(t *testing.T) {
	t.Run("uses redirect URI host when sector identifier is absent", func(t *testing.T) {
		client := &store.Client{
			ID:           "client-redirect-sector",
			SubjectType:  "pairwise",
			RedirectURIs: []string{"https://app.example.com/callback"},
		}

		subject, err := resolveTokenSubject(client, "identity-1", "https://issuer.example.com")
		require.NoError(t, err)
		assert.NotEmpty(t, subject)
		assert.NotEqual(t, "identity-1", subject)
	})

	t.Run("returns invalid client when no sector can be derived", func(t *testing.T) {
		client := &store.Client{
			ID:          "client-bad-pairwise",
			SubjectType: "pairwise",
		}

		_, err := resolveTokenSubject(client, "identity-1", "https://issuer.example.com")
		assert.ErrorIs(t, err, ErrInvalidClient)
	})
}

func ptrString(v string) *string {
	return &v
}
