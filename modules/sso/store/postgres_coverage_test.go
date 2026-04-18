package store

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func newUnreachableSSOPostgresPool(t *testing.T) *pgxpool.Pool {
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

	store, err := NewPostgres(newUnreachableSSOPostgresPool(t))
	if err != nil {
		t.Fatalf("NewPostgres(unreachable pool) error = %v", err)
	}

	ctx := context.Background()
	if _, err := store.ListConnections(ctx, false); err == nil {
		t.Fatal("ListConnections expected error with unreachable DB")
	}
	if _, err := store.ListConnections(ctx, true); err == nil {
		t.Fatal("ListConnections(includeDisabled=true) expected error with unreachable DB")
	}
	if _, err := store.GetConnectionBySlug(ctx, "acme"); err == nil {
		t.Fatal("GetConnectionBySlug expected error with unreachable DB")
	}
	if _, err := store.GetConnectionByDomain(ctx, "example.com"); err == nil {
		t.Fatal("GetConnectionByDomain expected error with unreachable DB")
	}
	if _, err := store.UpsertConnection(ctx, Connection{
		Slug:              "acme",
		DisplayName:       "Acme",
		EntityID:          "urn:test:idp",
		SSOURL:            "https://idp.example.com/sso",
		CertificatePEM:    "cert",
		MetadataURL:       "https://idp.example.com/metadata",
		Domains:           []string{"example.com"},
		AttributeMapping:  AttributeMapping{Subject: "sub", Email: "email", DisplayName: "name"},
		JITProvisioning:   true,
		DefaultRedirectTo: "/dashboard",
		ExtraAuthnContext: map[string]string{"prompt": "login"},
		Enabled:           true,
		CreatedAt:         time.Now().UTC(),
		UpdatedAt:         time.Now().UTC(),
	}); err == nil {
		t.Fatal("UpsertConnection expected error with unreachable DB")
	}
	if err := store.DeleteConnection(ctx, "acme"); err == nil {
		t.Fatal("DeleteConnection expected error with unreachable DB")
	}
}

func TestConnectionSanitizedReturnsCopy(t *testing.T) {
	in := Connection{Slug: "acme", DisplayName: "Acme"}
	got := in.Sanitized()
	if got.Slug != "acme" || got.DisplayName != "Acme" {
		t.Fatalf("Sanitized() returned unexpected value: %+v", got)
	}
}
