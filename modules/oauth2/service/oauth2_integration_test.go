package service_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/aegion/aegion/modules/oauth2/service/authorization"
	"github.com/aegion/aegion/modules/oauth2/service/revocation"
	tokenservice "github.com/aegion/aegion/modules/oauth2/service/token"
	"github.com/aegion/aegion/modules/oauth2/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type integrationStore struct {
	client            *store.Client
	sessionAuthCtx    map[string]*store.SessionAuthContext
	loginChallenges   map[string]*store.LoginChallenge
	consentChallenges map[string]*store.ConsentChallenge
	consentSessions   map[string]*store.ConsentSession
	authCodes         map[string]*store.AuthCode
	accessTokens      map[string]*store.AccessToken
	refreshTokens     map[string]*store.RefreshToken
	idTokens          map[string]*store.IDToken
}

func newIntegrationStore(client *store.Client) *integrationStore {
	return &integrationStore{
		client:            client,
		sessionAuthCtx:    map[string]*store.SessionAuthContext{},
		loginChallenges:   map[string]*store.LoginChallenge{},
		consentChallenges: map[string]*store.ConsentChallenge{},
		consentSessions:   map[string]*store.ConsentSession{},
		authCodes:         map[string]*store.AuthCode{},
		accessTokens:      map[string]*store.AccessToken{},
		refreshTokens:     map[string]*store.RefreshToken{},
		idTokens:          map[string]*store.IDToken{},
	}
}

func (s *integrationStore) consentKey(clientID, identityID string) string {
	return fmt.Sprintf("%s:%s", clientID, identityID)
}

func (s *integrationStore) GetClient(ctx context.Context, id string) (*store.Client, error) {
	if s.client != nil && s.client.ID == id {
		return s.client, nil
	}
	return nil, store.ErrNotFound
}

func (s *integrationStore) CreateAuthCode(ctx context.Context, code *store.AuthCode) error {
	s.authCodes[code.Code] = code
	return nil
}

func (s *integrationStore) GetAuthCode(ctx context.Context, code string) (*store.AuthCode, error) {
	if c, ok := s.authCodes[code]; ok {
		return c, nil
	}
	return nil, store.ErrNotFound
}

func (s *integrationStore) MarkAuthCodeUsed(ctx context.Context, code string) error {
	c, ok := s.authCodes[code]
	if !ok {
		return store.ErrNotFound
	}
	c.Used = true
	return nil
}

func (s *integrationStore) GetSessionAuthContext(ctx context.Context, sessionID string) (*store.SessionAuthContext, error) {
	if authCtx, ok := s.sessionAuthCtx[sessionID]; ok {
		return authCtx, nil
	}
	return nil, store.ErrNotFound
}

func (s *integrationStore) CreateLoginChallenge(ctx context.Context, challenge *store.LoginChallenge) error {
	s.loginChallenges[challenge.ID] = challenge
	return nil
}

func (s *integrationStore) GetLoginChallenge(ctx context.Context, id string) (*store.LoginChallenge, error) {
	if c, ok := s.loginChallenges[id]; ok {
		return c, nil
	}
	return nil, store.ErrNotFound
}

func (s *integrationStore) AcceptLoginChallenge(ctx context.Context, id, identityID, sessionID string) error {
	ch, ok := s.loginChallenges[id]
	if !ok {
		return store.ErrNotFound
	}
	ch.IdentityID = &identityID
	ch.SessionID = &sessionID
	now := time.Now().UTC()
	ch.AuthenticatedAt = &now
	return nil
}

func (s *integrationStore) CreateConsentChallenge(ctx context.Context, challenge *store.ConsentChallenge) error {
	s.consentChallenges[challenge.ID] = challenge
	return nil
}

func (s *integrationStore) GetConsentChallenge(ctx context.Context, id string) (*store.ConsentChallenge, error) {
	if c, ok := s.consentChallenges[id]; ok {
		return c, nil
	}
	return nil, store.ErrNotFound
}

func (s *integrationStore) AcceptConsentChallenge(ctx context.Context, id string, grantedScopes, grantedAudience []string, remember bool, rememberFor *int) error {
	ch, ok := s.consentChallenges[id]
	if !ok {
		return store.ErrNotFound
	}
	ch.Handled = true
	ch.GrantedScopes = grantedScopes
	ch.GrantedAudience = grantedAudience
	ch.Remember = &remember
	ch.RememberFor = rememberFor
	return nil
}

func (s *integrationStore) RejectConsentChallenge(ctx context.Context, id, errorCode, errorDesc string) error {
	ch, ok := s.consentChallenges[id]
	if !ok {
		return store.ErrNotFound
	}
	ch.Handled = true
	ch.Rejected = true
	ch.Error = &errorCode
	ch.ErrorDescription = &errorDesc
	return nil
}

func (s *integrationStore) GetConsentSession(ctx context.Context, clientID, identityID string) (*store.ConsentSession, error) {
	if cs, ok := s.consentSessions[s.consentKey(clientID, identityID)]; ok {
		return cs, nil
	}
	return nil, store.ErrNotFound
}

func (s *integrationStore) CreateConsentSession(ctx context.Context, consent *store.ConsentSession) error {
	s.consentSessions[s.consentKey(consent.ClientID, consent.IdentityID)] = consent
	return nil
}

func (s *integrationStore) CreateAccessToken(ctx context.Context, token *store.AccessToken) error {
	s.accessTokens[token.JTI] = token
	return nil
}

func (s *integrationStore) CreateRefreshToken(ctx context.Context, token *store.RefreshToken) error {
	s.refreshTokens[token.ID] = token
	return nil
}

func (s *integrationStore) CreateIDToken(ctx context.Context, token *store.IDToken) error {
	s.idTokens[token.JTI] = token
	return nil
}

func (s *integrationStore) GetRefreshToken(ctx context.Context, id string) (*store.RefreshToken, error) {
	if t, ok := s.refreshTokens[id]; ok {
		return t, nil
	}
	return nil, store.ErrNotFound
}

func (s *integrationStore) MarkRefreshTokenUsed(ctx context.Context, id, successorID string, gracePeriod time.Duration) error {
	t, ok := s.refreshTokens[id]
	if !ok {
		return store.ErrNotFound
	}
	t.Used = true
	t.SuccessorID = &successorID
	if gracePeriod > 0 {
		exp := time.Now().UTC().Add(gracePeriod)
		t.GracePeriodExpiresAt = &exp
	}
	return nil
}

func (s *integrationStore) InvalidateRefreshTokenFamily(ctx context.Context, familyID string) (int64, error) {
	var count int64
	for _, t := range s.refreshTokens {
		if t.FamilyID == familyID && t.Active {
			t.Active = false
			count++
		}
	}
	return count, nil
}

func (s *integrationStore) RevokeAccessToken(ctx context.Context, jti string) error {
	at, ok := s.accessTokens[jti]
	if !ok {
		return store.ErrNotFound
	}
	at.Revoked = true
	now := time.Now().UTC()
	at.RevokedAt = &now
	return nil
}

func (s *integrationStore) RevokeRefreshTokensBySession(ctx context.Context, sessionID string) (int64, error) {
	var count int64
	for _, t := range s.refreshTokens {
		if t.SessionID == sessionID && t.Active {
			t.Active = false
			count++
		}
	}
	return count, nil
}

func (s *integrationStore) RevokeAccessTokensBySession(ctx context.Context, sessionID string) (int64, error) {
	var count int64
	now := time.Now().UTC()
	for _, t := range s.accessTokens {
		if t.SessionID == sessionID && !t.Revoked {
			t.Revoked = true
			t.RevokedAt = &now
			count++
		}
	}
	return count, nil
}

func TestOAuth2AuthorizationCodeFlowIntegration(t *testing.T) {
	ctx := context.Background()
	client := &store.Client{
		ID:                      "client-1",
		Name:                    "Integration Client",
		RedirectURIs:            []string{"https://app.example.com/callback"},
		ResponseTypes:           []string{"code"},
		Scopes:                  []string{"openid", "profile", "offline_access"},
		TokenEndpointAuthMethod: "none",
		AccessTokenTTL:          900,
		RefreshTokenTTL:         2592000,
		IDTokenTTL:              3600,
		AllowOfflineAccess:      true,
		RequireConsent:          true,
	}
	memStore := newIntegrationStore(client)

	authzSvc := authorization.NewAuthorizationService(memStore)
	tokenSvc := tokenservice.NewTokenService(memStore, &tokenservice.MockJWTSigner{}, "https://issuer.example.com")
	revocationSvc := revocation.NewRevocationService(memStore)

	authorizeResp, err := authzSvc.StartAuthorization(ctx, &authorization.AuthorizeRequest{
		ClientID:     "client-1",
		RedirectURI:  "https://app.example.com/callback",
		ResponseType: "code",
		Scope:        "openid profile offline_access",
		State:        "state-123",
	})
	require.NoError(t, err)
	require.NotEmpty(t, authorizeResp.LoginChallenge)

	consentResp, err := authzSvc.AcceptLogin(ctx, authorizeResp.LoginChallenge, "identity-1", "session-1")
	require.NoError(t, err)
	require.NotEmpty(t, consentResp.ConsentChallenge)

	authResp, err := authzSvc.AcceptConsent(ctx, consentResp.ConsentChallenge, []string{"openid", "profile", "offline_access"}, true, nil)
	require.NoError(t, err)
	require.NotEmpty(t, authResp.Code)
	assert.Equal(t, "state-123", authResp.State)

	tokenResp, err := tokenSvc.ExchangeAuthorizationCode(ctx, &tokenservice.TokenRequest{
		GrantType:   "authorization_code",
		Code:        authResp.Code,
		RedirectURI: "https://app.example.com/callback",
		ClientID:    "client-1",
	})
	require.NoError(t, err)
	require.NotEmpty(t, tokenResp.AccessToken)
	require.NotNil(t, tokenResp.RefreshToken)
	require.NotNil(t, tokenResp.IDToken)

	refreshResp, err := tokenSvc.RefreshAccessToken(ctx, &tokenservice.TokenRequest{
		GrantType:    "refresh_token",
		RefreshToken: *tokenResp.RefreshToken,
		ClientID:     "client-1",
	})
	require.NoError(t, err)
	require.NotEmpty(t, refreshResp.AccessToken)
	require.NotNil(t, refreshResp.RefreshToken)

	err = revocationSvc.RevokeToken(ctx, &revocation.RevocationRequest{
		Token:         *tokenResp.RefreshToken,
		TokenTypeHint: "refresh_token",
		ClientID:      "client-1",
	})
	require.NoError(t, err)

	originalRefresh, err := memStore.GetRefreshToken(ctx, *tokenResp.RefreshToken)
	require.NoError(t, err)
	assert.False(t, originalRefresh.Active)
}

func TestOAuth2Integration_PKCEEnforcement(t *testing.T) {
	ctx := context.Background()
	client := &store.Client{
		ID:            "pkce-client",
		RedirectURIs:  []string{"https://app.example.com/callback"},
		ResponseTypes: []string{"code"},
		Scopes:        []string{"openid"},
		RequirePKCE:   true,
	}
	memStore := newIntegrationStore(client)
	authzSvc := authorization.NewAuthorizationService(memStore)

	_, err := authzSvc.StartAuthorization(ctx, &authorization.AuthorizeRequest{
		ClientID:     "pkce-client",
		RedirectURI:  "https://app.example.com/callback",
		ResponseType: "code",
		Scope:        "openid",
	})
	assert.ErrorIs(t, err, authorization.ErrPKCERequired)

	_, err = authzSvc.StartAuthorization(ctx, &authorization.AuthorizeRequest{
		ClientID:            "pkce-client",
		RedirectURI:         "https://app.example.com/callback",
		ResponseType:        "code",
		Scope:               "openid",
		CodeChallenge:       "challenge",
		CodeChallengeMethod: "S256",
	})
	require.NoError(t, err)
}

func TestOAuth2Integration_ReplayInvalidatesFamily(t *testing.T) {
	ctx := context.Background()
	client := &store.Client{
		ID:                 "client-1",
		AccessTokenTTL:     900,
		RefreshTokenTTL:    2592000,
		IDTokenTTL:         3600,
		AllowOfflineAccess: true,
	}
	memStore := newIntegrationStore(client)
	tokenSvc := tokenservice.NewTokenService(memStore, &tokenservice.MockJWTSigner{}, "https://issuer")

	memStore.refreshTokens["rt-replay"] = &store.RefreshToken{
		ID:         "rt-replay",
		FamilyID:   "family-1",
		ClientID:   "client-1",
		IdentityID: "identity-1",
		SessionID:  "session-1",
		Scopes:     []string{"openid", "offline_access"},
		Active:     true,
		Used:       true,
		ExpiresAt:  time.Now().UTC().Add(time.Hour),
	}

	_, err := tokenSvc.RefreshAccessToken(ctx, &tokenservice.TokenRequest{
		GrantType:    "refresh_token",
		RefreshToken: "rt-replay",
		ClientID:     "client-1",
	})
	assert.ErrorIs(t, err, store.ErrFamilyInvalidated)

	rt, err := memStore.GetRefreshToken(ctx, "rt-replay")
	require.NoError(t, err)
	assert.False(t, rt.Active)
}

func TestIntegrationStore_NotFoundCases(t *testing.T) {
	s := newIntegrationStore(&store.Client{ID: "client-1"})
	ctx := context.Background()

	_, err := s.GetAuthCode(ctx, "missing")
	assert.ErrorIs(t, err, store.ErrNotFound)
	_, err = s.GetRefreshToken(ctx, "missing")
	assert.ErrorIs(t, err, store.ErrNotFound)
	err = s.MarkAuthCodeUsed(ctx, "missing")
	assert.ErrorIs(t, err, store.ErrNotFound)
	err = s.MarkRefreshTokenUsed(ctx, "missing", "next", 0)
	assert.ErrorIs(t, err, store.ErrNotFound)
	err = s.RevokeAccessToken(ctx, "missing")
	assert.ErrorIs(t, err, store.ErrNotFound)

	_, err = s.GetClient(ctx, "other")
	assert.True(t, errors.Is(err, store.ErrNotFound))
}
