# Aegion — Proxy Specification

The proxy module makes Aegion an identity-aware ingress for your services.

It enforces auth at the edge so upstream services can focus on business logic.

---

## Purpose

Without proxy, every backend service must implement:

- token/session validation
- authorization checks
- header shaping
- auth failure responses

With Aegion proxy, those become centralized policy-driven controls.

---

## Pipeline

Each request is processed in order:

1. **Match** — find the applicable rule by path/method/priority
2. **Authenticate** — resolve subject identity
3. **Authorize** — evaluate access decision
4. **Mutate** — transform request (headers/tokens/context)
5. **Forward** — send to upstream

If any stage fails, request stops with explicit failure response.

---

## Match stage

Rule matching should support:

- path patterns
- HTTP methods
- host conditions
- priority ordering (first match wins)

A final deny/default rule is recommended to avoid accidental open routes.

---

## Authenticate stage

Common authenticators:

- `bearer_token` (JWT from `Authorization`)
- `cookie_session`
- `anonymous`
- `noop` (explicit bypass for public routes)

Multiple authenticators can be attempted in ordered fallback.

---

## Authorize stage

Common authorizers:

- `allow` / `deny`
- policy engine check (RBAC/ABAC/ReBAC)
- CEL expression evaluation
- remote decision webhook

Authorization should receive normalized subject/resource context and emit explicit reason codes.

---

## Mutate stage

Mutators can:

- inject identity headers (`X-User-ID`, `X-User-Email`, roles)
- issue short-lived id token for upstream compatibility
- call hydrator webhook for additional claims
- set/forward selected cookies

Mutation should be deterministic and auditable.

---

## Upstream contract

Upstream services should treat proxy-injected identity as trusted only when received from internal network boundaries.

Recommended controls:

- strip conflicting inbound identity headers at proxy edge
- add canonical prefixed headers only from proxy
- sign or verify internal hop metadata where needed

---

## Performance model (Go + Rust)

### Go handles

- request lifecycle orchestration
- route and rule dispatch
- middleware and forwarding behavior

### Rust can handle hot token/permission path

For high-throughput proxy workloads, Rust modules can optimize:

- JWT signature verification
- token validation primitives
- permission evaluation helpers

This keeps proxy control flow in Go while accelerating cryptographic and evaluation hot paths.

---

## Admin surface

Proxy admin should support:

- rule CRUD
- visual pipeline editor
- rule priority management
- dry-run/simulate request
- test panel for session/token + URL path

---

## Failure behavior

Fail responses should be explicit and configurable:

- 401 for authentication failure
- 403 for authorization failure
- safe error body for clients
- structured logs for operators

---

## Observability

Proxy should emit:

- matched rule id
- auth method used
- authorization decision path
- upstream target and latency
- status code and error reason

This is critical for debugging access incidents.

---

## Security posture

- deny-by-default policy baseline
- minimum required AAL per route where needed
- hard separation of public ingress and internal module traffic
- audit trail for rule changes and sensitive override actions

---

## Summary

Aegion proxy turns identity and authorization into an ingress capability, so teams enforce access consistently without rewriting auth logic in every service.
