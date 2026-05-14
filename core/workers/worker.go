// Package workers provides background job workers for Aegion.
package workers

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/aegion/aegion/internal/xlog"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

var errWorkerDBUnavailable = errors.New("worker database unavailable")

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
	db          *pgxpool.Pool
	log         *xlog.Logger
	workers     []Worker
	wg          sync.WaitGroup
	cancel      context.CancelFunc
	mu          sync.Mutex
	stopTimeout time.Duration
}

// ManagerConfig configures the worker manager.
type ManagerConfig struct {
	DB          *pgxpool.Pool
	Log         any
	StopTimeout time.Duration // Timeout for graceful shutdown (default: 30 seconds)
}

// NewManager creates a new worker manager.
func NewManager(cfg ManagerConfig) *Manager {
	log := cfg.Log
	adaptedLog := xlog.Adapt(log)
	if adaptedLog == nil {
		adaptedLog = xlog.New(xlog.Config{Level: "info", Format: "json"})
	}

	stopTimeout := cfg.StopTimeout
	if stopTimeout == 0 {
		stopTimeout = 30 * time.Second
	}

	return &Manager{
		db:          cfg.DB,
		log:         adaptedLog.WithComponent("worker_manager"),
		workers:     make([]Worker, 0),
		stopTimeout: stopTimeout,
	}
}

// Register adds a worker to the manager.
func (m *Manager) Register(w Worker) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.workers = append(m.workers, w)
	m.log.Info("worker registered", "worker", w.Name())
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
			m.log.Info("starting worker", "worker", worker.Name())
			if err := worker.Start(ctx); err != nil {
				m.log.Error("worker stopped with error", "error", err, "worker", worker.Name())
			} else {
				m.log.Info("worker stopped", "worker", worker.Name())
			}
		}(w)
	}

	m.log.Info("all workers started", "count", len(m.workers))
}

// Stop gracefully stops all workers.
func (m *Manager) Stop() {
	m.log.Info("stopping all workers")

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
		m.log.Info("all workers stopped gracefully")
	case <-time.After(m.stopTimeout):
		m.log.Warn("timeout waiting for workers to stop")
	}
}

// BaseWorker provides common functionality for workers.
type BaseWorker struct {
	name     string
	db       *pgxpool.Pool
	log      *xlog.Logger
	interval time.Duration
	done     chan struct{}
	mu       sync.Mutex
	running  bool

	execFn     func(ctx context.Context, sql string, args ...interface{}) (pgconn.CommandTag, error)
	queryFn    func(ctx context.Context, sql string, args ...interface{}) (pgx.Rows, error)
	queryRowFn func(ctx context.Context, sql string, args ...interface{}) pgx.Row
}

type workerErrorRow struct {
	err error
}

func (r workerErrorRow) Scan(dest ...interface{}) error {
	return r.err
}

// NewBaseWorker creates a new base worker.
func NewBaseWorker(name string, db *pgxpool.Pool, log any, interval time.Duration) *BaseWorker {
	adaptedLog := xlog.Adapt(log)
	if adaptedLog == nil {
		adaptedLog = xlog.New(xlog.Config{Level: "info", Format: "json"})
	}

	w := &BaseWorker{
		name:     name,
		db:       db,
		log:      adaptedLog.WithComponent(name),
		interval: interval,
		done:     make(chan struct{}),
	}
	if db != nil {
		w.execFn = func(ctx context.Context, sql string, args ...interface{}) (pgconn.CommandTag, error) {
			return db.Exec(ctx, sql, args...)
		}
		w.queryFn = func(ctx context.Context, sql string, args ...interface{}) (pgx.Rows, error) {
			return db.Query(ctx, sql, args...)
		}
		w.queryRowFn = func(ctx context.Context, sql string, args ...interface{}) pgx.Row {
			return db.QueryRow(ctx, sql, args...)
		}
	} else {
		w.execFn = func(context.Context, string, ...interface{}) (pgconn.CommandTag, error) {
			return pgconn.CommandTag{}, errWorkerDBUnavailable
		}
		w.queryFn = func(context.Context, string, ...interface{}) (pgx.Rows, error) {
			return nil, errWorkerDBUnavailable
		}
		w.queryRowFn = func(context.Context, string, ...interface{}) pgx.Row {
			return workerErrorRow{err: errWorkerDBUnavailable}
		}
	}
	return w
}

func (w *BaseWorker) exec(ctx context.Context, sql string, args ...interface{}) (pgconn.CommandTag, error) {
	if w.execFn == nil {
		return pgconn.CommandTag{}, errWorkerDBUnavailable
	}
	return w.execFn(ctx, sql, args...)
}

func (w *BaseWorker) query(ctx context.Context, sql string, args ...interface{}) (pgx.Rows, error) {
	if w.queryFn == nil {
		return nil, errWorkerDBUnavailable
	}
	return w.queryFn(ctx, sql, args...)
}

func (w *BaseWorker) queryRow(ctx context.Context, sql string, args ...interface{}) pgx.Row {
	if w.queryRowFn == nil {
		return workerErrorRow{err: errWorkerDBUnavailable}
	}
	return w.queryRowFn(ctx, sql, args...)
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
func (w *BaseWorker) Log() *xlog.Logger {
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
		w.log.Error("initial run failed", "error", err)
	}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-w.done:
			return nil
		case <-ticker.C:
			if err := w.safeRun(ctx, fn); err != nil {
				w.log.Error("periodic run failed", "error", err)
			}
		}
	}
}

// safeRun executes a function and recovers from panics.
func (w *BaseWorker) safeRun(ctx context.Context, fn func(ctx context.Context) error) (err error) {
	defer func() {
		if r := recover(); r != nil {
			w.log.Error("worker panicked", "panic", r)
			err = nil // Don't propagate panic
		}
	}()

	return fn(ctx)
}
