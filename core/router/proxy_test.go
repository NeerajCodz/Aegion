package router

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"

	"github.com/aegion/aegion/core/authtoken"
	"github.com/aegion/aegion/core/registry"
	"github.com/aegion/aegion/core/session"
	platformcrypto "github.com/aegion/aegion/internal/platform/crypto"
	policypb "github.com/aegion/aegion/internal/proto/policy/v1"
)

type stubPolicyChecker struct {
	resp      *policypb.CheckResponse
	err       error
	returnNil bool

	lastReq *policypb.CheckRequest
}

func (s *stubPolicyChecker) Check(ctx context.Context, req *policypb.CheckRequest) (*policypb.CheckResponse, error) {
	s.lastReq = req
	if s.err != nil {
		return nil, s.err
	}
	if s.returnNil {
		return nil, nil
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
		TrustForwardedHeaders:       false,
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
	if req.Header.Get("X-Forwarded-For") != "198.51.100.4" {
		t.Fatalf("unexpected x-forwarded-for: %q", req.Header.Get("X-Forwarded-For"))
	}
	if req.Header.Get("X-Forwarded-Proto") != "http" {
		t.Fatalf("expected forwarded proto http")
	}
	if req.Header.Get("X-Forwarded-Host") != "gateway.local" {
		t.Fatalf("expected forwarded host from original request host")
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

	expectedSig, err := platformcrypto.SignIdentityHeaders(
		[]byte("identity-signing-secret"),
		req.Header,
		proxy.config.SignedIdentityHeaders,
		time.Unix(1742912521, 0).UTC(),
	)
	if err != nil {
		t.Fatalf("failed to compute expected signature: %v", err)
	}
	if req.Header.Get("X-Aegion-Signature") != expectedSig {
		t.Fatalf("unexpected signature header: %q", req.Header.Get("X-Aegion-Signature"))
	}
}

func TestModuleProxyAddForwardedHeadersTrustForwardedHeaders(t *testing.T) {
	t.Setenv("AEGION_TRUSTED_PROXY_CIDRS", "198.51.100.0/24")
	proxy := NewModuleProxy(ModuleProxyConfig{
		ModuleID:                    "admin",
		TrustForwardedHeaders:       true,
		IdentitySigningSecret:       []byte("identity-signing-secret"),
		StripInboundIdentityHeaders: true,
		Logger:                      zerolog.Nop(),
	})

	original := httptest.NewRequest(http.MethodGet, "http://gateway.local/module/path", nil)
	original.RemoteAddr = "198.51.100.4:1234"
	original.Header.Set("X-Forwarded-For", "203.0.113.10")
	original.Header.Set("X-Forwarded-Proto", "https")
	original.Header.Set("X-Forwarded-Host", "gateway.example.com")

	req := httptest.NewRequest(http.MethodGet, "http://placeholder/module/path", nil)
	proxy.addForwardedHeaders(req, original)

	if req.Header.Get("X-Forwarded-For") != "203.0.113.10, 198.51.100.4" {
		t.Fatalf("unexpected x-forwarded-for: %q", req.Header.Get("X-Forwarded-For"))
	}
	if req.Header.Get("X-Forwarded-Proto") != "https" {
		t.Fatalf("expected forwarded proto https")
	}
	if req.Header.Get("X-Forwarded-Host") != "gateway.example.com" {
		t.Fatalf("expected forwarded host from original header")
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

func TestBuildPolicyCheckRequestUsesAuthenticatedModuleSubject(t *testing.T) {
	proxy := NewModuleProxy(ModuleProxyConfig{
		ModuleID: "admin",
		Logger:   zerolog.Nop(),
	})

	req := httptest.NewRequest(http.MethodGet, "http://gateway.local/resource", nil)
	ctx := context.WithValue(req.Context(), contextKeyRequestID, "req-1")
	ctx = context.WithValue(ctx, authtoken.ContextKeyModuleID, "billing")
	req = req.WithContext(ctx)

	checkReq := proxy.buildPolicyCheckRequest(req)
	if checkReq.GetSubject() != "module:billing" {
		t.Fatalf("expected module subject, got %q", checkReq.GetSubject())
	}
}

func TestBuildPolicyCheckRequestDoesNotTrustInboundTenantHeader(t *testing.T) {
	proxy := NewModuleProxy(ModuleProxyConfig{
		ModuleID: "admin",
		Logger:   zerolog.Nop(),
	})

	req := httptest.NewRequest(http.MethodGet, "http://gateway.local/resource", nil)
	req.Header.Set("X-Aegion-Tenant-ID", "tenant-admin")

	checkReq := proxy.buildPolicyCheckRequest(req)
	if checkReq.GetContext().GetTenantId() != "" {
		t.Fatalf("expected empty tenant id, got %q", checkReq.GetContext().GetTenantId())
	}
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

func TestModuleProxyServeHTTP_PolicyRequiredWithoutChecker(t *testing.T) {
	reg := registry.New(registry.DefaultConfig())
	defer reg.Stop()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	mustRegisterModule(t, reg, registry.RegistrationRequest{
		ID:      "policy-required",
		Name:    "policy-required",
		Version: "v1",
		Endpoints: []registry.Endpoint{
			{Type: registry.EndpointHTTP, URL: upstream.URL},
		},
		HealthURL: upstream.URL + "/health",
	})

	proxy := NewModuleProxy(ModuleProxyConfig{
		Registry:      reg,
		ModuleID:      "policy-required",
		RequirePolicy: true,
		Logger:        zerolog.Nop(),
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/module/private", nil)
	proxy.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected %d, got %d", http.StatusForbidden, rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "policy_unavailable") {
		t.Fatalf("expected policy_unavailable deny reason, got %q", rec.Body.String())
	}
}

func TestModuleProxyServeHTTP_PolicyNilDecision(t *testing.T) {
	reg := registry.New(registry.DefaultConfig())
	defer reg.Stop()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	mustRegisterModule(t, reg, registry.RegistrationRequest{
		ID:      "policy-nil",
		Name:    "policy-nil",
		Version: "v1",
		Endpoints: []registry.Endpoint{
			{Type: registry.EndpointHTTP, URL: upstream.URL},
		},
		HealthURL: upstream.URL + "/health",
	})

	proxy := NewModuleProxy(ModuleProxyConfig{
		Registry:      reg,
		ModuleID:      "policy-nil",
		PolicyChecker: &stubPolicyChecker{returnNil: true},
		Logger:        zerolog.Nop(),
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/module/private", nil)
	proxy.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected %d, got %d", http.StatusForbidden, rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "policy_no_decision") {
		t.Fatalf("expected policy_no_decision deny reason, got %q", rec.Body.String())
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

func TestWithRequestContextIPEmpty(t *testing.T) {
	ctx := context.Background()
	ctx = WithRequestContextIP(ctx, "  ")
	if ctx.Value(requestContextIPKey{}) != nil {
		t.Fatal("expected empty IP not to be stored in context")
	}

	ctx = WithRequestContextIP(ctx, "")
	if ctx.Value(requestContextIPKey{}) != nil {
		t.Fatal("expected empty IP not to be stored in context")
	}
}

func TestRequiredPolicyDenyResponseWithEmptyReason(t *testing.T) {
	resp := requiredPolicyDenyResponse("")
	if resp.Allowed {
		t.Fatal("expected deny response")
	}
	if resp.DenyReason != "policy_denied" {
		t.Fatalf("expected default deny reason, got %q", resp.DenyReason)
	}
	if resp.ModelUsed != "default" {
		t.Fatalf("expected default model, got %q", resp.ModelUsed)
	}

	resp = requiredPolicyDenyResponse("   ")
	if resp.DenyReason != "policy_denied" {
		t.Fatalf("expected default deny reason for whitespace, got %q", resp.DenyReason)
	}

	resp = requiredPolicyDenyResponse("custom_reason")
	if resp.DenyReason != "custom_reason" {
		t.Fatalf("expected custom reason to be preserved, got %q", resp.DenyReason)
	}
}

func TestPolicyActionFromMethodEdgeCases(t *testing.T) {
	tests := []struct {
		method   string
		expected string
	}{
		{"GET", "read"},
		{"HEAD", "read"},
		{"OPTIONS", "read"},
		{"POST", "write"},
		{"PUT", "write"},
		{"PATCH", "write"},
		{"DELETE", "write"},
		{"CONNECT", "connect"},
		{"TRACE", "trace"},
		{"UNKNOWN", "unknown"},
		{"  connect  ", "connect"},
		{"  GET  ", "read"},
	}

	for _, tt := range tests {
		got := policyActionFromMethod(tt.method)
		if got != tt.expected {
			t.Errorf("policyActionFromMethod(%q) = %q, want %q", tt.method, got, tt.expected)
		}
	}
}

func TestNormalizePolicyModelEdgeCases(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"", ""},
		{"default", "default"},
		{"rbac", "rbac"},
		{"abac", "abac"},
		{"rebac", "rebac"},
		{"RBAC", "rbac"},
		{"  AbAc  ", "abac"},
		{"invalid", ""},
		{"unknown_model", ""},
		{"rbac-extended", ""},
	}

	for _, tt := range tests {
		got := normalizePolicyModel(tt.input)
		if got != tt.expected {
			t.Errorf("normalizePolicyModel(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}
