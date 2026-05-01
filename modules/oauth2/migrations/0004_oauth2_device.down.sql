-- =============================================================================
-- OAuth2 Module Tables - Part 4: Device Authorization (Rollback)
-- Migration: 0004_oauth2_device
-- =============================================================================

DROP TABLE IF EXISTS oa2_jwt_assertions CASCADE;
DROP TABLE IF EXISTS oa2_device_codes CASCADE;
