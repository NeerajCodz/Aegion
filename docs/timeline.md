# Aegion — Design Timeline (Smart MVP Scope)

This is the delivery shape for Aegion as a super auth project.

Goal: ship high-value capabilities early, then layer protocol depth, authorization, and enterprise features without overloading the first release.

---

## Scope strategy

The roadmap is capability-first, not feature-count-first.

- Phase 1 delivers a production-usable auth core.
- Phase 2 turns Aegion into an OAuth2/OIDC provider.
- Phase 3 adds policy + gateway-level enforcement.
- Phase 4 completes enterprise readiness (SAML + SCIM).

---

## Phase 1 — Core identity platform

**Objective:** Deliver a fully usable authentication system that most products can run immediately.

### Included

- Core identity model
- Password authentication
- Sessions
- Email OTP
- Magic link
- Admin panel
- Postgres persistence
- Basic security controls
- Identity schema support
- Courier (email/SMS delivery pipeline)

### Outcome

By the end of Phase 1, teams can run a serious self-hosted auth platform with modern login methods and operational management.

---

## Phase 2 — OAuth2/OIDC server

**Objective:** Make Aegion a standards-compliant authorization server for first-party and third-party applications.

### Included

- OAuth2 clients
- Access and refresh token lifecycle
- PKCE enforcement
- JWT signing + verification flow
- Login/consent challenge flow
- OIDC discovery and JWKS compatibility

### Outcome

Aegion can now serve as the central auth authority for multiple apps and services.

---

## Phase 3 — Policy and proxy

**Objective:** Move from authentication-only to end-to-end access control enforcement.

### Included

- Policy engine:
  - RBAC
  - ABAC
  - ReBAC
- Identity-aware proxy:
  - match
  - authenticate
  - authorize
  - mutate
  - forward

### Outcome

Aegion can enforce authorization at the edge and inside application policy checks, not just issue identities and tokens.

---

## Phase 4 — Enterprise readiness

**Objective:** Complete enterprise identity requirements.

### Included

- SAML 2.0 enterprise SSO
- SCIM 2.0 provisioning
- Enterprise admin controls and integrations
- Domain routing and enterprise lifecycle controls

### Outcome

Aegion supports enterprise onboarding, identity lifecycle sync, and large-organization access models.

---

## Non-goals for early phases

- Expanding UI surface area before core protocol maturity
- Adding optional integrations that duplicate core value
- Broad SDK matrix before API and policy surfaces stabilize

---

## Success criteria by phase

| Phase | Success signal |
|---|---|
| 1 | Teams can replace basic hosted auth and run production login/session workflows |
| 2 | Multiple apps can rely on Aegion for OAuth2/OIDC with secure token operations |
| 3 | Authorization can be enforced consistently at API and proxy layers |
| 4 | Enterprise customers can onboard with SAML + SCIM and managed controls |

---

## Product posture

This timeline keeps Aegion practical from day one while still converging to a full super auth platform.
