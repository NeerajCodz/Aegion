# Analytics Troubleshooting Guide

**Version:** 1.0  
**Last Updated:** 2026-04-24  
**Module:** `modules/analytics`

---

## Common Issues & Solutions

### DuckDB Connection Issues

#### Error: "Connection refused"

**Symptoms:**
- API returns `503 Service Unavailable`
- Logs show "failed to connect to DuckDB"

**Solutions:**

1. **Check if DuckDB file exists:**
   ```bash
   ls -la ./data/duckdb/
   ```
   - If missing, restart analytics service
   - DuckDB will auto-initialize

2. **Check permissions:**
   ```bash
   chmod 755 ./data/duckdb/
   chmod 644 ./data/duckdb/analytics.duckdb
   ```

3. **Remove stale lock file:**
   ```bash
   rm -f ./data/duckdb/analytics.duckdb.lock
   ```

4. **Check disk space:**
   ```bash
   df -h ./data/duckdb/
   # Ensure > 10GB free
   ```

#### Error: "DuckDB out of memory"

**Symptoms:**
- Query returns `OOM Error`
- High memory usage

**Solutions:**

1. **Increase memory limit:**
   ```yaml
   analytics:
     duckdb:
       memory_limit_gb: 16  # Increase from 8
   ```

2. **Restart service:**
   ```bash
   systemctl restart aegion
   # or
   docker restart aegion_container
   ```

3. **Check query complexity:**
   ```bash
   # View slow queries
   curl http://localhost:8080/api/v1/analytics/stats
   ```

---

### PostgreSQL Sync Issues

#### Error: "Sync lag too high"

**Symptoms:**
- Dashboard data not updating
- Events appear with delay > 1 minute

**Solutions:**

1. **Check real-time sync status:**
   ```bash
   curl http://localhost:8080/api/v1/analytics/stats | jq '.sync.realTime'
   ```

2. **Verify PostgreSQL CDC is running:**
   ```sql
   -- Connect to PostgreSQL
   SELECT * FROM pg_replication_slots;
   
   -- Should show 'aegion_analytics' slot
   ```

3. **Increase batch workers:**
   ```yaml
   analytics:
     sync:
       batch:
         parallel_workers: 8  # Increase from 4
   ```

4. **Check PostgreSQL connection:**
   ```bash
   psql postgresql://user:pass@localhost:5432/aegion \
     -c "SELECT 1"
   ```

#### Error: "PostgreSQL connection lost"

**Symptoms:**
- Sync stops
- Logs show "connection refused"

**Solutions:**

1. **Verify PostgreSQL is running:**
   ```bash
   docker ps | grep postgres
   # or
   systemctl status postgresql
   ```

2. **Check credentials in config:**
   ```yaml
   analytics:
     sync:
       postgres:
         host: localhost
         port: 5432
         database: aegion
         user: analytics
         password: ${DB_PASSWORD}
   ```

3. **Verify firewall/network:**
   ```bash
   telnet localhost 5432
   # or
   nc -zv localhost 5432
   ```

4. **Restart sync:**
   ```bash
   # Trigger batch sync manually
   curl -X POST \
     -H "Authorization: Bearer token" \
     http://localhost:8080/api/v1/analytics/sync/batch/trigger
   ```

---

### Query Timeouts

#### Error: "Query timeout exceeded 30s"

**Symptoms:**
- Dashboard queries fail
- Complex reports timeout

**Solutions:**

1. **Increase query timeout:**
   ```yaml
   analytics:
     rest:
       query_timeout_seconds: 60  # Increase from 30
   ```

2. **Optimize slow query:**
   ```bash
   # Check execution plan
   EXPLAIN SELECT ...;
   ```

3. **Create missing indexes:**
   ```sql
   CREATE INDEX idx_events_category ON events(category);
   CREATE INDEX idx_events_userId ON events(userId);
   ```

4. **Reduce result set:**
   ```sql
   -- Add LIMIT
   SELECT * FROM events WHERE category = 'user_action'
   LIMIT 1000;
   
   -- Or use pagination
   LIMIT 1000 OFFSET 0;
   ```

---

### Authentication Errors

#### Error: "Unauthorized" (401)

**Symptoms:**
- API returns `401 Unauthorized`
- Cannot access any endpoints

**Solutions:**

1. **Check token is included:**
   ```bash
   curl -H "Authorization: Bearer YOUR_TOKEN" \
     http://localhost:8080/api/v1/analytics/health
   ```

2. **Verify token is valid:**
   ```bash
   # Decode JWT to check expiry
   echo YOUR_TOKEN | jq '.exp'
   
   # Compare with current timestamp
   date +%s
   ```

3. **Generate new token:**
   ```bash
   curl -X POST \
     -d 'username=admin&password=pass' \
     http://localhost:8080/auth/login
   ```

4. **Check JWT secret:**
   ```yaml
   analytics:
     security:
       jwt_secret: ${JWT_SECRET}  # Must match signing key
   ```

#### Error: "Forbidden" (403)

**Symptoms:**
- API returns `403 Forbidden`
- Cannot perform mutation

**Solutions:**

1. **Check user role:**
   ```bash
   # Decode JWT
   jwt-decode YOUR_TOKEN
   # Look for 'roles' claim
   ```

2. **Verify permission:**
   - `viewer` role: Read-only access
   - `analyst` role: Read+write dashboards/queries
   - `admin` role: Full access

3. **Request elevated permissions:**
   ```bash
   # Contact admin to upgrade role
   ```

---

### Rate Limiting Issues

#### Error: "Rate limit exceeded" (429)

**Symptoms:**
- API returns `429 Conflict`
- `X-RateLimit-Retry-After` header present

**Solutions:**

1. **Check rate limit:**
   ```bash
   curl -i http://localhost:8080/api/v1/analytics/health
   # View X-RateLimit-Remaining header
   ```

2. **Wait before retrying:**
   ```bash
   retry_after=$(curl -s -i http://localhost:8080/api/v1/analytics/health \
     | grep -i X-RateLimit-Retry-After | cut -d: -f2)
   sleep $retry_after
   ```

3. **Increase rate limit:**
   ```yaml
   analytics:
     rest:
       rate_limit_per_minute: 1000  # Increase from 600
   ```

4. **Use batch API:**
   - Instead of multiple individual requests
   - Use `/api/v1/analytics/events/batch`

---

### Webhook Delivery Failures

#### Error: "Webhook delivery failed"

**Symptoms:**
- Webhook events not received
- Status shows "failed" in Admin SPA

**Solutions:**

1. **Verify webhook URL is accessible:**
   ```bash
   curl -X POST https://your-webhook-url.com/events
   # Should return 200-299
   ```

2. **Check webhook configuration:**
   ```bash
   curl http://localhost:8080/api/v1/analytics/webhooks/{id}
   # Verify URL and filters
   ```

3. **View delivery history:**
   ```bash
   curl http://localhost:8080/api/v1/analytics/webhooks/{id}/deliveries
   # Check error message
   ```

4. **Manual retry:**
   ```bash
   curl -X POST \
     http://localhost:8080/api/v1/analytics/webhooks/deliveries/{deliveryId}/replay
   ```

5. **Check webhook timeout:**
   - Webhook has 30s timeout
   - If your endpoint is slow, increase timeout in config

#### Webhook Goes to Dead Letter Queue (DLQ)

**Symptoms:**
- Webhook delivery fails 3 times
- Moved to DLQ

**Solutions:**

1. **Fix the underlying issue:**
   - Check webhook URL
   - Verify endpoint is responding
   - Fix any auth/permission issues

2. **Replay from DLQ:**
   ```bash
   curl http://localhost:8080/api/v1/analytics/webhooks/{id}/dlq
   # Get delivery IDs
   
   curl -X POST \
     http://localhost:8080/api/v1/analytics/webhooks/deliveries/{deliveryId}/replay
   ```

3. **Update webhook configuration:**
   ```bash
   curl -X PUT \
     -H "Content-Type: application/json" \
     -d '{
       "url": "https://new-endpoint.com/webhooks",
       "retryPolicy": { "maxRetries": 5 }
     }' \
     http://localhost:8080/api/v1/analytics/webhooks/{id}
   ```

---

### Storage Issues

#### Error: "Storage backend unavailable"

**Symptoms:**
- Old data not accessible
- Tiering fails

**Solutions:**

1. **Check storage configuration:**
   ```yaml
   analytics:
     storage:
       type: s3  # or iceberg, k8s
       config:
         bucket: my-bucket
         region: us-east-1
   ```

2. **Verify S3 connectivity:**
   ```bash
   aws s3 ls s3://my-bucket/analytics/ \
     --region us-east-1
   ```

3. **Check AWS credentials:**
   ```bash
   echo $AWS_ACCESS_KEY_ID
   echo $AWS_SECRET_ACCESS_KEY
   # Or check ~/.aws/credentials
   ```

4. **Fall back to local storage:**
   ```yaml
   analytics:
     storage:
       type: local
       base_path: ./data/analytics
   ```

---

### Performance Issues

#### Problem: "Queries are slow"

**Symptoms:**
- Dashboard takes 5+ seconds to load
- P99 latency > 2 seconds

**Solutions:**

1. **Check query execution plan:**
   ```sql
   EXPLAIN SELECT ... FROM events WHERE ...;
   ```

2. **Create indexes:**
   ```sql
   CREATE INDEX idx_events_category ON events(category);
   CREATE INDEX idx_events_createdAt ON events(createdAt);
   ```

3. **Check cache hit rate:**
   ```bash
   curl http://localhost:8080/api/v1/analytics/stats \
     | jq '.cache'
   ```

4. **Increase DuckDB resources:**
   ```yaml
   analytics:
     duckdb:
       threads: 16  # Match CPU cores
       memory_limit_gb: 32  # Increase available memory
   ```

#### Problem: "High memory usage"

**Symptoms:**
- Process uses > 80% of available RAM
- OOM killer may terminate service

**Solutions:**

1. **Monitor memory:**
   ```bash
   top -p $(pgrep -f aegion)
   # Check VIRT and RES columns
   ```

2. **Reduce DuckDB memory:**
   ```yaml
   analytics:
     duckdb:
       memory_limit_gb: 8  # Reduce from 16
   ```

3. **Archive old data:**
   ```bash
   # Move events > 90 days old to S3
   curl -X POST \
     http://localhost:8080/api/v1/analytics/retention/archive
   ```

4. **Reduce concurrent connections:**
   ```yaml
   analytics:
     duckdb:
       connection_pool_size: 20  # Reduce from 50
   ```

---

### API Issues

#### Error: "CORS error"

**Symptoms:**
- Browser console shows CORS error
- Cross-origin requests blocked

**Solutions:**

1. **Configure CORS in config:**
   ```yaml
   analytics:
     rest:
       cors:
         enabled: true
         allowed_origins:
           - "http://localhost:3000"
           - "https://analytics.example.com"
         allowed_methods: ["GET", "POST", "PUT", "DELETE"]
         allowed_headers: ["Content-Type", "Authorization"]
   ```

2. **Restart service:**
   ```bash
   systemctl restart aegion
   ```

#### Error: "400 Bad Request"

**Symptoms:**
- API returns `400 Bad Request`
- Missing error details

**Solutions:**

1. **Check request body:**
   ```bash
   curl -X POST \
     -H "Content-Type: application/json" \
     -d '{"invalid": "json"}' \
     http://localhost:8080/api/v1/analytics/dashboards \
     | jq '.error'
   ```

2. **Validate schema:**
   - Check required fields
   - Verify data types
   - Ensure valid enums

3. **Check Content-Type:**
   ```bash
   -H "Content-Type: application/json"
   ```

---

## Debugging

### Enable Debug Logging

```yaml
analytics:
  logging:
    level: DEBUG
    log_all_queries: true
    explain_slow_queries: true
    log_request_bodies: true
```

**Restart service:**
```bash
docker-compose restart aegion
# or
systemctl restart aegion
```

**View logs:**
```bash
docker-compose logs -f aegion
# or
journalctl -u aegion -f
```

### Health Check Endpoint

```bash
# Full system health
curl http://localhost:8080/api/v1/analytics/health

# Detailed status
curl http://localhost:8080/api/v1/analytics/stats

# Liveness probe
curl http://localhost:8080/api/v1/analytics/live

# Readiness probe
curl http://localhost:8080/api/v1/analytics/ready
```

### Test Endpoints

```bash
# Test REST API
curl -H "Authorization: Bearer token" \
  http://localhost:8080/api/v1/analytics/events?limit=1

# Test GraphQL
curl -X POST \
  -H "Authorization: Bearer token" \
  -H "Content-Type: application/json" \
  -d '{"query": "{ health { status } }"}' \
  http://localhost:8080/api/v1/analytics/graphql

# Test gRPC
grpcurl -plaintext \
  -d '{}' \
  localhost:50051 analytics.Analytics.Health
```

---

## Getting Help

### Resources

- [Documentation Index](./README.md)
- [Setup Guide](./setup.md)
- [Architecture](./architecture.md)
- [Security](./security.md)

### Report Issues

For bugs or issues:
1. Check this troubleshooting guide
2. Review logs for error messages
3. Collect information:
   - Aegion version
   - Analytics version
   - PostgreSQL version
   - DuckDB version
   - Error logs
   - Steps to reproduce
4. Create issue on GitHub

### Support

- GitHub Issues: Report bugs
- Email: support@aegion.io
- Slack: #analytics-help channel

---

## Related Documentation

- [Admin SPA Guide](./admin-spa.md)
- [Performance Tuning](./performance.md)
- [Security](./security.md)
