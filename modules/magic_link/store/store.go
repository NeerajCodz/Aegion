// Package store provides database operations for the magic link module.
package store

import (
	"context"
	"encoding/base64"
	"errors"
	"time"

	platformcrypto "github.com/aegion/aegion/internal/platform/crypto"
	"github.com/aegion/aegion/internal/platform/observability"
	"github.com/aegion/aegion/internal/platform/secrettoken"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// DB interface defines methods needed for database operations
type DB interface {
	Exec(ctx context.Context, sql string, arguments ...interface{}) (pgconn.CommandTag, error)
	QueryRow(ctx context.Context, sql string, optionsAndArgs ...interface{}) pgx.Row
}

var (
	ErrCodeNotFound = errors.New("code not found")
	ErrCodeExpired  = errors.New("code expired")
	ErrCodeUsed     = errors.New("code already used")
	ErrRateLimited  = errors.New("rate limit exceeded")

	randomIntN  = platformcrypto.RandomIntN
	randomBytes = platformcrypto.RandomBytes
)

const tokenLookupPrefixLength = secrettoken.DefaultLookupPrefixLength

// CodeType represents the type of magic link/OTP code.
type CodeType string

const (
	CodeTypeLogin        CodeType = "login"
	CodeTypeVerification CodeType = "verification"
	CodeTypeRecovery     CodeType = "recovery"

	dbTableCodes      = "ml_codes"
	dbTableRateLimits = "ml_rate_limits"
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

type queryObserver interface {
	WrapQuery(ctx context.Context, operation, table string, fn func(context.Context) error) error
}

// Store handles magic link/OTP persistence.
type Store struct {
	db            DB
	codeLength    int
	codeCharset   string
	queryObserver queryObserver
}

// New creates a new magic link store.
func New(db *pgxpool.Pool) *Store {
	return newStore(db, newQueryObserver())
}

// NewWithDB creates a new magic link store with a custom DB interface (primarily for testing).
func NewWithDB(db DB) *Store {
	return newStore(db, newQueryObserver())
}

func newStore(db DB, observer queryObserver) *Store {
	return &Store{
		db:            db,
		codeLength:    6,
		codeCharset:   "0123456789",
		queryObserver: observer,
	}
}

func newQueryObserver() queryObserver {
	tracer := observability.NewTracerWrapper("aegion.magic_link.store")
	meter, err := observability.NewMeterWrapper("aegion.magic_link.store")
	if err != nil {
		return nil
	}
	return observability.NewDatabaseMiddleware(tracer, meter)
}

func (s *Store) withObservedQuery(ctx context.Context, operation, table string, fn func(context.Context) error) error {
	if s.queryObserver == nil {
		return fn(ctx)
	}
	return s.queryObserver.WrapQuery(ctx, operation, table, fn)
}

// SetCodeConfig sets the OTP code configuration.
func (s *Store) SetCodeConfig(length int, charset string) {
	s.codeLength = length
	s.codeCharset = charset
}

// Create creates a new magic link/OTP code.
func (s *Store) Create(ctx context.Context, recipient string, codeType CodeType, identityID *uuid.UUID, ttl time.Duration) (*Code, error) {
	codeValue, err := s.generateCode()
	if err != nil {
		return nil, err
	}

	tokenValue, err := s.generateToken()
	if err != nil {
		return nil, err
	}

	code := &Code{
		ID:         uuid.New(),
		IdentityID: identityID,
		Recipient:  recipient,
		Type:       codeType,
		Code:       codeValue,
		Token:      tokenValue,
		Used:       false,
		ExpiresAt:  time.Now().UTC().Add(ttl),
		CreatedAt:  time.Now().UTC(),
	}

	codeHash := secrettoken.Hash(code.Code)
	tokenHash := secrettoken.Hash(code.Token)
	tokenPrefix := secrettoken.Prefix(code.Token, tokenLookupPrefixLength)

	err = s.withObservedQuery(ctx, "INSERT", dbTableCodes, func(queryCtx context.Context) error {
		_, execErr := s.db.Exec(queryCtx, `
		INSERT INTO ml_codes (id, identity_id, recipient, type, code, code_hash, token, token_hash, token_prefix, used, expires_at, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
	`, code.ID, code.IdentityID, code.Recipient, code.Type, nil, codeHash, nil, tokenHash, tokenPrefix, code.Used, code.ExpiresAt, code.CreatedAt)
		return execErr
	})

	if err != nil {
		return nil, err
	}

	return code, nil
}

// GetByCode retrieves a code by OTP code and recipient.
func (s *Store) GetByCode(ctx context.Context, recipient string, otpCode string, codeType CodeType) (*Code, error) {
	code := &Code{}
	codeHash := secrettoken.Hash(otpCode)

	err := s.withObservedQuery(ctx, "SELECT", dbTableCodes, func(queryCtx context.Context) error {
		return s.db.QueryRow(queryCtx, `
		SELECT id, identity_id, recipient, type, COALESCE(code, ''), COALESCE(token, ''), used, used_at, expires_at, created_at
		FROM ml_codes
		WHERE recipient = $1 AND type = $2 AND used = FALSE
		  AND (code_hash = $3 OR code = $4)
		ORDER BY created_at DESC
		LIMIT 1
	`, recipient, codeType, codeHash, otpCode).Scan(
			&code.ID, &code.IdentityID, &code.Recipient, &code.Type,
			&code.Code, &code.Token, &code.Used, &code.UsedAt,
			&code.ExpiresAt, &code.CreatedAt,
		)
	})

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrCodeNotFound
		}
		return nil, err
	}
	code.Code = otpCode

	if time.Now().UTC().After(code.ExpiresAt) {
		return nil, ErrCodeExpired
	}

	return code, nil
}

// GetByToken retrieves a code by magic link token.
func (s *Store) GetByToken(ctx context.Context, token string) (*Code, error) {
	code := &Code{}
	tokenHash := secrettoken.Hash(token)
	tokenPrefix := secrettoken.Prefix(token, tokenLookupPrefixLength)

	err := s.withObservedQuery(ctx, "SELECT", dbTableCodes, func(queryCtx context.Context) error {
		return s.db.QueryRow(queryCtx, `
		SELECT id, identity_id, recipient, type, COALESCE(code, ''), COALESCE(token, ''), used, used_at, expires_at, created_at
		FROM ml_codes
		WHERE used = FALSE
		  AND (
			(token_hash = $1 AND token_prefix = $2)
			OR token = $3
		  )
		ORDER BY created_at DESC
		LIMIT 1
	`, tokenHash, tokenPrefix, token).Scan(
			&code.ID, &code.IdentityID, &code.Recipient, &code.Type,
			&code.Code, &code.Token, &code.Used, &code.UsedAt,
			&code.ExpiresAt, &code.CreatedAt,
		)
	})

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrCodeNotFound
		}
		return nil, err
	}
	code.Token = token

	if time.Now().UTC().After(code.ExpiresAt) {
		return nil, ErrCodeExpired
	}

	return code, nil
}

// MarkUsed marks a code as used.
func (s *Store) MarkUsed(ctx context.Context, codeID uuid.UUID) error {
	now := time.Now().UTC()
	var result pgconn.CommandTag
	err := s.withObservedQuery(ctx, "UPDATE", dbTableCodes, func(queryCtx context.Context) error {
		var execErr error
		result, execErr = s.db.Exec(queryCtx, `
		UPDATE ml_codes
		SET used = TRUE, used_at = $1
		WHERE id = $2 AND used = FALSE
	`, now, codeID)
		return execErr
	})

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
	return s.withObservedQuery(ctx, "UPDATE", dbTableCodes, func(queryCtx context.Context) error {
		_, err := s.db.Exec(queryCtx, `
		UPDATE ml_codes
		SET used = TRUE, used_at = NOW()
		WHERE recipient = $1 AND type = $2 AND used = FALSE
	`, recipient, codeType)
		return err
	})
}

// CheckRateLimit checks if a request is rate limited.
func (s *Store) CheckRateLimit(ctx context.Context, key string, limit int, window time.Duration) error {
	now := time.Now().UTC()
	windowEnd := now.Add(window)

	// Try to increment existing counter or insert new one
	var count int
	err := s.withObservedQuery(ctx, "UPSERT", dbTableRateLimits, func(queryCtx context.Context) error {
		return s.db.QueryRow(queryCtx, `
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
	})

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
	var result pgconn.CommandTag
	err := s.withObservedQuery(ctx, "DELETE", dbTableCodes, func(queryCtx context.Context) error {
		var execErr error
		result, execErr = s.db.Exec(queryCtx, `
		DELETE FROM ml_codes
		WHERE expires_at < $1 OR (used = TRUE AND used_at < $2)
	`, now, now.Add(-24*time.Hour))
		return execErr
	})
	if err != nil {
		return 0, err
	}
	codesDeleted := result.RowsAffected()

	// Clean up old rate limit entries
	err = s.withObservedQuery(ctx, "DELETE", dbTableRateLimits, func(queryCtx context.Context) error {
		_, execErr := s.db.Exec(queryCtx, `
		DELETE FROM ml_rate_limits
		WHERE window_end < $1
	`, now)
		return execErr
	})
	if err != nil {
		return codesDeleted, err
	}

	return codesDeleted, nil
}

// generateCode generates a random OTP code.
func (s *Store) generateCode() (string, error) {
	code := make([]byte, s.codeLength)
	charsetLen := len(s.codeCharset)

	for i := 0; i < s.codeLength; i++ {
		n, err := randomIntN(charsetLen)
		if err != nil {
			return "", err
		}
		code[i] = s.codeCharset[n]
	}

	return string(code), nil
}

// generateToken generates a random magic link token.
func (s *Store) generateToken() (string, error) {
	b, err := randomBytes(32)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
