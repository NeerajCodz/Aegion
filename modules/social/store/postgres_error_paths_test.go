package store

import (
	"context"
	"errors"
	"testing"
	"time"

	platformcrypto "github.com/aegion/aegion/internal/platform/crypto"
	"github.com/jackc/pgx/v5/pgxpool"
)

func newUnreachablePostgresPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	pool, err := pgxpool.New(context.Background(), "postgres://postgres:postgres@127.0.0.1:1/postgres?sslmode=disable&connect_timeout=1")
	if err != nil {
		t.Fatalf("create unreachable pool: %v", err)
	}
	t.Cleanup(func() { pool.Close() })
	return pool
}

func TestPostgresStoreErrorPathsOnUnreachableDB(t *testing.T) {
	pool := newUnreachablePostgresPool(t)
	validKey := make([]byte, platformcrypto.KeySize)

	if _, err := NewPostgres(nil, validKey); err == nil {
		t.Fatal("NewPostgres(nil pool) expected error")
	}
	if _, err := NewPostgres(pool, []byte("short")); !errors.Is(err, platformcrypto.ErrInvalidKeyLength) {
		t.Fatalf("NewPostgres(short key) error = %v, want %v", err, platformcrypto.ErrInvalidKeyLength)
	}

	store, err := NewPostgres(pool, validKey)
	if err != nil {
		t.Fatalf("NewPostgres(unreachable pool) error = %v", err)
	}

	ctx := context.Background()
	if _, err := store.ListProviders(ctx, true); err == nil {
		t.Fatal("ListProviders expected error with unreachable DB")
	}
	if _, err := store.GetProviderBySlug(ctx, "google"); err == nil {
		t.Fatal("GetProviderBySlug expected error with unreachable DB")
	}

	if _, err := store.UpsertProvider(ctx, Provider{}); !errors.Is(err, ErrProviderNotFound) {
		t.Fatalf("UpsertProvider(empty slug) error = %v, want %v", err, ErrProviderNotFound)
	}
	if _, err := store.UpsertProvider(ctx, Provider{
		Slug:               "google",
		DisplayName:        "Google",
		Protocol:           ProtocolOIDC,
		ClaimMapping:       ClaimMapping{Subject: "sub"},
		PKCEMethod:         PKCES256,
		AuthStyle:          AuthStyleClientSecretPost,
		ClaimSource:        ClaimSourceUserInfo,
		Enabled:            true,
		TrustEmailVerified: true,
	}); err == nil {
		t.Fatal("UpsertProvider(valid slug) expected error with unreachable DB")
	}
	if err := store.DeleteProvider(ctx, "google"); err == nil {
		t.Fatal("DeleteProvider expected error with unreachable DB")
	}

	if err := store.SaveState(ctx, AuthState{
		ID:           "state-1",
		ProviderSlug: "google",
		RedirectTo:   "/app",
		Nonce:        "nonce",
		PKCEVerifier: "verifier",
		ExpiresAt:    time.Now().UTC().Add(5 * time.Minute),
	}); err == nil {
		t.Fatal("SaveState expected error with unreachable DB")
	}
	if _, err := store.ConsumeState(ctx, "state-1"); err == nil {
		t.Fatal("ConsumeState expected error with unreachable DB")
	}

	if _, err := store.ResolveIdentity(ctx, Provider{Slug: "google"}, SocialProfile{
		Provider:     "google",
		ProviderUser: "user-1",
		Email:        "user@example.com",
		RawClaims:    map[string]any{"sub": "user-1"},
	}); err == nil {
		t.Fatal("ResolveIdentity expected error with unreachable DB")
	}
}
