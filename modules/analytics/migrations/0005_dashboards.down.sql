-- =============================================================================
-- Analytics Dashboards Schema Rollback
-- Migration: 0005_dashboards
-- =============================================================================

DROP TRIGGER IF EXISTS analytics_dashboard_metrics_updated_at_trigger ON analytics_dashboard_metrics;
DROP FUNCTION IF EXISTS update_analytics_dashboard_metrics_updated_at();

DROP TRIGGER IF EXISTS analytics_dashboard_alerts_updated_at_trigger ON analytics_dashboard_alerts;
DROP FUNCTION IF EXISTS update_analytics_dashboard_alerts_updated_at();

DROP TRIGGER IF EXISTS analytics_dashboard_shares_updated_at_trigger ON analytics_dashboard_shares;
DROP FUNCTION IF EXISTS update_analytics_dashboard_shares_updated_at();

DROP TRIGGER IF EXISTS analytics_dashboards_updated_at_trigger ON analytics_dashboards;
DROP FUNCTION IF EXISTS update_analytics_dashboards_updated_at();

DROP TABLE IF EXISTS analytics_dashboard_access_logs;
DROP TABLE IF EXISTS analytics_dashboard_query_cache;
DROP TABLE IF EXISTS analytics_dashboard_alerts;
DROP TABLE IF EXISTS analytics_dashboard_metrics;
DROP TABLE IF EXISTS analytics_dashboard_shares;
DROP TABLE IF EXISTS analytics_dashboards;
