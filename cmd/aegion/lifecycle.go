package main

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/aegion/aegion/core/workers"
	aegionloza "github.com/aegion/aegion/internal/platform/loza"
	"github.com/aegion/aegion/internal/xlog"
	lozasdk "github.com/astraive/loza/sdks/go"
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

	mu sync.RWMutex
}

func emitLifecycleEvent(ctx context.Context, eventName, outcome string, err error, attrs ...lozasdk.Attr) {
	logger := lozasdk.Default()
	eventCtx := aegionloza.Start(ctx, logger, lozasdk.Params{
		Event: eventName,
		Kind:  "system",
	})
	if len(attrs) > 0 {
		_ = logger.Set(eventCtx, attrs...)
	}
	if err != nil {
		_ = logger.FinishError(eventCtx, err)
	} else {
		_ = logger.Finish(eventCtx, aegionloza.NormalizeOutcome(outcome))
	}
	_ = logger.Emit(eventCtx)
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
		l.setDraining(true)

		var wg sync.WaitGroup

		// Stop accepting new HTTP connections and drain existing requests.
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := l.drainHTTP(ctx)
			if err != nil {
				recordShutdownErr(err)
			}
			emitLifecycleEvent(ctx, "aegion.shutdown", outcomeForError(err), err,
				lozasdk.String("shutdown.phase", "http_drain"))
		}()

		// Stop background workers.
		wg.Add(1)
		go func() {
			defer wg.Done()
			if l.workerManager != nil {
				l.workerManager.Stop()
			}
			emitLifecycleEvent(ctx, "aegion.worker_shutdown", "success", nil,
				lozasdk.String("shutdown.phase", "workers"))
		}()

		done := make(chan struct{})
		go func() {
			wg.Wait()
			close(done)
		}()

		select {
		case <-done:
			emitLifecycleEvent(ctx, "aegion.shutdown", "success", nil,
				lozasdk.String("shutdown.phase", "http_workers_complete"))
		case <-ctx.Done():
			recordShutdownErr(ctx.Err())
			emitLifecycleEvent(ctx, "aegion.shutdown", "timeout", ctx.Err(),
				lozasdk.String("shutdown.phase", "http_workers_timeout"))
		}

		registryErr := l.cleanupRegistry(ctx)
		if registryErr != nil {
			recordShutdownErr(registryErr)
		}
		emitLifecycleEvent(ctx, "aegion.module_registry", outcomeForError(registryErr), registryErr,
			lozasdk.String("shutdown.phase", "registry_cleanup"))

		serverErr := l.server.Shutdown(ctx)
		if serverErr != nil {
			recordShutdownErr(serverErr)
		}
		emitLifecycleEvent(ctx, "aegion.shutdown", outcomeForError(serverErr), serverErr,
			lozasdk.String("shutdown.phase", "server"))

		if l.observability != nil {
			observabilityErr := l.observability.Shutdown(ctx)
			if observabilityErr != nil {
				recordShutdownErr(observabilityErr)
			}
			emitLifecycleEvent(ctx, "aegion.shutdown", outcomeForError(observabilityErr), observabilityErr,
				lozasdk.String("shutdown.phase", "observability"))
		}

		emitLifecycleEvent(ctx, "aegion.shutdown", outcomeForError(shutdownErr), shutdownErr)
	})

	return shutdownErr
}

func outcomeForError(err error) string {
	if err == nil {
		return "success"
	}
	if err == context.DeadlineExceeded {
		return "timeout"
	}
	if err == context.Canceled {
		return "cancelled"
	}
	return "error"
}

// drainHTTP gracefully drains HTTP connections.
func (l *Lifecycle) drainHTTP(ctx context.Context) error {
	// Give in-flight requests time to complete
	drainTimeout := 5 * time.Second
	drainCtx, cancel := context.WithTimeout(ctx, drainTimeout)
	defer cancel()

	// Shutdown HTTP server (stops accepting new connections).
	if err := l.httpServer.Shutdown(drainCtx); err != nil {
		if err == context.DeadlineExceeded {
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

	modules := l.server.registry.ListModules(nil)
	for _, module := range modules {
		_, err := l.server.registry.Deregister(module.ID)
		emitLifecycleEvent(ctx, "aegion.module_registry", outcomeForError(err), err,
			lozasdk.String("module.id", module.ID))
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
func (h *ShutdownHooks) Run(ctx context.Context, _ *xlog.Logger) error {
	h.mu.Lock()
	hooks := make([]namedHook, len(h.hooks))
	copy(hooks, h.hooks)
	h.mu.Unlock()

	var lastErr error
	for i := len(hooks) - 1; i >= 0; i-- {
		hook := hooks[i]
		err := hook.fn(ctx)
		emitLifecycleEvent(ctx, "aegion.shutdown", outcomeForError(err), err,
			lozasdk.String("shutdown.phase", "hook"),
			lozasdk.String("shutdown.hook", hook.name))
		if err != nil {
			lastErr = err
		}
	}
	return lastErr
}
