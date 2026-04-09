package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/aegion/aegion/core/authtoken"
	"github.com/aegion/aegion/core/flows"
	"github.com/aegion/aegion/core/session"
	"github.com/aegion/aegion/internal/platform/database"
)

func TestParseFlowSubmitPayloadVariants(t *testing.T) {
	flowID := uuid.New()

	t.Run("json body flow_id and csrf token", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/submit", bytes.NewBufferString(`{"flow_id":"`+flowID.String()+`","csrf_token":"csrf"}`))
		req.Header.Set("Content-Type", "application/json")
		gotFlowID, csrf, err := parseFlowSubmitPayload(req)
		if err != nil {
			t.Fatalf("parseFlowSubmitPayload failed: %v", err)
		}
		if gotFlowID != flowID || csrf != "csrf" {
			t.Fatalf("unexpected payload parse result: id=%s csrf=%q", gotFlowID, csrf)
		}
	})

	t.Run("form body with header csrf fallback", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/submit", bytes.NewBufferString("flow="+flowID.String()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("X-CSRF-Token", "csrf-from-header")
		gotFlowID, csrf, err := parseFlowSubmitPayload(req)
		if err != nil {
			t.Fatalf("parseFlowSubmitPayload failed: %v", err)
		}
		if gotFlowID != flowID || csrf != "csrf-from-header" {
			t.Fatalf("unexpected payload parse result: id=%s csrf=%q", gotFlowID, csrf)
		}
	})

	t.Run("query fallback for flow and id", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/submit?flow="+flowID.String(), nil)
		req.Header.Set("X-CSRF-Token", "csrf-query")
		gotFlowID, csrf, err := parseFlowSubmitPayload(req)
		if err != nil {
			t.Fatalf("parseFlowSubmitPayload failed: %v", err)
		}
		if gotFlowID != flowID || csrf != "csrf-query" {
			t.Fatalf("unexpected payload parse result: id=%s csrf=%q", gotFlowID, csrf)
		}
	})

	t.Run("invalid and missing payloads return errors", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/submit", bytes.NewBufferString("{"))
		req.Header.Set("Content-Type", "application/json")
		if _, _, err := parseFlowSubmitPayload(req); err == nil {
			t.Fatal("expected invalid json error")
		}

		req = httptest.NewRequest(http.MethodPost, "/submit", nil)
		req.Header.Set("X-CSRF-Token", "csrf")
		if _, _, err := parseFlowSubmitPayload(req); err == nil {
			t.Fatal("expected missing flow id error")
		}

		req = httptest.NewRequest(http.MethodPost, "/submit?flow=not-a-uuid", nil)
		req.Header.Set("X-CSRF-Token", "csrf")
		if _, _, err := parseFlowSubmitPayload(req); err == nil {
			t.Fatal("expected invalid flow id error")
		}

		req = httptest.NewRequest(http.MethodPost, "/submit?flow="+flowID.String(), nil)
		if _, _, err := parseFlowSubmitPayload(req); err == nil {
			t.Fatal("expected missing csrf token error")
		}
	})
}

func TestWriteFlowValidationErrorBranches(t *testing.T) {
	s := newTestServer(t)
	cases := []struct {
		name string
		err  error
		code int
	}{
		{name: "not_found", err: flows.ErrFlowNotFound, code: http.StatusNotFound},
		{name: "invalid_csrf", err: flows.ErrInvalidCSRF, code: http.StatusForbidden},
		{name: "expired", err: flows.ErrFlowExpired, code: http.StatusGone},
		{name: "completed", err: flows.ErrFlowCompleted, code: http.StatusConflict},
		{name: "failed", err: flows.ErrFlowFailed, code: http.StatusConflict},
		{name: "unknown", err: errors.New("unexpected"), code: http.StatusInternalServerError},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			s.writeFlowValidationError(rec, tc.err)
			if rec.Code != tc.code {
				t.Fatalf("expected %d, got %d", tc.code, rec.Code)
			}
		})
	}
}

func TestModuleProxyHelpers(t *testing.T) {
	t.Run("module proxy runtime settings fallback on load errors", func(t *testing.T) {
		s := newTestServer(t)
		s.db = &database.DB{}
		s.dbQueryRowFn = func(ctx context.Context, sql string, args ...any) pgx.Row {
			return adminTestRow{scanFn: func(dest ...any) error { return errors.New("db read failed") }}
		}

		proxySettings, policySettings := s.moduleProxyRuntimeSettings(context.Background())
		if proxySettings.UpstreamTimeout == "" || proxySettings.IdentitySignatureHeader == "" {
			t.Fatalf("expected proxy defaults on runtime load failure, got %+v", proxySettings)
		}
		if policySettings.DefaultModel == "" {
			t.Fatalf("expected policy defaults on runtime load failure, got %+v", policySettings)
		}
	})

	t.Run("internal token and proxy secret helpers", func(t *testing.T) {
		s := newTestServer(t)
		s.tokenGen = nil
		if got := s.currentInternalTokenForProxy(); got != "" {
			t.Fatalf("expected empty token with nil generator, got %q", got)
		}

		tokenGen, err := authtoken.NewGenerator(authtoken.GeneratorConfig{
			Secret: []byte("test-internal-secret"),
		})
		if err != nil {
			t.Fatalf("failed to create token generator: %v", err)
		}
		s.tokenGen = tokenGen
		if got := s.currentInternalTokenForProxy(); got == "" {
			t.Fatal("expected generated internal token")
		}

		s.cfg.Secrets.Cookie = []string{"cookie-secret"}
		if got := string(s.sessionSecretForProxy()); got != "cookie-secret" {
			t.Fatalf("expected cookie secret, got %q", got)
		}
		s.cfg.Secrets.Cookie = nil
		s.cfg.Secrets.Internal = []string{"internal-secret"}
		if got := string(s.sessionSecretForProxy()); got != "internal-secret" {
			t.Fatalf("expected internal secret, got %q", got)
		}
		s.cfg.Secrets.Internal = nil
		s.cfg.Secrets.Cipher = []string{"cipher-secret"}
		if got := string(s.sessionSecretForProxy()); got != "cipher-secret" {
			t.Fatalf("expected cipher secret, got %q", got)
		}
		s.cfg.Secrets.Cipher = nil
		if got := s.sessionSecretForProxy(); got != nil {
			t.Fatalf("expected nil secret fallback, got %q", string(got))
		}

		s.cfg.Proxy.IdentitySigningSecret = "configured-signing-secret"
		if got := string(s.proxyIdentitySigningSecret("  override-signing-secret  ")); got != "override-signing-secret" {
			t.Fatalf("expected override signing secret, got %q", got)
		}
		if got := string(s.proxyIdentitySigningSecret("")); got != "configured-signing-secret" {
			t.Fatalf("expected configured signing secret, got %q", got)
		}
		s.cfg.Proxy.IdentitySigningSecret = ""
		s.cfg.Secrets.Internal = []string{"internal-signing-secret"}
		if got := string(s.proxyIdentitySigningSecret("")); got != "internal-signing-secret" {
			t.Fatalf("expected internal signing secret fallback, got %q", got)
		}
	})

	t.Run("extract request ip and context propagation", func(t *testing.T) {
		if got := extractRequestIP(nil); got != "" {
			t.Fatalf("expected empty IP for nil request, got %q", got)
		}

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = "198.51.100.99:443"
		req.Header.Set("X-Forwarded-For", "198.51.100.10, 198.51.100.11")
		if got := extractRequestIP(req); got != "198.51.100.99" {
			t.Fatalf("expected remote addr when forwarded headers are not trusted, got %q", got)
		}
		if got := extractRequestIPWithTrust(req, true); got != "198.51.100.10" {
			t.Fatalf("expected first forwarded IP when trusted, got %q", got)
		}

		req = httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = "198.51.100.98:443"
		req.Header.Set("X-Real-IP", "203.0.113.2")
		if got := extractRequestIP(req); got != "198.51.100.98" {
			t.Fatalf("expected remote addr when forwarded headers are not trusted, got %q", got)
		}
		if got := extractRequestIPWithTrust(req, true); got != "203.0.113.2" {
			t.Fatalf("expected real IP when trusted, got %q", got)
		}

		req = httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = "[2001:db8::1]:443"
		if got := extractRequestIP(req); got != "2001:db8::1" {
			t.Fatalf("expected parsed host from remote addr, got %q", got)
		}

		baseCtx := context.Background()
		req = httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = ""
		ctx := withModuleProxyRequestContext(baseCtx, req)
		if ctx != baseCtx {
			t.Fatal("expected unchanged context when no request ip is available")
		}

		req = httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = ""
		req.Header.Set("X-Forwarded-For", "198.51.100.20")
		ctx = withModuleProxyRequestContext(baseCtx, req)
		if ctx != baseCtx {
			t.Fatal("expected unchanged context when forwarded headers are not trusted")
		}
		ctx = withModuleProxyRequestContextWithTrust(baseCtx, req, true)
		if ctx == baseCtx {
			t.Fatal("expected derived context when trusted request ip is available")
		}
	})
}

func TestSessionHandlersAdditionalErrors(t *testing.T) {
	s := newTestServer(t)

	t.Run("whoami returns 500 when session manager missing", func(t *testing.T) {
		s.sessionManager = nil
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/v1/sessions/whoami", nil)
		s.handleWhoAmI(rec, req)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("expected %d, got %d", http.StatusInternalServerError, rec.Code)
		}
	})

	t.Run("whoami and logout return 500 on unexpected session errors", func(t *testing.T) {
		sm := &stubRouteSessionManager{getErr: errors.New("session backend failed")}
		s.sessionManager = sm

		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/v1/sessions/whoami", nil)
		s.handleWhoAmI(rec, req)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("expected %d, got %d", http.StatusInternalServerError, rec.Code)
		}

		rec = httptest.NewRecorder()
		req = httptest.NewRequest(http.MethodDelete, "/api/v1/sessions/logout", nil)
		s.handleLogout(rec, req)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("expected %d, got %d", http.StatusInternalServerError, rec.Code)
		}
	})

	t.Run("logout revoke failure returns 500", func(t *testing.T) {
		sm := &stubRouteSessionManager{
			session:   &session.Session{ID: uuid.New()},
			revokeErr: errors.New("revoke failed"),
		}
		s.sessionManager = sm

		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodDelete, "/api/v1/sessions/logout", nil)
		s.handleLogout(rec, req)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("expected %d, got %d", http.StatusInternalServerError, rec.Code)
		}
	})

	t.Run("logout returns 500 when session manager missing", func(t *testing.T) {
		s.sessionManager = nil
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodDelete, "/api/v1/sessions/logout", nil)
		s.handleLogout(rec, req)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("expected %d, got %d", http.StatusInternalServerError, rec.Code)
		}
	})
}

func TestHandleAdminGetConfig_WithHookedRuntimeRows(t *testing.T) {
	s := newHookedServer(t)
	s.dbQueryRowFn = func(ctx context.Context, sql string, args ...any) pgx.Row {
		switch args[0] {
		case systemConfigKeyPolicy:
			return adminTestRow{scanFn: func(dest ...any) error {
				*(dest[0].(*[]byte)) = []byte(`{"enabled":true,"default_model":"rbac","rbac":{"enabled":true},"abac":{"enabled":false},"rebac":{"enabled":false}}`)
				return nil
			}}
		case systemConfigKeyProxy:
			return adminTestRow{scanFn: func(dest ...any) error {
				*(dest[0].(*[]byte)) = []byte(`{"enabled":true,"upstream_timeout":"15s","preserve_host":true,"strip_inbound_identity_headers":true,"identity_signing_secret":"0123456789abcdef","identity_signature_header":"X-Sig","signed_identity_headers":["X-User-ID"]}`)
				return nil
			}}
		default:
			return adminTestRow{scanFn: func(dest ...any) error { return errors.New("unexpected key") }}
		}
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/aegion/api/v1/config", nil)
	s.handleAdminGetConfig(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected %d, got %d", http.StatusOK, rec.Code)
	}

	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("failed to decode config response: %v", err)
	}
	if body["proxy"] == nil || body["policy"] == nil {
		t.Fatalf("expected policy and proxy sections, got %v", body)
	}
}
