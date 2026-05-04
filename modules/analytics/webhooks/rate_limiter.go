package webhooks

import (
	"sync"
	"sync/atomic"
	"time"
)

// RateLimiter implements token bucket rate limiting.
type RateLimiter struct {
	capacity       int64
	tokens         int64
	refillRate     int64
	refillInterval time.Duration
	lastRefill     time.Time
	mu             sync.Mutex
}

// NewRateLimiter creates a new rate limiter.
// capacity is the number of tokens available
// interval is how often to refill the tokens
func NewRateLimiter(capacity int64, interval time.Duration) *RateLimiter {
	return &RateLimiter{
		capacity:       capacity,
		tokens:         capacity,
		refillRate:     capacity,
		refillInterval: interval,
		lastRefill:     time.Now(),
	}
}

// Allow checks if a request is allowed under the rate limit.
func (rl *RateLimiter) Allow() bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	rl.refill()

	if rl.tokens > 0 {
		rl.tokens--
		return true
	}

	return false
}

// refill replenishes tokens based on time elapsed.
func (rl *RateLimiter) refill() {
	now := time.Now()
	elapsed := now.Sub(rl.lastRefill)

	if elapsed >= rl.refillInterval {
		rl.tokens = rl.capacity
		rl.lastRefill = now
	}
}

// Remaining returns the number of tokens remaining.
func (rl *RateLimiter) Remaining() int64 {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	rl.refill()
	return atomic.LoadInt64(&rl.tokens)
}

// Reset resets the rate limiter to full capacity.
func (rl *RateLimiter) Reset() {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	rl.tokens = rl.capacity
	rl.lastRefill = time.Now()
}
