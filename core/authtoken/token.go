// Package authtoken provides internal module-to-module authentication tokens.
package authtoken

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

var (
	ErrInvalidToken   = errors.New("invalid token")
	ErrExpiredToken   = errors.New("token expired")
	ErrInvalidSecret  = errors.New("invalid secret")
	ErrModuleIDEmpty  = errors.New("module ID cannot be empty")
)

const (
	DefaultTTL        = 5 * time.Minute
	TokenSeparator    = "."
	SignatureLength   = 32 // SHA256 produces 32 bytes
)

// Token represents a decoded internal auth token.
type Token struct {
	ModuleID  string
	Timestamp time.Time
	Signature []byte
}

// Generator creates and validates internal auth tokens.
type Generator struct {
	secrets    [][]byte
	ttl        time.Duration
	mu         sync.RWMutex
}

// GeneratorConfig holds token generator configuration.
type GeneratorConfig struct {
	// Secret is the primary signing secret
	Secret []byte
	// TTL is the token validity duration (default: 5 minutes)
	TTL time.Duration
	// PreviousSecrets are accepted during rotation (oldest first)
	PreviousSecrets [][]byte
}

// NewGenerator creates a new token generator.
func NewGenerator(cfg GeneratorConfig) (*Generator, error) {
	if len(cfg.Secret) == 0 {
		return nil, ErrInvalidSecret
	}

	ttl := cfg.TTL
	if ttl == 0 {
		ttl = DefaultTTL
	}

	// Build secrets list: primary first, then previous secrets
	secrets := make([][]byte, 0, 1+len(cfg.PreviousSecrets))
	secrets = append(secrets, cfg.Secret)
	secrets = append(secrets, cfg.PreviousSecrets...)

	return &Generator{
		secrets: secrets,
		ttl:     ttl,
	}, nil
}

// Generate creates a new signed token for the given module ID.
func (g *Generator) Generate(moduleID string) (string, error) {
	if moduleID == "" {
		return "", ErrModuleIDEmpty
	}

	g.mu.RLock()
	secret := g.secrets[0] // Always sign with primary secret
	g.mu.RUnlock()

	timestamp := time.Now().UTC()
	payload := buildPayload(moduleID, timestamp)
	signature := sign(payload, secret)

	// Encode: base64(moduleID).base64(timestamp).base64(signature)
	token := fmt.Sprintf("%s%s%s%s%s",
		base64.RawURLEncoding.EncodeToString([]byte(moduleID)),
		TokenSeparator,
		base64.RawURLEncoding.EncodeToString([]byte(timestamp.Format(time.RFC3339Nano))),
		TokenSeparator,
		base64.RawURLEncoding.EncodeToString(signature),
	)

	return token, nil
}

// Validate validates a token and returns the decoded token data.
func (g *Generator) Validate(tokenStr string) (*Token, error) {
	parts := strings.Split(tokenStr, TokenSeparator)
	if len(parts) != 3 {
		return nil, ErrInvalidToken
	}

	// Decode module ID
	moduleIDBytes, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, ErrInvalidToken
	}
	moduleID := string(moduleIDBytes)

	// Decode timestamp
	timestampBytes, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, ErrInvalidToken
	}
	timestamp, err := time.Parse(time.RFC3339Nano, string(timestampBytes))
	if err != nil {
		return nil, ErrInvalidToken
	}

	// Decode signature
	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return nil, ErrInvalidToken
	}

	// Check expiration
	if time.Since(timestamp) > g.ttl {
		return nil, ErrExpiredToken
	}

	// Verify signature against all secrets (supports rotation)
	payload := buildPayload(moduleID, timestamp)
	
	g.mu.RLock()
	secrets := g.secrets
	g.mu.RUnlock()

	valid := false
	for _, secret := range secrets {
		expectedSig := sign(payload, secret)
		if hmac.Equal(signature, expectedSig) {
			valid = true
			break
		}
	}

	if !valid {
		return nil, ErrInvalidToken
	}

	return &Token{
		ModuleID:  moduleID,
		Timestamp: timestamp,
		Signature: signature,
	}, nil
}

// ValidateString is a convenience method that returns module ID or error.
func (g *Generator) ValidateString(tokenStr string) (string, error) {
	token, err := g.Validate(tokenStr)
	if err != nil {
		return "", err
	}
	return token.ModuleID, nil
}

// SetSecrets updates the secrets for rotation support.
// The first secret is primary, subsequent are accepted during grace period.
func (g *Generator) SetSecrets(primary []byte, previous ...[]byte) error {
	if len(primary) == 0 {
		return ErrInvalidSecret
	}

	secrets := make([][]byte, 0, 1+len(previous))
	secrets = append(secrets, primary)
	secrets = append(secrets, previous...)

	g.mu.Lock()
	g.secrets = secrets
	g.mu.Unlock()

	return nil
}

// GetTTL returns the token TTL.
func (g *Generator) GetTTL() time.Duration {
	return g.ttl
}

// buildPayload creates the payload to be signed.
func buildPayload(moduleID string, timestamp time.Time) []byte {
	return []byte(fmt.Sprintf("%s:%s", moduleID, timestamp.Format(time.RFC3339Nano)))
}

// sign creates an HMAC-SHA256 signature.
func sign(payload, secret []byte) []byte {
	h := hmac.New(sha256.New, secret)
	h.Write(payload)
	return h.Sum(nil)
}
