-- =============================================================================
-- Rollback: Core System Config & Keys
-- =============================================================================

DROP TRIGGER IF EXISTS update_core_system_config_updated_at ON core_system_config;
DROP TABLE IF EXISTS core_system_config;
DROP TABLE IF EXISTS core_signing_keys;
