package webhooks

import "fmt"

// WebhookError represents a webhook system error.
type WebhookError struct {
	Code    string
	Message string
}

func (e *WebhookError) Error() string {
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

// Common error types
var (
	ErrInvalidURL         = &WebhookError{Code: "INVALID_URL", Message: "webhook URL must be HTTPS or http for localhost"}
	ErrInvalidEventTypes  = &WebhookError{Code: "INVALID_EVENT_TYPES", Message: "event_types must not be empty"}
	ErrMaxWebhooksReached = &WebhookError{Code: "MAX_WEBHOOKS_REACHED", Message: "user has reached maximum webhook limit"}
	ErrWebhookDisabled    = &WebhookError{Code: "WEBHOOK_DISABLED", Message: "webhook has been disabled due to repeated failures"}
	ErrInvalidFilter      = &WebhookError{Code: "INVALID_FILTER", Message: "invalid event filter configuration"}
	ErrRateLimited        = &WebhookError{Code: "RATE_LIMITED", Message: "rate limit exceeded for webhook operations"}
	ErrDeliveryQueueFull  = &WebhookError{Code: "DELIVERY_QUEUE_FULL", Message: "delivery queue is full, please try again later"}
)

// ValidateWebhookRequest validates a webhook registration request.
func ValidateWebhookRequest(req *WebhookRequest, maxFiltersDepth int) error {
	if req == nil {
		return fmt.Errorf("webhook request cannot be nil")
	}

	// Validate URL
	if req.URL == "" {
		return fmt.Errorf("URL is required")
	}

	// URL must be HTTPS except for localhost
	isLocalhost := req.URL == "http://localhost" ||
		(len(req.URL) > 17 && req.URL[:17] == "http://localhost:")

	if !isLocalhost && len(req.URL) > 7 && req.URL[:7] != "https://" {
		return ErrInvalidURL
	}

	// Validate event filter
	if len(req.EventFilter.EventTypes) == 0 {
		return ErrInvalidEventTypes
	}

	// Validate custom filter if present
	if req.EventFilter.CustomFilter != nil && len(req.EventFilter.CustomFilter) > 0 {
		if !validateFilterDepth(req.EventFilter.CustomFilter, maxFiltersDepth, 0) {
			return ErrInvalidFilter
		}
	}

	return nil
}

// validateFilterDepth checks the depth of nested filters.
func validateFilterDepth(filter map[string]interface{}, maxDepth, currentDepth int) bool {
	if currentDepth > maxDepth {
		return false
	}

	for key, value := range filter {
		if key == "$and" || key == "$or" {
			if conditions, ok := value.([]interface{}); ok {
				for _, cond := range conditions {
					if condMap, ok := cond.(map[string]interface{}); ok {
						if !validateFilterDepth(condMap, maxDepth, currentDepth+1) {
							return false
						}
					}
				}
			}
		} else if key == "$not" {
			if condMap, ok := value.(map[string]interface{}); ok {
				if !validateFilterDepth(condMap, maxDepth, currentDepth+1) {
					return false
				}
			}
		}
	}

	return true
}
