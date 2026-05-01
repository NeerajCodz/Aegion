-- =============================================================================
-- OAuth2 Module Tables - Part 4: Device Authorization
-- Migration: 0004_oauth2_device
-- =============================================================================

-- Device Authorization (RFC 8628)
CREATE TABLE oa2_device_codes (
    device_code             TEXT PRIMARY KEY,
    user_code               TEXT NOT NULL UNIQUE,    -- short code shown to user
    client_id               TEXT NOT NULL REFERENCES oa2_clients(id) ON DELETE CASCADE,
    scopes                  TEXT[] NOT NULL,
    audience                TEXT[] DEFAULT '{}',
    verification_uri        TEXT NOT NULL,
    verification_uri_complete TEXT,                  -- with user_code embedded
    interval                INTEGER NOT NULL DEFAULT 5,  -- polling interval seconds
    identity_id             TEXT,                    -- set when user approves
    session_id              TEXT,
    status                  TEXT NOT NULL DEFAULT 'pending',  -- pending, approved, denied, expired
    approved_at             TIMESTAMPTZ,
    expires_at              TIMESTAMPTZ NOT NULL,
    last_poll_at            TIMESTAMPTZ,
    created_at              TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- For user code entry
CREATE INDEX idx_oa2_device_codes_user ON oa2_device_codes(user_code);
CREATE INDEX idx_oa2_device_codes_client ON oa2_device_codes(client_id);
CREATE INDEX idx_oa2_device_codes_expires ON oa2_device_codes(expires_at);
CREATE INDEX idx_oa2_device_codes_status ON oa2_device_codes(status) WHERE status = 'pending';

-- JWT Bearer Assertions (for jwt-bearer grant)
CREATE TABLE oa2_jwt_assertions (
    jti                     TEXT PRIMARY KEY,        -- assertion jti (replay protection)
    client_id               TEXT NOT NULL,
    issuer                  TEXT NOT NULL,
    subject                 TEXT NOT NULL,
    audience                TEXT NOT NULL,
    used                    BOOLEAN NOT NULL DEFAULT FALSE,
    used_at                 TIMESTAMPTZ,
    expires_at              TIMESTAMPTZ NOT NULL,
    created_at              TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- For replay detection
CREATE INDEX idx_oa2_jwt_assertions_expires ON oa2_jwt_assertions(expires_at);
CREATE INDEX idx_oa2_jwt_assertions_client ON oa2_jwt_assertions(client_id);
