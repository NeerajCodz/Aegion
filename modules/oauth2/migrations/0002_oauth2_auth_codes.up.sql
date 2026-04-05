-- =============================================================================
-- OAuth2 Module Tables - Part 2: Authorization Codes & Consent
-- Migration: 0002_oauth2_auth_codes
-- =============================================================================

-- Authorization Codes
CREATE TABLE oa2_auth_codes (
    code                    TEXT PRIMARY KEY,
    client_id               TEXT NOT NULL REFERENCES oa2_clients(id) ON DELETE CASCADE,
    identity_id             TEXT NOT NULL,
    session_id              TEXT NOT NULL,
    redirect_uri            TEXT NOT NULL,
    scopes                  TEXT[] NOT NULL,
    audience                TEXT[] DEFAULT '{}',
    code_challenge          TEXT,                    -- PKCE challenge
    code_challenge_method   TEXT DEFAULT 'S256',     -- S256 or plain
    nonce                   TEXT,                    -- OIDC nonce for replay protection
    state                   TEXT,                    -- client state
    acr                     TEXT DEFAULT 'aal1',     -- authentication context class
    amr                     TEXT[] DEFAULT '{}',     -- authentication methods
    auth_time               TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    request_object          TEXT,                    -- PAR request object (if used)
    used                    BOOLEAN NOT NULL DEFAULT FALSE,
    used_at                 TIMESTAMPTZ,
    expires_at              TIMESTAMPTZ NOT NULL,
    created_at              TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- For code exchange lookup
CREATE INDEX idx_oa2_auth_codes_client ON oa2_auth_codes(client_id);
CREATE INDEX idx_oa2_auth_codes_identity ON oa2_auth_codes(identity_id);

-- For cleanup of expired codes
CREATE INDEX idx_oa2_auth_codes_expires ON oa2_auth_codes(expires_at);

-- Consent Sessions (remembered consents)
CREATE TABLE oa2_consent_sessions (
    id                      TEXT PRIMARY KEY DEFAULT gen_random_uuid()::text,
    client_id               TEXT NOT NULL REFERENCES oa2_clients(id) ON DELETE CASCADE,
    identity_id             TEXT NOT NULL,
    scopes                  TEXT[] NOT NULL,
    audience                TEXT[] DEFAULT '{}',
    remember                BOOLEAN NOT NULL DEFAULT FALSE,
    remember_for            INTEGER,                 -- seconds to remember
    access_token_claims     JSONB DEFAULT '{}',      -- extra claims for access tokens
    id_token_claims         JSONB DEFAULT '{}',      -- extra claims for ID tokens
    handled                 BOOLEAN NOT NULL DEFAULT FALSE,
    granted_at              TIMESTAMPTZ,
    expires_at              TIMESTAMPTZ,
    created_at              TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at              TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- One consent session per client-identity pair (can be updated)
CREATE UNIQUE INDEX idx_oa2_consent_unique ON oa2_consent_sessions(client_id, identity_id);
CREATE INDEX idx_oa2_consent_identity ON oa2_consent_sessions(identity_id);
CREATE INDEX idx_oa2_consent_expires ON oa2_consent_sessions(expires_at) WHERE expires_at IS NOT NULL;

CREATE TRIGGER update_oa2_consent_sessions_updated_at
    BEFORE UPDATE ON oa2_consent_sessions
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

-- Login Challenges (pending logins during OAuth flow)
CREATE TABLE oa2_login_challenges (
    id                      TEXT PRIMARY KEY,
    client_id               TEXT NOT NULL REFERENCES oa2_clients(id) ON DELETE CASCADE,
    request_url             TEXT NOT NULL,
    redirect_uri            TEXT NOT NULL,
    scopes                  TEXT[] NOT NULL,
    audience                TEXT[] DEFAULT '{}',
    acr_values              TEXT[] DEFAULT '{}',
    state                   TEXT,
    code_challenge          TEXT,
    code_challenge_method   TEXT,
    nonce                   TEXT,
    skip                    BOOLEAN NOT NULL DEFAULT FALSE,
    identity_id             TEXT,                    -- set when authenticated
    session_id              TEXT,                    -- set when authenticated
    authenticated_at        TIMESTAMPTZ,
    expires_at              TIMESTAMPTZ NOT NULL,
    created_at              TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_oa2_login_challenges_expires ON oa2_login_challenges(expires_at);

-- Consent Challenges (pending consent decisions)
CREATE TABLE oa2_consent_challenges (
    id                      TEXT PRIMARY KEY,
    login_challenge_id      TEXT NOT NULL REFERENCES oa2_login_challenges(id) ON DELETE CASCADE,
    client_id               TEXT NOT NULL REFERENCES oa2_clients(id) ON DELETE CASCADE,
    identity_id             TEXT NOT NULL,
    session_id              TEXT NOT NULL,
    request_url             TEXT NOT NULL,
    redirect_uri            TEXT NOT NULL,
    requested_scopes        TEXT[] NOT NULL,
    requested_audience      TEXT[] DEFAULT '{}',
    skip                    BOOLEAN NOT NULL DEFAULT FALSE,
    granted_scopes          TEXT[],
    granted_audience        TEXT[],
    remember                BOOLEAN DEFAULT FALSE,
    remember_for            INTEGER,
    access_token_claims     JSONB DEFAULT '{}',
    id_token_claims         JSONB DEFAULT '{}',
    handled                 BOOLEAN NOT NULL DEFAULT FALSE,
    handled_at              TIMESTAMPTZ,
    rejected                BOOLEAN NOT NULL DEFAULT FALSE,
    error                   TEXT,
    error_description       TEXT,
    expires_at              TIMESTAMPTZ NOT NULL,
    created_at              TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_oa2_consent_challenges_login ON oa2_consent_challenges(login_challenge_id);
CREATE INDEX idx_oa2_consent_challenges_identity ON oa2_consent_challenges(identity_id);
CREATE INDEX idx_oa2_consent_challenges_expires ON oa2_consent_challenges(expires_at);
