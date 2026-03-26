# Aegion — Admin Specification

Admin is the operational control plane for the entire Aegion platform.

It is where operators configure runtime behavior, manage identities, enforce governance, and run enterprise integrations.

---

## Scope

Admin covers:

- identity lifecycle operations
- authentication and module configuration
- OAuth2 client and token operations
- policy and proxy management
- security controls and audit oversight
- enterprise integrations (SAML and SCIM)

---

## Admin surface model

Aegion admin can be:

- embedded UI (`/aegion`) when admin module is enabled
- API-first management plane for automation

Both must enforce the same capability permissions and audit logging.

### Bootstrap admin path

On first boot, Aegion creates the initial operator identity from `operator` fields in `aegion.yaml` when no operator identity exists yet.

- This operator is the first admin entrypoint into `/aegion`.
- Bootstrap credentials are initialization-only and must be rotated immediately after first login.
- After bootstrap, ongoing admin access should be managed through admin roles and capability grants, not shared bootstrap credentials.

---

## Admin identity lifecycle

1. Bootstrap operator created from yaml on first boot.
2. Operator signs in to `/aegion` and hardens initial security settings.
3. Additional admin users are invited or promoted as identities.
4. Access is controlled by Discord-style granular permissions with optional role presets.
5. Direct permission overrides can grant or deny specific capabilities per admin identity.

This keeps initial setup simple while enabling mature team governance at scale.

---

## Capability model

Admin permissions are granular (Discord-style), not coarse binary roles.

Example capability families:

- `users.*`
- `sessions.*`
- `mfa.*`
- `credentials.*`
- `oauth2.*`
- `policy.*`
- `proxy.*`
- `system.*`
- `admin_team.*`
- `audit.*`

Capabilities can be assigned directly or via admin roles.

### Role + override model

Admin authorization should support all three layers:

- Role presets (fast onboarding)
- Direct grants (specific additions)
- Direct denies (explicit override for high-risk actions)

This mirrors Discord-style permission ergonomics: flexible teams without sacrificing principle-of-least-privilege.

---

## Core admin domains

### 1) Identity operations

- create/update/suspend/ban/delete identities
- credential linking and unlinking
- forced verification and controlled recovery actions

### 2) Session operations

- view active sessions
- revoke one session
- revoke all sessions per identity
- emergency global revocation controls

### 3) Auth method operations

- provider configuration (social/SAML)
- passkey and MFA controls
- OTP/magic-link behavior controls

### 4) OAuth2 operations

- client registration and maintenance
- token and consent visibility/revocation
- key lifecycle and trusted issuer management

### 5) Policy/proxy operations

- role/rule/tuple management
- access rule authoring and test simulation

### 6) System operations

- runtime config flags
- hooks/webhooks and delivery templates
- security posture controls

---

## Admin session security

Admin sessions should be stricter than user sessions:

- dedicated admin session cookie
- shorter session lifespan
- optional minimum AAL enforcement
- re-authentication for privileged operations

All admin-auth critical events must be audited.

### Admin SPA security hardening

The embedded React admin panel (`/aegion`) implements multiple security layers:

**Content Security Policy (CSP):**
```
Content-Security-Policy: 
  default-src 'self';
  script-src 'self';
  style-src 'self' 'unsafe-inline';
  img-src 'self' data: https:;
  font-src 'self';
  connect-src 'self';
  frame-ancestors 'none';
  base-uri 'self';
  form-action 'self'
```

**Additional security headers:**
```
X-Frame-Options: DENY
X-Content-Type-Options: nosniff
X-XSS-Protection: 1; mode=block
Referrer-Policy: strict-origin-when-cross-origin
Permissions-Policy: geolocation=(), microphone=(), camera=()
```

**Session security:**
- HttpOnly session cookie (not accessible to JavaScript)
- SameSite=Lax for CSRF protection
- Secure flag enforced in production
- X-CSRF-Token on every mutation (rotated after each write)
- Automatic logout on session expiration
- Concurrent session limit per admin identity

**API security:**
- Same-origin requests only (no CORS on admin API)
- Request ID tracing for audit correlation
- Rate limiting per admin identity (stricter than user limits)
- Capability checks on every endpoint
- Sensitive operations require re-authentication within last 15 minutes

**Frontend hardening:**
- Subresource Integrity (SRI) hashes on all embedded assets
- No inline event handlers (CSP-compliant React patterns)
- Automatic XSS escaping via React's JSX
- Sanitized rendering of user-provided data
- No `dangerouslySetInnerHTML` usage except in explicitly sandboxed contexts

**Development vs production:**
In dev mode (`build tag: dev`), CSP is relaxed to allow Vite hot module reload. Production builds enforce strict CSP with no unsafe directives.

---

## Enterprise capabilities

### SAML management

Admin manages:

- IdP metadata and certificates
- domain routing
- attribute mapping and validation
- connection tests and rollout controls

### SCIM 2.0 provisioning

SCIM enables enterprise lifecycle automation:

- create users from IdP
- update profile and entitlement data
- deactivate/deprovision users

Admin should expose:

- SCIM endpoint status and health
- token/key management for SCIM clients
- mapping controls between SCIM fields and Aegion traits
- sync logs and failure diagnostics

SCIM is a first-class enterprise feature, not an add-on afterthought.

---

## Audit and compliance

Admin must write append-only records for:

- permission changes
- identity mutations
- auth config changes
- policy/proxy rule changes
- OAuth2 client/key/token operations
- enterprise connection changes (SAML/SCIM)

Audit entries should include actor, target, before/after, and reason where required.

---

## Operational UX requirements

- clear navigation by domain
- safe defaults and confirmation on destructive actions
- dry-run/test features for risky config (policy/proxy/providers)
- runtime changes without restart where supported
- strong diagnostics for failed integrations

---

## Admin and super-repo architecture

Even if admin is a separate module image artifact, it remains part of one Aegion platform runtime:

- governed by central auth/policy
- versioned with Aegion release
- exposed through the same controlled ingress model

---

## Summary

Admin is where Aegion becomes operable at scale: product controls, security governance, and enterprise integrations (including SCIM) in one management plane.
