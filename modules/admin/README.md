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
| `AEGION_CONFIG_PATH` | Path to configuration file | `admin.yaml` |
| `DATABASE_URL` | PostgreSQL connection string | - |
| `AEGION_LOG_PRETTY` | Enable pretty logging | `false` |

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
- `GET /api/admin/auth/profile` - Get current user profile

#### User Management
- `GET /api/admin/users` - List users with pagination
- `GET /api/admin/users/{id}` - Get specific user
- `PUT /api/admin/users/{id}` - Update user
- `DELETE /api/admin/users/{id}` - Delete user
- `POST /api/admin/users/{id}/disable` - Disable user account
- `POST /api/admin/users/{id}/enable` - Enable user account

#### Session Management
- `GET /api/admin/sessions` - List active sessions
- `DELETE /api/admin/sessions/{id}` - Terminate session
- `POST /api/admin/sessions/cleanup` - Clean expired sessions

#### Operator Management
- `GET /api/admin/operators` - List administrative users
- `POST /api/admin/operators` - Create new operator
- `PUT /api/admin/operators/{id}` - Update operator
- `DELETE /api/admin/operators/{id}` - Delete operator
- `PUT /api/admin/operators/{id}/permissions` - Update permissions

#### Audit & Monitoring
- `GET /api/admin/audit` - View audit logs
- `GET /api/admin/stats` - System statistics
- `GET /api/admin/metrics` - Performance metrics

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
   cp admin.yaml.example admin.yaml
   # Edit admin.yaml with your database settings
   ```

5. **Run the admin module**
   ```bash
   go run ./cmd/admin -config admin.yaml
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
./admin -migrate -config admin.yaml

# Check migration status
./admin -migrate -status -config admin.yaml
```

## Security Considerations

- All admin endpoints require authentication and proper authorization
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
  -v /path/to/admin.yaml:/admin.yaml \
  aegion/admin:latest -config /admin.yaml
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