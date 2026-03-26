package main

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/aegion/aegion/core/workers"
	"github.com/aegion/aegion/internal/platform/logger"
)

// LifecycleConfig holds the lifecycle manager configuration.
type LifecycleConfig struct {
	Log           *logger.Logger
	Server        *Server
	HTTPServer    *http.Server
	WorkerManager *workers.Manager
}

// Lifecycle manages graceful startup and shutdown of server components.
type Lifecycle struct {
	log           *logger.Logger
	server        *Server
	httpServer    *http.Server
	workerManager *workers.Manager

	shutdownOnce sync.Once
	draining     bool
	mu           sync.RWMutex
}

// NewLifecycle creates a new lifecycle manager.
func NewLifecycle(cfg *LifecycleConfig) *Lifecycle {
	return &Lifecycle{
		log:           cfg.Log,
		server:        cfg.Server,
		httpServer:    cfg.HTTPServer,
		workerManager: cfg.WorkerManager,
	}
}

// Shutdown performs graceful shutdown of all components.
func (l *Lifecycle) Shutdown(ctx context.Context) error {
	var shutdownErr error

	l.shutdownOnce.Do(func() {
		l.log.Info().Msg("Starting graceful shutdown")

		// Mark as draining
		l.setDraining(true)

		// Create error channel for concurrent shutdown
		errCh := make(chan error, 4)
		var wg sync.WaitGroup

		// 1. Stop accepting new HTTP connections and drain existing
		wg.Add(1)
		go func() {
			defer wg.Done()
			l.log.Info().Msg("Draining HTTP connections")
			if err := l.drainHTTP(ctx); err != nil {
				l.log.Error().Err(err).Msg("Error draining HTTP")
				errCh <- err
			}
		}()

		// 2. Stop background workers
		wg.Add(1)
		go func() {
			defer wg.Done()
			if l.workerManager != nil {
				l.log.Info().Msg("Stopping background workers")
				l.workerManager.Stop()
				l.log.Info().Msg("Background workers stopped")
			}
		}()

		// Wait for HTTP drain and workers with timeout
		done := make(chan struct{})
		go func() {
			wg.Wait()
			close(done)
		}()

		select {
		case <-done:
			l.log.Info().Msg("HTTP and workers shutdown complete")
		case <-ctx.Done():
			l.log.Warn().Msg("Shutdown timeout reached for HTTP/workers")
		}

		// 3. Cleanup registry (deregister all modules)
		l.log.Info().Msg("Cleaning up service registry")
		if err := l.cleanupRegistry(ctx); err != nil {
			l.log.Error().Err(err).Msg("Error cleaning up registry")
		}

		// 4. Shutdown server components
		l.log.Info().Msg("Shutting down server components")
		if err := l.server.Shutdown(ctx); err != nil {
			l.log.Error().Err(err).Msg("Error shutting down server")
			errCh <- err
		}

		close(errCh)

		// Collect any errors
		for err := range errCh {
			if shutdownErr == nil {
				shutdownErr = err
			}
		}
	})

	return shutdownErr
}

// drainHTTP gracefully drains HTTP connections.
func (l *Lifecycle) drainHTTP(ctx context.Context) error {
	// Give in-flight requests time to complete
	drainTimeout := 5 * time.Second
	drainCtx, cancel := context.WithTimeout(ctx, drainTimeout)
	defer cancel()

	// Shutdown HTTP server (stops accepting new connections)
	if err := l.httpServer.Shutdown(drainCtx); err != nil {
		if err == context.DeadlineExceeded {
			l.log.Warn().Msg("HTTP drain timeout, forcing close")
			return l.httpServer.Close()
		}
		return err
	}

	return nil
}

// cleanupRegistry deregisters all modules and stops health checks.
func (l *Lifecycle) cleanupRegistry(ctx context.Context) error {
	if l.server.registry == nil {
		return nil
	}

	// Get all registered modules
	modules := l.server.registry.ListModules(nil)

	l.log.Info().
		Int("count", len(modules)).
		Msg("Deregistering modules")

	// Deregister each module
	for _, module := range modules {
		if _, err := l.server.registry.Deregister(module.ID); err != nil {
			l.log.Warn().
				Str("module_id", module.ID).
				Err(err).
				Msg("Failed to deregister module")
		}
	}

	return nil
}

// setDraining sets the draining state.
func (l *Lifecycle) setDraining(draining bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.draining = draining
}

// IsDraining returns whether the server is draining connections.
func (l *Lifecycle) IsDraining() bool {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.draining
}

// DrainMiddleware returns middleware that rejects new requests during drain.
func (l *Lifecycle) DrainMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if l.IsDraining() {
			// Allow health checks even during drain
			if r.URL.Path == "/health" || r.URL.Path == "/health/live" {
				next.ServeHTTP(w, r)
				return
			}

			w.Header().Set("Connection", "close")
			w.Header().Set("Retry-After", "30")
			http.Error(w, "Service is shutting down", http.StatusServiceUnavailable)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// ShutdownHook represents a function to be called during shutdown.
type ShutdownHook func(ctx context.Context) error

// ShutdownHooks manages ordered shutdown hooks.
type ShutdownHooks struct {
	hooks []namedHook
	mu    sync.Mutex
}

type namedHook struct {
	name string
	fn   ShutdownHook
}

// NewShutdownHooks creates a new shutdown hooks manager.
func NewShutdownHooks() *ShutdownHooks {
	return &ShutdownHooks{
		hooks: make([]namedHook, 0),
	}
}

// Register adds a shutdown hook.
func (h *ShutdownHooks) Register(name string, fn ShutdownHook) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.hooks = append(h.hooks, namedHook{name: name, fn: fn})
}

// Run executes all hooks in reverse order (LIFO).
func (h *ShutdownHooks) Run(ctx context.Context, log *logger.Logger) error {
	h.mu.Lock()
	hooks := make([]namedHook, len(h.hooks))
	copy(hooks, h.hooks)
	h.mu.Unlock()

	// Run in reverse order
	var lastErr error
	for i := len(hooks) - 1; i >= 0; i-- {
		hook := hooks[i]
		log.Info().Str("hook", hook.name).Msg("Running shutdown hook")
		if err := hook.fn(ctx); err != nil {
			log.Error().Err(err).Str("hook", hook.name).Msg("Shutdown hook failed")
			lastErr = err
		}
	}

	return lastErr
}
