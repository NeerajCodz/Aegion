// Package workers provides background job workers for Aegion.
package workers

import (
	"context"
	"sync"
	"time"

	"github.com/aegion/aegion/internal/platform/logger"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Worker defines the interface for background workers.
type Worker interface {
	// Name returns the worker name for logging.
	Name() string
	// Start begins the worker's execution loop.
	Start(ctx context.Context) error
	// Stop gracefully stops the worker.
	Stop()
}

// Manager coordinates multiple workers.
type Manager struct {
	db      *pgxpool.Pool
	log     *logger.Logger
	workers []Worker
	wg      sync.WaitGroup
	cancel  context.CancelFunc
	mu      sync.Mutex
}

// ManagerConfig configures the worker manager.
type ManagerConfig struct {
	DB  *pgxpool.Pool
	Log *logger.Logger
}

// NewManager creates a new worker manager.
func NewManager(cfg ManagerConfig) *Manager {
	log := cfg.Log
	if log == nil {
		log = logger.New(logger.Config{Level: "info", Format: "json"})
	}

	return &Manager{
		db:      cfg.DB,
		log:     log.WithComponent("worker_manager"),
		workers: make([]Worker, 0),
	}
}

// Register adds a worker to the manager.
func (m *Manager) Register(w Worker) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.workers = append(m.workers, w)
	m.log.Info().Str("worker", w.Name()).Msg("worker registered")
}

// Start starts all registered workers.
func (m *Manager) Start(ctx context.Context) {
	m.mu.Lock()
	defer m.mu.Unlock()

	ctx, m.cancel = context.WithCancel(ctx)

	for _, w := range m.workers {
		m.wg.Add(1)
		go func(worker Worker) {
			defer m.wg.Done()
			m.log.Info().Str("worker", worker.Name()).Msg("starting worker")
			if err := worker.Start(ctx); err != nil {
				m.log.Error().Err(err).Str("worker", worker.Name()).Msg("worker stopped with error")
			} else {
				m.log.Info().Str("worker", worker.Name()).Msg("worker stopped")
			}
		}(w)
	}

	m.log.Info().Int("count", len(m.workers)).Msg("all workers started")
}

// Stop gracefully stops all workers.
func (m *Manager) Stop() {
	m.log.Info().Msg("stopping all workers")

	// Cancel context to signal workers to stop
	if m.cancel != nil {
		m.cancel()
	}

	// Stop each worker
	m.mu.Lock()
	for _, w := range m.workers {
		w.Stop()
	}
	m.mu.Unlock()

	// Wait for all workers to finish
	done := make(chan struct{})
	go func() {
		m.wg.Wait()
		close(done)
	}()

	// Wait with timeout
	select {
	case <-done:
		m.log.Info().Msg("all workers stopped gracefully")
	case <-time.After(30 * time.Second):
		m.log.Warn().Msg("timeout waiting for workers to stop")
	}
}

// BaseWorker provides common functionality for workers.
type BaseWorker struct {
	name     string
	db       *pgxpool.Pool
	log      *logger.Logger
	interval time.Duration
	done     chan struct{}
	mu       sync.Mutex
	running  bool
}

// NewBaseWorker creates a new base worker.
func NewBaseWorker(name string, db *pgxpool.Pool, log *logger.Logger, interval time.Duration) *BaseWorker {
	if log == nil {
		log = logger.New(logger.Config{Level: "info", Format: "json"})
	}

	return &BaseWorker{
		name:     name,
		db:       db,
		log:      log.WithComponent(name),
		interval: interval,
		done:     make(chan struct{}),
	}
}

// Name returns the worker name.
func (w *BaseWorker) Name() string {
	return w.name
}

// DB returns the database pool.
func (w *BaseWorker) DB() *pgxpool.Pool {
	return w.db
}

// Log returns the logger.
func (w *BaseWorker) Log() *logger.Logger {
	return w.log
}

// Interval returns the execution interval.
func (w *BaseWorker) Interval() time.Duration {
	return w.interval
}

// Done returns the done channel.
func (w *BaseWorker) Done() <-chan struct{} {
	return w.done
}

// Stop signals the worker to stop.
func (w *BaseWorker) Stop() {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.running {
		close(w.done)
		w.running = false
	}
}

// SetRunning marks the worker as running.
func (w *BaseWorker) SetRunning(running bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.running = running
}

// IsRunning returns whether the worker is running.
func (w *BaseWorker) IsRunning() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.running
}

// RunLoop executes a function periodically until context is cancelled.
func (w *BaseWorker) RunLoop(ctx context.Context, fn func(ctx context.Context) error) error {
	w.SetRunning(true)
	defer w.SetRunning(false)

	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	// Run immediately on start
	if err := w.safeRun(ctx, fn); err != nil {
		w.log.Error().Err(err).Msg("initial run failed")
	}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-w.done:
			return nil
		case <-ticker.C:
			if err := w.safeRun(ctx, fn); err != nil {
				w.log.Error().Err(err).Msg("periodic run failed")
			}
		}
	}
}

// safeRun executes a function and recovers from panics.
func (w *BaseWorker) safeRun(ctx context.Context, fn func(ctx context.Context) error) (err error) {
	defer func() {
		if r := recover(); r != nil {
			w.log.Error().Interface("panic", r).Msg("worker panicked")
			err = nil // Don't propagate panic
		}
	}()

	return fn(ctx)
}
