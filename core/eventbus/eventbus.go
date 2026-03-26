// Package eventbus provides an internal event bus for Aegion modules.
package eventbus

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Event represents an internal event.
type Event struct {
	ID           uuid.UUID              `json:"id"`
	Type         string                 `json:"type"`
	SourceModule string                 `json:"source_module"`
	EntityType   string                 `json:"entity_type,omitempty"`
	EntityID     string                 `json:"entity_id,omitempty"`
	IdentityID   *uuid.UUID             `json:"identity_id,omitempty"`
	Payload      map[string]interface{} `json:"payload"`
	Metadata     map[string]interface{} `json:"metadata"`
	OccurredAt   time.Time              `json:"occurred_at"`
}

// DeliveryStatus represents the status of an event delivery.
type DeliveryStatus string

const (
	DeliveryPending      DeliveryStatus = "pending"
	DeliveryDelivered    DeliveryStatus = "delivered"
	DeliveryFailed       DeliveryStatus = "failed"
	DeliveryDeadLettered DeliveryStatus = "dead_lettered"
)

// Handler is a function that handles an event.
type Handler func(ctx context.Context, event Event) error

// Subscription represents a subscription to event types.
type Subscription struct {
	ID           string
	Subscriber   string
	EventTypes   []string
	Handler      Handler
}

// Bus is the event bus implementation.
type Bus struct {
	db            *pgxpool.Pool
	subscriptions map[string][]Subscription
	mu            sync.RWMutex
	maxRetries    int
	retryDelay    time.Duration
}

// Config holds event bus configuration.
type Config struct {
	DB         *pgxpool.Pool
	MaxRetries int
	RetryDelay time.Duration
}

// New creates a new event bus.
func New(cfg Config) *Bus {
	if cfg.MaxRetries == 0 {
		cfg.MaxRetries = 3
	}
	if cfg.RetryDelay == 0 {
		cfg.RetryDelay = time.Second
	}

	return &Bus{
		db:            cfg.DB,
		subscriptions: make(map[string][]Subscription),
		maxRetries:    cfg.MaxRetries,
		retryDelay:    cfg.RetryDelay,
	}
}

// Publish publishes an event to the bus.
func (b *Bus) Publish(ctx context.Context, event Event) error {
	if event.ID == uuid.Nil {
		event.ID = uuid.New()
	}
	if event.OccurredAt.IsZero() {
		event.OccurredAt = time.Now().UTC()
	}
	if event.Metadata == nil {
		event.Metadata = make(map[string]interface{})
	}

	payloadJSON, err := json.Marshal(event.Payload)
	if err != nil {
		return err
	}

	metadataJSON, err := json.Marshal(event.Metadata)
	if err != nil {
		return err
	}

	tx, err := b.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	// Insert event
	_, err = tx.Exec(ctx, `
		INSERT INTO core_event_bus_events (
			id, event_type, source_module, entity_type, entity_id,
			identity_id, payload, metadata, occurred_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`,
		event.ID, event.Type, event.SourceModule, event.EntityType,
		event.EntityID, event.IdentityID, payloadJSON, metadataJSON,
		event.OccurredAt,
	)
	if err != nil {
		return err
	}

	// Create delivery records for all subscribers
	b.mu.RLock()
	subscribers := b.subscriptions[event.Type]
	b.mu.RUnlock()

	for _, sub := range subscribers {
		_, err = tx.Exec(ctx, `
			INSERT INTO core_event_bus_deliveries (
				event_id, subscriber, status, next_retry_at
			) VALUES ($1, $2, 'pending', NOW())
		`, event.ID, sub.Subscriber)
		if err != nil {
			return err
		}
	}

	return tx.Commit(ctx)
}

// Subscribe registers a handler for event types.
func (b *Bus) Subscribe(subscriber string, eventTypes []string, handler Handler) *Subscription {
	sub := Subscription{
		ID:         uuid.New().String(),
		Subscriber: subscriber,
		EventTypes: eventTypes,
		Handler:    handler,
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	for _, eventType := range eventTypes {
		b.subscriptions[eventType] = append(b.subscriptions[eventType], sub)
	}

	return &sub
}

// Unsubscribe removes a subscription.
func (b *Bus) Unsubscribe(sub *Subscription) {
	b.mu.Lock()
	defer b.mu.Unlock()

	for _, eventType := range sub.EventTypes {
		subs := b.subscriptions[eventType]
		for i, s := range subs {
			if s.ID == sub.ID {
				b.subscriptions[eventType] = append(subs[:i], subs[i+1:]...)
				break
			}
		}
	}
}

// ProcessPending processes pending event deliveries for a subscriber.
func (b *Bus) ProcessPending(ctx context.Context, subscriber string) error {
	b.mu.RLock()
	// Find handler for this subscriber
	var handler Handler
	for _, subs := range b.subscriptions {
		for _, sub := range subs {
			if sub.Subscriber == subscriber {
				handler = sub.Handler
				break
			}
		}
		if handler != nil {
			break
		}
	}
	b.mu.RUnlock()

	if handler == nil {
		return nil
	}

	// Get pending deliveries
	rows, err := b.db.Query(ctx, `
		SELECT d.id, d.event_id, d.attempt_count,
			   e.event_type, e.source_module, e.entity_type, e.entity_id,
			   e.identity_id, e.payload, e.metadata, e.occurred_at
		FROM core_event_bus_deliveries d
		JOIN core_event_bus_events e ON d.event_id = e.id
		WHERE d.subscriber = $1
		  AND d.status IN ('pending', 'failed')
		  AND d.next_retry_at <= NOW()
		ORDER BY e.occurred_at
		LIMIT 100
		FOR UPDATE SKIP LOCKED
	`, subscriber)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var (
			deliveryID   uuid.UUID
			eventID      uuid.UUID
			attemptCount int
			event        Event
			payloadJSON  []byte
			metadataJSON []byte
		)

		err := rows.Scan(
			&deliveryID, &eventID, &attemptCount,
			&event.Type, &event.SourceModule, &event.EntityType, &event.EntityID,
			&event.IdentityID, &payloadJSON, &metadataJSON, &event.OccurredAt,
		)
		if err != nil {
			continue
		}

		event.ID = eventID
		json.Unmarshal(payloadJSON, &event.Payload)
		json.Unmarshal(metadataJSON, &event.Metadata)

		// Process event
		err = handler(ctx, event)
		if err != nil {
			b.markFailed(ctx, deliveryID, attemptCount, err)
		} else {
			b.markDelivered(ctx, deliveryID)
		}
	}

	return rows.Err()
}

// markDelivered marks a delivery as successful.
func (b *Bus) markDelivered(ctx context.Context, deliveryID uuid.UUID) {
	b.db.Exec(ctx, `
		UPDATE core_event_bus_deliveries
		SET status = 'delivered', delivered_at = NOW(), updated_at = NOW()
		WHERE id = $1
	`, deliveryID)
}

// markFailed marks a delivery as failed and schedules retry.
func (b *Bus) markFailed(ctx context.Context, deliveryID uuid.UUID, attemptCount int, err error) {
	attemptCount++
	
	if attemptCount >= b.maxRetries {
		// Dead letter
		b.db.Exec(ctx, `
			UPDATE core_event_bus_deliveries
			SET status = 'dead_lettered',
				attempt_count = $2,
				last_error = $3,
				updated_at = NOW()
			WHERE id = $1
		`, deliveryID, attemptCount, err.Error())
	} else {
		// Schedule retry with exponential backoff
		retryDelay := b.retryDelay * time.Duration(1<<uint(attemptCount))
		b.db.Exec(ctx, `
			UPDATE core_event_bus_deliveries
			SET status = 'failed',
				attempt_count = $2,
				last_error = $3,
				next_retry_at = $4,
				updated_at = NOW()
			WHERE id = $1
		`, deliveryID, attemptCount, err.Error(), time.Now().Add(retryDelay))
	}
}

// Cleanup removes old events and deliveries.
func (b *Bus) Cleanup(ctx context.Context, olderThan time.Duration) (int64, error) {
	cutoff := time.Now().Add(-olderThan)

	// Delete old delivered/dead-lettered events
	result, err := b.db.Exec(ctx, `
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

	return result.RowsAffected(), nil
}

// Common event types
const (
	EventIdentityCreated       = "identity.created"
	EventIdentityUpdated       = "identity.updated"
	EventIdentityDeleted       = "identity.deleted"
	EventIdentityBanned        = "identity.banned"
	EventSessionCreated        = "session.created"
	EventSessionRevoked        = "session.revoked"
	EventLoginSucceeded        = "login.succeeded"
	EventLoginFailed           = "login.failed"
	EventPasswordChanged       = "password.changed"
	EventRecoveryRequested     = "recovery.requested"
	EventVerificationCompleted = "verification.completed"
)

// PublishIdentityCreated publishes an identity.created event.
func (b *Bus) PublishIdentityCreated(ctx context.Context, identityID uuid.UUID, source string) error {
	return b.Publish(ctx, Event{
		Type:         EventIdentityCreated,
		SourceModule: source,
		EntityType:   "identity",
		EntityID:     identityID.String(),
		IdentityID:   &identityID,
		Payload:      map[string]interface{}{"identity_id": identityID.String()},
	})
}

// PublishLoginSucceeded publishes a login.succeeded event.
func (b *Bus) PublishLoginSucceeded(ctx context.Context, identityID, sessionID uuid.UUID, method string, source string) error {
	return b.Publish(ctx, Event{
		Type:         EventLoginSucceeded,
		SourceModule: source,
		EntityType:   "session",
		EntityID:     sessionID.String(),
		IdentityID:   &identityID,
		Payload: map[string]interface{}{
			"identity_id": identityID.String(),
			"session_id":  sessionID.String(),
			"method":      method,
		},
	})
}

// PublishLoginFailed publishes a login.failed event.
func (b *Bus) PublishLoginFailed(ctx context.Context, identifier, reason, source string, identityID *uuid.UUID) error {
	return b.Publish(ctx, Event{
		Type:         EventLoginFailed,
		SourceModule: source,
		EntityType:   "login_attempt",
		IdentityID:   identityID,
		Payload: map[string]interface{}{
			"identifier": identifier,
			"reason":     reason,
		},
	})
}
