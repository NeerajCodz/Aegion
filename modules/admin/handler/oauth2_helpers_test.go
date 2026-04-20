package handler

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	platformcrypto "github.com/aegion/aegion/internal/platform/crypto"
	adminstore "github.com/aegion/aegion/modules/admin/store"
	oauth2store "github.com/aegion/aegion/modules/oauth2/store"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
)

func strPtr(v string) *string { return &v }
func intPtr(v int) *int       { return &v }
func boolPtr(v bool) *bool    { return &v }

func TestOAuth2HelperNormalizers(t *testing.T) {
	if got := buildWhereClause(nil); got != "" {
		t.Fatalf("buildWhereClause(nil) = %q", got)
	}
	if got := buildWhereClause([]string{"a = 1", "b = 2"}); got != " WHERE a = 1 AND b = 2" {
		t.Fatalf("buildWhereClause(non-empty) = %q", got)
	}

	redirects, err := normalizeRedirectURIs([]string{"https://b.example.com/cb", "https://a.example.com/cb", "https://a.example.com/cb", " "})
	if err != nil {
		t.Fatalf("normalizeRedirectURIs(valid) error = %v", err)
	}
	if len(redirects) != 2 || redirects[0] != "https://a.example.com/cb" || redirects[1] != "https://b.example.com/cb" {
		t.Fatalf("normalizeRedirectURIs(valid) = %#v", redirects)
	}
	if _, err := normalizeRedirectURIs([]string{"relative/path"}); err == nil {
		t.Fatal("normalizeRedirectURIs should reject relative URL")
	}
	if _, err := normalizeRedirectURIs([]string{"https://app.example.com/*"}); err == nil {
		t.Fatal("normalizeRedirectURIs should reject wildcards")
	}
	if _, err := normalizeRedirectURIs([]string{"https://app.example.com/cb#frag"}); err == nil {
		t.Fatal("normalizeRedirectURIs should reject fragments")
	}

	set, err := normalizeStringSet([]string{" Authorization_Code ", "refresh_token", "authorization_code"}, allowedOAuth2GrantTypes, []string{"authorization_code"}, true)
	if err != nil {
		t.Fatalf("normalizeStringSet(valid) error = %v", err)
	}
	if len(set) != 2 || set[0] != "authorization_code" || set[1] != "refresh_token" {
		t.Fatalf("normalizeStringSet(valid) = %#v", set)
	}
	if _, err := normalizeStringSet([]string{"invalid"}, allowedOAuth2GrantTypes, nil, true); err == nil {
		t.Fatal("normalizeStringSet should reject unsupported values")
	}
	set, err = normalizeStringSet(nil, allowedOAuth2GrantTypes, []string{"authorization_code"}, true)
	if err != nil || len(set) != 1 || set[0] != "authorization_code" {
		t.Fatalf("normalizeStringSet(defaults) = %#v, %v", set, err)
	}

	generic := normalizeGenericStringSet([]string{" email ", "openid", "openid"}, []string{"fallback"})
	if len(generic) != 2 || generic[0] != "email" || generic[1] != "openid" {
		t.Fatalf("normalizeGenericStringSet = %#v", generic)
	}
	generic = normalizeGenericStringSet(nil, []string{"fallback"})
	if len(generic) != 1 || generic[0] != "fallback" {
		t.Fatalf("normalizeGenericStringSet(defaults) = %#v", generic)
	}

	meta := normalizeMetadata(map[string]string{"  team  ": "  identity  ", "": "ignored"})
	if len(meta) != 1 || meta["team"] != "identity" {
		t.Fatalf("normalizeMetadata = %#v", meta)
	}
	if got := normalizeMetadata(map[string]string{" ": "x"}); got != nil {
		t.Fatalf("normalizeMetadata(empty keys only) = %#v", got)
	}

	if got := normalizedTTL(nil, 900); got != 900 {
		t.Fatalf("normalizedTTL(nil) = %d", got)
	}
	if got := normalizedTTL(intPtr(120), 900); got != 120 {
		t.Fatalf("normalizedTTL(pointer) = %d", got)
	}
	if got := normalizedBool(nil, true); !got {
		t.Fatalf("normalizedBool(nil,true) = %v", got)
	}
	if got := normalizedBool(boolPtr(false), true); got {
		t.Fatalf("normalizedBool(pointer,false) = %v", got)
	}
	if got := trimOptionalString(nil); got != nil {
		t.Fatalf("trimOptionalString(nil) = %#v", got)
	}
	if got := trimOptionalString(strPtr("   ")); got != nil {
		t.Fatalf("trimOptionalString(whitespace) = %#v", got)
	}
	if got := trimOptionalString(strPtr(" value ")); got == nil || *got != "value" {
		t.Fatalf("trimOptionalString(value) = %#v", got)
	}
}

func TestOAuth2HelperBuildClientFromRequest(t *testing.T) {
	baseReq := OAuth2ClientRequest{
		ID:           "client-1",
		Name:         "Web App",
		RedirectURIs: []string{"https://app.example.com/callback"},
	}

	if _, _, err := buildOAuth2ClientFromRequest(nil, OAuth2ClientRequest{}); err == nil {
		t.Fatal("expected create client to require name and redirect URI")
	}

	req := baseReq
	req.TokenEndpointAuthMethod = "unsupported"
	if _, _, err := buildOAuth2ClientFromRequest(nil, req); err == nil {
		t.Fatal("expected unsupported token endpoint auth method error")
	}

	req = baseReq
	req.SubjectType = "unsupported"
	if _, _, err := buildOAuth2ClientFromRequest(nil, req); err == nil {
		t.Fatal("expected unsupported subject_type error")
	}

	req = baseReq
	req.AccessTokenStrategy = "invalid"
	if _, _, err := buildOAuth2ClientFromRequest(nil, req); err == nil {
		t.Fatal("expected unsupported access_token_strategy error")
	}

	req = baseReq
	req.TokenEndpointAuthMethod = "private_key_jwt"
	if _, _, err := buildOAuth2ClientFromRequest(nil, req); err == nil {
		t.Fatal("expected private_key_jwt without jwks to fail")
	}

	req = baseReq
	req.TokenEndpointAuthMethod = "none"
	req.ClientSecret = "secret"
	if _, _, err := buildOAuth2ClientFromRequest(nil, req); err == nil {
		t.Fatal("expected public clients with client_secret to fail")
	}

	req = baseReq
	req.AccessTokenTTL = intPtr(0)
	if _, _, err := buildOAuth2ClientFromRequest(nil, req); err == nil {
		t.Fatal("expected non-positive ttl to fail")
	}

	req = baseReq
	req.Description = strPtr("  desc  ")
	req.LogoURI = strPtr(" https://app.example.com/logo.png ")
	req.ClientURI = strPtr(" https://app.example.com ")
	req.PolicyURI = strPtr(" https://app.example.com/policy ")
	req.TOSURI = strPtr(" https://app.example.com/tos ")
	req.PostLogoutRedirectURIs = []string{"https://app.example.com/logout"}
	req.GrantTypes = []string{"authorization_code", "refresh_token"}
	req.ResponseTypes = []string{"code"}
	req.Scopes = []string{"openid", "email"}
	req.Audience = []string{"api://app"}
	req.SubjectType = "pairwise"
	req.AccessTokenStrategy = "jwt"
	req.AccessTokenTTL = intPtr(900)
	req.RefreshTokenTTL = intPtr(2592000)
	req.IDTokenTTL = intPtr(3600)
	req.AuthCodeTTL = intPtr(600)
	req.RequirePKCE = boolPtr(false)
	req.RequireConsent = boolPtr(true)
	req.AllowOfflineAccess = boolPtr(false)
	req.Metadata = map[string]string{" team ": " identity "}

	client, plainSecret, err := buildOAuth2ClientFromRequest(nil, req)
	if err != nil {
		t.Fatalf("buildOAuth2ClientFromRequest(create) error = %v", err)
	}
	if client.ID != "client-1" || client.Name != "Web App" || len(client.RedirectURIs) != 1 || client.RedirectURIs[0] != "https://app.example.com/callback" {
		t.Fatalf("unexpected client core fields: %+v", client)
	}
	if plainSecret == nil || strings.TrimSpace(*plainSecret) == "" {
		t.Fatalf("expected generated secret for confidential client, got %#v", plainSecret)
	}
	if client.Description == nil || *client.Description != "desc" {
		t.Fatalf("expected trimmed description, got %#v", client.Description)
	}
	if client.Metadata["team"] != "identity" {
		t.Fatalf("expected normalized metadata, got %#v", client.Metadata)
	}

	existing := &oauth2store.Client{
		ID:                      "existing-id",
		Name:                    "Existing",
		RedirectURIs:            []string{"https://existing.example.com/callback"},
		TokenEndpointAuthMethod: "none",
		SubjectType:             "public",
		AccessTokenStrategy:     "opaque",
		AccessTokenTTL:          900,
		RefreshTokenTTL:         900,
		IDTokenTTL:              900,
		AuthCodeTTL:             900,
		RequirePKCE:             false,
		RequireConsent:          false,
		AllowOfflineAccess:      false,
	}
	updateReq := OAuth2ClientRequest{
		Name:         " ",
		RedirectURIs: nil,
	}
	updated, updatedSecret, err := buildOAuth2ClientFromRequest(existing, updateReq)
	if err != nil {
		t.Fatalf("buildOAuth2ClientFromRequest(update) error = %v", err)
	}
	if updated.Name != "Existing" {
		t.Fatalf("expected existing name retained on blank update, got %q", updated.Name)
	}
	if len(updated.RedirectURIs) != 1 || updated.RedirectURIs[0] != "https://existing.example.com/callback" {
		t.Fatalf("expected existing redirect uris retained, got %#v", updated.RedirectURIs)
	}
	if updatedSecret != nil {
		t.Fatalf("did not expect secret generation for existing public client update, got %#v", updatedSecret)
	}
	if !updated.RequirePKCE {
		t.Fatalf("expected public client update to force PKCE, got %+v", updated)
	}
}

func TestOAuth2HelperSecretAndRevocationHelpers(t *testing.T) {
	secret, err := generateOAuth2ClientSecret()
	if err != nil {
		t.Fatalf("generateOAuth2ClientSecret error = %v", err)
	}
	if secret == "" {
		t.Fatal("generateOAuth2ClientSecret returned empty value")
	}
	if _, err := base64.RawURLEncoding.DecodeString(secret); err != nil {
		t.Fatalf("generated secret is not raw URL-safe base64: %v", err)
	}

	hash, err := hashOAuth2ClientSecret("top-secret")
	if err != nil {
		t.Fatalf("hashOAuth2ClientSecret error = %v", err)
	}
	ok, err := platformcrypto.VerifyPassword("top-secret", hash)
	if err != nil || !ok {
		t.Fatalf("expected VerifyPassword(top-secret) true, got ok=%v err=%v", ok, err)
	}

	operator := &adminstore.Operator{ID: uuid.New(), IdentityID: uuid.New()}
	h := New(&fakeService{store: &fakeStore{}})
	boom := errors.New("boom")
	h.db = &fakeDB{execFn: func(context.Context, string, ...any) (pgconn.CommandTag, error) { return pgconn.CommandTag{}, boom }}
	req := httptest.NewRequest(http.MethodPost, "/admin/oauth2/tokens/revoke", nil)
	if err := h.recordOAuth2TokenRevocation(req, "access_token", "tok-1", "client-1", "identity-1", time.Now().UTC(), "reason", operator); !errors.Is(err, boom) {
		t.Fatalf("recordOAuth2TokenRevocation(expected error) = %v", err)
	}

	h.db = &fakeDB{execFn: func(context.Context, string, ...any) (pgconn.CommandTag, error) {
		return pgconn.NewCommandTag("INSERT 1"), nil
	}}
	if err := h.recordOAuth2TokenRevocation(req, "refresh_token", "tok-2", "client-2", "identity-2", time.Now().UTC(), "manual", operator); err != nil {
		t.Fatalf("recordOAuth2TokenRevocation(success) error = %v", err)
	}

	meta := map[string]string{"team": "identity"}
	raw, err := json.Marshal(meta)
	if err != nil || len(raw) == 0 {
		t.Fatalf("json marshal sanity check failed err=%v raw=%q", err, string(raw))
	}
}
