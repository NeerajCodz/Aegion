package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	platformconfig "github.com/aegion/aegion/internal/platform/config"
)

func boolPtr(v bool) *bool {
	return &v
}

func stringPtr(v string) *string {
	return &v
}

func TestAdminPaginationHelpers(t *testing.T) {
	t.Run("admin page settings honor bounds", func(t *testing.T) {
		s := &Server{}
		defaultSize, maxSize := s.adminPageSettings()
		if defaultSize != 20 || maxSize != 100 {
			t.Fatalf("expected defaults (20,100), got (%d,%d)", defaultSize, maxSize)
		}

		s.cfg = &platformconfig.Config{
			Admin: platformconfig.AdminConfig{
				DefaultPageSize: 250,
				MaxPageSize:     40,
			},
		}
		defaultSize, maxSize = s.adminPageSettings()
		if defaultSize != 40 || maxSize != 40 {
			t.Fatalf("expected bounded values (40,40), got (%d,%d)", defaultSize, maxSize)
		}

		s.cfg.Admin.DefaultPageSize = 30
		s.cfg.Admin.MaxPageSize = 0
		defaultSize, maxSize = s.adminPageSettings()
		if defaultSize != 30 || maxSize != 100 {
			t.Fatalf("expected fallback max page size (30,100), got (%d,%d)", defaultSize, maxSize)
		}
	})

	t.Run("parse admin pagination clamps and computes offset", func(t *testing.T) {
		s := &Server{
			cfg: &platformconfig.Config{
				Admin: platformconfig.AdminConfig{
					DefaultPageSize: 25,
					MaxPageSize:     50,
				},
			},
		}

		req := httptest.NewRequest(http.MethodGet, "/aegion?page=3&per_page=500", nil)
		page, perPage, offset := s.parseAdminPagination(req)
		if page != 3 || perPage != 50 || offset != 100 {
			t.Fatalf("expected (3,50,100), got (%d,%d,%d)", page, perPage, offset)
		}

		req = httptest.NewRequest(http.MethodGet, "/aegion?page=0&per_page=-9", nil)
		page, perPage, offset = s.parseAdminPagination(req)
		if page != 1 || perPage != 25 || offset != 0 {
			t.Fatalf("expected fallback values (1,25,0), got (%d,%d,%d)", page, perPage, offset)
		}
	})
}

func TestAdminScalarHelpers(t *testing.T) {
	if got := parsePositiveInt(" 42 ", 7); got != 42 {
		t.Fatalf("expected 42, got %d", got)
	}
	if got := parsePositiveInt("", 7); got != 7 {
		t.Fatalf("expected fallback 7 for empty input, got %d", got)
	}
	if got := parsePositiveInt("0", 7); got != 7 {
		t.Fatalf("expected fallback 7 for non-positive input, got %d", got)
	}
	if got := parsePositiveInt("not-a-number", 7); got != 7 {
		t.Fatalf("expected fallback 7 for invalid integer, got %d", got)
	}

	meta := adminPaginationMeta(45, 2, 20)
	if meta["total_pages"] != 3 {
		t.Fatalf("expected total_pages=3, got %v", meta["total_pages"])
	}
	if meta["total"] != int64(45) {
		t.Fatalf("expected total=45, got %v", meta["total"])
	}

	meta = adminPaginationMeta(0, 1, 20)
	if meta["total_pages"] != 1 {
		t.Fatalf("expected total_pages=1 for empty result set, got %v", meta["total_pages"])
	}
}

func TestAdminIdentityHelpers(t *testing.T) {
	sortCases := map[string]string{
		"created_at":  "ci.created_at ASC",
		"-created_at": "ci.created_at DESC",
		"updated_at":  "ci.updated_at ASC",
		"-updated_at": "ci.updated_at DESC",
		"state":       "ci.state ASC, ci.created_at DESC",
		"-state":      "ci.state DESC, ci.created_at DESC",
		"unknown":     "ci.created_at DESC",
	}
	for input, want := range sortCases {
		if got := adminIdentitySortExpr(input); got != want {
			t.Fatalf("adminIdentitySortExpr(%q)=%q, want %q", input, got, want)
		}
	}

	if got := mapAdminIdentityStateFromDB("inactive"); got != "inactive" {
		t.Fatalf("expected inactive mapping, got %q", got)
	}
	if got := mapAdminIdentityStateFromDB("banned"); got != "blocked" {
		t.Fatalf("expected banned->blocked mapping, got %q", got)
	}
	if got := mapAdminIdentityStateFromDB("unexpected"); got != "active" {
		t.Fatalf("expected unknown state to map to active, got %q", got)
	}

	if got, err := normalizeAdminIdentityState(""); err != nil || got != "active" {
		t.Fatalf("expected empty state to normalize to active, got (%q, %v)", got, err)
	}
	if got, err := normalizeAdminIdentityState("blocked"); err != nil || got != "banned" {
		t.Fatalf("expected blocked alias to normalize to banned, got (%q, %v)", got, err)
	}
	if _, err := normalizeAdminIdentityState("disabled"); err == nil {
		t.Fatalf("expected invalid state to return an error")
	}

	if email, ok := adminEmailFromTraits(map[string]interface{}{"email": "  USER@Example.com  "}); !ok || email != "user@example.com" {
		t.Fatalf("expected normalized email user@example.com, got (%q,%t)", email, ok)
	}
	if _, ok := adminEmailFromTraits(map[string]interface{}{"email": 123}); ok {
		t.Fatalf("expected non-string email value to be rejected")
	}
	if _, ok := adminEmailFromTraits(map[string]interface{}{"email": "   "}); ok {
		t.Fatalf("expected blank email value to be rejected")
	}
	if _, ok := adminEmailFromTraits(nil); ok {
		t.Fatalf("expected nil traits to be rejected")
	}

	validID := uuid.New().String()
	if !isUUID(" " + validID + " ") {
		t.Fatalf("expected UUID helper to accept trimmed valid UUID")
	}
	if isUUID("not-a-uuid") {
		t.Fatalf("expected UUID helper to reject invalid UUID")
	}
}

func TestRuntimePolicyHelpers(t *testing.T) {
	defaultSettings := defaultRuntimePolicySettings(nil)
	if defaultSettings.DefaultModel != "rbac" {
		t.Fatalf("expected nil config default model to be rbac, got %q", defaultSettings.DefaultModel)
	}
	if !defaultSettings.RBAC.Enabled {
		t.Fatalf("expected nil config to enable RBAC by default")
	}

	cfg := &platformconfig.Config{
		Policy: platformconfig.PolicyConfig{
			Enabled:      true,
			DefaultModel: "abac",
			RBAC:         platformconfig.PolicyRBACConfig{Enabled: true},
			ABAC:         platformconfig.PolicyABACConfig{Enabled: false},
			ReBAC:        platformconfig.PolicyReBACConfig{Enabled: false},
		},
	}
	settings := defaultRuntimePolicySettings(cfg)
	if settings.DefaultModel != "rbac" {
		t.Fatalf("expected default model to fall back to enabled rbac, got %q", settings.DefaultModel)
	}

	cfg.Policy.DefaultModel = "custom"
	cfg.Policy.RBAC.Enabled = false
	cfg.Policy.ABAC.Enabled = false
	cfg.Policy.ReBAC.Enabled = false
	settings = defaultRuntimePolicySettings(cfg)
	if !settings.RBAC.Enabled {
		t.Fatalf("expected RBAC to be auto-enabled when policy is enabled with no models")
	}
	if settings.DefaultModel != "rbac" {
		t.Fatalf("expected default model to normalize to rbac, got %q", settings.DefaultModel)
	}

	enabled := runtimePolicySettings{}
	enabled.ABAC.Enabled = true
	if got := firstEnabledRuntimePolicyModel(enabled); got != "abac" {
		t.Fatalf("expected first enabled model to be abac, got %q", got)
	}
}

func TestRuntimeProxyHelpers(t *testing.T) {
	defaults := defaultRuntimeProxySettings(nil)
	if defaults.UpstreamTimeout != "30s" {
		t.Fatalf("expected default upstream timeout 30s, got %q", defaults.UpstreamTimeout)
	}
	if defaults.IdentitySignatureHeader != "X-Aegion-Signature" {
		t.Fatalf("expected default signature header, got %q", defaults.IdentitySignatureHeader)
	}
	if len(defaults.SignedIdentityHeaders) != 3 {
		t.Fatalf("expected 3 default signed identity headers, got %d", len(defaults.SignedIdentityHeaders))
	}

	cfg := &platformconfig.Config{
		Proxy: platformconfig.ProxyConfig{
			Enabled:                     true,
			UpstreamTimeout:             platformconfig.Duration(12 * time.Second),
			PreserveHost:                true,
			TrustForwardedHeaders:       true,
			StripInboundIdentityHeaders: true,
			IdentitySigningSecret:       "  keep-secret-value  ",
			IdentitySignatureHeader:     "X-Proxy-Sig",
			SignedIdentityHeaders:       []string{"X-User-ID"},
		},
	}
	proxySettings := defaultRuntimeProxySettings(cfg)
	if !proxySettings.Enabled || !proxySettings.PreserveHost || !proxySettings.TrustForwardedHeaders || !proxySettings.StripInboundIdentityHeaders {
		t.Fatalf("expected proxy boolean flags to carry from config")
	}
	if proxySettings.UpstreamTimeout != "12s" {
		t.Fatalf("expected upstream timeout 12s, got %q", proxySettings.UpstreamTimeout)
	}
	if proxySettings.IdentitySigningSecret != "keep-secret-value" {
		t.Fatalf("expected identity signing secret to be trimmed, got %q", proxySettings.IdentitySigningSecret)
	}
	if proxySettings.IdentitySignatureHeader != "X-Proxy-Sig" {
		t.Fatalf("expected identity signature header from config, got %q", proxySettings.IdentitySignatureHeader)
	}
	if len(proxySettings.SignedIdentityHeaders) != 1 || proxySettings.SignedIdentityHeaders[0] != "X-User-ID" {
		t.Fatalf("expected signed identity headers from config, got %#v", proxySettings.SignedIdentityHeaders)
	}

	base := runtimeProxySettings{
		UpstreamTimeout:         "45s",
		IdentitySignatureHeader: " X-Test-Sig ",
		SignedIdentityHeaders:   []string{"X-User-ID", "X-User-Session-ID"},
		IdentitySigningSecret:   "example-signing-secret",
	}
	if err := validateRuntimeProxySettings(base); err != nil {
		t.Fatalf("expected valid proxy settings, got %v", err)
	}

	cases := []struct {
		name   string
		mutate func(*runtimeProxySettings)
		want   string
	}{
		{
			name: "invalid duration",
			mutate: func(s *runtimeProxySettings) {
				s.UpstreamTimeout = "not-a-duration"
			},
			want: "upstream_timeout must be a valid duration",
		},
		{
			name: "missing signature header",
			mutate: func(s *runtimeProxySettings) {
				s.IdentitySignatureHeader = " "
			},
			want: "identity_signature_header cannot be empty",
		},
		{
			name: "missing signed headers",
			mutate: func(s *runtimeProxySettings) {
				s.SignedIdentityHeaders = nil
			},
			want: "signed_identity_headers cannot be empty",
		},
		{
			name: "blank signed header value",
			mutate: func(s *runtimeProxySettings) {
				s.SignedIdentityHeaders = []string{"X-User-ID", " "}
			},
			want: "signed_identity_headers cannot contain empty values",
		},
		{
			name: "short identity signing secret",
			mutate: func(s *runtimeProxySettings) {
				s.IdentitySigningSecret = "too-short"
			},
			want: "identity_signing_secret must be at least 16 characters when set",
		},
		{
			name: "trust forwarded headers without cidrs",
			mutate: func(s *runtimeProxySettings) {
				s.TrustForwardedHeaders = true
			},
			want: "trusted proxy CIDRs are required when trust_forwarded_headers is enabled",
		},
		{
			name: "missing signing secret when stripping disabled",
			mutate: func(s *runtimeProxySettings) {
				s.StripInboundIdentityHeaders = false
				s.IdentitySigningSecret = ""
			},
			want: "identity_signing_secret is required when strip_inbound_identity_headers is disabled",
		},
		{
			name: "production cannot disable strip inbound identity headers",
			mutate: func(s *runtimeProxySettings) {
				t.Setenv("AEGION_ENV", "production")
				s.StripInboundIdentityHeaders = false
			},
			want: "strip_inbound_identity_headers cannot be disabled in production",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			candidate := base
			tc.mutate(&candidate)
			err := validateRuntimeProxySettings(candidate)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected error containing %q, got %v", tc.want, err)
			}
		})
	}

	headers := []string{"X-User-ID", "X-User-Session-ID"}
	t.Setenv("AEGION_TRUSTED_PROXY_CIDRS", "198.51.100.0/24")
	patch := &runtimeProxySettingsPatch{
		Enabled:                     boolPtr(true),
		UpstreamTimeout:             stringPtr("60s"),
		PreserveHost:                boolPtr(true),
		TrustForwardedHeaders:       boolPtr(true),
		StripInboundIdentityHeaders: boolPtr(true),
		IdentitySigningSecret:       stringPtr("  example-signing-secret  "),
		IdentitySignatureHeader:     stringPtr(" X-Patched-Sig "),
		SignedIdentityHeaders:       &headers,
	}
	next, err := applyRuntimeProxyPatch(defaults, patch)
	if err != nil {
		t.Fatalf("expected valid patch application, got %v", err)
	}
	if !next.Enabled || !next.PreserveHost || !next.TrustForwardedHeaders || !next.StripInboundIdentityHeaders {
		t.Fatalf("expected proxy flags to be patched to true")
	}
	if next.UpstreamTimeout != "60s" {
		t.Fatalf("expected upstream timeout patch to be applied, got %q", next.UpstreamTimeout)
	}
	if next.IdentitySigningSecret != "example-signing-secret" {
		t.Fatalf("expected trimmed identity signing secret, got %q", next.IdentitySigningSecret)
	}
	if next.IdentitySignatureHeader != "X-Patched-Sig" {
		t.Fatalf("expected trimmed signature header, got %q", next.IdentitySignatureHeader)
	}

	headers[0] = "X-Mutated"
	if next.SignedIdentityHeaders[0] != "X-User-ID" {
		t.Fatalf("expected signed headers patch to copy input slice")
	}

	badPatch := &runtimeProxySettingsPatch{
		IdentitySignatureHeader: stringPtr(" "),
	}
	if _, err := applyRuntimeProxyPatch(defaults, badPatch); err == nil {
		t.Fatalf("expected invalid proxy patch to be rejected")
	}
}

func TestServerUtilityHelpers(t *testing.T) {
	if got := bootstrapDisplayName("alice@example.com"); got != "alice" {
		t.Fatalf("expected local-part display name alice, got %q", got)
	}
	if got := bootstrapDisplayName("operator"); got != "operator" {
		t.Fatalf("expected display name to preserve raw value, got %q", got)
	}
	if got := bootstrapDisplayName(""); got != "" {
		t.Fatalf("expected empty input to remain empty, got %q", got)
	}

	if got := parseSameSite("strict"); got != http.SameSiteStrictMode {
		t.Fatalf("expected strict same-site mode, got %v", got)
	}
	if got := parseSameSite("none"); got != http.SameSiteNoneMode {
		t.Fatalf("expected none same-site mode, got %v", got)
	}
	if got := parseSameSite("unexpected"); got != http.SameSiteLaxMode {
		t.Fatalf("expected fallback same-site lax mode, got %v", got)
	}
}
