# Aegion — OAuth2 / OIDC Server

`aegion-oauth2` is a full OpenID Connect-compliant authorization server. It is independently scalable and sits on the hot path for every token issuance and refresh operation.

---

## The consent model

Aegion does not own the login or consent UI. Your application does.

```
OAuth2 client               aegion-oauth2                  Your UI
     │                           │                            │
     │── authorization_code ────►│                            │
     │   request                 │── redirect: login_challenge►│
     │                           │◄── accept_login ───────────│
     │                           │── redirect: consent_challenge►│
     │                           │◄── accept_consent ─────────│
     │◄── redirect: auth_code ───│                            │
     │── exchange code ─────────►│
     │◄── access + refresh ──────│
```

`aegion-oauth2` handles the protocol. You handle the forms.

---

## How oauth2 calls other modules

During every authorization flow:

```
aegion-oauth2  ──gRPC: SessionAPI.Resolve──────────► core
               ──gRPC: MFAEngine.GetStatus─────────► aegion-mfa (if enabled)
               ──gRPC: PolicyEngine.Check──────────► aegion-policy (scope enforcement)
               ──gRPC: core JWT engine─────────────► core (JWT signing)

Event bus subscriptions:
               ── session.revoked   → invalidate consent sessions, revoke tokens
               ── identity.suspended → revoke all tokens for identity
               ── identity.deleted  → revoke all tokens and consents for identity
               ── key.rotated       → refresh signing key reference
```

---

## Grant types

| Grant type | RFC | Primary use case |
|---|---|---|
| `authorization_code` + PKCE | RFC 6749, RFC 7636 | Browser apps, mobile, web apps |
| `client_credentials` | RFC 6749 | Machine-to-machine, service accounts |
| `refresh_token` | RFC 6749 | Renew access tokens without re-auth |
| `device_code` | RFC 8628 | CLIs, smart TVs, input-constrained devices |
| `jwt_bearer` | RFC 7523 | Service-to-service token exchange via trusted JWT |
| `implicit` | RFC 6749 | Legacy only — not recommended, PKCE preferred |

---

## Client authentication methods

Each OAuth2 client declares how it authenticates to the token endpoint via `token_endpoint_auth_method`:

| Method | How it works | Recommended for |
|---|---|---|
| `client_secret_post` | `client_id` and `client_secret` in POST body | Legacy server-side apps |
| `client_secret_basic` | Basic auth header: `Authorization: Basic base64(client_id:client_secret)` | Server-side apps, standard HTTP auth |
| `client_secret_jwt` | Client signs a JWT assertion with its own secret (HMAC) | Higher assurance than shared secret |
| `private_key_jwt` | Client signs a JWT assertion with its own private key | Strongest auth — M2M, high-value clients |
| `none` | No authentication — public client | SPAs, mobile apps (must use PKCE) |

### Private key JWT authentication flow

For `private_key_jwt`, the client:
1. Creates a JWT with claims: `iss=client_id`, `sub=client_id`, `aud=token_endpoint_url`, `jti=<random>`, `exp=now+60s`
2. Signs it with the client's registered private key (RS256 or ES256)
3. Sends it as `client_assertion` with `client_assertion_type=urn:ietf:params:oauth:client-assertion-type:jwt-bearer`

`aegion-oauth2` verifies the assertion against the client's registered JWKS URL or public key. This is the recommended method for high-value machine-to-machine clients.

---

## PKCE implementation

PKCE (Proof Key for Code Exchange) prevents authorization code interception attacks.

### How it works

```
Client-side:
  code_verifier  = random 43-128 char string [A-Za-z0-9\-._~]
  code_challenge = BASE64URL(SHA256(ASCII(code_verifier)))

Authorization request:
  ?code_challenge=<code_challenge>&code_challenge_method=S256

Stored in oa2_auth_codes:
  code_challenge       = <code_challenge>
  code_challenge_method = "S256"

Token exchange:
  POST /oauth2/token
  code_verifier=<original_code_verifier>

aegion-oauth2 verifies:
  BASE64URL(SHA256(ASCII(code_verifier))) == stored code_challenge
  If mismatch: reject with error=invalid_grant
```

### PKCE enforcement settings

| Setting | Effect |
|---|---|
| `pkce.enforced_for_public_clients: true` | All clients with `token_endpoint_auth_method: none` must use PKCE (default) |
| `pkce.enforced: true` | All clients regardless of type must use PKCE (RFC 9700 compliant) |

For new deployments, `pkce.enforced: true` is recommended.

---

## JWT access token claims structure

When `access_token_strategy: jwt`, issued tokens contain:

### Standard claims

```json
{
  "iss": "https://auth.example.com",
  "sub": "id_alice_xyz789",
  "aud": ["my-api", "https://auth.example.com/userinfo"],
  "iat": 1742912521,
  "exp": 1742913421,
  "jti": "oa2_at_abc123def456",
  "client_id": "spa-frontend",
  "scope": "openid profile email"
}
```

### OIDC claims (added when `openid` scope is granted)

```json
{
  "email":          "alice@example.com",
  "email_verified": true,
  "name":           "Alice Smith",
  "given_name":     "Alice",
  "family_name":    "Smith",
  "preferred_username": "alice",
  "updated_at":     1742900000
}
```

### Custom claims (from token hook or consent session)

Any claims returned by the token hook or added to the consent session's `access_token_claims` field are merged into the JWT payload. Operators control which top-level claims are allowed via `allowed_top_level_claims` in aegion.yaml (empty = all claims allowed).

### Pairwise subject identifiers

When `subject_identifiers.supported_types` includes `pairwise`, the `sub` claim is computed as:

```
sub = BASE64URL(SHA256(client_sector_identifier + "|" + identity_id + "|" + pairwise_salt))
```

This means the same identity has a different `sub` value for each client. The `pairwise_salt` is a stable secret — changing it invalidates all existing pairwise subjects. Never change it in production.

---

## ID token claims structure

ID tokens (issued when `openid` scope is granted) carry additional OIDC-specific claims:

```json
{
  "iss":       "https://auth.example.com",
  "sub":       "id_alice_xyz789",
  "aud":       "spa-frontend",
  "iat":       1742912521,
  "exp":       1742916121,
  "auth_time": 1742912500,
  "nonce":     "random-nonce-from-auth-request",
  "at_hash":   "halfHashOfAccessToken",
  "acr":       "aal1",
  "amr":       ["pwd"],
  "sid":       "sess_abc123"
}
```

`acr` (Authentication Context Class Reference) maps to Aegion's AAL:
- `aal1` → `acr: "aal1"`, `amr: ["pwd"]` (or `["passkey"]`, `["social"]`, etc.)
- `aal2` → `acr: "aal2"`, `amr: ["pwd", "totp"]`

---

## Key rotation (zero-downtime)

```
Step 1  Core generates new RS256/ES256 keypair (Rust JWT engine)
        Stores in core_signing_keys with status=active
        Old active key → status=retiring

Step 2  key.rotated event published on event bus
        aegion-oauth2 receives event → updates its key reference

Step 3  All new tokens signed with new key (new kid in JWT header)
        Old tokens remain valid — verified with retiring key

Step 4  JWKS endpoint always returns active + all retiring keys
        GET /.well-known/jwks.json → { keys: [new_key, old_key] }

Step 5  After key_rotation_grace_period (default 24h):
        Old key → status=expired
        Removed from JWKS response

Step 6  Downstream services that cached the old JWKS:
        - Receive a token with new kid
        - Try cached keys → verification fails (kid not found)
        - Re-fetch JWKS → get new key → verification succeeds
        This is the required fetch-on-failure pattern (see below)
```

### Required downstream integration: fetch-on-failure JWKS caching

This is a **required integration behavior** for downstream services verifying JWT access tokens locally:

```python
def verify_token(token):
    kid = extract_kid(token)
    
    # Try cached keys first
    if kid in jwks_cache:
        try:
            return verify_with_key(token, jwks_cache[kid])
        except InvalidSignature:
            pass  # Key may have been rotated; fall through to re-fetch
    
    # Cache miss OR signature failed with cached key → re-fetch
    new_jwks = fetch_jwks("https://auth.example.com/.well-known/jwks.json")
    jwks_cache.update(new_jwks)
    
    if kid not in jwks_cache:
        raise InvalidToken("kid not found in JWKS after re-fetch")
    
    return verify_with_key(token, jwks_cache[kid])
```

Without this pattern, any key rotation will cause a brief outage for services that have cached the old key. Most JWT libraries support this natively — check your library's JWKS caching documentation.

---

## Refresh token family invalidation

Refresh tokens rotate on every use. The `family_id` column in `oa2_refresh_tokens` links the entire rotation chain.

### Normal rotation

```
token_A (family: F1, active: true)
   │── used → token_B issued
   ▼
token_A (family: F1, active: false, used: true)
token_B (family: F1, active: true)  ← new active token
```

### Replay detection and family invalidation

```
token_A already used (active: false, used: true)
New request presents token_A again:
  → aegion-oauth2 detects replay (token used AND already has a successor)
  → UPDATE oa2_refresh_tokens SET active=false WHERE family_id=F1
     (single query invalidates ALL tokens in the family)
  → oauth2.token_family_invalidated event published
  → introspection module and proxy flush caches for all tokens in F1
  → Both attacker and legitimate user must re-authenticate
```

### Grace period for mobile clients

Mobile apps sometimes lose the server's response on a refresh, then retry with the same refresh token. The `grace_period` setting (default 0s) allows the same refresh token to be used again within the grace window without triggering family invalidation:

```
grace_period: 2m

token_A used at T+0 → token_B issued
token_A used again at T+90s (within 2m grace):
  → return token_B again (the same token already issued)
  → no family invalidation

token_A used again at T+3m (outside grace period):
  → family invalidation triggered
```

---

## Token claims hook

Optional webhook called synchronously before every token issuance:

```
Request timeout: 2s (configurable, never block token issuance for longer)

POST <hook_url>
Content-Type: application/json

{
  "subject":   "id_alice_xyz789",
  "client_id": "my-app",
  "scopes":    ["openid", "profile"],
  "session": {
    "id":  "sess_abc123",
    "aal": "aal1"
  },
  "identity": {
    "traits":   { "email": "alice@example.com" },
    "metadata": {}
  },
  "grant_type": "authorization_code"
}

Expected response:
{
  "claims": {
    "subscription_tier": "pro",
    "feature_flags":     ["new_dashboard"],
    "internal_user_id":  "usr_123"
  }
}
```

Claims in the response are merged into the token payload. Claims that would override standard JWT reserved claims (`iss`, `sub`, `aud`, `iat`, `exp`, `jti`) are ignored with a warning log.

If the hook returns a non-2xx status or times out, behavior depends on configuration:
- `on_error: ignore` (default) — token issuance continues without additional claims
- `on_error: reject` — token issuance fails with `server_error`

---

## OIDC discovery

All three endpoints are served by core's routing layer (assembled from oauth2 module data):

- `GET /.well-known/openid-configuration` — full OIDC provider metadata document
- `GET /.well-known/jwks.json` — public key set for local token verification
- `GET /oauth2/userinfo` — identity traits for the authenticated user (requires `openid` scope, valid access token)

### Userinfo response shape

```json
{
  "sub":            "id_alice_xyz789",
  "email":          "alice@example.com",
  "email_verified": true,
  "name":           "Alice Smith",
  "given_name":     "Alice",
  "family_name":    "Smith",
  "updated_at":     1742900000
}
```

Only claims in `oidc_discovery.supported_claims` and granted via the token's scope are returned.

---

## Scaling aegion-oauth2

`aegion-oauth2` is stateless — all state lives in Postgres (with Redis as optional cache). Every instance connects to the same database. Instances are interchangeable.

JWT signing uses the active key fetched from Postgres on startup and cached in-process. On `key.rotated` event, instances refresh their key cache without restart.

For token-heavy workloads (high issuance rate), the JWT signing Rust engine is the CPU bottleneck. Scale horizontally:

```bash
kubectl scale deployment aegion-oauth2 --replicas=8
```

Core's service registry load-balances token requests across all healthy instances.

---

## Device Authorization Grant

```
Device/CLI                    aegion-oauth2               User's browser
    │── POST /oauth2/device ──►│                                │
    │◄── { device_code,        │                                │
    │      user_code,          │                                │
    │      verification_uri,   │   User visits verification_uri►│
    │      expires_in,         │   Enters user_code             │
    │      interval: 5 } ──────│◄── approves ───────────────────│
    │                          │
    │   poll every 5s:         │
    │── POST /oauth2/token ────►│
    │   grant_type=device_code  │
    │   device_code=<value>     │
    │                          │
    │◄── pending (authorization_pending) or tokens
```

The `interval` returned in the device code response is the minimum polling interval. The device client must not poll faster than this value. `aegion-oauth2` enforces this with rate limiting per `device_code`.

---

## Security hardening

- `aud` and `iss` validation on every token verification — tokens issued for one service cannot be used for another
- No `HS256` — symmetric keys are unsuitable for distributed verification (any verifier also becomes a signer)
- Access token TTL default 15m — limits blast radius of leaked tokens to a short window
- `jti` claim in every JWT — enables revocation lookup via bloom filter without full token hash storage
- Short auth code TTL (10m) — authorization codes expire quickly to limit interception window
- Redirect URI strict matching — no wildcard domains, no open redirect via `redirect_uri` parameter
- Client secret shown once on creation, stored as bcrypt hash — never retrievable after creation
