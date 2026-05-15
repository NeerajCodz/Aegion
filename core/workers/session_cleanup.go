package workers

import (
	"context"
	"time"

	"github.com/aegion/aegion/internal/xlog"
	"github.com/jackc/pgx/v5/pgxpool"
)

// SessionCleanupConfig configures the session cleanup worker.
type SessionCleanupConfig struct {
	DB            *pgxpool.Pool
	Log           *xlog.Logger
	Interval      time.Duration // How often to run cleanup (default: 1 hour)
	ExpiredAfter  time.Duration // Delete sessions expired more than this (default: 7 days)
	InactiveAfter time.Duration // Delete inactive sessions after this (default: 1 day)
}

// NewSessionCleanupWorker creates a new session cleanup worker.
func NewSessionCleanupWorker(cfg SessionCleanupConfig) *SessionCleanupWorker {
	if cfg.Interval == 0 {
		cfg.Interval = time.Hour
	}
	if cfg.ExpiredAfter == 0 {
		cfg.ExpiredAfter = 7 * 24 * time.Hour
	}
	if cfg.ExpiredAfter < 0 {
		cfg.ExpiredAfter = 7 * 24 * time.Hour
	}
	if cfg.InactiveAfter == 0 {
		cfg.InactiveAfter = 24 * time.Hour
	}
	if cfg.InactiveAfter < 0 {
		cfg.InactiveAfter = 24 * time.Hour
	}

	return &SessionCleanupWorker{
		BaseWorker:    NewBaseWorker("session_cleanup", cfg.DB, cfg.Log, cfg.Interval),
		expiredAfter:  cfg.ExpiredAfter,
		inactiveAfter: cfg.InactiveAfter,
	}
}

// SessionCleanupWorker periodically cleans up expired sessions.
type SessionCleanupWorker struct {
	*BaseWorker
	expiredAfter  time.Duration
	inactiveAfter time.Duration
}

// Start begins the session cleanup worker.
func (w *SessionCleanupWorker) Start(ctx context.Context) error {
	return w.RunLoop(ctx, w.cleanup)
}

// cleanup removes expired sessions from the database.
func (w *SessionCleanupWorker) cleanup(ctx context.Context) error {
	w.Log().Debug("starting session cleanup")

	sql := `
		DELETE FROM core_sessions
		WHERE expires_at < NOW() - ($1 * INTERVAL '1 second')
		   OR (active = FALSE AND updated_at < NOW() - ($2 * INTERVAL '1 second'))
	`

	result, err := w.exec(ctx, sql, w.expiredAfter.Seconds(), w.inactiveAfter.Seconds())
	if err != nil {
		return err
	}

	deleted := result.RowsAffected()
	if deleted > 0 {
		w.Log().Info("expired sessions cleaned up", "deleted", deleted)
	} else {
		w.Log().Debug("no expired sessions to clean up")
	}

	// Also clean up orphaned session auth methods
	result, err = w.exec(ctx, `
		DELETE FROM core_session_auth_methods
		WHERE session_id NOT IN (SELECT id FROM core_sessions)
	`)
	if err != nil {
		w.Log().Warn("failed to clean up orphaned auth methods", "error", err)
	} else if result.RowsAffected() > 0 {
		w.Log().Info("orphaned auth methods cleaned up", "deleted", result.RowsAffected())
	}

	return nil
}
