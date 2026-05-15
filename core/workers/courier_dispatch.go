package workers

import (
	"context"
	"time"

	"github.com/aegion/aegion/core/courier"
	"github.com/aegion/aegion/internal/xlog"
	"github.com/jackc/pgx/v5/pgxpool"
)

// CourierDispatchConfig configures the courier dispatch worker.
type CourierDispatchConfig struct {
	DB         *pgxpool.Pool
	Log        *xlog.Logger
	Courier    *courier.Courier
	Interval   time.Duration // How often to check for messages (default: 30 seconds)
	BatchSize  int           // Number of messages to process per batch (default: 10)
	MaxRetries int           // Maximum retry attempts (default: 3)
}

// CourierDispatchWorker processes queued courier messages.
type CourierDispatchWorker struct {
	*BaseWorker
	courier    *courier.Courier
	batchSize  int
	maxRetries int
}

// NewCourierDispatchWorker creates a new courier dispatch worker.
func NewCourierDispatchWorker(cfg CourierDispatchConfig) *CourierDispatchWorker {
	if cfg.Interval == 0 {
		cfg.Interval = 30 * time.Second
	}
	if cfg.BatchSize == 0 {
		cfg.BatchSize = 10
	}
	if cfg.MaxRetries == 0 {
		cfg.MaxRetries = 3
	}

	return &CourierDispatchWorker{
		BaseWorker: NewBaseWorker("courier_dispatch", cfg.DB, cfg.Log, cfg.Interval),
		courier:    cfg.Courier,
		batchSize:  cfg.BatchSize,
		maxRetries: cfg.MaxRetries,
	}
}

// Start begins the courier dispatch worker.
func (w *CourierDispatchWorker) Start(ctx context.Context) error {
	return w.RunLoop(ctx, w.dispatch)
}

// dispatch processes queued messages.
func (w *CourierDispatchWorker) dispatch(ctx context.Context) error {
	w.Log().Debug("processing courier queue")

	// Use courier's ProcessQueue if available
	if w.courier != nil {
		processed, err := w.courier.ProcessQueue(ctx, w.batchSize)
		if err != nil {
			return err
		}
		if processed > 0 {
			w.Log().Info("messages dispatched", "processed", processed)
		}
		return nil
	}

	// Fallback: direct database processing if courier not provided
	return w.processDirectly(ctx)
}

// processDirectly processes messages without a Courier instance.
func (w *CourierDispatchWorker) processDirectly(ctx context.Context) error {
	// Get pending messages using FOR UPDATE SKIP LOCKED
	rows, err := w.query(ctx, `
		UPDATE core_courier_messages
		SET status = 'processing', updated_at = NOW()
		WHERE id IN (
			SELECT id FROM core_courier_messages
			WHERE status = 'queued'
			  AND (send_after IS NULL OR send_after <= NOW())
			ORDER BY created_at
			LIMIT $1
			FOR UPDATE SKIP LOCKED
		)
		RETURNING id, type, recipient, subject, body, send_count
	`, w.batchSize)
	if err != nil {
		return err
	}
	defer rows.Close()

	var processed int
	for rows.Next() {
		var id, msgType, recipient, subject, body string
		var sendCount int

		if err := rows.Scan(&id, &msgType, &recipient, &subject, &body, &sendCount); err != nil {
			w.Log().Error("failed to scan message row", "error", err)
			continue
		}

		// Log that we found a message (actual sending would be done by courier)
		w.Log().Info("processing message",
			"id", id,
			"type", msgType,
			"recipient", recipient,
		)

		// Mark as failed since we can't actually send without courier config
		sendCount++
		if sendCount >= w.maxRetries {
			_, _ = w.exec(ctx, `
				UPDATE core_courier_messages
				SET status = 'abandoned', send_count = $2, 
				    last_error = 'no courier configured', updated_at = NOW()
				WHERE id = $1
			`, id, sendCount)
		} else {
			_, _ = w.exec(ctx, `
				UPDATE core_courier_messages
				SET status = 'queued', send_count = $2,
				    last_error = 'retrying', updated_at = NOW()
				WHERE id = $1
			`, id, sendCount)
		}

		processed++
	}

	if processed > 0 {
		w.Log().Info("messages processed", "processed", processed)
	}

	return rows.Err()
}

// CleanupOldMessages removes old sent/abandoned messages.
func (w *CourierDispatchWorker) CleanupOldMessages(ctx context.Context, olderThan time.Duration) (int64, error) {
	cutoff := time.Now().Add(-olderThan)

	result, err := w.exec(ctx, `
		DELETE FROM core_courier_messages
		WHERE status IN ('sent', 'abandoned', 'cancelled')
		  AND updated_at < $1
	`, cutoff)
	if err != nil {
		return 0, err
	}

	deleted := result.RowsAffected()
	if deleted > 0 {
		w.Log().Info("old messages cleaned up", "deleted", deleted, "older_than", olderThan)
	}

	return deleted, nil
}
