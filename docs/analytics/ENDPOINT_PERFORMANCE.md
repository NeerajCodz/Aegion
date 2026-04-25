# Aegion Analytics - Performance Notes

Performance characteristics, response times, and optimization recommendations for all endpoints.

---

## Performance Summary

**Total Endpoints:** 56  
**Average Response Time:** 150-300ms  
**P95 Response Time:** 500-2000ms  
**P99 Response Time:** 2000-5000ms

---

## Endpoint Performance by Category

### Health & Status Endpoints (6)

#### GET /health
- **Expected Response Time:** <10ms
- **Typical Data Size:** <1KB
- **Rate Limiting:** No
- **Caching:** No
- **Notes:** Direct in-memory check, always fast

#### GET /ready
- **Expected Response Time:** 10-50ms
- **Typical Data Size:** 1-5KB
- **Rate Limiting:** No
- **Caching:** 10 seconds
- **Notes:** Checks database and cache connectivity

#### GET /live
- **Expected Response Time:** <5ms
- **Typical Data Size:** <1KB
- **Rate Limiting:** No
- **Caching:** No
- **Notes:** Simple health signal

#### GET /metrics
- **Expected Response Time:** 50-200ms
- **Typical Data Size:** 10-100KB
- **Rate Limiting:** No
- **Caching:** 1 minute
- **Notes:** Prometheus format, cache to reduce CPU

#### GET /stats
- **Expected Response Time:** 50-100ms
- **Typical Data Size:** 1-5KB
- **Rate Limiting:** No
- **Caching:** 1 minute
- **Notes:** Aggregates counters from memory

#### GET /export-formats
- **Expected Response Time:** <5ms
- **Typical Data Size:** <1KB
- **Rate Limiting:** No
- **Caching:** 1 hour
- **Notes:** Static list, heavily cache

---

### Events Endpoints (5)

#### GET /events
- **Expected Response Time:** 50-200ms
- **Typical Data Size:** 10-500KB (depends on limit)
- **Rate Limiting:** Yes (100/min per user)
- **Caching:** 10 seconds
- **Database Index:** `idx_events_created_at`, `idx_events_category`
- **Notes:** 
  - Default 50 items, increase limit for more data
  - Filter by category/type reduces query time
  - Consider pagination for large result sets
  - Cache frequently accessed pages (1, recent dates)

#### POST /events/search
- **Expected Response Time:** 200-1000ms
- **Typical Data Size:** 20-1000KB
- **Rate Limiting:** Yes (50/min per user)
- **Caching:** No (search results vary by query)
- **Database Index:** Full-text search indexes on data
- **Notes:**
  - Time range filters dramatically improve performance
  - Complex filters slower than simple category filters
  - Default 50 results, max 100
  - Consider pagination for large result sets
  - Search can be slow on large date ranges

#### GET /events/{id}
- **Expected Response Time:** 20-50ms
- **Typical Data Size:** 2-10KB
- **Rate Limiting:** Yes
- **Caching:** 5 minutes
- **Database Index:** `pk_events`
- **Notes:**
  - Direct lookup, very fast
  - Cache aggressively to reduce load

#### GET /events/{id}/related
- **Expected Response Time:** 100-300ms
- **Typical Data Size:** 20-500KB
- **Rate Limiting:** Yes
- **Caching:** 5 minutes
- **Notes:**
  - Session relation: fast (indexed)
  - User relation: moderate (indexed)
  - Data relation: slow (full scan)
  - Limit to 20-50 items for best performance

#### POST /events/export
- **Expected Response Time:** 500-5000ms
- **Typical Data Size:** 100KB-10MB (file size)
- **Rate Limiting:** Yes (20/min per user)
- **Caching:** No
- **Notes:**
  - Time proportional to data size
  - CSV faster than JSON/Parquet
  - Request may be async for large exports
  - Recommend limiting to 7-30 day ranges

---

### Dashboard Endpoints (8)

#### GET /dashboards
- **Expected Response Time:** 50-150ms
- **Typical Data Size:** 10-200KB
- **Rate Limiting:** Yes
- **Caching:** 5 minutes
- **Database Index:** `idx_dashboards_owner`
- **Notes:**
  - Includes shared dashboards (slower)
  - Cache frequently accessed lists
  - Default 50 items

#### POST /dashboards
- **Expected Response Time:** 100-300ms
- **Typical Data Size:** 1-10KB response
- **Rate Limiting:** Yes
- **Caching:** N/A
- **Notes:**
  - Includes validation and ownership setup
  - I/O bound on database write

#### GET /dashboards/{id}
- **Expected Response Time:** 20-50ms
- **Typical Data Size:** 5-50KB
- **Rate Limiting:** Yes
- **Caching:** 5 minutes
- **Database Index:** `pk_dashboards`
- **Notes:**
  - Fast lookup
  - Large config objects can slow down
  - Cache aggressively

#### PUT /dashboards/{id}
- **Expected Response Time:** 100-300ms
- **Typical Data Size:** 1-10KB response
- **Rate Limiting:** Yes
- **Caching:** Invalidate on update
- **Notes:**
  - Includes validation
  - Updates cache invalidation

#### DELETE /dashboards/{id}
- **Expected Response Time:** 100-200ms
- **Typical Data Size:** 0KB
- **Rate Limiting:** Yes
- **Caching:** Invalidate cache
- **Notes:**
  - May cascade delete components/queries

#### POST /dashboards/{id}/share
- **Expected Response Time:** 50-150ms
- **Typical Data Size:** 1-5KB response
- **Rate Limiting:** Yes
- **Caching:** Invalidate for affected users
- **Notes:**
  - Permission check adds latency

#### POST /dashboards/{id}/components/{componentId}/execute
- **Expected Response Time:** 200-5000ms
- **Typical Data Size:** 10-1000KB (query results)
- **Rate Limiting:** Yes (50/min per user)
- **Caching:** 1 minute (unless cache disabled)
- **Notes:**
  - Query execution dominates time
  - Results cached by default
  - Can be slow for complex queries
  - Recommend query optimization

---

### Query Endpoints (4)

#### GET /queries
- **Expected Response Time:** 50-100ms
- **Typical Data Size:** 5-100KB
- **Rate Limiting:** Yes
- **Caching:** 5 minutes
- **Notes:**
  - Fast metadata retrieval
  - Does not execute queries

#### POST /queries
- **Expected Response Time:** 100-300ms
- **Typical Data Size:** 1-10KB response
- **Rate Limiting:** Yes
- **Caching:** N/A
- **Notes:**
  - Includes SQL validation
  - Can be slow if query is complex

#### GET /queries/{id}/execute
- **Expected Response Time:** 200-5000ms
- **Typical Data Size:** 10-1000KB
- **Rate Limiting:** Yes (50/min per user)
- **Caching:** 1 minute
- **Notes:**
  - Dominant factor: query complexity
  - Add indexes for frequently used queries
  - Cache results when possible
  - Complex queries can take 30+ seconds

#### DELETE /queries/{id}
- **Expected Response Time:** 50-100ms
- **Typical Data Size:** 0KB
- **Rate Limiting:** Yes
- **Caching:** N/A
- **Notes:**
  - Simple delete, very fast

---

### Report Endpoints (7)

#### GET /reports
- **Expected Response Time:** 50-100ms
- **Typical Data Size:** 5-100KB
- **Rate Limiting:** Yes
- **Caching:** 5 minutes
- **Notes:**
  - Fast metadata retrieval

#### POST /reports
- **Expected Response Time:** 100-300ms
- **Typical Data Size:** 1-10KB response
- **Rate Limiting:** Yes
- **Caching:** N/A
- **Notes:**
  - Includes template validation

#### GET /reports/{id}
- **Expected Response Time:** 20-50ms
- **Typical Data Size:** 2-20KB
- **Rate Limiting:** Yes
- **Caching:** 5 minutes
- **Notes:**
  - Fast lookup

#### PUT /reports/{id}
- **Expected Response Time:** 100-200ms
- **Typical Data Size:** 1-10KB response
- **Rate Limiting:** Yes
- **Caching:** Invalidate on update
- **Notes:**
  - Update validation

#### DELETE /reports/{id}
- **Expected Response Time:** 50-100ms
- **Typical Data Size:** 0KB
- **Rate Limiting:** Yes
- **Caching:** N/A
- **Notes:**
  - Simple delete

#### POST /reports/{id}/generate
- **Expected Response Time:** 100-500ms (returns immediately)
- **Typical Data Size:** 1-5KB response (job status)
- **Rate Limiting:** Yes (20/min per user)
- **Caching:** N/A
- **Notes:**
  - Async operation
  - Returns job ID immediately
  - Actual generation: 30 seconds - 5 minutes

#### GET /reports/{id}/download
- **Expected Response Time:** 200-5000ms
- **Typical Data Size:** 100KB-10MB
- **Rate Limiting:** Yes
- **Caching:** No (files cached on disk)
- **Notes:**
  - Speed depends on file size
  - Disk I/O bound
  - Stream responses for large files

---

### Configuration Endpoints (11)

#### GET /config/storage
- **Expected Response Time:** 20-50ms
- **Typical Data Size:** 1-5KB
- **Caching:** 10 minutes
- **Notes:**
  - Metadata only

#### PUT /config/storage
- **Expected Response Time:** 100-300ms
- **Caching:** Invalidate
- **Notes:**
  - Includes validation

#### POST /config/storage/test
- **Expected Response Time:** 100-5000ms
- **Notes:**
  - Network latency to storage backend
  - Can be slow if backend is distant
  - Not rate limited to allow tests

#### GET /config/sync
- **Expected Response Time:** 20-50ms
- **Caching:** 10 minutes
- **Notes:**
  - Fast metadata

#### PUT /config/sync
- **Expected Response Time:** 50-100ms
- **Caching:** Invalidate
- **Notes:**
  - Updates configuration

#### POST /config/sync/trigger
- **Expected Response Time:** 100-500ms
- **Notes:**
  - Returns immediately with job ID
  - Actual sync: seconds to minutes

#### GET /config/sync/{syncId}/status
- **Expected Response Time:** 20-100ms
- **Caching:** 5 seconds
- **Notes:**
  - Status check, fast

#### GET /config/retention
- **Expected Response Time:** 20-50ms
- **Caching:** 10 minutes
- **Notes:**
  - Fast metadata

#### PUT /config/retention
- **Expected Response Time:** 50-100ms
- **Caching:** Invalidate
- **Notes:**
  - Updates policy

#### POST /config/retention/archive
- **Expected Response Time:** 100-500ms
- **Notes:**
  - Returns immediately with job ID
  - Actual archival: minutes to hours

#### GET /config/retention/archive-history
- **Expected Response Time:** 50-150ms
- **Caching:** 5 minutes
- **Notes:**
  - Fast history retrieval

---

### Validation Endpoints (4)

All validation endpoints:
- **Expected Response Time:** 100-500ms
- **Rate Limiting:** No (validation before update)
- **Notes:**
  - Perform connectivity checks
  - Not cached (returns fresh validation)

#### POST /validate/storage
- **Expected Response Time:** 100-5000ms
- **Notes:** Network latency to storage

#### POST /validate/sync
- **Expected Response Time:** 100-300ms
- **Notes:** Format validation

#### POST /validate/retention
- **Expected Response Time:** 50-100ms
- **Notes:** Policy validation

#### POST /validate/webhook
- **Expected Response Time:** 100-1000ms
- **Notes:** URL connectivity check

---

### Webhook Endpoints (8)

#### GET /webhooks
- **Expected Response Time:** 50-100ms
- **Caching:** 5 minutes
- **Notes:**
  - Fast metadata

#### POST /webhooks
- **Expected Response Time:** 100-300ms
- **Notes:**
  - Includes validation

#### GET /webhooks/{id}
- **Expected Response Time:** 20-50ms
- **Caching:** 5 minutes
- **Notes:**
  - Direct lookup

#### PUT /webhooks/{id}
- **Expected Response Time:** 100-200ms
- **Notes:**
  - Update validation

#### DELETE /webhooks/{id}
- **Expected Response Time:** 50-100ms
- **Notes:**
  - Simple delete

#### POST /webhooks/{id}/test
- **Expected Response Time:** 1000-10000ms
- **Caching:** No
- **Notes:**
  - Network latency to webhook endpoint
  - Can be slow if endpoint is distant
  - Not rate limited to allow testing

#### GET /webhooks/{id}/delivery-history
- **Expected Response Time:** 50-200ms
- **Caching:** 5 minutes
- **Notes:**
  - History retrieval

#### POST /webhooks/{id}/replay
- **Expected Response Time:** 100-500ms
- **Notes:**
  - Returns immediately with job ID
  - Actual replay: background job

---

### User Preferences Endpoints (4)

All user preference endpoints:
- **Expected Response Time:** 20-100ms
- **Caching:** 10 minutes per user
- **Notes:**
  - Fast metadata operations

#### GET /user/preferences
- **Expected Response Time:** 20-50ms

#### PUT /user/preferences
- **Expected Response Time:** 50-100ms

#### POST /user/favorites/dashboards/{id}
- **Expected Response Time:** 50-100ms

#### DELETE /user/favorites/dashboards/{id}
- **Expected Response Time:** 50-100ms

---

## Performance Optimization Recommendations

### Database Optimization

1. **Essential Indexes**
   - `idx_events_created_at` - For date range queries
   - `idx_events_category` - For category filtering
   - `idx_dashboards_owner` - For dashboard listing
   - `idx_queries_owner` - For query listing
   - Full-text search indexes on event data

2. **Query Optimization**
   - Always include date range in event queries
   - Use category/type filters when possible
   - Limit result sets to 50-100 items
   - Enable query result caching

3. **Partition Strategy**
   - Partition events by date (daily or weekly)
   - Archive old events monthly
   - Hot data (<30 days) on fast storage
   - Warm/cold data on archival storage

### Caching Strategy

1. **Cache Headers**
   ```
   GET /health                    → Cache-Control: max-age=0 (no cache)
   GET /ready                     → Cache-Control: max-age=10
   GET /metrics                   → Cache-Control: max-age=60
   GET /dashboards/{id}           → Cache-Control: max-age=300
   GET /queries/{id}/execute      → Cache-Control: max-age=60
   ```

2. **Redis Cache Keys**
   - `dashboard:{id}` - 5 minutes
   - `query:{id}:results` - 1 minute
   - `user:preferences:{userId}` - 10 minutes
   - Event search results - 10 seconds

3. **Query Result Cache**
   - Cache all query results by default
   - Disable with `cache=false` parameter
   - TTL: 1 minute (configurable)

### Rate Limiting

1. **Default Limits**
   - Health endpoints: No limit
   - List operations: 100/minute per user
   - Queries/Reports: 50/minute per user
   - Exports: 20/minute per user
   - Configuration: 10/minute per user

2. **Burst Allowance**
   - Allow 10 requests per second bursts
   - Smooth traffic over 1-minute windows

### Connection Pooling

1. **Database Connections**
   - Min: 10
   - Max: 100
   - Idle timeout: 5 minutes

2. **Storage Connections**
   - Min: 5
   - Max: 50
   - Connection reuse

---

## Performance Benchmarks

### Small Deployments (< 1M events)

| Operation | Response Time | Notes |
|-----------|----------------|-------|
| List events | 50-100ms | Simple queries |
| Search events | 100-300ms | Full-text search |
| Execute query | 100-500ms | Simple aggregations |
| Generate report | 5-10s | PDF generation |

### Medium Deployments (1M-100M events)

| Operation | Response Time | Notes |
|-----------|----------------|-------|
| List events | 100-300ms | Need pagination |
| Search events | 200-1000ms | Complex filters slower |
| Execute query | 500-3000ms | Larger result sets |
| Generate report | 15-60s | More data to process |

### Large Deployments (> 100M events)

| Operation | Response Time | Notes |
|-----------|----------------|-------|
| List events | 200-500ms | Tight pagination essential |
| Search events | 1-5s | Narrow date ranges needed |
| Execute query | 2-30s | Indexes critical |
| Generate report | 30-300s | Async only |

---

## Monitoring Metrics

### Key Performance Indicators (KPIs)

1. **P95 Response Time**
   - Health checks: < 50ms
   - API calls: < 2000ms
   - Database queries: < 5000ms

2. **Error Rates**
   - Target: < 0.1%
   - Database errors: < 0.01%
   - Timeout errors: < 0.05%

3. **Availability**
   - Target: 99.9% uptime
   - Health checks must pass 99%+

### Recommended Monitoring

```bash
# Response time percentiles
histogram_quantile(0.95, rate(http_request_duration_seconds[5m]))
histogram_quantile(0.99, rate(http_request_duration_seconds[5m]))

# Error rates
rate(http_requests_total{status=~"5.."}[5m])

# Query performance
histogram_quantile(0.95, rate(query_duration_seconds[5m]))
```

---

## Scalability Considerations

### Horizontal Scaling

1. **Stateless Services**
   - All endpoints are stateless
   - Can run multiple instances
   - Use load balancer

2. **Database Optimization**
   - Read replicas for list/get operations
   - Write to primary only
   - Cache frequent queries

3. **Cache Distribution**
   - Use distributed cache (Redis cluster)
   - Share cache across instances

### Vertical Scaling

1. **CPU Optimization**
   - Query compilation: 5-20% CPU
   - Result serialization: 10-30% CPU
   - Caching reduces CPU load

2. **Memory Requirements**
   - Per instance: 512MB base
   - Cache: 1-5GB recommended
   - Result buffers: 100-500MB

3. **Disk I/O**
   - Database: 50-100 IOPS typical
   - Report generation: 10-50 IOPS bursts
   - Archival: 100-1000 IOPS

---

**Last Updated:** January 2024 | **Version:** 1.0.0
