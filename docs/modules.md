# Aegion — Modules

Every Aegion capability is a module. Every module is a standalone Docker image. `core` pulls and orchestrates them.

---

## The fundamental model

Aegion is a **hub-and-spoke container platform**. `core` is the hub; every feature module is a spoke — a separate container image pulled on demand, wired internally, and hidden behind a single public ingress.

```
docker-compose (user)
    └── aegion/aegion-core:latest     ← the ONLY container users declare
            │
            ├── reads aegion.yaml
            ├── resolves enabled module set
            ├── validates dependency graph (fail fast before pulling anything)
            ├── pulls module images   (aegion/aegion-mfa:2.1.0, etc.)
            ├── starts containers on private aegion_modules network
            ├── health-checks each container
            ├── registers module endpoints in service registry
            ├── routes inbound requests to the right module
            └── exposes ONE public port
```

The user never writes a compose entry for `aegion-mfa`. They never manage module networking. They never version individual modules (unless they want to). Core handles everything, driven by `aegion.yaml`.

---

## Why separate images, not a monolith

| Concern | Monolith | Aegion module images |
|---|---|---|
| Footprint | All code always in memory | Only enabled modules are present |
| Scaling | Scale everything or nothing | Scale each module independently |
| Security surface | Disabled features still in binary | Disabled modules physically absent |
| Update velocity | Full rebuild for any change | Update one module image independently |
| Isolation | Shared process, shared failure | Module crash does not crash core |
| Startup time | Grows with feature count | Proportional to enabled set only |
| Testing | Full integration test always | Per-module CI + platform integration suite |

---

## Module catalog

| Module image | Registry tag | Default | Purpose |
|---|---|---|---|
| `aegion/aegion-core` | `core` | always on | Identity, sessions, flows, courier, key lifecycle, Rust engines |
| `aegion/aegion-password` | `password` | on | Password authentication, HIBP, history |
| `aegion/aegion-mfa` | `mfa` | off | TOTP, SMS, WebAuthn 2FA, backup codes, trusted devices |
| `aegion/aegion-passkeys` | `passkeys` | off | WebAuthn passwordless first-factor |
| `aegion/aegion-magic-link` | `magic_link` | off | Email/SMS OTP and magic link flows |
| `aegion/aegion-social` | `social` | off | OAuth2/OIDC social login providers |
| `aegion/aegion-sso` | `sso` | off | Enterprise SAML SSO + SCIM 2.0 provisioning |
| `aegion/aegion-oauth2` | `oauth2` | off | OAuth2/OIDC authorization server |
| `aegion/aegion-introspect` | `introspect` | off | RFC 7662 token introspection |
| `aegion/aegion-policy` | `policy` | off | RBAC + ABAC + ReBAC authorization engine |
| `aegion/aegion-proxy` | `proxy` | off | Identity-aware ingress and policy enforcement |
| `aegion/aegion-admin` | `admin` | on | Admin panel SPA + management APIs |
| `aegion/aegion-cli` | `cli` | off | Operator CLI tooling |

> `core` is always present. All other modules are resolved from `aegion.yaml`.

---

## Startup sequence (detailed)

Core executes a deterministic startup sequence every time it boots:

```
Phase 1 — Configuration loading
  1a. Read aegion.yaml (or path from AEGION_CONFIG env var)
  1b. Apply environment variable overrides (AEGION_DATABASE_URL, etc.)
  1c. Validate configuration structure (fail fast on schema errors)

Phase 2 — Dependency validation (before any network calls)
  2a. Build enabled module set from yaml
  2b. Check all dependency constraints (e.g. introspection → oauth2)
  2c. If any constraint unsatisfied: print clear error, exit non-zero
  2d. Validate module_versions compatibility matrix against release manifest

Phase 3 — Infrastructure connections
  3a. Connect to Postgres (retry with exponential backoff, max 30s)
  3b. Acquire migration advisory lock
  3c. Run pending migrations (core first, then modules in dependency order)
  3d. Release migration advisory lock
  3e. Connect to Redis if cache.enabled (non-fatal if unavailable)

Phase 4 — Module image resolution
  4a. For each enabled module in dependency order:
        - check local Docker image cache
        - if absent or outdated per pull_policy:
            pull from registry (retry up to 3 times on transient failure)
            if pull fails: log error, mark module unavailable
            if module is in required_modules set: exit non-zero
  4b. Log final image set with digests

Phase 5 — Module container startup
  5a. Create aegion_modules Docker network if not present
  5b. For each enabled module in dependency order:
        - start container with: image, internal network, env vars
          (AEGION_INTERNAL_TOKEN, AEGION_CORE_ADDR, AEGION_DB_URL,
           AEGION_MODULE_LISTEN_ADDR, AEGION_LOG_LEVEL)
        - wait for /ready endpoint to return 200 (timeout: startup_timeout)
        - on timeout: stop container, log error
          if required module: exit non-zero
          if optional module: continue with reduced capability set
        - register module in service registry
  5c. Log registered module set

Phase 6 — Public ingress
  6a. Start HTTP server on configured port
  6b. Begin routing inbound requests to registered modules
  6c. Log startup complete with module set and version info
```

### Startup failure behavior

| Failure type | Default behavior | Configuration |
|---|---|---|
| Postgres unreachable | Retry for 30s, then exit non-zero | `database.connect_timeout` |
| Required module pull fails | Exit non-zero — cannot start without it | All enabled modules are required by default |
| Module /ready timeout | Log error, skip module (degraded mode) | `internal_network.startup_timeout` |
| Module dependency missing | Exit non-zero immediately (Phase 2) | Not configurable — hard constraint |
| Version incompatibility | Exit non-zero with compatibility report | Not configurable — hard constraint |

Core never starts in a partially valid state for required modules. It either starts with the declared module set or does not start.

---

## Module container contract

Every module image must implement this contract to be loadable by core.

### Required HTTP endpoints (internal)

| Endpoint | Method | Response | Purpose |
|---|---|---|---|
| `/health` | GET | 200 when alive | Liveness — core polls every 5s |
| `/ready` | GET | 200 when ready to serve | Readiness — core waits for this at startup |
| `/meta` | GET | JSON module metadata | Version, capabilities, routes |

`/meta` response shape:
```json
{
  "module":       "mfa",
  "version":      "2.1.0",
  "capabilities": ["totp", "webauthn", "backup_codes", "trusted_devices"],
  "routes": [
    "/self-service/mfa/*",
    "/api/v1/mfa/*"
  ],
  "grpc_services": ["mfa.MFAEngine"],
  "event_subscriptions": ["session.created", "identity.updated", "identity.deleted"]
}
```

### Registration handshake

On startup, each module calls core's `ModuleRegistry.Register` gRPC endpoint:

```json
{
  "module":       "mfa",
  "version":      "2.1.0",
  "address":      "aegion-mfa-1:9002",
  "routes":       ["/self-service/mfa/*", "/api/v1/mfa/*"],
  "capabilities": ["totp", "webauthn", "backup_codes"],
  "health_url":   "http://aegion-mfa-1:9002/health",
  "ready_url":    "http://aegion-mfa-1:9002/ready"
}
```

Core acknowledges with an `instance_id`. The module then subscribes to the event bus for its declared event types. Routing of declared routes begins immediately.

### Environment variables injected by core

Every module container receives these environment variables at start time:

| Variable | Purpose |
|---|---|
| `AEGION_INTERNAL_TOKEN` | Current internal auth token for gRPC authentication |
| `AEGION_CORE_ADDR` | gRPC address of core's internal API server |
| `AEGION_DB_URL` | Postgres connection string (module-specific role) |
| `AEGION_MODULE_LISTEN_ADDR` | Address the module should listen on (e.g., `0.0.0.0:9002`) |
| `AEGION_LOG_LEVEL` | Log level (inherited from core's aegion.yaml setting) |
| `AEGION_LOG_FORMAT` | Log format: json or text |
| `AEGION_CIPHER_ALGORITHM` | Encryption algorithm for field-level encryption |
| `AEGION_PUBLIC_URL` | Aegion's public base URL |

Modules never read `aegion.yaml` directly. All configuration flows through environment variables injected by core or via runtime config retrieved from Postgres on startup.

---

## Individual module scaling

Because each module is a separate container, they scale independently.

### Docker Compose override

```yaml
services:
  aegion:
    image: aegion/aegion-core:2.1.0
    environment:
      AEGION_MODULE_REPLICAS_OAUTH2: "4"
      AEGION_MODULE_REPLICAS_POLICY: "3"
      AEGION_MODULE_REPLICAS_PROXY:  "6"
```

Core starts the declared number of replicas for each module and registers all instances in the service registry. Inbound requests are load-balanced across replicas using least-connections selection.

### Kubernetes deployment

In Kubernetes, each module is a separate `Deployment`. Core's service registry is updated via module self-registration — when a new pod starts, it registers with core; when it terminates (gracefully), it calls `ModuleRegistry.Deregister` before shutting down.

```yaml
# Example: scale policy independently
apiVersion: apps/v1
kind: Deployment
metadata:
  name: aegion-policy
spec:
  replicas: 6
  template:
    spec:
      containers:
        - name: policy
          image: aegion/aegion-policy:2.1.0
          env:
            - name: AEGION_CORE_ADDR
              value: "aegion-core:9001"
            - name: AEGION_DB_URL
              valueFrom:
                secretKeyRef:
                  name: aegion-module-secrets
                  key: policy-db-url
```

### Scaling profiles by module

| Module | Scale driver | Notes |
|---|---|---|
| `core` | Total request throughput | Routes all traffic; scale proportionally to overall load |
| `mfa` | MFA verification concurrency | TOTP is CPU-light; scale for concurrent flows |
| `oauth2` | Token issuance rate | JWT signing is CPU-bound (Rust engine); scale for throughput |
| `policy` | Authorization check rate | ReBAC traversal is the expensive path; scale for P99 latency |
| `proxy` | Upstream forwarding volume | Scale with upstream traffic rate |
| `admin` | Admin operator headcount | Usually minimal; 1–2 replicas sufficient |
| `sso` | Enterprise login concurrency | Scale during workday peaks for large organizations |
| `introspection` | Token introspection rate | Scale when many services use opaque tokens |

---

## Module upgrade lifecycle

### Rolling upgrade (no downtime)

Because modules are separate containers, each module can be upgraded independently via a rolling deployment:

```
1. Push new image: aegion/aegion-mfa:2.1.1

2. Kubernetes rolling update:
   - Start one new mfa:2.1.1 container
   - Wait for /ready to return 200
   - New instance self-registers with core
   - Core begins routing some traffic to new instance
   - Terminate one old mfa:2.1.0 instance (after graceful drain)
   - Repeat until all old instances replaced

3. At no point is mfa unavailable
4. Core's service registry always reflects live instances only
```

The rolling upgrade is safe because:
- Module APIs are backward-compatible within a minor version
- The proto contracts in `proto/` are versioned and backward-compatible
- Core's service registry handles mixed-version instances during the rollout window

### Graceful shutdown

When a module container receives `SIGTERM`, it:

1. Calls `ModuleRegistry.Deregister` on core — core stops routing new requests to this instance
2. Waits for in-flight requests to complete (grace period: 15s by default)
3. Closes gRPC server and event bus connection
4. Exits cleanly

New requests are routed to remaining healthy instances during the grace period. There is no traffic loss during a clean shutdown.

### Rollback

To roll back a module to the previous version:

```yaml
# aegion.yaml — change version pin
module_versions:
  mfa: "2.1.0"   # was 2.1.1
```

Then restart core (which will pull the downgraded image) or update the Kubernetes deployment directly:

```bash
kubectl set image deployment/aegion-mfa mfa=aegion/aegion-mfa:2.1.0
```

### Major version upgrades (breaking proto changes)

When a proto interface in `proto/<module>/` changes in a backward-incompatible way, a major version bump is required:

1. Both old and new gRPC services are registered in the proto (old version deprecated, not removed)
2. Callers are updated to use the new service before the old one is removed
3. A migration guide documents the transition path
4. The minimum `core` version that supports the new proto is declared in the module's release notes

---

## Image caching and pull policy

| `pull_policy` | Behavior |
|---|---|
| `if-not-present` | Pull only if image not in local Docker cache. Fastest startup. Good for development. |
| `always` | Always pull on core startup. Verifies digest against registry. Good for production with `latest` tag (though version pinning is still preferred). |
| `never` | Never pull. Fail if image not present locally. Required for air-gapped deployments. |

For air-gapped deployments:
1. Set `pull_policy: never`
2. Set `module_registry.base_url` to your internal registry
3. Pre-load all module images into the internal registry before deploying core
4. Core will resolve images from the internal registry only

### Image digest verification

When `pull_policy: always` is set, core verifies the pulled image digest against the expected digest from the release manifest (`aegion-release-<version>.json`) shipped with each core release. If the digest does not match, core refuses to start the module and logs an error. This prevents supply chain attacks where an image at a given tag has been replaced.

---

## Dependency rules

| Module | Depends on |
|---|---|
| `core` | — |
| `password` | `core` |
| `mfa` | `core`, `password` |
| `passkeys` | `core` |
| `magic_link` | `core`, courier (in core) |
| `social` | `core`, courier (in core) |
| `sso` | `core`, courier (in core) |
| `oauth2` | `core` |
| `introspection` | `oauth2` |
| `policy` | `core` |
| `proxy` | `core`, `policy` (recommended — without policy, only `allow`/`deny` authorizers work) |
| `admin` | `core`, `policy` |
| `cli` | `core` via public ingress only |

Dependency validation runs in Phase 2 of startup — before any images are pulled. Violations produce a clear error:

```
ERROR: module dependency not satisfied
  module "introspection" requires "oauth2"
  "oauth2" is not enabled in aegion.yaml

  To fix:
    Set oauth2.enabled: true in aegion.yaml
    OR set introspection.enabled: false
```

---

## Module networking

### Internal network topology

```
Public internet
      │
      ▼
 ┌──────────────────────────────────────────────────────────────┐
 │  Host machine                                                 │
 │                                                               │
 │  Public port :8080 ──► core ingress                         │
 │                                                               │
 │  ┌────────────────  aegion_modules (172.20.0.0/16)  ───────┐ │
 │  │                                                          │ │
 │  │  core      :9001    password  :9002    mfa       :9003  │ │
 │  │  oauth2    :9004    policy    :9005    proxy     :9006  │ │
 │  │  admin     :9007    sso       :9008    passkeys  :9009  │ │
 │  │                                                          │ │
 │  └──────────────────────────────────────────────────────────┘ │
 └──────────────────────────────────────────────────────────────┘
```

No module port is exposed on the host. Only core's single public port is reachable from outside the host.

Module-to-module calls use DNS names on the internal network (e.g., `aegion-mfa:9003`). For scaled modules with multiple replicas, DNS round-robin combined with core's service registry provides the address list for client-side load balancing.

---

## Operator checklist

Before deployment:

1. Enable only required modules in `aegion.yaml`
2. Verify all dependency constraints are satisfied
3. Pin module versions for production (`module_versions` block)
4. Set `pull_policy: always` or pre-load images for air-gapped environments
5. Configure `module_registry.base_url` for private registries
6. Expose only core's single public ingress port
7. Set replica counts per module based on expected load profile
8. Configure resource limits per module container (CPU/memory) appropriate to workload
9. Set `module_auto_restart: true` in feature flags for production
10. Monitor the Platform → Modules view in admin after first boot
