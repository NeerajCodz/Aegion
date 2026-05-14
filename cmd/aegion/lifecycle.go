package main

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/aegion/aegion/core/workers"
	"github.com/aegion/aegion/internal/xlog"
)

// LifecycleConfig holds the lifecycle manager configuration.
type LifecycleConfig struct {
	Log           *xlog.Logger
	Server        *Server
	HTTPServer    *http.Server
	WorkerManager *workers.Manager
	Observability telemetryProvider
}

// Lifecycle manages graceful startup and shutdown of server components.
type Lifecycle struct {
	log           *xlog.Logger
	server        *Server
	httpServer    *http.Server
	workerManager *workers.Manager
	observability telemetryProvider

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
		observability: cfg.Observability,
	}
}

// Shutdown performs graceful shutdown of all components.
func (l *Lifecycle) Shutdown(ctx context.Context) error {
	var shutdownErr error
	var shutdownErrMu sync.Mutex
	recordShutdownErr := func(err error) {
		if err == nil {
			return
		}
		shutdownErrMu.Lock()
		if shutdownErr == nil {
			shutdownErr = err
		}
		shutdownErrMu.Unlock()
	}

	l.shutdownOnce.Do(func() {
		l.log.Info("Starting graceful shutdown")

		// Mark as draining
		l.setDraining(true)

		var wg sync.WaitGroup

		// 1. Stop accepting new HTTP connections and drain existing
		wg.Add(1)
		go func() {
			defer wg.Done()
			l.log.Info("Draining HTTP connections")
			if err := l.drainHTTP(ctx); err != nil {
				l.log.Error("Error draining HTTP", "error", err)
				recordShutdownErr(err)
			}
		}()

		// 2. Stop background workers
		wg.Add(1)
		go func() {
			defer wg.Done()
			if l.workerManager != nil {
				l.log.Info("Stopping background workers")
				l.workerManager.Stop()
				l.log.Info("Background workers stopped")
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
			l.log.Info("HTTP and workers shutdown complete")
		case <-ctx.Done():
			l.log.Warn("Shutdown timeout reached for HTTP/workers")
		}

		// 3. Cleanup registry (deregister all modules)
		l.log.Info("Cleaning up service registry")
		if err := l.cleanupRegistry(ctx); err != nil {
			l.log.Error("Error cleaning up registry", "error", err)
		}

		// 4. Shutdown server components
		l.log.Info("Shutting down server components")
		if err := l.server.Shutdown(ctx); err != nil {
			l.log.Error("Error shutting down server", "error", err)
			recordShutdownErr(err)
		}

		// 5. Shutdown observability provider
		if l.observability != nil {
			l.log.Info("Shutting down observability provider")
			if err := l.observability.Shutdown(ctx); err != nil {
				l.log.Error("Error shutting down observability provider", "error", err)
				recordShutdownErr(err)
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
			l.log.Warn("HTTP drain timeout, forcing close")
			return l.httpServer.Close()
		}
		l.log.Error("Error shutting down HTTP server", "error", err)
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

	l.log.Info("Deregistering modules", "count", len(modules))

	// Deregister each module
	for _, module := range modules {
		if _, err := l.server.registry.Deregister(module.ID); err != nil {
			l.log.Warn("Failed to deregister module",
				"module_id", module.ID,
				"error", err,
			)
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
func (h *ShutdownHooks) Run(ctx context.Context, log *xlog.Logger) error {
	h.mu.Lock()
	hooks := make([]namedHook, len(h.hooks))
	copy(hooks, h.hooks)
	h.mu.Unlock()

	// Run in reverse order
	var lastErr error
	for i := len(hooks) - 1; i >= 0; i-- {
		hook := hooks[i]
		log.Info("Running shutdown hook", "hook", hook.name)
		if err := hook.fn(ctx); err != nil {
			log.Error("Shutdown hook failed", "error", err, "hook", hook.name)
			lastErr = err
		}
	}

	return lastErr
}
