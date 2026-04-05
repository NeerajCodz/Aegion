package grpc

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/aegion/aegion/modules/oauth2/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockTokenStore struct {
	token         *store.AccessToken
	getErr        error
	revokeErr     error
	invalidateErr error
	lastGet       string
	lastRevoke    string
	lastFamily    string
	invalidated   int64
}

func (m *mockTokenStore) GetAccessToken(ctx context.Context, jti string) (*store.AccessToken, error) {
	m.lastGet = jti
	if m.getErr != nil {
		return nil, m.getErr
	}
	if m.token != nil {
		return m.token, nil
	}
	return nil, store.ErrNotFound
}

func (m *mockTokenStore) RevokeAccessToken(ctx context.Context, jti string) error {
	m.lastRevoke = jti
	return m.revokeErr
}

func (m *mockTokenStore) InvalidateRefreshTokenFamily(ctx context.Context, familyID string) (int64, error) {
	m.lastFamily = familyID
	if m.invalidateErr != nil {
		return 0, m.invalidateErr
	}
	return m.invalidated, nil
}

func TestNewServer(t *testing.T) {
	st := &mockTokenStore{}
	s := NewServer(st)
	require.NotNil(t, s)
}

func TestServer_Introspect(t *testing.T) {
	ctx := context.Background()

	t.Run("validation and not found", func(t *testing.T) {
		s := NewServer(&mockTokenStore{})
		_, err := s.Introspect(ctx, nil)
		assert.ErrorContains(t, err, "token is required")

		resp, err := s.Introspect(ctx, &IntrospectTokenRequest{Token: "missing"})
		require.NoError(t, err)
		assert.False(t, resp.Active)
	})

	t.Run("store error", func(t *testing.T) {
		st := &mockTokenStore{getErr: errors.New("db down")}
		s := NewServer(st)
		_, err := s.Introspect(ctx, &IntrospectTokenRequest{Token: "at-1"})
		assert.ErrorContains(t, err, "db down")
	})

	t.Run("revoked expired and active", func(t *testing.T) {
		st := &mockTokenStore{
			token: &store.AccessToken{
				JTI:        "at-1",
				ClientID:   "client-1",
				IdentityID: "identity-1",
				Scopes:     []string{"openid"},
				Audience:   []string{"api"},
				ExpiresAt:  time.Now().UTC().Add(time.Hour),
			},
		}
		s := NewServer(st)
		resp, err := s.Introspect(ctx, &IntrospectTokenRequest{Token: "at-1"})
		require.NoError(t, err)
		assert.True(t, resp.Active)
		assert.Equal(t, "client-1", resp.ClientID)
		assert.Equal(t, "identity-1", resp.IdentityID)

		st.token.Revoked = true
		resp, err = s.Introspect(ctx, &IntrospectTokenRequest{Token: "at-1"})
		require.NoError(t, err)
		assert.False(t, resp.Active)

		st.token.Revoked = false
		st.token.ExpiresAt = time.Now().UTC().Add(-time.Second)
		resp, err = s.Introspect(ctx, &IntrospectTokenRequest{Token: "at-1"})
		require.NoError(t, err)
		assert.False(t, resp.Active)
	})
}

func TestServer_Revoke(t *testing.T) {
	ctx := context.Background()

	t.Run("validation and not found", func(t *testing.T) {
		s := NewServer(&mockTokenStore{})
		_, err := s.Revoke(ctx, nil)
		assert.ErrorContains(t, err, "token is required")

		st := &mockTokenStore{revokeErr: store.ErrNotFound}
		s = NewServer(st)
		resp, err := s.Revoke(ctx, &RevokeTokenRequest{Token: "missing"})
		require.NoError(t, err)
		assert.False(t, resp.Revoked)
	})

	t.Run("success and error", func(t *testing.T) {
		st := &mockTokenStore{}
		s := NewServer(st)
		resp, err := s.Revoke(ctx, &RevokeTokenRequest{Token: "at-1"})
		require.NoError(t, err)
		assert.True(t, resp.Revoked)
		assert.Equal(t, "at-1", st.lastRevoke)

		st.revokeErr = errors.New("write failed")
		_, err = s.Revoke(ctx, &RevokeTokenRequest{Token: "at-1"})
		assert.ErrorContains(t, err, "write failed")
	})
}

func TestServer_InvalidateFamily(t *testing.T) {
	ctx := context.Background()

	t.Run("validation", func(t *testing.T) {
		s := NewServer(&mockTokenStore{})
		_, err := s.InvalidateFamily(ctx, nil)
		assert.ErrorContains(t, err, "family_id is required")
	})

	t.Run("success and store error", func(t *testing.T) {
		st := &mockTokenStore{invalidated: 3}
		s := NewServer(st)
		resp, err := s.InvalidateFamily(ctx, &InvalidateFamilyRequest{FamilyID: "fam-1"})
		require.NoError(t, err)
		assert.Equal(t, int64(3), resp.Invalidated)
		assert.Equal(t, "fam-1", st.lastFamily)

		st.invalidateErr = errors.New("db unavailable")
		_, err = s.InvalidateFamily(ctx, &InvalidateFamilyRequest{FamilyID: "fam-1"})
		assert.ErrorContains(t, err, "db unavailable")
	})
}
