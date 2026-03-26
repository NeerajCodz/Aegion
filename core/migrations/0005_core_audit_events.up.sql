-- =============================================================================
-- Core Audit & Events
-- Migration: 0005_core_audit_events
-- =============================================================================

-- Audit Events (Append-Only)
CREATE TABLE core_audit_events (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    event_type   TEXT NOT NULL,
    actor_id     UUID,
    actor_type   TEXT,
    target_type  TEXT,
    target_id    TEXT,
    action       TEXT NOT NULL,
    outcome      TEXT NOT NULL,
    before_state JSONB,
    after_state  JSONB,
    metadata     JSONB NOT NULL DEFAULT '{}',
    ip_address   TEXT,
    user_agent   TEXT,
    occurred_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    
    CONSTRAINT core_audit_events_actor_type_check
        CHECK (actor_type IN ('user', 'admin', 'system', 'module')),
    CONSTRAINT core_audit_events_outcome_check
        CHECK (outcome IN ('success', 'failure'))
);

-- Row Level Security for append-only
ALTER TABLE core_audit_events ENABLE ROW LEVEL SECURITY;

-- Allow insert only (no update, no delete)
CREATE POLICY audit_insert_only ON core_audit_events 
    FOR INSERT WITH CHECK (TRUE);

CREATE INDEX idx_core_audit_actor ON core_audit_events(actor_id, occurred_at DESC) WHERE actor_id IS NOT NULL;
CREATE INDEX idx_core_audit_target ON core_audit_events(target_type, target_id, occurred_at DESC);
CREATE INDEX idx_core_audit_type ON core_audit_events(event_type, occurred_at DESC);
CREATE INDEX idx_core_audit_occurred ON core_audit_events(occurred_at DESC);

-- Event Bus Events (internal async events)
CREATE TABLE core_event_bus_events (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    event_type    TEXT NOT NULL,
    source_module TEXT NOT NULL,
    entity_type   TEXT,
    entity_id     TEXT,
    identity_id   UUID,
    payload       JSONB NOT NULL,
    metadata      JSONB NOT NULL DEFAULT '{}',
    occurred_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_evbus_events_occurred ON core_event_bus_events(occurred_at DESC);
CREATE INDEX idx_evbus_events_type ON core_event_bus_events(event_type, occurred_at DESC);

-- Event Bus Deliveries (tracks delivery to each subscriber)
CREATE TABLE core_event_bus_deliveries (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    event_id      UUID NOT NULL REFERENCES core_event_bus_events(id),
    subscriber    TEXT NOT NULL,
    status        TEXT NOT NULL DEFAULT 'pending',
    attempt_count INT NOT NULL DEFAULT 0,
    last_error    TEXT,
    next_retry_at TIMESTAMPTZ,
    delivered_at  TIMESTAMPTZ,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    
    CONSTRAINT core_event_bus_deliveries_status_check
        CHECK (status IN ('pending', 'delivered', 'failed', 'dead_lettered'))
);

CREATE INDEX idx_evbus_pending 
    ON core_event_bus_deliveries(subscriber, next_retry_at) 
    WHERE status IN ('pending', 'failed');
CREATE INDEX idx_evbus_event 
    ON core_event_bus_deliveries(event_id);

-- Triggers
CREATE TRIGGER update_core_event_bus_deliveries_updated_at
    BEFORE UPDATE ON core_event_bus_deliveries
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();
