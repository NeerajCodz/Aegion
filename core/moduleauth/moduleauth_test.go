package moduleauth

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type memoryStore struct {
	mu          sync.RWMutex
	credentials map[string]Credential
}

func (s *memoryStore) Credential(_ context.Context, moduleID string) (Credential, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	credential, ok := s.credentials[moduleID]
	if !ok {
		return Credential{}, errors.New("credential not found")
	}
	credential.Permissions = append([]string(nil), credential.Permissions...)
	credential.Audiences = append([]string(nil), credential.Audiences...)
	return credential, nil
}

func TestManagerExchangesScopedCredentialAndRevokesImmediately(t *testing.T) {
	raw, hash, err := NewCredential()
	require.NoError(t, err)
	store := &memoryStore{credentials: map[string]Credential{
		"analytics": {
			ID:          "credential-analytics-1",
			ModuleID:    "analytics",
			SecretHash:  hash,
			Permissions: []string{"registry:register", "registry:heartbeat"},
			Audiences:   []string{"core.registry"},
			Enabled:     true,
		},
	}}
	manager, err := NewManager(store, []byte("01234567890123456789012345678901"), time.Minute)
	require.NoError(t, err)

	token, claims, err := manager.Exchange(context.Background(), "analytics", raw, "core.registry", []string{"registry:register"})
	require.NoError(t, err)
	require.Equal(t, "analytics", claims.ModuleID)
	require.Equal(t, "core.registry", claims.Audience)
	require.NotEmpty(t, token)

	validated, err := manager.Validate(context.Background(), token, "core.registry", "registry:register")
	require.NoError(t, err)
	require.Equal(t, claims.TokenID, validated.TokenID)
	_, err = manager.Validate(context.Background(), token, "core.registry", "registry:heartbeat")
	require.ErrorIs(t, err, ErrTokenDenied)

	store.mu.Lock()
	credential := store.credentials["analytics"]
	credential.Enabled = false
	store.credentials["analytics"] = credential
	store.mu.Unlock()
	_, err = manager.Validate(context.Background(), token, "core.registry", "registry:register")
	require.ErrorIs(t, err, ErrCredentialRevoked)
}

func TestManagerRejectsUnauthorizedExchangeAndExpiredToken(t *testing.T) {
	raw, hash, err := NewCredential()
	require.NoError(t, err)
	store := &memoryStore{credentials: map[string]Credential{
		"proxy": {
			ID:          "credential-proxy-1",
			ModuleID:    "proxy",
			SecretHash:  hash,
			Permissions: []string{"registry:register"},
			Audiences:   []string{"core.registry"},
			Enabled:     true,
		},
	}}
	manager, err := NewManager(store, []byte("01234567890123456789012345678901"), time.Second)
	require.NoError(t, err)
	clock := time.Date(2026, time.August, 10, 0, 0, 0, 0, time.UTC)
	manager.now = func() time.Time { return clock }

	_, _, err = manager.Exchange(context.Background(), "proxy", raw, "outside.service", []string{"registry:register"})
	require.ErrorIs(t, err, ErrTokenDenied)
	_, _, err = manager.Exchange(context.Background(), "proxy", raw, "core.registry", []string{"registry:heartbeat"})
	require.ErrorIs(t, err, ErrTokenDenied)

	token, _, err := manager.Exchange(context.Background(), "proxy", raw, "core.registry", []string{"registry:register"})
	require.NoError(t, err)
	clock = clock.Add(2 * time.Second)
	_, err = manager.Validate(context.Background(), token, "core.registry", "registry:register")
	require.ErrorIs(t, err, ErrTokenExpired)
}
