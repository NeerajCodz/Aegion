package webhooks

import (
	"context"
	"time"

	analytics "github.com/aegion/aegion/modules/analytics"
)

// DeliveryWorker processes webhook delivery jobs.
type DeliveryWorker struct {
	id         int
	queue      *Queue
	store      *Store
	dispatcher *Dispatcher
	retry      *RetryPolicy
	signer     *Signature
	logger     Logger
	stopChan   chan struct{}
}

// NewDeliveryWorker creates a new delivery worker.
func NewDeliveryWorker(id int, queue *Queue, store *Store, dispatcher *Dispatcher, retry *RetryPolicy, signer *Signature, logger Logger) *DeliveryWorker {
	return &DeliveryWorker{
		id:         id,
		queue:      queue,
		store:      store,
		dispatcher: dispatcher,
		retry:      retry,
		signer:     signer,
		logger:     logger,
		stopChan:   make(chan struct{}),
	}
}

// Start begins processing delivery jobs.
func (w *DeliveryWorker) Start(ctx context.Context) {
	w.logger.Debug("starting delivery worker", "worker_id", w.id)

	for {
		select {
		case <-w.stopChan:
			w.logger.Debug("delivery worker stopping", "worker_id", w.id)
			return
		case <-ctx.Done():
			return
		default:
			// Try to dequeue with timeout
			job := w.queue.DequeueTimeout(1 * time.Second)
			if job == nil {
				continue
			}

			w.processJob(ctx, job)
		}
	}
}

// processJob processes a single delivery job.
func (w *DeliveryWorker) processJob(ctx context.Context, job *DeliveryJob) {
	w.logger.Debug("processing delivery job", "job_id", job.ID, "webhook_id", job.WebhookID)

	// Get webhook details
	webhook, err := w.store.GetWebhook(ctx, job.WebhookID)
	if err != nil {
		w.logger.Error("failed to get webhook", "webhook_id", job.WebhookID, "error", err)
		w.queue.Remove(job.ID)
		return
	}

	// Check if webhook is disabled
	if !webhook.Active || w.retry.ShouldCircuitBreak(webhook.FailureCount) {
		w.logger.Warn("webhook is disabled or circuit broken", "webhook_id", job.WebhookID)
		w.queue.Remove(job.ID)

		// Move to DLQ
		dlqEvent := &analytics.DLQWebhookEvent{
			ID:         job.ID,
			WebhookID:  job.WebhookID,
			EventID:    job.EventID,
			ErrorMsg:   "webhook disabled or circuit breaker tripped",
			RetryCount: job.Attempts,
			LastErrorAt: time.Now(),
			CreatedAt:  time.Now(),
			UpdatedAt:  time.Now(),
		}
		w.store.SaveDLQEvent(ctx, dlqEvent)
		return
	}

	// Get or create delivery record
	var delivery *analytics.WebhookDelivery

	// Try to deliver
	result := w.dispatcher.Deliver(ctx, webhook.URL, job.Payload, job.Headers)

	// Create or update delivery record
	now := time.Now()
	delivery = &analytics.WebhookDelivery{
		ID:            job.ID,
		WebhookID:     job.WebhookID,
		EventID:       job.EventID,
		Status:        "pending",
		StatusCode:    result.StatusCode,
		ResponseBody:  result.Body,
		Error:         result.Error,
		Attempts:      job.Attempts,
		MaxRetries:    job.MaxRetries,
		LastAttemptAt: now,
		CreatedAt:     now,
		UpdatedAt:     now,
	}

	if result.Success {
		delivery.Status = "success"
		delivery.CompletedAt = &now
		w.store.ResetFailureCount(ctx, job.WebhookID)
		w.logger.Info("webhook delivered successfully", "webhook_id", job.WebhookID, "event_id", job.EventID)
		w.store.SaveDelivery(ctx, delivery)
		w.queue.Remove(job.ID)
		return
	}

	// Handle failure
	w.logger.Warn("webhook delivery failed", "webhook_id", job.WebhookID, "status_code", result.StatusCode, "error", result.Error)

	// Check if we should retry
	if w.retry.ShouldRetry(job.Attempts, result.StatusCode, nil) && job.Attempts < job.MaxRetries {
		nextRetry := w.retry.NextRetryTime(now, job.Attempts)
		delivery.Status = "retrying"
		delivery.NextRetryAt = &nextRetry
		w.store.SaveDelivery(ctx, delivery)

		// Re-queue for retry
		job.Attempts++
		job.NextRetryAt = &nextRetry
		w.queue.Enqueue(job)
		w.logger.Debug("webhook requeued for retry", "webhook_id", job.WebhookID, "attempt", job.Attempts, "next_retry", nextRetry)
	} else {
		// Final failure
		delivery.Status = "failed"
		delivery.CompletedAt = &now
		w.store.SaveDelivery(ctx, delivery)

		// Increment failure count
		w.store.IncrementFailureCount(ctx, job.WebhookID)

		// Check if we should circuit break
		if w.retry.ShouldCircuitBreak(webhook.FailureCount + 1) {
			w.logger.Error("webhook circuit breaker triggered", "webhook_id", job.WebhookID)

			// Move to DLQ
			dlqEvent := &analytics.DLQWebhookEvent{
				ID:         job.ID,
				WebhookID:  job.WebhookID,
				EventID:    job.EventID,
				ErrorMsg:   "circuit breaker triggered",
				RetryCount: job.Attempts,
				LastErrorAt: now,
				CreatedAt:  now,
				UpdatedAt:  now,
			}
			w.store.SaveDLQEvent(ctx, dlqEvent)
		}

		w.queue.Remove(job.ID)
	}
}

// Stop stops the delivery worker.
func (w *DeliveryWorker) Stop() {
	close(w.stopChan)
}
