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
)

type Server struct {
	Config  *Config
	DB      *pgxpool.Pool
	Handler *handler.Handler
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

func (s *Server) setupRouter() chi.Router {
	r := chi.NewRouter()

	// Global middleware
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(60 * time.Second))
	r.Use(s.securityHeaders)
	r.Use(s.logRequest)

	// Health endpoint (no auth required)
	r.Get("/health", s.handleHealth)
	r.Get("/health/ready", s.handleReady)

	// Admin API routes
	r.Route("/api/admin", func(r chi.Router) {
		r.Use(security.RateLimitAdmin)
		r.Use(security.CSRFProtection)
		r.Use(security.SecurityAudit)
		s.Handler.RegisterRoutes(r)
	})

	// Serve embedded SPA
	r.Mount(s.Config.Admin.Path, s.spaHandler())

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

	json.NewEncoder(w).Encode(health)
}

func (s *Server) handleReady(w http.ResponseWriter, r *http.Request) {
	// Check database connectivity
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	if err := s.DB.Ping(ctx); err != nil {
		log.Error().Err(err).Msg("Database health check failed")
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(map[string]string{
			"status": "not ready",
			"error":  "database unavailable",
		})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":    "ready",
		"service":   "aegion-admin",
		"database":  "connected",
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	})
}

func (s *Server) spaHandler() http.Handler {
	// Mount the embedded SPA files
	return http.StripPrefix(s.Config.Admin.Path, NewSPAFileServer())
}

func (s *Server) spaFallback(w http.ResponseWriter, r *http.Request) {
	// For SPA routes that don't match files, serve index.html
	// This allows client-side routing to work
	path := strings.TrimPrefix(r.URL.Path, s.Config.Admin.Path)

	// Only serve SPA fallback for admin paths
	if strings.HasPrefix(r.URL.Path, s.Config.Admin.Path) {
		// Check if this is an API call that shouldn't get the SPA
		if strings.HasPrefix(path, "/api/") {
			http.NotFound(w, r)
			return
		}

		// Serve index.html for SPA routing
		indexHandler := NewSPAFileServer()
		indexHandler.ServeHTTP(w, &http.Request{
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
			"spa_path":    s.Config.Admin.Path,
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
	defer resp.Body.Close()

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
		w.Header().Set("Cache-Control", "public, max-age=31536000") // 1 year
	case ".html":
		w.Header().Set("Cache-Control", "no-cache, must-revalidate")
	default:
		w.Header().Set("Cache-Control", "public, max-age=3600") // 1 hour
	}

	spa.fileServer.ServeHTTP(w, r)
}
