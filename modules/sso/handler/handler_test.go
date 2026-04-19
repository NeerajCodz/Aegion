package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/aegion/aegion/modules/sso/service"
	"github.com/aegion/aegion/modules/sso/store"
)

type stubSSOService struct{}

func (stubSSOService) ListConnections(context.Context) ([]store.Connection, error) {
	return []store.Connection{{Slug: "acme", DisplayName: "Acme", Enabled: true}}, nil
}
func (stubSSOService) ListConfiguredConnections(context.Context, bool) ([]store.Connection, error) {
	return []store.Connection{{Slug: "acme", DisplayName: "Acme", Enabled: true}}, nil
}
func (stubSSOService) GetConnection(context.Context, string) (*store.Connection, error) {
	return &store.Connection{Slug: "acme", DisplayName: "Acme", Enabled: true}, nil
}
func (stubSSOService) GetConnectionForDomain(context.Context, string) (*store.Connection, error) {
	return &store.Connection{Slug: "acme", DisplayName: "Acme", Enabled: true}, nil
}
func (stubSSOService) UpsertConnection(context.Context, service.ConnectionUpsertRequest) (*store.Connection, error) {
	return &store.Connection{Slug: "acme", DisplayName: "Acme", Enabled: true}, nil
}
func (stubSSOService) DeleteConnection(context.Context, string) error { return nil }
func (stubSSOService) StartAuth(context.Context, string, string) (*service.StartResponse, error) {
	return &service.StartResponse{Connection: "acme", RedirectURL: "https://idp.example.com", RelayState: "relay"}, nil
}
func (stubSSOService) CompleteAuth(context.Context, string, string, string, string, string, map[string]interface{}) (*service.CallbackResult, error) {
	return &service.CallbackResult{Connection: "acme", Subject: "sub", RedirectTo: "/after"}, nil
}

func TestSSOHandlersRequireManagementToken(t *testing.T) {
	h := New(stubSSOService{})
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/sso/admin/connections", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected %d, got %d", http.StatusServiceUnavailable, rec.Code)
	}
}

func TestSSOHandlersServePublicAndAdminRoutes(t *testing.T) {
	h := New(stubSSOService{}, Config{ManagementToken: "secret"})
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	authHeader := http.Header{}
	authHeader.Set("Authorization", "Bearer secret")

	t.Run("public list", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/sso/connections", nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected %d, got %d", http.StatusOK, rec.Code)
		}
	})

	t.Run("resolve domain", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/sso/resolve-domain?domain=example.com", nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected %d, got %d", http.StatusOK, rec.Code)
		}
	})

	t.Run("admin save", func(t *testing.T) {
		body, _ := json.Marshal(service.ConnectionUpsertRequest{Slug: "acme", DisplayName: "Acme", EntityID: "urn:acme", SSOURL: "https://idp.example.com"})
		req := httptest.NewRequest(http.MethodPost, "/api/v1/sso/admin/connections", bytes.NewReader(body))
		req.Header = authHeader.Clone()
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected %d, got %d", http.StatusOK, rec.Code)
		}
	})

	t.Run("start", func(t *testing.T) {
		body, _ := json.Marshal(map[string]string{"redirect_to": "/after"})
		req := httptest.NewRequest(http.MethodPost, "/self-service/sso/acme/start", bytes.NewReader(body))
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected %d, got %d", http.StatusOK, rec.Code)
		}
	})

	t.Run("callback rejects caller supplied identity", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/self-service/sso/acme/callback?RelayState=relay&subject=attacker", nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected %d, got %d", http.StatusBadRequest, rec.Code)
		}
	})
}
