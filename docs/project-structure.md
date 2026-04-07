# Aegion — Project Structure

This document defines the canonical monorepo layout for Aegion. The structure mirrors the runtime model: `core` is the hub, `modules/` contains every capability as a separately deployable image, `rust/` contains all performance-critical engines, and `internal/` holds the shared platform contracts everything depends on.

> **Implementation status (current repo):** currently integrated modules are `password`, `magic_link`, and `admin`.
> Additional module trees now exist for `oauth2`, `policy`, `mfa`, `passkeys`, `social`, `sso`, `introspection`, `proxy`, and `cli`, with capability completion continuing by roadmap phase.

---

## Full layout

```text
aegion/
├── docs/                          ← all platform documentation
│   ├── overview.md
│   ├── architecture.md
│   ├── modules.md
│   ├── inter-module-communication.md
│   ├── aegion-db-schema.md
│   ├── security.md
│   ├── oauth.md
│   ├── policy.md
│   ├── proxy.md
│   ├── admin.md
│   ├── config.md
│   ├── timeline.md
│   ├── project-structure.md
│   └── aegion-project.md
│
├── cmd/                           ← binary entry points
│   └── aegion/
│       └── main.go                ← core entry point (starts module orchestrator)
│
├── core/                          ← core platform logic (Go)
│   ├── orchestrator/              ← module pull / start / register / health lifecycle
│   ├── registry/                  ← in-memory service registry + gRPC server
│   ├── router/                    ← HTTP routing table, prefix trie, load balancing
│   ├── eventbus/                  ← internal event broker + Postgres-backed delivery
│   ├── session/                   ← session resolution, AAL computation, context injection
│   ├── courier/                   ← email/SMS queue + dispatcher background worker
│   ├── workers/                   ← all background goroutines (cleanup, rotation, etc.)
│   ├── authtoken/                 ← internal inter-module token generation + rotation
│   ├── migrations/                ← core schema migration files (core_* tables)
│   └── server/                    ← HTTP + gRPC server setup, middleware stack
│
├── modules/                       ← one directory = one independently deployable Docker image
│   │
│   ├── password/
│   │   ├── handler/               ← HTTP handlers for /self-service/login/methods/password
│   │   ├── service/               ← business logic (credential check, history, HIBP)
│   │   ├── store/                 ← Postgres adapters for pwd_credentials, pwd_history
│   │   ├── grpc/                  ← gRPC server stub (no external gRPC for password; registers only)
│   │   ├── migrations/            ← pwd_* table migration files
│   │   ├── cmd/server/main.go     ← module entry point
│   │   └── Dockerfile
│   │
│   ├── mfa/
│   │   ├── handler/               ← HTTP handlers for /self-service/mfa/*
│   │   ├── service/
│   │   │   ├── totp/              ← TOTP enrollment + verification
│   │   │   ├── webauthn/          ← WebAuthn second-factor
│   │   │   ├── sms/               ← SMS factor
│   │   │   └── backup_codes/      ← backup code generation + verification
│   │   ├── store/                 ← Postgres adapters for mfa_credentials, mfa_trusted_devices
│   │   ├── grpc/                  ← MFAEngine gRPC server (GetStatus, VerifyFactor, etc.)
│   │   ├── migrations/
│   │   ├── cmd/server/main.go
│   │   └── Dockerfile
│   │
│   ├── passkeys/
│   │   ├── handler/
│   │   ├── service/               ← WebAuthn registration + authentication ceremony
│   │   ├── store/                 ← Postgres adapters for pk_credentials
│   │   ├── grpc/
│   │   ├── migrations/
│   │   ├── cmd/server/main.go
│   │   └── Dockerfile
│   │
│   ├── magic_link/
│   │   ├── handler/
│   │   ├── service/               ← OTP code generation, magic link URL construction
│   │   ├── store/                 ← Postgres adapters for ml_codes
│   │   ├── grpc/
│   │   ├── migrations/
│   │   ├── cmd/server/main.go
│   │   └── Dockerfile
│   │
│   ├── social/
│   │   ├── handler/
│   │   ├── service/               ← OAuth2 PKCE flow, Jsonnet claim mapper
│   │   ├── store/                 ← Postgres adapters for soc_connections, soc_state
│   │   ├── grpc/
│   │   ├── migrations/
│   │   ├── cmd/server/main.go
│   │   └── Dockerfile
│   │
│   ├── sso/
│   │   ├── handler/
│   │   ├── service/
│   │   │   ├── saml/              ← SAML 2.0 SP implementation
│   │   │   └── scim/              ← SCIM 2.0 provisioning (users, groups, bulk, filters)
│   │   ├── store/                 ← sso_saml_providers, sso_saml_sessions, sso_scim_*
│   │   ├── grpc/
│   │   ├── migrations/
│   │   ├── cmd/server/main.go
│   │   └── Dockerfile
│   │
│   ├── oauth2/
│   │   ├── handler/               ← /oauth2/authorize, /oauth2/token, /oauth2/consent, etc.
│   │   ├── service/
│   │   │   ├── authorization/     ← auth code flow, consent challenge
│   │   │   ├── token/             ← issuance, rotation, revocation, family invalidation
│   │   │   ├── device/            ← RFC 8628 device authorization grant
│   │   │   └── hook/              ← token claims webhook client
│   │   ├── store/                 ← oa2_clients, oa2_auth_codes, oa2_access_tokens, etc.
│   │   ├── grpc/                  ← TokenStore gRPC server (Introspect, Revoke, InvalidateFamily)
│   │   ├── migrations/
│   │   ├── cmd/server/main.go
│   │   └── Dockerfile
│   │
│   ├── introspection/
│   │   ├── handler/               ← POST /oauth2/introspect
│   │   ├── service/               ← token validation, cache layer
│   │   ├── store/                 ← no owned tables; reads via gRPC → oauth2
│   │   ├── grpc/
│   │   ├── migrations/            ← empty (no owned tables)
│   │   ├── cmd/server/main.go
│   │   └── Dockerfile
│   │
│   ├── policy/
│   │   ├── handler/               ← /relation-tuples/check, /roles, /abac/rules, etc.
│   │   ├── service/
│   │   │   ├── rbac/              ← role + permission evaluation, assignment cache
│   │   │   ├── abac/              ← CEL rule loading, compilation, evaluation
│   │   │   └── rebac/             ← namespace config, tuple management, gRPC → Rust engine
│   │   ├── store/                 ← pol_roles, pol_permissions, pol_abac_rules, pol_rebac_*
│   │   ├── grpc/                  ← PolicyEngine gRPC server (Check, BatchCheck, Explain)
│   │   ├── migrations/
│   │   ├── cmd/server/main.go
│   │   └── Dockerfile
│   │
│   ├── proxy/
│   │   ├── handler/               ← request ingress, pipeline execution, upstream forwarding
│   │   ├── service/
│   │   │   ├── matcher/           ← route rule trie matching
│   │   │   ├── authenticator/     ← bearer_token, cookie_session, anonymous, noop
│   │   │   ├── authorizer/        ← allow, deny, policy_engine, cel, remote_json
│   │   │   ├── mutator/           ← header, id_token, cookie, hydrator, noop
│   │   │   └── circuit/           ← per-upstream circuit breaker
│   │   ├── store/                 ← prx_access_rules
│   │   ├── grpc/
│   │   ├── migrations/
│   │   ├── cmd/server/main.go
│   │   └── Dockerfile
│   │
│   └── admin/
│       ├── handler/               ← /aegion/api/v1/* management API handlers
│       ├── service/               ← identity ops, session ops, capability checks
│       ├── store/                 ← adm_identities, adm_roles, adm_capability_overrides
│       ├── grpc/
│       ├── migrations/
│       ├── ui/                    ← React + TypeScript admin SPA
│       │   ├── src/
│       │   │   ├── pages/         ← Identities, Sessions, OAuth2, Policy, Proxy, Enterprise, System
│       │   │   ├── components/
│       │   │   ├── hooks/
│       │   │   └── api/           ← typed API client (generated from OpenAPI spec)
│       │   ├── dist/              ← compiled SPA — embedded into module image at build time
│       │   └── package.json
│       ├── cmd/server/main.go
│       └── Dockerfile
│
├── internal/                      ← shared Go libraries used by core and all modules
│   ├── platform/                  ← module registration client, event bus client, health handler
│   │   ├── registry/              ← gRPC client for ModuleRegistry.Register / Deregister / Heartbeat
│   │   ├── eventbus/              ← gRPC client for EventBus.Publish / Subscribe / Acknowledge
│   │   ├── authtoken/             ← middleware: validates x-aegion-internal-token on incoming calls
│   │   ├── sessionctx/            ← extracts + HMAC-verifies X-Aegion-Session-Ctx header
│   │   ├── health/                ← standard /health /ready /meta HTTP handler
│   │   └── module/                ← ModuleConfig (reads env vars injected by core), startup helper
│   └── proto/                     ← generated gRPC stubs (committed — no protoc needed at build time)
│       ├── core/                  ← registry, session, courier, events, internal_token
│       ├── mfa/
│       ├── policy/
│       └── oauth2/
│
├── rust/                          ← Rust performance engines (compiled into core image only)
│   ├── crypto/                    ← Argon2id, bcrypt, scrypt, XChaCha20-Poly1305, AES-GCM, constant-time compare
│   │   ├── src/
│   │   │   ├── hash.rs            ← hash_password / verify_password
│   │   │   ├── encrypt.rs         ← field-level encrypt / decrypt
│   │   │   └── compare.rs         ← constant-time HMAC and token comparison
│   │   ├── Cargo.toml
│   │   └── fuzz/                  ← cargo-fuzz targets for each public function
│   ├── jwt/                       ← RS256 / ES256 sign + verify, JWKS serialization, key generation
│   │   ├── src/
│   │   │   ├── sign.rs
│   │   │   ├── verify.rs
│   │   │   ├── jwks.rs
│   │   │   └── keygen.rs
│   │   ├── Cargo.toml
│   │   └── fuzz/
│   ├── policy/                    ← ReBAC tuple graph traversal + in-process expansion cache
│   │   ├── src/
│   │   │   ├── engine.rs          ← iterative BFS expansion algorithm
│   │   │   ├── cache.rs           ← LRU expansion result cache
│   │   │   └── namespace.rs       ← namespace config parsing + relation inheritance resolution
│   │   ├── Cargo.toml
│   │   └── fuzz/
│   └── proxy/                     ← optional high-throughput JWT + session token hot-path helpers
│       ├── src/
│       │   ├── verify.rs          ← fast-path JWT signature check for proxy inline validation
│       │   └── session.rs         ← session context header HMAC verify
│       ├── Cargo.toml
│       └── fuzz/
│
├── proto/                         ← protobuf source definitions (source of truth for all gRPC interfaces)
│   ├── core/
│   │   ├── registry.proto
│   │   ├── session.proto
│   │   ├── courier.proto
│   │   ├── events.proto
│   │   └── internal_token.proto
│   ├── mfa/
│   │   └── mfa.proto
│   ├── policy/
│   │   └── policy.proto
│   └── oauth2/
│       └── tokens.proto
│
├── scripts/
│   ├── build-all.sh               ← build all module images
│   ├── build-module.sh            ← build a single module image: ./scripts/build-module.sh mfa
│   ├── gen-proto.sh               ← regenerate internal/proto/ stubs from proto/ sources
│   ├── gen-rust-bindings.sh       ← regenerate CGo bindings from rust/ crates
│   ├── resolve-tags.sh            ← legacy: resolve Go build tags (kept for compatibility)
│   └── lint.sh                    ← run golangci-lint + cargo clippy across the whole repo
│
├── configs/
│   ├── aegion.yaml                ← development default config (safe defaults, local URLs)
│   ├── aegion.production.yaml     ← production-oriented configuration
│   ├── aegion.staging.yaml        ← staging-oriented configuration
│   └── aegion.test.yaml           ← test configuration
│
├── build/
│   ├── Dockerfile.base            ← shared base image: Go + Rust toolchain + CA certs + non-root user
│   ├── Dockerfile.base-runtime    ← minimal runtime base: CA certs + non-root user (no toolchain)
│   └── release-manifest.json      ← per-release image digest map (used by core for digest verification)
│
├── deploy/
│   ├── docker-compose.yml         ← local dev: core + postgres + redis + mailpit
│   ├── docker-compose.prod.yml    ← production compose reference
│   ├── kubernetes/
│   │   ├── core/
│   │   │   ├── deployment.yaml
│   │   │   └── service.yaml
│   │   ├── modules/               ← one Deployment + Service per module
│   │   │   ├── mfa.yaml
│   │   │   ├── oauth2.yaml
│   │   │   ├── policy.yaml
│   │   │   └── ...
│   │   └── shared/
│   │       ├── postgres.yaml
│   │       ├── redis.yaml
│   │       ├── network-policy.yaml   ← blocks all inter-module traffic not via aegion_modules
│   │       └── rbac.yaml
│   └── helm/
│       └── aegion/
│           ├── Chart.yaml
│           ├── values.yaml
│           └── templates/
│
└── aegion.yaml                    ← root config read by core at startup
```

---

## Boundary rules

### `cmd/`

One entry point only — `cmd/aegion/main.go`. This is core's binary entry point. It wires together everything in `core/` and starts the module orchestrator. There are no other binaries in `cmd/` — every module has its own `cmd/server/main.go` inside its module directory.

### `core/`

Core platform logic that runs inside the core container. This is the control plane: orchestration, routing, event bus, session management, courier. It does not contain any auth feature logic — no password hashing, no TOTP, no OAuth2 flows. Those live in `modules/`.

Core is the only place that links the Rust engines (via CGo). No module links Rust directly.

### `modules/<n>/`

One directory per module image. Each module is a fully self-contained Go service with its own handlers, service layer, Postgres store, gRPC server, migrations, and Dockerfile. Modules share nothing except what is exported from `internal/`.

Modules never import from each other's directories. A module that needs something from another module calls it via gRPC. This boundary is enforced by `go vet` and a custom linter rule that fails the CI build on any cross-module import.

### `internal/`

Two sub-trees:

**`internal/platform/`** — the shared Go libraries every module depends on to participate in the platform: how to register with core, how to publish and subscribe to events, how to validate incoming internal auth tokens, how to extract and verify the session context header, and how to serve the standard health/ready/meta endpoints. Every module imports these. Changes here require cross-module review because they affect every image.

**`internal/proto/`** — the generated gRPC stubs compiled from `proto/`. These are committed to the repo so that module builds do not require `protoc` to be installed. Regenerated by running `./scripts/gen-proto.sh` whenever a `.proto` file changes.

### `rust/`

All four Rust crates live here. They are compiled once during the core image build and linked into the core binary via CGo. No module Dockerfile touches `rust/` — only `build/Dockerfile.base` does.

The four crates and their scope:

| Crate | What it provides |
|---|---|
| `rust/crypto/` | Password hashing (Argon2id, bcrypt), field-level encryption (XChaCha20, AES-GCM), constant-time comparison |
| `rust/jwt/` | JWT signing and verification (RS256, ES256), JWKS serialization, keypair generation |
| `rust/policy/` | ReBAC tuple graph traversal, expansion cache, namespace config resolution |
| `rust/proxy/` | Fast-path JWT verification and session HMAC check for high-throughput proxy inline validation |

Each crate has a `fuzz/` directory with `cargo-fuzz` targets. Fuzzing runs continuously on the main branch.

### `proto/`

Source of truth for all gRPC interfaces. No `.proto` file is scattered in a module directory. All proto definitions live here, organized by the service that owns them. When a proto changes, `./scripts/gen-proto.sh` regenerates `internal/proto/` and the change is committed. PRs that modify a `.proto` file require review from both the owning module's team and all consumer modules.

### `scripts/`

Thin shell scripts that wrap build and codegen commands. No business logic here. If a script exceeds ~30 lines it should be a Go tool in `cmd/tools/` instead.

### `configs/`

Config files are environment-specific. The development default (`aegion.yaml`) uses safe localhost defaults; `aegion.production.yaml`, `aegion.staging.yaml`, and `aegion.test.yaml` provide environment variants.

### `build/`

Docker base images and the release manifest. `Dockerfile.base` is the build-time image containing Go + Rust toolchains. `Dockerfile.base-runtime` is the minimal runtime image (no toolchains, no shell, distroless-style). All module Dockerfiles derive from `base-runtime`.

`release-manifest.json` maps module names to their expected image digests at each platform version. Core reads this at startup when `pull_policy: always` to verify image integrity.

### `deploy/`

Everything needed to run Aegion in an environment. Local dev uses `docker-compose.yml`. Production Kubernetes manifests are in `deploy/kubernetes/`. The Helm chart in `deploy/helm/` wraps the Kubernetes manifests for parameterized deployment.

The `deploy/kubernetes/shared/network-policy.yaml` is important: it enforces that all inter-module traffic flows through the `aegion_modules` network only. Direct pod-to-pod calls outside this network are blocked at the Kubernetes network layer.

---

## Dockerfile pattern (all modules follow this)

```dockerfile
# Stage 1: build
FROM aegion/base:latest AS build
WORKDIR /workspace

# Copy shared dependencies first (better layer caching)
COPY internal/ ./internal/
COPY go.mod go.sum ./
RUN go mod download

# Copy the specific module
COPY modules/<n>/ ./modules/<n>/

# Build the module binary
RUN CGO_ENABLED=0 go build \
    -ldflags="-s -w -X main.version=${VERSION}" \
    -o /bin/module \
    ./modules/<n>/cmd/server/

# Stage 2: runtime
FROM aegion/base-runtime:latest
COPY --from=build /bin/module /bin/module

# Standard module ports:
#   9001 → core
#   9002 → password
#   9003 → mfa
#   9004 → passkeys
#   9005 → magic_link
#   9006 → social
#   9007 → sso
#   9008 → oauth2
#   9009 → introspection
#   9010 → policy
#   9011 → proxy
#   9012 → admin
EXPOSE 9000

USER aegion
ENTRYPOINT ["/bin/module"]
```

CGo is disabled in module images (`CGO_ENABLED=0`) — modules are pure Go. Only the core image build enables CGo to link the Rust engines.

---

## Module standard port assignments

Each module listens on a fixed default port within the `aegion_modules` network. These are defaults — overridable via `AEGION_MODULE_LISTEN_ADDR` environment variable injected by core.

| Module | Default port |
|---|---|
| core (internal gRPC) | 9001 |
| password | 9002 |
| mfa | 9003 |
| passkeys | 9004 |
| magic_link | 9005 |
| social | 9006 |
| sso | 9007 |
| oauth2 | 9008 |
| introspection | 9009 |
| policy | 9010 |
| proxy | 9011 |
| admin | 9012 |

None of these ports are exposed on the host. Only core's public HTTP port (default 8080) is host-exposed.

---

## CI pipeline per module

Each module has an independent CI pipeline triggered by changes to its path:

```
on push:
  paths:
    - modules/mfa/**
    - internal/**         ← shared changes trigger all module pipelines

steps:
  - go vet ./modules/mfa/...
  - golangci-lint run ./modules/mfa/...
  - go test ./modules/mfa/...
  - docker build -f modules/mfa/Dockerfile -t aegion/aegion-mfa:$SHA .
  - docker push aegion/aegion-mfa:$SHA
```

A separate platform integration pipeline runs on every PR and on merges to main. It composes core + all modules at the current SHA and runs the full integration test suite.

A release pipeline tags all images at the same semantic version, validates the full compatibility matrix, and publishes `build/release-manifest.json`.
