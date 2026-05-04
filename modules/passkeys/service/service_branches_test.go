package service

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"testing"
	"time"

	"github.com/aegion/aegion/modules/passkeys/store"
)

type mockChallengeStore struct {
	consumeChallengeResp store.Challenge
	consumeChallengeErr  error
	getCredentialResp    store.Credential
	getCredentialErr     error
	listCredentialsResp  []store.Credential
	updateSignCountErr   error
	createCredentialErr  error

	savedChallenges []store.Challenge
	upserted        []store.Credential
}

func (m *mockChallengeStore) SaveChallenge(challenge store.Challenge) {
	m.savedChallenges = append(m.savedChallenges, challenge)
}

func (m *mockChallengeStore) ConsumeChallenge(challengeID string) (store.Challenge, error) {
	if m.consumeChallengeErr != nil {
		return store.Challenge{}, m.consumeChallengeErr
	}
	return m.consumeChallengeResp, nil
}

func (m *mockChallengeStore) CreateCredential(credential store.Credential) error {
	m.upserted = append(m.upserted, credential)
	return m.createCredentialErr
}

func (m *mockChallengeStore) GetCredential(credentialID string) (store.Credential, error) {
	if m.getCredentialErr != nil {
		return store.Credential{}, m.getCredentialErr
	}
	return m.getCredentialResp, nil
}

func (m *mockChallengeStore) ListCredentialsByIdentity(identityID string) []store.Credential {
	return append([]store.Credential(nil), m.listCredentialsResp...)
}

func (m *mockChallengeStore) UpdateCredentialSignCount(credentialID string, signCount uint32) error {
	return m.updateSignCountErr
}

func TestServiceDefaultsAndValidationBranches(t *testing.T) {
	mockStore := &mockChallengeStore{}
	svc := New(mockStore, Config{})

	if svc.cfg.ChallengeTTL != 5*time.Minute {
		t.Fatalf("unexpected default challenge ttl: %s", svc.cfg.ChallengeTTL)
	}
	if svc.cfg.RPID != "localhost" {
		t.Fatalf("unexpected default rp id: %s", svc.cfg.RPID)
	}
	if svc.cfg.RPOrigin != "http://localhost" {
		t.Fatalf("unexpected default rp origin: %s", svc.cfg.RPOrigin)
	}
	if svc.cfg.AllowedCredentials != 20 {
		t.Fatalf("unexpected default allowed credentials: %d", svc.cfg.AllowedCredentials)
	}

	if _, err := svc.BeginRegistration("   "); !errors.Is(err, ErrInvalidIdentity) {
		t.Fatalf("BeginRegistration expected ErrInvalidIdentity, got %v", err)
	}
	if _, err := svc.BeginAuthentication(""); !errors.Is(err, ErrInvalidIdentity) {
		t.Fatalf("BeginAuthentication expected ErrInvalidIdentity, got %v", err)
	}

	if err := svc.FinishRegistration(nil); !errors.Is(err, ErrInvalidIdentity) {
		t.Fatalf("FinishRegistration(nil) expected ErrInvalidIdentity, got %v", err)
	}
	if err := svc.FinishRegistration(&RegistrationFinishRequest{IdentityID: "id"}); !errors.Is(err, ErrInvalidCredential) {
		t.Fatalf("FinishRegistration missing fields expected ErrInvalidCredential, got %v", err)
	}
	if err := svc.FinishAuthentication(nil); !errors.Is(err, ErrInvalidIdentity) {
		t.Fatalf("FinishAuthentication(nil) expected ErrInvalidIdentity, got %v", err)
	}
	if err := svc.FinishAuthentication(&AuthenticationFinishRequest{IdentityID: "id"}); !errors.Is(err, ErrInvalidCredential) {
		t.Fatalf("FinishAuthentication missing fields expected ErrInvalidCredential, got %v", err)
	}
}

func TestChallengeEntropyFailureBranches(t *testing.T) {
	origRandomChallengeBytes := randomChallengeBytes
	randomChallengeBytes = func(int) ([]byte, error) {
		return nil, errors.New("entropy failed")
	}
	t.Cleanup(func() { randomChallengeBytes = origRandomChallengeBytes })

	mockStore := &mockChallengeStore{}
	svc := New(mockStore, Config{ChallengeTTL: time.Minute})

	if _, err := randomChallenge(); err == nil || err.Error() != "entropy failed" {
		t.Fatalf("randomChallenge expected entropy failure, got %v", err)
	}
	if _, err := svc.BeginRegistration("identity-1"); err == nil || err.Error() != "entropy failed" {
		t.Fatalf("BeginRegistration expected entropy failure, got %v", err)
	}
	if _, err := svc.BeginAuthentication("identity-1"); err == nil || err.Error() != "entropy failed" {
		t.Fatalf("BeginAuthentication expected entropy failure, got %v", err)
	}
}

func TestFinishRegistrationBranches(t *testing.T) {
	_, badPublicKey := mustGenerateCredentialKey(t)

	t.Run("invalid public key rejected", func(t *testing.T) {
		mockStore := &mockChallengeStore{}
		svc := New(mockStore, Config{ChallengeTTL: time.Minute})
		err := svc.FinishRegistration(&RegistrationFinishRequest{
			IdentityID:   "identity-1",
			Challenge:    "challenge-1",
			CredentialID: "cred-1",
			PublicKey:    "not-a-pem",
		})
		if !errors.Is(err, ErrInvalidCredential) {
			t.Fatalf("expected ErrInvalidCredential, got %v", err)
		}
	})

	t.Run("challenge consume error maps to invalid challenge", func(t *testing.T) {
		mockStore := &mockChallengeStore{consumeChallengeErr: errors.New("missing")}
		svc := New(mockStore, Config{ChallengeTTL: time.Minute})
		err := svc.FinishRegistration(&RegistrationFinishRequest{
			IdentityID:   "identity-1",
			Challenge:    "challenge-1",
			CredentialID: "cred-1",
			PublicKey:    badPublicKey,
		})
		if !errors.Is(err, ErrInvalidChallenge) {
			t.Fatalf("expected ErrInvalidChallenge, got %v", err)
		}
	})

	t.Run("challenge mismatch rejected", func(t *testing.T) {
		mockStore := &mockChallengeStore{
			consumeChallengeResp: store.Challenge{ID: "challenge-1", IdentityID: "other", Purpose: "registration", ExpiresAt: time.Now().UTC().Add(time.Minute)},
		}
		svc := New(mockStore, Config{ChallengeTTL: time.Minute})
		err := svc.FinishRegistration(&RegistrationFinishRequest{
			IdentityID:   "identity-1",
			Challenge:    "challenge-1",
			CredentialID: "cred-1",
			PublicKey:    badPublicKey,
		})
		if !errors.Is(err, ErrInvalidChallenge) {
			t.Fatalf("expected ErrInvalidChallenge, got %v", err)
		}
	})
}

func TestFinishAuthenticationBranches(t *testing.T) {
	privateKey, publicKeyPEM := mustGenerateCredentialKey(t)
	challenge := "challenge-1"
	validSignature := mustSignAssertion(t, privateKey, "identity-1", "cred-1", challenge)

	t.Run("challenge consume error", func(t *testing.T) {
		mockStore := &mockChallengeStore{consumeChallengeErr: errors.New("missing")}
		svc := New(mockStore, Config{ChallengeTTL: time.Minute})
		err := svc.FinishAuthentication(&AuthenticationFinishRequest{
			IdentityID:   "identity-1",
			Challenge:    challenge,
			CredentialID: "cred-1",
			Signature:    validSignature,
			SignCount:    1,
		})
		if !errors.Is(err, ErrInvalidChallenge) {
			t.Fatalf("expected ErrInvalidChallenge, got %v", err)
		}
	})

	t.Run("credential lookup error", func(t *testing.T) {
		mockStore := &mockChallengeStore{
			consumeChallengeResp: store.Challenge{ID: challenge, IdentityID: "identity-1", Purpose: "authentication", ExpiresAt: time.Now().UTC().Add(time.Minute)},
			getCredentialErr:     errors.New("missing"),
		}
		svc := New(mockStore, Config{ChallengeTTL: time.Minute})
		err := svc.FinishAuthentication(&AuthenticationFinishRequest{
			IdentityID:   "identity-1",
			Challenge:    challenge,
			CredentialID: "cred-1",
			Signature:    validSignature,
			SignCount:    1,
		})
		if !errors.Is(err, ErrInvalidCredential) {
			t.Fatalf("expected ErrInvalidCredential, got %v", err)
		}
	})

	t.Run("credential identity mismatch", func(t *testing.T) {
		mockStore := &mockChallengeStore{
			consumeChallengeResp: store.Challenge{ID: challenge, IdentityID: "identity-1", Purpose: "authentication", ExpiresAt: time.Now().UTC().Add(time.Minute)},
			getCredentialResp:    store.Credential{ID: "cred-1", IdentityID: "other-identity", PublicKey: publicKeyPEM, SignCount: 0},
		}
		svc := New(mockStore, Config{ChallengeTTL: time.Minute})
		err := svc.FinishAuthentication(&AuthenticationFinishRequest{
			IdentityID:   "identity-1",
			Challenge:    challenge,
			CredentialID: "cred-1",
			Signature:    validSignature,
			SignCount:    1,
		})
		if !errors.Is(err, ErrInvalidCredential) {
			t.Fatalf("expected ErrInvalidCredential, got %v", err)
		}
	})

	t.Run("sign count replay", func(t *testing.T) {
		mockStore := &mockChallengeStore{
			consumeChallengeResp: store.Challenge{ID: challenge, IdentityID: "identity-1", Purpose: "authentication", ExpiresAt: time.Now().UTC().Add(time.Minute)},
			getCredentialResp:    store.Credential{ID: "cred-1", IdentityID: "identity-1", PublicKey: publicKeyPEM, SignCount: 3},
		}
		svc := New(mockStore, Config{ChallengeTTL: time.Minute})
		err := svc.FinishAuthentication(&AuthenticationFinishRequest{
			IdentityID:   "identity-1",
			Challenge:    challenge,
			CredentialID: "cred-1",
			Signature:    validSignature,
			SignCount:    3,
		})
		if !errors.Is(err, ErrSignCountReplay) {
			t.Fatalf("expected ErrSignCountReplay, got %v", err)
		}
	})

	t.Run("invalid signature", func(t *testing.T) {
		mockStore := &mockChallengeStore{
			consumeChallengeResp: store.Challenge{ID: challenge, IdentityID: "identity-1", Purpose: "authentication", ExpiresAt: time.Now().UTC().Add(time.Minute)},
			getCredentialResp:    store.Credential{ID: "cred-1", IdentityID: "identity-1", PublicKey: publicKeyPEM, SignCount: 0},
		}
		svc := New(mockStore, Config{ChallengeTTL: time.Minute})
		err := svc.FinishAuthentication(&AuthenticationFinishRequest{
			IdentityID:   "identity-1",
			Challenge:    challenge,
			CredentialID: "cred-1",
			Signature:    "not-base64",
			SignCount:    1,
		})
		if !errors.Is(err, ErrInvalidSignature) {
			t.Fatalf("expected ErrInvalidSignature, got %v", err)
		}
	})

	t.Run("update sign count error propagates", func(t *testing.T) {
		mockStore := &mockChallengeStore{
			consumeChallengeResp: store.Challenge{ID: challenge, IdentityID: "identity-1", Purpose: "authentication", ExpiresAt: time.Now().UTC().Add(time.Minute)},
			getCredentialResp:    store.Credential{ID: "cred-1", IdentityID: "identity-1", PublicKey: publicKeyPEM, SignCount: 0},
			updateSignCountErr:   errors.New("update failed"),
		}
		svc := New(mockStore, Config{ChallengeTTL: time.Minute})
		err := svc.FinishAuthentication(&AuthenticationFinishRequest{
			IdentityID:   "identity-1",
			Challenge:    challenge,
			CredentialID: "cred-1",
			Signature:    validSignature,
			SignCount:    1,
		})
		if err == nil || err.Error() != "update failed" {
			t.Fatalf("expected update error propagation, got %v", err)
		}
	})
}

func TestParseCredentialPublicKeyAndSignatureFallback(t *testing.T) {
	privateKey, publicKeyPEM := mustGenerateCredentialKey(t)

	if _, err := parseCredentialPublicKey(publicKeyPEM); err != nil {
		t.Fatalf("parseCredentialPublicKey(valid) error = %v", err)
	}

	rsaKey, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		t.Fatalf("rsa key generation failed: %v", err)
	}
	rsaDER, err := x509.MarshalPKIXPublicKey(&rsaKey.PublicKey)
	if err != nil {
		t.Fatalf("rsa public key marshal failed: %v", err)
	}
	rsaPEM := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: rsaDER})
	if _, err := parseCredentialPublicKey(string(rsaPEM)); err == nil {
		t.Fatalf("expected unsupported public key type error")
	}
	invalidDERPEM := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: []byte{1, 2, 3}})
	if _, err := parseCredentialPublicKey(string(invalidDERPEM)); err == nil {
		t.Fatalf("expected pkix parse error for invalid der")
	}

	rawSignature := mustSignAssertion(t, privateKey, "identity-1", "cred-1", "challenge-raw")
	sigBytes, err := base64.RawURLEncoding.DecodeString(rawSignature)
	if err != nil {
		t.Fatalf("decode raw signature failed: %v", err)
	}
	stdSignature := base64.StdEncoding.EncodeToString(sigBytes)

	pub, err := parseCredentialPublicKey(publicKeyPEM)
	if err != nil {
		t.Fatalf("parseCredentialPublicKey(valid) error = %v", err)
	}
	if err := verifyAssertionSignature(pub, "identity-1", "cred-1", "challenge-raw", stdSignature); err != nil {
		t.Fatalf("verifyAssertionSignature std base64 fallback failed: %v", err)
	}
	if err := verifyAssertionSignature(pub, "identity-1", "cred-1", "different-challenge", stdSignature); !errors.Is(err, ErrInvalidSignature) {
		t.Fatalf("verifyAssertionSignature expected ErrInvalidSignature on digest mismatch, got %v", err)
	}
}
