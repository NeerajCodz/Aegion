# Aegion Modular Architecture - Current Implementation Status

## Overview

Aegion is designed as a modular identity platform where capabilities are distributed across microservices. However, the current implementation uses a **hybrid approach**:

- **Core-embedded modules**: Tightly integrated into the main `aegion` binary
- **Standalone modules**: Run as separate Docker containers/processes

This document clarifies which modules are which, and the roadmap for full modularization.

## Current Architecture

### Core-Embedded Modules

These modules are currently compiled into and run within the core `aegion` binary:

| Module | Status | Reason | Standalone ETA |
|--------|--------|--------|----------------|
| **password** | ✓ Functional | Session/identity integration tight | Q2 2025 |
| **magic_link** | ✓ Functional | Session/identity integration tight | Q2 2025 |
| **policy** | ⚠ Partial | gRPC interface defined, embedded runtime | Q1 2025 |

**Architecture**:
```
┌─────────────────────────────────────┐
│     aegion core (single process)    │
├─────────────────────────────────────┤
│  ┌─────────────┐  ┌──────────────┐ │
│  │  password   │  │  magic_link  │ │
│  │  handler/   │  │  handler/    │ │
│  │  service/   │  │  service/    │ │
│  │  store      │  │  store       │ │
│  └─────────────┘  └──────────────┘ │
│          │               │          │
│  ┌───────▼───────────────▼───────┐ │
│  │   core/session (shared)       │ │
│  │   core/identity (shared)      │ │
│  └───────────────────────────────┘ │
└─────────────────────────────────────┘
```

**Deployment**: Enabled/disabled via `aegion.yaml` configuration, not via separate containers.

**Benefits of embedded approach**:
- Lower latency (no network hop for auth operations)
- Simpler deployment (fewer containers to manage)
- Easier transaction consistency (same DB connection pool)

**Drawbacks**:
- Cannot scale auth modules independently
- Shared failure domain (module crash can crash core)
- Harder to update modules without restarting core

### Standalone Modules

These modules run as independent microservices:

| Module | Status | Communication | Ready for Prod |
|--------|--------|---------------|----------------|
| **admin** | ✓ Functional | HTTP + gRPC | Yes |
| **oauth2** | ✓ Functional | HTTP + gRPC | Yes |
| **mfa** | ✗ Scaffolded | gRPC (planned) | No |
| **passkeys** | ✗ Scaffolded | gRPC (planned) | No |
| **social** | ✗ Scaffolded | gRPC (planned) | No |
| **sso** | ✗ Scaffolded | gRPC (planned) | No |
| **introspection** | ✗ Scaffolded | gRPC (planned) | No |
| **proxy** | ⚠ Partial | HTTP (reverse proxy) | Partial |
| **cli** | ✗ Scaffolded | CLI (not a service) | No |

**Architecture**:
```
┌──────────────┐     ┌──────────────┐     ┌──────────────┐
│ aegion core  │◄───►│ module-admin │◄───►│ module-oauth2│
└──────┬───────┘     └──────────────┘     └──────────────┘
       │
       │ gRPC / HTTP
       │
       ▼
┌──────────────┐     ┌──────────────┐     ┌──────────────┐
│ postgres     │     │ redis        │     │ service      │
│              │     │              │     │ registry     │
└──────────────┘     └──────────────┘     └──────────────┘
```

**Deployment**: Each module is a separate Docker image/container, orchestrated via docker-compose or Kubernetes.

**Benefits**:
- Independent scaling (scale OAuth2 separately from admin)
- Isolated failure domains (module crash doesn't affect core)
- Independent deployments (update OAuth2 without touching core)
- Language flexibility (could rewrite modules in Rust, etc.)

**Drawbacks**:
- Higher operational complexity (more containers to manage)
- Network latency between modules
- Distributed transaction complexity

## Hybrid Benefits

The current hybrid approach provides:

1. **Fast auth flows**: Password/magic-link authentication happens in-process (low latency)
2. **Scalable OAuth2**: Authorization server can scale independently
3. **Simple deployments**: Small deployments can run just core (all auth embedded)
4. **Enterprise flexibility**: Large deployments can enable standalone modules for scale

## Deployment Scenarios

### Small Deployment (< 1K users)

**Recommended**:
```yaml
# Single container with embedded modules
services:
  aegion:
    image: aegion/core:latest
    # password, magic_link, policy embedded
    # No separate module containers needed
```

**Advantages**:
- Minimal infrastructure (1 container + DB + cache)
- Lower latency
- Simpler monitoring

### Medium Deployment (1K - 10K users)

**Recommended**:
```yaml
services:
  aegion:
    image: aegion/core:latest
    # Core with embedded auth modules
  
  module-admin:
    image: aegion/module-admin:latest
    # Separate admin panel (can be behind firewall)
  
  module-oauth2:
    image: aegion/module-oauth2:latest
    # Separate OAuth2 server (can scale independently)
```

**Advantages**:
- Admin panel isolated (security boundary)
- OAuth2 scales separately (high token issuance load)
- Core remains simple

### Large Deployment (10K+ users)

**Recommended**:
```yaml
services:
  aegion:
    image: aegion/core:latest
    replicas: 3  # Load balanced
  
  module-admin:
    image: aegion/module-admin:latest
    replicas: 1  # Low traffic
  
  module-oauth2:
    image: aegion/module-oauth2:latest
    replicas: 5  # High token traffic
  
  module-proxy:
    image: aegion/module-proxy:latest
    replicas: 3  # Identity-aware reverse proxy
  
  # When ready:
  # module-mfa, module-social, module-sso, etc.
```

**Advantages**:
- Maximum scalability
- Isolated failure domains
- Independent module updates
- Advanced features (proxy, MFA, social, SSO)

## Modularization Roadmap

### Phase 1: Core Stabilization (Q4 2024 - ✓ Complete)

- [x] Core orchestrator and service registry
- [x] Database migrations per module
- [x] Module dependency validation
- [x] Production config validation
- [x] Observability integration

### Phase 2: Standalone Module Maturity (Q1 2025 - In Progress)

- [x] Admin module standalone deployment
- [x] OAuth2 module standalone deployment
- [ ] Policy module extraction from core
- [ ] gRPC contracts for all modules
- [ ] Service discovery integration

### Phase 3: Auth Module Extraction (Q2 2025)

- [ ] Password module standalone server
- [ ] Magic Link module standalone server
- [ ] Backward compatibility (embedded mode still supported)
- [ ] Migration guide for embedded → standalone

### Phase 4: Advanced Modules (Q2-Q3 2025)

- [ ] MFA module implementation
- [ ] Passkeys (WebAuthn) module implementation
- [ ] Social login module implementation
- [ ] SSO (SAML) module implementation
- [ ] Introspection module implementation

### Phase 5: Full Modularization (Q4 2025)

- [ ] All modules available as standalone services
- [ ] Core becomes pure orchestrator (minimal business logic)
- [ ] Module marketplace/registry
- [ ] Third-party module support

## Developer Guide

### Adding a New Standalone Module

1. **Create module structure**:
   ```bash
   modules/
     my_module/
       cmd/
         server/
           main.go           # Server entry point
       handler/
         handler.go          # HTTP/gRPC handlers
       service/
         service.go          # Business logic
       store/
         store.go            # Data persistence
       migrations/
         001_initial.up.sql
       Dockerfile            # Container image
   ```

2. **Implement server entry point** (`cmd/server/main.go`):
   ```go
   package main
   
   import (
       "github.com/aegion/aegion/modules/my_module/handler"
       "github.com/aegion/aegion/modules/my_module/service"
       "github.com/aegion/aegion/modules/my_module/store"
   )
   
   func main() {
       // Load config
       // Connect to DB
       // Initialize store → service → handler
       // Start HTTP/gRPC server
       // Register with service discovery
       // Handle graceful shutdown
   }
   ```

3. **Create Dockerfile**:
   ```dockerfile
   FROM golang:1.25-alpine AS builder
   COPY . .
   RUN go build -o /out/my-module ./modules/my_module/cmd/server
   
   FROM gcr.io/distroless/static-debian12:nonroot
   COPY --from=builder /out/my-module /app/my-module
   ENTRYPOINT ["/app/my-module"]
   CMD ["serve"]
   ```

4. **Add to docker-compose.yml**:
   ```yaml
   module-my-module:
     build:
       dockerfile: modules/my_module/Dockerfile
     environment:
       MODULE_PORT: 9XXX
     depends_on:
       - aegion
     healthcheck:
       test: ["CMD", "wget", "-q", "--spider", "http://localhost:9XXX/health"]
   ```

5. **Register in core**:
   - Add to `docs/modules.md` catalog
   - Add to `core/orchestrator/config.go` dependency graph
   - Add migrations to `cmd/aegion/module_migrations.go`

### Converting Embedded Module to Standalone

(See Phase 3 roadmap - coming Q2 2025)

Key considerations:
- gRPC service definition for inter-module communication
- Session/identity access via core API (not direct DB)
- Service discovery registration
- Health check implementation
- Backward compatibility (support both modes)

## Testing

### Testing Embedded Modules

```bash
# Run core with embedded modules
go test ./modules/password/...
go test ./modules/magic_link/...

# Integration tests
go test ./tests/e2e/...
```

### Testing Standalone Modules

```bash
# Build and run module container
docker build -f modules/oauth2/Dockerfile -t aegion/oauth2:test .
docker run -p 9005:9005 aegion/oauth2:test

# Test module health
curl http://localhost:9005/health

# Integration test with core
./scripts/test-docker-modules.sh
```

## FAQ

**Q: Why not make everything standalone from day 1?**

A: Premature modularization adds operational complexity without clear benefits for small deployments. The hybrid approach lets simple deployments stay simple while enabling advanced users to scale.

**Q: Will embedded modules be removed?**

A: No - embedded mode will remain supported for small/simple deployments. The goal is to support *both* embedded and standalone modes.

**Q: How do I choose embedded vs standalone?**

A: Start embedded (simpler). Move modules to standalone when you need:
- Independent scaling (high load on specific module)
- Isolated failure domains (module crashes shouldn't affect core)
- Independent deployments (update OAuth2 without restarting core)
- Advanced features (only available in standalone mode)

**Q: What's the performance difference?**

A: Embedded modules: ~1-2ms auth latency (in-process)  
Standalone modules: ~5-10ms auth latency (network hop + gRPC overhead)

For most workloads, this difference is negligible. Network latency from client to server is typically 20-100ms+.

**Q: Can I run some modules embedded and others standalone?**

A: Yes! That's the point of the hybrid architecture. For example:
- Password embedded (low latency auth)
- OAuth2 standalone (independent scaling)
- Admin standalone (security isolation)

---

**Last Updated**: 2025-01-01  
**Maintained By**: Aegion Architecture Team
