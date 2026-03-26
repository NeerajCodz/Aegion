-- =============================================================================
-- Rollback: Core Audit & Events
-- =============================================================================

DROP TRIGGER IF EXISTS update_core_event_bus_deliveries_updated_at ON core_event_bus_deliveries;
DROP TABLE IF EXISTS core_event_bus_deliveries;
DROP TABLE IF EXISTS core_event_bus_events;

DROP POLICY IF EXISTS audit_insert_only ON core_audit_events;
ALTER TABLE core_audit_events DISABLE ROW LEVEL SECURITY;
DROP TABLE IF EXISTS core_audit_events;
