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

| Capability area | Primary code evidence | Status | Notes |
|---|---|---|---|
| Module process contract (`/health`, `/ready`, `/meta`) | `internal/platform/moduleserver/server.go`, `modules/*/cmd/server/main.go` (scaffolded modules) | **Partial** | Contract runner exists and is used by new scaffold modules; legacy modules still use mixed server patterns. |
| Core module lifecycle orchestration | `core/orchestrator/*`, `cmd/aegion/server.go` (start/stop wiring), `cmd/aegion/routes.go` (admin restart) | **Partial** | Orchestrator lifecycle is wired and restart endpoint exists; full dependency-driven runtime composition is still incomplete. |
| Service registry and module health authority | `core/registry/*`, `cmd/aegion/routes.go` (`/internal/registry/*`) | **Implemented** | Register/deregister/list/heartbeat paths are active with tests. |
| Self-service flow lifecycle and session endpoints | `core/flows/*`, `core/session/*`, `cmd/aegion/routes.go` (init/get/submit/logout/whoami/jwks) | **Partial** | Core flow/session paths are implemented, but deeper method-specific UX/business logic remains uneven across flows. |
| Admin control plane + bootstrap operator | `cmd/aegion/routes.go` (admin handlers), `cmd/aegion/server.go` (`bootstrapAdmin`), `modules/admin/cmd/admin/main.go` | **Implemented** | CRUD-style admin/system surfaces and bootstrap/migration wiring are now concrete. |
| OAuth2/OIDC server hardening | `modules/oauth2/handler/*`, `modules/oauth2/service/*`, `modules/oauth2/grpc/*` | **Implemented** | Validation, grant/consent/scope checks, token/revocation hardening, and tests are in place. |
| Policy + proxy fail-safe enforcement | `modules/policy/grpc/server.go`, `core/router/proxy.go`, `cmd/aegion/routes.go` (runtime policy config) | **Implemented** | Default-deny/fail-closed behavior and strict runtime config validation are enforced and tested. |
| Observability standards rollout | `internal/platform/observability/*`, `internal/platform/logger/logger.go`, `core/router/middleware.go`, `modules/admin/security/middleware.go`, `modules/magic_link/store/store.go` | **Implemented** | Route/method/db low-cardinality standards and trace/request-id correlation are applied across core/module paths. |
| Inter-module protobuf contracts | `proto/policy/v1/policy.proto`, `proto/mfa/mfa.proto`, `proto/oauth2/tokens.proto`, `internal/proto/*` | **Partial** | Contract coverage has expanded beyond policy, but many module-to-module APIs are still undocumented in proto form. |
| CI/release discipline and consistency gates | `.github/workflows/ci.yml`, `.github/workflows/release.yml`, `scripts/check-proto-consistency.sh`, `scripts/check-release-manifest.sh`, `scripts/run-integration-smoke.sh`, `build/release-manifest.json` | **Implemented** | CI now enforces proto/release-manifest consistency and module/integration quality gates. |
| Enterprise SSO + SCIM completeness | `modules/sso/*` (scaffold), `modules/admin/scim/*` | **Partial** | SCIM groundwork exists, but end-to-end SSO/enterprise lifecycle completeness is not yet fully realized. |
| Newly scaffolded roadmap modules (`passkeys`, `social`, `introspection`, `proxy`, `cli`) | `modules/passkeys/*`, `modules/social/*`, `modules/introspection/*`, `modules/proxy/*`, `modules/cli/*` | **Missing** | Runtime skeletons and migrations exist, but production business logic/integration depth is still pending. |
