-- =============================================================================
-- Rollback: Core Sessions
-- =============================================================================

DROP TRIGGER IF EXISTS update_core_sessions_updated_at ON core_sessions;
DROP TABLE IF EXISTS core_session_auth_methods;
DROP TABLE IF EXISTS core_sessions;
