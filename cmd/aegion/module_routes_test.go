package main

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/aegion/aegion/core/registry"
	"github.com/aegion/aegion/internal/platform/config"
	"github.com/stretchr/testify/require"
)

func TestModuleRouteTableUsesResolvedExternalModules(t *testing.T) {
	plan, err := config.ResolveModulePlan(&config.Config{ModuleVersions: map[string]string{
		"oauth2": "v1.2.3",
		"policy": "v1.2.3",
	}})
	require.NoError(t, err)

	table, err := NewModuleRouteTable(plan)
	require.NoError(t, err)

	moduleID, ok := table.Match("GET", "/.well-known/jwks.json")
	require.True(t, ok)
	require.Equal(t, "oauth2", moduleID)

	moduleID, ok = table.Match("POST", "/oauth2/token")
	require.True(t, ok)
	require.Equal(t, "oauth2", moduleID)

	_, ok = table.Match("POST", "/.well-known/jwks.json")
	require.False(t, ok)
	_, ok = table.Match("GET", "/internal/registry/register")
	require.False(t, ok)
}

func TestModuleRouteTableExcludesDisabledModuleRoutes(t *testing.T) {
	plan, err := config.ResolveModulePlan(&config.Config{ModuleVersions: map[string]string{"oauth2": "off"}})
	require.NoError(t, err)

	table, err := NewModuleRouteTable(plan)
	require.NoError(t, err)
	_, ok := table.Match("GET", "/.well-known/jwks.json")
	require.False(t, ok)
}

func TestSetupRoutesForwardsOnlyOwnedModulePaths(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/oauth2/token" {
			t.Fatalf("expected original module path, got %q", r.URL.Path)
		}
		if got := r.Header.Get("X-User-ID"); got != "" {
			t.Fatalf("expected spoofed identity header to be stripped, got %q", got)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(target.Close)

	server := newTestServer(t)
	server.cfg.Proxy.Enabled = true
	server.cfg.Proxy.StripInboundIdentityHeaders = true
	server.cfg.Proxy.IdentitySigningSecret = "test-module-identity-signing-secret"
	_, err := server.registry.Register(registry.RegistrationRequest{
		ID:      "oauth2",
		Name:    "oauth2",
		Version: "v1.0.0",
		Endpoints: []registry.Endpoint{{
			Type: registry.EndpointHTTP,
			URL:  target.URL,
		}},
		HealthURL: target.URL + "/health",
	})
	require.NoError(t, err)

	handler := SetupRoutes(server)
	request := httptest.NewRequest(http.MethodPost, "/oauth2/token", nil)
	request.Header.Set("X-User-ID", "spoofed-user")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	require.Equal(t, http.StatusNoContent, response.Code)

	response = httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/not-a-module-route", nil))
	require.Equal(t, http.StatusNotFound, response.Code)
}
