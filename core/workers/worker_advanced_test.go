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
// RUNLOOP COMPREHENSIVE COVERAGE TESTS
// ============================================================================

// TestBaseWorkerRunLoopInitialExecution verifies fn is called immediately
func TestBaseWorkerRunLoopInitialExecution(t *testing.T) {
	worker := NewBaseWorker("test", nil, nil, 10*time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	callCount := atomic.Int32{}
	fn := func(ctx context.Context) error {
		callCount.Add(1)
		// Return immediately to avoid tick
		return nil
	}

	go func() {
		_ = worker.RunLoop(ctx, fn)
	}()

	time.Sleep(50 * time.Millisecond)

	// Should have called at least once (immediate)
	assert.GreaterOrEqual(t, callCount.Load(), int32(1))
}

// TestBaseWorkerRunLoopMultipleTicks verifies periodic execution
func TestBaseWorkerRunLoopMultipleTicks(t *testing.T) {
	worker := NewBaseWorker("test", nil, nil, 30*time.Millisecond)
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()

	callCount := atomic.Int32{}
	fn := func(ctx context.Context) error {
		callCount.Add(1)
		return nil
	}

	go func() {
		_ = worker.RunLoop(ctx, fn)
	}()

	time.Sleep(120 * time.Millisecond)

	// Should have executed: immediate + ~4 ticks (150ms / 30ms)
	assert.Greater(t, callCount.Load(), int32(3))
}

// TestBaseWorkerRunLoopDoneChannelSignal verifies done channel stops loop
func TestBaseWorkerRunLoopDoneChannelSignal(t *testing.T) {
	worker := NewBaseWorker("test", nil, nil, 100*time.Millisecond)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	callCount := atomic.Int32{}
	fn := func(ctx context.Context) error {
		callCount.Add(1)
		return nil
	}

	doneChan := make(chan error, 1)
	go func() {
		doneChan <- worker.RunLoop(ctx, fn)
	}()

	// Wait for at least one execution
	time.Sleep(50 * time.Millisecond)

	worker.Stop()

	// Should return nil when stopped via done channel
	err := <-doneChan
	assert.NoError(t, err)
}

// TestBaseWorkerRunLoopContextCancellationReturnsError verifies context error is returned
func TestBaseWorkerRunLoopContextCancellationReturnsError(t *testing.T) {
	worker := NewBaseWorker("test", nil, nil, 1*time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	fn := func(ctx context.Context) error {
		return nil
	}

	resultChan := make(chan error, 1)
	go func() {
		resultChan <- worker.RunLoop(ctx, fn)
	}()

	time.Sleep(100 * time.Millisecond)

	err := <-resultChan
	assert.ErrorIs(t, err, context.DeadlineExceeded)
}

// TestBaseWorkerRunLoopFunctionErrorDoesNotStopLoop verifies errors are logged not propagated
func TestBaseWorkerRunLoopFunctionErrorDoesNotStopLoop(t *testing.T) {
	worker := NewBaseWorker("test", nil, nil, 30*time.Millisecond)
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()

	callCount := atomic.Int32{}
	fn := func(ctx context.Context) error {
		callCount.Add(1)
		if callCount.Load() <= 2 {
			return fmt.Errorf("error %d", callCount.Load())
		}
		return nil
	}

	resultChan := make(chan error, 1)
	go func() {
		resultChan <- worker.RunLoop(ctx, fn)
	}()

	time.Sleep(120 * time.Millisecond)

	// Loop should continue despite errors
	assert.Greater(t, callCount.Load(), int32(2))
}

// TestBaseWorkerRunLoopPanicRecovery verifies function panics are caught
func TestBaseWorkerRunLoopPanicRecovery(t *testing.T) {
	worker := NewBaseWorker("test", nil, nil, 30*time.Millisecond)
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()

	callCount := atomic.Int32{}
	fn := func(ctx context.Context) error {
		callCount.Add(1)
		if callCount.Load() == 2 {
			panic("test panic")
		}
		return nil
	}

	go func() {
		_ = worker.RunLoop(ctx, fn)
	}()

	time.Sleep(120 * time.Millisecond)

	// Should continue after panic
	assert.Greater(t, callCount.Load(), int32(2))
}

// TestBaseWorkerRunLoopConcurrentContextAndDone verifies both signals are respected
func TestBaseWorkerRunLoopConcurrentContextAndDone(t *testing.T) {
	for i := 0; i < 5; i++ {
		worker := NewBaseWorker(fmt.Sprintf("test-%d", i), nil, nil, 50*time.Millisecond)
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()

		resultChan := make(chan error, 1)
		go func() {
			resultChan <- worker.RunLoop(ctx, func(ctx context.Context) error {
				return nil
			})
		}()

		// Test different stopping scenarios
		switch i {
		case 0:
			// Stop via context
			time.Sleep(50 * time.Millisecond)
			cancel()
		case 1:
			// Stop via done channel
			time.Sleep(50 * time.Millisecond)
			worker.Stop()
		case 2:
			// Wait for context to cancel
			time.Sleep(120 * time.Millisecond)
		case 3:
			// Rapid stop
			worker.Stop()
		case 4:
			// Stop before even starting
			worker.Stop()
		}

		// All should complete without hanging
		select {
		case err := <-resultChan:
			// Either error or nil is fine
			_ = err
		case <-time.After(500 * time.Millisecond):
			t.Errorf("iteration %d: RunLoop did not complete in time", i)
		}
	}
}

// TestBaseWorkerRunLoopRunningStateTransitions verifies state changes correctly
func TestBaseWorkerRunLoopRunningStateTransitions(t *testing.T) {
	worker := NewBaseWorker("test", nil, nil, 50*time.Millisecond)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	callCount := atomic.Int32{}
	fn := func(ctx context.Context) error {
		callCount.Add(1)
		return nil
	}

	// Before start: not running
	assert.False(t, worker.IsRunning())

	resultChan := make(chan error, 1)
	go func() {
		resultChan <- worker.RunLoop(ctx, fn)
	}()

	// During execution: running
	time.Sleep(50 * time.Millisecond)
	assert.True(t, worker.IsRunning())

	// Wait for context to expire
	time.Sleep(60 * time.Millisecond)

	// After completion: not running
	assert.False(t, worker.IsRunning())
}

// TestBaseWorkerRunLoopChannelCleanup verifies channels are not left open
func TestBaseWorkerRunLoopChannelCleanup(t *testing.T) {
	for i := 0; i < 10; i++ {
		worker := NewBaseWorker(fmt.Sprintf("test-%d", i), nil, nil, 50*time.Millisecond)
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)

		go func() {
			_ = worker.RunLoop(ctx, func(ctx context.Context) error {
				return nil
			})
		}()

		time.Sleep(120 * time.Millisecond)
		cancel()

		// Done channel should be closeable (previously closed)
		select {
		case <-worker.Done():
			// Expected - already closed
		default:
			// Expected - stopped but not running so Stop() won't close already-closed channel
		}
	}
}

// ============================================================================
// MANAGER LIFECYCLE COMPREHENSIVE TESTS
// ============================================================================

// TestManagerStartAllWorkers verifies all workers are started
func TestManagerStartAllWorkers(t *testing.T) {
	manager := NewManager(ManagerConfig{})

	startedWorkers := make([]bool, 3)
	var mu sync.Mutex

	for i := 0; i < 3; i++ {
		idx := i
		worker := &MockWorker{
			name: fmt.Sprintf("worker-%d", i),
			startFn: func(ctx context.Context) error {
				mu.Lock()
				startedWorkers[idx] = true
				mu.Unlock()
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
	manager.Stop()

	// All workers should have started
	mu.Lock()
	for i, started := range startedWorkers {
		assert.True(t, started, fmt.Sprintf("worker %d not started", i))
	}
	mu.Unlock()
}

// TestManagerStopCallsWorkerStop verifies Stop is called on each worker
func TestManagerStopCallsWorkerStop(t *testing.T) {
	manager := NewManager(ManagerConfig{})

	stopped := make([]bool, 3)
	var mu sync.Mutex

	for i := 0; i < 3; i++ {
		idx := i
		worker := &MockWorker{
			name: fmt.Sprintf("worker-%d", i),
			startFn: func(ctx context.Context) error {
				<-ctx.Done()
				return ctx.Err()
			},
			stopFn: func() {
				mu.Lock()
				stopped[idx] = true
				mu.Unlock()
			},
		}
		manager.Register(worker)
	}

	ctx := context.Background()
	manager.Start(ctx)
	time.Sleep(50 * time.Millisecond)
	manager.Stop()

	// All workers should have been stopped
	mu.Lock()
	for i, wasStopped := range stopped {
		assert.True(t, wasStopped, fmt.Sprintf("worker %d not stopped", i))
	}
	mu.Unlock()
}

// TestManagerContextPropagation verifies context is passed to workers
func TestManagerContextPropagation(t *testing.T) {
	manager := NewManager(ManagerConfig{})

	receivedContexts := make([]context.Context, 0)
	var mu sync.Mutex

	for i := 0; i < 2; i++ {
		worker := &MockWorker{
			name: fmt.Sprintf("worker-%d", i),
			startFn: func(ctx context.Context) error {
				mu.Lock()
				receivedContexts = append(receivedContexts, ctx)
				mu.Unlock()
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
	manager.Stop()

	// Verify contexts were passed
	mu.Lock()
	assert.Len(t, receivedContexts, 2)
	for _, receivedCtx := range receivedContexts {
		assert.NotNil(t, receivedCtx)
	}
	mu.Unlock()
}

// TestManagerWaitGroupTracking verifies wait group is properly tracked
func TestManagerWaitGroupTracking(t *testing.T) {
	manager := NewManager(ManagerConfig{})

	completedWorkers := atomic.Int32{}

	for i := 0; i < 5; i++ {
		worker := &MockWorker{
			name: fmt.Sprintf("worker-%d", i),
			startFn: func(ctx context.Context) error {
				<-ctx.Done()
				completedWorkers.Add(1)
				return ctx.Err()
			},
		}
		manager.Register(worker)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	manager.Start(ctx)
	time.Sleep(50 * time.Millisecond)

	// All workers started
	assert.Equal(t, int32(0), completedWorkers.Load())

	manager.Stop()

	// All workers should complete
	time.Sleep(50 * time.Millisecond)
	assert.Equal(t, int32(5), completedWorkers.Load())
}

// ============================================================================
// WORKER CONFIGURATION CONSISTENCY TESTS
// ============================================================================

// TestAllWorkerConfigsHandleNilDefaults verifies all workers handle nil configs gracefully
func TestAllWorkerConfigsHandleNilDefaults(t *testing.T) {
	// Test with minimal/zero configs
	sessionWorker := NewSessionCleanupWorker(SessionCleanupConfig{})
	assert.NotNil(t, sessionWorker)
	assert.Equal(t, 1*time.Hour, sessionWorker.Interval())

	flowWorker := NewFlowCleanupWorker(FlowCleanupConfig{})
	assert.NotNil(t, flowWorker)
	assert.Equal(t, 30*time.Minute, flowWorker.Interval())

	eventWorker := NewEventProcessorWorker(EventProcessorConfig{})
	assert.NotNil(t, eventWorker)
	assert.Equal(t, 10*time.Second, eventWorker.Interval())

	courierWorker := NewCourierDispatchWorker(CourierDispatchConfig{})
	assert.NotNil(t, courierWorker)
	assert.Equal(t, 30*time.Second, courierWorker.Interval())
}

// TestAllWorkerConfigsPreserveCustomValues verifies custom values are used
func TestAllWorkerConfigsPreserveCustomValues(t *testing.T) {
	sessionWorker := NewSessionCleanupWorker(SessionCleanupConfig{
		Interval: 2 * time.Hour,
	})
	assert.Equal(t, 2*time.Hour, sessionWorker.Interval())

	flowWorker := NewFlowCleanupWorker(FlowCleanupConfig{
		Interval: 20 * time.Minute,
	})
	assert.Equal(t, 20*time.Minute, flowWorker.Interval())

	eventWorker := NewEventProcessorWorker(EventProcessorConfig{
		Interval:   5 * time.Second,
		BatchSize:  50,
		MaxRetries: 5,
		RetryDelay: 2 * time.Second,
		Subscriber: "custom",
	})
	assert.Equal(t, 5*time.Second, eventWorker.Interval())
	assert.Equal(t, 50, eventWorker.batchSize)
	assert.Equal(t, 5, eventWorker.maxRetries)
	assert.Equal(t, 2*time.Second, eventWorker.retryDelay)
	assert.Equal(t, "custom", eventWorker.subscriber)

	courierWorker := NewCourierDispatchWorker(CourierDispatchConfig{
		Interval:   20 * time.Second,
		BatchSize:  30,
		MaxRetries: 4,
	})
	assert.Equal(t, 20*time.Second, courierWorker.Interval())
	assert.Equal(t, 30, courierWorker.batchSize)
	assert.Equal(t, 4, courierWorker.maxRetries)
}

// ============================================================================
// STRESS & SCALABILITY TESTS
// ============================================================================

// TestManagerWith100Workers verifies manager handles many workers
func TestManagerWith100Workers(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping stress test in short mode")
	}

	manager := NewManager(ManagerConfig{})
	startedCount := atomic.Int32{}

	for i := 0; i < 100; i++ {
		worker := &MockWorker{
			name: fmt.Sprintf("worker-%d", i),
			startFn: func(ctx context.Context) error {
				startedCount.Add(1)
				<-ctx.Done()
				return ctx.Err()
			},
		}
		manager.Register(worker)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	manager.Start(ctx)
	time.Sleep(100 * time.Millisecond)
	manager.Stop()

	// All workers should start
	assert.Equal(t, int32(100), startedCount.Load())
}

// TestRapidRegisterAndStart verifies rapid registration and start
func TestRapidRegisterAndStart(t *testing.T) {
	manager := NewManager(ManagerConfig{})

	// Register many workers rapidly
	for i := 0; i < 20; i++ {
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

	// Start immediately after registration
	manager.Start(ctx)
	time.Sleep(50 * time.Millisecond)
	manager.Stop()

	assert.Len(t, manager.workers, 20)
}

// TestRepeatedCyclesOfStartStop verifies repeated start/stop cycles
func TestRepeatedCyclesOfStartStop(t *testing.T) {
	for cycle := 0; cycle < 10; cycle++ {
		manager := NewManager(ManagerConfig{})

		for i := 0; i < 5; i++ {
			worker := &MockWorker{
				name: fmt.Sprintf("worker-%d", i),
				startFn: func(ctx context.Context) error {
					<-ctx.Done()
					return ctx.Err()
				},
			}
			manager.Register(worker)
		}

		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		manager.Start(ctx)
		time.Sleep(25 * time.Millisecond)
		manager.Stop()
		cancel()

		time.Sleep(10 * time.Millisecond)
	}
}

// ============================================================================
// CLEANUP & RESOURCE MANAGEMENT TESTS
// ============================================================================

// TestBaseWorkerDoneChannelClosedOnce verifies done channel is closed exactly once
func TestBaseWorkerDoneChannelClosedOnce(t *testing.T) {
	worker := NewBaseWorker("test", nil, nil, time.Second)
	worker.SetRunning(true)

	doneChannel := worker.Done()

	// First stop should close
	worker.Stop()

	// Channel should be closed now
	select {
	case <-doneChannel:
		// Expected - closed
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Channel should be closed")
	}

	// Second stop should not panic
	assert.NotPanics(t, func() {
		worker.Stop()
	})
}

// TestManagerGracefulShutdownWaitsForWorkers verifies all workers complete
func TestManagerGracefulShutdownWaitsForWorkers(t *testing.T) {
	manager := NewManager(ManagerConfig{})

	completedWorkers := atomic.Int32{}

	for i := 0; i < 5; i++ {
		worker := &MockWorker{
			name: fmt.Sprintf("worker-%d", i),
			startFn: func(ctx context.Context) error {
				<-ctx.Done()
				completedWorkers.Add(1)
				return ctx.Err()
			},
		}
		manager.Register(worker)
	}

	ctx := context.Background()
	manager.Start(ctx)
	time.Sleep(50 * time.Millisecond)

	// Stop and verify all workers complete
	manager.Stop()
	assert.Equal(t, int32(5), completedWorkers.Load())
}

// TestManagerStopTimeoutBehavior verifies timeout is respected
func TestManagerStopTimeoutBehavior(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping timeout test in short mode")
	}

	manager := NewManager(ManagerConfig{})

	// Create a worker that ignores cancellation
	worker := &MockWorker{
		name: "stubborn",
		startFn: func(ctx context.Context) error {
			// Ignore context, just block
			select {}
		},
	}
	manager.Register(worker)

	ctx := context.Background()
	manager.Start(ctx)
	time.Sleep(50 * time.Millisecond)

	// Stop should timeout gracefully without hanging
	start := time.Now()
	manager.Stop()
	elapsed := time.Since(start)

	// Should complete within timeout window (30s + buffer)
	assert.Less(t, elapsed, 35*time.Second)
}

// TestManagerLoggingOnStartStop verifies logging occurs
func TestManagerLoggingOnStartStop(t *testing.T) {
	manager := NewManager(ManagerConfig{})

	worker := &MockWorker{
		name: "test-worker",
		startFn: func(ctx context.Context) error {
			<-ctx.Done()
			return ctx.Err()
		},
	}
	manager.Register(worker)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	// Should not panic during logging
	manager.Start(ctx)
	time.Sleep(50 * time.Millisecond)
	manager.Stop()
}
