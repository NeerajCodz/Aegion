-- =============================================================================
-- OAuth2 Module Tables - Part 3: Access & Refresh Tokens
-- Migration: 0003_oauth2_tokens
-- =============================================================================

-- Access Tokens (for JWT tokens, we store metadata for revocation checks)
CREATE TABLE oa2_access_tokens (
    jti                     TEXT PRIMARY KEY,        -- JWT ID
    signature               TEXT,                    -- last N chars for opaque token lookup
    client_id               TEXT NOT NULL,
    identity_id             TEXT NOT NULL,
    session_id              TEXT NOT NULL,
    scopes                  TEXT[] NOT NULL,
    audience                TEXT[] NOT NULL,
    issuer                  TEXT NOT NULL,
    subject                 TEXT NOT NULL,           -- may be pairwise
    extra_claims            JSONB DEFAULT '{}',
    revoked                 BOOLEAN NOT NULL DEFAULT FALSE,
    revoked_at              TIMESTAMPTZ,
    expires_at              TIMESTAMPTZ NOT NULL,
    created_at              TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- For token introspection and revocation
CREATE INDEX idx_oa2_access_tokens_client ON oa2_access_tokens(client_id);
CREATE INDEX idx_oa2_access_tokens_identity ON oa2_access_tokens(identity_id);
CREATE INDEX idx_oa2_access_tokens_session ON oa2_access_tokens(session_id);

-- For signature-based lookup (opaque tokens)
CREATE INDEX idx_oa2_access_tokens_sig ON oa2_access_tokens(signature) WHERE signature IS NOT NULL;

-- For cleanup
CREATE INDEX idx_oa2_access_tokens_expires ON oa2_access_tokens(expires_at);
CREATE INDEX idx_oa2_access_tokens_revoked ON oa2_access_tokens(revoked) WHERE revoked = false;

-- Refresh Tokens
CREATE TABLE oa2_refresh_tokens (
    id                      TEXT PRIMARY KEY,
    family_id               TEXT NOT NULL,           -- for family invalidation
    client_id               TEXT NOT NULL,
    identity_id             TEXT NOT NULL,
    session_id              TEXT NOT NULL,
    scopes                  TEXT[] NOT NULL,
    audience                TEXT[] DEFAULT '{}',
    active                  BOOLEAN NOT NULL DEFAULT TRUE,
    used                    BOOLEAN NOT NULL DEFAULT FALSE,
    used_at                 TIMESTAMPTZ,
    successor_id            TEXT,                    -- points to next token in rotation chain
    grace_period_expires_at TIMESTAMPTZ,             -- for mobile grace period
    first_used_at           TIMESTAMPTZ,             -- first use time for grace period
    access_token_jti        TEXT,                    -- associated access token
    extra_claims            JSONB DEFAULT '{}',
    expires_at              TIMESTAMPTZ NOT NULL,
    created_at              TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- For refresh and rotation
CREATE INDEX idx_oa2_refresh_tokens_client ON oa2_refresh_tokens(client_id);
CREATE INDEX idx_oa2_refresh_tokens_identity ON oa2_refresh_tokens(identity_id);
CREATE INDEX idx_oa2_refresh_tokens_session ON oa2_refresh_tokens(session_id);

-- For family invalidation
CREATE INDEX idx_oa2_refresh_tokens_family ON oa2_refresh_tokens(family_id);

-- For cleanup of inactive tokens
CREATE INDEX idx_oa2_refresh_tokens_active ON oa2_refresh_tokens(active) WHERE active = true;
CREATE INDEX idx_oa2_refresh_tokens_expires ON oa2_refresh_tokens(expires_at);

-- ID Tokens (stored for session logout and backchannel logout)
CREATE TABLE oa2_id_tokens (
    jti                     TEXT PRIMARY KEY,
    client_id               TEXT NOT NULL,
    identity_id             TEXT NOT NULL,
    session_id              TEXT NOT NULL,
    nonce                   TEXT,
    at_hash                 TEXT,                    -- access token hash
    c_hash                  TEXT,                    -- code hash (hybrid flow)
    acr                     TEXT DEFAULT 'aal1',
    amr                     TEXT[] DEFAULT '{}',
    auth_time               TIMESTAMPTZ NOT NULL,
    extra_claims            JSONB DEFAULT '{}',
    revoked                 BOOLEAN NOT NULL DEFAULT FALSE,
    expires_at              TIMESTAMPTZ NOT NULL,
    created_at              TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_oa2_id_tokens_identity ON oa2_id_tokens(identity_id);
CREATE INDEX idx_oa2_id_tokens_session ON oa2_id_tokens(session_id);
CREATE INDEX idx_oa2_id_tokens_expires ON oa2_id_tokens(expires_at);

-- Token Revocation Registry (bloom filter seed + explicit revocations)
CREATE TABLE oa2_token_revocations (
    jti                     TEXT PRIMARY KEY,
    token_type              TEXT NOT NULL,           -- access_token, refresh_token, id_token
    client_id               TEXT,
    identity_id             TEXT,
    reason                  TEXT,
    revoked_by              TEXT,                    -- admin identity or 'system'
    expires_at              TIMESTAMPTZ NOT NULL,    -- when entry can be cleaned up
    created_at              TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- For cleanup of old revocation entries
CREATE INDEX idx_oa2_token_revocations_expires ON oa2_token_revocations(expires_at);
CREATE INDEX idx_oa2_token_revocations_identity ON oa2_token_revocations(identity_id);
