# Aegion — Authentication Methods & Security

---

## Authentication methods

### Password

Standard email/username + password. Always available when Aegion runs.

| Detail | Value |
|---|---|
| Algorithm | Argon2id (via Rust engine) |
| Auto-tuning | Parameters adjusted on startup to hit 500ms target |
| HIBP check | k-anonymity — 5-char SHA-1 prefix only, raw password never leaves |
| History | Configurable — block reuse of last N passwords |
| Similarity check | Rejects passwords too close to user's own email/username |
| Re-auth gate | Sensitive ops (email/password change) require re-confirmation |
| Complexity rules | Min length, uppercase, numbers, symbols — all configurable at runtime |

---

### Email OTP

6-digit code (configurable length/charset) sent to user's email. Single-use. Short-lived (default 15m).

- Works as a **first factor** (passwordless) or **second factor** (MFA confirmation)
- Rate-limited per email address per hour (configurable)
- No prior enrollment needed — code generated on demand

---

### Magic link

Signed URL sent to user's email. User clicks → session issued. No typing required.

- Same use cases as email OTP, different UX
- Better for desktop email clients; OTP better for mobile
- Same `magic_link` module — strategy (`code` vs `link`) is configurable

---

### Phone / SMS OTP

Same flow as email OTP but via SMS. Supports any HTTP-based SMS provider (Twilio, Vonage, AWS SNS, etc.) configured in the admin panel.

- E.164 phone number normalization on write
- Separate rate limits from email OTP
- First factor (passwordless phone login) or second factor (SMS MFA)

---

### Passkeys / WebAuthn

Platform authenticators (Touch ID, Face ID, Windows Hello) and hardware keys (YubiKey) as **first-factor passwordless** auth.

**How it works:**
1. Registration: browser generates keypair, private key stays on device, public key stored in Aegion
2. Authentication: Aegion issues a challenge, browser signs it with private key, Aegion verifies signature
3. No shared secret, no password transmitted, phishing-resistant (credential scoped to exact domain)

**Critical:** The `rp.id` (Relying Party ID) in `aegion.yaml` cannot be changed after passkeys are enrolled.

---

### Social login (OAuth2 / OIDC)

OAuth2 authorization code flow with PKCE against external identity providers.

**Supported providers:**

| Provider | Notes |
|---|---|
| Google | Discovery URL auto-populated |
| GitHub | Requires `user:email` scope |
| GitHub App | Uses installation tokens, alternative to OAuth |
| Apple | JWT private key auth — no `client_secret` |
| Microsoft | Requires `microsoft_tenant` (common / org / consumer / tenant ID) |
| Discord, Slack, LinkedIn, GitLab | Standard OIDC |
| Spotify, Twitch, Facebook, Twitter/X | |
| Amazon, Salesforce, Patreon | |
| Generic OIDC | Any spec-compliant provider |

Each provider uses a **Jsonnet claim mapper** — transforms provider claims into Aegion identity traits. Configured and tested live in the admin panel.

---

### SAML 2.0 enterprise SSO

Aegion as SAML Service Provider. Enterprise IdPs: Okta, Azure AD, ADFS, Google Workspace, OneLogin, PingFederate.

- Domain-based routing: user types `@corp.example.com` → auto-redirected to the right IdP
- Attribute mapping configurable per-provider in the admin panel
- SP metadata at `{public_url}/saml/metadata` for IdP configuration
- Test SSO flow button in admin panel — verify end-to-end before enabling for users

---

### Anonymous / guest sessions

Identities with `is_anonymous: true`. Accumulate state (cart, preferences, partial onboarding) before converting to a full account.

- Controlled by `enable_anonymous_signups` system config flag (off by default)
- Shorter session lifespan than authenticated sessions
- Stricter per-IP rate limits
- On signup/login: anonymous identity merges into real identity, anonymous session terminated

---

### API key authentication

Machine-to-machine without OAuth2. Keys are:
- Associated with an identity
- Stored as bcrypt hashes — never retrievable after creation
- Scoped to a permission subset at creation time
- Tracked: `last_used_at`, `expires_at`, `revoked`

Header: `Authorization: ApiKey <key>`

---

### Invitation-only registration

When `enable_invitations_only: true`, new accounts can only be created via admin-issued invites.

- Admin pastes list of emails → one-time tokens generated → delivered via courier
- User clicks invite link → registration flow with email pre-filled, token consumed
- Invite can pre-assign roles to the new identity

---

## Multi-factor authentication

### Authentication Assurance Levels (AAL)

| Level | Meaning | How achieved |
|---|---|---|
| AAL0 | Not authenticated | During an incomplete flow |
| AAL1 | Single factor | Password, social login, passkey, magic link, SAML |
| AAL2 | Two factors | AAL1 + any second factor |

Sessions carry their AAL level. Routes, OAuth2 consent flows, and proxy access rules can require a minimum AAL.

### TOTP

RFC 6238. QR code enrollment via settings flow. Codes verified against current ±1 time window (±30s clock skew).

### WebAuthn second factor

Hardware keys (YubiKey, etc.) as second factor after password. Distinct from passkeys (which are first-factor).

### SMS factor

OTP code via SMS as second factor. Uses the same SMS gateway as phone OTP auth.

### Backup codes

12 single-use codes (default, configurable), bcrypt-hashed. Generated on first MFA enrollment. Old set is fully invalidated when a new set is generated — no stockpiling. Grants AAL2, allows MFA re-enrollment.

### Trusted devices

After AAL2 completion, user can mark current device as trusted for N days. Subsequent logins from that device skip MFA but still receive AAL2 sessions. Device records are viewable and revocable from settings and admin panel.

---

## Security mechanisms

### Account enumeration protection

`account_enumeration_mitigation: true` (default) — all login failure responses return identical error text in identical response time, regardless of whether the email exists or the password is wrong.

### Brute force lockout

After `login_max_attempts` (default 10) consecutive failures per IP, the IP is locked out for `login_lockout_duration` (default 15m). Per-identity lockout tracked separately (repeated failures from different IPs). Works across replicas when Redis is available.

### Rate limiting

| Layer | Default |
|---|---|
| IP login attempts | 20/minute |
| IP registrations | 10/hour |
| Email delivery | 10/address/hour |
| SMS delivery | 10/number/hour |
| Anonymous signups | 30/IP/hour |

All configurable at runtime in system config.

### CSRF protection

Double-submit cookie pattern. X-CSRF-Token header required on all mutations. Token rotated after every successful mutation response.

### Password security (summary)

- Argon2id via Rust engine — deterministic memory zeroing, no GC involvement
- HIBP k-anonymity check — only 5-char SHA-1 prefix sent
- Password history — blocks last N reused passwords
- Similarity check — rejects passwords close to user's own identifiers
- Complexity rules — configurable at runtime

### Session fixation protection

Session ID regenerated after successful authentication — the pre-auth session token cannot be used to access the authenticated session.

### Refresh token family invalidation

Refresh tokens rotate on every use. If a token that was already used is presented again (replay), Aegion invalidates the **entire token family** — both the attacker's and the legitimate user's tokens. Both are forced to re-authenticate. This detects theft even when the attacker has already used the stolen token.

Configurable grace period (default 0, can be 2m for mobile apps) handles network race conditions without triggering false invalidation.

### JWT security

- RS256 or ES256 — no HS256 (symmetric keys unsuitable for distributed verification)
- Short access token TTL (default 15m) — limits blast radius of leaked tokens
- Audience (`aud`) and issuer (`iss`) validation on every token
- Signing keys rotated on schedule — zero-downtime via retiring key transition
- Revoked tokens tracked in Redis bloom filter for near-zero-cost checks

### Field-level encryption

Sensitive DB fields encrypted before storage via Rust crypto engine:
- TOTP secrets
- OIDC initial access/refresh tokens
- JWK private keys
- OTP codes in MFA challenges

Cipher: XChaCha20-Poly1305 (default) or AES-GCM. Key rotation: prepend new key to `secrets.cipher` array — old keys remain for decryption of existing records.

### Audit log

Append-only. Every state-changing operation recorded. Postgres row-level security can enforce the append-only constraint at the DB level.

Captured events include: every login success/failure (IP, user agent, method), session create/revoke, identity create/update/delete/ban, every admin action (before + after state), every permission change, every system config change, every key rotation, every OAuth2 token issuance and revocation.

### Impersonation visibility

Admin impersonation is intentionally visible to the target user. The impersonated session appears in their active sessions list labelled "Administrative session". The audit log records both the admin's identity ID and the target identity ID. Impersonation sessions are time-limited (default 1 hour).

Invisible impersonation is indistinguishable from account compromise. It should never be a feature.

### Secure email change

`secure_email_change: true` (default) — changing email sends confirmation to **both** old and new addresses. Change only completes when both confirm. Prevents an attacker who has gained account access from silently redirecting all email.

### CAPTCHA integration

`captcha_enabled: true` in system config enables CAPTCHA on login and registration. Supported: hCaptcha, Cloudflare Turnstile, Google reCAPTCHA v2/v3. Configured entirely at runtime — no restart required to change provider or rotate keys.

### IP allowlisting / blocklisting

Per-identity and global IP allow/block lists. An identity with an allowlist can only authenticate from those CIDR ranges. A global blocklist rejects matching IPs before any auth logic runs. Managed in admin panel.

### Suspicious login detection

Tracks login history per identity. Login from unusual IP, unusual geography, or unusual time triggers configurable response: step-up auth challenge (require MFA even without enrollment) or notification email. Thresholds configurable.

### Geographic access restrictions

Restrict authentication to specific countries or regions using geographic IP lookup.

**Configuration options:**
- Per-identity geo-fencing: restrict individual identities to specific countries
- Global geo-allowlist: only allow authentication from specified countries
- Global geo-blocklist: deny authentication from specified countries
- Exception rules: trusted IPs bypass geo restrictions

Geographic lookups use MaxMind GeoIP2 database (updated monthly). Restrictions are evaluated before authentication logic runs. Denied requests are logged with country code for audit purposes.

**Use cases:**
- Comply with data residency requirements
- Prevent access from high-risk jurisdictions
- Enforce location-based access policies for contractors
- Block regions with no legitimate user base

### Rate limit bypass for trusted sources

Configurable IP allowlist for rate limit exemptions:

```yaml
security:
  rate_limits:
    bypass_cidrs:
      - "10.0.0.0/8"      # Internal monitoring
      - "172.16.0.0/12"   # VPN
      - "192.168.1.0/24"  # Office network
```

Bypassed IPs are logged separately to maintain audit trail. This prevents legitimate internal traffic (monitoring, load testing, automated scripts) from triggering rate limits while maintaining protection against external attacks.

### Passwordless-only enforcement

Force passkey-only authentication by disabling password method entirely:

```yaml
password:
  enabled: false

passkeys:
  enabled: true
  require_for_new_users: true
```

When password module is disabled:
- Registration flows require passkey enrollment
- Password recovery flows are unavailable
- Existing password credentials remain in database but cannot be used
- Admin can still force-reset identities to passkey-only state
- Social/SAML login continues to work (external authentication)

This provides the highest security posture by eliminating password-based attacks entirely. Recommended for high-security environments where all users have compatible devices.
