# Aegion — Project Structure (Monorepo / Super-Repo)

This document defines the recommended structure for Aegion as a high-efficiency super auth monorepo.

Focus:

- clean module boundaries
- high developer velocity
- safe ownership separation
- efficient build and release flow

---

## Architecture stance

Aegion should be organized as a **super-repo** with module-centric boundaries and shared platform primitives.

Principle:

- shared platform contracts at root
- capability modules isolated by domain
- single orchestration layer controlling composition

---

## Recommended top-level layout

```text
aegion/
├── docs/
├── cmd/
├── internal/
│   ├── core/
│   ├── password/
│   ├── mfa/
│   ├── passkeys/
│   ├── magic_link/
│   ├── social/
│   ├── sso/
│   ├── oauth2/
│   ├── introspection/
│   ├── policy/
│   ├── proxy/
│   ├── admin/
│   └── platform/
├── rust/
│   ├── crypto/
│   ├── jwt/
│   ├── policy/
│   └── proxy/
├── scripts/
├── configs/
├── build/
├── deploy/
└── aegion.yaml
```

---

## Boundary rules

### Core boundary

`internal/core` provides foundational contracts:

- identity primitives
- session primitives
- flow framework
- courier interfaces
- storage abstractions

Other modules depend on core contracts, not each other’s internals.

### Module boundary

Each module should contain:

- transport handlers
- service logic
- storage adapters
- module-specific migrations
- module registration hooks

Avoid cross-module private imports except through approved platform contracts.

### Platform boundary

`internal/platform` should hold:

- module resolver/orchestrator
- dependency validation
- bootstrap lifecycle
- shared runtime wiring

---

## Monorepo efficiency patterns

### 1) Shared primitives, isolated capabilities

- keep common libs minimal and stable
- move business logic into module-local packages

### 2) Build graph awareness

- module-level builds/tests
- incremental CI where possible
- fail fast on contract breaks

### 3) Contract-first integration

- explicit interfaces between control plane and engines
- versioned schema/migration contracts
- stable API envelopes for admin/proxy/policy hooks

### 4) Ownership clarity

- codeowners per module
- clear reviewer map by domain
- platform-level review required for cross-boundary changes

---

## Go + Rust placement

### Go side (control plane)

- HTTP server and routing
- OAuth2 and session flows
- admin API surface
- proxy orchestration
- worker orchestration
- module composition and runtime wiring

### Rust side (critical engines)

- cryptographic hot path operations
- JWT sign/verify engine
- heavy ReBAC traversal engine
- optional high-throughput proxy validation primitives

Keep the FFI boundary narrow, explicit, and well-tested.

---

## Super-repo runtime alignment

Even with module images:

- runtime remains one platform topology
- user-facing deployment remains simple
- resolver uses `aegion.yaml` to choose enabled capabilities

Repository structure should mirror this operational model.

---

## CI/CD recommendations

- lint/test at module level + integration suite
- contract tests for module boundaries
- migration validation per module
- release manifests generated from enabled module set

---

## Documentation alignment

Docs should map 1:1 to architecture domains:

- architecture, modules, config, policy, proxy, admin, oauth, security, timeline

No single monolithic “everything doc” as primary reference.

---

## Summary

The best Aegion structure is a super-repo monorepo with strict module boundaries, shared platform contracts, and a Go-control-plane + Rust-engines split for performance and security-critical paths.
