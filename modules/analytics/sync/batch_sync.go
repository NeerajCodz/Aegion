package sync

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/aegion/aegion/modules/analytics"
)

// BatchSync implements scheduled batch synchronization.
type BatchSync struct {
	enabled      bool
	interval     string
	startTime    string
	tables       []string
	batchSize    int
	chunkSize    int
	logger       Logger
	db           DB
	duckdb       DuckDB
	mu           sync.RWMutex
	ticker       *time.Ticker
	done         chan struct{}
	isRunning    bool
	errorCount   int
	lastErrorMsg *string
	lastSyncAt   *time.Time
	positions    map[string]*analytics.SyncPosition
}

// NewBatchSync creates a new batch sync strategy instance.
func NewBatchSync(
	enabled bool,
	interval string,
	startTime string,
	tables []string,
	batchSize int,
	chunkSize int,
	logger Logger,
	db DB,
	duckdb DuckDB,
) *BatchSync {
	return &BatchSync{
		enabled:   enabled,
		interval:  interval,
		startTime: startTime,
		tables:    tables,
		batchSize: batchSize,
		chunkSize: chunkSize,
		logger:    logger,
		db:        db,
		duckdb:    duckdb,
		done:      make(chan struct{}),
		positions: make(map[string]*analytics.SyncPosition),
	}
}

// Name returns the strategy identifier.
func (b *BatchSync) Name() string {
	return "batch"
}

// Start initializes batch sync and begins processing.
func (b *BatchSync) Start(ctx context.Context) error {
	if !b.enabled {
		b.logger.Debug("batch sync disabled, skipping start")
		return nil
	}

	b.mu.Lock()
	if b.isRunning {
		b.mu.Unlock()
		return fmt.Errorf("batch sync already running")
	}
	b.isRunning = true

	// For demo purposes, use a simple ticker
	// In production, you'd use a proper cron scheduler (robfig/cron)
	interval := 1 * time.Hour
	if b.interval == "1h" {
		interval = 1 * time.Hour
	} else if b.interval == "1d" || b.interval == "@daily" {
		interval = 24 * time.Hour
	}

	b.ticker = time.NewTicker(interval)
	b.mu.Unlock()

	b.logger.Info("starting batch sync strategy", "interval", b.interval, "tables", b.tables)

	// Start the batch loop
	go b.batchLoop()

	return nil
}

// Stop gracefully shuts down batch sync.
func (b *BatchSync) Stop(ctx context.Context) error {
	b.mu.Lock()
	if !b.isRunning {
		b.mu.Unlock()
		return nil
	}
	b.isRunning = false
	if b.ticker != nil {
		b.ticker.Stop()
	}
	b.mu.Unlock()

	close(b.done)
	b.logger.Info("stopping batch sync strategy")
	return nil
}

// PublishEvent is a no-op for batch sync (batch doesn't react to individual events).
func (b *BatchSync) PublishEvent(ctx context.Context, event *analytics.SyncEvent) error {
	// Batch sync doesn't process individual events
	return nil
}

// Health returns the current health status of batch sync.
func (b *BatchSync) Health(ctx context.Context) (*analytics.StrategyHealthStatus, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	syncLag := int64(0)
	if b.lastSyncAt != nil {
		syncLag = time.Since(*b.lastSyncAt).Milliseconds()
	}

	status := &analytics.StrategyHealthStatus{
		Enabled:       b.enabled,
		Healthy:       b.isRunning && b.errorCount == 0,
		LastSyncAt:    b.lastSyncAt,
		SyncLagMs:     syncLag,
		ErrorCount:    b.errorCount,
		WarningCount:  0,
		LastErrorMsg:  b.lastErrorMsg,
		PositionCount: len(b.positions),
	}

	return status, nil
}

// GetPosition returns the current sync position for a table.
func (b *BatchSync) GetPosition(ctx context.Context, table string) (*analytics.SyncPosition, error) {
	b.mu.RLock()
	pos, exists := b.positions[table]
	b.mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("no position found for table: %s", table)
	}

	return pos, nil
}

// SetPosition updates the sync position for a table.
func (b *BatchSync) SetPosition(ctx context.Context, position *analytics.SyncPosition) error {
	b.mu.Lock()
	b.positions[position.SourceTable] = position
	b.mu.Unlock()

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

	return b.db.Exec(ctx, sql, b.Name(), position.SourceTable, position.LastSyncedID, position.LastSyncedAt, checkpointData)
}

// IsEnabled returns whether batch sync is enabled.
func (b *BatchSync) IsEnabled() bool {
	return b.enabled
}

// batchLoop periodically runs batch sync jobs.
func (b *BatchSync) batchLoop() {
	for {
		select {
		case <-b.done:
			return
		case <-b.ticker.C:
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			if err := b.runBatchSync(ctx); err != nil {
				b.logger.Error("error running batch sync", "err", err)
			}
			cancel()
		}
	}
}

// runBatchSync runs the batch synchronization for configured tables.
func (b *BatchSync) runBatchSync(ctx context.Context) error {
	startTime := time.Now()
	totalRecords := 0
	var lastErr error

	for _, table := range b.tables {
		records, err := b.syncTable(ctx, table)
		if err != nil {
			b.logger.Error("error syncing table in batch mode", "table", table, "err", err)
			lastErr = err
			continue
		}
		totalRecords += records
	}

	now := time.Now()
	duration := time.Since(startTime)

	b.mu.Lock()
	b.lastSyncAt = &now
	if lastErr != nil {
		b.errorCount++
		msg := lastErr.Error()
		b.lastErrorMsg = &msg
	} else {
		b.errorCount = 0
		b.lastErrorMsg = nil
	}
	b.mu.Unlock()

	b.logger.Info("batch sync completed",
		"tables", len(b.tables),
		"total_records", totalRecords,
		"duration_ms", duration.Milliseconds(),
	)

	// Log sync event
	b.logSyncEvent(ctx, totalRecords, duration, lastErr)

	return lastErr
}

// syncTable syncs a single table from PostgreSQL to DuckDB.
func (b *BatchSync) syncTable(ctx context.Context, table string) (int, error) {
	// Get current position
	pos, err := b.getOrCreatePosition(ctx, table)
	if err != nil {
		return 0, fmt.Errorf("failed to get position for table %s: %w", table, err)
	}

	// Query data from PostgreSQL starting from last synced ID
	query := fmt.Sprintf(`SELECT * FROM %s WHERE id > $1 ORDER BY id LIMIT $2`, quoteSQLIdentifier(table))
	rows, err := b.db.Query(ctx, query, pos.LastSyncedID, b.batchSize)
	if err != nil {
		return 0, fmt.Errorf("failed to query table %s: %w", table, err)
	}

	if len(rows) == 0 {
		return 0, nil
	}

	// Insert into DuckDB
	for _, row := range rows {
		insertSQL := fmt.Sprintf(`INSERT INTO %s VALUES ($1, $2, $3, $4)`, quoteSQLIdentifier(table+"_sync"))
		if err := b.duckdb.Exec(ctx, insertSQL,
			row["id"],
			row["data"],
			row["created_at"],
			row["updated_at"],
		); err != nil {
			return 0, fmt.Errorf("failed to insert into DuckDB for table %s: %w", table, err)
		}
	}

	// Update position
	lastRow := rows[len(rows)-1]
	lastID := fmt.Sprintf("%v", lastRow["id"])
	nowTime := time.Now()
	pos.LastSyncedID = &lastID
	pos.LastSyncedAt = &nowTime
	if err := b.SetPosition(ctx, pos); err != nil {
		b.logger.Error("failed to update position", "table", table, "err", err)
	}

	return len(rows), nil
}

func quoteSQLIdentifier(identifier string) string {
	return `"` + strings.ReplaceAll(identifier, `"`, `""`) + `"`
}

// getOrCreatePosition retrieves or creates a sync position for a table.
func (b *BatchSync) getOrCreatePosition(ctx context.Context, table string) (*analytics.SyncPosition, error) {
	// Try to get from cache
	b.mu.RLock()
	if pos, exists := b.positions[table]; exists {
		b.mu.RUnlock()
		return pos, nil
	}
	b.mu.RUnlock()

	// Try to get from database
	sql := `SELECT id, strategy, source_table, last_synced_id, last_synced_at, checkpoint_data
			FROM analytics_sync_position
			WHERE strategy = $1 AND source_table = $2`

	result, err := b.db.QueryRow(ctx, sql, b.Name(), table)
	if err == nil && result != nil {
		pos := &analytics.SyncPosition{
			Strategy:    b.Name(),
			SourceTable: table,
		}
		// Hydrate from result
		b.mu.Lock()
		b.positions[table] = pos
		b.mu.Unlock()
		return pos, nil
	}

	// Create new position
	pos := &analytics.SyncPosition{
		ID:          "",
		Strategy:    b.Name(),
		SourceTable: table,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}

	b.mu.Lock()
	b.positions[table] = pos
	b.mu.Unlock()

	return pos, nil
}

// logSyncEvent records a sync operation in the database.
func (b *BatchSync) logSyncEvent(ctx context.Context, recordCount int, duration time.Duration, err error) {
	sql := `INSERT INTO analytics_sync_events (strategy, event_type, records_synced, duration_ms, error_message)
			VALUES ($1, $2, $3, $4, $5)`

	eventType := "sync_complete"
	errorMsg := ""
	if err != nil {
		eventType = "sync_error"
		errorMsg = err.Error()
	}

	if execErr := b.db.Exec(ctx, sql, b.Name(), eventType, recordCount, duration.Milliseconds(), errorMsg); execErr != nil {
		b.logger.Error("error logging sync event", "err", execErr)
	}
}
