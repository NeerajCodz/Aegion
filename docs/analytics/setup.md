# Analytics Setup & Deployment Guide

**Version:** 1.0  
**Last Updated:** 2026-04-24  
**Module:** `modules/analytics`

## Quick Start

### Prerequisites
- Go 1.21+
- PostgreSQL 14+
- Docker & Docker Compose (recommended)
- Git

### 5-Minute Local Setup

```bash
# 1. Clone repository
git clone https://github.com/neerajcodz/aegion.git
cd aegion

# 2. Start services with Docker Compose
docker-compose -f deploy/docker-compose.yml up -d

# 3. Verify services
docker-compose ps

# 4. Run migrations
go run ./cmd/migrate up

# 5. Test analytics endpoint
curl -H "Authorization: Bearer test-token" \
  http://localhost:8080/api/v1/analytics/health
```

Expected response:
```json
{
  "status": "healthy",
  "timestamp": "2026-04-24T10:30:00Z",
  "components": {
    "database": "ready",
    "sync": "running",
    "storage": "ready"
  }
}
```

---

## Local Development Setup

### 1. Configure Environment

Create `.env` file in project root:
```env
# Database
POSTGRES_USER=analytics
POSTGRES_PASSWORD=dev_password_123
POSTGRES_DB=aegion
POSTGRES_HOST=localhost
POSTGRES_PORT=5432

# Analytics
ANALYTICS_ENABLED=true
ANALYTICS_DUCKDB_DIR=./data/duckdb
ANALYTICS_DUCKDB_THREADS=4
ANALYTICS_DUCKDB_MEMORY_LIMIT_GB=2

# Sync
ANALYTICS_SYNC_ENABLE_REAL_TIME=true
ANALYTICS_SYNC_ENABLE_BATCH=true
ANALYTICS_SYNC_BATCH_SCHEDULE="*/5 * * * *"

# REST API
ANALYTICS_REST_ENABLED=true
ANALYTICS_REST_ENDPOINT=/api/v1/analytics
ANALYTICS_REST_QUERY_TIMEOUT=30
ANALYTICS_REST_RATE_LIMIT=600

# GraphQL
ANALYTICS_GRAPHQL_ENABLED=true

# gRPC
ANALYTICS_GRPC_ENABLED=true
ANALYTICS_GRPC_PORT=50051
```

### 2. Start PostgreSQL

```bash
# Using Docker
docker run -d \
  --name aegion_postgres \
  -e POSTGRES_USER=analytics \
  -e POSTGRES_PASSWORD=dev_password_123 \
  -e POSTGRES_DB=aegion \
  -p 5432:5432 \
  postgres:15-alpine

# Or use existing PostgreSQL:
psql postgresql://analytics:dev_password_123@localhost:5432/aegion
```

### 3. Initialize DuckDB

```bash
# Create data directory
mkdir -p ./data/duckdb

# DuckDB initializes automatically on first connection
# The database file will be created at ./data/duckdb/analytics.duckdb
```

### 4. Run Migrations

```bash
# View pending migrations
go run ./cmd/aegion migrate status

# Run all migrations
go run ./cmd/aegion migrate up

# Verify schema
go run ./cmd/aegion migrate verify
```

### 5. Start Aegion

```bash
# Build
make build

# Run
./aegion server

# Or with hot reload (requires entr or similar)
make dev
```

Verify services:
```bash
# REST API
curl -H "Authorization: Bearer test-token" \
  http://localhost:8080/api/v1/analytics/health

# GraphQL
curl -X POST \
  -H "Authorization: Bearer test-token" \
  -H "Content-Type: application/json" \
  -d '{"query": "{ health { status } }"}' \
  http://localhost:8080/api/v1/analytics/graphql

# gRPC (using grpcurl)
grpcurl -plaintext localhost:50051 list analytics.Analytics
```

### 6. Start Admin SPA (Optional)

```bash
cd modules/admin/spa
npm install
npm run dev

# Open browser to http://localhost:3000
```

---

## Docker Compose Setup

### Single Command Start

```bash
docker-compose -f deploy/docker-compose.yml up -d
```

### Included Services
- `aegion` - Core + Analytics
- `postgres` - PostgreSQL 15
- `duckdb` - DuckDB (via embedded connection)
- `redis` - Redis (for sessions/cache)

### Verify Deployment

```bash
# Check all services
docker-compose ps

# View logs
docker-compose logs -f aegion

# Test health
curl http://localhost:8080/api/v1/analytics/health
```

### Stop Services

```bash
docker-compose down

# Also remove data volumes (WARNING: destructive)
docker-compose down -v
```

### Custom Configuration

Edit `deploy/docker-compose.yml`:
```yaml
services:
  aegion:
    environment:
      - ANALYTICS_DUCKDB_THREADS=8
      - ANALYTICS_DUCKDB_MEMORY_LIMIT_GB=4
      - ANALYTICS_REST_RATE_LIMIT=1000
```

Then restart:
```bash
docker-compose up -d aegion
```

---

## Kubernetes Deployment

### Prerequisites
- Kubernetes 1.20+
- kubectl configured
- Helm 3+ (optional but recommended)

### Using Helm

```bash
# Add Aegion Helm repo
helm repo add aegion https://helm.neerajcodz.com/aegion
helm repo update

# Install with analytics enabled
helm install aegion aegion/aegion \
  --namespace analytics \
  --create-namespace \
  --values deploy/helm/values-analytics.yaml

# Verify installation
kubectl get pods -n analytics
```

### Manual Kubernetes Deployment

```bash
# Create namespace
kubectl create namespace analytics

# Create ConfigMap
kubectl create configmap aegion-analytics-config \
  --from-file=aegion.yaml=configs/aegion.yaml \
  -n analytics

# Create Secrets
kubectl create secret generic aegion-analytics-secrets \
  --from-literal=postgres-password=YOUR_PASSWORD \
  --from-literal=db-url=postgresql://user:pass@postgres:5432/aegion \
  -n analytics

# Deploy Aegion
kubectl apply -f deploy/k8s/aegion-deployment.yaml -n analytics

# Deploy PostgreSQL (if not already running)
kubectl apply -f deploy/k8s/postgres-statefulset.yaml -n analytics

# Deploy Webhooks Queue (optional)
kubectl apply -f deploy/k8s/redis-deployment.yaml -n analytics

# Expose services
kubectl expose deployment aegion \
  --type LoadBalancer \
  --port 8080 \
  --target-port 8080 \
  -n analytics
```

### Verify Kubernetes Deployment

```bash
# Check pods
kubectl get pods -n analytics

# View logs
kubectl logs -n analytics deployment/aegion

# Port forward for testing
kubectl port-forward -n analytics svc/aegion 8080:8080

# Test health endpoint
curl http://localhost:8080/api/v1/analytics/health
```

### Scale Deployment

```bash
# Scale API instances (read-only)
kubectl scale deployment aegion \
  --replicas=3 \
  -n analytics

# Check replication
kubectl get pods -n analytics
```

### Persistent Storage

```bash
# Create PersistentVolumeClaim for DuckDB
kubectl apply -f - <<EOF
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: duckdb-data
  namespace: analytics
spec:
  accessModes:
    - ReadWriteOnce
  resources:
    requests:
      storage: 100Gi
  storageClassName: fast-ssd
EOF

# Mount in deployment via volumeMounts
```

---

## DuckDB Configuration

### File-Based Storage

```yaml
analytics:
  duckdb:
    data_dir: "./data/duckdb"
    read_only: false
```

Data stored at: `./data/duckdb/analytics.duckdb`

### In-Memory Storage

```yaml
analytics:
  duckdb:
    data_dir: ":memory:"
    read_only: false
```

⚠️ Data lost on restart. Use only for testing.

### Performance Tuning

```yaml
analytics:
  duckdb:
    threads: 8              # Match CPU cores
    memory_limit_gb: 16     # Leave margin for OS
    connection_pool_size: 50
    enable_parallel: true
    max_memory_per_thread_gb: 2
```

### Backup

```bash
# Export schema
duckdb ./data/duckdb/analytics.duckdb \
  "SELECT sql FROM duckdb_schemas();" > schema.sql

# Full backup
cp -r ./data/duckdb ./data/duckdb.backup

# Or via DuckDB backup command
duckdb ./data/duckdb/analytics.duckdb \
  "BACKUP DATABASE TO 's3://my-bucket/backup'"
```

---

## Storage Backend Setup

### Local Storage

Default configuration (no setup needed):

```yaml
analytics:
  storage:
    type: local
    hot_ttl_hours: 24
    config:
      base_path: "./data/analytics"
```

### S3 Storage (Warm Tier)

```yaml
analytics:
  storage:
    type: s3
    warm_ttl_hours: 168     # 7 days
    config:
      bucket: my-analytics-bucket
      region: us-east-1
      prefix: analytics/
      enable_versioning: true
      enable_encryption: true
      encryption_key_id: arn:aws:kms:...
```

Setup S3:
```bash
# Create bucket
aws s3 mb s3://my-analytics-bucket --region us-east-1

# Enable versioning
aws s3api put-bucket-versioning \
  --bucket my-analytics-bucket \
  --versioning-configuration Status=Enabled

# Enable encryption
aws s3api put-bucket-encryption \
  --bucket my-analytics-bucket \
  --server-side-encryption-configuration '{
    "Rules": [{
      "ApplyServerSideEncryptionByDefault": {
        "SSEAlgorithm": "aws:kms",
        "KMSMasterKeyID": "arn:aws:kms:us-east-1:123456789:key/12345"
      }
    }]
  }'

# Set lifecycle policy
aws s3api put-bucket-lifecycle-configuration \
  --bucket my-analytics-bucket \
  --lifecycle-configuration '{
    "Rules": [{
      "Id": "Archive to Glacier",
      "Prefix": "analytics/",
      "Status": "Enabled",
      "Transitions": [{
        "Days": 90,
        "StorageClass": "GLACIER"
      }]
    }]
  }'
```

### Iceberg Storage (Archive)

```yaml
analytics:
  storage:
    type: iceberg
    cold_ttl_hours: 2160   # 90 days
    config:
      warehouse: s3://my-iceberg-warehouse/
      catalog_uri: http://glue-catalog:9082
      namespace: analytics
      enable_partitioning: true
      partition_spec:
        - field: createdAt
          type: month
```

Setup Iceberg:
```bash
# Create warehouse bucket
aws s3 mb s3://my-iceberg-warehouse

# Or use local warehouse
mkdir -p ./data/iceberg-warehouse

# Configure Aegion to use local warehouse
ANALYTICS_STORAGE_ICEBERG_WAREHOUSE=file:///data/iceberg-warehouse
```

### Kubernetes Storage (Cold Tier)

```yaml
analytics:
  storage:
    type: kubernetes
    config:
      namespace: analytics
      storage_class: archive
      retention_days: 2555   # 7 years
```

Setup K8s storage:
```bash
# Create storage class for archive
kubectl apply -f - <<EOF
apiVersion: storage.k8s.io/v1
kind: StorageClass
metadata:
  name: archive
provisioner: kubernetes.io/azure-disk
parameters:
  skuName: Standard_LRS
  kind: Managed
  cachingmode: None
EOF
```

---

## Data Sync Setup

### Real-Time Sync

```yaml
analytics:
  sync:
    enable_real_time: true
    real_time:
      batch_size: 100
      max_latency_ms: 1000
      enable_cdc: true      # Use PostgreSQL CDC
      enable_triggers: false
```

PostgreSQL CDC requires:
- PostgreSQL 10+ with logical decoding enabled
- `wal_level = logical` in postgresql.conf
- Replication slot created

Setup PostgreSQL CDC:
```bash
# Connect to PostgreSQL
psql -U postgres

# Enable logical decoding
SELECT pg_create_logical_replication_slot('aegion_analytics', 'test_decoding');

# Verify
SELECT * FROM pg_replication_slots;
```

### Batch Sync

```yaml
analytics:
  sync:
    enable_batch: true
    batch:
      schedule: "*/5 * * * *"     # Every 5 minutes
      batch_size: 10000
      parallel_workers: 4
      skip_if_recent: 2            # Minutes of real-time sync
```

### Async Sync

```yaml
analytics:
  sync:
    enable_async: true
    async:
      queue_depth: 100000
      worker_count: 8
      retry_policy: exponential
      max_retries: 3
      base_retry_delay_ms: 100
```

### Verify Sync Status

```bash
# Check sync health
curl -H "Authorization: Bearer token" \
  http://localhost:8080/api/v1/analytics/health

# Query sync metrics
curl -H "Authorization: Bearer token" \
  http://localhost:8080/api/v1/analytics/stats

# View sync logs
docker-compose logs -f aegion | grep sync
```

---

## Health Check Verification

### REST Endpoint

```bash
curl -i http://localhost:8080/api/v1/analytics/health
```

Response (healthy):
```json
{
  "status": "healthy",
  "timestamp": "2026-04-24T10:30:00Z",
  "components": {
    "postgres": "ready",
    "duckdb": "ready",
    "sync_real_time": "running",
    "sync_batch": "scheduled",
    "storage": "ready",
    "webhooks": "ready"
  },
  "latency_ms": 5
}
```

### Liveness/Readiness

```bash
# Liveness (pod should be alive)
curl http://localhost:8080/api/v1/analytics/live

# Readiness (pod ready for traffic)
curl http://localhost:8080/api/v1/analytics/ready
```

Both return `200 OK` if healthy.

### Kubernetes Health Probes

```yaml
spec:
  containers:
  - name: aegion
    livenessProbe:
      httpGet:
        path: /api/v1/analytics/live
        port: 8080
      initialDelaySeconds: 30
      periodSeconds: 10
    readinessProbe:
      httpGet:
        path: /api/v1/analytics/ready
        port: 8080
      initialDelaySeconds: 10
      periodSeconds: 5
```

---

## Troubleshooting Setup

### Port Already in Use

```bash
# Check what's using port 8080
lsof -i :8080

# Kill the process
kill -9 <PID>

# Or use different port
ANALYTICS_REST_PORT=8081 ./aegion server
```

### PostgreSQL Connection Error

```bash
# Test connection
psql postgresql://analytics:password@localhost:5432/aegion

# Check credentials in .env
# Verify PostgreSQL is running
docker ps | grep postgres
```

### DuckDB Lock Timeout

```bash
# Remove stale DuckDB lock
rm -f ./data/duckdb/analytics.duckdb.lock

# Restart
./aegion server
```

### Sync Lag

```bash
# Check sync status
curl http://localhost:8080/api/v1/analytics/stats

# If lag too high, increase workers
ANALYTICS_SYNC_BATCH_PARALLEL_WORKERS=8 ./aegion server
```

See [troubleshooting.md](./troubleshooting.md) for more issues.

---

## Related Documentation

- [Architecture](./architecture.md)
- [Performance Tuning](./performance.md)
- [Security](./security.md)
- [Integration Guide](./integration.md)
- [Troubleshooting](./troubleshooting.md)
