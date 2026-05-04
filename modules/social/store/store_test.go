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

func TestMemoryStoreSaveStatePurgesExpiredEntries(t *testing.T) {
	s := New()
	ctx := context.Background()

	expired := AuthState{ID: "expired", ProviderSlug: "google", ExpiresAt: time.Now().UTC().Add(-time.Minute)}
	fresh := AuthState{ID: "fresh", ProviderSlug: "google", ExpiresAt: time.Now().UTC().Add(time.Minute)}
	if err := s.SaveState(ctx, expired); err != nil {
		t.Fatalf("SaveState(expired) failed: %v", err)
	}
	if err := s.SaveState(ctx, fresh); err != nil {
		t.Fatalf("SaveState(fresh) failed: %v", err)
	}

	if _, err := s.ConsumeState(ctx, "expired"); err != ErrStateNotFound {
		t.Fatalf("expected expired state to be purged, got %v", err)
	}
	if _, err := s.ConsumeState(ctx, "fresh"); err != nil {
		t.Fatalf("expected fresh state available, got %v", err)
	}
}

func TestMemoryStoreSaveStateEnforcesStateCapacity(t *testing.T) {
	s := New()
	ctx := context.Background()
	base := time.Now().UTC().Add(time.Minute)

	for i := 0; i < maxInMemoryAuthStates; i++ {
		if err := s.SaveState(ctx, AuthState{
			ID:           "state-" + time.Unix(int64(i), 0).Format("150405") + "-" + uuid.NewString(),
			ProviderSlug: "google",
			ExpiresAt:    base.Add(time.Duration(i) * time.Second),
		}); err != nil {
			t.Fatalf("SaveState(seed %d) failed: %v", i, err)
		}
	}

	oldest := AuthState{ID: "oldest", ProviderSlug: "google", ExpiresAt: base.Add(-time.Second)}
	if err := s.SaveState(ctx, oldest); err != nil {
		t.Fatalf("SaveState(oldest) failed: %v", err)
	}
	latest := AuthState{ID: "latest", ProviderSlug: "google", ExpiresAt: base.Add(time.Hour)}
	if err := s.SaveState(ctx, latest); err != nil {
		t.Fatalf("SaveState(latest) failed: %v", err)
	}

	if _, err := s.ConsumeState(ctx, "oldest"); err != ErrStateNotFound {
		t.Fatalf("expected oldest state to be evicted, got %v", err)
	}
	if _, err := s.ConsumeState(ctx, "latest"); err != nil {
		t.Fatalf("expected latest state to remain, got %v", err)
	}
}
