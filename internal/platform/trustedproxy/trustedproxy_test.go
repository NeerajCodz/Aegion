package trustedproxy

import (
	"net/http/httptest"
	"testing"
)

func TestClientIPTrustsOnlyAllowlistedPeers(t *testing.T) {
	t.Setenv("AEGION_TEST_TRUSTED_PROXY_CIDRS", "192.0.2.0/24")

	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "192.0.2.10:1234"
	req.Header.Set("X-Forwarded-For", "198.51.100.9")
	if got := ClientIP(req, true, "AEGION_TEST_TRUSTED_PROXY_CIDRS"); got != "198.51.100.9" {
		t.Fatalf("expected forwarded IP from trusted peer, got %q", got)
	}

	req.RemoteAddr = "203.0.113.10:1234"
	if got := ClientIP(req, true, "AEGION_TEST_TRUSTED_PROXY_CIDRS"); got != "203.0.113.10" {
		t.Fatalf("expected direct peer IP from untrusted remote, got %q", got)
	}
}
