package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

type testScanConnection struct {
	values []any
	err    error
}

func (s testScanConnection) Scan(dest ...any) error {
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
	now := time.Now().UTC().Round(0)

	_, err := s.UpsertConnection(ctx, Connection{
		Slug:        " ACME ",
		DisplayName: "Acme",
		EntityID:    "urn:acme",
		SSOURL:      "https://idp.example.com",
		Domains:     []string{"Example.com", "@corp.example.com"},
		Enabled:     true,
		CreatedAt:   now,
		UpdatedAt:   now,
	})
	if err != nil {
		t.Fatalf("UpsertConnection(create) error = %v", err)
	}

	updated, err := s.UpsertConnection(ctx, Connection{
		Slug:        "acme",
		DisplayName: "Acme Updated",
		EntityID:    "urn:acme:v2",
		SSOURL:      "https://idp-v2.example.com",
		Domains:     []string{"Example.com", "@corp.example.com"},
		Enabled:     true,
	})
	if err != nil {
		t.Fatalf("UpsertConnection(update) error = %v", err)
	}
	if updated.ID == uuid.Nil {
		t.Fatalf("UpsertConnection(update) expected stable non-nil ID")
	}

	if _, err := s.GetConnectionBySlug(ctx, "missing"); !errors.Is(err, ErrConnectionNotFound) {
		t.Fatalf("GetConnectionBySlug(missing) error = %v, want %v", err, ErrConnectionNotFound)
	}
	if _, err := s.GetConnectionByDomain(ctx, "missing.example.com"); !errors.Is(err, ErrConnectionNotFound) {
		t.Fatalf("GetConnectionByDomain(missing) error = %v, want %v", err, ErrConnectionNotFound)
	}

	byDomain, err := s.GetConnectionByDomain(ctx, "@corp.example.com")
	if err != nil {
		t.Fatalf("GetConnectionByDomain(existing) error = %v", err)
	}
	if byDomain.Slug != "acme" {
		t.Fatalf("GetConnectionByDomain(existing) slug = %q, want acme", byDomain.Slug)
	}

	all, err := s.ListConnections(ctx, true)
	if err != nil {
		t.Fatalf("ListConnections(includeDisabled=true) error = %v", err)
	}
	if len(all) != 1 {
		t.Fatalf("ListConnections(includeDisabled=true) len = %d, want 1", len(all))
	}

	_, err = s.UpsertConnection(ctx, Connection{
		Slug:        "disabled",
		DisplayName: "Disabled",
		EntityID:    "urn:disabled",
		SSOURL:      "https://disabled.example.com",
		Enabled:     false,
	})
	if err != nil {
		t.Fatalf("UpsertConnection(disabled) error = %v", err)
	}
	enabledOnly, err := s.ListConnections(ctx, false)
	if err != nil {
		t.Fatalf("ListConnections(includeDisabled=false) error = %v", err)
	}
	if len(enabledOnly) != 1 || enabledOnly[0].Slug != "acme" {
		t.Fatalf("ListConnections(includeDisabled=false) = %#v, want only acme", enabledOnly)
	}

	if err := s.DeleteConnection(ctx, "disabled"); err != nil {
		t.Fatalf("DeleteConnection(existing) error = %v", err)
	}
	if err := s.DeleteConnection(ctx, "disabled"); !errors.Is(err, ErrConnectionNotFound) {
		t.Fatalf("DeleteConnection(missing) error = %v, want %v", err, ErrConnectionNotFound)
	}

	_, err = s.UpsertConnection(ctx, Connection{})
	if !errors.Is(err, ErrConnectionNotFound) {
		t.Fatalf("UpsertConnection(empty slug) error = %v, want %v", err, ErrConnectionNotFound)
	}
}

func TestCloneAndNormalizeHelpers(t *testing.T) {
	in := Connection{
		Slug:              "acme",
		Domains:           []string{"a.com"},
		ExtraAuthnContext: map[string]string{"foo": "bar"},
	}
	cloned := cloneConnection(in)
	cloned.Domains[0] = "mutated"
	cloned.ExtraAuthnContext["foo"] = "mutated"
	if in.Domains[0] != "a.com" || in.ExtraAuthnContext["foo"] != "bar" {
		t.Fatalf("cloneConnection should deep-clone map/slice")
	}

	if got := normalizeSlug("  ACME  "); got != "acme" {
		t.Fatalf("normalizeSlug() = %q, want acme", got)
	}
	if got := normalizeDomain("  @Example.COM "); got != "example.com" {
		t.Fatalf("normalizeDomain() = %q, want example.com", got)
	}

	if got := cloneStringMap(nil); len(got) != 0 {
		t.Fatalf("cloneStringMap(nil) len = %d, want 0", len(got))
	}
}

func TestScanConnectionBranches(t *testing.T) {
	now := time.Now().UTC().Round(0)
	id := uuid.New()

	validValues := []any{
		id,
		"acme",
		"Acme",
		"urn:acme",
		"https://idp.example.com",
		"cert",
		"https://metadata.example.com",
		[]byte(`["example.com"]`),
		[]byte(`{"subject":"sub","email":"mail","display_name":"name","first_name":"first","last_name":"last"}`),
		true,
		"https://app.example.com/home",
		[]byte(`{"acr":"urn:acme:loa2"}`),
		true,
		now,
		now,
	}

	t.Run("scan error", func(t *testing.T) {
		_, err := scanConnection(testScanConnection{err: errors.New("scan failed")})
		if err == nil || err.Error() != "scan failed" {
			t.Fatalf("scanConnection(scan error) = %v, want scan failed", err)
		}
	})

	t.Run("domains json error", func(t *testing.T) {
		values := append([]any(nil), validValues...)
		values[7] = []byte(`{`)
		_, err := scanConnection(testScanConnection{values: values})
		if err == nil {
			t.Fatalf("scanConnection(domains json error) expected error")
		}
	})

	t.Run("mapping json error", func(t *testing.T) {
		values := append([]any(nil), validValues...)
		values[8] = []byte(`{`)
		_, err := scanConnection(testScanConnection{values: values})
		if err == nil {
			t.Fatalf("scanConnection(mapping json error) expected error")
		}
	})

	t.Run("extra authn context json error", func(t *testing.T) {
		values := append([]any(nil), validValues...)
		values[11] = []byte(`{`)
		_, err := scanConnection(testScanConnection{values: values})
		if err == nil {
			t.Fatalf("scanConnection(extra json error) expected error")
		}
	})

	t.Run("success normalizes nil slices/maps", func(t *testing.T) {
		values := append([]any(nil), validValues...)
		values[7] = []byte(`null`)
		values[11] = []byte(`null`)
		got, err := scanConnection(testScanConnection{values: values})
		if err != nil {
			t.Fatalf("scanConnection(success) error = %v", err)
		}
		if got.ID != id || got.Slug != "acme" {
			t.Fatalf("scanConnection(success) returned unexpected connection: %#v", got)
		}
		if got.Domains == nil || len(got.Domains) != 0 {
			t.Fatalf("scanConnection(success) expected empty domains slice, got %#v", got.Domains)
		}
		if got.ExtraAuthnContext == nil || len(got.ExtraAuthnContext) != 0 {
			t.Fatalf("scanConnection(success) expected empty extra_authn_context map, got %#v", got.ExtraAuthnContext)
		}
	})
}

func TestUUIDTextHelper(t *testing.T) {
	if got := uuidText(uuid.Nil); got != "" {
		t.Fatalf("uuidText(uuid.Nil) = %q, want empty", got)
	}
	id := uuid.New()
	if got := uuidText(id); got != id.String() {
		t.Fatalf("uuidText(non-nil) = %q, want %q", got, id.String())
	}
}
