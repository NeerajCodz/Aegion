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

func TestChallengeLifecycle(t *testing.T) {
	s := New()
	s.SaveChallenge(Challenge{
		ID:         "challenge-1",
		IdentityID: "identity-1",
		Purpose:    "registration",
		ExpiresAt:  time.Now().UTC().Add(time.Minute),
	})
	challenge, err := s.ConsumeChallenge("challenge-1")
	if err != nil {
		t.Fatalf("expected challenge consume success, got %v", err)
	}
	if challenge.IdentityID != "identity-1" {
		t.Fatalf("unexpected identity id: %s", challenge.IdentityID)
	}
}

func TestCredentialLifecycle(t *testing.T) {
	s := New()
	s.CreateCredential(Credential{
		ID:         "cred-1",
		IdentityID: "identity-1",
		PublicKey:  "pk",
		CreatedAt:  time.Now().UTC(),
	})
	credential, err := s.GetCredential("cred-1")
	if err != nil {
		t.Fatalf("expected credential retrieval success, got %v", err)
	}
	if credential.PublicKey != "pk" {
		t.Fatalf("unexpected public key: %s", credential.PublicKey)
	}
	list := s.ListCredentialsByIdentity("identity-1")
	if len(list) != 1 {
		t.Fatalf("expected one credential, got %d", len(list))
	}
	if err := s.UpdateCredentialSignCount("cred-1", 7); err != nil {
		t.Fatalf("expected sign count update success, got %v", err)
	}
	updated, err := s.GetCredential("cred-1")
	if err != nil {
		t.Fatalf("expected credential retrieval success, got %v", err)
	}
	if updated.SignCount != 7 {
		t.Fatalf("unexpected sign count: %d", updated.SignCount)
	}
}
