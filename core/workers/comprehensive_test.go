package workers

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// ============================================================================
// MANAGER TESTS
// ============================================================================

// TestManagerRegisterSingleWorker verifies that a single worker can be registered
func TestManagerRegisterSingleWorker(t *testing.T) {
	manager := NewManager(ManagerConfig{})
	worker := &MockWorker{name: "test-worker"}

	manager.Register(worker)

	assert.Len(t, manager.workers, 1)
	assert.Equal(t, "test-worker", manager.workers[0].Name())
}

// TestManagerRegisterMultipleWorkers verifies that multiple workers can be registered
func TestManagerRegisterMultipleWorkers(t *testing.T) {
	manager := NewManager(ManagerConfig{})

	for i := 0; i < 5; i++ {
		worker := &MockWorker{name: fmt.Sprintf("worker-%d", i)}
		manager.Register(worker)
	}

	assert.Len(t, manager.workers, 5)
}

// TestManagerRegisterConcurrent verifies thread-safety of worker registration
func TestManagerRegisterConcurrent(t *testing.T) {
	manager := NewManager(ManagerConfig{})
	var wg sync.WaitGroup

	// Register workers concurrently
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			worker := &MockWorker{name: fmt.Sprintf("worker-%d", id)}
			manager.Register(worker)
		}(i)
	}

	wg.Wait()
	assert.Len(t, manager.workers, 10)
}

// TestManagerStartWithoutWorkers verifies graceful start with no workers
func TestManagerStartWithoutWorkers(t *testing.T) {
	manager := NewManager(ManagerConfig{})
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	// Should not panic
	manager.Start(ctx)
	time.Sleep(50 * time.Millisecond)
	manager.Stop()
}

// TestManagerStartSingleWorker verifies worker is started correctly
func TestManagerStartSingleWorker(t *testing.T) {
	manager := NewManager(ManagerConfig{})

	// Create a test worker that increments a counter when started
	callCount := atomic.Int32{}
	testWorker := &MockWorker{
		name: "test",
		startFn: func(ctx context.Context) error {
			callCount.Add(1)
			return nil
		},
	}

	manager.Register(testWorker)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	manager.Start(ctx)
	time.Sleep(50 * time.Millisecond)
	manager.Stop()

	assert.Greater(t, callCount.Load(), int32(0))
}

// TestManagerStartMultipleWorkers verifies all workers are started
func TestManagerStartMultipleWorkers(t *testing.T) {
	manager := NewManager(ManagerConfig{})
	startedWorkers := atomic.Int32{}

	for i := 0; i < 3; i++ {
		testWorker := &MockWorker{
			name: fmt.Sprintf("worker-%d", i),
			startFn: func(ctx context.Context) error {
				startedWorkers.Add(1)
				<-ctx.Done()
				return ctx.Err()
			},
		}
		manager.Register(testWorker)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	manager.Start(ctx)
	time.Sleep(50 * time.Millisecond)

	assert.Equal(t, int32(3), startedWorkers.Load())
	manager.Stop()
}

// TestManagerStopGraceful verifies graceful shutdown
func TestManagerStopGraceful(t *testing.T) {
	manager := NewManager(ManagerConfig{})

	stoppedWorkers := atomic.Int32{}
	for i := 0; i < 2; i++ {
		testWorker := &MockWorker{
			name: fmt.Sprintf("worker-%d", i),
			startFn: func(ctx context.Context) error {
				<-ctx.Done()
				return ctx.Err()
			},
			stopFn: func() {
				stoppedWorkers.Add(1)
			},
		}
		manager.Register(testWorker)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	manager.Start(ctx)
	time.Sleep(50 * time.Millisecond)

	// Stop should call Stop() on all workers
	manager.Stop()

	assert.Equal(t, int32(2), stoppedWorkers.Load())
}

// TestManagerStopTimeout verifies timeout handling during shutdown
func TestManagerStopTimeout(t *testing.T) {
	manager := NewManager(ManagerConfig{})

	// Create a worker that ignores context cancellation
	testWorker := &MockWorker{
		name: "stubborn",
		startFn: func(ctx context.Context) error {
			// Ignore context, just sleep forever (until test times out)
			select {
			case <-time.After(60 * time.Second):
				return nil
			}
		},
	}

	manager.Register(testWorker)

	ctx := context.Background()
	manager.Start(ctx)
	time.Sleep(50 * time.Millisecond)

	// Stop with short timeout - should log warning but not hang
	start := time.Now()
	manager.Stop()
	elapsed := time.Since(start)

	// Should complete within a few seconds (30s timeout + buffer)
	assert.Less(t, elapsed, 35*time.Second)
}

// TestManagerContextCancellation verifies context cancellation propagates to workers
func TestManagerContextCancellation(t *testing.T) {
	manager := NewManager(ManagerConfig{})

	ctxReceivedCancellation := atomic.Bool{}
	testWorker := &MockWorker{
		name: "ctx-test",
		startFn: func(ctx context.Context) error {
			<-ctx.Done()
			ctxReceivedCancellation.Store(true)
			return ctx.Err()
		},
	}

	manager.Register(testWorker)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	manager.Start(ctx)

	time.Sleep(50 * time.Millisecond)
	cancel()

	time.Sleep(50 * time.Millisecond)
	manager.Stop()

	assert.True(t, ctxReceivedCancellation.Load())
}

// ============================================================================
// BASE WORKER RUN LOOP TESTS
// ============================================================================

// TestBaseWorkerRunLoopExecutesImmediately verifies immediate first execution
func TestBaseWorkerRunLoopExecutesImmediately(t *testing.T) {
	worker := NewBaseWorker("test", nil, nil, 1*time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	executionCount := atomic.Int32{}
	fn := func(ctx context.Context) error {
		executionCount.Add(1)
		return nil
	}

	go func() {
		_ = worker.RunLoop(ctx, fn)
	}()

	// Wait a bit for initial execution
	time.Sleep(50 * time.Millisecond)

	// Should have executed at least once immediately
	assert.Greater(t, executionCount.Load(), int32(0))
}

// TestBaseWorkerRunLoopPeriodicExecution verifies periodic execution
func TestBaseWorkerRunLoopPeriodicExecution(t *testing.T) {
	worker := NewBaseWorker("test", nil, nil, 50*time.Millisecond)
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	executionCount := atomic.Int32{}
	fn := func(ctx context.Context) error {
		executionCount.Add(1)
		return nil
	}

	go func() {
		_ = worker.RunLoop(ctx, fn)
	}()

	// Wait for periodic executions
	time.Sleep(150 * time.Millisecond)

	// Should have executed multiple times (immediate + at least 2 periodic)
	assert.Greater(t, executionCount.Load(), int32(2))
}

// TestBaseWorkerRunLoopContextCancellation verifies context cancellation stops loop
func TestBaseWorkerRunLoopContextCancellation(t *testing.T) {
	worker := NewBaseWorker("test", nil, nil, 100*time.Millisecond)
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()

	executionCount := atomic.Int32{}
	fn := func(ctx context.Context) error {
		executionCount.Add(1)
		return nil
	}

	err := make(chan error, 1)
	go func() {
		err <- worker.RunLoop(ctx, fn)
	}()

	// Wait for context to timeout
	time.Sleep(200 * time.Millisecond)

	// Should return context error
	returnErr := <-err
	assert.ErrorIs(t, returnErr, context.DeadlineExceeded)
}

// TestBaseWorkerRunLoopDoneChannelStop verifies done channel stops loop
func TestBaseWorkerRunLoopDoneChannelStop(t *testing.T) {
	worker := NewBaseWorker("test", nil, nil, 100*time.Millisecond)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	executionCount := atomic.Int32{}
	fn := func(ctx context.Context) error {
		executionCount.Add(1)
		return nil
	}

	err := make(chan error, 1)
	go func() {
		err <- worker.RunLoop(ctx, fn)
	}()

	// Wait for at least one execution
	time.Sleep(50 * time.Millisecond)

	// Stop the worker via done channel
	worker.Stop()

	// Should return nil (normal stop)
	returnErr := <-err
	assert.NoError(t, returnErr)
}

// TestBaseWorkerRunLoopErrorHandling verifies errors don't stop the loop
func TestBaseWorkerRunLoopErrorHandling(t *testing.T) {
	worker := NewBaseWorker("test", nil, nil, 50*time.Millisecond)
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	executionCount := atomic.Int32{}
	fn := func(ctx context.Context) error {
		executionCount.Add(1)
		if executionCount.Load() == 1 {
			return errors.New("test error")
		}
		return nil
	}

	go func() {
		_ = worker.RunLoop(ctx, fn)
	}()

	time.Sleep(150 * time.Millisecond)

	// Should have executed multiple times despite first error
	assert.Greater(t, executionCount.Load(), int32(1))
}

// TestBaseWorkerRunLoopSetRunningState verifies running state is set/unset
func TestBaseWorkerRunLoopSetRunningState(t *testing.T) {
	worker := NewBaseWorker("test", nil, nil, 100*time.Millisecond)
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()

	fn := func(ctx context.Context) error {
		return nil
	}

	// Should not be running initially
	assert.False(t, worker.IsRunning())

	go func() {
		_ = worker.RunLoop(ctx, fn)
	}()

	// Should be running during loop
	time.Sleep(50 * time.Millisecond)
	assert.True(t, worker.IsRunning())

	// Wait for completion
	<-ctx.Done()
	time.Sleep(50 * time.Millisecond)

	// Should not be running after loop ends
	assert.False(t, worker.IsRunning())
}

// TestBaseWorkerSafeRunPanicRecovery verifies panic recovery in safeRun
func TestBaseWorkerSafeRunPanicRecovery(t *testing.T) {
	worker := NewBaseWorker("test", nil, nil, 50*time.Millisecond)
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	executionCount := atomic.Int32{}
	panicCount := atomic.Int32{}

	fn := func(ctx context.Context) error {
		executionCount.Add(1)
		if executionCount.Load() == 2 {
			panicCount.Add(1)
			panic("test panic")
		}
		return nil
	}

	go func() {
		_ = worker.RunLoop(ctx, fn)
	}()

	time.Sleep(150 * time.Millisecond)

	// Should have executed multiple times despite panic
	assert.Greater(t, executionCount.Load(), int32(2))
	assert.Equal(t, int32(1), panicCount.Load())
}

// TestBaseWorkerConcurrentRunLoop verifies multiple concurrent run loops work
func TestBaseWorkerConcurrentRunLoop(t *testing.T) {
	count := atomic.Int32{}

	for i := 0; i < 5; i++ {
		worker := NewBaseWorker(fmt.Sprintf("worker-%d", i), nil, nil, 50*time.Millisecond)
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)

		go func(w *BaseWorker, c context.CancelFunc) {
			defer c()
			_ = w.RunLoop(ctx, func(ctx context.Context) error {
				count.Add(1)
				return nil
			})
		}(worker, cancel)
	}

	time.Sleep(150 * time.Millisecond)

	// All workers should have executed
	assert.Greater(t, count.Load(), int32(5))
}

// ============================================================================
// SESSION CLEANUP WORKER TESTS
// ============================================================================

// TestSessionCleanupWorkerCreation verifies worker creation and defaults
func TestSessionCleanupWorkerCreation(t *testing.T) {
	cfg := SessionCleanupConfig{
		DB:  nil,
		Log: nil,
	}

	worker := NewSessionCleanupWorker(cfg)

	assert.NotNil(t, worker)
	assert.Equal(t, "session_cleanup", worker.Name())
	assert.Equal(t, 1*time.Hour, worker.Interval())
}

// TestSessionCleanupWorkerCustomInterval verifies custom interval is used
func TestSessionCleanupWorkerCustomInterval(t *testing.T) {
	customInterval := 30 * time.Minute

	cfg := SessionCleanupConfig{
		DB:       nil,
		Log:      nil,
		Interval: customInterval,
	}

	worker := NewSessionCleanupWorker(cfg)

	assert.Equal(t, customInterval, worker.Interval())
}

// TestSessionCleanupWorkerImplementsWorker verifies interface implementation
func TestSessionCleanupWorkerImplementsWorker(t *testing.T) {
	worker := NewSessionCleanupWorker(SessionCleanupConfig{})

	// Should implement Worker interface
	var _ Worker = worker
	assert.Equal(t, "session_cleanup", worker.Name())
}

// ============================================================================
// FLOW CLEANUP WORKER TESTS
// ============================================================================

// TestFlowCleanupWorkerCreation verifies worker creation and defaults
func TestFlowCleanupWorkerCreation(t *testing.T) {
	cfg := FlowCleanupConfig{
		DB:  nil,
		Log: nil,
	}

	worker := NewFlowCleanupWorker(cfg)

	assert.NotNil(t, worker)
	assert.Equal(t, "flow_cleanup", worker.Name())
	assert.Equal(t, 30*time.Minute, worker.Interval())
}

// TestFlowCleanupWorkerCustomInterval verifies custom interval is used
func TestFlowCleanupWorkerCustomInterval(t *testing.T) {
	customInterval := 15 * time.Minute

	cfg := FlowCleanupConfig{
		DB:       nil,
		Log:      nil,
		Interval: customInterval,
	}

	worker := NewFlowCleanupWorker(cfg)

	assert.Equal(t, customInterval, worker.Interval())
}

// TestFlowCleanupWorkerImplementsWorker verifies interface implementation
func TestFlowCleanupWorkerImplementsWorker(t *testing.T) {
	worker := NewFlowCleanupWorker(FlowCleanupConfig{})

	// Should implement Worker interface
	var _ Worker = worker
	assert.Equal(t, "flow_cleanup", worker.Name())
}

// ============================================================================
// EVENT PROCESSOR WORKER TESTS
// ============================================================================

// TestEventProcessorWorkerCreation verifies worker creation and defaults
func TestEventProcessorWorkerCreation(t *testing.T) {
	cfg := EventProcessorConfig{
		DB:  nil,
		Log: nil,
	}

	worker := NewEventProcessorWorker(cfg)

	assert.NotNil(t, worker)
	assert.Equal(t, "event_processor", worker.Name())
	assert.Equal(t, 10*time.Second, worker.Interval())
	assert.Equal(t, 100, worker.batchSize)
	assert.Equal(t, 3, worker.maxRetries)
	assert.Equal(t, 1*time.Second, worker.retryDelay)
	assert.Equal(t, "default", worker.subscriber)
}

// TestEventProcessorWorkerCustomConfig verifies custom configuration
func TestEventProcessorWorkerCustomConfig(t *testing.T) {
	cfg := EventProcessorConfig{
		DB:         nil,
		Log:        nil,
		Subscriber: "custom-subscriber",
		Interval:   5 * time.Second,
		BatchSize:  50,
		MaxRetries: 5,
		RetryDelay: 2 * time.Second,
	}

	worker := NewEventProcessorWorker(cfg)

	assert.Equal(t, 5*time.Second, worker.Interval())
	assert.Equal(t, 50, worker.batchSize)
	assert.Equal(t, 5, worker.maxRetries)
	assert.Equal(t, 2*time.Second, worker.retryDelay)
	assert.Equal(t, "custom-subscriber", worker.subscriber)
}

// TestEventProcessorWorkerImplementsWorker verifies interface implementation
func TestEventProcessorWorkerImplementsWorker(t *testing.T) {
	worker := NewEventProcessorWorker(EventProcessorConfig{})

	// Should implement Worker interface
	var _ Worker = worker
	assert.Equal(t, "event_processor", worker.Name())
}

// ============================================================================
// COURIER DISPATCH WORKER TESTS
// ============================================================================

// TestCourierDispatchWorkerCreation verifies worker creation and defaults
func TestCourierDispatchWorkerCreation(t *testing.T) {
	cfg := CourierDispatchConfig{
		DB:  nil,
		Log: nil,
	}

	worker := NewCourierDispatchWorker(cfg)

	assert.NotNil(t, worker)
	assert.Equal(t, "courier_dispatch", worker.Name())
	assert.Equal(t, 30*time.Second, worker.Interval())
	assert.Equal(t, 10, worker.batchSize)
	assert.Equal(t, 3, worker.maxRetries)
}

// TestCourierDispatchWorkerCustomConfig verifies custom configuration
func TestCourierDispatchWorkerCustomConfig(t *testing.T) {
	cfg := CourierDispatchConfig{
		DB:         nil,
		Log:        nil,
		Interval:   15 * time.Second,
		BatchSize:  20,
		MaxRetries: 5,
	}

	worker := NewCourierDispatchWorker(cfg)

	assert.Equal(t, 15*time.Second, worker.Interval())
	assert.Equal(t, 20, worker.batchSize)
	assert.Equal(t, 5, worker.maxRetries)
}

// TestCourierDispatchWorkerImplementsWorker verifies interface implementation
func TestCourierDispatchWorkerImplementsWorker(t *testing.T) {
	worker := NewCourierDispatchWorker(CourierDispatchConfig{})

	// Should implement Worker interface
	var _ Worker = worker
	assert.Equal(t, "courier_dispatch", worker.Name())
}

// ============================================================================
// CONCURRENT WORKER POOL TESTS
// ============================================================================

// TestConcurrentWorkerPoolExecution verifies multiple workers execute concurrently
func TestConcurrentWorkerPoolExecution(t *testing.T) {
	manager := NewManager(ManagerConfig{})
	executionCounts := make([]atomic.Int32, 3)

	for i := 0; i < 3; i++ {
		idx := i
		testWorker := &MockWorker{
			name: fmt.Sprintf("worker-%d", i),
			startFn: func(ctx context.Context) error {
				for j := 0; j < 5; j++ {
					executionCounts[idx].Add(1)
					select {
					case <-ctx.Done():
						return ctx.Err()
					case <-time.After(10 * time.Millisecond):
					}
				}
				return nil
			},
		}
		manager.Register(testWorker)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	manager.Start(ctx)
	time.Sleep(200 * time.Millisecond)
	manager.Stop()

	// All workers should have executed
	for i := 0; i < 3; i++ {
		assert.Greater(t, executionCounts[i].Load(), int32(0), fmt.Sprintf("worker-%d not executed", i))
	}
}

// TestWorkerPoolLoad verifies manager handles many workers
func TestWorkerPoolLoad(t *testing.T) {
	manager := NewManager(ManagerConfig{})
	totalExecutions := atomic.Int32{}

	// Register 20 workers
	for i := 0; i < 20; i++ {
		testWorker := &MockWorker{
			name: fmt.Sprintf("worker-%d", i),
			startFn: func(ctx context.Context) error {
				totalExecutions.Add(1)
				<-ctx.Done()
				return ctx.Err()
			},
		}
		manager.Register(testWorker)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	manager.Start(ctx)
	time.Sleep(100 * time.Millisecond)
	manager.Stop()

	// All workers should have been started
	assert.Equal(t, int32(20), totalExecutions.Load())
}

// TestWorkerPoolDataRaces verifies no data races under concurrent load
func TestWorkerPoolDataRaces(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping race test in short mode")
	}

	manager := NewManager(ManagerConfig{})

	// Register workers
	for i := 0; i < 5; i++ {
		testWorker := &MockWorker{
			name: fmt.Sprintf("worker-%d", i),
			startFn: func(ctx context.Context) error {
				<-ctx.Done()
				return ctx.Err()
			},
		}
		manager.Register(testWorker)
	}

	// Start and stress test with concurrent operations
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	manager.Start(ctx)

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			manager.Register(&MockWorker{name: "extra"})
		}()
	}

	wg.Wait()
	cancel()
	manager.Stop()

	// Should complete without panics
}

// ============================================================================
// ERROR HANDLING & RECOVERY TESTS
// ============================================================================

// TestWorkerErrorRecovery verifies worker continues after errors
func TestWorkerErrorRecovery(t *testing.T) {
	worker := NewBaseWorker("test", nil, nil, 50*time.Millisecond)
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	executionCount := atomic.Int32{}
	fn := func(ctx context.Context) error {
		executionCount.Add(1)
		if executionCount.Load()%2 == 0 {
			return errors.New("alternating error")
		}
		return nil
	}

	go func() {
		_ = worker.RunLoop(ctx, fn)
	}()

	time.Sleep(150 * time.Millisecond)

	// Should have executed multiple times despite alternating errors
	assert.Greater(t, executionCount.Load(), int32(2))
}

// TestManagerWorkerError verifies manager handles worker errors
func TestManagerWorkerError(t *testing.T) {
	manager := NewManager(ManagerConfig{})

	errorWorker := &MockWorker{
		name: "error-worker",
		startFn: func(ctx context.Context) error {
			return errors.New("worker error")
		},
	}

	okWorker := &MockWorker{
		name: "ok-worker",
		startFn: func(ctx context.Context) error {
			<-ctx.Done()
			return ctx.Err()
		},
	}

	manager.Register(errorWorker)
	manager.Register(okWorker)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	// Should not panic even with error worker
	manager.Start(ctx)
	time.Sleep(50 * time.Millisecond)
	manager.Stop()

	// Should complete successfully
}

// TestManagerStopWithoutStart verifies Stop() is safe without Start()
func TestManagerStopWithoutStart(t *testing.T) {
	manager := NewManager(ManagerConfig{})

	worker := &MockWorker{name: "test"}
	manager.Register(worker)

	// Should not panic
	manager.Stop()
}

// TestManagerMultipleStops verifies Stop() is idempotent
func TestManagerMultipleStops(t *testing.T) {
	manager := NewManager(ManagerConfig{})

	worker := &MockWorker{name: "test"}
	manager.Register(worker)

	ctx := context.Background()
	manager.Start(ctx)
	time.Sleep(50 * time.Millisecond)

	// Should not panic when called multiple times
	manager.Stop()
	manager.Stop()
	manager.Stop()
}

// ============================================================================
// EDGE CASES & BOUNDARY TESTS
// ============================================================================

// TestZeroIntervalWorker handles workers with zero interval
func TestZeroIntervalWorker(t *testing.T) {
	// SessionCleanupWorker should default to 1 hour
	worker := NewSessionCleanupWorker(SessionCleanupConfig{Interval: 0})
	assert.Equal(t, 1*time.Hour, worker.Interval())

	// FlowCleanupWorker should default to 30 minutes
	flowWorker := NewFlowCleanupWorker(FlowCleanupConfig{Interval: 0})
	assert.Equal(t, 30*time.Minute, flowWorker.Interval())

	// EventProcessorWorker should default to 10 seconds
	eventWorker := NewEventProcessorWorker(EventProcessorConfig{Interval: 0})
	assert.Equal(t, 10*time.Second, eventWorker.Interval())
}

// TestZeroBatchSizeDefaults handles zero batch sizes
func TestZeroBatchSizeDefaults(t *testing.T) {
	eventWorker := NewEventProcessorWorker(EventProcessorConfig{BatchSize: 0})
	assert.Equal(t, 100, eventWorker.batchSize)

	courierWorker := NewCourierDispatchWorker(CourierDispatchConfig{BatchSize: 0})
	assert.Equal(t, 10, courierWorker.batchSize)
}

// TestZeroRetryDefaults handles zero retry counts
func TestZeroRetryDefaults(t *testing.T) {
	eventWorker := NewEventProcessorWorker(EventProcessorConfig{MaxRetries: 0})
	assert.Equal(t, 3, eventWorker.maxRetries)

	courierWorker := NewCourierDispatchWorker(CourierDispatchConfig{MaxRetries: 0})
	assert.Equal(t, 3, courierWorker.maxRetries)
}

// TestVeryShortInterval verifies workers handle very short intervals
func TestVeryShortInterval(t *testing.T) {
	worker := NewBaseWorker("fast", nil, nil, 1*time.Millisecond)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	executionCount := atomic.Int32{}
	fn := func(ctx context.Context) error {
		executionCount.Add(1)
		return nil
	}

	go func() {
		_ = worker.RunLoop(ctx, fn)
	}()

	time.Sleep(45 * time.Millisecond)

	// Should have executed many times
	assert.Greater(t, executionCount.Load(), int32(10))
}

// TestLongRunningFunction handles slow execution
func TestLongRunningFunction(t *testing.T) {
	worker := NewBaseWorker("slow", nil, nil, 50*time.Millisecond)
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	executionCount := atomic.Int32{}
	fn := func(ctx context.Context) error {
		executionCount.Add(1)
		time.Sleep(100 * time.Millisecond) // Longer than interval
		return nil
	}

	go func() {
		_ = worker.RunLoop(ctx, fn)
	}()

	time.Sleep(250 * time.Millisecond)

	// Should still execute multiple times
	assert.Greater(t, executionCount.Load(), int32(1))
}

// TestManyWorkersStopSequential verifies stopping many workers completes
func TestManyWorkersStopSequential(t *testing.T) {
	manager := NewManager(ManagerConfig{})

	for i := 0; i < 50; i++ {
		testWorker := &MockWorker{
			name: fmt.Sprintf("worker-%d", i),
			startFn: func(ctx context.Context) error {
				<-ctx.Done()
				return ctx.Err()
			},
		}
		manager.Register(testWorker)
	}

	ctx := context.Background()
	manager.Start(ctx)
	time.Sleep(50 * time.Millisecond)

	// Should complete in reasonable time
	start := time.Now()
	manager.Stop()
	elapsed := time.Since(start)

	// Should complete within timeout + buffer
	assert.Less(t, elapsed, 35*time.Second)
}

// ============================================================================
// HELPER TYPES
// ============================================================================

// MockWorker is a test implementation of the Worker interface
type MockWorker struct {
	name    string
	startFn func(ctx context.Context) error
	stopFn  func()
}

func (m *MockWorker) Name() string {
	return m.name
}

func (m *MockWorker) Start(ctx context.Context) error {
	if m.startFn != nil {
		return m.startFn(ctx)
	}
	return nil
}

func (m *MockWorker) Stop() {
	if m.stopFn != nil {
		m.stopFn()
	}
}
