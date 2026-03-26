# Deployment Guide

This guide covers deploying Aegion in various environments from development to production.

## Deployment Options

### Quick Start with Docker Compose

The easiest way to deploy Aegion:

```bash
# Clone repository
git clone https://github.com/NeerajCodz/Aegion
cd Aegion

# Start services
docker-compose up -d

# Check status
docker-compose ps
docker-compose logs aegion
```

### Production Docker Compose

Use the production configuration:

```yaml
# docker-compose.prod.yml
version: '3.8'

services:
  aegion:
    image: aegion:latest
    restart: unless-stopped
    ports:
      - "8080:8080"
    environment:
      - AEGION_CONFIG=/etc/aegion/aegion.yaml
    volumes:
      - ./configs/production.yaml:/etc/aegion/aegion.yaml:ro
      - aegion_data:/var/lib/aegion
    depends_on:
      - postgres
    healthcheck:
      test: ["CMD", "curl", "-f", "http://localhost:8080/health"]
      interval: 30s
      timeout: 10s
      retries: 3

  postgres:
    image: postgres:14
    restart: unless-stopped
    environment:
      - POSTGRES_DB=aegion
      - POSTGRES_USER=aegion
      - POSTGRES_PASSWORD_FILE=/run/secrets/postgres_password
    volumes:
      - postgres_data:/var/lib/postgresql/data
    secrets:
      - postgres_password
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U aegion"]
      interval: 30s
      timeout: 10s
      retries: 3

  nginx:
    image: nginx:alpine
    restart: unless-stopped
    ports:
      - "80:80"
      - "443:443"
    volumes:
      - ./nginx.conf:/etc/nginx/nginx.conf:ro
      - ./ssl:/etc/ssl/aegion:ro
    depends_on:
      - aegion

volumes:
  aegion_data:
  postgres_data:

secrets:
  postgres_password:
    file: ./secrets/postgres_password.txt
```

```bash
# Deploy production
docker-compose -f docker-compose.prod.yml up -d
```

## Environment Variables Reference

### Core Configuration

| Variable | Description | Default | Required |
|----------|-------------|---------|----------|
| `AEGION_CONFIG` | Path to config file | `/etc/aegion/aegion.yaml` | No |
| `AEGION_ENV` | Environment mode | `production` | No |
| `AEGION_HOST` | Server bind address | `0.0.0.0` | No |
| `AEGION_PORT` | Server port | `8080` | No |

### Database

| Variable | Description | Default | Required |
|----------|-------------|---------|----------|
| `DATABASE_URL` | PostgreSQL connection string | - | Yes |
| `DATABASE_MAX_CONNECTIONS` | Max pool connections | `25` | No |
| `DATABASE_LOG_QUERIES` | Enable query logging | `false` | No |

### Security

| Variable | Description | Default | Required |
|----------|-------------|---------|----------|
| `SESSION_SECRET` | Session encryption key | - | Yes |
| `COOKIE_DOMAIN` | Cookie domain | - | No |
| `COOKIE_SECURE` | Use secure cookies | `true` | No |
| `CSRF_SECRET` | CSRF protection key | - | Yes |

### Email/SMS

| Variable | Description | Default | Required |
|----------|-------------|---------|----------|
| `SMTP_HOST` | SMTP server host | - | No |
| `SMTP_PORT` | SMTP server port | `587` | No |
| `SMTP_USERNAME` | SMTP username | - | No |
| `SMTP_PASSWORD` | SMTP password | - | No |
| `EMAIL_FROM` | Default from address | - | No |

### Logging

| Variable | Description | Default | Required |
|----------|-------------|---------|----------|
| `LOG_LEVEL` | Logging level | `info` | No |
| `LOG_FORMAT` | Log format (json/text) | `json` | No |
| `LOG_OUTPUT` | Log output (stdout/file) | `stdout` | No |

## Production Configuration

### Complete Production Config

```yaml
# configs/production.yaml
server:
  host: "0.0.0.0"
  port: 8080
  read_timeout: 30s
  write_timeout: 30s
  idle_timeout: 120s
  
database:
  dsn: "${DATABASE_URL}"
  max_connections: 25
  max_idle_connections: 10
  connection_max_lifetime: "1h"
  
security:
  session_secret: "${SESSION_SECRET}"
  csrf_secret: "${CSRF_SECRET}"
  cookie:
    domain: ".your-domain.com"
    secure: true
    same_site: "strict"
    max_age: "24h"
  rate_limiting:
    enabled: true
    login_attempts: 5
    registration_attempts: 3
    
email:
  delivery: "smtp"
  smtp:
    host: "${SMTP_HOST}"
    port: ${SMTP_PORT}
    username: "${SMTP_USERNAME}"
    password: "${SMTP_PASSWORD}"
    tls: true
  from: "noreply@your-domain.com"
  
logging:
  level: "info"
  format: "json"
  output: "stdout"
  
monitoring:
  metrics:
    enabled: true
    path: "/metrics"
  health:
    enabled: true
    path: "/health"
    
cors:
  allowed_origins:
    - "https://your-app.com"
    - "https://admin.your-domain.com"
  allowed_methods: ["GET", "POST", "PUT", "DELETE"]
  allowed_headers: ["Content-Type", "Authorization"]
  max_age: "12h"
```

## Secrets Management

### Using Environment Files

```bash
# .env.production
DATABASE_URL=postgres://aegion:secure_password@postgres:5432/aegion?sslmode=require
SESSION_SECRET=your-32-character-session-secret-key
CSRF_SECRET=your-32-character-csrf-secret-key
SMTP_PASSWORD=your-smtp-password
```

### Using Docker Secrets

```yaml
# In docker-compose.prod.yml
services:
  aegion:
    secrets:
      - session_secret
      - csrf_secret
      - database_password
      - smtp_password

secrets:
  session_secret:
    file: ./secrets/session_secret.txt
  csrf_secret:
    file: ./secrets/csrf_secret.txt
  database_password:
    file: ./secrets/database_password.txt
  smtp_password:
    file: ./secrets/smtp_password.txt
```

### Using External Secret Management

```yaml
# For Kubernetes with sealed-secrets
apiVersion: bitnami.com/v1alpha1
kind: SealedSecret
metadata:
  name: aegion-secrets
spec:
  encryptedData:
    database-url: AgBy3i4OJSWK...
    session-secret: AgAR4w2Y...
    smtp-password: AgBla1...
```

## SSL/TLS Configuration

### Nginx Reverse Proxy

```nginx
# nginx.conf
events {
    worker_connections 1024;
}

http {
    upstream aegion {
        server aegion:8080;
    }

    server {
        listen 80;
        server_name your-domain.com;
        return 301 https://$server_name$request_uri;
    }

    server {
        listen 443 ssl http2;
        server_name your-domain.com;

        ssl_certificate /etc/ssl/aegion/cert.pem;
        ssl_certificate_key /etc/ssl/aegion/key.pem;
        ssl_protocols TLSv1.2 TLSv1.3;
        ssl_ciphers HIGH:!aNULL:!MD5;

        location / {
            proxy_pass http://aegion;
            proxy_set_header Host $host;
            proxy_set_header X-Real-IP $remote_addr;
            proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
            proxy_set_header X-Forwarded-Proto $scheme;
        }

        location /health {
            access_log off;
            proxy_pass http://aegion;
        }
    }
}
```

### Let's Encrypt with Certbot

```bash
# Install certbot
apt-get install certbot python3-certbot-nginx

# Obtain certificate
certbot --nginx -d your-domain.com

# Auto-renewal
crontab -e
# Add: 0 12 * * * /usr/bin/certbot renew --quiet
```

## Health Checks

### Application Health Check

Aegion provides built-in health checks:

```bash
# Basic health check
curl http://localhost:8080/health

# Response
{
  "status": "ok",
  "timestamp": "2024-01-15T12:00:00Z",
  "checks": {
    "database": "ok",
    "email": "ok",
    "security_engine": "ok"
  }
}
```

### Docker Health Checks

```yaml
# In docker-compose.yml
healthcheck:
  test: ["CMD", "curl", "-f", "http://localhost:8080/health"]
  interval: 30s
  timeout: 10s
  retries: 3
  start_period: 40s
```

### Kubernetes Health Checks

```yaml
# In deployment.yaml
livenessProbe:
  httpGet:
    path: /health
    port: 8080
  initialDelaySeconds: 30
  periodSeconds: 30

readinessProbe:
  httpGet:
    path: /health
    port: 8080
  initialDelaySeconds: 5
  periodSeconds: 10
```

## Monitoring and Observability

### Metrics

Enable Prometheus metrics:

```yaml
# In aegion.yaml
monitoring:
  metrics:
    enabled: true
    path: "/metrics"
```

### Logging

Configure structured logging for production:

```yaml
logging:
  level: "info"
  format: "json"
  output: "stdout"
  fields:
    service: "aegion"
    version: "1.0.0"
```

### Tracing

Enable distributed tracing:

```yaml
tracing:
  enabled: true
  provider: "jaeger"
  endpoint: "http://jaeger:14268/api/traces"
  sample_rate: 0.1
```

## Scaling

### Horizontal Scaling

Run multiple Aegion instances:

```yaml
# docker-compose.scale.yml
services:
  aegion:
    deploy:
      replicas: 3
    environment:
      - AEGION_INSTANCE_ID=${HOSTNAME}
```

### Database Scaling

#### Read Replicas

```yaml
database:
  primary:
    dsn: "postgres://user:pass@primary:5432/aegion"
  replicas:
    - dsn: "postgres://user:pass@replica1:5432/aegion"
    - dsn: "postgres://user:pass@replica2:5432/aegion"
```

#### Connection Pooling

Use PgBouncer for connection pooling:

```yaml
# docker-compose.yml
pgbouncer:
  image: pgbouncer/pgbouncer:latest
  environment:
    DATABASES_HOST: postgres
    DATABASES_PORT: 5432
    DATABASES_USER: aegion
    DATABASES_PASSWORD: "${DB_PASSWORD}"
    POOL_MODE: transaction
    MAX_CLIENT_CONN: 1000
    DEFAULT_POOL_SIZE: 25
```

### Session Storage

For multi-instance deployments, use external session storage:

```yaml
# Redis for sessions
redis:
  image: redis:7-alpine
  restart: unless-stopped

# In aegion.yaml
session:
  store: "redis"
  redis:
    url: "redis://redis:6379/0"
```

## Backup and Recovery

### Database Backups

```bash
# Daily backup script
#!/bin/bash
BACKUP_DIR="/var/backups/aegion"
DATE=$(date +%Y%m%d_%H%M%S)

# Create backup
docker-compose exec -T postgres pg_dump -U aegion aegion > \
  "${BACKUP_DIR}/aegion_${DATE}.sql"

# Compress backup
gzip "${BACKUP_DIR}/aegion_${DATE}.sql"

# Keep only last 30 days
find "${BACKUP_DIR}" -name "aegion_*.sql.gz" -mtime +30 -delete
```

### Automated Backups

```yaml
# In docker-compose.yml
backup:
  image: prodrigestivill/postgres-backup-local
  restart: unless-stopped
  environment:
    - POSTGRES_HOST=postgres
    - POSTGRES_DB=aegion
    - POSTGRES_USER=aegion
    - POSTGRES_PASSWORD=${DB_PASSWORD}
    - BACKUP_KEEP_DAYS=7
    - BACKUP_KEEP_WEEKS=4
    - BACKUP_KEEP_MONTHS=6
  volumes:
    - ./backups:/backups
```

### Disaster Recovery

1. **Regular backups** of database and configuration
2. **Store backups** in multiple locations (cloud storage)
3. **Test restoration** procedures regularly
4. **Document recovery steps** for your team

```bash
# Recovery procedure
# 1. Restore database
psql -U aegion -h localhost aegion < backup.sql

# 2. Restart services
docker-compose up -d

# 3. Verify functionality
curl http://localhost:8080/health
```

## Kubernetes Deployment

### Basic Deployment

```yaml
# k8s/deployment.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: aegion
spec:
  replicas: 3
  selector:
    matchLabels:
      app: aegion
  template:
    metadata:
      labels:
        app: aegion
    spec:
      containers:
      - name: aegion
        image: aegion:latest
        ports:
        - containerPort: 8080
        env:
        - name: DATABASE_URL
          valueFrom:
            secretKeyRef:
              name: aegion-secrets
              key: database-url
        livenessProbe:
          httpGet:
            path: /health
            port: 8080
          initialDelaySeconds: 30
          periodSeconds: 30
---
apiVersion: v1
kind: Service
metadata:
  name: aegion-service
spec:
  selector:
    app: aegion
  ports:
  - port: 80
    targetPort: 8080
  type: LoadBalancer
```

### Ingress Configuration

```yaml
# k8s/ingress.yaml
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: aegion-ingress
  annotations:
    kubernetes.io/ingress.class: nginx
    cert-manager.io/cluster-issuer: letsencrypt-prod
spec:
  tls:
  - hosts:
    - your-domain.com
    secretName: aegion-tls
  rules:
  - host: your-domain.com
    http:
      paths:
      - path: /
        pathType: Prefix
        backend:
          service:
            name: aegion-service
            port:
              number: 80
```

## Security Considerations

### Network Security

- Use TLS/SSL for all connections
- Implement proper firewall rules
- Use private networks for internal communication
- Regular security updates

### Application Security

```yaml
security:
  # Strong session security
  session_secret: "32-character-random-key"
  cookie:
    secure: true
    same_site: "strict"
    http_only: true
  
  # CSRF protection
  csrf_secret: "32-character-csrf-key"
  
  # Rate limiting
  rate_limiting:
    enabled: true
    login_attempts: 5
    window: "1h"
```

### Database Security

- Use strong passwords
- Enable SSL connections
- Regular security patches
- Proper access controls
- Encryption at rest

## Troubleshooting

### Common Deployment Issues

**Container Won't Start**:
```bash
# Check logs
docker-compose logs aegion

# Check configuration
docker-compose exec aegion aegion validate --config /etc/aegion/aegion.yaml
```

**Database Connection Issues**:
```bash
# Test database connectivity
docker-compose exec aegion pg_isready -h postgres -p 5432

# Check migrations
docker-compose exec aegion aegion migrate status
```

**SSL/TLS Issues**:
```bash
# Check certificate
openssl x509 -in cert.pem -text -noout

# Test SSL connection
openssl s_client -connect your-domain.com:443
```

**Performance Issues**:
```bash
# Check resource usage
docker stats

# Enable debug logging temporarily
docker-compose exec aegion aegion serve --log-level debug
```

For more help, check the [Development Guide](development.md) or create an issue on [GitHub](https://github.com/NeerajCodz/Aegion/issues).