# Aegion — Inter-Module Communication

This document defines how Aegion modules talk to each other, how core coordinates them, and how shared state is managed across a distributed module graph.

---

## Communication architecture overview

Modules in Aegion communicate through three complementary mechanisms, each chosen for a specific type of interaction:

```
┌────────────────────────────────────────────────────────────────────┐
│                      aegion_modules network                         │
│                                                                      │
│  ┌──────────┐   gRPC (sync)    ┌──────────┐                        │
│  │  oauth2  │ ─────────────── ▶│  policy  │  → authorization check │
│  └──────────┘                  └──────────┘                        │
│                                                                      │
│  ┌──────────┐  internal event  ┌──────────┐                        │
│  │   core   │ ───────────────▶ │   mfa    │  → session.created     │
│  └──────────┘    bus (async)   └──────────┘                        │
│                                                                      │
│  ┌──────────┐   Postgres       ┌──────────┐                        │
│  │  oauth2  │ ────────────── ▶ │   sso    │  → shared identity     │
│  └──────────┘   shared store   └──────────┘    records             │
│                                                                      │
└────────────────────────────────────────────────────────────────────┘
```

| Mechanism | Type | Used for |
|---|---|---|
| **gRPC over internal network** | Synchronous | Real-time cross-module calls that need a response in the same request path |
| **Internal event bus** | Asynchronous | State changes that other modules should react to, without blocking the caller |
| **Shared Postgres** | Persistent state | Identity records, sessions, flows — canonical data any module can read |

---

## Mechanism 1 — gRPC (synchronous inter-module calls)

When module A needs a response from module B as part of handling a single request, it calls module B via gRPC on the internal network.

### Why gRPC and not internal HTTP

- **Typed contracts**: Proto definitions are the source of truth for the interface. A module cannot call another with a malformed request — the compiler and generated clients prevent it.
- **Performance**: Binary wire format over HTTP/2 multiplexing is faster than JSON/HTTP/1.1, relevant for high-throughput hot paths like policy checks on every proxied request.
- **Bidirectional streaming**: Useful for long-running operations like bulk identity sync from SCIM.
- **Built-in deadline propagation**: gRPC deadlines flow through multi-hop calls automatically — if the outer request times out, all downstream module calls timeout and cancel, preventing resource leaks.

### Call addressing

Core's service registry holds the current address of every module instance. Before making a gRPC call, a module queries the registry:

```
caller module → GET /internal/registry/module/policy → { "addresses": ["aegion-policy:9005", "aegion-policy:9006"] }
caller module → gRPC call to one of the addresses (round-robin or least-connections)
```

For scaled modules (multiple replicas), the service registry returns all healthy instance addresses. The calling module's gRPC client load-balances across them.

### Internal auth on gRPC calls

Every gRPC call carries the internal auth token injected by core at startup, in the gRPC metadata header `x-aegion-internal-token`. Core rotates this token on a configurable interval (default 1h). Module gRPC servers reject calls with missing or invalid tokens with `UNAUTHENTICATED`.

This prevents a compromised container on the same network from impersonating a legitimate module.

### Key gRPC call paths

#### Policy check (oauth2 / proxy → policy)

Every authorization decision in the proxy pipeline and every OAuth2 consent scope enforcement calls the policy module synchronously:

```protobuf
service PolicyEngine {
  rpc Check(CheckRequest) returns (CheckResponse);
  rpc BatchCheck(BatchCheckRequest) returns (BatchCheckResponse);
}

message CheckRequest {
  string subject   = 1;  // "user:alice"
  string resource  = 2;  // "document:spec-123"
  string action    = 3;  // "read"
  Context context  = 4;  // { ip, time, tenant, ... }
}

message CheckResponse {
  bool   allowed       = 1;
  string model_used    = 2;  // "rbac" | "abac" | "rebac"
  string deny_reason   = 3;
  repeated string eval_path = 4;  // trace for audit/debug
}
```

Batch checks are used when the proxy pipeline evaluates multiple resources in parallel.

#### MFA status check (core / oauth2 → mfa)

When core resolves a session's AAL or when oauth2 is determining whether a consent flow requires step-up auth, it calls mfa synchronously:

```protobuf
service MFAEngine {
  rpc GetStatus(MFAStatusRequest) returns (MFAStatusResponse);
  rpc GetEnrolledFactors(FactorListRequest) returns (FactorListResponse);
}

message MFAStatusResponse {
  bool   mfa_enrolled   = 1;
  string highest_aal    = 2;  // "aal1" | "aal2"
  repeated string enrolled_methods = 3;
}
```

#### Token introspection (introspection → oauth2)

The introspection module calls oauth2 to validate token state for tokens not resolvable from local cache:

```protobuf
service TokenStore {
  rpc Introspect(IntrospectRequest) returns (IntrospectResponse);
  rpc Revoke(RevokeRequest) returns (RevokeResponse);
}
```

#### Courier dispatch (core → core courier, sso/social/magic_link → core)

All modules that need to send email or SMS call core's courier interface. The courier is owned by core — modules never manage delivery directly:

```protobuf
service Courier {
  rpc Enqueue(CourierMessage) returns (EnqueueResponse);
}

message CourierMessage {
  string  recipient_address = 1;
  string  channel           = 2;  // "email" | "sms"
  string  template_id       = 3;
  bytes   template_data     = 4;  // JSON
  string  idempotency_key   = 5;
}
```

---

## Mechanism 2 — internal event bus (asynchronous)

State changes that do not need to block the caller, but that other modules should react to, are published to the internal event bus.

### Event bus topology

The internal event bus runs inside core as a lightweight pub/sub broker. It does not require Kafka or Redis — it is in-memory by default, with durable delivery backed by a Postgres events table for cross-instance delivery when modules are scaled.

```
publisher module
      │
      ▼
core event broker (Postgres-backed when replicated)
      │
      ├── subscriber: mfa       (session.created → check if step-up needed)
      ├── subscriber: oauth2    (session.revoked → invalidate tokens)
      ├── subscriber: proxy     (identity.updated → invalidate cached headers)
      ├── subscriber: admin     (all events → audit log write)
      └── subscriber: sso       (identity.deprovisioned → SCIM callback)
```

### Delivery guarantee

- **At-least-once**: events are written to Postgres before the publisher returns. If a subscriber crashes before acknowledging, the event is redelivered on restart.
- **Ordered per-entity**: events for the same identity ID or session ID are delivered in order to each subscriber. Events for different entities may be interleaved.
- **Acknowledgement**: subscribers acknowledge events after processing. Unacknowledged events are retried with exponential backoff (max 5 retries, then dead-letter).

### Canonical event schema

```json
{
  "event_id":      "01J...",
  "event_type":    "session.created",
  "source_module": "core",
  "occurred_at":   "2026-03-25T14:22:01Z",
  "entity_type":   "session",
  "entity_id":     "sess_abc123",
  "identity_id":   "id_xyz789",
  "payload": {
    "aal":         "aal1",
    "auth_method": "password",
    "ip":          "203.0.113.10",
    "user_agent":  "Mozilla/5.0..."
  },
  "metadata": {
    "request_id":  "req_def456",
    "tenant_id":   "tenant_ghi"
  }
}
```

### Event catalog

| Event type | Publisher | Key subscribers | Purpose |
|---|---|---|---|
| `identity.created` | core | admin, sso | New identity created in any flow |
| `identity.updated` | core | admin, proxy, sso | Traits, schema, or credentials changed |
| `identity.suspended` | core, admin | oauth2, proxy | Revoke all active tokens and sessions |
| `identity.deleted` | core, admin | oauth2, mfa, passkeys | Cleanup all associated data |
| `session.created` | core | mfa, oauth2, admin | New session issued after authentication |
| `session.revoked` | core, admin | oauth2, proxy | Session explicitly revoked |
| `session.expired` | core cleanup worker | oauth2, proxy | Session reached its TTL |
| `mfa.enrolled` | mfa | core, admin | First factor enrolled on identity |
| `mfa.challenged` | mfa | core | MFA step-up required for current session |
| `mfa.verified` | mfa | core | MFA step-up completed — upgrade session AAL |
| `password.changed` | password | core, admin, courier | Password changed via settings or recovery |
| `password.reset_requested` | password | courier | Recovery flow initiated |
| `oauth2.token_issued` | oauth2 | admin, introspection | Access or refresh token created |
| `oauth2.token_revoked` | oauth2, admin | introspection, proxy | Token explicitly revoked |
| `oauth2.consent_accepted` | oauth2 | admin | Consent granted for client+scope pair |
| `policy.rule_changed` | policy, admin | proxy | RBAC/ABAC/ReBAC rule mutated |
| `proxy.rule_changed` | proxy, admin | proxy instances | Proxy access rule updated |
| `sso.connection_activated` | sso, admin | core, courier | SAML IdP connection enabled |
| `scim.user_provisioned` | sso | core | SCIM provisioned a new identity |
| `scim.user_deprovisioned` | sso | core, oauth2, mfa | SCIM deprovisioned an identity |
| `admin.action` | admin | core audit | Any admin mutation — full before/after |
| `key.rotated` | core | oauth2, mfa | Signing or encryption key rotated |

---

## Mechanism 3 — shared Postgres (canonical state)

All modules read and write canonical state through a shared Postgres instance. Modules do not maintain their own separate databases — they use namespaced tables within the single Aegion database.

### Table ownership model

Each module owns its own tables. Cross-module reads are permitted via the shared connection pool. Cross-module writes are not permitted — a module never writes to another module's tables directly.

```
┌──────────────────────────────────────────────────────────────────┐
│  Shared Postgres                                                  │
│                                                                   │
│  core tables:                                                     │
│    identities, credentials, addresses, sessions,                 │
│    continuity_containers, flows, courier_messages, audit_events  │
│                                                                   │
│  password tables:   password_credentials, password_history       │
│  mfa tables:        mfa_credentials, totp_devices, backup_codes  │
│  passkeys tables:   webauthn_credentials                         │
│  magic_link tables: otp_codes, magic_link_tokens                 │
│  social tables:     social_connections, social_state             │
│  sso tables:        saml_providers, saml_sessions, scim_tokens   │
│  oauth2 tables:     clients, auth_codes, access_tokens,          │
│                     refresh_tokens, consent_sessions             │
│  policy tables:     roles, permissions, assignments, rules,      │
│                     relation_tuples, namespaces                  │
│  proxy tables:      access_rules, mutator_config                 │
│  admin tables:      admin_identities, admin_roles, capabilities  │
└──────────────────────────────────────────────────────────────────┘
```

### Cross-module read pattern

When module A needs to read data owned by module B, it either:

1. **Reads directly** from module B's tables via the shared connection pool (read-only access granted via Postgres row-level permissions per module role)
2. **Calls module B via gRPC** if the data requires business logic to prepare (e.g., policy check, MFA status computation)

Direct cross-module reads are preferred for simple lookups. gRPC is used when the data requires the owning module's logic to be correct.

### Migration ownership

Each module ships its own migration files. Core runs all pending migrations on startup in dependency order — core's migrations run first, then each enabled module's migrations in the order they were enabled.

Migration files are embedded in each module image and are never applied out of order. A module's migrations are namespaced with its own prefix to prevent collision:

```
0001_core_identities.sql
0002_core_sessions.sql
...
0100_password_credentials.sql
0101_password_history.sql
...
0200_mfa_totp_devices.sql
```

If a migration for a disabled module exists from a previous enable, it is left in place — the tables remain but the module does not run. Re-enabling the module does not re-run already-applied migrations.

---

## Core as service coordinator

Core plays a role beyond just routing HTTP traffic — it is the coordination layer for the entire module graph.

### Service registry

Core maintains an in-memory service registry of all running module instances:

```json
{
  "modules": {
    "mfa": {
      "instances": [
        { "address": "aegion-mfa-1:9002", "healthy": true, "registered_at": "..." },
        { "address": "aegion-mfa-2:9002", "healthy": true, "registered_at": "..." }
      ],
      "version":      "1.4.2",
      "capabilities": ["totp", "webauthn", "backup_codes"]
    },
    "oauth2": {
      "instances": [
        { "address": "aegion-oauth2-1:9003", "healthy": true, "registered_at": "..." }
      ]
    }
  }
}
```

The registry is authoritative. A module is not considered available until it is registered and healthy. Core exposes the registry state via the admin API and in the admin dashboard under Platform → Modules.

### Health monitoring

Core health-checks every registered module instance on a configurable interval (default 5s). Consecutive failures (default 3) result in:

1. Instance removed from active routing
2. `module.health_failed` event emitted to all subscribers
3. Admin dashboard alert
4. Core attempts to restart the module container (if it has container restart authority)
5. If all instances of a module fail: core returns 503 for requests that require that module, with a clear error body naming the unavailable capability

### Session context propagation

When core forwards a request to a module, it injects the resolved session context as a signed header:

```
X-Aegion-Session-Id:      sess_abc123
X-Aegion-Identity-Id:     id_xyz789
X-Aegion-Identity-AAL:    aal2
X-Aegion-Session-Token:   <HMAC-signed envelope>
```

The HMAC signature uses the internal auth token as the key. Modules verify the signature before trusting the injected context. This prevents a compromised upstream from impersonating an identity.

---

## Request flow through the module graph

A complete example: user submits login with password + TOTP.

```
1.  Browser POST /self-service/login/methods/password
         │
         ▼
2.  Core ingress
    - assigns request ID
    - applies rate limiting
    - resolves CSRF token
         │
         ▼
3.  Core routes to password module
    - password validates credential against Postgres
    - password emits: credential_checked (internal, not full event)
    - password returns: identity_id, credential_valid=true
         │
         ▼
4.  Core checks MFA requirement
    - gRPC call to mfa: GetStatus(identity_id)
    - mfa returns: mfa_enrolled=true, enrolled_methods=["totp"]
         │
         ▼
5.  Core determines: AAL2 required, TOTP step needed
    - creates/updates flow state in Postgres (core tables)
    - returns 200 with flow state → browser shows TOTP prompt
         │
6.  Browser POST /self-service/login/methods/totp
         │
         ▼
7.  Core routes to mfa module
    - mfa validates TOTP code
    - mfa returns: valid=true, factor_type=totp
         │
         ▼
8.  Core creates session (core tables)
    - session.created event emitted to event bus
    - oauth2 subscriber: notes session in token issuance cache
    - admin subscriber: writes audit log entry
         │
         ▼
9.  Core returns session cookie + 200
    - browser receives authenticated session
```

At no point does the browser know that `password` and `mfa` are separate containers. The entire flow appears as a single coherent API from the client's perspective.

---

## Module isolation and failure behavior

### Module crash isolation

If `aegion-mfa` crashes:

- Core detects failure via health check (within 5s by default)
- Requests requiring MFA return 503 with `X-Aegion-Unavailable-Module: mfa`
- Requests NOT requiring MFA continue normally — password auth, session validation, OAuth2 all unaffected
- Core attempts container restart
- On recovery, module re-registers and routing resumes

### Partial degradation surface

| Module failure | Impact | Services unaffected |
|---|---|---|
| `mfa` down | MFA enrollment/verification unavailable | Login (aal1), sessions, OAuth2 |
| `oauth2` down | Token issuance unavailable | Session auth, self-service flows |
| `policy` down | Authorization checks fail (deny-safe) | Authentication flows |
| `proxy` down | Proxy forwarding unavailable | Auth API, admin |
| `admin` down | Admin UI unavailable | All auth flows |
| `sso` down | SAML login unavailable | All other login methods |

Core is the only single point of failure. Core should be run with multiple replicas in production, coordinated via Postgres and Redis.

---

## Security model for inter-module communication

- **Network isolation**: All module-to-module traffic stays on `aegion_modules` — never touches the public network
- **Mutual auth**: All gRPC calls carry the internal auth token; all module servers validate it
- **Token rotation**: Internal auth tokens rotate hourly; core pushes new tokens to all registered modules ahead of rotation
- **No module-to-module direct trust**: Modules do not trust each other — they trust core's injected session context only
- **Least-privilege Postgres roles**: Each module connects to Postgres with its own role, granted only the permissions it needs (SELECT on foreign tables, full access on its own tables)
- **No secret sharing between modules**: Cipher keys and cookie secrets live only in core; modules request cryptographic operations from core via gRPC rather than receiving raw key material

---

## Summary

Aegion's inter-module communication model is:

- **gRPC** for synchronous, typed, latency-sensitive calls between modules
- **internal event bus** for asynchronous state propagation and audit coverage
- **shared Postgres** for canonical persistent state with clear ownership boundaries
- **core as coordinator** — service registry, health monitoring, session context injection, and internal auth

This keeps module boundaries clean while enabling the coherent, single-platform experience that operators and users see.
