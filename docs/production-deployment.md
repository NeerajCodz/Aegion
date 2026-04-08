# Aegion Production Deployment Guide

## Overview

This guide covers deploying Aegion to production environments with enterprise-grade security, scalability, and reliability.

## Pre-Deployment Checklist

### Infrastructure Requirements

- [ ] **Database**: PostgreSQL 15+ with SSL/TLS enabled
- [ ] **Cache**: Redis 7+ with persistence enabled
- [ ] **Container Runtime**: Docker 24+ or Kubernetes 1.28+
- [ ] **Load Balancer**: HTTPS/TLS 1.3 capable
- [ ] **Monitoring**: Prometheus + Grafana or equivalent
- [ ] **Logging**: ELK Stack, Loki, or CloudWatch
- [ ] **DNS**: Configured domain with valid SSL certificates

### Security Requirements

- [ ] TLS certificates from trusted CA (Let's Encrypt, DigiCert, etc.)
- [ ] Strong secrets generated (32+ character random strings)
- [ ] Database credentials rotated from defaults
- [ ] Network isolation configured (VPC/subnets)
- [ ] Firewall rules configured (minimal port exposure)
- [ ] Secret management solution (Vault, AWS Secrets Manager, etc.)

### Compliance Requirements

- [ ] Data residency requirements identified
- [ ] GDPR/CCPA compliance procedures documented
- [ ] Audit logging configured
- [ ] Backup and retention policies defined
- [ ] Incident response plan documented

## Deployment Options

### Option 1: Docker Compose (Small Deployments)

**Best for**: Single server, small teams (<1000 users)

#### Step 1: Prepare Environment

```bash
# Create deployment directory
mkdir -p /opt/aegion
cd /opt/aegion

# Clone repository (or copy release)
git clone https://github.com/NeerajCodz/Aegion.git
cd Aegion
git checkout <version-tag>  # e.g., v1.0.0
```

#### Step 2: Configure Environment

```bash
# Copy and customize environment
cp deploy/.env.example deploy/.env

# Generate secure secrets
export AEGION_SECRETS_COOKIE=$(openssl rand -base64 32 | head -c 32)
export AEGION_SECRETS_CIPHER=$(openssl rand -base64 32 | head -c 32)
export AEGION_SECRETS_INTERNAL=$(openssl rand -base64 32 | head -c 32)

# Update .env file
sed -i "s/AEGION_SECRETS_COOKIE=.*/AEGION_SECRETS_COOKIE=$AEGION_SECRETS_COOKIE/" deploy/.env
sed -i "s/AEGION_SECRETS_CIPHER=.*/AEGION_SECRETS_CIPHER=$AEGION_SECRETS_CIPHER/" deploy/.env
sed -i "s/AEGION_SECRETS_INTERNAL=.*/AEGION_SECRETS_INTERNAL=$AEGION_SECRETS_INTERNAL/" deploy/.env

# Configure database (use external managed DB recommended)
# Edit deploy/.env:
# DATABASE_URL=postgres://user:pass@db.example.com:5432/aegion?sslmode=require
# CACHE_URL=redis://redis.example.com:6379/0
```

#### Step 3: Configure Module Set

Edit `configs/aegion.yaml`:

```yaml
# Production module configuration
module_versions:
  # Mature modules (production-ready)
  password: "1.0.0"        # Enable password auth
  magic_link: "1.0.0"      # Enable passwordless
  admin: "1.0.0"           # Enable admin panel
  
  # Optional mature modules
  oauth2: "1.0.0"          # Uncomment if needed
  policy: "1.0.0"          # Uncomment if needed
  
  # Experimental modules (DO NOT enable in production)
  # mfa: "latest"          # Not production-ready
  # passkeys: "latest"     # Not production-ready
  # social: "latest"       # Not production-ready
  # sso: "latest"          # Not production-ready
  # introspection: "latest" # Not production-ready
  # cli: "latest"          # Not production-ready
  # proxy: "latest"        # Not production-ready

# Environment
environment: production    # CRITICAL: Enables production validation

# Security
session:
  cookie:
    secure: true           # REQUIRED for production
    domain: ".example.com" # Your domain
    same_site: "strict"
    http_only: true
  timeout: 3600            # 1 hour

# Database
database:
  url: ${AEGION_DATABASE_URL}  # Use environment variable
  max_connections: 50
  ssl_mode: require        # REQUIRED for production

# Logging
log:
  level: info              # info or warn (not debug)
  format: json             # REQUIRED for production

# Rate limiting
rate_limit:
  enabled: true
  requests_per_minute: 100
  burst: 20

# Observability
observability:
  enabled: true
  traces:
    enabled: true
    exporter: otlp
    endpoint: http://otel-collector:4317
  metrics:
    enabled: true
    exporter: prometheus
    endpoint: :9090
```

#### Step 4: Deploy

```bash
# Build images
docker compose -f deploy/docker-compose.yml -f deploy/docker-compose.prod.yml build

# Start services
docker compose -f deploy/docker-compose.yml -f deploy/docker-compose.prod.yml up -d

# Check health
curl -k https://localhost:8080/health

# View logs
docker compose logs -f
```

#### Step 5: Configure Reverse Proxy (Nginx)

```nginx
# /etc/nginx/sites-available/aegion
upstream aegion {
    server 127.0.0.1:8080;
}

server {
    listen 443 ssl http2;
    server_name auth.example.com;

    ssl_certificate /etc/letsencrypt/live/auth.example.com/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/auth.example.com/privkey.pem;
    ssl_protocols TLSv1.3 TLSv1.2;
    ssl_ciphers HIGH:!aNULL:!MD5;
    ssl_prefer_server_ciphers on;
    
    # Security headers
    add_header Strict-Transport-Security "max-age=31536000; includeSubDomains" always;
    add_header X-Frame-Options DENY always;
    add_header X-Content-Type-Options nosniff always;
    add_header X-XSS-Protection "1; mode=block" always;
    add_header Referrer-Policy "strict-origin-when-cross-origin" always;
    add_header Content-Security-Policy "default-src 'self'; script-src 'self' 'unsafe-inline'; style-src 'self' 'unsafe-inline';" always;

    # Proxy settings
    location / {
        proxy_pass http://aegion;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_buffering off;
        proxy_request_buffering off;
        
        # WebSocket support
        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection "upgrade";
    }
    
    # Health check endpoint
    location /health {
        proxy_pass http://aegion/health;
        access_log off;
    }
}

# Redirect HTTP to HTTPS
server {
    listen 80;
    server_name auth.example.com;
    return 301 https://$server_name$request_uri;
}
```

### Option 2: Kubernetes (Enterprise Deployments)

**Best for**: High availability, auto-scaling, multi-region (>1000 users)

#### Prerequisites

```bash
# Install kubectl
curl -LO "https://dl.k8s.io/release/$(curl -L -s https://dl.k8s.io/release/stable.txt)/bin/linux/amd64/kubectl"
chmod +x kubectl
sudo mv kubectl /usr/local/bin/

# Install Helm
curl https://raw.githubusercontent.com/helm/helm/main/scripts/get-helm-3 | bash

# Configure kubectl for your cluster
# AWS EKS:
aws eks update-kubeconfig --region us-west-2 --name aegion-prod

# GCP GKE:
gcloud container clusters get-credentials aegion-prod --region us-west1

# Azure AKS:
az aks get-credentials --resource-group aegion-rg --name aegion-prod
```

#### Deploy with Helm

```bash
# Add Aegion Helm repository (when available)
# helm repo add aegion https://charts.aegion.io
# helm repo update

# Or use local charts
cd deploy/helm

# Create namespace
kubectl create namespace aegion

# Create secrets
kubectl create secret generic aegion-secrets \
  --from-literal=cookie-secret=$(openssl rand -base64 32 | head -c 32) \
  --from-literal=cipher-secret=$(openssl rand -base64 32 | head -c 32) \
  --from-literal=internal-secret=$(openssl rand -base64 32 | head -c 32) \
  -n aegion

# Configure values
cat > values-prod.yaml <<EOF
replicaCount: 3

image:
  repository: ghcr.io/aegion/core
  tag: "1.0.0"
  pullPolicy: IfNotPresent

ingress:
  enabled: true
  className: nginx
  annotations:
    cert-manager.io/cluster-issuer: letsencrypt-prod
  hosts:
    - host: auth.example.com
      paths:
        - path: /
          pathType: Prefix
  tls:
    - secretName: aegion-tls
      hosts:
        - auth.example.com

resources:
  limits:
    cpu: 1000m
    memory: 1Gi
  requests:
    cpu: 500m
    memory: 512Mi

autoscaling:
  enabled: true
  minReplicas: 3
  maxReplicas: 10
  targetCPUUtilizationPercentage: 70
  targetMemoryUtilizationPercentage: 80

postgresql:
  enabled: false  # Use managed database
  external:
    host: "aegion-db.cluster-xxx.us-west-2.rds.amazonaws.com"
    port: 5432
    database: aegion
    username: aegion
    passwordSecret: aegion-db-password

redis:
  enabled: false  # Use managed cache
  external:
    host: "aegion-cache.xxx.0001.usw2.cache.amazonaws.com"
    port: 6379

monitoring:
  enabled: true
  serviceMonitor:
    enabled: true
  grafana:
    enabled: true
  prometheus:
    enabled: true

modules:
  password:
    enabled: true
    replicaCount: 2
  magicLink:
    enabled: true
    replicaCount: 2
  admin:
    enabled: true
    replicaCount: 1
  oauth2:
    enabled: true
    replicaCount: 2
  policy:
    enabled: true
    replicaCount: 2
EOF

# Install
helm install aegion ./aegion \
  -f values-prod.yaml \
  -n aegion

# Check status
kubectl get pods -n aegion
kubectl get svc -n aegion
kubectl get ingress -n aegion

# View logs
kubectl logs -f deployment/aegion-core -n aegion
```

## Post-Deployment Validation

### Health Checks

```bash
# Core health
curl https://auth.example.com/health

# Module health
curl https://auth.example.com/.well-known/aegion/meta

# OpenID configuration
curl https://auth.example.com/.well-known/openid-configuration

# JWKS endpoint
curl https://auth.example.com/.well-known/jwks.json
```

### Smoke Tests

```bash
# Test registration flow (if enabled)
curl -X POST https://auth.example.com/self-service/registration/browser \
  -H "Content-Type: application/json" \
  -d '{"traits":{"email":"test@example.com"}}'

# Test login flow
curl -X POST https://auth.example.com/self-service/login/browser \
  -H "Content-Type: application/json" \
  -d '{"identifier":"test@example.com","password":"test123"}'

# Test admin API (with auth)
curl -X GET https://auth.example.com/admin/identities \
  -H "Authorization: Bearer <admin-token>"
```

### Security Validation

```bash
# Verify TLS configuration
nmap --script ssl-enum-ciphers -p 443 auth.example.com

# Check security headers
curl -I https://auth.example.com | grep -i "strict-transport\|x-frame\|x-content"

# Verify no root processes
docker exec aegion-core ps aux | grep "^root"  # Should only see PID 1

# Check for exposed secrets
docker exec aegion-core env | grep -i "secret\|password\|token"  # Should be empty or redacted
```

## Monitoring and Alerting

### Metrics to Monitor

- **Availability**: Uptime, health check success rate
- **Performance**: Response times (p50, p95, p99), throughput (req/s)
- **Errors**: 4xx/5xx rates, failed login attempts, token errors
- **Resources**: CPU usage, memory usage, connection pool saturation
- **Business**: Registrations/hour, logins/hour, active sessions

### Recommended Alerts

1. **Critical (P0)**:
   - Service down (health check fails for >2min)
   - Database unreachable
   - Error rate >5% for >5min
   - CPU usage >90% for >5min
   - Memory usage >90% for >5min

2. **High (P1)**:
   - Response time p95 >1s for >5min
   - Login failure rate >10% for >5min
   - Database connection pool >80% for >5min

3. **Medium (P2)**:
   - Response time p99 >2s for >10min
   - Cache miss rate >50% for >10min
   - Disk usage >80%

### Grafana Dashboard

Import the provided dashboard: `deploy/grafana/aegion-dashboard.json`

Key panels:
- Request rate and latency
- Error rates by endpoint
- Active sessions
- Database query performance
- Module health status

## Backup and Disaster Recovery

### Database Backups

```bash
# Automated daily backups (cron)
0 2 * * * pg_dump -h db.example.com -U aegion aegion | gzip > /backups/aegion-$(date +\%Y\%m\%d).sql.gz

# Retention: Keep 30 days
0 3 * * * find /backups -name "aegion-*.sql.gz" -mtime +30 -delete
```

### Configuration Backups

```bash
# Backup configs and secrets
kubectl get secret aegion-secrets -n aegion -o yaml > backups/secrets-$(date +%Y%m%d).yaml
kubectl get configmap aegion-config -n aegion -o yaml > backups/config-$(date +%Y%m%d).yaml

# Encrypt backups
gpg --encrypt --recipient ops@example.com backups/*.yaml
```

### Disaster Recovery Procedure

1. **Restore Database**:
   ```bash
   gunzip < backups/aegion-20250101.sql.gz | psql -h db.example.com -U aegion aegion
   ```

2. **Restore Secrets**:
   ```bash
   kubectl apply -f backups/secrets-20250101.yaml -n aegion
   ```

3. **Redeploy Services**:
   ```bash
   helm upgrade aegion ./aegion -f values-prod.yaml -n aegion
   ```

4. **Validate**:
   ```bash
   kubectl get pods -n aegion
   curl https://auth.example.com/health
   ```

## Scaling Recommendations

### Vertical Scaling (Per Module)

| Users | Core | Password | OAuth2 | Admin | DB | Redis |
|-------|------|----------|--------|-------|----|----|
| <1K | 1 CPU, 512MB | 0.5 CPU, 256MB | 0.5 CPU, 256MB | 0.5 CPU, 256MB | 2 CPU, 4GB | 1 CPU, 1GB |
| <10K | 2 CPU, 1GB | 1 CPU, 512MB | 1 CPU, 512MB | 1 CPU, 512MB | 4 CPU, 8GB | 2 CPU, 2GB |
| <100K | 4 CPU, 2GB | 2 CPU, 1GB | 2 CPU, 1GB | 1 CPU, 512MB | 8 CPU, 16GB | 4 CPU, 4GB |
| 100K+ | 8 CPU, 4GB | 4 CPU, 2GB | 4 CPU, 2GB | 2 CPU, 1GB | 16 CPU, 32GB | 8 CPU, 8GB |

### Horizontal Scaling

- **Core**: 2-10 replicas (load balanced)
- **Password**: 2-5 replicas
- **OAuth2**: 2-10 replicas (stateless)
- **Admin**: 1-3 replicas (low traffic)
- **Database**: Read replicas for reporting/analytics
- **Redis**: Sentinel (3 nodes) or Cluster (6+ nodes)

## Troubleshooting

### Common Issues

#### 1. Startup Fails with "Production validation failed"

**Cause**: Insecure configuration detected  
**Solution**: Check logs for specific validation error and fix config

```bash
# Check logs
kubectl logs deployment/aegion-core -n aegion | grep "validation"

# Common fixes:
# - Set session.cookie.secure=true
# - Use json log format
# - Enable database SSL
# - Use production-grade secrets (not placeholders)
```

#### 2. Modules Not Starting

**Cause**: Dependency validation failure or image pull issues  
**Solution**: Check module dependencies and registry access

```bash
# Check orchestrator logs
kubectl logs deployment/aegion-core -n aegion | grep "module"

# Verify images exist
docker pull ghcr.io/aegion/module-password:1.0.0

# Check module dependencies in aegion.yaml
```

#### 3. High Memory Usage

**Cause**: Connection pool leaks or cache bloat  
**Solution**: Adjust pool sizes and cache eviction

```yaml
database:
  max_connections: 50  # Reduce if too high
  max_idle: 10

cache:
  maxmemory: 256mb
  maxmemory_policy: allkeys-lru
```

#### 4. Slow Login Performance

**Cause**: Database N+1 queries or missing indexes  
**Solution**: Enable query logging and add indexes

```sql
-- Check slow queries
SELECT query, mean_exec_time, calls 
FROM pg_stat_statements 
ORDER BY mean_exec_time DESC 
LIMIT 10;

-- Add missing indexes
CREATE INDEX CONCURRENTLY idx_identities_email ON identities(email);
CREATE INDEX CONCURRENTLY idx_sessions_expires_at ON sessions(expires_at);
```

## Security Hardening (Production)

### 1. Enable WAF

```yaml
# AWS WAF example
Resources:
  WebACL:
    Type: AWS::WAFv2::WebACL
    Properties:
      Rules:
        - Name: RateLimitRule
          Priority: 1
          Statement:
            RateBasedStatement:
              Limit: 2000
              AggregateKeyType: IP
        - Name: SQLInjectionRule
          Priority: 2
          Statement:
            SqliMatchStatement:
              FieldToMatch:
                AllQueryArguments: {}
```

### 2. Enable DDoS Protection

- AWS Shield Advanced
- Cloudflare DDoS protection
- Rate limiting at load balancer

### 3. Regular Security Scans

```bash
# Weekly vulnerability scans
0 0 * * 0 trivy image --severity HIGH,CRITICAL ghcr.io/aegion/core:latest

# Monthly penetration tests
# Use OWASP ZAP, Burp Suite, or professional pentesters
```

### 4. Compliance Audits

- [ ] SOC 2 Type II (annual)
- [ ] GDPR compliance review (annual)
- [ ] Penetration testing (annual)
- [ ] Security training (quarterly)

## Support and Escalation

### Support Levels

- **L1 (Community)**: GitHub Issues, Discord
- **L2 (Professional)**: Email support, SLA
- **L3 (Enterprise)**: Dedicated support engineer, 24/7 on-call

### Escalation Contacts

- **Security Issues**: security@aegion.io (encrypted)
- **Critical Incidents**: incidents@aegion.io
- **Sales/Licensing**: sales@aegion.io

---

**Document Version**: 1.0  
**Last Updated**: 2025-01-01  
**Maintained By**: Aegion DevOps Team
