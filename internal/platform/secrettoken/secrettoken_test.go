package secrettoken

import "testing"

func TestHashValidateAndPrefix(t *testing.T) {
	token := "aegion_test_token_123"
	hash := Hash(token)

	if hash == "" {
		t.Fatalf("expected non-empty hash")
	}
	if !Validate(token, hash) {
		t.Fatalf("expected token to validate against its hash")
	}
	if Validate(token+"x", hash) {
		t.Fatalf("expected mismatched token validation to fail")
	}

	if got := Prefix(token, 6); got != "aegion" {
		t.Fatalf("expected prefix aegion, got %q", got)
	}
	if got := Prefix(token, 0); got != "" {
		t.Fatalf("expected empty prefix for zero length, got %q", got)
	}
}
