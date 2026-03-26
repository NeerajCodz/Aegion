package main

import (
	"context"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"aegion/internal/platform/config"
	"aegion/internal/platform/database"
	"aegion/internal/platform/logger"
)

// Server represents the main Aegion server.
type Server struct {
	cfg    *config.Config
	db     *database.DB
	log    *logger.Logger
	router chi.Router
}

// NewServer creates and initializes a new server instance.
func NewServer(ctx context.Context, cfg *config.Config, db *database.DB, log *logger.Logger) (*Server, error) {
	s := &Server{
		cfg: cfg,
		db:  db,
		log: log,
	}

	s.setupRouter()

	// TODO: Initialize module orchestrator
	// TODO: Initialize session manager
	// TODO: Initialize event bus
	// TODO: Initialize courier
	// TODO: Start background workers

	return s, nil
}

// setupRouter configures the HTTP router and middleware.
func (s *Server) setupRouter() {
	r := chi.NewRouter()

	// Middleware stack
	r.Use(middleware.RequestID)
	r.Use(s.requestLogger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(s.cfg.Server.RequestTimeout.Duration()))

	// CORS
	if s.cfg.Server.CORS.Enabled {
		r.Use(s.corsMiddleware)
	}

	// Health check endpoints
	r.Get("/health", s.handleHealth)
	r.Get("/health/ready", s.handleReady)
	r.Get("/health/live", s.handleLive)

	// API routes
	r.Route("/api/v1", func(r chi.Router) {
		// Self-service flows
		r.Route("/self-service", func(r chi.Router) {
			// Login
			r.Get("/login/browser", s.handleNotImplemented)
			r.Get("/login/api", s.handleNotImplemented)
			r.Post("/login", s.handleNotImplemented)

			// Registration
			r.Get("/registration/browser", s.handleNotImplemented)
			r.Get("/registration/api", s.handleNotImplemented)
			r.Post("/registration", s.handleNotImplemented)

			// Recovery
			r.Get("/recovery/browser", s.handleNotImplemented)
			r.Get("/recovery/api", s.handleNotImplemented)
			r.Post("/recovery", s.handleNotImplemented)

			// Settings
			r.Get("/settings/browser", s.handleNotImplemented)
			r.Get("/settings/api", s.handleNotImplemented)
			r.Post("/settings", s.handleNotImplemented)
		})

		// Session endpoints
		r.Route("/sessions", func(r chi.Router) {
			r.Get("/whoami", s.handleNotImplemented)
			r.Delete("/", s.handleNotImplemented)
		})

		// JWKS endpoint
		r.Get("/.well-known/jwks.json", s.handleNotImplemented)
	})

	// Admin routes (if enabled)
	if s.cfg.Admin.Enabled {
		r.Route(s.cfg.Admin.Path, func(r chi.Router) {
			// Admin API
			r.Route("/api/v1", func(r chi.Router) {
				// Identity management
				r.Route("/identities", func(r chi.Router) {
					r.Get("/", s.handleNotImplemented)
					r.Post("/", s.handleNotImplemented)
					r.Get("/{id}", s.handleNotImplemented)
					r.Patch("/{id}", s.handleNotImplemented)
					r.Delete("/{id}", s.handleNotImplemented)
				})

				// Session management
				r.Route("/sessions", func(r chi.Router) {
					r.Get("/", s.handleNotImplemented)
					r.Delete("/{id}", s.handleNotImplemented)
				})

				// System config
				r.Route("/system", func(r chi.Router) {
					r.Get("/config", s.handleNotImplemented)
					r.Patch("/config", s.handleNotImplemented)
				})
			})

			// Serve admin SPA (catch-all)
			// TODO: Embed and serve React SPA
		})
	}

	s.router = r
}

// Handler returns the HTTP handler for the server.
func (s *Server) Handler() http.Handler {
	return s.router
}

// Shutdown gracefully shuts down the server.
func (s *Server) Shutdown(ctx context.Context) error {
	// TODO: Stop background workers
	// TODO: Stop module orchestrator
	// TODO: Flush event bus
	return nil
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

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"ready"}`))
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
