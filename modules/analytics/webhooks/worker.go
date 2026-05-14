package webhooks

import (
	"context"
	"time"

	"github.com/aegion/aegion/internal/xlog"
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
	logger     *xlog.Logger
	stopChan   chan struct{}
}

// NewDeliveryWorker creates a new delivery worker.
func NewDeliveryWorker(id int, queue *Queue, store *Store, dispatcher *Dispatcher, retry *RetryPolicy, signer *Signature, logger *xlog.Logger) *DeliveryWorker {
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
			job := w.queue.DequeueTimeout(1 * time.Second)
			if job == nil {
				continue
			}

			w.processJob(ctx, job)
		}
	}
}

// processJob processes a single delivery job using wide events pattern.
func (w *DeliveryWorker) processJob(ctx context.Context, job *DeliveryJob) {
	startTime := time.Now()

	webhook, err := w.store.GetWebhook(ctx, job.WebhookID)
	if err != nil {
		w.logger.Error("failed to get webhook", "webhook_id", job.WebhookID, "error", err)
		w.queue.Remove(job.ID)
		w.logWideEvent(ctx, "webhook_job_processing", startTime, map[string]any{
			"job_id":     job.ID,
			"webhook_id": job.WebhookID,
			"event_id":   job.EventID,
			"outcome":    "error",
			"error":      "failed to get webhook: " + err.Error(),
			"attempt":    job.Attempts,
		})
		return
	}

	if !webhook.Active || w.retry.ShouldCircuitBreak(webhook.FailureCount) {
		w.logger.Warn("webhook is disabled or circuit broken", "webhook_id", job.WebhookID)
		w.queue.Remove(job.ID)

		dlqEvent := &analytics.DLQWebhookEvent{
			ID:          job.ID,
			WebhookID:   job.WebhookID,
			EventID:     job.EventID,
			ErrorMsg:    "webhook disabled or circuit breaker tripped",
			RetryCount:  job.Attempts,
			LastErrorAt: time.Now(),
			CreatedAt:   time.Now(),
			UpdatedAt:   time.Now(),
		}
		if saveErr := w.store.SaveDLQEvent(ctx, dlqEvent); saveErr != nil {
			w.logger.Error("failed to save DLQ event", "error", saveErr)
		}

		w.logWideEvent(ctx, "webhook_job_processing", startTime, map[string]any{
			"job_id":     job.ID,
			"webhook_id": job.WebhookID,
			"event_id":   job.EventID,
			"outcome":    "skipped",
			"reason":     "webhook disabled or circuit broken",
			"attempt":    job.Attempts,
		})
		return
	}

	result := w.dispatcher.Deliver(ctx, webhook.URL, job.Payload, job.Headers)

	now := time.Now()
	delivery := &analytics.WebhookDelivery{
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

		if err := w.store.ResetFailureCount(ctx, job.WebhookID); err != nil {
			w.logger.Error("failed to reset failure count", "webhook_id", job.WebhookID, "error", err)
		}

		if err := w.store.SaveDelivery(ctx, delivery); err != nil {
			w.logger.Error("failed to save delivery", "error", err)
		}

		w.queue.Remove(job.ID)

		w.logWideEvent(ctx, "webhook_job_processing", startTime, map[string]any{
			"job_id":      job.ID,
			"webhook_id":  job.WebhookID,
			"event_id":    job.EventID,
			"outcome":     "success",
			"status_code": result.StatusCode,
			"attempt":     job.Attempts,
			"url":         webhook.URL,
		})
		return
	}

	w.logger.Warn("webhook delivery failed", "webhook_id", job.WebhookID, "status_code", result.StatusCode, "error", result.Error)

	if w.retry.ShouldRetry(job.Attempts, result.StatusCode, nil) && job.Attempts < job.MaxRetries {
		nextRetry := w.retry.NextRetryTime(now, job.Attempts)
		delivery.Status = "retrying"
		delivery.NextRetryAt = &nextRetry

		if err := w.store.SaveDelivery(ctx, delivery); err != nil {
			w.logger.Error("failed to save delivery", "error", err)
		}

		job.Attempts++
		job.NextRetryAt = &nextRetry

		if err := w.queue.Enqueue(job); err != nil {
			w.logger.Error("failed to enqueue job", "error", err)
		}

		w.logWideEvent(ctx, "webhook_job_processing", startTime, map[string]any{
			"job_id":        job.ID,
			"webhook_id":    job.WebhookID,
			"event_id":      job.EventID,
			"outcome":      "retry",
			"status_code":   result.StatusCode,
			"attempt":      job.Attempts,
			"max_retries":  job.MaxRetries,
			"next_retry_at": nextRetry,
			"url":          webhook.URL,
		})
	} else {
		delivery.Status = "failed"
		delivery.CompletedAt = &now

		if err := w.store.SaveDelivery(ctx, delivery); err != nil {
			w.logger.Error("failed to save delivery", "error", err)
		}

		if err := w.store.IncrementFailureCount(ctx, job.WebhookID); err != nil {
			w.logger.Error("failed to increment failure count", "webhook_id", job.WebhookID, "error", err)
		}

		circuitBroken := w.retry.ShouldCircuitBreak(webhook.FailureCount + 1)
		if circuitBroken {
			w.logger.Error("webhook circuit breaker triggered", "webhook_id", job.WebhookID)

			dlqEvent := &analytics.DLQWebhookEvent{
				ID:          job.ID,
				WebhookID:   job.WebhookID,
				EventID:     job.EventID,
				ErrorMsg:    "circuit breaker triggered",
				RetryCount:  job.Attempts,
				LastErrorAt: now,
				CreatedAt:   now,
				UpdatedAt:   now,
			}
			if err := w.store.SaveDLQEvent(ctx, dlqEvent); err != nil {
				w.logger.Error("failed to save DLQ event", "error", err)
			}
		}

		w.queue.Remove(job.ID)

		w.logWideEvent(ctx, "webhook_job_processing", startTime, map[string]any{
			"job_id":         job.ID,
			"webhook_id":     job.WebhookID,
			"event_id":       job.EventID,
			"outcome":        "failed",
			"status_code":    result.StatusCode,
			"attempt":        job.Attempts,
			"max_retries":    job.MaxRetries,
			"circuit_broken": circuitBroken,
			"error":          result.Error,
			"url":            webhook.URL,
			"failure_count":  webhook.FailureCount + 1,
		})
	}
}

func (w *DeliveryWorker) logWideEvent(ctx context.Context, msg string, startTime time.Time, attrs map[string]any) {
	durationMs := time.Since(startTime).Milliseconds()

	args := make([]any, 0, len(attrs)*2+2)
	args = append(args, "latency_ms", durationMs)
	for k, v := range attrs {
		args = append(args, k, v)
	}

	w.logger.InfoContext(ctx, msg, args...)
}

// Stop stops the delivery worker.
func (w *DeliveryWorker) Stop() {
	close(w.stopChan)
}
