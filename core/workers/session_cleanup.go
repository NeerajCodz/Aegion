package workers

import (
	"context"
	"time"

	"github.com/aegion/aegion/internal/platform/logger"
	"github.com/jackc/pgx/v5/pgxpool"
)

// SessionCleanupConfig configures the session cleanup worker.
type SessionCleanupConfig struct {
	DB       *pgxpool.Pool
	Log      *logger.Logger
	Interval time.Duration // How often to run cleanup (default: 1 hour)
}

// SessionCleanupWorker periodically cleans up expired sessions.
type SessionCleanupWorker struct {
	*BaseWorker
}

// NewSessionCleanupWorker creates a new session cleanup worker.
func NewSessionCleanupWorker(cfg SessionCleanupConfig) *SessionCleanupWorker {
	if cfg.Interval == 0 {
		cfg.Interval = time.Hour
	}

	return &SessionCleanupWorker{
		BaseWorker: NewBaseWorker("session_cleanup", cfg.DB, cfg.Log, cfg.Interval),
	}
}

// Start begins the session cleanup worker.
func (w *SessionCleanupWorker) Start(ctx context.Context) error {
	return w.RunLoop(ctx, w.cleanup)
}

// cleanup removes expired sessions from the database.
func (w *SessionCleanupWorker) cleanup(ctx context.Context) error {
	w.Log().Debug().Msg("starting session cleanup")

	// Delete expired sessions (expired more than 7 days ago)
	// and inactive sessions (revoked more than 1 day ago)
	result, err := w.DB().Exec(ctx, `
		DELETE FROM core_sessions
		WHERE expires_at < NOW() - INTERVAL '7 days'
		   OR (active = FALSE AND updated_at < NOW() - INTERVAL '1 day')
	`)
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
	result, err = w.DB().Exec(ctx, `
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
