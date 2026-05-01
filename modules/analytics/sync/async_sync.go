package sync

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/aegion/aegion/modules/analytics"
)

// AsyncSync implements message queue based async synchronization.
type AsyncSync struct {
	enabled        bool
	broker         string
	topic          string
	consumerGroup  string
	workerCount    int
	maxRetries     int
	retryBackoff   time.Duration
	logger         Logger
	db             DB
	duckdb         DuckDB
	mu             sync.RWMutex
	eventQueue     chan *analytics.SyncEvent
	done           chan struct{}
	isRunning      bool
	errorCount     int
	warningCount   int
	lastErrorMsg   *string
	lastSyncAt     *time.Time
	positions      map[string]*analytics.SyncPosition
}

// NewAsyncSync creates a new async sync strategy instance.
func NewAsyncSync(
	enabled bool,
	broker string,
	topic string,
	consumerGroup string,
	workerCount int,
	maxRetries int,
	retryBackoffMs int,
	logger Logger,
	db DB,
	duckdb DuckDB,
) *AsyncSync {
	return &AsyncSync{
		enabled:       enabled,
		broker:        broker,
		topic:         topic,
		consumerGroup: consumerGroup,
		workerCount:   workerCount,
		maxRetries:    maxRetries,
		retryBackoff:  time.Duration(retryBackoffMs) * time.Millisecond,
		logger:        logger,
		db:            db,
		duckdb:        duckdb,
		eventQueue:    make(chan *analytics.SyncEvent, 1000),
		done:          make(chan struct{}),
		positions:     make(map[string]*analytics.SyncPosition),
	}
}

// Name returns the strategy identifier.
func (a *AsyncSync) Name() string {
	return "async"
}

// Start initializes async sync and begins processing.
func (a *AsyncSync) Start(ctx context.Context) error {
	if !a.enabled {
		a.logger.Debug("async sync disabled, skipping start")
		return nil
	}

	a.mu.Lock()
	if a.isRunning {
		a.mu.Unlock()
		return fmt.Errorf("async sync already running")
	}
	a.isRunning = true
	a.mu.Unlock()

	a.logger.Info("starting async sync strategy",
		"broker", a.broker,
		"topic", a.topic,
		"workers", a.workerCount,
	)

	// Start worker pool
	for i := 0; i < a.workerCount; i++ {
		go a.worker(ctx, i)
	}

	return nil
}

// Stop gracefully shuts down async sync.
func (a *AsyncSync) Stop(ctx context.Context) error {
	a.mu.Lock()
	if !a.isRunning {
		a.mu.Unlock()
		return nil
	}
	a.isRunning = false
	a.mu.Unlock()

	close(a.eventQueue)
	close(a.done)

	a.logger.Info("stopping async sync strategy")
	return nil
}

// PublishEvent publishes an event to the async queue.
func (a *AsyncSync) PublishEvent(ctx context.Context, event *analytics.SyncEvent) error {
	if !a.enabled {
		return fmt.Errorf("async sync is disabled")
	}

	select {
	case a.eventQueue <- event:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	default:
		// Queue is full, move to DLQ
		a.logger.Warn("async queue full, moving event to DLQ", "event_id", event.ID)
		return a.moveToDLQ(ctx, event, "queue full")
	}
}

// Health returns the current health status of async sync.
func (a *AsyncSync) Health(ctx context.Context) (*analytics.StrategyHealthStatus, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	syncLag := int64(0)
	if a.lastSyncAt != nil {
		syncLag = time.Since(*a.lastSyncAt).Milliseconds()
	}

	status := &analytics.StrategyHealthStatus{
		Enabled:       a.enabled,
		Healthy:       a.isRunning && a.errorCount == 0,
		LastSyncAt:    a.lastSyncAt,
		SyncLagMs:     syncLag,
		ErrorCount:    a.errorCount,
		WarningCount:  a.warningCount,
		LastErrorMsg:  a.lastErrorMsg,
		PositionCount: len(a.positions),
	}

	return status, nil
}

// GetPosition returns the current sync position for a table.
func (a *AsyncSync) GetPosition(ctx context.Context, table string) (*analytics.SyncPosition, error) {
	a.mu.RLock()
	pos, exists := a.positions[table]
	a.mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("no position found for table: %s", table)
	}

	return pos, nil
}

// SetPosition updates the sync position for a table.
func (a *AsyncSync) SetPosition(ctx context.Context, position *analytics.SyncPosition) error {
	a.mu.Lock()
	a.positions[position.SourceTable] = position
	a.mu.Unlock()

	// Persist to database
	sql := `INSERT INTO analytics_sync_position (strategy, source_table, last_synced_id, last_synced_at, checkpoint_data)
			VALUES ($1, $2, $3, $4, $5)
			ON CONFLICT (strategy, source_table) DO UPDATE SET
				last_synced_id = EXCLUDED.last_synced_id,
				last_synced_at = EXCLUDED.last_synced_at,
				checkpoint_data = EXCLUDED.checkpoint_data`

	checkpointData := "{}"
	if position.CheckpointData != nil {
		checkpointData = "{}"
	}

	return a.db.Exec(ctx, sql, a.Name(), position.SourceTable, position.LastSyncedID, position.LastSyncedAt, checkpointData)
}

// IsEnabled returns whether async sync is enabled.
func (a *AsyncSync) IsEnabled() bool {
	return a.enabled
}

// worker processes events from the async queue.
func (a *AsyncSync) worker(ctx context.Context, id int) {
	a.logger.Debug("async worker started", "worker_id", id)

	for {
		select {
		case event, ok := <-a.eventQueue:
			if !ok {
				a.logger.Debug("async worker shutting down", "worker_id", id)
				return
			}

			if err := a.processEvent(ctx, event); err != nil {
				a.logger.Error("error processing async event", "worker_id", id, "event_id", event.ID, "err", err)
			}

		case <-a.done:
			a.logger.Debug("async worker received stop signal", "worker_id", id)
			return
		}
	}
}

// processEvent processes a single event with retry logic.
func (a *AsyncSync) processEvent(ctx context.Context, event *analytics.SyncEvent) error {
	var lastErr error

	for attempt := 0; attempt <= a.maxRetries; attempt++ {
		if attempt > 0 {
			backoff := time.Duration(attempt) * a.retryBackoff
			time.Sleep(backoff)
		}

		// Insert into DuckDB
		sql := `INSERT INTO analytics_events (id, category, event_type, user_id, session_id, data, created_at, updated_at)
				VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`

		err := a.duckdb.Exec(ctx, sql,
			event.ID,
			"async_event",
			event.EventType,
			event.Metadata["user_id"],
			event.Metadata["session_id"],
			event.SourceRecord,
			event.Timestamp,
			time.Now(),
		)

		if err == nil {
			// Success
			now := time.Now()
			a.mu.Lock()
			a.lastSyncAt = &now
			a.errorCount = 0
			a.lastErrorMsg = nil
			a.mu.Unlock()

			a.logSyncEvent(ctx, 1, time.Millisecond, nil)
			return nil
		}

		lastErr = err
		a.logger.Warn("error processing async event, will retry",
			"event_id", event.ID,
			"attempt", attempt+1,
			"max_retries", a.maxRetries+1,
			"err", err,
		)
	}

	// All retries failed, move to DLQ
	a.logger.Error("async event failed after all retries, moving to DLQ",
		"event_id", event.ID,
		"err", lastErr,
	)

	a.mu.Lock()
	a.errorCount++
	msg := lastErr.Error()
	a.lastErrorMsg = &msg
	a.mu.Unlock()

	return a.moveToDLQ(ctx, event, lastErr.Error())
}

// moveToDLQ moves a failed event to the dead letter queue.
func (a *AsyncSync) moveToDLQ(ctx context.Context, event *analytics.SyncEvent, errorMsg string) error {
	sql := `INSERT INTO analytics_dlq_events (event_data, error_message, retry_count)
			VALUES ($1, $2, $3)`

	data, err := json.Marshal(event)
	if err != nil {
		a.logger.Error("error marshaling event to JSON", "event_id", event.ID, "err", err)
		return err
	}

	if err := a.db.Exec(ctx, sql, string(data), errorMsg, a.maxRetries); err != nil {
		a.logger.Error("error inserting into DLQ", "event_id", event.ID, "err", err)
		return err
	}

	a.mu.Lock()
	a.warningCount++
	a.mu.Unlock()

	return nil
}

// logSyncEvent records a sync operation in the database.
func (a *AsyncSync) logSyncEvent(ctx context.Context, recordCount int, duration time.Duration, err error) {
	sql := `INSERT INTO analytics_sync_events (strategy, event_type, records_synced, duration_ms, error_message)
			VALUES ($1, $2, $3, $4, $5)`

	eventType := "sync_complete"
	errorMsg := ""
	if err != nil {
		eventType = "sync_error"
		errorMsg = err.Error()
	}

	if execErr := a.db.Exec(ctx, sql, a.Name(), eventType, recordCount, duration.Milliseconds(), errorMsg); execErr != nil {
		a.logger.Error("error logging sync event", "err", execErr)
	}
}
