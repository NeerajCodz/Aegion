-- =============================================================================
-- Admin Module SCIM Tables
-- Migration: 0003_admin_scim
-- =============================================================================

CREATE TABLE adm_scim_groups (
    id           TEXT PRIMARY KEY,
    external_id  TEXT,
    display_name TEXT NOT NULL UNIQUE,
    members      JSONB NOT NULL DEFAULT '[]',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_adm_scim_groups_display_name ON adm_scim_groups(display_name);

CREATE TABLE adm_scim_mappings (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name              TEXT NOT NULL UNIQUE,
    description       TEXT NOT NULL DEFAULT '',
    username_source   TEXT NOT NULL DEFAULT 'email',
    username_custom   TEXT NOT NULL DEFAULT '',
    email_source      TEXT NOT NULL DEFAULT 'primary',
    name_mapping      JSONB NOT NULL DEFAULT '{}',
    attribute_mapping JSONB NOT NULL DEFAULT '{}',
    group_mapping     JSONB NOT NULL DEFAULT '{}',
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE adm_scim_tokens (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name         TEXT NOT NULL,
    description  TEXT NOT NULL DEFAULT '',
    token_hash   TEXT NOT NULL,
    prefix       TEXT NOT NULL UNIQUE,
    permissions  JSONB NOT NULL DEFAULT '[]',
    created_by   UUID NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at   TIMESTAMPTZ,
    last_used_at TIMESTAMPTZ,
    active       BOOLEAN NOT NULL DEFAULT TRUE,

    CONSTRAINT fk_adm_scim_tokens_created_by
        FOREIGN KEY (created_by) REFERENCES adm_operators(id) ON DELETE CASCADE
);

CREATE INDEX idx_adm_scim_tokens_active ON adm_scim_tokens(active);
CREATE INDEX idx_adm_scim_tokens_expires ON adm_scim_tokens(expires_at) WHERE expires_at IS NOT NULL;

CREATE TRIGGER update_adm_scim_groups_updated_at
    BEFORE UPDATE ON adm_scim_groups
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TRIGGER update_adm_scim_mappings_updated_at
    BEFORE UPDATE ON adm_scim_mappings
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();
