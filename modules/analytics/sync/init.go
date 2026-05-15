package sync

import (
	"context"
	"fmt"

	"github.com/aegion/aegion/internal/xlog"
	"github.com/aegion/aegion/modules/analytics"
)

// InitParams holds parameters for initializing the sync layer.
type InitParams struct {
	Config *analytics.Config
	Logger *xlog.Logger
	DB     DB
	DuckDB DuckDB
}

// InitializeSyncManager creates and initializes all configured sync strategies.
func InitializeSyncManager(params InitParams) (*Manager, error) {
	if !params.Config.Sync.Enabled {
		params.Logger.Debug("sync layer disabled in configuration")
		return nil, nil
	}

	manager := NewManager(params.Logger)

	// Register strategies based on configuration
	for _, strategyName := range params.Config.Sync.Strategies {
		var strategy Strategy

		switch strategyName {
		case "real_time":
			if !params.Config.Sync.RealTime.Enabled {
				params.Logger.Debug("real-time sync disabled, skipping")
				continue
			}
			strategy = NewRealTimeSync(
				true,
				params.Config.Sync.RealTime.BatchSize,
				params.Config.Sync.RealTime.FlushIntervalMs,
				params.Config.Sync.RealTime.MaxRetries,
				params.Config.Sync.RealTime.RetryBackoffMs,
				params.Logger,
				params.DB,
				params.DuckDB,
			)

		case "batch":
			if !params.Config.Sync.Batch.Enabled {
				params.Logger.Debug("batch sync disabled, skipping")
				continue
			}
			strategy = NewBatchSync(
				true,
				params.Config.Sync.Batch.Interval,
				params.Config.Sync.Batch.StartTime,
				params.Config.Sync.Batch.Tables,
				params.Config.Sync.Batch.BatchSize,
				params.Config.Sync.Batch.ChunkSize,
				params.Logger,
				params.DB,
				params.DuckDB,
			)

		case "async":
			if !params.Config.Sync.Async.Enabled {
				params.Logger.Debug("async sync disabled, skipping")
				continue
			}
			strategy = NewAsyncSync(
				true,
				params.Config.Sync.Async.Broker,
				params.Config.Sync.Async.Topic,
				params.Config.Sync.Async.ConsumerGroup,
				params.Config.Sync.Async.WorkerCount,
				params.Config.Sync.Async.MaxRetries,
				params.Config.Sync.Async.RetryBackoffMs,
				params.Logger,
				params.DB,
				params.DuckDB,
			)

		default:
			params.Logger.Warn("unknown sync strategy", "strategy", strategyName)
			continue
		}

		if err := manager.RegisterStrategy(strategy); err != nil {
			return nil, fmt.Errorf("failed to register strategy %s: %w", strategyName, err)
		}
	}

	return manager, nil
}

// StartSyncManager starts the sync manager and all registered strategies.
func StartSyncManager(ctx context.Context, manager *Manager) error {
	if manager == nil {
		return nil
	}

	return manager.Start(ctx)
}

// StopSyncManager gracefully shuts down the sync manager.
func StopSyncManager(ctx context.Context, manager *Manager) error {
	if manager == nil {
		return nil
	}

	return manager.Stop(ctx)
}
