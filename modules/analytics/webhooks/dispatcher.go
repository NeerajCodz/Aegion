package webhooks

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/aegion/aegion/internal/xlog"
)

// Dispatcher handles webhook HTTP delivery.
type Dispatcher struct {
	client *http.Client
	logger *xlog.Logger
}

// NewDispatcher creates a new webhook dispatcher.
func NewDispatcher(timeoutSeconds int, logger *xlog.Logger) *Dispatcher {
	client := &http.Client{
		Timeout: time.Duration(timeoutSeconds) * time.Second,
	}

	return &Dispatcher{
		client: client,
		logger: logger,
	}
}

// DeliveryResult contains the result of a delivery attempt.
type DeliveryResult struct {
	Success    bool
	StatusCode int
	Body       string
	Error      string
	Attempts   int
}

// Deliver sends a webhook to the specified URL.
func (d *Dispatcher) Deliver(ctx context.Context, url string, payload interface{}, headers map[string]string) *DeliveryResult {
	result := &DeliveryResult{Attempts: 1}

	// Marshal payload
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		result.Error = fmt.Sprintf("failed to marshal payload: %v", err)
		d.logger.Error("webhook dispatch error", "url", url, "error", result.Error)
		return result
	}

	// Create request
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payloadBytes))
	if err != nil {
		result.Error = fmt.Sprintf("failed to create request: %v", err)
		d.logger.Error("webhook dispatch error", "url", url, "error", result.Error)
		return result
	}

	// Add headers
	req.Header.Set("Content-Type", "application/json")
	for key, value := range headers {
		req.Header.Set(key, value)
	}

	// Send request
	resp, err := d.client.Do(req)
	if err != nil {
		result.Error = fmt.Sprintf("request failed: %v", err)
		d.logger.Warn("webhook delivery failed", "url", url, "error", result.Error)
		return result
	}
	defer func() { _ = resp.Body.Close() }()

	result.StatusCode = resp.StatusCode

	// Read response body (limit to 1KB)
	bodyBytes, err := io.ReadAll(io.LimitReader(resp.Body, 1024))
	if err != nil {
		result.Error = fmt.Sprintf("failed to read response body: %v", err)
		d.logger.Error("webhook dispatch error", "url", url, "error", result.Error)
		return result
	}

	result.Body = string(bodyBytes)

	// Determine success
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		result.Success = true
		d.logger.Debug("webhook delivered successfully", "url", url, "status_code", resp.StatusCode)
	} else {
		result.Error = fmt.Sprintf("received status code %d", resp.StatusCode)
		d.logger.Warn("webhook delivery status error", "url", url, "status_code", resp.StatusCode)
	}

	return result
}

// IsRetryableError determines if an error should be retried.
func (d *Dispatcher) IsRetryableError(statusCode int, err error) bool {
	if err != nil {
		return true // Network errors are retryable
	}

	// 5xx errors are retryable
	if statusCode >= 500 {
		return true
	}

	// 4xx errors are not retryable (except 408, 429)
	if statusCode >= 400 && statusCode < 500 {
		return statusCode == 408 || statusCode == 429
	}

	return false
}
