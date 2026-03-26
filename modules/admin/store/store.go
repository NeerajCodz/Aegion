// Package store provides database operations for the admin module.
package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Errors for the admin store.
var (
	ErrOperatorNotFound  = errors.New("operator not found")
	ErrRoleNotFound      = errors.New("role not found")
	ErrAPIKeyNotFound    = errors.New("api key not found")
	ErrDuplicateOperator = errors.New("operator already exists for this identity")
	ErrDuplicateRole     = errors.New("role with this name already exists")
	ErrSystemRole        = errors.New("cannot modify system role")
)

// Operator represents an admin user with permissions.
type Operator struct {
	ID          uuid.UUID
	IdentityID  uuid.UUID
	Role        string
	Permissions map[string]interface{}
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// Role represents an admin role with permissions.
type Role struct {
	ID          uuid.UUID
	Name        string
	Description string
	Permissions []string
	IsSystem    bool
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// AuditLogEntry represents an admin audit log entry.
type AuditLogEntry struct {
	ID           uuid.UUID
	OperatorID   *uuid.UUID
	Action       string
	ResourceType string
	ResourceID   string
	Details      map[string]interface{}
	IPAddress    string
	CreatedAt    time.Time
}

// APIKey represents an admin API key.
type APIKey struct {
	ID          uuid.UUID
	OperatorID  uuid.UUID
	Name        string
	KeyHash     string
	KeyPrefix   string
	Permissions map[string]interface{}
	LastUsedAt  *time.Time
	ExpiresAt   *time.Time
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// ListOptions contains pagination and filtering options.
type ListOptions struct {
	Limit  int
	Offset int
	Sort   string
}

// Store handles admin data persistence.
type Store struct {
	db *pgxpool.Pool
}

// New creates a new admin store.
func New(db *pgxpool.Pool) *Store {
	return &Store{db: db}
}

// ============================================================================
// OperatorStore Implementation
// ============================================================================

// CreateOperator creates a new admin operator.
func (s *Store) CreateOperator(ctx context.Context, op *Operator) error {
	permsJSON, err := json.Marshal(op.Permissions)
	if err != nil {
		return err
	}

	_, err = s.db.Exec(ctx, `
		INSERT INTO adm_operators (id, identity_id, role, permissions, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6)
	`, op.ID, op.IdentityID, op.Role, permsJSON, op.CreatedAt, op.UpdatedAt)

	if err != nil {
		if isDuplicateKeyError(err) {
			return ErrDuplicateOperator
		}
		return err
	}

	return nil
}

// GetOperator retrieves an operator by ID.
func (s *Store) GetOperator(ctx context.Context, id uuid.UUID) (*Operator, error) {
	op := &Operator{}
	var permsJSON []byte

	err := s.db.QueryRow(ctx, `
		SELECT id, identity_id, role, permissions, created_at, updated_at
		FROM adm_operators
		WHERE id = $1
	`, id).Scan(&op.ID, &op.IdentityID, &op.Role, &permsJSON, &op.CreatedAt, &op.UpdatedAt)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrOperatorNotFound
		}
		return nil, err
	}

	if err := json.Unmarshal(permsJSON, &op.Permissions); err != nil {
		op.Permissions = make(map[string]interface{})
	}

	return op, nil
}

// GetOperatorByIdentityID retrieves an operator by identity ID.
func (s *Store) GetOperatorByIdentityID(ctx context.Context, identityID uuid.UUID) (*Operator, error) {
	op := &Operator{}
	var permsJSON []byte

	err := s.db.QueryRow(ctx, `
		SELECT id, identity_id, role, permissions, created_at, updated_at
		FROM adm_operators
		WHERE identity_id = $1
	`, identityID).Scan(&op.ID, &op.IdentityID, &op.Role, &permsJSON, &op.CreatedAt, &op.UpdatedAt)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrOperatorNotFound
		}
		return nil, err
	}

	if err := json.Unmarshal(permsJSON, &op.Permissions); err != nil {
		op.Permissions = make(map[string]interface{})
	}

	return op, nil
}

// UpdateOperator updates an existing operator.
func (s *Store) UpdateOperator(ctx context.Context, op *Operator) error {
	permsJSON, err := json.Marshal(op.Permissions)
	if err != nil {
		return err
	}

	result, err := s.db.Exec(ctx, `
		UPDATE adm_operators
		SET role = $1, permissions = $2, updated_at = NOW()
		WHERE id = $3
	`, op.Role, permsJSON, op.ID)

	if err != nil {
		return err
	}

	if result.RowsAffected() == 0 {
		return ErrOperatorNotFound
	}

	return nil
}

// DeleteOperator removes an operator.
func (s *Store) DeleteOperator(ctx context.Context, id uuid.UUID) error {
	result, err := s.db.Exec(ctx, "DELETE FROM adm_operators WHERE id = $1", id)
	if err != nil {
		return err
	}

	if result.RowsAffected() == 0 {
		return ErrOperatorNotFound
	}

	return nil
}

// ListOperators retrieves all operators with pagination.
func (s *Store) ListOperators(ctx context.Context, opts ListOptions) ([]*Operator, int64, error) {
	var totalCount int64
	err := s.db.QueryRow(ctx, "SELECT COUNT(*) FROM adm_operators").Scan(&totalCount)
	if err != nil {
		return nil, 0, err
	}

	if opts.Limit <= 0 {
		opts.Limit = 10
	}
	if opts.Sort == "" {
		opts.Sort = "created_at DESC"
	}

	rows, err := s.db.Query(ctx, `
		SELECT id, identity_id, role, permissions, created_at, updated_at
		FROM adm_operators
		ORDER BY `+opts.Sort+`
		LIMIT $1 OFFSET $2
	`, opts.Limit, opts.Offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var operators []*Operator
	for rows.Next() {
		op := &Operator{}
		var permsJSON []byte
		if err := rows.Scan(&op.ID, &op.IdentityID, &op.Role, &permsJSON, &op.CreatedAt, &op.UpdatedAt); err != nil {
			return nil, 0, err
		}
		if err := json.Unmarshal(permsJSON, &op.Permissions); err != nil {
			op.Permissions = make(map[string]interface{})
		}
		operators = append(operators, op)
	}

	return operators, totalCount, rows.Err()
}

// ============================================================================
// RoleStore Implementation
// ============================================================================

// CreateRole creates a new role.
func (s *Store) CreateRole(ctx context.Context, role *Role) error {
	permsJSON, err := json.Marshal(role.Permissions)
	if err != nil {
		return err
	}

	_, err = s.db.Exec(ctx, `
		INSERT INTO adm_roles (id, name, description, permissions, is_system, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`, role.ID, role.Name, role.Description, permsJSON, role.IsSystem, role.CreatedAt, role.UpdatedAt)

	if err != nil {
		if isDuplicateKeyError(err) {
			return ErrDuplicateRole
		}
		return err
	}

	return nil
}

// GetRole retrieves a role by ID.
func (s *Store) GetRole(ctx context.Context, id uuid.UUID) (*Role, error) {
	role := &Role{}
	var permsJSON []byte

	err := s.db.QueryRow(ctx, `
		SELECT id, name, description, permissions, is_system, created_at, updated_at
		FROM adm_roles
		WHERE id = $1
	`, id).Scan(&role.ID, &role.Name, &role.Description, &permsJSON, &role.IsSystem, &role.CreatedAt, &role.UpdatedAt)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrRoleNotFound
		}
		return nil, err
	}

	if err := json.Unmarshal(permsJSON, &role.Permissions); err != nil {
		role.Permissions = []string{}
	}

	return role, nil
}

// GetRoleByName retrieves a role by name.
func (s *Store) GetRoleByName(ctx context.Context, name string) (*Role, error) {
	role := &Role{}
	var permsJSON []byte

	err := s.db.QueryRow(ctx, `
		SELECT id, name, description, permissions, is_system, created_at, updated_at
		FROM adm_roles
		WHERE name = $1
	`, name).Scan(&role.ID, &role.Name, &role.Description, &permsJSON, &role.IsSystem, &role.CreatedAt, &role.UpdatedAt)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrRoleNotFound
		}
		return nil, err
	}

	if err := json.Unmarshal(permsJSON, &role.Permissions); err != nil {
		role.Permissions = []string{}
	}

	return role, nil
}

// UpdateRole updates an existing role.
func (s *Store) UpdateRole(ctx context.Context, role *Role) error {
	// Check if role is a system role
	var isSystem bool
	err := s.db.QueryRow(ctx, "SELECT is_system FROM adm_roles WHERE id = $1", role.ID).Scan(&isSystem)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrRoleNotFound
		}
		return err
	}
	if isSystem {
		return ErrSystemRole
	}

	permsJSON, err := json.Marshal(role.Permissions)
	if err != nil {
		return err
	}

	result, err := s.db.Exec(ctx, `
		UPDATE adm_roles
		SET name = $1, description = $2, permissions = $3, updated_at = NOW()
		WHERE id = $4 AND is_system = FALSE
	`, role.Name, role.Description, permsJSON, role.ID)

	if err != nil {
		if isDuplicateKeyError(err) {
			return ErrDuplicateRole
		}
		return err
	}

	if result.RowsAffected() == 0 {
		return ErrRoleNotFound
	}

	return nil
}

// DeleteRole removes a role.
func (s *Store) DeleteRole(ctx context.Context, id uuid.UUID) error {
	// Check if role is a system role
	var isSystem bool
	err := s.db.QueryRow(ctx, "SELECT is_system FROM adm_roles WHERE id = $1", id).Scan(&isSystem)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrRoleNotFound
		}
		return err
	}
	if isSystem {
		return ErrSystemRole
	}

	result, err := s.db.Exec(ctx, "DELETE FROM adm_roles WHERE id = $1 AND is_system = FALSE", id)
	if err != nil {
		return err
	}

	if result.RowsAffected() == 0 {
		return ErrRoleNotFound
	}

	return nil
}

// ListRoles retrieves all roles with pagination.
func (s *Store) ListRoles(ctx context.Context, opts ListOptions) ([]*Role, int64, error) {
	var totalCount int64
	err := s.db.QueryRow(ctx, "SELECT COUNT(*) FROM adm_roles").Scan(&totalCount)
	if err != nil {
		return nil, 0, err
	}

	if opts.Limit <= 0 {
		opts.Limit = 10
	}
	if opts.Sort == "" {
		opts.Sort = "is_system DESC, name ASC"
	}

	rows, err := s.db.Query(ctx, `
		SELECT id, name, description, permissions, is_system, created_at, updated_at
		FROM adm_roles
		ORDER BY `+opts.Sort+`
		LIMIT $1 OFFSET $2
	`, opts.Limit, opts.Offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var roles []*Role
	for rows.Next() {
		role := &Role{}
		var permsJSON []byte
		if err := rows.Scan(&role.ID, &role.Name, &role.Description, &permsJSON, &role.IsSystem, &role.CreatedAt, &role.UpdatedAt); err != nil {
			return nil, 0, err
		}
		if err := json.Unmarshal(permsJSON, &role.Permissions); err != nil {
			role.Permissions = []string{}
		}
		roles = append(roles, role)
	}

	return roles, totalCount, rows.Err()
}

// ============================================================================
// AuditStore Implementation
// ============================================================================

// LogAction records an admin action to the audit log.
func (s *Store) LogAction(ctx context.Context, entry *AuditLogEntry) error {
	detailsJSON, err := json.Marshal(entry.Details)
	if err != nil {
		return err
	}

	_, err = s.db.Exec(ctx, `
		INSERT INTO adm_audit_log (id, operator_id, action, resource_type, resource_id, details, ip_address, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`, entry.ID, entry.OperatorID, entry.Action, entry.ResourceType, entry.ResourceID, detailsJSON, entry.IPAddress, entry.CreatedAt)

	return err
}

// AuditFilter contains filter options for audit log queries.
type AuditFilter struct {
	OperatorID   *uuid.UUID
	Action       string
	ResourceType string
	ResourceID   string
	StartTime    *time.Time
	EndTime      *time.Time
}

// ListAuditLogs retrieves audit log entries with filtering.
func (s *Store) ListAuditLogs(ctx context.Context, filter AuditFilter, opts ListOptions) ([]*AuditLogEntry, int64, error) {
	whereClause := "WHERE 1=1"
	args := []interface{}{}
	argIndex := 1

	if filter.OperatorID != nil {
		whereClause += " AND operator_id = $" + itoa(argIndex)
		args = append(args, *filter.OperatorID)
		argIndex++
	}
	if filter.Action != "" {
		whereClause += " AND action = $" + itoa(argIndex)
		args = append(args, filter.Action)
		argIndex++
	}
	if filter.ResourceType != "" {
		whereClause += " AND resource_type = $" + itoa(argIndex)
		args = append(args, filter.ResourceType)
		argIndex++
	}
	if filter.ResourceID != "" {
		whereClause += " AND resource_id = $" + itoa(argIndex)
		args = append(args, filter.ResourceID)
		argIndex++
	}
	if filter.StartTime != nil {
		whereClause += " AND created_at >= $" + itoa(argIndex)
		args = append(args, *filter.StartTime)
		argIndex++
	}
	if filter.EndTime != nil {
		whereClause += " AND created_at <= $" + itoa(argIndex)
		args = append(args, *filter.EndTime)
		argIndex++
	}

	var totalCount int64
	countQuery := "SELECT COUNT(*) FROM adm_audit_log " + whereClause
	err := s.db.QueryRow(ctx, countQuery, args...).Scan(&totalCount)
	if err != nil {
		return nil, 0, err
	}

	if opts.Limit <= 0 {
		opts.Limit = 50
	}
	if opts.Sort == "" {
		opts.Sort = "created_at DESC"
	}

	query := `
		SELECT id, operator_id, action, resource_type, resource_id, details, ip_address, created_at
		FROM adm_audit_log
		` + whereClause + `
		ORDER BY ` + opts.Sort + `
		LIMIT $` + itoa(argIndex) + ` OFFSET $` + itoa(argIndex+1)
	args = append(args, opts.Limit, opts.Offset)

	rows, err := s.db.Query(ctx, query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var entries []*AuditLogEntry
	for rows.Next() {
		entry := &AuditLogEntry{}
		var detailsJSON []byte
		if err := rows.Scan(&entry.ID, &entry.OperatorID, &entry.Action, &entry.ResourceType, &entry.ResourceID, &detailsJSON, &entry.IPAddress, &entry.CreatedAt); err != nil {
			return nil, 0, err
		}
		if err := json.Unmarshal(detailsJSON, &entry.Details); err != nil {
			entry.Details = make(map[string]interface{})
		}
		entries = append(entries, entry)
	}

	return entries, totalCount, rows.Err()
}

// ============================================================================
// APIKeyStore Implementation
// ============================================================================

// CreateAPIKey creates a new API key.
func (s *Store) CreateAPIKey(ctx context.Context, key *APIKey) error {
	permsJSON, err := json.Marshal(key.Permissions)
	if err != nil {
		return err
	}

	_, err = s.db.Exec(ctx, `
		INSERT INTO adm_api_keys (id, operator_id, name, key_hash, key_prefix, permissions, expires_at, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`, key.ID, key.OperatorID, key.Name, key.KeyHash, key.KeyPrefix, permsJSON, key.ExpiresAt, key.CreatedAt, key.UpdatedAt)

	return err
}

// GetAPIKey retrieves an API key by ID.
func (s *Store) GetAPIKey(ctx context.Context, id uuid.UUID) (*APIKey, error) {
	key := &APIKey{}
	var permsJSON []byte

	err := s.db.QueryRow(ctx, `
		SELECT id, operator_id, name, key_hash, key_prefix, permissions, last_used_at, expires_at, created_at, updated_at
		FROM adm_api_keys
		WHERE id = $1
	`, id).Scan(&key.ID, &key.OperatorID, &key.Name, &key.KeyHash, &key.KeyPrefix, &permsJSON, &key.LastUsedAt, &key.ExpiresAt, &key.CreatedAt, &key.UpdatedAt)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrAPIKeyNotFound
		}
		return nil, err
	}

	if err := json.Unmarshal(permsJSON, &key.Permissions); err != nil {
		key.Permissions = make(map[string]interface{})
	}

	return key, nil
}

// GetAPIKeyByPrefix retrieves an API key by its prefix (for lookup during auth).
func (s *Store) GetAPIKeyByPrefix(ctx context.Context, prefix string) (*APIKey, error) {
	key := &APIKey{}
	var permsJSON []byte

	err := s.db.QueryRow(ctx, `
		SELECT id, operator_id, name, key_hash, key_prefix, permissions, last_used_at, expires_at, created_at, updated_at
		FROM adm_api_keys
		WHERE key_prefix = $1
	`, prefix).Scan(&key.ID, &key.OperatorID, &key.Name, &key.KeyHash, &key.KeyPrefix, &permsJSON, &key.LastUsedAt, &key.ExpiresAt, &key.CreatedAt, &key.UpdatedAt)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrAPIKeyNotFound
		}
		return nil, err
	}

	if err := json.Unmarshal(permsJSON, &key.Permissions); err != nil {
		key.Permissions = make(map[string]interface{})
	}

	return key, nil
}

// UpdateAPIKeyLastUsed updates the last_used_at timestamp for an API key.
func (s *Store) UpdateAPIKeyLastUsed(ctx context.Context, id uuid.UUID) error {
	_, err := s.db.Exec(ctx, `
		UPDATE adm_api_keys SET last_used_at = NOW(), updated_at = NOW() WHERE id = $1
	`, id)
	return err
}

// DeleteAPIKey removes an API key.
func (s *Store) DeleteAPIKey(ctx context.Context, id uuid.UUID) error {
	result, err := s.db.Exec(ctx, "DELETE FROM adm_api_keys WHERE id = $1", id)
	if err != nil {
		return err
	}

	if result.RowsAffected() == 0 {
		return ErrAPIKeyNotFound
	}

	return nil
}

// ListAPIKeysByOperator retrieves all API keys for an operator.
func (s *Store) ListAPIKeysByOperator(ctx context.Context, operatorID uuid.UUID) ([]*APIKey, error) {
	rows, err := s.db.Query(ctx, `
		SELECT id, operator_id, name, key_hash, key_prefix, permissions, last_used_at, expires_at, created_at, updated_at
		FROM adm_api_keys
		WHERE operator_id = $1
		ORDER BY created_at DESC
	`, operatorID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var keys []*APIKey
	for rows.Next() {
		key := &APIKey{}
		var permsJSON []byte
		if err := rows.Scan(&key.ID, &key.OperatorID, &key.Name, &key.KeyHash, &key.KeyPrefix, &permsJSON, &key.LastUsedAt, &key.ExpiresAt, &key.CreatedAt, &key.UpdatedAt); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(permsJSON, &key.Permissions); err != nil {
			key.Permissions = make(map[string]interface{})
		}
		keys = append(keys, key)
	}

	return keys, rows.Err()
}

// ============================================================================
// Helper functions
// ============================================================================

// isDuplicateKeyError checks if error is a duplicate key violation.
func isDuplicateKeyError(err error) bool {
	if err == nil {
		return false
	}
	errStr := err.Error()
	return contains(errStr, "duplicate key") || contains(errStr, "23505")
}

func contains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func itoa(n int) string {
	if n < 10 {
		return string(rune('0' + n))
	}
	return itoa(n/10) + string(rune('0'+n%10))
}
