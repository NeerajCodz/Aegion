// Package store provides database operations for the password module.
package store

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrCredentialNotFound = errors.New("credential not found")
	ErrCredentialExists   = errors.New("credential already exists for this identifier")
)

// Credential represents a password credential.
type Credential struct {
	ID         uuid.UUID
	IdentityID uuid.UUID
	Identifier string
	Hash       string
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// DB interface defines methods needed for database operations
type DB interface {
	Exec(ctx context.Context, sql string, arguments ...interface{}) (pgconn.CommandTag, error)
	QueryRow(ctx context.Context, sql string, optionsAndArgs ...interface{}) pgx.Row
	Query(ctx context.Context, sql string, optionsAndArgs ...interface{}) (pgx.Rows, error)
}

// Store handles password credential persistence.
type Store struct {
	db DB
}

// New creates a new password store.
func New(db *pgxpool.Pool) *Store {
	return &Store{db: db}
}

// NewWithDB creates a new password store with a custom DB interface (primarily for testing).
func NewWithDB(db DB) *Store {
	return &Store{db: db}
}

// Create creates a new password credential.
func (s *Store) Create(ctx context.Context, cred *Credential) error {
	_, err := s.db.Exec(ctx, `
		INSERT INTO pwd_credentials (id, identity_id, identifier, hash, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6)
	`, cred.ID, cred.IdentityID, cred.Identifier, cred.Hash, cred.CreatedAt, cred.UpdatedAt)

	if err != nil {
		// Check for unique constraint violation
		if isDuplicateKeyError(err) {
			return ErrCredentialExists
		}
		return err
	}

	return nil
}

// GetByIdentifier retrieves a credential by identifier (email, username).
func (s *Store) GetByIdentifier(ctx context.Context, identifier string) (*Credential, error) {
	cred := &Credential{}

	err := s.db.QueryRow(ctx, `
		SELECT id, identity_id, identifier, hash, created_at, updated_at
		FROM pwd_credentials
		WHERE identifier = $1
	`, identifier).Scan(
		&cred.ID, &cred.IdentityID, &cred.Identifier, &cred.Hash,
		&cred.CreatedAt, &cred.UpdatedAt,
	)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrCredentialNotFound
		}
		return nil, err
	}

	return cred, nil
}

// GetByIdentityID retrieves a credential by identity ID.
func (s *Store) GetByIdentityID(ctx context.Context, identityID uuid.UUID) (*Credential, error) {
	cred := &Credential{}

	err := s.db.QueryRow(ctx, `
		SELECT id, identity_id, identifier, hash, created_at, updated_at
		FROM pwd_credentials
		WHERE identity_id = $1
	`, identityID).Scan(
		&cred.ID, &cred.IdentityID, &cred.Identifier, &cred.Hash,
		&cred.CreatedAt, &cred.UpdatedAt,
	)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrCredentialNotFound
		}
		return nil, err
	}

	return cred, nil
}

// Update updates a credential's hash.
func (s *Store) Update(ctx context.Context, credID uuid.UUID, newHash string) error {
	result, err := s.db.Exec(ctx, `
		UPDATE pwd_credentials
		SET hash = $1, updated_at = NOW()
		WHERE id = $2
	`, newHash, credID)

	if err != nil {
		return err
	}

	if result.RowsAffected() == 0 {
		return ErrCredentialNotFound
	}

	return nil
}

// Delete removes a credential.
func (s *Store) Delete(ctx context.Context, credID uuid.UUID) error {
	_, err := s.db.Exec(ctx, "DELETE FROM pwd_credentials WHERE id = $1", credID)
	return err
}

// DeleteByIdentityID removes all credentials for an identity.
func (s *Store) DeleteByIdentityID(ctx context.Context, identityID uuid.UUID) error {
	_, err := s.db.Exec(ctx, "DELETE FROM pwd_credentials WHERE identity_id = $1", identityID)
	return err
}

// AddToHistory adds a hash to the password history.
func (s *Store) AddToHistory(ctx context.Context, credID uuid.UUID, hash string) error {
	_, err := s.db.Exec(ctx, `
		INSERT INTO pwd_history (credential_id, hash, created_at)
		VALUES ($1, $2, NOW())
	`, credID, hash)
	return err
}

// GetHistory retrieves the password history for a credential.
func (s *Store) GetHistory(ctx context.Context, credID uuid.UUID, limit int) ([]string, error) {
	if limit == 0 {
		limit = 5
	}

	rows, err := s.db.Query(ctx, `
		SELECT hash FROM pwd_history
		WHERE credential_id = $1
		ORDER BY created_at DESC
		LIMIT $2
	`, credID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var hashes []string
	for rows.Next() {
		var hash string
		if err := rows.Scan(&hash); err != nil {
			return nil, err
		}
		hashes = append(hashes, hash)
	}

	return hashes, rows.Err()
}

// CleanupHistory removes old password history entries.
func (s *Store) CleanupHistory(ctx context.Context, credID uuid.UUID, keepCount int) error {
	_, err := s.db.Exec(ctx, `
		DELETE FROM pwd_history
		WHERE credential_id = $1
		  AND id NOT IN (
			SELECT id FROM pwd_history
			WHERE credential_id = $1
			ORDER BY created_at DESC
			LIMIT $2
		  )
	`, credID, keepCount)
	return err
}

// isDuplicateKeyError checks if error is a duplicate key violation.
func isDuplicateKeyError(err error) bool {
	// pgx wraps postgres errors, check for unique_violation code 23505
	if err == nil {
		return false
	}
	// Simple string check for now
	return err.Error() != "" && (contains(err.Error(), "duplicate key") ||
		contains(err.Error(), "23505"))
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
