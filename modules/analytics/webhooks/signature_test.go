package webhooks

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSignPayloadAddsHeadersAndSignature(t *testing.T) {
	payload, headers, err := SignPayload(
		"event-123",
		"auth.login",
		"authentication",
		map[string]interface{}{"success": true},
		"secret",
	)
	require.NoError(t, err)
	require.NotNil(t, payload)

	assert.Equal(t, "event-123", headers["X-Aegion-Event-ID"])
	assert.Equal(t, "auth.login", headers["X-Aegion-Event-Type"])
	assert.Contains(t, headers["X-Aegion-Signature"], "sha256=")
}

func TestSignatureVerifyDetectsTampering(t *testing.T) {
	signer := NewSignature()
	payload := map[string]interface{}{"event_type": "auth.login"}

	signature, err := signer.Sign("secret", payload)
	require.NoError(t, err)

	assert.True(t, signer.Verify("secret", payload, signature))
	assert.False(t, signer.Verify("secret", map[string]interface{}{"event_type": "auth.logout"}, signature))
	assert.False(t, signer.Verify("wrong", payload, signature))
}

func TestRetryPolicyAdditionalScenarios(t *testing.T) {
	policy := NewRetryPolicy(RetryConfig{
		MaxRetries:             3,
		BackoffBaseMs:          100,
		CircuitBreakerThreshold: 2,
	})

	assert.True(t, policy.ShouldRetry(1, 500, nil))
	assert.True(t, policy.ShouldRetry(1, 0, assert.AnError))
	assert.False(t, policy.ShouldRetry(1, 400, nil))
	assert.False(t, policy.ShouldRetry(3, 500, nil))
	assert.True(t, policy.ShouldCircuitBreak(2))

	backoff := policy.CalculateBackoff(2)
	assert.Greater(t, backoff, time.Duration(0))
	assert.WithinDuration(t, time.Now().Add(backoff), policy.NextRetryTime(time.Now(), 2), time.Second)
}

func TestMatcherSupportsCustomFilters(t *testing.T) {
	matcher := NewMatcher(MatcherConfig{MaxCustomFilterDepth: 5})
	filter := EventFilter{
		EventTypes: []string{"auth.*"},
		Categories: []string{"authentication"},
		CustomFilter: map[string]interface{}{
			"$and": []interface{}{
				map[string]interface{}{"status": map[string]interface{}{"$eq": "success"}},
				map[string]interface{}{"message": map[string]interface{}{"$contains": "passkey"}},
			},
		},
	}

	match := matcher.Matches(filter, "auth.login", "authentication", map[string]interface{}{
		"status":  "success",
		"message": "passkey verified",
	})
	assert.True(t, match)

	noMatch := matcher.Matches(filter, "auth.login", "authentication", map[string]interface{}{
		"status":  "failed",
		"message": "password invalid",
	})
	assert.False(t, noMatch)
}

func TestQueueTimeoutAndPendingTracking(t *testing.T) {
	queue := NewQueue(1)
	job := &DeliveryJob{ID: "job-1", CreatedAt: time.Now()}

	require.NoError(t, queue.Enqueue(job))
	assert.Equal(t, 1, queue.Pending())

	dequeued := queue.DequeueTimeout(50 * time.Millisecond)
	require.NotNil(t, dequeued)
	assert.Equal(t, "job-1", dequeued.ID)

	queue.Remove(job.ID)
	assert.Equal(t, 0, queue.Pending())

	assert.Nil(t, queue.DequeueTimeout(10*time.Millisecond))
	require.NoError(t, queue.Close())
	assert.ErrorIs(t, queue.Enqueue(job), ErrQueueClosed)
}
