package store

import (
	"context"
	"sync"
	"time"
)

// AuditEventType represents the type of audit event
type AuditEventType string

const (
	AuditEventQuery        AuditEventType = "query"
	AuditEventExport       AuditEventType = "export"
	AuditEventDashboard    AuditEventType = "dashboard"
	AuditEventWebhook      AuditEventType = "webhook"
	AuditEventAuth         AuditEventType = "auth"
	AuditEventConfigChange AuditEventType = "config_change"
	AuditEventAccessDenied AuditEventType = "access_denied"
	AuditEventDelete       AuditEventType = "delete"
)

// AuditEvent represents an immutable audit log entry
type AuditEvent struct {
	ID           string
	Timestamp    time.Time
	UserID       string
	EventType    AuditEventType
	ResourceID   string
	ResourceType string
	Action       string
	Status       string // success, failure
	ErrorMsg     string
	Details      map[string]interface{}
	IPAddress    string
	UserAgent    string
}

// AuditStore stores immutable audit events
type AuditStore struct {
	mu     sync.RWMutex
	events []AuditEvent
}

// NewAuditStore creates a new audit store
func NewAuditStore() *AuditStore {
	return &AuditStore{
		events: []AuditEvent{},
	}
}

// LogEvent records an audit event
func (as *AuditStore) LogEvent(ctx context.Context, event AuditEvent) error {
	as.mu.Lock()
	defer as.mu.Unlock()

	// Events are immutable - append only
	as.events = append(as.events, event)

	return nil
}

// GetEvents retrieves audit events with filtering
func (as *AuditStore) GetEvents(ctx context.Context, filters map[string]interface{}, limit int) ([]AuditEvent, error) {
	as.mu.RLock()
	defer as.mu.RUnlock()

	results := []AuditEvent{}
	count := 0

	// Iterate in reverse to get most recent events first
	for i := len(as.events) - 1; i >= 0 && count < limit; i-- {
		event := as.events[i]

		// Apply filters
		if !matchFilters(event, filters) {
			continue
		}

		results = append(results, event)
		count++
	}

	return results, nil
}

// GetEventsByUser retrieves events for a specific user
func (as *AuditStore) GetEventsByUser(ctx context.Context, userID string, limit int) ([]AuditEvent, error) {
	return as.GetEvents(ctx, map[string]interface{}{
		"user_id": userID,
	}, limit)
}

// GetEventsByType retrieves events of a specific type
func (as *AuditStore) GetEventsByType(ctx context.Context, eventType AuditEventType, limit int) ([]AuditEvent, error) {
	return as.GetEvents(ctx, map[string]interface{}{
		"event_type": eventType,
	}, limit)
}

// GetEventsByResource retrieves events for a specific resource
func (as *AuditStore) GetEventsByResource(ctx context.Context, resourceID string, limit int) ([]AuditEvent, error) {
	return as.GetEvents(ctx, map[string]interface{}{
		"resource_id": resourceID,
	}, limit)
}

// CountEvents returns the total number of events
func (as *AuditStore) CountEvents(ctx context.Context) int {
	as.mu.RLock()
	defer as.mu.RUnlock()

	return len(as.events)
}

// matchFilters checks if an event matches the given filters
func matchFilters(event AuditEvent, filters map[string]interface{}) bool {
	for key, value := range filters {
		switch key {
		case "user_id":
			if userID, ok := value.(string); ok && event.UserID != userID {
				return false
			}
		case "event_type":
			if eventType, ok := value.(AuditEventType); ok && event.EventType != eventType {
				return false
			}
		case "resource_id":
			if resourceID, ok := value.(string); ok && event.ResourceID != resourceID {
				return false
			}
		case "status":
			if status, ok := value.(string); ok && event.Status != status {
				return false
			}
		}
	}
	return true
}
