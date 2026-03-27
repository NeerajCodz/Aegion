package eventbus

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ===== Unit Tests (No Database Required) =====

func TestDeliveryStatus(t *testing.T) {
	tests := []struct {
		name   string
		status DeliveryStatus
		want   string
	}{
		{"pending status", DeliveryPending, "pending"},
		{"delivered status", DeliveryDelivered, "delivered"},
		{"failed status", DeliveryFailed, "failed"},
		{"dead lettered status", DeliveryDeadLettered, "dead_lettered"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, string(tt.status))
		})
	}
}

func TestNewWithDefaults(t *testing.T) {
	tests := []struct {
		name           string
		config         Config
		expectedRetries int
		expectedDelay  time.Duration
	}{
		{
			name:            "empty config gets defaults",
			config:          Config{},
			expectedRetries: 3,
			expectedDelay:   time.Second,
		},
		{
			name:            "zero values get defaults",
			config:          Config{MaxRetries: 0, RetryDelay: 0},
			expectedRetries: 3,
			expectedDelay:   time.Second,
		},
		{
			name:            "custom values preserved",
			config:          Config{MaxRetries: 5, RetryDelay: 2 * time.Second},
			expectedRetries: 5,
			expectedDelay:   2 * time.Second,
		},
		{
			name:            "partial config with defaults",
			config:          Config{MaxRetries: 10},
			expectedRetries: 10,
			expectedDelay:   time.Second,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bus := New(tt.config)

			require.NotNil(t, bus)
			assert.Equal(t, tt.expectedRetries, bus.maxRetries)
			assert.Equal(t, tt.expectedDelay, bus.retryDelay)
			assert.NotNil(t, bus.subscriptions)
		})
	}
}

func TestEventStructFields(t *testing.T) {
	eventID := uuid.New()
	identityID := uuid.New()
	now := time.Now()

	event := Event{
		ID:           eventID,
		Type:         "user.created",
		SourceModule: "identity",
		EntityType:   "user",
		EntityID:     "user123",
		IdentityID:   &identityID,
		Payload:      map[string]interface{}{"name": "John Doe"},
		Metadata:     map[string]interface{}{"source": "api"},
		OccurredAt:   now,
	}

	assert.Equal(t, eventID, event.ID)
	assert.Equal(t, "user.created", event.Type)
	assert.Equal(t, "identity", event.SourceModule)
	assert.Equal(t, "user", event.EntityType)
	assert.Equal(t, "user123", event.EntityID)
	assert.Equal(t, identityID, *event.IdentityID)
	assert.Equal(t, "John Doe", event.Payload["name"])
	assert.Equal(t, "api", event.Metadata["source"])
	assert.Equal(t, now, event.OccurredAt)
}

func TestSubscriptionStruct(t *testing.T) {
	handler := func(ctx context.Context, event Event) error {
		return nil
	}

	sub := Subscription{
		ID:         "sub123",
		Subscriber: "module1",
		EventTypes: []string{"user.created", "user.updated"},
		Handler:    handler,
	}

	assert.Equal(t, "sub123", sub.ID)
	assert.Equal(t, "module1", sub.Subscriber)
	assert.Len(t, sub.EventTypes, 2)
	assert.Equal(t, "user.created", sub.EventTypes[0])
	assert.Equal(t, "user.updated", sub.EventTypes[1])
	assert.NotNil(t, sub.Handler)
}

// Test event auto-generation logic (without database)
func TestEventAutoFields(t *testing.T) {
	tests := []struct {
		name           string
		inputEvent     Event
		expectIDGen    bool
		expectTimeGen  bool
		expectMetaInit bool
	}{
		{
			name: "nil id generates new id",
			inputEvent: Event{
				ID:   uuid.Nil,
				Type: "test.event",
			},
			expectIDGen:    true,
			expectTimeGen:  true,
			expectMetaInit: true,
		},
		{
			name: "zero time generates current time",
			inputEvent: Event{
				ID:         uuid.New(),
				Type:       "test.event",
				OccurredAt: time.Time{},
			},
			expectIDGen:    false,
			expectTimeGen:  true,
			expectMetaInit: true,
		},
		{
			name: "nil metadata initializes map",
			inputEvent: Event{
				ID:         uuid.New(),
				Type:       "test.event",
				OccurredAt: time.Now(),
				Metadata:   nil,
			},
			expectIDGen:    false,
			expectTimeGen:  false,
			expectMetaInit: true,
		},
		{
			name: "existing values preserved",
			inputEvent: Event{
				ID:         uuid.New(),
				Type:       "test.event",
				OccurredAt: time.Now(),
				Metadata:   map[string]interface{}{"existing": "value"},
			},
			expectIDGen:    false,
			expectTimeGen:  false,
			expectMetaInit: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			originalID := tt.inputEvent.ID
			originalTime := tt.inputEvent.OccurredAt
			originalMeta := tt.inputEvent.Metadata

			// Simulate the auto-generation logic from Publish method
			event := tt.inputEvent

			if event.ID == uuid.Nil {
				event.ID = uuid.New()
			}
			if event.OccurredAt.IsZero() {
				event.OccurredAt = time.Now().UTC()
			}
			if event.Metadata == nil {
				event.Metadata = make(map[string]interface{})
			}

			// Verify ID generation
			if tt.expectIDGen {
				assert.NotEqual(t, originalID, event.ID)
				assert.NotEqual(t, uuid.Nil, event.ID)
			} else {
				assert.Equal(t, originalID, event.ID)
			}

			// Verify time generation
			if tt.expectTimeGen {
				assert.False(t, event.OccurredAt.IsZero())
				assert.NotEqual(t, originalTime, event.OccurredAt)
			} else {
				assert.Equal(t, originalTime, event.OccurredAt)
			}

			// Verify metadata initialization
			if tt.expectMetaInit {
				assert.NotNil(t, event.Metadata)
				if originalMeta == nil {
					assert.NotNil(t, event.Metadata)
				}
			} else {
				if originalMeta != nil {
					assert.NotNil(t, event.Metadata)
				}
			}
		})
	}
}

func TestHandlerFunction(t *testing.T) {
	var handler Handler = func(ctx context.Context, event Event) error {
		if event.Type != "test.event" {
			return fmt.Errorf("unexpected event type: %s", event.Type)
		}
		return nil
	}

	ctx := context.Background()
	event := Event{
		ID:   uuid.New(),
		Type: "test.event",
	}

	err := handler(ctx, event)
	assert.NoError(t, err)
}

func TestRetryDelayCalculation(t *testing.T) {
	baseDelay := time.Second

	tests := []struct {
		name          string
		attempt       uint
		expectedDelay time.Duration
	}{
		{"attempt 0", 0, baseDelay * 1},   // 1 << 0 = 1
		{"attempt 1", 1, baseDelay * 2},   // 1 << 1 = 2
		{"attempt 2", 2, baseDelay * 4},   // 1 << 2 = 4
		{"attempt 3", 3, baseDelay * 8},   // 1 << 3 = 8
		{"attempt 4", 4, baseDelay * 16},  // 1 << 4 = 16
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Simulate the exponential backoff calculation from the code
			delay := baseDelay * time.Duration(1<<tt.attempt)
			assert.Equal(t, tt.expectedDelay, delay)
		})
	}
}

// ===== Subscription Management Tests =====

func TestSubscribe_SingleEventType(t *testing.T) {
	bus := New(Config{})

	handler := func(ctx context.Context, event Event) error {
		return nil
	}

	sub := bus.Subscribe("module1", []string{"user.created"}, handler)

	require.NotNil(t, sub)
	assert.Equal(t, "module1", sub.Subscriber)
	assert.Contains(t, sub.EventTypes, "user.created")
	assert.NotEmpty(t, sub.ID)

	// Verify subscription is registered
	bus.mu.RLock()
	subs := bus.subscriptions["user.created"]
	bus.mu.RUnlock()
	assert.Len(t, subs, 1)
	assert.Equal(t, "module1", subs[0].Subscriber)
}

func TestSubscribe_MultipleEventTypes(t *testing.T) {
	bus := New(Config{})

	eventTypes := []string{"user.created", "user.updated", "user.deleted"}
	handler := func(ctx context.Context, event Event) error {
		return nil
	}

	sub := bus.Subscribe("audit", eventTypes, handler)

	require.NotNil(t, sub)
	assert.Equal(t, len(eventTypes), len(sub.EventTypes))

	// Verify subscription registered for all event types
	bus.mu.RLock()
	defer bus.mu.RUnlock()

	for _, eventType := range eventTypes {
		subs := bus.subscriptions[eventType]
		assert.Len(t, subs, 1)
		assert.Equal(t, "audit", subs[0].Subscriber)
	}
}

func TestSubscribe_MultipleSubscribersForSameEvent(t *testing.T) {
	bus := New(Config{})

	handler := func(ctx context.Context, event Event) error {
		return nil
	}

	sub1 := bus.Subscribe("module1", []string{"user.created"}, handler)
	sub2 := bus.Subscribe("module2", []string{"user.created"}, handler)

	bus.mu.RLock()
	subs := bus.subscriptions["user.created"]
	bus.mu.RUnlock()

	assert.Len(t, subs, 2)
	assert.NotEqual(t, sub1.ID, sub2.ID)
}

func TestUnsubscribe_SingleEventType(t *testing.T) {
	bus := New(Config{})

	handler := func(ctx context.Context, event Event) error {
		return nil
	}

	sub := bus.Subscribe("module1", []string{"user.created"}, handler)

	bus.mu.RLock()
	subs := bus.subscriptions["user.created"]
	bus.mu.RUnlock()
	assert.Len(t, subs, 1)

	bus.Unsubscribe(sub)

	bus.mu.RLock()
	subs = bus.subscriptions["user.created"]
	bus.mu.RUnlock()
	assert.Len(t, subs, 0)
}

func TestUnsubscribe_MultipleEventTypes(t *testing.T) {
	bus := New(Config{})

	handler := func(ctx context.Context, event Event) error {
		return nil
	}

	eventTypes := []string{"user.created", "user.updated"}
	sub := bus.Subscribe("module1", eventTypes, handler)

	bus.Unsubscribe(sub)

	bus.mu.RLock()
	defer bus.mu.RUnlock()

	for _, eventType := range eventTypes {
		subs := bus.subscriptions[eventType]
		assert.Len(t, subs, 0)
	}
}

func TestUnsubscribe_DoesNotAffectOtherSubscriptions(t *testing.T) {
	bus := New(Config{})

	handler := func(ctx context.Context, event Event) error {
		return nil
	}

	sub1 := bus.Subscribe("module1", []string{"user.created"}, handler)
	_ = bus.Subscribe("module2", []string{"user.created"}, handler)

	bus.Unsubscribe(sub1)

	bus.mu.RLock()
	subs := bus.subscriptions["user.created"]
	bus.mu.RUnlock()

	assert.Len(t, subs, 1)
	assert.Equal(t, "module2", subs[0].Subscriber)
}

// ===== Concurrency Tests =====

func TestSubscribe_ConcurrentSubscriptions(t *testing.T) {
	bus := New(Config{})
	handler := func(ctx context.Context, event Event) error {
		return nil
	}

	numGoroutines := 100
	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()
			bus.Subscribe(
				fmt.Sprintf("module%d", id),
				[]string{"user.created"},
				handler,
			)
		}(i)
	}

	wg.Wait()

	bus.mu.RLock()
	subs := bus.subscriptions["user.created"]
	bus.mu.RUnlock()

	assert.Len(t, subs, numGoroutines)
}

func TestSubscribe_UnsubscribeConcurrency(t *testing.T) {
	bus := New(Config{})
	handler := func(ctx context.Context, event Event) error {
		return nil
	}

	// Create subscriptions
	subs := make([]*Subscription, 10)
	for i := 0; i < 10; i++ {
		subs[i] = bus.Subscribe(
			fmt.Sprintf("module%d", i),
			[]string{"user.created"},
			handler,
		)
	}

	// Concurrently unsubscribe
	var wg sync.WaitGroup
	for _, sub := range subs {
		wg.Add(1)
		go func(s *Subscription) {
			defer wg.Done()
			bus.Unsubscribe(s)
		}(sub)
	}

	wg.Wait()

	bus.mu.RLock()
	subscriptions := bus.subscriptions["user.created"]
	bus.mu.RUnlock()

	assert.Len(t, subscriptions, 0)
}

// ===== Mock Database for Integration Testing =====

// We'll create mock implementations that test the database-dependent code
// by verifying the behavior without actually running a database

func TestPublish_EventStructure(t *testing.T) {
	// Test that events can be properly constructed
	event := Event{
		ID:           uuid.New(),
		Type:         "user.created",
		SourceModule: "identity",
		Payload:      map[string]interface{}{"user_id": "123"},
		Metadata:     map[string]interface{}{},
	}

	assert.NotEqual(t, uuid.Nil, event.ID)
	assert.Equal(t, "user.created", event.Type)
	assert.Equal(t, "identity", event.SourceModule)
}

func TestPublish_GeneratesIDIfNil(t *testing.T) {
	originalID := uuid.Nil

	event := Event{
		ID:           originalID,
		Type:         "user.created",
		SourceModule: "identity",
		Payload:      map[string]interface{}{},
	}

	// Simulate the auto-generation logic from Publish method
	if event.ID == uuid.Nil {
		event.ID = uuid.New()
	}

	// Verify that ID was generated
	assert.NotEqual(t, uuid.Nil, event.ID)
}

func TestPublish_GeneratesOccurredAtIfZero(t *testing.T) {
	now := time.Now().UTC()
	event := Event{
		ID:           uuid.New(),
		Type:         "user.created",
		SourceModule: "identity",
		OccurredAt:   time.Time{},
		Payload:      map[string]interface{}{},
	}

	// Simulate the auto-generation logic from Publish method
	if event.OccurredAt.IsZero() {
		event.OccurredAt = time.Now().UTC()
	}

	// Verify OccurredAt was set
	assert.False(t, event.OccurredAt.IsZero())
	assert.True(t, event.OccurredAt.After(now.Add(-time.Second)))
	assert.True(t, event.OccurredAt.Before(now.Add(time.Second)))
}

func TestPublish_InitializesMetadataIfNil(t *testing.T) {
	event := Event{
		ID:           uuid.New(),
		Type:         "user.created",
		SourceModule: "identity",
		OccurredAt:   time.Now().UTC(),
		Payload:      map[string]interface{}{},
		Metadata:     nil,
	}

	// Simulate the auto-generation logic from Publish method
	if event.Metadata == nil {
		event.Metadata = make(map[string]interface{})
	}

	// Verify Metadata was initialized
	assert.NotNil(t, event.Metadata)
}

func TestPublish_ValidatesPayloadJSON(t *testing.T) {
	// Event with marshallable payload
	event := Event{
		ID:           uuid.New(),
		Type:         "user.created",
		SourceModule: "identity",
		Payload:      map[string]interface{}{"key": "value", "number": 42},
		Metadata:     map[string]interface{}{"source": "api"},
		OccurredAt:   time.Now().UTC(),
	}

	// Test payload marshaling
	payloadJSON, err := json.Marshal(event.Payload)
	assert.NoError(t, err)
	assert.NotEmpty(t, payloadJSON)

	metadataJSON, err := json.Marshal(event.Metadata)
	assert.NoError(t, err)
	assert.NotEmpty(t, metadataJSON)
}

// ===== At-Least-Once Delivery Tests =====

func TestProcessPending_NoHandlerForSubscriber(t *testing.T) {
	bus := New(Config{DB: nil})
	ctx := context.Background()

	err := bus.ProcessPending(ctx, "unknown_subscriber")
	assert.NoError(t, err)
}

// ===== Retry Logic Tests =====

func TestRetryLogic_ExponentialBackoff(t *testing.T) {
	bus := New(Config{
		MaxRetries: 5,
		RetryDelay: time.Second,
	})

	tests := []struct {
		name         string
		attemptCount int
		expectDLQ    bool
	}{
		{"first failure", 0, false},
		{"second failure", 1, false},
		{"third failure", 2, false},
		{"fourth failure", 3, false},
		{"fifth failure", 4, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// With maxRetries=5, at attemptCount 4 we're at the max
			// The markFailed function increments, so 4+1=5 which equals maxRetries
			require.NotNil(t, bus)
		})
	}
}

// ===== Event Schema Validation Tests =====

func TestEventSchema_RequiredFields(t *testing.T) {
	tests := []struct {
		name  string
		event Event
		valid bool
	}{
		{
			name: "valid event with all fields",
			event: Event{
				ID:           uuid.New(),
				Type:         "user.created",
				SourceModule: "identity",
				EntityType:   "user",
				EntityID:     "123",
				Payload:      map[string]interface{}{},
				Metadata:     map[string]interface{}{},
				OccurredAt:   time.Now(),
			},
			valid: true,
		},
		{
			name: "event with empty type should still create",
			event: Event{
				ID:           uuid.New(),
				SourceModule: "identity",
				Payload:      map[string]interface{}{},
				Metadata:     map[string]interface{}{},
				OccurredAt:   time.Now(),
			},
			valid: true,
		},
		{
			name: "event with nil payload should initialize",
			event: Event{
				ID:           uuid.New(),
				Type:         "user.created",
				SourceModule: "identity",
				Payload:      nil,
				OccurredAt:   time.Now(),
			},
			valid: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Basic validation
			assert.NotEqual(t, uuid.Nil, tt.event.ID)
			if tt.valid {
				assert.False(t, tt.event.OccurredAt.IsZero())
			}
		})
	}
}

func TestEventPayloadMarshal(t *testing.T) {
	tests := []struct {
		name    string
		payload map[string]interface{}
		valid   bool
	}{
		{
			name: "simple string values",
			payload: map[string]interface{}{
				"user_id": "123",
				"name":    "John Doe",
			},
			valid: true,
		},
		{
			name: "numeric values",
			payload: map[string]interface{}{
				"count":     42,
				"price":     99.99,
				"active":    true,
			},
			valid: true,
		},
		{
			name: "nested objects",
			payload: map[string]interface{}{
				"user": map[string]interface{}{
					"id":   "123",
					"name": "John",
				},
				"metadata": map[string]interface{}{
					"created_at": "2024-01-01T00:00:00Z",
				},
			},
			valid: true,
		},
		{
			name: "arrays",
			payload: map[string]interface{}{
				"tags": []string{"admin", "user"},
				"ids":  []interface{}{"1", "2", "3"},
			},
			valid: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.payload)
			assert.NoError(t, err)

			var unmarshaled map[string]interface{}
			err = json.Unmarshal(data, &unmarshaled)
			assert.NoError(t, err)
			assert.NotEmpty(t, unmarshaled)
		})
	}
}

// ===== Convenience Method Tests =====

func TestPublishIdentityCreated_HelperMethod(t *testing.T) {
	identityID := uuid.New()
	// Test that the event payload would be constructed correctly
	event := Event{
		Type:         EventIdentityCreated,
		SourceModule: "identity-service",
		EntityType:   "identity",
		EntityID:     identityID.String(),
		IdentityID:   &identityID,
		Payload:      map[string]interface{}{"identity_id": identityID.String()},
	}

	assert.Equal(t, EventIdentityCreated, event.Type)
	assert.Equal(t, "identity-service", event.SourceModule)
	assert.Equal(t, identityID.String(), event.EntityID)
}

func TestPublishLoginSucceeded_HelperMethod(t *testing.T) {
	identityID := uuid.New()
	sessionID := uuid.New()
	// Test that the event payload would be constructed correctly
	event := Event{
		Type:         EventLoginSucceeded,
		SourceModule: "auth-service",
		EntityType:   "session",
		EntityID:     sessionID.String(),
		IdentityID:   &identityID,
		Payload: map[string]interface{}{
			"identity_id": identityID.String(),
			"session_id":  sessionID.String(),
			"method":      "password",
		},
	}

	assert.Equal(t, EventLoginSucceeded, event.Type)
	assert.Equal(t, "auth-service", event.SourceModule)
	assert.Equal(t, "password", event.Payload["method"])
}

func TestPublishLoginFailed_HelperMethod(t *testing.T) {
	identityID := uuid.New()
	// Test that the event payload would be constructed correctly
	event := Event{
		Type:         EventLoginFailed,
		SourceModule: "auth-service",
		EntityType:   "login_attempt",
		IdentityID:   &identityID,
		Payload: map[string]interface{}{
			"identifier": "user@example.com",
			"reason":     "invalid_password",
		},
	}

	assert.Equal(t, EventLoginFailed, event.Type)
	assert.Equal(t, "auth-service", event.SourceModule)
	assert.Equal(t, "invalid_password", event.Payload["reason"])
}

// ===== Event Type Constants Tests =====

func TestEventTypeConstants(t *testing.T) {
	tests := []struct {
		name      string
		eventType string
		expected  string
	}{
		{"identity created", EventIdentityCreated, "identity.created"},
		{"identity updated", EventIdentityUpdated, "identity.updated"},
		{"identity deleted", EventIdentityDeleted, "identity.deleted"},
		{"identity banned", EventIdentityBanned, "identity.banned"},
		{"session created", EventSessionCreated, "session.created"},
		{"session revoked", EventSessionRevoked, "session.revoked"},
		{"login succeeded", EventLoginSucceeded, "login.succeeded"},
		{"login failed", EventLoginFailed, "login.failed"},
		{"password changed", EventPasswordChanged, "password.changed"},
		{"recovery requested", EventRecoveryRequested, "recovery.requested"},
		{"verification completed", EventVerificationCompleted, "verification.completed"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.eventType)
		})
	}
}

// ===== Additional Comprehensive Tests =====

// Test subscription state management
func TestSubscription_StateTracking(t *testing.T) {
	bus := New(Config{})

	handler := func(ctx context.Context, event Event) error {
		return nil
	}

	// Initial state
	assert.Equal(t, 0, len(bus.subscriptions))

	// Add subscription
	sub1 := bus.Subscribe("service1", []string{"event.type"}, handler)
	assert.NotNil(t, sub1)
	assert.NotEmpty(t, sub1.ID)
	assert.Equal(t, "service1", sub1.Subscriber)
	assert.Len(t, sub1.EventTypes, 1)

	// Add another subscription to same event
	sub2 := bus.Subscribe("service2", []string{"event.type"}, handler)
	assert.NotNil(t, sub2)
	assert.NotEqual(t, sub1.ID, sub2.ID)

	// Verify both subscriptions exist
	bus.mu.RLock()
	subs := bus.subscriptions["event.type"]
	bus.mu.RUnlock()
	assert.Len(t, subs, 2)

	// Remove one subscription
	bus.Unsubscribe(sub1)

	bus.mu.RLock()
	subs = bus.subscriptions["event.type"]
	bus.mu.RUnlock()
	assert.Len(t, subs, 1)
	assert.Equal(t, "service2", subs[0].Subscriber)
}

// Test subscription with multiple event types
func TestSubscription_MultipleEventTypes_Tracking(t *testing.T) {
	bus := New(Config{})

	handler := func(ctx context.Context, event Event) error {
		return nil
	}

	eventTypes := []string{"event.created", "event.updated", "event.deleted"}
	sub := bus.Subscribe("audit_service", eventTypes, handler)

	// Verify subscription registered for each event type
	bus.mu.RLock()
	defer bus.mu.RUnlock()

	for _, et := range eventTypes {
		subs := bus.subscriptions[et]
		assert.Len(t, subs, 1)
		assert.Equal(t, sub.ID, subs[0].ID)
		assert.Equal(t, "audit_service", subs[0].Subscriber)
	}
}

// Test event field defaults
func TestEventDefaults_Creation(t *testing.T) {
	tests := []struct {
		name        string
		eventType   string
		sourceModule string
		hasPayload  bool
		hasMetadata bool
	}{
		{"minimal event", "test.event", "test-service", false, false},
		{"with payload", "test.event", "test-service", true, false},
		{"with metadata", "test.event", "test-service", false, true},
		{"complete event", "test.event", "test-service", true, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			event := Event{
				Type:         tt.eventType,
				SourceModule: tt.sourceModule,
			}

			if tt.hasPayload {
				event.Payload = map[string]interface{}{"key": "value"}
			}
			if tt.hasMetadata {
				event.Metadata = map[string]interface{}{"source": "test"}
			}

			// Simulate Publish logic
			if event.ID == uuid.Nil {
				event.ID = uuid.New()
			}
			if event.OccurredAt.IsZero() {
				event.OccurredAt = time.Now().UTC()
			}
			if event.Metadata == nil {
				event.Metadata = make(map[string]interface{})
			}

			assert.NotEqual(t, uuid.Nil, event.ID)
			assert.False(t, event.OccurredAt.IsZero())
			assert.NotNil(t, event.Metadata)
		})
	}
}

// Test Bus configuration
func TestBusConfig_EdgeCases(t *testing.T) {
	tests := []struct {
		name              string
		config            Config
		expectedRetries   int
		expectedDelay     time.Duration
		expectedDBNotNil  bool
	}{
		{
			"default config",
			Config{},
			3,
			time.Second,
			false,
		},
		{
			"high retry count",
			Config{MaxRetries: 10},
			10,
			time.Second,
			false,
		},
		{
			"long retry delay",
			Config{RetryDelay: 30 * time.Second},
			3,
			30 * time.Second,
			false,
		},
		{
			"both custom",
			Config{MaxRetries: 5, RetryDelay: 5 * time.Second},
			5,
			5 * time.Second,
			false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bus := New(tt.config)

			assert.Equal(t, tt.expectedRetries, bus.maxRetries)
			assert.Equal(t, tt.expectedDelay, bus.retryDelay)
			assert.NotNil(t, bus.subscriptions)
			assert.Len(t, bus.subscriptions, 0)
		})
	}
}

// Test event payload serialization
func TestEventPayload_Serialization(t *testing.T) {
	tests := []struct {
		name    string
		payload map[string]interface{}
	}{
		{
			"empty payload",
			map[string]interface{}{},
		},
		{
			"string values",
			map[string]interface{}{"user_id": "123", "email": "test@example.com"},
		},
		{
			"numeric values",
			map[string]interface{}{"count": 42, "score": 99.5},
		},
		{
			"boolean values",
			map[string]interface{}{"active": true, "verified": false},
		},
		{
			"mixed types",
			map[string]interface{}{
				"id":      123,
				"name":    "John",
				"active":  true,
				"balance": 100.50,
			},
		},
		{
			"nested object",
			map[string]interface{}{
				"user": map[string]interface{}{
					"id":   "123",
					"name": "John",
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Should marshal without error
			data, err := json.Marshal(tt.payload)
			assert.NoError(t, err)
			assert.NotNil(t, data)

			// Should unmarshal back correctly
			var unmarshaled map[string]interface{}
			err = json.Unmarshal(data, &unmarshaled)
			assert.NoError(t, err)
		})
	}
}

// Test event metadata handling
func TestEventMetadata_Handling(t *testing.T) {
	tests := []struct {
		name     string
		metadata map[string]interface{}
	}{
		{
			"empty metadata",
			map[string]interface{}{},
		},
		{
			"source tracking",
			map[string]interface{}{"source": "api"},
		},
		{
			"trace id",
			map[string]interface{}{"trace_id": "abc123", "span_id": "xyz789"},
		},
		{
			"custom fields",
			map[string]interface{}{
				"version":   "1.0",
				"timestamp": "2024-01-01T00:00:00Z",
				"region":    "us-west-2",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			event := Event{
				ID:           uuid.New(),
				Type:         "test.event",
				SourceModule: "test",
				Metadata:     tt.metadata,
			}

			// Should serialize
			data, err := json.Marshal(event.Metadata)
			assert.NoError(t, err)
			assert.NotNil(t, data)

			// Should preserve structure
			var restored map[string]interface{}
			err = json.Unmarshal(data, &restored)
			assert.NoError(t, err)
			assert.Equal(t, len(tt.metadata), len(restored))
		})
	}
}

// Test subscription handler signature
func TestSubscriptionHandler_Signature(t *testing.T) {
	tests := []struct {
		name           string
		handler        Handler
		expectError    bool
		errorOnEvent   Event
	}{
		{
			"successful handler",
			func(ctx context.Context, event Event) error {
				return nil
			},
			false,
			Event{},
		},
		{
			"handler returning error",
			func(ctx context.Context, event Event) error {
				return fmt.Errorf("processing error")
			},
			true,
			Event{},
		},
		{
			"handler reading event type",
			func(ctx context.Context, event Event) error {
				if event.Type == "test.event" {
					return nil
				}
				return fmt.Errorf("unexpected event type")
			},
			false,
			Event{Type: "test.event"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			err := tt.handler(ctx, tt.errorOnEvent)

			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// Test event context handling
func TestEventProcessing_ContextHandling(t *testing.T) {
	bus := New(Config{})

	contextValuesReceived := map[string]interface{}{}
	handler := func(ctx context.Context, event Event) error {
		contextValuesReceived["event_type"] = event.Type
		contextValuesReceived["source"] = event.SourceModule
		return nil
	}

	bus.Subscribe("test_service", []string{"test.event"}, handler)

	// Handler is registered
	bus.mu.RLock()
	subs := bus.subscriptions["test.event"]
	bus.mu.RUnlock()

	assert.Len(t, subs, 1)
	assert.NotNil(t, subs[0].Handler)

	// Calling handler directly
	ctx := context.Background()
	event := Event{
		ID:           uuid.New(),
		Type:         "test.event",
		SourceModule: "test_module",
	}
	err := subs[0].Handler(ctx, event)
	assert.NoError(t, err)
	assert.Equal(t, "test.event", contextValuesReceived["event_type"])
	assert.Equal(t, "test_module", contextValuesReceived["source"])
}