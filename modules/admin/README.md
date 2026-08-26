# Aegion Admin Module

The Admin Module provides a comprehensive web-based administration interface for the Aegion identity platform. It includes both a REST API for programmatic access and a modern SPA (Single Page Application) for interactive administration.

## Features

- **User Management**: Create, update, delete, and manage user identities
- **Session Management**: View active sessions and force logouts
- **Operator Management**: Manage administrative users and permissions
- **Audit Logging**: Track all administrative actions
- **Role-Based Access Control**: Fine-grained permissions system
- **Real-time Monitoring**: System health and usage statistics
- **Configuration Management**: Platform-wide settings and policies

## Configuration

The admin module is configured through YAML files and environment variables.

### Environment Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `AEGION_CONFIG_PATH` | Path to configuration file | `aegion.yaml` |
| `DATABASE_URL` | PostgreSQL connection string | - |
| `AEGION_LOG_PRETTY` | Enable pretty logging | `false` |
| `AEGION_ADMIN_TRUST_FORWARDED_HEADERS` | Trust `X-Forwarded-For` and `X-Real-IP` for client IP derivation | `false` |
| `AEGION_ADMIN_OBSERVABILITY_ENABLED` | Enable dashboard observability backend probes | `false` |
| `AEGION_ADMIN_OBSERVABILITY_PROBE_TIMEOUT` | Probe timeout duration (Go duration format) | `5s` |
| `AEGION_LOZA_COLLECTOR_URL` | Loza collector health URL | `http://loza-collector:9308/health` |
| `AEGION_ADMIN_OBS_PROMETHEUS_URL` | Prometheus health URL | `http://prometheus:9090/-/healthy` |
| `AEGION_ADMIN_OBS_GRAFANA_URL` | Grafana health URL | `http://grafana:3000/api/health` |
| `AEGION_ADMIN_OBS_TEMPO_URL` | Tempo readiness URL | `http://tempo:3200/ready` |
| `AEGION_LOZA_COLLECTOR_URL` | Loza collector health URL | `http://loza-collector:9308/health` |
| `OTEL_SERVICE_NAME` | Telemetry service name surfaced in admin observability | `aegion` |
| `AEGION_SERVICE_VERSION` | Telemetry service version surfaced in admin observability | `v1.0.0` |
| `AEGION_ENVIRONMENT` | Telemetry environment label | `development` |
| `AEGION_INSTANCE_ID` | Telemetry instance identifier | Hostname |
| `OTEL_EXPORTER_OTLP_TRACES_ENDPOINT` | OTLP traces export endpoint | `http://localhost:4318` |
| `OTEL_EXPORTER_OTLP_METRICS_ENDPOINT` | OTLP metrics export endpoint | `http://localhost:4318` |
| `OTEL_TRACES_SAMPLER_ARG` | Trace sampling ratio | `1.0` |
| `OTEL_METRIC_EXPORT_INTERVAL` | Metrics export interval | `30s` |
| `OTEL_BSP_EXPORT_TIMEOUT` | Trace export timeout | `10s` |
| `OTEL_EXPORTER_OTLP_INSECURE` | Allow insecure OTLP transport | `false` |
| `AEGION_OBS_ENABLE_TRACES` | Enable traces signal | `true` |
| `AEGION_OBS_ENABLE_METRICS` | Enable metrics signal | `true` |

### Configuration File Structure

```yaml
database:
  url: "${DATABASE_URL}"
  max_conns: 25
  min_conns: 5
  max_idle_time: "5m"

server:
  address: "0.0.0.0"
  port: 8082
  read_timeout: 15s
  write_timeout: 15s
  idle_timeout: 60s

admin:
  enabled: true
  path: "/admin"
  session_lifespan: 4h

core:
  service_url: "${AEGION_CORE_URL:-http://localhost:8080}"
  api_key: "${AEGION_CORE_API_KEY}"

observability:
  enabled: false
  probe_timeout: 5s
  endpoints:
    loza_collector: "${AEGION_LOZA_COLLECTOR_URL:-http://loza-collector:9308/health}"
    prometheus: "${AEGION_ADMIN_OBS_PROMETHEUS_URL:-http://prometheus:9090/-/healthy}"
    grafana: "${AEGION_ADMIN_OBS_GRAFANA_URL:-http://grafana:3000/api/health}"
    tempo: "${AEGION_ADMIN_OBS_TEMPO_URL:-http://tempo:3200/ready}"
    loza_collector: "${AEGION_LOZA_COLLECTOR_URL:-http://loza-collector:9308/health}"
  telemetry:
    service_name: "${OTEL_SERVICE_NAME:-aegion}"
    service_version: "${AEGION_SERVICE_VERSION:-v1.0.0}"
    environment: "${AEGION_ENVIRONMENT:-development}"
    instance_id: "${AEGION_INSTANCE_ID:-}"
    traces_endpoint: "${OTEL_EXPORTER_OTLP_TRACES_ENDPOINT:-http://localhost:4318}"
    metrics_endpoint: "${OTEL_EXPORTER_OTLP_METRICS_ENDPOINT:-http://localhost:4318}"
    trace_sampling_ratio: 1.0
    metric_export_interval: 30s
    trace_export_timeout: 10s
    insecure: false
    enable_traces: true
    enable_metrics: true

log:
  level: "info"  # debug, info, warn, error
  format: "json" # json, pretty
```

## API Endpoints

### Health Endpoints
- `GET /health` - Basic health check
- `GET /health/ready` - Readiness check (includes database connectivity)

### Admin API
All admin API endpoints are mounted at `/api/admin/*` and require authentication.

#### Authentication
- `POST /api/admin/auth/login` - Authenticate admin user
- `POST /api/admin/auth/logout` - Logout admin user
- `GET /api/admin/auth/me` - Get current user profile and effective permissions

#### User Management
- `GET /api/admin/identities` - List identities with pagination
- `GET /api/admin/identities/{id}` - Get specific identity
- `PATCH /api/admin/identities/{id}` - Update identity
- `DELETE /api/admin/identities/{id}` - Delete identity
- `GET /api/admin/identities/{id}/sessions` - List sessions for an identity
- `DELETE /api/admin/identities/{id}/sessions` - Revoke all sessions for an identity

#### Session Management
- `GET /api/admin/sessions` - List active sessions
- `DELETE /api/admin/sessions/{id}` - Terminate session

#### Operator Management
- `GET /api/admin/operators` - List administrative users
- `POST /api/admin/operators` - Create new operator
- `PATCH /api/admin/operators/{id}` - Update operator
- `DELETE /api/admin/operators/{id}` - Delete operator

#### Role Management
- `GET /api/admin/roles` - List roles
- `GET /api/admin/roles/{name}` - Get role by name
- `GET /api/admin/roles/permissions` - List assignable RBAC permissions
- `POST /api/admin/roles` - Create custom role
- `PATCH /api/admin/roles/{name}` - Update custom role
- `DELETE /api/admin/roles/{name}` - Delete custom role (if unassigned)

#### Audit & Monitoring
- `GET /api/admin/audit` - View audit logs
- `GET /api/admin/dashboard/stats` - System statistics
- `GET /api/admin/dashboard/observability` - Authenticated observability summary with stack probes, telemetry config, SCIM posture, and guardrail warnings

### Web Interface
The SPA is served at `/admin/*` and provides a complete administrative interface.

## Development Setup

### Prerequisites
- Go 1.22 or later
- Node.js 20 or later
- PostgreSQL 13 or later

### Local Development

1. **Clone the repository**
   ```bash
   git clone https://github.com/aegion/aegion.git
   cd aegion/modules/admin
   ```

2. **Install SPA dependencies**
   ```bash
   cd spa
   npm install
   ```

3. **Build SPA for development**
   ```bash
   npm run dev
   ```

4. **Set up configuration**
   ```bash
    cp configs/aegion.example.yaml configs/aegion.yaml
    # Edit configs/aegion.yaml with your database settings
   ```

5. **Run the admin module**
   ```bash
    go run ./cmd/admin -config configs/aegion.yaml
   ```

The admin interface will be available at `http://localhost:8082/admin`.

### Building for Production

#### Build SPA
```bash
cd spa
npm run build
```

#### Build Go binary
```bash
go build -o admin ./cmd/admin
```

#### Build Docker image
```bash
docker build -f ../../build/Dockerfile.admin -t aegion/admin:latest .
```

### Database Migrations

The admin module includes its own migration system for admin-specific tables.

```bash
# Run migrations
./admin -migrate -config configs/aegion.yaml

# Check migration status
./admin -migrate -status -config configs/aegion.yaml
```

## Security Considerations

- All admin endpoints require authentication and proper authorization
- Admin authentication requires an API key or stronger upstream trust boundary; raw identity headers are not accepted
- Forwarded client-IP headers are disabled by default and should only be enabled behind a trusted reverse proxy
- RBAC (Role-Based Access Control) with fine-grained permissions
- Audit logging for all administrative actions
- Session management with configurable timeouts
- CSRF protection for web interface
- Content Security Policy (CSP) headers
- Secure password handling with bcrypt

## Deployment

### Docker Deployment

```bash
docker run -d \
  --name aegion-admin \
  -p 8082:8082 \
  -e DATABASE_URL="postgres://user:pass@host:5432/aegion" \
  -e AEGION_CORE_URL="http://aegion-core:8080" \
  -v /path/to/aegion.yaml:/config/aegion.yaml \
  aegion/admin:latest -config /config/aegion.yaml
```

### Kubernetes Deployment

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: aegion-admin
spec:
  replicas: 2
  selector:
    matchLabels:
      app: aegion-admin
  template:
    metadata:
      labels:
        app: aegion-admin
    spec:
      containers:
      - name: admin
        image: aegion/admin:latest
        ports:
        - containerPort: 8082
        env:
        - name: DATABASE_URL
          valueFrom:
            secretKeyRef:
              name: aegion-db-secret
              key: url
        - name: AEGION_CORE_URL
          value: "http://aegion-core:8080"
        livenessProbe:
          httpGet:
            path: /health
            port: 8082
          initialDelaySeconds: 30
          periodSeconds: 10
        readinessProbe:
          httpGet:
            path: /health/ready
            port: 8082
          initialDelaySeconds: 5
          periodSeconds: 5
```

## License

This project is licensed under the MIT License - see the [LICENSE](../../LICENSE) file for details.
