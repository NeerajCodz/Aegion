package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/aegion/aegion/core/authtoken"
	"github.com/aegion/aegion/core/registry"
	"github.com/aegion/aegion/internal/platform/config"
	"github.com/aegion/aegion/internal/platform/logger"
)

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
		registry: registry.New(registry.DefaultConfig()),
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
			ID:      "password",
			Name:    "password",
			Version: "v1.0.0",
			Endpoints: []registry.Endpoint{
				{Type: registry.EndpointHTTP, URL: "http://localhost:9000"},
			},
			HealthURL: "http://localhost:9000/health",
		}))
		rec = httptest.NewRecorder()
		s.handleModuleRegister(rec, req)
		if rec.Code != http.StatusConflict {
			t.Fatalf("expected %d, got %d", http.StatusConflict, rec.Code)
		}
	})

	t.Run("deregister and not found", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/internal/registry/deregister", mustJSONBody(t, registry.DeregistrationRequest{
			ModuleID: "password",
		}))
		rec := httptest.NewRecorder()
		s.handleModuleDeregister(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected %d, got %d", http.StatusOK, rec.Code)
		}

		rec = httptest.NewRecorder()
		req = httptest.NewRequest(http.MethodPost, "/internal/registry/deregister", mustJSONBody(t, registry.DeregistrationRequest{
			ModuleID: "password",
		}))
		s.handleModuleDeregister(rec, req)
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
		req = req.WithContext(context.WithValue(req.Context(), authtoken.ContextKeyModuleID, "heartbeat-module"))
		s.handleModuleHeartbeat(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected %d, got %d", http.StatusOK, rec.Code)
		}
	})
}

func TestModuleProxyHandler(t *testing.T) {
	s := newTestServer(t)

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
		target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusAccepted)
			_, _ = w.Write([]byte("proxied"))
		}))
		defer target.Close()

		registerTestModule(t, s, "proxy-ok", registry.EndpointHTTP, target.URL)

		req := httptest.NewRequest(http.MethodGet, "/internal/proxy/proxy-ok/path", nil)
		req = withURLParam(req, "moduleId", "proxy-ok")
		rec := httptest.NewRecorder()
		s.handleModuleProxy(rec, req)

		if rec.Code != http.StatusAccepted {
			t.Fatalf("expected %d, got %d", http.StatusAccepted, rec.Code)
		}
		if body := rec.Body.String(); body != "proxied" {
			t.Fatalf("expected proxied response body, got %q", body)
		}
	})
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
		if rec.Code != http.StatusOK {
			t.Fatalf("expected %d, got %d", http.StatusOK, rec.Code)
		}
	})
}
