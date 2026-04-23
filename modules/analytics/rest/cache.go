package rest

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// DefaultCache provides in-memory caching with TTL
type DefaultCache struct {
	mu    sync.RWMutex
	cache map[string]*cacheEntry
}

type cacheEntry struct {
	value     interface{}
	expiresAt time.Time
}

// NewCache creates a new cache
func NewCache() *DefaultCache {
	cache := &DefaultCache{
		cache: make(map[string]*cacheEntry),
	}

	// Start cleanup goroutine
	go cache.cleanup()

	return cache
}

// Get retrieves a value from cache
func (c *DefaultCache) Get(ctx context.Context, key string) (interface{}, bool, error) {
	if err := ValidateContext(ctx); err != nil {
		return nil, false, err
	}

	c.mu.RLock()
	defer c.mu.RUnlock()

	entry, exists := c.cache[key]
	if !exists {
		return nil, false, nil
	}

	// Check if expired
	if time.Now().After(entry.expiresAt) {
		return nil, false, nil
	}

	return entry.value, true, nil
}

// Set stores a value in cache with TTL
func (c *DefaultCache) Set(ctx context.Context, key string, value interface{}, ttl time.Duration) error {
	if err := ValidateContext(ctx); err != nil {
		return err
	}

	if key == "" {
		return fmt.Errorf("cache key cannot be empty")
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	c.cache[key] = &cacheEntry{
		value:     value,
		expiresAt: time.Now().Add(ttl),
	}

	return nil
}

// Delete removes a value from cache
func (c *DefaultCache) Delete(ctx context.Context, key string) error {
	if err := ValidateContext(ctx); err != nil {
		return err
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	delete(c.cache, key)
	return nil
}

// Clear removes all entries from cache
func (c *DefaultCache) Clear(ctx context.Context) error {
	if err := ValidateContext(ctx); err != nil {
		return err
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	c.cache = make(map[string]*cacheEntry)
	return nil
}

// cleanup periodically removes expired entries
func (c *DefaultCache) cleanup() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		c.mu.Lock()

		now := time.Now()
		for key, entry := range c.cache {
			if now.After(entry.expiresAt) {
				delete(c.cache, key)
			}
		}

		c.mu.Unlock()
	}
}

// Size returns the number of entries in cache
func (c *DefaultCache) Size() int {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return len(c.cache)
}
