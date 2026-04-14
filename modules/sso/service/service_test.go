package service

import (
	"context"
	"strings"
	"testing"

	"github.com/aegion/aegion/modules/sso/store"
)

func TestStartAndCompleteAuth(t *testing.T) {
	repo := store.New()
	svc := New(repo, []byte("01234567890123456789012345678901"))
	ctx := context.Background()

	_, err := svc.UpsertConnection(ctx, ConnectionUpsertRequest{
		Slug:              "acme",
		DisplayName:       "Acme",
		EntityID:          "urn:acme:test",
		SSOURL:            "https://idp.example.com/sso",
		Domains:           []string{"example.com"},
		DefaultRedirectTo: "/welcome",
		Enabled:           true,
	})
	if err != nil {
		t.Fatalf("upsert connection: %v", err)
	}

	start, err := svc.StartAuth(ctx, "acme", "/after")
	if err != nil {
		t.Fatalf("start auth: %v", err)
	}
	if !strings.Contains(start.RedirectURL, "RelayState=") {
		t.Fatalf("expected relay state in redirect URL, got %q", start.RedirectURL)
	}

	result, err := svc.CompleteAuth(ctx, "acme", start.RelayState, "sub-123", "user@example.com", "User", map[string]interface{}{"department": "eng"})
	if err != nil {
		t.Fatalf("complete auth: %v", err)
	}
	if result.Subject != "sub-123" || result.Email != "user@example.com" || result.RedirectTo != "/after" {
		t.Fatalf("unexpected callback result: %+v", result)
	}
}

func TestGetConnectionForDomain(t *testing.T) {
	repo := store.New()
	svc := New(repo, []byte("01234567890123456789012345678901"))
	ctx := context.Background()
	_, err := svc.UpsertConnection(ctx, ConnectionUpsertRequest{
		Slug:        "acme",
		DisplayName: "Acme",
		EntityID:    "urn:acme:test",
		SSOURL:      "https://idp.example.com/sso",
		Domains:     []string{"example.com"},
		Enabled:     true,
	})
	if err != nil {
		t.Fatalf("upsert connection: %v", err)
	}
	connection, err := svc.GetConnectionForDomain(ctx, "@example.com")
	if err != nil {
		t.Fatalf("get connection by domain: %v", err)
	}
	if connection.Slug != "acme" {
		t.Fatalf("expected acme connection, got %+v", connection)
	}
}
