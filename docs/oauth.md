# Aegion — OAuth2 / OIDC Server

Aegion's `oauth2` module turns it into a full OpenID Connect-compliant authorization server. Applications register as clients and request tokens through standard OAuth2 flows.

---

## The consent model

Aegion does not own the login or consent UI. **Your application does.**

```
OAuth2 client                Aegion                    Your login UI
     │                          │                            │
     │── authorization_code ───►│                            │
     │   request                │── redirect with ──────────►│
     │                          │   login_challenge           │
     │                          │                            │
     │                          │◄── accept_login ───────────│
     │                          │    (calls Aegion API)       │
     │                          │                            │
     │                          │── redirect with ──────────►│
     │                          │   consent_challenge  (consent UI)
     │                          │                            │
     │                          │◄── accept_consent ─────────│
     │                          │    (calls Aegion API)       │
     │                          │                            │
     │◄── redirect with ────────│                            │
     │    authorization_code    │
     │                          │
     │── exchange code ─────────►│
     │   for tokens             │
     │◄── access + refresh ─────│
     │    tokens                │
```

This model means Aegion makes zero assumptions about your login experience. You build the form. Aegion handles the protocol.

---

## Grant types

| Grant type | RFC | Use case |
|---|---|---|
| `authorization_code` + PKCE | RFC 6749, RFC 7636 | Browser apps, mobile, web apps |
| `client_credentials` | RFC 6749 | Machine-to-machine, service accounts |
| `refresh_token` | RFC 6749 | Renew access tokens without re-auth |
| `implicit` | RFC 6749 | Legacy — not recommended |
| `urn:ietf:params:oauth:grant-type:device_code` | RFC 8628 | CLIs, smart TVs, input-constrained devices |
| `urn:ietf:params:oauth:grant-type:jwt-bearer` | RFC 7523 | Service-to-service token exchange |

---

## Token strategy

### JWT access tokens (default)

Self-contained tokens verified locally by downstream services using Aegion's JWKS public key.

**Pros:**
- No round-trip to Aegion on every request
- Downstream services verify cryptographically — no single point of failure
- Low latency

**Cons:**
- Cannot be revoked mid-TTL (only by waiting for expiry)
- Recommended TTL: 15 minutes

### Opaque access tokens

Random strings that require calling `/oauth2/introspect` to validate.

**Pros:**
- Instantly revocable
- Token revocation takes effect on next request

**Cons:**
- Every authenticated request makes a network call to Aegion
- Aegion becomes a synchronous dependency

### Recommended pattern

- Short-lived **JWT access tokens** (15m TTL)
- Longer-lived **opaque refresh tokens** (rotated on every use with family invalidation)
- Local JWKS verification for access tokens
- Introspection only for services that need immediate revocation guarantees

---

## Key rotation

Zero-downtime rotation process:

```
1. New keypair generated
2. New key promoted to status: active (signs new tokens)
3. Old key demoted to status: retiring (verifies old tokens, won't sign new ones)
4. After key_rotation_grace_period (default 24h): old key marked expired
5. JWKS endpoint reflects current active + retiring keys at all times
```

Downstream services that cache the JWKS public key automatically recover — when a verification fails with the cached key, they re-fetch JWKS and try again with the new key.

---

## PKCE

Proof Key for Code Exchange. Prevents authorization code interception attacks.

| Setting | Behaviour |
|---|---|
| `pkce_enforced_for_public_clients: true` | Public clients (no secret) must use PKCE. **Default.** |
| `pkce_enforced: true` | All clients must use PKCE regardless of type (RFC 9700) |

---

## Refresh token family invalidation

Refresh tokens rotate on every use — each use produces a new refresh token and invalidates the old one.

If the same refresh token is used twice (replay attack / token theft), Aegion invalidates the **entire family** for that client/subject pair. Both the attacker and the legitimate user lose their tokens and must re-authenticate.

```
Legitimate user:     use refresh_token_A → get refresh_token_B ✓
                     use refresh_token_B → get refresh_token_C ✓

Attacker steals token_A:
                     use refresh_token_A (already used) → FAMILY INVALIDATED
                     both user and attacker must re-authenticate
```

A configurable `grace_period` (default 0s, can be 2m for mobile apps) handles network race conditions without triggering false invalidation.

---

## Client management

All OAuth2 clients are database records managed in the admin panel at runtime.

**Client configuration includes:**
- Client ID and hashed secret (secret shown once on creation, never again)
- Allowed redirect URIs (strict matching)
- Enabled grant types and response types
- Allowed scopes
- `skip_consent` flag for first-party applications
- CORS origins for browser-based clients
- Front/back-channel logout URIs
- Token endpoint auth method
- Access token strategy (jwt vs opaque) per client

**Client wizard** in admin panel: guided setup for SPA, server app, mobile app, machine-to-machine, CLI/device — pre-configures sensible defaults per type.

**Test authorization flow** button: initiates a real auth code flow from the admin's browser to verify client config before deploying.

---

## Device Authorization Grant (CLIs / smart TVs)

For devices that cannot open a browser or handle redirects.

```
Device/CLI                    Aegion                    User's browser
    │── POST /oauth2/device ──►│                              │
    │◄── device_code ──────────│                              │
    │    user_code             │                              │
    │    verification_uri      │                              │
    │                          │                              │
    │   (poll /oauth2/token)   │   User navigates to ────────►│
    │                          │   verification_uri           │
    │                          │   enters user_code           │
    │                          │◄── approves ─────────────────│
    │                          │                              │
    │◄── access + refresh ─────│
         tokens
```

---

## JWT Bearer Grant (service-to-service)

Allows a service to exchange a JWT issued by a trusted third-party issuer for an Aegion access token — without a full authorization flow.

Use case: your internal service has a JWT from your company's identity system. Rather than implementing a full OAuth2 flow, it exchanges that JWT directly for an Aegion token scoped to what it needs.

Trusted issuers are configured in the admin panel with the issuer claim, expected subject (or "any"), the public JWKS for signature verification, and the scopes that can be granted.

---

## Token claims hook

An optional webhook called before every token issuance.

```
Aegion token engine
    │── POST to your hook URL ──►  Your service
    │   (token context: identity,  │
    │    session, scopes)          │── return additional claims
    │◄── { claims: { ... } } ──────│
    │
    │   (inject claims into token)
```

Use to inject application-specific data — subscription tier, feature flags, internal user ID — into tokens without modifying Aegion.

---

## OIDC discovery

`GET /.well-known/openid-configuration` — standard OIDC discovery document.

`GET /.well-known/jwks.json` — public key set for local token verification.

`GET /oauth2/userinfo` — identity traits for the authenticated user (requires `openid` scope).
