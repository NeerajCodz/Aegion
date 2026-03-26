package eventbus

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
)

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
			if string(tt.status) != tt.want {
				t.Errorf("DeliveryStatus = %s, want %s", string(tt.status), tt.want)
			}
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
			name:           "empty config gets defaults",
			config:         Config{},
			expectedRetries: 3,
			expectedDelay:  time.Second,
		},
		{
			name:           "zero values get defaults",
			config:         Config{MaxRetries: 0, RetryDelay: 0},
			expectedRetries: 3,
			expectedDelay:  time.Second,
		},
		{
			name:           "custom values preserved",
			config:         Config{MaxRetries: 5, RetryDelay: 2 * time.Second},
			expectedRetries: 5,
			expectedDelay:  2 * time.Second,
		},
		{
			name:           "partial config with defaults",
			config:         Config{MaxRetries: 10},
			expectedRetries: 10,
			expectedDelay:  time.Second,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bus := New(tt.config)
			
			if bus == nil {
				t.Fatal("New() returned nil")
			}
			
			if bus.maxRetries != tt.expectedRetries {
				t.Errorf("maxRetries = %d, want %d", bus.maxRetries, tt.expectedRetries)
			}
			
			if bus.retryDelay != tt.expectedDelay {
				t.Errorf("retryDelay = %v, want %v", bus.retryDelay, tt.expectedDelay)
			}

			if bus.subscriptions == nil {
				t.Error("subscriptions map is nil")
			}
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

	if event.ID != eventID {
		t.Errorf("ID = %v, want %v", event.ID, eventID)
	}
	if event.Type != "user.created" {
		t.Errorf("Type = %s, want user.created", event.Type)
	}
	if event.SourceModule != "identity" {
		t.Errorf("SourceModule = %s, want identity", event.SourceModule)
	}
	if event.EntityType != "user" {
		t.Errorf("EntityType = %s, want user", event.EntityType)
	}
	if event.EntityID != "user123" {
		t.Errorf("EntityID = %s, want user123", event.EntityID)
	}
	if *event.IdentityID != identityID {
		t.Errorf("IdentityID = %v, want %v", *event.IdentityID, identityID)
	}
	if event.Payload["name"] != "John Doe" {
		t.Errorf("Payload[name] = %v, want John Doe", event.Payload["name"])
	}
	if event.Metadata["source"] != "api" {
		t.Errorf("Metadata[source] = %v, want api", event.Metadata["source"])
	}
	if event.OccurredAt != now {
		t.Errorf("OccurredAt mismatch")
	}
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

	if sub.ID != "sub123" {
		t.Errorf("ID = %s, want sub123", sub.ID)
	}
	if sub.Subscriber != "module1" {
		t.Errorf("Subscriber = %s, want module1", sub.Subscriber)
	}
	if len(sub.EventTypes) != 2 {
		t.Errorf("EventTypes length = %d, want 2", len(sub.EventTypes))
	}
	if sub.EventTypes[0] != "user.created" {
		t.Errorf("EventTypes[0] = %s, want user.created", sub.EventTypes[0])
	}
	if sub.EventTypes[1] != "user.updated" {
		t.Errorf("EventTypes[1] = %s, want user.updated", sub.EventTypes[1])
	}
	if sub.Handler == nil {
		t.Error("Handler is nil")
	}
}

// Test event auto-generation logic (without database)
func TestEventAutoFields(t *testing.T) {
	tests := []struct {
		name        string
		inputEvent  Event
		expectIDGen bool
		expectTimeGen bool
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
				if event.ID == originalID || event.ID == uuid.Nil {
					t.Error("Expected ID to be generated")
				}
			} else {
				if event.ID != originalID {
					t.Error("Expected ID to be preserved")
				}
			}

			// Verify time generation
			if tt.expectTimeGen {
				if event.OccurredAt.IsZero() || event.OccurredAt == originalTime {
					t.Error("Expected OccurredAt to be generated")
				}
			} else {
				if event.OccurredAt != originalTime {
					t.Error("Expected OccurredAt to be preserved")
				}
			}

			// Verify metadata initialization
			if tt.expectMetaInit {
				if event.Metadata == nil {
					t.Error("Expected Metadata to be initialized")
				}
				// Can't directly compare maps in Go, but we can check if it was initialized
				if originalMeta == nil && event.Metadata == nil {
					t.Error("Expected new map to be created")
				}
			} else {
				// For cases where we don't expect initialization, just check it's not nil if it wasn't before
				if originalMeta != nil && event.Metadata == nil {
					t.Error("Expected Metadata to be preserved")
				}
			}
		})
	}
}

func TestHandlerFunction(t *testing.T) {
	// Test that handler function signature works
	var handler Handler = func(ctx context.Context, event Event) error {
		if event.Type != "test.event" {
			t.Errorf("Unexpected event type: %s", event.Type)
		}
		return nil
	}

	ctx := context.Background()
	event := Event{
		ID:   uuid.New(),
		Type: "test.event",
	}

	err := handler(ctx, event)
	if err != nil {
		t.Errorf("Handler returned error: %v", err)
	}
}

func TestRetryDelayCalculation(t *testing.T) {
	baseDelay := time.Second
	
	tests := []struct {
		name         string
		attempt      uint
		expectedDelay time.Duration
	}{
		{"attempt 0", 0, baseDelay * 1},      // 1 << 0 = 1
		{"attempt 1", 1, baseDelay * 2},      // 1 << 1 = 2  
		{"attempt 2", 2, baseDelay * 4},      // 1 << 2 = 4
		{"attempt 3", 3, baseDelay * 8},      // 1 << 3 = 8
		{"attempt 4", 4, baseDelay * 16},     // 1 << 4 = 16
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Simulate the exponential backoff calculation from the code
			delay := baseDelay * time.Duration(1<<tt.attempt)
			
			if delay != tt.expectedDelay {
				t.Errorf("Exponential backoff delay = %v, want %v", delay, tt.expectedDelay)
			}
		})
	}
}