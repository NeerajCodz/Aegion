package webhooks

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"time"
)

// Signature handles HMAC-SHA256 signing for webhooks.
type Signature struct {
	algorithm string
}

// NewSignature creates a new Signature instance.
func NewSignature() *Signature {
	return &Signature{
		algorithm: "sha256",
	}
}

// Sign generates an HMAC-SHA256 signature for the payload.
func (s *Signature) Sign(secret string, payload interface{}) (string, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("failed to marshal payload: %w", err)
	}

	h := hmac.New(sha256.New, []byte(secret))
	h.Write(data)
	signature := h.Sum(nil)

	return fmt.Sprintf("sha256=%s", fmt.Sprintf("%x", signature)), nil
}

// Verify checks if the signature is valid for the payload.
func (s *Signature) Verify(secret string, payload interface{}, signature string) bool {
	expected, err := s.Sign(secret, payload)
	if err != nil {
		return false
	}

	return hmac.Equal([]byte(expected), []byte(signature))
}

// WebhookPayloadWithSignature wraps a payload with signing headers.
type WebhookPayloadWithSignature struct {
	Payload   interface{}            `json:"payload"`
	Headers   map[string]string      `json:"headers"`
	Signature string                 `json:"-"` // Not included in payload JSON
	Timestamp time.Time              `json:"timestamp"`
}

// SignPayload creates a signed payload with all necessary headers.
func SignPayload(eventID string, eventType string, category string, data map[string]interface{}, secret string) (interface{}, map[string]string, error) {
	payload := map[string]interface{}{
		"id":        eventID,
		"timestamp": time.Now().UTC().Format(time.RFC3339),
		"event_type": eventType,
		"category":  category,
		"data":      data,
		"attempts":  1,
		"signatures": map[string]string{},
	}

	signer := NewSignature()
	sig, err := signer.Sign(secret, payload)
	if err != nil {
		return nil, nil, err
	}

	payload["signatures"] = map[string]string{
		"sha256": sig,
	}

	headers := map[string]string{
		"X-Aegion-Event-ID":     eventID,
		"X-Aegion-Event-Type":   eventType,
		"X-Aegion-Timestamp":    time.Now().UTC().Format(time.RFC3339),
		"X-Aegion-Signature":    sig,
		"Content-Type":          "application/json",
	}

	return payload, headers, nil
}
