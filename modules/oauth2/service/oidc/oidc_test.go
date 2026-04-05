package oidc

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type errJWKSProvider struct {
	err error
}

func (p *errJWKSProvider) GetPublicKeys(ctx context.Context) ([]JWK, error) {
	return nil, p.err
}

func TestDiscoveryService(t *testing.T) {
	svc := NewDiscoveryService("https://issuer.example.com/", "https://auth.example.com")

	doc, err := svc.GetDiscoveryDocument(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "https://issuer.example.com", doc.Issuer)
	assert.Equal(t, "https://auth.example.com/oauth2/authorize", doc.AuthorizationEndpoint)
	assert.Contains(t, doc.GrantTypesSupported, "authorization_code")
	assert.Contains(t, doc.TokenEndpointAuthMethodsSupported, "private_key_jwt")
	assert.Contains(t, doc.CodeChallengeMethodsSupported, "S256")

	raw, err := svc.MarshalDiscoveryDocument(context.Background())
	require.NoError(t, err)
	var out DiscoveryDocument
	require.NoError(t, json.Unmarshal(raw, &out))
	assert.Equal(t, doc.Issuer, out.Issuer)

	assert.Equal(t, "https://x.example.com", issuer("https://x.example.com/"))
	assert.Equal(t, "https://x.example.com", issuer("https://x.example.com"))
	assert.Equal(t, "", issuer(""))
}

func TestJWKSService(t *testing.T) {
	ctx := context.Background()

	mockProvider := &MockJWKSProvider{
		Keys: []JWK{
			{KTY: "RSA", KID: "k1", ALG: "RS256", N: "mod", E: "AQAB"},
		},
	}
	svc := NewJWKSService(mockProvider)

	jwks, err := svc.GetJWKS(ctx)
	require.NoError(t, err)
	require.Len(t, jwks.Keys, 1)
	assert.Equal(t, "k1", jwks.Keys[0].KID)

	data, err := svc.MarshalJWKS(ctx)
	require.NoError(t, err)
	var parsed JWKS
	require.NoError(t, json.Unmarshal(data, &parsed))
	require.Len(t, parsed.Keys, 1)
	assert.Equal(t, "RSA", parsed.Keys[0].KTY)

	svc = NewJWKSService(&errJWKSProvider{err: errors.New("boom")})
	_, err = svc.GetJWKS(ctx)
	assert.ErrorContains(t, err, "boom")
	_, err = svc.MarshalJWKS(ctx)
	assert.ErrorContains(t, err, "boom")

	defaultProvider := &MockJWKSProvider{}
	keys, err := defaultProvider.GetPublicKeys(ctx)
	require.NoError(t, err)
	require.NotEmpty(t, keys)
	assert.Equal(t, "key-1", keys[0].KID)
}

type errTokenValidator struct {
	err   error
	token *AccessToken
}

func (v *errTokenValidator) ValidateAccessToken(ctx context.Context, token string) (*AccessToken, error) {
	if v.err != nil {
		return nil, v.err
	}
	return v.token, nil
}

type errUserInfoProvider struct {
	err    error
	claims *UserInfoClaims
}

func (p *errUserInfoProvider) GetUserInfo(ctx context.Context, identityID string, scopes []string) (*UserInfoClaims, error) {
	if p.err != nil {
		return nil, p.err
	}
	return p.claims, nil
}

func TestUserInfoService(t *testing.T) {
	ctx := context.Background()

	email := "user@example.com"
	claims := &UserInfoClaims{Sub: "identity-1", Email: &email}

	svc := NewUserInfoService(
		&errTokenValidator{
			token: &AccessToken{
				JTI:        "jti-1",
				IdentityID: "identity-1",
				Scopes:     []string{"openid", "profile"},
				ExpiresAt:  time.Now().Add(time.Minute),
			},
		},
		&errUserInfoProvider{claims: claims},
	)

	got, err := svc.GetUserInfo(ctx, "access-token")
	require.NoError(t, err)
	assert.Equal(t, "identity-1", got.Sub)
	require.NotNil(t, got.Email)
	assert.Equal(t, "user@example.com", *got.Email)

	svc = NewUserInfoService(&errTokenValidator{err: errors.New("bad token")}, &errUserInfoProvider{})
	_, err = svc.GetUserInfo(ctx, "access-token")
	assert.ErrorIs(t, err, ErrInvalidToken)

	svc = NewUserInfoService(
		&errTokenValidator{
			token: &AccessToken{
				IdentityID: "identity-1",
				Scopes:     []string{"profile"},
				ExpiresAt:  time.Now().Add(time.Minute),
			},
		},
		&errUserInfoProvider{},
	)
	_, err = svc.GetUserInfo(ctx, "access-token")
	assert.ErrorIs(t, err, ErrInsufficientScope)

	svc = NewUserInfoService(
		&errTokenValidator{
			token: &AccessToken{
				IdentityID: "identity-1",
				Scopes:     []string{"openid"},
				ExpiresAt:  time.Now().Add(time.Minute),
			},
		},
		&errUserInfoProvider{err: errors.New("provider down")},
	)
	_, err = svc.GetUserInfo(ctx, "access-token")
	assert.ErrorContains(t, err, "provider down")
}

func TestUserInfoHelpersAndMocks(t *testing.T) {
	assert.True(t, hasScope([]string{"openid", "profile"}, "openid"))
	assert.False(t, hasScope([]string{"profile"}, "openid"))

	tv := &MockTokenValidator{}
	token, err := tv.ValidateAccessToken(context.Background(), "x")
	require.NoError(t, err)
	require.NotNil(t, token)
	assert.Equal(t, "identity-123", token.IdentityID)

	tv = &MockTokenValidator{Err: errors.New("bad")}
	_, err = tv.ValidateAccessToken(context.Background(), "x")
	assert.ErrorContains(t, err, "bad")

	tv = &MockTokenValidator{
		Token: &AccessToken{
			JTI:        "custom-jti",
			IdentityID: "custom-identity",
			Scopes:     []string{"openid"},
			ExpiresAt:  time.Now().Add(time.Minute),
		},
	}
	token, err = tv.ValidateAccessToken(context.Background(), "x")
	require.NoError(t, err)
	assert.Equal(t, "custom-identity", token.IdentityID)

	provider := &MockUserInfoProvider{}
	info, err := provider.GetUserInfo(context.Background(), "id-1", []string{"openid"})
	require.NoError(t, err)
	assert.Equal(t, "id-1", info.Sub)

	customClaims := &UserInfoClaims{Sub: "custom-sub"}
	provider = &MockUserInfoProvider{Claims: customClaims}
	info, err = provider.GetUserInfo(context.Background(), "ignored", nil)
	require.NoError(t, err)
	assert.Equal(t, "custom-sub", info.Sub)

	provider = &MockUserInfoProvider{Err: errors.New("oops")}
	_, err = provider.GetUserInfo(context.Background(), "id-1", nil)
	assert.ErrorContains(t, err, "oops")
}
