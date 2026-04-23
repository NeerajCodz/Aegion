package webhooks

import (
	"testing"
	"time"
)

// TestSignature tests HMAC-SHA256 signature generation and verification.
func TestSignature(t *testing.T) {
	signer := NewSignature()
	secret := "test_secret_12345"

	payload := map[string]interface{}{
		"id":        "event-123",
		"event_type": "auth.login",
	}

	// Test signature generation
	sig, err := signer.Sign(secret, payload)
	if err != nil {
		t.Fatalf("Failed to sign payload: %v", err)
	}

	if sig == "" {
		t.Fatal("Signature should not be empty")
	}

	if len(sig) < 10 {
		t.Fatal("Signature too short")
	}

	// Test verification
	if !signer.Verify(secret, payload, sig) {
		t.Fatal("Signature verification failed")
	}

	// Test with wrong secret
	if signer.Verify("wrong_secret", payload, sig) {
		t.Fatal("Should fail with wrong secret")
	}
}

// TestRetryPolicy tests exponential backoff calculation.
func TestRetryPolicy(t *testing.T) {
	config := RetryConfig{
		MaxRetries:     5,
		BackoffBaseMs:  1000,
		TimeoutSeconds: 30,
		CircuitBreakerThreshold: 5,
	}

	policy := NewRetryPolicy(config)

	// Test backoff sequence
	backoffs := policy.BackoffSequence(5)
	if len(backoffs) != 5 {
		t.Fatalf("Expected 5 backoffs, got %d", len(backoffs))
	}

	// Verify increasing backoffs (allowing for jitter)
	for i := 0; i < len(backoffs); i++ {
		if backoffs[i] < 0 {
			t.Fatalf("Backoff should not be negative at attempt %d", i)
		}
	}

	// Test should retry logic
	if !policy.ShouldRetry(1, 500, nil) {
		t.Fatal("Should retry on 5xx error")
	}

	if policy.ShouldRetry(1, 400, nil) {
		t.Fatal("Should not retry on 4xx error")
	}

	if !policy.ShouldRetry(1, 0, nil) {
		t.Fatal("Should retry with status code 0")
	}

	if policy.ShouldRetry(6, 500, nil) {
		t.Fatal("Should not retry after max attempts")
	}

	// Test circuit breaker
	if !policy.ShouldCircuitBreak(5) {
		t.Fatal("Should circuit break at threshold")
	}

	if policy.ShouldCircuitBreak(4) {
		t.Fatal("Should not circuit break below threshold")
	}
}

// TestMatcher tests event matching with glob patterns and custom filters.
func TestMatcher(t *testing.T) {
	config := MatcherConfig{MaxCustomFilterDepth: 10}
	matcher := NewMatcher(config)

	testCases := []struct {
		name     string
		filter   EventFilter
		eventType string
		category string
		data     map[string]interface{}
		expected bool
	}{
		{
			name: "exact event type match",
			filter: EventFilter{
				EventTypes: []string{"user.created"},
			},
			eventType: "user.created",
			category:  "user",
			expected:  true,
		},
		{
			name: "glob pattern match",
			filter: EventFilter{
				EventTypes: []string{"auth.*"},
			},
			eventType: "auth.login",
			category:  "auth",
			expected:  true,
		},
		{
			name: "glob pattern no match",
			filter: EventFilter{
				EventTypes: []string{"auth.*"},
			},
			eventType: "user.created",
			category:  "user",
			expected:  false,
		},
		{
			name: "category match",
			filter: EventFilter{
				EventTypes: []string{"*"},
				Categories: []string{"user"},
			},
			eventType: "user.created",
			category:  "user",
			expected:  true,
		},
		{
			name: "category no match",
			filter: EventFilter{
				EventTypes: []string{"*"},
				Categories: []string{"user"},
			},
			eventType: "auth.login",
			category:  "auth",
			expected:  false,
		},
	}

	for _, tc := range testCases {
		result := matcher.Matches(tc.filter, tc.eventType, tc.category, tc.data)
		if result != tc.expected {
			t.Errorf("%s: expected %v, got %v", tc.name, tc.expected, result)
		}
	}
}

// TestQueue tests the delivery queue.
func TestQueue(t *testing.T) {
	queue := NewQueue(10)

	job := &DeliveryJob{
		ID:        "job-1",
		WebhookID: "webhook-1",
		EventID:   "event-1",
		CreatedAt: time.Now(),
	}

	// Test enqueue
	if err := queue.Enqueue(job); err != nil {
		t.Fatalf("Failed to enqueue job: %v", err)
	}

	// Test dequeue
	dequeued := queue.Dequeue()
	if dequeued == nil {
		t.Fatal("Dequeued job should not be nil")
	}

	if dequeued.ID != job.ID {
		t.Fatalf("Expected job ID %s, got %s", job.ID, dequeued.ID)
	}

	// Test pending count
	if queue.Pending() != 1 {
		t.Fatalf("Expected 1 pending job, got %d", queue.Pending())
	}

	queue.Remove(job.ID)
	if queue.Pending() != 0 {
		t.Fatal("Pending count should be 0 after remove")
	}

	// Test close
	if err := queue.Close(); err != nil {
		t.Fatalf("Failed to close queue: %v", err)
	}

	// Enqueue should fail after close
	if err := queue.Enqueue(job); err != ErrQueueClosed {
		t.Fatal("Should get ErrQueueClosed after close")
	}
}

// TestRateLimiter tests token bucket rate limiting.
func TestRateLimiter(t *testing.T) {
	limiter := NewRateLimiter(5, 1*time.Second)

	// Should allow 5 requests
	for i := 0; i < 5; i++ {
		if !limiter.Allow() {
			t.Fatalf("Request %d should be allowed", i+1)
		}
	}

	// 6th request should be denied
	if limiter.Allow() {
		t.Fatal("6th request should be denied")
	}

	// Reset should allow requests again
	limiter.Reset()
	if !limiter.Allow() {
		t.Fatal("Request after reset should be allowed")
	}
}
