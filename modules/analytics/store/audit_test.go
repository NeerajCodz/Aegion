package store

import (
	"context"
	"testing"
	"time"
)

func TestNewAuditStore(t *testing.T) {
	as := NewAuditStore()
	if as == nil {
		t.Error("NewAuditStore returned nil")
	}
}

func TestLogEvent(t *testing.T) {
	as := NewAuditStore()
	ctx := context.Background()

	event := AuditEvent{
		ID:        "event1",
		Timestamp: time.Now(),
		UserID:    "user1",
		EventType: AuditEventQuery,
		Action:    "SELECT * FROM events",
		Status:    "success",
	}

	err := as.LogEvent(ctx, event)
	if err != nil {
		t.Fatalf("LogEvent failed: %v", err)
	}

	// Verify event was stored
	count := as.CountEvents(ctx)
	if count != 1 {
		t.Errorf("Expected 1 event, got %d", count)
	}
}

func TestGetEvents(t *testing.T) {
	as := NewAuditStore()
	ctx := context.Background()

	// Add multiple events
	for i := 0; i < 5; i++ {
		event := AuditEvent{
			ID:        string(rune(48 + i)), // "0" to "4"
			Timestamp: time.Now(),
			UserID:    "user1",
			EventType: AuditEventQuery,
			Status:    "success",
		}
		as.LogEvent(ctx, event)
	}

	// Get all events
	events, err := as.GetEvents(ctx, nil, 10)
	if err != nil {
		t.Fatalf("GetEvents failed: %v", err)
	}

	if len(events) != 5 {
		t.Errorf("Expected 5 events, got %d", len(events))
	}
}

func TestGetEventsByUser(t *testing.T) {
	as := NewAuditStore()
	ctx := context.Background()

	// Add events for different users
	as.LogEvent(ctx, AuditEvent{
		ID:     "event1",
		UserID: "user1",
		Status: "success",
	})
	as.LogEvent(ctx, AuditEvent{
		ID:     "event2",
		UserID: "user2",
		Status: "success",
	})
	as.LogEvent(ctx, AuditEvent{
		ID:     "event3",
		UserID: "user1",
		Status: "success",
	})

	events, _ := as.GetEventsByUser(ctx, "user1", 10)
	if len(events) != 2 {
		t.Errorf("Expected 2 events for user1, got %d", len(events))
	}
}

func TestGetEventsByType(t *testing.T) {
	as := NewAuditStore()
	ctx := context.Background()

	// Add events of different types
	as.LogEvent(ctx, AuditEvent{
		ID:        "event1",
		EventType: AuditEventQuery,
		Status:    "success",
	})
	as.LogEvent(ctx, AuditEvent{
		ID:        "event2",
		EventType: AuditEventExport,
		Status:    "success",
	})
	as.LogEvent(ctx, AuditEvent{
		ID:        "event3",
		EventType: AuditEventQuery,
		Status:    "success",
	})

	events, _ := as.GetEventsByType(ctx, AuditEventQuery, 10)
	if len(events) != 2 {
		t.Errorf("Expected 2 query events, got %d", len(events))
	}
}

func TestGetEventsByResource(t *testing.T) {
	as := NewAuditStore()
	ctx := context.Background()

	// Add events for different resources
	as.LogEvent(ctx, AuditEvent{
		ID:         "event1",
		ResourceID: "dash1",
		Status:     "success",
	})
	as.LogEvent(ctx, AuditEvent{
		ID:         "event2",
		ResourceID: "dash2",
		Status:     "success",
	})
	as.LogEvent(ctx, AuditEvent{
		ID:         "event3",
		ResourceID: "dash1",
		Status:     "success",
	})

	events, _ := as.GetEventsByResource(ctx, "dash1", 10)
	if len(events) != 2 {
		t.Errorf("Expected 2 events for dash1, got %d", len(events))
	}
}

func TestCountEvents(t *testing.T) {
	as := NewAuditStore()
	ctx := context.Background()

	if as.CountEvents(ctx) != 0 {
		t.Error("New store should have 0 events")
	}

	as.LogEvent(ctx, AuditEvent{
		ID:     "event1",
		Status: "success",
	})

	if as.CountEvents(ctx) != 1 {
		t.Error("Store should have 1 event")
	}
}

func TestAuditEventImmutability(t *testing.T) {
	as := NewAuditStore()
	ctx := context.Background()

	// Add an event
	as.LogEvent(ctx, AuditEvent{
		ID:     "event1",
		Status: "success",
	})

	// Get it back
	events, _ := as.GetEvents(ctx, nil, 10)
	if len(events) != 1 {
		t.Error("Should have event")
	}

	// Events are appended, not replaced
	as.LogEvent(ctx, AuditEvent{
		ID:     "event2",
		Status: "success",
	})

	events, _ = as.GetEvents(ctx, nil, 10)
	if len(events) != 2 {
		t.Error("Should have 2 events (immutable append-only)")
	}
}

func TestGetEventsLimit(t *testing.T) {
	as := NewAuditStore()
	ctx := context.Background()

	// Add 10 events
	for i := 0; i < 10; i++ {
		as.LogEvent(ctx, AuditEvent{
			ID:     "event" + string(rune(48+i)),
			Status: "success",
		})
	}

	// Request only 5
	events, _ := as.GetEvents(ctx, nil, 5)
	if len(events) != 5 {
		t.Errorf("Expected 5 events with limit, got %d", len(events))
	}
}
