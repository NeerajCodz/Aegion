package proxy

import (
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/aegion/aegion/core/session"
)

var (
	ErrRateLimitExceeded = errors.New("rate limit exceeded")
)

// RateLimitStore defines the interface for rate limit storage.
type RateLimitStore interface {
	// Allow checks if a request with the given key should be allowed
	// Returns true if allowed, false if rate limited, and time until next allowed request
	Allow(key string, limit int, window time.Duration) (bool, time.Duration, error)

	// Reset resets the rate limit for a key
	Reset(key string) error

	// GetCount returns the current count for a key
	GetCount(key string) (int, error)
}

// MemoryStore implements RateLimitStore using in-memory storage with token bucket algorithm.
type MemoryStore struct {
	buckets map[string]*tokenBucket
	mutex   sync.RWMutex
	cleanup time.Duration
}

// tokenBucket represents a token bucket for rate limiting.
type tokenBucket struct {
	tokens       int
	capacity     int
	refillRate   int // tokens per second
	lastRefill   time.Time
	mutex        sync.Mutex
}

// NewMemoryStore creates a new in-memory rate limit store.
func NewMemoryStore() *MemoryStore {
	store := &MemoryStore{
		buckets: make(map[string]*tokenBucket),
		cleanup: 5 * time.Minute,
	}

	// Start cleanup goroutine
	go store.cleanupLoop()

	return store
}

// Allow implements RateLimitStore.Allow using token bucket algorithm.
func (m *MemoryStore) Allow(key string, limit int, window time.Duration) (bool, time.Duration, error) {
	m.mutex.RLock()
	bucket, exists := m.buckets[key]
	m.mutex.RUnlock()

	if !exists {
		m.mutex.Lock()
		// Double-check after acquiring write lock
		if bucket, exists = m.buckets[key]; !exists {
			bucket = &tokenBucket{
				tokens:     limit,
				capacity:   limit,
				refillRate: int(float64(limit) / window.Seconds()),
				lastRefill: time.Now(),
			}
			// Ensure refill rate is at least 1
			if bucket.refillRate == 0 {
				bucket.refillRate = 1
			}
			m.buckets[key] = bucket
		}
		m.mutex.Unlock()
	}

	return bucket.consume()
}

// Reset implements RateLimitStore.Reset.
func (m *MemoryStore) Reset(key string) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	if bucket, exists := m.buckets[key]; exists {
		bucket.mutex.Lock()
		bucket.tokens = bucket.capacity
		bucket.lastRefill = time.Now()
		bucket.mutex.Unlock()
	}
	return nil
}

// GetCount implements RateLimitStore.GetCount.
func (m *MemoryStore) GetCount(key string) (int, error) {
	m.mutex.RLock()
	bucket, exists := m.buckets[key]
	m.mutex.RUnlock()

	if !exists {
		return 0, nil
	}

	bucket.mutex.Lock()
	defer bucket.mutex.Unlock()
	
	bucket.refill()
	return bucket.capacity - bucket.tokens, nil
}

// cleanupLoop periodically removes unused buckets.
func (m *MemoryStore) cleanupLoop() {
	ticker := time.NewTicker(m.cleanup)
	defer ticker.Stop()

	for range ticker.C {
		now := time.Now()
		m.mutex.Lock()
		for key, bucket := range m.buckets {
			bucket.mutex.Lock()
			// Remove buckets that haven't been used for 2x cleanup interval
			if now.Sub(bucket.lastRefill) > 2*m.cleanup {
				delete(m.buckets, key)
			}
			bucket.mutex.Unlock()
		}
		m.mutex.Unlock()
	}
}

// consume attempts to consume a token from the bucket.
func (tb *tokenBucket) consume() (bool, time.Duration, error) {
	tb.mutex.Lock()
	defer tb.mutex.Unlock()

	tb.refill()

	if tb.tokens > 0 {
		tb.tokens--
		return true, 0, nil
	}

	// Calculate time until next token is available
	nextToken := time.Duration(float64(time.Second) / float64(tb.refillRate))
	return false, nextToken, nil
}

// refill adds tokens to the bucket based on elapsed time.
func (tb *tokenBucket) refill() {
	now := time.Now()
	elapsed := now.Sub(tb.lastRefill)
	
	// Calculate tokens to add
	tokensToAdd := int(elapsed.Seconds()) * tb.refillRate
	
	if tokensToAdd > 0 {
		tb.tokens = min(tb.capacity, tb.tokens+tokensToAdd)
		tb.lastRefill = now
	}
}

// min returns the minimum of two integers.
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// RateLimiter handles rate limiting for proxy requests.
type RateLimiter struct {
	store  RateLimitStore
	config RateLimitConfig
}

// NewRateLimiter creates a new rate limiter.
func NewRateLimiter(config RateLimitConfig, store RateLimitStore) *RateLimiter {
	if store == nil {
		store = NewMemoryStore()
	}

	return &RateLimiter{
		store:  store,
		config: config,
	}
}

// Allow checks if a request should be allowed based on rate limiting rules.
func (rl *RateLimiter) Allow(r *http.Request) (bool, time.Duration, error) {
	keys := rl.generateKeys(r)
	if len(keys) == 0 {
		return true, 0, nil // No rate limiting configured
	}

	var minWaitTime time.Duration

	// Check each key and return false if any are rate limited
	for _, key := range keys {
		allowed, waitTime, err := rl.store.Allow(
			key,
			rl.config.RequestsPerSecond,
			time.Second,
		)
		if err != nil {
			return false, 0, err
		}

		if !allowed {
			if minWaitTime == 0 || waitTime < minWaitTime {
				minWaitTime = waitTime
			}
			return false, minWaitTime, ErrRateLimitExceeded
		}
	}

	return true, 0, nil
}

// generateKeys creates rate limiting keys based on configuration.
func (rl *RateLimiter) generateKeys(r *http.Request) []string {
	var keys []string

	// Rate limit by IP
	if rl.config.ByIP {
		ip := getClientIP(r)
		if ip != "" {
			keys = append(keys, "ip:"+ip)
		}
	}

	// Rate limit by user (requires session)
	if rl.config.ByUser {
		if sess := session.FromContext(r.Context()); sess != nil {
			keys = append(keys, "user:"+sess.IdentityID.String())
		}
	}

	// Rate limit by path
	if rl.config.ByPath {
		path := strings.ToLower(r.URL.Path)
		keys = append(keys, "path:"+path)
	}

	return keys
}

// getClientIP extracts the real client IP from the request.
func getClientIP(r *http.Request) string {
	// Check X-Forwarded-For header first (most common)
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		// X-Forwarded-For can contain multiple IPs, take the first one
		if ips := strings.Split(xff, ","); len(ips) > 0 {
			return strings.TrimSpace(ips[0])
		}
	}

	// Check X-Real-IP header
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return strings.TrimSpace(xri)
	}

	// Check CF-Connecting-IP header (Cloudflare)
	if cfip := r.Header.Get("CF-Connecting-IP"); cfip != "" {
		return strings.TrimSpace(cfip)
	}

	// Fall back to RemoteAddr
	if ip := r.RemoteAddr; ip != "" {
		// RemoteAddr includes port, strip it
		if idx := strings.LastIndex(ip, ":"); idx != -1 {
			return ip[:idx]
		}
		return ip
	}

	return ""
}

// Reset resets rate limiting for a specific key.
func (rl *RateLimiter) Reset(key string) error {
	return rl.store.Reset(key)
}

// GetMetrics returns rate limiting metrics for monitoring.
func (rl *RateLimiter) GetMetrics(keys []string) map[string]int {
	metrics := make(map[string]int)
	
	for _, key := range keys {
		if count, err := rl.store.GetCount(key); err == nil {
			metrics[key] = count
		}
	}
	
	return metrics
}