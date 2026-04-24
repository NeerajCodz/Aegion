# Phase 13 - Deployment Checklist

## Pre-Deployment Verification

### ✓ Code Quality
- [x] GitHub Actions workflow passes
- [x] Code coverage >85%
- [x] All linting checks pass
- [x] Security scans pass (gosec)
- [x] No hardcoded secrets
- [x] All dependencies resolved

### ✓ Testing
- [x] Unit tests pass (all Go versions)
- [x] Integration tests pass
- [x] Migration tests pass
- [x] E2E tests pass
- [x] Rollback scenarios tested

### ✓ Docker
- [x] Dockerfile builds successfully
- [x] Multi-stage build optimized
- [x] Non-root user configured
- [x] Health check endpoint works
- [x] Image size acceptable
- [x] Layers cached efficiently

### ✓ Kubernetes
- [x] YAML manifests valid
- [x] Resource limits set
- [x] RBAC policies defined
- [x] NetworkPolicy configured
- [x] PVC storage class exists
- [x] Ingress configuration valid

### ✓ Configuration
- [x] All environment variables documented
- [x] Secrets properly handled
- [x] ConfigMaps created
- [x] Default values sensible
- [x] Logging configured
- [x] Metrics enabled

### ✓ Documentation
- [x] Deployment guide complete
- [x] Configuration options documented
- [x] Troubleshooting guide included
- [x] Quick reference available
- [x] Architecture documented
- [x] Health checks explained

## Staging Deployment

### Prerequisites
```bash
# Required
- kubectl access to staging cluster
- Helm 3+ installed
- Docker registry credentials
- PostgreSQL connection string
- S3 credentials (if using archival)
```

### Deployment Steps

#### 1. Prepare Secrets
```bash
# Create namespace
kubectl create namespace aegion-staging

# Create secrets
kubectl create secret generic postgres-secret \
  -n aegion-staging \
  --from-literal=url="postgres://user:pass@postgres:5432/db"

kubectl create secret generic internal-secret \
  -n aegion-staging \
  --from-literal=secret="your-internal-secret"
```

#### 2. Deploy with Kubectl
```bash
# Apply manifests
kubectl apply -f deploy/kubernetes/modules/analytics.yaml

# Verify deployment
kubectl get statefulsets -n aegion -w
kubectl get pods -n aegion -w
```

#### 3. Deploy with Helm
```bash
# Create values override for staging
cat > staging-values.yaml <<EOF
replicaCount: 2
image:
  tag: staging-latest
ingress:
  hosts:
    - host: analytics-staging.example.com
      paths:
        - path: /
          pathType: Prefix
resources:
  requests:
    memory: "256Mi"
    cpu: "200m"
  limits:
    memory: "1Gi"
    cpu: "1000m"
EOF

# Install
helm install aegion-analytics ./deploy/analytics-helm-chart.yaml \
  -n aegion-staging \
  -f staging-values.yaml
```

#### 4. Verify Health
```bash
# Wait for pods to be ready
kubectl wait --for=condition=ready pod \
  -l app=analytics \
  -n aegion-staging \
  --timeout=300s

# Port forward
kubectl port-forward -n aegion-staging svc/analytics 8082:8082 &

# Health checks
curl http://localhost:8082/health | jq
curl http://localhost:8082/ready | jq
curl http://localhost:8082/live | jq
curl http://localhost:8082/metrics | head -20
```

#### 5. Run Smoke Tests
```bash
# Test data retrieval
curl -X GET http://localhost:8082/events \
  -H "Authorization: Bearer token"

# Test query execution
curl -X POST http://localhost:8082/events/search \
  -H "Content-Type: application/json" \
  -d '{"page": 1, "page_size": 10}'

# Test metrics export
curl http://localhost:8082/metrics | grep analytics_health
```

#### 6. Monitor Logs
```bash
# Real-time logs
kubectl logs -f analytics-0 -n aegion-staging

# Search for errors
kubectl logs analytics-0 -n aegion-staging | grep ERROR

# Check migrations
kubectl logs analytics-0 -n aegion-staging | grep migration
```

## Production Deployment

### Pre-Production Checklist
- [ ] Staging deployment successful (24h+ running)
- [ ] No error logs in staging
- [ ] Performance metrics acceptable
- [ ] Backups verified
- [ ] Runbook reviewed
- [ ] Team trained
- [ ] Rollback plan documented
- [ ] Change window scheduled

### Production Deployment
```bash
# 1. Create production namespace
kubectl create namespace aegion-prod

# 2. Create secrets
kubectl create secret generic postgres-secret \
  -n aegion-prod \
  --from-literal=url="postgres://prod-user:prod-pass@prod-postgres:5432/db"

# 3. Deploy
helm install aegion-analytics ./deploy/analytics-helm-chart.yaml \
  -n aegion-prod \
  -f prod-values.yaml

# 4. Verify
kubectl get all -n aegion-prod
kubectl wait --for=condition=ready pod -l app=analytics -n aegion-prod --timeout=600s

# 5. Test
kubectl port-forward -n aegion-prod svc/analytics 8082:8082
curl http://localhost:8082/health
```

### Post-Deployment

#### Monitoring
- [ ] Prometheus scraping analytics metrics
- [ ] Grafana dashboards displaying data
- [ ] Alerts configured
- [ ] Log aggregation working
- [ ] Tracing spans visible

#### Operations
- [ ] Health checks passing
- [ ] Metrics endpoint responsive
- [ ] Queries executing normally
- [ ] Cache hit rate acceptable
- [ ] No error spikes

#### Validation
- [ ] Data integrity verified
- [ ] Performance baseline established
- [ ] Backups running
- [ ] Security scans clean
- [ ] RBAC policies working

## Rollback Procedure

### If Issues Detected
```bash
# 1. Check status
kubectl describe pod analytics-0 -n aegion-prod
kubectl logs analytics-0 -n aegion-prod | tail -50

# 2. Rollback with Helm
helm rollback aegion-analytics -n aegion-prod

# 3. Verify rollback
kubectl get pods -n aegion-prod
kubectl logs analytics-0 -n aegion-prod | grep "started"

# 4. If manual rollback needed
kubectl delete statefulset analytics -n aegion-prod
helm install aegion-analytics ./deploy/analytics-helm-chart.yaml \
  -n aegion-prod \
  -f prod-values.yaml \
  --set image.tag=previous-stable-tag
```

## Post-Deployment Verification

### Health Checks
```bash
# /health endpoint
curl http://analytics:8082/health | jq '.' | verify:
- status should be "healthy"
- services.database should be "up"
- version should match deployment
- uptime should be > 0

# /ready endpoint
curl http://analytics:8082/ready | verify:
- ready should be true
- services.database should be true
- status 200 OK

# /live endpoint
curl http://analytics:8082/live | verify:
- alive should be true
- status 200 OK

# /metrics endpoint
curl http://analytics:9090/metrics | verify:
- analytics_health = 1
- analytics_cache_hit_rate >= 0
- analytics_total_queries >= 0
```

### Performance Metrics
```bash
# Query latency P95 (should be < 500ms)
curl http://analytics:9090/metrics | grep query_latency_p95_ms

# Cache hit rate (should be > 50%)
curl http://analytics:9090/metrics | grep cache_hit_rate

# Total queries (should be increasing)
curl http://analytics:9090/metrics | grep total_queries
```

### Data Validation
```bash
# Verify sync lag < 5 minutes
curl http://analytics:8082/health | jq '.sync_lag_ms'

# Verify data in DuckDB
kubectl exec -it analytics-0 -- duckdb /data/analytics.db
# Run: SELECT COUNT(*) FROM events;

# Verify PostgreSQL sync
kubectl exec -it postgres -- psql -U user -c "SELECT COUNT(*) FROM events;"
```

### Log Analysis
```bash
# Check for errors
kubectl logs analytics-0 | grep ERROR | wc -l

# Check migrations
kubectl logs analytics-0 | grep "migration\|Migration"

# Check connections
kubectl logs analytics-0 | grep "connection\|Connection" | tail -10
```

## Maintenance Tasks

### Weekly
- [ ] Review health metrics
- [ ] Check disk usage
- [ ] Review error logs
- [ ] Backup verification

### Monthly
- [ ] Update dependencies
- [ ] Review security scans
- [ ] Capacity planning
- [ ] Performance analysis

### Quarterly
- [ ] Major version updates
- [ ] Disaster recovery drill
- [ ] Documentation update
- [ ] Team training

## Support & Escalation

### Tier 1 - On Call
```bash
# Quick checks
curl http://analytics:8082/health
kubectl logs analytics-0 -n aegion-prod | tail -100
kubectl top pod analytics-0 -n aegion-prod
```

### Tier 2 - Engineering
```bash
# Deep dive
kubectl describe pod analytics-0 -n aegion-prod
kubectl get events -n aegion-prod --sort-by='.lastTimestamp'
kubectl exec -it analytics-0 -- /bin/bash
```

### Tier 3 - Architecture
```bash
# Escalation
- Review architecture documentation
- Check K8s cluster health
- Analyze Prometheus metrics
- Review recent changes (git log)
```

## Sign-Off

**Deployment Date:** _______________

**Deployed By:** _______________

**Approved By:** _______________

**Rollback Status:** _______________

**Notes:** ________________________________________________

---

**Version:** 1.0
**Last Updated:** 2024-01-15
**Next Review:** 2024-02-15
