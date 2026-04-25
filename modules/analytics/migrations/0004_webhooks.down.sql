-- =============================================================================
-- Webhook Schema Rollback
-- Migration: 0004_webhooks
-- =============================================================================

DROP TABLE IF EXISTS webhook_dlq;
DROP TABLE IF EXISTS webhook_deliveries;
DROP TABLE IF EXISTS webhooks;
