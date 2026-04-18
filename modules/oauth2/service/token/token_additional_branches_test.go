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

type introspectionJTIErrorStore struct {
	*mockTokenStore
	err error
}

func (s *introspectionJTIErrorStore) GetAccessToken(ctx context.Context, jti string) (*store.AccessToken, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.mockTokenStore.GetAccessToken(ctx, jti)
}

type introspectionSignatureErrorStore struct {
	*mockTokenStore
	err error
}

func (s *introspectionSignatureErrorStore) GetAccessTokenBySignature(ctx context.Context, signature string) (*store.AccessToken, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.mockTokenStore.GetAccessTokenBySignature(ctx, signature)
}

type tokenStoreWithoutReaders struct{}

func (tokenStoreWithoutReaders) GetClient(ctx context.Context, id string) (*store.Client, error) {
	return &store.Client{ID: id}, nil
}
func (tokenStoreWithoutReaders) GetAuthCode(ctx context.Context, code string) (*store.AuthCode, error) {
	return nil, store.ErrNotFound
}
func (tokenStoreWithoutReaders) MarkAuthCodeUsed(ctx context.Context, code string) error {
	return nil
}
func (tokenStoreWithoutReaders) CreateAccessToken(ctx context.Context, token *store.AccessToken) error {
	return nil
}
func (tokenStoreWithoutReaders) CreateRefreshToken(ctx context.Context, token *store.RefreshToken) error {
	return nil
}
func (tokenStoreWithoutReaders) CreateIDToken(ctx context.Context, token *store.IDToken) error {
	return nil
}
func (tokenStoreWithoutReaders) GetRefreshToken(ctx context.Context, id string) (*store.RefreshToken, error) {
	return nil, store.ErrNotFound
}
func (tokenStoreWithoutReaders) MarkRefreshTokenUsed(ctx context.Context, id, successorID string, gracePeriod time.Duration) error {
	return nil
}
func (tokenStoreWithoutReaders) InvalidateRefreshTokenFamily(ctx context.Context, familyID string) (int64, error) {
	return 0, nil
}
func (tokenStoreWithoutReaders) RevokeAccessToken(ctx context.Context, jti string) error {
	return nil
}
func (tokenStoreWithoutReaders) RevokeRefreshTokensBySession(ctx context.Context, sessionID string) (int64, error) {
	return 0, nil
}
func (tokenStoreWithoutReaders) RevokeAccessTokensBySession(ctx context.Context, sessionID string) (int64, error) {
	return 0, nil
}

func TestIntrospectTokenAdditionalErrorBranches(t *testing.T) {
	ctx := context.Background()
	hash, err := bcrypt.GenerateFromPassword([]byte("secret"), bcrypt.DefaultCost)
	require.NoError(t, err)
	secretHash := string(hash)

	t.Run("validation and client lookup failures", func(t *testing.T) {
		svc := NewTokenService(&mockTokenStore{}, &MockJWTSigner{}, "https://issuer.example.com")
		_, err := svc.IntrospectToken(ctx, nil)
		assert.ErrorIs(t, err, ErrInvalidRequest)

		_, err = svc.IntrospectToken(ctx, &IntrospectionRequest{Token: "tok", ClientID: ""})
		assert.ErrorIs(t, err, ErrInvalidRequest)

		svc = NewTokenService(&mockTokenStore{getClientErr: errors.New("client missing")}, &MockJWTSigner{}, "https://issuer.example.com")
		_, err = svc.IntrospectToken(ctx, &IntrospectionRequest{Token: "tok", ClientID: "client-1"})
		assert.ErrorIs(t, err, ErrInvalidClient)
	})

	t.Run("client authentication failure", func(t *testing.T) {
		svc := NewTokenService(&mockTokenStore{
			client: &store.Client{
				ID:                      "client-1",
				TokenEndpointAuthMethod: "client_secret_post",
				SecretHash:              &secretHash,
			},
		}, &MockJWTSigner{}, "https://issuer.example.com")

		_, err := svc.IntrospectToken(ctx, &IntrospectionRequest{
			Token:        "tok",
			ClientID:     "client-1",
			ClientSecret: "wrong",
		})
		assert.ErrorIs(t, err, ErrInvalidClient)
	})

	t.Run("introspection lookup returns non-notfound errors", func(t *testing.T) {
		jtiErr := errors.New("jti lookup failed")
		jtiStore := &introspectionJTIErrorStore{
			mockTokenStore: &mockTokenStore{
				client: &store.Client{
					ID:                      "client-1",
					TokenEndpointAuthMethod: "client_secret_post",
					SecretHash:              &secretHash,
				},
			},
			err: jtiErr,
		}
		svc := NewTokenService(jtiStore, &MockJWTSigner{}, "https://issuer.example.com")
		_, err := svc.IntrospectToken(ctx, &IntrospectionRequest{
			Token:        "opaque-token",
			ClientID:     "client-1",
			ClientSecret: "secret",
		})
		assert.ErrorIs(t, err, jtiErr)

		sigErr := errors.New("signature lookup failed")
		sigStore := &introspectionSignatureErrorStore{
			mockTokenStore: &mockTokenStore{
				client: &store.Client{
					ID:                      "client-1",
					TokenEndpointAuthMethod: "client_secret_post",
					SecretHash:              &secretHash,
				},
			},
			err: sigErr,
		}
		svc = NewTokenService(sigStore, &MockJWTSigner{}, "https://issuer.example.com")
		_, err = svc.IntrospectToken(ctx, &IntrospectionRequest{
			Token:        "opaque-token",
			ClientID:     "client-1",
			ClientSecret: "secret",
		})
		assert.ErrorIs(t, err, sigErr)
	})

	t.Run("resolve introspection fallback when signature reader is unavailable", func(t *testing.T) {
		svc := NewTokenService(tokenStoreWithoutReaders{}, &MockJWTSigner{}, "https://issuer.example.com")
		_, err := svc.resolveAccessTokenForIntrospection(ctx, "opaque-token")
		assert.ErrorIs(t, err, store.ErrNotFound)
	})
}

func TestTokenSubjectAndGracePeriodAdditionalBranches(t *testing.T) {
	_, err := resolveTokenSubject(&store.Client{ID: "client-1"}, "   ", "https://issuer.example.com")
	assert.ErrorIs(t, err, ErrInvalidRequest)

	_, err = resolveTokenSubject(nil, "identity-1", "https://issuer.example.com")
	assert.ErrorIs(t, err, ErrInvalidClient)

	_, err = resolveTokenSubject(&store.Client{
		ID:          "client-1",
		SubjectType: "unsupported",
	}, "identity-1", "https://issuer.example.com")
	assert.ErrorIs(t, err, ErrInvalidClient)

	assert.Equal(t, "", resolvePairwiseSector(nil))
	assert.Equal(t, "", normalizeSectorIdentifier("  "))
	assert.Equal(t, "://bad-uri", normalizeSectorIdentifier("://bad-uri"))
	assert.Equal(t, "mailto:team@example.com", normalizeSectorIdentifier("mailto:Team@Example.com"))

	_, err = refreshTokenGracePeriod(nil)
	assert.ErrorIs(t, err, ErrInvalidClient)

	grace, err := refreshTokenGracePeriod(&store.Client{
		ID:       "client-grace",
		Metadata: map[string]string{"refresh_token_grace_period_seconds": "7"},
	})
	require.NoError(t, err)
	assert.Equal(t, 7*time.Second, grace)

	grace, err = refreshTokenGracePeriod(&store.Client{
		ID:       "client-no-grace",
		Metadata: map[string]string{},
	})
	require.NoError(t, err)
	assert.Equal(t, time.Duration(0), grace)
}
