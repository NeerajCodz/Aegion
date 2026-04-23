-- =============================================================================
-- Analytics Base Schema Rollback
-- Migration: 0001_analytics_base (down)
-- =============================================================================

-- Drop triggers first
DROP TRIGGER IF EXISTS analytics_events_updated_at_trigger ON analytics_events;
DROP TRIGGER IF EXISTS analytics_metrics_updated_at_trigger ON analytics_metrics;
DROP TRIGGER IF EXISTS analytics_dashboards_updated_at_trigger ON analytics_dashboards;
DROP TRIGGER IF EXISTS analytics_queries_updated_at_trigger ON analytics_queries;
DROP TRIGGER IF EXISTS analytics_webhooks_updated_at_trigger ON analytics_webhooks;

-- Drop trigger functions
DROP FUNCTION IF EXISTS update_analytics_events_updated_at();
DROP FUNCTION IF EXISTS update_analytics_metrics_updated_at();
DROP FUNCTION IF EXISTS update_analytics_dashboards_updated_at();
DROP FUNCTION IF EXISTS update_analytics_queries_updated_at();
DROP FUNCTION IF EXISTS update_analytics_webhooks_updated_at();

-- Drop tables
DROP TABLE IF EXISTS analytics_webhooks;
DROP TABLE IF EXISTS analytics_queries;
DROP TABLE IF EXISTS analytics_dashboards;
DROP TABLE IF EXISTS analytics_metrics;
DROP TABLE IF EXISTS analytics_events;
