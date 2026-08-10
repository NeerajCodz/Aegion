# Aegion modules

The core process is the public gateway and control plane. It owns configuration
validation, core and enabled-module migration ordering, the mTLS registry
listener, protected gateway routes, and its embedded capabilities. Deployment
manifests own the lifecycle of external module workloads.

`core` does not pull images, create Docker networks, start containers, or
inject runtime configuration into module processes. Docker Compose and Helm
start, restart, scale, and stop external modules.

The machine-checked maturity source is
[`build/release-maturity.json`](../build/release-maturity.json); its rendered
summary is [the release maturity matrix](release-maturity-matrix.md).

## Deployment model

```text
Internet
   │
   ▼
core public gateway (the only published module ingress)
   │ mTLS registry + signed internal identity context
   ▼
private module services
   ├── admin, analytics, introspection, mfa, oauth2, passkeys
   ├── proxy, social, sso
   └── cli (on-demand operator job; no HTTP listener)
```

External module services are private to the deployment network. Compose and
Helm use service DNS names for their advertised HTTP and gRPC endpoints. Core
forwards only the configured, statically owned public routes after the module
has registered a healthy endpoint; it returns an unavailable response instead
of forwarding to an unregistered or unhealthy instance.

## Module catalog

| Module | Mode | Deployment ownership | Public gateway routes |
|---|---|---|---|
| `core` | core | core deployment | gateway, sessions, flows, registry control plane |
| `password` | embedded | core | core self-service flows |
| `magic_link` | embedded | core | core self-service flows |
| `policy` | embedded | core | no public route |
| `admin` | external | Compose/Helm service | `/aegion` |
| `analytics` | external | Compose/Helm service | `/api/v1/analytics`, `/graphql/analytics` |
| `oauth2` | external | Compose/Helm service | `/.well-known/openid-configuration`, `/.well-known/jwks.json`, `/oidc/userinfo`, `/oauth2` |
| `introspection` | external | Compose/Helm service | `POST /api/v1/introspection/token` |
| `mfa` | external | Compose/Helm service | `/api/v1/self-service/mfa` |
| `passkeys` | external | Compose/Helm service | `/api/v1/self-service/passkeys` |
| `social` | external | Compose/Helm service | `/api/v1/self-service/social` |
| `sso` | external | Compose/Helm service | `/api/v1/self-service/sso` |
| `proxy` | external | Compose/Helm service | no public route |
| `cli` | on-demand | Compose profile or Helm Job | no HTTP listener |

Route ownership is a compile-time module-plan contract. A route cannot be
claimed by two modules and no module can register additional public routes.

## Startup and registration contract

1. Core loads and validates the deployment configuration. In production,
   enabled external modules require a pinned image, HTTPS public URL, database
   URL, CA file, client certificate/key, and bootstrap credential file.
2. Core connects to Postgres, obtains the migration lock, then migrates core
   followed by enabled modules in dependency order.
3. Core starts its HTTP gateway and mTLS gRPC registry control plane.
4. The deployment platform starts each declared external module with its
   per-module database, mTLS material, bootstrap credential, and private
   advertise addresses.
5. A module exchanges its bootstrap credential for a short-lived scoped token,
   registers its declared metadata and private endpoints over mTLS, and
   heartbeats until shutdown.
6. Core routes an owned public request only while the registered module is
   healthy. On graceful shutdown, a module deregisters before its listeners
   close.

Modules expose `/health`, `/ready`, and `/meta` on their private HTTP service.
`/ready` reports dependency readiness rather than mere process liveness.
Registry registration carries private HTTP and optional gRPC addresses plus
health/readiness URLs; it never creates public route ownership.

## Dependencies

| Module | Required dependencies |
|---|---|
| `admin` | `core`, `policy` |
| `analytics` | `core` |
| `introspection` | `oauth2` |
| `mfa` | `core`, `password` |
| `oauth2` | `core` |
| `passkeys` | `core` |
| `policy` | `core` |
| `proxy` | `core` |
| `social` | `core` |
| `sso` | `core` |

`module_versions` determines the enabled set. `off`, an empty value, and a
missing entry disable a module. Invalid IDs, dependency omissions, overlapping
public routes, non-semantic production versions, and incomplete production
deployment records fail configuration validation before serving traffic.

## Operator rules

- Publish only core gateway ports. Do not publish external module HTTP or gRPC
  ports.
- Use the supplied Compose or Helm manifests; do not expect core to provision
  workloads or Docker networking.
- Mount each module's bootstrap credential and mTLS client material from
  secrets. Do not pass credentials as command-line values.
- Keep external advertised addresses private service-DNS names, never
  wildcard or public bind addresses.
- The CLI is an explicit on-demand operator artifact. Supply its API base URL,
  client certificate/key, CA, and optional API-key file; it does not
  participate in the service registry or `module_versions`.
- Treat registry health as routing eligibility. Diagnose a missing module from
  core registry health and the module's private `/ready` endpoint before
  changing gateway routing.
