package store

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestNewAndOAuth2Accessor(t *testing.T) {
	first := New(nil)
	second := New(nil)
	if first == nil || second == nil {
		t.Fatal("New returned nil instance")
	}
	if first == second {
		t.Fatal("New returned shared instance")
	}
	if first.OAuth2() != nil {
		t.Fatalf("expected nil oauth2 store when initialized without pool")
	}

	pool, err := pgxpool.New(context.Background(), "postgres://postgres:postgres@127.0.0.1:1/postgres?sslmode=disable&connect_timeout=1")
	if err != nil {
		t.Fatalf("create test pool: %v", err)
	}
	t.Cleanup(func() { pool.Close() })

	withPool := New(pool)
	if withPool == nil || withPool.OAuth2() == nil {
		t.Fatal("expected oauth2 store when initialized with pool")
	}

	var nilStore *Store
	if nilStore.OAuth2() != nil {
		t.Fatal("expected nil receiver OAuth2 accessor to return nil")
	}
}
