-- =============================================================================
-- OAuth2 Module Tables - Part 1: Clients (Rollback)
-- Migration: 0001_oauth2_clients
-- =============================================================================

DROP TABLE IF EXISTS oa2_client_origins CASCADE;
DROP TABLE IF EXISTS oa2_clients CASCADE;
