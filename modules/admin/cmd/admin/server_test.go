package main

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	admin "github.com/aegion/aegion/modules/admin"
)

func TestNormalizeAdminPath(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "empty defaults to aegion", input: "", want: "/aegion"},
		{name: "trim and add leading slash", input: "  admin  ", want: "/admin"},
		{name: "trim trailing slash", input: "/admin/", want: "/admin"},
		{name: "root stays root", input: "/", want: "/"},
		{name: "trim many trailing slashes", input: "/aegion///", want: "/aegion"},
		{name: "all slashes fallback to default", input: "////", want: "/aegion"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := normalizeAdminPath(tc.input)
			if got != tc.want {
				t.Fatalf("normalizeAdminPath(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestIsDevMode(t *testing.T) {
	prevEnv := os.Getenv("AEGION_ENV")
	prevEnvironment := os.Getenv("AEGION_ENVIRONMENT")
	defer func() {
		_ = os.Setenv("AEGION_ENV", prevEnv)
		_ = os.Setenv("AEGION_ENVIRONMENT", prevEnvironment)
	}()

	_ = os.Unsetenv("AEGION_ENV")
	_ = os.Unsetenv("AEGION_ENVIRONMENT")
	if isDevMode() {
		t.Fatalf("expected false when env variables are not set")
	}

	_ = os.Setenv("AEGION_ENV", "development")
	if !isDevMode() {
		t.Fatalf("expected true for AEGION_ENV=development")
	}

	_ = os.Setenv("AEGION_ENV", "")
	_ = os.Setenv("AEGION_ENVIRONMENT", "local")
	if !isDevMode() {
		t.Fatalf("expected true for AEGION_ENVIRONMENT=local")
	}
}

func TestHandleDashboardConfigUsesNormalizedPath(t *testing.T) {
	s := &Server{
		Config: &Config{},
	}
	s.Config.Admin.Path = " admin "

	req := httptest.NewRequest(http.MethodGet, "/api/admin/dashboard/config", nil)
	rec := httptest.NewRecorder()
	s.handleDashboardConfig(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rec.Code)
	}
	if contentType := rec.Header().Get("Content-Type"); !strings.Contains(contentType, "application/json") {
		t.Fatalf("expected JSON response, got %q", contentType)
	}

	var body map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode JSON response: %v", err)
	}
	if body["base_path"] != "/admin" {
		t.Fatalf("expected base_path=/admin, got %q", body["base_path"])
	}
}

func TestRegisterWithCore(t *testing.T) {
	tests := []struct {
		name          string
		serviceURL    string
		address       string
		apiKey        string
		statusCode    int
		wantErr       bool
		wantAuth      string
		wantEndpoint  string
		wantHealthURL string
	}{
		{
			name:       "skips when core URL missing",
			serviceURL: "",
			wantErr:    false,
		},
		{
			name:          "uses localhost for wildcard bind",
			address:       "0.0.0.0",
			apiKey:        "secret",
			statusCode:    http.StatusCreated,
			wantErr:       false,
			wantAuth:      "Bearer secret",
			wantEndpoint:  "http://localhost:8082",
			wantHealthURL: "http://localhost:8082/health",
		},
		{
			name:          "propagates registration failure",
			address:       "127.0.0.1",
			statusCode:    http.StatusBadGateway,
			wantErr:       true,
			wantAuth:      "",
			wantEndpoint:  "http://127.0.0.1:8082",
			wantHealthURL: "http://127.0.0.1:8082/health",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var (
				gotAuth string
				gotBody RegistrationRequest
			)

			serverURL := tc.serviceURL
			if tc.serviceURL == "" && tc.statusCode != 0 {
				core := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					gotAuth = r.Header.Get("Authorization")
					if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
						t.Fatalf("failed to decode request body: %v", err)
					}
					w.WriteHeader(tc.statusCode)
				}))
				defer core.Close()
				serverURL = core.URL
			} else if tc.statusCode != 0 {
				core := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					gotAuth = r.Header.Get("Authorization")
					if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
						t.Fatalf("failed to decode request body: %v", err)
					}
					w.WriteHeader(tc.statusCode)
				}))
				defer core.Close()
				serverURL = core.URL
			}

			s := &Server{
				Config: &Config{},
			}
			s.Config.Core.ServiceURL = serverURL
			s.Config.Core.APIKey = tc.apiKey
			s.Config.Server.Address = tc.address
			s.Config.Server.Port = 8082
			s.Config.Admin.Path = "/aegion"

			err := s.registerWithCore(context.Background())
			if tc.wantErr && err == nil {
				t.Fatalf("expected error, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("expected nil error, got %v", err)
			}

			if tc.statusCode != 0 {
				if gotAuth != tc.wantAuth {
					t.Fatalf("expected auth header %q, got %q", tc.wantAuth, gotAuth)
				}
				if gotBody.ID != "admin" {
					t.Fatalf("expected module id admin, got %q", gotBody.ID)
				}
				if len(gotBody.Endpoints) != 1 {
					t.Fatalf("expected 1 endpoint, got %d", len(gotBody.Endpoints))
				}
				if gotBody.Endpoints[0].URL != tc.wantEndpoint {
					t.Fatalf("expected endpoint URL %q, got %q", tc.wantEndpoint, gotBody.Endpoints[0].URL)
				}
				if gotBody.HealthURL != tc.wantHealthURL {
					t.Fatalf("expected health URL %q, got %q", tc.wantHealthURL, gotBody.HealthURL)
				}
				if gotBody.Metadata["spa_path"] != "/aegion" {
					t.Fatalf("expected spa_path metadata /aegion, got %q", gotBody.Metadata["spa_path"])
				}
			}
		})
	}
}

func TestSPAFileServerBehavior(t *testing.T) {
	spa := NewSPAFileServer()

	t.Run("javascript assets are immutable", func(t *testing.T) {
		assetPath := findEmbeddedAssetPath(t, ".js", ".css")
		if assetPath == "" {
			t.Skip("no embedded immutable assets found; skipping cache-header assertion")
		}
		req := httptest.NewRequest(http.MethodGet, "/"+assetPath, nil)
		rec := httptest.NewRecorder()
		spa.ServeHTTP(rec, req)

		if cache := rec.Header().Get("Cache-Control"); cache != "public, max-age=31536000, immutable" {
			t.Fatalf("expected immutable cache header, got %q", cache)
		}
		if rec.Code != http.StatusOK {
			t.Fatalf("expected status 200 for existing immutable asset, got %d", rec.Code)
		}
	})

	t.Run("missing static asset returns 404", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/assets/not-found.css", nil)
		rec := httptest.NewRecorder()
		spa.ServeHTTP(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected 404 for missing asset, got %d", rec.Code)
		}
	})

	t.Run("route fallback serves html no-cache", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/operators/123", nil)
		rec := httptest.NewRecorder()
		spa.ServeHTTP(rec, req)

		if rec.Code != http.StatusMovedPermanently {
			t.Fatalf("expected 301 for route fallback, got %d", rec.Code)
		}
		if cache := rec.Header().Get("Cache-Control"); cache != "no-cache, must-revalidate" {
			t.Fatalf("expected HTML cache header, got %q", cache)
		}
		if location := rec.Header().Get("Location"); location != "./" {
			t.Fatalf("expected redirect to ./, got %q", location)
		}
	})
}

func findEmbeddedAssetPath(t *testing.T, exts ...string) string {
	t.Helper()
	if len(exts) == 0 {
		t.Fatalf("at least one extension must be provided")
	}

	var found string
	err := fs.WalkDir(admin.GetSPAFiles(), ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		for _, ext := range exts {
			if strings.HasSuffix(path, ext) {
				found = path
				return fs.SkipAll
			}
		}
		return nil
	})
	if err != nil && err != fs.SkipAll {
		t.Fatalf("failed to walk embedded SPA files: %v", err)
	}

	return found
}

func TestSPAFallbackRouting(t *testing.T) {
	s := &Server{
		Config: &Config{},
	}
	s.Config.Admin.Path = "/aegion"

	t.Run("api path remains 404", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/aegion/api/unknown", nil)
		rec := httptest.NewRecorder()
		s.spaFallback(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected 404 for api fallback, got %d", rec.Code)
		}
	})

	t.Run("admin ui path serves index", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/aegion/operators", nil)
		rec := httptest.NewRecorder()
		s.spaFallback(rec, req)
		if rec.Code != http.StatusMovedPermanently {
			t.Fatalf("expected 301 for admin route fallback, got %d", rec.Code)
		}
		if location := rec.Header().Get("Location"); location != "./" {
			t.Fatalf("expected redirect to ./, got %q", location)
		}
	})

	t.Run("non-admin path remains 404", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/outside", nil)
		rec := httptest.NewRecorder()
		s.spaFallback(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected 404 for non-admin path, got %d", rec.Code)
		}
	})
}

func TestSetupRouter(t *testing.T) {
	s := &Server{
		Config: &Config{},
	}
	s.Config.Admin.Path = "/admin"
	s.Handler = nil // Can be nil for this test

	r := s.setupRouter()
	if r == nil {
		t.Fatal("setupRouter() returned nil")
	}

	// Test that health routes are registered (they don't need handler)
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("Health endpoint not working, got status %d", rec.Code)
	}
}

func TestSetupRouterInitializesRoutingAssets(t *testing.T) {
	s := &Server{
		Config: &Config{},
	}
	s.Config.Admin.Path = " admin "

	if r := s.setupRouter(); r == nil {
		t.Fatal("setupRouter() returned nil")
	}
	if s.Config.Admin.Path != "/admin" {
		t.Fatalf("expected normalized admin path /admin, got %q", s.Config.Admin.Path)
	}
	if s.adminPath != "/admin" {
		t.Fatalf("expected cached adminPath /admin, got %q", s.adminPath)
	}
	if s.spaServer == nil {
		t.Fatal("expected spaServer to be initialized")
	}
}

func TestSPAHandlerReusesCachedServer(t *testing.T) {
	s := &Server{
		Config: &Config{},
	}
	s.Config.Admin.Path = "/admin"

	first := s.spaHandler()
	if first == nil || s.spaServer == nil {
		t.Fatal("expected non-nil handler and cached spaServer")
	}
	cached := s.spaServer
	second := s.spaHandler()
	if second == nil {
		t.Fatal("expected non-nil second handler")
	}
	if s.spaServer != cached {
		t.Fatal("expected cached spaServer to be reused")
	}
}

func TestSecurityHeaders(t *testing.T) {
	s := &Server{Config: &Config{}}

	handler := s.securityHeaders(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	// Should apply security headers
	if rec.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rec.Code)
	}
}

func TestLogRequest(t *testing.T) {
	s := &Server{Config: &Config{}}

	handler := s.logRequest(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("test"))
	}))

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rec.Code)
	}
	if rec.Body.String() != "test" {
		t.Errorf("Expected body 'test', got %s", rec.Body.String())
	}
}

func TestHandleHealth(t *testing.T) {
	s := &Server{Config: &Config{}}

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	s.handleHealth(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rec.Code)
	}

	var health map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&health); err != nil {
		t.Fatalf("Failed to decode health response: %v", err)
	}

	if health["status"] != "ok" {
		t.Errorf("Expected status 'ok', got %v", health["status"])
	}
	if health["service"] != "aegion-admin" {
		t.Errorf("Expected service 'aegion-admin', got %v", health["service"])
	}
}

// fakeDBPinger implements DBPinger for testing
type fakeDBPinger struct {
	pingErr error
}

func (f *fakeDBPinger) Ping(ctx context.Context) error {
	return f.pingErr
}

func TestHandleReady(t *testing.T) {
	t.Run("healthy db", func(t *testing.T) {
		s := &Server{
			Config: &Config{},
			dbPing: &fakeDBPinger{pingErr: nil},
		}

		req := httptest.NewRequest(http.MethodGet, "/health/ready", nil)
		rec := httptest.NewRecorder()
		s.handleReady(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("expected status %d, got %d", http.StatusOK, rec.Code)
		}

		var resp map[string]interface{}
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed to parse response: %v", err)
		}
		if resp["status"] != "ready" {
			t.Errorf("expected status 'ready', got %v", resp["status"])
		}
	})

	t.Run("unhealthy db", func(t *testing.T) {
		s := &Server{
			Config: &Config{},
			dbPing: &fakeDBPinger{pingErr: errors.New("connection refused")},
		}

		req := httptest.NewRequest(http.MethodGet, "/health/ready", nil)
		rec := httptest.NewRecorder()
		s.handleReady(rec, req)

		if rec.Code != http.StatusServiceUnavailable {
			t.Errorf("expected status %d, got %d", http.StatusServiceUnavailable, rec.Code)
		}

		var resp map[string]interface{}
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed to parse response: %v", err)
		}
		if resp["status"] != "not ready" {
			t.Errorf("expected status 'not ready', got %v", resp["status"])
		}
	})

	t.Run("no db configured", func(t *testing.T) {
		s := &Server{
			Config: &Config{},
			DB:     nil,
			dbPing: nil,
		}

		req := httptest.NewRequest(http.MethodGet, "/health/ready", nil)
		rec := httptest.NewRecorder()
		s.handleReady(rec, req)

		if rec.Code != http.StatusServiceUnavailable {
			t.Errorf("expected status %d, got %d", http.StatusServiceUnavailable, rec.Code)
		}
	})
}

func TestSPAHandler(t *testing.T) {
	s := &Server{Config: &Config{}}
	handler := s.spaHandler()

	if handler == nil {
		t.Fatal("spaHandler() returned nil")
	}

	// Test that it serves something
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	// Should return some response (either redirect or content)
	if rec.Code == 0 {
		t.Error("spaHandler didn't serve any response")
	}
}

func TestSecurityHeaders_InDevMode(t *testing.T) {
	// Set dev mode
	if err := os.Setenv("AEGION_ENV", "dev"); err != nil {
		t.Fatalf("failed to set AEGION_ENV: %v", err)
	}
	defer func() {
		_ = os.Unsetenv("AEGION_ENV")
	}()

	s := &Server{Config: &Config{}}

	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := s.securityHeaders(nextHandler)
	if handler == nil {
		t.Fatal("securityHeaders returned nil in dev mode")
	}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, rec.Code)
	}
}

func TestHandleDashboardObservabilityDisabled(t *testing.T) {
	s := &Server{
		Config: &Config{},
	}

	req := httptest.NewRequest(http.MethodGet, "/api/admin/dashboard/observability", nil)
	rec := httptest.NewRecorder()
	s.handleDashboardObservability(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rec.Code)
	}

	var probes []dashboardObservabilityProbe
	if err := json.NewDecoder(rec.Body).Decode(&probes); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(probes) != 0 {
		t.Fatalf("expected 0 probes when disabled, got %d", len(probes))
	}
}

func TestHandleDashboardObservability(t *testing.T) {
	healthy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	defer healthy.Close()

	degraded := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"status":"degraded"}`))
	}))
	defer degraded.Close()

	s := &Server{
		Config: &Config{},
	}
	s.Config.Observability.Enabled = true
	s.Config.Observability.ProbeTimeout = 200 * time.Millisecond
	s.Config.Observability.Endpoints.OTelCollector = healthy.URL
	s.Config.Observability.Endpoints.Prometheus = degraded.URL
	s.Config.Observability.Endpoints.Grafana = "http://127.0.0.1:1"

	req := httptest.NewRequest(http.MethodGet, "/api/admin/dashboard/observability", nil)
	rec := httptest.NewRecorder()
	s.handleDashboardObservability(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rec.Code)
	}

	var probes []dashboardObservabilityProbe
	if err := json.NewDecoder(rec.Body).Decode(&probes); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(probes) != 5 {
		t.Fatalf("expected 5 probes, got %d", len(probes))
	}

	statusByKey := make(map[string]dashboardObservabilityProbe, len(probes))
	for _, probe := range probes {
		statusByKey[probe.Key] = probe
	}

	if got := statusByKey["otel-collector"]; got.Status != "healthy" || got.StatusCode != http.StatusOK {
		t.Fatalf("expected healthy otel collector probe, got status=%q code=%d", got.Status, got.StatusCode)
	}
	if got := statusByKey["prometheus"]; got.Status != "degraded" || got.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("expected degraded prometheus probe, got status=%q code=%d", got.Status, got.StatusCode)
	}
	if got := statusByKey["grafana"]; got.Status != "offline" || got.StatusCode != 0 {
		t.Fatalf("expected offline grafana probe, got status=%q code=%d", got.Status, got.StatusCode)
	}
	if got := statusByKey["tempo"]; got.Status != "offline" || got.Message != "endpoint not configured" {
		t.Fatalf("expected offline tempo probe due missing endpoint, got status=%q message=%q", got.Status, got.Message)
	}
}
