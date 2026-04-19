package store

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func newUnreachablePasskeysPostgresPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	pool, err := pgxpool.New(context.Background(), "postgres://postgres:postgres@127.0.0.1:1/postgres?sslmode=disable&connect_timeout=1")
	if err != nil {
		t.Fatalf("create unreachable pool: %v", err)
	}
	t.Cleanup(func() { pool.Close() })
	return pool
}

func TestPostgresStoreErrorPathsOnUnreachableDB(t *testing.T) {
	if _, err := NewPostgres(nil); err == nil {
		t.Fatal("NewPostgres(nil) expected error")
	}

	store, err := NewPostgres(newUnreachablePasskeysPostgresPool(t))
	if err != nil {
		t.Fatalf("NewPostgres(unreachable pool) error = %v", err)
	}

	now := time.Now().UTC()

	store.SaveChallenge(Challenge{
		ID:         "challenge-1",
		IdentityID: "identity-1",
		Purpose:    "auth",
		ExpiresAt:  now.Add(time.Minute),
	})

	if _, err := store.ConsumeChallenge("challenge-1"); err == nil {
		t.Fatal("ConsumeChallenge expected error with unreachable DB")
	}

	store.UpsertCredential(Credential{
		ID:         "cred-1",
		IdentityID: "identity-1",
		PublicKey:  "pubkey",
		CreatedAt:  now,
	})

	if _, err := store.GetCredential("cred-1"); err == nil {
		t.Fatal("GetCredential expected error with unreachable DB")
	}
	if err := store.UpdateCredentialSignCount("cred-1", 3); err == nil {
		t.Fatal("UpdateCredentialSignCount expected error with unreachable DB")
	}

	credentials := store.ListCredentialsByIdentity("identity-1")
	if len(credentials) != 0 {
		t.Fatalf("ListCredentialsByIdentity expected empty result on query error, got %#v", credentials)
	}
}
