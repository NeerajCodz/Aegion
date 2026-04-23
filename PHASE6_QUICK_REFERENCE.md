# Phase 6 Quick Reference - Retention & Archival

## Quick Start

### Initialize Manager
```go
import "github.com/aegion/aegion/modules/analytics/retention"

// Create manager
policy := retention.DefaultRetentionPolicy()
manager := retention.NewManager(db, policy)

// Initialize
if err := manager.Initialize(ctx); err != nil {
    log.Fatal(err)
}

// Register storage backends
manager.RegisterStorageBackend(retention.TierWarm, s3Backend)
manager.RegisterStorageBackend(retention.TierCold, icebergBackend)

// Start automated scheduling
if err := manager.StartScheduler(ctx); err != nil {
    log.Fatal(err)
}

// Cleanup on shutdown
defer manager.Close(ctx)
```

## Configuration

### Default TTLs
- **Hot**: 7 days (DuckDB)
- **Warm**: 90 days (S3)
- **Cold**: 730 days (Iceberg)

### Custom Configuration
```yaml
analytics:
  retention:
    enabled: true
    hot_ttl_days: 7
    warm_ttl_days: 90
    cold_ttl_days: 730
    archival_interval: 24h
    cleanup_interval: 7d
    tiering_interval: 6h
```

## Common Operations

### Manual Archival
```go
job, err := manager.ArchiveCategory(ctx, "audit_events")
if err != nil {
    log.Printf("archival failed: %v", err)
}
log.Printf("archived %d rows, %d bytes", job.RowCount, job.BytesTransferred)
```

### Tier Transitions
```go
transition, err := manager.TransitionCategory(ctx, "sessions")
if err != nil {
    log.Printf("transition failed: %v", err)
}
log.Printf("moved %d rows", transition.RowsAffected)
```

### Cleanup Operations
```go
cleanupJob, err := manager.CleanupCategory(ctx, "audit_events")
if err != nil {
    log.Printf("cleanup failed: %v", err)
}
log.Printf("deleted %d records", cleanupJob.RowsDeleted)
```

## Monitoring

### Check Status
```go
status := manager.GetStatus()
fmt.Printf("Initialized: %v, Scheduler: %v\n", 
    status.Initialized, status.SchedulerRunning)
```

### Get Tier Metrics
```go
metrics, err := manager.GetTierMetrics(ctx, "audit_events")
for _, m := range metrics {
    fmt.Printf("%s: %d rows, %.2fGB, $%.2f/month\n",
        m.Tier, m.RowCount, m.EstimatedSizeGB, m.EstimatedMonthlyCost)
}
```

### View Audit Logs
```go
logs, err := manager.GetAuditLogs(ctx, "ArchivalExecutor", 50)
for _, log := range logs {
    fmt.Printf("%s: %s\n", log.Timestamp, log.Details)
}
```

## Tier Definitions

| Tier | Duration | Storage | Cost | Use Case |
|------|----------|---------|------|----------|
| Hot | <7 days | DuckDB | Highest | Real-time queries |
| Warm | 7-90 days | S3 | Mid | Recent history |
| Cold | >90 days | Iceberg | Lowest | Archive/compliance |

## Compression Options

For warm storage S3 tier:
- `snappy` (default) - Fast compression, moderate ratio
- `gzip` - Better compression, slower
- `zstd` - Best compression, very slow
- `none` - No compression

## Key Data Structures

### TierType
```go
const (
    TierHot  TierType = "hot"
    TierWarm TierType = "warm"
    TierCold TierType = "cold"
)
```

### ArchivalJob
```go
type ArchivalJob struct {
    ID                string
    Category          string
    SourceTier        TierType
    TargetTier        TierType
    Status            string      // pending, in_progress, completed, failed
    RowCount          int64
    BytesTransferred  int64
    Checksum          string
    RetryCount        int
}
```

### TierMetrics
```go
type TierMetrics struct {
    Tier                 TierType
    RowCount             int64
    EstimatedSizeGB      float64
    OldestRecordAge      int    // days
    EstimatedMonthlyCost float64
}
```

## Categories & TTLs

### Default Categories

**audit_events**
- Hot: 30 days (longer for compliance)
- Warm: 180 days
- Cold: 730 days

**authentication**
- Hot: 14 days
- Warm: 60 days
- Cold: 365 days

### Custom Category
```yaml
categories:
  custom_events:
    hot_days: 3
    warm_days: 30
    cold_days: 180
```

## Scheduling

### Job Intervals
- **Archival**: Daily (off-peak, e.g., 2 AM)
- **Tiering**: Every 6 hours
- **Cleanup**: Weekly

### Adjusting Intervals
```go
config := &retention.ScheduleConfig{
    ArchivalInterval: 24 * time.Hour,     // Daily
    CleanupInterval:  7 * 24 * time.Hour, // Weekly
    TieringInterval:  6 * time.Hour,      // Every 6 hours
}
```

## Troubleshooting

### Data Not Moving
1. Check `manager.GetStatus()` - is manager initialized?
2. Verify storage backends registered
3. Check `GetAuditLogs()` for errors
4. Verify policy TTLs are configured

### High CPU/Memory During Archival
1. Reduce batch size (default: 1,000)
2. Increase archival interval
3. Check storage backend performance

### Missing Data
1. Run `FindOrphanRecords()` to detect
2. Use `RepairOrphanRecords()` to fix
3. Check audit log for failures
4. Verify storage backend connectivity

## Performance Tips

1. **Batch Size**: Tune to 500-2,000 based on row size
2. **Schedule**: Run archival at off-peak hours
3. **Intervals**: Balance cost savings vs. query performance
4. **Storage**: Use S3 for warm, Glacier for cold
5. **Monitoring**: Review metrics monthly

## Cost Optimization

### Typical Breakdown (100GB)
```
Hot tier (5GB):        $0.12/month
Warm tier (20GB):      $0.50/month  
Cold tier (75GB):      $0.30/month
Total:                 $0.92/month (vs $2.30 all-hot)
Savings:               60% reduction
```

### Optimization Strategies
1. Reduce hot TTL for less-critical data
2. Increase warm TTL if access patterns allow
3. Use cold/archive tier aggressively
4. Monitor compression ratios (Warm tier)
5. Archive by category - different patterns

## API Methods

### Manager Public API
- `Initialize(ctx)` - Set up system
- `StartScheduler(ctx)` - Begin automated jobs
- `StopScheduler()` - Pause automation
- `ArchiveCategory(ctx, category)` - Manual archive
- `TransitionCategory(ctx, category)` - Manual transition
- `CleanupCategory(ctx, category)` - Manual cleanup
- `GetTierMetrics(ctx, category)` - Fetch metrics
- `GetCleanupStats(ctx, category)` - Cleanup stats
- `GetAuditLogs(ctx, component, limit)` - Query logs
- `GetPolicy()` - Get current policy
- `UpdatePolicy(policy)` - Change policy
- `GetStatus()` - Manager status
- `Close(ctx)` - Shutdown

## Database Tables Created

- `analytics_events` - Extended with tier/archive fields
- `tier_transitions` - Transition history
- `archival_jobs` - Archival job records
- `cleanup_jobs` - Cleanup job records
- `retention_audit_log` - Immutable audit trail

## Environment Variables

Typically configured in storage backend setup:
- `AEGION_ANALYTICS_S3_ENDPOINT_URL` - S3 endpoint
- `AEGION_ANALYTICS_S3_ACCESS_KEY_ID` - AWS credentials
- `AEGION_ANALYTICS_S3_SECRET_ACCESS_KEY` - AWS credentials
- `AEGION_ANALYTICS_NESSIE_URI` - Iceberg catalog URI

## Storage Backend Implementation

### S3 Backend Example
```go
s3Backend := &s3storage.Backend{
    Bucket:    "my-analytics",
    Prefix:    "warm/",
    Region:    "us-east-1",
    Compression: retention.CompressionSnappy,
}
manager.RegisterStorageBackend(retention.TierWarm, s3Backend)
```

### Iceberg Backend Example
```go
icebergBackend := &icebergstorage.Backend{
    WarehousePath: "s3://my-analytics/cold/",
    CatalogType:   "hive",
    CatalogName:   "analytics",
}
manager.RegisterStorageBackend(retention.TierCold, icebergBackend)
```

## Audit Log Queries

### Get Recent Archival Operations
```go
logs, _ := manager.GetAuditLogs(ctx, "ArchivalExecutor", 50)
```

### Get Tier Transition History
```go
logs, _ := manager.GetAuditLogs(ctx, "TieringEngine", 50)
```

### Get Cleanup Operations
```go
logs, _ := manager.GetAuditLogs(ctx, "CleanupManager", 50)
```

## Files Created

```
modules/analytics/retention/
├── policy.go           - Retention policies
├── archival.go         - Archival executor
├── tiering.go          - Tier transitions
├── cleanup.go          - Cleanup operations
├── scheduler.go        - Job scheduling
├── manager.go          - Central manager
├── audit.go            - Audit logging
├── retention_test.go   - Unit tests
└── README.md           - Full documentation
```

## Dependencies

- `database/sql` - Database operations
- `context` - Context management
- `time` - Scheduling and timestamps
- `sync` - Concurrency control
- `encoding/json` - Data serialization
- `crypto/sha256` - Checksum calculation

## Related Documentation

- `PHASE6_COMPLETION.md` - Detailed completion summary
- `modules/analytics/retention/README.md` - Full module documentation
- `configs/aegion.yaml` - Configuration reference

---

For full documentation, see `modules/analytics/retention/README.md`
