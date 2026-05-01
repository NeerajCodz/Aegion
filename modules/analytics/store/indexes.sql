-- =============================================================================
-- Analytics Performance Indexes Strategy
-- File: modules/analytics/store/indexes.sql
-- Purpose: Define all indexes needed for production-grade performance
-- =============================================================================

-- ============================================================================
-- ANALYTICS_EVENTS TABLE INDEXES
-- ============================================================================

-- Single column indexes for basic filtering
CREATE INDEX IF NOT EXISTS idx_ae_event_type 
    ON analytics_events(event_type)
    WHERE archived_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_ae_category 
    ON analytics_events(category)
    WHERE archived_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_ae_source_system 
    ON analytics_events(source_system)
    WHERE archived_at IS NULL;

-- Time-range query optimization (descending for recent events)
CREATE INDEX IF NOT EXISTS idx_ae_created_at_desc 
    ON analytics_events(created_at DESC)
    WHERE archived_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_ae_created_at_asc 
    ON analytics_events(created_at ASC)
    WHERE archived_at IS NULL;

-- User and session based queries
CREATE INDEX IF NOT EXISTS idx_ae_user_id 
    ON analytics_events(user_id)
    WHERE user_id IS NOT NULL AND archived_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_ae_session_id 
    ON analytics_events(session_id)
    WHERE session_id IS NOT NULL AND archived_at IS NULL;

-- Composite indexes for common filter combinations (order matters!)

-- Category + Time range (very common: "events by category in date range")
CREATE INDEX IF NOT EXISTS idx_ae_category_created_at 
    ON analytics_events(category, created_at DESC)
    WHERE archived_at IS NULL;

-- Event type + Time range (auth events filtering)
CREATE INDEX IF NOT EXISTS idx_ae_event_type_created_at 
    ON analytics_events(event_type, created_at DESC)
    WHERE archived_at IS NULL;

-- User + Time range (user activity analysis)
CREATE INDEX IF NOT EXISTS idx_ae_user_id_created_at 
    ON analytics_events(user_id, created_at DESC)
    WHERE user_id IS NOT NULL AND archived_at IS NULL;

-- Source system + Time range
CREATE INDEX IF NOT EXISTS idx_ae_source_system_created_at 
    ON analytics_events(source_system, created_at DESC)
    WHERE archived_at IS NULL;

-- Partial index for active events only (excludes archived records)
CREATE INDEX IF NOT EXISTS idx_ae_archived_at_null 
    ON analytics_events(created_at DESC)
    WHERE archived_at IS NULL;

-- Index for soft delete queries
CREATE INDEX IF NOT EXISTS idx_ae_archived_at 
    ON analytics_events(archived_at)
    WHERE archived_at IS NOT NULL;

-- JSON data searching (for DuckDB/PostgreSQL compatibility)
CREATE INDEX IF NOT EXISTS idx_ae_data_gin 
    ON analytics_events USING GIN(data);


-- ============================================================================
-- ANALYTICS_METRICS TABLE INDEXES
-- ============================================================================

-- Metric lookup by name and category (common composite filter)
CREATE INDEX IF NOT EXISTS idx_am_name_category 
    ON analytics_metrics(name, category);

-- Time-based metric queries
CREATE INDEX IF NOT EXISTS idx_am_created_at 
    ON analytics_metrics(created_at DESC);

-- Category-based metric filtering
CREATE INDEX IF NOT EXISTS idx_am_category 
    ON analytics_metrics(category);

-- Composite: Category + Time (metric aggregations)
CREATE INDEX IF NOT EXISTS idx_am_category_created_at 
    ON analytics_metrics(category, created_at DESC);


-- ============================================================================
-- ANALYTICS_DASHBOARDS TABLE INDEXES
-- ============================================================================

-- Owner filtering (very common: "my dashboards")
CREATE INDEX IF NOT EXISTS idx_ad_owner_id 
    ON analytics_dashboards(owner_id);

-- Public dashboard filtering
CREATE INDEX IF NOT EXISTS idx_ad_public 
    ON analytics_dashboards(public)
    WHERE public = TRUE;

-- Default dashboard filtering
CREATE INDEX IF NOT EXISTS idx_ad_is_default 
    ON analytics_dashboards(is_default)
    WHERE is_default = TRUE;

-- Owner + Default (find user's default dashboard)
CREATE INDEX IF NOT EXISTS idx_ad_owner_is_default 
    ON analytics_dashboards(owner_id, is_default);

-- Name-based dashboard lookup (dashboard switching)
CREATE INDEX IF NOT EXISTS idx_ad_name 
    ON analytics_dashboards(name);

-- Updated_at for "recently modified" queries
CREATE INDEX IF NOT EXISTS idx_ad_updated_at 
    ON analytics_dashboards(updated_at DESC);


-- ============================================================================
-- ANALYTICS_QUERIES TABLE INDEXES
-- ============================================================================

-- Query ownership lookup
CREATE INDEX IF NOT EXISTS idx_aq_owner_id 
    ON analytics_queries(owner_id);

-- Query name filtering (search by name)
CREATE INDEX IF NOT EXISTS idx_aq_name 
    ON analytics_queries(name);

-- Owner + Created for "my recent queries"
CREATE INDEX IF NOT EXISTS idx_aq_owner_created_at 
    ON analytics_queries(owner_id, created_at DESC);


-- ============================================================================
-- ANALYTICS_WEBHOOKS TABLE INDEXES
-- ============================================================================

-- Filter webhooks by event type (active only)
CREATE INDEX IF NOT EXISTS idx_aw_event_type_active 
    ON analytics_webhooks(event_type, active)
    WHERE active = TRUE;

-- Active webhook filtering
CREATE INDEX IF NOT EXISTS idx_aw_active 
    ON analytics_webhooks(active);

-- URL-based webhook lookup
CREATE INDEX IF NOT EXISTS idx_aw_url 
    ON analytics_webhooks(url);


-- ============================================================================
-- QUERY EXECUTION OPTIMIZATION HINTS
-- ============================================================================

-- For DuckDB: Enable adaptive query execution
-- This enables DuckDB to reorder predicates based on selectivity

-- For common date range queries, partition by month:
-- SELECT * FROM analytics_events 
--   WHERE category = ? AND created_at BETWEEN ? AND ?
-- EXPLAIN: Should use idx_ae_category_created_at

-- For user activity queries:
-- SELECT * FROM analytics_events
--   WHERE user_id = ? AND created_at > NOW() - INTERVAL '7 days'
-- EXPLAIN: Should use idx_ae_user_id_created_at

-- For dashboard loads:
-- SELECT * FROM analytics_dashboards
--   WHERE owner_id = ? AND is_default = TRUE
-- EXPLAIN: Should use idx_ad_owner_is_default


-- ============================================================================
-- MAINTENANCE QUERIES FOR MONITORING
-- ============================================================================

-- Show all indexes and their size (DuckDB specific queries):
-- SELECT * FROM duckdb_indexes();
-- SELECT * FROM duckdb_index_size();

-- Analyze query performance:
-- PRAGMA QUERY_PROFILING;
-- EXPLAIN ANALYZE SELECT * FROM analytics_events...;

-- Vacuum and optimize:
-- VACUUM;
-- ANALYZE;
