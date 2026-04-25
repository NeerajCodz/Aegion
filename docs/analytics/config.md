# Analytics Configuration Reference

Complete configuration guide for the Aegion Analytics module, including all available settings, environment variable overrides, and examples for common deployment scenarios.

## Table of Contents

1. [Configuration File Location](#configuration-file-location)
2. [Overview](#overview)
3. [Top-Level Sections](#top-level-sections)
4. [Detailed Configuration Reference](#detailed-configuration-reference)
5. [Environment Variable Overrides](#environment-variable-overrides)
6. [Validation Rules](#validation-rules)
7. [Examples](#examples)
8. [Troubleshooting](#troubleshooting)

## Configuration File Location

The analytics module is configured in the main Aegion configuration file:

- **Development**: `configs/aegion.yaml`
- **Production**: `configs/aegion.production.yaml`

Configuration is loaded on server startup. Changes require server restart.

## Overview

The analytics module provides:

- **DuckDB**: Embedded OLAP database for analytics queries
- **Storage**: Multiple backend support (local, S3, Iceberg, Kubernetes)
- **Sync**: Real-time, batch, and async data synchronization from PostgreSQL
- **APIs**: REST, GraphQL, and gRPC interfaces
- **Webhooks**: Event-driven notifications with delivery tracking
- **Retention**: Automated data tiering and archival policies
- **Security**: RBAC, encryption, audit logging, query validation

## Top-Level Sections

### `analytics.enabled` (boolean)

Enable/disable the entire analytics module.

- **Default**: `true`
- **Type**: boolean
- **Env Override**: `AEGION_ANALYTICS_ENABLED`

### `analytics.security` (object)

Security configuration for the analytics module.

See [Security Configuration](#security-configuration) below.

### `analytics.duckdb` (object)

DuckDB database configuration.

See [DuckDB Configuration](#duckdb-configuration) below.

### `analytics.storage` (object)

Storage backend configuration.

See [Storage Configuration](#storage-configuration) below.

### `analytics.sync` (object)

Data synchronization configuration.

See [Sync Configuration](#sync-configuration) below.

### `analytics.webhooks` (object)

Webhook system configuration.

See [Webhooks Configuration](#webhooks-configuration) below.

### `analytics.retention` (object)

Data retention and tiering configuration.

See [Retention Configuration](#retention-configuration) below.

### `analytics.rest` (object)

REST API configuration.

See [REST API Configuration](#rest-api-configuration) below.

### `analytics.graphql` (object)

GraphQL API configuration.

See [GraphQL API Configuration](#graphql-api-configuration) below.

### `analytics.grpc` (object)

gRPC API configuration.

See [gRPC API Configuration](#grpc-api-configuration) below.

## Detailed Configuration Reference

### Security Configuration

**`analytics.security.enabled` (boolean)**

Enable/disable all security features.

- **Default**: `true`
- **Type**: boolean

**`analytics.security.rbac` (object)**

Role-Based Access Control settings.

- `enabled` (boolean): Enable RBAC - **Default**: `true`
- `default_role` (string): Default role for new users - **Default**: `"user"`

**`analytics.security.encryption` (object)**

Data encryption configuration.

- `enabled` (boolean): Enable encryption - **Default**: `true`
- `algorithm` (string): Encryption algorithm - **Default**: `"aes256"` - **Valid values**: `aes256`, `aes192`, `aes128`
- `key_rotation_days` (integer): Key rotation interval in days - **Default**: `90` - **Min**: `1`

**`analytics.security.rate_limiting` (object)**

Rate limiting configuration.

- `enabled` (boolean): Enable rate limiting - **Default**: `true`
- `requests_per_minute` (integer): Global rate limit - **Default**: `1000` - **Min**: `1`
- `endpoints` (object): Per-endpoint rate limits - **Default**: `{export: 60, query: 500}`
  - `export`: Rate limit for export operations
  - `query`: Rate limit for query operations

**`analytics.security.audit` (object)**

Audit logging configuration.

- `enabled` (boolean): Enable audit logging - **Default**: `true`
- `retention_days` (integer): How long to keep audit logs - **Default**: `365` - **Min**: `1`

**`analytics.security.query_validation` (object)**

Query validation to prevent abuse.

- `max_complexity` (integer): Maximum query complexity score - **Default**: `1000` - **Min**: `1`
- `max_recursion_depth` (integer): Maximum recursion depth for nested queries - **Default**: `10` - **Min**: `1`
- `max_fields` (integer): Maximum fields allowed in response - **Default**: `100` - **Min**: `1`

### DuckDB Configuration

**`analytics.duckdb.path` (string)**

Path to the DuckDB database file.

- **Default**: `analytics.duckdb`
- **Type**: string
- **Notes**: Use `:memory:` for in-memory database (not recommended for production)
- **Env Override**: `AEGION_ANALYTICS_DUCKDB_PATH`

**`analytics.duckdb.max_memory` (integer)**

Maximum memory DuckDB can use in MB.

- **Default**: `4096`
- **Type**: integer
- **Min**: `256`
- **Max**: `1048576` (1TB)
- **Notes**: Either `path` or `max_memory` must be specified

**`analytics.duckdb.threads` (integer)**

Number of threads DuckDB will use.

- **Default**: `4`
- **Type**: integer
- **Min**: `1`
- **Max**: CPU core count
- **Notes**: Set to CPU core count for optimal performance

**`analytics.duckdb.connection_pool_size` (integer)**

Maximum number of concurrent database connections.

- **Default**: `10`
- **Type**: integer
- **Min**: `1`
- **Max**: `1000`

**`analytics.duckdb.health_check_interval` (duration)**

How often to check connection health.

- **Default**: `30s`
- **Type**: duration (Go format: `30s`, `1m`, `2h`)
- **Valid range**: `5s` to `5m`

**`analytics.duckdb.initialize_on_startup` (boolean)**

Automatically create schema on startup.

- **Default**: `true`
- **Type**: boolean

**`analytics.duckdb.performance` (object)**

Performance tuning settings.

- `query_timeout_seconds` (integer): Query timeout in seconds - **Default**: `30` - **Min**: `1`
- `max_concurrent_queries` (integer): Max concurrent queries - **Default**: `50` - **Min**: `1`
- `explain_threshold_ms` (integer): Explain plan threshold (ms) - **Default**: `5000` - **Min**: `100`
- `caching_enabled` (boolean): Enable query result caching - **Default**: `true`
- `cache_ttl_minutes` (integer): Cache TTL in minutes - **Default**: `15` - **Min**: `1` - **Max**: `1440`
- `cache_max_size_mb` (integer): Max cache size in MB - **Default**: `512` - **Min**: `10` - **Max**: `10240`
- `gc_interval_ms` (integer): Garbage collection interval (ms) - **Default**: `300000` - **Min**: `10000`
- `sync_batch_size` (integer): Batch size for sync operations - **Default**: `1000` - **Min**: `10`
- `sync_flush_interval_ms` (integer): Flush interval (ms) - **Default**: `5000` - **Min**: `100`
- `export_batch_size` (integer): Export batch size - **Default**: `10000` - **Min**: `100`
- `webhook_batch_size` (integer): Webhook batch size - **Default**: `100` - **Min**: `1`

### Storage Configuration

**`analytics.storage.type` (string)**

Storage backend type.

- **Default**: `local`
- **Type**: string
- **Valid values**: `local`, `s3`, `iceberg`, `k8s`
- **Required**: yes

**`analytics.storage.local_path` (string)**

Path for local storage (used when `type` is `local`).

- **Default**: `./analytics_data`
- **Type**: string
- **Required** if `type: local`

**`analytics.storage.s3` (object)**

S3 configuration (used when `type` is `s3`).

- `bucket` (string): S3 bucket name - **Required**
- `region` (string): AWS region - **Required** - **Example**: `us-east-1`
- `prefix` (string): Object key prefix - **Default**: `duckdb`
- `endpoint_url` (string): Custom S3 endpoint URL (for S3-compatible services) - **Env Override**: `AEGION_ANALYTICS_S3_ENDPOINT_URL`
- `use_path_style` (boolean): Use path-style S3 URLs - **Default**: `false`
- `access_key_id` (string): AWS access key - **Env Override**: `AEGION_ANALYTICS_S3_ACCESS_KEY_ID` - **Recommended**: Use environment variable in production
- `secret_access_key` (string): AWS secret access key - **Env Override**: `AEGION_ANALYTICS_S3_SECRET_ACCESS_KEY` - **Recommended**: Use environment variable in production

**`analytics.storage.iceberg` (object)**

Apache Iceberg configuration (used when `type` is `iceberg`).

- `catalog_type` (string): Catalog type - **Required** - **Valid values**: `hive`, `glue`, `rest`, `nessie`
- `warehouse_path` (string): Warehouse location - **Required**
- `catalog_name` (string): Default catalog name - **Default**: `analytics`
- `nessie_uri` (string): Nessie server URI (if using Nessie catalog) - **Env Override**: `AEGION_ANALYTICS_NESSIE_URI`

**`analytics.storage.k8s` (object)**

Kubernetes PVC configuration (used when `type` is `k8s`).

- `pvc_name` (string): Persistent Volume Claim name - **Required**
- `mount_path` (string): Mount path inside container - **Required** - **Example**: `/data/analytics`
- `storage_class` (string): Storage class for dynamic provisioning - **Default**: `standard`
- `size` (string): PVC size - **Default**: `10Gi` - **Format**: Kubernetes size (e.g., `10Gi`, `100Mi`)

**`analytics.storage.failover_backends` (array of objects)**

Failover storage backends. The system tries these in order if the primary backend fails.

- **Type**: array of storage backend configurations
- **Default**: empty
- **Example**:
  ```yaml
  failover_backends:
    - type: s3
      s3:
        bucket: analytics-backup
        region: us-west-2
  ```

### Sync Configuration

**`analytics.sync.enabled` (boolean)**

Enable data synchronization.

- **Default**: `false`
- **Type**: boolean

**`analytics.sync.strategies` (array of strings)**

Active synchronization strategies.

- **Type**: array
- **Valid values**: `real_time`, `batch`, `async`
- **Default**: empty
- **Notes**: Can specify multiple strategies; system will use all enabled ones

**`analytics.sync.real_time` (object)**

Real-time CDC/trigger-based sync configuration.

- `enabled` (boolean): Enable real-time sync - **Default**: `false`
- `batch_size` (integer): Batch events before flushing - **Default**: `100` - **Min**: `1`
- `flush_interval_ms` (integer): Max time before flushing (ms) - **Default**: `5000` - **Min**: `100`
- `max_retries` (integer): Max retry attempts - **Default**: `3` - **Min**: `0`
- `retry_backoff_ms` (integer): Initial retry backoff (exponential, ms) - **Default**: `100` - **Min**: `10`

**`analytics.sync.batch` (object)**

Batch scheduler sync configuration.

- `enabled` (boolean): Enable batch sync - **Default**: `false`
- `interval` (string): Cron-like interval - **Default**: `1h` - **Examples**: `1h`, `1d`, `@hourly`, `@daily`, `0 2 * * *`
- `start_time` (string): Start time for first batch - **Default**: `02:00` - **Format**: `HH:MM` (UTC)
- `tables` (array of strings): Tables to include (if empty, all) - **Default**: empty
- `batch_size` (integer): Records per batch - **Default**: `1000` - **Min**: `10`
- `chunk_size` (integer): Internal chunk size for bulk inserts - **Default**: `100` - **Min**: `10`

**`analytics.sync.async` (object)**

Message queue based async sync configuration.

- `enabled` (boolean): Enable async sync - **Default**: `false`
- `broker` (string): Message broker type - **Default**: `memory` - **Valid values**: `kafka`, `rabbitmq`, `redis`, `memory`
- `topic` (string): Topic or queue name - **Default**: `analytics-events`
- `partitions` (integer): Number of partitions (Kafka-style) - **Default**: `3` - **Min**: `1`
- `consumer_group` (string): Consumer group name - **Default**: `aegion-analytics`
- `worker_count` (integer): Concurrent worker threads - **Default**: `4` - **Min**: `1`
- `retry_backoff_ms` (integer): Initial retry backoff (exponential, ms) - **Default**: `1000` - **Min**: `10`
- `max_retries` (integer): Max retry attempts - **Default**: `5` - **Min**: `0`
- `broker_config` (object): Broker-specific settings (JSON) - **Default**: `{}`

### Webhooks Configuration

**`analytics.webhooks.enabled` (boolean)**

Enable webhook system.

- **Default**: `true`
- **Type**: boolean

**`analytics.webhooks.max_per_user` (integer)**

Maximum webhooks allowed per user.

- **Default**: `50`
- **Type**: integer
- **Min**: `1`
- **Max**: `1000`

**`analytics.webhooks.max_retries` (integer)**

Maximum retry attempts for webhook delivery.

- **Default**: `5`
- **Type**: integer
- **Min**: `1`
- **Max**: `50`

**`analytics.webhooks.retry_backoff_base_ms` (integer)**

Initial retry backoff in milliseconds (exponential backoff).

- **Default**: `1000`
- **Type**: integer
- **Min**: `100`
- **Max**: `60000`

**`analytics.webhooks.timeout_seconds` (integer)**

HTTP request timeout for webhook delivery.

- **Default**: `30`
- **Type**: integer
- **Min**: `1`
- **Max**: `300`

**`analytics.webhooks.batch_size` (integer)**

Number of events per webhook delivery batch.

- **Default**: `100`
- **Type**: integer
- **Min**: `1`
- **Max**: `10000`

**`analytics.webhooks.worker_threads` (integer)**

Number of concurrent webhook delivery workers.

- **Default**: `10`
- **Type**: integer
- **Min**: `1`
- **Max**: `100`

**`analytics.webhooks.store_delivery_history_days` (integer)**

How long to keep webhook delivery history and failed events (DLQ).

- **Default**: `30`
- **Type**: integer
- **Min**: `1`
- **Max**: `365`

### Retention Configuration

**`analytics.retention.enabled` (boolean)**

Enable retention management.

- **Default**: `true`
- **Type**: boolean

**`analytics.retention.default_policy` (string)**

Default retention policy.

- **Default**: `tiered`
- **Type**: string
- **Valid values**: `tiered`, `archive`, `delete`

**`analytics.retention.hot_ttl_days` (integer)**

Default hot tier TTL in days.

- **Default**: `7`
- **Type**: integer
- **Min**: `1`
- **Max**: `3650`

**`analytics.retention.warm_ttl_days` (integer)**

Default warm tier TTL in days.

- **Default**: `90`
- **Type**: integer
- **Min**: `1`
- **Max**: `3650`

**`analytics.retention.cold_ttl_days` (integer)**

Default cold tier TTL in days.

- **Default**: `730`
- **Type**: integer
- **Min**: `1`
- **Max**: `3650`

**`analytics.retention.archival_interval` (duration)**

How often to run archival jobs.

- **Default**: `24h`
- **Type**: duration
- **Valid range**: `1h` to `30d`

**`analytics.retention.cleanup_interval` (duration)**

How often to run cleanup jobs.

- **Default**: `168h` (1 week)
- **Type**: duration
- **Valid range**: `1h` to `30d`

**`analytics.retention.tiering_interval` (duration)**

How often to re-tier data between tiers.

- **Default**: `6h`
- **Type**: duration
- **Valid range**: `1h` to `24h`

**`analytics.retention.categories` (object)**

Category-specific retention overrides.

- **Type**: map of category name to CategoryConfig
- **Keys**: Event category names (e.g., `audit_events`, `authentication`)
- **Each category has**:
  - `hot_days` (integer): Hot tier TTL
  - `warm_days` (integer): Warm tier TTL
  - `cold_days` (integer): Cold tier TTL

### REST API Configuration

**`analytics.rest.enabled` (boolean)**

Enable REST API.

- **Default**: `true`
- **Type**: boolean

**`analytics.rest.endpoint` (string)**

Base endpoint path.

- **Default**: `/api/v1/analytics`
- **Type**: string

**`analytics.rest.query_timeout_seconds` (integer)**

Query execution timeout in seconds.

- **Default**: `30`
- **Type**: integer
- **Min**: `1`
- **Max**: `300`

**`analytics.rest.rate_limit_per_minute` (integer)**

Rate limit in requests per minute.

- **Default**: `100`
- **Type**: integer
- **Min**: `1`

**`analytics.rest.max_page_size` (integer)**

Maximum page size for list results.

- **Default**: `1000`
- **Type**: integer
- **Min**: `1`

**`analytics.rest.default_page_size` (integer)**

Default page size for list results.

- **Default**: `50`
- **Type**: integer
- **Min**: `1`

**`analytics.rest.cors` (object)**

CORS configuration for REST API.

- `enabled` (boolean): Enable CORS - **Default**: `true`
- `allowed_origins` (array of strings): Allowed origin URLs
- `allowed_methods` (array of strings): Allowed HTTP methods - **Default**: `[GET, POST, PUT, DELETE, OPTIONS]`
- `allowed_headers` (array of strings): Allowed request headers
- `allow_credentials` (boolean): Allow credentials - **Default**: `true`
- `max_age` (integer): CORS max-age in seconds - **Default**: `3600`

### GraphQL API Configuration

**`analytics.graphql.enabled` (boolean)**

Enable GraphQL API.

- **Default**: `true`
- **Type**: boolean

**`analytics.graphql.endpoint` (string)**

GraphQL endpoint path.

- **Default**: `/api/v1/graphql`
- **Type**: string

**`analytics.graphql.introspection` (boolean)**

Enable schema introspection.

- **Default**: `true`
- **Type**: boolean

**`analytics.graphql.playground` (boolean)**

Enable GraphQL Playground UI.

- **Default**: `true`
- **Type**: boolean

**`analytics.graphql.max_query_depth` (integer)**

Maximum query depth.

- **Default**: `10`
- **Type**: integer
- **Min**: `1`
- **Max**: `100`

**`analytics.graphql.max_query_complexity` (integer)**

Maximum query complexity score.

- **Default**: `1000`
- **Type**: integer
- **Min**: `1`

**`analytics.graphql.query_timeout_seconds` (integer)**

Query execution timeout in seconds.

- **Default**: `30`
- **Type**: integer
- **Min**: `1`
- **Max**: `300`

**`analytics.graphql.rate_limit_per_minute` (integer)**

Rate limit in requests per minute.

- **Default**: `100`
- **Type**: integer
- **Min**: `1`

### gRPC API Configuration

**`analytics.grpc.enabled` (boolean)**

Enable gRPC API.

- **Default**: `true`
- **Type**: boolean

**`analytics.grpc.port` (integer)**

gRPC server port.

- **Default**: `50051`
- **Type**: integer
- **Min**: `1024`
- **Max**: `65535`

**`analytics.grpc.max_concurrent_streams` (integer)**

Maximum concurrent streams per connection.

- **Default**: `100`
- **Type**: integer
- **Min**: `1`

**`analytics.grpc.keepalive_time_seconds` (integer)**

Keepalive ping interval in seconds.

- **Default**: `20`
- **Type**: integer
- **Min**: `1`

**`analytics.grpc.keepalive_timeout_seconds` (integer)**

Keepalive ping timeout in seconds.

- **Default**: `10`
- **Type**: integer
- **Min**: `1`

**`analytics.grpc.max_connection_idle_seconds` (integer)**

Maximum idle time for connections in seconds.

- **Default**: `300`
- **Type**: integer
- **Min**: `1`

**`analytics.grpc.enable_auth` (boolean)**

Require service authentication.

- **Default**: `true`
- **Type**: boolean

**`analytics.grpc.enable_logging` (boolean)**

Enable request/response logging.

- **Default**: `true`
- **Type**: boolean

**`analytics.grpc.enable_tracing` (boolean)**

Enable OpenTelemetry tracing.

- **Default**: `true`
- **Type**: boolean

## Environment Variable Overrides

Many configuration values can be overridden via environment variables. The following pattern is used:

`AEGION_ANALYTICS_<SECTION>_<FIELD>` (uppercase, underscores for nested paths)

### Common Environment Variable Overrides

```bash
# Core Configuration
AEGION_ANALYTICS_ENABLED=true
AEGION_ANALYTICS_DUCKDB_PATH=/var/lib/analytics/analytics.duckdb

# S3 Storage
AEGION_ANALYTICS_S3_ENDPOINT_URL=https://s3.amazonaws.com
AEGION_ANALYTICS_S3_ACCESS_KEY_ID=AKIA...
AEGION_ANALYTICS_S3_SECRET_ACCESS_KEY=...

# Iceberg Storage
AEGION_ANALYTICS_NESSIE_URI=https://nessie.example.com

# DuckDB Performance
AEGION_ANALYTICS_DUCKDB_MAX_MEMORY=8192

# Retention
AEGION_ANALYTICS_RETENTION_HOT_TTL_DAYS=14
AEGION_ANALYTICS_RETENTION_COLD_TTL_DAYS=1095
```

## Validation Rules

The analytics configuration is validated on startup. The following rules are enforced:

### DuckDB Validation

- At least one of `path` or `max_memory` must be specified
- `connection_pool_size` must be > 0
- `max_memory` must be non-negative
- `threads` must be > 0

### Storage Validation

- `type` must be one of: `local`, `s3`, `iceberg`, `k8s`
- For `local`: `local_path` must be set
- For `s3`: `bucket` and `region` must be set
- For `iceberg`: `warehouse_path` and `catalog_type` must be set
- For `k8s`: `pvc_name` and `mount_path` must be set

### Retention Validation

- `hot_ttl_days` <= `warm_ttl_days` <= `cold_ttl_days`
- All TTL values must be positive integers

### Sync Validation

- If `enabled: true`, at least one strategy must be listed in `strategies`
- Active strategies must match their `enabled` status

## Examples

### Local Development Setup

```yaml
analytics:
  enabled: true
  
  duckdb:
    path: analytics.duckdb
    max_memory: 2048
    threads: 4
    connection_pool_size: 5
  
  storage:
    type: local
    local_path: ./analytics_data
  
  sync:
    enabled: false
    strategies: []
  
  retention:
    enabled: true
    hot_ttl_days: 7
    warm_ttl_days: 30
    cold_ttl_days: 90
  
  webhooks:
    enabled: true
    max_retries: 3
  
  rest:
    enabled: true
    rate_limit_per_minute: 1000
  
  graphql:
    enabled: true
    playground: true
  
  grpc:
    enabled: false
```

### Docker Production Setup

```yaml
analytics:
  enabled: true
  
  duckdb:
    path: /data/analytics/analytics.duckdb
    max_memory: 8192
    threads: 8
    connection_pool_size: 20
    performance:
      cache_enabled: true
      cache_ttl_minutes: 30
  
  storage:
    type: s3
    s3:
      bucket: analytics-prod
      region: us-east-1
      endpoint_url: ${AEGION_ANALYTICS_S3_ENDPOINT_URL}
      access_key_id: ${AEGION_ANALYTICS_S3_ACCESS_KEY_ID}
      secret_access_key: ${AEGION_ANALYTICS_S3_SECRET_ACCESS_KEY}
    failover_backends:
      - type: local
        local_path: /data/analytics/backup
  
  sync:
    enabled: true
    strategies:
      - batch
      - async
    batch:
      enabled: true
      interval: 1h
      start_time: "02:00"
      batch_size: 10000
    async:
      enabled: true
      broker: kafka
      topic: analytics-events
      partitions: 6
      worker_count: 8
  
  retention:
    enabled: true
    default_policy: tiered
    hot_ttl_days: 14
    warm_ttl_days: 180
    cold_ttl_days: 730
  
  webhooks:
    enabled: true
    max_per_user: 100
    max_retries: 5
    worker_threads: 20
  
  rest:
    enabled: true
    rate_limit_per_minute: 1000
  
  graphql:
    enabled: true
    playground: false
  
  grpc:
    enabled: true
    port: 50051
    enable_auth: true
```

### Kubernetes Setup

```yaml
analytics:
  enabled: true
  
  duckdb:
    path: /data/analytics/analytics.duckdb
    max_memory: 16384
    threads: 16
    connection_pool_size: 50
  
  storage:
    type: k8s
    k8s:
      namespace: aegion
      pvc_name: analytics-storage
      storage_class: ssd-retain
      access_mode: ReadWriteOnce
      size_gb: 500
  
  sync:
    enabled: true
    strategies:
      - real_time
      - batch
    real_time:
      enabled: true
      batch_size: 500
      flush_interval_ms: 2000
    batch:
      enabled: true
      interval: "6h"
      start_time: "00:00"
      batch_size: 50000
  
  retention:
    enabled: true
    archival_interval: 24h
    cleanup_interval: 168h
    tiering_interval: 6h
  
  grpc:
    enabled: true
    enable_logging: true
    enable_tracing: true
```

## Error Messages and Troubleshooting

### Configuration Validation Errors

**Error: "duckdb configuration missing: must specify path or max_memory"**

- Solution: Set either `analytics.duckdb.path` or `analytics.duckdb.max_memory`

**Error: "duckdb connection_pool_size must be greater than 0"**

- Solution: Set `analytics.duckdb.connection_pool_size` to a value > 0

**Error: "S3 storage requires bucket to be set"**

- Solution: When `analytics.storage.type: s3`, set `analytics.storage.s3.bucket`

**Error: "invalid storage type"**

- Solution: Set `analytics.storage.type` to one of: `local`, `s3`, `iceberg`, `k8s`

### Performance Issues

**Slow Query Performance**

- Increase `analytics.duckdb.threads`
- Enable query caching: `analytics.duckdb.performance.caching_enabled: true`
- Increase `analytics.duckdb.max_memory`
- Lower `analytics.duckdb.performance.explain_threshold_ms` to identify slow queries

**High Memory Usage**

- Reduce `analytics.duckdb.max_memory`
- Reduce `analytics.duckdb.performance.cache_max_size_mb`
- Reduce `analytics.duckdb.connection_pool_size`

**Webhook Delivery Issues**

- Increase `analytics.webhooks.worker_threads`
- Increase `analytics.webhooks.batch_size`
- Increase `analytics.webhooks.max_retries`

### Common Configuration Issues

**Storage Connection Failures**

- For S3: Verify bucket name, region, and credentials
- For Iceberg: Verify warehouse path exists and is accessible
- For K8s: Verify PVC exists in the specified namespace

**Sync Not Working**

- Ensure `analytics.sync.enabled: true`
- Verify at least one strategy is listed in `analytics.sync.strategies`
- For batch sync: Verify interval and start_time are valid
- For async sync: Verify broker is accessible

**Webhook Delivery Not Working**

- Check webhook URL is accessible
- Verify retry settings allow sufficient retry attempts
- Check webhook delivery history for error details
