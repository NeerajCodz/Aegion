package handler

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type identityDB interface {
	QueryRow(ctx context.Context, sql string, optionsAndArgs ...interface{}) pgx.Row
	Exec(ctx context.Context, sql string, arguments ...interface{}) (pgconn.CommandTag, error)
}

// CoreIdentityStore integrates magic_link handlers with core identity tables.
type CoreIdentityStore struct {
	db identityDB
}

// NewCoreIdentityStore creates a core-backed identity integration store.
func NewCoreIdentityStore(db identityDB) *CoreIdentityStore {
	return &CoreIdentityStore{db: db}
}

// GetIdentityByEmail resolves an identity by email address from core tables.
func (s *CoreIdentityStore) GetIdentityByEmail(ctx context.Context, email string) (*uuid.UUID, error) {
	if s == nil || s.db == nil {
		return nil, nil
	}
	email = strings.TrimSpace(email)
	if email == "" {
		return nil, nil
	}

	var identityID uuid.UUID
	err := s.db.QueryRow(ctx, `
		SELECT i.id
		FROM core_identities i
		JOIN core_identity_addresses a ON a.identity_id = i.id
		WHERE a.type = 'email'
		  AND LOWER(a.value) = LOWER($1)
		  AND i.deleted_at IS NULL
		ORDER BY a.verified DESC, a.is_primary DESC, a.created_at DESC
		LIMIT 1
	`, email).Scan(&identityID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}

	return &identityID, nil
}

// MarkEmailVerified marks an email address as verified in core identity tables.
func (s *CoreIdentityStore) MarkEmailVerified(ctx context.Context, identityID uuid.UUID, email string) error {
	if s == nil || s.db == nil {
		return nil
	}
	email = strings.TrimSpace(email)
	if email == "" {
		return nil
	}

	_, err := s.db.Exec(ctx, `
		UPDATE core_identity_addresses
		SET verified = TRUE,
		    verified_at = COALESCE(verified_at, NOW()),
		    updated_at = NOW()
		WHERE identity_id = $1
		  AND type = 'email'
		  AND LOWER(value) = LOWER($2)
	`, identityID, email)
	return err
}
