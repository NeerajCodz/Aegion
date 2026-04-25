# Analytics Module - Upgrade Guide

This guide provides step-by-step instructions for upgrading the Aegion Analytics module to a new version.

## Pre-Upgrade Checklist

Before starting the upgrade process, ensure:

- [ ] **Backup created**: Full backup of DuckDB data directory (`/data/duckdb`)
- [ ] **Backup verified**: Restore test on backup to confirm integrity
- [ ] **Downtime window scheduled**: Plan for upgrade downtime if needed
- [ ] **Team notified**: Inform stakeholders of planned maintenance
- [ ] **Monitoring prepared**: Set up alerts for any issues during upgrade
- [ ] **Rollback plan ready**: Confirm rollback procedure is documented and tested
- [ ] **Database migrations reviewed**: Review migration scripts for breaking changes
- [ ] **Dependencies checked**: Verify all service dependencies can handle the upgrade
- [ ] **Current logs archived**: Archive existing logs for reference
- [ ] **Health checks passing**: Ensure all services pass health checks before starting

## Step-by-Step Upgrade Procedure

### 1. Prepare Environment

```bash
# Set variables
BACKUP_DIR=/data/backups
CURRENT_VERSION=$(grep version configs/analytics.yaml | cut -d' ' -f2)
NEW_VERSION="1.1.0"  # Update to target version
BACKUP_TIMESTAMP=$(date +%Y%m%d_%H%M%S)

# Create timestamped backup directory
mkdir -p $BACKUP_DIR/$BACKUP_TIMESTAMP

# Create backup of data
cp -r /data/duckdb $BACKUP_DIR/$BACKUP_TIMESTAMP/

echo "Pre-upgrade backup created at: $BACKUP_DIR/$BACKUP_TIMESTAMP"
```

### 2. Stop the Analytics Service

```bash
# Gracefully shutdown the service
systemctl stop aegion-analytics

# Verify service stopped
systemctl status aegion-analytics

# Wait for graceful shutdown (default 30 seconds)
sleep 35
```

### 3. Archive Current Configuration

```bash
# Backup current configuration
cp -r configs/ $BACKUP_DIR/$BACKUP_TIMESTAMP/configs_$CURRENT_VERSION/

# Archive current binary
cp ./aegion-analytics $BACKUP_DIR/$BACKUP_TIMESTAMP/aegion-analytics_$CURRENT_VERSION
```

### 4. Download and Install New Version

```bash
# Download new version
cd /opt/aegion
wget https://releases.example.com/aegion-analytics-$NEW_VERSION.tar.gz

# Extract
tar -xzf aegion-analytics-$NEW_VERSION.tar.gz

# Verify checksum (important for security)
sha256sum -c aegion-analytics-$NEW_VERSION.tar.gz.sha256
```

### 5. Run Database Migrations

```bash
# Review migrations first
cat migrations/from_$CURRENT_VERSION-to_$NEW_VERSION.sql

# Apply migrations
./aegion-analytics -config configs/analytics.yaml -migrate

# Check migration status
echo "Migrations completed. Check logs for any issues."
tail -100 logs/aegion-analytics.log
```

### 6. Verify Configuration Compatibility

```bash
# Validate new configuration against schema
./aegion-analytics -config configs/analytics.yaml -validate-config

# If config validation fails, review breaking changes below
# and update configuration as needed
```

### 7. Start the Analytics Service

```bash
# Start service with new version
systemctl start aegion-analytics

# Give service time to initialize (usually 10-15 seconds)
sleep 15

# Check service status
systemctl status aegion-analytics

# Verify service is healthy
curl http://localhost:8080/health
```

### 8. Verify Upgrade Success

```bash
# Check health endpoint
curl http://localhost:8080/health

# Check readiness
curl http://localhost:8080/ready

# Verify metrics are being collected
curl http://localhost:8080/metrics | head -20

# Check logs for any errors
tail -50 logs/aegion-analytics.log

# Test basic functionality
curl -X GET http://localhost:8080/api/v1/events?limit=1

# Verify no data loss
SELECT COUNT(*) FROM events;
```

### 9. Monitor for Issues

```bash
# Monitor service for first hour
watch -n 5 'systemctl status aegion-analytics'

# Check error rate
tail -f logs/aegion-analytics.log | grep ERROR

# Monitor resource usage
watch 'ps aux | grep aegion-analytics'
```

## Breaking Changes

### Version 1.1.0

**Breaking Changes:**
- gRPC error messages now return generic messages instead of detailed error descriptions
  - Impact: Client applications relying on specific error text will need to update
  - Action: Update error handling to use error codes instead of error messages
  
- Metrics endpoint now requires authentication (optional, configurable)
  - Impact: `/metrics` endpoint may require Bearer token
  - Action: Update monitoring systems to include authentication headers

**Deprecated Features:**
- `cache_ttl` configuration option is deprecated
  - Alternative: Use per-query cache hints in API requests
  - Timeline: Will be removed in version 1.3.0

**New Features:**
- Health endpoint now includes sync lag information
- Graceful shutdown timeout is now configurable
- Metrics export now includes additional storage metrics

## Rollback Procedure

If issues occur during or after upgrade, follow these steps to rollback:

### Quick Rollback (within 1 hour)

```bash
# Stop current service
systemctl stop aegion-analytics

# Restore backup
rm -rf /data/duckdb
cp -r $BACKUP_DIR/$BACKUP_TIMESTAMP/duckdb /data/

# Restore old binary
cp $BACKUP_DIR/$BACKUP_TIMESTAMP/aegion-analytics_$CURRENT_VERSION ./aegion-analytics

# Start with old version
systemctl start aegion-analytics

# Verify rollback
curl http://localhost:8080/health

echo "Rollback complete. Service restored to version $CURRENT_VERSION"
```

### Complete Rollback (with config restore)

```bash
# Stop service
systemctl stop aegion-analytics

# Restore data
rm -rf /data/duckdb
cp -r $BACKUP_DIR/$BACKUP_TIMESTAMP/duckdb /data/

# Restore configuration
rm -rf configs/
cp -r $BACKUP_DIR/$BACKUP_TIMESTAMP/configs_$CURRENT_VERSION configs/

# Restore binary
cp $BACKUP_DIR/$BACKUP_TIMESTAMP/aegion-analytics_$CURRENT_VERSION ./aegion-analytics

# Start service
systemctl start aegion-analytics

# Verify rollback
sleep 10
curl http://localhost:8080/health
tail -20 logs/aegion-analytics.log

echo "Complete rollback to version $CURRENT_VERSION finished"
```

### Data Recovery

```bash
# If data corruption suspected, restore from timestamped backup
RESTORE_TIMESTAMP="20240101_143022"  # Use specific backup timestamp

rm -rf /data/duckdb
cp -r $BACKUP_DIR/$RESTORE_TIMESTAMP/duckdb /data/

# Verify data integrity
systemctl start aegion-analytics
sleep 10
SELECT COUNT(*) FROM events;
```

## Troubleshooting

### Upgrade Hangs During Migration

**Symptom**: Migration process doesn't complete after 5+ minutes

**Solution**:
```bash
# Check if process is stuck
ps aux | grep aegion-analytics

# Check migration logs
tail -100 logs/aegion-analytics.log | grep -i migration

# If truly stuck, kill and rollback
killall aegion-analytics
# Then follow Quick Rollback procedure
```

### Service Won't Start After Upgrade

**Symptom**: `systemctl start aegion-analytics` fails

**Solution**:
```bash
# Check logs
journalctl -u aegion-analytics -n 50

# Validate configuration
./aegion-analytics -config configs/analytics.yaml -validate-config

# Try manual startup to see errors
./aegion-analytics -config configs/analytics.yaml

# If config issue, consult breaking changes section
# If data issue, check database integrity
```

### Health Endpoint Not Responding

**Symptom**: `curl http://localhost:8080/health` times out

**Solution**:
```bash
# Check if service is listening
netstat -tlnp | grep 8080

# Check for port conflicts
lsof -i :8080

# Check service logs
systemctl status aegion-analytics -n 30

# If port issue, update configuration and restart
# If service crashed, check logs for errors
```

### Data Appears Lost

**Symptom**: Event count lower than expected after upgrade

**Solution**:
```bash
# Verify you're querying correct database
SELECT COUNT(*) FROM events;

# Check backup was from correct timestamp
ls -la $BACKUP_DIR/

# Restore from backup if needed
# Follow Complete Rollback procedure
```

## Performance Considerations

- **Large datasets (>100GB)**: Migrations may take 30-60 minutes
  - Allocate adequate time and monitor disk space
  
- **High write volume**: Schedule upgrade during low-traffic periods
  - Monitor write latency during startup
  
- **Cache implications**: Cache will be cleared on startup
  - Monitor performance for 10-15 minutes after upgrade
  - May see increased database load initially

## Support and Escalation

If upgrade fails and rollback doesn't resolve issues:

1. **Collect diagnostics**:
   ```bash
   systemctl status aegion-analytics > /tmp/status.txt
   journalctl -u aegion-analytics -n 200 > /tmp/logs.txt
   tar -czf /tmp/analytics-diagnostics.tar.gz /tmp/status.txt /tmp/logs.txt
   ```

2. **Contact support** with diagnostics package

3. **Keep backups**: Don't delete backup until confirmed stable for 48+ hours

## Verification Checklist

After upgrade completes, verify:

- [ ] Health endpoint returns 200 with `"status": "healthy"`
- [ ] Ready endpoint returns 200 with all services ready
- [ ] Metrics endpoint exports valid Prometheus format
- [ ] Can query events without errors
- [ ] Dashboard queries execute successfully
- [ ] Webhooks are functioning
- [ ] Audit logs show no errors
- [ ] Performance metrics are nominal
- [ ] No error spikes in logs
- [ ] Graceful shutdown works (systemctl stop responds quickly)

---

## Version History

| Version | Release Date | Key Changes |
|---------|------------|-------------|
| 1.0.0 | 2024-01-15 | Initial production release |
| 1.1.0 | 2024-02-15 | Error message hardening, metrics expansion |

---

**Questions?** Refer to the [Analytics FAQ](./faq.md) or contact the platform team.
