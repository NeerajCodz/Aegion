-- =============================================================================
-- Analytics Sync Position Tracking Rollback
-- Migration: 0002_sync_position (down)
-- =============================================================================

-- Drop triggers
DROP TRIGGER IF EXISTS analytics_sync_position_updated_at_trigger ON analytics_sync_position;
DROP TRIGGER IF EXISTS analytics_dlq_events_updated_at_trigger ON analytics_dlq_events;

-- Drop trigger functions
DROP FUNCTION IF EXISTS update_analytics_sync_position_updated_at();
DROP FUNCTION IF EXISTS update_analytics_dlq_events_updated_at();

-- Drop tables
DROP TABLE IF EXISTS analytics_dlq_events;
DROP TABLE IF EXISTS analytics_sync_events;
DROP TABLE IF EXISTS analytics_sync_position;
