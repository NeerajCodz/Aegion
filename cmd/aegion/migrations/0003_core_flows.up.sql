-- =============================================================================
-- Core Flows (Self-Service Flow State)
-- Migration: 0003_core_flows
-- =============================================================================

-- Self-Service Flows
CREATE TABLE core_flows (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    type        TEXT NOT NULL,
    state       TEXT NOT NULL DEFAULT 'active',
    identity_id UUID REFERENCES core_identities(id) ON DELETE CASCADE,
    session_id  UUID REFERENCES core_sessions(id) ON DELETE SET NULL,
    request_url TEXT NOT NULL,
    return_to   TEXT,
    ui          JSONB NOT NULL DEFAULT '{}',
    context     JSONB NOT NULL DEFAULT '{}',
    issued_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at  TIMESTAMPTZ NOT NULL,
    csrf_token  TEXT NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    
    CONSTRAINT core_flows_type_check
        CHECK (type IN ('login', 'registration', 'recovery', 'settings', 'verification')),
    CONSTRAINT core_flows_state_check
        CHECK (state IN ('active', 'complete', 'failed'))
);

CREATE INDEX idx_core_flows_type_state ON core_flows(type, state);
CREATE INDEX idx_core_flows_expires ON core_flows(expires_at) WHERE state = 'active';
CREATE INDEX idx_core_flows_identity ON core_flows(identity_id) WHERE identity_id IS NOT NULL;

-- Continuity Containers (cross-redirect state)
CREATE TABLE core_continuity_containers (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name        TEXT NOT NULL,
    identity_id UUID REFERENCES core_identities(id) ON DELETE CASCADE,
    session_id  UUID REFERENCES core_sessions(id) ON DELETE SET NULL,
    payload     JSONB NOT NULL DEFAULT '{}',
    expires_at  TIMESTAMPTZ NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_core_cc_expires ON core_continuity_containers(expires_at);
CREATE INDEX idx_core_cc_name ON core_continuity_containers(name, identity_id);

-- Triggers
CREATE TRIGGER update_core_flows_updated_at
    BEFORE UPDATE ON core_flows
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();
