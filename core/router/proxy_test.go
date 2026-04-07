package router

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"

	"github.com/aegion/aegion/core/registry"
	"github.com/aegion/aegion/core/session"
	policypb "github.com/aegion/aegion/internal/proto/policy/v1"
)

type stubPolicyChecker struct {
	resp *policypb.CheckResponse
	err  error

	lastReq *policypb.CheckRequest
}

func (s *stubPolicyChecker) Check(ctx context.Context, req *policypb.CheckRequest) (*policypb.CheckResponse, error) {
	s.lastReq = req
	if s.err != nil {
		return nil, s.err
	}
	if s.resp != nil {
		return s.resp, nil
	}
	return &policypb.CheckResponse{Allowed: true, ModelUsed: "rbac", EvalPath: []string{"rbac:allow"}}, nil
}

func mustRegisterModule(t *testing.T, reg *registry.Registry, req registry.RegistrationRequest) {
	t.Helper()
	if _, err := reg.Register(req); err != nil {
		t.Fatalf("failed to register module: %v", err)
	}
}

func TestModuleProxyHelpersAndEndpointSelection(t *testing.T) {
	reg := registry.New(registry.DefaultConfig())
	defer reg.Stop()

	proxy := NewModuleProxy(ModuleProxyConfig{
		Registry:      reg,
		ModuleID:      "password",
		InternalToken: "internal-token",
		SessionSecret: []byte("test-session-secret"),
		Timeout:       10 * time.Millisecond,
		Logger:        zerolog.Nop(),
	})

	if _, err := proxy.getModuleEndpoint(context.Background()); !errors.Is(err, ErrModuleUnavailable) {
		t.Fatalf("expected ErrModuleUnavailable for missing module, got %v", err)
	}

	mustRegisterModule(t, reg, registry.RegistrationRequest{
		ID:      "password",
		Name:    "password",
		Version: "v1",
		Endpoints: []registry.Endpoint{
			{Type: registry.EndpointHTTP, URL: "http://127.0.0.1:18081"},
		},
		HealthURL: "http://127.0.0.1:18081/health",
	})

	endpoint, err := proxy.getModuleEndpoint(context.Background())
	if err != nil {
		t.Fatalf("getModuleEndpoint returned error: %v", err)
	}
	if endpoint.String() != "http://127.0.0.1:18081" {
		t.Fatalf("unexpected endpoint: %s", endpoint.String())
	}

	if err := reg.UpdateStatus("password", registry.StatusUnhealthy); err != nil {
		t.Fatalf("update status: %v", err)
	}
	if _, err := proxy.getModuleEndpoint(context.Background()); !errors.Is(err, ErrModuleUnavailable) {
		t.Fatalf("expected ErrModuleUnavailable for unhealthy module, got %v", err)
	}

	if err := reg.UpdateStatus("password", registry.StatusHealthy); err != nil {
		t.Fatalf("update status: %v", err)
	}

	_, _ = reg.Deregister("password")
	mustRegisterModule(t, reg, registry.RegistrationRequest{
		ID:      "password",
		Name:    "password",
		Version: "v1",
		Endpoints: []registry.Endpoint{
			{Type: registry.EndpointGRPC, URL: "grpc://127.0.0.1:19090"},
		},
		HealthURL: "http://127.0.0.1:18081/health",
	})
	if _, err := proxy.getModuleEndpoint(context.Background()); !errors.Is(err, ErrNoHealthyEndpoint) {
		t.Fatalf("expected ErrNoHealthyEndpoint, got %v", err)
	}
}

func TestModuleProxyDirectorAndHeaders(t *testing.T) {
	proxy := NewModuleProxy(ModuleProxyConfig{
		InternalToken:               "module-token",
		SessionSecret:               []byte("module-secret"),
		IdentitySigningSecret:       []byte("identity-signing-secret"),
		StripInboundIdentityHeaders: true,
		ModuleID:                    "admin",
		Logger:                      zerolog.Nop(),
	})
	proxy.now = func() time.Time {
		return time.Unix(1742912521, 0).UTC()
	}

	target, _ := url.Parse("http://module.local/base")
	original := httptest.NewRequest(http.MethodPost, "http://gateway.local/module/path?q=1", strings.NewReader("payload"))
	original.RemoteAddr = "198.51.100.4:1234"
	original.Header.Set("X-Forwarded-For", "203.0.113.10")
	original.Header.Set("X-Forwarded-Proto", "https")
	original.Header.Set("X-Forwarded-Host", "gateway.example.com")
	original.Header.Set("X-User-ID", "spoofed")
	original = original.WithContext(context.WithValue(original.Context(), contextKeyRequestID, "req-123"))

	// Add a session to ensure session headers are injected.
	sess := &session.Session{
		ID:         uuid.New(),
		IdentityID: uuid.New(),
		ExpiresAt:  time.Now().Add(5 * time.Minute),
	}
	original = original.WithContext(session.WithSession(original.Context(), sess))

	req := httptest.NewRequest(http.MethodPost, "http://placeholder/module/path?drop=true", strings.NewReader("payload"))
	req = req.WithContext(original.Context())

	proxy.director(target, original)(req)

	if req.URL.Host != "module.local" || req.URL.Scheme != "http" {
		t.Fatalf("expected rewritten URL host/scheme, got %s", req.URL.String())
	}
	if req.URL.Path != "/base/module/path" {
		t.Fatalf("expected rewritten path /base/module/path, got %s", req.URL.Path)
	}
	if req.URL.RawQuery != "q=1" {
		t.Fatalf("expected original query q=1, got %s", req.URL.RawQuery)
	}
	if req.Host != "module.local" {
		t.Fatalf("expected host header module.local, got %s", req.Host)
	}
	if req.Header.Get("X-Aegion-Internal-Token") != "module-token" {
		t.Fatalf("expected internal token header")
	}
	if req.Header.Get("X-Request-ID") != "req-123" {
		t.Fatalf("expected forwarded request id")
	}
	if req.Header.Get("X-Forwarded-For") != "203.0.113.10, 198.51.100.4" {
		t.Fatalf("unexpected x-forwarded-for: %q", req.Header.Get("X-Forwarded-For"))
	}
	if req.Header.Get("X-Forwarded-Proto") != "https" {
		t.Fatalf("expected forwarded proto https")
	}
	if req.Header.Get("X-Forwarded-Host") != "gateway.example.com" {
		t.Fatalf("expected forwarded host from original header")
	}
	if req.Header.Get(session.HeaderPrefix+"Session-ID") == "" || req.Header.Get(session.HeaderPrefix+"Signature") == "" {
		t.Fatalf("expected session headers to be injected")
	}
	if req.Header.Get("X-User-ID") != sess.IdentityID.String() {
		t.Fatalf("expected canonical user id header to be injected")
	}
	if req.Header.Get("X-User-Session-ID") != sess.ID.String() {
		t.Fatalf("expected canonical session id header to be injected")
	}
	if req.Header.Get("X-User-AAL") != string(sess.AAL) {
		t.Fatalf("expected canonical AAL header to be injected")
	}
	if sig := req.Header.Get("X-Aegion-Signature"); sig == "" {
		t.Fatalf("expected signed identity header")
	}

	expectedCanonical := "x-user-id:" + sess.IdentityID.String() + "\n" +
		"x-user-session-id:" + sess.ID.String() + "\n" +
		"x-user-aal:"
	payload := strconv.FormatInt(1742912521, 10) + "." + expectedCanonical
	mac := hmac.New(sha256.New, []byte("identity-signing-secret"))
	_, _ = mac.Write([]byte(payload))
	expectedSig := hex.EncodeToString(mac.Sum(nil))
	if req.Header.Get("X-Aegion-Signature") != fmt.Sprintf("t=%d,v1=%s", 1742912521, expectedSig) {
		t.Fatalf("unexpected signature header: %q", req.Header.Get("X-Aegion-Signature"))
	}
}

func TestModuleProxyDirectorPreserveHost(t *testing.T) {
	t.Run("preserve host true", func(t *testing.T) {
		proxy := NewModuleProxy(ModuleProxyConfig{
			ModuleID:      "admin",
			PreserveHost:  true,
			SessionSecret: []byte("module-secret"),
			Logger:        zerolog.Nop(),
		})

		target, _ := url.Parse("http://module.local/base")
		original := httptest.NewRequest(http.MethodGet, "http://gateway.local/module/path", nil)
		original.Host = "gateway.example.com"
		req := httptest.NewRequest(http.MethodGet, "http://placeholder/module/path", nil)
		req = req.WithContext(original.Context())

		proxy.director(target, original)(req)
		if req.Host != "gateway.example.com" {
			t.Fatalf("expected original host to be preserved, got %q", req.Host)
		}
	})

	t.Run("preserve host false", func(t *testing.T) {
		proxy := NewModuleProxy(ModuleProxyConfig{
			ModuleID:      "admin",
			PreserveHost:  false,
			SessionSecret: []byte("module-secret"),
			Logger:        zerolog.Nop(),
		})

		target, _ := url.Parse("http://module.local/base")
		original := httptest.NewRequest(http.MethodGet, "http://gateway.local/module/path", nil)
		original.Host = "gateway.example.com"
		req := httptest.NewRequest(http.MethodGet, "http://placeholder/module/path", nil)
		req = req.WithContext(original.Context())

		proxy.director(target, original)(req)
		if req.Host != "module.local" {
			t.Fatalf("expected target host, got %q", req.Host)
		}
	})
}

func TestModuleProxyErrorHandlers(t *testing.T) {
	proxy := NewModuleProxy(ModuleProxyConfig{
		ModuleID: "password",
		Logger:   zerolog.Nop(),
	})

	t.Run("setup error with timeout", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/module", nil)
		proxy.handleError(rec, req, ErrModuleTimeout, "req-timeout")

		if rec.Code != http.StatusGatewayTimeout {
			t.Fatalf("expected %d, got %d", http.StatusGatewayTimeout, rec.Code)
		}
		var body map[string]map[string]interface{}
		if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
			t.Fatalf("decode error body: %v", err)
		}
		if body["error"]["request_id"] != "req-timeout" {
			t.Fatalf("expected request_id in error response")
		}
	})

	t.Run("proxy transport connection refused", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/module", nil)
		proxy.handleProxyError(rec, req, errors.New("dial tcp: connection refused"), "req-refused")

		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("expected %d, got %d", http.StatusServiceUnavailable, rec.Code)
		}
	})

	t.Run("proxy transport deadline exceeded", func(t *testing.T) {
		rec := httptest.NewRecorder()
		baseReq := httptest.NewRequest(http.MethodGet, "/module", nil)
		ctx, cancel := context.WithCancel(baseReq.Context())
		cancel()
		req := baseReq.WithContext(ctx)

		proxy.handleProxyError(rec, req, errors.New("upstream failed"), "req-timeout-2")
		// No context deadline here, so default should be bad gateway.
		if rec.Code != http.StatusBadGateway {
			t.Fatalf("expected %d, got %d", http.StatusBadGateway, rec.Code)
		}
	})
}

func TestModuleProxyServeHTTP(t *testing.T) {
	reg := registry.New(registry.DefaultConfig())
	defer reg.Stop()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Aegion-Internal-Token") != "int-token" {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte("missing token"))
			return
		}
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte("ok"))
	}))
	defer upstream.Close()

	mustRegisterModule(t, reg, registry.RegistrationRequest{
		ID:      "password",
		Name:    "password",
		Version: "v1",
		Endpoints: []registry.Endpoint{
			{Type: registry.EndpointHTTP, URL: upstream.URL},
		},
		HealthURL: upstream.URL + "/health",
	})

	proxy := NewModuleProxy(ModuleProxyConfig{
		Registry:      reg,
		ModuleID:      "password",
		InternalToken: "int-token",
		Timeout:       2 * time.Second,
		Logger:        zerolog.Nop(),
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/module/test", nil)
	req = req.WithContext(context.WithValue(req.Context(), contextKeyRequestID, "req-proxy"))
	proxy.ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected %d, got %d", http.StatusAccepted, rec.Code)
	}
	if body := rec.Body.String(); body != "ok" {
		t.Fatalf("unexpected body: %q", body)
	}
}

func TestModuleProxyServeHTTP_ModuleUnavailable(t *testing.T) {
	proxy := NewModuleProxy(ModuleProxyConfig{
		Registry: nil,
		ModuleID: "missing",
		Logger:   zerolog.Nop(),
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/module/test", nil)
	proxy.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected %d, got %d", http.StatusServiceUnavailable, rec.Code)
	}
}

func TestModuleProxyServeHTTP_PolicyDeny(t *testing.T) {
	reg := registry.New(registry.DefaultConfig())
	defer reg.Stop()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	mustRegisterModule(t, reg, registry.RegistrationRequest{
		ID:      "policy-module",
		Name:    "policy-module",
		Version: "v1",
		Endpoints: []registry.Endpoint{
			{Type: registry.EndpointHTTP, URL: upstream.URL},
		},
		HealthURL: upstream.URL + "/health",
	})

	checker := &stubPolicyChecker{resp: &policypb.CheckResponse{
		Allowed:    false,
		ModelUsed:  "abac",
		DenyReason: "abac_deny_rule_matched",
		EvalPath:   []string{"abac:deny:block"},
	}}

	proxy := NewModuleProxy(ModuleProxyConfig{
		Registry:      reg,
		ModuleID:      "policy-module",
		InternalToken: "int-token",
		Timeout:       2 * time.Second,
		PolicyChecker: checker,
		Logger:        zerolog.Nop(),
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/module/private", nil)
	req = req.WithContext(context.WithValue(req.Context(), contextKeyRequestID, "req-policy-deny"))
	proxy.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected %d, got %d", http.StatusForbidden, rec.Code)
	}
	if checker.lastReq == nil {
		t.Fatalf("expected policy checker to be called")
	}
	if checker.lastReq.GetAction() != "write" {
		t.Fatalf("expected write action, got %q", checker.lastReq.GetAction())
	}
	if checker.lastReq.GetResourceType() != "module:policy-module" {
		t.Fatalf("unexpected resource type: %q", checker.lastReq.GetResourceType())
	}
}

func TestModuleProxyServeHTTP_PolicyError(t *testing.T) {
	reg := registry.New(registry.DefaultConfig())
	defer reg.Stop()

	mustRegisterModule(t, reg, registry.RegistrationRequest{
		ID:      "policy-module",
		Name:    "policy-module",
		Version: "v1",
		Endpoints: []registry.Endpoint{
			{Type: registry.EndpointHTTP, URL: "http://127.0.0.1:18081"},
		},
		HealthURL: "http://127.0.0.1:18081/health",
	})

	checker := &stubPolicyChecker{err: errors.New("policy unavailable")}
	proxy := NewModuleProxy(ModuleProxyConfig{
		Registry:      reg,
		ModuleID:      "policy-module",
		Timeout:       2 * time.Second,
		PolicyChecker: checker,
		Logger:        zerolog.Nop(),
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/module/private", nil)
	req = req.WithContext(context.WithValue(req.Context(), contextKeyRequestID, "req-policy-error"))
	proxy.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("expected %d, got %d", http.StatusBadGateway, rec.Code)
	}
}

func TestWithRequestContextIPAndExtraction(t *testing.T) {
	ctx := context.Background()
	ctx = WithRequestContextIP(ctx, "198.51.100.10:4444")

	ip := requestContextIPFromContext(ctx)
	if ip != "198.51.100.10" {
		t.Fatalf("expected extracted host IP, got %q", ip)
	}
}
