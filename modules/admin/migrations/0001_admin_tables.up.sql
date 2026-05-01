-- =============================================================================
-- Admin Module Tables
-- Migration: 0001_admin_tables
-- =============================================================================

-- Admin Roles (predefined and custom roles)
CREATE TABLE adm_roles (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name        TEXT NOT NULL UNIQUE,
    description TEXT,
    permissions JSONB NOT NULL DEFAULT '[]',
    is_system   BOOLEAN NOT NULL DEFAULT FALSE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_adm_roles_is_system ON adm_roles(is_system);

-- Admin Operators (users with admin access)
CREATE TABLE adm_operators (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    identity_id UUID NOT NULL UNIQUE,
    role        TEXT NOT NULL,
    permissions JSONB NOT NULL DEFAULT '{}',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    
    CONSTRAINT fk_adm_operators_identity
        FOREIGN KEY (identity_id) REFERENCES core_identities(id) ON DELETE CASCADE,
    CONSTRAINT adm_operators_role_check
        CHECK (role IN ('super_admin', 'admin', 'operator', 'viewer'))
);

CREATE INDEX idx_adm_operators_role ON adm_operators(role);
CREATE INDEX idx_adm_operators_identity ON adm_operators(identity_id);

-- Admin Audit Log (tracks all admin actions)
CREATE TABLE adm_audit_log (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    operator_id   UUID,
    action        TEXT NOT NULL,
    resource_type TEXT NOT NULL,
    resource_id   TEXT,
    details       JSONB NOT NULL DEFAULT '{}',
    ip_address    TEXT,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    
    CONSTRAINT fk_adm_audit_log_operator
        FOREIGN KEY (operator_id) REFERENCES adm_operators(id) ON DELETE SET NULL,
    CONSTRAINT adm_audit_log_action_check
        CHECK (action IN ('create', 'read', 'update', 'delete'))
);

CREATE INDEX idx_adm_audit_log_operator ON adm_audit_log(operator_id);
CREATE INDEX idx_adm_audit_log_action ON adm_audit_log(action);
CREATE INDEX idx_adm_audit_log_resource ON adm_audit_log(resource_type, resource_id);
CREATE INDEX idx_adm_audit_log_created ON adm_audit_log(created_at DESC);

-- Admin API Keys (for programmatic access)
CREATE TABLE adm_api_keys (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    operator_id  UUID NOT NULL,
    name         TEXT NOT NULL,
    key_hash     TEXT NOT NULL,
    key_prefix   TEXT NOT NULL,
    permissions  JSONB NOT NULL DEFAULT '{}',
    last_used_at TIMESTAMPTZ,
    expires_at   TIMESTAMPTZ,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    
    CONSTRAINT fk_adm_api_keys_operator
        FOREIGN KEY (operator_id) REFERENCES adm_operators(id) ON DELETE CASCADE
);

CREATE INDEX idx_adm_api_keys_operator ON adm_api_keys(operator_id);
CREATE INDEX idx_adm_api_keys_prefix ON adm_api_keys(key_prefix);
CREATE INDEX idx_adm_api_keys_expires ON adm_api_keys(expires_at) WHERE expires_at IS NOT NULL;

-- Triggers for updated_at
CREATE TRIGGER update_adm_roles_updated_at
    BEFORE UPDATE ON adm_roles
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TRIGGER update_adm_operators_updated_at
    BEFORE UPDATE ON adm_operators
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TRIGGER update_adm_api_keys_updated_at
    BEFORE UPDATE ON adm_api_keys
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

-- Insert default system roles
INSERT INTO adm_roles (name, description, permissions, is_system) VALUES
    ('super_admin', 'Full system access with all permissions', '["*"]', TRUE),
    ('admin', 'Administrative access to manage identities and configurations', '["identities:*", "sessions:*", "config:read", "config:update", "audit:read"]', TRUE),
    ('operator', 'Operational access to manage identities and view sessions', '["identities:read", "identities:update", "sessions:read", "sessions:delete", "audit:read"]', TRUE),
    ('viewer', 'Read-only access to view system data', '["identities:read", "sessions:read", "config:read", "audit:read"]', TRUE);
