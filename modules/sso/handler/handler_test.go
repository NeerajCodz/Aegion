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

type captureSSOService struct {
	stubSSOService
	relayState  string
	subject     string
	email       string
	displayName string
	attributes  map[string]interface{}
}

func (s *captureSSOService) CompleteAuth(_ context.Context, _ string, relayState, subject, email, displayName string, attributes map[string]interface{}) (*service.CallbackResult, error) {
	s.relayState = relayState
	s.subject = subject
	s.email = email
	s.displayName = displayName
	s.attributes = attributes
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

func TestHandleCallbackIgnoresUntrustedIdentityFields(t *testing.T) {
	svc := &captureSSOService{}
	h := New(svc)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodPost, "/self-service/sso/acme/callback?state=relay-state&subject=attacker&email=attacker@example.com&display_name=attacker", bytes.NewBufferString("attributes=%7B%22subject%22%3A%22attacker%22%7D&SAMLResponse=fake"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("expected %d, got %d", http.StatusSeeOther, rec.Code)
	}
	if svc.relayState != "relay-state" {
		t.Fatalf("expected relay state to be forwarded, got %q", svc.relayState)
	}
	if svc.subject != "" || svc.email != "" || svc.displayName != "" {
		t.Fatalf("expected callback identity fields to be stripped, got subject=%q email=%q display_name=%q", svc.subject, svc.email, svc.displayName)
	}
	if got := svc.attributes["_saml_response"]; got != "fake" {
		t.Fatalf("expected SAML response to be forwarded, got %+v", got)
	}
	recipients, ok := svc.attributes["_expected_recipients"].([]string)
	if !ok || len(recipients) == 0 {
		t.Fatalf("expected callback recipients to be forwarded, got %+v", svc.attributes["_expected_recipients"])
	}
	if got := svc.attributes["attributes"]; got != nil {
		t.Fatalf("expected untrusted attributes to be dropped, got %+v", got)
	}
}
