package grants

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/aegion/aegion/modules/oauth2/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockGrantStore struct {
	client         *store.Client
	getClientErr   error
	createTokenErr error
	lastToken      *store.AccessToken
}

func (m *mockGrantStore) GetClient(ctx context.Context, id string) (*store.Client, error) {
	if m.getClientErr != nil {
		return nil, m.getClientErr
	}
	if m.client != nil {
		return m.client, nil
	}
	return nil, store.ErrNotFound
}

func (m *mockGrantStore) CreateAccessToken(ctx context.Context, token *store.AccessToken) error {
	m.lastToken = token
	return m.createTokenErr
}

type mockSigner struct {
	token string
	err   error
	claims map[string]interface{}
}

func (m *mockSigner) SignAccessToken(claims map[string]interface{}) (string, error) {
	m.claims = claims
	if m.err != nil {
		return "", m.err
	}
	if m.token != "" {
		return m.token, nil
	}
	return "signed.jwt", nil
}

type mockValidator struct {
	claims *JWTAssertionClaims
	err    error
}

func (m *mockValidator) ValidateJWTAssertion(ctx context.Context, assertion string, clientID string) (*JWTAssertionClaims, error) {
	if m.err != nil {
		return nil, m.err
	}
	if m.claims != nil {
		return m.claims, nil
	}
	return &JWTAssertionClaims{
		Issuer:    "issuer",
		Subject:   "subject",
		Audience:  []string{"aud"},
		ExpiresAt: time.Now().Add(time.Minute),
		IssuedAt:  time.Now(),
		Scopes:    []string{"read"},
	}, nil
}

func TestClientCredentialsService_IssueClientCredentials(t *testing.T) {
	ctx := context.Background()
	baseClient := &store.Client{
		ID:             "client-1",
		GrantTypes:     []string{"client_credentials"},
		Scopes:         []string{"read", "write"},
		AccessTokenTTL: 900,
	}

	t.Run("success", func(t *testing.T) {
		st := &mockGrantStore{client: baseClient}
		signer := &mockSigner{token: "token.cc"}
		svc := NewClientCredentialsService(st, signer, "https://issuer.example.com")

		resp, err := svc.IssueClientCredentials(ctx, &ClientCredentialsRequest{
			ClientID: "client-1",
			Scope:    "read write",
		})
		require.NoError(t, err)
		require.NotNil(t, resp)
		assert.Equal(t, "token.cc", resp.AccessToken)
		assert.Equal(t, "Bearer", resp.TokenType)
		assert.Equal(t, 900, resp.ExpiresIn)
		assert.Equal(t, "read write", resp.Scope)
		require.NotNil(t, st.lastToken)
		assert.Equal(t, "client-1", st.lastToken.ClientID)
		assert.Equal(t, []string{"read", "write"}, st.lastToken.Scopes)
	})

	t.Run("invalid client grant and scope", func(t *testing.T) {
		st := &mockGrantStore{getClientErr: errors.New("missing")}
		signer := &mockSigner{}
		svc := NewClientCredentialsService(st, signer, "issuer")
		_, err := svc.IssueClientCredentials(ctx, &ClientCredentialsRequest{ClientID: "missing"})
		assert.ErrorIs(t, err, ErrInvalidClient)

		st = &mockGrantStore{
			client: &store.Client{
				ID:             "client-1",
				GrantTypes:     []string{"authorization_code"},
				Scopes:         []string{"read"},
				AccessTokenTTL: 900,
			},
		}
		svc = NewClientCredentialsService(st, signer, "issuer")
		_, err = svc.IssueClientCredentials(ctx, &ClientCredentialsRequest{ClientID: "client-1"})
		assert.ErrorIs(t, err, ErrUnauthorizedClient)

		st.client.GrantTypes = []string{"client_credentials"}
		_, err = svc.IssueClientCredentials(ctx, &ClientCredentialsRequest{
			ClientID: "client-1",
			Scope:    "admin",
		})
		assert.ErrorIs(t, err, ErrInvalidScope)
	})

	t.Run("sign and store errors", func(t *testing.T) {
		st := &mockGrantStore{client: baseClient}
		signer := &mockSigner{err: errors.New("sign failed")}
		svc := NewClientCredentialsService(st, signer, "issuer")
		_, err := svc.IssueClientCredentials(ctx, &ClientCredentialsRequest{ClientID: "client-1"})
		assert.ErrorIs(t, err, signer.err)

		st = &mockGrantStore{client: baseClient, createTokenErr: errors.New("db")}
		signer = &mockSigner{}
		svc = NewClientCredentialsService(st, signer, "issuer")
		_, err = svc.IssueClientCredentials(ctx, &ClientCredentialsRequest{ClientID: "client-1"})
		assert.ErrorIs(t, err, st.createTokenErr)
	})
}

func TestJWTBearerService_IssueJWTBearer(t *testing.T) {
	ctx := context.Background()
	client := &store.Client{
		ID:             "client-1",
		GrantTypes:     []string{"urn:ietf:params:oauth:grant-type:jwt-bearer"},
		AccessTokenTTL: 600,
	}
	claims := &JWTAssertionClaims{
		Issuer:    "trusted",
		Subject:   "service-account",
		Audience:  []string{"api"},
		ExpiresAt: time.Now().Add(time.Minute),
		IssuedAt:  time.Now(),
		Scopes:    []string{"s1", "s2"},
	}

	t.Run("success default assertion scopes", func(t *testing.T) {
		st := &mockGrantStore{client: client}
		signer := &mockSigner{token: "token.jwt"}
		validator := &mockValidator{claims: claims}
		svc := NewJWTBearerService(st, signer, "https://issuer", validator)

		resp, err := svc.IssueJWTBearer(ctx, &JWTBearerRequest{
			ClientID:  "client-1",
			Assertion: "assertion",
		})
		require.NoError(t, err)
		assert.Equal(t, "token.jwt", resp.AccessToken)
		assert.Equal(t, "s1 s2", resp.Scope)
		require.NotNil(t, st.lastToken)
		assert.Equal(t, "service-account", st.lastToken.IdentityID)
	})

	t.Run("explicit scope overrides assertion scopes", func(t *testing.T) {
		st := &mockGrantStore{client: client}
		signer := &mockSigner{}
		validator := &mockValidator{claims: claims}
		svc := NewJWTBearerService(st, signer, "https://issuer", validator)

		resp, err := svc.IssueJWTBearer(ctx, &JWTBearerRequest{
			ClientID:  "client-1",
			Assertion: "assertion",
			Scope:     "custom one",
		})
		require.NoError(t, err)
		assert.Equal(t, "custom one", resp.Scope)
		assert.Equal(t, []string{"custom", "one"}, st.lastToken.Scopes)
	})

	t.Run("invalid client unauthorized and validator errors", func(t *testing.T) {
		st := &mockGrantStore{getClientErr: errors.New("missing")}
		signer := &mockSigner{}
		validator := &mockValidator{}
		svc := NewJWTBearerService(st, signer, "issuer", validator)
		_, err := svc.IssueJWTBearer(ctx, &JWTBearerRequest{ClientID: "x"})
		assert.ErrorIs(t, err, ErrInvalidClient)

		st = &mockGrantStore{
			client: &store.Client{
				ID:             "x",
				GrantTypes:     []string{"authorization_code"},
				AccessTokenTTL: 600,
			},
		}
		svc = NewJWTBearerService(st, signer, "issuer", validator)
		_, err = svc.IssueJWTBearer(ctx, &JWTBearerRequest{ClientID: "x"})
		assert.ErrorIs(t, err, ErrUnauthorizedClient)

		st.client.GrantTypes = []string{"urn:ietf:params:oauth:grant-type:jwt-bearer"}
		validator.err = errors.New("bad assertion")
		_, err = svc.IssueJWTBearer(ctx, &JWTBearerRequest{ClientID: "x"})
		assert.ErrorIs(t, err, validator.err)
	})

	t.Run("sign and store errors", func(t *testing.T) {
		st := &mockGrantStore{client: client}
		signer := &mockSigner{err: errors.New("sign")}
		validator := &mockValidator{claims: claims}
		svc := NewJWTBearerService(st, signer, "issuer", validator)
		_, err := svc.IssueJWTBearer(ctx, &JWTBearerRequest{ClientID: "client-1"})
		assert.ErrorIs(t, err, signer.err)

		st = &mockGrantStore{client: client, createTokenErr: errors.New("db")}
		signer = &mockSigner{}
		svc = NewJWTBearerService(st, signer, "issuer", validator)
		_, err = svc.IssueJWTBearer(ctx, &JWTBearerRequest{ClientID: "client-1"})
		assert.ErrorIs(t, err, st.createTokenErr)
	})
}

func TestGrantHelpers(t *testing.T) {
	assert.True(t, hasGrantType([]string{"a", "b"}, "b"))
	assert.False(t, hasGrantType([]string{"a", "b"}, "c"))
	assert.Equal(t, []string{}, parseScopes(""))
	assert.Equal(t, []string{"read", "write"}, parseScopes(" read  write "))
}

func TestMockJWTValidator_DefaultAndError(t *testing.T) {
	m := &MockJWTValidator{}
	claims, err := m.ValidateJWTAssertion(context.Background(), "assert", "client")
	require.NoError(t, err)
	require.NotNil(t, claims)
	assert.NotEmpty(t, claims.Subject)

	m = &MockJWTValidator{Err: errors.New("boom")}
	_, err = m.ValidateJWTAssertion(context.Background(), "assert", "client")
	assert.ErrorIs(t, err, m.Err)

	custom := &JWTAssertionClaims{Subject: "custom"}
	m = &MockJWTValidator{Claims: custom}
	claims, err = m.ValidateJWTAssertion(context.Background(), "assert", "client")
	require.NoError(t, err)
	assert.Equal(t, "custom", claims.Subject)
}
