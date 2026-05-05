package store

import (
	"errors"
	"testing"
	"time"
)

func TestAdditionalChallengeAndCredentialBranches(t *testing.T) {
	s := New()
	now := time.Now().UTC().Round(0)

	if _, err := s.ConsumeChallenge("missing"); !errors.Is(err, ErrChallengeNotFound) {
		t.Fatalf("ConsumeChallenge(missing) error = %v, want %v", err, ErrChallengeNotFound)
	}

	s.SaveChallenge(Challenge{
		ID:         "expired",
		IdentityID: "identity-1",
		Purpose:    "auth",
		ExpiresAt:  now.Add(-time.Second),
	})
	if _, err := s.ConsumeChallenge("expired"); !errors.Is(err, ErrChallengeExpired) {
		t.Fatalf("ConsumeChallenge(expired) error = %v, want %v", err, ErrChallengeExpired)
	}

	if _, err := s.GetCredential("missing"); !errors.Is(err, ErrCredentialNotFound) {
		t.Fatalf("GetCredential(missing) error = %v, want %v", err, ErrCredentialNotFound)
	}
	if err := s.UpdateCredentialSignCount("missing", 2); !errors.Is(err, ErrCredentialNotFound) {
		t.Fatalf("UpdateCredentialSignCount(missing) error = %v, want %v", err, ErrCredentialNotFound)
	}

	s.CreateCredential(Credential{ID: "cred-1", IdentityID: "identity-1", PublicKey: "pk1", CreatedAt: now})
	s.CreateCredential(Credential{ID: "cred-2", IdentityID: "identity-2", PublicKey: "pk2", CreatedAt: now})
	list := s.ListCredentialsByIdentity("identity-1")
	if len(list) != 1 || list[0].ID != "cred-1" {
		t.Fatalf("ListCredentialsByIdentity(identity-1) = %#v, want only cred-1", list)
	}
}
