-- =============================================================================
-- Core System Config & Keys
-- Migration: 0006_core_system
-- =============================================================================

-- Signing Keys (JWT, etc.)
CREATE TABLE core_signing_keys (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    key_id       TEXT NOT NULL UNIQUE,
    use          TEXT NOT NULL,
    algorithm    TEXT NOT NULL,
    status       TEXT NOT NULL DEFAULT 'active',
    public_key   BYTEA NOT NULL,
    private_key  BYTEA NOT NULL,  -- Encrypted with cipher secret
    activated_at TIMESTAMPTZ,
    retired_at   TIMESTAMPTZ,
    expired_at   TIMESTAMPTZ,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    
    CONSTRAINT core_signing_keys_use_check
        CHECK (use IN ('sig', 'enc')),
    CONSTRAINT core_signing_keys_algorithm_check
        CHECK (algorithm IN ('RS256', 'RS384', 'RS512', 'ES256', 'ES384', 'ES512')),
    CONSTRAINT core_signing_keys_status_check
        CHECK (status IN ('active', 'retiring', 'expired'))
);

CREATE INDEX idx_core_keys_status ON core_signing_keys(status, use);

-- System Configuration (Runtime Config)
CREATE TABLE core_system_config (
    key        TEXT PRIMARY KEY,
    value      JSONB NOT NULL,
    updated_by UUID REFERENCES core_identities(id) ON DELETE SET NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Triggers
CREATE TRIGGER update_core_system_config_updated_at
    BEFORE UPDATE ON core_system_config
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();
