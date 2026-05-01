package service

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"fmt"
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

	privateKey, publicKeyPEM := mustGenerateCredentialKey(t)

	if err := svc.FinishRegistration(&RegistrationFinishRequest{
		IdentityID:   "identity-1",
		Challenge:    regStart.Challenge,
		CredentialID: "cred-1",
		PublicKey:    publicKeyPEM,
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

	signature := mustSignAssertion(t, privateKey, "identity-1", "cred-1", authStart.Challenge)
	if err := svc.FinishAuthentication(&AuthenticationFinishRequest{
		IdentityID:   "identity-1",
		Challenge:    authStart.Challenge,
		CredentialID: "cred-1",
		Signature:    signature,
		SignCount:    1,
	}); err != nil {
		t.Fatalf("finish auth failed: %v", err)
	}
}

func mustGenerateCredentialKey(t *testing.T) (*ecdsa.PrivateKey, string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	der, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		t.Fatalf("marshal pubkey: %v", err)
	}
	pemEncoded := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der})
	return key, string(pemEncoded)
}

func mustSignAssertion(t *testing.T, key *ecdsa.PrivateKey, identityID, credentialID, challenge string) string {
	t.Helper()
	message := fmt.Sprintf("%s:%s:%s", identityID, credentialID, challenge)
	digest := sha256.Sum256([]byte(message))
	signature, err := ecdsa.SignASN1(rand.Reader, key, digest[:])
	if err != nil {
		t.Fatalf("sign assertion: %v", err)
	}
	return base64.RawURLEncoding.EncodeToString(signature)
}
