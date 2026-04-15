// Package authtoken provides internal module-to-module authentication tokens.
package authtoken

import (
	"encoding/base64"
	"encoding/hex"
	"errors"
	"strconv"
	"sync"
	"time"

	platformcrypto "github.com/aegion/aegion/internal/platform/crypto"
)

var (
	ErrInvalidToken  = errors.New("invalid token")
	ErrExpiredToken  = errors.New("token expired")
	ErrInvalidSecret = errors.New("invalid secret")
	ErrModuleIDEmpty = errors.New("module ID cannot be empty")
)

const (
	DefaultTTL      = 5 * time.Minute
	TokenSeparator  = "."
	SignatureLength = 32 // SHA256 produces 32 bytes
)

// Token represents a decoded internal auth token.
type Token struct {
	ModuleID  string
	Timestamp time.Time
	Signature []byte
}

// buildPayload creates the canonical payload for internal-token signatures.
func buildPayload(moduleID string, timestamp time.Time) []byte {
	encodedModuleID := base64.RawURLEncoding.EncodeToString([]byte(moduleID))
	return []byte("internal_token\nv1\n" + strconv.FormatInt(timestamp.UTC().UnixMilli(), 10) + "\n" + encodedModuleID)
}

// sign creates an HMAC-SHA256 signature through the Rust-backed crypto layer.
func sign(payload, secret []byte) []byte {
	signature, err := platformcrypto.HMACSHA256Hex(secret, payload)
	if err != nil {
		return nil
	}
	decoded, err := hex.DecodeString(signature)
	if err != nil {
		return nil
	}
	return decoded
}

// Generator creates and validates internal auth tokens.
type Generator struct {
	secrets [][]byte
	ttl     time.Duration
	mu      sync.RWMutex
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
	return platformcrypto.GenerateInternalToken(secret, moduleID, timestamp)
}

// Validate validates a token and returns the decoded token data.
func (g *Generator) Validate(tokenStr string) (*Token, error) {
	g.mu.RLock()
	secrets := g.secrets
	g.mu.RUnlock()

	for _, secret := range secrets {
		parsed, err := platformcrypto.VerifyInternalToken(secret, tokenStr, g.ttl, time.Now().UTC())
		if err == nil {
			return &Token{
				ModuleID:  parsed.ModuleID,
				Timestamp: parsed.Timestamp,
				Signature: parsed.Signature,
			}, nil
		}
		if errors.Is(err, platformcrypto.ErrExpired) {
			return nil, ErrExpiredToken
		}
		if errors.Is(err, platformcrypto.ErrInternalToken) || errors.Is(err, platformcrypto.ErrInvalidSignature) {
			continue
		}
	}
	return nil, ErrInvalidToken
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
