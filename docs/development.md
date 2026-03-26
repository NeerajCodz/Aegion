# Development Guide

This guide covers local development setup, testing, and contribution workflows for Aegion.

## Prerequisites

Ensure you have these tools installed:

- **Go 1.22+** with modules support
- **Rust 1.75+** with Cargo
- **Node.js 20+** with npm/yarn
- **PostgreSQL 14+** for database
- **Docker & Docker Compose** for testing
- **Git** for version control

## Local Development Setup

### 1. Clone and Setup

```bash
# Clone repository
git clone https://github.com/NeerajCodz/Aegion
cd Aegion

# Install Go dependencies
go mod download

# Setup Rust components
cd rust/security-engine
cargo build
cd ../..

# Install Node.js dependencies (admin panel)
cd web/admin
npm install
cd ../..
```

### 2. Database Setup

```bash
# Start PostgreSQL
docker run -d \
  --name aegion-dev-postgres \
  -e POSTGRES_DB=aegion_dev \
  -e POSTGRES_USER=aegion \
  -e POSTGRES_PASSWORD=aegion_dev \
  -p 5432:5432 \
  postgres:14

# Run migrations
go run cmd/aegion/main.go migrate --config configs/development.yaml
```

### 3. Configuration

Copy and customize the development configuration:

```bash
# Copy example config
cp configs/aegion.example.yaml configs/development.yaml

# Edit for local development
vim configs/development.yaml
```

Key development settings:

```yaml
# configs/development.yaml
server:
  host: "127.0.0.1"
  port: 8080
  debug: true

database:
  dsn: "postgres://aegion:aegion_dev@localhost:5432/aegion_dev?sslmode=disable"
  
logging:
  level: "debug"
  format: "text"

security:
  session_secret: "development-secret-32-characters-long"
  cookie:
    secure: false  # Allow HTTP in development
    
email:
  delivery: "mock"  # Don't send real emails
```

### 4. Running Services

#### Core API Server

```bash
# Run the main server
go run cmd/aegion/main.go serve --config configs/development.yaml

# With live reload (using air)
air -c .air.toml
```

#### Admin Panel (Development)

```bash
# Start development server
cd web/admin
npm run dev

# Panel available at http://localhost:3000
```

#### Full Stack with Docker

```bash
# Use development compose file
docker-compose -f docker-compose.dev.yml up
```

## Building

### Build All Components

```bash
# Use Makefile
make build

# Or build manually
go build -o build/aegion cmd/aegion/main.go
cargo build --release --manifest-path rust/security-engine/Cargo.toml
cd web/admin && npm run build
```

### Cross-Platform Builds

```bash
# Linux
GOOS=linux GOARCH=amd64 go build -o build/aegion-linux cmd/aegion/main.go

# Windows  
GOOS=windows GOARCH=amd64 go build -o build/aegion.exe cmd/aegion/main.go

# macOS
GOOS=darwin GOARCH=amd64 go build -o build/aegion-darwin cmd/aegion/main.go
```

## Testing

### Unit Tests

```bash
# Run all Go tests
go test ./...

# Run with coverage
go test -v -coverprofile=coverage.out ./...
go tool cover -html=coverage.out

# Run specific package
go test ./internal/identity

# Run Rust tests
cd rust/security-engine
cargo test
```

### Integration Tests

```bash
# Start test database
docker-compose -f docker-compose.test.yml up -d postgres

# Run integration tests
go test -tags=integration ./tests/...

# Clean up
docker-compose -f docker-compose.test.yml down -v
```

### E2E Tests

```bash
# Start full environment
docker-compose -f docker-compose.test.yml up -d

# Run end-to-end tests
cd tests/e2e
go test -v ./...

# With specific browsers
E2E_BROWSER=chrome go test -v ./...
```

### Performance Tests

```bash
# Load testing with autocannon
cd tests/load
npm install
npm run test:load

# Memory profiling
go test -memprofile=mem.prof -bench=. ./internal/...
go tool pprof mem.prof
```

## Code Style and Linting

### Go Code Style

We follow standard Go conventions:

```bash
# Format code
go fmt ./...

# Run linter
golangci-lint run

# Check for common issues
go vet ./...

# Install pre-commit hooks
pre-commit install
```

Key style rules:
- Use `gofmt` for formatting
- Follow effective Go guidelines
- Use meaningful variable names
- Add comments for exported functions
- Keep functions small and focused

### Rust Code Style

```bash
# Format code
cd rust/security-engine
cargo fmt

# Run Clippy linter
cargo clippy -- -D warnings

# Check for unsafe code
cargo geiger
```

### JavaScript/TypeScript

```bash
cd web/admin

# Format with Prettier
npm run format

# Lint with ESLint
npm run lint

# Type check
npm run type-check
```

## Commit Conventions

We use Conventional Commits for consistent commit messages:

### Format

```
<type>[optional scope]: <description>

[optional body]

[optional footer(s)]
```

### Types

- **feat**: A new feature
- **fix**: A bug fix
- **docs**: Documentation changes
- **style**: Code style changes (formatting, etc.)
- **refactor**: Code refactoring
- **test**: Adding or updating tests
- **chore**: Build tasks, dependencies, etc.

### Examples

```bash
# Feature
git commit -m "feat(auth): add email OTP authentication"

# Bug fix
git commit -m "fix(session): prevent session fixation attacks"

# Documentation
git commit -m "docs: add API reference for admin endpoints"

# Breaking change
git commit -m "feat(api)!: change login endpoint structure

BREAKING CHANGE: login endpoint now requires method field"
```

### Commit Message Guidelines

- Use imperative mood ("add feature" not "added feature")
- Keep subject line under 72 characters
- Include body for complex changes
- Reference issues when applicable

## Module Development

### Adding New Modules

1. **Create module structure**:
```bash
mkdir modules/new-module
cd modules/new-module
go mod init aegion/modules/new-module
```

2. **Implement module interface**:
```go
type Module interface {
    Name() string
    Initialize(config Config) error
    Routes() []Route
    Cleanup() error
}
```

3. **Register module**:
```go
// In cmd/aegion/modules.go
import "aegion/modules/new-module"

func init() {
    registry.Register("new-module", newmodule.New())
}
```

### Security Engine Extensions

Add new security policies in Rust:

```bash
cd rust/security-engine/src
mkdir new_policy
cd new_policy
```

Implement the policy trait:
```rust
use crate::Policy;

pub struct NewPolicy {
    config: PolicyConfig,
}

impl Policy for NewPolicy {
    fn evaluate(&self, context: &Context) -> Result<Decision> {
        // Policy logic here
    }
}
```

## Database Migrations

### Creating Migrations

```bash
# Generate new migration
go run cmd/aegion/main.go migrate create add_new_table

# Edit migration files in migrations/
# - {timestamp}_add_new_table.up.sql
# - {timestamp}_add_new_table.down.sql
```

### Running Migrations

```bash
# Apply migrations
go run cmd/aegion/main.go migrate up --config configs/development.yaml

# Rollback last migration
go run cmd/aegion/main.go migrate down 1 --config configs/development.yaml

# Check status
go run cmd/aegion/main.go migrate status --config configs/development.yaml
```

## Debugging

### Debug Server

```bash
# Run with debugger
dlv debug cmd/aegion/main.go -- serve --config configs/development.yaml

# Attach to running process
dlv attach $(pgrep aegion)
```

### Logging

Enable detailed logging:

```yaml
logging:
  level: "debug"
  format: "json"
  output: "stdout"
```

### Database Debugging

```bash
# Enable query logging
DATABASE_LOG_QUERIES=true go run cmd/aegion/main.go serve

# Connect to development database
psql postgres://aegion:aegion_dev@localhost:5432/aegion_dev
```

## Contributing Workflow

1. **Fork and clone** the repository
2. **Create feature branch** from `main`
3. **Make changes** following code style
4. **Write tests** for new functionality
5. **Run full test suite** locally
6. **Commit** with conventional commit format
7. **Push** to your fork
8. **Create pull request** with detailed description

### Pull Request Checklist

- [ ] Tests pass locally
- [ ] Code follows style guidelines  
- [ ] Documentation updated if needed
- [ ] Commit messages follow conventions
- [ ] No secrets or sensitive data included
- [ ] Breaking changes documented

### Code Review Process

- All changes require review from maintainers
- Address feedback promptly
- Keep PRs focused and reasonably sized
- Update documentation for API changes

## Environment Variables

Development-specific environment variables:

```bash
# Development mode
AEGION_ENV=development

# Database
DATABASE_URL=postgres://aegion:aegion_dev@localhost:5432/aegion_dev

# Logging
LOG_LEVEL=debug
LOG_FORMAT=text

# Security (development only)
DISABLE_CSRF=true
COOKIE_SECURE=false

# Email (development)
EMAIL_DELIVERY=mock
SMTP_HOST=localhost
SMTP_PORT=1025  # Use mailhog for testing
```

## Troubleshooting

### Common Issues

**Build Failures**:
```bash
# Clear module cache
go clean -modcache
go mod download

# Rebuild Rust components
cd rust/security-engine && cargo clean && cargo build
```

**Database Connection**:
```bash
# Check PostgreSQL is running
docker ps | grep postgres

# Test connection
psql postgres://aegion:aegion_dev@localhost:5432/aegion_dev -c "SELECT 1;"
```

**Port Conflicts**:
```bash
# Find what's using port 8080
netstat -tulpn | grep :8080

# Use different port
AEGION_PORT=8081 go run cmd/aegion/main.go serve
```

### Getting Help

- Check existing [GitHub Issues](https://github.com/NeerajCodz/Aegion/issues)
- Join discussions in [GitHub Discussions](https://github.com/NeerajCodz/Aegion/discussions)  
- Read the [Architecture documentation](architecture.md)
- Review the [Configuration reference](config.md)

For urgent issues, create a new GitHub issue with:
- Detailed description of the problem
- Steps to reproduce
- Environment details (OS, Go version, etc.)
- Relevant logs or error messages