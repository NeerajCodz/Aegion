# Aegion — Modules

Every Aegion capability is a module.

In this architecture, each module is packaged as a Docker image, but you do **not** run Aegion as many public services. You run **main `aegion`** (super repo), and it resolves, fetches, and wires the required module images based on `aegion.yaml`.

The user experience stays simple:

- one compose entry for `aegion`
- one public entrypoint (proxy in the super repo)
- one config source (`aegion.yaml`)

---

## The model in one view

```
docker-compose (user)
    └── aegion (super repo image)   ← only public service users add
            │
            ├── reads aegion.yaml
            ├── resolves enabled modules
            ├── fetches needed module images
            ├── starts/wires internal module runtime
            └── exposes one ingress port through super-repo proxy
```

Even if `admin` and `aegion-cli` are separate images, they are still treated as Aegion modules in one platform topology, not as separate products users must manually stitch together.

---

## Module contract

Each module follows the same contract:

- Declared in `aegion.yaml` with `enabled: true|false`
- Built/published as its own image artifact
- Loaded by main `aegion` only when enabled
- Connected internally behind the Aegion proxy/runtime bus
- Hidden from public network unless explicitly exposed by super-repo policy

This keeps module boundaries clear for engineering while keeping deployment simple for operators.

---

## Module catalog

| Module | Build/resolve tag | Default | Purpose |
|---|---|---|---|
| `core` | `core` | always on | Identity core, sessions, flows, courier, key lifecycle |
| `password` | `password` | on | Password authentication |
| `mfa` | `mfa` | off | TOTP/SMS/WebAuthn second factor + backup codes |
| `passkeys` | `passkeys` | off | WebAuthn passwordless first factor |
| `magic_link` | `magic_link` | off | Email/SMS OTP and magic link flows |
| `social` | `social` | off | OAuth2/OIDC social login providers |
| `sso` | `sso` | off | Enterprise SAML SSO |
| `oauth2` | `oauth2` | off | OAuth2/OIDC authorization server |
| `introspection` | `introspect` | off | RFC 7662 token introspection |
| `policy` | `policy` | off | RBAC + ABAC + ReBAC engine |
| `proxy` | `proxy` | off | Identity-aware ingress and policy enforcement |
| `admin` | `admin` | on | Admin panel/UI + management APIs |
| `aegion-cli` | `cli` | off | Operator CLI tooling image |

> `core` is mandatory. Other modules are selected through `aegion.yaml`.

---

## How resolution works

At startup/build orchestration:

1. Main `aegion` reads `aegion.yaml`.
2. Enabled modules are resolved into a module set.
3. Matching module images are fetched (or verified locally).
4. Main `aegion` wires module endpoints internally.
5. Public traffic enters through the super-repo proxy only.

Conceptually:

```bash
# pseudo-flow
module_set = resolve(aegion.yaml)
fetch_images(module_set)
boot_runtime(module_set)
open_public_ingress(proxy)
```

The key rule: **modules are many artifacts, one platform surface.**

---

## Dependency rules

| Module | Depends on |
|---|---|
| `core` | — |
| `password` | `core` |
| `mfa` | `core`, `password` (for common AAL2 path) |
| `passkeys` | `core` |
| `magic_link` | `core`, courier capabilities |
| `social` | `core`, courier capabilities |
| `sso` | `core`, courier capabilities |
| `oauth2` | `core` |
| `introspection` | `oauth2` |
| `policy` | `core` |
| `proxy` | `core`, `policy` (recommended) |
| `admin` | `core`, `policy` |
| `aegion-cli` | `core` APIs reachable via super-repo ingress |

If a required dependency is not enabled, super-repo resolution should fail fast and report the missing dependency before runtime.

---

## Networking and ports

### What users should see

- One exposed Aegion ingress port (from proxy in super repo)
- Optional explicit exposure only when operator chooses

### What users should NOT manage

- Per-module public ports
- Per-module separate external routing
- Per-module independent compose services for normal usage

Internally, modules can communicate over private network contracts managed by main `aegion`.

---

## Admin and CLI as module images

`admin` and `aegion-cli` are separate Docker images at artifact level, but operationally they are still first-class Aegion modules:

- versioned with the platform
- selected through Aegion module resolution rules
- routed/authorized by main Aegion controls

This avoids drift and keeps UX consistent: users think in **Aegion capabilities**, not infrastructure sprawl.

---

## Example `aegion.yaml` intent

```yaml
password:
  enabled: true

mfa:
  enabled: true

oauth2:
  enabled: true

proxy:
  enabled: true

admin:
  enabled: true

aegion_cli:
  enabled: false
```

The resolver interprets this as: fetch and activate only these module images (plus `core`), keep everything else absent.

---

## Operator checklist

Before deployment:

1. Enable only required modules in `aegion.yaml`.
2. Ensure dependency pairs are satisfied (`introspection` → `oauth2`, etc.).
3. Expose only the super-repo proxy port publicly.
4. Keep module-to-module traffic private/internal.
5. Treat admin/cli as modules in the same release lifecycle.

---

## Summary

Aegion is modular by artifact, unified by operation:

- each module is a Docker image
- all modules are orchestrated by main `aegion`
- users integrate one super-repo service in compose
- proxy gives one clean public ingress

That is the intended architecture boundary.
