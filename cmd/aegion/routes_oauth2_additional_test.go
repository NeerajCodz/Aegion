package main

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/aegion/aegion/core/registry"
)

func TestOIDCProxyHandlerMethodRestrictions(t *testing.T) {
	s := newTestServer(t)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/.well-known/jwks.json", nil)
	s.handleJWKS(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("handleJWKS method check expected %d got %d", http.StatusMethodNotAllowed, rec.Code)
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/.well-known/openid-configuration", nil)
	s.handleOIDCDiscovery(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("handleOIDCDiscovery method check expected %d got %d", http.StatusMethodNotAllowed, rec.Code)
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPut, "/oidc/userinfo", nil)
	s.handleOAuth2UserInfo(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("handleOAuth2UserInfo method check expected %d got %d", http.StatusMethodNotAllowed, rec.Code)
	}
}

func TestOAuth2EndpointURLBranches(t *testing.T) {
	t.Run("module missing", func(t *testing.T) {
		s := newTestServer(t)
		if _, err := s.oauth2EndpointURL("/oidc/userinfo"); err == nil {
			t.Fatal("oauth2EndpointURL(module missing) expected error")
		}
	})

	t.Run("module unhealthy", func(t *testing.T) {
		s := newTestServer(t)
		registerTestModule(t, s, "oauth2", registry.EndpointHTTP, "http://oauth2.example.com")
		if err := s.registry.UpdateStatus("oauth2", registry.StatusUnhealthy); err != nil {
			t.Fatalf("UpdateStatus(unhealthy) = %v", err)
		}
		if _, err := s.oauth2EndpointURL("/oidc/userinfo"); err == nil {
			t.Fatal("oauth2EndpointURL(unhealthy) expected error")
		}
	})

	t.Run("no http endpoint", func(t *testing.T) {
		s := newTestServer(t)
		registerTestModule(t, s, "oauth2", registry.EndpointGRPC, "grpc://oauth2.example.com")
		if _, err := s.oauth2EndpointURL("/oidc/userinfo"); err == nil {
			t.Fatal("oauth2EndpointURL(no http endpoint) expected error")
		}
	})

	t.Run("invalid parse", func(t *testing.T) {
		s := newTestServer(t)
		registerTestModule(t, s, "oauth2", registry.EndpointHTTP, "://bad")
		if _, err := s.oauth2EndpointURL("/oidc/userinfo"); err == nil {
			t.Fatal("oauth2EndpointURL(parse error) expected error")
		}
	})

	t.Run("invalid scheme", func(t *testing.T) {
		s := newTestServer(t)
		registerTestModule(t, s, "oauth2", registry.EndpointHTTP, "ftp://oauth2.example.com")
		if _, err := s.oauth2EndpointURL("/oidc/userinfo"); err == nil {
			t.Fatal("oauth2EndpointURL(scheme error) expected error")
		}
	})

	t.Run("missing host", func(t *testing.T) {
		s := newTestServer(t)
		registerTestModule(t, s, "oauth2", registry.EndpointHTTP, "http:///missing-host")
		if _, err := s.oauth2EndpointURL("/oidc/userinfo"); err == nil {
			t.Fatal("oauth2EndpointURL(host error) expected error")
		}
	})

	t.Run("success", func(t *testing.T) {
		s := newTestServer(t)
		registerTestModule(t, s, "oauth2", registry.EndpointHTTP, "http://oauth2.example.com")
		u, err := s.oauth2EndpointURL("/oidc/userinfo")
		if err != nil {
			t.Fatalf("oauth2EndpointURL(success) err=%v", err)
		}
		if got := u.String(); got != "http://oauth2.example.com/oidc/userinfo" {
			t.Fatalf("oauth2EndpointURL(success) got %q", got)
		}
	})
}

func TestProxyOAuth2EndpointUpstreamFailure(t *testing.T) {
	s := newTestServer(t)
	registerTestModule(t, s, "oauth2", registry.EndpointHTTP, "http://127.0.0.1:1")
	if err := s.registry.UpdateStatus("oauth2", registry.StatusHealthy); err != nil {
		t.Fatalf("UpdateStatus(healthy) = %v", err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/.well-known/jwks.json", nil)
	s.handleJWKS(rec, req)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("proxy upstream failure expected %d got %d", http.StatusBadGateway, rec.Code)
	}
}

