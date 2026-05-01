# Phase 15A - Configuration Alignment Plan

## Overview
Audit and align aegion.yaml with runtime analytics config.go, ensuring SPA forms match backend contracts.

## Key Findings (Task 15A1 - Audit Complete)

### Config Structure Analysis

#### In config.go but MISSING/INCOMPLETE in aegion.yaml:

1. **REST API Configuration** (RestAPIConfig)
   - enabled
   - endpoint
   - query_timeout_seconds
   - rate_limit_per_minute
   - max_page_size
   - default_page_size
   - cors (with enabled, allowed_origins, allowed_methods, allowed_headers, allow_credentials, max_age)

2. **GraphQL API Configuration** (GraphQLAPIConfig)
   - enabled
   - endpoint
   - introspection
   - playground
   - max_query_depth
   - max_query_complexity
   - query_timeout_seconds
   - rate_limit_per_minute

3. **gRPC API Configuration** (gRPCAPIConfig)
   - enabled
   - port
   - max_concurrent_streams
   - keepalive_time_seconds
   - keepalive_timeout_seconds
   - max_connection_idle_seconds
   - enable_auth
   - enable_logging
   - enable_tracing

4. **DuckDB Performance Config** (PerformanceConfig)
   - query_timeout_seconds
   - max_concurrent_queries
   - explain_threshold_ms
   - caching_enabled
   - cache_ttl_minutes
   - cache_max_size_mb
   - gc_interval_ms
   - sync_batch_size
   - sync_flush_interval_ms
   - export_batch_size
   - webhook_batch_size

5. **Sync Strategies** - NEEDS RESTRUCTURING
   - Current: strategies list in SyncConfig
   - Should have: Nested real_time, batch, async configs with enable flags
   - Missing fields:
     - real_time.batch_size, flush_interval_ms, max_retries, retry_backoff_ms
     - batch.interval, start_time, tables, batch_size, chunk_size
     - async.broker, topic, partitions, consumer_group, worker_count, retry_backoff_ms, max_retries, broker_config

6. **Storage Failover** (not in config.go but mentioned in requirements)
   - Need: failover_backends[] array for multi-backend setup

7. **Rate Limiting Endpoints** (RateLimitingConfig)
   - Currently only has "export": 60
   - Should support map for dynamic endpoint limiting

### SPA Type Analysis vs YAML

The SPA types (`modules/admin/spa/src/types/analytics.ts`) define:
- SyncStrategy: 'real-time' | 'batch' | 'async' | 'hybrid' (but config.go only has first 3)
- StorageBackend config with backend field (YAML has type)
- Sync/Batch/Async/Hybrid configs with different field names (broker_type vs broker)
- Tier configs with storage_backend and compression settings

**Action:** Align SPA types with config.go, update forms to match actual backend capabilities

## Implementation Plan

### Task 15A2: Update aegion.yaml
1. Add REST API section (currently missing)
2. Add GraphQL API section (currently missing)
3. Add gRPC API section (currently missing)
4. Add Performance tuning section to DuckDB
5. Restructure Sync section with real_time, batch, async subsections
6. Enhance storage section with failover_backends

### Task 15A3: Create docs/analytics/config.md
- Configuration file location and format
- Environment variable overrides
- All top-level sections documentation
- Per-field descriptions with defaults and constraints
- Examples for local dev, Docker, Kubernetes

### Task 15A4: Verify SPA forms match backend
- SyncConfig.tsx: Update to show all strategy configs
- StorageConfig.tsx: Complete (mostly good)
- RetentionConfig.tsx: Verify tier configs
- WebhookConfig.tsx: Verify matches backend

### Task 15A5: Add config validation tests
- Test valid minimal config
- Test storage type validation
- Test strategy combinations
- Test environment variable overrides
- Test retention tiers

### Task 15A6: End-to-end verification
- Run config tests
- Build SPA
- Start server
- Test admin config UI

## Config Fields Summary

**Total fields to add/update:**
- REST API: 7 fields
- GraphQL: 7 fields  
- gRPC: 8 fields
- Performance: 11 fields
- Sync strategies: ~15 fields (across 3 subsections)
- Storage failover: 1 array field
- Rate limiting endpoints: Already in place, just needs expansion

**Validation Rules to Enforce:**
- DuckDB path/max_memory: at least one required
- Connection pool size: > 0
- DuckDB threads: > 0
- Storage backend selection: must have valid type
- Strategy combos: Can have multiple strategies
