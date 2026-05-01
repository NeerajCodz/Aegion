package store

import (
	"context"
	"testing"
)

func TestMemoryStoreConnectionLifecycle(t *testing.T) {
	repo := New()
	ctx := context.Background()
	_, err := repo.UpsertConnection(ctx, Connection{
		Slug:        "acme",
		DisplayName: "Acme",
		EntityID:    "urn:acme",
		SSOURL:      "https://idp.example.com",
		Domains:     []string{"example.com"},
		Enabled:     true,
	})
	if err != nil {
		t.Fatalf("upsert connection: %v", err)
	}
	connection, err := repo.GetConnectionByDomain(ctx, "example.com")
	if err != nil {
		t.Fatalf("get by domain: %v", err)
	}
	if connection.Slug != "acme" {
		t.Fatalf("expected acme, got %+v", connection)
	}
}
