package router

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/aegion/aegion/core/registry"
	"github.com/aegion/aegion/core/session"
	"github.com/aegion/aegion/internal/platform/logger"
	policypb "github.com/aegion/aegion/internal/proto/policy/v1"
)

func TestPolicyDenyError_AdditionalBranches(t *testing.T) {
	var nilErr *policyDenyError
	if got := nilErr.Error(); got != ErrPolicyDenied.Error() {
		t.Fatalf("expected default deny error for nil receiver, got %q", got)
	}

	errWithBlankReason := &policyDenyError{response: &policypb.CheckResponse{DenyReason: "   "}}
	if got := errWithBlankReason.Error(); got != "policy_denied" {
		t.Fatalf("expected policy_denied fallback reason, got %q", got)
	}
}

func TestNewModuleProxy_DefaultNowFunction(t *testing.T) {
	proxy := NewModuleProxy(ModuleProxyConfig{
		ModuleID: "default-clock",
		Logger:   logger.New(logger.Config{Level: "error"}).Logger,
	})
	if proxy.now().IsZero() {
		t.Fatalf("expected default now function to return a valid timestamp")
	}
}

func TestModuleProxyServeHTTP_PolicyAllowAdditionalBranches(t *testing.T) {
	reg := registry.New(registry.DefaultConfig(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	defer reg.Stop()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer upstream.Close()

	mustRegisterModule(t, reg, registry.RegistrationRequest{
		ID:      "policy-allow",
		Name:    "policy-allow",
		Version: "v1",
		Endpoints: []registry.Endpoint{
			{Type: registry.EndpointHTTP, URL: upstream.URL},
		},
		HealthURL: upstream.URL + "/health",
	})

	checker := &stubPolicyChecker{
		resp: &policypb.CheckResponse{
			Allowed:   true,
			ModelUsed: "rbac",
			EvalPath:  []string{"rbac:allow"},
		},
	}

	proxy := NewModuleProxy(ModuleProxyConfig{
		Registry:      reg,
		ModuleID:      "policy-allow",
		InternalToken: "int-token",
		PolicyChecker: checker,
		Logger:        logger.New(logger.Config{Level: "error"}).Logger,
	})

	sess := &session.Session{
		ID:         uuid.New(),
		IdentityID: uuid.New(),
	}
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("User-Agent", "coverage-agent/1.0")
	req = req.WithContext(session.WithSession(req.Context(), sess))
	req = req.WithContext(context.WithValue(req.Context(), contextKeyRequestID, "req-policy-allow"))
	rec := httptest.NewRecorder()

	proxy.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected %d, got %d", http.StatusNoContent, rec.Code)
	}
	if checker.lastReq == nil {
		t.Fatalf("expected policy checker request")
	}
	if checker.lastReq.GetSubject() != "user:"+sess.IdentityID.String() {
		t.Fatalf("unexpected policy subject: %q", checker.lastReq.GetSubject())
	}
	if checker.lastReq.GetResource() != "policy-allow:_root" {
		t.Fatalf("unexpected policy resource: %q", checker.lastReq.GetResource())
	}
	if checker.lastReq.GetContext().GetExtra()["user_agent"] != "coverage-agent/1.0" {
		t.Fatalf("expected user agent in policy context extras")
	}
}

func TestModuleProxyIdentityAndForwarded_AdditionalBranches(t *testing.T) {
	t.Setenv("AEGION_TRUSTED_PROXY_CIDRS", "198.51.100.0/24")
	proxy := NewModuleProxy(ModuleProxyConfig{
		ModuleID:              "module-a",
		SignedIdentityHeaders: []string{"X-User-ID", "  ", "X-User-AAL"},
		TrustForwardedHeaders: true,
		Logger:                logger.New(logger.Config{Level: "error"}).Logger,
	})

	sess := &session.Session{
		ID:         uuid.New(),
		IdentityID: uuid.New(),
	}
	req := httptest.NewRequest(http.MethodGet, "/module", nil)
	req = req.WithContext(session.WithSession(req.Context(), sess))

	proxy.injectIdentityHeaders(req)
	if sig := req.Header.Get("X-Aegion-Signature"); sig != "" {
		t.Fatalf("expected no signature header when signing secret is empty, got %q", sig)
	}

	canonical := proxy.canonicalIdentityHeaders(req)
	if strings.Contains(canonical, "\n:") {
		t.Fatalf("expected blank signed headers to be skipped, canonical=%q", canonical)
	}

	original := httptest.NewRequest(http.MethodGet, "http://edge.example.com/resource", nil)
	original.RemoteAddr = "198.51.100.41:1234"
	original.Host = "edge.example.com"
	original.Header.Set("X-Real-IP", "198.51.100.40")
	forwarded := httptest.NewRequest(http.MethodGet, "http://upstream.example.com/resource", nil)

	proxy.addForwardedHeaders(forwarded, original)
	if forwarded.Header.Get("X-Forwarded-For") != "198.51.100.41" {
		t.Fatalf("expected X-Forwarded-For fallback from getClientIP, got %q", forwarded.Header.Get("X-Forwarded-For"))
	}
}

func TestModuleProxyErrorAndEndpoint_AdditionalBranches(t *testing.T) {
	proxy := NewModuleProxy(ModuleProxyConfig{
		ModuleID: "module-a",
		Logger:   logger.New(logger.Config{Level: "error"}).Logger,
	})

	req := httptest.NewRequest(http.MethodGet, "/module", nil)

	recNotFound := httptest.NewRecorder()
	proxy.handleError(recNotFound, req, fmt.Errorf("wrapped: %w", registry.ErrModuleNotFound), "req-not-found")
	if recNotFound.Code != http.StatusNotFound {
		t.Fatalf("expected %d, got %d", http.StatusNotFound, recNotFound.Code)
	}

	recNoEndpoint := httptest.NewRecorder()
	proxy.handleError(recNoEndpoint, req, ErrNoHealthyEndpoint, "req-no-endpoint")
	if recNoEndpoint.Code != http.StatusBadGateway {
		t.Fatalf("expected %d, got %d", http.StatusBadGateway, recNoEndpoint.Code)
	}

	recURL := httptest.NewRecorder()
	proxy.handleError(recURL, req, &url.Error{Op: "parse", URL: "http://[::1", Err: errors.New("bad host")}, "req-url")
	if recURL.Code != http.StatusBadGateway {
		t.Fatalf("expected %d, got %d", http.StatusBadGateway, recURL.Code)
	}
	if !strings.Contains(recURL.Body.String(), "invalid module endpoint") {
		t.Fatalf("expected invalid module endpoint message, got %q", recURL.Body.String())
	}

	reg := registry.New(registry.DefaultConfig(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	defer reg.Stop()
	mustRegisterModule(t, reg, registry.RegistrationRequest{
		ID:      "bad-endpoint",
		Name:    "bad-endpoint",
		Version: "v1",
		Endpoints: []registry.Endpoint{
			{Type: registry.EndpointHTTP, URL: "http://[::1"},
		},
		HealthURL: "",
	})

	badProxy := NewModuleProxy(ModuleProxyConfig{
		Registry: reg,
		ModuleID: "bad-endpoint",
		Logger:   logger.New(logger.Config{Level: "error"}).Logger,
	})

	if _, err := badProxy.getModuleEndpoint(context.Background(), "http"); err == nil {
		t.Fatalf("expected endpoint URL parse error")
	}
}
