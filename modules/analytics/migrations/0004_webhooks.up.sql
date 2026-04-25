-- =============================================================================
-- Webhook Schema
-- Migration: 0004_webhooks
-- =============================================================================

-- Webhooks Table
CREATE TABLE IF NOT EXISTS webhooks (
    id VARCHAR(255) PRIMARY KEY,
    user_id VARCHAR(255) NOT NULL,
    url VARCHAR(2048) NOT NULL,
    event_types JSON NOT NULL,
    categories JSON,
    custom_filter JSON,
    secret VARCHAR(255) NOT NULL,
    active BOOLEAN DEFAULT TRUE,
    failure_count INTEGER DEFAULT 0,
    created_at TIMESTAMP NOT NULL,
    updated_at TIMESTAMP NOT NULL
);

-- Index for user_id
CREATE INDEX IF NOT EXISTS idx_webhooks_user_id ON webhooks(user_id);

-- Index for active webhooks
CREATE INDEX IF NOT EXISTS idx_webhooks_active ON webhooks(active) WHERE active = TRUE;

-- Webhook Deliveries Table
CREATE TABLE IF NOT EXISTS webhook_deliveries (
    id VARCHAR(255) PRIMARY KEY,
    webhook_id VARCHAR(255) NOT NULL,
    event_id VARCHAR(255) NOT NULL,
    status VARCHAR(50) NOT NULL,
    status_code INTEGER,
    response_body TEXT,
    error TEXT,
    attempts INTEGER DEFAULT 1,
    max_retries INTEGER DEFAULT 5,
    next_retry_at TIMESTAMP,
    last_attempt_at TIMESTAMP NOT NULL,
    completed_at TIMESTAMP,
    created_at TIMESTAMP NOT NULL,
    updated_at TIMESTAMP NOT NULL,
    FOREIGN KEY (webhook_id) REFERENCES webhooks(id)
);

-- Index for webhook_id
CREATE INDEX IF NOT EXISTS idx_webhook_deliveries_webhook_id ON webhook_deliveries(webhook_id);

-- Index for event_id
CREATE INDEX IF NOT EXISTS idx_webhook_deliveries_event_id ON webhook_deliveries(event_id);

-- Index for status
CREATE INDEX IF NOT EXISTS idx_webhook_deliveries_status ON webhook_deliveries(status);

-- Index for next_retry_at (for retry processing)
CREATE INDEX IF NOT EXISTS idx_webhook_deliveries_next_retry ON webhook_deliveries(next_retry_at) 
    WHERE status = 'retrying' AND next_retry_at IS NOT NULL;

-- Webhook Dead Letter Queue
CREATE TABLE IF NOT EXISTS webhook_dlq (
    id VARCHAR(255) PRIMARY KEY,
    webhook_id VARCHAR(255) NOT NULL,
    event_id VARCHAR(255) NOT NULL,
    event_data JSON NOT NULL,
    error_msg TEXT NOT NULL,
    retry_count INTEGER DEFAULT 0,
    last_error_at TIMESTAMP NOT NULL,
    created_at TIMESTAMP NOT NULL,
    updated_at TIMESTAMP NOT NULL,
    FOREIGN KEY (webhook_id) REFERENCES webhooks(id)
);

-- Index for webhook_id
CREATE INDEX IF NOT EXISTS idx_webhook_dlq_webhook_id ON webhook_dlq(webhook_id);

-- Index for created_at (for cleanup)
CREATE INDEX IF NOT EXISTS idx_webhook_dlq_created_at ON webhook_dlq(created_at);
