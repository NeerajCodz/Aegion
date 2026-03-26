package workers

import (
	"context"
	"encoding/json"
	"time"

	"github.com/aegion/aegion/core/eventbus"
	"github.com/aegion/aegion/internal/platform/logger"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// EventProcessorConfig configures the event processor worker.
type EventProcessorConfig struct {
	DB         *pgxpool.Pool
	Log        *logger.Logger
	EventBus   *eventbus.Bus
	Subscriber string        // Subscriber name (required if EventBus is nil)
	Interval   time.Duration // How often to check for events (default: 10 seconds)
	BatchSize  int           // Number of events to process per batch (default: 100)
	MaxRetries int           // Maximum retry attempts (default: 3)
	RetryDelay time.Duration // Base delay for retries (default: 1 second)
}

// EventProcessorWorker processes pending event deliveries.
type EventProcessorWorker struct {
	*BaseWorker
	eventBus   *eventbus.Bus
	subscriber string
	batchSize  int
	maxRetries int
	retryDelay time.Duration
}

// NewEventProcessorWorker creates a new event processor worker.
func NewEventProcessorWorker(cfg EventProcessorConfig) *EventProcessorWorker {
	if cfg.Interval == 0 {
		cfg.Interval = 10 * time.Second
	}
	if cfg.BatchSize == 0 {
		cfg.BatchSize = 100
	}
	if cfg.MaxRetries == 0 {
		cfg.MaxRetries = 3
	}
	if cfg.RetryDelay == 0 {
		cfg.RetryDelay = time.Second
	}
	if cfg.Subscriber == "" {
		cfg.Subscriber = "default"
	}

	return &EventProcessorWorker{
		BaseWorker: NewBaseWorker("event_processor", cfg.DB, cfg.Log, cfg.Interval),
		eventBus:   cfg.EventBus,
		subscriber: cfg.Subscriber,
		batchSize:  cfg.BatchSize,
		maxRetries: cfg.MaxRetries,
		retryDelay: cfg.RetryDelay,
	}
}

// Start begins the event processor worker.
func (w *EventProcessorWorker) Start(ctx context.Context) error {
	return w.RunLoop(ctx, w.process)
}

// process handles pending event deliveries.
func (w *EventProcessorWorker) process(ctx context.Context) error {
	w.Log().Debug().Msg("processing pending events")

	// Use event bus's ProcessPending if available
	if w.eventBus != nil {
		err := w.eventBus.ProcessPending(ctx, w.subscriber)
		if err != nil {
			return err
		}
		return nil
	}

	// Fallback: direct database processing
	return w.processDirectly(ctx)
}

// processDirectly processes events without an EventBus instance.
func (w *EventProcessorWorker) processDirectly(ctx context.Context) error {
	// Get pending deliveries using FOR UPDATE SKIP LOCKED
	rows, err := w.DB().Query(ctx, `
		SELECT d.id, d.event_id, d.attempt_count,
		       e.event_type, e.source_module, e.entity_type, e.entity_id,
		       e.identity_id, e.payload, e.metadata, e.occurred_at
		FROM core_event_bus_deliveries d
		JOIN core_event_bus_events e ON d.event_id = e.id
		WHERE d.subscriber = $1
		  AND d.status IN ('pending', 'failed')
		  AND d.next_retry_at <= NOW()
		ORDER BY e.occurred_at
		LIMIT $2
		FOR UPDATE OF d SKIP LOCKED
	`, w.subscriber, w.batchSize)
	if err != nil {
		return err
	}
	defer rows.Close()

	var processed int
	for rows.Next() {
		var (
			deliveryID   uuid.UUID
			eventID      uuid.UUID
			attemptCount int
			eventType    string
			sourceModule string
			entityType   *string
			entityID     *string
			identityID   *uuid.UUID
			payloadJSON  []byte
			metadataJSON []byte
			occurredAt   time.Time
		)

		err := rows.Scan(
			&deliveryID, &eventID, &attemptCount,
			&eventType, &sourceModule, &entityType, &entityID,
			&identityID, &payloadJSON, &metadataJSON, &occurredAt,
		)
		if err != nil {
			w.Log().Error().Err(err).Msg("failed to scan delivery row")
			continue
		}

		// Parse payload and metadata
		var payload, metadata map[string]interface{}
		_ = json.Unmarshal(payloadJSON, &payload)
		_ = json.Unmarshal(metadataJSON, &metadata)

		// Log the event being processed
		w.Log().Debug().
			Str("delivery_id", deliveryID.String()).
			Str("event_id", eventID.String()).
			Str("event_type", eventType).
			Int("attempt", attemptCount+1).
			Msg("processing event delivery")

		// Since we don't have a handler, mark as delivered
		// In production, this would call the actual handler
		w.markDelivered(ctx, deliveryID)

		processed++
	}

	if processed > 0 {
		w.Log().Info().Int("processed", processed).Msg("events processed")
	}

	return rows.Err()
}

// markDelivered marks a delivery as successfully delivered.
func (w *EventProcessorWorker) markDelivered(ctx context.Context, deliveryID uuid.UUID) {
	_, err := w.DB().Exec(ctx, `
		UPDATE core_event_bus_deliveries
		SET status = 'delivered', delivered_at = NOW(), updated_at = NOW()
		WHERE id = $1
	`, deliveryID)
	if err != nil {
		w.Log().Error().Err(err).Str("delivery_id", deliveryID.String()).Msg("failed to mark delivery as delivered")
	}
}

// markFailed marks a delivery as failed and schedules retry.
func (w *EventProcessorWorker) markFailed(ctx context.Context, deliveryID uuid.UUID, attemptCount int, err error) {
	attemptCount++

	if attemptCount >= w.maxRetries {
		// Dead letter
		_, dbErr := w.DB().Exec(ctx, `
			UPDATE core_event_bus_deliveries
			SET status = 'dead_lettered',
			    attempt_count = $2,
			    last_error = $3,
			    updated_at = NOW()
			WHERE id = $1
		`, deliveryID, attemptCount, err.Error())
		if dbErr != nil {
			w.Log().Error().Err(dbErr).Str("delivery_id", deliveryID.String()).Msg("failed to dead-letter delivery")
		}
	} else {
		// Schedule retry with exponential backoff
		retryDelay := w.retryDelay * time.Duration(1<<uint(attemptCount))
		nextRetry := time.Now().Add(retryDelay)

		_, dbErr := w.DB().Exec(ctx, `
			UPDATE core_event_bus_deliveries
			SET status = 'failed',
			    attempt_count = $2,
			    last_error = $3,
			    next_retry_at = $4,
			    updated_at = NOW()
			WHERE id = $1
		`, deliveryID, attemptCount, err.Error(), nextRetry)
		if dbErr != nil {
			w.Log().Error().Err(dbErr).Str("delivery_id", deliveryID.String()).Msg("failed to mark delivery as failed")
		}

		w.Log().Debug().
			Str("delivery_id", deliveryID.String()).
			Int("attempt", attemptCount).
			Time("next_retry", nextRetry).
			Msg("delivery scheduled for retry")
	}
}

// CleanupOldEvents removes old delivered events.
func (w *EventProcessorWorker) CleanupOldEvents(ctx context.Context, olderThan time.Duration) (int64, error) {
	cutoff := time.Now().Add(-olderThan)

	// Delete events where all deliveries are complete (delivered or dead-lettered)
	result, err := w.DB().Exec(ctx, `
		DELETE FROM core_event_bus_events e
		WHERE e.occurred_at < $1
		  AND NOT EXISTS (
			SELECT 1 FROM core_event_bus_deliveries d
			WHERE d.event_id = e.id
			  AND d.status IN ('pending', 'failed')
		  )
	`, cutoff)
	if err != nil {
		return 0, err
	}

	deleted := result.RowsAffected()
	if deleted > 0 {
		w.Log().Info().Int64("deleted", deleted).Dur("older_than", olderThan).Msg("old events cleaned up")
	}

	return deleted, nil
}

// GetPendingCount returns the number of pending deliveries for this subscriber.
func (w *EventProcessorWorker) GetPendingCount(ctx context.Context) (int64, error) {
	var count int64
	err := w.DB().QueryRow(ctx, `
		SELECT COUNT(*) FROM core_event_bus_deliveries
		WHERE subscriber = $1 AND status IN ('pending', 'failed')
	`, w.subscriber).Scan(&count)
	return count, err
}

// GetDeadLetteredCount returns the number of dead-lettered deliveries for this subscriber.
func (w *EventProcessorWorker) GetDeadLetteredCount(ctx context.Context) (int64, error) {
	var count int64
	err := w.DB().QueryRow(ctx, `
		SELECT COUNT(*) FROM core_event_bus_deliveries
		WHERE subscriber = $1 AND status = 'dead_lettered'
	`, w.subscriber).Scan(&count)
	return count, err
}
