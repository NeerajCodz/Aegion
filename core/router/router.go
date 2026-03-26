// Package router provides centralized HTTP routing and middleware for Aegion.
package router

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog"

	"github.com/aegion/aegion/core/registry"
)

// Router wraps chi.Router with Aegion-specific functionality.
type Router struct {
	mux    *chi.Mux
	config Config
	logger zerolog.Logger

	// Dependencies
	registry *registry.Registry
}

// Config holds router configuration.
type Config struct {
	// CORS settings
	CORS CORSConfig

	// Rate limiting
	RateLimit RateLimitConfig

	// Request timeout
	RequestTimeout time.Duration

	// Internal token for module communication
	InternalToken string

	// Session signing secret
	SessionSecret []byte

	// Module proxy timeout
	ModuleTimeout time.Duration

	// Trust proxy headers (X-Forwarded-For, etc.)
	TrustProxy bool

	// Development mode (relaxed security)
	DevMode bool
}

// CORSConfig holds CORS settings.
type CORSConfig struct {
	Enabled          bool
	AllowedOrigins   []string
	AllowedMethods   []string
	AllowedHeaders   []string
	ExposedHeaders   []string
	AllowCredentials bool
	MaxAge           int
}

// RateLimitConfig holds rate limiting settings.
type RateLimitConfig struct {
	Enabled           bool
	RequestsPerSecond float64
	Burst             int
}

// DefaultConfig returns sensible defaults.
func DefaultConfig() Config {
	return Config{
		CORS: CORSConfig{
			Enabled:          true,
			AllowedOrigins:   []string{"*"},
			AllowedMethods:   []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
			AllowedHeaders:   []string{"Authorization", "Content-Type", "X-Session-Token", "X-CSRF-Token"},
			ExposedHeaders:   []string{"X-Request-ID"},
			AllowCredentials: true,
			MaxAge:           300,
		},
		RateLimit: RateLimitConfig{
			Enabled:           true,
			RequestsPerSecond: 100,
			Burst:             200,
		},
		RequestTimeout: 60 * time.Second,
		ModuleTimeout:  30 * time.Second,
		TrustProxy:     false,
		DevMode:        false,
	}
}

// New creates a new Router with the default middleware stack.
func New(cfg Config, logger zerolog.Logger, reg *registry.Registry) *Router {
	r := &Router{
		mux:      chi.NewRouter(),
		config:   cfg,
		logger:   logger.With().Str("component", "router").Logger(),
		registry: reg,
	}

	r.setupMiddleware()
	r.setupHealthEndpoints()

	return r
}

// setupMiddleware configures the default middleware stack.
// Order matters: RequestID -> Logger -> Recoverer -> CORS -> RateLimit -> SecurityHeaders
func (r *Router) setupMiddleware() {
	// Request ID must be first to ensure all logs have it
	r.mux.Use(RequestID)

	// Logger middleware for structured logging
	r.mux.Use(Logger(r.logger))

	// Panic recovery
	r.mux.Use(Recoverer(r.logger))

	// CORS handling
	if r.config.CORS.Enabled {
		r.mux.Use(CORS(r.config.CORS))
	}

	// Rate limiting
	if r.config.RateLimit.Enabled {
		r.mux.Use(RateLimit(r.config.RateLimit))
	}

	// Security headers
	r.mux.Use(SecurityHeaders(r.config.DevMode))
}

// setupHealthEndpoints configures health check endpoints.
func (r *Router) setupHealthEndpoints() {
	r.mux.Get("/health", r.handleHealth)
	r.mux.Get("/ready", r.handleReady)
	r.mux.Get("/metrics", r.handleMetrics)
}

// ServeHTTP implements http.Handler.
func (r *Router) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	r.mux.ServeHTTP(w, req)
}

// Mount mounts a sub-router at the given pattern.
func (r *Router) Mount(pattern string, handler http.Handler) {
	r.mux.Mount(pattern, handler)
	r.logger.Debug().
		Str("pattern", pattern).
		Msg("mounted sub-router")
}

// Route creates a new route group with the given pattern.
func (r *Router) Route(pattern string, fn func(chi.Router)) {
	r.mux.Route(pattern, fn)
}

// Group creates a new inline-router with a fresh middleware stack.
func (r *Router) Group(fn func(chi.Router)) {
	r.mux.Group(fn)
}

// With adds inline middlewares for an endpoint handler.
func (r *Router) With(middlewares ...func(http.Handler) http.Handler) chi.Router {
	return r.mux.With(middlewares...)
}

// Get registers a GET handler.
func (r *Router) Get(pattern string, handlerFn http.HandlerFunc) {
	r.mux.Get(pattern, handlerFn)
}

// Post registers a POST handler.
func (r *Router) Post(pattern string, handlerFn http.HandlerFunc) {
	r.mux.Post(pattern, handlerFn)
}

// Put registers a PUT handler.
func (r *Router) Put(pattern string, handlerFn http.HandlerFunc) {
	r.mux.Put(pattern, handlerFn)
}

// Patch registers a PATCH handler.
func (r *Router) Patch(pattern string, handlerFn http.HandlerFunc) {
	r.mux.Patch(pattern, handlerFn)
}

// Delete registers a DELETE handler.
func (r *Router) Delete(pattern string, handlerFn http.HandlerFunc) {
	r.mux.Delete(pattern, handlerFn)
}

// Handle registers a handler for all methods.
func (r *Router) Handle(pattern string, handler http.Handler) {
	r.mux.Handle(pattern, handler)
}

// HandleFunc registers a handler function for all methods.
func (r *Router) HandleFunc(pattern string, handlerFn http.HandlerFunc) {
	r.mux.HandleFunc(pattern, handlerFn)
}

// NotFound sets the not found handler.
func (r *Router) NotFound(handlerFn http.HandlerFunc) {
	r.mux.NotFound(handlerFn)
}

// MethodNotAllowed sets the method not allowed handler.
func (r *Router) MethodNotAllowed(handlerFn http.HandlerFunc) {
	r.mux.MethodNotAllowed(handlerFn)
}

// ProxyToModule creates a handler that proxies requests to a module.
func (r *Router) ProxyToModule(moduleID string) http.Handler {
	proxy := NewModuleProxy(ModuleProxyConfig{
		Registry:      r.registry,
		ModuleID:      moduleID,
		InternalToken: r.config.InternalToken,
		SessionSecret: r.config.SessionSecret,
		Timeout:       r.config.ModuleTimeout,
		Logger:        r.logger,
	})
	return proxy
}

// MountModule mounts a module proxy at the given pattern.
func (r *Router) MountModule(pattern, moduleID string) {
	r.mux.Mount(pattern, r.ProxyToModule(moduleID))
	r.logger.Info().
		Str("pattern", pattern).
		Str("module", moduleID).
		Msg("mounted module proxy")
}

// Chi returns the underlying chi.Mux for advanced usage.
func (r *Router) Chi() *chi.Mux {
	return r.mux
}
