package store

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestNewReturnsDistinctInstances(t *testing.T) {
	first := New()
	second := New()
	if first == nil || second == nil {
		t.Fatal("New returned nil instance")
	}
	if first == second {
		t.Fatal("New returned shared instance")
	}
}

func TestMemoryStoreStateLifecycle(t *testing.T) {
	s := New()
	ctx := context.Background()
	state := AuthState{
		ID:           "state-1",
		ProviderSlug: "google",
		ExpiresAt:    time.Now().UTC().Add(time.Minute),
	}
	if err := s.SaveState(ctx, state); err != nil {
		t.Fatalf("SaveState failed: %v", err)
	}

	loaded, err := s.ConsumeState(ctx, "state-1")
	if err != nil {
		t.Fatalf("expected to consume state, got %v", err)
	}
	if loaded.ProviderSlug != "google" {
		t.Fatalf("unexpected provider slug: %s", loaded.ProviderSlug)
	}

	if _, err := s.ConsumeState(ctx, "state-1"); err == nil {
		t.Fatal("expected consumed state to be removed")
	}
}

func TestMemoryStoreRejectsExpiredState(t *testing.T) {
	s := New()
	ctx := context.Background()
	if err := s.SaveState(ctx, AuthState{
		ID:           "expired",
		ProviderSlug: "github",
		ExpiresAt:    time.Now().UTC().Add(-time.Second),
	}); err != nil {
		t.Fatalf("SaveState failed: %v", err)
	}

	if _, err := s.ConsumeState(ctx, "expired"); err != ErrStateExpired {
		t.Fatalf("expected ErrStateExpired, got %v", err)
	}
}

func TestMemoryStoreProviderAndIdentityLifecycle(t *testing.T) {
	s := New()
	ctx := context.Background()

	saved, err := s.UpsertProvider(ctx, Provider{
		Slug:        "google",
		DisplayName: "Google",
		ClientID:    "client-1",
		RedirectURI: "https://app.example.com/callback",
		Enabled:     true,
	})
	if err != nil {
		t.Fatalf("UpsertProvider failed: %v", err)
	}
	if saved.ID == uuid.Nil {
		t.Fatal("expected provider id to be assigned")
	}

	providers, err := s.ListProviders(ctx, false)
	if err != nil {
		t.Fatalf("ListProviders failed: %v", err)
	}
	if len(providers) != 1 || providers[0].Slug != "google" {
		t.Fatalf("unexpected providers: %#v", providers)
	}

	link, err := s.ResolveIdentity(ctx, providers[0], SocialProfile{
		ProviderUser: "sub-1",
		Email:        "user@example.com",
	})
	if err != nil {
		t.Fatalf("ResolveIdentity failed: %v", err)
	}
	if !link.Created || !link.Linked {
		t.Fatalf("expected created linked identity, got %#v", link)
	}
}
