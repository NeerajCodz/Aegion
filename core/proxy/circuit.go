package proxy

import (
	"errors"
	"sync"
	"time"
)

var (
	ErrCircuitBreakerOpen     = errors.New("circuit breaker is open")
	ErrCircuitBreakerHalfOpen = errors.New("circuit breaker is half-open")
)

// State represents the circuit breaker state.
type State int

const (
	// StateClosed means requests are allowed through
	StateClosed State = iota
	// StateOpen means requests are rejected
	StateOpen
	// StateHalfOpen means limited requests are allowed to test if service recovered
	StateHalfOpen
)

// String returns the string representation of the state.
func (s State) String() string {
	switch s {
	case StateClosed:
		return "closed"
	case StateOpen:
		return "open"
	case StateHalfOpen:
		return "half-open"
	default:
		return "unknown"
	}
}

// CircuitBreaker implements the circuit breaker pattern.
type CircuitBreaker struct {
	config CircuitBreakerConfig
	mutex  sync.RWMutex

	state         State
	failures      int
	successes     int
	lastFailure   time.Time
	lastSuccess   time.Time
	lastStateTime time.Time

	// Metrics
	totalRequests   int64
	totalFailures   int64
	totalSuccesses  int64
	rejectedCount   int64
}

// NewCircuitBreaker creates a new circuit breaker with the given configuration.
func NewCircuitBreaker(config CircuitBreakerConfig) *CircuitBreaker {
	now := time.Now()
	return &CircuitBreaker{
		config:        config,
		state:         StateClosed,
		lastStateTime: now,
	}
}

// Allow checks if a request should be allowed through.
// Returns true if the request can proceed, false if it should be rejected.
func (cb *CircuitBreaker) Allow() bool {
	cb.mutex.Lock()
	defer cb.mutex.Unlock()

	cb.totalRequests++

	switch cb.state {
	case StateClosed:
		return true

	case StateOpen:
		// Check if we should transition to half-open
		if time.Since(cb.lastStateTime) >= cb.config.Timeout {
			cb.setState(StateHalfOpen)
			return true
		}
		cb.rejectedCount++
		return false

	case StateHalfOpen:
		// Allow a limited number of requests to test service recovery
		return true

	default:
		cb.rejectedCount++
		return false
	}
}

// RecordSuccess records a successful request.
func (cb *CircuitBreaker) RecordSuccess() {
	cb.mutex.Lock()
	defer cb.mutex.Unlock()

	cb.totalSuccesses++
	cb.lastSuccess = time.Now()

	switch cb.state {
	case StateClosed:
		cb.failures = 0 // Reset failure count

	case StateHalfOpen:
		cb.successes++
		cb.failures = 0 // Reset failure count

		// If we have enough successes, close the circuit
		if cb.successes >= cb.config.SuccessThreshold {
			cb.setState(StateClosed)
		}

	case StateOpen:
		// This shouldn't happen as requests should be rejected
		// But if it does, we treat it as a half-open success
		cb.successes = 1
		cb.failures = 0
		cb.setState(StateHalfOpen)
	}
}

// RecordFailure records a failed request.
func (cb *CircuitBreaker) RecordFailure() {
	cb.mutex.Lock()
	defer cb.mutex.Unlock()

	cb.totalFailures++
	cb.failures++
	cb.successes = 0 // Reset success count
	cb.lastFailure = time.Now()

	// Check if we should open the circuit
	if cb.failures >= cb.config.FailureThreshold {
		if cb.state == StateClosed || cb.state == StateHalfOpen {
			cb.setState(StateOpen)
		}
	}
}

// GetState returns the current circuit breaker state.
func (cb *CircuitBreaker) GetState() State {
	cb.mutex.RLock()
	defer cb.mutex.RUnlock()
	return cb.state
}

// GetMetrics returns circuit breaker metrics.
func (cb *CircuitBreaker) GetMetrics() CircuitBreakerMetrics {
	cb.mutex.RLock()
	defer cb.mutex.RUnlock()

	return CircuitBreakerMetrics{
		State:           cb.state,
		TotalRequests:   cb.totalRequests,
		TotalSuccesses:  cb.totalSuccesses,
		TotalFailures:   cb.totalFailures,
		RejectedCount:   cb.rejectedCount,
		Failures:        cb.failures,
		Successes:       cb.successes,
		LastFailure:     cb.lastFailure,
		LastSuccess:     cb.lastSuccess,
		LastStateChange: cb.lastStateTime,
	}
}

// Reset resets the circuit breaker to closed state.
func (cb *CircuitBreaker) Reset() {
	cb.mutex.Lock()
	defer cb.mutex.Unlock()

	cb.state = StateClosed
	cb.failures = 0
	cb.successes = 0
	cb.lastStateTime = time.Now()
}

// setState changes the circuit breaker state and updates the timestamp.
func (cb *CircuitBreaker) setState(newState State) {
	if cb.state != newState {
		cb.state = newState
		cb.lastStateTime = time.Now()
		
		// Reset counters when state changes
		if newState == StateClosed {
			cb.failures = 0
			cb.successes = 0
		} else if newState == StateHalfOpen {
			cb.successes = 0
		}
	}
}

// CircuitBreakerMetrics holds metrics for monitoring circuit breaker health.
type CircuitBreakerMetrics struct {
	State           State     `json:"state"`
	TotalRequests   int64     `json:"total_requests"`
	TotalSuccesses  int64     `json:"total_successes"`
	TotalFailures   int64     `json:"total_failures"`
	RejectedCount   int64     `json:"rejected_count"`
	Failures        int       `json:"current_failures"`
	Successes       int       `json:"current_successes"`
	LastFailure     time.Time `json:"last_failure"`
	LastSuccess     time.Time `json:"last_success"`
	LastStateChange time.Time `json:"last_state_change"`
}

// SuccessRate returns the overall success rate.
func (m CircuitBreakerMetrics) SuccessRate() float64 {
	if m.TotalRequests == 0 {
		return 0
	}
	return float64(m.TotalSuccesses) / float64(m.TotalRequests)
}

// FailureRate returns the overall failure rate.
func (m CircuitBreakerMetrics) FailureRate() float64 {
	if m.TotalRequests == 0 {
		return 0
	}
	return float64(m.TotalFailures) / float64(m.TotalRequests)
}