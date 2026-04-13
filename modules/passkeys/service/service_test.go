package service

import (
	"testing"
	"time"

	"github.com/aegion/aegion/modules/passkeys/store"
)

func TestRegistrationAndAuthenticationFlow(t *testing.T) {
	svc := New(store.New(), Config{
		RPID:         "example.com",
		RPOrigin:     "https://example.com",
		ChallengeTTL: time.Minute,
	})

	regStart, err := svc.BeginRegistration("identity-1")
	if err != nil {
		t.Fatalf("begin registration failed: %v", err)
	}
	if regStart.Challenge == "" {
		t.Fatal("expected registration challenge")
	}

	if err := svc.FinishRegistration(&RegistrationFinishRequest{
		IdentityID:   "identity-1",
		Challenge:    regStart.Challenge,
		CredentialID: "cred-1",
		PublicKey:    "public-key",
	}); err != nil {
		t.Fatalf("finish registration failed: %v", err)
	}

	authStart, err := svc.BeginAuthentication("identity-1")
	if err != nil {
		t.Fatalf("begin auth failed: %v", err)
	}
	if len(authStart.AllowedCredentialIDs) != 1 || authStart.AllowedCredentialIDs[0] != "cred-1" {
		t.Fatalf("unexpected allowed credentials: %#v", authStart.AllowedCredentialIDs)
	}

	if err := svc.FinishAuthentication(&AuthenticationFinishRequest{
		IdentityID:   "identity-1",
		Challenge:    authStart.Challenge,
		CredentialID: "cred-1",
	}); err != nil {
		t.Fatalf("finish auth failed: %v", err)
	}
}
