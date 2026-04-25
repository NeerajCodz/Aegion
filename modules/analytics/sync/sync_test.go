package sync

import (
	"context"
	"testing"
	"time"

	"github.com/aegion/aegion/modules/analytics"
	"github.com/stretchr/testify/assert"
)

// MockLogger implements the Logger interface for testing.
type MockLogger struct {
	messages []string
}

func (ml *MockLogger) Debug(msg string, keysAndValues ...interface{}) {
	ml.messages = append(ml.messages, msg)
}

func (ml *MockLogger) Info(msg string, keysAndValues ...interface{}) {
	ml.messages = append(ml.messages, msg)
}

func (ml *MockLogger) Warn(msg string, keysAndValues ...interface{}) {
	ml.messages = append(ml.messages, msg)
}

func (ml *MockLogger) Error(msg string, keysAndValues ...interface{}) {
	ml.messages = append(ml.messages, msg)
}

// MockDB implements the DB interface for testing.
type MockDB struct {
	lastExecSQL string
	queryResult []map[string]interface{}
}

func (md *MockDB) Exec(ctx context.Context, sql string, args ...interface{}) error {
	md.lastExecSQL = sql
	return nil
}

func (md *MockDB) Query(ctx context.Context, sql string, args ...interface{}) ([]map[string]interface{}, error) {
	return md.queryResult, nil
}

func (md *MockDB) QueryRow(ctx context.Context, sql string, args ...interface{}) (map[string]interface{}, error) {
	if len(md.queryResult) > 0 {
		return md.queryResult[0], nil
	}
	return make(map[string]interface{}), nil
}

// MockDuckDB implements the DuckDB interface for testing.
type MockDuckDB struct {
	lastExecSQL string
}

func (mddb *MockDuckDB) Exec(ctx context.Context, sql string, args ...interface{}) error {
	mddb.lastExecSQL = sql
	return nil
}

func (mddb *MockDuckDB) Query(ctx context.Context, sql string, args ...interface{}) ([]map[string]interface{}, error) {
	return []map[string]interface{}{}, nil
}

func (mddb *MockDuckDB) QueryRow(ctx context.Context, sql string, args ...interface{}) (map[string]interface{}, error) {
	return make(map[string]interface{}), nil
}

// TestRealTimeSyncStrategy tests the real-time sync strategy.
func TestRealTimeSyncStrategy(t *testing.T) {
	logger := &MockLogger{}
	db := &MockDB{}
	duckdb := &MockDuckDB{}

	strategy := NewRealTimeSync(
		true,    // enabled
		10,      // batch size
		100,     // flush interval ms
		3,       // max retries
		100,     // retry backoff ms
		logger,  // logger
		db,      // postgres db
		duckdb,  // duckdb
	)

	assert.NotNil(t, strategy)
	assert.Equal(t, "real_time", strategy.Name())
	assert.True(t, strategy.IsEnabled())

	ctx := context.Background()

	// Test Start
	err := strategy.Start(ctx)
	assert.NoError(t, err)

	// Test PublishEvent
	event := &analytics.SyncEvent{
		ID:          "test-event-1",
		SourceTable: "test_table",
		EventType:   "insert",
		Timestamp:   time.Now(),
		SourceRecord: map[string]interface{}{
			"id":   "123",
			"name": "test",
		},
		Metadata: map[string]interface{}{
			"user_id": "user-123",
		},
	}

	err = strategy.PublishEvent(ctx, event)
	assert.NoError(t, err)

	// Test Health
	health, err := strategy.Health(ctx)
	assert.NoError(t, err)
	assert.NotNil(t, health)
	assert.Equal(t, "real_time", strategy.Name())

	// Test Stop
	err = strategy.Stop(ctx)
	assert.NoError(t, err)
}

// TestBatchSyncStrategy tests the batch sync strategy.
func TestBatchSyncStrategy(t *testing.T) {
	logger := &MockLogger{}
	db := &MockDB{}
	duckdb := &MockDuckDB{}

	strategy := NewBatchSync(
		true,                             // enabled
		"1h",                             // interval
		"02:00",                          // start time
		[]string{"test_table"},           // tables
		100,                              // batch size
		10,                               // chunk size
		logger,                           // logger
		db,                               // postgres db
		duckdb,                           // duckdb
	)

	assert.NotNil(t, strategy)
	assert.Equal(t, "batch", strategy.Name())
	assert.True(t, strategy.IsEnabled())

	ctx := context.Background()

	// Test Start
	err := strategy.Start(ctx)
	assert.NoError(t, err)

	// Test PublishEvent (should be no-op)
	event := &analytics.SyncEvent{
		ID:          "test-event-1",
		SourceTable: "test_table",
		EventType:   "insert",
		Timestamp:   time.Now(),
	}

	err = strategy.PublishEvent(ctx, event)
	assert.NoError(t, err)

	// Test Health
	health, err := strategy.Health(ctx)
	assert.NoError(t, err)
	assert.NotNil(t, health)

	// Test Stop
	err = strategy.Stop(ctx)
	assert.NoError(t, err)
}

// TestAsyncSyncStrategy tests the async sync strategy.
func TestAsyncSyncStrategy(t *testing.T) {
	logger := &MockLogger{}
	db := &MockDB{}
	duckdb := &MockDuckDB{}

	strategy := NewAsyncSync(
		true,                 // enabled
		"memory",             // broker
		"test-topic",         // topic
		"test-group",         // consumer group
		2,                    // worker count
		3,                    // max retries
		100,                  // retry backoff ms
		logger,               // logger
		db,                   // postgres db
		duckdb,               // duckdb
	)

	assert.NotNil(t, strategy)
	assert.Equal(t, "async", strategy.Name())
	assert.True(t, strategy.IsEnabled())

	ctx := context.Background()

	// Test Start
	err := strategy.Start(ctx)
	assert.NoError(t, err)

	// Test PublishEvent
	event := &analytics.SyncEvent{
		ID:          "test-event-1",
		SourceTable: "test_table",
		EventType:   "insert",
		Timestamp:   time.Now(),
		SourceRecord: map[string]interface{}{
			"id":   "123",
			"name": "test",
		},
	}

	err = strategy.PublishEvent(ctx, event)
	assert.NoError(t, err)

	// Give workers time to process
	time.Sleep(500 * time.Millisecond)

	// Test Health
	health, err := strategy.Health(ctx)
	assert.NoError(t, err)
	assert.NotNil(t, health)

	// Test Stop
	err = strategy.Stop(ctx)
	assert.NoError(t, err)
}

// TestHybridSyncStrategy tests the hybrid sync strategy.
func TestHybridSyncStrategy(t *testing.T) {
	logger := &MockLogger{}
	db := &MockDB{}
	duckdb := &MockDuckDB{}

	primary := NewRealTimeSync(true, 10, 100, 3, 100, logger, db, duckdb)
	fallback := NewBatchSync(true, "1h", "02:00", []string{"test_table"}, 100, 10, logger, db, duckdb)

	hybrid := NewHybridSync(primary, fallback, logger)

	assert.NotNil(t, hybrid)
	assert.Equal(t, "hybrid", hybrid.Name())
	assert.True(t, hybrid.IsEnabled())

	ctx := context.Background()

	// Test Start
	err := hybrid.Start(ctx)
	assert.NoError(t, err)

	// Test PublishEvent
	event := &analytics.SyncEvent{
		ID:          "test-event-1",
		SourceTable: "test_table",
		EventType:   "insert",
		Timestamp:   time.Now(),
	}

	err = hybrid.PublishEvent(ctx, event)
	assert.NoError(t, err)

	// Test Health
	health, err := hybrid.Health(ctx)
	assert.NoError(t, err)
	assert.NotNil(t, health)

	// Test Stop
	err = hybrid.Stop(ctx)
	assert.NoError(t, err)
}

// TestSyncManager tests the sync manager orchestration.
func TestSyncManager(t *testing.T) {
	logger := &MockLogger{}
	db := &MockDB{}
	duckdb := &MockDuckDB{}

	manager := NewManager(logger)
	assert.NotNil(t, manager)

	// Register strategies
	real := NewRealTimeSync(true, 10, 100, 3, 100, logger, db, duckdb)
	batch := NewBatchSync(true, "1h", "02:00", []string{"test_table"}, 100, 10, logger, db, duckdb)
	async := NewAsyncSync(true, "memory", "topic", "group", 2, 3, 100, logger, db, duckdb)

	err := manager.RegisterStrategy(real)
	assert.NoError(t, err)

	err = manager.RegisterStrategy(batch)
	assert.NoError(t, err)

	err = manager.RegisterStrategy(async)
	assert.NoError(t, err)

	ctx := context.Background()

	// Test Start
	err = manager.Start(ctx)
	assert.NoError(t, err)

	// Test PublishEvent
	event := &analytics.SyncEvent{
		ID:          "test-event-1",
		SourceTable: "test_table",
		EventType:   "insert",
		Timestamp:   time.Now(),
	}

	err = manager.PublishEvent(ctx, event)
	assert.NoError(t, err)

	// Test duplicate detection
	err = manager.PublishEvent(ctx, event)
	assert.NoError(t, err) // Duplicate should be silently dropped

	// Test Health
	health, err := manager.Health(ctx)
	assert.NoError(t, err)
	assert.NotNil(t, health)

	// Test GetStrategy
	strategy, err := manager.GetStrategy("real_time")
	assert.NoError(t, err)
	assert.Equal(t, "real_time", strategy.Name())

	// Test unknown strategy
	_, err = manager.GetStrategy("unknown")
	assert.Error(t, err)

	// Test Stop
	err = manager.Stop(ctx)
	assert.NoError(t, err)
}

// TestRateLimiter tests the rate limiter.
func TestRateLimiter(t *testing.T) {
	rl := NewRateLimiter(5, 100*time.Millisecond)

	// Should allow up to capacity
	for i := 0; i < 5; i++ {
		assert.True(t, rl.Allow())
	}

	// Should deny when exhausted
	assert.False(t, rl.Allow())

	// Wait for refill
	time.Sleep(150 * time.Millisecond)
	assert.True(t, rl.Allow())
}

// TestEventDeduplicator tests event deduplication.
func TestEventDeduplicator(t *testing.T) {
	ed := NewEventDeduplicator(1 * time.Second)

	// First occurrence should not be duplicate
	assert.False(t, ed.IsDuplicate("event-1"))

	// Second occurrence should be duplicate
	assert.True(t, ed.IsDuplicate("event-1"))

	// Different event should not be duplicate
	assert.False(t, ed.IsDuplicate("event-2"))

	// Wait for expiration
	time.Sleep(1100 * time.Millisecond)
	assert.False(t, ed.IsDuplicate("event-1"))
}
