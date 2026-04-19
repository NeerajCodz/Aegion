package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/aegion/aegion/core/authtoken"
	"github.com/aegion/aegion/core/registry"
)

func TestModuleRegistrationPayloadIDRequirements(t *testing.T) {
	s := newTestServer(t)

	t.Run("register requires payload module id", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/internal/registry/register", mustJSONBody(t, registry.RegistrationRequest{
			ID:      " ",
			Name:    "password",
			Version: "v1.0.0",
			Endpoints: []registry.Endpoint{
				{Type: registry.EndpointHTTP, URL: "http://localhost:9000"},
			},
			HealthURL: "http://localhost:9000/health",
		}))
		req = req.WithContext(context.WithValue(req.Context(), authtoken.ContextKeyModuleID, "password"))
		rec := httptest.NewRecorder()
		s.handleModuleRegister(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected %d, got %d", http.StatusBadRequest, rec.Code)
		}
	})

	t.Run("deregister requires payload module id", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/internal/registry/deregister", mustJSONBody(t, registry.DeregistrationRequest{
			ModuleID: " ",
		}))
		req = req.WithContext(context.WithValue(req.Context(), authtoken.ContextKeyModuleID, "password"))
		rec := httptest.NewRecorder()
		s.handleModuleDeregister(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected %d, got %d", http.StatusBadRequest, rec.Code)
		}
	})
}

func TestValidateRuntimePolicySettingsReBACEnabled(t *testing.T) {
	settings := runtimePolicySettings{
		Enabled:      true,
		DefaultModel: "rebac",
	}
	settings.ReBAC.Enabled = true

	if err := validateRuntimePolicySettings(settings); err != nil {
		t.Fatalf("expected rebac-enabled settings to validate, got %v", err)
	}
}
