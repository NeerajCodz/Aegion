# Aegion — Architecture

---

## The core decision: Go + Rust hybrid

Aegion is not a pure Go application, and it is not a pure Rust application. It is a **Go control plane with Rust performance engines**.

This hybrid is not a compromise — it is a deliberate assignment of each language to the problems it is genuinely better at.

---

## Go: the control plane and protocol layer

Go owns everything that is about *orchestration, networking, and application logic*.

```
┌─────────────────────────────────────────────────────────────────┐
│                        Go layer                                  │
│                                                                  │
│  HTTP server (chi)          Module system (build tags)          │
│  OAuth2 flows               Session management                  │
│  Self-service flows         Admin panel API                     │
│  Proxy pipeline             Database layer (sqlx + Postgres)    │
│  Background workers         Courier (email/SMS queue)           │
│  Policy orchestration       SAML / OIDC redirect flows          │
│                                                                  │
└─────────────────────────────────────────────────────────────────┘
```

Go is chosen for this layer because:

- **Concurrency model** maps directly to HTTP server patterns — goroutines per request, channels for worker pools
- **Fast development** — the surface area of the control plane is large; Go's simplicity keeps it maintainable
- **Easy deployment** — single static binary, minimal runtime dependencies
- **Excellent stdlib** — net/http, crypto, encoding/json, embed.FS are production-grade out of the box
- **The Ory stack is Go** — borrowing patterns from Kratos, Hydra, Keto, Oathkeeper has direct precedent

### Go ownership checklist

In Aegion, Go is expected to own:

- HTTP server and routing
- OAuth2 and self-service flow orchestration
- session lifecycle and session APIs
- admin panel API and runtime config endpoints
- proxy orchestration and forwarding logic
- database access and migration orchestration
- background workers and courier orchestration
- module system and platform composition logic
- policy orchestration (model dispatch, context shaping)

---

## Rust: the performance and security engines

Rust owns a small set of components where **memory safety, performance, and cryptographic correctness** are non-negotiable.

```
┌─────────────────────────────────────────────────────────────────┐
│                        Rust layer                                │
│                                                                  │
│  ┌─────────────────┐   ┌─────────────────┐   ┌───────────────┐ │
│  │  crypto engine  │   │   JWT engine    │   │ policy engine │ │
│  │                 │   │                 │   │               │ │
│  │ Argon2id        │   │ RS256 / ES256   │   │ ReBAC tuple   │ │
│  │ bcrypt / scrypt │   │ signature       │   │ graph         │ │
│  │ constant-time   │   │ verification    │   │ traversal     │ │
│  │ comparisons     │   │ JWKS key mgmt   │   │               │ │
│  └────────┬────────┘   └────────┬────────┘   └───────┬───────┘ │
│           │                    │                     │         │
└───────────┼────────────────────┼─────────────────────┼─────────┘
            │  FFI / CGo calls   │                     │
┌───────────┼────────────────────┼─────────────────────┼─────────┐
│           ▼                    ▼                     ▼         │
│                        Go control plane                         │
└─────────────────────────────────────────────────────────────────┘
```

### Rust ownership checklist

Rust should be used for narrow, critical engines:

- password hashing and secure comparisons (Argon2/bcrypt/scrypt primitives)
- JWT signing and verification hot paths
- high-cost ReBAC graph traversal/evaluation
- optional high-throughput proxy token validation helpers (JWT/session signature checks)

This keeps the architecture deliberate: Go runs the control plane, Rust accelerates and hardens critical inner loops.

### Why Rust for these three specifically

**1. Crypto engine (password hashing)**

Password hashing is the one operation where a GC pause or a compiler optimization is a security problem, not just a performance problem.

- Argon2id is memory-hard by design — it deliberately uses large memory buffers. A Go garbage collector interacting with large allocations during a hash operation introduces timing variance that can leak information.
- Rust's ownership model means the password bytes are zeroed and dropped deterministically at the end of scope — no lingering sensitive data waiting for GC.
- Constant-time comparison (for HMAC verification, for token equality checks) is guaranteed in Rust without `unsafe`. In Go you have to be very careful.
- Battle-tested Rust crypto libraries (`argon2`, `bcrypt`, `ring`) have independent security audits.

Go calls the Rust crypto engine via CGo bindings with a narrow interface: `hash_password(password, params) -> hash` and `verify_password(password, hash) -> bool`. Nothing else crosses the boundary.

**2. JWT engine (signing and verification)**

At high throughput, JWT signature verification is on the hot path for every authenticated request.

- RS256 signature verification in Rust (`ring` crate) is measurably faster than Go's `crypto/rsa` — not dramatically, but consistently, and with lower memory overhead per verification.
- More importantly: Rust makes it structurally impossible to accidentally use the wrong key, mix up signing vs verification operations, or call a function with uninitialized memory.
- Key material (the private key bytes) lives in Rust-allocated memory that is zeroed on drop, with no Go GC involved in its lifetime.

The JWT engine exposes a thin interface: `sign(claims, key_id) -> token` and `verify(token, jwks) -> claims`. Key rotation, JWKS serialization, and the `kid` header are all managed within the Rust layer.

**3. Policy engine (ReBAC graph traversal)**

RBAC and ABAC are fast — a few SQL queries and a CEL evaluation. ReBAC is potentially expensive.

A Zanzibar-style permission check can require traversing a graph of relation tuples many levels deep. For a system where `user:alice` is a member of `group:eng`, which is a viewer of `folder:designs`, which contains `document:spec`, answering "can Alice view spec?" requires four tuple lookups and three subject-set expansions.

At scale (thousands of concurrent permission checks, complex namespace hierarchies), this traversal benefits from:

- Rust's zero-cost abstractions for graph traversal algorithms
- No GC pauses during recursive tuple expansion
- Cache-friendly memory layout for the tuple store

The Go policy orchestrator calls the Rust engine with a check request: `(namespace, object, relation, subject)`. The Rust engine handles the expansion, cache lookup, and evaluation, and returns allow/deny with the evaluation path.

---

## Module system

Aegion uses Go build tags as its module system. This is not a runtime feature flag — it is selective compilation.

```
aegion.yaml  ──→  scripts/resolve-tags  ──→  go build -tags "..."  ──→  binary
```

A file that begins with `//go:build oauth2` does not exist in a binary built without that tag. The handler is not there, the route is not registered, the migration does not run, the table does not exist. This is a security property: you cannot call an API that was not compiled in, even if you craft a raw HTTP request.

The Rust engines are compiled into shared libraries and linked at build time via CGo. If the `crypto` Rust module is not needed (e.g. a deployment using bcrypt from Go's stdlib as a fallback), it can be excluded from the link step.

---

## Request lifecycle

```
Inbound HTTP request
        │
        ▼
┌───────────────────────────────────────────┐
│           chi middleware stack            │
│  - Request ID injection                   │
│  - CORS headers                           │
│  - Rate limiting (Redis or Postgres)      │
│  - CSRF token validation (mutations)      │
│  - Session resolution → attach to ctx    │
└───────────────┬───────────────────────────┘
                │
                ▼
┌───────────────────────────────────────────┐
│           Route handler (Go)              │
│                                           │
│  Self-service flow handlers               │
│  OAuth2 flow handlers                     │
│  Admin API handlers                       │
│  Proxy pipeline                           │
└──────┬──────────────┬─────────────────────┘
       │              │
       ▼              ▼
┌────────────┐   ┌─────────────────────────┐
│ Rust engine│   │   Postgres (sqlx)        │
│            │   │                         │
│ - hash pw  │   │ - read/write identity   │
│ - verify   │   │ - session lookup        │
│ - sign JWT │   │ - flow state            │
│ - check    │   │ - audit log write       │
│   policy   │   │ - courier queue         │
└────────────┘   └─────────────────────────┘
```

---

## Background workers

All background work runs as goroutines within the same process — no separate worker processes, no job queues beyond the courier table in Postgres.

| Worker | What it does | Schedule |
|---|---|---|
| Courier dispatcher | Polls `courier_messages` for queued messages, dispatches via SMTP/HTTP | Every 1s |
| Session cleanup | Deletes expired sessions and continuity containers | Every 5m |
| Flow cleanup | Deletes expired self-service flow records | Every 15m |
| Token cleanup | Marks expired OAuth2 tokens inactive | Every 10m |
| SAML metadata refresh | Re-fetches IdP metadata from configured URLs | Every 24h |
| Key rotation check | Triggers scheduled key rotation if due | Every 1h |

---

## Data stores

| Store | Role | Required |
|---|---|---|
| **Postgres** | Everything — identity, sessions, flows, tokens, config | Yes — hard dependency |
| **Redis** | Session cache, rate limit counters, token revocation bloom filter | No — degrades gracefully |
| **Kafka / Redis Streams** | Domain event streaming for external consumers | No — events module only |

---

## Admin SPA architecture

The admin panel is a React + TypeScript SPA compiled at build time and embedded into the Go binary via `//go:embed internal/admin/ui/dist`.

```
Browser  ←──── same-origin XHR ────→  /aegion/api/*  (Go handler)
                                              │
                                         permission
                                           check
                                          (Rust)
                                              │
                                          Postgres
```

- No CORS on admin API calls (same-origin)
- HttpOnly session cookie — not accessible to JavaScript
- X-CSRF-Token on every mutation — rotated after each successful write
- In dev mode (build tag: `dev`), Go proxies `/aegion` to a Vite dev server on port 5173

---

## Directory structure
refer to `project-structure.md` for the recommended monorepo layout, but at a high level:
