package service_test

import (
	"context"
	"testing"
	"time"

	"github.com/aegion/aegion/modules/oauth2/service/authorization"
	tokenservice "github.com/aegion/aegion/modules/oauth2/service/token"
	"github.com/aegion/aegion/modules/oauth2/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSecurity_PKCETokenExchangeEnforcement(t *testing.T) {
	ctx := context.Background()
	client := &store.Client{
		ID:                 "client-1",
		AccessTokenTTL:     900,
		RefreshTokenTTL:    2592000,
		IDTokenTTL:         3600,
		AllowOfflineAccess: true,
	}
	memStore := newIntegrationStore(client)

	challenge := "expected-verifier"
	method := "plain"
	memStore.authCodes["ac-pkce"] = &store.AuthCode{
		Code:                "ac-pkce",
		ClientID:            "client-1",
		IdentityID:          "identity-1",
		SessionID:           "session-1",
		RedirectURI:         "https://app.example.com/callback",
		Scopes:              []string{"openid"},
		CodeChallenge:       &challenge,
		CodeChallengeMethod: &method,
		ExpiresAt:           time.Now().UTC().Add(time.Minute),
	}

	tokenSvc := tokenservice.NewTokenService(memStore, &tokenservice.MockJWTSigner{}, "https://issuer")

	_, err := tokenSvc.ExchangeAuthorizationCode(ctx, &tokenservice.TokenRequest{
		GrantType:   "authorization_code",
		Code:        "ac-pkce",
		ClientID:    "client-1",
		RedirectURI: "https://app.example.com/callback",
	})
	assert.ErrorIs(t, err, store.ErrPKCERequired)

	_, err = tokenSvc.ExchangeAuthorizationCode(ctx, &tokenservice.TokenRequest{
		GrantType:    "authorization_code",
		Code:         "ac-pkce",
		ClientID:     "client-1",
		RedirectURI:  "https://app.example.com/callback",
		CodeVerifier: "wrong",
	})
	assert.ErrorIs(t, err, store.ErrPKCEMismatch)
}

func TestSecurity_RefreshReplayInvalidatesWholeFamily(t *testing.T) {
	ctx := context.Background()
	client := &store.Client{
		ID:                 "client-1",
		AccessTokenTTL:     900,
		RefreshTokenTTL:    2592000,
		IDTokenTTL:         3600,
		AllowOfflineAccess: true,
	}
	memStore := newIntegrationStore(client)
	memStore.refreshTokens["rt-used"] = &store.RefreshToken{
		ID:         "rt-used",
		FamilyID:   "family-1",
		ClientID:   "client-1",
		IdentityID: "identity-1",
		SessionID:  "session-1",
		Scopes:     []string{"openid", "offline_access"},
		Active:     true,
		Used:       true,
		ExpiresAt:  time.Now().UTC().Add(time.Hour),
	}
	memStore.refreshTokens["rt-sibling"] = &store.RefreshToken{
		ID:         "rt-sibling",
		FamilyID:   "family-1",
		ClientID:   "client-1",
		IdentityID: "identity-1",
		SessionID:  "session-1",
		Scopes:     []string{"openid", "offline_access"},
		Active:     true,
		ExpiresAt:  time.Now().UTC().Add(time.Hour),
	}

	tokenSvc := tokenservice.NewTokenService(memStore, &tokenservice.MockJWTSigner{}, "https://issuer")
	_, err := tokenSvc.RefreshAccessToken(ctx, &tokenservice.TokenRequest{
		GrantType:    "refresh_token",
		RefreshToken: "rt-used",
		ClientID:     "client-1",
	})
	assert.ErrorIs(t, err, store.ErrFamilyInvalidated)

	require.False(t, memStore.refreshTokens["rt-used"].Active)
	require.False(t, memStore.refreshTokens["rt-sibling"].Active)
}

func TestSecurity_StrictRedirectURIValidation(t *testing.T) {
	ctx := context.Background()
	client := &store.Client{
		ID:            "client-1",
		RedirectURIs:  []string{"https://app.example.com/callback"},
		ResponseTypes: []string{"code"},
		Scopes:        []string{"openid"},
	}
	memStore := newIntegrationStore(client)
	authzSvc := authorization.NewAuthorizationService(memStore)

	_, err := authzSvc.StartAuthorization(ctx, &authorization.AuthorizeRequest{
		ClientID:     "client-1",
		RedirectURI:  "https://evil.example.com/callback",
		ResponseType: "code",
		Scope:        "openid",
	})
	assert.ErrorIs(t, err, authorization.ErrInvalidRequest)
}
