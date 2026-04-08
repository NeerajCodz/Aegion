# Aegion Operational Runbooks

## Overview

This document provides step-by-step procedures for common operational tasks, incidents, and emergencies for the Aegion identity platform.

---

## Table of Contents

1. [Deployment Procedures](#deployment-procedures)
2. [Incident Response](#incident-response)
3. [Scaling Procedures](#scaling-procedures)
4. [Backup and Recovery](#backup-and-recovery)
5. [Troubleshooting](#troubleshooting)
6. [Maintenance Windows](#maintenance-windows)

---

## 1. Deployment Procedures

### 1.1 Production Deployment (Zero Downtime)

**Prerequisites**:
- [ ] Staging deployment tested and validated
- [ ] Database migrations reviewed and dry-run executed
- [ ] Rollback plan documented
- [ ] Team notified (deploy window scheduled)
- [ ] Monitoring dashboards open

**Procedure**:

```bash
# Step 1: Pre-deployment health check
curl https://auth.example.com/health
# Expected: HTTP 200

# Step 2: Database backup (CRITICAL)
pg_dump -h $DB_HOST -U aegion aegion > backup_$(date +%Y%m%d_%H%M%S).sql

# Step 3: Pull new images
docker compose -f deploy/docker-compose.yml pull

# Step 4: Run migrations (if any)
docker compose run --rm aegion migrate up
# Verify: Check migration logs for errors

# Step 5: Rolling restart (one container at a time for HA)
# For Docker Compose:
docker compose -f deploy/docker-compose.yml up -d --no-deps --scale aegion=2 aegion
sleep 30  # Wait for new container to be healthy
docker compose -f deploy/docker-compose.yml up -d --no-deps --scale aegion=1 aegion

# For Kubernetes:
kubectl set image deployment/aegion aegion=aegion/core:v1.2.0 -n aegion
kubectl rollout status deployment/aegion -n aegion
# Monitor: kubectl get pods -n aegion -w

# Step 6: Verify deployment
curl https://auth.example.com/health
curl https://auth.example.com/.well-known/aegion/meta

# Step 7: Smoke tests
./scripts/smoke-tests.sh production

# Step 8: Monitor for 15 minutes
# Watch: Error rates, response times, active sessions
# Dashboards: Grafana production board
```

**Rollback Procedure** (if needed):

```bash
# Step 1: Stop new containers
docker compose -f deploy/docker-compose.yml down

# Step 2: Restore previous version
docker compose -f deploy/docker-compose.yml up -d --scale aegion=3

# Step 3: Rollback migrations (if applied)
docker compose run --rm aegion migrate down 1

# Step 4: Restore database (if corrupted)
psql -h $DB_HOST -U aegion aegion < backup_YYYYMMDD_HHMMSS.sql

# Step 5: Verify
curl https://auth.example.com/health
```

**Success Criteria**:
- ✓ Health endpoint returns 200
- ✓ Meta endpoint shows new version
- ✓ Error rate < 0.1%
- ✓ P95 latency < 200ms
- ✓ No increase in failed logins

### 1.2 Emergency Hotfix Deployment

**When to Use**: Critical security vulnerability or data corruption

**Procedure**:

```bash
# Step 1: Notify team immediately
# Slack: #incidents channel

# Step 2: Fast-track code review
# Get approval from 2+ senior engineers

# Step 3: Build and tag hotfix
git checkout production
git cherry-pick <fix-commit-sha>
git tag v1.1.1-hotfix
docker build -f build/Dockerfile.core -t aegion/core:v1.1.1-hotfix .

# Step 4: Deploy to production (skip staging)
kubectl set image deployment/aegion aegion=aegion/core:v1.1.1-hotfix -n aegion

# Step 5: Monitor closely for 30 minutes
# Full team on call

# Step 6: Post-mortem within 24 hours
# Document: What, Why, How, Prevention
```

---

## 2. Incident Response

### 2.1 Service Down (P0 - Critical)

**Symptoms**: Health checks failing, no traffic routing

**Immediate Actions**:

```bash
# Step 1: Acknowledge alert
# Slack: "ACK - investigating service down"

# Step 2: Check container status
docker ps -a | grep aegion
# OR
kubectl get pods -n aegion

# Step 3: Check logs (last 100 lines)
docker logs --tail 100 aegion-core-1
# OR
kubectl logs -n aegion deployment/aegion --tail=100

# Step 4: Common fixes:

# Fix 1: Container crashed - restart
docker compose -f deploy/docker-compose.yml restart aegion
# OR
kubectl rollout restart deployment/aegion -n aegion

# Fix 2: Database connection lost
# Check DB health:
docker exec aegion-postgres-1 pg_isready
# Restart if needed:
docker compose restart postgres

# Fix 3: Out of memory
# Check memory usage:
docker stats aegion-core-1 --no-stream
# Scale up if needed:
docker compose up -d --scale aegion=3

# Step 5: Verify recovery
curl https://auth.example.com/health

# Step 6: Update incident timeline
# Slack: "Service restored - investigating root cause"
```

**Root Cause Investigation**:

```bash
# Check system resources
df -h  # Disk space
free -h  # Memory
top  # CPU usage

# Check database queries
psql -h $DB_HOST -U aegion -c "SELECT * FROM pg_stat_activity WHERE state = 'active';"

# Check for deadlocks
psql -h $DB_HOST -U aegion -c "SELECT * FROM pg_locks WHERE NOT GRANTED;"

# Export logs for analysis
docker logs aegion-core-1 > incident_$(date +%Y%m%d_%H%M%S).log
```

### 2.2 High Error Rate (P1 - High)

**Symptoms**: Error rate > 5%, increased 5xx responses

**Investigation Steps**:

```bash
# Step 1: Identify error source
# Check logs for error patterns:
docker logs aegion-core-1 | grep ERROR | tail -50

# Step 2: Check upstream dependencies
curl http://localhost:8080/health/dependencies

# Step 3: Database issues?
# Check connection pool:
docker exec aegion-core-1 netstat -an | grep 5432

# Check slow queries:
psql -h $DB_HOST -U aegion -c "SELECT * FROM pg_stat_statements ORDER BY mean_exec_time DESC LIMIT 10;"

# Step 4: Redis issues?
docker exec aegion-redis-1 redis-cli INFO stats

# Step 5: Module issues?
# Check module health individually:
curl http://localhost:9003/health  # admin
curl http://localhost:9005/health  # oauth2

# Step 6: Apply fix based on findings
# Common fixes:
# - Increase connection pool: Update aegion.yaml, restart
# - Clear stuck locks: psql -c "SELECT pg_cancel_backend(pid) FROM pg_stat_activity WHERE state = 'active' AND query_start < now() - interval '5 minutes';"
# - Flush Redis cache: docker exec aegion-redis-1 redis-cli FLUSHDB
```

### 2.3 Performance Degradation (P2 - Medium)

**Symptoms**: P95 latency > 1s, slow response times

**Investigation Steps**:

```bash
# Step 1: Measure current performance
# Use Apache Bench:
ab -n 1000 -c 10 https://auth.example.com/health

# Step 2: Check database performance
# Enable query logging temporarily:
psql -h $DB_HOST -U aegion -c "ALTER SYSTEM SET log_min_duration_statement = '100ms';"
psql -h $DB_HOST -U aegion -c "SELECT pg_reload_conf();"

# Watch slow queries:
tail -f /var/log/postgresql/postgresql.log | grep "duration:"

# Step 3: Check cache hit ratio
docker exec aegion-redis-1 redis-cli INFO stats | grep keyspace

# Step 4: Profile application
# Enable pprof endpoint (dev only):
curl http://localhost:8080/debug/pprof/profile?seconds=30 > cpu.prof
go tool pprof cpu.prof

# Step 5: Common fixes:
# - Add database indexes
# - Increase cache TTL
# - Scale horizontally (add more containers)
# - Optimize slow queries
```

### 2.4 Security Incident (P0 - Critical)

**Symptoms**: Unauthorized access, suspicious activity, data breach

**Immediate Actions**:

```bash
# Step 1: STOP - Do not restart services
# Preserve evidence for forensics

# Step 2: Isolate affected systems
# Block network access:
iptables -A INPUT -p tcp --dport 8080 -j DROP
# OR
kubectl scale deployment aegion --replicas=0 -n aegion

# Step 3: Capture evidence
# Full memory dump:
docker exec aegion-core-1 gcore $(docker exec aegion-core-1 pidof aegion)

# Full logs:
docker logs aegion-core-1 > security_incident_$(date +%Y%m%d_%H%M%S).log

# Database dump:
pg_dump -h $DB_HOST -U aegion aegion > incident_db_$(date +%Y%m%d_%H%M%S).sql

# Step 4: Notify security team
# Email: security@company.com
# Subject: "SECURITY INCIDENT - Aegion Identity Platform"

# Step 5: Rotate all secrets
# Immediately rotate:
# - Database passwords
# - API keys
# - Session secrets
# - JWT signing keys

# Step 6: Force logout all sessions
redis-cli FLUSHDB  # Clears all sessions

# Step 7: Enable audit logging
# Set log level to DEBUG temporarily:
docker compose -f deploy/docker-compose.yml exec aegion kill -USR1 1

# Step 8: Forensic analysis
# Analyze logs for:
# - Failed login attempts
# - Privilege escalation
# - Data exfiltration
# - Unusual API calls
```

**Post-Incident**:
- [ ] Change all credentials
- [ ] Review and tighten access controls
- [ ] Apply security patches
- [ ] Update firewall rules
- [ ] Notify affected users (if applicable)
- [ ] File incident report

---

## 3. Scaling Procedures

### 3.1 Horizontal Scaling (Add More Instances)

**When to Scale Up**:
- CPU usage > 70% sustained for 10+ minutes
- Memory usage > 80% sustained for 10+ minutes
- Request queue depth > 50
- P95 latency > 500ms

**Procedure**:

```bash
# Docker Compose:
docker compose -f deploy/docker-compose.yml up -d --scale aegion=5

# Kubernetes:
kubectl scale deployment aegion --replicas=5 -n aegion

# Verify:
kubectl get pods -n aegion
curl https://auth.example.com/health  # Should hit different instances

# Update load balancer if needed
```

**When to Scale Down**:
- CPU usage < 30% sustained for 30+ minutes
- Cost optimization needed
- Maintenance window

```bash
# Gradual scale down (one at a time):
kubectl scale deployment aegion --replicas=4 -n aegion
# Wait 5 minutes, monitor
kubectl scale deployment aegion --replicas=3 -n aegion
```

### 3.2 Vertical Scaling (Increase Resources)

**Procedure**:

```bash
# Kubernetes - Update resource limits:
kubectl edit deployment aegion -n aegion

# Change:
resources:
  limits:
    cpu: 2000m      # was: 1000m
    memory: 2Gi     # was: 1Gi
  requests:
    cpu: 1000m      # was: 500m
    memory: 1Gi     # was: 512Mi

# Apply:
kubectl rollout restart deployment/aegion -n aegion

# Docker Compose - Update docker-compose.yml:
services:
  aegion:
    deploy:
      resources:
        limits:
          cpus: '2'
          memory: 2G

# Restart:
docker compose -f deploy/docker-compose.yml up -d
```

---

## 4. Backup and Recovery

### 4.1 Daily Backup

**Automated Daily Backup** (configure in cron):

```bash
#!/bin/bash
# /etc/cron.daily/aegion-backup.sh

DATE=$(date +%Y%m%d)
BACKUP_DIR="/backups/aegion"

# Database backup
pg_dump -h $DB_HOST -U aegion aegion | gzip > $BACKUP_DIR/db_$DATE.sql.gz

# Configuration backup
kubectl get configmap aegion-config -n aegion -o yaml > $BACKUP_DIR/config_$DATE.yaml
kubectl get secret aegion-secrets -n aegion -o yaml > $BACKUP_DIR/secrets_$DATE.yaml

# Retention: Keep 30 days
find $BACKUP_DIR -name "*.sql.gz" -mtime +30 -delete
find $BACKUP_DIR -name "*.yaml" -mtime +30 -delete

# Upload to S3 (optional)
aws s3 sync $BACKUP_DIR s3://aegion-backups/$(date +%Y/%m)/
```

### 4.2 Point-in-Time Recovery

**Scenario**: Need to restore database to specific timestamp

**Procedure**:

```bash
# Step 1: Stop application
kubectl scale deployment aegion --replicas=0 -n aegion

# Step 2: Create current backup (safety)
pg_dump -h $DB_HOST -U aegion aegion > current_$(date +%Y%m%d_%H%M%S).sql

# Step 3: Restore from backup
gunzip < /backups/aegion/db_20260408.sql.gz | psql -h $DB_HOST -U aegion aegion

# Step 4: Verify data
psql -h $DB_HOST -U aegion aegion -c "SELECT COUNT(*) FROM identities;"

# Step 5: Restart application
kubectl scale deployment aegion --replicas=3 -n aegion

# Step 6: Test functionality
curl https://auth.example.com/health
```

### 4.3 Disaster Recovery (Complete Outage)

**Scenario**: Data center failure, complete infrastructure loss

**Procedure**:

```bash
# Step 1: Spin up new infrastructure
# Use IaC (Terraform/Pulumi) to recreate:
cd infrastructure/
terraform apply -var-file=production.tfvars

# Step 2: Restore database from latest backup
# Download from S3:
aws s3 cp s3://aegion-backups/latest/db.sql.gz /tmp/
gunzip < /tmp/db.sql.gz | psql -h $NEW_DB_HOST -U aegion aegion

# Step 3: Restore secrets
# From encrypted backup:
gpg --decrypt backups/secrets.yaml.gpg | kubectl apply -f -

# Step 4: Deploy application
helm install aegion ./charts/aegion -f values-prod.yaml -n aegion

# Step 5: Update DNS
# Point auth.example.com to new load balancer

# Step 6: Verify
curl https://auth.example.com/health

# Step 7: Communicate with users
# Email: "Service restored after outage"
```

**Recovery Time Objective (RTO)**: 2 hours  
**Recovery Point Objective (RPO)**: 24 hours (daily backups)

---

## 5. Troubleshooting

### 5.1 Common Issues

#### Issue: "Database connection refused"

**Symptoms**: Application logs show connection errors

**Fix**:

```bash
# Check DB is running:
docker ps | grep postgres

# Check connection string:
docker exec aegion-core-1 env | grep DATABASE_URL

# Test connection manually:
docker exec aegion-postgres-1 psql -U aegion -c "SELECT 1"

# Fix: Restart database
docker compose restart postgres
```

#### Issue: "Redis connection timeout"

**Symptoms**: Slow response times, cache misses

**Fix**:

```bash
# Check Redis is running:
docker exec aegion-redis-1 redis-cli ping

# Check memory:
docker exec aegion-redis-1 redis-cli INFO memory

# Fix: Increase maxmemory or restart
docker exec aegion-redis-1 redis-cli CONFIG SET maxmemory 512mb
docker compose restart redis
```

#### Issue: "JWT verification failed"

**Symptoms**: Authentication errors, invalid token errors

**Fix**:

```bash
# Check JWKS endpoint:
curl http://localhost:8080/.well-known/jwks.json

# Verify token:
jwt decode <token>

# Fix: Rotate keys if compromised
docker exec aegion-core-1 aegion keys rotate
```

#### Issue: "Module not responding"

**Symptoms**: Module health checks failing

**Fix**:

```bash
# Check module logs:
docker logs aegion-module-oauth2-1 --tail 100

# Restart module:
docker compose restart module-oauth2

# Check gRPC connectivity:
grpcurl -plaintext localhost:9005 grpc.health.v1.Health/Check
```

### 5.2 Performance Troubleshooting

**Slow Login Flow**:

```bash
# Enable query logging:
psql -c "SET log_min_duration_statement = 0;"

# Perform login, then check slow queries:
psql -c "SELECT query, calls, mean_exec_time FROM pg_stat_statements ORDER BY mean_exec_time DESC LIMIT 10;"

# Add missing indexes:
psql -c "CREATE INDEX CONCURRENTLY idx_identities_email ON identities(email);"
```

**High Memory Usage**:

```bash
# Check Go heap profile:
curl http://localhost:8080/debug/pprof/heap > heap.prof
go tool pprof -http=:8081 heap.prof

# Common fixes:
# - Increase GOMEMLIMIT
# - Reduce connection pool size
# - Enable connection pooling (PgBouncer)
```

---

## 6. Maintenance Windows

### 6.1 Planned Maintenance

**Schedule**: Second Tuesday of each month, 02:00-04:00 UTC

**Communication**:
- T-7 days: Email notification to users
- T-1 day: Status page banner
- T-1 hour: Slack reminder to team

**Procedure**:

```bash
# T-0: Enable maintenance mode
curl -X POST https://auth.example.com/admin/maintenance/enable \
  -H "Authorization: Bearer $ADMIN_TOKEN"

# T+5min: Apply updates
# - Database maintenance (VACUUM, ANALYZE)
# - Certificate renewal
# - Security patches
# - Version upgrades

# T+1h: Smoke tests
./scripts/smoke-tests.sh production

# T+1h30min: Disable maintenance mode
curl -X POST https://auth.example.com/admin/maintenance/disable \
  -H "Authorization: Bearer $ADMIN_TOKEN"

# T+2h: Send all-clear notification
```

### 6.2 Emergency Maintenance

**When**: Critical security patch, data corruption

**Procedure**:

```bash
# Step 1: Notify immediately
# Slack: @channel Emergency maintenance starting NOW

# Step 2: Enable maintenance mode
# Display maintenance page to users

# Step 3: Apply fix (hotfix procedure)

# Step 4: Verify fix

# Step 5: Disable maintenance mode

# Step 6: Post-mortem within 4 hours
```

---

## Appendix: Contact List

**On-Call Rotation**:
- Primary: ops-primary@company.com
- Secondary: ops-secondary@company.com
- Escalation: cto@company.com

**Vendor Support**:
- Cloud Provider: support@aws.amazon.com
- Database: support@postgresql.org
- Monitoring: support@grafana.com

**Internal Teams**:
- Security: security@company.com
- Platform: platform@company.com
- Product: product@company.com

---

**Document Version**: 1.0  
**Last Updated**: 2026-04-08  
**Next Review**: 2026-07-08  
**Owner**: Platform Team
