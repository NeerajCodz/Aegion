# Aegion — End-to-End Product Specification

### The super auth project: everything Auth0, Ory, and Supabase Auth do, unified and self-hosted.

> One platform. One operator experience. Modular internals. Enterprise-ready controls.

---

## 1) Product definition

Aegion is a self-hosted identity and access platform that combines:

- modern authentication methods
- session and token infrastructure
- OAuth2/OIDC authorization server
- policy and proxy enforcement
- admin control plane with granular permissions
- enterprise identity lifecycle support (SAML + SCIM)

It is designed as a complete platform, not a single login feature.

---

## 2) Core deployment model

Aegion follows a super-repo/platform model:

- users add `aegion` to compose as the primary service
- `aegion.yaml` defines desired platform shape
- enabled modules are resolved and loaded
- ingress is controlled through one platform edge model

Even when modules are image-separated internally, user-facing operation remains one coherent platform experience.

---

## 3) Admin control plane (explicit model)

### Admin path

Admin UI is served at **`/aegion`** when the admin module is enabled.

### Bootstrap operator

The first admin is seeded from `operator` in `aegion.yaml` on first boot (when no operator identity exists).

- bootstrap operator signs into `/aegion`
- hardens initial system settings
- rotates bootstrap credentials
- transitions governance to team-based admin roles/capabilities

### Ongoing admin governance

Admin access uses Discord-style granular permission controls:

- role presets for efficient onboarding
- direct grants for specific capabilities
- direct denies as override layer for high-risk actions

This supports least-privilege operations at team scale.

---

## 4) Smart MVP scope

### Phase 1 — Core identity platform

- core identity
- password auth
- sessions
- email OTP
- magic link
- admin panel
- postgres
- identity schemas
- courier
- baseline security controls

### Phase 2 — OAuth2/OIDC

- clients
- token lifecycle
- PKCE
- JWT flows
- consent/login challenge model

### Phase 3 — Policy + proxy

- RBAC/ABAC/ReBAC policy
- identity-aware proxy enforcement pipeline

### Phase 4 — Enterprise

- SAML SSO
- SCIM provisioning
- enterprise operations and governance controls

---

## 5) Go + Rust architecture posture

### Go is the control plane and protocol layer

Go owns:

- HTTP/routing/middleware
- OAuth2/session/self-service flow orchestration
- admin and config APIs
- proxy orchestration
- database and worker orchestration
- module composition and runtime lifecycle

### Rust is for critical engines

Rust is used where safety/performance are critical:

- password hashing and constant-time primitives
- JWT sign/verify hot paths
- heavy ReBAC graph traversal/evaluation
- optional high-throughput proxy token validation helpers

The architecture is deliberate: broad orchestration in Go, critical inner loops in Rust.

---

## 6) Security coverage (modular matrix)

| Security feature | Primary modules | Covered docs |
|---|---|---|
| Password hashing (Argon2/bcrypt), HIBP, history, similarity | `password`, `security` | `security.md`, `architecture.md`, `modules.md` |
| Email/SMS OTP and magic link controls | `magic_link`, `mfa`, `courier` | `security.md`, `modules.md` |
| Passkeys + WebAuthn constraints (`rp.id`) | `passkeys`, `mfa` | `security.md`, `modules.md`, `config.md` |
| MFA (TOTP/SMS/WebAuthn2FA/backup/trusted devices) | `mfa` | `security.md`, `modules.md` |
| Session fixation and revocation controls | `core`, `security` | `security.md`, `admin.md` |
| OAuth2 PKCE + refresh-family invalidation | `oauth2` | `oauth.md`, `security.md`, `config.md` |
| JWT algorithm, issuer/audience validation, key rotation | `oauth2`, token engine | `oauth.md`, `security.md`, `architecture.md` |
| CSRF protection model | platform middleware | `security.md`, `architecture.md` |
| Rate limits + brute-force + enumeration mitigation | `security`, `cache`, `core` | `security.md`, `config.md` |
| CAPTCHA controls | `security` | `security.md`, `config.md` |
| IP allowlist/blocklist + suspicious login detection | `security`, `admin` | `security.md`, `admin.md` |
| Geographic access restrictions (geo-fencing) | `security` | `security.md`, `config.md` |
| Rate limit bypass for trusted IPs | `security`, `core` | `security.md`, `config.md` |
| Passwordless-only enforcement | `password`, `passkeys` | `security.md`, `config.md` |
| Field-level encryption at rest | crypto engine | `security.md`, `architecture.md`, `config.md` |
| Append-only audit trail + admin action trace | `core`, `admin` | `security.md`, `admin.md`, `aegion-db-schema.md` |
| Admin session hardening + re-auth gates + CSP | `admin`, `mfa` | `admin.md`, `config.md`, `security.md` |
| Proxy-stage authz enforcement | `proxy`, `policy` | `proxy.md`, `policy.md` |
| Webhook signature verification (token hooks) | `oauth2` | `oauth.md`, `config.md` |
| TLS certificate pinning (inter-module) | `core`, all modules | `inter-module-communication.md`, `config.md` |

This matrix is the completeness contract: security is documented by feature, module, and spec location.

---

## 7) Enterprise completeness

Enterprise scope includes:

- SAML IdP integration and domain routing
- SCIM 2.0 provisioning lifecycle (create/update/deprovision)
- admin governance and auditability
- policy-driven access and proxy enforcement

SCIM is treated as a first-class enterprise capability in admin and architecture documentation.

---

## 8) `aegion.yaml` and runtime ownership

`aegion.yaml` defines bootstrap/platform shape:

- module enablement
- server/database/secrets/operator bootstrap
- initial default behavior

Runtime-admin-managed domains include:

- clients/providers/rules/templates
- policy and proxy runtime data
- admin roles/capabilities/team assignments
- system flags after bootstrap

This separation avoids config drift while preserving operational agility.

---

## 9) Canonical documentation map

- `overview.md` — high-level narrative and positioning
- `architecture.md` — Go+Rust internals and runtime flow
- `modules.md` — module system and dependency model
- `security.md` — authentication and security controls
- `oauth.md` — OAuth2/OIDC server behavior
- `policy.md` — authorization models and evaluation strategy
- `proxy.md` — edge enforcement pipeline
- `admin.md` — admin control plane + enterprise operations
- `config.md` — `aegion.yaml` model and ownership split
- `timeline.md` — phased rollout and smart MVP scope
- `project-structure.md` — monorepo/super-repo structure guidance
- `aegion-db-schema.md` — persistence-level reference

---

## 10) End-to-end navigation

For fastest onboarding:

1. Read `overview.md` (product context).
2. Read `config.md` (bootstrap/operator/platform shape).
3. Read `admin.md` (control plane and governance model).
4. Read `security.md` + `oauth.md` (security and protocol behavior).
5. Read `policy.md` + `proxy.md` (authorization and edge enforcement).
6. Read `modules.md` + `architecture.md` (module and runtime internals).
7. Use `aegion-db-schema.md` for persistence-level implementation details.

---

## 11) Completion statement

This document is intentionally concise but complete at the product-spec level.

It defines:

- what Aegion is
- how it is operated
- how admin bootstrap and governance work
- how security features are covered modularly
- where every deep detail lives in the focused docs

---

## Appendix A) Docs-to-code gap matrix (current repo snapshot)

This matrix maps major requirements from `modules.md`, `inter-module-communication.md`, `observability.md`, `policy.md`, `timeline.md`, `project-structure.md`, and this product spec to concrete code.

| Requirement source | Major capability | Primary code evidence | Status | Notes |
|---|---|---|---|---|
| `modules.md`, `timeline.md` | Core/module lifecycle orchestration | `core/orchestrator/{orchestrator.go,docker.go,network.go}`, `cmd/aegion/server.go` | partial | Start/stop/restart wiring exists; full enabled-module graph composition at boot is still incomplete. |
| `modules.md` | Pre-pull dependency/version validation contract | `core/orchestrator/config.go` | missing | Current validation is mostly per-module field checks, not the documented dependency/compatibility pipeline. |
| `modules.md`, `project-structure.md` | Module process contract (`/health`, `/ready`, `/meta`) | `internal/platform/moduleserver/server.go`, `modules/*/cmd/server/main.go` | partial | Contract runner exists for scaffold modules; legacy modules still use mixed server patterns and no shared self-registration hook. |
| `inter-module-communication.md` | Service registry + health monitoring | `core/registry/{registry.go,health.go,discovery.go}`, `cmd/aegion/routes.go` (`/internal/registry/*`) | partial | Registry/heartbeat/health checks are implemented, but the runtime path is HTTP-centric vs the documented gRPC registry flow. |
| `inter-module-communication.md` | gRPC inter-module runtime with internal token metadata | `internal/proto/*_grpc.pb.go`, `modules/policy/grpc/server.go`, `modules/mfa/grpc/server.go` | missing | Proto and adapter code exist, but runtime listeners/client call paths are not started from `cmd/aegion` or scaffold module mains. |
| `inter-module-communication.md` | Async event bus with retry/dead-letter semantics | `core/eventbus/eventbus.go`, `core/workers/event_processor.go` | partial | Durable delivery, retry, and dead-letter behavior exist; production subscriber wiring in main runtime remains limited. |
| `inter-module-communication.md`, `timeline.md` | Shared Postgres + module migration ownership | `cmd/aegion/main.go`, `cmd/aegion/migrations/*.sql`, `modules/{password,magic_link,oauth2,policy}/migrations` | partial | Core migrates embedded schema set (core + policy); per-enabled-module migration orchestration is incomplete. |
| `timeline.md` | Self-service flow + session APIs | `cmd/aegion/routes.go`, `core/flows/service.go`, `core/session/{session.go,context.go}` | partial | Init/get/submit + whoami/logout are live; submit currently validates CSRF then marks flow complete without method-specific auth execution. |
| `timeline.md`, `modules.md` | Password + magic-link authentication capability | `modules/password/{service,handler,store}.go`, `modules/magic_link/{service,handler,store}.go`, `cmd/aegion/server.go` | partial | Module logic is substantial, but core runtime wiring does not yet route self-service method execution through these handlers. |
| `timeline.md` | OAuth2/OIDC module coverage | `modules/oauth2/{cmd/server/main.go,handler/handler.go,service/*,store/*}` | partial | Auth code/refresh/client credentials/discovery/userinfo/introspection and device-code token issuance are implemented; remaining work is cross-module wiring depth and advanced claim/provider features. |
| `policy.md`, `timeline.md` | Policy engine precedence + proxy authorization checks | `modules/policy/grpc/server.go`, `cmd/aegion/server.go`, `cmd/aegion/routes.go` (`handleModuleProxy`) | implemented | RBAC/ABAC/ReBAC default-deny evaluation and fail-safe proxy enforcement are active in the in-process checker path. |
| `aegion-project.md`, `policy.md` | Runtime ownership for policy/proxy settings | `cmd/aegion/routes.go` (`handleAdminGetConfig`, `handleAdminUpdateConfig`, `loadRuntime*Settings`) | implemented | Bootstrap defaults plus runtime DB-backed overrides are implemented via `core_system_config`. |
| `observability.md` | OTEL provider + correlation pipeline | `internal/platform/observability/*`, `core/router/middleware.go`, `internal/platform/logger/logger.go`, `cmd/aegion/main.go` | partial | Instrumentation primitives exist, but startup does not initialize an OTEL provider/collector pipeline. |
| `timeline.md`, `aegion-project.md` | Enterprise SSO + SCIM | `modules/sso/{service,handler,store}.go`, `modules/admin/scim/*.go`, `modules/admin/cmd/admin/server.go` | partial | SCIM service/handlers exist but are not mounted in admin server routing; SSO module remains scaffold-level. |
| `project-structure.md`, `aegion-project.md` | Go+Rust critical-engine posture | `rust/{crypto,jwt}`, `internal/platform/{crypto,jwt}` | partial | Rust crypto/JWT crates and bindings exist; policy/proxy Rust engines and broad runtime adoption remain incomplete. |
| `project-structure.md` | Roadmap module depth (`mfa`, `passkeys`, `social`, `introspection`, `proxy`, `cli`) | `modules/{mfa,passkeys,social,introspection,proxy,cli}/*` | partial | `passkeys` and `social` now include concrete HTTP service flows (challenge/state lifecycle, provider/profile handling, cryptographic assertion verification, and sign-counter replay protection); full FIDO2 attestation depth and deeper runtime integration remain in progress. |
