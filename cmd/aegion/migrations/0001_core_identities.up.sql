-- =============================================================================
-- Core Identity Tables
-- Migration: 0001_core_identities
-- =============================================================================

-- Identity Schemas
CREATE TABLE core_identity_schemas (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name       TEXT NOT NULL UNIQUE,
    is_default BOOLEAN NOT NULL DEFAULT FALSE,
    schema     JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Ensure only one default schema
CREATE UNIQUE INDEX idx_core_identity_schemas_default 
    ON core_identity_schemas(is_default) 
    WHERE is_default = TRUE;

-- Identities
CREATE TABLE core_identities (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    schema_id       UUID NOT NULL REFERENCES core_identity_schemas(id),
    traits          JSONB NOT NULL DEFAULT '{}',
    state           TEXT NOT NULL DEFAULT 'active',
    is_anonymous    BOOLEAN NOT NULL DEFAULT FALSE,
    metadata_public JSONB NOT NULL DEFAULT '{}',
    metadata_admin  JSONB NOT NULL DEFAULT '{}',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at      TIMESTAMPTZ,
    
    CONSTRAINT core_identities_state_check 
        CHECK (state IN ('active', 'inactive', 'banned'))
);

CREATE INDEX idx_core_identities_state ON core_identities(state) WHERE deleted_at IS NULL;
CREATE INDEX idx_core_identities_traits ON core_identities USING GIN(traits);
CREATE INDEX idx_core_identities_schema ON core_identities(schema_id);
CREATE INDEX idx_core_identities_created ON core_identities(created_at DESC);

-- Identity Addresses (email, phone)
CREATE TABLE core_identity_addresses (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    identity_id UUID NOT NULL REFERENCES core_identities(id) ON DELETE CASCADE,
    type        TEXT NOT NULL,
    value       TEXT NOT NULL,
    is_primary  BOOLEAN NOT NULL DEFAULT FALSE,
    verified    BOOLEAN NOT NULL DEFAULT FALSE,
    verified_at TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    
    CONSTRAINT core_identity_addresses_type_check 
        CHECK (type IN ('email', 'phone'))
);

-- Unique verified address per type
CREATE UNIQUE INDEX idx_core_addr_value_type 
    ON core_identity_addresses(type, value) 
    WHERE verified = TRUE;
    
CREATE INDEX idx_core_addr_identity ON core_identity_addresses(identity_id);

-- Only one primary per type per identity
CREATE UNIQUE INDEX idx_core_addr_primary 
    ON core_identity_addresses(identity_id, type) 
    WHERE is_primary = TRUE;

-- Trigger to update updated_at
CREATE OR REPLACE FUNCTION update_updated_at_column()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER update_core_identities_updated_at
    BEFORE UPDATE ON core_identities
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TRIGGER update_core_identity_schemas_updated_at
    BEFORE UPDATE ON core_identity_schemas
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TRIGGER update_core_identity_addresses_updated_at
    BEFORE UPDATE ON core_identity_addresses
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();
