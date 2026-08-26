package rest

import (
	"context"
	"fmt"
	"sync"
	"time"

	aegionloza "github.com/aegion/aegion/internal/platform/loza"
	lozasdk "github.com/astraive/loza/sdks/go"
)

// QueryCacheConfig holds configuration for query result caching
type QueryCacheConfig struct {
	// Enabled enables/disables caching
	Enabled bool
	// MaxSize is maximum number of entries to keep
	MaxSize int
	// TTL is time-to-live for cache entries
	TTL time.Duration
	// StatsInterval is how often to log cache statistics
	StatsInterval time.Duration
}

// CacheStats tracks cache performance metrics
type CacheStats struct {
	Hits      int64
	Misses    int64
	Evictions int64
	Size      int
	MaxSize   int
	HitRate   float64
	LastReset time.Time
}

// cacheNode represents a node in the LRU doubly-linked list
type cacheNode struct {
	key       string
	value     interface{}
	expiresAt time.Time
	prev      *cacheNode
	next      *cacheNode
}

// LRUQueryCache provides LRU caching with TTL and statistics
type LRUQueryCache struct {
	mu        sync.RWMutex
	config    QueryCacheConfig
	cache     map[string]*cacheNode
	head      *cacheNode // most recently used
	tail      *cacheNode // least recently used
	stats     CacheStats
	statsTick *time.Ticker
	stopCh    chan struct{}
}

// NewLRUQueryCache creates a new LRU cache
func NewLRUQueryCache(config QueryCacheConfig) *LRUQueryCache {
	if config.MaxSize == 0 {
		config.MaxSize = 1000
	}
	if config.TTL == 0 {
		config.TTL = 15 * time.Minute
	}
	if config.StatsInterval == 0 {
		config.StatsInterval = 1 * time.Minute
	}

	cache := &LRUQueryCache{
		config:    config,
		cache:     make(map[string]*cacheNode),
		stats:     CacheStats{MaxSize: config.MaxSize, LastReset: time.Now()},
		statsTick: time.NewTicker(config.StatsInterval),
		stopCh:    make(chan struct{}),
	}

	// Create dummy head and tail nodes
	cache.head = &cacheNode{}
	cache.tail = &cacheNode{}
	cache.head.next = cache.tail
	cache.tail.prev = cache.head

	// Start stats reporter
	go cache.reportStats()
	// Start cleanup goroutine
	go cache.cleanup()

	return cache
}

// Get retrieves a value from cache and moves it to most-recently-used
func (c *LRUQueryCache) Get(ctx context.Context, key string) (interface{}, bool, error) {
	if !c.config.Enabled {
		return nil, false, nil
	}

	if err := ValidateContext(ctx); err != nil {
		return nil, false, err
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	node, exists := c.cache[key]
	if !exists {
		c.stats.Misses++
		return nil, false, nil
	}

	// Check if expired
	if time.Now().After(node.expiresAt) {
		c.removeNode(node)
		delete(c.cache, key)
		c.stats.Misses++
		return nil, false, nil
	}

	// Move to head (most recently used)
	c.moveToHead(node)
	c.stats.Hits++
	return node.value, true, nil
}

// Set stores a value in cache, evicting LRU entry if necessary
func (c *LRUQueryCache) Set(ctx context.Context, key string, value interface{}, ttl time.Duration) error {
	if !c.config.Enabled {
		return nil
	}

	if err := ValidateContext(ctx); err != nil {
		return err
	}

	if key == "" {
		return fmt.Errorf("cache key cannot be empty")
	}

	if ttl == 0 {
		ttl = c.config.TTL
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	// If key already exists, update it
	if node, exists := c.cache[key]; exists {
		node.value = value
		node.expiresAt = time.Now().Add(ttl)
		c.moveToHead(node)
		return nil
	}

	// Create new node and add to head
	newNode := &cacheNode{
		key:       key,
		value:     value,
		expiresAt: time.Now().Add(ttl),
	}
	c.addNodeToHead(newNode)
	c.cache[key] = newNode

	// Evict LRU entry if size exceeded
	if len(c.cache) > c.config.MaxSize {
		c.evictLRU()
	}

	return nil
}

// Delete removes a value from cache
func (c *LRUQueryCache) Delete(ctx context.Context, key string) error {
	if err := ValidateContext(ctx); err != nil {
		return err
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if node, exists := c.cache[key]; exists {
		c.removeNode(node)
		delete(c.cache, key)
	}

	return nil
}

// Clear removes all entries from cache
func (c *LRUQueryCache) Clear(ctx context.Context) error {
	if err := ValidateContext(ctx); err != nil {
		return err
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	c.cache = make(map[string]*cacheNode)
	c.head.next = c.tail
	c.tail.prev = c.head
	c.stats.Hits = 0
	c.stats.Misses = 0
	c.stats.Evictions = 0
	c.stats.LastReset = time.Now()

	return nil
}

// Size returns the number of entries in cache
func (c *LRUQueryCache) Size() int {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return len(c.cache)
}

// GetStats returns cache statistics
func (c *LRUQueryCache) GetStats() CacheStats {
	c.mu.RLock()
	defer c.mu.RUnlock()

	stats := c.stats
	stats.Size = len(c.cache)

	total := stats.Hits + stats.Misses
	if total > 0 {
		stats.HitRate = float64(stats.Hits) / float64(total)
	}

	return stats
}

// InvalidatePattern invalidates all cache entries matching a pattern
func (c *LRUQueryCache) InvalidatePattern(ctx context.Context, pattern string) error {
	if err := ValidateContext(ctx); err != nil {
		return err
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	// Simple substring matching for pattern
	for key, node := range c.cache {
		if matchesPattern(key, pattern) {
			c.removeNode(node)
			delete(c.cache, key)
		}
	}

	return nil
}

// Close stops background routines
func (c *LRUQueryCache) Close() error {
	c.statsTick.Stop()
	close(c.stopCh)
	return nil
}

// --- Internal helper methods ---

// moveToHead moves a node to the head (most recently used)
func (c *LRUQueryCache) moveToHead(node *cacheNode) {
	c.removeNode(node)
	c.addNodeToHead(node)
}

// addNodeToHead adds a node right after the head (most recently used position)
func (c *LRUQueryCache) addNodeToHead(node *cacheNode) {
	node.prev = c.head
	node.next = c.head.next
	c.head.next.prev = node
	c.head.next = node
}

// removeNode removes a node from the linked list
func (c *LRUQueryCache) removeNode(node *cacheNode) {
	node.prev.next = node.next
	node.next.prev = node.prev
}

// evictLRU removes the least recently used (tail) entry
func (c *LRUQueryCache) evictLRU() {
	lruNode := c.tail.prev
	if lruNode != c.head {
		c.removeNode(lruNode)
		delete(c.cache, lruNode.key)
		c.stats.Evictions++
	}
}

// cleanup periodically removes expired entries
func (c *LRUQueryCache) cleanup() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			c.cleanupExpired()
		case <-c.stopCh:
			return
		}
	}
}

// cleanupExpired removes all expired entries
func (c *LRUQueryCache) cleanupExpired() {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()
	for key, node := range c.cache {
		if now.After(node.expiresAt) {
			c.removeNode(node)
			delete(c.cache, key)
		}
	}
}

// reportStats emits one wide event per cache statistics interval.
func (c *LRUQueryCache) reportStats() {
	for {
		select {
		case <-c.statsTick.C:
			stats := c.GetStats()
			eventCtx := aegionloza.Start(context.Background(), lozasdk.Default(), lozasdk.Params{
				Event:   "analytics.cache_stats",
				Kind:    "system",
				Service: "aegion.module.analytics",
				Custom: []lozasdk.Attr{
					lozasdk.String("cache.name", "analytics.query"),
					lozasdk.Int("cache.size", stats.Size),
					lozasdk.Int("cache.max_size", stats.MaxSize),
					lozasdk.Int64("cache.hits", stats.Hits),
					lozasdk.Int64("cache.misses", stats.Misses),
					lozasdk.Int64("cache.evictions", stats.Evictions),
					lozasdk.Float64("cache.hit_rate", stats.HitRate),
					lozasdk.Int64("cache.interval_ms", time.Since(stats.LastReset).Milliseconds()),
				},
			})
			_ = lozasdk.Default().Finish(eventCtx, "success")
			_ = lozasdk.Default().Emit(eventCtx)
		case <-c.stopCh:
			return
		}
	}
}

// matchesPattern checks if a key matches a pattern (simple substring matching)
func matchesPattern(key, pattern string) bool {
	if pattern == "*" {
		return true
	}
	return len(key) >= len(pattern) && key[:len(pattern)] == pattern
}
