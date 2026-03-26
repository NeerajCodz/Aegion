-- =============================================================================
-- Core Sessions
-- Migration: 0002_core_sessions
-- =============================================================================

-- Sessions
CREATE TABLE core_sessions (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    token            TEXT NOT NULL UNIQUE,
    identity_id      UUID NOT NULL REFERENCES core_identities(id) ON DELETE CASCADE,
    aal              TEXT NOT NULL DEFAULT 'aal1',
    issued_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at       TIMESTAMPTZ NOT NULL,
    authenticated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    logout_token     TEXT UNIQUE,
    devices          JSONB NOT NULL DEFAULT '[]',
    active           BOOLEAN NOT NULL DEFAULT TRUE,
    is_impersonation BOOLEAN NOT NULL DEFAULT FALSE,
    impersonator_id  UUID REFERENCES core_identities(id),
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    
    CONSTRAINT core_sessions_aal_check 
        CHECK (aal IN ('aal0', 'aal1', 'aal2'))
);

CREATE INDEX idx_core_sessions_identity ON core_sessions(identity_id) WHERE active = TRUE;
CREATE INDEX idx_core_sessions_expires ON core_sessions(expires_at) WHERE active = TRUE;
CREATE INDEX idx_core_sessions_token ON core_sessions(token) WHERE active = TRUE;

-- Session Authentication Methods
CREATE TABLE core_session_auth_methods (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    session_id      UUID NOT NULL REFERENCES core_sessions(id) ON DELETE CASCADE,
    method          TEXT NOT NULL,
    aal_contributed TEXT NOT NULL,
    completed_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    
    CONSTRAINT core_session_auth_methods_method_check
        CHECK (method IN ('password', 'totp', 'webauthn', 'magic_link', 'social', 'saml', 'passkey', 'sms', 'backup_code')),
    CONSTRAINT core_session_auth_methods_aal_check
        CHECK (aal_contributed IN ('aal1', 'aal2'))
);

CREATE INDEX idx_core_sam_session ON core_session_auth_methods(session_id);

-- Triggers
CREATE TRIGGER update_core_sessions_updated_at
    BEFORE UPDATE ON core_sessions
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();
