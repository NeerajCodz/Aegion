package main

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/aegion/aegion/core/registry"
)

func registerOAuth2EndpointFixture(t *testing.T, s *Server, endpointType registry.EndpointType, endpointURL string) {
	t.Helper()
	_, err := s.registry.Register(registry.RegistrationRequest{
		ID:        "oauth2",
		Name:      "oauth2",
		Version:   "v1.0.0",
		Endpoints: []registry.Endpoint{{Type: endpointType, URL: endpointURL}},
	})
	if err != nil {
		t.Fatalf("failed to register module: %v", err)
	}
}

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

func TestProxyOAuth2EndpointUpstreamFailure(t *testing.T) {
	s := newTestServer(t)
	registerTestModule(t, s, "oauth2", registry.EndpointHTTP, "http://127.0.0.1:1")
	if err := s.registry.UpdateStatus("oauth2", registry.StatusHealthy); err != nil {
		t.Fatalf("UpdateStatus(healthy) = %v", err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/.well-known/jwks.json", nil)
	s.handleJWKS(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("proxy upstream failure expected %d got %d", http.StatusServiceUnavailable, rec.Code)
	}
}
