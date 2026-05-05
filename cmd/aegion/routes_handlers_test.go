package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/aegion/aegion/core/authtoken"
	"github.com/aegion/aegion/core/flows"
	"github.com/aegion/aegion/core/orchestrator"
	"github.com/aegion/aegion/core/registry"
	"github.com/aegion/aegion/core/session"
	"github.com/aegion/aegion/internal/platform/config"
	"github.com/aegion/aegion/internal/platform/logger"
	policypb "github.com/aegion/aegion/internal/proto/policy/v1"
)

type stubCmdPolicyChecker struct {
	resp *policypb.CheckResponse
	err  error

	lastReq *policypb.CheckRequest
}

func (s *stubCmdPolicyChecker) Check(ctx context.Context, req *policypb.CheckRequest) (*policypb.CheckResponse, error) {
	s.lastReq = req
	if s.err != nil {
		return nil, s.err
	}
	if s.resp != nil {
		return s.resp, nil
	}
	return &policypb.CheckResponse{Allowed: true, ModelUsed: "rbac", EvalPath: []string{"rbac:allow"}}, nil
}

type stubRouteOrchestrator struct {
	restartErr   error
	restartCalls []string
}

func (s *stubRouteOrchestrator) Start(ctx context.Context) error {
	return nil
}

func (s *stubRouteOrchestrator) Stop(ctx context.Context) error {
	return nil
}

func (s *stubRouteOrchestrator) RestartModule(ctx context.Context, moduleID string) error {
	s.restartCalls = append(s.restartCalls, moduleID)
	return s.restartErr
}

type stubRouteSessionManager struct {
	session *session.Session
	getErr  error

	createErr      error
	created        []*session.Session
	setCookieCount int

	revokeErr          error
	revokeAllErr       error
	addAuthMethodErr   error
	revokedIDs         []uuid.UUID
	revokedIdentityIDs []uuid.UUID
	addedAuthMethods   []session.AuthMethod
	addedAuthMethodIDs []uuid.UUID
	clearCookie        int
}

func (s *stubRouteSessionManager) GetFromRequest(ctx context.Context, r *http.Request) (*session.Session, error) {
	if s.getErr != nil {
		return nil, s.getErr
	}
	if s.session != nil {
		return s.session, nil
	}
	return nil, session.ErrSessionNotFound
}

func (s *stubRouteSessionManager) Create(ctx context.Context, identityID uuid.UUID, method session.AuthMethod, device session.DeviceInfo) (*session.Session, error) {
	if s.createErr != nil {
		return nil, s.createErr
	}
	created := &session.Session{
		ID:         uuid.New(),
		IdentityID: identityID,
		AAL:        session.AAL1,
		Active:     true,
		Devices:    []session.DeviceInfo{device},
		AuthMethods: []session.SessionAuthMethod{
			{
				Method: method,
			},
		},
	}
	s.created = append(s.created, created)
	s.session = created
	return created, nil
}

func (s *stubRouteSessionManager) Revoke(ctx context.Context, sessionID uuid.UUID) error {
	s.revokedIDs = append(s.revokedIDs, sessionID)
	return s.revokeErr
}

func (s *stubRouteSessionManager) RevokeAllForIdentity(ctx context.Context, identityID uuid.UUID) error {
	s.revokedIdentityIDs = append(s.revokedIdentityIDs, identityID)
	return s.revokeAllErr
}

func (s *stubRouteSessionManager) AddAuthMethod(ctx context.Context, sessionID uuid.UUID, method session.AuthMethod) error {
	s.addedAuthMethodIDs = append(s.addedAuthMethodIDs, sessionID)
	s.addedAuthMethods = append(s.addedAuthMethods, method)
	return s.addAuthMethodErr
}

func (s *stubRouteSessionManager) SetCookie(w http.ResponseWriter, sess *session.Session) {
	s.setCookieCount++
	s.session = sess
}

func (s *stubRouteSessionManager) ClearCookie(w http.ResponseWriter) {
	s.clearCookie++
}

func newTestServer(t *testing.T) *Server {
	t.Helper()

	tokenGen, err := authtoken.NewGenerator(authtoken.GeneratorConfig{
		Secret: []byte("test-internal-secret"),
		TTL:    5 * time.Minute,
	})
	if err != nil {
		t.Fatalf("failed to create token generator: %v", err)
	}

	cfg := &config.Config{
		Server: config.ServerConfig{
			RequestTimeout: config.Duration(10 * time.Second),
			CORS: config.CORSConfig{
				Enabled:          false,
				AllowedOrigins:   []string{"https://example.com"},
				AllowedMethods:   []string{"GET", "POST", "PATCH", "DELETE"},
				AllowedHeaders:   []string{"Content-Type", "Authorization"},
				AllowCredentials: true,
			},
		},
		Admin: config.AdminConfig{
			Enabled: false,
			Path:    "/aegion",
		},
	}

	return &Server{
		cfg:      cfg,
		log:      logger.New(logger.Config{Level: "error", Format: "json"}),
		registry: registry.New(registry.DefaultConfig(), slog.Default()),
		tokenGen: tokenGen,
	}
}

func withURLParam(req *http.Request, key, value string) *http.Request {
	routeCtx := chi.NewRouteContext()
	routeCtx.URLParams.Add(key, value)
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, routeCtx))
}

func mustJSONBody(t *testing.T, v interface{}) *bytes.Buffer {
	t.Helper()

	payload, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("failed to marshal json: %v", err)
	}
	return bytes.NewBuffer(payload)
}

func registerTestModule(t *testing.T, s *Server, moduleID string, endpointType registry.EndpointType, endpointURL string) {
	t.Helper()
	_, err := s.registry.Register(registry.RegistrationRequest{
		ID:      moduleID,
		Name:    "test-module",
		Version: "v1.0.0",
		Endpoints: []registry.Endpoint{
			{Type: endpointType, URL: endpointURL},
		},
		HealthURL: endpointURL + "/health",
	})
	if err != nil {
		t.Fatalf("failed to register module: %v", err)
	}
}

func TestJoinStrings(t *testing.T) {
	if got := joinStrings(nil); got != "" {
		t.Fatalf("expected empty string for nil slice, got %q", got)
	}
	if got := joinStrings([]string{"GET"}); got != "GET" {
		t.Fatalf("expected GET, got %q", got)
	}
	if got := joinStrings([]string{"GET", "POST", "PATCH"}); got != "GET, POST, PATCH" {
		t.Fatalf("unexpected joined string: %q", got)
	}
}

func TestWriteJSONAndWriteError(t *testing.T) {
	rec := httptest.NewRecorder()
	writeJSON(rec, http.StatusCreated, map[string]string{"status": "ok"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected %d, got %d", http.StatusCreated, rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("expected application/json, got %q", ct)
	}

	var body map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode writeJSON body: %v", err)
	}
	if body["status"] != "ok" {
		t.Fatalf("expected status ok, got %q", body["status"])
	}

	rec = httptest.NewRecorder()
	writeError(rec, http.StatusBadRequest, "bad request", nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected %d, got %d", http.StatusBadRequest, rec.Code)
	}
	var errBody map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&errBody); err != nil {
		t.Fatalf("failed to decode writeError body: %v", err)
	}
	if errBody["error"] != "bad request" {
		t.Fatalf("expected error message, got %v", errBody["error"])
	}

	rec = httptest.NewRecorder()
	writeError(rec, http.StatusInternalServerError, "internal error", errors.New("database connection string leaked"))
	errBody = map[string]interface{}{}
	if err := json.NewDecoder(rec.Body).Decode(&errBody); err != nil {
		t.Fatalf("failed to decode writeError body: %v", err)
	}
	if _, ok := errBody["details"]; ok {
		t.Fatalf("expected writeError to avoid exposing internal error details")
	}
}

func TestCORSMiddleware(t *testing.T) {
	s := newTestServer(t)

	nextCalled := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusNoContent)
	})
	handler := s.corsMiddleware(next)

	// Preflight for allowed origin should short-circuit with 204.
	req := httptest.NewRequest(http.MethodOptions, "/api/v1/sessions/whoami", nil)
	req.Header.Set("Origin", "https://example.com")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected preflight %d, got %d", http.StatusNoContent, rec.Code)
	}
	if nextCalled {
		t.Fatalf("expected next handler not to be called on preflight")
	}
	if rec.Header().Get("Access-Control-Allow-Origin") != "https://example.com" {
		t.Fatalf("expected allow origin header to be set for allowed origin")
	}
	if rec.Header().Get("Access-Control-Allow-Credentials") != "true" {
		t.Fatalf("expected allow credentials header true")
	}

	// Non-preflight allowed origin should call next and still emit CORS headers.
	nextCalled = false
	req = httptest.NewRequest(http.MethodGet, "/api/v1/sessions/whoami", nil)
	req.Header.Set("Origin", "https://example.com")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if !nextCalled {
		t.Fatalf("expected next handler to be called")
	}
	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected next status %d, got %d", http.StatusNoContent, rec.Code)
	}
	if rec.Header().Get("Access-Control-Allow-Origin") != "https://example.com" {
		t.Fatalf("expected allow origin header for GET")
	}

	// Disallowed origin should not emit CORS headers.
	nextCalled = false
	req = httptest.NewRequest(http.MethodGet, "/api/v1/sessions/whoami", nil)
	req.Header.Set("Origin", "https://evil.example")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatalf("expected no allow origin header for disallowed origin")
	}
}

func TestCORSMiddlewareWildcardOriginWithCredentialsDoesNotReflectOrigin(t *testing.T) {
	s := newTestServer(t)
	s.cfg.Server.CORS.AllowedOrigins = []string{"*"}
	s.cfg.Server.CORS.AllowCredentials = true

	nextCalled := false
	handler := s.corsMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodOptions, "/api/v1/sessions/whoami", nil)
	req.Header.Set("Origin", "https://evil.example")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected preflight %d, got %d", http.StatusNoContent, rec.Code)
	}
	if nextCalled {
		t.Fatalf("expected next handler not to be called on preflight")
	}
	if rec.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatalf("expected no allow-origin header when credentials are enabled with wildcard origin")
	}
	if rec.Header().Get("Access-Control-Allow-Credentials") != "" {
		t.Fatalf("expected no allow-credentials header when origin is not explicitly allowed")
	}

	nextCalled = false
	req = httptest.NewRequest(http.MethodGet, "/api/v1/sessions/whoami", nil)
	req.Header.Set("Origin", "https://evil.example")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if !nextCalled {
		t.Fatalf("expected next handler to be called for non-preflight request")
	}
	if rec.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatalf("expected no allow-origin header for non-explicit origin")
	}
}

func TestModuleHandlers(t *testing.T) {
	s := newTestServer(t)

	t.Run("register and list/get module", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/internal/registry/register", mustJSONBody(t, registry.RegistrationRequest{
			ID:      "password",
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
		if rec.Code != http.StatusCreated {
			t.Fatalf("expected %d, got %d", http.StatusCreated, rec.Code)
		}

		rec = httptest.NewRecorder()
		req = httptest.NewRequest(http.MethodGet, "/internal/registry/modules", nil)
		s.handleListModules(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected %d, got %d", http.StatusOK, rec.Code)
		}

		rec = httptest.NewRecorder()
		req = httptest.NewRequest(http.MethodGet, "/internal/registry/modules/password", nil)
		req = withURLParam(req, "id", "password")
		s.handleGetModule(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected %d, got %d", http.StatusOK, rec.Code)
		}
	})

	t.Run("register validation and conflict", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/internal/registry/register", bytes.NewBufferString("{"))
		rec := httptest.NewRecorder()
		s.handleModuleRegister(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected %d, got %d", http.StatusBadRequest, rec.Code)
		}

		req = httptest.NewRequest(http.MethodPost, "/internal/registry/register", mustJSONBody(t, registry.RegistrationRequest{
			ID:      "invalid-module",
			Version: "v1.0.0",
		}))
		req = req.WithContext(context.WithValue(req.Context(), authtoken.ContextKeyModuleID, "invalid-module"))
		rec = httptest.NewRecorder()
		s.handleModuleRegister(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected %d, got %d", http.StatusBadRequest, rec.Code)
		}

		req = httptest.NewRequest(http.MethodPost, "/internal/registry/register", mustJSONBody(t, registry.RegistrationRequest{
			ID:      "password",
			Name:    "password",
			Version: "v1.0.0",
			Endpoints: []registry.Endpoint{
				{Type: registry.EndpointHTTP, URL: "http://localhost:9000"},
			},
			HealthURL: "http://localhost:9000/health",
		}))
		req = req.WithContext(context.WithValue(req.Context(), authtoken.ContextKeyModuleID, "password"))
		rec = httptest.NewRecorder()
		s.handleModuleRegister(rec, req)
		if rec.Code != http.StatusConflict {
			t.Fatalf("expected %d, got %d", http.StatusConflict, rec.Code)
		}
	})

	t.Run("register enforces module identity", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/internal/registry/register", mustJSONBody(t, registry.RegistrationRequest{
			ID:      "password",
			Name:    "password",
			Version: "v1.0.0",
			Endpoints: []registry.Endpoint{
				{Type: registry.EndpointHTTP, URL: "http://localhost:9000"},
			},
			HealthURL: "http://localhost:9000/health",
		}))
		rec := httptest.NewRecorder()
		s.handleModuleRegister(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected %d, got %d", http.StatusUnauthorized, rec.Code)
		}

		req = httptest.NewRequest(http.MethodPost, "/internal/registry/register", mustJSONBody(t, registry.RegistrationRequest{
			ID:      "password",
			Name:    "password",
			Version: "v1.0.0",
			Endpoints: []registry.Endpoint{
				{Type: registry.EndpointHTTP, URL: "http://localhost:9000"},
			},
			HealthURL: "http://localhost:9000/health",
		}))
		req = req.WithContext(context.WithValue(req.Context(), authtoken.ContextKeyModuleID, "magic_link"))
		rec = httptest.NewRecorder()
		s.handleModuleRegister(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("expected %d, got %d", http.StatusForbidden, rec.Code)
		}
	})

	t.Run("deregister and not found", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/internal/registry/deregister", bytes.NewBufferString("{"))
		rec := httptest.NewRecorder()
		s.handleModuleDeregister(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected %d, got %d", http.StatusBadRequest, rec.Code)
		}

		req = httptest.NewRequest(http.MethodPost, "/internal/registry/deregister", mustJSONBody(t, registry.DeregistrationRequest{
			ModuleID: "password",
		}))
		req = req.WithContext(context.WithValue(req.Context(), authtoken.ContextKeyModuleID, "magic_link"))
		rec = httptest.NewRecorder()
		s.handleModuleDeregister(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("expected %d, got %d", http.StatusForbidden, rec.Code)
		}

		req = httptest.NewRequest(http.MethodPost, "/internal/registry/deregister", mustJSONBody(t, registry.DeregistrationRequest{
			ModuleID: "password",
		}))
		req = req.WithContext(context.WithValue(req.Context(), authtoken.ContextKeyModuleID, "password"))
		rec = httptest.NewRecorder()
		s.handleModuleDeregister(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected %d, got %d", http.StatusOK, rec.Code)
		}

		rec = httptest.NewRecorder()
		req = httptest.NewRequest(http.MethodPost, "/internal/registry/deregister", mustJSONBody(t, registry.DeregistrationRequest{
			ModuleID: "password",
		}))
		req = req.WithContext(context.WithValue(req.Context(), authtoken.ContextKeyModuleID, "password"))
		s.handleModuleDeregister(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected %d, got %d", http.StatusNotFound, rec.Code)
		}
	})

	t.Run("get module not found", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/internal/registry/modules/missing", nil)
		req = withURLParam(req, "id", "missing")
		s.handleGetModule(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected %d, got %d", http.StatusNotFound, rec.Code)
		}
	})

	t.Run("heartbeat scenarios", func(t *testing.T) {
		registerTestModule(t, s, "heartbeat-module", registry.EndpointHTTP, "http://localhost:9010")

		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/internal/registry/heartbeat", nil)
		s.handleModuleHeartbeat(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected %d, got %d", http.StatusUnauthorized, rec.Code)
		}

		rec = httptest.NewRecorder()
		req = httptest.NewRequest(http.MethodPost, "/internal/registry/heartbeat", nil)
		req = req.WithContext(context.WithValue(req.Context(), authtoken.ContextKeyModuleID, "missing-module"))
		s.handleModuleHeartbeat(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected %d, got %d", http.StatusNotFound, rec.Code)
		}

		rec = httptest.NewRecorder()
		req = httptest.NewRequest(http.MethodPost, "/internal/registry/heartbeat", nil)
		req = req.WithContext(context.WithValue(req.Context(), authtoken.ContextKeyModuleID, "heartbeat-module"))
		s.handleModuleHeartbeat(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected %d, got %d", http.StatusOK, rec.Code)
		}
	})
}

func TestModuleProxyHandler(t *testing.T) {
	s := newTestServer(t)

	t.Run("proxy disabled", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/internal/proxy/missing/anything", nil)
		req = withURLParam(req, "moduleId", "missing")
		rec := httptest.NewRecorder()
		s.handleModuleProxy(rec, req)
		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("expected %d, got %d", http.StatusServiceUnavailable, rec.Code)
		}
	})

	s.cfg.Proxy.Enabled = true

	t.Run("module not found", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/internal/proxy/missing/anything", nil)
		req = withURLParam(req, "moduleId", "missing")
		rec := httptest.NewRecorder()
		s.handleModuleProxy(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected %d, got %d", http.StatusNotFound, rec.Code)
		}
	})

	t.Run("no http endpoint", func(t *testing.T) {
		registerTestModule(t, s, "grpc-only", registry.EndpointGRPC, "grpc://localhost:9001")

		req := httptest.NewRequest(http.MethodGet, "/internal/proxy/grpc-only/anything", nil)
		req = withURLParam(req, "moduleId", "grpc-only")
		rec := httptest.NewRecorder()
		s.handleModuleProxy(rec, req)
		if rec.Code != http.StatusBadGateway {
			t.Fatalf("expected %d, got %d", http.StatusBadGateway, rec.Code)
		}
	})

	t.Run("invalid target url", func(t *testing.T) {
		registerTestModule(t, s, "bad-url", registry.EndpointHTTP, "://bad")

		req := httptest.NewRequest(http.MethodGet, "/internal/proxy/bad-url/anything", nil)
		req = withURLParam(req, "moduleId", "bad-url")
		rec := httptest.NewRecorder()
		s.handleModuleProxy(rec, req)
		if rec.Code != http.StatusBadGateway {
			t.Fatalf("expected %d, got %d", http.StatusBadGateway, rec.Code)
		}
	})

	t.Run("proxy success", func(t *testing.T) {
		s.cfg.Proxy.PreserveHost = true
		s.cfg.Proxy.StripInboundIdentityHeaders = true
		s.cfg.Proxy.IdentitySigningSecret = "proxy-signing-secret"

		var upstreamHost string
		var upstreamUserID string
		var upstreamSignature string
		var upstreamForwardedFor string
		var upstreamForwardedProto string
		var upstreamForwardedHost string
		target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			upstreamHost = r.Host
			upstreamUserID = r.Header.Get("X-User-ID")
			upstreamSignature = r.Header.Get("X-Aegion-Signature")
			upstreamForwardedFor = r.Header.Get("X-Forwarded-For")
			upstreamForwardedProto = r.Header.Get("X-Forwarded-Proto")
			upstreamForwardedHost = r.Header.Get("X-Forwarded-Host")
			w.WriteHeader(http.StatusAccepted)
			_, _ = w.Write([]byte("proxied"))
		}))
		defer target.Close()

		registerTestModule(t, s, "proxy-ok", registry.EndpointHTTP, target.URL)

		req := httptest.NewRequest(http.MethodGet, "/internal/proxy/proxy-ok/path", nil)
		req.Host = "gateway.example.test"
		req.RemoteAddr = "198.51.100.25:1234"
		req.Header.Set("X-Forwarded-For", "203.0.113.10")
		req.Header.Set("X-Forwarded-Proto", "https")
		req.Header.Set("X-Forwarded-Host", "spoofed.example")
		req.Header.Set("X-User-ID", "spoofed-user")
		req = req.WithContext(session.WithSession(req.Context(), &session.Session{
			ID:         uuid.New(),
			IdentityID: uuid.New(),
			AAL:        session.AAL1,
			ExpiresAt:  time.Now().Add(10 * time.Minute),
		}))
		req = withURLParam(req, "moduleId", "proxy-ok")
		rec := httptest.NewRecorder()
		s.handleModuleProxy(rec, req)

		if rec.Code != http.StatusAccepted {
			t.Fatalf("expected %d, got %d", http.StatusAccepted, rec.Code)
		}
		if body := rec.Body.String(); body != "proxied" {
			t.Fatalf("expected proxied response body, got %q", body)
		}
		if upstreamHost != "gateway.example.test" {
			t.Fatalf("expected preserve_host to forward original host, got %q", upstreamHost)
		}
		if upstreamUserID == "" || upstreamUserID == "spoofed-user" {
			t.Fatalf("expected canonical X-User-ID to be injected, got %q", upstreamUserID)
		}
		if upstreamSignature == "" {
			t.Fatalf("expected signed identity header to be injected")
		}
		if upstreamForwardedFor != "198.51.100.25, 198.51.100.25" {
			t.Fatalf("expected sanitized X-Forwarded-For, got %q", upstreamForwardedFor)
		}
		if upstreamForwardedProto != "http" {
			t.Fatalf("expected sanitized X-Forwarded-Proto, got %q", upstreamForwardedProto)
		}
		if upstreamForwardedHost != "gateway.example.test" {
			t.Fatalf("expected sanitized X-Forwarded-Host, got %q", upstreamForwardedHost)
		}
	})

	t.Run("proxy can trust forwarded headers when explicitly enabled", func(t *testing.T) {
		t.Setenv("AEGION_TRUSTED_PROXY_CIDRS", "198.51.100.0/24")
		s.cfg.Proxy.TrustForwardedHeaders = true
		defer func() {
			s.cfg.Proxy.TrustForwardedHeaders = false
		}()

		var upstreamForwardedFor string
		var upstreamForwardedProto string
		var upstreamForwardedHost string
		target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			upstreamForwardedFor = r.Header.Get("X-Forwarded-For")
			upstreamForwardedProto = r.Header.Get("X-Forwarded-Proto")
			upstreamForwardedHost = r.Header.Get("X-Forwarded-Host")
			w.WriteHeader(http.StatusAccepted)
			_, _ = w.Write([]byte("proxied"))
		}))
		defer target.Close()

		registerTestModule(t, s, "proxy-trust-headers", registry.EndpointHTTP, target.URL)

		req := httptest.NewRequest(http.MethodGet, "/internal/proxy/proxy-trust-headers/path", nil)
		req.RemoteAddr = "198.51.100.30:4567"
		req.Header.Set("X-Forwarded-For", "203.0.113.10")
		req.Header.Set("X-Forwarded-Proto", "https")
		req.Header.Set("X-Forwarded-Host", "gateway.example.com")
		req = withURLParam(req, "moduleId", "proxy-trust-headers")

		rec := httptest.NewRecorder()
		s.handleModuleProxy(rec, req)
		if rec.Code != http.StatusAccepted {
			t.Fatalf("expected %d, got %d", http.StatusAccepted, rec.Code)
		}
		if upstreamForwardedFor != "203.0.113.10, 198.51.100.30, 198.51.100.30" {
			t.Fatalf("expected trusted forwarded-for chain, got %q", upstreamForwardedFor)
		}
		if upstreamForwardedProto != "https" {
			t.Fatalf("expected trusted forwarded proto, got %q", upstreamForwardedProto)
		}
		if upstreamForwardedHost != "gateway.example.com" {
			t.Fatalf("expected trusted forwarded host, got %q", upstreamForwardedHost)
		}
	})

	t.Run("policy deny returns forbidden with reason", func(t *testing.T) {
		s.cfg.Policy.Enabled = true
		s.cfg.Policy.DefaultModel = "rbac"

		target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusAccepted)
			_, _ = w.Write([]byte("proxied"))
		}))
		defer target.Close()

		registerTestModule(t, s, "policy-deny", registry.EndpointHTTP, target.URL)
		s.policyChecker = &stubCmdPolicyChecker{resp: &policypb.CheckResponse{
			Allowed:    false,
			ModelUsed:  "abac",
			DenyReason: "abac_deny_rule_matched",
			EvalPath:   []string{"abac:deny:block"},
		}}

		req := httptest.NewRequest(http.MethodPost, "/internal/proxy/policy-deny/private", nil)
		req = withURLParam(req, "moduleId", "policy-deny")
		rec := httptest.NewRecorder()

		s.handleModuleProxy(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("expected %d, got %d", http.StatusForbidden, rec.Code)
		}
		if !strings.Contains(rec.Body.String(), "abac_deny_rule_matched") {
			t.Fatalf("expected deny reason in response body, got %q", rec.Body.String())
		}
	})

	t.Run("policy enabled without checker denies by default", func(t *testing.T) {
		s.cfg.Policy.Enabled = true
		s.cfg.Policy.DefaultModel = "rbac"

		target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusAccepted)
			_, _ = w.Write([]byte("proxied"))
		}))
		defer target.Close()

		registerTestModule(t, s, "policy-required", registry.EndpointHTTP, target.URL)
		s.policyChecker = nil

		req := httptest.NewRequest(http.MethodGet, "/internal/proxy/policy-required/private", nil)
		req = withURLParam(req, "moduleId", "policy-required")
		rec := httptest.NewRecorder()

		s.handleModuleProxy(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("expected %d, got %d", http.StatusForbidden, rec.Code)
		}
		if !strings.Contains(rec.Body.String(), "policy_unavailable") {
			t.Fatalf("expected policy_unavailable deny reason, got %q", rec.Body.String())
		}
	})

	t.Run("policy allow forwards and maps request context", func(t *testing.T) {
		s.cfg.Policy.Enabled = true
		s.cfg.Policy.DefaultModel = "rbac"

		target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusAccepted)
			_, _ = w.Write([]byte("proxied"))
		}))
		defer target.Close()

		registerTestModule(t, s, "policy-allow", registry.EndpointHTTP, target.URL)
		checker := &stubCmdPolicyChecker{resp: &policypb.CheckResponse{Allowed: true, ModelUsed: "rbac", EvalPath: []string{"rbac:allow"}}}
		s.policyChecker = checker

		req := httptest.NewRequest(http.MethodGet, "/internal/proxy/policy-allow/resource", nil)
		req.RemoteAddr = "203.0.113.99:4321"
		req.Header.Set("X-Request-ID", "rid-cmd-1")
		req = withURLParam(req, "moduleId", "policy-allow")
		rec := httptest.NewRecorder()

		s.handleModuleProxy(rec, req)
		if rec.Code != http.StatusAccepted {
			t.Fatalf("expected %d, got %d", http.StatusAccepted, rec.Code)
		}
		if checker.lastReq == nil {
			t.Fatalf("expected policy check request to be captured")
		}
		if checker.lastReq.GetContext().GetIp() != "203.0.113.99" {
			t.Fatalf("expected mapped client ip, got %q", checker.lastReq.GetContext().GetIp())
		}
		if checker.lastReq.GetContext().GetExtra()["module_id"] != "policy-allow" {
			t.Fatalf("expected module_id extra context")
		}
	})

	t.Run("runtime policy disable bypasses policy checker", func(t *testing.T) {
		target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusAccepted)
			_, _ = w.Write([]byte("proxied"))
		}))
		defer target.Close()

		registerTestModule(t, s, "policy-runtime-off", registry.EndpointHTTP, target.URL)
		checker := &stubCmdPolicyChecker{resp: &policypb.CheckResponse{Allowed: false, DenyReason: "should_not_apply"}}
		s.policyChecker = checker

		recSeed := httptest.NewRecorder()
		reqSeed := httptest.NewRequest(http.MethodPatch, "/aegion/api/v1/system/config", bytes.NewBufferString(`{"policy":{"enabled":false,"rbac":{"enabled":true}}}`))
		s.handleAdminUpdateConfig(recSeed, reqSeed)
		if recSeed.Code != http.StatusInternalServerError {
			t.Fatalf("expected runtime config persist to fail without db, got %d", recSeed.Code)
		}

		// Runtime load should gracefully fall back to bootstrap config when DB is unavailable.
		s.cfg.Policy.Enabled = false

		req := httptest.NewRequest(http.MethodGet, "/internal/proxy/policy-runtime-off/resource", nil)
		req = withURLParam(req, "moduleId", "policy-runtime-off")
		rec := httptest.NewRecorder()

		s.handleModuleProxy(rec, req)
		if rec.Code != http.StatusAccepted {
			t.Fatalf("expected %d, got %d", http.StatusAccepted, rec.Code)
		}
		if checker.lastReq != nil {
			t.Fatalf("expected policy checker to be bypassed when runtime policy is disabled")
		}
	})

	t.Run("runtime proxy timeout applies from config", func(t *testing.T) {
		target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			time.Sleep(80 * time.Millisecond)
			w.WriteHeader(http.StatusAccepted)
			_, _ = w.Write([]byte("slow proxied"))
		}))
		defer target.Close()

		registerTestModule(t, s, "proxy-runtime-timeout", registry.EndpointHTTP, target.URL)
		s.cfg.Proxy.UpstreamTimeout = config.Duration(20 * time.Millisecond)

		req := httptest.NewRequest(http.MethodGet, "/internal/proxy/proxy-runtime-timeout/resource", nil)
		req = withURLParam(req, "moduleId", "proxy-runtime-timeout")
		rec := httptest.NewRecorder()

		s.handleModuleProxy(rec, req)
		if rec.Code != http.StatusGatewayTimeout {
			t.Fatalf("expected %d, got %d", http.StatusGatewayTimeout, rec.Code)
		}
	})
}

func TestSetupRoutes_EnablesCORSMiddleware(t *testing.T) {
	s := newTestServer(t)
	s.cfg.Server.CORS.Enabled = true
	router := SetupRoutes(s)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	req.Header.Set("Origin", "https://example.com")
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected %d, got %d", http.StatusOK, rec.Code)
	}
	if rec.Header().Get("Access-Control-Allow-Origin") != "https://example.com" {
		t.Fatalf("expected CORS allow origin header to be set")
	}
}

func TestSetupRoutes_InternalAuthAndAdminMount(t *testing.T) {
	s := newTestServer(t)
	router := SetupRoutes(s)

	t.Run("internal health bypasses auth middleware", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/internal/health", nil)
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected %d, got %d", http.StatusOK, rec.Code)
		}
	})

	t.Run("internal route requires auth token", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/internal/registry/modules", nil)
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected %d, got %d", http.StatusUnauthorized, rec.Code)
		}
	})

	t.Run("internal route accepts valid auth token", func(t *testing.T) {
		token, err := s.tokenGen.Generate("module-a")
		if err != nil {
			t.Fatalf("failed to generate token: %v", err)
		}

		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/internal/registry/modules", nil)
		req.Header.Set(authtoken.HeaderInternalToken, token)
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected %d, got %d", http.StatusOK, rec.Code)
		}
	})

	t.Run("admin route not mounted when disabled", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/aegion/api/v1/system/health", nil)
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected %d, got %d", http.StatusNotFound, rec.Code)
		}
	})

	t.Run("admin route mounted when enabled", func(t *testing.T) {
		s2 := newTestServer(t)
		s2.cfg.Admin.Enabled = true
		router2 := SetupRoutes(s2)

		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/aegion/api/v1/system/health", nil)
		router2.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected %d, got %d", http.StatusUnauthorized, rec.Code)
		}
	})

	t.Run("admin route accepts valid internal auth token", func(t *testing.T) {
		s2 := newTestServer(t)
		s2.cfg.Admin.Enabled = true
		router2 := SetupRoutes(s2)

		token, err := s2.tokenGen.Generate("admin")
		if err != nil {
			t.Fatalf("failed to generate token: %v", err)
		}

		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/aegion/api/v1/system/health", nil)
		req.Header.Set(authtoken.HeaderInternalToken, token)
		router2.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected %d, got %d", http.StatusOK, rec.Code)
		}
	})

	t.Run("admin route rejects non-admin module token", func(t *testing.T) {
		s2 := newTestServer(t)
		s2.cfg.Admin.Enabled = true
		router2 := SetupRoutes(s2)

		token, err := s2.tokenGen.Generate("password")
		if err != nil {
			t.Fatalf("failed to generate token: %v", err)
		}

		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/aegion/api/v1/system/health", nil)
		req.Header.Set(authtoken.HeaderInternalToken, token)
		router2.ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("expected %d, got %d", http.StatusForbidden, rec.Code)
		}
	})
}

type routeFlowStore struct {
	flows     map[uuid.UUID]*flows.Flow
	createErr error
	getErr    error
	updateErr error
}

func newRouteFlowStore() *routeFlowStore {
	return &routeFlowStore{
		flows: make(map[uuid.UUID]*flows.Flow),
	}
}

func (s *routeFlowStore) Create(_ context.Context, flow *flows.Flow) error {
	if s.createErr != nil {
		return s.createErr
	}
	s.flows[flow.ID] = flow
	return nil
}

func (s *routeFlowStore) Get(_ context.Context, id uuid.UUID) (*flows.Flow, error) {
	if s.getErr != nil {
		return nil, s.getErr
	}
	flow, ok := s.flows[id]
	if !ok {
		return nil, flows.ErrFlowNotFound
	}
	return flow, nil
}

func (s *routeFlowStore) GetByCSRF(_ context.Context, csrfToken string) (*flows.Flow, error) {
	for _, flow := range s.flows {
		if flow.CSRFToken == csrfToken {
			return flow, nil
		}
	}
	return nil, flows.ErrFlowNotFound
}

func (s *routeFlowStore) Update(_ context.Context, flow *flows.Flow) error {
	if s.updateErr != nil {
		return s.updateErr
	}
	if _, ok := s.flows[flow.ID]; !ok {
		return flows.ErrFlowNotFound
	}
	s.flows[flow.ID] = flow
	return nil
}

func (s *routeFlowStore) Delete(_ context.Context, id uuid.UUID) error {
	if _, ok := s.flows[id]; !ok {
		return flows.ErrFlowNotFound
	}
	delete(s.flows, id)
	return nil
}

func (s *routeFlowStore) DeleteExpired(_ context.Context) (int64, error) {
	return 0, nil
}

func (s *routeFlowStore) ListByIdentity(_ context.Context, identityID uuid.UUID, flowType flows.FlowType) ([]*flows.Flow, error) {
	result := make([]*flows.Flow, 0)
	for _, flow := range s.flows {
		if flow.IdentityID != nil && *flow.IdentityID == identityID && flow.Type == flowType {
			result = append(result, flow)
		}
	}
	return result, nil
}

func newFlowServer(t *testing.T) (*Server, *routeFlowStore) {
	t.Helper()

	s := newTestServer(t)
	store := newRouteFlowStore()
	s.flowService = flows.NewService(store, flows.DefaultConfig())
	return s, store
}

func TestSelfServiceFlowInitHandlers(t *testing.T) {
	t.Run("create flow failures", func(t *testing.T) {
		s, store := newFlowServer(t)
		store.createErr = errors.New("create failed")
		s.sessionManager = &stubRouteSessionManager{
			session: &session.Session{
				ID:         uuid.New(),
				IdentityID: uuid.New(),
				AAL:        session.AAL1,
				Active:     true,
			},
		}

		handlers := []struct {
			name    string
			handler func(http.ResponseWriter, *http.Request)
			path    string
		}{
			{"login browser", s.handleInitLoginBrowser, "/api/v1/self-service/login/browser"},
			{"login api", s.handleInitLoginAPI, "/api/v1/self-service/login/api"},
			{"registration browser", s.handleInitRegistrationBrowser, "/api/v1/self-service/registration/browser"},
			{"registration api", s.handleInitRegistrationAPI, "/api/v1/self-service/registration/api"},
			{"recovery browser", s.handleInitRecoveryBrowser, "/api/v1/self-service/recovery/browser"},
			{"recovery api", s.handleInitRecoveryAPI, "/api/v1/self-service/recovery/api"},
			{"settings browser", s.handleInitSettingsBrowser, "/api/v1/self-service/settings/browser"},
			{"settings api", s.handleInitSettingsAPI, "/api/v1/self-service/settings/api"},
			{"verification browser", s.handleInitVerificationBrowser, "/api/v1/self-service/verification/browser"},
			{"verification api", s.handleInitVerificationAPI, "/api/v1/self-service/verification/api"},
		}

		for _, tc := range handlers {
			t.Run(tc.name, func(t *testing.T) {
				rec := httptest.NewRecorder()
				req := httptest.NewRequest(http.MethodGet, tc.path, nil)
				tc.handler(rec, req)
				if rec.Code != http.StatusInternalServerError {
					t.Fatalf("expected %d, got %d", http.StatusInternalServerError, rec.Code)
				}
			})
		}
	})

	t.Run("successful browser and api initialization", func(t *testing.T) {
		s, _ := newFlowServer(t)
		s.sessionManager = &stubRouteSessionManager{
			session: &session.Session{
				ID:         uuid.New(),
				IdentityID: uuid.New(),
				AAL:        session.AAL1,
				Active:     true,
			},
		}

		browserCases := []struct {
			name     string
			path     string
			location string
			handler  func(http.ResponseWriter, *http.Request)
		}{
			{"login", "/api/v1/self-service/login/browser", "/ui/login?flow=", s.handleInitLoginBrowser},
			{"registration", "/api/v1/self-service/registration/browser", "/ui/registration?flow=", s.handleInitRegistrationBrowser},
			{"recovery", "/api/v1/self-service/recovery/browser", "/ui/recovery?flow=", s.handleInitRecoveryBrowser},
			{"settings", "/api/v1/self-service/settings/browser", "/ui/settings?flow=", s.handleInitSettingsBrowser},
			{"verification", "/api/v1/self-service/verification/browser", "/ui/verification?flow=", s.handleInitVerificationBrowser},
		}

		for _, tc := range browserCases {
			t.Run(tc.name+" browser", func(t *testing.T) {
				rec := httptest.NewRecorder()
				req := httptest.NewRequest(http.MethodGet, tc.path, nil)
				tc.handler(rec, req)
				if rec.Code != http.StatusSeeOther {
					t.Fatalf("expected %d, got %d", http.StatusSeeOther, rec.Code)
				}
				if !strings.HasPrefix(rec.Header().Get("Location"), tc.location) {
					t.Fatalf("expected redirect prefix %q, got %q", tc.location, rec.Header().Get("Location"))
				}
			})
		}

		apiCases := []struct {
			name    string
			path    string
			want    flows.FlowType
			handler func(http.ResponseWriter, *http.Request)
		}{
			{"login", "/api/v1/self-service/login/api", flows.TypeLogin, s.handleInitLoginAPI},
			{"registration", "/api/v1/self-service/registration/api", flows.TypeRegistration, s.handleInitRegistrationAPI},
			{"recovery", "/api/v1/self-service/recovery/api", flows.TypeRecovery, s.handleInitRecoveryAPI},
			{"settings", "/api/v1/self-service/settings/api", flows.TypeSettings, s.handleInitSettingsAPI},
			{"verification", "/api/v1/self-service/verification/api", flows.TypeVerification, s.handleInitVerificationAPI},
		}

		for _, tc := range apiCases {
			t.Run(tc.name+" api", func(t *testing.T) {
				rec := httptest.NewRecorder()
				req := httptest.NewRequest(http.MethodGet, tc.path, nil)
				tc.handler(rec, req)
				if rec.Code != http.StatusOK {
					t.Fatalf("expected %d, got %d", http.StatusOK, rec.Code)
				}
				var flow flows.Flow
				if err := json.NewDecoder(rec.Body).Decode(&flow); err != nil {
					t.Fatalf("failed to decode flow: %v", err)
				}
				if flow.Type != tc.want {
					t.Fatalf("expected flow type %q, got %q", tc.want, flow.Type)
				}
			})
		}
	})

	t.Run("settings requires active session", func(t *testing.T) {
		s, _ := newFlowServer(t)
		s.sessionManager = &stubRouteSessionManager{getErr: session.ErrSessionNotFound}

		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/v1/self-service/settings/browser", nil)
		s.handleInitSettingsBrowser(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected %d, got %d", http.StatusUnauthorized, rec.Code)
		}

		rec = httptest.NewRecorder()
		req = httptest.NewRequest(http.MethodGet, "/api/v1/self-service/settings/api", nil)
		s.handleInitSettingsAPI(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected %d, got %d", http.StatusUnauthorized, rec.Code)
		}
	})
}

func TestSelfServiceFlowSubmitHandlers(t *testing.T) {
	s, _ := newFlowServer(t)

	t.Run("payload validation", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/v1/self-service/login", mustJSONBody(t, map[string]any{
			"flow_id":    "not-a-uuid",
			"csrf_token": "token",
		}))
		req.Header.Set("Content-Type", "application/json")
		s.handleSubmitLogin(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected %d, got %d", http.StatusBadRequest, rec.Code)
		}

		rec = httptest.NewRecorder()
		req = httptest.NewRequest(http.MethodPost, "/api/v1/self-service/login", mustJSONBody(t, map[string]any{
			"flow_id": uuid.New().String(),
		}))
		req.Header.Set("Content-Type", "application/json")
		s.handleSubmitLogin(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected %d, got %d", http.StatusBadRequest, rec.Code)
		}
	})

	t.Run("flow validation errors", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/v1/self-service/login", mustJSONBody(t, map[string]any{
			"flow_id":    uuid.New().String(),
			"csrf_token": "token",
		}))
		req.Header.Set("Content-Type", "application/json")
		s.handleSubmitLogin(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected %d, got %d", http.StatusNotFound, rec.Code)
		}

		loginFlow, err := s.flowService.CreateLoginFlow(context.Background(), "http://example.com/login")
		if err != nil {
			t.Fatalf("failed to create flow: %v", err)
		}
		rec = httptest.NewRecorder()
		req = httptest.NewRequest(http.MethodPost, "/api/v1/self-service/login", mustJSONBody(t, map[string]any{
			"flow_id":    loginFlow.ID.String(),
			"csrf_token": "wrong-token",
		}))
		req.Header.Set("Content-Type", "application/json")
		s.handleSubmitLogin(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("expected %d, got %d", http.StatusForbidden, rec.Code)
		}
	})

	t.Run("flow type mismatch", func(t *testing.T) {
		regFlow, err := s.flowService.CreateRegistrationFlow(context.Background(), "http://example.com/registration")
		if err != nil {
			t.Fatalf("failed to create registration flow: %v", err)
		}

		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/v1/self-service/login", mustJSONBody(t, map[string]any{
			"flow_id":    regFlow.ID.String(),
			"csrf_token": regFlow.CSRFToken,
		}))
		req.Header.Set("Content-Type", "application/json")
		s.handleSubmitLogin(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected %d, got %d", http.StatusBadRequest, rec.Code)
		}
	})

	t.Run("successful submissions", func(t *testing.T) {
		cases := []struct {
			name    string
			handler func(http.ResponseWriter, *http.Request)
			create  func() (*flows.Flow, error)
			path    string
		}{
			{
				name:    "login",
				handler: s.handleSubmitLogin,
				path:    "/api/v1/self-service/login",
				create: func() (*flows.Flow, error) {
					return s.flowService.CreateLoginFlow(context.Background(), "http://example.com/login")
				},
			},
			{
				name:    "registration",
				handler: s.handleSubmitRegistration,
				path:    "/api/v1/self-service/registration",
				create: func() (*flows.Flow, error) {
					return s.flowService.CreateRegistrationFlow(context.Background(), "http://example.com/registration")
				},
			},
			{
				name:    "recovery",
				handler: s.handleSubmitRecovery,
				path:    "/api/v1/self-service/recovery",
				create: func() (*flows.Flow, error) {
					return s.flowService.CreateRecoveryFlow(context.Background(), "http://example.com/recovery")
				},
			},
			{
				name:    "settings",
				handler: s.handleSubmitSettings,
				path:    "/api/v1/self-service/settings",
				create: func() (*flows.Flow, error) {
					return s.flowService.CreateSettingsFlow(context.Background(), "http://example.com/settings", uuid.New(), uuid.New())
				},
			},
			{
				name:    "verification",
				handler: s.handleSubmitVerification,
				path:    "/api/v1/self-service/verification",
				create: func() (*flows.Flow, error) {
					return s.flowService.CreateVerificationFlow(context.Background(), "http://example.com/verification", nil)
				},
			},
		}

		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				flow, err := tc.create()
				if err != nil {
					t.Fatalf("failed to create flow: %v", err)
				}

				rec := httptest.NewRecorder()
				req := httptest.NewRequest(http.MethodPost, tc.path, mustJSONBody(t, map[string]any{
					"flow_id":    flow.ID.String(),
					"csrf_token": flow.CSRFToken,
				}))
				req.Header.Set("Content-Type", "application/json")
				tc.handler(rec, req)

				if rec.Code != http.StatusOK {
					t.Fatalf("expected %d, got %d", http.StatusOK, rec.Code)
				}
			})
		}
	})
}

func TestSelfServiceFlowGetHandlers(t *testing.T) {
	s, _ := newFlowServer(t)
	ctx := context.Background()

	loginFlow, err := s.flowService.CreateLoginFlow(ctx, "http://example.com/login")
	if err != nil {
		t.Fatalf("failed to create login flow: %v", err)
	}
	regFlow, err := s.flowService.CreateRegistrationFlow(ctx, "http://example.com/registration")
	if err != nil {
		t.Fatalf("failed to create registration flow: %v", err)
	}
	recoveryFlow, err := s.flowService.CreateRecoveryFlow(ctx, "http://example.com/recovery")
	if err != nil {
		t.Fatalf("failed to create recovery flow: %v", err)
	}
	settingsFlow, err := s.flowService.CreateSettingsFlow(ctx, "http://example.com/settings", uuid.New(), uuid.New())
	if err != nil {
		t.Fatalf("failed to create settings flow: %v", err)
	}
	verificationFlow, err := s.flowService.CreateVerificationFlow(ctx, "http://example.com/verification", nil)
	if err != nil {
		t.Fatalf("failed to create verification flow: %v", err)
	}

	cases := []struct {
		name    string
		handler func(http.ResponseWriter, *http.Request)
		id      uuid.UUID
	}{
		{"login", s.handleGetLoginFlow, loginFlow.ID},
		{"registration", s.handleGetRegistrationFlow, regFlow.ID},
		{"recovery", s.handleGetRecoveryFlow, recoveryFlow.ID},
		{"settings", s.handleGetSettingsFlow, settingsFlow.ID},
		{"verification", s.handleGetVerificationFlow, verificationFlow.ID},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/flows", nil)
			tc.handler(rec, req)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("expected %d for missing id, got %d", http.StatusBadRequest, rec.Code)
			}

			rec = httptest.NewRecorder()
			req = httptest.NewRequest(http.MethodGet, "/flows?id=not-a-uuid", nil)
			tc.handler(rec, req)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("expected %d for invalid id, got %d", http.StatusBadRequest, rec.Code)
			}

			rec = httptest.NewRecorder()
			req = httptest.NewRequest(http.MethodGet, "/flows?id="+uuid.New().String(), nil)
			tc.handler(rec, req)
			if rec.Code != http.StatusNotFound {
				t.Fatalf("expected %d for missing flow, got %d", http.StatusNotFound, rec.Code)
			}

			rec = httptest.NewRecorder()
			req = httptest.NewRequest(http.MethodGet, "/flows?id="+tc.id.String(), nil)
			tc.handler(rec, req)
			if rec.Code != http.StatusOK {
				t.Fatalf("expected %d for existing flow, got %d", http.StatusOK, rec.Code)
			}
		})
	}
}

func TestInternalFlowHandlers(t *testing.T) {
	t.Run("get flow", func(t *testing.T) {
		s, _ := newFlowServer(t)
		created, err := s.flowService.CreateLoginFlow(context.Background(), "http://example.com/login")
		if err != nil {
			t.Fatalf("failed to create flow: %v", err)
		}

		rec := httptest.NewRecorder()
		req := withURLParam(httptest.NewRequest(http.MethodGet, "/internal/flows/not-a-uuid", nil), "id", "not-a-uuid")
		s.handleInternalGetFlow(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected %d, got %d", http.StatusBadRequest, rec.Code)
		}

		rec = httptest.NewRecorder()
		req = withURLParam(httptest.NewRequest(http.MethodGet, "/internal/flows/missing", nil), "id", uuid.New().String())
		s.handleInternalGetFlow(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected %d, got %d", http.StatusNotFound, rec.Code)
		}

		rec = httptest.NewRecorder()
		req = withURLParam(httptest.NewRequest(http.MethodGet, "/internal/flows/"+created.ID.String(), nil), "id", created.ID.String())
		s.handleInternalGetFlow(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected %d, got %d", http.StatusOK, rec.Code)
		}
	})

	t.Run("complete flow", func(t *testing.T) {
		s, store := newFlowServer(t)
		created, err := s.flowService.CreateLoginFlow(context.Background(), "http://example.com/login")
		if err != nil {
			t.Fatalf("failed to create flow: %v", err)
		}

		rec := httptest.NewRecorder()
		req := withURLParam(httptest.NewRequest(http.MethodPost, "/internal/flows/not-a-uuid/complete", nil), "id", "not-a-uuid")
		s.handleInternalCompleteFlow(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected %d, got %d", http.StatusBadRequest, rec.Code)
		}

		rec = httptest.NewRecorder()
		req = withURLParam(httptest.NewRequest(http.MethodPost, "/internal/flows/missing/complete", nil), "id", uuid.New().String())
		s.handleInternalCompleteFlow(rec, req)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("expected %d, got %d", http.StatusInternalServerError, rec.Code)
		}

		store.updateErr = errors.New("update failed")
		rec = httptest.NewRecorder()
		req = withURLParam(httptest.NewRequest(http.MethodPost, "/internal/flows/"+created.ID.String()+"/complete", nil), "id", created.ID.String())
		s.handleInternalCompleteFlow(rec, req)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("expected %d, got %d", http.StatusInternalServerError, rec.Code)
		}

		store.updateErr = nil
		freshFlow, err := s.flowService.CreateLoginFlow(context.Background(), "http://example.com/login-2")
		if err != nil {
			t.Fatalf("failed to create fresh flow: %v", err)
		}
		rec = httptest.NewRecorder()
		req = withURLParam(httptest.NewRequest(http.MethodPost, "/internal/flows/"+freshFlow.ID.String()+"/complete", nil), "id", freshFlow.ID.String())
		s.handleInternalCompleteFlow(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected %d, got %d", http.StatusOK, rec.Code)
		}
	})

	t.Run("fail flow", func(t *testing.T) {
		s, store := newFlowServer(t)
		created, err := s.flowService.CreateLoginFlow(context.Background(), "http://example.com/login")
		if err != nil {
			t.Fatalf("failed to create flow: %v", err)
		}

		rec := httptest.NewRecorder()
		req := withURLParam(httptest.NewRequest(http.MethodPost, "/internal/flows/not-a-uuid/fail", bytes.NewBufferString(`{"error":"x"}`)), "id", "not-a-uuid")
		s.handleInternalFailFlow(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected %d, got %d", http.StatusBadRequest, rec.Code)
		}

		rec = httptest.NewRecorder()
		req = withURLParam(httptest.NewRequest(http.MethodPost, "/internal/flows/"+created.ID.String()+"/fail", bytes.NewBufferString("{")), "id", created.ID.String())
		s.handleInternalFailFlow(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected %d, got %d", http.StatusBadRequest, rec.Code)
		}

		rec = httptest.NewRecorder()
		req = withURLParam(httptest.NewRequest(http.MethodPost, "/internal/flows/missing/fail", bytes.NewBufferString(`{"error":"x"}`)), "id", uuid.New().String())
		s.handleInternalFailFlow(rec, req)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("expected %d, got %d", http.StatusInternalServerError, rec.Code)
		}

		store.updateErr = errors.New("update failed")
		rec = httptest.NewRecorder()
		req = withURLParam(httptest.NewRequest(http.MethodPost, "/internal/flows/"+created.ID.String()+"/fail", bytes.NewBufferString(`{"error":"x"}`)), "id", created.ID.String())
		s.handleInternalFailFlow(rec, req)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("expected %d, got %d", http.StatusInternalServerError, rec.Code)
		}

		store.updateErr = nil
		rec = httptest.NewRecorder()
		req = withURLParam(httptest.NewRequest(http.MethodPost, "/internal/flows/"+created.ID.String()+"/fail", bytes.NewBufferString(`{"error":"x"}`)), "id", created.ID.String())
		s.handleInternalFailFlow(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected %d, got %d", http.StatusOK, rec.Code)
		}
	})

	t.Run("update flow ui", func(t *testing.T) {
		s, store := newFlowServer(t)
		created, err := s.flowService.CreateLoginFlow(context.Background(), "http://example.com/login")
		if err != nil {
			t.Fatalf("failed to create flow: %v", err)
		}

		rec := httptest.NewRecorder()
		req := withURLParam(httptest.NewRequest(http.MethodPatch, "/internal/flows/not-a-uuid/ui", bytes.NewBufferString(`{"action":"x","method":"POST"}`)), "id", "not-a-uuid")
		s.handleInternalUpdateFlowUI(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected %d, got %d", http.StatusBadRequest, rec.Code)
		}

		rec = httptest.NewRecorder()
		req = withURLParam(httptest.NewRequest(http.MethodPatch, "/internal/flows/"+created.ID.String()+"/ui", bytes.NewBufferString("{")), "id", created.ID.String())
		s.handleInternalUpdateFlowUI(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected %d, got %d", http.StatusBadRequest, rec.Code)
		}

		rec = httptest.NewRecorder()
		req = withURLParam(httptest.NewRequest(http.MethodPatch, "/internal/flows/missing/ui", bytes.NewBufferString(`{"action":"x","method":"POST"}`)), "id", uuid.New().String())
		s.handleInternalUpdateFlowUI(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected %d, got %d", http.StatusNotFound, rec.Code)
		}

		store.updateErr = errors.New("update failed")
		rec = httptest.NewRecorder()
		req = withURLParam(httptest.NewRequest(http.MethodPatch, "/internal/flows/"+created.ID.String()+"/ui", bytes.NewBufferString(`{"action":"x","method":"POST"}`)), "id", created.ID.String())
		s.handleInternalUpdateFlowUI(rec, req)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("expected %d, got %d", http.StatusInternalServerError, rec.Code)
		}

		store.updateErr = nil
		rec = httptest.NewRecorder()
		req = withURLParam(httptest.NewRequest(http.MethodPatch, "/internal/flows/"+created.ID.String()+"/ui", bytes.NewBufferString(`{"ui":{"action":"x","method":"POST","nodes":[]}}`)), "id", created.ID.String())
		s.handleInternalUpdateFlowUI(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected %d, got %d", http.StatusOK, rec.Code)
		}
	})
}

func TestHandleNotImplemented(t *testing.T) {
	s := newTestServer(t)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	s.handleNotImplemented(rec, req)
	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("expected %d, got %d", http.StatusNotImplemented, rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "not implemented") {
		t.Fatalf("expected not implemented response body, got %q", rec.Body.String())
	}
}

func TestAdminHandlersValidationAndDatabaseGuards(t *testing.T) {
	s := newTestServer(t)

	t.Run("list identities requires database", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/aegion/api/v1/identities", nil)
		s.handleAdminListIdentities(rec, req)
		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("expected %d, got %d", http.StatusServiceUnavailable, rec.Code)
		}
	})

	t.Run("create identity validates request body", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/aegion/api/v1/identities", bytes.NewBufferString("{"))
		s.handleAdminCreateIdentity(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected %d, got %d", http.StatusBadRequest, rec.Code)
		}
	})

	t.Run("create identity requires database", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/aegion/api/v1/identities", bytes.NewBufferString(`{"traits":{"email":"user@example.com"}}`))
		s.handleAdminCreateIdentity(rec, req)
		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("expected %d, got %d", http.StatusServiceUnavailable, rec.Code)
		}
	})

	t.Run("get identity validates id format", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := withURLParam(httptest.NewRequest(http.MethodGet, "/aegion/api/v1/identities/invalid", nil), "id", "invalid")
		s.handleAdminGetIdentity(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected %d, got %d", http.StatusBadRequest, rec.Code)
		}
	})

	t.Run("update identity validates body and payload", func(t *testing.T) {
		identityID := uuid.New().String()

		rec := httptest.NewRecorder()
		req := withURLParam(httptest.NewRequest(http.MethodPatch, "/aegion/api/v1/identities/"+identityID, bytes.NewBufferString("{")), "id", identityID)
		s.handleAdminUpdateIdentity(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected %d, got %d", http.StatusBadRequest, rec.Code)
		}

		rec = httptest.NewRecorder()
		req = withURLParam(httptest.NewRequest(http.MethodPatch, "/aegion/api/v1/identities/"+identityID, bytes.NewBufferString(`{}`)), "id", identityID)
		s.handleAdminUpdateIdentity(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected %d, got %d", http.StatusBadRequest, rec.Code)
		}
	})

	t.Run("delete identity validates id format", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := withURLParam(httptest.NewRequest(http.MethodDelete, "/aegion/api/v1/identities/invalid", nil), "id", "invalid")
		s.handleAdminDeleteIdentity(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected %d, got %d", http.StatusBadRequest, rec.Code)
		}
	})

	t.Run("list sessions requires database", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/aegion/api/v1/sessions", nil)
		s.handleAdminListSessions(rec, req)
		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("expected %d, got %d", http.StatusServiceUnavailable, rec.Code)
		}
	})

	t.Run("delete session validates id format", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := withURLParam(httptest.NewRequest(http.MethodDelete, "/aegion/api/v1/sessions/invalid", nil), "id", "invalid")
		s.handleAdminDeleteSession(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected %d, got %d", http.StatusBadRequest, rec.Code)
		}
	})

	t.Run("delete identity sessions validates id format", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := withURLParam(httptest.NewRequest(http.MethodDelete, "/aegion/api/v1/sessions/identity/invalid", nil), "identityId", "invalid")
		s.handleAdminDeleteIdentitySessions(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected %d, got %d", http.StatusBadRequest, rec.Code)
		}
	})

	t.Run("metrics returns module metrics without database", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/aegion/api/v1/system/metrics", nil)
		s.handleAdminMetrics(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected %d, got %d", http.StatusOK, rec.Code)
		}

		var body map[string]interface{}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("failed to decode metrics response: %v", err)
		}
		if body["database"] != "unavailable" {
			t.Fatalf("expected database=unavailable, got %v", body["database"])
		}
		if _, ok := body["module_count"]; !ok {
			t.Fatalf("expected module_count in metrics response")
		}
	})
}

func TestSessionHandlers(t *testing.T) {
	s := newTestServer(t)

	t.Run("whoami returns unauthorized without session", func(t *testing.T) {
		sm := &stubRouteSessionManager{getErr: session.ErrSessionNotFound}
		s.sessionManager = sm

		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/v1/sessions/whoami", nil)
		s.handleWhoAmI(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected %d, got %d", http.StatusUnauthorized, rec.Code)
		}
	})

	t.Run("whoami returns session payload", func(t *testing.T) {
		sm := &stubRouteSessionManager{
			session: &session.Session{
				ID:         uuid.New(),
				IdentityID: uuid.New(),
				AAL:        session.AAL1,
				Active:     true,
			},
		}
		s.sessionManager = sm

		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/v1/sessions/whoami", nil)
		s.handleWhoAmI(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected %d, got %d", http.StatusOK, rec.Code)
		}

		var body map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("failed to parse response: %v", err)
		}
		if body["id"] == "" {
			t.Fatalf("expected non-empty session id in response")
		}
	})

	t.Run("logout is idempotent for missing session", func(t *testing.T) {
		sm := &stubRouteSessionManager{getErr: session.ErrSessionNotFound}
		s.sessionManager = sm

		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodDelete, "/api/v1/sessions", nil)
		s.handleLogout(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected %d, got %d", http.StatusOK, rec.Code)
		}
		if sm.clearCookie != 1 {
			t.Fatalf("expected clear cookie to be called once, got %d", sm.clearCookie)
		}
	})

	t.Run("logout revokes active session", func(t *testing.T) {
		sessionID := uuid.New()
		sm := &stubRouteSessionManager{
			session: &session.Session{
				ID:         sessionID,
				IdentityID: uuid.New(),
				AAL:        session.AAL1,
				Active:     true,
			},
		}
		s.sessionManager = sm

		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodDelete, "/api/v1/sessions", nil)
		s.handleLogout(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected %d, got %d", http.StatusOK, rec.Code)
		}
		if len(sm.revokedIDs) != 1 || sm.revokedIDs[0] != sessionID {
			t.Fatalf("expected revoked session id %s, got %+v", sessionID, sm.revokedIDs)
		}
		if sm.clearCookie != 1 {
			t.Fatalf("expected clear cookie to be called once, got %d", sm.clearCookie)
		}
	})

	t.Run("logout returns error when revoke fails", func(t *testing.T) {
		sm := &stubRouteSessionManager{
			session: &session.Session{
				ID:         uuid.New(),
				IdentityID: uuid.New(),
				AAL:        session.AAL1,
				Active:     true,
			},
			revokeErr: errors.New("revoke failed"),
		}
		s.sessionManager = sm

		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodDelete, "/api/v1/sessions", nil)
		s.handleLogout(rec, req)

		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("expected %d, got %d", http.StatusInternalServerError, rec.Code)
		}
	})

	t.Run("jwks returns keyset response", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/.well-known/jwks.json", nil)
		s.handleJWKS(rec, req)

		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("expected %d, got %d", http.StatusServiceUnavailable, rec.Code)
		}
	})
}

func TestOIDCRootRoutesProxyToOAuth2Module(t *testing.T) {
	s := newTestServer(t)

	var gotDiscoveryPath string
	var gotJWKSPath string
	var gotUserInfoPath string
	var gotUserInfoAuthz string

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/openid-configuration":
			gotDiscoveryPath = r.URL.Path
			writeJSON(w, http.StatusOK, map[string]any{"issuer": "https://issuer.example"})
		case "/.well-known/jwks.json":
			gotJWKSPath = r.URL.Path
			writeJSON(w, http.StatusOK, map[string]any{"keys": []map[string]string{{"kid": "key-1"}}})
		case "/oidc/userinfo":
			gotUserInfoPath = r.URL.Path
			gotUserInfoAuthz = r.Header.Get("Authorization")
			writeJSON(w, http.StatusOK, map[string]any{"sub": "user-1"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()

	registerTestModule(t, s, "oauth2", registry.EndpointHTTP, upstream.URL)
	if err := s.registry.UpdateStatus("oauth2", registry.StatusHealthy); err != nil {
		t.Fatalf("failed to set oauth2 module healthy: %v", err)
	}

	router := SetupRoutes(s)

	t.Run("discovery proxied from root path", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/.well-known/openid-configuration", nil)
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected %d, got %d", http.StatusOK, rec.Code)
		}
		if gotDiscoveryPath != "/.well-known/openid-configuration" {
			t.Fatalf("unexpected upstream discovery path: %q", gotDiscoveryPath)
		}
	})

	t.Run("jwks proxied from root path", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/.well-known/jwks.json", nil)
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected %d, got %d", http.StatusOK, rec.Code)
		}
		var body map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("failed to parse response: %v", err)
		}
		keys, ok := body["keys"].([]any)
		if !ok {
			t.Fatalf("expected keys array in response")
		}
		if len(keys) != 1 {
			t.Fatalf("expected single key from oauth2 module, got %v", keys)
		}
		if gotJWKSPath != "/.well-known/jwks.json" {
			t.Fatalf("unexpected upstream jwks path: %q", gotJWKSPath)
		}
	})

	t.Run("userinfo proxied to oauth2 module path", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/oauth2/userinfo", nil)
		req.Header.Set("Authorization", "Bearer test-token")
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected %d, got %d", http.StatusOK, rec.Code)
		}
		if gotUserInfoPath != "/oidc/userinfo" {
			t.Fatalf("unexpected upstream userinfo path: %q", gotUserInfoPath)
		}
		if gotUserInfoAuthz != "Bearer test-token" {
			t.Fatalf("expected authorization header to be forwarded, got %q", gotUserInfoAuthz)
		}
	})
}

func TestHandleAdminRestartModule(t *testing.T) {
	s := newTestServer(t)

	t.Run("module id required", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/aegion/api/v1/modules//restart", nil)
		s.handleAdminRestartModule(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected %d, got %d", http.StatusBadRequest, rec.Code)
		}
	})

	t.Run("orchestrator unavailable", func(t *testing.T) {
		registerTestModule(t, s, "password-unavailable", registry.EndpointHTTP, "http://password:8080")
		rec := httptest.NewRecorder()
		req := withURLParam(httptest.NewRequest(http.MethodPost, "/aegion/api/v1/modules/password-unavailable/restart", nil), "id", "password-unavailable")
		s.handleAdminRestartModule(rec, req)
		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("expected %d, got %d", http.StatusServiceUnavailable, rec.Code)
		}
	})

	t.Run("module not found", func(t *testing.T) {
		stub := &stubRouteOrchestrator{restartErr: orchestrator.ErrModuleNotFound}
		s.orchestrator = stub

		rec := httptest.NewRecorder()
		req := withURLParam(httptest.NewRequest(http.MethodPost, "/aegion/api/v1/modules/missing/restart", nil), "id", "missing")
		s.handleAdminRestartModule(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected %d, got %d", http.StatusNotFound, rec.Code)
		}
	})

	t.Run("orchestrator closed", func(t *testing.T) {
		registerTestModule(t, s, "password-closed", registry.EndpointHTTP, "http://password:8080")
		stub := &stubRouteOrchestrator{restartErr: orchestrator.ErrOrchestratorClosed}
		s.orchestrator = stub

		rec := httptest.NewRecorder()
		req := withURLParam(httptest.NewRequest(http.MethodPost, "/aegion/api/v1/modules/password-closed/restart", nil), "id", "password-closed")
		s.handleAdminRestartModule(rec, req)
		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("expected %d, got %d", http.StatusServiceUnavailable, rec.Code)
		}
	})

	t.Run("restart error", func(t *testing.T) {
		registerTestModule(t, s, "password-error", registry.EndpointHTTP, "http://password:8080")
		stub := &stubRouteOrchestrator{restartErr: errors.New("restart failed")}
		s.orchestrator = stub

		rec := httptest.NewRecorder()
		req := withURLParam(httptest.NewRequest(http.MethodPost, "/aegion/api/v1/modules/password-error/restart", nil), "id", "password-error")
		s.handleAdminRestartModule(rec, req)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("expected %d, got %d", http.StatusInternalServerError, rec.Code)
		}
	})

	t.Run("success", func(t *testing.T) {
		registerTestModule(t, s, "password-success", registry.EndpointHTTP, "http://password:8080")
		stub := &stubRouteOrchestrator{}
		s.orchestrator = stub

		rec := httptest.NewRecorder()
		req := withURLParam(httptest.NewRequest(http.MethodPost, "/aegion/api/v1/modules/password-success/restart", nil), "id", "password-success")
		s.handleAdminRestartModule(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected %d, got %d", http.StatusOK, rec.Code)
		}
		if len(stub.restartCalls) != 1 || stub.restartCalls[0] != "password-success" {
			t.Fatalf("expected restart to be called for password-success, got %+v", stub.restartCalls)
		}

		var resp map[string]string
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed to parse response: %v", err)
		}
		if resp["status"] != "restarted" {
			t.Fatalf("expected status restarted, got %q", resp["status"])
		}
		if resp["module_id"] != "password-success" {
			t.Fatalf("expected module_id=password-success, got %q", resp["module_id"])
		}
	})
}

func TestHandleAdminGetConfig_DefaultsWithoutDB(t *testing.T) {
	s := newTestServer(t)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/aegion/api/v1/system/config", nil)
	s.handleAdminGetConfig(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected %d, got %d", http.StatusOK, rec.Code)
	}

	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	policy, ok := resp["policy"].(map[string]any)
	if !ok {
		t.Fatalf("expected policy section")
	}
	if policy["default_model"] != "rbac" {
		t.Fatalf("expected default policy model rbac, got %v", policy["default_model"])
	}

	proxy, ok := resp["proxy"].(map[string]any)
	if !ok {
		t.Fatalf("expected proxy section")
	}
	if proxy["upstream_timeout"] != "30s" {
		t.Fatalf("expected default upstream_timeout 30s, got %v", proxy["upstream_timeout"])
	}
	if proxy["identity_signature_header"] != "X-Aegion-Signature" {
		t.Fatalf("expected default identity signature header, got %v", proxy["identity_signature_header"])
	}
	if proxy["identity_signing_secret_set"] != false {
		t.Fatalf("expected identity_signing_secret_set=false, got %v", proxy["identity_signing_secret_set"])
	}
	if proxy["trust_forwarded_headers"] != false {
		t.Fatalf("expected trust_forwarded_headers=false by default, got %v", proxy["trust_forwarded_headers"])
	}
}

func TestHandleAdminUpdateConfig_ValidationAndDBErrors(t *testing.T) {
	s := newTestServer(t)

	t.Run("empty payload", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPatch, "/aegion/api/v1/system/config", bytes.NewBufferString(`{}`))
		s.handleAdminUpdateConfig(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected %d, got %d", http.StatusBadRequest, rec.Code)
		}
	})

	t.Run("invalid policy model", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPatch, "/aegion/api/v1/system/config", bytes.NewBufferString(`{"policy":{"default_model":"unknown"}}`))
		s.handleAdminUpdateConfig(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected %d, got %d", http.StatusBadRequest, rec.Code)
		}
	})

	t.Run("unknown top-level field", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPatch, "/aegion/api/v1/system/config", bytes.NewBufferString(`{"unknown":true}`))
		s.handleAdminUpdateConfig(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected %d, got %d", http.StatusBadRequest, rec.Code)
		}
	})

	t.Run("default model must be enabled when policy enabled", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPatch, "/aegion/api/v1/system/config", bytes.NewBufferString(`{"policy":{"enabled":true,"default_model":"abac","rbac":{"enabled":true},"abac":{"enabled":false},"rebac":{"enabled":false}}}`))
		s.handleAdminUpdateConfig(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected %d, got %d", http.StatusBadRequest, rec.Code)
		}
	})

	t.Run("policy disabled allows all model toggles off", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPatch, "/aegion/api/v1/system/config", bytes.NewBufferString(`{"policy":{"enabled":false,"rbac":{"enabled":false},"abac":{"enabled":false},"rebac":{"enabled":false}}}`))
		s.handleAdminUpdateConfig(rec, req)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("expected %d, got %d", http.StatusInternalServerError, rec.Code)
		}
		if !strings.Contains(rec.Body.String(), "failed to persist policy runtime config") {
			t.Fatalf("expected generic persistence error, got %q", rec.Body.String())
		}
	})

	t.Run("invalid proxy duration", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPatch, "/aegion/api/v1/system/config", bytes.NewBufferString(`{"proxy":{"upstream_timeout":"not-a-duration"}}`))
		s.handleAdminUpdateConfig(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected %d, got %d", http.StatusBadRequest, rec.Code)
		}
	})

	t.Run("strip-disabled proxy config requires bootstrap signing secret", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPatch, "/aegion/api/v1/system/config", bytes.NewBufferString(`{"proxy":{"strip_inbound_identity_headers":false}}`))
		s.handleAdminUpdateConfig(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected %d, got %d", http.StatusBadRequest, rec.Code)
		}
	})

	t.Run("valid payload without db", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPatch, "/aegion/api/v1/system/config", bytes.NewBufferString(`{"policy":{"enabled":true,"rbac":{"enabled":true}}}`))
		s.handleAdminUpdateConfig(rec, req)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("expected %d, got %d", http.StatusInternalServerError, rec.Code)
		}
		if !strings.Contains(rec.Body.String(), "failed to persist policy runtime config") {
			t.Fatalf("expected generic persistence error, got %q", rec.Body.String())
		}
	})
}

func TestHandleAdminListModules(t *testing.T) {
	s := newTestServer(t)

	// Register a test module
	_, err := s.registry.Register(registry.RegistrationRequest{
		ID:        "test-module",
		Name:      "Test Module",
		Version:   "v1.0.0",
		Endpoints: []registry.Endpoint{{Type: registry.EndpointHTTP, URL: "http://localhost:8001"}},
		HealthURL: "http://localhost:8001/health",
	})
	if err != nil {
		t.Fatalf("failed to register module: %v", err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/admin/modules", nil)
	s.handleAdminListModules(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected %d, got %d", http.StatusOK, rec.Code)
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	modules, ok := resp["modules"].([]interface{})
	if !ok {
		t.Fatalf("expected modules array in response")
	}
	if len(modules) != 1 {
		t.Fatalf("expected 1 module, got %d", len(modules))
	}
}

func TestHandleAdminGetModule(t *testing.T) {
	s := newTestServer(t)

	// Register a test module
	_, err := s.registry.Register(registry.RegistrationRequest{
		ID:        "test-module",
		Name:      "Test Module",
		Version:   "v1.0.0",
		Endpoints: []registry.Endpoint{{Type: registry.EndpointHTTP, URL: "http://localhost:8001"}},
		HealthURL: "http://localhost:8001/health",
	})
	if err != nil {
		t.Fatalf("failed to register module: %v", err)
	}

	t.Run("existing module", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := withURLParam(httptest.NewRequest(http.MethodGet, "/admin/modules/test-module", nil), "id", "test-module")
		s.handleAdminGetModule(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected %d, got %d", http.StatusOK, rec.Code)
		}
	})

	t.Run("non-existent module", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := withURLParam(httptest.NewRequest(http.MethodGet, "/admin/modules/non-existent", nil), "id", "non-existent")
		s.handleAdminGetModule(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected %d, got %d", http.StatusNotFound, rec.Code)
		}
	})
}
