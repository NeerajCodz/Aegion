package main

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"

	"github.com/aegion/aegion/core/authtoken"
	"github.com/aegion/aegion/core/flows"
	"github.com/aegion/aegion/core/orchestrator"
	"github.com/aegion/aegion/core/registry"
	"github.com/aegion/aegion/core/session"
	"github.com/aegion/aegion/core/workers"
	"github.com/aegion/aegion/internal/platform/config"
	"github.com/aegion/aegion/internal/platform/database"
	"github.com/aegion/aegion/internal/platform/logger"
	policypb "github.com/aegion/aegion/internal/proto/policy/v1"
	policygrpc "github.com/aegion/aegion/modules/policy/grpc"
	policystore "github.com/aegion/aegion/modules/policy/store"
)

// ServerConfig holds the server configuration.
type ServerConfig struct {
	Config         *config.Config
	ConfigPath     string
	DB             *database.DB
	Log            *logger.Logger
	WorkerManager  *workers.Manager
	AdminBootstrap bool
}

// Server represents the main Aegion server.
type Server struct {
	cfg            *config.Config
	db             *database.DB
	log            *logger.Logger
	router         chi.Router
	registry       *registry.Registry
	orchestrator   moduleOrchestrator
	sessionManager sessionManager
	tokenGen       *authtoken.Generator
	flowService    *flows.Service
	policyChecker  policyChecker
	workerManager  *workers.Manager
}

type policyChecker interface {
	Check(ctx context.Context, req *policypb.CheckRequest) (*policypb.CheckResponse, error)
}

type moduleOrchestrator interface {
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
	RestartModule(ctx context.Context, moduleID string) error
}

type sessionManager interface {
	GetFromRequest(ctx context.Context, r *http.Request) (*session.Session, error)
	Revoke(ctx context.Context, sessionID uuid.UUID) error
	ClearCookie(w http.ResponseWriter)
}

var newModuleOrchestrator = func(cfg orchestrator.Config) (moduleOrchestrator, error) {
	return orchestrator.New(cfg)
}

var newSessionManager = func(cfg session.ManagerConfig) sessionManager {
	return session.NewManager(cfg)
}

var pingDatabase = func(ctx context.Context, db *database.DB) error {
	return db.Pool.Ping(ctx)
}

// NewServer creates and initializes a new server instance.
func NewServer(ctx context.Context, cfg *ServerConfig) (*Server, error) {
	// Initialize auth token generator
	var internalSecret string
	if len(cfg.Config.Secrets.Internal) > 0 {
		internalSecret = cfg.Config.Secrets.Internal[0]
	} else if len(cfg.Config.Secrets.Cookie) > 0 {
		internalSecret = cfg.Config.Secrets.Cookie[0]
	} else if len(cfg.Config.Secrets.Cipher) > 0 {
		internalSecret = cfg.Config.Secrets.Cipher[0]
	} else {
		internalSecret = "dev-internal-secret-change-me-32chars"
	}

	tokenGen, err := authtoken.NewGenerator(authtoken.GeneratorConfig{
		Secret: []byte(internalSecret),
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

	sessionSecret := []byte(internalSecret)
	if len(cfg.Config.Secrets.Cookie) > 0 {
		sessionSecret = []byte(cfg.Config.Secrets.Cookie[0])
	}
	sessionMgr := newSessionManager(session.ManagerConfig{
		DB:           cfg.DB.Pool,
		CookieSecret: sessionSecret,
		CookieConfig: session.CookieConfig{
			Name:     cfg.Config.Sessions.Cookie.Name,
			Path:     cfg.Config.Sessions.Cookie.Path,
			SameSite: parseSameSite(cfg.Config.Sessions.Cookie.SameSite),
			Secure:   cfg.Config.Sessions.Cookie.Secure,
			HTTPOnly: cfg.Config.Sessions.Cookie.HTTPOnly,
		},
		Lifespan:    cfg.Config.Sessions.Lifespan.Duration(),
		IdleTimeout: cfg.Config.Sessions.IdleTimeout.Duration(),
	})

	var checker policyChecker
	if cfg.Config.Policy.Enabled {
		checker = policygrpc.NewServer(policystore.New(cfg.DB.Pool))
	}

	s := &Server{
		cfg:            cfg.Config,
		db:             cfg.DB,
		log:            cfg.Log,
		registry:       reg,
		tokenGen:       tokenGen,
		sessionManager: sessionMgr,
		flowService:    flowService,
		policyChecker:  checker,
		workerManager:  cfg.WorkerManager,
	}

	// Setup routes
	s.router = SetupRoutes(s)

	// Start registry
	reg.Start()
	s.log.Info().Msg("Service registry started")

	if cfg.ConfigPath != "" {
		moduleOrchestrator, err := newModuleOrchestrator(orchestrator.Config{
			ConfigPath:  cfg.ConfigPath,
			Registry:    reg,
			TokenSecret: []byte(internalSecret),
		})
		if err != nil {
			reg.Stop()
			return nil, err
		}
		if err := moduleOrchestrator.Start(ctx); err != nil {
			reg.Stop()
			return nil, err
		}
		s.orchestrator = moduleOrchestrator
		s.log.Info().Str("config_path", cfg.ConfigPath).Msg("Module orchestrator started")
	}

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

	var shutdownErr error

	if s.orchestrator != nil {
		if err := s.orchestrator.Stop(ctx); err != nil {
			s.log.Error().Err(err).Msg("Failed to stop module orchestrator")
			shutdownErr = err
		} else {
			s.log.Info().Msg("Module orchestrator stopped")
		}
	}

	// Stop registry
	if s.registry != nil {
		s.registry.Stop()
		s.log.Info().Msg("Service registry stopped")
	}

	return shutdownErr
}

// Registry returns the service registry.
func (s *Server) Registry() *registry.Registry {
	return s.registry
}

// Orchestrator returns the module orchestrator.
func (s *Server) Orchestrator() moduleOrchestrator {
	return s.orchestrator
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

func parseSameSite(value string) http.SameSite {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "strict":
		return http.SameSiteStrictMode
	case "none":
		return http.SameSiteNoneMode
	default:
		return http.SameSiteLaxMode
	}
}

// Handlers

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"ok"}`))
}

func (s *Server) handleReady(w http.ResponseWriter, r *http.Request) {
	// Check database connection
	if err := pingDatabase(r.Context(), s.db); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"status":"not ready","reason":"database unavailable"}`))
		return
	}

	// Check registry health
	registryOK := s.registry != nil && s.registry.ModuleCount() >= 0

	resp := map[string]interface{}{
		"status":        "ready",
		"database":      "ok",
		"registry":      registryOK,
		"orchestrator":  s.orchestrator != nil,
		"module_count":  s.registry.ModuleCount(),
		"healthy_count": s.registry.HealthyCount(),
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(resp)
}

func (s *Server) handleLive(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"alive"}`))
}

func (s *Server) handleNotImplemented(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusNotImplemented)
	_, _ = w.Write([]byte(`{"error":"not implemented"}`))
}
