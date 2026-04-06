package workers

import (
	"context"
	"fmt"
	"time"

	"github.com/aegion/aegion/internal/platform/logger"
	"github.com/jackc/pgx/v5/pgxpool"
)

// FlowCleanupConfig configures the flow cleanup worker.
type FlowCleanupConfig struct {
	DB             *pgxpool.Pool
	Log            *logger.Logger
	Interval       time.Duration // How often to run cleanup (default: 30 minutes)
	ExpiredAfter   time.Duration // Active flows expired more than this (default: 1 hour)
	CompletedAfter time.Duration // Completed/failed flows older than this (default: 24 hours)
}

// NewFlowCleanupWorker creates a new flow cleanup worker.
func NewFlowCleanupWorker(cfg FlowCleanupConfig) *FlowCleanupWorker {
	if cfg.Interval == 0 {
		cfg.Interval = 30 * time.Minute
	}
	if cfg.ExpiredAfter == 0 {
		cfg.ExpiredAfter = time.Hour
	}
	if cfg.CompletedAfter == 0 {
		cfg.CompletedAfter = 24 * time.Hour
	}

	return &FlowCleanupWorker{
		BaseWorker:     NewBaseWorker("flow_cleanup", cfg.DB, cfg.Log, cfg.Interval),
		expiredAfter:   cfg.ExpiredAfter,
		completedAfter: cfg.CompletedAfter,
	}
}

// FlowCleanupWorker periodically cleans up expired flows and continuity containers.
type FlowCleanupWorker struct {
	*BaseWorker
	expiredAfter   time.Duration
	completedAfter time.Duration
}

// Start begins the flow cleanup worker.
func (w *FlowCleanupWorker) Start(ctx context.Context) error {
	return w.RunLoop(ctx, w.cleanup)
}

// cleanup removes expired flows and continuity containers.
func (w *FlowCleanupWorker) cleanup(ctx context.Context) error {
	w.Log().Debug().Msg("starting flow cleanup")

	// Clean up expired flows
	flowsDeleted, err := w.cleanupFlows(ctx)
	if err != nil {
		w.Log().Error().Err(err).Msg("failed to clean up flows")
	}

	// Clean up expired continuity containers
	containersDeleted, err := w.cleanupContinuityContainers(ctx)
	if err != nil {
		w.Log().Error().Err(err).Msg("failed to clean up continuity containers")
	}

	if flowsDeleted > 0 || containersDeleted > 0 {
		w.Log().Info().
			Int64("flows_deleted", flowsDeleted).
			Int64("containers_deleted", containersDeleted).
			Msg("flow cleanup completed")
	} else {
		w.Log().Debug().Msg("no expired flows or containers to clean up")
	}

	return nil
}

// cleanupFlows removes expired and old completed flows.
func (w *FlowCleanupWorker) cleanupFlows(ctx context.Context) (int64, error) {
	expiredHours := int(w.expiredAfter.Hours())
	completedHours := int(w.completedAfter.Hours())

	sql := fmt.Sprintf(`
		DELETE FROM core_flows
		WHERE (state = 'active' AND expires_at < NOW() - INTERVAL '%d hours')
		   OR (state IN ('complete', 'failed') AND updated_at < NOW() - INTERVAL '%d hours')
	`, expiredHours, completedHours)

	result, err := w.exec(ctx, sql)
	if err != nil {
		return 0, err
	}

	return result.RowsAffected(), nil
}

// cleanupContinuityContainers removes expired continuity containers.
func (w *FlowCleanupWorker) cleanupContinuityContainers(ctx context.Context) (int64, error) {
	// Delete containers that have expired
	result, err := w.exec(ctx, `
		DELETE FROM core_continuity_containers
		WHERE expires_at < NOW()
	`)
	if err != nil {
		return 0, err
	}

	return result.RowsAffected(), nil
}

// CleanupOldFlows removes flows older than the specified duration.
func (w *FlowCleanupWorker) CleanupOldFlows(ctx context.Context, olderThan time.Duration) (int64, error) {
	cutoff := time.Now().Add(-olderThan)

	result, err := w.exec(ctx, `
		DELETE FROM core_flows
		WHERE created_at < $1
	`, cutoff)
	if err != nil {
		return 0, err
	}

	return result.RowsAffected(), nil
}

// CleanupOldContainers removes continuity containers older than the specified duration.
func (w *FlowCleanupWorker) CleanupOldContainers(ctx context.Context, olderThan time.Duration) (int64, error) {
	cutoff := time.Now().Add(-olderThan)

	result, err := w.exec(ctx, `
		DELETE FROM core_continuity_containers
		WHERE created_at < $1
	`, cutoff)
	if err != nil {
		return 0, err
	}

	return result.RowsAffected(), nil
}
