-- =============================================================================
-- Analytics Dashboards Schema
-- Migration: 0005_dashboards
-- =============================================================================

-- Analytics Dashboard Definitions Table
CREATE TABLE IF NOT EXISTS analytics_dashboards (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name VARCHAR NOT NULL UNIQUE,
    description TEXT,
    config JSON NOT NULL DEFAULT '{}',
    owner_id UUID,
    public BOOLEAN NOT NULL DEFAULT FALSE,
    pinned BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

ALTER TABLE analytics_dashboards
    ADD COLUMN IF NOT EXISTS pinned BOOLEAN NOT NULL DEFAULT FALSE;

-- Index for dashboard ownership and filtering
CREATE INDEX IF NOT EXISTS idx_analytics_dashboards_owner 
    ON analytics_dashboards(owner_id);

-- Index for public dashboards
CREATE INDEX IF NOT EXISTS idx_analytics_dashboards_public 
    ON analytics_dashboards(public) WHERE public = TRUE;

-- Index for pinned dashboards
CREATE INDEX IF NOT EXISTS idx_analytics_dashboards_pinned 
    ON analytics_dashboards(pinned) WHERE pinned = TRUE;

-- Dashboard Shared Links Table
CREATE TABLE IF NOT EXISTS analytics_dashboard_shares (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    dashboard_id UUID NOT NULL REFERENCES analytics_dashboards(id) ON DELETE CASCADE,
    token VARCHAR NOT NULL UNIQUE,
    expires_at TIMESTAMPTZ,
    read_only BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Index for share token lookups
CREATE INDEX IF NOT EXISTS idx_analytics_dashboard_shares_token 
    ON analytics_dashboard_shares(token);

-- Index for dashboard shares
CREATE INDEX IF NOT EXISTS idx_analytics_dashboard_shares_dashboard 
    ON analytics_dashboard_shares(dashboard_id);

-- Dashboard Metrics Metadata Table
CREATE TABLE IF NOT EXISTS analytics_dashboard_metrics (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    dashboard_id UUID NOT NULL REFERENCES analytics_dashboards(id) ON DELETE CASCADE,
    metric_name VARCHAR NOT NULL,
    last_computed TIMESTAMPTZ,
    next_compute TIMESTAMPTZ,
    compute_status VARCHAR NOT NULL DEFAULT 'pending',
    error_message TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Index for dashboard metrics
CREATE INDEX IF NOT EXISTS idx_analytics_dashboard_metrics_dashboard 
    ON analytics_dashboard_metrics(dashboard_id);

-- Index for compute status
CREATE INDEX IF NOT EXISTS idx_analytics_dashboard_metrics_status 
    ON analytics_dashboard_metrics(compute_status) WHERE compute_status IN ('pending', 'running');

-- Alert Thresholds Table
CREATE TABLE IF NOT EXISTS analytics_dashboard_alerts (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    dashboard_id UUID NOT NULL REFERENCES analytics_dashboards(id) ON DELETE CASCADE,
    metric_name VARCHAR NOT NULL,
    operator VARCHAR NOT NULL, -- "gt", "lt", "eq", "gte", "lte"
    threshold DOUBLE PRECISION NOT NULL,
    severity_level VARCHAR NOT NULL DEFAULT 'warning', -- "info", "warning", "critical"
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (dashboard_id, metric_name)
);

-- Index for alert lookups
CREATE INDEX IF NOT EXISTS idx_analytics_dashboard_alerts_dashboard 
    ON analytics_dashboard_alerts(dashboard_id);

-- Index for enabled alerts
CREATE INDEX IF NOT EXISTS idx_analytics_dashboard_alerts_enabled 
    ON analytics_dashboard_alerts(enabled) WHERE enabled = TRUE;

-- Dashboard Query Cache Table
CREATE TABLE IF NOT EXISTS analytics_dashboard_query_cache (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    query_id VARCHAR NOT NULL,
    result JSON NOT NULL,
    execution_time_ms INTEGER,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at TIMESTAMPTZ NOT NULL,
    UNIQUE (query_id)
);

-- Index for cache expiration cleanup
CREATE INDEX IF NOT EXISTS idx_analytics_dashboard_query_cache_expires 
    ON analytics_dashboard_query_cache(expires_at);

-- Dashboard Access Logs Table (for auditing)
CREATE TABLE IF NOT EXISTS analytics_dashboard_access_logs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    dashboard_id UUID NOT NULL REFERENCES analytics_dashboards(id) ON DELETE CASCADE,
    user_id UUID,
    action VARCHAR NOT NULL, -- "view", "edit", "share", "export"
    details JSON,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Index for dashboard access logs
CREATE INDEX IF NOT EXISTS idx_analytics_dashboard_access_logs_dashboard 
    ON analytics_dashboard_access_logs(dashboard_id, created_at DESC);

-- Index for user access logs
CREATE INDEX IF NOT EXISTS idx_analytics_dashboard_access_logs_user 
    ON analytics_dashboard_access_logs(user_id, created_at DESC);

-- Trigger to update updated_at on dashboards
CREATE OR REPLACE FUNCTION update_analytics_dashboards_updated_at()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS analytics_dashboards_updated_at_trigger ON analytics_dashboards;
CREATE TRIGGER analytics_dashboards_updated_at_trigger
    BEFORE UPDATE ON analytics_dashboards
    FOR EACH ROW
    EXECUTE FUNCTION update_analytics_dashboards_updated_at();

-- Trigger to update updated_at on shares
CREATE OR REPLACE FUNCTION update_analytics_dashboard_shares_updated_at()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS analytics_dashboard_shares_updated_at_trigger ON analytics_dashboard_shares;
CREATE TRIGGER analytics_dashboard_shares_updated_at_trigger
    BEFORE UPDATE ON analytics_dashboard_shares
    FOR EACH ROW
    EXECUTE FUNCTION update_analytics_dashboard_shares_updated_at();

-- Trigger to update updated_at on alerts
CREATE OR REPLACE FUNCTION update_analytics_dashboard_alerts_updated_at()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS analytics_dashboard_alerts_updated_at_trigger ON analytics_dashboard_alerts;
CREATE TRIGGER analytics_dashboard_alerts_updated_at_trigger
    BEFORE UPDATE ON analytics_dashboard_alerts
    FOR EACH ROW
    EXECUTE FUNCTION update_analytics_dashboard_alerts_updated_at();

-- Trigger to update updated_at on metrics
CREATE OR REPLACE FUNCTION update_analytics_dashboard_metrics_updated_at()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS analytics_dashboard_metrics_updated_at_trigger ON analytics_dashboard_metrics;
CREATE TRIGGER analytics_dashboard_metrics_updated_at_trigger
    BEFORE UPDATE ON analytics_dashboard_metrics
    FOR EACH ROW
    EXECUTE FUNCTION update_analytics_dashboard_metrics_updated_at();
