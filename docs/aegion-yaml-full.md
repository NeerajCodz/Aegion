# aegion.yaml — Complete Configuration Reference

```yaml
# =============================================================================
#  aegion.yaml  —  Complete configuration reference
#
#  PHILOSOPHY
#  ──────────────────────────────────────────────────────────────────────────
#  This file is the ignition key, not the engine.
#
#  It contains only what Aegion needs BEFORE the database and modules exist:
#    • Which module images to pull and at what version
#    • Server binding, TLS, CORS
#    • Database connection
#    • Encryption secrets
#    • Bootstrap operator credentials
#    • SMTP connection credentials
#    • Module-level enable flags + their infrastructure-level startup defaults
#
#  Everything that is "runtime config" (OAuth2 clients, social login providers,
#  SAML metadata, policy namespaces, proxy rules, identity schemas, roles,
#  notification templates, webhook endpoints) lives in Postgres and is managed
#  through the admin panel at /aegion. No restart needed for those changes.
#
#  Each module block has:
#    enabled: true|false   → controls whether core pulls and starts that image
#    ...sub-config         → startup defaults seeded to DB on first boot only;
#                            editable at runtime via the admin panel
#
#  Changing enabled flags requires a core restart (core will pull/stop images).
#  Changing sub-config values in yaml has NO effect after first boot —
#  use the admin panel instead.
#
#  Environment variable override pattern:
#    database.url          →  AEGION_DATABASE_URL
#    secrets.cookie[0]     →  AEGION_SECRETS_COOKIE_0
#    oauth2.issuer         →  AEGION_OAUTH2_ISSUER
# =============================================================================


# ─────────────────────────────────────────────────────────────────────────────
#  MODULE IMAGE RESOLUTION
#  Controls which Docker images core pulls and what versions to use.
#  If module_versions is not set for a module, core uses 'latest'.
#  In production: ALWAYS pin versions. 'latest' means unpredictable updates.
#
#  All official images at the same version (e.g. 2.1.0) are tested together
#  and guaranteed compatible. Mixing versions from different releases is not
#  supported.
# ─────────────────────────────────────────────────────────────────────────────
module_versions:
  password:      "2.1.0"
  mfa:           "2.1.0"
  passkeys:      "2.1.0"
  magic_link:    "2.1.0"
  social:        "2.1.0"
  sso:           "2.1.0"
  oauth2:        "2.1.0"
  introspection: "2.1.0"
  policy:        "2.1.0"
  proxy:         "2.1.0"
  admin:         "2.1.0"
  cli:           "2.1.0"

# ─────────────────────────────────────────────────────────────────────────────
#  MODULE REGISTRY
#  Where core pulls module images from.
#  Defaults to Docker Hub (aegion/aegion-<module>:<version>).
#  Override for air-gapped environments or private registries.
# ─────────────────────────────────────────────────────────────────────────────
module_registry:
  base_url: ""          # Default: Docker Hub (aegion/aegion-*)
  # Example for private registry:
  # base_url: registry.internal.example.com/aegion
  pull_secret: ""       # Name of Docker credential secret (if auth required)
  pull_policy: "if-not-present"
  # if-not-present  → pull only if image not in local Docker cache (faster, default)
  # always          → always pull on startup (ensures latest pinned version)
  # never           → never pull; fail if image not present locally (air-gapped)


# ─────────────────────────────────────────────────────────────────────────────
#  SERVER
# ─────────────────────────────────────────────────────────────────────────────
server:
  port: 8080
  host: 0.0.0.0                   # Use 127.0.0.1 to restrict to localhost only

  tls:
    enabled: false
    cert:
      path: ""                    # /path/to/tls.crt  (PEM-encoded)
      base64: ""                  # Alternative: base64-encoded cert inline
    key:
      path: ""
      base64: ""
    min_version: TLS1.2           # TLS1.2 | TLS1.3

  cors:
    enabled: true
    allowed_origins:
      - http://localhost:3000
      # - https://app.yourdomain.com
      # - https://*.yourdomain.com
    allowed_methods:
      - GET
      - POST
      - PUT
      - PATCH
      - DELETE
      - OPTIONS
    allowed_headers:
      - Authorization
      - Content-Type
      - Cookie
      - X-Session-Token
      - X-CSRF-Token
    exposed_headers:
      - Content-Type
      - Set-Cookie
    allow_credentials: true
    max_age: 300                  # Preflight cache seconds

  # Request lifecycle timeouts
  request_timeout: 60s
  read_timeout:    30s
  write_timeout:   60s
  idle_timeout:    120s

  # Internal module network settings
  # Core manages the aegion_modules Docker network.
  # These settings control the internal network created for modules.
  internal_network:
    name: aegion_modules          # Docker network name
    subnet: 172.20.0.0/16        # Internal subnet for module containers
    # Module health check settings
    health_check_interval:  5s   # How often core polls module /health endpoints
    health_check_timeout:   2s   # Timeout for each health check call
    health_check_failures:  3    # Consecutive failures before marking instance unhealthy
    restart_on_failure:     true # Whether core attempts to restart failed containers
    startup_timeout:        30s  # How long core waits for a module to become ready


# ─────────────────────────────────────────────────────────────────────────────
#  DATABASE
#  Migrations run automatically on startup for all enabled modules.
#  Core runs core migrations first, then each enabled module's migrations
#  in dependency order. Already-applied migrations are never re-run.
# ─────────────────────────────────────────────────────────────────────────────
database:
  url: postgres://aegion:secret@postgres:5432/aegion?sslmode=disable
  # Supported DSN schemes:
  #   postgres://   — PostgreSQL (recommended for production)
  #   cockroach://  — CockroachDB (Postgres-compatible)
  #   sqlite://     — SQLite (development only, not for production)

  # Connection pool
  max_open_connections:     25
  max_idle_connections:     10
  connection_max_lifetime:  1h
  connection_max_idle_time: 5m

  # Migrator role connection (used only for DDL operations at startup)
  # If not set, core uses the main database.url with the assumption
  # that the provided role has DDL permissions.
  migrator_url: ""              # e.g. postgres://aegion_migrator:secret@postgres:5432/aegion

  # Set true to only run pending migrations then exit.
  # Use as a Kubernetes init container or job before starting the main pod.
  migrate_only: false

  # Schema prefix isolation (advanced — only needed for multi-tenant deployments
  # sharing a single Postgres instance)
  schema_prefix: ""             # Empty = use default public schema


# ─────────────────────────────────────────────────────────────────────────────
#  SECRETS
#  Rotate by prepending a new value — old values stay for decryption.
#  All values must be at least 32 characters.
#  NEVER commit real secrets to version control.
#  In production, inject via environment variables or a secrets manager.
# ─────────────────────────────────────────────────────────────────────────────
secrets:
  # Signs session cookies, CSRF tokens, and logout tokens.
  cookie:
    - CHANGE-ME-cookie-secret-at-least-32-chars
    # - previous-cookie-secret   # Keep for rotation window

  # AES-GCM / XChaCha20 encryption of sensitive database fields:
  # TOTP secrets, OIDC tokens, JWK private keys, social access tokens.
  cipher:
    - CHANGE-ME-cipher-secret-at-least-32-chars
    # - previous-cipher-secret

  # HMAC signing for internal session context headers injected by core
  # into forwarded requests to modules. Modules verify this signature.
  internal:
    - CHANGE-ME-internal-secret-at-least-32-chars

  # Fallback secret for HMAC signing not covered above.
  default:
    - CHANGE-ME-default-secret-at-least-32-chars

  # Encryption algorithm for field-level encryption.
  # xchacha20-poly1305 — nonce-extended, harder to misuse (recommended)
  # aes-gcm            — AES-256-GCM (standard alternative)
  cipher_algorithm: xchacha20-poly1305


# ─────────────────────────────────────────────────────────────────────────────
#  OPERATOR BOOTSTRAP
#  Created once on first boot if no operator identity exists.
#  After first boot these values are IGNORED ENTIRELY.
#  Change the password immediately after first login via /aegion → Settings.
# ─────────────────────────────────────────────────────────────────────────────
operator:
  username: admin
  email:    admin@example.com
  password: CHANGE-ME-before-production    # Minimum 12 characters


# ─────────────────────────────────────────────────────────────────────────────
#  PUBLIC BASE URL
#  Aegion's externally reachable origin. Used as:
#    - OIDC issuer claim (iss) in tokens
#    - SAML Service Provider entity ID and ACS base
#    - Redirect base for self-service flows
#    - JWKS endpoint advertised to OAuth2 clients
#    - Callback URLs for social providers
# ─────────────────────────────────────────────────────────────────────────────
public_url: http://localhost:8080
# Production: public_url: https://auth.yourdomain.com


# ─────────────────────────────────────────────────────────────────────────────
#  UI (SELF-SERVICE FLOW ENDPOINTS)
#  Where Aegion redirects browsers to render auth forms.
#  Your frontend owns these pages; it calls Aegion's API to drive each flow.
#  Seeded on first boot; editable at runtime via admin panel → Settings → Flows.
# ─────────────────────────────────────────────────────────────────────────────
ui:
  login:        http://localhost:3000/auth/login
  registration: http://localhost:3000/auth/registration
  settings:     http://localhost:3000/auth/settings
  recovery:     http://localhost:3000/auth/recovery
  verification: http://localhost:3000/auth/verification
  error:        http://localhost:3000/auth/error

  default_return_url: http://localhost:3000/

  # Whitelist of domains allowed as return_to values.
  # Aegion rejects any return_to that does not match — prevents open redirect.
  allowed_return_urls:
    - http://localhost:3000
    # - https://app.yourdomain.com


# ─────────────────────────────────────────────────────────────────────────────
#  SESSION
# ─────────────────────────────────────────────────────────────────────────────
session:
  lifespan: 24h

  cookie:
    name:      aegion_session
    domain:    ""          # Empty = use the request's Host header
    path:      /
    same_site: Lax         # Lax | Strict | None (None requires Secure + HTTPS)
    persistent: true       # false = session cookie (expires on browser close)

  whoami:
    # Minimum AAL required on /sessions/whoami.
    # aal1 = any single factor. aal2 = requires MFA if enrolled.
    required_aal: aal1

  # Tokenized session — signed JWT representation alongside the opaque token.
  # Useful for stateless verification without a DB lookup.
  tokenize:
    enabled: false
    ttl: 5m                # Keep short — JWTs cannot be revoked mid-life


# ─────────────────────────────────────────────────────────────────────────────
#  TOKENS (JWT signing key management)
# ─────────────────────────────────────────────────────────────────────────────
tokens:
  # RS256 = RSA 2048-bit  |  ES256 = ECDSA P-256 (smaller, faster verification)
  # The keypair is auto-generated on first boot if none exists.
  algorithm: RS256

  access_ttl:   15m    # Keep short — verified locally, no revocation call per request
  refresh_ttl:  720h   # 30 days. -1 = non-expiring (not recommended)
  id_token_ttl: 1h     # OIDC ID token TTL
  auth_code_ttl: 10m   # OAuth2 authorization code TTL

  # How long a retiring key stays in JWKS after rotation.
  # Downstream services caching the old key recover within this window.
  # Must be longer than the max cache TTL of any downstream service.
  key_rotation_grace_period: 24h

  # jwt    → self-contained, verified locally using JWKS (recommended)
  # opaque → random string, must call /oauth2/introspect per request
  access_token_strategy: jwt

  # Schedule for automatic key rotation.
  # Set to 0 to disable automatic rotation (rotate manually via admin panel).
  key_rotation_interval: 90d


# ─────────────────────────────────────────────────────────────────────────────
#  HASHERS (password hashing)
# ─────────────────────────────────────────────────────────────────────────────
hashers:
  # argon2id — memory-hard, OWASP recommended (default)
  # bcrypt   — widely supported alternative
  algorithm: argon2id

  argon2:
    memory:      131072     # KiB (128 MiB). OWASP minimum: 19456 (19 MiB)
    iterations:  1          # Time cost. Auto-tuned with expected_duration.
    parallelism: 4          # Match GOMAXPROCS (number of vCPUs)
    salt_length: 16         # Bytes
    key_length:  32         # Output bytes
    # Target hash duration. Core adjusts memory/iterations on startup to hit this.
    # Lower values increase brute-force risk. 500ms is a reasonable production value.
    expected_duration:  500ms
    # Max memory dedicated to concurrent hashing operations.
    dedicated_memory:   1GB

  bcrypt:
    cost: 12                # Min 10. Each +1 doubles hashing time.


# ─────────────────────────────────────────────────────────────────────────────
#  COURIER (notification delivery infrastructure)
#  SMTP credentials live here — they're needed before the DB exists.
#  Template content, retry policy, from-address, and SMS provider config
#  are managed at runtime via the admin panel.
# ─────────────────────────────────────────────────────────────────────────────
courier:
  # SMTP URL formats:
  #   smtp://user:pass@host:587/      → STARTTLS (most common for port 587)
  #   smtps://user:pass@host:465/     → Implicit TLS
  #   smtp://host:25/                 → No auth, no TLS (local only / dev)
  #   smtp://localhost:1025/          → MailHog / Mailpit (dev)
  smtp_url:     smtps://user:password@smtp.example.com:465/
  from_address: noreply@example.com
  from_name:    My Application

  worker:
    pull_count:  10      # Messages to process per tick
    pull_wait:   1s      # Interval between ticks
    max_retries: 5       # Attempts before marking a message 'abandoned'
    retry_backoff: exponential  # fixed | exponential

  sms:
    enabled:      false
    provider_url: ""     # e.g. Twilio: https://api.twilio.com/...


# ─────────────────────────────────────────────────────────────────────────────
#  SECURITY (cross-cutting security posture)
# ─────────────────────────────────────────────────────────────────────────────
security:
  # Return identical errors for unknown-email vs wrong-password.
  # Prevents user enumeration via response differences or timing attacks.
  account_enumeration_mitigation: true

  login:
    max_attempts:     10          # Consecutive failures before lockout
    lockout_duration: 15m
    ip_rate_limit:
      enabled:        true
      max_per_minute: 20

  registration:
    ip_rate_limit:
      enabled:      true
      max_per_hour: 10

  # HaveIBeenPwned password check (k-anonymity: only 5-char hash prefix sent)
  haveibeenpwned:
    enabled:              true
    host:                 api.pwnedpasswords.com
    max_breaches:         0       # 0 = any breach triggers rejection
    ignore_network_errors: true   # Don't block login if HIBP is unreachable

  # CAPTCHA on login and registration endpoints
  captcha:
    enabled:    false
    provider:   hcaptcha           # hcaptcha | turnstile | recaptcha_v2 | recaptcha_v3
    site_key:   ""
    secret_key: ""

  # Password history: block reuse of last N passwords
  password_history:
    enabled: true
    count:   5

  # Re-authentication required before sensitive operations
  # (email change, password change, MFA management)
  privileged_session_max_age: 15m

  # Suspicious login detection
  suspicious_login_detection:
    enabled: true
    # notify  → send email notification
    # step_up → require MFA challenge even if not enrolled
    action:  notify


# ─────────────────────────────────────────────────────────────────────────────
#  CACHE (optional — Redis)
#  Without Redis: everything falls back to Postgres (correct but slower).
#  With Redis: distributed session caching, rate-limit state sharing across
#  replicas, and a token revocation bloom filter.
# ─────────────────────────────────────────────────────────────────────────────
cache:
  enabled: false
  # URL formats:
  #   redis://:password@host:6379/0           plain
  #   rediss://:password@host:6380/0          TLS
  #   redis+sentinel://s1:26379,s2/master/0   HA sentinel
  url: redis://:@redis:6379/0

  pool:
    max_connections: 10
    min_idle:        2
    max_idle_time:   5m

  # Session record TTL in Redis. Must be <= session.lifespan.
  # Revocations may lag by up to this duration for cached sessions.
  session_ttl: 5m

  # Bloom filter for token revocation.
  # Reduces DB lookups to near-zero for valid tokens.
  # False positives (valid token treated as revoked) resolve on next request.
  revocation_bloom_filter:
    enabled:             true
    expected_items:      1000000   # Expected revoked token count
    false_positive_rate: 0.001     # 0.1%


# ─────────────────────────────────────────────────────────────────────────────
#  EVENTS (optional — external streaming)
#  Domain event streaming for audit tails, analytics, and webhooks.
#  The internal event bus (core) runs independently of this.
#  This section controls forwarding of internal events to external systems.
# ─────────────────────────────────────────────────────────────────────────────
events:
  enabled: false
  driver:  kafka            # kafka | redis_streams

  kafka:
    brokers:
      - kafka:9092
    tls:
      enabled:   false
      cert_path: ""
      key_path:  ""
      ca_path:   ""
    producer:
      compression:    snappy      # none | gzip | snappy | lz4 | zstd
      required_acks:  all         # none | leader | all
      retry_max:      3
      flush_frequency: 100ms

  redis_streams:
    url: ""                 # Leave empty to reuse cache.url when cache is enabled

  topics:
    users:    aegion.users
    sessions: aegion.sessions
    tokens:   aegion.tokens
    oauth2:   aegion.oauth2
    policy:   aegion.policy
    flows:    aegion.flows


# =============================================================================
#  MODULES
#  Each module block:
#    enabled: true|false   → whether core pulls and starts this image
#    sub-config            → startup defaults seeded to DB on first boot only
# =============================================================================


# ─────────────────────────────────────────────────────────────────────────────
#  PASSWORD AUTH
# ─────────────────────────────────────────────────────────────────────────────
password:
  enabled: true

  min_length: 8

  # Reject passwords too similar to the user's email or username.
  identifier_similarity_check: true

  haveibeenpwned:
    enabled:              true
    max_breaches:         0
    ignore_network_errors: true

  # Auto-issue a session after password registration.
  # Set false if email verification is required before login.
  auto_session_after_registration: true


# ─────────────────────────────────────────────────────────────────────────────
#  MFA (multi-factor authentication)
#  Image: aegion/aegion-mfa
#  Provides: TOTP, WebAuthn-as-2FA, SMS factor, backup codes, trusted devices.
#  Depends on: core, password (for AAL2 common path)
# ─────────────────────────────────────────────────────────────────────────────
mfa:
  enabled: false

  # Minimum session age for sensitive operations (password change, etc.)
  settings_privileged_session_max_age: 15m

  totp:
    enabled: true
    issuer: "My Application"
    # Time window for TOTP validation.
    # 1 = accept current window ± 1 (±30s clock skew)
    window: 1

  lookup_secret:
    enabled:    true
    code_count: 12     # Number of backup codes generated per enrollment
    code_length: 8     # Characters per code

  webauthn_second_factor:
    enabled: false

  trusted_devices:
    enabled:    true
    duration:   30d    # How long a trusted device remains trusted


# ─────────────────────────────────────────────────────────────────────────────
#  PASSKEYS (WebAuthn first-factor passwordless)
#  Image: aegion/aegion-passkeys
#  Depends on: core
# ─────────────────────────────────────────────────────────────────────────────
passkeys:
  enabled: false

  rp:
    # CRITICAL: cannot be changed after passkeys are enrolled.
    id:           localhost
    display_name: "My Application"
    origins:
      - http://localhost:3000

  passwordless:       true
  user_verification:  preferred    # preferred | required | discouraged


# ─────────────────────────────────────────────────────────────────────────────
#  MAGIC LINK / CODE AUTH
#  Image: aegion/aegion-magic-link
#  Depends on: core, courier (in core)
# ─────────────────────────────────────────────────────────────────────────────
magic_link:
  enabled: false

  strategy:            code      # code | link
  passwordless_enabled: false
  mfa_enabled:          false
  lifespan:             15m
  code_length:          6
  code_reuse:           false
  max_requests_per_flow: 5
  default_channel:      email    # email | sms | auto


# ─────────────────────────────────────────────────────────────────────────────
#  SOCIAL LOGIN
#  Image: aegion/aegion-social
#  Depends on: core, courier (in core)
#  Provider instances (client IDs, secrets) are configured at runtime
#  via the admin panel — not in this file.
# ─────────────────────────────────────────────────────────────────────────────
social:
  enabled: false

  auto_session_after_registration:    true
  default_pkce_mode:                  auto       # auto | force | never
  default_claims_source:              id_token   # id_token | userinfo
  link_existing_account_on_email_match: false
  default_mapper_url:                 ""


# ─────────────────────────────────────────────────────────────────────────────
#  SSO / SAML + SCIM
#  Image: aegion/aegion-sso
#  Depends on: core, courier (in core)
#  IdP connections are configured at runtime via the admin panel.
# ─────────────────────────────────────────────────────────────────────────────
sso:
  enabled: false

  saml:
    enabled: true
    sp:
      entity_id:  ""   # Defaults to public_url + /saml/metadata
      certificate:
        path:   ""
        base64: ""
      private_key:
        path:   ""
        base64: ""
      want_assertions_signed:    true
      want_assertions_encrypted: false
    metadata_refresh_interval: 24h

  scim:
    enabled: true
    # Base URL for the SCIM 2.0 endpoint exposed to IdPs:
    # {public_url}/scim/v2
    # Individual connection tokens are created in the admin panel.


# ─────────────────────────────────────────────────────────────────────────────
#  OAUTH2 / OIDC SERVER
#  Image: aegion/aegion-oauth2
#  Depends on: core
#  OAuth2 clients are registered at runtime via the admin panel.
# ─────────────────────────────────────────────────────────────────────────────
oauth2:
  enabled: false

  issuer: http://localhost:8080    # Must match public_url in most deployments

  urls:
    login:               http://localhost:3000/oauth2/login
    consent:             http://localhost:3000/oauth2/consent
    logout:              http://localhost:3000/oauth2/logout
    error:               http://localhost:3000/oauth2/error
    post_logout_redirect: http://localhost:3000/

  scopes:
    - openid
    - offline
    - offline_access
    - profile
    - email
    - address
    - phone

  pkce:
    enforced_for_public_clients: true
    enforced:                    false    # RFC 9700 — enforce for all clients

  refresh_token:
    rotation_enabled: true
    grace_period:     0s    # 0 = strict. Use 2m for mobile apps.

  token_hook:
    enabled: false
    url:     ""
    method:  POST
    timeout: 2s
    auth:
      type:   ""    # api_key | basic_auth
      config: {}

  subject_identifiers:
    supported_types:
      - public
      # - pairwise
    pairwise:
      salt: ""      # Required if pairwise in supported_types. Never change after use.

  oidc_discovery:
    supported_claims:
      - sub
      - iss
      - aud
      - iat
      - exp
      - email
      - email_verified
      - name
      - given_name
      - family_name
      - nickname
      - picture
      - website
      - phone_number
      - phone_number_verified
      - birthdate
      - locale
      - zoneinfo
      - updated_at

  jwks:
    broadcast_keys:
      - aegion.openid.id-token
      # - aegion.jwt.access-token   # Uncomment if using JWT access tokens

  access_token_strategy:     jwt
  allowed_top_level_claims:  []   # Empty = include all claims granted at consent

  device_authorization:
    enabled:                false
    token_polling_interval: 5s

  jwt_bearer:
    enabled: false

  dynamic_client_registration:
    enabled:       false
    default_scope: []

  expose_internal_errors: false   # NEVER enable in production


# ─────────────────────────────────────────────────────────────────────────────
#  INTROSPECTION (RFC 7662)
#  Image: aegion/aegion-introspect
#  Depends on: oauth2
# ─────────────────────────────────────────────────────────────────────────────
introspection:
  enabled: false

  # Who can call /oauth2/introspect:
  # authenticated_clients → only registered OAuth2 clients with credentials
  # any                   → any caller (leaks token metadata — not recommended)
  access:    authenticated_clients

  # Cache introspection results in Redis (requires cache.enabled).
  # Delays revocation propagation by this window.
  cache_ttl: 0s     # 0 = no caching


# ─────────────────────────────────────────────────────────────────────────────
#  POLICY ENGINE (RBAC + ABAC + ReBAC)
#  Image: aegion/aegion-policy
#  Depends on: core
# ─────────────────────────────────────────────────────────────────────────────
policy:
  enabled: false

  default_model: rbac    # rbac | abac | rebac

  rbac:
    enabled: true
    seed_roles:
      - name: admin
        description: Full access to all resources
      - name: user
        description: Standard authenticated user
      - name: viewer
        description: Read-only access

  abac:
    enabled: false
    # Rules are defined in the admin panel as CEL expressions.
    # CEL evaluation context includes:
    #   subject.id, subject.roles[], subject.traits.*, subject.metadata.*
    #   resource.id, resource.type, resource.owner_id, resource.metadata.*
    #   action (string)
    #   request.context.ip, request.context.time, request.context.tenant_id

  rebac:
    enabled: false
    seed_namespaces:
      - name: files
      - name: organizations
      - name: projects
    # Tuple expansion cache TTL (in-process cache in aegion-policy container)
    expansion_cache_ttl: 60s
    # Maximum tuple traversal depth before aborting (cycle protection)
    max_depth: 20


# ─────────────────────────────────────────────────────────────────────────────
#  PROXY (identity-aware reverse proxy)
#  Image: aegion/aegion-proxy
#  Depends on: core, policy (recommended)
#  Access rules are configured at runtime via the admin panel.
# ─────────────────────────────────────────────────────────────────────────────
proxy:
  enabled: false

  upstream_timeout:  30s
  preserve_host:     true

  # Circuit breaker for upstream services
  circuit_breaker:
    enabled:               true
    failure_threshold:     5      # Consecutive failures before opening circuit
    success_threshold:     2      # Successes needed to close a half-open circuit
    timeout:               30s    # How long circuit stays open before half-open

  authenticators:
    anonymous:
      enabled: true
    bearer_token:
      enabled: true
      config:
        jwks_urls:
          - http://localhost:8080/.well-known/jwks.json
        target_audience: []
        scope_strategy:  none    # none | hierarchy | wildcard
        # Cache JWKS keys to avoid re-fetching on every request
        jwks_cache_ttl:  5m
    cookie_session:
      enabled: true
      config:
        check_session_url: http://localhost:8080/sessions/whoami
        preserve_path:     true
        only:
          - aegion_session
        # Cache session lookups to reduce core load
        session_cache_ttl: 30s
    noop:
      enabled: true

  authorizers:
    allow:
      enabled: true
    deny:
      enabled: true
    policy_engine:
      enabled: false
      config:
        base_url: http://localhost:8080    # Resolved internally via service registry
    remote_json:
      enabled: true
    cel:
      enabled: false

  mutators:
    noop:
      enabled: true
    header:
      enabled: true
      config:
        # Headers injected from resolved session claims.
        # All inbound X-Aegion-* and X-User-* headers are stripped before injection.
        headers:
          X-User-Id:     "{{ .Subject }}"
          X-User-Email:  "{{ .Extra.email }}"
          X-User-Roles:  "{{ .Extra.roles | join \",\" }}"
    cookie:
      enabled: false
    id_token:
      enabled: false
      config:
        issuer_url: http://localhost:8080
        jwks_url:   http://localhost:8080/.well-known/jwks.json
        ttl:        60s
        claims: |
          {
            "sub":     "{{ print .Subject }}",
            "session": {{ .Extra | toJson }}
          }
    hydrator:
      enabled: false
      config:
        api:
          url: ""
          timeout: 2s
          retry:
            give_up_after: 2s
            max_delay:     100ms

  errors:
    fallback: json
    handlers:
      unauthorized:
        handler: json
        config:
          verbose: false     # Never expose internal details
      forbidden:
        handler: json
      not_found:
        handler: json


# ─────────────────────────────────────────────────────────────────────────────
#  ADMIN DASHBOARD
#  Image: aegion/aegion-admin
#  Depends on: core, policy
# ─────────────────────────────────────────────────────────────────────────────
admin:
  enabled: true

  path:             /aegion
  session_lifespan: 8h

  require_mfa:  false
  required_aal: aal1

  cors:
    enabled: true
    allowed_origins:
      - http://localhost:3000


# ─────────────────────────────────────────────────────────────────────────────
#  OBSERVABILITY
# ─────────────────────────────────────────────────────────────────────────────
log:
  level:  info         # panic | fatal | error | warn | info | debug | trace
  format: json         # json (production) | text (development)
  # NEVER enable in production — logs raw session tokens and passwords
  leak_sensitive_values: false

tracing:
  provider:     ""     # otel | jaeger | zipkin | datadog | "" (disabled)
  service_name: aegion

  otel:
    server_url:     http://localhost:4318
    insecure:       true
    sampling_ratio: 1.0    # 1.0 = 100%, 0.1 = 10%

  jaeger:
    local_agent_address: localhost:6831

  zipkin:
    server_url: http://localhost:9411/api/v2/spans

  datadog:
    use_128_bit_trace_id: true

metrics:
  enabled: true
  path:    /metrics    # Prometheus scrape endpoint

profiling:
  enabled: false
  path:    /debug/pprof    # NEVER expose to public internet


# ─────────────────────────────────────────────────────────────────────────────
#  FEATURE FLAGS
# ─────────────────────────────────────────────────────────────────────────────
feature_flags:
  # Cache /sessions/whoami responses briefly to reduce DB load.
  # Caveat: revoked sessions may appear valid for up to max_age.
  cacheable_sessions:         false
  cacheable_sessions_max_age: 0s

  # Use the continue_with flow transition model (Kratos v1.1+ pattern).
  # Recommended for new deployments.
  use_continue_with_transitions: true

  # Lazy-load identity credentials and addresses only when needed.
  # Reduces DB read overhead for high-volume session validation paths.
  lazy_loading: true

  # Enable module auto-restart on health failure.
  # Core will attempt to restart failed module containers automatically.
  module_auto_restart: true

  # Emit platform-level metrics per module (request rate, error rate, latency).
  per_module_metrics: true
```
