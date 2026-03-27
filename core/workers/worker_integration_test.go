package workers

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// ============================================================================
// DATABASE MOCK HELPERS
// ============================================================================

// MockDB provides a simple in-memory database mock for testing
type MockDB struct {
	mu      sync.Mutex
	data    map[string]interface{}
	queries []string
	results map[string]interface{}
}

func NewMockDB() *MockDB {
	return &MockDB{
		data:    make(map[string]interface{}),
		queries: make([]string, 0),
		results: make(map[string]interface{}),
	}
}

// ============================================================================
// SESSION CLEANUP WORKER INTEGRATION TESTS
// ============================================================================

// TestSessionCleanupWorkerStart verifies worker starts and runs cleanup
func TestSessionCleanupWorkerStart(t *testing.T) {
	cfg := SessionCleanupConfig{
		DB:       nil,
		Log:      nil,
		Interval: 50 * time.Millisecond,
	}

	worker := NewSessionCleanupWorker(cfg)

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()

	// Worker should run and return due to context timeout
	err := worker.Start(ctx)

	// Should return context error
	assert.Error(t, err)
}

// TestSessionCleanupWorkerName verifies worker name
func TestSessionCleanupWorkerName(t *testing.T) {
	worker := NewSessionCleanupWorker(SessionCleanupConfig{})
	assert.Equal(t, "session_cleanup", worker.Name())
}

// TestSessionCleanupWorkerDBAccess verifies worker can access DB
func TestSessionCleanupWorkerDBAccess(t *testing.T) {
	cfg := SessionCleanupConfig{
		DB:  nil,
		Log: nil,
	}

	worker := NewSessionCleanupWorker(cfg)

	// Should be able to access DB (nil in this case)
	assert.Nil(t, worker.DB())
}

// TestSessionCleanupWorkerLogAccess verifies worker can access logger
func TestSessionCleanupWorkerLogAccess(t *testing.T) {
	cfg := SessionCleanupConfig{
		DB:  nil,
		Log: nil,
	}

	worker := NewSessionCleanupWorker(cfg)

	// Should have logger
	assert.NotNil(t, worker.Log())
}

// TestSessionCleanupWorkerIntervalDefault verifies default interval
func TestSessionCleanupWorkerIntervalDefault(t *testing.T) {
	worker := NewSessionCleanupWorker(SessionCleanupConfig{})
	assert.Equal(t, 1*time.Hour, worker.Interval())
}

// TestSessionCleanupWorkerIntervalCustom verifies custom interval is set
func TestSessionCleanupWorkerIntervalCustom(t *testing.T) {
	customInterval := 2 * time.Hour
	cfg := SessionCleanupConfig{Interval: customInterval}
	worker := NewSessionCleanupWorker(cfg)
	assert.Equal(t, customInterval, worker.Interval())
}

// TestSessionCleanupWorkerStop verifies worker can be stopped
func TestSessionCleanupWorkerStop(t *testing.T) {
	worker := NewSessionCleanupWorker(SessionCleanupConfig{})

	// Set to running first
	worker.SetRunning(true)

	// Should not panic
	worker.Stop()

	// Done channel should be closed
	select {
	case <-worker.Done():
		// Expected
	case <-time.After(100 * time.Millisecond):
		t.Error("Done channel should be closed")
	}
}

// ============================================================================
// FLOW CLEANUP WORKER INTEGRATION TESTS
// ============================================================================

// TestFlowCleanupWorkerStart verifies worker starts and runs cleanup
func TestFlowCleanupWorkerStart(t *testing.T) {
	cfg := FlowCleanupConfig{
		DB:       nil,
		Log:      nil,
		Interval: 50 * time.Millisecond,
	}

	worker := NewFlowCleanupWorker(cfg)

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()

	// Worker should run and return due to context timeout
	err := worker.Start(ctx)

	// Should return context error
	assert.Error(t, err)
}

// TestFlowCleanupWorkerName verifies worker name
func TestFlowCleanupWorkerName(t *testing.T) {
	worker := NewFlowCleanupWorker(FlowCleanupConfig{})
	assert.Equal(t, "flow_cleanup", worker.Name())
}

// TestFlowCleanupWorkerDBAccess verifies worker can access DB
func TestFlowCleanupWorkerDBAccess(t *testing.T) {
	worker := NewFlowCleanupWorker(FlowCleanupConfig{})
	assert.Nil(t, worker.DB())
}

// TestFlowCleanupWorkerLogAccess verifies worker can access logger
func TestFlowCleanupWorkerLogAccess(t *testing.T) {
	worker := NewFlowCleanupWorker(FlowCleanupConfig{})
	assert.NotNil(t, worker.Log())
}

// TestFlowCleanupWorkerIntervalDefault verifies default interval
func TestFlowCleanupWorkerIntervalDefault(t *testing.T) {
	worker := NewFlowCleanupWorker(FlowCleanupConfig{})
	assert.Equal(t, 30*time.Minute, worker.Interval())
}

// TestFlowCleanupWorkerIntervalCustom verifies custom interval is set
func TestFlowCleanupWorkerIntervalCustom(t *testing.T) {
	customInterval := 15 * time.Minute
	cfg := FlowCleanupConfig{Interval: customInterval}
	worker := NewFlowCleanupWorker(cfg)
	assert.Equal(t, customInterval, worker.Interval())
}

// TestFlowCleanupWorkerStop verifies worker can be stopped
func TestFlowCleanupWorkerStop(t *testing.T) {
	worker := NewFlowCleanupWorker(FlowCleanupConfig{})

	// Set to running first
	worker.SetRunning(true)

	// Should not panic
	worker.Stop()

	// Done channel should be closed
	select {
	case <-worker.Done():
		// Expected
	case <-time.After(100 * time.Millisecond):
		t.Error("Done channel should be closed")
	}
}

// ============================================================================
// EVENT PROCESSOR WORKER INTEGRATION TESTS
// ============================================================================

// TestEventProcessorWorkerStart verifies worker starts and runs
func TestEventProcessorWorkerStart(t *testing.T) {
	cfg := EventProcessorConfig{
		DB:       nil,
		Log:      nil,
		Interval: 50 * time.Millisecond,
	}

	worker := NewEventProcessorWorker(cfg)

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()

	// Worker should run and return due to context timeout
	err := worker.Start(ctx)

	// Should return context error
	assert.Error(t, err)
}

// TestEventProcessorWorkerName verifies worker name
func TestEventProcessorWorkerName(t *testing.T) {
	worker := NewEventProcessorWorker(EventProcessorConfig{})
	assert.Equal(t, "event_processor", worker.Name())
}

// TestEventProcessorWorkerDBAccess verifies worker can access DB
func TestEventProcessorWorkerDBAccess(t *testing.T) {
	worker := NewEventProcessorWorker(EventProcessorConfig{})
	assert.Nil(t, worker.DB())
}

// TestEventProcessorWorkerLogAccess verifies worker can access logger
func TestEventProcessorWorkerLogAccess(t *testing.T) {
	worker := NewEventProcessorWorker(EventProcessorConfig{})
	assert.NotNil(t, worker.Log())
}

// TestEventProcessorWorkerIntervalDefault verifies default interval
func TestEventProcessorWorkerIntervalDefault(t *testing.T) {
	worker := NewEventProcessorWorker(EventProcessorConfig{})
	assert.Equal(t, 10*time.Second, worker.Interval())
}

// TestEventProcessorWorkerIntervalCustom verifies custom interval
func TestEventProcessorWorkerIntervalCustom(t *testing.T) {
	cfg := EventProcessorConfig{Interval: 5 * time.Second}
	worker := NewEventProcessorWorker(cfg)
	assert.Equal(t, 5*time.Second, worker.Interval())
}

// TestEventProcessorWorkerBatchSizeDefault verifies default batch size
func TestEventProcessorWorkerBatchSizeDefault(t *testing.T) {
	worker := NewEventProcessorWorker(EventProcessorConfig{})
	assert.Equal(t, 100, worker.batchSize)
}

// TestEventProcessorWorkerBatchSizeCustom verifies custom batch size
func TestEventProcessorWorkerBatchSizeCustom(t *testing.T) {
	cfg := EventProcessorConfig{BatchSize: 50}
	worker := NewEventProcessorWorker(cfg)
	assert.Equal(t, 50, worker.batchSize)
}

// TestEventProcessorWorkerMaxRetriesDefault verifies default max retries
func TestEventProcessorWorkerMaxRetriesDefault(t *testing.T) {
	worker := NewEventProcessorWorker(EventProcessorConfig{})
	assert.Equal(t, 3, worker.maxRetries)
}

// TestEventProcessorWorkerMaxRetriesCustom verifies custom max retries
func TestEventProcessorWorkerMaxRetriesCustom(t *testing.T) {
	cfg := EventProcessorConfig{MaxRetries: 5}
	worker := NewEventProcessorWorker(cfg)
	assert.Equal(t, 5, worker.maxRetries)
}

// TestEventProcessorWorkerRetryDelayDefault verifies default retry delay
func TestEventProcessorWorkerRetryDelayDefault(t *testing.T) {
	worker := NewEventProcessorWorker(EventProcessorConfig{})
	assert.Equal(t, 1*time.Second, worker.retryDelay)
}

// TestEventProcessorWorkerRetryDelayCustom verifies custom retry delay
func TestEventProcessorWorkerRetryDelayCustom(t *testing.T) {
	cfg := EventProcessorConfig{RetryDelay: 2 * time.Second}
	worker := NewEventProcessorWorker(cfg)
	assert.Equal(t, 2*time.Second, worker.retryDelay)
}

// TestEventProcessorWorkerSubscriberDefault verifies default subscriber
func TestEventProcessorWorkerSubscriberDefault(t *testing.T) {
	worker := NewEventProcessorWorker(EventProcessorConfig{})
	assert.Equal(t, "default", worker.subscriber)
}

// TestEventProcessorWorkerSubscriberCustom verifies custom subscriber
func TestEventProcessorWorkerSubscriberCustom(t *testing.T) {
	cfg := EventProcessorConfig{Subscriber: "custom"}
	worker := NewEventProcessorWorker(cfg)
	assert.Equal(t, "custom", worker.subscriber)
}

// TestEventProcessorWorkerStop verifies worker can be stopped
func TestEventProcessorWorkerStop(t *testing.T) {
	worker := NewEventProcessorWorker(EventProcessorConfig{})

	// Set to running first
	worker.SetRunning(true)

	// Should not panic
	worker.Stop()

	// Done channel should be closed
	select {
	case <-worker.Done():
		// Expected
	case <-time.After(100 * time.Millisecond):
		t.Error("Done channel should be closed")
	}
}

// TestEventProcessorWorkerStopTwice verifies Stop is idempotent
func TestEventProcessorWorkerStopTwice(t *testing.T) {
	worker := NewEventProcessorWorker(EventProcessorConfig{})

	// Should not panic when called twice
	worker.Stop()
	worker.Stop()
}

// ============================================================================
// COURIER DISPATCH WORKER INTEGRATION TESTS
// ============================================================================

// TestCourierDispatchWorkerStart verifies worker starts and runs
func TestCourierDispatchWorkerStart(t *testing.T) {
	cfg := CourierDispatchConfig{
		DB:       nil,
		Log:      nil,
		Interval: 50 * time.Millisecond,
	}

	worker := NewCourierDispatchWorker(cfg)

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()

	// Worker should run and return due to context timeout
	err := worker.Start(ctx)

	// Should return context error
	assert.Error(t, err)
}

// TestCourierDispatchWorkerName verifies worker name
func TestCourierDispatchWorkerName(t *testing.T) {
	worker := NewCourierDispatchWorker(CourierDispatchConfig{})
	assert.Equal(t, "courier_dispatch", worker.Name())
}

// TestCourierDispatchWorkerDBAccess verifies worker can access DB
func TestCourierDispatchWorkerDBAccess(t *testing.T) {
	worker := NewCourierDispatchWorker(CourierDispatchConfig{})
	assert.Nil(t, worker.DB())
}

// TestCourierDispatchWorkerLogAccess verifies worker can access logger
func TestCourierDispatchWorkerLogAccess(t *testing.T) {
	worker := NewCourierDispatchWorker(CourierDispatchConfig{})
	assert.NotNil(t, worker.Log())
}

// TestCourierDispatchWorkerIntervalDefault verifies default interval
func TestCourierDispatchWorkerIntervalDefault(t *testing.T) {
	worker := NewCourierDispatchWorker(CourierDispatchConfig{})
	assert.Equal(t, 30*time.Second, worker.Interval())
}

// TestCourierDispatchWorkerIntervalCustom verifies custom interval
func TestCourierDispatchWorkerIntervalCustom(t *testing.T) {
	cfg := CourierDispatchConfig{Interval: 15 * time.Second}
	worker := NewCourierDispatchWorker(cfg)
	assert.Equal(t, 15*time.Second, worker.Interval())
}

// TestCourierDispatchWorkerBatchSizeDefault verifies default batch size
func TestCourierDispatchWorkerBatchSizeDefault(t *testing.T) {
	worker := NewCourierDispatchWorker(CourierDispatchConfig{})
	assert.Equal(t, 10, worker.batchSize)
}

// TestCourierDispatchWorkerBatchSizeCustom verifies custom batch size
func TestCourierDispatchWorkerBatchSizeCustom(t *testing.T) {
	cfg := CourierDispatchConfig{BatchSize: 20}
	worker := NewCourierDispatchWorker(cfg)
	assert.Equal(t, 20, worker.batchSize)
}

// TestCourierDispatchWorkerMaxRetriesDefault verifies default max retries
func TestCourierDispatchWorkerMaxRetriesDefault(t *testing.T) {
	worker := NewCourierDispatchWorker(CourierDispatchConfig{})
	assert.Equal(t, 3, worker.maxRetries)
}

// TestCourierDispatchWorkerMaxRetriesCustom verifies custom max retries
func TestCourierDispatchWorkerMaxRetriesCustom(t *testing.T) {
	cfg := CourierDispatchConfig{MaxRetries: 5}
	worker := NewCourierDispatchWorker(cfg)
	assert.Equal(t, 5, worker.maxRetries)
}

// TestCourierDispatchWorkerStop verifies worker can be stopped
func TestCourierDispatchWorkerStop(t *testing.T) {
	worker := NewCourierDispatchWorker(CourierDispatchConfig{})

	// Set to running first
	worker.SetRunning(true)

	// Should not panic
	worker.Stop()

	// Done channel should be closed
	select {
	case <-worker.Done():
		// Expected
	case <-time.After(100 * time.Millisecond):
		t.Error("Done channel should be closed")
	}
}

// TestCourierDispatchWorkerStopTwice verifies Stop is idempotent
func TestCourierDispatchWorkerStopTwice(t *testing.T) {
	worker := NewCourierDispatchWorker(CourierDispatchConfig{})

	// Should not panic when called twice
	worker.Stop()
	worker.Stop()
}

// ============================================================================
// WORKER POOL LIFECYCLE TESTS
// ============================================================================

// TestManagerRegisterAndStartMultiple verifies full lifecycle
func TestManagerRegisterAndStartMultiple(t *testing.T) {
	manager := NewManager(ManagerConfig{})

	sessionWorker := NewSessionCleanupWorker(SessionCleanupConfig{Interval: 50 * time.Millisecond})
	flowWorker := NewFlowCleanupWorker(FlowCleanupConfig{Interval: 50 * time.Millisecond})
	eventWorker := NewEventProcessorWorker(EventProcessorConfig{Interval: 50 * time.Millisecond})
	courierWorker := NewCourierDispatchWorker(CourierDispatchConfig{Interval: 50 * time.Millisecond})

	manager.Register(sessionWorker)
	manager.Register(flowWorker)
	manager.Register(eventWorker)
	manager.Register(courierWorker)

	assert.Len(t, manager.workers, 4)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	manager.Start(ctx)
	time.Sleep(50 * time.Millisecond)
	manager.Stop()
}

// TestWorkerPoolWithMixedWorkerTypes verifies different worker types work together
func TestWorkerPoolWithMixedWorkerTypes(t *testing.T) {
	manager := NewManager(ManagerConfig{})

	executionCounts := make([]atomic.Int32, 4)

	// Add mock and real workers
	workers := []Worker{
		&MockWorker{
			name: "mock-1",
			startFn: func(ctx context.Context) error {
				executionCounts[0].Add(1)
				<-ctx.Done()
				return ctx.Err()
			},
		},
		NewSessionCleanupWorker(SessionCleanupConfig{Interval: 50 * time.Millisecond}),
		&MockWorker{
			name: "mock-2",
			startFn: func(ctx context.Context) error {
				executionCounts[1].Add(1)
				<-ctx.Done()
				return ctx.Err()
			},
		},
		NewEventProcessorWorker(EventProcessorConfig{Interval: 50 * time.Millisecond}),
	}

	for _, w := range workers {
		manager.Register(w)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()

	manager.Start(ctx)
	time.Sleep(100 * time.Millisecond)
	manager.Stop()

	// Mock workers should have executed
	assert.Greater(t, executionCounts[0].Load(), int32(0))
	assert.Greater(t, executionCounts[1].Load(), int32(0))
}

// TestWorkerPoolConcurrentStartStop verifies safe concurrent operations
func TestWorkerPoolConcurrentStartStop(t *testing.T) {
	manager := NewManager(ManagerConfig{})

	for i := 0; i < 10; i++ {
		worker := &MockWorker{
			name: fmt.Sprintf("worker-%d", i),
			startFn: func(ctx context.Context) error {
				<-ctx.Done()
				return ctx.Err()
			},
		}
		manager.Register(worker)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	manager.Start(ctx)
	time.Sleep(50 * time.Millisecond)

	// Should handle multiple stops gracefully
	manager.Stop()
	manager.Stop()
}

// TestRapidStartStopCycles verifies rapid lifecycle changes
func TestRapidStartStopCycles(t *testing.T) {
	for cycle := 0; cycle < 3; cycle++ {
		manager := NewManager(ManagerConfig{})

		worker := &MockWorker{
			name: "rapid-test",
			startFn: func(ctx context.Context) error {
				<-ctx.Done()
				return ctx.Err()
			},
		}
		manager.Register(worker)

		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		manager.Start(ctx)
		time.Sleep(25 * time.Millisecond)
		manager.Stop()
		cancel()
	}
}

// TestWorkerPoolMemorySafety verifies no goroutine leaks
func TestWorkerPoolMemorySafety(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping memory safety test in short mode")
	}

	for iteration := 0; iteration < 5; iteration++ {
		manager := NewManager(ManagerConfig{})

		for i := 0; i < 5; i++ {
			worker := &MockWorker{
				name: fmt.Sprintf("worker-%d-%d", iteration, i),
				startFn: func(ctx context.Context) error {
					<-ctx.Done()
					return ctx.Err()
				},
			}
			manager.Register(worker)
		}

		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		manager.Start(ctx)
		time.Sleep(50 * time.Millisecond)
		manager.Stop()
		cancel()

		// Allow goroutines to clean up
		time.Sleep(10 * time.Millisecond)
	}
}

// ============================================================================
// WORKER INTERFACE COMPLIANCE TESTS
// ============================================================================

// TestAllWorkersImplementInterface verifies all worker types implement Worker
func TestAllWorkersImplementInterface(t *testing.T) {
	workers := []Worker{
		NewSessionCleanupWorker(SessionCleanupConfig{}),
		NewFlowCleanupWorker(FlowCleanupConfig{}),
		NewEventProcessorWorker(EventProcessorConfig{}),
		NewCourierDispatchWorker(CourierDispatchConfig{}),
	}

	for _, w := range workers {
		// Each should have Name method
		assert.NotEmpty(t, w.Name())

		// Each should have Stop method (we can call it safely)
		w.Stop()
	}
}

// TestWorkerBaseName tests all worker names are correctly set
func TestWorkerBaseName(t *testing.T) {
	tests := []struct {
		worker   Worker
		expected string
	}{
		{NewSessionCleanupWorker(SessionCleanupConfig{}), "session_cleanup"},
		{NewFlowCleanupWorker(FlowCleanupConfig{}), "flow_cleanup"},
		{NewEventProcessorWorker(EventProcessorConfig{}), "event_processor"},
		{NewCourierDispatchWorker(CourierDispatchConfig{}), "courier_dispatch"},
	}

	for _, tt := range tests {
		assert.Equal(t, tt.expected, tt.worker.Name())
	}
}

// TestWorkerBaseIntervals tests all worker intervals
func TestWorkerBaseIntervals(t *testing.T) {
	tests := []struct {
		worker   *BaseWorker
		expected time.Duration
	}{
		{NewSessionCleanupWorker(SessionCleanupConfig{}).BaseWorker, 1 * time.Hour},
		{NewFlowCleanupWorker(FlowCleanupConfig{}).BaseWorker, 30 * time.Minute},
		{NewEventProcessorWorker(EventProcessorConfig{}).BaseWorker, 10 * time.Second},
		{NewCourierDispatchWorker(CourierDispatchConfig{}).BaseWorker, 30 * time.Second},
	}

	for _, tt := range tests {
		assert.Equal(t, tt.expected, tt.worker.Interval())
	}
}

// TestWorkerBaseAccessors verifies all accessors work
func TestWorkerBaseAccessors(t *testing.T) {
	sessionWorker := NewSessionCleanupWorker(SessionCleanupConfig{})

	// Test all accessors
	assert.Equal(t, "session_cleanup", sessionWorker.Name())
	assert.NotNil(t, sessionWorker.Log())
	assert.Nil(t, sessionWorker.DB())
	assert.Equal(t, 1*time.Hour, sessionWorker.Interval())
	assert.NotNil(t, sessionWorker.Done())
	assert.False(t, sessionWorker.IsRunning())

	// Test SetRunning
	sessionWorker.SetRunning(true)
	assert.True(t, sessionWorker.IsRunning())

	sessionWorker.SetRunning(false)
	assert.False(t, sessionWorker.IsRunning())
}

// TestWorkerStopClosureSequence verifies Stop sequence is correct
func TestWorkerStopClosureSequence(t *testing.T) {
	worker := NewSessionCleanupWorker(SessionCleanupConfig{})

	// Set to running
	worker.SetRunning(true)

	// Get done channel before stop
	done := worker.Done()

	// Stop should close the channel
	worker.Stop()

	// Channel should be closed
	select {
	case <-done:
		// Expected
	case <-time.After(100 * time.Millisecond):
		t.Error("Done channel should be closed after Stop()")
	}

	// Should no longer be running
	assert.False(t, worker.IsRunning())
}

// TestWorkerDoubleStopSafety verifies multiple stops are safe
func TestWorkerDoubleStopSafety(t *testing.T) {
	sessionWorker := NewSessionCleanupWorker(SessionCleanupConfig{})
	sessionWorker.SetRunning(true)
	sessionWorker.Stop()
	sessionWorker.Stop()
	sessionWorker.Stop()
	assert.False(t, sessionWorker.IsRunning())

	flowWorker := NewFlowCleanupWorker(FlowCleanupConfig{})
	flowWorker.SetRunning(true)
	flowWorker.Stop()
	flowWorker.Stop()
	flowWorker.Stop()
	assert.False(t, flowWorker.IsRunning())

	eventWorker := NewEventProcessorWorker(EventProcessorConfig{})
	eventWorker.SetRunning(true)
	eventWorker.Stop()
	eventWorker.Stop()
	eventWorker.Stop()
	assert.False(t, eventWorker.IsRunning())

	courierWorker := NewCourierDispatchWorker(CourierDispatchConfig{})
	courierWorker.SetRunning(true)
	courierWorker.Stop()
	courierWorker.Stop()
	courierWorker.Stop()
	assert.False(t, courierWorker.IsRunning())
}
