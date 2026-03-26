package main

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/aegion/aegion/core/authtoken"
	"github.com/aegion/aegion/core/flows"
	"github.com/aegion/aegion/core/registry"
	"github.com/aegion/aegion/core/workers"
	"github.com/aegion/aegion/internal/platform/config"
	"github.com/aegion/aegion/internal/platform/database"
	"github.com/aegion/aegion/internal/platform/logger"
)

// ServerConfig holds the server configuration.
type ServerConfig struct {
	Config         *config.Config
	DB             *database.DB
	Log            *logger.Logger
	WorkerManager  *workers.Manager
	AdminBootstrap bool
}

// Server represents the main Aegion server.
type Server struct {
	cfg           *config.Config
	db            *database.DB
	log           *logger.Logger
	router        chi.Router
	registry      *registry.Registry
	tokenGen      *authtoken.Generator
	flowService   *flows.Service
	workerManager *workers.Manager
}

// NewServer creates and initializes a new server instance.
func NewServer(ctx context.Context, cfg *ServerConfig) (*Server, error) {
	// Initialize auth token generator
	var internalSecret []byte
	if len(cfg.Config.Secrets.Internal) > 0 {
		internalSecret = []byte(cfg.Config.Secrets.Internal[0])
	} else {
		internalSecret = []byte("default-internal-secret-for-dev")
	}

	tokenGen, err := authtoken.NewGenerator(authtoken.GeneratorConfig{
		Secret: internalSecret,
		TTL:    5 * time.Minute,
	})
	if err != nil {
		return nil, err
	}

	// Initialize service registry
	reg := registry.New(registry.Config{
		HealthCheckInterval: cfg.Config.Server.InternalNet.HealthCheckInt.Duration(),
		HealthCheckTimeout:  cfg.Config.Server.InternalNet.HealthCheckTimeout.Duration(),
	})

	// Initialize flow store and service
	flowStore := flows.NewPostgresFlowStore(cfg.DB.Pool)
	flowService := flows.NewService(flowStore, flows.DefaultConfig())

	s := &Server{
		cfg:           cfg.Config,
		db:            cfg.DB,
		log:           cfg.Log,
		registry:      reg,
		tokenGen:      tokenGen,
		flowService:   flowService,
		workerManager: cfg.WorkerManager,
	}

	// Setup routes
	s.router = SetupRoutes(s)

	// Start registry
	reg.Start()
	s.log.Info().Msg("Service registry started")

	// Bootstrap admin if requested
	if cfg.AdminBootstrap {
		if err := s.bootstrapAdmin(ctx); err != nil {
			s.log.Warn().Err(err).Msg("Admin bootstrap failed")
		}
	}

	// Register workers if manager is available
	if s.workerManager != nil {
		s.registerWorkers()
	}

	return s, nil
}

// bootstrapAdmin creates the initial admin user if not exists.
func (s *Server) bootstrapAdmin(ctx context.Context) error {
	if s.cfg.Operator.Email == "" || s.cfg.Operator.Password == "" {
		s.log.Info().Msg("Admin bootstrap skipped: no operator credentials configured")
		return nil
	}

	s.log.Info().
		Str("email", s.cfg.Operator.Email).
		Msg("Admin bootstrap requested")

	// TODO: Implement actual admin user creation
	return nil
}

// registerWorkers registers background workers with the manager.
func (s *Server) registerWorkers() {
	// Register session cleanup worker
	s.workerManager.Register(workers.NewSessionCleanupWorker(workers.SessionCleanupConfig{
		DB:       s.db.Pool,
		Log:      s.log,
		Interval: 15 * time.Minute,
	}))

	// Register flow cleanup worker
	s.workerManager.Register(workers.NewFlowCleanupWorker(workers.FlowCleanupConfig{
		DB:       s.db.Pool,
		Log:      s.log,
		Interval: 10 * time.Minute,
	}))

	s.log.Info().Msg("Background workers registered")
}

// Handler returns the HTTP handler for the server.
func (s *Server) Handler() http.Handler {
	return s.router
}

// Shutdown gracefully shuts down the server.
func (s *Server) Shutdown(ctx context.Context) error {
	s.log.Info().Msg("Shutting down server components")

	// Stop registry
	if s.registry != nil {
		s.registry.Stop()
		s.log.Info().Msg("Service registry stopped")
	}

	return nil
}

// Registry returns the service registry.
func (s *Server) Registry() *registry.Registry {
	return s.registry
}

// FlowService returns the flow service.
func (s *Server) FlowService() *flows.Service {
	return s.flowService
}

// TokenGenerator returns the auth token generator.
func (s *Server) TokenGenerator() *authtoken.Generator {
	return s.tokenGen
}

// Middleware

func (s *Server) requestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)

		defer func() {
			s.log.Info().
				Str("method", r.Method).
				Str("path", r.URL.Path).
				Int("status", ww.Status()).
				Dur("duration", time.Since(start)).
				Str("request_id", middleware.GetReqID(r.Context())).
				Msg("HTTP request")
		}()

		next.ServeHTTP(ww, r)
	})
}

func (s *Server) corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")

		// Check if origin is allowed
		allowed := false
		for _, o := range s.cfg.Server.CORS.AllowedOrigins {
			if o == "*" || o == origin {
				allowed = true
				break
			}
		}

		if allowed {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Methods", joinStrings(s.cfg.Server.CORS.AllowedMethods))
			w.Header().Set("Access-Control-Allow-Headers", joinStrings(s.cfg.Server.CORS.AllowedHeaders))
			if s.cfg.Server.CORS.AllowCredentials {
				w.Header().Set("Access-Control-Allow-Credentials", "true")
			}
		}

		// Handle preflight
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func joinStrings(ss []string) string {
	result := ""
	for i, s := range ss {
		if i > 0 {
			result += ", "
		}
		result += s
	}
	return result
}

// Handlers

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"ok"}`))
}

func (s *Server) handleReady(w http.ResponseWriter, r *http.Request) {
	// Check database connection
	if err := s.db.Pool.Ping(r.Context()); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write([]byte(`{"status":"not ready","reason":"database unavailable"}`))
		return
	}

	// Check registry health
	registryOK := s.registry != nil && s.registry.ModuleCount() >= 0

	resp := map[string]interface{}{
		"status":        "ready",
		"database":      "ok",
		"registry":      registryOK,
		"module_count":  s.registry.ModuleCount(),
		"healthy_count": s.registry.HealthyCount(),
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(resp)
}

func (s *Server) handleLive(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"alive"}`))
}

func (s *Server) handleNotImplemented(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusNotImplemented)
	w.Write([]byte(`{"error":"not implemented"}`))
}
