package trustedproxy

import (
	"net/http/httptest"
	"testing"
)

func TestTrustedProxyAdditionalBranches(t *testing.T) {
	t.Run("remote ip empty and bracket fallback", func(t *testing.T) {
		if got := RemoteIP("  "); got != "" {
			t.Fatalf("RemoteIP(empty) = %q", got)
		}
		if got := RemoteIP("[2001:db8::1]"); got != "2001:db8::1" {
			t.Fatalf("RemoteIP(bracketed host) = %q", got)
		}
	})

	t.Run("canTrustForwardedHeaders guard branches", func(t *testing.T) {
		if canTrustForwardedHeaders(nil, true, "AEGION_TEST_TRUSTED_PROXY_CIDRS") {
			t.Fatal("expected false for nil request")
		}
		req := httptest.NewRequest("GET", "/", nil)
		if canTrustForwardedHeaders(req, false, "AEGION_TEST_TRUSTED_PROXY_CIDRS") {
			t.Fatal("expected false when trustForwarded is disabled")
		}
		req.RemoteAddr = ""
		if canTrustForwardedHeaders(req, true, "AEGION_TEST_TRUSTED_PROXY_CIDRS") {
			t.Fatal("expected false when peer is empty")
		}

		req.RemoteAddr = "not-an-ip:1234"
		if canTrustForwardedHeaders(req, true, "AEGION_TEST_TRUSTED_PROXY_CIDRS") {
			t.Fatal("expected false for invalid peer ip")
		}

		t.Setenv("AEGION_TEST_TRUSTED_PROXY_CIDRS", "")
		req.RemoteAddr = "192.0.2.5:8080"
		if canTrustForwardedHeaders(req, true, "AEGION_TEST_TRUSTED_PROXY_CIDRS") {
			t.Fatal("expected false when trusted cidr list is empty")
		}
	})

	t.Run("parse cidrs skips empty and invalid entries", func(t *testing.T) {
		parsed := parseCIDRs(" ,invalid,192.0.2.0/24, ")
		if len(parsed) != 1 || parsed[0].String() != "192.0.2.0/24" {
			t.Fatalf("unexpected parsed cidrs: %#v", parsed)
		}
	})
}
