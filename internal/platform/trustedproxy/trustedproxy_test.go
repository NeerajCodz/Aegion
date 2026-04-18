package trustedproxy

import (
	"crypto/tls"
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

func TestForwardedAccessorsAndFallbacks(t *testing.T) {
	t.Setenv("AEGION_TEST_TRUSTED_PROXY_CIDRS", "192.0.2.0/24")

	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "192.0.2.42:8080"
	req.Host = "app.example.com"
	req.Header.Set("X-Forwarded-For", "198.51.100.10, 198.51.100.11")
	req.Header.Set("X-Forwarded-Proto", "https")
	req.Header.Set("X-Forwarded-Host", "proxy.example.com")
	req.Header.Set("X-Real-IP", "198.51.100.12")
	req.Header.Set("CF-Connecting-IP", "198.51.100.13")

	if got := PriorForwardedFor(req, true, "AEGION_TEST_TRUSTED_PROXY_CIDRS"); got != "198.51.100.10, 198.51.100.11" {
		t.Fatalf("unexpected prior forwarded-for: %q", got)
	}
	if got := ForwardedProto(req, true, "AEGION_TEST_TRUSTED_PROXY_CIDRS"); got != "https" {
		t.Fatalf("unexpected forwarded proto: %q", got)
	}
	if got := ForwardedHost(req, true, "AEGION_TEST_TRUSTED_PROXY_CIDRS"); got != "proxy.example.com" {
		t.Fatalf("unexpected forwarded host: %q", got)
	}
	if got := ClientIP(req, true, "AEGION_TEST_TRUSTED_PROXY_CIDRS"); got != "198.51.100.10" {
		t.Fatalf("expected first forwarded-for IP, got %q", got)
	}

	xffOnlySpaces := httptest.NewRequest("GET", "/", nil)
	xffOnlySpaces.RemoteAddr = "192.0.2.45:8080"
	xffOnlySpaces.Header.Set("X-Forwarded-For", "  ")
	xffOnlySpaces.Header.Set("X-Real-IP", "198.51.100.20")
	if got := ClientIP(xffOnlySpaces, true, "AEGION_TEST_TRUSTED_PROXY_CIDRS"); got != "198.51.100.20" {
		t.Fatalf("expected x-real-ip fallback, got %q", got)
	}

	realOnlySpaces := httptest.NewRequest("GET", "/", nil)
	realOnlySpaces.RemoteAddr = "192.0.2.46:8080"
	realOnlySpaces.Header.Set("X-Real-IP", "  ")
	realOnlySpaces.Header.Set("CF-Connecting-IP", "198.51.100.21")
	if got := ClientIP(realOnlySpaces, true, "AEGION_TEST_TRUSTED_PROXY_CIDRS"); got != "198.51.100.21" {
		t.Fatalf("expected CF-Connecting-IP fallback, got %q", got)
	}

	noForwarded := httptest.NewRequest("GET", "/", nil)
	noForwarded.RemoteAddr = "192.0.2.47:8080"
	noForwarded.TLS = &tls.ConnectionState{}
	if got := ForwardedProto(noForwarded, true, "AEGION_TEST_TRUSTED_PROXY_CIDRS"); got != "https" {
		t.Fatalf("expected https proto fallback for TLS request, got %q", got)
	}
	if got := ForwardedHost(noForwarded, true, "AEGION_TEST_TRUSTED_PROXY_CIDRS"); got != noForwarded.Host {
		t.Fatalf("expected host fallback, got %q", got)
	}
}

func TestForwardedAccessorsHandleUntrustedAndNilRequests(t *testing.T) {
	t.Setenv("AEGION_TEST_TRUSTED_PROXY_CIDRS", "192.0.2.0/24")

	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "203.0.113.9:1234"
	req.Host = "direct.example.com"
	req.Header.Set("X-Forwarded-For", "198.51.100.1")
	req.Header.Set("X-Forwarded-Proto", "https")
	req.Header.Set("X-Forwarded-Host", "proxy.example.com")

	if got := PriorForwardedFor(req, true, "AEGION_TEST_TRUSTED_PROXY_CIDRS"); got != "" {
		t.Fatalf("expected no prior forwarded-for from untrusted peer, got %q", got)
	}
	if got := ForwardedProto(req, true, "AEGION_TEST_TRUSTED_PROXY_CIDRS"); got != "http" {
		t.Fatalf("expected http proto fallback from untrusted peer, got %q", got)
	}
	if got := ForwardedHost(req, true, "AEGION_TEST_TRUSTED_PROXY_CIDRS"); got != "direct.example.com" {
		t.Fatalf("expected direct host fallback from untrusted peer, got %q", got)
	}

	if got := PriorForwardedFor(nil, true, "AEGION_TEST_TRUSTED_PROXY_CIDRS"); got != "" {
		t.Fatalf("expected empty forwarded-for for nil request, got %q", got)
	}
	if got := ForwardedHost(nil, true, "AEGION_TEST_TRUSTED_PROXY_CIDRS"); got != "" {
		t.Fatalf("expected empty host for nil request, got %q", got)
	}
	if got := ForwardedProto(nil, true, "AEGION_TEST_TRUSTED_PROXY_CIDRS"); got != "http" {
		t.Fatalf("expected http for nil request, got %q", got)
	}
	if got := ClientIP(nil, true, "AEGION_TEST_TRUSTED_PROXY_CIDRS"); got != "" {
		t.Fatalf("expected empty client IP for nil request, got %q", got)
	}
}
