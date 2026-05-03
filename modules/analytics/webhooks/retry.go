package webhooks

import (
	"math"
	"math/rand/v2"
	"time"
)

// RetryConfig holds retry configuration.
type RetryConfig struct {
	MaxRetries              int // Maximum number of retries
	BackoffBaseMs           int // Base backoff in milliseconds (1000 = 1s)
	TimeoutSeconds          int // HTTP request timeout
	CircuitBreakerThreshold int // Failures before disabling webhook
}

// RetryPolicy determines how to retry failed deliveries.
type RetryPolicy struct {
	config RetryConfig
}

// NewRetryPolicy creates a new retry policy.
func NewRetryPolicy(config RetryConfig) *RetryPolicy {
	return &RetryPolicy{config: config}
}

// CalculateBackoff calculates the backoff duration for a given attempt.
// Uses exponential backoff with jitter: base * 2^attempt + random jitter
func (rp *RetryPolicy) CalculateBackoff(attempt int) time.Duration {
	if attempt <= 0 {
		return 0
	}

	// Exponential backoff: 2^(attempt-1)
	exponential := math.Pow(2, float64(attempt-1))
	backoffMs := float64(rp.config.BackoffBaseMs) * exponential

	// Add jitter (±10%)
	jitterAmount := backoffMs * 0.1
	jitter := (rand.Float64() * 2 * jitterAmount) - jitterAmount

	totalMs := backoffMs + jitter
	if totalMs < 0 {
		totalMs = 0
	}

	return time.Duration(int64(totalMs)) * time.Millisecond
}

// NextRetryTime calculates when the next retry should be attempted.
func (rp *RetryPolicy) NextRetryTime(lastAttempt time.Time, attempt int) time.Time {
	backoff := rp.CalculateBackoff(attempt)
	return lastAttempt.Add(backoff)
}

// ShouldRetry determines if a delivery should be retried.
func (rp *RetryPolicy) ShouldRetry(attempt int, statusCode int, err error) bool {
	if attempt >= rp.config.MaxRetries {
		return false
	}

	// Retry on network errors or 5xx server errors
	if err != nil {
		return true
	}

	// Don't retry on 4xx client errors
	if statusCode >= 400 && statusCode < 500 {
		return false
	}

	// Retry on 5xx errors and timeouts
	return statusCode >= 500 || statusCode == 0
}

// ShouldCircuitBreak determines if the webhook should be disabled.
func (rp *RetryPolicy) ShouldCircuitBreak(failureCount int) bool {
	return failureCount >= rp.config.CircuitBreakerThreshold
}

// BackoffSequence generates the backoff sequence for documentation/testing.
func (rp *RetryPolicy) BackoffSequence(attempts int) []time.Duration {
	backoffs := make([]time.Duration, 0, attempts)
	for i := 0; i < attempts; i++ {
		backoffs = append(backoffs, rp.CalculateBackoff(i))
	}
	return backoffs
}
