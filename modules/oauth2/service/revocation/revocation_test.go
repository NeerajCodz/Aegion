package revocation

import (
	"context"
	"encoding/base64"
	"errors"
	"testing"

	bcrypt "github.com/aegion/aegion/internal/platform/bcryptcompat"
	"github.com/aegion/aegion/modules/oauth2/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockRevocationStore struct {
	client              *store.Client
	refreshToken        *store.RefreshToken
	getClientErr        error
	revokeAccessErr     error
	getRefreshErr       error
	invalidateErr       error
	lastRevokedJTI      string
	lastInvalidatedFam  string
	invalidateCallCount int
}

func (m *mockRevocationStore) GetClient(ctx context.Context, id string) (*store.Client, error) {
	if m.getClientErr != nil {
		return nil, m.getClientErr
	}
	if m.client != nil {
		return m.client, nil
	}
	return nil, store.ErrNotFound
}

func (m *mockRevocationStore) RevokeAccessToken(ctx context.Context, jti string) error {
	m.lastRevokedJTI = jti
	return m.revokeAccessErr
}

func (m *mockRevocationStore) GetRefreshToken(ctx context.Context, id string) (*store.RefreshToken, error) {
	if m.getRefreshErr != nil {
		return nil, m.getRefreshErr
	}
	if m.refreshToken != nil {
		return m.refreshToken, nil
	}
	return nil, store.ErrNotFound
}

func (m *mockRevocationStore) InvalidateRefreshTokenFamily(ctx context.Context, familyID string) (int64, error) {
	m.lastInvalidatedFam = familyID
	m.invalidateCallCount++
	if m.invalidateErr != nil {
		return 0, m.invalidateErr
	}
	return 1, nil
}

func TestRevocationService_RevokeToken(t *testing.T) {
	ctx := context.Background()

	t.Run("invalid client auth", func(t *testing.T) {
		svc := NewRevocationService(&mockRevocationStore{})

		err := svc.RevokeToken(ctx, &RevocationRequest{Token: "x"})
		assert.ErrorIs(t, err, ErrInvalidClient)

		st := &mockRevocationStore{getClientErr: errors.New("missing")}
		svc = NewRevocationService(st)
		err = svc.RevokeToken(ctx, &RevocationRequest{
			ClientID: "client-1",
			Token:    "x",
		})
		assert.ErrorIs(t, err, ErrInvalidClient)

		st = &mockRevocationStore{
			client: &store.Client{
				ID:                      "client-1",
				TokenEndpointAuthMethod: "client_secret_post",
				SecretHash:              ptrString("expected-secret"),
			},
		}
		svc = NewRevocationService(st)
		err = svc.RevokeToken(ctx, &RevocationRequest{
			ClientID: "client-1",
			Token:    "x",
		})
		assert.ErrorIs(t, err, ErrInvalidClient)

		err = svc.RevokeToken(ctx, &RevocationRequest{
			ClientID:     "client-1",
			ClientSecret: "wrong-secret",
			Token:        "x",
		})
		assert.ErrorIs(t, err, ErrInvalidClient)

		st.client.TokenEndpointAuthMethod = "client_secret_jwt"
		err = svc.RevokeToken(ctx, &RevocationRequest{
			ClientID:     "client-1",
			ClientSecret: "expected-secret",
			Token:        "x",
		})
		assert.ErrorIs(t, err, ErrInvalidClient)
	})

	t.Run("access token revoke success", func(t *testing.T) {
		st := &mockRevocationStore{
			client: &store.Client{
				ID:                      "client-1",
				TokenEndpointAuthMethod: "none",
			},
		}
		svc := NewRevocationService(st)
		err := svc.RevokeToken(ctx, &RevocationRequest{
			ClientID: "client-1",
			Token:    "at-jti",
		})
		require.NoError(t, err)
		assert.Equal(t, "at-jti", st.lastRevokedJTI)
	})

	t.Run("refresh token family invalidation", func(t *testing.T) {
		st := &mockRevocationStore{
			client: &store.Client{
				ID:                      "client-1",
				TokenEndpointAuthMethod: "none",
			},
			revokeAccessErr: errors.New("not access"),
			refreshToken: &store.RefreshToken{
				ID:       "rt-1",
				ClientID: "client-1",
				FamilyID: "family-1",
			},
		}
		svc := NewRevocationService(st)
		err := svc.RevokeToken(ctx, &RevocationRequest{
			ClientID:      "client-1",
			Token:         "rt-1",
			TokenTypeHint: "refresh_token",
		})
		require.NoError(t, err)
		assert.Equal(t, 1, st.invalidateCallCount)
		assert.Equal(t, "family-1", st.lastInvalidatedFam)
	})

	t.Run("token belongs to different client or unknown token still succeeds", func(t *testing.T) {
		st := &mockRevocationStore{
			client: &store.Client{
				ID:                      "client-1",
				TokenEndpointAuthMethod: "none",
			},
			revokeAccessErr: errors.New("not access"),
			refreshToken: &store.RefreshToken{
				ID:       "rt-2",
				ClientID: "other-client",
				FamilyID: "family-2",
			},
		}
		svc := NewRevocationService(st)
		err := svc.RevokeToken(ctx, &RevocationRequest{
			ClientID: "client-1",
			Token:    "rt-2",
		})
		require.NoError(t, err)
		assert.Equal(t, 0, st.invalidateCallCount)

		st.refreshToken = nil
		st.getRefreshErr = store.ErrNotFound
		err = svc.RevokeToken(ctx, &RevocationRequest{
			ClientID: "client-1",
			Token:    "unknown",
		})
		require.NoError(t, err)
	})
}

func TestRevocationHelpers(t *testing.T) {
	assert.True(t, authenticateClientSecret("secret", "secret"))
	assert.False(t, authenticateClientSecret("secret", "nope"))

	hash, err := bcrypt.GenerateFromPassword([]byte("super-secret"), bcrypt.DefaultCost)
	require.NoError(t, err)
	assert.True(t, authenticateClientSecret(string(hash), "super-secret"))
	assert.False(t, authenticateClientSecret(string(hash), "invalid"))

	token, err := ExtractTokenFromHeader("Bearer abc.def")
	require.NoError(t, err)
	assert.Equal(t, "abc.def", token)

	_, err = ExtractTokenFromHeader("")
	assert.ErrorIs(t, err, ErrInvalidToken)
	_, err = ExtractTokenFromHeader("Basic abc")
	assert.ErrorIs(t, err, ErrInvalidToken)
	_, err = ExtractTokenFromHeader("Bearer   ")
	assert.ErrorIs(t, err, ErrInvalidToken)

	encoded := base64.StdEncoding.EncodeToString([]byte("client:secret"))
	clientID, clientSecret, err := ExtractClientCredentials("Basic " + encoded)
	require.NoError(t, err)
	assert.Equal(t, "client", clientID)
	assert.Equal(t, "secret", clientSecret)

	_, _, err = ExtractClientCredentials("")
	assert.ErrorIs(t, err, ErrInvalidClient)
	_, _, err = ExtractClientCredentials("Bearer token")
	assert.ErrorIs(t, err, ErrInvalidClient)
	_, _, err = ExtractClientCredentials("Basic bad%%%")
	assert.ErrorIs(t, err, ErrInvalidClient)
	badFmt := base64.StdEncoding.EncodeToString([]byte("nocolon"))
	_, _, err = ExtractClientCredentials("Basic " + badFmt)
	assert.ErrorIs(t, err, ErrInvalidClient)
}

func ptrString(v string) *string {
	return &v
}
