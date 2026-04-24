# DuckDB Analytics Migration Plan - Aegion (Beta Branch)

## Overview
Migrate Aegion's analytics layer to DuckDB while maintaining PostgreSQL for core operations. Build a complete, production-ready analytics system with flexible configuration, multiple storage backends, multiple API layers (REST/GraphQL/gRPC), configurable data retention, real-time dashboards with webhooks, and comprehensive test coverage.

**Branch:** beta  
**Scope:** Full end-to-end implementation  
**Cadence:** Commit and push after every major change to origin/beta  
**Commit Strategy:** Semantic messages (fix:, feat:, docs:, chore:, refactor:, security:)

---

## Current Status (as of 2026-04-24)

This plan now tracks the *actual* `beta` branch state (not just intentions).

- **Last verified head:** `3cfc31e`
- **Verified working slices (tests passing):**
  - `modules/analytics/{dashboards,rest,graphql,grpc,integration,e2e,store,retention,sync,webhooks}`
  - `internal/proto/analytics` exists (gRPC contract alignment for `modules/analytics/grpc`)
  - `modules/admin/spa` builds (`npm run build`)
- **Recent milestones completed (beta):**
  - gRPC/proto alignment: `57c426d`
  - Test hygiene / explicit skips: `2ed9522`
  - REST validation + query hardening: `c8a8432`
  - CI module matrix includes analytics: `6d84682`
  - REST validator micro-perf cleanup: `7715408`
  - Retention SQL portability + FK schema fix: `33797b1`
  - Retention sqlite-backed unit tests (no longer skipped) + archival correctness fixes: `5d01903`
  - Webhooks store unit coverage expansion: `3cfc31e`
- **Remaining roadmap focus (not blockers):**
  - Reduce remaining placeholder logic (REST dashboard/query persistence, GraphQL auth)
  - Improve performance and simplify implementations once correctness/CI is stable
- **QA / regression:** see `docs/analytics/qa.md`
- **Verification commands:**
  - `go test ./modules/analytics/grpc ./modules/analytics/dashboards ./modules/analytics/rest ./modules/analytics/graphql ./modules/analytics/integration ./modules/analytics/e2e ./modules/analytics/store ./modules/analytics/retention ./modules/analytics/sync ./modules/analytics/webhooks`
  - `npm run build` (in `modules/admin/spa`)
  - `go test ./modules/analytics/grpc`

## Architecture Overview

```
┌─────────────────────────────────────────────────────────────────┐
│                    Aegion Analytics Layer                       │
├─────────────────────────────────────────────────────────────────┤
│                                                                   │
│  ┌─────────────┐      ┌──────────────┐      ┌──────────────┐   │
│  │  REST API   │      │ GraphQL API  │      │  gRPC API    │   │
│  └──────┬──────┘      └──────┬───────┘      └──────┬───────┘   │
│         │                    │                      │            │
│         └────────────────────┼──────────────────────┘            │
│                              │                                   │
│                      ┌───────▼────────┐                          │
│                      │ Analytics Core │                          │
│                      │  (Query Engine)│                          │
│                      └───────┬────────┘                          │
│                              │                                   │
│         ┌────────────────────┼────────────────────┐              │
│         │                    │                    │              │
│    ┌────▼─────┐    ┌────────▼────────┐  ┌───────▼──────┐       │
│    │  DuckDB  │    │ Data Sync Layer │  │ Webhook Mgr  │       │
│    │(Hot/Warm)│    │(Real/Batch/Async)  │ (Pub/Sub)    │       │
│    └────┬─────┘    └────────┬────────┘  └───────┬──────┘       │
│         │                   │                    │              │
│    ┌────▼──────────────────▼──────────┬─────────▼──────┐       │
│    │         Storage Backends         │  Event Stream  │       │
│    │ • Local FS                       │  (KafkaRPC)    │       │
│    │ • S3 + Apache Iceberg            │                │       │
│    │ • Cold storage (Archive)         │                │       │
│    │ • K8s persistent volumes         │                │       │
│    └────────────────────────────────┬─┴────────────────┘       │
│                                      │                          │
│                           ┌──────────▼──────┐                   │
│                           │ PostgreSQL      │                   │
│                           │ (Source events) │                   │
│                           └─────────────────┘                   │
│                                                                   │
└─────────────────────────────────────────────────────────────────┘
```

---

## Data Flow & Sync Strategies

### 1. Real-time Sync (via CDC/Triggers)
- PostgreSQL triggers publish events to DuckDB
- Immediate consistency for critical events
- Configuration option: `analytics.sync.real_time.enabled`

### 2. Batch Sync (Scheduled)
- Scheduled jobs (hourly/daily/weekly/custom)
- Bulk insert from Postgres to DuckDB
- Configuration option: `analytics.sync.batch.interval`

### 3. Asynchronous Queue (Message Broker)
- Events published to Kafka/RabbitMQ-like stream
- Workers consume and write to DuckDB
- Configuration option: `analytics.sync.async.broker`

### 4. Hybrid Mode
- Real-time for high-priority events
- Batch/async for non-critical data
- Automatic fallback on failure

---

## Data Retention & Storage Tiers

### Hot Storage
- Recent data (configurable, default: last 7 days)
- Local DuckDB for fast queries
- Full query performance

### Warm Storage
- Medium-term data (default: 7-90 days)
- Compressed storage (S3 or local)
- Slightly slower queries

### Cold Storage
- Archive data (default: >90 days)
- Apache Iceberg + S3 or local
- Infrequent access

### Configuration
```yaml
analytics:
  retention:
    hot:
      enabled: true
      ttl_days: 7
      storage: local
    warm:
      enabled: true
      ttl_days: 90
      storage: s3
      compression: snappy
    cold:
      enabled: true
      storage: s3_iceberg
      archive_format: parquet
  # Per-category overrides
  categories:
    audit_events:
      hot: 30
      warm: 180
      cold: 730
    user_activity:
      hot: 14
      warm: 90
      cold: 365
```

---

## Event Categories to Track

All events stored in DuckDB with categorization:

1. **Authentication Events**
   - Login attempts (success/failure)
   - Logout events
   - Token generation/refresh/revocation
   - MFA/passkey events

2. **User Activity**
   - User creation/update/deletion
   - Profile changes
   - Permission/role changes

3. **Session Metrics**
   - Session creation/termination
   - Session duration
   - Concurrent sessions
   - Device/location tracking

4. **OAuth2 Events**
   - Authorization code flow
   - Token endpoint access
   - Consent events
   - Client access logs

5. **Audit Events**
   - Admin actions
   - Policy changes
   - Configuration updates
   - Security events

6. **System Events**
   - Service startup/shutdown
   - Health checks
   - Error/warning logs
   - Performance metrics

---

## API Endpoints

### REST API (`/api/v1/analytics/`)
- `GET /events` - Query events with filters
- `GET /dashboards` - List configured dashboards
- `GET /dashboards/:id` - Get dashboard data
- `POST /dashboards` - Create custom dashboard
- `GET /reports/:id` - Generate/fetch reports
- `GET /export/:format` - Export analytics (CSV/JSON/Parquet)

### GraphQL API (`/graphql`)
- Query operations for flexible event/metric retrieval
- Subscription support for real-time updates
- Mutations for dashboard/report management

### gRPC API (internal service communication)
- `AnalyticsService.QueryEvents`
- `AnalyticsService.GetDashboard`
- `AnalyticsService.StreamEvents` (bidirectional streaming)
- `AnalyticsService.ExportData`

---

## Pre-built Dashboards

1. **Authentication Dashboard**
   - Login success/failure rates
   - Peak login times
   - Failed auth attempts by reason
   - MFA adoption

2. **User Activity Dashboard**
   - New user signups
   - Active users (DAU/MAU)
   - User lifecycle phases

3. **Session Analytics**
   - Current active sessions
   - Session duration trends
   - Concurrent user peaks

4. **Security Dashboard**
   - Suspicious activities
   - Rate limit violations
   - Policy violation attempts
   - Geographic anomalies

5. **System Health**
   - API latency
   - Error rates
   - Database performance
   - Resource usage

---

## Webhook System

Real-time event notifications:
- Webhook registration API
- Event filtering (by type, category, source)
- Retry mechanism with exponential backoff
- Payload signing (HMAC-SHA256)
- Event history and replay

Example webhook trigger:
```
POST /webhooks/analytics/auth-spike
{
  "timestamp": "2026-04-23T18:47:49Z",
  "event_type": "authentication.failed_spike",
  "threshold": 10,
  "current_rate": 45,
  "duration_seconds": 300
}
```

---

## Configuration Schema (aegion.yaml)

```yaml
modules:
  analytics:
    enabled: true
    
    # Storage configuration
    storage:
      primary:
        type: duckdb  # Required
        path: /data/analytics.duckdb
        memory_limit_mb: 8192
      
      backends:
        local:
          enabled: true
          path: /data/analytics_storage
        s3:
          enabled: false
          bucket: my-analytics
          region: us-east-1
          prefix: analytics/
        iceberg:
          enabled: false
          catalog_type: s3
          warehouse_path: s3://my-analytics/iceberg
    
    # Data sync strategies
    sync:
      real_time:
        enabled: true
        batch_size: 100
        flush_interval_ms: 5000
      batch:
        enabled: true
        interval: "1h"
        start_time: "02:00"
      async:
        enabled: false
        broker: kafka
        topic: analytics-events
        partitions: 3
    
    # Retention policies
    retention:
      default_policy: tiered
      hot:
        ttl_days: 7
        enabled: true
      warm:
        ttl_days: 90
        enabled: true
        compression: snappy
      cold:
        ttl_days: 730
        enabled: true
        archive_type: iceberg
      
      # Category-specific overrides
      categories:
        audit_events:
          hot_days: 30
          warm_days: 180
          cold_days: 730
        authentication:
          hot_days: 14
          warm_days: 60
          cold_days: 365
    
    # API configuration
    api:
      rest:
        enabled: true
        base_path: /api/v1/analytics
        rate_limit: 1000  # per minute
      graphql:
        enabled: true
        endpoint: /graphql
        introspection: true
        playground: true
      grpc:
        enabled: true
        port: 50051
    
    # Dashboard settings
    dashboards:
      auto_refresh_interval_seconds: 30
      max_custom_dashboards: 50
      default_time_range_days: 7
    
    # Webhooks
    webhooks:
      enabled: true
      max_retry: 5
      retry_backoff_seconds: 60
      timeout_seconds: 30
      signature_algorithm: hmac256
    
    # Performance
    performance:
      query_timeout_seconds: 300
      max_concurrent_queries: 50
      enable_query_cache: true
      cache_ttl_minutes: 15
```

---

## Implementation Phases

### Phase 1: Foundation (Data Layer & Storage)
- [x] DuckDB integration module (basic; env-dependent extensions may skip some tests)
- [x] Storage backend abstraction (local, S3, Iceberg, K8s)
- [x] Database schema for analytics (module migrations present under `modules/analytics/migrations/`)
- [ ] Migration system updates (core migrations not yet extended for analytics)
- [ ] Initial configuration in aegion.yaml (spec exists; verify runtime wiring)
- **Commit pattern:** `feat: duckdb analytics foundation`

### Phase 2: Data Sync Layer
- [x] Real-time sync engine (implementation present; CDC/trigger details depend on deployment wiring)
- [x] Batch sync scheduler
- [x] Async queue integration (in-memory broker implemented; external brokers require env)
- [x] Event publishing system
- [x] Sync health monitoring
- **Commit pattern:** `feat: analytics data sync`

### Phase 3: REST API
- [x] Query builder/parser
- [x] Event filtering & aggregation (baseline)
- [x] Export functionality (CSV, JSON, Parquet) (baseline)
- [ ] Rate limiting & auth (needs hardening + real auth middleware integration)
- [x] Pagination & sorting
- **Commit pattern:** `feat: analytics rest api`

### Phase 4: GraphQL API
- [x] Schema definition
- [x] Query resolvers
- [x] Subscription support (basic channels; production readiness requires review)
- [x] Playground setup (config present)
- [ ] Authentication/authorization (needs hardening + real auth context enforcement)
- **Commit pattern:** `feat: analytics graphql api`

### Phase 5: gRPC API
- [x] Proto definitions (`internal/proto/analytics`)
- [x] Service implementation (QueryEvents/GetDashboard/ExportData)
- [x] Streaming support (StreamEvents)
- [x] Interceptors for auth/logging (baseline)
- [x] Unit tests (service + interceptors)
- **Commit pattern:** `fix: align analytics grpc with internal proto contract`

### Phase 6: Retention & Storage Management
- [x] Hot/warm/cold tier management (baseline)
- [x] Automatic archival jobs (scheduler present; env-dependent execution)
- [x] Retention policy enforcement (baseline)
- [x] Apache Iceberg integration (stub backend present; production integration pending)
- [x] S3 backend implementation (requires env)
- **Commit pattern:** `feat: analytics retention & archival`

### Phase 7: Webhook System
- [x] Webhook registration/management (baseline)
- [x] Event filtering
- [x] Signature generation
- [x] Retry mechanism
- [ ] Event history (delivery history persistence depends on store wiring)
- **Commit pattern:** `feat: analytics webhooks`

### Phase 8: Pre-built Dashboards
- [x] Dashboard data models
- [x] Auth dashboard queries (baseline set)
- [x] Activity dashboard queries (baseline set)
- [x] Security dashboard queries (baseline set)
- [x] System health dashboard queries (baseline set)
- [x] Dashboard API endpoints (REST + GraphQL baseline)
- **Commit pattern:** `feat: analytics dashboards`

### Phase 9: Admin SPA Integration
- [x] Analytics configuration UI (baseline screens)
- [x] Dashboard builder
- [ ] Sync strategy selector (screen exists; verify backend wiring + validation UX)
- [x] Retention policy UI
- [x] Event viewer
- [ ] Webhook manager (screen exists; verify backend wiring + delivery history UI)
- **Commit pattern:** `feat: admin spa analytics module`

### Phase 10: Testing Suite
- [x] Unit tests (core slices covered; see `modules/analytics/*/*_test.go`)
- [ ] Integration tests (Postgres ↔ DuckDB) (skipped until env is provided)
- [ ] E2E tests (API workflows) (skipped until env is provided)
- [ ] Performance benchmarks (not yet)
- [ ] Security tests (auth, injection, etc.) (not yet)
- [ ] Load tests (not yet)
- **Commit pattern:** `test: analytics full coverage`

### Phase 11: Documentation
- [ ] OpenAPI schema
- [ ] GraphQL schema documentation
- [ ] Configuration guide (docs/analytics/config.md)
- [ ] API usage guide (docs/analytics/api.md)
- [ ] Architecture guide (docs/analytics/architecture.md)
- [ ] Setup guide (docs/analytics/setup.md)
- [ ] Implementation plan with tickboxes (docs/analytics/plan.md)
- **Commit pattern:** `docs: analytics documentation`

### Phase 12: Security & Production Hardening
- [ ] RBAC for analytics APIs
- [ ] Data encryption at rest/in-transit
- [ ] Audit logging for analytics queries
- [ ] Rate limiting per user/tenant
- [ ] Query validation & sanitization
- [ ] Secrets management
- **Commit pattern:** `security: analytics hardening`

### Phase 13: CI/CD & Deployment
- [ ] GitHub Actions workflow updates
- [ ] Migration testing in CI
- [ ] Docker image updates
- [ ] Kubernetes manifests
- [ ] Health check endpoints
- **Commit pattern:** `chore: analytics ci/cd setup`

### Phase 14: Performance Optimization
- [ ] Query optimization
- [ ] Index creation
- [ ] Caching strategies
- [ ] Memory management
- [ ] Benchmarking & profiling
- **Commit pattern:** `perf: analytics optimization`

### Phase 15: Bug Fixes & Cleanup
- [ ] Known issue resolution
- [ ] Edge case handling
- [ ] Code cleanup
- [ ] Technical debt resolution
- **Commit pattern:** `fix: analytics issues`

---

## Database Schema for Analytics

### Core Tables (DuckDB)

```sql
-- Events table (immutable)
CREATE TABLE analytics_events (
    id UUID PRIMARY KEY,
    event_type VARCHAR NOT NULL,
    category VARCHAR NOT NULL,
    source_system VARCHAR,
    timestamp TIMESTAMP NOT NULL,
    data JSON,
    storage_tier VARCHAR DEFAULT 'hot',  -- hot, warm, cold
    created_at TIMESTAMP DEFAULT now(),
    archived_at TIMESTAMP NULL
) PARTITION BY (MONTH, category);

-- Metrics aggregates
CREATE TABLE analytics_metrics (
    id UUID PRIMARY KEY,
    metric_name VARCHAR NOT NULL,
    category VARCHAR NOT NULL,
    time_bucket TIMESTAMP NOT NULL,
    value DOUBLE NOT NULL,
    dimensions JSON,
    created_at TIMESTAMP DEFAULT now()
) PARTITION BY (MONTH);

-- Dashboard definitions
CREATE TABLE analytics_dashboards (
    id UUID PRIMARY KEY,
    name VARCHAR NOT NULL,
    category VARCHAR,
    definition JSON NOT NULL,
    is_default BOOLEAN DEFAULT false,
    created_by UUID,
    created_at TIMESTAMP DEFAULT now(),
    updated_at TIMESTAMP DEFAULT now()
);

-- Custom queries
CREATE TABLE analytics_queries (
    id UUID PRIMARY KEY,
    name VARCHAR NOT NULL,
    sql_query TEXT NOT NULL,
    description TEXT,
    created_by UUID,
    is_public BOOLEAN DEFAULT false,
    created_at TIMESTAMP DEFAULT now()
);

-- Webhooks
CREATE TABLE analytics_webhooks (
    id UUID PRIMARY KEY,
    url VARCHAR NOT NULL,
    events JSON NOT NULL,  -- array of event types
    secret VARCHAR NOT NULL,
    is_active BOOLEAN DEFAULT true,
    created_by UUID,
    created_at TIMESTAMP DEFAULT now()
);
```

---

## Migration Strategy

### On Beta Branch:
1. **Delete existing analytics tables** (fresh start - no data yet)
2. **Update migration files** (overwrite, don't create new drop migrations)
3. **Add DuckDB migrations** in `core/migrations/0009_analytics_schema.up.sql`
4. **Test locally** before pushing

### Migration File Structure:
```
core/migrations/
├── 0009_analytics_schema.up.sql       (Create DuckDB schema)
├── 0009_analytics_schema.down.sql     (Drop DuckDB schema)
modules/analytics/
├── migrations/0001_analytics_base.up.sql
├── migrations/0001_analytics_base.down.sql
└── ...
```

---

## Commit Strategy

Every major feature/component → commit and push:

```bash
# After Phase 1
git add .
git commit -m "feat: add duckdb analytics foundation with storage backends"
git push origin beta

# After Phase 2
git add .
git commit -m "feat: implement data sync layer (real-time, batch, async)"
git push origin beta

# Bug fixes during development
git add .
git commit -m "fix: analytics sync race condition"
git push origin beta

# Documentation updates
git add .
git commit -m "docs: add analytics configuration guide"
git push origin beta

# Refactoring
git add .
git commit -m "refactor: consolidate analytics query logic"
git push origin beta

# Security updates
git add .
git commit -m "security: add RBAC for analytics apis"
git push origin beta
```

---

## Testing Strategy

### Unit Tests
- [ ] DuckDB connection management
- [ ] Query builders
- [ ] Data formatting/validation
- [ ] Retention policy calculations
- [ ] Event categorization

### Integration Tests
- [ ] Postgres → DuckDB sync
- [ ] Real-time event publishing
- [ ] Batch job execution
- [ ] Webhook delivery
- [ ] Dashboard data fetching

### E2E Tests
- [ ] REST API workflows (query → export)
- [ ] GraphQL subscription lifecycle
- [ ] Dashboard creation → data population
- [ ] Multi-tier retention workflow
- [ ] Failure & recovery scenarios

### Performance Tests
- [ ] 1M event ingestion
- [ ] Query latency under load
- [ ] Concurrent API requests
- [ ] Memory usage profiling
- [ ] Storage tier transitions

### Security Tests
- [ ] SQL injection prevention
- [ ] Unauthorized API access
- [ ] Webhook signature validation
- [ ] Rate limiting enforcement
- [ ] Data isolation (multi-tenant scenarios)

---

## Documentation Deliverables

All in `docs/analytics/`:

1. **plan.md** - Tickbox implementation plan (this file becomes the source)
2. **architecture.md** - System design, data flows, component descriptions
3. **config.md** - Complete configuration reference for aegion.yaml
4. **api.md** - REST API reference and examples
5. **graphql-schema.md** - GraphQL schema with examples
6. **setup.md** - Installation and local development guide
7. **openapi.yaml** - OpenAPI 3.0 specification
8. **graphql.schema** - GraphQL schema in standard format

---

## Success Criteria (Production Ready)

- ✅ All phases 1-15 completed
- ✅ Unit test coverage >85%
- ✅ Integration tests all passing
- ✅ E2E tests for core workflows passing
- ✅ Performance benchmarks meet targets
- ✅ Security audit passed (no critical vulnerabilities)
- ✅ Documentation complete and reviewed
- ✅ Zero race conditions in sync layer
- ✅ All configurations working in aegion.yaml
- ✅ Admin SPA fully functional
- ✅ CI/CD pipeline green
- ✅ Load tested to expected QPS

---

## Risk Mitigation

| Risk | Mitigation |
|------|-----------|
| DuckDB performance at scale | Early performance testing, query optimization, indexing strategy |
| Data consistency Postgres ↔ DuckDB | Dual-write validation, sync verification jobs, reconciliation |
| Storage backend failures | Multi-backend support, fallback mechanisms, health checks |
| Webhook delivery reliability | Retry logic, DLQ, monitoring, alerting |
| Configuration complexity | Admin SPA simplifies, sensible defaults, clear documentation |

---

## Timeline Notes

- No time estimates provided (long-running project)
- Prioritize by phase order but remain flexible
- Each commit should be a working, testable state
- Push frequently to origin/beta for team visibility
- Use semantic commits consistently
- Document decisions and rationale in commit messages

---

*Plan created: 2026-04-23*  
*Status: Ready for implementation*  
*Branch: beta*  
*Cadence: Continuous (commit after each major change)*
