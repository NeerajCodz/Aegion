# Phase 13 - Quick Reference Guide

## 🚀 Quick Start

### Local Development
```bash
# Build analytics module
cd modules/analytics
go test -v ./... -cover

# Run with Docker Compose
docker-compose -f deploy/docker-compose.yml up -d

# Check health
curl http://localhost:8082/health | jq
```

### Testing
```bash
# Run tests (all Go versions in CI)
go test -v ./modules/analytics/...

# With coverage
go test -cover ./modules/analytics/... -coverprofile=coverage.out

# Coverage threshold: 85%
go tool cover -html=coverage.out
```

### Docker
```bash
# Build image
docker build -f modules/analytics/Dockerfile -t aegion-analytics:latest .

# Push to registry
docker push ghcr.io/neerajcodz/aegion/analytics:latest
```

### Kubernetes
```bash
# Deploy
kubectl apply -f deploy/kubernetes/modules/analytics.yaml

# Check status
kubectl get statefulsets -n aegion
kubectl get svc -n aegion

# Logs
kubectl logs -f analytics-0 -n aegion

# Test
kubectl port-forward svc/analytics 8082:8082 -n aegion
curl http://localhost:8082/health
```

### Helm
```bash
# Install
helm install aegion-analytics ./deploy/analytics-helm-chart.yaml \
  -n aegion --create-namespace

# Upgrade
helm upgrade aegion-analytics ./deploy/analytics-helm-chart.yaml -n aegion

# Rollback
helm rollback aegion-analytics -n aegion
```

## 📋 Checklist for Deployment

- [ ] CI/CD pipeline passing
- [ ] Coverage >85%
- [ ] Security scans passing
- [ ] Docker image built
- [ ] K8s manifests applied
- [ ] Health checks responding
- [ ] Metrics endpoint working
- [ ] Migrations completed
- [ ] Data persisted
- [ ] Monitoring configured

## 🔍 Health Check Endpoints

| Endpoint | Purpose | Usage |
|----------|---------|-------|
| `/health` | Service health | Manual checks |
| `/ready` | Readiness probe | K8s readinessProbe |
| `/live` | Liveness probe | K8s livenessProbe |
| `/metrics` | Prometheus metrics | Monitoring |

## 📊 Metrics Available

- `analytics_cache_hit_rate` - Cache hit ratio
- `analytics_query_latency_p95_ms` - 95th percentile latency
- `analytics_total_queries` - Total queries executed
- `analytics_cached_queries` - Queries from cache
- `analytics_health` - Health status (1=healthy)

## 🐳 Docker Ports

- `8082` - REST API
- `50051` - gRPC
- `9090` - Prometheus metrics

## 🏗️ Architecture

```
┌─────────────────────────────────────┐
│    Analytics Service                 │
├─────────────────────────────────────┤
│ REST (8082) │ gRPC (50051)          │
├─────────────────────────────────────┤
│           DuckDB OLAP                │
├─────────────────────────────────────┤
│      PostgreSQL (Sync Source)        │
└─────────────────────────────────────┘
     ↓
┌─────────────────────────────────────┐
│   Prometheus Metrics (9090)          │
├─────────────────────────────────────┤
│ Cache Hit Rate │ Query Latency      │
│ Total Queries  │ Health Status      │
└─────────────────────────────────────┘
```

## 🔧 Configuration

See `modules/analytics/config.yaml` for all available options:
- Server ports
- Database settings
- Cache configuration
- Query timeouts
- Sync intervals
- Export formats
- Retention policies
- Security settings

## 📚 Files Overview

| File | Purpose |
|------|---------|
| `.github/workflows/analytics-ci.yml` | CI/CD pipeline |
| `modules/analytics/Dockerfile` | Container image |
| `modules/analytics/rest/health.go` | Health endpoints |
| `modules/analytics/migrations.go` | Migration utilities |
| `deploy/docker-compose.yml` | Local development |
| `deploy/kubernetes/modules/analytics.yaml` | K8s manifests |
| `deploy/analytics-helm-chart.yaml` | Helm chart |
| `PHASE13_CICD_DEPLOYMENT.md` | Full guide |

## 🐛 Troubleshooting

### Pod won't start
```bash
kubectl describe pod analytics-0 -n aegion
kubectl logs analytics-0 -n aegion
```

### Health checks failing
```bash
kubectl port-forward svc/analytics 8082:8082 -n aegion
curl http://localhost:8082/health
```

### Database connection issues
```bash
kubectl exec -it analytics-0 -n aegion -- \
  pg_isready -h postgres -p 5432
```

### Migrations failed
```bash
kubectl logs analytics-0 -n aegion | grep migration
```

## ✅ Success Indicators

- ✓ GitHub Actions: All jobs passing
- ✓ Coverage: >85%
- ✓ Docker: Image builds successfully
- ✓ K8s: Pods running and healthy
- ✓ Health: All endpoints returning 200
- ✓ Metrics: Prometheus scrape succeeds
- ✓ Logs: JSON formatted in stdout

## 📞 Support

For issues or questions:
1. Check `PHASE13_CICD_DEPLOYMENT.md` for detailed guide
2. Review GitHub Actions logs
3. Check pod logs: `kubectl logs <pod> -n aegion`
4. Verify configuration: `kubectl get configmaps -n aegion`

## 🎯 Next Steps

1. Deploy to staging
2. Run smoke tests
3. Monitor metrics
4. Deploy to production
5. Set up alerting
6. Document runbooks
