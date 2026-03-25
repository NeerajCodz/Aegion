This is a deep topic. Let me cover every attack vector and the corresponding defense, then tell you exactly what Aegion needs to implement.

---

## The attack surface

Cookie and JWT theft happens through four main vectors:

**1. XSS (Cross-Site Scripting)** — attacker injects JavaScript into your page, reads `document.cookie` or `localStorage`, exfiltrates the token.

**2. Network interception** — token travels over HTTP (not HTTPS), attacker sniffs it in transit.

**3. CSRF (Cross-Site Request Forgery)** — attacker tricks the browser into making an authenticated request from a malicious site. The cookie is sent automatically by the browser — the attacker never sees it, but the request succeeds.

**4. Stolen refresh token replay** — attacker intercepts a refresh token (from logs, a compromised device, a MITM) and uses it to mint new access tokens indefinitely.

---

## Defense layer by layer

### Layer 1 — Make cookies inaccessible to JavaScript

This is the single most important defence against XSS-based cookie theft.

```
Set-Cookie: aegion_session=<token>; HttpOnly; Secure; SameSite=Lax; Path=/
```

| Flag | What it does |
|---|---|
| `HttpOnly` | Completely blocks `document.cookie` access from JavaScript. XSS cannot read it. |
| `Secure` | Cookie is never sent over plain HTTP. Only HTTPS. |
| `SameSite=Lax` | Cookie is not sent on cross-site requests initiated by third-party pages (forms, iframes, `fetch` from other origins). Sent on top-level navigations (clicking a link). |
| `SameSite=Strict` | Cookie never sent on any cross-site request at all. Breaks flows triggered from external links (e.g. email magic links). Usually too aggressive. |
| `SameSite=None; Secure` | Required for cross-site iframes. Must be paired with `Secure`. Opens CSRF risk — compensate with CSRF tokens. |

**JWT access tokens must never live in `localStorage` or `sessionStorage`.** These are fully accessible to any JavaScript on the page. If your frontend needs to call an API with a JWT, use a short-lived token issued from an HttpOnly cookie via Aegion's `id_token` mutator (proxy injects a signed JWT into the forwarded request — your frontend never sees the raw token).

---

### Layer 2 — CSRF protection (double-submit cookie pattern)

`SameSite=Lax` alone is not enough for mutations. Aegion uses the double-submit cookie pattern:

```
1. On session creation, core sets two cookies:
   aegion_session=<token>   HttpOnly; Secure; SameSite=Lax
   aegion_csrf=<csrf_token>  Secure; SameSite=Lax  (NOT HttpOnly — JS must read it)

2. Frontend JavaScript reads aegion_csrf and sends it as a header:
   X-CSRF-Token: <csrf_token>

3. Core validates: cookie value == header value
   An attacker's cross-origin request cannot set this header
   (SameSite blocks the cookie; CORS blocks the header)

4. After every successful mutation, core rotates the CSRF token:
   - old token invalidated immediately
   - new token set in cookie + returned in response header
```

The critical property: a malicious site can trigger your browser to send the cookie (if `SameSite` is misconfigured), but it cannot read the `aegion_csrf` cookie value via JavaScript (same-origin policy), so it cannot set the `X-CSRF-Token` header.

---

### Layer 3 — Session binding (device fingerprinting)

A stolen session cookie from device A should not work on device B. Bind sessions to device characteristics at creation time and re-verify on every request.

```go
// At session creation — store binding data
type SessionBinding struct {
    IPSubnet    string   // /24 prefix of client IP (not full IP — breaks mobile users)
    UserAgent   string   // hashed
    // Do NOT bind to full IP — mobile users change IPs constantly
}

// On every request — verify binding
func verifySessionBinding(session *Session, req *http.Request) error {
    currentSubnet := toSubnet24(req.RemoteAddr)
    if session.Binding.IPSubnet != currentSubnet {
        // Suspicious — IP changed significantly
        // Options: require re-auth, invalidate session, or just log + alert
    }
    // UserAgent mismatch: log and alert but don't hard-reject
    // (browsers update mid-session; too aggressive to reject on UA change)
}
```

This is a soft signal, not a hard block. A stolen cookie used from a wildly different IP (different country, different ASN) triggers a step-up re-auth requirement rather than a silent session use.

---

### Layer 4 — Short-lived access tokens + opaque refresh tokens

JWTs have a fundamental problem: they cannot be revoked mid-TTL. If someone steals your 24h access token, they have 24h of access regardless of what you do.

The correct pattern:

```
Access token:   JWT, 15 minute TTL, stateless verification
Refresh token:  opaque random string, long-lived, stored in DB
                issued in HttpOnly cookie, rotated on every use
```

When the access token expires, the frontend silently refreshes it using the refresh token cookie. The short TTL means a stolen access token is only useful for 15 minutes. The refresh token is in an HttpOnly cookie so JavaScript cannot exfiltrate it.

---

### Layer 5 — Refresh token rotation + family invalidation

This is Aegion's existing design, but worth stating clearly as a theft defence:

```
Every refresh use:
  old_token → USED, new_token → ISSUED

Replay detection:
  used_token presented again → entire family invalidated
  both attacker and legitimate user lose access
  both forced to re-authenticate
```

This means a stolen refresh token has a limited window: the legitimate user's next refresh attempt triggers the invalidation. The attacker is ejected. The legitimate user gets an "unexpected logout" and must re-auth — annoying but far better than silent persistent compromise.

For mobile apps that retry on network failure, the grace period (`grace_period: 2m`) prevents false invalidations.

---

### Layer 6 — Token binding (DPoP — Demonstration of Proof-of-Possession)

This is the strongest defence against JWT theft and the most underimplemented. DPoP binds a JWT access token to a specific client keypair — the token is useless without the private key, even if it's stolen.

```
Client generates an ephemeral keypair at session start (stored in memory only)

On every API request:
  1. Client creates a DPoP proof JWT:
     {
       "typ": "dpop+jwt",
       "alg": "ES256",
       "jwk": <client_public_key>
     }.{
       "htm": "GET",
       "htu": "https://api.example.com/resource",
       "iat": 1742912521,
       "jti": "<random — prevents replay>"
     }
     Signed with the client's ephemeral private key

  2. Client sends both:
     Authorization: DPoP <access_token>
     DPoP: <proof_jwt>

  3. Server verifies:
     a. The access_token's `cnf.jkt` claim matches SHA-256(client_public_key)
     b. The DPoP proof signature is valid
     c. `htm` and `htu` match the current request
     d. `jti` has not been seen before (prevents replay)
     e. `iat` is recent (within 30s)
```

An attacker who steals the access token cannot use it — they don't have the private key. DPoP is standardised in RFC 9449 and is the current best practice for public clients (SPAs, mobile apps).

**What Aegion needs to add:**

```go
// In aegion-oauth2: DPoP token issuance
// When client presents DPoP proof at /oauth2/token:
// 1. Verify the DPoP proof
// 2. Compute cnf.jkt = BASE64URL(SHA256(DPoP public key JWK))
// 3. Include cnf claim in issued access token:
//    { "cnf": { "jkt": "<thumbprint>" } }

// In aegion-proxy: DPoP proof verification on every request
// 1. Extract DPoP header and Authorization: DPoP token
// 2. Verify access token cnf.jkt matches proof's jwk thumbprint
// 3. Verify proof htm/htu match current request
// 4. Check jti not in replay cache (Redis set, TTL = proof's iat window)
```

---

### Layer 7 — Secure cookie `__Host-` prefix

The `__Host-` prefix is a browser security feature that enforces three properties on the cookie automatically:

```
Set-Cookie: __Host-aegion_session=<token>; Secure; Path=/; HttpOnly
```

| Property enforced by `__Host-` | What it prevents |
|---|---|
| Must have `Secure` flag | No HTTP transmission |
| Must have `Path=/` | Cookie not scoped to a subdirectory |
| Must NOT have `Domain` attribute | Cookie cannot be set by a subdomain |

The third point matters: without `__Host-`, an XSS vulnerability on `evil.yourdomain.com` could set or overwrite the session cookie for `yourdomain.com`. With `__Host-`, subdomains cannot touch the cookie.

```
Cookie name in aegion.yaml:
  session.cookie.name: "__Host-aegion_session"
  session.cookie.domain: ""         # must be empty for __Host- prefix
  session.cookie.secure: true       # must be true
  session.cookie.path: "/"          # must be /
```

---

### Layer 8 — Session fixation prevention

Session fixation is when an attacker sets a known session token (via URL parameter or cookie injection) before the user logs in, then waits for the user to authenticate — at which point the attacker's known token becomes authenticated.

Defence: **regenerate the session ID on every authentication event.**

```go
// In core/session — after successful authentication:
func (s *SessionStore) Authenticate(ctx context.Context, flowID string, method string) (*Session, error) {
    // Get the pre-auth session (from the login flow continuity)
    preAuthSession := getFlowSession(flowID)
    
    // Issue a BRAND NEW session — new token, new ID
    newSession := &Session{
        ID:         newUUID(),
        Token:      generateSecureToken(),   // completely new token
        IdentityID: identity.ID,
        AAL:        computeAAL(method),
    }
    
    // Invalidate the pre-auth session
    preAuthSession.Active = false
    store.Save(preAuthSession)
    
    // The pre-auth token is now dead — cannot be used to access the authenticated session
    return newSession, nil
}
```

---

### Layer 9 — Anomaly detection and step-up re-auth

For high-value sessions, monitor for signs of theft at runtime:

```
Signals that suggest a stolen session:
  - Request from IP in different country than session was created
  - Request from IP in different ASN than last 10 requests
  - User-Agent changed significantly (browser update is fine; Safari → Firefox is suspicious)
  - Concurrent requests from two geographically impossible locations
    (NYC at 10:00:00 and London at 10:00:05 — impossible travel)
  - Sudden spike in API calls inconsistent with normal usage pattern

Response options (in order of aggressiveness):
  1. Log + alert (silent — good for monitoring without friction)
  2. Require re-auth on next sensitive operation (step-up AAL)
  3. Require re-auth immediately (invalidate current session, redirect to login)
  4. Send "was this you?" notification email, wait for confirm/deny
     → if deny: invalidate session, lock account, notify user
```

---

### Layer 10 — Content Security Policy (defence in depth against XSS)

If XSS cannot run, it cannot steal tokens. CSP is the last line of defence if HttpOnly fails or if a future bug exposes a token to JavaScript.

```
Content-Security-Policy:
  default-src 'self';
  script-src 'self' 'nonce-<per-request-nonce>';
  object-src 'none';
  base-uri 'self';
  frame-ancestors 'none';
```

Key restrictions:
- `script-src 'nonce-...'` — only scripts with the server-generated nonce run. Injected scripts have no nonce and are blocked.
- `object-src 'none'` — no Flash/plugin vectors.
- `frame-ancestors 'none'` — your login page cannot be iframed (clickjacking prevention).

This is set by your frontend app, not by Aegion directly — but Aegion should document it as a required integration pattern and optionally inject it in the proxy mutator stage.

---

## What Aegion specifically needs to add

Based on the current spec, here is the gap list:

| Feature | Status | Priority |
|---|---|---|
| HttpOnly + Secure + SameSite cookies | Already in spec | — |
| CSRF double-submit | Already in spec | — |
| Refresh token rotation + family invalidation | Already in spec | — |
| Session fixation prevention | Already in spec | — |
| `__Host-` cookie prefix | **Missing** | High |
| DPoP (RFC 9449) token binding | **Missing** | High — add to `aegion-oauth2` and `aegion-proxy` |
| IP subnet binding on sessions | **Missing** | Medium — add to `core/session` |
| Impossible travel detection | **Missing** | Medium — add to suspicious login detection worker |
| CSP header injection via proxy mutator | **Missing** | Medium — add as optional `csp` mutator |
| JWT access tokens banned from localStorage | Documentation gap | High — call this out explicitly in integration docs |
| DPoP jti replay cache | **Missing** (depends on DPoP) | High — Redis set with short TTL |

The two highest-value additions are `__Host-` prefix (zero cost, done in config) and DPoP (significant implementation effort but closes the stolen JWT problem entirely for public clients).