package store

import "testing"

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
}
