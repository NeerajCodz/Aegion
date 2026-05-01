# Analytics System Architecture

**Version:** 1.0  
**Last Updated:** 2026-04-24  
**Module:** `modules/analytics`

## Overview

The Aegion analytics system is a modern, distributed analytics platform designed for real-time event processing, complex queries, and rich visualization. It decouples data ingestion from analytics querying through a sync layer and specialized storage backends.

```
┌─────────────────────────────────────────────────────────────────────┐
│                         Aegion Core                                  │
│  (User actions, system events, audit logs)                           │
└────────────────────────┬────────────────────────────────────────────┘
                         │
                         │ Events & Data
                         ▼
        ┌────────────────────────────────┐
        │    PostgreSQL Database         │
        │  (System of Record - SoR)      │
        └────────────┬───────────────────┘
                     │
        ┌────────────▼───────────────────┐
        │      Sync Layer               │
        │  Real-time CDC/Triggers        │
        │  Batch Sync (scheduled)        │
        │  Async Queue                   │
        └────────────┬───────────────────┘
                     │
    ┌────────────────┼────────────────────┐
    │                │                    │
    ▼                ▼                    ▼
┌─────────────┐ ┌─────────────┐ ┌─────────────┐
│  DuckDB     │ │  S3/Ice..   │ │  K8s/Local  │
│  (Hot)      │ │  (Warm)     │ │  (Cold)     │
└─────────────┘ └─────────────┘ └─────────────┘
    │
    │ Query Results
    ▼
┌──────────────────────────┐
│    API Layer             │
│ ┌──────────────────────┐ │
│ │ REST API (/api/...)  │ │
│ │ GraphQL (/graphql)   │ │
│ │ gRPC (port 50051)    │ │
│ └──────────────────────┘ │
└──────┬─────────────┬─────┘
       │             │
       ▼             ▼
  ┌────────────┐  ┌──────────┐
  │ Webhooks   │  │ Admin SPA │
  └────────────┘  └──────────┘
```

---

## Core Components

### 1. PostgreSQL (Source of Record)

**Role:** System of Record for all Aegion data
- User activities, events, audit logs
- All mutations go here first
- ACID guarantees, full transaction support

**Connection:** Configured in `aegion.yaml` → `analytics.sync.postgres`

### 2. Sync Layer

The sync layer is the heart of the analytics system. It continuously mirrors data from PostgreSQL to DuckDB using three complementary strategies:

#### Real-time Sync
- **Mechanism:** PostgreSQL CDC (Change Data Capture) / triggers
- **Latency:** < 100ms
- **Use Case:** Dashboard updates, live dashboards, webhooks
- **Configuration:** `analytics.sync.enable_real_time`

```go
// Real-time sync monitors PostgreSQL for changes and immediately
// reflects them in DuckDB. Uses WAL (Write-Ahead Log) when available.
type RealTimeSync struct {
  Enabled bool
  BatchSize int       // Batch CDC records before writing
  MaxLatency time.Duration
}
```

#### Batch Sync
- **Mechanism:** Periodic scheduled bulk copy
- **Latency:** Configurable (default 5 minutes)
- **Use Case:** Full reconciliation, handling missed events
- **Configuration:** `analytics.sync.enable_batch`

```go
type BatchSync struct {
  Enabled bool
  Schedule string      // Cron expression: "*/5 * * * *"
  BatchSize int        // Records per batch (default 10000)
  Parallel int         // Concurrent sync workers
}
```

#### Async Sync
- **Mechanism:** Asynchronous queue-based processing
- **Latency:** Minutes to hours
- **Use Case:** Heavy computations, archive sync, retry logic
- **Configuration:** `analytics.sync.enable_async`

```go
type AsyncSync struct {
  Enabled bool
  QueueDepth int
  WorkerCount int
  RetryPolicy RetryPolicy  // exponential backoff
}
```

#### Hybrid Sync Strategy
Combines all three for maximum reliability:
- Real-time for immediate consistency
- Batch for scheduled reconciliation
- Async for complex transformations

### 3. DuckDB (Hot Storage)

**Role:** Primary analytical query engine
- In-process columnar database
- Optimized for OLAP queries
- Sub-second query response
- Local file or in-memory

**Configuration:**
```yaml
analytics:
  duckdb:
    data_dir: "./data/duckdb"
    threads: 4
    memory_limit_gb: 8
    connection_pool_size: 20
```

**Schema:**
- `events` - Event records
- `dashboards` - Dashboard definitions
- `analytics_queries` - Saved queries
- `webhooks` - Webhook configurations
- `reports` - Generated reports
- `metrics` - System metrics

### 4. Storage Backends

Multi-tier storage for cost and performance optimization:

#### Local Storage (Hot)
- **TTL:** 24 hours - 7 days
- **Use:** Real-time dashboards, recent events
- **Speed:** Microseconds
- **Cost:** High per GB

#### S3/Object Storage (Warm)
- **TTL:** 1 - 90 days
- **Use:** Medium-term analytics, weekly reports
- **Speed:** Milliseconds
- **Cost:** Medium per GB

#### Iceberg (Archive)
- **TTL:** 90+ days
- **Use:** Compliance, long-term trends
- **Speed:** Seconds
- **Cost:** Low per GB

#### Kubernetes Storage (Cold)
- **TTL:** Indefinite
- **Use:** Long-term compliance archive
- **Speed:** Minutes (network-dependent)
- **Cost:** Variable

**Automatic Tiering:**
```
Ingestion → DuckDB (hot) → S3 (warm after 7d) → Iceberg (archive after 90d)
```

See [performance.md](./performance.md) for tuning details.

### 5. API Layer

Three APIs serving different use cases:

#### REST API
- HTTP/JSON endpoint: `/api/v1/analytics`
- CRUD operations for dashboards, queries, webhooks
- Standard for web/mobile clients
- Built with Go chi router

#### GraphQL API
- HTTP endpoint: `/api/v1/analytics/graphql`
- Flexible query language
- Real-time subscriptions
- Ideal for admin SPA

#### gRPC API
- Binary protocol on port 50051
- High-performance service-to-service
- Strict type safety with protobuf
- Streaming support

### 6. Admin SPA

React-based administration interface:
- Configuration management
- Dashboard builder
- Event viewer
- Query executor
- Webhook management
- Health monitoring

Located in: `modules/admin/spa/src/components/Analytics`

---

## Data Flow

### Ingestion Flow

```
1. User Action (Aegion Core)
   └─> PostgreSQL (INSERT into events)
       └─> PostgreSQL Triggers
           └─> Sync Layer (CDC)
               ├─> Real-time: Immediate DuckDB update
               ├─> Batch: Queued for periodic sync
               └─> Async: Queued for deferred processing
```

### Query Flow

```
1. API Request (REST/GraphQL/gRPC)
   └─> Authentication & Authorization
       └─> Query Parser & Validator
           └─> DuckDB Query Engine
               └─> LRU Cache Check
                   ├─> Cache Hit: Return cached result
                   └─> Cache Miss: Execute query
                       ├─> Hot (DuckDB): Sub-second
                       ├─> Warm (S3): Seconds
                       └─> Cold (Iceberg): Minutes
                   └─> Cache & Return to Client
```

### Webhook Flow

```
1. Event matches webhook filter
   └─> Create WebhookEventPayload
       └─> Queue delivery task
           └─> HTTP POST to webhook URL
               ├─> Success (2xx): Mark delivered
               └─> Failure: Retry logic
                   ├─> Retry 1: 5 seconds
                   ├─> Retry 2: 30 seconds
                   ├─> Retry 3: 5 minutes
                   └─> Max retries exceeded: Dead Letter Queue
```

---

## Security Architecture

### Authentication & Authorization

```
Request
  ├─> Extract token/session
  │   └─> Validate token signature
  │       └─> Check token expiry
  │           └─> Extract user identity
  │
  └─> RBAC Check
      └─> Verify user role (admin/analyst/viewer)
          └─> Check resource ownership
              └─> Enforce query permissions
```

### Query Validation

```
Raw Query
  ├─> Parse SQL/GraphQL
  │   └─> Validate syntax
  │       └─> Check for injection patterns
  │           └─> Resolve table/column references
  │               └─> Validate user access to tables/columns
  │
  └─> Approved Query
      └─> Execute with resource limits
```

### Encryption

- **In Transit:** TLS 1.3 (all APIs)
- **At Rest:** Optional field-level encryption for sensitive columns
- **Keys:** Managed via Aegion key management

---

## Performance Characteristics

### Query Performance

| Query Type | Typical Time | Limit |
|-----------|--------------|-------|
| Simple aggregation | 10-100ms | 1M rows |
| Complex join | 100ms-1s | 100k rows |
| Full scan | 1-10s | 10M rows |
| Archive scan (Iceberg) | 10s-60s | Unlimited |

### Throughput

- **Ingestion:** 10k+ events/second
- **Queries:** 100+ concurrent users
- **Webhooks:** 1000+ deliveries/second

### Storage

- **Hot (DuckDB):** 100GB typical
- **Warm (S3):** Unlimited, pay-per-use
- **Archive (Iceberg):** Unlimited, compliance-grade

---

## Deployment Architecture

### Local Development
```
Docker Compose
├─> Aegion Core (Go)
├─> PostgreSQL 14+
├─> DuckDB (embedded)
└─> Admin SPA (React)
```

### Docker
```
Docker Deployment
├─> Aegion Container
│   ├─> Core service
│   ├─> Analytics module
│   └─> Admin SPA
├─> PostgreSQL Container
└─> DuckDB Data Volume
```

### Kubernetes
```
Kubernetes Cluster
├─> Aegion Service (Deployment)
│   ├─> Core Pod
│   ├─> Analytics Pod
│   └─> Admin SPA Pod
├─> PostgreSQL Service (StatefulSet)
├─> DuckDB PVC (Persistent Volume)
└─> Webhooks Queue (Message Broker)
```

See [setup.md](./setup.md) for detailed deployment instructions.

---

## Scaling Strategy

### Horizontal Scaling

1. **Read Scaling:** Multiple API instances sharing DuckDB replica
2. **Write Scaling:** Queue-based sync allows independent scalability
3. **Storage Scaling:** S3/Iceberg provide unlimited capacity

### Vertical Scaling

1. **DuckDB Threads:** Increase `duckdb.threads` for parallel queries
2. **Memory:** Increase `duckdb.memory_limit_gb` for larger working set
3. **Connection Pool:** Increase `rest.connection_pool_size` for more concurrent clients

### State Management

- **Shared State:** PostgreSQL (single source of truth)
- **Local State:** DuckDB cache on each instance (eventual consistency)
- **Session State:** Redis or server sessions (configurable)

---

## Failure Modes & Recovery

### DuckDB Unavailable
- Real-time sync queues events
- Batch sync reconciles on recovery
- Webhooks delivered when available

### PostgreSQL Unavailable
- Analytics continue serving cached data
- Sync paused until recovery
- Writes rejected

### Sync Lag
- Configurable thresholds
- Real-time CDC provides immediate visibility
- Batch sync provides eventual consistency

### Webhook Delivery Failure
- Exponential backoff retry: 5s, 30s, 5min
- After max retries, moved to Dead Letter Queue
- DLQ events available for replay/investigation

---

## Monitoring & Observability

### Key Metrics

```
Sync Latency
├─> Real-time lag: < 100ms
├─> Batch lag: < 5 minutes
└─> Async lag: Variable

Query Performance
├─> P50: 50ms
├─> P95: 500ms
└─> P99: 2s

API Metrics
├─> Requests/sec: Target > 1000
├─> Error rate: Target < 0.1%
└─> Latency: P99 < 2s

Storage
├─> Hot utilization: Track growth
├─> Tiering: Track bytes moved to warm/cold
└─> Query cost: Track expensive scans
```

### Logging

- Request/response logging: All APIs
- Query logging: Slow queries (configurable threshold)
- Sync events: Real-time, batch, async
- Error logging: All exceptions with stack traces
- Audit logging: User actions, config changes

See [troubleshooting.md](./troubleshooting.md) for debugging.

---

## Related Documentation

- [API Reference](./api.md)
- [Setup Guide](./setup.md)
- [Security Model](./security.md)
- [Performance Tuning](./performance.md)
- [Integration Guide](./integration.md)
