-- =============================================================================
-- Core Courier (Email/SMS Queue)
-- Migration: 0004_core_courier
-- =============================================================================

-- Courier Messages
CREATE TABLE core_courier_messages (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    type            TEXT NOT NULL,
    status          TEXT NOT NULL DEFAULT 'queued',
    recipient       TEXT NOT NULL,
    subject         TEXT,
    body            TEXT NOT NULL,
    template_id     TEXT,
    template_data   JSONB,
    send_count      INT NOT NULL DEFAULT 0,
    last_error      TEXT,
    idempotency_key TEXT UNIQUE,
    send_after      TIMESTAMPTZ,
    sent_at         TIMESTAMPTZ,
    identity_id     UUID REFERENCES core_identities(id) ON DELETE SET NULL,
    source_module   TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    
    CONSTRAINT core_courier_messages_type_check
        CHECK (type IN ('email', 'sms')),
    CONSTRAINT core_courier_messages_status_check
        CHECK (status IN ('queued', 'processing', 'sent', 'failed', 'abandoned', 'cancelled'))
);

CREATE INDEX idx_core_courier_status 
    ON core_courier_messages(status, send_after) 
    WHERE status IN ('queued', 'processing');
CREATE INDEX idx_core_courier_identity 
    ON core_courier_messages(identity_id) 
    WHERE identity_id IS NOT NULL;

-- Courier Templates
CREATE TABLE core_courier_templates (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name       TEXT NOT NULL UNIQUE,
    type       TEXT NOT NULL,
    subject    TEXT,
    body       TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    
    CONSTRAINT core_courier_templates_type_check
        CHECK (type IN ('email', 'sms'))
);

-- Triggers
CREATE TRIGGER update_core_courier_messages_updated_at
    BEFORE UPDATE ON core_courier_messages
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TRIGGER update_core_courier_templates_updated_at
    BEFORE UPDATE ON core_courier_templates
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();
