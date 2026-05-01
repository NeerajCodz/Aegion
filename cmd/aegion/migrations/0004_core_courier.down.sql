-- =============================================================================
-- Rollback: Core Courier
-- =============================================================================

DROP TRIGGER IF EXISTS update_core_courier_templates_updated_at ON core_courier_templates;
DROP TRIGGER IF EXISTS update_core_courier_messages_updated_at ON core_courier_messages;
DROP TABLE IF EXISTS core_courier_templates;
DROP TABLE IF EXISTS core_courier_messages;
