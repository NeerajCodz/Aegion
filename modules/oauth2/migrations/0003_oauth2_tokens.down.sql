-- =============================================================================
-- OAuth2 Module Tables - Part 3: Access & Refresh Tokens (Rollback)
-- Migration: 0003_oauth2_tokens
-- =============================================================================

DROP TABLE IF EXISTS oa2_token_revocations CASCADE;
DROP TABLE IF EXISTS oa2_id_tokens CASCADE;
DROP TABLE IF EXISTS oa2_refresh_tokens CASCADE;
DROP TABLE IF EXISTS oa2_access_tokens CASCADE;
