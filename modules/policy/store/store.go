package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrNotFound = errors.New("not found")
)

// DB abstracts database operations used by the policy store.
type DB interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

// Store handles policy persistence.
type Store struct {
	db DB
}

// New creates a policy store with a pgx connection pool.
func New(db *pgxpool.Pool) *Store {
	return &Store{db: db}
}

// NewWithDB creates a policy store with a custom DB implementation.
func NewWithDB(db DB) *Store {
	return &Store{db: db}
}

// Permission is an RBAC permission tuple.
type Permission struct {
	ResourceType string
	Action       string
}

// ListRoleIDsByIdentity returns role IDs assigned to an identity.
func (s *Store) ListRoleIDsByIdentity(ctx context.Context, identityID string) ([]string, error) {
	rows, err := s.db.Query(ctx, `
		SELECT role_id::text
		FROM pol_role_assignments
		WHERE identity_id = $1
	`, identityID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	roles := make([]string, 0, 4)
	for rows.Next() {
		var roleID string
		if err := rows.Scan(&roleID); err != nil {
			return nil, err
		}
		roles = append(roles, roleID)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return roles, nil
}

// ListPermissionsByRoleIDs returns permissions for the provided role IDs.
func (s *Store) ListPermissionsByRoleIDs(ctx context.Context, roleIDs []string) ([]Permission, error) {
	if len(roleIDs) == 0 {
		return []Permission{}, nil
	}

	rows, err := s.db.Query(ctx, `
		SELECT resource_type, action
		FROM pol_permissions
		WHERE role_id::text = ANY($1)
	`, roleIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	permissions := make([]Permission, 0, len(roleIDs))
	for rows.Next() {
		var p Permission
		if err := rows.Scan(&p.ResourceType, &p.Action); err != nil {
			return nil, err
		}
		permissions = append(permissions, p)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return permissions, nil
}

// GetRoleIDByName resolves a role name to role ID.
func (s *Store) GetRoleIDByName(ctx context.Context, roleName string) (string, error) {
	var roleID string
	err := s.db.QueryRow(ctx, `SELECT id::text FROM pol_roles WHERE name = $1`, roleName).Scan(&roleID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", ErrNotFound
		}
		return "", err
	}
	if roleID == "" {
		return "", fmt.Errorf("empty role id returned for role %q", roleName)
	}
	return roleID, nil
}
