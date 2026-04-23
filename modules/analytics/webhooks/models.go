package webhooks

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"
)

// EventFilter defines how a webhook filters events.
type EventFilter struct {
	EventTypes   []string               `json:"event_types"`   // Glob patterns like "auth.*", "user.created"
	Categories   []string               `json:"categories,omitempty"`
	CustomFilter map[string]interface{} `json:"custom_filter,omitempty"`
}

// WebhookRequest is used for registration/updates via API.
type WebhookRequest struct {
	URL          string                 `json:"url"`
	EventFilter  EventFilter            `json:"event_filter"`
	Secret       string                 `json:"secret,omitempty"`
	Active       bool                   `json:"active"`
	CustomFilter map[string]interface{} `json:"custom_filter,omitempty"`
}

// WebhookResponse is returned from API endpoints.
type WebhookResponse struct {
	ID             string      `json:"id"`
	URL            string      `json:"url"`
	EventFilter    EventFilter `json:"event_filter"`
	Active         bool        `json:"active"`
	FailureCount   int         `json:"failure_count"`
	LastDeliveryAt *time.Time  `json:"last_delivery_at,omitempty"`
	CreatedAt      time.Time   `json:"created_at"`
	UpdatedAt      time.Time   `json:"updated_at"`
}

// DeliveryRequest wraps event data for delivery.
type DeliveryRequest struct {
	WebhookID string
	EventID   string
	Payload   interface{}
}

// EventInfo is information about the event being delivered.
type EventInfo struct {
	ID       string
	Type     string
	Category string
	Data     map[string]interface{}
}

// SignatureConfig holds signature configuration.
type SignatureConfig struct {
	Algorithm string // "sha256"
	Secret    string
}

// MatcherConfig provides configuration for event matching.
type MatcherConfig struct {
	MaxCustomFilterDepth int // Maximum nesting depth for custom filters
}
