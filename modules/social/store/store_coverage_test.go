package store

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	platformcrypto "github.com/aegion/aegion/internal/platform/crypto"
	"github.com/google/uuid"
)

type testScanProvider struct {
	values []any
	err    error
}

func (s testScanProvider) Scan(dest ...any) error {
	if s.err != nil {
		return s.err
	}
	for i := range dest {
		switch d := dest[i].(type) {
		case *uuid.UUID:
			*d = s.values[i].(uuid.UUID)
		case *string:
			*d = s.values[i].(string)
		case *[]byte:
			*d = s.values[i].([]byte)
		case *Protocol:
			switch v := s.values[i].(type) {
			case Protocol:
				*d = v
			case string:
				*d = Protocol(v)
			default:
				return errors.New("unsupported protocol value")
			}
		case *PKCEMethod:
			switch v := s.values[i].(type) {
			case PKCEMethod:
				*d = v
			case string:
				*d = PKCEMethod(v)
			default:
				return errors.New("unsupported pkce value")
			}
		case *AuthStyle:
			switch v := s.values[i].(type) {
			case AuthStyle:
				*d = v
			case string:
				*d = AuthStyle(v)
			default:
				return errors.New("unsupported auth style value")
			}
		case *ClaimSource:
			switch v := s.values[i].(type) {
			case ClaimSource:
				*d = v
			case string:
				*d = ClaimSource(v)
			default:
				return errors.New("unsupported claim source value")
			}
		case *bool:
			*d = s.values[i].(bool)
		case *time.Time:
			*d = s.values[i].(time.Time)
		default:
			return errors.New("unsupported destination type")
		}
	}
	return nil
}

func TestMemoryStoreAdditionalBranches(t *testing.T) {
	ctx := context.Background()
	s := New()

	_, err := s.UpsertProvider(ctx, Provider{
		Slug:         " GOOGLE ",
		DisplayName:  "Google",
		ClientID:     "client-1",
		ClientSecret: "secret-1",
		Enabled:      true,
	})
	if err != nil {
		t.Fatalf("UpsertProvider(create) error = %v", err)
	}

	updated, err := s.UpsertProvider(ctx, Provider{
		Slug:         "google",
		DisplayName:  "Google Updated",
		ClientID:     "client-2",
		ClientSecret: "   ",
		Enabled:      false,
	})
	if err != nil {
		t.Fatalf("UpsertProvider(update) error = %v", err)
	}
	if strings.TrimSpace(updated.ClientSecret) != "secret-1" {
		t.Fatalf("UpsertProvider(update) expected previous secret preserved, got %q", updated.ClientSecret)
	}

	enabledOnly, err := s.ListProviders(ctx, false)
	if err != nil {
		t.Fatalf("ListProviders(includeDisabled=false) error = %v", err)
	}
	if len(enabledOnly) != 0 {
		t.Fatalf("ListProviders(includeDisabled=false) len = %d, want 0", len(enabledOnly))
	}
	all, err := s.ListProviders(ctx, true)
	if err != nil {
		t.Fatalf("ListProviders(includeDisabled=true) error = %v", err)
	}
	if len(all) != 1 || all[0].Slug != "google" {
		t.Fatalf("ListProviders(includeDisabled=true) = %#v, want one google provider", all)
	}

	provider, err := s.GetProviderBySlug(ctx, "GOOGLE")
	if err != nil {
		t.Fatalf("GetProviderBySlug(existing) error = %v", err)
	}
	provider.DisplayName = "mutated"
	providerAgain, err := s.GetProviderBySlug(ctx, "google")
	if err != nil {
		t.Fatalf("GetProviderBySlug(second) error = %v", err)
	}
	if providerAgain.DisplayName == "mutated" {
		t.Fatalf("GetProviderBySlug should return clone copy")
	}

	if _, err := s.GetProviderBySlug(ctx, "missing"); !errors.Is(err, ErrProviderNotFound) {
		t.Fatalf("GetProviderBySlug(missing) error = %v, want %v", err, ErrProviderNotFound)
	}

	if _, err := s.ConsumeState(ctx, "missing"); !errors.Is(err, ErrStateNotFound) {
		t.Fatalf("ConsumeState(missing) error = %v, want %v", err, ErrStateNotFound)
	}

	if err := s.SaveState(ctx, AuthState{
		ID:           "state-expired",
		ProviderSlug: "google",
		ExpiresAt:    time.Now().UTC().Add(-time.Second),
	}); err != nil {
		t.Fatalf("SaveState(expired) error = %v", err)
	}
	if _, err := s.ConsumeState(ctx, "state-expired"); !errors.Is(err, ErrStateExpired) {
		t.Fatalf("ConsumeState(expired) error = %v, want %v", err, ErrStateExpired)
	}

	firstLink, err := s.ResolveIdentity(ctx, Provider{Slug: "google"}, SocialProfile{
		ProviderUser: "sub-1",
		Email:        "User@Example.com",
	})
	if err != nil {
		t.Fatalf("ResolveIdentity(first) error = %v", err)
	}
	if !firstLink.Created || !firstLink.Linked {
		t.Fatalf("ResolveIdentity(first) expected created+linked result, got %#v", firstLink)
	}

	secondBySubject, err := s.ResolveIdentity(ctx, Provider{Slug: "google"}, SocialProfile{
		ProviderUser: "sub-1",
		Email:        "other@example.com",
	})
	if err != nil {
		t.Fatalf("ResolveIdentity(subject existing) error = %v", err)
	}
	if secondBySubject.IdentityID != firstLink.IdentityID || secondBySubject.Created {
		t.Fatalf("ResolveIdentity(subject existing) result = %#v", secondBySubject)
	}

	secondByEmail, err := s.ResolveIdentity(ctx, Provider{Slug: "google"}, SocialProfile{
		ProviderUser: "sub-2",
		Email:        " user@example.com ",
	})
	if err != nil {
		t.Fatalf("ResolveIdentity(email existing) error = %v", err)
	}
	if secondByEmail.IdentityID != firstLink.IdentityID || secondByEmail.Created {
		t.Fatalf("ResolveIdentity(email existing) result = %#v", secondByEmail)
	}

	if err := s.DeleteProvider(ctx, "google"); err != nil {
		t.Fatalf("DeleteProvider(existing) error = %v", err)
	}
	if err := s.DeleteProvider(ctx, "google"); !errors.Is(err, ErrProviderNotFound) {
		t.Fatalf("DeleteProvider(missing) error = %v, want %v", err, ErrProviderNotFound)
	}
}

func TestProviderSanitizedAndHelpers(t *testing.T) {
	p := Provider{ClientSecret: "top-secret"}
	if got := p.Sanitized(); got.ClientSecret != "" {
		t.Fatalf("Provider.Sanitized() expected empty ClientSecret, got %q", got.ClientSecret)
	}

	if got := normalizeSlug("  GOOGLE "); got != "google" {
		t.Fatalf("normalizeSlug() = %q, want google", got)
	}
	if got := normalizeScopes([]string{"openid", " ", "email", "openid", "email"}); len(got) != 2 || got[0] != "openid" || got[1] != "email" {
		t.Fatalf("normalizeScopes() = %#v, want [openid email]", got)
	}
	if got := uuidText(uuid.Nil); got != "" {
		t.Fatalf("uuidText(uuid.Nil) = %q, want empty", got)
	}
	id := uuid.New()
	if got := uuidText(id); got != id.String() {
		t.Fatalf("uuidText(non-nil) = %q, want %q", got, id.String())
	}

	s := &PostgresStore{}
	if s.shouldTrustEmail(Provider{TrustEmailVerified: true}, SocialProfile{EmailVerified: true, Email: " "}) {
		t.Fatalf("shouldTrustEmail should require non-empty trimmed email")
	}
	if !s.shouldTrustEmail(Provider{TrustEmailVerified: true}, SocialProfile{EmailVerified: true, Email: "user@example.com"}) {
		t.Fatalf("shouldTrustEmail expected true for trusted+verified+non-empty email")
	}
}

func TestPostgresStoreCryptoAndScanBranches(t *testing.T) {
	key := make([]byte, platformcrypto.KeySize)
	s := &PostgresStore{cipherKey: key}
	now := time.Now().UTC().Round(0)
	id := uuid.New()

	ciphertext, err := s.encryptSecret(" GOOGLE ", "super-secret")
	if err != nil {
		t.Fatalf("encryptSecret() error = %v", err)
	}
	plaintext, err := s.decryptSecret("google", ciphertext)
	if err != nil {
		t.Fatalf("decryptSecret() error = %v", err)
	}
	if plaintext != "super-secret" {
		t.Fatalf("decryptSecret() = %q, want super-secret", plaintext)
	}
	empty, err := s.decryptSecret("google", "   ")
	if err != nil || empty != "" {
		t.Fatalf("decryptSecret(empty) = %q, %v; want empty,nil", empty, err)
	}
	if _, err := (&PostgresStore{cipherKey: bytesOf(platformcrypto.KeySize, 1)}).decryptSecret("google", ciphertext); err == nil {
		t.Fatalf("decryptSecret(wrong key) expected error")
	}

	validValues := []any{
		id,
		"google",
		"Google",
		"google",
		string(ProtocolOIDC),
		"https://issuer.example.com",
		"https://issuer.example.com/.well-known/openid-configuration",
		"",
		"",
		"https://issuer.example.com/userinfo",
		"https://issuer.example.com/jwks.json",
		[]byte(`["openid","email"]`),
		[]byte(`{"subject":"sub","email":"email","email_verified":"email_verified","name":"name","picture":"picture"}`),
		[]byte(`{"prompt":"login"}`),
		string(PKCES256),
		string(AuthStyleClientSecretPost),
		string(ClaimSourceUserInfo),
		true,
		true,
		"https://app.example.com/callback",
		"client-id",
		ciphertext,
		now,
		now,
	}

	t.Run("scan error", func(t *testing.T) {
		_, err := s.scanProvider(testScanProvider{err: errors.New("scan failed")})
		if err == nil || err.Error() != "scan failed" {
			t.Fatalf("scanProvider(scan error) = %v, want scan failed", err)
		}
	})
	t.Run("scopes json error", func(t *testing.T) {
		values := append([]any(nil), validValues...)
		values[11] = []byte(`{`)
		_, err := s.scanProvider(testScanProvider{values: values})
		if err == nil {
			t.Fatalf("scanProvider(scopes json error) expected error")
		}
	})
	t.Run("claim mapping json error", func(t *testing.T) {
		values := append([]any(nil), validValues...)
		values[12] = []byte(`{`)
		_, err := s.scanProvider(testScanProvider{values: values})
		if err == nil {
			t.Fatalf("scanProvider(claim json error) expected error")
		}
	})
	t.Run("extra auth params json error", func(t *testing.T) {
		values := append([]any(nil), validValues...)
		values[13] = []byte(`{`)
		_, err := s.scanProvider(testScanProvider{values: values})
		if err == nil {
			t.Fatalf("scanProvider(extra json error) expected error")
		}
	})
	t.Run("decrypt error", func(t *testing.T) {
		values := append([]any(nil), validValues...)
		values[21] = "not-a-valid-ciphertext"
		_, err := s.scanProvider(testScanProvider{values: values})
		if err == nil || !strings.Contains(err.Error(), "decrypt provider secret") {
			t.Fatalf("scanProvider(decrypt error) = %v, want wrapped decrypt error", err)
		}
	})
	t.Run("success normalizes empty collections", func(t *testing.T) {
		values := append([]any(nil), validValues...)
		values[11] = []byte(`[]`)
		values[13] = []byte(`null`)
		got, err := s.scanProvider(testScanProvider{values: values})
		if err != nil {
			t.Fatalf("scanProvider(success) error = %v", err)
		}
		if got.ID != id || got.Slug != "google" || got.ClientSecret != "super-secret" {
			t.Fatalf("scanProvider(success) unexpected provider: %#v", got)
		}
		if got.Scopes == nil || len(got.Scopes) != 0 {
			t.Fatalf("scanProvider(success) expected empty scopes slice, got %#v", got.Scopes)
		}
		if got.ExtraAuthParams == nil || len(got.ExtraAuthParams) != 0 {
			t.Fatalf("scanProvider(success) expected empty extra map, got %#v", got.ExtraAuthParams)
		}
	})
}

func TestNewPostgresValidation(t *testing.T) {
	if _, err := NewPostgres(nil, make([]byte, platformcrypto.KeySize)); err == nil {
		t.Fatalf("NewPostgres(nil pool) expected error")
	}
	if _, err := NewPostgres(nil, []byte("short")); !errors.Is(err, errors.New("postgres pool is required")) && err == nil {
		t.Fatalf("NewPostgres(short key) with nil pool should still fail")
	}
}

func bytesOf(n int, b byte) []byte {
	out := make([]byte, n)
	for i := range out {
		out[i] = b
	}
	return out
}
