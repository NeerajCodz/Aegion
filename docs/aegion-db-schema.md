# Aegion — Database Schema Reference

This document describes the complete Postgres schema for all Aegion modules. Each module owns its own tables, namespaced by prefix. Core tables are the foundation; module tables reference them via foreign keys but are never written to by modules other than the owner.

All tables use UUIDs as primary keys. All timestamps are `timestamptz` stored in UTC. Soft-delete patterns are used where audit trails matter; hard-delete is used only for ephemeral records.

---

## Schema conventions

```sql
-- Standard primary key
id UUID PRIMARY KEY DEFAULT gen_random_uuid()

-- Standard timestamps on all non-ephemeral tables
created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()

-- Soft-delete (used on identity-linked records)
deleted_at  TIMESTAMPTZ

-- Module table prefix convention
core_*   → core           pwd_*  → aegion-password
mfa_*    → aegion-mfa     pk_*   → aegion-passkeys
ml_*     → aegion-magic-link     soc_* → aegion-social
sso_*    → aegion-sso     oa2_*  → aegion-oauth2
pol_*    → aegion-policy  prx_*  → aegion-proxy
adm_*    → aegion-admin
```

---

## Core tables

### `core_identities`

The canonical identity record. Every person or machine has exactly one row.

```sql
CREATE TABLE core_identities (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    schema_id       UUID NOT NULL REFERENCES core_identity_schemas(id),
    traits          JSONB NOT NULL DEFAULT '{}',
    state           TEXT NOT NULL DEFAULT 'active',   -- active | inactive | banned
    is_anonymous    BOOLEAN NOT NULL DEFAULT FALSE,
    metadata_public JSONB NOT NULL DEFAULT '{}',
    metadata_admin  JSONB NOT NULL DEFAULT '{}',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at      TIMESTAMPTZ
);

CREATE INDEX idx_core_identities_state  ON core_identities(state) WHERE deleted_at IS NULL;
CREATE INDEX idx_core_identities_traits ON core_identities USING GIN(traits);
CREATE INDEX idx_core_identities_schema ON core_identities(schema_id);
```

### `core_identity_schemas`

Defines the `traits` shape per identity type. Multiple schemas can coexist.

```sql
CREATE TABLE core_identity_schemas (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name       TEXT NOT NULL UNIQUE,
    is_default BOOLEAN NOT NULL DEFAULT FALSE,
    schema     JSONB NOT NULL,   -- JSON Schema document
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
```

### `core_identity_addresses`

Verifiable contact addresses (email, phone) linked to an identity. Multiple per identity; at most one primary per type.

```sql
CREATE TABLE core_identity_addresses (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    identity_id UUID NOT NULL REFERENCES core_identities(id) ON DELETE CASCADE,
    type        TEXT NOT NULL,                -- email | phone
    value       TEXT NOT NULL,
    is_primary  BOOLEAN NOT NULL DEFAULT FALSE,
    verified    BOOLEAN NOT NULL DEFAULT FALSE,
    verified_at TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX idx_core_addr_value_type
    ON core_identity_addresses(type, value) WHERE verified = TRUE;
CREATE INDEX idx_core_addr_identity
    ON core_identity_addresses(identity_id);
```

### `core_sessions`

Active sessions. Sessions are the canonical authentication artifact for browser-based flows.

```sql
CREATE TABLE core_sessions (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    token            TEXT NOT NULL UNIQUE,        -- SHA-256 hash of opaque session token
    identity_id      UUID NOT NULL REFERENCES core_identities(id) ON DELETE CASCADE,
    aal              TEXT NOT NULL DEFAULT 'aal1', -- aal1 | aal2
    issued_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at       TIMESTAMPTZ NOT NULL,
    authenticated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    logout_token     TEXT UNIQUE,
    devices          JSONB NOT NULL DEFAULT '[]', -- [{ ip, user_agent, last_seen_at }]
    active           BOOLEAN NOT NULL DEFAULT TRUE,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_core_sessions_identity ON core_sessions(identity_id) WHERE active = TRUE;
CREATE INDEX idx_core_sessions_expires  ON core_sessions(expires_at)  WHERE active = TRUE;
```

### `core_session_auth_methods`

Which authentication methods contributed to a session's AAL. Multiple rows per session when step-up has occurred.

```sql
CREATE TABLE core_session_auth_methods (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    session_id      UUID NOT NULL REFERENCES core_sessions(id) ON DELETE CASCADE,
    method          TEXT NOT NULL,    -- password | totp | webauthn | magic_link | social | saml | passkey
    aal_contributed TEXT NOT NULL,    -- aal1 | aal2
    completed_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_core_sam_session ON core_session_auth_methods(session_id);
```

### `core_flows`

Self-service flow state. Tracks a user through a multi-step operation.

```sql
CREATE TABLE core_flows (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    type        TEXT NOT NULL,   -- login | registration | recovery | settings | verification
    state       TEXT NOT NULL,   -- active | complete | failed
    identity_id UUID REFERENCES core_identities(id) ON DELETE CASCADE,
    session_id  UUID REFERENCES core_sessions(id) ON DELETE SET NULL,
    request_url TEXT NOT NULL,
    return_to   TEXT,
    ui          JSONB NOT NULL,  -- form nodes, messages, action URLs
    context     JSONB NOT NULL DEFAULT '{}',
    issued_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at  TIMESTAMPTZ NOT NULL,
    csrf_token  TEXT NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_core_flows_type_state ON core_flows(type, state);
CREATE INDEX idx_core_flows_expires    ON core_flows(expires_at) WHERE state = 'active';
CREATE INDEX idx_core_flows_identity   ON core_flows(identity_id) WHERE identity_id IS NOT NULL;
```

### `core_continuity_containers`

Short-lived context containers maintaining state across redirects.

```sql
CREATE TABLE core_continuity_containers (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name        TEXT NOT NULL,
    identity_id UUID REFERENCES core_identities(id) ON DELETE CASCADE,
    payload     JSONB NOT NULL DEFAULT '{}',
    expires_at  TIMESTAMPTZ NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_core_cc_expires ON core_continuity_containers(expires_at);
```

### `core_courier_messages`

Outbound email and SMS messages queued for delivery by core's courier worker.

```sql
CREATE TABLE core_courier_messages (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    type            TEXT NOT NULL,    -- email | sms
    status          TEXT NOT NULL DEFAULT 'queued',
                                      -- queued | processing | sent | abandoned
    recipient       TEXT NOT NULL,
    subject         TEXT,
    body            TEXT NOT NULL,
    template_id     TEXT,
    template_data   JSONB,
    send_count      INT NOT NULL DEFAULT 0,
    last_error      TEXT,
    idempotency_key TEXT UNIQUE,
    send_after      TIMESTAMPTZ,
    sent_at         TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_core_courier_status
    ON core_courier_messages(status, send_after)
    WHERE status IN ('queued', 'processing');
```

### `core_audit_events`

Append-only audit log. Row-level security enforces no UPDATE or DELETE at the database level.

```sql
CREATE TABLE core_audit_events (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    event_type   TEXT NOT NULL,
    actor_id     UUID,                -- NULL for system events
    actor_type   TEXT,                -- user | admin | system | module
    target_type  TEXT,
    target_id    TEXT,
    action       TEXT NOT NULL,
    outcome      TEXT NOT NULL,       -- success | failure
    before_state JSONB,
    after_state  JSONB,
    metadata     JSONB NOT NULL DEFAULT '{}',
    occurred_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

ALTER TABLE core_audit_events ENABLE ROW LEVEL SECURITY;
CREATE POLICY audit_insert_only ON core_audit_events FOR INSERT WITH CHECK (TRUE);
-- No UPDATE or DELETE policy = prohibited for all application roles

CREATE INDEX idx_core_audit_actor  ON core_audit_events(actor_id, occurred_at DESC)
    WHERE actor_id IS NOT NULL;
CREATE INDEX idx_core_audit_target ON core_audit_events(target_type, target_id, occurred_at DESC);
CREATE INDEX idx_core_audit_type   ON core_audit_events(event_type, occurred_at DESC);
```

### `core_event_bus_events` and `core_event_bus_deliveries`

Persistent backing store for the internal event bus — at-least-once delivery across module instances.

```sql
CREATE TABLE core_event_bus_events (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    event_type    TEXT NOT NULL,
    source_module TEXT NOT NULL,
    entity_type   TEXT,
    entity_id     TEXT,
    payload       JSONB NOT NULL,
    metadata      JSONB NOT NULL DEFAULT '{}',
    occurred_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE core_event_bus_deliveries (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    event_id      UUID NOT NULL REFERENCES core_event_bus_events(id),
    subscriber    TEXT NOT NULL,    -- module name
    status        TEXT NOT NULL DEFAULT 'pending',
                                    -- pending | delivered | failed | dead_lettered
    attempt_count INT NOT NULL DEFAULT 0,
    last_error    TEXT,
    next_retry_at TIMESTAMPTZ,
    delivered_at  TIMESTAMPTZ,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Drives the redelivery worker — must stay healthy
CREATE INDEX idx_evbus_pending
    ON core_event_bus_deliveries(subscriber, next_retry_at)
    WHERE status IN ('pending', 'failed');
CREATE INDEX idx_evbus_events_occurred
    ON core_event_bus_events(occurred_at DESC);
```

### `core_signing_keys`

Cryptographic signing keys. Private key bytes encrypted using the cipher secret before storage.

```sql
CREATE TABLE core_signing_keys (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    key_id       TEXT NOT NULL UNIQUE,   -- kid in JWT header
    use          TEXT NOT NULL,          -- sig | enc
    algorithm    TEXT NOT NULL,          -- RS256 | ES256 | RS512
    status       TEXT NOT NULL,          -- active | retiring | expired
    public_key   BYTEA NOT NULL,         -- PEM, unencrypted
    private_key  BYTEA NOT NULL,         -- PEM, AES-GCM or XChaCha20 encrypted
    activated_at TIMESTAMPTZ,
    retired_at   TIMESTAMPTZ,
    expired_at   TIMESTAMPTZ,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_core_keys_status ON core_signing_keys(status, use);
```

### `core_system_config`

Runtime configuration values managed via admin panel. Overrides yaml defaults after first boot.

```sql
CREATE TABLE core_system_config (
    key        TEXT PRIMARY KEY,
    value      JSONB NOT NULL,
    updated_by UUID REFERENCES core_identities(id),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
```

---

## Password module (`pwd_*`)

### `pwd_credentials`

```sql
CREATE TABLE pwd_credentials (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    identity_id UUID NOT NULL REFERENCES core_identities(id) ON DELETE CASCADE,
    identifier  TEXT NOT NULL,   -- email or username used to log in
    hashed_pw   TEXT NOT NULL,   -- Argon2id hash (includes algorithm + param prefix)
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX idx_pwd_cred_identifier ON pwd_credentials(identifier);
CREATE INDEX        idx_pwd_cred_identity   ON pwd_credentials(identity_id);
```

### `pwd_history`

```sql
CREATE TABLE pwd_history (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    identity_id UUID NOT NULL REFERENCES core_identities(id) ON DELETE CASCADE,
    hashed_pw   TEXT NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_pwd_history_identity ON pwd_history(identity_id, created_at DESC);
```

---

## MFA module (`mfa_*`)

### `mfa_credentials`

One row per enrolled MFA factor per identity.

```sql
CREATE TABLE mfa_credentials (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    identity_id  UUID NOT NULL REFERENCES core_identities(id) ON DELETE CASCADE,
    type         TEXT NOT NULL,          -- totp | webauthn | sms | backup_codes
    name         TEXT,                   -- user-given name
    config       BYTEA NOT NULL,         -- encrypted: TOTP secret / WebAuthn cred / backup hashes
    is_primary   BOOLEAN NOT NULL DEFAULT FALSE,
    last_used_at TIMESTAMPTZ,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_mfa_cred_identity ON mfa_credentials(identity_id, type);
```

### `mfa_trusted_devices`

```sql
CREATE TABLE mfa_trusted_devices (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    identity_id  UUID NOT NULL REFERENCES core_identities(id) ON DELETE CASCADE,
    device_token TEXT NOT NULL UNIQUE,
    name         TEXT,
    user_agent   TEXT,
    ip_address   TEXT,
    trusted_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at   TIMESTAMPTZ NOT NULL,
    last_used_at TIMESTAMPTZ
);

CREATE INDEX idx_mfa_trusted_identity ON mfa_trusted_devices(identity_id, expires_at);
```

---

## Passkeys module (`pk_*`)

### `pk_credentials`

WebAuthn public key credentials.

```sql
CREATE TABLE pk_credentials (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    identity_id      UUID NOT NULL REFERENCES core_identities(id) ON DELETE CASCADE,
    credential_id    BYTEA NOT NULL UNIQUE,
    public_key       BYTEA NOT NULL,           -- COSE-encoded
    aaguid           UUID,
    sign_count       BIGINT NOT NULL DEFAULT 0,
    attestation_type TEXT,
    transports       TEXT[],                   -- internal | usb | nfc | ble | hybrid
    user_verified    BOOLEAN NOT NULL DEFAULT FALSE,
    backup_eligible  BOOLEAN NOT NULL DEFAULT FALSE,
    backup_state     BOOLEAN NOT NULL DEFAULT FALSE,
    name             TEXT,
    last_used_at     TIMESTAMPTZ,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_pk_cred_identity ON pk_credentials(identity_id);
```

---

## Magic link module (`ml_*`)

### `ml_codes`

OTP codes and magic link tokens.

```sql
CREATE TABLE ml_codes (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    flow_id     UUID NOT NULL REFERENCES core_flows(id) ON DELETE CASCADE,
    identity_id UUID REFERENCES core_identities(id) ON DELETE CASCADE,
    type        TEXT NOT NULL,       -- otp | magic_link
    code        TEXT NOT NULL,       -- hashed 6-digit OTP or URL token
    channel     TEXT NOT NULL,       -- email | sms
    address     TEXT NOT NULL,
    used        BOOLEAN NOT NULL DEFAULT FALSE,
    used_at     TIMESTAMPTZ,
    expires_at  TIMESTAMPTZ NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_ml_codes_flow    ON ml_codes(flow_id);
CREATE INDEX idx_ml_codes_expires ON ml_codes(expires_at) WHERE used = FALSE;
```

---

## Social module (`soc_*`)

### `soc_connections`

```sql
CREATE TABLE soc_connections (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    identity_id   UUID NOT NULL REFERENCES core_identities(id) ON DELETE CASCADE,
    provider      TEXT NOT NULL,
    subject       TEXT NOT NULL,       -- provider sub claim
    access_token  BYTEA,               -- encrypted
    refresh_token BYTEA,               -- encrypted
    token_expiry  TIMESTAMPTZ,
    raw_claims    JSONB,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX idx_soc_conn_provider_sub ON soc_connections(provider, subject);
CREATE INDEX        idx_soc_conn_identity     ON soc_connections(identity_id);
```

### `soc_state`

```sql
CREATE TABLE soc_state (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    flow_id       UUID NOT NULL REFERENCES core_flows(id) ON DELETE CASCADE,
    provider      TEXT NOT NULL,
    state         TEXT NOT NULL UNIQUE,
    pkce_verifier TEXT,
    nonce         TEXT,
    expires_at    TIMESTAMPTZ NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_soc_state_expires ON soc_state(expires_at);
```

---

## SSO module (`sso_*`)

### `sso_saml_providers`

```sql
CREATE TABLE sso_saml_providers (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name                TEXT NOT NULL,
    label               TEXT NOT NULL,
    enabled             BOOLEAN NOT NULL DEFAULT FALSE,
    metadata_url        TEXT,
    metadata_xml        TEXT,
    metadata_updated_at TIMESTAMPTZ,
    entity_id           TEXT NOT NULL,
    sso_url             TEXT NOT NULL,
    slo_url             TEXT,
    certificate         TEXT NOT NULL,
    attribute_map       JSONB NOT NULL DEFAULT '{}',
    domain_routing      TEXT[],
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_sso_saml_domains
    ON sso_saml_providers USING GIN(domain_routing) WHERE enabled = TRUE;
```

### `sso_saml_sessions`

```sql
CREATE TABLE sso_saml_sessions (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    flow_id     UUID NOT NULL REFERENCES core_flows(id) ON DELETE CASCADE,
    provider_id UUID NOT NULL REFERENCES sso_saml_providers(id),
    request_id  TEXT NOT NULL UNIQUE,
    relay_state TEXT NOT NULL UNIQUE,
    expires_at  TIMESTAMPTZ NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_sso_saml_sess_relay   ON sso_saml_sessions(relay_state);
CREATE INDEX idx_sso_saml_sess_expires ON sso_saml_sessions(expires_at);
```

### `sso_scim_connections`

```sql
CREATE TABLE sso_scim_connections (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name           TEXT NOT NULL,
    enabled        BOOLEAN NOT NULL DEFAULT FALSE,
    token_hash     TEXT NOT NULL UNIQUE,
    schema_version TEXT NOT NULL DEFAULT '2.0',
    attribute_map  JSONB NOT NULL DEFAULT '{}',
    conflict_mode  TEXT NOT NULL DEFAULT 'reject',  -- reject | merge | overwrite
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
```

### `sso_scim_log`

```sql
CREATE TABLE sso_scim_log (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    connection_id UUID NOT NULL REFERENCES sso_scim_connections(id),
    operation     TEXT NOT NULL,   -- create | update | deactivate | bulk
    status        TEXT NOT NULL,   -- success | failure | conflict
    scim_id       TEXT,
    identity_id   UUID REFERENCES core_identities(id),
    request_body  JSONB,
    response_body JSONB,
    error         TEXT,
    occurred_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_scim_log_conn     ON sso_scim_log(connection_id, occurred_at DESC);
CREATE INDEX idx_scim_log_identity ON sso_scim_log(identity_id) WHERE identity_id IS NOT NULL;
```

---

## OAuth2 module (`oa2_*`)

### `oa2_clients`

```sql
CREATE TABLE oa2_clients (
    id                           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    client_id                    TEXT NOT NULL UNIQUE,
    client_secret_hash           TEXT,                   -- NULL = public client
    name                         TEXT NOT NULL,
    description                  TEXT,
    logo_uri                     TEXT,
    redirect_uris                TEXT[] NOT NULL DEFAULT '{}',
    post_logout_redirect_uris    TEXT[] NOT NULL DEFAULT '{}',
    grant_types                  TEXT[] NOT NULL DEFAULT '{}',
    response_types               TEXT[] NOT NULL DEFAULT '{}',
    scopes                       TEXT[] NOT NULL DEFAULT '{}',
    token_endpoint_auth_method   TEXT NOT NULL DEFAULT 'client_secret_post',
    access_token_strategy        TEXT NOT NULL DEFAULT 'jwt',
    skip_consent                 BOOLEAN NOT NULL DEFAULT FALSE,
    cors_origins                 TEXT[] NOT NULL DEFAULT '{}',
    frontchannel_logout_uri      TEXT,
    backchannel_logout_uri       TEXT,
    access_token_ttl_override    INTERVAL,
    refresh_token_ttl_override   INTERVAL,
    metadata                     JSONB NOT NULL DEFAULT '{}',
    enabled                      BOOLEAN NOT NULL DEFAULT TRUE,
    created_at                   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at                   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
```

### `oa2_auth_codes`

Short-lived; deleted on use.

```sql
CREATE TABLE oa2_auth_codes (
    id                    UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    code                  TEXT NOT NULL UNIQUE,   -- hashed
    client_id             TEXT NOT NULL REFERENCES oa2_clients(client_id),
    subject               UUID NOT NULL REFERENCES core_identities(id),
    session_id            UUID NOT NULL REFERENCES core_sessions(id),
    redirect_uri          TEXT NOT NULL,
    scopes                TEXT[] NOT NULL,
    nonce                 TEXT,
    code_challenge        TEXT,
    code_challenge_method TEXT,
    auth_time             TIMESTAMPTZ NOT NULL,
    expires_at            TIMESTAMPTZ NOT NULL,
    used                  BOOLEAN NOT NULL DEFAULT FALSE,
    created_at            TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_oa2_codes_expires ON oa2_auth_codes(expires_at) WHERE used = FALSE;
```

### `oa2_access_tokens`

```sql
CREATE TABLE oa2_access_tokens (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    token_hash TEXT UNIQUE,       -- hashed for opaque; NULL for JWT (jti used instead)
    client_id  TEXT NOT NULL REFERENCES oa2_clients(client_id),
    subject    UUID NOT NULL REFERENCES core_identities(id),
    session_id UUID REFERENCES core_sessions(id),
    scopes     TEXT[] NOT NULL,
    strategy   TEXT NOT NULL,     -- jwt | opaque
    jti        TEXT UNIQUE,       -- JWT ID for JWT tokens
    active     BOOLEAN NOT NULL DEFAULT TRUE,
    issued_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX idx_oa2_at_subject ON oa2_access_tokens(subject, active);
CREATE INDEX idx_oa2_at_expires ON oa2_access_tokens(expires_at) WHERE active = TRUE;
CREATE INDEX idx_oa2_at_jti     ON oa2_access_tokens(jti) WHERE jti IS NOT NULL;
```

### `oa2_refresh_tokens`

Family tracking enables single-query family invalidation on replay detection.

```sql
CREATE TABLE oa2_refresh_tokens (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    token_hash    TEXT NOT NULL UNIQUE,
    family_id     UUID NOT NULL,   -- shared across the entire rotation chain
    client_id     TEXT NOT NULL REFERENCES oa2_clients(client_id),
    subject       UUID NOT NULL REFERENCES core_identities(id),
    session_id    UUID REFERENCES core_sessions(id),
    scopes        TEXT[] NOT NULL,
    active        BOOLEAN NOT NULL DEFAULT TRUE,
    used          BOOLEAN NOT NULL DEFAULT FALSE,
    used_at       TIMESTAMPTZ,
    next_token_id UUID REFERENCES oa2_refresh_tokens(id),
    issued_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at    TIMESTAMPTZ NOT NULL
);

-- Critical for family invalidation: UPDATE WHERE family_id = $1
CREATE INDEX idx_oa2_rt_family  ON oa2_refresh_tokens(family_id, active);
CREATE INDEX idx_oa2_rt_subject ON oa2_refresh_tokens(subject, active);
CREATE INDEX idx_oa2_rt_expires ON oa2_refresh_tokens(expires_at) WHERE active = TRUE;
```

### `oa2_consent_sessions`

```sql
CREATE TABLE oa2_consent_sessions (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    client_id           TEXT NOT NULL REFERENCES oa2_clients(client_id),
    subject             UUID NOT NULL REFERENCES core_identities(id),
    session_id          UUID REFERENCES core_sessions(id),
    granted_scopes      TEXT[] NOT NULL,
    denied_scopes       TEXT[] NOT NULL DEFAULT '{}',
    remember            BOOLEAN NOT NULL DEFAULT FALSE,
    remember_for        INTERVAL,
    access_token_claims JSONB NOT NULL DEFAULT '{}',
    id_token_claims     JSONB NOT NULL DEFAULT '{}',
    revoked             BOOLEAN NOT NULL DEFAULT FALSE,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX idx_oa2_consent_client_sub
    ON oa2_consent_sessions(client_id, subject)
    WHERE remember = TRUE AND revoked = FALSE;
```

### `oa2_device_codes`

```sql
CREATE TABLE oa2_device_codes (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    device_code_hash TEXT NOT NULL UNIQUE,
    user_code        TEXT NOT NULL UNIQUE,
    client_id        TEXT NOT NULL REFERENCES oa2_clients(client_id),
    scopes           TEXT[] NOT NULL,
    subject          UUID REFERENCES core_identities(id),
    status           TEXT NOT NULL DEFAULT 'pending',
                                  -- pending | approved | denied | expired
    last_polled_at   TIMESTAMPTZ,
    expires_at       TIMESTAMPTZ NOT NULL,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_oa2_dc_user_code ON oa2_device_codes(user_code)   WHERE status = 'pending';
CREATE INDEX idx_oa2_dc_expires   ON oa2_device_codes(expires_at)  WHERE status = 'pending';
```

---

## Policy module (`pol_*`)

### `pol_roles`

```sql
CREATE TABLE pol_roles (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name        TEXT NOT NULL UNIQUE,
    description TEXT,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
```

### `pol_permissions`

```sql
CREATE TABLE pol_permissions (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    role_id       UUID NOT NULL REFERENCES pol_roles(id) ON DELETE CASCADE,
    resource_type TEXT NOT NULL,   -- "posts" | "*"
    action        TEXT NOT NULL,   -- "read" | "*"
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX idx_pol_perm_unique
    ON pol_permissions(role_id, resource_type, action);
```

### `pol_role_assignments`

```sql
CREATE TABLE pol_role_assignments (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    identity_id UUID NOT NULL REFERENCES core_identities(id) ON DELETE CASCADE,
    role_id     UUID NOT NULL REFERENCES pol_roles(id) ON DELETE CASCADE,
    granted_by  UUID REFERENCES core_identities(id),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX idx_pol_assign_unique
    ON pol_role_assignments(identity_id, role_id);
```

### `pol_abac_rules`

```sql
CREATE TABLE pol_abac_rules (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name        TEXT NOT NULL,
    description TEXT,
    expression  TEXT NOT NULL,       -- CEL expression
    priority    INT NOT NULL,
    effect      TEXT NOT NULL,       -- allow | deny
    enabled     BOOLEAN NOT NULL DEFAULT TRUE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_pol_abac_priority ON pol_abac_rules(priority) WHERE enabled = TRUE;
```

### `pol_rebac_namespaces`

```sql
CREATE TABLE pol_rebac_namespaces (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name       TEXT NOT NULL UNIQUE,
    config     JSONB NOT NULL,   -- OPL namespace config
    version    INT NOT NULL DEFAULT 1,
    active     BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
```

### `pol_rebac_tuples`

The raw facts of the ReBAC graph. Both indexes are load-bearing for expansion queries.

```sql
CREATE TABLE pol_rebac_tuples (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    namespace  TEXT NOT NULL,
    object_id  TEXT NOT NULL,
    relation   TEXT NOT NULL,
    subject_id TEXT NOT NULL,   -- "user:alice" or "group:eng#member"
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX idx_pol_tuple_unique
    ON pol_rebac_tuples(namespace, object_id, relation, subject_id);
-- Object traversal (check object → relation → subjects)
CREATE INDEX idx_pol_tuple_obj
    ON pol_rebac_tuples(namespace, object_id, relation);
-- Subject traversal (expand all objects a subject can access)
CREATE INDEX idx_pol_tuple_subject
    ON pol_rebac_tuples(namespace, subject_id);
```

---

## Proxy module (`prx_*`)

### `prx_access_rules`

```sql
CREATE TABLE prx_access_rules (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name           TEXT NOT NULL,
    priority       INT NOT NULL DEFAULT 100,
    enabled        BOOLEAN NOT NULL DEFAULT TRUE,
    match_config   JSONB NOT NULL,    -- { methods[], paths[], hosts[] }
    authenticators JSONB NOT NULL DEFAULT '[]',
    authorizer     JSONB NOT NULL,
    mutators       JSONB NOT NULL DEFAULT '[]',
    upstream       JSONB NOT NULL,    -- { url, preserve_host, timeout }
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_prx_rules_priority ON prx_access_rules(priority) WHERE enabled = TRUE;
```

---

## Admin module (`adm_*`)

### `adm_identities`

Admin operators. Separate from end-user identities.

```sql
CREATE TABLE adm_identities (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    identity_id  UUID NOT NULL UNIQUE REFERENCES core_identities(id) ON DELETE CASCADE,
    display_name TEXT NOT NULL,
    last_login_at TIMESTAMPTZ,
    is_super     BOOLEAN NOT NULL DEFAULT FALSE,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
```

### `adm_roles` and `adm_role_assignments`

```sql
CREATE TABLE adm_roles (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name        TEXT NOT NULL UNIQUE,
    description TEXT,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE adm_role_assignments (
    id       UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    admin_id UUID NOT NULL REFERENCES adm_identities(id) ON DELETE CASCADE,
    role_id  UUID NOT NULL REFERENCES adm_roles(id)      ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX idx_adm_assign_unique
    ON adm_role_assignments(admin_id, role_id);
```

### `adm_capability_overrides`

Discord-style direct grants and denies on top of role assignments.

```sql
CREATE TABLE adm_capability_overrides (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    admin_id   UUID NOT NULL REFERENCES adm_identities(id) ON DELETE CASCADE,
    capability TEXT NOT NULL,
    effect     TEXT NOT NULL,    -- grant | deny
    granted_by UUID REFERENCES adm_identities(id),
    reason     TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX idx_adm_cap_unique
    ON adm_capability_overrides(admin_id, capability);
```

---

## Migration sequencing

Migrations run at core startup in dependency order:

1. Core tables (`core_*`)
2. `pwd_*` — if password enabled
3. `mfa_*` — if mfa enabled
4. `pk_*` — if passkeys enabled
5. `ml_*` — if magic_link enabled
6. `soc_*` — if social enabled
7. `sso_*` — if sso enabled
8. `oa2_*` — if oauth2 enabled
9. `pol_*` — if policy enabled
10. `prx_*` — if proxy enabled
11. `adm_*` — if admin enabled

Migration files are embedded in each module image and tracked in a `schema_migrations` table. Already-applied migrations from a previously disabled module are retained and not re-run on re-enable.

---

## Postgres role model

Each module connects with a dedicated, least-privilege role:

| Role | Write access | Read access |
|---|---|---|
| `aegion_core` | `core_*` | — |
| `aegion_password` | `pwd_*` | `core_identities`, `core_identity_addresses` |
| `aegion_mfa` | `mfa_*` | `core_identities`, `core_sessions` |
| `aegion_passkeys` | `pk_*` | `core_identities` |
| `aegion_magic_link` | `ml_*` | `core_flows`, `core_identities` |
| `aegion_social` | `soc_*` | `core_identities`, `core_flows` |
| `aegion_sso` | `sso_*`, INSERT/UPDATE `core_identities` (SCIM) | all sso tables |
| `aegion_oauth2` | `oa2_*` | `core_identities`, `core_sessions`, `core_signing_keys` |
| `aegion_policy` | `pol_*` | `core_identities` |
| `aegion_proxy` | `prx_*` | `core_sessions` |
| `aegion_admin` | `adm_*`, INSERT `core_audit_events` | SELECT all tables |
| `aegion_migrator` | CREATE/ALTER/DROP all | — |

---

## Key performance notes

- **`pol_rebac_tuples`**: Both composite indexes are load-bearing. Disable or rebuild them and ReBAC check latency spikes. Monitor index bloat on this table — it receives heavy write load when using SCIM group provisioning.
- **`oa2_refresh_tokens`**: The `family_id` index makes family invalidation a single `UPDATE WHERE family_id = $1`. Without this index, a replay attack detection becomes a full table scan.
- **`core_audit_events`**: Grows without bound. Implement time-based partitioning (`PARTITION BY RANGE (occurred_at)`) for large deployments. Archive partitions older than 90 days to cold storage.
- **`core_event_bus_deliveries`**: The partial index on `(subscriber, next_retry_at) WHERE status IN ('pending', 'failed')` drives the redelivery worker. Autovacuum must keep up with the update churn on this table — tune `autovacuum_vacuum_scale_factor` down for this table specifically.
- **`core_sessions`**: Session token is stored as a SHA-256 hash. The raw token is never written to the database — only passed in cookies. Session lookup is always by hash, not raw value.
