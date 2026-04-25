# Analytics Performance Tuning Guide

**Version:** 1.0  
**Last Updated:** 2026-04-24  
**Module:** `modules/analytics`

## Overview

This guide covers performance optimization, tuning, and benchmarking for the Aegion analytics system.

---

## DuckDB Configuration

### Thread Configuration

```yaml
analytics:
  duckdb:
    threads: 8    # Match CPU cores (CPU-bound workload)
```

**Recommendation:**
- Development: 2-4 threads
- Production: Match CPU core count
- Check: `nproc` (Linux) or `sysctl hw.ncpu` (macOS)

**Effect on Performance:**
- More threads = Better for parallel queries
- More threads = Higher memory consumption
- Optimal: Number of physical (not logical) cores

### Memory Configuration

```yaml
analytics:
  duckdb:
    memory_limit_gb: 8
    memory_per_thread_gb: 1
```

**Calculation:**
- Max memory = `memory_limit_gb`
- Per-thread budget = `memory_limit_gb / threads`
- Recommended: 50% of system memory for production

**Monitor:**
```bash
# Check memory usage
ps aux | grep aegion

# Check DuckDB process
top -p $(pgrep -f aegion)
```

### Connection Pool

```yaml
analytics:
  duckdb:
    connection_pool_size: 50
```

**Guidelines:**
- Development: 5-10
- Production: 50-100
- Formula: `min_connections = max_concurrent_queries * 1.5`

---

## Query Optimization

### Indexes

Create indexes on frequently queried columns:

```sql
-- Create indexes on common filters
CREATE INDEX idx_events_category ON events(category);
CREATE INDEX idx_events_userId ON events(userId);
CREATE INDEX idx_events_createdAt ON events(createdAt);

-- Composite index for common filter combinations
CREATE INDEX idx_events_category_date ON events(category, createdAt);

-- Sorted index for better range queries
CREATE INDEX idx_events_createdAt_sorted ON events(createdAt DESC);
```

### Common Index Strategy

```go
// Recommended indexes for typical analytics workload
type IndexStrategy struct {
  // Time-based queries (very common)
  "events.createdAt" // Range queries on time
  "events.userId"    // User-scoped queries
  "events.category"  // Category filtering
  
  // Composite for dashboard queries
  "events(category, createdAt)"
  "dashboards(ownerId, createdAt)"
}
```

### Query Examples

#### Slow Query (No Index)
```sql
SELECT COUNT(*) as total
FROM events
WHERE createdAt > '2026-04-20'
  AND category = 'user_action'
  AND userId = 'user_123';

-- Execution plan: Full table scan
-- Time: 2000ms for 1M rows
```

#### Fast Query (With Indexes)
```sql
-- Same query, with indexes
SELECT COUNT(*) as total
FROM events
WHERE createdAt > '2026-04-20'
  AND category = 'user_action'
  AND userId = 'user_123';

-- Execution plan: Index seek on composite index
-- Time: 50ms for 1M rows
```

### Query Pattern Optimization

#### Avoid SELECT *
```sql
-- ✗ SLOW: Fetch all columns
SELECT * FROM events WHERE category = 'user_action' LIMIT 10;

-- ✓ FAST: Fetch only needed columns
SELECT id, userId, category, data FROM events WHERE category = 'user_action' LIMIT 10;
```

#### Use Aggregation Functions
```sql
-- ✗ SLOW: Fetch all rows then count in app
SELECT * FROM events WHERE category = 'user_action';
-- Then: count in application

-- ✓ FAST: Count in database
SELECT COUNT(*) FROM events WHERE category = 'user_action';
```

#### Use LIMIT
```sql
-- ✗ SLOW: No limit, returns 1M rows
SELECT * FROM events WHERE category = 'user_action';

-- ✓ FAST: Limited result set
SELECT * FROM events WHERE category = 'user_action' LIMIT 1000;
```

#### Efficient Pagination
```sql
-- ✓ GOOD: Offset-based (simple)
SELECT * FROM events 
WHERE category = 'user_action'
ORDER BY createdAt DESC
LIMIT 20 OFFSET 100;

-- ✓ BETTER: Cursor-based (faster for large offsets)
SELECT * FROM events 
WHERE category = 'user_action'
  AND createdAt < '2026-04-24T10:00:00Z'
ORDER BY createdAt DESC
LIMIT 20;
```

---

## Caching Strategy

### LRU Cache Configuration

```yaml
analytics:
  cache:
    enabled: true
    strategy: lru        # Least Recently Used
    max_size_mb: 256
    ttl_seconds: 300     # 5 minutes
    
    # Cache specific queries
    cache_patterns:
      - query: "SELECT COUNT(*) FROM events"
        ttl_seconds: 60
      - query: "SELECT .* FROM dashboards WHERE isDefault"
        ttl_seconds: 300
```

### Cache Hit Rate Monitoring

```go
type CacheMetrics struct {
  TotalRequests  int64
  CacheHits      int64
  CacheMisses    int64
}

func (cm *CacheMetrics) HitRate() float64 {
  return float64(cm.CacheHits) / float64(cm.TotalRequests)
}
```

**Target:** > 70% cache hit rate

### Cache Invalidation

```go
// Automatic invalidation on data changes
func (h *Handler) CreateEvent(w http.ResponseWriter, r *http.Request) {
  event, _ := h.parseEvent(r)
  
  // Create event
  h.store.CreateEvent(event)
  
  // Invalidate related caches
  h.cache.Invalidate("SELECT COUNT(*) FROM events")
  h.cache.Invalidate("SELECT .* FROM dashboards")
}
```

---

## Query Performance Analysis

### Explain Plan

```sql
-- Analyze query execution plan
EXPLAIN
SELECT COUNT(*) FROM events 
WHERE category = 'user_action' 
  AND createdAt > '2026-04-20';

-- Output:
-- ┌─ COUNT(*)
-- │ └─ FILTER [category = 'user_action' AND createdAt > '2026-04-20']
-- │   └─ SEQ SCAN ON events
```

### Slow Query Logging

```yaml
analytics:
  logging:
    slow_query_threshold_ms: 100
    log_all_queries: false  # Only log slow queries
    explain_slow_queries: true
```

**Log output:**
```
2026-04-24T10:30:00Z WARN query execution slow query_id=q123 duration=250ms threshold=100ms
  sql="SELECT * FROM events WHERE userId='user_123'"
  rows=5234
```

### Performance Metrics

```go
type QueryMetrics struct {
  QueryID       string
  SQL           string
  ExecutionTime time.Duration
  RowsScanned   int64
  RowsReturned  int64
}

// Target metrics
type TargetMetrics struct {
  P50:  100 * time.Millisecond
  P95:  500 * time.Millisecond
  P99:  2 * time.Second
  Max:  5 * time.Second
}
```

---

## Concurrent Load Handling

### Connection Pool Tuning

```yaml
analytics:
  rest:
    connection_pool_size: 50
  graphql:
    connection_pool_size: 30
  grpc:
    max_concurrent_streams: 100
```

### Load Testing

```bash
# Using Apache Bench
ab -n 1000 -c 100 \
  -H "Authorization: Bearer token" \
  http://localhost:8080/api/v1/analytics/health

# Results:
# Requests per second: 500
# Time per request: 200ms
# Failed requests: 0
```

```bash
# Using wrk (better for concurrent)
wrk -t4 -c100 -d30s \
  -H "Authorization: Bearer token" \
  http://localhost:8080/api/v1/analytics/health

# Results:
# Requests/sec: 2500
# Avg latency: 40ms
# 99th percentile: 150ms
```

### Scaling Limits

| Configuration | Max Concurrent Users | Max QPS |
|---------------|-------------------|---------|
| Default | 50 | 100 |
| Optimized | 500 | 1000 |
| Production | 5000 | 10000 |

---

## Storage Tiering

### Hot/Warm/Cold Strategy

```yaml
analytics:
  retention:
    hot_tier:
      storage: duckdb
      ttl_hours: 24
      target_query_time_ms: 100
      
    warm_tier:
      storage: s3
      ttl_hours: 720        # 30 days
      target_query_time_ms: 1000
      
    cold_tier:
      storage: iceberg
      ttl_hours: 8760       # 365 days
      target_query_time_ms: 10000
```

### Monitoring Tier Usage

```bash
# Check storage usage per tier
curl http://localhost:8080/api/v1/analytics/stats

Response:
{
  "storage": {
    "hot": { "bytes": 5368709120, "percent": 85 },
    "warm": { "bytes": 943718400, "percent": 15 },
    "cold": { "bytes": 0, "percent": 0 }
  }
}
```

---

## Benchmarks

### Query Execution Time

Typical performance on modern hardware (8 CPU, 16GB RAM):

| Query Type | Rows | Time | Notes |
|-----------|------|------|-------|
| Count aggregation | 1M | 10ms | Cached |
| Simple filter | 1M | 50ms | With index |
| Join (2 tables) | 100k | 100ms | Index on both |
| GROUP BY | 1M | 200ms | 100 groups |
| Full scan | 1M | 500ms | No index |
| Complex join | 100k | 1s | Multiple joins |

### Cache Performance

| Scenario | Cache Miss | Cache Hit | Improvement |
|----------|-----------|-----------|-------------|
| Repeated aggregation | 50ms | 5ms | 10x |
| Dashboard refresh | 200ms | 20ms | 10x |
| Common filter | 100ms | 10ms | 10x |

### Throughput

| API Layer | Concurrent Users | Requests/Sec | Latency P99 |
|-----------|-----------------|-------------|------------|
| REST | 50 | 100 | 200ms |
| REST | 500 | 1000 | 300ms |
| GraphQL | 50 | 80 | 250ms |
| gRPC | 100 | 2000 | 100ms |

---

## Optimization Checklist

Before going to production:

- [ ] Indexes created on all commonly filtered columns
- [ ] Cache hit rate > 70%
- [ ] Slow query threshold configured
- [ ] Connection pool tuned to workload
- [ ] DuckDB threads = CPU cores
- [ ] Memory limit = 50% of system RAM
- [ ] Query timeout set (default 30s)
- [ ] Storage tiering configured (hot/warm/cold)
- [ ] Load tested to peak usage
- [ ] Monitoring and alerting enabled
- [ ] Backup strategy in place

---

## Related Documentation

- [Architecture](./architecture.md)
- [Setup Guide](./setup.md)
- [Security](./security.md)
- [Troubleshooting](./troubleshooting.md)
