-- =============================================================================
-- Analytics Performance Indexes
-- Migration: 0006_performance_indexes
-- Purpose: Add comprehensive indexes for production performance
-- =============================================================================

-- ============================================================================
-- ANALYTICS_EVENTS TABLE INDEXES
-- ============================================================================

-- Single column indexes for basic filtering
CREATE INDEX IF NOT EXISTS idx_ae_event_type 
    ON analytics_events(event_type);

CREATE INDEX IF NOT EXISTS idx_ae_category 
    ON analytics_events(category);

-- Time-range query optimization (descending for recent events)
CREATE INDEX IF NOT EXISTS idx_ae_created_at_desc 
    ON analytics_events(created_at DESC);

CREATE INDEX IF NOT EXISTS idx_ae_created_at_asc 
    ON analytics_events(created_at ASC);

-- User and session based queries
CREATE INDEX IF NOT EXISTS idx_ae_user_id 
    ON analytics_events(user_id)
    WHERE user_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_ae_session_id 
    ON analytics_events(session_id)
    WHERE session_id IS NOT NULL;

-- Composite indexes for common filter combinations
CREATE INDEX IF NOT EXISTS idx_ae_category_created_at 
    ON analytics_events(category, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_ae_event_type_created_at 
    ON analytics_events(event_type, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_ae_user_id_created_at 
    ON analytics_events(user_id, created_at DESC)
    WHERE user_id IS NOT NULL;


-- JSON data searching
CREATE INDEX IF NOT EXISTS idx_ae_data_gin 
    ON analytics_events USING GIN(data);


-- ============================================================================
-- ANALYTICS_METRICS TABLE INDEXES
-- ============================================================================

CREATE INDEX IF NOT EXISTS idx_am_name_category 
    ON analytics_metrics(name, category);

CREATE INDEX IF NOT EXISTS idx_am_created_at 
    ON analytics_metrics(created_at DESC);

CREATE INDEX IF NOT EXISTS idx_am_category 
    ON analytics_metrics(category);

CREATE INDEX IF NOT EXISTS idx_am_category_created_at 
    ON analytics_metrics(category, created_at DESC);


-- ============================================================================
-- ANALYTICS_DASHBOARDS TABLE INDEXES
-- ============================================================================

CREATE INDEX IF NOT EXISTS idx_ad_owner_id 
    ON analytics_dashboards(owner_id);

CREATE INDEX IF NOT EXISTS idx_ad_public 
    ON analytics_dashboards(public)
    WHERE public = TRUE;

CREATE INDEX IF NOT EXISTS idx_ad_owner_pinned 
    ON analytics_dashboards(owner_id, pinned);

CREATE INDEX IF NOT EXISTS idx_ad_name 
    ON analytics_dashboards(name);

CREATE INDEX IF NOT EXISTS idx_ad_updated_at 
    ON analytics_dashboards(updated_at DESC);


-- ============================================================================
-- ANALYTICS_QUERIES TABLE INDEXES
-- ============================================================================

CREATE INDEX IF NOT EXISTS idx_aq_owner_id 
    ON analytics_queries(owner_id);

CREATE INDEX IF NOT EXISTS idx_aq_name 
    ON analytics_queries(name);

CREATE INDEX IF NOT EXISTS idx_aq_owner_created_at 
    ON analytics_queries(owner_id, created_at DESC);


-- ============================================================================
-- ANALYTICS_WEBHOOKS TABLE INDEXES
-- ============================================================================

CREATE INDEX IF NOT EXISTS idx_aw_event_type_active 
    ON analytics_webhooks(event_type, active)
    WHERE active = TRUE;

CREATE INDEX IF NOT EXISTS idx_aw_active 
    ON analytics_webhooks(active);

CREATE INDEX IF NOT EXISTS idx_aw_url 
    ON analytics_webhooks(url);


-- ============================================================================
-- PERFORMANCE ANALYSIS HELPERS
-- ============================================================================

-- Analyze tables to update statistics
ANALYZE;

-- Table stats query
SELECT 
    schemaname,
    tablename,
    pg_size_pretty(pg_total_relation_size(schemaname||'.'||tablename)) AS size
FROM pg_tables
WHERE schemaname = 'public'
AND tablename LIKE 'analytics_%'
ORDER BY pg_total_relation_size(schemaname||'.'||tablename) DESC;
