# Analytics Retention & Storage Management

This module implements comprehensive data retention policies with hot/warm/cold tiering and automatic archival for the Aegion analytics system.

## Overview

The retention module provides:

- **Tiered Storage System**: Automatically moves data between hot (DuckDB), warm (S3), and cold (Iceberg) storage tiers based on age
- **Configurable Retention Policies**: Define TTLs per tier and category
- **Automated Archival**: Background jobs handle data movement between tiers
- **Cleanup & Garbage Collection**: Remove expired data and orphan records
- **Audit Logging**: Immutable log of all retention operations
- **Cost Optimization**: Tier data by access patterns (hot=frequent, warm=occasional, cold=archive)

## Architecture

### Storage Tiers

1. **Hot Tier (DuckDB)**
   - Duration: 0-7 days (default)
   - Storage: Local DuckDB instance
   - Use case: Real-time analytics, current events
   - Performance: Full query performance
   - Cost: Highest (~$0.023/GB/month)

2. **Warm Tier (S3)**
   - Duration: 7-90 days (default)
   - Storage: AWS S3 with compression
   - Use case: Recent historical data, trend analysis
   - Performance: ~90% cheaper than hot, queryable via S3 Select
   - Cost: Mid-range (~$0.025/GB/month)
   - Compression: Snappy, gzip, or zstd

3. **Cold Tier (Apache Iceberg)**
   - Duration: 90+ days (default, up to 730+ days)
   - Storage: S3 with Iceberg format
   - Use case: Long-term archive, compliance
   - Performance: Bulk export capability, slow queries
   - Cost: Lowest (~$0.004/GB/month for Glacier)

### Components

#### Policy Engine (`policy.go`)
- Manages retention policy definitions
- Determines which tier data belongs in
- Validates TTL configurations
- Supports category-specific overrides

#### Archival Executor (`archival.go`)
- Moves data between tiers
- Batch processing for efficiency
- Checksums for data integrity
- Automatic retry on failure
- All-or-nothing transaction support

#### Tiering Engine (`tiering.go`)
- Transitions data between consecutive tiers
- Tracks tier transition history
- Provides data distribution metrics
- Cost estimation per tier
- Non-blocking background operations

#### Cleanup Manager (`cleanup.go`)
- Removes expired data from cold storage
- Soft-delete cleanup (grace period)
- Orphan record detection and repair
- Gradual deletion to avoid locking

#### Job Scheduler (`scheduler.go`)
- Orchestrates scheduled retention jobs
- Configurable intervals for archival, tiering, cleanup
- Job history tracking
- Error handling and notifications

#### Audit Log (`audit.go`)
- Immutable operation logging
- Tracks all archival, tiering, cleanup operations
- Metadata capture (row counts, bytes moved, checksums)
- Compliance-ready audit trail

#### Manager (`manager.go`)
- Central orchestration point
- Initializes all components
- Provides public API for retention operations
- Status and health monitoring

## Configuration

### YAML Configuration

```yaml
analytics:
  retention:
    enabled: true
    default_policy: tiered
    hot_ttl_days: 7
    warm_ttl_days: 90
    cold_ttl_days: 730
    archival_interval: 24h
    cleanup_interval: 168h          # 7 days
    tiering_interval: 6h
    
    categories:
      audit_events:
        hot_days: 30
        warm_days: 180
        cold_days: 730
      authentication:
        hot_days: 14
        warm_days: 60
        cold_days: 365
```

### Programmatic Configuration

```go
policy := &retention.RetentionPolicy{
    DefaultPolicy: "tiered",
    Hot: retention.TierConfig{
        TTLDays:     7,
        Enabled:     true,
        Storage:     "local",
    },
    Warm: retention.TierConfig{
        TTLDays:     90,
        Enabled:     true,
        Storage:     "s3",
        Compression: retention.CompressionSnappy,
    },
    Cold: retention.TierConfig{
        TTLDays:     730,
        Enabled:     true,
        Storage:     "s3_iceberg",
    },
    Categories: map[string]retention.CategoryRetention{
        "audit_events": {
            HotDays:  30,
            WarmDays: 180,
            ColdDays: 730,
        },
    },
}

manager := retention.NewManager(db, policy)
if err := manager.Initialize(ctx); err != nil {
    log.Fatal(err)
}
```

## Usage

### Initialize the Manager

```go
// Create and initialize manager
manager := retention.NewManager(db, policy)
if err := manager.Initialize(ctx); err != nil {
    return err
}

// Register storage backends
manager.RegisterStorageBackend(retention.TierWarm, s3Backend)
manager.RegisterStorageBackend(retention.TierCold, icebergBackend)

// Start the scheduler for automated jobs
if err := manager.StartScheduler(ctx); err != nil {
    return err
}

// Defer shutdown
defer manager.Close(ctx)
```

### Manual Archival

```go
// Archive a specific category
job, err := manager.ArchiveCategory(ctx, "audit_events")
if err != nil {
    log.Printf("archival failed: %v", err)
} else {
    log.Printf("archived %d rows, %d bytes", job.RowCount, job.BytesTransferred)
}
```

### Tier Transitions

```go
// Manually trigger tier transitions
transition, err := manager.TransitionCategory(ctx, "sessions")
if err != nil {
    return err
}
log.Printf("transitioned %d rows", transition.RowsAffected)
```

### Cleanup Operations

```go
// Cleanup expired data
job, err := manager.CleanupCategory(ctx, "audit_events")
if err != nil {
    return err
}
log.Printf("deleted %d expired records", job.RowsDeleted)
```

### Monitoring

```go
// Get tier metrics
metrics, err := manager.GetTierMetrics(ctx, "audit_events")
for _, m := range metrics {
    fmt.Printf("Tier: %s, Rows: %d, Size: %.2fGB, Cost: $%.2f/month\n",
        m.Tier, m.RowCount, m.EstimatedSizeGB, m.EstimatedMonthlyCost)
}

// Get cleanup statistics
stats, err := manager.GetCleanupStats(ctx, "audit_events")
fmt.Printf("Orphan records: %d, Soft-deleted: %d, Expired: %d\n",
    stats.OrphanRecords, stats.SoftDeletedRecords, stats.ExpiredRecords)

// Get audit logs
logs, err := manager.GetAuditLogs(ctx, "ArchivalExecutor", 50)
for _, log := range logs {
    fmt.Printf("%s: %s\n", log.Timestamp, log.Details)
}

// Check manager status
status := manager.GetStatus()
fmt.Printf("Manager initialized: %v, Scheduler running: %v\n",
    status.Initialized, status.SchedulerRunning)
```

## Data Flow

### Archival Process

1. **Scan Phase**: Identify rows past tier TTL
   ```
   SELECT * FROM analytics_events 
   WHERE created_at < (NOW - hot_ttl_days)
   AND tier = 'hot'
   ```

2. **Transform Phase**: Convert to target format (Parquet/Iceberg)
   - Calculate checksum for integrity verification
   - Apply compression if configured

3. **Transfer Phase**: Upload to target storage
   - Write to S3 or Iceberg storage
   - Update metadata (archive_path, archived_at)

4. **Verify Phase**: Confirm data in destination
   - Query target storage
   - Compare row counts and checksums
   - Handle mismatches with retry

5. **Cleanup Phase**: Soft-delete from source
   - Mark with deleted_at timestamp
   - Retain for grace period (optional)
   - Hard-delete after confirmation

### Tier Transition

```
Hot (DuckDB) <-- 7 days old
    |
    v
Warm (S3) <-- 90 days old
    |
    v
Cold (Iceberg) <-- 730 days old
    |
    v
Deleted
```

## Performance Considerations

### Batch Processing
- Default batch size: 1,000 rows
- Configurable per operation
- Reduces memory usage and lock contention

### Non-Blocking Operations
- Archival and cleanup run in background
- Don't block query execution
- Graceful degradation if backend fails

### Database Indices
- Automatically created on:
  - `analytics_events(category, tier)`
  - `analytics_events(created_at DESC)`
  - Tier transition and cleanup job tables

### Query Optimization
- Data distribution analysis shows tier occupancy
- Cost estimates help optimize tier TTLs
- Suggests hot/warm/cold split for cost savings

## Cost Optimization

Typical monthly costs (per GB):
- **Hot**: $0.023 (DuckDB local)
- **Warm**: $0.025 (S3 Standard)
- **Cold**: $0.004 (S3 Glacier Deep Archive)

For example, 100GB of data:
- All hot: $2.30/month
- Split 7d hot + 90d warm + cold: ~$0.30-0.50/month (78% savings)

## Failure Handling

### Archival Failures
- Automatic retry with exponential backoff (1s, 2s, 4s)
- Max 3 retries per job
- Failed operations logged with full context

### Tier Transition Failures
- Logged but don't block subsequent tiers
- Can be retried manually or via scheduler
- Data remains in current tier if transition fails

### Cleanup Failures
- Graceful batch-based deletion to avoid locking
- Failed batches can be retried
- Soft-deleted records remain safe with deleted_at timestamp

### Storage Backend Failures
- Operations fail fast with clear errors
- Log full context for debugging
- Scheduler continues on individual operation failure

## Testing

Run unit tests:
```bash
go test ./modules/analytics/retention -v
```

Tests cover:
- ✅ Policy validation and TTL calculations
- ✅ Tier determination based on record age
- ✅ Data expiration checks
- ✅ Category-specific overrides
- ✅ Scheduler start/stop
- ✅ Manager lifecycle

Integration tests require database driver; use `-skip` flag if needed.

## Audit Trail

All retention operations are logged immutably:

```go
type AuditLogEntry struct {
    ID         string    // Unique identifier
    Timestamp  time.Time // When it happened
    Component  string    // Which component (ArchivalExecutor, etc.)
    Operation  string    // What operation (Archive, TierTransition, etc.)
    Details    string    // Human-readable details
    Metadata   map[string]interface{} // Structured data
}
```

Example log entry:
```json
{
  "id": "audit_arch_1234567890",
  "timestamp": "2024-01-15T02:30:45Z",
  "component": "ArchivalExecutor",
  "operation": "Archive",
  "details": "Archived 1500 rows from hot to warm (status: completed, error: )",
  "metadata": {
    "job_id": "arch_audit_events_1234567890",
    "category": "audit_events",
    "source_tier": "hot",
    "target_tier": "warm",
    "row_count": 1500,
    "bytes_transferred": 5242880,
    "checksum": "abc123def456..."
  }
}
```

## Integration with Analytics APIs

The retention module integrates transparently:

1. **Query Layer**: Automatically spans hot/warm/cold tiers
2. **REST API**: New endpoints for retention status and operations
3. **GraphQL API**: Schema includes retention mutations and queries
4. **gRPC API**: Methods for archival, tiering, cleanup operations

Example REST endpoints:
- `GET /api/v1/analytics/retention/status` - Manager status
- `GET /api/v1/analytics/retention/tiers/:category` - Tier metrics
- `POST /api/v1/analytics/retention/cleanup/:category` - Trigger cleanup
- `GET /api/v1/analytics/retention/audit-log` - Audit log entries

## Best Practices

1. **TTL Strategy**
   - Hot: 7-30 days (frequently accessed)
   - Warm: 30-180 days (occasional access)
   - Cold: 180-730+ days (compliance/archive)

2. **Category Configuration**
   - Audit events: longer hot window (30d) for compliance
   - Session data: shorter hot window (7d), minimal warm (30d)
   - Error logs: similar to audit events

3. **Scheduling**
   - Archival: Daily at off-peak hours (e.g., 2 AM)
   - Tiering: Every 6 hours to balance performance
   - Cleanup: Weekly to avoid excessive locking

4. **Monitoring**
   - Track tier occupancy trends
   - Monitor archival job success rates
   - Alert on failed operations
   - Review cost estimates monthly

5. **Storage Backend Selection**
   - Local filesystem: Development/testing
   - S3: Production warm tier
   - Iceberg: Production cold tier with analytics
   - Glacier: Long-term archive (lowest cost)

## Troubleshooting

### Data Not Moving Between Tiers
1. Check retention policy is enabled
2. Verify storage backends are registered
3. Check data timestamps and TTLs
4. Review audit log for errors

### High Archival Job Duration
1. Reduce batch size if locking occurs
2. Check storage backend performance
3. Increase scheduler interval to reduce frequency
4. Monitor database load

### Orphan Records
1. Run FindOrphanRecords to detect
2. Check archive_path column for NULL values
3. Call RepairOrphanRecords to fix
4. Investigate storage backend for missing files

### Checksum Mismatches
1. Indicates data corruption
2. Retry archival operation
3. Check storage backend integrity
4. Review error logs for detailed context
