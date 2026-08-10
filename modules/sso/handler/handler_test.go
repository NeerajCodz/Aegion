package handler

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	platformcrypto "github.com/aegion/aegion/internal/platform/crypto"

	"github.com/aegion/aegion/modules/sso/service"
	"github.com/aegion/aegion/modules/sso/store"
)

const testIdentityContextSecret = "01234567890123456789012345678901"

func authenticatedIdentityHeaders(t *testing.T) http.Header {
	return signedIdentityHeadersAt(t, time.Now().UTC())
}

func signedIdentityHeadersAt(t *testing.T, at time.Time) http.Header {
	t.Helper()
	headers := make(http.Header, len(signedIdentityHeaders)+1)
	headers.Set("X-User-ID", "identity-1")
	headers.Set("X-User-Session-ID", "session-1")
	headers.Set("X-User-AAL", "aal2")
	signature, err := platformcrypto.SignIdentityHeaders(
		[]byte(testIdentityContextSecret),
		headers,
		signedIdentityHeaders,
		at,
	)
	if err != nil {
		t.Fatalf("sign identity headers: %v", err)
	}
	headers.Set("X-Aegion-Signature", signature)
	return headers
}

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

type unsafeRedirectSSOService struct{ stubSSOService }

func (unsafeRedirectSSOService) CompleteAuth(context.Context, string, string, string, string, string, map[string]interface{}) (*service.CallbackResult, error) {
	return &service.CallbackResult{
		Connection: "acme",
		Subject:    "sub",
		Email:      "user@example.com",
		RedirectTo: "https://attacker.example/post-login",
	}, nil
}

func (s *captureSSOService) CompleteAuth(_ context.Context, _ string, relayState, subject, email, displayName string, attributes map[string]interface{}) (*service.CallbackResult, error) {
	s.relayState = relayState
	s.subject = subject
	s.email = email
	s.displayName = displayName
	s.attributes = attributes
	return &service.CallbackResult{Connection: "acme", Subject: "sub", RedirectTo: "/after"}, nil
}

func TestSSOManagementRequiresVerifiedIdentityContext(t *testing.T) {
	t.Run("missing signing secret disables management", func(t *testing.T) {
		h := New(stubSSOService{})
		mux := http.NewServeMux()
		h.RegisterRoutes(mux)
		req := httptest.NewRequest(http.MethodGet, "/api/v1/sso/admin/connections", nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("expected %d, got %d", http.StatusServiceUnavailable, rec.Code)
		}
	})

	t.Run("unsigned or expired identity context is rejected", func(t *testing.T) {
		h := New(stubSSOService{}, Config{IdentityContextSecret: []byte(testIdentityContextSecret)})
		mux := http.NewServeMux()
		h.RegisterRoutes(mux)

		unsigned := httptest.NewRequest(http.MethodGet, "/api/v1/sso/admin/connections", nil)
		unsigned.Header.Set("X-User-ID", "identity-1")
		unsigned.Header.Set("X-User-Session-ID", "session-1")
		unsigned.Header.Set("X-User-AAL", "aal2")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, unsigned)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("unsigned context status = %d, want %d", rec.Code, http.StatusUnauthorized)
		}

		now := time.Now().UTC()
		h.now = func() time.Time { return now }
		expired := httptest.NewRequest(http.MethodGet, "/api/v1/sso/admin/connections", nil)
		expired.Header = signedIdentityHeadersAt(t, now.Add(-2*time.Minute))
		rec = httptest.NewRecorder()
		mux.ServeHTTP(rec, expired)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expired context status = %d, want %d", rec.Code, http.StatusUnauthorized)
		}
	})
}

func TestSSOHandlersServePublicAndAdminRoutes(t *testing.T) {
	h := New(stubSSOService{}, Config{IdentityContextSecret: []byte(testIdentityContextSecret)})
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
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
		req.Header = authenticatedIdentityHeaders(t)
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected %d, got %d", http.StatusOK, rec.Code)
		}
	})

	t.Run("start", func(t *testing.T) {
		body, _ := json.Marshal(map[string]string{"redirect_to": "/after"})
		req := httptest.NewRequest(http.MethodPost, "/self-service/sso/acme/start", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected %d, got %d", http.StatusOK, rec.Code)
		}
	})

	t.Run("generic API protocol route is not exposed", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/sso/acme/start", nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected %d, got %d", http.StatusNotFound, rec.Code)
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

	t.Run("callback enforces browser-safe local redirect", func(t *testing.T) {
		h := New(unsafeRedirectSSOService{}, Config{IdentityContextSecret: []byte(testIdentityContextSecret)})
		mux := http.NewServeMux()
		h.RegisterRoutes(mux)

		req := httptest.NewRequest(http.MethodGet, "/self-service/sso/acme/callback?RelayState=relay", nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusSeeOther {
			t.Fatalf("expected %d, got %d", http.StatusSeeOther, rec.Code)
		}
		if location := rec.Header().Get("Location"); !strings.HasPrefix(location, "/") {
			t.Fatalf("expected local redirect target, got %q", location)
		}
	})
}

func TestHandleCallbackRejectsUntrustedIdentityFields(t *testing.T) {
	svc := &captureSSOService{}
	h := New(svc)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodPost, "/self-service/sso/acme/callback?state=relay-state&subject=attacker&email=attacker@example.com&display_name=attacker", bytes.NewBufferString("attributes=%7B%22subject%22%3A%22attacker%22%7D&SAMLResponse=fake"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected %d, got %d", http.StatusBadRequest, rec.Code)
	}
	if svc.relayState != "" || svc.attributes != nil {
		t.Fatalf("expected callback to be rejected before service invocation, got relay_state=%q attrs=%+v", svc.relayState, svc.attributes)
	}
}

func TestExpectedRecipientsTrustForwardedHeaders(t *testing.T) {
	t.Setenv("AEGION_TRUSTED_PROXY_CIDRS", "198.51.100.0/24")

	t.Run("ignores forwarded headers when trust disabled", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "https://service.example.com/self-service/sso/acme/callback", nil)
		req.Host = "service.example.com"
		req.RemoteAddr = "198.51.100.10:1234"
		req.TLS = &tls.ConnectionState{}
		req.Header.Set("X-Forwarded-Host", "attacker.example")
		req.Header.Set("X-Forwarded-Proto", "https")

		recipients := expectedRecipients(req, false)
		if containsRecipient(recipients, "https://attacker.example/self-service/sso/acme/callback") {
			t.Fatalf("expected forwarded host to be ignored when trust is disabled")
		}
		if !containsRecipient(recipients, "https://service.example.com/self-service/sso/acme/callback") {
			t.Fatalf("expected local host recipient when trust is disabled, got %v", recipients)
		}
	})

	t.Run("uses forwarded headers when trusted", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "http://service.example.com/self-service/sso/acme/callback", nil)
		req.Host = "service.example.com"
		req.RemoteAddr = "198.51.100.10:1234"
		req.Header.Set("X-Forwarded-Host", "sso.example.com")
		req.Header.Set("X-Forwarded-Proto", "https")

		recipients := expectedRecipients(req, true)
		if !containsRecipient(recipients, "https://sso.example.com/self-service/sso/acme/callback") {
			t.Fatalf("expected forwarded host recipient when trusted, got %v", recipients)
		}
	})

	t.Run("falls back when proxy untrusted", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "https://service.example.com/self-service/sso/acme/callback", nil)
		req.Host = "service.example.com"
		req.RemoteAddr = "203.0.113.10:1234"
		req.TLS = &tls.ConnectionState{}
		req.Header.Set("X-Forwarded-Host", "sso.example.com")
		req.Header.Set("X-Forwarded-Proto", "https")

		recipients := expectedRecipients(req, true)
		if containsRecipient(recipients, "https://sso.example.com/self-service/sso/acme/callback") {
			t.Fatalf("expected forwarded host to be ignored when proxy is untrusted")
		}
		if !containsRecipient(recipients, "https://service.example.com/self-service/sso/acme/callback") {
			t.Fatalf("expected local host recipient when proxy is untrusted, got %v", recipients)
		}
	})
}

func containsRecipient(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
