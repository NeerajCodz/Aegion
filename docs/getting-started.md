# Getting Started

Welcome to Aegion! This guide will help you set up and run your identity platform.

## Prerequisites

Before starting, ensure you have the following installed:

- **Go 1.22+** - Required for building the core API
- **Rust 1.75+** - Required for the security engine
- **Docker & Docker Compose** - For containerized deployment
- **Node.js 20+** - For admin panel development
- **PostgreSQL 14+** - Database (can use Docker)

## Quick Setup with Docker Compose

The fastest way to get Aegion running:

### 1. Clone the Repository

```bash
git clone https://github.com/NeerajCodz/Aegion
cd Aegion
```

### 2. Start with Docker Compose

```bash
# Start all services
docker-compose up -d

# Check status
docker-compose ps
```

This will start:
- Aegion API on port 8080
- PostgreSQL database on port 5432
- Admin panel at http://localhost:8080/aegion

### 3. Bootstrap Admin User

Create your first admin user:

```bash
# Bootstrap with email
docker-compose exec aegion aegion bootstrap --admin-email admin@example.com

# Or with custom password
docker-compose exec aegion aegion bootstrap \
  --admin-email admin@example.com \
  --admin-password your-secure-password
```

### 4. Access the Admin Panel

Navigate to [http://localhost:8080/aegion](http://localhost:8080/aegion) and log in with your admin credentials.

## Manual Setup (Development)

For development or custom deployments:

### 1. Database Setup

```bash
# Start PostgreSQL
docker run -d \
  --name aegion-postgres \
  -e POSTGRES_DB=aegion \
  -e POSTGRES_USER=aegion \
  -e POSTGRES_PASSWORD=aegion \
  -p 5432:5432 \
  postgres:14

# Run migrations
cd Aegion
go run cmd/aegion/main.go migrate --config configs/aegion.yaml
```

### 2. Build and Run

```bash
# Build all components
make build

# Run with configuration
./aegion.exe serve --config configs/aegion.yaml
```

## Configuration

Aegion uses a single configuration file `aegion.yaml`. Key settings:

```yaml
# configs/aegion.yaml
server:
  host: "0.0.0.0"
  port: 8080

database:
  dsn: "postgres://aegion:aegion@localhost:5432/aegion?sslmode=disable"

security:
  session_secret: "your-session-secret-32-chars"
  
email:
  smtp:
    host: "localhost"
    port: 587
    username: ""
    password: ""
```

## Your First API Calls

Once running, test the API:

### Health Check

```bash
curl http://localhost:8080/health
```

### Create an Identity

```bash
curl -X POST http://localhost:8080/self-service/registration/api \
  -H "Content-Type: application/json" \
  -d '{
    "method": "password",
    "password": "your-password",
    "traits": {
      "email": "user@example.com",
      "name": {
        "first": "John",
        "last": "Doe"
      }
    }
  }'
```

### Login

```bash
curl -X POST http://localhost:8080/self-service/login/api \
  -H "Content-Type: application/json" \
  -d '{
    "method": "password",
    "password_identifier": "user@example.com",
    "password": "your-password"
  }'
```

## Next Steps

- [API Reference](api-reference.md) - Complete API documentation
- [Development Guide](development.md) - Contributing and local development
- [Deployment Guide](deployment.md) - Production deployment
- [Configuration Reference](config.md) - Complete configuration options

## Troubleshooting

### Port Already in Use

If port 8080 is already in use:

```bash
# Change port in docker-compose.yml
ports:
  - "8081:8080"  # Use port 8081 instead
```

### Database Connection Issues

```bash
# Check PostgreSQL is running
docker-compose logs postgres

# Reset database
docker-compose down -v
docker-compose up -d
```

### Admin Bootstrap Fails

```bash
# Check logs
docker-compose logs aegion

# Manual user creation
docker-compose exec aegion aegion admin create-operator \
  --email admin@example.com \
  --password your-password
```

For more help, check the [GitHub Issues](https://github.com/NeerajCodz/Aegion/issues) or create a new one.