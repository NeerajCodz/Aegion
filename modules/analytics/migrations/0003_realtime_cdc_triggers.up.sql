-- =============================================================================
-- Analytics Real-Time Sync Triggers (CDC)
-- Migration: 0003_realtime_cdc_triggers
-- =============================================================================

-- This migration adds PostgreSQL triggers to publish change events for real-time sync

-- Trigger function that captures analytics_events changes
CREATE OR REPLACE FUNCTION capture_analytics_events_changes()
RETURNS TRIGGER AS $$
BEGIN
    -- In a real implementation, this would publish to a message broker or event log
    -- For now, we log to a local events table that can be polled
    
    INSERT INTO core_event_bus_events (event_type, source_module, entity_type, entity_id, payload, metadata)
    VALUES (
        CASE WHEN TG_OP = 'INSERT' THEN 'analytics_event_created'
             WHEN TG_OP = 'UPDATE' THEN 'analytics_event_updated'
             WHEN TG_OP = 'DELETE' THEN 'analytics_event_deleted'
        END,
        'analytics',
        'analytics_event',
        COALESCE(NEW.id::text, OLD.id::text),
        CASE WHEN TG_OP = 'DELETE' THEN
            jsonb_build_object(
                'id', OLD.id,
                'category', OLD.category,
                'event_type', OLD.event_type,
                'user_id', OLD.user_id,
                'session_id', OLD.session_id,
                'data', OLD.data
            )
        ELSE
            jsonb_build_object(
                'id', NEW.id,
                'category', NEW.category,
                'event_type', NEW.event_type,
                'user_id', NEW.user_id,
                'session_id', NEW.session_id,
                'data', NEW.data
            )
        END,
        jsonb_build_object(
            'operation', TG_OP,
            'table', TG_TABLE_NAME,
            'timestamp', NOW()
        )
    );
    
    RETURN COALESCE(NEW, OLD);
END;
$$ LANGUAGE plpgsql;

-- Attach trigger to analytics_events
CREATE TRIGGER analytics_events_cdc_trigger
    AFTER INSERT OR UPDATE OR DELETE ON analytics_events
    FOR EACH ROW
    EXECUTE FUNCTION capture_analytics_events_changes();

-- Trigger function for analytics_metrics
CREATE OR REPLACE FUNCTION capture_analytics_metrics_changes()
RETURNS TRIGGER AS $$
BEGIN
    INSERT INTO core_event_bus_events (event_type, source_module, entity_type, entity_id, payload, metadata)
    VALUES (
        CASE WHEN TG_OP = 'INSERT' THEN 'analytics_metric_created'
             WHEN TG_OP = 'UPDATE' THEN 'analytics_metric_updated'
             WHEN TG_OP = 'DELETE' THEN 'analytics_metric_deleted'
        END,
        'analytics',
        'analytics_metric',
        COALESCE(NEW.id::text, OLD.id::text),
        CASE WHEN TG_OP = 'DELETE' THEN
            jsonb_build_object(
                'id', OLD.id,
                'name', OLD.name,
                'category', OLD.category,
                'value', OLD.value
            )
        ELSE
            jsonb_build_object(
                'id', NEW.id,
                'name', NEW.name,
                'category', NEW.category,
                'value', NEW.value
            )
        END,
        jsonb_build_object(
            'operation', TG_OP,
            'table', TG_TABLE_NAME,
            'timestamp', NOW()
        )
    );
    
    RETURN COALESCE(NEW, OLD);
END;
$$ LANGUAGE plpgsql;

-- Attach trigger to analytics_metrics
CREATE TRIGGER analytics_metrics_cdc_trigger
    AFTER INSERT OR UPDATE OR DELETE ON analytics_metrics
    FOR EACH ROW
    EXECUTE FUNCTION capture_analytics_metrics_changes();
