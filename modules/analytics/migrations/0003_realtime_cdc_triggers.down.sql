-- =============================================================================
-- Analytics Real-Time Sync Triggers Rollback (CDC)
-- Migration: 0003_realtime_cdc_triggers (down)
-- =============================================================================

-- Drop triggers
DROP TRIGGER IF EXISTS analytics_events_cdc_trigger ON analytics_events;
DROP TRIGGER IF EXISTS analytics_metrics_cdc_trigger ON analytics_metrics;

-- Drop trigger functions
DROP FUNCTION IF EXISTS capture_analytics_events_changes();
DROP FUNCTION IF EXISTS capture_analytics_metrics_changes();
