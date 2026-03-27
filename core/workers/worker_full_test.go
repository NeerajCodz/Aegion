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
// WORKER ACCESSOR TESTS FOR COVERAGE
// ============================================================================

// TestSessionCleanupWorkerAccessors tests all accessor methods
func TestSessionCleanupWorkerAccessors(t *testing.T) {
	cfg := SessionCleanupConfig{
		Interval: 45 * time.Minute,
	}
	worker := NewSessionCleanupWorker(cfg)

	// Test Name()
	assert.Equal(t, "session_cleanup", worker.Name())

	// Test Interval()
	assert.Equal(t, 45*time.Minute, worker.Interval())

	// Test Log()
	assert.NotNil(t, worker.Log())

	// Test DB()
	assert.Nil(t, worker.DB())

	// Test Done()
	assert.NotNil(t, worker.Done())

	// Test IsRunning()
	assert.False(t, worker.IsRunning())

	// Test SetRunning()
	worker.SetRunning(true)
	assert.True(t, worker.IsRunning())
	worker.SetRunning(false)
	assert.False(t, worker.IsRunning())
}

// TestFlowCleanupWorkerAccessors tests all accessor methods
func TestFlowCleanupWorkerAccessors(t *testing.T) {
	cfg := FlowCleanupConfig{
		Interval: 25 * time.Minute,
	}
	worker := NewFlowCleanupWorker(cfg)

	assert.Equal(t, "flow_cleanup", worker.Name())
	assert.Equal(t, 25*time.Minute, worker.Interval())
	assert.NotNil(t, worker.Log())
	assert.Nil(t, worker.DB())
	assert.NotNil(t, worker.Done())
	assert.False(t, worker.IsRunning())

	worker.SetRunning(true)
	assert.True(t, worker.IsRunning())
}

// TestEventProcessorWorkerAccessors tests all accessor methods
func TestEventProcessorWorkerAccessors(t *testing.T) {
	cfg := EventProcessorConfig{
		Subscriber: "test-sub",
		Interval:   7 * time.Second,
		BatchSize:  75,
		MaxRetries: 4,
		RetryDelay: 1500 * time.Millisecond,
	}
	worker := NewEventProcessorWorker(cfg)

	assert.Equal(t, "event_processor", worker.Name())
	assert.Equal(t, 7*time.Second, worker.Interval())
	assert.NotNil(t, worker.Log())
	assert.Nil(t, worker.DB())
	assert.NotNil(t, worker.Done())
	assert.False(t, worker.IsRunning())

	// Test internal fields via behavior
	worker.SetRunning(true)
	assert.True(t, worker.IsRunning())
	worker.SetRunning(false)
	assert.False(t, worker.IsRunning())
}

// TestCourierDispatchWorkerAccessors tests all accessor methods
func TestCourierDispatchWorkerAccessors(t *testing.T) {
	cfg := CourierDispatchConfig{
		Interval:   25 * time.Second,
		BatchSize:  25,
		MaxRetries: 6,
	}
	worker := NewCourierDispatchWorker(cfg)

	assert.Equal(t, "courier_dispatch", worker.Name())
	assert.Equal(t, 25*time.Second, worker.Interval())
	assert.NotNil(t, worker.Log())
	assert.Nil(t, worker.DB())
	assert.NotNil(t, worker.Done())
	assert.False(t, worker.IsRunning())

	worker.SetRunning(true)
	assert.True(t, worker.IsRunning())
	worker.SetRunning(false)
	assert.False(t, worker.IsRunning())
}

// ============================================================================
// MANAGER ACCESSOR TESTS FOR COVERAGE
// ============================================================================

// TestManagerWorkersList tests manager's workers list
func TestManagerWorkersList(t *testing.T) {
	manager := NewManager(ManagerConfig{})

	assert.Len(t, manager.workers, 0)

	// Add workers
	for i := 0; i < 3; i++ {
		manager.Register(&MockWorker{name: fmt.Sprintf("worker-%d", i)})
	}

	assert.Len(t, manager.workers, 3)

	// Verify they're in order
	for i := 0; i < 3; i++ {
		assert.Equal(t, fmt.Sprintf("worker-%d", i), manager.workers[i].Name())
	}
}

// TestManagerLoggerInitialization tests logger creation
func TestManagerLoggerInitialization(t *testing.T) {
	// With nil logger, should create default
	manager := NewManager(ManagerConfig{Log: nil})
	assert.NotNil(t, manager.log)

	// With provided logger, should use it
	customLog := NewManager(ManagerConfig{}).log
	manager2 := NewManager(ManagerConfig{Log: customLog})
	assert.NotNil(t, manager2.log)
}

// ============================================================================
// CONCURRENT OPERATION TESTS FOR COVERAGE
// ============================================================================

// TestConcurrentWorkerRegisterAndAccess tests thread-safe registration
func TestConcurrentWorkerRegisterAndAccess(t *testing.T) {
	manager := NewManager(ManagerConfig{})
	var wg sync.WaitGroup

	// Concurrent registration
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			worker := &MockWorker{name: fmt.Sprintf("worker-%d", id)}
			manager.Register(worker)
		}(i)
	}

	wg.Wait()

	assert.Len(t, manager.workers, 20)
}

// TestConcurrentWorkerStartStop tests concurrent start/stop operations
func TestConcurrentWorkerStartStop(t *testing.T) {
	var managers []*Manager
	var wg sync.WaitGroup

	// Create managers concurrently
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			manager := NewManager(ManagerConfig{})
			for j := 0; j < 3; j++ {
				worker := &MockWorker{
					name: fmt.Sprintf("manager-%d-worker-%d", id, j),
					startFn: func(ctx context.Context) error {
						<-ctx.Done()
						return ctx.Err()
					},
				}
				manager.Register(worker)
			}
			managers = append(managers, manager)
		}(i)
	}

	wg.Wait()

	// Start/stop concurrently
	for _, manager := range managers {
		wg.Add(1)
		go func(m *Manager) {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
			m.Start(ctx)
			time.Sleep(25 * time.Millisecond)
			m.Stop()
			cancel()
		}(manager)
	}

	wg.Wait()
}

// TestBaseWorkerConcurrentSetGetRunning tests thread-safe state operations
func TestBaseWorkerConcurrentSetGetRunning(t *testing.T) {
	worker := NewBaseWorker("test", nil, nil, time.Second)
	var wg sync.WaitGroup
	readCount := atomic.Int32{}
	writeCount := atomic.Int32{}

	// Concurrent reads and writes
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				if id%2 == 0 {
					worker.SetRunning(j%2 == 0)
					writeCount.Add(1)
				} else {
					_ = worker.IsRunning()
					readCount.Add(1)
				}
			}
		}(i)
	}

	wg.Wait()

	assert.Equal(t, int32(500), writeCount.Load())
	assert.Equal(t, int32(500), readCount.Load())
}

// ============================================================================
// RUNLOOP STATE MACHINE TESTS
// ============================================================================

// TestBaseWorkerRunLoopStateSequence tests state transitions
func TestBaseWorkerRunLoopStateSequence(t *testing.T) {
	states := make([]bool, 0)
	var mu sync.Mutex

	worker := NewBaseWorker("test", nil, nil, 50*time.Millisecond)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	recordState := func() {
		mu.Lock()
		states = append(states, worker.IsRunning())
		mu.Unlock()
	}

	recordState() // Before start
	assert.False(t, states[0])

	go func() {
		_ = worker.RunLoop(ctx, func(ctx context.Context) error {
			return nil
		})
	}()

	time.Sleep(25 * time.Millisecond)
	recordState() // During run
	assert.True(t, states[1])

	time.Sleep(80 * time.Millisecond)
	recordState() // After stop
	// May or may not be running depending on timing
}

// TestBaseWorkerStopSequenceWithDone tests Stop() sequence
func TestBaseWorkerStopSequenceWithDone(t *testing.T) {
	worker := NewBaseWorker("test", nil, nil, time.Second)

	// Not running initially
	assert.False(t, worker.IsRunning())

	// Set running
	worker.SetRunning(true)
	assert.True(t, worker.IsRunning())

	// Get done channel reference
	done := worker.Done()

	// Stop
	worker.Stop()

	// Should not be running
	assert.False(t, worker.IsRunning())

	// Done channel should be closed
	select {
	case <-done:
		// Expected
	case <-time.After(100 * time.Millisecond):
		t.Error("Done channel should be closed")
	}
}

// ============================================================================
// WORKER START VARIATIONS TESTS
// ============================================================================

// TestAllWorkersStartMethod tests Start implementation
func TestAllWorkersStartMethod(t *testing.T) {
	tests := []struct {
		name   string
		worker Worker
	}{
		{"SessionCleanup", NewSessionCleanupWorker(SessionCleanupConfig{Interval: 50 * time.Millisecond})},
		{"FlowCleanup", NewFlowCleanupWorker(FlowCleanupConfig{Interval: 50 * time.Millisecond})},
		{"EventProcessor", NewEventProcessorWorker(EventProcessorConfig{Interval: 50 * time.Millisecond})},
		{"CourierDispatch", NewCourierDispatchWorker(CourierDispatchConfig{Interval: 50 * time.Millisecond})},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
			defer cancel()

			// Start should return context error (since it will timeout)
			err := tt.worker.Start(ctx)

			// Should get context error or nil
			assert.True(t, err == nil || err == context.DeadlineExceeded,
				fmt.Sprintf("unexpected error: %v", err))
		})
	}
}

// ============================================================================
// EDGE CASE CONFIGURATION TESTS
// ============================================================================

// TestEventProcessorWorkerConfigEdgeCases tests config boundary values
func TestEventProcessorWorkerConfigEdgeCases(t *testing.T) {
	tests := []struct {
		name string
		cfg  EventProcessorConfig
		want struct {
			interval   time.Duration
			batchSize  int
			maxRetries int
			retryDelay time.Duration
			subscriber string
		}
	}{
		{
			name: "all zero values",
			cfg:  EventProcessorConfig{},
			want: struct {
				interval   time.Duration
				batchSize  int
				maxRetries int
				retryDelay time.Duration
				subscriber string
			}{
				interval:   10 * time.Second,
				batchSize:  100,
				maxRetries: 3,
				retryDelay: 1 * time.Second,
				subscriber: "default",
			},
		},
		{
			name: "very large values",
			cfg: EventProcessorConfig{
				Interval:   24 * time.Hour,
				BatchSize:  10000,
				MaxRetries: 100,
				RetryDelay: 1 * time.Hour,
				Subscriber: "massive",
			},
			want: struct {
				interval   time.Duration
				batchSize  int
				maxRetries int
				retryDelay time.Duration
				subscriber string
			}{
				interval:   24 * time.Hour,
				batchSize:  10000,
				maxRetries: 100,
				retryDelay: 1 * time.Hour,
				subscriber: "massive",
			},
		},
		{
			name: "minimum non-zero values",
			cfg: EventProcessorConfig{
				Interval:   1 * time.Millisecond,
				BatchSize:  1,
				MaxRetries: 1,
				RetryDelay: 1 * time.Millisecond,
				Subscriber: "x",
			},
			want: struct {
				interval   time.Duration
				batchSize  int
				maxRetries int
				retryDelay time.Duration
				subscriber string
			}{
				interval:   1 * time.Millisecond,
				batchSize:  1,
				maxRetries: 1,
				retryDelay: 1 * time.Millisecond,
				subscriber: "x",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			worker := NewEventProcessorWorker(tt.cfg)
			assert.Equal(t, tt.want.interval, worker.Interval())
			assert.Equal(t, tt.want.batchSize, worker.batchSize)
			assert.Equal(t, tt.want.maxRetries, worker.maxRetries)
			assert.Equal(t, tt.want.retryDelay, worker.retryDelay)
			assert.Equal(t, tt.want.subscriber, worker.subscriber)
		})
	}
}

// TestCourierDispatchWorkerConfigEdgeCases tests config boundary values
func TestCourierDispatchWorkerConfigEdgeCases(t *testing.T) {
	tests := []struct {
		name string
		cfg  CourierDispatchConfig
		want struct {
			interval   time.Duration
			batchSize  int
			maxRetries int
		}
	}{
		{
			name: "all zero",
			cfg:  CourierDispatchConfig{},
			want: struct {
				interval   time.Duration
				batchSize  int
				maxRetries int
			}{
				interval:   30 * time.Second,
				batchSize:  10,
				maxRetries: 3,
			},
		},
		{
			name: "large values",
			cfg: CourierDispatchConfig{
				Interval:   5 * time.Minute,
				BatchSize:  1000,
				MaxRetries: 50,
			},
			want: struct {
				interval   time.Duration
				batchSize  int
				maxRetries int
			}{
				interval:   5 * time.Minute,
				batchSize:  1000,
				maxRetries: 50,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			worker := NewCourierDispatchWorker(tt.cfg)
			assert.Equal(t, tt.want.interval, worker.Interval())
			assert.Equal(t, tt.want.batchSize, worker.batchSize)
			assert.Equal(t, tt.want.maxRetries, worker.maxRetries)
		})
	}
}

// ============================================================================
// MOCK WORKER BEHAVIOR TESTS
// ============================================================================

// TestMockWorkerBehavior tests MockWorker implementation
func TestMockWorkerBehavior(t *testing.T) {
	startCalled := atomic.Bool{}
	stopCalled := atomic.Bool{}

	worker := &MockWorker{
		name: "test",
		startFn: func(ctx context.Context) error {
			startCalled.Store(true)
			return nil
		},
		stopFn: func() {
			stopCalled.Store(true)
		},
	}

	// Test Name
	assert.Equal(t, "test", worker.Name())

	// Test Start
	err := worker.Start(context.Background())
	assert.NoError(t, err)
	assert.True(t, startCalled.Load())

	// Test Stop
	worker.Stop()
	assert.True(t, stopCalled.Load())
}

// TestMockWorkerNilHandlers tests MockWorker with nil handlers
func TestMockWorkerNilHandlers(t *testing.T) {
	worker := &MockWorker{
		name:    "test",
		startFn: nil,
		stopFn:  nil,
	}

	// Should not panic
	assert.Equal(t, "test", worker.Name())
	assert.NoError(t, worker.Start(context.Background()))
	worker.Stop()
}

// ============================================================================
// MANAGER INITIALIZATION VARIATIONS
// ============================================================================

// TestManagerInitializationVariations tests different manager configs
func TestManagerInitializationVariations(t *testing.T) {
	// Nil config
	m1 := NewManager(ManagerConfig{})
	assert.NotNil(t, m1.log)
	assert.NotNil(t, m1.workers)
	assert.Len(t, m1.workers, 0)

	// With log
	m2 := NewManager(ManagerConfig{Log: m1.log})
	assert.NotNil(t, m2.log)

	// Multiple managers
	managers := make([]*Manager, 5)
	for i := 0; i < 5; i++ {
		managers[i] = NewManager(ManagerConfig{})
		assert.NotNil(t, managers[i])
	}
}

// ============================================================================
// WORKER POOL ORCHESTRATION TESTS
// ============================================================================

// TestMultipleManagersIndependent tests independent manager instances
func TestMultipleManagersIndependent(t *testing.T) {
	m1Started := atomic.Bool{}
	m2Started := atomic.Bool{}

	m1 := NewManager(ManagerConfig{})
	m1.Register(&MockWorker{
		name: "m1-worker",
		startFn: func(ctx context.Context) error {
			m1Started.Store(true)
			<-ctx.Done()
			return ctx.Err()
		},
	})

	m2 := NewManager(ManagerConfig{})
	m2.Register(&MockWorker{
		name: "m2-worker",
		startFn: func(ctx context.Context) error {
			m2Started.Store(true)
			<-ctx.Done()
			return ctx.Err()
		},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	m1.Start(ctx)
	m2.Start(ctx)

	time.Sleep(50 * time.Millisecond)

	assert.True(t, m1Started.Load())
	assert.True(t, m2Started.Load())

	m1.Stop()
	m2.Stop()
}

// TestWorkerPoolScaling tests pool scaling with many workers
func TestWorkerPoolScaling(t *testing.T) {
	scenarios := []int{1, 5, 10, 25}

	for _, numWorkers := range scenarios {
		t.Run(fmt.Sprintf("workers-%d", numWorkers), func(t *testing.T) {
			manager := NewManager(ManagerConfig{})
			counter := atomic.Int32{}

			for i := 0; i < numWorkers; i++ {
				manager.Register(&MockWorker{
					name: fmt.Sprintf("worker-%d", i),
					startFn: func(ctx context.Context) error {
						counter.Add(1)
						<-ctx.Done()
						return ctx.Err()
					},
				})
			}

			ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
			manager.Start(ctx)
			time.Sleep(50 * time.Millisecond)
			manager.Stop()
			cancel()

			assert.Equal(t, int32(numWorkers), counter.Load())
		})
	}
}
