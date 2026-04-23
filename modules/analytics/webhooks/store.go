package webhooks

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

// Store manages webhook persistence.
type Store struct {
	db DB
}

// DB interface for database operations.
type DB interface {
	QueryRowContext(ctx context.Context, query string, args ...interface{}) *sql.Row
	QueryContext(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error)
	ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error)
}

// NewStore creates a new webhook store.
func NewStore(db DB) *Store {
	return &Store{db: db}
}

// CreateWebhook stores a new webhook.
func (s *Store) CreateWebhook(ctx context.Context, webhook *Webhook) error {
	eventTypesJSON, _ := json.Marshal(webhook.EventTypes)
	categoriesJSON, _ := json.Marshal(webhook.Categories)
	customFilterJSON, _ := json.Marshal(webhook.CustomFilter)

	query := `
		INSERT INTO webhooks (id, user_id, url, event_types, categories, custom_filter, secret, active, failure_count, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`

	_, err := s.db.ExecContext(ctx, query,
		webhook.ID, webhook.UserID, webhook.URL, string(eventTypesJSON), string(categoriesJSON), string(customFilterJSON),
		webhook.Secret, webhook.Active, webhook.FailureCount, webhook.CreatedAt, webhook.UpdatedAt,
	)

	return err
}

// GetWebhook retrieves a webhook by ID.
func (s *Store) GetWebhook(ctx context.Context, webhookID string) (*Webhook, error) {
	query := `
		SELECT id, user_id, url, event_types, categories, custom_filter, secret, active, failure_count, created_at, updated_at
		FROM webhooks WHERE id = ?
	`

	row := s.db.QueryRowContext(ctx, query, webhookID)
	webhook := &Webhook{}

	var eventTypesJSON, categoriesJSON, customFilterJSON string

	err := row.Scan(&webhook.ID, &webhook.UserID, &webhook.URL, &eventTypesJSON, &categoriesJSON, &customFilterJSON,
		&webhook.Secret, &webhook.Active, &webhook.FailureCount, &webhook.CreatedAt, &webhook.UpdatedAt)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrWebhookNotFound
		}
		return nil, err
	}

	json.Unmarshal([]byte(eventTypesJSON), &webhook.EventTypes)
	json.Unmarshal([]byte(categoriesJSON), &webhook.Categories)
	json.Unmarshal([]byte(customFilterJSON), &webhook.CustomFilter)

	return webhook, nil
}

// ListWebhooks retrieves all webhooks for a user.
func (s *Store) ListWebhooks(ctx context.Context, userID string) ([]*Webhook, error) {
	query := `
		SELECT id, user_id, url, event_types, categories, custom_filter, secret, active, failure_count, created_at, updated_at
		FROM webhooks WHERE user_id = ? ORDER BY created_at DESC
	`

	rows, err := s.db.QueryContext(ctx, query, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var webhooks []*Webhook
	for rows.Next() {
		webhook := &Webhook{}
		var eventTypesJSON, categoriesJSON, customFilterJSON string

		err := rows.Scan(&webhook.ID, &webhook.UserID, &webhook.URL, &eventTypesJSON, &categoriesJSON, &customFilterJSON,
			&webhook.Secret, &webhook.Active, &webhook.FailureCount, &webhook.CreatedAt, &webhook.UpdatedAt)

		if err != nil {
			return nil, err
		}

		json.Unmarshal([]byte(eventTypesJSON), &webhook.EventTypes)
		json.Unmarshal([]byte(categoriesJSON), &webhook.Categories)
		json.Unmarshal([]byte(customFilterJSON), &webhook.CustomFilter)

		webhooks = append(webhooks, webhook)
	}

	return webhooks, rows.Err()
}

// UpdateWebhook updates an existing webhook.
func (s *Store) UpdateWebhook(ctx context.Context, webhook *Webhook) error {
	eventTypesJSON, _ := json.Marshal(webhook.EventTypes)
	categoriesJSON, _ := json.Marshal(webhook.Categories)
	customFilterJSON, _ := json.Marshal(webhook.CustomFilter)

	query := `
		UPDATE webhooks 
		SET url = ?, event_types = ?, categories = ?, custom_filter = ?, active = ?, failure_count = ?, updated_at = ?
		WHERE id = ? AND user_id = ?
	`

	result, err := s.db.ExecContext(ctx, query,
		webhook.URL, string(eventTypesJSON), string(categoriesJSON), string(customFilterJSON),
		webhook.Active, webhook.FailureCount, time.Now(), webhook.ID, webhook.UserID,
	)

	if err != nil {
		return err
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}

	if rows == 0 {
		return ErrWebhookNotFound
	}

	return nil
}

// DeleteWebhook deletes a webhook (soft delete by setting active=false).
func (s *Store) DeleteWebhook(ctx context.Context, webhookID, userID string) error {
	query := `UPDATE webhooks SET active = false, updated_at = ? WHERE id = ? AND user_id = ?`

	result, err := s.db.ExecContext(ctx, query, time.Now(), webhookID, userID)
	if err != nil {
		return err
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}

	if rows == 0 {
		return ErrWebhookNotFound
	}

	return nil
}

// IncrementFailureCount increments the failure count for a webhook.
func (s *Store) IncrementFailureCount(ctx context.Context, webhookID string) error {
	query := `UPDATE webhooks SET failure_count = failure_count + 1, updated_at = ? WHERE id = ?`
	_, err := s.db.ExecContext(ctx, query, time.Now(), webhookID)
	return err
}

// ResetFailureCount resets the failure count for a webhook.
func (s *Store) ResetFailureCount(ctx context.Context, webhookID string) error {
	query := `UPDATE webhooks SET failure_count = 0, updated_at = ? WHERE id = ?`
	_, err := s.db.ExecContext(ctx, query, time.Now(), webhookID)
	return err
}

// SaveDelivery stores a webhook delivery record.
func (s *Store) SaveDelivery(ctx context.Context, delivery *WebhookDelivery) error {
	query := `
		INSERT INTO webhook_deliveries (id, webhook_id, event_id, status, status_code, response_body, error, attempts, max_retries, next_retry_at, last_attempt_at, completed_at, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`

	_, err := s.db.ExecContext(ctx, query,
		delivery.ID, delivery.WebhookID, delivery.EventID, delivery.Status, delivery.StatusCode,
		delivery.ResponseBody, delivery.Error, delivery.Attempts, delivery.MaxRetries,
		delivery.NextRetryAt, delivery.LastAttemptAt, delivery.CompletedAt, delivery.CreatedAt, delivery.UpdatedAt,
	)

	return err
}

// UpdateDelivery updates a delivery record.
func (s *Store) UpdateDelivery(ctx context.Context, delivery *WebhookDelivery) error {
	query := `
		UPDATE webhook_deliveries
		SET status = ?, status_code = ?, response_body = ?, error = ?, attempts = ?, next_retry_at = ?, last_attempt_at = ?, completed_at = ?, updated_at = ?
		WHERE id = ?
	`

	_, err := s.db.ExecContext(ctx, query,
		delivery.Status, delivery.StatusCode, delivery.ResponseBody, delivery.Error, delivery.Attempts,
		delivery.NextRetryAt, delivery.LastAttemptAt, delivery.CompletedAt, time.Now(), delivery.ID,
	)

	return err
}

// GetDelivery retrieves a delivery record.
func (s *Store) GetDelivery(ctx context.Context, deliveryID string) (*WebhookDelivery, error) {
	query := `
		SELECT id, webhook_id, event_id, status, status_code, response_body, error, attempts, max_retries, next_retry_at, last_attempt_at, completed_at, created_at, updated_at
		FROM webhook_deliveries WHERE id = ?
	`

	row := s.db.QueryRowContext(ctx, query, deliveryID)
	delivery := &WebhookDelivery{}

	err := row.Scan(&delivery.ID, &delivery.WebhookID, &delivery.EventID, &delivery.Status, &delivery.StatusCode,
		&delivery.ResponseBody, &delivery.Error, &delivery.Attempts, &delivery.MaxRetries,
		&delivery.NextRetryAt, &delivery.LastAttemptAt, &delivery.CompletedAt, &delivery.CreatedAt, &delivery.UpdatedAt)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrDeliveryNotFound
		}
		return nil, err
	}

	return delivery, nil
}

// ListDeliveries retrieves delivery history for a webhook.
func (s *Store) ListDeliveries(ctx context.Context, webhookID string, limit int) ([]*WebhookDelivery, error) {
	if limit <= 0 {
		limit = 100
	}

	query := `
		SELECT id, webhook_id, event_id, status, status_code, response_body, error, attempts, max_retries, next_retry_at, last_attempt_at, completed_at, created_at, updated_at
		FROM webhook_deliveries WHERE webhook_id = ? ORDER BY created_at DESC LIMIT ?
	`

	rows, err := s.db.QueryContext(ctx, query, webhookID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var deliveries []*WebhookDelivery
	for rows.Next() {
		delivery := &WebhookDelivery{}
		err := rows.Scan(&delivery.ID, &delivery.WebhookID, &delivery.EventID, &delivery.Status, &delivery.StatusCode,
			&delivery.ResponseBody, &delivery.Error, &delivery.Attempts, &delivery.MaxRetries,
			&delivery.NextRetryAt, &delivery.LastAttemptAt, &delivery.CompletedAt, &delivery.CreatedAt, &delivery.UpdatedAt)

		if err != nil {
			return nil, err
		}

		deliveries = append(deliveries, delivery)
	}

	return deliveries, rows.Err()
}

// SaveDLQEvent saves a failed event to the dead letter queue.
func (s *Store) SaveDLQEvent(ctx context.Context, dlqEvent *DLQWebhookEvent) error {
	eventDataJSON, _ := json.Marshal(dlqEvent.EventData)

	query := `
		INSERT INTO webhook_dlq (id, webhook_id, event_id, event_data, error_msg, retry_count, last_error_at, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`

	_, err := s.db.ExecContext(ctx, query,
		dlqEvent.ID, dlqEvent.WebhookID, dlqEvent.EventID, string(eventDataJSON), dlqEvent.ErrorMsg,
		dlqEvent.RetryCount, dlqEvent.LastErrorAt, dlqEvent.CreatedAt, dlqEvent.UpdatedAt,
	)

	return err
}

// Error types
var (
	ErrWebhookNotFound  = fmt.Errorf("webhook not found")
	ErrDeliveryNotFound = fmt.Errorf("delivery not found")
)
