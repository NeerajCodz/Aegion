package store

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// PerformanceConfig holds performance tuning settings
type PerformanceConfig struct {
	// Query execution
	QueryTimeoutSeconds int `yaml:"query_timeout_seconds"`
	MaxConcurrentQueries int `yaml:"max_concurrent_queries"`
	ExplainThresholdMs  int `yaml:"explain_threshold_ms"`

	// Caching
	CachingEnabled bool          `yaml:"caching_enabled"`
	CacheTTLMinutes int          `yaml:"cache_ttl_minutes"`
	CacheMaxSizeMB int           `yaml:"cache_max_size_mb"`

	// Memory management
	MemoryMaxMB    int `yaml:"memory_max_mb"`
	ThreadCount    int `yaml:"thread_count"`
	GCIntervalMs   int `yaml:"gc_interval_ms"`

	// Batch operations
	SyncBatchSize     int `yaml:"sync_batch_size"`
	SyncFlushIntervalMs int `yaml:"sync_flush_interval_ms"`
	ExportBatchSize   int `yaml:"export_batch_size"`
	WebhookBatchSize  int `yaml:"webhook_batch_size"`
}

// QueryMetrics tracks performance metrics for queries
type QueryMetrics struct {
	Query          string
	DurationMs     int64
	RowsProcessed  int64
	RowsReturned   int64
	CacheHit       bool
	Timestamp      time.Time
}

// PerformanceMonitor tracks analytics performance metrics
type PerformanceMonitor struct {
	mu              sync.RWMutex
	config          PerformanceConfig

	// Counters
	queriesExecuted   int64
	cacheHits         int64
	cacheMisses       int64
	queryTimeoutCount int64

	// Latency tracking (percentiles)
	queryDurations []int64 // in milliseconds
	maxDurations   int     // keep last N durations

	// Concurrent query tracking
	concurrentQueries int64
	maxConcurrent     int64

	// Memory tracking
	currentMemoryMB int64
	peakMemoryMB    int64

	// Metrics history for trending
	metrics      []QueryMetrics
	maxMetrics   int
	lastSnapshot time.Time
}

// NewPerformanceMonitor creates a new performance monitor
func NewPerformanceMonitor(config PerformanceConfig) *PerformanceMonitor {
	if config.QueryTimeoutSeconds == 0 {
		config.QueryTimeoutSeconds = 300
	}
	if config.MaxConcurrentQueries == 0 {
		config.MaxConcurrentQueries = 50
	}
	if config.ExplainThresholdMs == 0 {
		config.ExplainThresholdMs = 1000
	}
	if config.MemoryMaxMB == 0 {
		config.MemoryMaxMB = 4096
	}
	if config.ThreadCount == 0 {
		config.ThreadCount = 8
	}
	if config.SyncBatchSize == 0 {
		config.SyncBatchSize = 1000
	}
	if config.ExportBatchSize == 0 {
		config.ExportBatchSize = 10000
	}

	return &PerformanceMonitor{
		config:        config,
		queryDurations: make([]int64, 0, 1000),
		maxDurations:  1000,
		metrics:       make([]QueryMetrics, 0, 10000),
		maxMetrics:    10000,
		lastSnapshot:  time.Now(),
	}
}

// RecordQuery records a query execution metric
func (pm *PerformanceMonitor) RecordQuery(metric QueryMetrics) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	atomic.AddInt64(&pm.queriesExecuted, 1)
	
	if metric.CacheHit {
		atomic.AddInt64(&pm.cacheHits, 1)
	} else {
		atomic.AddInt64(&pm.cacheMisses, 1)
	}

	// Track duration
	pm.queryDurations = append(pm.queryDurations, metric.DurationMs)
	if len(pm.queryDurations) > pm.maxDurations {
		pm.queryDurations = pm.queryDurations[1:]
	}

	// Track metrics history
	pm.metrics = append(pm.metrics, metric)
	if len(pm.metrics) > pm.maxMetrics {
		pm.metrics = pm.metrics[1:]
	}

	// Check if query exceeded slow query threshold
	if metric.DurationMs > int64(pm.config.ExplainThresholdMs) {
		fmt.Printf("[SLOW QUERY] %s took %dms (returned %d rows)\n",
			metric.Query[:min(50, len(metric.Query))], metric.DurationMs, metric.RowsReturned)
	}
}

// RecordConcurrentQuery increments concurrent query counter
func (pm *PerformanceMonitor) RecordConcurrentQuery() {
	current := atomic.AddInt64(&pm.concurrentQueries, 1)
	for {
		max := atomic.LoadInt64(&pm.maxConcurrent)
		if current <= max || atomic.CompareAndSwapInt64(&pm.maxConcurrent, max, current) {
			break
		}
	}
}

// ReleaseQuerySlot decrements concurrent query counter
func (pm *PerformanceMonitor) ReleaseQuerySlot() {
	atomic.AddInt64(&pm.concurrentQueries, -1)
}

// RecordQueryTimeout increments query timeout counter
func (pm *PerformanceMonitor) RecordQueryTimeout() {
	atomic.AddInt64(&pm.queryTimeoutCount, 1)
}

// UpdateMemory updates current memory usage
func (pm *PerformanceMonitor) UpdateMemory(memoryMB int64) {
	atomic.StoreInt64(&pm.currentMemoryMB, memoryMB)
	for {
		peak := atomic.LoadInt64(&pm.peakMemoryMB)
		if memoryMB <= peak || atomic.CompareAndSwapInt64(&pm.peakMemoryMB, peak, memoryMB) {
			break
		}
	}
}

// GetStats returns current performance statistics
func (pm *PerformanceMonitor) GetStats() map[string]interface{} {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	queries := atomic.LoadInt64(&pm.queriesExecuted)
	hits := atomic.LoadInt64(&pm.cacheHits)
	misses := atomic.LoadInt64(&pm.cacheMisses)

	hitRate := 0.0
	if queries > 0 {
		hitRate = float64(hits) / float64(queries) * 100
	}

	p50, p95, p99 := pm.calculatePercentiles()

	return map[string]interface{}{
		"queries_executed":      queries,
		"cache_hits":            hits,
		"cache_misses":          misses,
		"cache_hit_rate":        fmt.Sprintf("%.2f%%", hitRate),
		"query_timeout_count":   atomic.LoadInt64(&pm.queryTimeoutCount),
		"concurrent_queries":    atomic.LoadInt64(&pm.concurrentQueries),
		"max_concurrent":        atomic.LoadInt64(&pm.maxConcurrent),
		"current_memory_mb":     atomic.LoadInt64(&pm.currentMemoryMB),
		"peak_memory_mb":        atomic.LoadInt64(&pm.peakMemoryMB),
		"p50_latency_ms":        p50,
		"p95_latency_ms":        p95,
		"p99_latency_ms":        p99,
		"max_timeout_seconds":   pm.config.QueryTimeoutSeconds,
		"max_memory_mb":         pm.config.MemoryMaxMB,
		"thread_count":          pm.config.ThreadCount,
	}
}

// CheckSLA checks if performance meets SLA targets
func (pm *PerformanceMonitor) CheckSLA(ctx context.Context) (bool, string) {
	stats := pm.GetStats()

	p99 := stats["p99_latency_ms"].(int64)
	hitRate := stats["cache_hits"].(int64)
	memory := stats["current_memory_mb"].(int64)

	violations := []string{}

	// Check latency SLA
	if p99 > 100 {
		violations = append(violations, fmt.Sprintf("P99 latency %.0fms exceeds 100ms SLA", float64(p99)))
	}

	// Check cache hit rate SLA
	total := stats["queries_executed"].(int64)
	if total > 0 && hitRate < int64(float64(total)*0.7) {
		violations = append(violations, fmt.Sprintf("Cache hit rate below 70%% SLA"))
	}

	// Check memory SLA
	if memory > int64(pm.config.MemoryMaxMB) {
		violations = append(violations, fmt.Sprintf("Memory usage %dMB exceeds limit", memory))
	}

	if len(violations) > 0 {
		return false, fmt.Sprintf("SLA violations: %v", violations)
	}

	return true, "All SLAs met"
}

// calculatePercentiles calculates p50, p95, p99 from recorded durations
func (pm *PerformanceMonitor) calculatePercentiles() (int64, int64, int64) {
	if len(pm.queryDurations) == 0 {
		return 0, 0, 0
	}

	// Simple percentile calculation (in production, use a better algorithm)
	sorted := make([]int64, len(pm.queryDurations))
	copy(sorted, pm.queryDurations)
	
	// Bubble sort for simplicity (ok for small sets)
	for i := 0; i < len(sorted); i++ {
		for j := i + 1; j < len(sorted); j++ {
			if sorted[j] < sorted[i] {
				sorted[i], sorted[j] = sorted[j], sorted[i]
			}
		}
	}

	p50Idx := len(sorted) / 2
	p95Idx := (len(sorted) * 95) / 100
	p99Idx := (len(sorted) * 99) / 100

	p50 := sorted[p50Idx]
	p95 := sorted[min(p95Idx, len(sorted)-1)]
	p99 := sorted[min(p99Idx, len(sorted)-1)]

	return p50, p95, p99
}

// GetSlowQueries returns the slowest queries from history
func (pm *PerformanceMonitor) GetSlowQueries(limit int) []QueryMetrics {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	if len(pm.metrics) == 0 {
		return []QueryMetrics{}
	}

	// Sort by duration (descending)
	sorted := make([]QueryMetrics, len(pm.metrics))
	copy(sorted, pm.metrics)
	
	for i := 0; i < len(sorted); i++ {
		for j := i + 1; j < len(sorted); j++ {
			if sorted[j].DurationMs > sorted[i].DurationMs {
				sorted[i], sorted[j] = sorted[j], sorted[i]
			}
		}
	}

	if limit > len(sorted) {
		limit = len(sorted)
	}

	return sorted[:limit]
}

// Reset clears all metrics
func (pm *PerformanceMonitor) Reset() {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	atomic.StoreInt64(&pm.queriesExecuted, 0)
	atomic.StoreInt64(&pm.cacheHits, 0)
	atomic.StoreInt64(&pm.cacheMisses, 0)
	atomic.StoreInt64(&pm.queryTimeoutCount, 0)
	atomic.StoreInt64(&pm.maxConcurrent, 0)
	
	pm.queryDurations = make([]int64, 0, pm.maxDurations)
	pm.metrics = make([]QueryMetrics, 0, pm.maxMetrics)
	pm.lastSnapshot = time.Now()
}

// min returns the minimum of two integers
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
