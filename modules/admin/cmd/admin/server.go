package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog/log"

	admin "github.com/aegion/aegion/modules/admin"
	"github.com/aegion/aegion/modules/admin/handler"
	"github.com/aegion/aegion/modules/admin/security"
	"github.com/aegion/aegion/modules/admin/service"
)

// DBPinger is an interface for DB health checking
type DBPinger interface {
	Ping(ctx context.Context) error
}

type Server struct {
	Config  *Config
	DB      *pgxpool.Pool
	dbPing  DBPinger // for testing
	Handler *handler.Handler

	adminPath string
	spaServer *SPAFileServer
}

type RegistrationRequest struct {
	ID        string            `json:"id"`
	Name      string            `json:"name"`
	Version   string            `json:"version"`
	Endpoints []Endpoint        `json:"endpoints"`
	HealthURL string            `json:"health_url"`
	Metadata  map[string]string `json:"metadata,omitempty"`
}

type Endpoint struct {
	Type string `json:"type"`
	URL  string `json:"url"`
	Path string `json:"path"`
}

type dashboardObservabilityEndpoint struct {
	Key   string
	Label string
	URL   string
}

type dashboardObservabilityProbe struct {
	Key            string `json:"key"`
	Label          string `json:"label"`
	URL            string `json:"url"`
	Status         string `json:"status"`
	StatusCode     int    `json:"status_code"`
	ResponseTimeMS int64  `json:"response_time_ms"`
	Message        string `json:"message"`
	CheckedAt      string `json:"checked_at"`
}

func (s *Server) setupRouter() chi.Router {
	s.ensureRoutingAssets()
	r := chi.NewRouter()

	// Global middleware
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(60 * time.Second))
	r.Use(middleware.Compress(5))
	r.Use(s.securityHeaders)
	r.Use(s.logRequest)

	// Health endpoint (no auth required)
	r.Get("/health", s.handleHealth)
	r.Get("/health/ready", s.handleReady)

	// Admin API routes
	r.Route("/api/admin", func(r chi.Router) {
		r.Get("/dashboard/config", s.handleDashboardConfig)
		r.Group(func(r chi.Router) {
			r.Use(security.RateLimitAdmin)
			r.Use(security.CSRFProtection)
			r.Use(security.SecurityAudit)
			r.With(s.Handler.RequireAdmin, handler.RequirePermission(s.Handler, service.PermAuditRead)).
				Get("/dashboard/observability", s.handleDashboardObservability)
			s.Handler.RegisterRoutes(r)
		})
	})

	// Serve embedded SPA
	r.Mount(s.adminPath, s.spaHandler())

	// Fallback route for SPA routing
	r.NotFound(s.spaFallback)

	return r
}

func (s *Server) securityHeaders(next http.Handler) http.Handler {
	if isDevMode() {
		return security.DevHeaders(next)
	}

	return security.Headers(next)
}

func isDevMode() bool {
	env := strings.ToLower(strings.TrimSpace(os.Getenv("AEGION_ENV")))
	if env == "" {
		env = strings.ToLower(strings.TrimSpace(os.Getenv("AEGION_ENVIRONMENT")))
	}

	return env == "dev" || env == "development" || env == "local"
}

func (s *Server) logRequest(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)

		defer func() {
			log.Info().
				Str("method", r.Method).
				Str("path", r.URL.Path).
				Int("status", ww.Status()).
				Int("bytes", ww.BytesWritten()).
				Dur("duration", time.Since(start)).
				Msg("request completed")
		}()

		next.ServeHTTP(ww, r)
	})
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	health := map[string]interface{}{
		"status":    "ok",
		"service":   "aegion-admin",
		"version":   "1.0.0",
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	}

	if err := json.NewEncoder(w).Encode(health); err != nil {
		log.Error().Err(err).Msg("Failed to encode health response")
	}
}

func (s *Server) handleReady(w http.ResponseWriter, r *http.Request) {
	// Check database connectivity
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	// Use dbPing if set (for testing), otherwise use DB
	var pinger DBPinger
	if s.dbPing != nil {
		pinger = s.dbPing
	} else if s.DB != nil {
		pinger = s.DB
	}

	if pinger == nil {
		log.Error().Msg("No database connection configured")
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"status": "not ready",
			"error":  "database not configured",
		})
		return
	}

	if err := pinger.Ping(ctx); err != nil {
		log.Error().Err(err).Msg("Database health check failed")
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"status": "not ready",
			"error":  "database unavailable",
		})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"status":    "ready",
		"service":   "aegion-admin",
		"database":  "connected",
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	})
}

func (s *Server) handleDashboardConfig(w http.ResponseWriter, r *http.Request) {
	s.ensureRoutingAssets()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"base_path": s.adminPath,
	})
}

func (s *Server) handleDashboardObservability(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if !s.Config.Observability.Enabled {
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode([]dashboardObservabilityProbe{})
		return
	}

	timeout := s.Config.Observability.ProbeTimeout
	if timeout <= 0 {
		timeout = 5 * time.Second
	}

	results := make([]dashboardObservabilityProbe, 0, 5)
	for _, endpoint := range s.dashboardObservabilityEndpoints() {
		results = append(results, s.probeDashboardObservability(r.Context(), endpoint, timeout))
	}

	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(results)
}

func (s *Server) dashboardObservabilityEndpoints() []dashboardObservabilityEndpoint {
	return []dashboardObservabilityEndpoint{
		{Key: "otel-collector", Label: "OTel Collector", URL: strings.TrimSpace(s.Config.Observability.Endpoints.OTelCollector)},
		{Key: "prometheus", Label: "Prometheus", URL: strings.TrimSpace(s.Config.Observability.Endpoints.Prometheus)},
		{Key: "grafana", Label: "Grafana", URL: strings.TrimSpace(s.Config.Observability.Endpoints.Grafana)},
		{Key: "tempo", Label: "Tempo", URL: strings.TrimSpace(s.Config.Observability.Endpoints.Tempo)},
		{Key: "loki", Label: "Loki", URL: strings.TrimSpace(s.Config.Observability.Endpoints.Loki)},
	}
}

func (s *Server) probeDashboardObservability(
	ctx context.Context,
	endpoint dashboardObservabilityEndpoint,
	timeout time.Duration,
) dashboardObservabilityProbe {
	result := dashboardObservabilityProbe{
		Key:       endpoint.Key,
		Label:     endpoint.Label,
		URL:       endpoint.URL,
		Status:    "offline",
		StatusCode: 0,
		Message:   "endpoint not configured",
		CheckedAt: time.Now().UTC().Format(time.RFC3339),
	}

	if endpoint.URL == "" {
		return result
	}

	started := time.Now()
	client := &http.Client{Timeout: timeout}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.URL, nil)
	if err != nil {
		result.Message = err.Error()
		return result
	}

	resp, err := client.Do(req)
	result.ResponseTimeMS = time.Since(started).Milliseconds()
	if err != nil {
		result.Message = err.Error()
		return result
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	result.StatusCode = resp.StatusCode
	result.Message = resp.Status
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		result.Status = "healthy"
		return result
	}

	result.Status = "degraded"
	return result
}

func normalizeAdminPath(adminPath string) string {
	trimmed := strings.TrimSpace(adminPath)
	if trimmed == "" {
		return "/aegion"
	}

	if !strings.HasPrefix(trimmed, "/") {
		trimmed = "/" + trimmed
	}

	if len(trimmed) > 1 {
		trimmed = strings.TrimRight(trimmed, "/")
	}

	if trimmed == "" {
		return "/aegion"
	}

	return trimmed
}

func (s *Server) spaHandler() http.Handler {
	s.ensureRoutingAssets()
	// Mount the embedded SPA files
	return http.StripPrefix(s.adminPath, s.spaServer)
}

func (s *Server) spaFallback(w http.ResponseWriter, r *http.Request) {
	s.ensureRoutingAssets()
	// For SPA routes that don't match files, serve index.html
	// This allows client-side routing to work
	path := strings.TrimPrefix(r.URL.Path, s.adminPath)

	// Only serve SPA fallback for admin paths
	if strings.HasPrefix(r.URL.Path, s.adminPath) {
		// Check if this is an API call that shouldn't get the SPA
		if strings.HasPrefix(path, "/api/") {
			http.NotFound(w, r)
			return
		}

		// Serve index.html for SPA routing
		s.spaServer.ServeHTTP(w, &http.Request{
			Method: "GET",
			URL:    &url.URL{Path: "/index.html"},
			Header: r.Header,
		})
		return
	}

	// Regular 404 for non-admin paths
	http.NotFound(w, r)
}

func (s *Server) registerWithCore(ctx context.Context) error {
	s.ensureRoutingAssets()
	if s.Config.Core.ServiceURL == "" {
		log.Warn().Msg("Core service URL not configured, skipping registration")
		return nil
	}

	serverAddr := fmt.Sprintf("%s:%d", s.Config.Server.Address, s.Config.Server.Port)
	if s.Config.Server.Address == "0.0.0.0" {
		// Use hostname instead of 0.0.0.0 for registration
		serverAddr = fmt.Sprintf("localhost:%d", s.Config.Server.Port)
	}

	// Registration payload
	registration := RegistrationRequest{
		ID:      "admin",
		Name:    "Admin Module",
		Version: "1.0.0",
		Endpoints: []Endpoint{
			{
				Type: "http",
				URL:  fmt.Sprintf("http://%s", serverAddr),
				Path: "/api/admin",
			},
		},
		HealthURL: fmt.Sprintf("http://%s/health", serverAddr),
		Metadata: map[string]string{
			"spa_path":    s.adminPath,
			"description": "Aegion Administration Interface",
		},
	}

	body, err := json.Marshal(registration)
	if err != nil {
		return fmt.Errorf("failed to marshal registration: %w", err)
	}

	// Register with core service
	registrationURL := fmt.Sprintf("%s/internal/registry/modules", s.Config.Core.ServiceURL)
	req, err := http.NewRequestWithContext(ctx, "POST", registrationURL, bytes.NewBuffer(body))
	if err != nil {
		return fmt.Errorf("failed to create registration request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	if s.Config.Core.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+s.Config.Core.APIKey)
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to register with core: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return fmt.Errorf("registration failed with status %d", resp.StatusCode)
	}

	log.Info().
		Str("core_url", s.Config.Core.ServiceURL).
		Str("module_id", registration.ID).
		Msg("Successfully registered with core service")

	return nil
}

// SPAFileServer handles serving static files with fallback to index.html
type SPAFileServer struct {
	fileServer http.Handler
}

func NewSPAFileServer() *SPAFileServer {
	return &SPAFileServer{
		fileServer: http.FileServer(http.FS(admin.GetSPAFiles())),
	}
}

func (spa *SPAFileServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/")
	if path == "" {
		path = "index.html"
	}

	// Check if the file exists
	if _, err := admin.GetSPAFiles().Open(path); err != nil {
		// File doesn't exist, check if it's a potential route
		ext := filepath.Ext(path)
		if ext == "" || ext == ".html" {
			// Likely a client-side route, serve index.html
			r.URL.Path = "/index.html"
		} else {
			// Static asset that doesn't exist, return 404
			http.NotFound(w, r)
			return
		}
	}

	// Set appropriate cache headers
	ext := filepath.Ext(r.URL.Path)
	switch ext {
	case ".js", ".css":
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable") // 1 year
	case ".html":
		w.Header().Set("Cache-Control", "no-cache, must-revalidate")
	default:
		w.Header().Set("Cache-Control", "public, max-age=3600") // 1 hour
	}

	spa.fileServer.ServeHTTP(w, r)
}

func (s *Server) ensureRoutingAssets() {
	if s.adminPath == "" {
		s.adminPath = normalizeAdminPath(s.Config.Admin.Path)
		s.Config.Admin.Path = s.adminPath
	}
	if s.spaServer == nil {
		s.spaServer = NewSPAFileServer()
	}
}
