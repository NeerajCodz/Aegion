package workers

import (
	"context"
	"fmt"
	"time"

	"github.com/aegion/aegion/internal/platform/logger"
	"github.com/jackc/pgx/v5/pgxpool"
)

// SessionCleanupConfig configures the session cleanup worker.
type SessionCleanupConfig struct {
	DB            *pgxpool.Pool
	Log           *logger.Logger
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
	if cfg.InactiveAfter == 0 {
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
	w.Log().Debug().Msg("starting session cleanup")

	// Build dynamic SQL based on configured durations
	days := int(w.expiredAfter.Hours() / 24)
	hours := int(w.inactiveAfter.Hours())

	sql := fmt.Sprintf(`
		DELETE FROM core_sessions
		WHERE expires_at < NOW() - INTERVAL '%d days'
		   OR (active = FALSE AND updated_at < NOW() - INTERVAL '%d hours')
	`, days, hours)

	result, err := w.exec(ctx, sql)
	if err != nil {
		return err
	}

	deleted := result.RowsAffected()
	if deleted > 0 {
		w.Log().Info().Int64("deleted", deleted).Msg("expired sessions cleaned up")
	} else {
		w.Log().Debug().Msg("no expired sessions to clean up")
	}

	// Also clean up orphaned session auth methods
	result, err = w.exec(ctx, `
		DELETE FROM core_session_auth_methods
		WHERE session_id NOT IN (SELECT id FROM core_sessions)
	`)
	if err != nil {
		w.Log().Warn().Err(err).Msg("failed to clean up orphaned auth methods")
	} else if result.RowsAffected() > 0 {
		w.Log().Info().Int64("deleted", result.RowsAffected()).Msg("orphaned auth methods cleaned up")
	}

	return nil
}
