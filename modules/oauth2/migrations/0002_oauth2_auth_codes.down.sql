-- =============================================================================
-- OAuth2 Module Tables - Part 2: Authorization Codes & Consent (Rollback)
-- Migration: 0002_oauth2_auth_codes
-- =============================================================================

DROP TABLE IF EXISTS oa2_consent_challenges CASCADE;
DROP TABLE IF EXISTS oa2_login_challenges CASCADE;
DROP TABLE IF EXISTS oa2_consent_sessions CASCADE;
DROP TABLE IF EXISTS oa2_auth_codes CASCADE;
