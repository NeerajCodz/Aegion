# Aegion — Overview

> One container. One port. One config file. Complete auth.

Aegion is a self-hosted identity and access platform. It replaces Auth0, the Ory stack, and Supabase Auth with a single Go binary you own and run yourself.

---

## The one-line pitch

Add Aegion to your `docker-compose.yml`. Fill in a 100-line yaml. Open a browser, log into the admin panel, and configure the rest at runtime — no restarts, no redeploys.

---

## What it gives you

| Capability | Description |
|---|---|
| **Every login method** | Password, magic link, OTP, passkeys, social (Google, GitHub, Apple, …), SAML SSO |
| **MFA** | TOTP, SMS, WebAuthn hardware keys, backup codes, trusted devices |
| **Sessions** | AAL-tracked, multi-device, revocable, impersonation-safe |
| **OAuth2 server** | Full OIDC-compliant AS — clients, scopes, consent, JWT/opaque tokens |
| **Policy engine** | RBAC, ABAC (CEL rules), ReBAC (Zanzibar tuple store) |
| **Reverse proxy** | Identity-aware ingress — authenticate, authorize, mutate headers, forward |
| **Admin panel** | React SPA embedded in the binary — 50+ granular permissions, audit log, analytics |
| **Extensibility** | Flow hooks, token claims hooks, webhook events, SCIM 2.0 provisioning |

---

## The mental model

```
Your frontend  ──────────────────────────────────────────────────────────────┐
                                                                              │
    Renders the form.                                                         │
    Calls Aegion's API.          ┌──────────────────────────────────┐        │
    Gets back session.           │            Aegion                │        │
                                 │                                  │        │
                                 │  owns:  protocol, storage,       │        │
                                 │         hashing, tokens,         │        │
                                 │         email/SMS delivery        │        │
                                 │                                  │        │
Your backend ────────────────────┤  reads: X-User-ID, X-User-Email │        │
                                 │          from forwarded headers  │        │
    Reads HTTP headers.          │                                  │        │
    Never sees a session.        └──────────────────────────────────┘        │
    Never imports Aegion code.                                                │
                                                                              │
```

**Aegion owns the protocol. You own the UI.**

This means you can redesign your login page completely without touching auth logic. You can run Aegion headlessly and drive everything via API. You can use any frontend framework — React, Vue, native iOS, server-rendered HTML.

---

## What it is NOT

- Not a SaaS — you host it, you own the data
- Not a microservices mesh — it is a single binary, single port
- Not a framework you import — it is a sidecar service your app calls
- Not opinionated about your frontend — it returns form state as JSON, you render it

---

## Two-container quickstart

```yaml
services:
  aegion:
    image: aegion/aegion:latest
    ports: ["8080:8080"]
    volumes:
      - ./aegion.yaml:/config/aegion.yaml
    depends_on:
      postgres: { condition: service_healthy }

  postgres:
    image: postgres:16-alpine
    environment:
      POSTGRES_DB: aegion
      POSTGRES_USER: aegion
      POSTGRES_PASSWORD: secret
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U aegion"]
      interval: 5s
      retries: 5
```

Navigate to `http://localhost:8080/aegion` to open the admin panel.

---

## Compared to alternatives

| | Aegion | Auth0 | Ory stack | Supabase Auth | Keycloak |
|---|---|---|---|---|---|
| Self-hosted | ✓ | ✗ | ✓ | ✓ | ✓ |
| Single binary | ✓ | — | ✗ (4 services) | ✓ | ✗ |
| Admin panel | ✓ embedded | ✓ cloud | ✗ | partial | ✓ complex |
| OAuth2 server | ✓ | ✓ | ✓ | ✗ | ✓ |
| Policy engine | ✓ (3 models) | partial | ✓ separate | ✗ | RBAC only |
| Reverse proxy | ✓ | ✗ | ✓ separate | ✗ | ✗ |
| Runtime config | ✓ | ✓ | ✗ | partial | partial |
| Binary size | 15–40MB | — | 4×100MB+ | ~50MB | 400MB+ |

---

## Documentation index

| Doc | What it covers |
|---|---|
| [architecture.md](./architecture.md) | Go + Rust hybrid, module system, build pipeline, request flow |
| [modules.md](./modules.md) | Module model, dependencies, image-based modular runtime |
| [security.md](./security.md) | Auth methods, MFA, session security, platform security controls |
| [oauth.md](./oauth.md) | OAuth2/OIDC server, grant types, token strategy, consent model |
| [policy.md](./policy.md) | RBAC, ABAC, ReBAC model and evaluation strategy |
| [proxy.md](./proxy.md) | Identity-aware ingress pipeline and enforcement behavior |
| [admin.md](./admin.md) | Admin control plane, enterprise controls, SAML/SCIM operations |
| [config.md](./config.md) | `aegion.yaml` model: build-time vs runtime configuration |
| [timeline.md](./timeline.md) | Smart MVP scope and phased platform rollout |
| [project-structure.md](./project-structure.md) | Recommended super-repo/monorepo structure |
| [aegion-project.md](./aegion-project.md) | Product specification entry and documentation map |
