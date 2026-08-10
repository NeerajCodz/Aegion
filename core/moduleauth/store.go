package moduleauth

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type credentialQueryer interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

type credentialExecutor interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
}

// PostgresStore persists module bootstrap credential hashes in core's database.
type PostgresStore struct {
	query credentialQueryer
	exec  credentialExecutor
}

// NewPostgresStore constructs durable credential storage from the core pool.
func NewPostgresStore(pool *pgxpool.Pool) *PostgresStore {
	return &PostgresStore{query: pool, exec: pool}
}

// Credential fetches a module's current bootstrap identity.
func (s *PostgresStore) Credential(ctx context.Context, moduleID string) (Credential, error) {
	if s == nil || s.query == nil {
		return Credential{}, fmt.Errorf("module credential store is unavailable")
	}
	var credential Credential
	var expiresAt *time.Time
	err := s.query.QueryRow(ctx, `
SELECT id, module_id, secret_hash, permissions, audiences, enabled, expires_at
FROM core_module_credentials
WHERE module_id = $1`, strings.TrimSpace(moduleID)).Scan(
		&credential.ID,
		&credential.ModuleID,
		&credential.SecretHash,
		&credential.Permissions,
		&credential.Audiences,
		&credential.Enabled,
		&expiresAt,
	)
	if err != nil {
		return Credential{}, err
	}
	credential.ExpiresAt = expiresAt
	return credential, nil
}

// Create stores a new credential. The caller is responsible for delivering the
// raw credential once over an operator-approved secret channel.
func (s *PostgresStore) Create(ctx context.Context, credential Credential) error {
	if s == nil || s.exec == nil {
		return fmt.Errorf("module credential store is unavailable")
	}
	if strings.TrimSpace(credential.ID) == "" {
		credential.ID = uuid.NewString()
	}
	if strings.TrimSpace(credential.ModuleID) == "" || strings.TrimSpace(credential.SecretHash) == "" || len(credential.Permissions) == 0 || len(credential.Audiences) == 0 {
		return fmt.Errorf("module credential requires module, hash, permissions, and audiences")
	}
	_, err := s.exec.Exec(ctx, `
INSERT INTO core_module_credentials (id, module_id, secret_hash, permissions, audiences, enabled, expires_at)
VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		credential.ID,
		credential.ModuleID,
		credential.SecretHash,
		credential.Permissions,
		credential.Audiences,
		credential.Enabled,
		credential.ExpiresAt,
	)
	return err
}

// Rotate atomically replaces the stored hash and reenables the module.
func (s *PostgresStore) Rotate(ctx context.Context, moduleID, secretHash string, expiresAt *time.Time) error {
	if s == nil || s.exec == nil {
		return fmt.Errorf("module credential store is unavailable")
	}
	if strings.TrimSpace(moduleID) == "" || strings.TrimSpace(secretHash) == "" {
		return fmt.Errorf("module ID and credential hash are required")
	}
	tag, err := s.exec.Exec(ctx, `
UPDATE core_module_credentials
SET secret_hash = $2, enabled = TRUE, expires_at = $3, rotated_at = NOW()
WHERE module_id = $1`, strings.TrimSpace(moduleID), secretHash, expiresAt)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("module credential %q was not found", moduleID)
	}
	return nil
}

// Revoke disables a credential and therefore invalidates every token issued
// from it on the next core-side authorization check.
func (s *PostgresStore) Revoke(ctx context.Context, moduleID string) error {
	if s == nil || s.exec == nil {
		return fmt.Errorf("module credential store is unavailable")
	}
	tag, err := s.exec.Exec(ctx, `
UPDATE core_module_credentials
SET enabled = FALSE, revoked_at = NOW()
WHERE module_id = $1`, strings.TrimSpace(moduleID))
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("module credential %q was not found", moduleID)
	}
	return nil
}
