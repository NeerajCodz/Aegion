-- =============================================================================
-- Rollback: Core Flows
-- =============================================================================

DROP TRIGGER IF EXISTS update_core_flows_updated_at ON core_flows;
DROP TABLE IF EXISTS core_continuity_containers;
DROP TABLE IF EXISTS core_flows;
