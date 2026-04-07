-- =============================================================================
-- Policy Module Tables
-- Migration: 0001_policy_tables
-- =============================================================================

CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE OR REPLACE FUNCTION update_updated_at_column()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TABLE pol_roles (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name        TEXT NOT NULL UNIQUE,
    description TEXT,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE pol_permissions (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    role_id       UUID NOT NULL REFERENCES pol_roles(id) ON DELETE CASCADE,
    resource_type TEXT NOT NULL,
    action        TEXT NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX idx_pol_perm_unique
    ON pol_permissions(role_id, resource_type, action);

CREATE TABLE pol_role_assignments (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    identity_id UUID NOT NULL REFERENCES core_identities(id) ON DELETE CASCADE,
    role_id     UUID NOT NULL REFERENCES pol_roles(id) ON DELETE CASCADE,
    granted_by  UUID REFERENCES core_identities(id),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX idx_pol_assign_unique
    ON pol_role_assignments(identity_id, role_id);

CREATE TABLE pol_abac_rules (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name        TEXT NOT NULL,
    description TEXT,
    expression  TEXT NOT NULL,
    priority    INT NOT NULL,
    effect      TEXT NOT NULL,
    enabled     BOOLEAN NOT NULL DEFAULT TRUE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT pol_abac_effect_check CHECK (effect IN ('allow', 'deny'))
);

CREATE INDEX idx_pol_abac_priority ON pol_abac_rules(priority) WHERE enabled = TRUE;

CREATE TABLE pol_rebac_namespaces (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name       TEXT NOT NULL UNIQUE,
    config     JSONB NOT NULL,
    version    INT NOT NULL DEFAULT 1,
    active     BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE pol_rebac_tuples (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    namespace  TEXT NOT NULL,
    object_id  TEXT NOT NULL,
    relation   TEXT NOT NULL,
    subject_id TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX idx_pol_tuple_unique
    ON pol_rebac_tuples(namespace, object_id, relation, subject_id);

CREATE INDEX idx_pol_tuple_obj
    ON pol_rebac_tuples(namespace, object_id, relation);

CREATE INDEX idx_pol_tuple_subject
    ON pol_rebac_tuples(namespace, subject_id);

CREATE TRIGGER update_pol_roles_updated_at
    BEFORE UPDATE ON pol_roles
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TRIGGER update_pol_abac_rules_updated_at
    BEFORE UPDATE ON pol_abac_rules
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TRIGGER update_pol_rebac_namespaces_updated_at
    BEFORE UPDATE ON pol_rebac_namespaces
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();
