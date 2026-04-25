# Analytics FAQ

**Version:** 1.0  
**Last Updated:** 2026-04-24  
**Module:** `modules/analytics`

---

## General Questions

### Q: What is Aegion Analytics?

**A:** Aegion Analytics is a real-time event processing and analytics platform built into the Aegion system. It provides:
- Event ingestion and streaming
- Multi-API layers (REST, GraphQL, gRPC)
- Real-time dashboards and queries
- Webhook notifications
- Storage tiering and retention management
- Admin SPA for management

### Q: How is Aegion Analytics different from other analytics systems?

**A:**
- **Integrated:** Part of core Aegion (no external dependencies)
- **Real-time:** Event processing with sub-100ms latency
- **Flexible:** Three API layers for different use cases
- **Self-hosted:** Deploy on-premise or cloud
- **Open source:** Full transparency and control
- **Cost-effective:** Efficient storage tiering (hot/warm/cold)

### Q: What are typical use cases?

**A:**
- **User analytics** - Track page views, clicks, conversions
- **System monitoring** - Capture system events, errors, performance metrics
- **Audit logging** - Full audit trail of all system changes
- **Business intelligence** - Generate reports and dashboards
- **Real-time alerts** - Webhook notifications for critical events
- **Data warehouse** - Long-term analytics data repository

### Q: Can I use Analytics without Aegion Core?

**A:** Not currently. Analytics is tightly integrated with Aegion Core. However, you can ingest custom events via REST API.

---

## Architecture & Design

### Q: Why is DuckDB used instead of PostgreSQL?

**A:** DuckDB is optimized for OLAP (Online Analytical Processing):
- **Columnar storage** - Better for analytics queries
- **Vectorized execution** - 10-100x faster for aggregations
- **In-process** - No network latency
- **Zero-copy** - Memory efficient
- **Free & open** - No licensing costs

PostgreSQL remains the system of record.

### Q: What's the real-time sync strategy?

**A:** Three complementary strategies:
1. **Real-time CDC** - Immediate data replication (< 100ms)
2. **Batch sync** - Scheduled full/incremental sync (default 5 min)
3. **Async queue** - Background processing for complex transformations

This ensures both immediate consistency and eventual correctness.

### Q: How does storage tiering work?

**A:**
- **Hot (DuckDB)** - Last 24 hours, instant queries
- **Warm (S3)** - 1-30 days, second-scale queries
- **Cold (Iceberg)** - 30+ years, minute-scale queries

Automatic tiering reduces costs while maintaining query access.

### Q: Can I query data from all tiers simultaneously?

**A:** Yes. Queries transparently access data from all tiers:
- DuckDB first (cache check)
- S3 if not in DuckDB
- Iceberg for historical data

Query execution time varies by tier (10ms vs 60s).

---

## API & Integration

### Q: Which API should I use for my use case?

**A:**
- **REST** - Traditional web/mobile apps, webhooks, simple integrations
- **GraphQL** - Complex queries, real-time subscriptions, admin UIs
- **gRPC** - High-performance services, internal communication

See [API Reference](./api.md) for detailed comparison.

### Q: How do I authenticate to the Analytics API?

**A:** Three authentication methods:
1. **Bearer token** - JWT token in Authorization header
2. **Session cookie** - X-Session-ID cookie
3. **mTLS** - Certificate-based (gRPC only, production)

All tokens expire (default 24 hours) and can be refreshed.

### Q: Can I access Analytics from my custom application?

**A:** Yes! Examples:
- Python: `import requests; requests.get(..., headers={'Authorization': 'Bearer token'})`
- JavaScript: Fetch API or axios
- Go: Standard net/http package
- Any language: HTTP client + JSON

### Q: How do I handle pagination in large result sets?

**A:**
- **REST** - Use `limit` and `offset` parameters
- **GraphQL** - Use cursor-based pagination with `after` and `first`
- **Both** - Efficient: start with small limit, use cursors

Example:
```
GET /api/v1/analytics/events?limit=100&offset=0
GET /api/v1/analytics/events?limit=100&offset=100  # Next page
```

---

## Security & Compliance

### Q: Is my data encrypted?

**A:**
- **In transit** - Yes, TLS 1.3 (HTTPS/gRPC with mTLS)
- **At rest** - Optional field-level encryption available
- **Backup** - Encrypted S3 and Iceberg storage

See [Security Guide](./security.md) for details.

### Q: How do I ensure GDPR compliance?

**A:**
1. Set appropriate data retention policies
2. Use right to deletion endpoint: `DELETE /users/{userId}`
3. Enable audit logging for all access
4. Implement encryption for sensitive fields
5. Regularly review and minimize data collection

### Q: Can I restrict access to certain users/roles?

**A:** Yes! Three role levels:
- **admin** - Full access
- **analyst** - Read+write dashboards/queries
- **viewer** - Read-only

Plus resource-level permissions (own dashboards only).

### Q: Is there audit logging?

**A:** Yes! Every action is logged:
- User logins/logouts
- Query executions
- Configuration changes
- Webhook deliveries
- Permission denials

Audit logs retained for 90 days.

---

## Performance & Scaling

### Q: How many events per second can Analytics handle?

**A:** Depends on hardware:
- **Development (4 CPU, 8GB RAM)** - 1k events/sec
- **Production (16 CPU, 64GB RAM)** - 10k+ events/sec
- **Scaled (multi-instance)** - 100k+ events/sec

Real-time sync keeps latency < 100ms.

### Q: What's the largest dataset I can store?

**A:**
- **Hot (DuckDB)** - Limited to available disk (typically 100-500 GB)
- **Warm (S3)** - Unlimited (pay per GB)
- **Cold (Iceberg)** - Unlimited (pay per GB, cheaper)

Automatic tiering keeps hot storage manageable.

### Q: How do I optimize slow queries?

**A:**
1. Create indexes on filtered columns
2. Use LIMIT and pagination
3. Leverage caching (5-minute default TTL)
4. Break complex queries into smaller pieces
5. Consider using pre-aggregated dashboards

See [Performance Guide](./performance.md).

### Q: Can I run Analytics on my laptop?

**A:** Yes! Requirements:
- **CPU** - 2+ cores (more is better)
- **RAM** - 4GB minimum (8GB+ recommended)
- **Disk** - 50GB for hot storage
- **PostgreSQL** - Any version 12+

Use Docker Compose for easy setup.

---

## Operations & Maintenance

### Q: How do I backup my analytics data?

**A:**
```bash
# Full backup
docker-compose exec duckdb \
  duckdb /data/analytics.duckdb \
  "BACKUP DATABASE TO 's3://backup-bucket/analytics-backup'"

# Or export data
cp -r ./data/duckdb ./backup/
```

Scheduled backups: Configure in admin SPA.

### Q: How do I migrate from Analytics v0 to v1?

**A:**
1. Back up existing data
2. Run migrations: `go run ./cmd/migrate up`
3. Verify data integrity
4. Test all dashboards/queries
5. Monitor for issues

Full migration guide available.

### Q: How often should I run maintenance?

**A:**
- **Daily** - Monitor sync lag and health
- **Weekly** - Review audit logs
- **Monthly** - Optimize indexes, archive old data
- **Quarterly** - Review retention policies
- **Annually** - Security audit

### Q: What happens if DuckDB fills up?

**A:**
1. Old data automatically moves to S3 (warm tier)
2. Old data eventually archived to Iceberg (cold tier)
3. Hot storage stays manageable
4. Queries automatically span all tiers

Configure tier thresholds in config.

### Q: How do I scale Analytics horizontally?

**A:**
1. Run multiple API instances (load balanced)
2. Share PostgreSQL and storage backend
3. Each instance has local DuckDB replica
4. Eventual consistency across instances

For details, see [Setup Guide](./setup.md).

---

## Troubleshooting

### Q: Why is my dashboard not updating?

**A:** Common causes:
1. **Sync not running** - Check health endpoint
2. **Event filters too strict** - Verify webhook filters
3. **Clock skew** - Check system time synchronization
4. **Network issues** - Check PostgreSQL connectivity

See [Troubleshooting Guide](./troubleshooting.md).

### Q: Why are my queries slow?

**A:** Common causes:
1. **Missing indexes** - Create indexes on filtered columns
2. **Large result set** - Use LIMIT and pagination
3. **Cache miss** - Repeated queries should be cached
4. **Resource constraints** - Increase DuckDB threads/memory

See [Performance Guide](./performance.md).

### Q: Why are webhooks not delivering?

**A:** Common causes:
1. **URL not accessible** - Verify endpoint is reachable
2. **Wrong auth headers** - Check webhook configuration
3. **Timeout** - Your endpoint takes > 30 seconds
4. **Filter mismatch** - Event doesn't match filters

Check delivery history in Admin SPA.

### Q: How do I debug API issues?

**A:**
1. Enable debug logging in config
2. Check API response headers
3. Use curl to test endpoints
4. Check authentication and authorization
5. Review request/response in browser DevTools

---

## Advanced Topics

### Q: Can I use external Iceberg catalogs?

**A:** Yes! Configure Iceberg:
```yaml
analytics:
  storage:
    iceberg:
      catalog_uri: "http://glue-catalog:9082"
      namespace: analytics
```

Supports AWS Glue, Nessie, REST catalogs.

### Q: How do I integrate Analytics with my data lake?

**A:**
1. Export data via webhooks to S3
2. Use DuckDB to read from external Parquet files
3. Mount object storage for Iceberg
4. Replicate to data warehouse via batch job

### Q: Can I run multiple Analytics instances?

**A:** Yes! Architecture:
```
Load Balancer
├─ Analytics Instance 1
├─ Analytics Instance 2
└─ Analytics Instance 3
    ↓
PostgreSQL (shared)
DuckDB replicas (eventual consistency)
S3/Iceberg (shared)
```

---

## Getting Help

### Q: Where can I find more documentation?

**A:** See [Documentation Index](./README.md) for complete list of guides.

### Q: What if I still have questions?

**A:**
1. Check [Troubleshooting Guide](./troubleshooting.md)
2. Review code comments in `modules/analytics/`
3. Search GitHub issues
4. Create new GitHub issue with:
   - Aegion version
   - Exact steps to reproduce
   - Error logs
   - Expected vs actual behavior

---

## Version & Migration

### Q: What version of Analytics am I running?

**A:**
```bash
curl http://localhost:8080/api/v1/analytics/health | jq '.version'
# or check admin SPA: Settings → About
```

### Q: How do I upgrade Analytics?

**A:**
```bash
# Pull latest code
git pull origin beta

# Run migrations
go run ./cmd/migrate up

# Restart service
systemctl restart aegion
```

### Q: What's the upgrade path?

**A:**
- v0 → v1 - Breaking changes, full migration required
- v1.x → v1.y - Backward compatible, migration optional

---

## Related Documentation

- [Setup Guide](./setup.md)
- [Architecture](./architecture.md)
- [Performance Tuning](./performance.md)
- [Troubleshooting](./troubleshooting.md)
