package proxy

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestCircuitBreaker_InitialState(t *testing.T) {
	config := CircuitBreakerConfig{
		FailureThreshold: 5,
		Timeout:          time.Minute,
		SuccessThreshold: 3,
	}

	cb := NewCircuitBreaker(config)

	assert.Equal(t, StateClosed, cb.GetState())
	
	metrics := cb.GetMetrics()
	assert.Equal(t, StateClosed, metrics.State)
	assert.Equal(t, int64(0), metrics.TotalRequests)
	assert.Equal(t, int64(0), metrics.TotalSuccesses)
	assert.Equal(t, int64(0), metrics.TotalFailures)
	assert.Equal(t, 0, metrics.Failures)
	assert.Equal(t, 0, metrics.Successes)
}

func TestCircuitBreaker_ClosedState(t *testing.T) {
	config := CircuitBreakerConfig{
		FailureThreshold: 3,
		Timeout:          time.Minute,
		SuccessThreshold: 2,
	}

	cb := NewCircuitBreaker(config)

	// In closed state, all requests should be allowed
	for i := 0; i < 10; i++ {
		assert.True(t, cb.Allow(), "request %d should be allowed in closed state", i+1)
	}

	// Record some successes
	for i := 0; i < 5; i++ {
		cb.RecordSuccess()
	}

	// Should still be closed
	assert.Equal(t, StateClosed, cb.GetState())
	
	metrics := cb.GetMetrics()
	assert.Equal(t, int64(10), metrics.TotalRequests)
	assert.Equal(t, int64(5), metrics.TotalSuccesses)
	assert.Equal(t, 0, metrics.Failures) // Reset after successes
}

func TestCircuitBreaker_OpenOnFailures(t *testing.T) {
	config := CircuitBreakerConfig{
		FailureThreshold: 3,
		Timeout:          time.Minute,
		SuccessThreshold: 2,
	}

	cb := NewCircuitBreaker(config)

	// Record failures until threshold
	for i := 0; i < config.FailureThreshold; i++ {
		assert.True(t, cb.Allow(), "request should be allowed before opening")
		cb.RecordFailure()
		
		if i < config.FailureThreshold-1 {
			assert.Equal(t, StateClosed, cb.GetState(), "should still be closed")
		}
	}

	// Circuit should now be open
	assert.Equal(t, StateOpen, cb.GetState())

	// Subsequent requests should be rejected
	for i := 0; i < 5; i++ {
		assert.False(t, cb.Allow(), "requests should be rejected in open state")
	}

	metrics := cb.GetMetrics()
	assert.Equal(t, StateOpen, metrics.State)
	assert.Equal(t, int64(8), metrics.TotalRequests) // 3 allowed + 5 rejected
	assert.Equal(t, int64(3), metrics.TotalFailures)
	assert.Equal(t, int64(5), metrics.RejectedCount)
}

func TestCircuitBreaker_HalfOpenTransition(t *testing.T) {
	config := CircuitBreakerConfig{
		FailureThreshold: 3,
		Timeout:          100 * time.Millisecond,
		SuccessThreshold: 2,
	}

	cb := NewCircuitBreaker(config)

	// Open the circuit
	for i := 0; i < config.FailureThreshold; i++ {
		cb.Allow()
		cb.RecordFailure()
	}
	assert.Equal(t, StateOpen, cb.GetState())

	// Wait for timeout
	time.Sleep(config.Timeout + 10*time.Millisecond)

	// Next request should be allowed (half-open state)
	assert.True(t, cb.Allow())
	assert.Equal(t, StateHalfOpen, cb.GetState())

	// In half-open state, requests should still be allowed for testing
	assert.True(t, cb.Allow())
}

func TestCircuitBreaker_HalfOpenToClosedOnSuccess(t *testing.T) {
	config := CircuitBreakerConfig{
		FailureThreshold: 2,
		Timeout:          50 * time.Millisecond,
		SuccessThreshold: 3,
	}

	cb := NewCircuitBreaker(config)

	// Open the circuit
	for i := 0; i < config.FailureThreshold; i++ {
		cb.Allow()
		cb.RecordFailure()
	}
	assert.Equal(t, StateOpen, cb.GetState())

	// Wait for timeout and transition to half-open
	time.Sleep(config.Timeout + 10*time.Millisecond)
	cb.Allow()
	assert.Equal(t, StateHalfOpen, cb.GetState())

	// Record successes until threshold
	for i := 0; i < config.SuccessThreshold; i++ {
		cb.RecordSuccess()
		
		if i < config.SuccessThreshold-1 {
			assert.Equal(t, StateHalfOpen, cb.GetState(), "should still be half-open")
		}
	}

	// Should be closed now
	assert.Equal(t, StateClosed, cb.GetState())

	// Should allow requests normally
	assert.True(t, cb.Allow())
}

func TestCircuitBreaker_HalfOpenToOpenOnFailure(t *testing.T) {
	config := CircuitBreakerConfig{
		FailureThreshold: 2,
		Timeout:          50 * time.Millisecond,
		SuccessThreshold: 3,
	}

	cb := NewCircuitBreaker(config)

	// Open the circuit
	for i := 0; i < config.FailureThreshold; i++ {
		cb.Allow()
		cb.RecordFailure()
	}
	assert.Equal(t, StateOpen, cb.GetState())

	// Wait for timeout and transition to half-open
	time.Sleep(config.Timeout + 10*time.Millisecond)
	cb.Allow()
	assert.Equal(t, StateHalfOpen, cb.GetState())

	// Record a failure - should open the circuit again
	cb.RecordFailure()
	assert.Equal(t, StateOpen, cb.GetState())

	// Should reject subsequent requests
	assert.False(t, cb.Allow())
}

func TestCircuitBreaker_Reset(t *testing.T) {
	config := CircuitBreakerConfig{
		FailureThreshold: 2,
		Timeout:          time.Minute,
		SuccessThreshold: 2,
	}

	cb := NewCircuitBreaker(config)

	// Open the circuit
	for i := 0; i < config.FailureThreshold; i++ {
		cb.Allow()
		cb.RecordFailure()
	}
	assert.Equal(t, StateOpen, cb.GetState())

	// Reset circuit breaker
	cb.Reset()

	// Should be closed and allow requests
	assert.Equal(t, StateClosed, cb.GetState())
	assert.True(t, cb.Allow())
	
	metrics := cb.GetMetrics()
	assert.Equal(t, 0, metrics.Failures)
	assert.Equal(t, 0, metrics.Successes)
}

func TestCircuitBreaker_Metrics(t *testing.T) {
	config := CircuitBreakerConfig{
		FailureThreshold: 5,
		Timeout:          time.Minute,
		SuccessThreshold: 3,
	}

	cb := NewCircuitBreaker(config)

	// Make some requests and record results
	for i := 0; i < 10; i++ {
		assert.True(t, cb.Allow())
		
		if i%2 == 0 {
			cb.RecordSuccess()
		} else {
			cb.RecordFailure()
		}
	}

	metrics := cb.GetMetrics()
	assert.Equal(t, StateClosed, metrics.State) // Still closed (5 failures but threshold is 5)
	assert.Equal(t, int64(10), metrics.TotalRequests)
	assert.Equal(t, int64(5), metrics.TotalSuccesses)
	assert.Equal(t, int64(5), metrics.TotalFailures)
	assert.Equal(t, float64(0.5), metrics.SuccessRate())
	assert.Equal(t, float64(0.5), metrics.FailureRate())
	
	// The current failures should be reset after successes
	assert.Equal(t, 1, metrics.Failures) // Last operation was a failure
}

func TestCircuitBreakerMetrics_Rates(t *testing.T) {
	tests := []struct {
		name               string
		totalRequests      int64
		totalSuccesses     int64
		expectedSuccessRate float64
		expectedFailureRate float64
	}{
		{
			name:               "no requests",
			totalRequests:      0,
			totalSuccesses:     0,
			expectedSuccessRate: 0,
			expectedFailureRate: 0,
		},
		{
			name:               "all successes",
			totalRequests:      10,
			totalSuccesses:     10,
			expectedSuccessRate: 1.0,
			expectedFailureRate: 0,
		},
		{
			name:               "all failures",
			totalRequests:      10,
			totalSuccesses:     0,
			expectedSuccessRate: 0,
			expectedFailureRate: 1.0,
		},
		{
			name:               "mixed results",
			totalRequests:      10,
			totalSuccesses:     7,
			expectedSuccessRate: 0.7,
			expectedFailureRate: 0.3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			metrics := CircuitBreakerMetrics{
				TotalRequests:  tt.totalRequests,
				TotalSuccesses: tt.totalSuccesses,
				TotalFailures:  tt.totalRequests - tt.totalSuccesses,
			}

			assert.Equal(t, tt.expectedSuccessRate, metrics.SuccessRate())
			assert.Equal(t, tt.expectedFailureRate, metrics.FailureRate())
		})
	}
}

func TestCircuitBreaker_ConcurrentAccess(t *testing.T) {
	config := CircuitBreakerConfig{
		FailureThreshold: 10,
		Timeout:          100 * time.Millisecond,
		SuccessThreshold: 5,
	}

	cb := NewCircuitBreaker(config)

	numGoroutines := 100
	numOperationsPerGoroutine := 100

	var wg sync.WaitGroup
	var mu sync.Mutex
	allowedCount := 0
	deniedCount := 0

	// Start multiple goroutines making requests
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(goroutineID int) {
			defer wg.Done()

			for j := 0; j < numOperationsPerGoroutine; j++ {
				allowed := cb.Allow()

				mu.Lock()
				if allowed {
					allowedCount++
					// Simulate some successes and failures
					if (goroutineID+j)%3 == 0 {
						cb.RecordFailure()
					} else {
						cb.RecordSuccess()
					}
				} else {
					deniedCount++
				}
				mu.Unlock()

				// Small delay to create more realistic timing
				time.Sleep(time.Microsecond * 10)
			}
		}(i)
	}

	wg.Wait()

	totalOperations := numGoroutines * numOperationsPerGoroutine
	t.Logf("Total operations: %d, Allowed: %d, Denied: %d", 
		totalOperations, allowedCount, deniedCount)

	// Verify that all operations were accounted for
	assert.Equal(t, totalOperations, allowedCount+deniedCount)

	// Get final metrics
	metrics := cb.GetMetrics()
	t.Logf("Final state: %s, Success rate: %.2f", 
		metrics.State.String(), metrics.SuccessRate())

	// Should have some operations (exact number depends on timing)
	assert.Greater(t, allowedCount, 0)
}

func TestCircuitBreaker_StateTransitions(t *testing.T) {
	config := CircuitBreakerConfig{
		FailureThreshold: 3,
		Timeout:          50 * time.Millisecond,
		SuccessThreshold: 2,
	}

	cb := NewCircuitBreaker(config)

	// Test complete state transition cycle
	
	// 1. Start in Closed state
	assert.Equal(t, StateClosed, cb.GetState())
	assert.True(t, cb.Allow())

	// 2. Cause failures to open circuit
	for i := 0; i < config.FailureThreshold; i++ {
		cb.Allow()
		cb.RecordFailure()
	}
	assert.Equal(t, StateOpen, cb.GetState())
	assert.False(t, cb.Allow())

	// 3. Wait for timeout, transition to half-open
	time.Sleep(config.Timeout + 10*time.Millisecond)
	assert.True(t, cb.Allow()) // This call transitions to half-open
	assert.Equal(t, StateHalfOpen, cb.GetState())

	// 4. Record successes to close circuit
	for i := 0; i < config.SuccessThreshold; i++ {
		cb.RecordSuccess()
	}
	assert.Equal(t, StateClosed, cb.GetState())
	assert.True(t, cb.Allow())

	// 5. Verify circuit works normally again
	cb.RecordSuccess()
	assert.Equal(t, StateClosed, cb.GetState())
}

func TestCircuitBreaker_EdgeCases(t *testing.T) {
	config := CircuitBreakerConfig{
		FailureThreshold: 1,
		Timeout:          10 * time.Millisecond,
		SuccessThreshold: 1,
	}

	cb := NewCircuitBreaker(config)

	// Single failure should open circuit
	cb.Allow()
	cb.RecordFailure()
	assert.Equal(t, StateOpen, cb.GetState())

	// Should reject requests
	assert.False(t, cb.Allow())

	// Wait for timeout
	time.Sleep(config.Timeout + 5*time.Millisecond)

	// Single success should close circuit
	assert.True(t, cb.Allow())
	assert.Equal(t, StateHalfOpen, cb.GetState())
	
	cb.RecordSuccess()
	assert.Equal(t, StateClosed, cb.GetState())
}

func TestState_String(t *testing.T) {
	tests := []struct {
		state    State
		expected string
	}{
		{StateClosed, "closed"},
		{StateOpen, "open"},
		{StateHalfOpen, "half-open"},
		{State(999), "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.state.String())
		})
	}
}

func BenchmarkCircuitBreaker_Allow(b *testing.B) {
	config := CircuitBreakerConfig{
		FailureThreshold: 100,
		Timeout:          time.Minute,
		SuccessThreshold: 50,
	}

	cb := NewCircuitBreaker(config)

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			cb.Allow()
		}
	})
}

func BenchmarkCircuitBreaker_RecordSuccess(b *testing.B) {
	config := CircuitBreakerConfig{
		FailureThreshold: 100,
		Timeout:          time.Minute,
		SuccessThreshold: 50,
	}

	cb := NewCircuitBreaker(config)

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			cb.RecordSuccess()
		}
	})
}

func BenchmarkCircuitBreaker_RecordFailure(b *testing.B) {
	config := CircuitBreakerConfig{
		FailureThreshold: 100,
		Timeout:          time.Minute,
		SuccessThreshold: 50,
	}

	cb := NewCircuitBreaker(config)

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			cb.RecordFailure()
		}
	})
}