// Package moduleauth issues and validates short-lived, module-scoped control
// plane tokens from durable, revocable module credentials.
package moduleauth

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	platformcrypto "github.com/aegion/aegion/internal/platform/crypto"
)

var (
	ErrCredentialInvalid = errors.New("module credential is invalid")
	ErrCredentialRevoked = errors.New("module credential is revoked")
	ErrTokenInvalid      = errors.New("module token is invalid")
	ErrTokenExpired      = errors.New("module token is expired")
	ErrTokenDenied       = errors.New("module token does not grant the requested access")
)

const tokenVersion = 2

// Credential is durable state for one module bootstrap identity. The raw
// credential exists only when created or rotated; persistence holds its Argon2id
// hash.
type Credential struct {
	ID          string
	ModuleID    string
	SecretHash  string
	Permissions []string
	Audiences   []string
	Enabled     bool
	ExpiresAt   *time.Time
}

// Store persists one revocable bootstrap credential per module.
type Store interface {
	Credential(ctx context.Context, moduleID string) (Credential, error)
}

// Manager exchanges module credentials for short-lived scoped tokens and
// verifies every token against durable credential state to make revocation
// immediate.
type Manager struct {
	store Store
	key   []byte
	ttl   time.Duration
	now   func() time.Time
}

// NewManager constructs a manager using the core-only signing key.
func NewManager(store Store, signingKey []byte, ttl time.Duration) (*Manager, error) {
	if store == nil {
		return nil, errors.New("module credential store is required")
	}
	if len(signingKey) == 0 {
		return nil, errors.New("module token signing key is required")
	}
	if ttl <= 0 {
		ttl = 5 * time.Minute
	}
	return &Manager{
		store: store,
		key:   slices.Clone(signingKey),
		ttl:   ttl,
		now:   time.Now,
	}, nil
}

// Exchange validates a raw bootstrap credential and issues only the requested
// audience and permission subset.
func (m *Manager) Exchange(ctx context.Context, moduleID, rawCredential, audience string, permissions []string) (string, Claims, error) {
	moduleID = strings.TrimSpace(moduleID)
	audience = strings.TrimSpace(audience)
	if moduleID == "" || rawCredential == "" || audience == "" {
		return "", Claims{}, ErrCredentialInvalid
	}
	credential, err := m.usableCredential(ctx, moduleID)
	if err != nil {
		return "", Claims{}, err
	}
	valid, err := platformcrypto.VerifyPassword(rawCredential, credential.SecretHash)
	if err != nil || !valid {
		return "", Claims{}, ErrCredentialInvalid
	}
	if len(permissions) == 0 {
		permissions = credential.Permissions
	}
	if !contains(credential.Audiences, audience) || !subset(permissions, credential.Permissions) {
		return "", Claims{}, ErrTokenDenied
	}
	return m.issue(credential, audience, permissions)
}

// Validate verifies token integrity, expiry, audience, permission, and the
// current durable credential state. A disabled or rotated credential invalidates
// already-issued tokens immediately.
func (m *Manager) Validate(ctx context.Context, token, audience, permission string) (Claims, error) {
	claims, err := m.parse(token)
	if err != nil {
		return Claims{}, err
	}
	if claims.Audience != strings.TrimSpace(audience) || !contains(claims.Permissions, permission) {
		return Claims{}, ErrTokenDenied
	}
	credential, err := m.usableCredential(ctx, claims.ModuleID)
	if err != nil {
		return Claims{}, err
	}
	if credential.ID != claims.CredentialID || !contains(credential.Audiences, claims.Audience) || !subset(claims.Permissions, credential.Permissions) {
		return Claims{}, ErrTokenDenied
	}
	return claims, nil
}

func (m *Manager) usableCredential(ctx context.Context, moduleID string) (Credential, error) {
	credential, err := m.store.Credential(ctx, moduleID)
	if err != nil {
		return Credential{}, fmt.Errorf("load module credential: %w", err)
	}
	if !credential.Enabled {
		return Credential{}, ErrCredentialRevoked
	}
	if credential.ExpiresAt != nil && !m.now().UTC().Before(credential.ExpiresAt.UTC()) {
		return Credential{}, ErrCredentialRevoked
	}
	return credential, nil
}

func (m *Manager) issue(credential Credential, audience string, permissions []string) (string, Claims, error) {
	issuedAt := m.now().UTC()
	tokenID, err := randomValue(16)
	if err != nil {
		return "", Claims{}, err
	}
	claims := Claims{
		Version:      tokenVersion,
		TokenID:      tokenID,
		CredentialID: credential.ID,
		ModuleID:     credential.ModuleID,
		Audience:     audience,
		Permissions:  slices.Clone(permissions),
		IssuedAt:     issuedAt.Unix(),
		ExpiresAt:    issuedAt.Add(m.ttl).Unix(),
	}
	payload, err := json.Marshal(claims)
	if err != nil {
		return "", Claims{}, fmt.Errorf("encode module token claims: %w", err)
	}
	signature, err := m.signature(payload)
	if err != nil {
		return "", Claims{}, err
	}
	return "v2." + base64.RawURLEncoding.EncodeToString(payload) + "." + base64.RawURLEncoding.EncodeToString(signature), claims, nil
}

func (m *Manager) parse(token string) (Claims, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 || parts[0] != "v2" {
		return Claims{}, ErrTokenInvalid
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return Claims{}, ErrTokenInvalid
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return Claims{}, ErrTokenInvalid
	}
	expected, err := m.signature(payload)
	if err != nil || !platformcrypto.ConstantTimeCompare(signature, expected) {
		return Claims{}, ErrTokenInvalid
	}
	var claims Claims
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&claims); err != nil || claims.Version != tokenVersion || claims.TokenID == "" || claims.CredentialID == "" || claims.ModuleID == "" || claims.Audience == "" || len(claims.Permissions) == 0 {
		return Claims{}, ErrTokenInvalid
	}
	now := m.now().UTC().Unix()
	if claims.ExpiresAt <= now {
		return Claims{}, ErrTokenExpired
	}
	if claims.IssuedAt > now+int64(time.Minute/time.Second) {
		return Claims{}, ErrTokenInvalid
	}
	return claims, nil
}

func (m *Manager) signature(payload []byte) ([]byte, error) {
	hexSignature, err := platformcrypto.HMACSHA256Hex(m.key, payload)
	if err != nil {
		return nil, err
	}
	signature, err := hex.DecodeString(hexSignature)
	if err != nil {
		return nil, err
	}
	return signature, nil
}

// Claims are the intentionally narrow authorization facts carried by a
type Claims struct {
	Version      int      `json:"v"`
	TokenID      string   `json:"jti"`
	CredentialID string   `json:"credential_id"`
	ModuleID     string   `json:"module_id"`
	Audience     string   `json:"aud"`
	Permissions  []string `json:"permissions"`
	IssuedAt     int64    `json:"iat"`
	ExpiresAt    int64    `json:"exp"`
}

// NewCredential creates a URL-safe bootstrap credential and its Argon2id hash.
func NewCredential() (raw string, hash string, err error) {
	raw, err = randomValue(32)
	if err != nil {
		return "", "", err
	}
	hash, err = platformcrypto.HashPassword(raw)
	if err != nil {
		return "", "", err
	}
	return raw, hash, nil
}

func randomValue(size int) (string, error) {
	value, err := platformcrypto.RandomBytes(size)
	if err != nil {
		return "", fmt.Errorf("generate random credential value: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(value), nil
}

func contains(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func subset(values, allowed []string) bool {
	for _, value := range values {
		if !contains(allowed, value) {
			return false
		}
	}
	return true
}
