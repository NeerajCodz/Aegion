-- =============================================================================
-- Analytics Base Schema
-- Migration: 0001_analytics_base
-- =============================================================================

-- Analytics Events Table (main fact table)
CREATE TABLE IF NOT EXISTS analytics_events (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    category VARCHAR NOT NULL,
    event_type VARCHAR NOT NULL,
    user_id UUID,
    session_id UUID,
    data JSONB DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Time-range index for monthly and windowed event queries.
CREATE INDEX IF NOT EXISTS idx_analytics_events_month 
    ON analytics_events(created_at DESC, category);

-- Index by category for filtering
CREATE INDEX IF NOT EXISTS idx_analytics_events_category 
    ON analytics_events(category, created_at DESC);

-- Index by user_id for user-based queries
CREATE INDEX IF NOT EXISTS idx_analytics_events_user_id 
    ON analytics_events(user_id) WHERE user_id IS NOT NULL;

-- Index by session_id for session analysis
CREATE INDEX IF NOT EXISTS idx_analytics_events_session_id 
    ON analytics_events(session_id) WHERE session_id IS NOT NULL;

-- Index for JSON data queries
CREATE INDEX IF NOT EXISTS idx_analytics_events_data 
    ON analytics_events USING GIN(data);

-- Index for created_at (for sync position tracking)
CREATE INDEX IF NOT EXISTS idx_analytics_events_created_at 
    ON analytics_events(created_at DESC);

-- Analytics Metrics Table (aggregated data)
CREATE TABLE IF NOT EXISTS analytics_metrics (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name VARCHAR NOT NULL,
    category VARCHAR NOT NULL,
    value DOUBLE PRECISION NOT NULL,
    unit VARCHAR,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Index for metric lookups
CREATE INDEX IF NOT EXISTS idx_analytics_metrics_name_category 
    ON analytics_metrics(name, category);

-- Index for time-based queries
CREATE INDEX IF NOT EXISTS idx_analytics_metrics_created 
    ON analytics_metrics(created_at DESC);

-- Analytics Dashboards Table
CREATE TABLE IF NOT EXISTS analytics_dashboards (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name VARCHAR NOT NULL UNIQUE,
    description TEXT,
    config JSON NOT NULL DEFAULT '{}',
    owner_id UUID NOT NULL,
    public BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Index for dashboard ownership and filtering
CREATE INDEX IF NOT EXISTS idx_analytics_dashboards_owner 
    ON analytics_dashboards(owner_id);

-- Index for public dashboards
CREATE INDEX IF NOT EXISTS idx_analytics_dashboards_public 
    ON analytics_dashboards(public) WHERE public = TRUE;

-- Analytics Queries Table (saved analytics queries)
CREATE TABLE IF NOT EXISTS analytics_queries (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name VARCHAR NOT NULL,
    description TEXT,
    sql TEXT NOT NULL,
    owner_id UUID NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Index for query ownership
CREATE INDEX IF NOT EXISTS idx_analytics_queries_owner 
    ON analytics_queries(owner_id);

-- Index for finding queries by name
CREATE INDEX IF NOT EXISTS idx_analytics_queries_name 
    ON analytics_queries(name);

-- Analytics Webhooks Table
CREATE TABLE IF NOT EXISTS analytics_webhooks (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    url VARCHAR NOT NULL,
    event_type VARCHAR NOT NULL,
    secret TEXT NOT NULL,
    active BOOLEAN NOT NULL DEFAULT TRUE,
    retry_count INTEGER DEFAULT 0,
    last_fired_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Index for webhook filtering
CREATE INDEX IF NOT EXISTS idx_analytics_webhooks_event_type 
    ON analytics_webhooks(event_type) WHERE active = TRUE;

-- Index for finding active webhooks
CREATE INDEX IF NOT EXISTS idx_analytics_webhooks_active 
    ON analytics_webhooks(active);

-- Trigger to update updated_at on events
CREATE OR REPLACE FUNCTION update_analytics_events_updated_at()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER analytics_events_updated_at_trigger
    BEFORE UPDATE ON analytics_events
    FOR EACH ROW
    EXECUTE FUNCTION update_analytics_events_updated_at();

-- Trigger to update updated_at on metrics
CREATE OR REPLACE FUNCTION update_analytics_metrics_updated_at()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER analytics_metrics_updated_at_trigger
    BEFORE UPDATE ON analytics_metrics
    FOR EACH ROW
    EXECUTE FUNCTION update_analytics_metrics_updated_at();

-- Trigger to update updated_at on dashboards
CREATE OR REPLACE FUNCTION update_analytics_dashboards_updated_at()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER analytics_dashboards_updated_at_trigger
    BEFORE UPDATE ON analytics_dashboards
    FOR EACH ROW
    EXECUTE FUNCTION update_analytics_dashboards_updated_at();

-- Trigger to update updated_at on queries
CREATE OR REPLACE FUNCTION update_analytics_queries_updated_at()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER analytics_queries_updated_at_trigger
    BEFORE UPDATE ON analytics_queries
    FOR EACH ROW
    EXECUTE FUNCTION update_analytics_queries_updated_at();

-- Trigger to update updated_at on webhooks
CREATE OR REPLACE FUNCTION update_analytics_webhooks_updated_at()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER analytics_webhooks_updated_at_trigger
    BEFORE UPDATE ON analytics_webhooks
    FOR EACH ROW
    EXECUTE FUNCTION update_analytics_webhooks_updated_at();
