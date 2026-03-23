# Aegion — Configuration Specification (`aegion.yaml`)

`aegion.yaml` is the platform contract for Aegion shape and bootstrap behavior.

It defines what is compiled/loaded and what is seeded at first boot.

---

## Core principle: build-time vs runtime

This distinction is mandatory:

- **Build-time config**: module `enabled` flags that control platform shape
- **Runtime config**: operational values managed through admin/API after bootstrap

If you change build-time module enablement, the platform shape changes.
If you change runtime values, behavior changes without changing product shape.

---

## Role of `aegion.yaml`

`aegion.yaml` should include:

- server and network bootstrap settings
- database connection and migration bootstrap settings
- secret/key bootstrap settings
- module enable/disable declarations
- initial defaults for module behavior

After first boot, many operational values are expected to be managed in runtime config storage.

---

## Super-repo resolution model

In the module-image architecture:

1. Main `aegion` reads `aegion.yaml`.
2. Resolver builds enabled module set.
3. Required module images are fetched/verified.
4. Runtime graph is started with dependencies satisfied.

`aegion.yaml` is therefore the desired-state declaration for platform composition.

---

## Required sections (conceptual)

### Platform bootstrap

- `server`
- `database`
- `secrets`
- `operator`

`operator` is the bootstrap admin identity source.

- On first boot, if no operator admin exists, Aegion seeds the initial admin identity from `operator`.
- That identity signs into `/aegion` and performs first-time setup.
- After bootstrap, admin team lifecycle should move to role/capability management in the admin control plane.

### Module blocks

Examples:

- `password`
- `mfa`
- `passkeys`
- `magic_link`
- `social`
- `sso`
- `oauth2`
- `introspection`
- `policy`
- `proxy`
- `admin`
- optional `aegion_cli`

Each module block should contain `enabled: true|false`.

---

## Dependency validation

Resolver must enforce dependency constraints, such as:

- `introspection` requires `oauth2`
- `admin` requires policy capabilities
- `proxy` should validate required auth/policy support

Startup should fail fast on invalid combinations with clear errors.

---

## Security-sensitive fields

`aegion.yaml` contains high-sensitivity values:

- cookie/cipher secrets
- operator bootstrap credentials
- signing and encryption keys
- integration credentials (if present)

Operational guidance:

- never commit real secrets to source control
- rotate secrets with documented key rollover model
- keep bootstrap credentials one-time and replace immediately
- do not use bootstrap operator credentials as long-term shared team access

---

## Runtime-managed domains

These should be managed at runtime (admin/API), not continuously edited in yaml:

- OAuth2 clients
- social provider instances
- SAML provider metadata
- policy namespaces/rules/tuples
- proxy access rules
- templates and hooks
- most system flags after bootstrap
- admin roles, admin permissions, and admin team assignments

This keeps deployment stable while preserving operational agility.

---

## Example intent snippet

```yaml
password:
  enabled: true

magic_link:
  enabled: true

oauth2:
  enabled: false

policy:
  enabled: false

proxy:
  enabled: false

admin:
  enabled: true
```

This describes a strong Phase-1 style deployment with admin UI and modern login, without OAuth2/policy/proxy yet.

---

## Anti-patterns

- Treating module enable flags as runtime toggles
- Exposing per-module public ports by default
- Storing long-lived production secrets in plain committed yaml
- Using yaml as the only mutable source after runtime management is available

---

## Summary

`aegion.yaml` is not just a config file. It is the declaration of Aegion platform shape, bootstrap state, and module composition contract.
