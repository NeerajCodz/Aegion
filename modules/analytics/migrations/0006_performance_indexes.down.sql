-- =============================================================================
-- Analytics Performance Indexes - Rollback
-- Migration: 0006_performance_indexes (down)
-- Purpose: Remove performance indexes
-- =============================================================================

-- Drop all performance indexes
DROP INDEX IF EXISTS idx_ae_event_type;
DROP INDEX IF EXISTS idx_ae_category;
DROP INDEX IF EXISTS idx_ae_source_system;
DROP INDEX IF EXISTS idx_ae_created_at_desc;
DROP INDEX IF EXISTS idx_ae_created_at_asc;
DROP INDEX IF EXISTS idx_ae_user_id;
DROP INDEX IF EXISTS idx_ae_session_id;
DROP INDEX IF EXISTS idx_ae_category_created_at;
DROP INDEX IF EXISTS idx_ae_event_type_created_at;
DROP INDEX IF EXISTS idx_ae_user_id_created_at;
DROP INDEX IF EXISTS idx_ae_source_system_created_at;
DROP INDEX IF EXISTS idx_ae_archived_at_null;
DROP INDEX IF EXISTS idx_ae_archived_at;
DROP INDEX IF EXISTS idx_ae_data_gin;

DROP INDEX IF EXISTS idx_am_name_category;
DROP INDEX IF EXISTS idx_am_created_at;
DROP INDEX IF EXISTS idx_am_category;
DROP INDEX IF EXISTS idx_am_category_created_at;

DROP INDEX IF EXISTS idx_ad_owner_id;
DROP INDEX IF EXISTS idx_ad_public;
DROP INDEX IF EXISTS idx_ad_is_default;
DROP INDEX IF EXISTS idx_ad_owner_pinned;
DROP INDEX IF EXISTS idx_ad_name;
DROP INDEX IF EXISTS idx_ad_updated_at;

DROP INDEX IF EXISTS idx_aq_owner_id;
DROP INDEX IF EXISTS idx_aq_name;
DROP INDEX IF EXISTS idx_aq_owner_created_at;

DROP INDEX IF EXISTS idx_aw_event_type_active;
DROP INDEX IF EXISTS idx_aw_active;
DROP INDEX IF EXISTS idx_aw_url;
