-- =============================================================================
-- Analytics Sync Position Tracking
-- Migration: 0002_sync_position
-- =============================================================================

-- Sync Position Table (tracks progress of batch syncs)
CREATE TABLE IF NOT EXISTS analytics_sync_position (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    strategy VARCHAR NOT NULL,           -- "real_time", "batch", "async"
    source_table VARCHAR NOT NULL,       -- table being synced from
    last_synced_id UUID,                 -- last ID synced (for resumable syncs)
    last_synced_at TIMESTAMPTZ,          -- timestamp of last successful sync
    checkpoint_data JSONB DEFAULT '{}',  -- strategy-specific checkpoint data
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(strategy, source_table)
);

-- Index for efficient position lookups
CREATE INDEX IF NOT EXISTS idx_sync_position_strategy_table 
    ON analytics_sync_position(strategy, source_table);

-- Sync Events Table (audit trail of sync operations)
CREATE TABLE IF NOT EXISTS analytics_sync_events (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    strategy VARCHAR NOT NULL,           -- which strategy performed the sync
    event_type VARCHAR NOT NULL,         -- "sync_start", "sync_complete", "sync_error"
    source_table VARCHAR,
    records_synced INTEGER,
    error_message TEXT,
    duration_ms INTEGER,                 -- how long the sync took
    metadata JSONB DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Index for finding recent sync events
CREATE INDEX IF NOT EXISTS idx_sync_events_created 
    ON analytics_sync_events(created_at DESC);

-- Index for strategy/table lookups
CREATE INDEX IF NOT EXISTS idx_sync_events_strategy_table 
    ON analytics_sync_events(strategy, source_table, created_at DESC);

-- Dead Letter Queue for failed events (async strategy)
CREATE TABLE IF NOT EXISTS analytics_dlq_events (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    event_data JSONB NOT NULL,
    error_message TEXT NOT NULL,
    retry_count INTEGER DEFAULT 0,
    last_error_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Index for DLQ management
CREATE INDEX IF NOT EXISTS idx_dlq_events_retry_count 
    ON analytics_dlq_events(retry_count);
CREATE INDEX IF NOT EXISTS idx_dlq_events_created 
    ON analytics_dlq_events(created_at DESC);

-- Trigger to update analytics_sync_position.updated_at
CREATE OR REPLACE FUNCTION update_analytics_sync_position_updated_at()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER analytics_sync_position_updated_at_trigger
    BEFORE UPDATE ON analytics_sync_position
    FOR EACH ROW
    EXECUTE FUNCTION update_analytics_sync_position_updated_at();

-- Trigger to update analytics_dlq_events.updated_at
CREATE OR REPLACE FUNCTION update_analytics_dlq_events_updated_at()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER analytics_dlq_events_updated_at_trigger
    BEFORE UPDATE ON analytics_dlq_events
    FOR EACH ROW
    EXECUTE FUNCTION update_analytics_dlq_events_updated_at();
