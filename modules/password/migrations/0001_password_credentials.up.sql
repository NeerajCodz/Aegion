-- =============================================================================
-- Password Module Tables
-- Migration: 0001_password_credentials
-- =============================================================================

-- Password Credentials
CREATE TABLE pwd_credentials (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    identity_id UUID NOT NULL,
    identifier  TEXT NOT NULL,  -- email, username, etc.
    hash        TEXT NOT NULL,  -- PHC-encoded Argon2id hash
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    
    CONSTRAINT fk_pwd_credentials_identity
        FOREIGN KEY (identity_id) REFERENCES core_identities(id) ON DELETE CASCADE
);

-- One credential per identifier
CREATE UNIQUE INDEX idx_pwd_credentials_identifier ON pwd_credentials(identifier);
CREATE INDEX idx_pwd_credentials_identity ON pwd_credentials(identity_id);

-- Password History (for reuse prevention)
CREATE TABLE pwd_history (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    credential_id UUID NOT NULL REFERENCES pwd_credentials(id) ON DELETE CASCADE,
    hash          TEXT NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_pwd_history_credential ON pwd_history(credential_id, created_at DESC);

-- Triggers
CREATE TRIGGER update_pwd_credentials_updated_at
    BEFORE UPDATE ON pwd_credentials
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();
