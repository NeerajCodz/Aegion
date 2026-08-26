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

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	admin "github.com/aegion/aegion/modules/admin"
	adminhandler "github.com/aegion/aegion/modules/admin/handler"
	"github.com/aegion/aegion/modules/admin/scim"
	adminservice "github.com/aegion/aegion/modules/admin/service"
	adminstore "github.com/aegion/aegion/modules/admin/store"
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
func TestSetupPublicRouterOnlyMountsCoreOwnedPrefix(t *testing.T) {
	cfg := &Config{}
	cfg.Admin.Path = adminPublicRoutePrefix
	server := &Server{
		Config:  cfg,
		Handler: adminhandler.New(nil),
	}

	router := server.setupPublicRouter()
	if !router.Match(chi.NewRouteContext(), http.MethodPost, "/aegion/api/admin/auth/login") {
		t.Fatal("expected public admin login route to be mounted beneath /aegion")
	}
	if router.Match(chi.NewRouteContext(), http.MethodPost, "/api/admin/auth/login") {
		t.Fatal("legacy unprefixed admin API route must not be mounted by the public runtime")
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

type errWriteResponseWriter struct {
	header http.Header
	status int
}

func (w *errWriteResponseWriter) Header() http.Header {
	if w.header == nil {
		w.header = make(http.Header)
	}
	return w.header
}

func (w *errWriteResponseWriter) WriteHeader(statusCode int) { w.status = statusCode }

func (w *errWriteResponseWriter) Write([]byte) (int, error) { return 0, errors.New("write failed") }

type authOnlyAdminStore struct {
	adminservice.Store
	apiKey   *adminstore.APIKey
	operator *adminstore.Operator
}

func (s *authOnlyAdminStore) GetAPIKeyByPrefix(context.Context, string) (*adminstore.APIKey, error) {
	if s.apiKey == nil {
		return nil, errors.New("api key missing")
	}
	return s.apiKey, nil
}

func (s *authOnlyAdminStore) GetOperator(context.Context, uuid.UUID) (*adminstore.Operator, error) {
	if s.operator == nil {
		return nil, errors.New("operator missing")
	}
	return s.operator, nil
}

func (s *authOnlyAdminStore) UpdateAPIKeyLastUsed(context.Context, uuid.UUID) error { return nil }

type authOnlyHandlerService struct {
	adminhandler.Service
	store adminservice.Store
}

func (s *authOnlyHandlerService) Store() adminservice.Store { return s.store }

func (s *authOnlyHandlerService) EvaluateCapability(context.Context, uuid.UUID, string) error {
	return nil
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

	var resp dashboardObservabilityResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.Enabled {
		t.Fatalf("expected observability to be disabled")
	}
	if len(resp.Stack) != 0 {
		t.Fatalf("expected 0 probes when disabled, got %d", len(resp.Stack))
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
	s.Config.Observability.Endpoints.LozaCollector = healthy.URL
	s.Config.Observability.Endpoints.Prometheus = degraded.URL
	s.Config.Observability.Endpoints.Grafana = "http://127.0.0.1:1"

	req := httptest.NewRequest(http.MethodGet, "/api/admin/dashboard/observability", nil)
	rec := httptest.NewRecorder()
	s.handleDashboardObservability(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rec.Code)
	}

	var resp dashboardObservabilityResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if !resp.Enabled {
		t.Fatalf("expected observability to be enabled")
	}
	if len(resp.Stack) != 4 {
		t.Fatalf("expected 4 probes, got %d", len(resp.Stack))
	}

	statusByKey := make(map[string]dashboardObservabilityProbe, len(resp.Stack))
	for _, probe := range resp.Stack {
		statusByKey[probe.Key] = probe
	}

	if got := statusByKey["loza-collector"]; got.Status != "healthy" || got.StatusCode != http.StatusOK {
		t.Fatalf("expected healthy Loza collector probe, got status=%q code=%d", got.Status, got.StatusCode)
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
	if resp.Telemetry.ServiceName == "" {
		t.Fatal("expected telemetry summary to be populated")
	}
	if !resp.Guardrails.AdminAuthRequired || !resp.Guardrails.ObservabilityRBAC {
		t.Fatal("expected observability guardrails to be enabled")
	}
}

func TestDashboardSCIMSummary(t *testing.T) {
	now := time.Now().UTC()
	expiringSoon := now.Add(48 * time.Hour)
	expired := now.Add(-48 * time.Hour)
	lastUsed := now.Add(-time.Hour)

	s := &Server{
		Config: &Config{},
		SCIMService: scim.NewService(&serverSCIMStore{
			listTokensFn: func(context.Context) ([]*scim.SCIMToken, error) {
				return []*scim.SCIMToken{
					{
						ID:          uuid.New(),
						Permissions: []string{"*"},
						Active:      true,
						ExpiresAt:   &expiringSoon,
						LastUsedAt:  &lastUsed,
					},
					{
						ID:          uuid.New(),
						Permissions: []string{"users:write"},
						Active:      true,
						ExpiresAt:   &expired,
					},
				}, nil
			},
			listMappingsFn: func(context.Context) ([]*scim.SCIMMapping, error) {
				return []*scim.SCIMMapping{{ID: uuid.New(), Name: "default"}}, nil
			},
		}, nil),
	}
	s.Config.Admin.SCIM.Enabled = true
	s.Config.Admin.SCIM.BasePath = "/scim/v2"
	s.Config.Admin.SCIM.TokenPrefix = "aegion_scim_"

	summary := s.dashboardSCIMSummary(context.Background())
	if summary == nil {
		t.Fatal("expected SCIM summary")
	}
	if summary.TokenCount != 2 || summary.ActiveTokenCount != 2 {
		t.Fatalf("unexpected token counts: %+v", summary)
	}
	if summary.WildcardTokenCount != 1 || summary.WriteTokenCount != 2 {
		t.Fatalf("unexpected permission counts: %+v", summary)
	}
	if summary.ExpiredTokenCount != 1 || summary.ExpiringTokenCount != 1 {
		t.Fatalf("unexpected expiry counts: %+v", summary)
	}
	if summary.LastTokenUsedAt == "" {
		t.Fatal("expected last token used timestamp")
	}
}

type serverSCIMStore struct {
	scim.Store
	listTokensFn    func(context.Context) ([]*scim.SCIMToken, error)
	createTokenFn   func(context.Context, *scim.SCIMToken) error
	deleteTokenFn   func(context.Context, uuid.UUID) error
	listMappingsFn  func(context.Context) ([]*scim.SCIMMapping, error)
	createMappingFn func(context.Context, *scim.SCIMMapping) error
	updateMappingFn func(context.Context, *scim.SCIMMapping) error
	deleteMappingFn func(context.Context, uuid.UUID) error
}

func (s *serverSCIMStore) ListSCIMTokens(ctx context.Context) ([]*scim.SCIMToken, error) {
	if s.listTokensFn != nil {
		return s.listTokensFn(ctx)
	}
	return nil, nil
}

func (s *serverSCIMStore) CreateSCIMToken(ctx context.Context, token *scim.SCIMToken) error {
	if s.createTokenFn != nil {
		return s.createTokenFn(ctx, token)
	}
	return nil
}

func (s *serverSCIMStore) DeleteSCIMToken(ctx context.Context, id uuid.UUID) error {
	if s.deleteTokenFn != nil {
		return s.deleteTokenFn(ctx, id)
	}
	return nil
}

func (s *serverSCIMStore) ListSCIMMappings(ctx context.Context) ([]*scim.SCIMMapping, error) {
	if s.listMappingsFn != nil {
		return s.listMappingsFn(ctx)
	}
	return nil, nil
}

func (s *serverSCIMStore) CreateSCIMMapping(ctx context.Context, mapping *scim.SCIMMapping) error {
	if s.createMappingFn != nil {
		return s.createMappingFn(ctx, mapping)
	}
	return nil
}

func (s *serverSCIMStore) UpdateSCIMMapping(ctx context.Context, mapping *scim.SCIMMapping) error {
	if s.updateMappingFn != nil {
		return s.updateMappingFn(ctx, mapping)
	}
	return nil
}

func (s *serverSCIMStore) DeleteSCIMMapping(ctx context.Context, id uuid.UUID) error {
	if s.deleteMappingFn != nil {
		return s.deleteMappingFn(ctx, id)
	}
	return nil
}

func withSCIMRouteParam(req *http.Request, key, value string) *http.Request {
	routeCtx := chi.NewRouteContext()
	routeCtx.URLParams.Add(key, value)
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, routeCtx))
}

func TestNormalizeMountedPath(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "empty defaults", in: "", want: "/scim/v2"},
		{name: "adds leading slash", in: "scim/v2", want: "/scim/v2"},
		{name: "trims trailing slash", in: "/scim/v2/", want: "/scim/v2"},
		{name: "root stays root", in: "/", want: "/"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := normalizeMountedPath(tc.in); got != tc.want {
				t.Fatalf("normalizeMountedPath(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestSCIMTokenHandlersBranches(t *testing.T) {
	now := time.Now().UTC()

	t.Run("list tokens handles service error and success", func(t *testing.T) {
		s := &Server{
			Config: &Config{},
			SCIMService: scim.NewService(&serverSCIMStore{
				listTokensFn: func(context.Context) ([]*scim.SCIMToken, error) {
					return nil, errors.New("list failed")
				},
			}, nil),
		}

		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/admin/scim/tokens", nil)
		s.handleListSCIMTokens(rec, req)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("expected %d, got %d", http.StatusInternalServerError, rec.Code)
		}

		s.SCIMService = scim.NewService(&serverSCIMStore{
			listTokensFn: func(context.Context) ([]*scim.SCIMToken, error) {
				return []*scim.SCIMToken{{
					ID:        uuid.New(),
					Name:      "token-1",
					CreatedAt: now,
					Active:    true,
				}}, nil
			},
		}, nil)

		rec = httptest.NewRecorder()
		s.handleListSCIMTokens(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected %d, got %d body=%s", http.StatusOK, rec.Code, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), `"token-1"`) {
			t.Fatalf("expected token payload, got %s", rec.Body.String())
		}
	})

	t.Run("create token requires operator context", func(t *testing.T) {
		s := &Server{Config: &Config{}}
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/admin/scim/tokens", strings.NewReader(`{"name":"token-a"}`))
		req.Header.Set("Content-Type", "application/json")
		s.handleCreateSCIMToken(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected %d, got %d", http.StatusUnauthorized, rec.Code)
		}
	})

	t.Run("create token authorized decode, validation, service error, and success", func(t *testing.T) {
		operator := &adminstore.Operator{ID: uuid.New(), Role: "super_admin"}
		fullToken := "aegion_abcdefghijklmnopqrstuv"
		apiKey := &adminstore.APIKey{
			ID:         uuid.New(),
			OperatorID: operator.ID,
			KeyHash:    adminstore.HashAPIKeyToken(fullToken),
		}
		authStore := &authOnlyAdminStore{apiKey: apiKey, operator: operator}
		authHandler := adminhandler.New(&authOnlyHandlerService{store: authStore})

		runCreate := func(scimStore *serverSCIMStore, body string) *httptest.ResponseRecorder {
			s := &Server{
				Config:      &Config{},
				SCIMService: scim.NewService(scimStore, nil),
				Handler:     authHandler,
			}
			secured := s.Handler.RequireAdmin(adminhandler.RequirePermission(s.Handler, adminservice.PermConfigUpdate)(http.HandlerFunc(s.handleCreateSCIMToken)))

			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/api/admin/scim/tokens", strings.NewReader(body))
			req.Header.Set("Authorization", "Bearer "+fullToken)
			req.Header.Set("Content-Type", "application/json")
			secured.ServeHTTP(rec, req)
			return rec
		}

		rec := runCreate(&serverSCIMStore{}, `{`)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected %d, got %d", http.StatusBadRequest, rec.Code)
		}

		rec = runCreate(&serverSCIMStore{}, `{"name":"  "}`)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected %d, got %d", http.StatusBadRequest, rec.Code)
		}

		rec = runCreate(&serverSCIMStore{
			createTokenFn: func(context.Context, *scim.SCIMToken) error { return errors.New("create failed") },
		}, `{"name":"token-a","permissions":["users:read"]}`)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("expected %d, got %d", http.StatusInternalServerError, rec.Code)
		}

		rec = runCreate(&serverSCIMStore{}, `{"name":"token-a","description":"demo","permissions":["users:read"]}`)
		if rec.Code != http.StatusCreated {
			t.Fatalf("expected %d, got %d body=%s", http.StatusCreated, rec.Code, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), "plain_token") {
			t.Fatalf("expected plain token in response, got %s", rec.Body.String())
		}
	})

	t.Run("delete token validates id and handles service result", func(t *testing.T) {
		s := &Server{
			Config: &Config{},
			SCIMService: scim.NewService(&serverSCIMStore{
				deleteTokenFn: func(context.Context, uuid.UUID) error {
					return errors.New("delete failed")
				},
			}, nil),
		}

		rec := httptest.NewRecorder()
		req := withSCIMRouteParam(httptest.NewRequest(http.MethodDelete, "/api/admin/scim/tokens/bad", nil), "id", "bad")
		s.handleDeleteSCIMToken(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected %d, got %d", http.StatusBadRequest, rec.Code)
		}

		tokenID := uuid.New()
		rec = httptest.NewRecorder()
		req = withSCIMRouteParam(httptest.NewRequest(http.MethodDelete, "/api/admin/scim/tokens/"+tokenID.String(), nil), "id", tokenID.String())
		s.handleDeleteSCIMToken(rec, req)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("expected %d, got %d", http.StatusInternalServerError, rec.Code)
		}

		s.SCIMService = scim.NewService(&serverSCIMStore{
			deleteTokenFn: func(context.Context, uuid.UUID) error { return nil },
		}, nil)
		rec = httptest.NewRecorder()
		s.handleDeleteSCIMToken(rec, req)
		if rec.Code != http.StatusNoContent {
			t.Fatalf("expected %d, got %d", http.StatusNoContent, rec.Code)
		}
	})
}

func TestHandleHealthAndReadyAdditionalBranches(t *testing.T) {
	t.Run("health encode write failure branch", func(t *testing.T) {
		s := &Server{Config: &Config{}}
		req := httptest.NewRequest(http.MethodGet, "/health", nil)
		w := &errWriteResponseWriter{}
		s.handleHealth(w, req)
		if w.status != http.StatusOK {
			t.Fatalf("expected %d status, got %d", http.StatusOK, w.status)
		}
	})

	t.Run("ready falls back to DB pool when dbPing nil", func(t *testing.T) {
		pool, err := pgxpool.New(context.Background(), "postgres://postgres:postgres@127.0.0.1:1/postgres?sslmode=disable&connect_timeout=1")
		if err != nil {
			t.Fatalf("create pool: %v", err)
		}
		defer pool.Close()

		s := &Server{Config: &Config{}, DB: pool}
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/health/ready", nil)
		s.handleReady(rec, req)
		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("expected %d, got %d", http.StatusServiceUnavailable, rec.Code)
		}
	})
}

func TestSCIMMappingHandlersBranches(t *testing.T) {
	t.Run("list mappings handles service error and success", func(t *testing.T) {
		s := &Server{
			Config: &Config{},
			SCIMService: scim.NewService(&serverSCIMStore{
				listMappingsFn: func(context.Context) ([]*scim.SCIMMapping, error) {
					return nil, errors.New("list mappings failed")
				},
			}, nil),
		}

		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/admin/scim/mappings", nil)
		s.handleListSCIMMappings(rec, req)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("expected %d, got %d", http.StatusInternalServerError, rec.Code)
		}

		s.SCIMService = scim.NewService(&serverSCIMStore{
			listMappingsFn: func(context.Context) ([]*scim.SCIMMapping, error) {
				return []*scim.SCIMMapping{{ID: uuid.New(), Name: "default"}}, nil
			},
		}, nil)
		rec = httptest.NewRecorder()
		s.handleListSCIMMappings(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected %d, got %d", http.StatusOK, rec.Code)
		}
		if !strings.Contains(rec.Body.String(), `"default"`) {
			t.Fatalf("expected mapping payload, got %s", rec.Body.String())
		}
	})

	t.Run("create mapping validation and service branches", func(t *testing.T) {
		s := &Server{
			Config:      &Config{},
			SCIMService: scim.NewService(&serverSCIMStore{}, nil),
		}

		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/admin/scim/mappings", strings.NewReader("{"))
		s.handleCreateSCIMMapping(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected %d, got %d", http.StatusBadRequest, rec.Code)
		}

		rec = httptest.NewRecorder()
		req = httptest.NewRequest(http.MethodPost, "/api/admin/scim/mappings", strings.NewReader(`{"name":"  "}`))
		req.Header.Set("Content-Type", "application/json")
		s.handleCreateSCIMMapping(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected %d, got %d", http.StatusBadRequest, rec.Code)
		}

		s.SCIMService = scim.NewService(&serverSCIMStore{
			createMappingFn: func(context.Context, *scim.SCIMMapping) error {
				return errors.New("store create failed")
			},
		}, nil)
		rec = httptest.NewRecorder()
		req = httptest.NewRequest(http.MethodPost, "/api/admin/scim/mappings", strings.NewReader(`{"name":"mapping-a"}`))
		req.Header.Set("Content-Type", "application/json")
		s.handleCreateSCIMMapping(rec, req)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("expected %d, got %d", http.StatusInternalServerError, rec.Code)
		}

		s.SCIMService = scim.NewService(&serverSCIMStore{
			createMappingFn: func(context.Context, *scim.SCIMMapping) error { return nil },
		}, nil)
		rec = httptest.NewRecorder()
		req = httptest.NewRequest(http.MethodPost, "/api/admin/scim/mappings", strings.NewReader(`{"name":"mapping-a"}`))
		req.Header.Set("Content-Type", "application/json")
		s.handleCreateSCIMMapping(rec, req)
		if rec.Code != http.StatusCreated {
			t.Fatalf("expected %d, got %d body=%s", http.StatusCreated, rec.Code, rec.Body.String())
		}
	})

	t.Run("update and delete mapping branches", func(t *testing.T) {
		s := &Server{
			Config: &Config{},
			SCIMService: scim.NewService(&serverSCIMStore{
				updateMappingFn: func(context.Context, *scim.SCIMMapping) error { return nil },
				deleteMappingFn: func(context.Context, uuid.UUID) error { return nil },
			}, nil),
		}

		rec := httptest.NewRecorder()
		req := withSCIMRouteParam(httptest.NewRequest(http.MethodPut, "/api/admin/scim/mappings/bad", nil), "id", "bad")
		s.handleUpdateSCIMMapping(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected %d, got %d", http.StatusBadRequest, rec.Code)
		}

		mappingID := uuid.New()
		req = withSCIMRouteParam(httptest.NewRequest(http.MethodPut, "/api/admin/scim/mappings/"+mappingID.String(), strings.NewReader("{")), "id", mappingID.String())
		rec = httptest.NewRecorder()
		s.handleUpdateSCIMMapping(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected %d, got %d", http.StatusBadRequest, rec.Code)
		}

		req = withSCIMRouteParam(httptest.NewRequest(http.MethodPut, "/api/admin/scim/mappings/"+mappingID.String(), strings.NewReader(`{"name":" "}`)), "id", mappingID.String())
		req.Header.Set("Content-Type", "application/json")
		rec = httptest.NewRecorder()
		s.handleUpdateSCIMMapping(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected %d, got %d", http.StatusBadRequest, rec.Code)
		}

		s.SCIMService = scim.NewService(&serverSCIMStore{
			updateMappingFn: func(context.Context, *scim.SCIMMapping) error { return errors.New("update failed") },
		}, nil)
		req = withSCIMRouteParam(httptest.NewRequest(http.MethodPut, "/api/admin/scim/mappings/"+mappingID.String(), strings.NewReader(`{"name":"mapping-updated"}`)), "id", mappingID.String())
		req.Header.Set("Content-Type", "application/json")
		rec = httptest.NewRecorder()
		s.handleUpdateSCIMMapping(rec, req)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("expected %d, got %d", http.StatusInternalServerError, rec.Code)
		}

		s.SCIMService = scim.NewService(&serverSCIMStore{
			updateMappingFn: func(context.Context, *scim.SCIMMapping) error { return nil },
		}, nil)
		rec = httptest.NewRecorder()
		req = withSCIMRouteParam(httptest.NewRequest(http.MethodPut, "/api/admin/scim/mappings/"+mappingID.String(), strings.NewReader(`{"name":"mapping-updated"}`)), "id", mappingID.String())
		req.Header.Set("Content-Type", "application/json")
		s.handleUpdateSCIMMapping(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected %d, got %d body=%s", http.StatusOK, rec.Code, rec.Body.String())
		}

		rec = httptest.NewRecorder()
		req = withSCIMRouteParam(httptest.NewRequest(http.MethodDelete, "/api/admin/scim/mappings/bad", nil), "id", "bad")
		s.handleDeleteSCIMMapping(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected %d, got %d", http.StatusBadRequest, rec.Code)
		}

		s.SCIMService = scim.NewService(&serverSCIMStore{
			deleteMappingFn: func(context.Context, uuid.UUID) error { return errors.New("delete failed") },
		}, nil)
		req = withSCIMRouteParam(httptest.NewRequest(http.MethodDelete, "/api/admin/scim/mappings/"+mappingID.String(), nil), "id", mappingID.String())
		rec = httptest.NewRecorder()
		s.handleDeleteSCIMMapping(rec, req)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("expected %d, got %d", http.StatusInternalServerError, rec.Code)
		}

		s.SCIMService = scim.NewService(&serverSCIMStore{
			deleteMappingFn: func(context.Context, uuid.UUID) error { return nil },
		}, nil)
		rec = httptest.NewRecorder()
		s.handleDeleteSCIMMapping(rec, req)
		if rec.Code != http.StatusNoContent {
			t.Fatalf("expected %d, got %d", http.StatusNoContent, rec.Code)
		}
	})
}
