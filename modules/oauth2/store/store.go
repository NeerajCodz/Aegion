// Package store provides database operations for the OAuth2 module.
package store

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	platformcrypto "github.com/aegion/aegion/internal/platform/crypto"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Common errors
var (
	ErrNotFound           = errors.New("not found")
	ErrAlreadyExists      = errors.New("already exists")
	ErrCodeExpired        = errors.New("code expired")
	ErrCodeUsed           = errors.New("code already used")
	ErrTokenRevoked       = errors.New("token revoked")
	ErrTokenExpired       = errors.New("token expired")
	ErrTokenInactive      = errors.New("token inactive")
	ErrFamilyInvalidated  = errors.New("token family invalidated")
	ErrPKCERequired       = errors.New("PKCE required")
	ErrPKCEMismatch       = errors.New("PKCE verification failed")
	ErrInvalidRedirectURI = errors.New("invalid redirect URI")
	ErrInvalidGrant       = errors.New("invalid grant type")
	ErrInvalidScope       = errors.New("invalid scope")
)

// DB interface defines methods needed for database operations
type DB interface {
	Exec(ctx context.Context, sql string, arguments ...interface{}) (pgconn.CommandTag, error)
	QueryRow(ctx context.Context, sql string, optionsAndArgs ...interface{}) pgx.Row
	Query(ctx context.Context, sql string, optionsAndArgs ...interface{}) (pgx.Rows, error)
}

// Store handles OAuth2 persistence.
type Store struct {
	db DB
}

// New creates a new OAuth2 store.
func New(db *pgxpool.Pool) *Store {
	return &Store{db: db}
}

// NewWithDB creates a new OAuth2 store with a custom DB interface (primarily for testing).
func NewWithDB(db DB) *Store {
	return &Store{db: db}
}

// generateID generates a cryptographically secure random ID
func generateID(prefix string, length int) (string, error) {
	b, err := platformcrypto.RandomBytes(length)
	if err != nil {
		return "", fmt.Errorf("failed to generate random bytes: %w", err)
	}
	return prefix + base64.RawURLEncoding.EncodeToString(b), nil
}

// GenerateClientID generates a new client ID
func GenerateClientID() (string, error) {
	return generateID("oa2_", 24)
}

// GenerateAuthCode generates a new authorization code
func GenerateAuthCode() (string, error) {
	return generateID("", 32)
}

// GenerateAccessTokenJTI generates a new access token JTI
func GenerateAccessTokenJTI() (string, error) {
	return generateID("at_", 32)
}

// GenerateRefreshToken generates a new refresh token ID
func GenerateRefreshToken() (string, error) {
	return generateID("rt_", 32)
}

// GenerateRefreshTokenFamily generates a new refresh token family ID
func GenerateRefreshTokenFamily() (string, error) {
	return generateID("rtf_", 16)
}

// GenerateIDTokenJTI generates a new ID token JTI
func GenerateIDTokenJTI() (string, error) {
	return generateID("idt_", 32)
}

// GenerateLoginChallenge generates a new login challenge ID
func GenerateLoginChallenge() (string, error) {
	return generateID("lc_", 24)
}

// GenerateConsentChallenge generates a new consent challenge ID
func GenerateConsentChallenge() (string, error) {
	return generateID("cc_", 24)
}

// GenerateDeviceCode generates a new device code
func GenerateDeviceCode() (string, error) {
	return generateID("dc_", 32)
}

// GenerateUserCode generates a human-readable user code (8 chars, uppercase)
func GenerateUserCode() (string, error) {
	const charset = "BCDFGHJKLMNPQRSTVWXZ" // no vowels to avoid words
	b, err := platformcrypto.RandomBytes(8)
	if err != nil {
		return "", fmt.Errorf("failed to generate random bytes: %w", err)
	}
	code := make([]byte, 8)
	for i := range code {
		code[i] = charset[int(b[i])%len(charset)]
	}
	// Format as XXXX-XXXX
	return string(code[:4]) + "-" + string(code[4:]), nil
}

// isDuplicateKeyError checks if error is a duplicate key violation.
func isDuplicateKeyError(err error) bool {
	if err == nil {
		return false
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "23505" // unique_violation
	}
	return false
}

// nowUTC returns the current time in UTC
func nowUTC() time.Time {
	return time.Now().UTC()
}
