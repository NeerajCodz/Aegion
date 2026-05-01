-- =============================================================================
-- Rollback: Core Identity Tables
-- =============================================================================

DROP TRIGGER IF EXISTS update_core_identity_addresses_updated_at ON core_identity_addresses;
DROP TRIGGER IF EXISTS update_core_identity_schemas_updated_at ON core_identity_schemas;
DROP TRIGGER IF EXISTS update_core_identities_updated_at ON core_identities;
DROP FUNCTION IF EXISTS update_updated_at_column();

DROP TABLE IF EXISTS core_identity_addresses;
DROP TABLE IF EXISTS core_identities;
DROP TABLE IF EXISTS core_identity_schemas;
