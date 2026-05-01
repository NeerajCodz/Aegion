package webhooks

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	analytics "github.com/aegion/aegion/modules/analytics"
)

// Manager orchestrates webhook registration, event matching, and delivery.
type Manager struct {
	store       *Store
	dispatcher  *Dispatcher
	queue       *Queue
	matcher     *Matcher
	retry       *RetryPolicy
	signer      *Signature
	logger      Logger
	config      ManagerConfig
	mu          sync.RWMutex
	workers     []*DeliveryWorker
	ctx         context.Context
	cancel      context.CancelFunc
	isRunning   bool
	rateLimiter map[string]*RateLimiter
}

// ManagerConfig holds manager configuration.
type ManagerConfig struct {
	MaxPerUser                  int
	MaxRetries                  int
	RetryBackoffBaseMs          int
	TimeoutSeconds              int
	BatchSize                   int
	WorkerThreads               int
	StoreDeliveryHistoryDays    int
	MaxCustomFilterDepth        int
	CircuitBreakerFailureCount  int
}

// NewManager creates a new webhook manager.
func NewManager(store *Store, logger Logger, config ManagerConfig) *Manager {
	if config.MaxPerUser <= 0 {
		config.MaxPerUser = 50
	}
	if config.MaxRetries <= 0 {
		config.MaxRetries = 5
	}
	if config.RetryBackoffBaseMs <= 0 {
		config.RetryBackoffBaseMs = 1000
	}
	if config.TimeoutSeconds <= 0 {
		config.TimeoutSeconds = 30
	}
	if config.BatchSize <= 0 {
		config.BatchSize = 100
	}
	if config.WorkerThreads <= 0 {
		config.WorkerThreads = 10
	}
	if config.StoreDeliveryHistoryDays <= 0 {
		config.StoreDeliveryHistoryDays = 30
	}
	if config.MaxCustomFilterDepth <= 0 {
		config.MaxCustomFilterDepth = 10
	}
	if config.CircuitBreakerFailureCount <= 0 {
		config.CircuitBreakerFailureCount = 5
	}

	return &Manager{
		store:       store,
		dispatcher:  NewDispatcher(config.TimeoutSeconds, logger),
		queue:       NewQueue(config.BatchSize * 10),
		matcher:     NewMatcher(MatcherConfig{MaxCustomFilterDepth: config.MaxCustomFilterDepth}),
		retry:       NewRetryPolicy(RetryConfig{MaxRetries: config.MaxRetries, BackoffBaseMs: config.RetryBackoffBaseMs, TimeoutSeconds: config.TimeoutSeconds, CircuitBreakerThreshold: config.CircuitBreakerFailureCount}),
		signer:      NewSignature(),
		logger:      logger,
		config:      config,
		rateLimiter: make(map[string]*RateLimiter),
	}
}

// Start initializes the webhook manager and starts delivery workers.
func (m *Manager) Start(ctx context.Context) error {
	m.mu.Lock()
	if m.isRunning {
		m.mu.Unlock()
		return fmt.Errorf("webhook manager already running")
	}

	m.ctx, m.cancel = context.WithCancel(ctx)
	m.isRunning = true
	m.mu.Unlock()

	m.logger.Info("starting webhook manager", "workers", m.config.WorkerThreads)

	// Start delivery workers
	m.workers = make([]*DeliveryWorker, m.config.WorkerThreads)
	for i := 0; i < m.config.WorkerThreads; i++ {
		worker := NewDeliveryWorker(i, m.queue, m.store, m.dispatcher, m.retry, m.signer, m.logger)
		m.workers[i] = worker
		go worker.Start(m.ctx)
	}

	return nil
}

// Stop gracefully shuts down the webhook manager.
func (m *Manager) Stop(ctx context.Context) error {
	m.mu.Lock()
	if !m.isRunning {
		m.mu.Unlock()
		return nil
	}

	m.isRunning = false
	m.mu.Unlock()

	m.logger.Info("stopping webhook manager")

	if m.cancel != nil {
		m.cancel()
	}

	// Wait for workers to finish
	done := make(chan struct{})
	go func() {
		for _, worker := range m.workers {
			worker.Stop()
		}
		close(done)
	}()

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// RegisterWebhook registers a new webhook for a user.
func (m *Manager) RegisterWebhook(ctx context.Context, userID string, req *WebhookRequest) (*analytics.Webhook, error) {
	// Validate request
	if err := ValidateWebhookRequest(req, m.config.MaxCustomFilterDepth); err != nil {
		return nil, err
	}

	// Check webhook count
	webhooks, err := m.store.ListWebhooks(ctx, userID)
	if err != nil {
		return nil, err
	}

	if len(webhooks) >= m.config.MaxPerUser {
		return nil, ErrMaxWebhooksReached
	}

	// Check rate limit
	if !m.getRateLimiter(userID).Allow() {
		return nil, ErrRateLimited
	}

	// Create webhook
	webhook := &analytics.Webhook{
		ID:           uuid.New().String(),
		UserID:       userID,
		URL:          req.URL,
		EventTypes:   req.EventFilter.EventTypes,
		Categories:   req.EventFilter.Categories,
		CustomFilter: req.EventFilter.CustomFilter,
		Secret:       req.Secret,
		Active:       true,
		FailureCount: 0,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}

	if err := m.store.CreateWebhook(ctx, webhook); err != nil {
		return nil, err
	}

	m.logger.Info("webhook registered", "webhook_id", webhook.ID, "user_id", userID)
	return webhook, nil
}

// UpdateWebhook updates an existing webhook.
func (m *Manager) UpdateWebhook(ctx context.Context, userID, webhookID string, req *WebhookRequest) (*analytics.Webhook, error) {
	// Validate request
	if err := ValidateWebhookRequest(req, m.config.MaxCustomFilterDepth); err != nil {
		return nil, err
	}

	// Get existing webhook
	webhook, err := m.store.GetWebhook(ctx, webhookID)
	if err != nil {
		return nil, err
	}

	if webhook.UserID != userID {
		return nil, fmt.Errorf("unauthorized")
	}

	// Update fields
	webhook.URL = req.URL
	webhook.EventTypes = req.EventFilter.EventTypes
	webhook.Categories = req.EventFilter.Categories
	webhook.CustomFilter = req.EventFilter.CustomFilter
	webhook.Active = req.Active
	webhook.UpdatedAt = time.Now()

	if err := m.store.UpdateWebhook(ctx, webhook); err != nil {
		return nil, err
	}

	m.logger.Info("webhook updated", "webhook_id", webhookID, "user_id", userID)
	return webhook, nil
}

// DeleteWebhook deactivates a webhook.
func (m *Manager) DeleteWebhook(ctx context.Context, userID, webhookID string) error {
	if err := m.store.DeleteWebhook(ctx, webhookID, userID); err != nil {
		return err
	}

	m.logger.Info("webhook deleted", "webhook_id", webhookID, "user_id", userID)
	return nil
}

// ListWebhooks lists all webhooks for a user.
func (m *Manager) ListWebhooks(ctx context.Context, userID string) ([]*analytics.Webhook, error) {
	return m.store.ListWebhooks(ctx, userID)
}

// GetWebhook retrieves a specific webhook.
func (m *Manager) GetWebhook(ctx context.Context, webhookID string) (*analytics.Webhook, error) {
	return m.store.GetWebhook(ctx, webhookID)
}

// PublishEvent publishes an event to matching webhooks.
func (m *Manager) PublishEvent(ctx context.Context, eventID, eventType, category string, data map[string]interface{}) error {
	m.mu.RLock()
	if !m.isRunning {
		m.mu.RUnlock()
		return fmt.Errorf("webhook manager not running")
	}
	m.mu.RUnlock()

	// This would be called by the sync manager or event publisher
	// For now, we'll queue deliveries for all matching webhooks
	m.logger.Debug("publishing event to webhooks", "event_id", eventID, "event_type", eventType)

	return nil
}

// DispatchEvent queues webhook deliveries for a specific event.
// This is called internally when a matching event occurs.
func (m *Manager) DispatchEvent(ctx context.Context, eventID, eventType, category string, data map[string]interface{}, webhooks []*analytics.Webhook) error {
	for _, webhook := range webhooks {
		if !webhook.Active {
			continue
		}

		// Check if webhook matches the event
		filter := EventFilter{
			EventTypes:   webhook.EventTypes,
			Categories:   webhook.Categories,
			CustomFilter: webhook.CustomFilter,
		}

		if !m.matcher.Matches(filter, eventType, category, data) {
			continue
		}

		// Create signed payload
		payload, headers, err := SignPayload(eventID, eventType, category, data, webhook.Secret)
		if err != nil {
			m.logger.Error("failed to sign payload", "error", err)
			continue
		}

		// Create delivery job
		job := &DeliveryJob{
			ID:        uuid.New().String(),
			WebhookID: webhook.ID,
			EventID:   eventID,
			EventType: eventType,
			Category:  category,
			Payload:   payload,
			Headers:   headers,
			CreatedAt: time.Now(),
			Attempts:  1,
			MaxRetries: m.config.MaxRetries,
		}

		// Enqueue job
		if err := m.queue.Enqueue(job); err != nil {
			m.logger.Warn("failed to enqueue webhook delivery", "webhook_id", webhook.ID, "error", err)
		}
	}

	return nil
}

// TestWebhook sends a test event to a webhook.
func (m *Manager) TestWebhook(ctx context.Context, webhookID string) (string, error) {
	webhook, err := m.store.GetWebhook(ctx, webhookID)
	if err != nil {
		return "", err
	}

	testData := map[string]interface{}{
		"test": true,
		"message": "This is a test webhook delivery",
	}

	payload, headers, err := SignPayload(uuid.New().String(), "test.event", "test", testData, webhook.Secret)
	if err != nil {
		return "", err
	}

	result := m.dispatcher.Deliver(ctx, webhook.URL, payload, headers)

	deliveryID := uuid.New().String()
	delivery := &analytics.WebhookDelivery{
		ID:            deliveryID,
		WebhookID:     webhook.ID,
		EventID:       "test-event",
		Status:        "success",
		StatusCode:    result.StatusCode,
		ResponseBody:  result.Body,
		Error:         result.Error,
		Attempts:      1,
		MaxRetries:    0,
		LastAttemptAt: time.Now(),
		CompletedAt:   &time.Time{},
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
	}

	if !result.Success {
		delivery.Status = "failed"
	}

	m.store.SaveDelivery(ctx, delivery)

	return deliveryID, nil
}

// GetDeliveryHistory retrieves delivery history for a webhook.
func (m *Manager) GetDeliveryHistory(ctx context.Context, webhookID string, limit int) ([]*analytics.WebhookDelivery, error) {
	return m.store.ListDeliveries(ctx, webhookID, limit)
}

// GetDelivery retrieves a delivery record by ID.
func (m *Manager) GetDelivery(ctx context.Context, deliveryID string) (*analytics.WebhookDelivery, error) {
	return m.store.GetDelivery(ctx, deliveryID)
}

// ReplayEvent replays an event to a webhook.
func (m *Manager) ReplayEvent(ctx context.Context, deliveryID string) error {
	delivery, err := m.store.GetDelivery(ctx, deliveryID)
	if err != nil {
		return err
	}

	webhook, err := m.store.GetWebhook(ctx, delivery.WebhookID)
	if err != nil {
		return err
	}

	if !webhook.Active {
		return ErrWebhookDisabled
	}

	// Create new delivery job for replay
	job := &DeliveryJob{
		ID:        uuid.New().String(),
		WebhookID: webhook.ID,
		EventID:   delivery.EventID,
		CreatedAt: time.Now(),
		Attempts:  1,
		MaxRetries: m.config.MaxRetries,
	}

	if err := m.queue.Enqueue(job); err != nil {
		return err
	}

	m.logger.Info("event replay queued", "delivery_id", deliveryID, "webhook_id", webhook.ID)
	return nil
}

// getRateLimiter gets or creates a rate limiter for a user.
func (m *Manager) getRateLimiter(userID string) *RateLimiter {
	m.mu.Lock()
	defer m.mu.Unlock()

	if limiter, exists := m.rateLimiter[userID]; exists {
		return limiter
	}

	// 50 requests per minute
	limiter := NewRateLimiter(50, time.Minute)
	m.rateLimiter[userID] = limiter
	return limiter
}
