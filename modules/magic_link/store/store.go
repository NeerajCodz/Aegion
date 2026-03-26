// Package store provides database operations for the magic link module.
package store

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"math/big"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrCodeNotFound = errors.New("code not found")
	ErrCodeExpired  = errors.New("code expired")
	ErrCodeUsed     = errors.New("code already used")
	ErrRateLimited  = errors.New("rate limit exceeded")
)

// CodeType represents the type of magic link/OTP code.
type CodeType string

const (
	CodeTypeLogin        CodeType = "login"
	CodeTypeVerification CodeType = "verification"
	CodeTypeRecovery     CodeType = "recovery"
)

// Code represents a magic link/OTP code.
type Code struct {
	ID         uuid.UUID
	IdentityID *uuid.UUID
	Recipient  string
	Type       CodeType
	Code       string // 6-digit OTP
	Token      string // Magic link token
	Used       bool
	UsedAt     *time.Time
	ExpiresAt  time.Time
	CreatedAt  time.Time
}

// Store handles magic link/OTP persistence.
type Store struct {
	db          *pgxpool.Pool
	codeLength  int
	codeCharset string
}

// New creates a new magic link store.
func New(db *pgxpool.Pool) *Store {
	return &Store{
		db:          db,
		codeLength:  6,
		codeCharset: "0123456789",
	}
}

// SetCodeConfig sets the OTP code configuration.
func (s *Store) SetCodeConfig(length int, charset string) {
	s.codeLength = length
	s.codeCharset = charset
}

// Create creates a new magic link/OTP code.
func (s *Store) Create(ctx context.Context, recipient string, codeType CodeType, identityID *uuid.UUID, ttl time.Duration) (*Code, error) {
	code := &Code{
		ID:         uuid.New(),
		IdentityID: identityID,
		Recipient:  recipient,
		Type:       codeType,
		Code:       s.generateCode(),
		Token:      s.generateToken(),
		Used:       false,
		ExpiresAt:  time.Now().UTC().Add(ttl),
		CreatedAt:  time.Now().UTC(),
	}

	_, err := s.db.Exec(ctx, `
		INSERT INTO ml_codes (id, identity_id, recipient, type, code, token, used, expires_at, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`, code.ID, code.IdentityID, code.Recipient, code.Type, code.Code, code.Token, code.Used, code.ExpiresAt, code.CreatedAt)

	if err != nil {
		return nil, err
	}

	return code, nil
}

// GetByCode retrieves a code by OTP code and recipient.
func (s *Store) GetByCode(ctx context.Context, recipient string, otpCode string, codeType CodeType) (*Code, error) {
	code := &Code{}

	err := s.db.QueryRow(ctx, `
		SELECT id, identity_id, recipient, type, code, token, used, used_at, expires_at, created_at
		FROM ml_codes
		WHERE recipient = $1 AND code = $2 AND type = $3 AND used = FALSE
		ORDER BY created_at DESC
		LIMIT 1
	`, recipient, otpCode, codeType).Scan(
		&code.ID, &code.IdentityID, &code.Recipient, &code.Type,
		&code.Code, &code.Token, &code.Used, &code.UsedAt,
		&code.ExpiresAt, &code.CreatedAt,
	)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrCodeNotFound
		}
		return nil, err
	}

	if time.Now().UTC().After(code.ExpiresAt) {
		return nil, ErrCodeExpired
	}

	return code, nil
}

// GetByToken retrieves a code by magic link token.
func (s *Store) GetByToken(ctx context.Context, token string) (*Code, error) {
	code := &Code{}

	err := s.db.QueryRow(ctx, `
		SELECT id, identity_id, recipient, type, code, token, used, used_at, expires_at, created_at
		FROM ml_codes
		WHERE token = $1 AND used = FALSE
	`, token).Scan(
		&code.ID, &code.IdentityID, &code.Recipient, &code.Type,
		&code.Code, &code.Token, &code.Used, &code.UsedAt,
		&code.ExpiresAt, &code.CreatedAt,
	)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrCodeNotFound
		}
		return nil, err
	}

	if time.Now().UTC().After(code.ExpiresAt) {
		return nil, ErrCodeExpired
	}

	return code, nil
}

// MarkUsed marks a code as used.
func (s *Store) MarkUsed(ctx context.Context, codeID uuid.UUID) error {
	now := time.Now().UTC()
	result, err := s.db.Exec(ctx, `
		UPDATE ml_codes
		SET used = TRUE, used_at = $1
		WHERE id = $2 AND used = FALSE
	`, now, codeID)

	if err != nil {
		return err
	}

	if result.RowsAffected() == 0 {
		return ErrCodeUsed
	}

	return nil
}

// InvalidatePrevious invalidates all previous codes for a recipient and type.
func (s *Store) InvalidatePrevious(ctx context.Context, recipient string, codeType CodeType) error {
	_, err := s.db.Exec(ctx, `
		UPDATE ml_codes
		SET used = TRUE, used_at = NOW()
		WHERE recipient = $1 AND type = $2 AND used = FALSE
	`, recipient, codeType)
	return err
}

// CheckRateLimit checks if a request is rate limited.
func (s *Store) CheckRateLimit(ctx context.Context, key string, limit int, window time.Duration) error {
	now := time.Now().UTC()
	windowEnd := now.Add(window)

	// Try to increment existing counter or insert new one
	var count int
	err := s.db.QueryRow(ctx, `
		INSERT INTO ml_rate_limits (key, count, window_end)
		VALUES ($1, 1, $2)
		ON CONFLICT (key) DO UPDATE
		SET count = CASE
			WHEN ml_rate_limits.window_end < $3 THEN 1
			ELSE ml_rate_limits.count + 1
		END,
		window_end = CASE
			WHEN ml_rate_limits.window_end < $3 THEN $2
			ELSE ml_rate_limits.window_end
		END
		RETURNING count
	`, key, windowEnd, now).Scan(&count)

	if err != nil {
		return err
	}

	if count > limit {
		return ErrRateLimited
	}

	return nil
}

// Cleanup removes expired codes and rate limit entries.
func (s *Store) Cleanup(ctx context.Context) (int64, error) {
	now := time.Now().UTC()

	// Clean up expired codes
	result, err := s.db.Exec(ctx, `
		DELETE FROM ml_codes
		WHERE expires_at < $1 OR (used = TRUE AND used_at < $2)
	`, now, now.Add(-24*time.Hour))
	if err != nil {
		return 0, err
	}
	codesDeleted := result.RowsAffected()

	// Clean up old rate limit entries
	_, err = s.db.Exec(ctx, `
		DELETE FROM ml_rate_limits
		WHERE window_end < $1
	`, now)
	if err != nil {
		return codesDeleted, err
	}

	return codesDeleted, nil
}

// generateCode generates a random OTP code.
func (s *Store) generateCode() string {
	code := make([]byte, s.codeLength)
	charsetLen := big.NewInt(int64(len(s.codeCharset)))

	for i := 0; i < s.codeLength; i++ {
		n, _ := rand.Int(rand.Reader, charsetLen)
		code[i] = s.codeCharset[n.Int64()]
	}

	return string(code)
}

// generateToken generates a random magic link token.
func (s *Store) generateToken() string {
	b := make([]byte, 32)
	rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}
