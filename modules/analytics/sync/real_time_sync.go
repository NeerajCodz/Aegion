package sync

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/aegion/aegion/modules/analytics"
)

// RealTimeSync implements real-time CDC/trigger based synchronization.
type RealTimeSync struct {
	enabled       bool
	batchSize     int
	flushInterval time.Duration
	maxRetries    int
	retryBackoff  time.Duration
	logger        Logger
	db            DB
	duckdb        DuckDB
	mu            sync.RWMutex
	eventBuffer   []*analytics.SyncEvent
	ticker        *time.Ticker
	done          chan struct{}
	isRunning     bool
	errorCount    int
	lastErrorMsg  *string
	lastSyncAt    *time.Time
	positions     map[string]*analytics.SyncPosition
}

// NewRealTimeSync creates a new real-time sync strategy instance.
func NewRealTimeSync(
	enabled bool,
	batchSize int,
	flushIntervalMs int,
	maxRetries int,
	retryBackoffMs int,
	logger Logger,
	db DB,
	duckdb DuckDB,
) *RealTimeSync {
	return &RealTimeSync{
		enabled:       enabled,
		batchSize:     batchSize,
		flushInterval: time.Duration(flushIntervalMs) * time.Millisecond,
		maxRetries:    maxRetries,
		retryBackoff:  time.Duration(retryBackoffMs) * time.Millisecond,
		logger:        logger,
		db:            db,
		duckdb:        duckdb,
		eventBuffer:   make([]*analytics.SyncEvent, 0, batchSize),
		done:          make(chan struct{}),
		positions:     make(map[string]*analytics.SyncPosition),
	}
}

// Name returns the strategy identifier.
func (r *RealTimeSync) Name() string {
	return "real_time"
}

// Start initializes real-time sync and begins processing.
func (r *RealTimeSync) Start(ctx context.Context) error {
	if !r.enabled {
		r.logger.Debug("real-time sync disabled, skipping start")
		return nil
	}

	r.mu.Lock()
	if r.isRunning {
		r.mu.Unlock()
		return fmt.Errorf("real-time sync already running")
	}
	r.isRunning = true
	r.ticker = time.NewTicker(r.flushInterval)
	r.mu.Unlock()

	r.logger.Info("starting real-time sync strategy")

	// Start the flush loop
	go r.flushLoop()

	return nil
}

// Stop gracefully shuts down real-time sync.
func (r *RealTimeSync) Stop(ctx context.Context) error {
	r.mu.Lock()
	if !r.isRunning {
		r.mu.Unlock()
		return nil
	}
	r.isRunning = false
	if r.ticker != nil {
		r.ticker.Stop()
	}
	r.mu.Unlock()

	close(r.done)
	r.logger.Info("stopping real-time sync strategy")

	// Final flush
	return r.flush(ctx)
}

// PublishEvent publishes an event for real-time syncing.
func (r *RealTimeSync) PublishEvent(ctx context.Context, event *analytics.SyncEvent) error {
	if !r.enabled {
		return fmt.Errorf("real-time sync is disabled")
	}

	r.mu.Lock()
	r.eventBuffer = append(r.eventBuffer, event)
	shouldFlush := len(r.eventBuffer) >= r.batchSize
	r.mu.Unlock()

	if shouldFlush {
		return r.flush(ctx)
	}

	return nil
}

// Health returns the current health status of real-time sync.
func (r *RealTimeSync) Health(ctx context.Context) (*analytics.StrategyHealthStatus, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	syncLag := int64(0)
	if r.lastSyncAt != nil {
		syncLag = time.Since(*r.lastSyncAt).Milliseconds()
	}

	status := &analytics.StrategyHealthStatus{
		Enabled:       r.enabled,
		Healthy:       r.isRunning && r.errorCount == 0,
		LastSyncAt:    r.lastSyncAt,
		SyncLagMs:     syncLag,
		ErrorCount:    r.errorCount,
		WarningCount:  0,
		LastErrorMsg:  r.lastErrorMsg,
		PositionCount: len(r.positions),
	}

	return status, nil
}

// GetPosition returns the current sync position for a table.
func (r *RealTimeSync) GetPosition(ctx context.Context, table string) (*analytics.SyncPosition, error) {
	r.mu.RLock()
	pos, exists := r.positions[table]
	r.mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("no position found for table: %s", table)
	}

	return pos, nil
}

// SetPosition updates the sync position for a table.
func (r *RealTimeSync) SetPosition(ctx context.Context, position *analytics.SyncPosition) error {
	r.mu.Lock()
	r.positions[position.SourceTable] = position
	r.mu.Unlock()

	// Persist to database
	sql := `INSERT INTO analytics_sync_position (strategy, source_table, last_synced_id, last_synced_at, checkpoint_data)
			VALUES ($1, $2, $3, $4, $5)
			ON CONFLICT (strategy, source_table) DO UPDATE SET
				last_synced_id = EXCLUDED.last_synced_id,
				last_synced_at = EXCLUDED.last_synced_at,
				checkpoint_data = EXCLUDED.checkpoint_data`

	checkpointData := "{}"
	if position.CheckpointData != nil {
		// In a real implementation, marshal to JSON
		checkpointData = "{}"
	}

	return r.db.Exec(ctx, sql, r.Name(), position.SourceTable, position.LastSyncedID, position.LastSyncedAt, checkpointData)
}

// IsEnabled returns whether real-time sync is enabled.
func (r *RealTimeSync) IsEnabled() bool {
	return r.enabled
}

// flushLoop periodically flushes buffered events.
func (r *RealTimeSync) flushLoop() {
	for {
		select {
		case <-r.done:
			return
		case <-r.ticker.C:
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			if err := r.flush(ctx); err != nil {
				r.logger.Error("error flushing real-time sync events", "err", err)
			}
			cancel()
		}
	}
}

// flush writes buffered events to DuckDB.
func (r *RealTimeSync) flush(ctx context.Context) error {
	r.mu.Lock()
	if len(r.eventBuffer) == 0 {
		r.mu.Unlock()
		return nil
	}

	events := r.eventBuffer
	r.eventBuffer = make([]*analytics.SyncEvent, 0, r.batchSize)
	r.mu.Unlock()

	startTime := time.Now()
	var lastErr error

	for attempt := 0; attempt <= r.maxRetries; attempt++ {
		if attempt > 0 {
			backoff := time.Duration(attempt) * r.retryBackoff
			time.Sleep(backoff)
		}

		// Build insert statement for events
		sql := `INSERT INTO analytics_events (id, category, event_type, user_id, session_id, data, created_at, updated_at)
				VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`

		for _, event := range events {
			if err := r.duckdb.Exec(ctx, sql,
				event.ID,
				"sync_event",
				event.EventType,
				event.Metadata["user_id"],
				event.Metadata["session_id"],
				event.SourceRecord,
				event.Timestamp,
				time.Now(),
			); err != nil {
				lastErr = err
				r.logger.Error("error inserting event into DuckDB", "err", err)
				continue
			}
		}

		if lastErr == nil {
			break
		}
	}

	now := time.Now()
	r.mu.Lock()
	r.lastSyncAt = &now
	if lastErr != nil {
		r.errorCount++
		msg := lastErr.Error()
		r.lastErrorMsg = &msg
	} else {
		r.errorCount = 0
		r.lastErrorMsg = nil
	}
	r.mu.Unlock()

	duration := time.Since(startTime)
	r.logger.Info("flushed real-time sync events",
		"count", len(events),
		"duration_ms", duration.Milliseconds(),
		"error", lastErr,
	)

	// Log sync event
	r.logSyncEvent(ctx, len(events), duration, lastErr)

	return lastErr
}

// logSyncEvent records a sync operation in the database.
func (r *RealTimeSync) logSyncEvent(ctx context.Context, recordCount int, duration time.Duration, err error) {
	sql := `INSERT INTO analytics_sync_events (strategy, event_type, records_synced, duration_ms, error_message)
			VALUES ($1, $2, $3, $4, $5)`

	eventType := "sync_complete"
	errorMsg := ""
	if err != nil {
		eventType = "sync_error"
		errorMsg = err.Error()
	}

	if execErr := r.db.Exec(ctx, sql, r.Name(), eventType, recordCount, duration.Milliseconds(), errorMsg); execErr != nil {
		r.logger.Error("error logging sync event", "err", execErr)
	}
}
