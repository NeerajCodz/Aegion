package store

import (
	"testing"
	"time"
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

func TestStoreStateLifecycle(t *testing.T) {
	s := New()
	state := AuthState{
		ID:        "state-1",
		Provider:  "google",
		ExpiresAt: time.Now().UTC().Add(time.Minute),
	}
	s.SaveState(state)

	loaded, err := s.ConsumeState("state-1")
	if err != nil {
		t.Fatalf("expected to consume state, got %v", err)
	}
	if loaded.Provider != "google" {
		t.Fatalf("unexpected provider: %s", loaded.Provider)
	}

	if _, err := s.ConsumeState("state-1"); err == nil {
		t.Fatal("expected consumed state to be removed")
	}
}

func TestStoreRejectsExpiredState(t *testing.T) {
	s := New()
	s.SaveState(AuthState{
		ID:        "state-expired",
		Provider:  "github",
		ExpiresAt: time.Now().UTC().Add(-time.Second),
	})

	_, err := s.ConsumeState("state-expired")
	if err == nil {
		t.Fatal("expected error for expired state")
	}
}
