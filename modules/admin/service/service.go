// Package service provides admin business logic.
package service

import (
	"context"
	"errors"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/aegion/aegion/modules/admin/store"
)

// Errors for the admin service.
var (
	ErrUnauthorized        = errors.New("unauthorized")
	ErrForbidden           = errors.New("access denied")
	ErrInvalidRole         = errors.New("invalid role")
	ErrInvalidRoleName     = errors.New("invalid role name")
	ErrInvalidPermission   = errors.New("invalid permission")
	ErrRoleInUse           = errors.New("role is still assigned to operators")
	ErrPermissionDenied    = errors.New("insufficient permissions")
	ErrBootstrapNotAllowed = errors.New("bootstrap not allowed: operators already exist")
	ErrSelfDeletion        = errors.New("cannot delete your own operator account")
	ErrSelfDemotion        = errors.New("cannot demote your own super_admin account")
)

// SystemRoles are seeded immutable roles.
var SystemRoles = map[string]struct{}{
	"super_admin": {},
	"admin":       {},
	"operator":    {},
	"viewer":      {},
}

// ValidRoles is retained for compatibility with existing tests and seeded role assumptions.
var ValidRoles = map[string]bool{
	"super_admin": true,
	"admin":       true,
	"operator":    true,
	"viewer":      true,
}

var roleNamePattern = regexp.MustCompile(`^[a-z][a-z0-9_]{2,31}$`)

// Config holds admin service configuration.
type Config struct {
	// BootstrapEnabled allows first operator creation without auth
	BootstrapEnabled bool
}

// Service handles admin operations.
type Service struct {
	store  Store
	config Config
}

// New creates a new admin service.
func New(s Store, config Config) *Service {
	return &Service{
		store:  s,
		config: config,
	}
}

// Store returns the underlying store for direct access if needed.
func (s *Service) Store() Store {
	return s.store
}

// ============================================================================
// Bootstrap Operations
// ============================================================================

// CanBootstrap checks if the system can be bootstrapped (no operators exist).
func (s *Service) CanBootstrap(ctx context.Context) (bool, error) {
	operators, _, err := s.store.ListOperators(ctx, store.ListOptions{Limit: 1})
	if err != nil {
		return false, err
	}
	return len(operators) == 0 && s.config.BootstrapEnabled, nil
}

// Bootstrap creates the first super_admin operator.
// This can only be called when no operators exist.
func (s *Service) Bootstrap(ctx context.Context, identityID uuid.UUID, ipAddress string) (*store.Operator, error) {
	canBootstrap, err := s.CanBootstrap(ctx)
	if err != nil {
		return nil, err
	}
	if !canBootstrap {
		return nil, ErrBootstrapNotAllowed
	}

	op := &store.Operator{
		ID:          uuid.New(),
		IdentityID:  identityID,
		Role:        "super_admin",
		Permissions: make(map[string]interface{}),
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
	}

	if err := s.store.CreateOperator(ctx, op); err != nil {
		return nil, err
	}

	// Log the bootstrap action
	s.logAction(ctx, &op.ID, "create", "operator", op.ID.String(), map[string]interface{}{
		"bootstrap":   true,
		"identity_id": identityID.String(),
		"role":        "super_admin",
	}, ipAddress)

	return op, nil
}

// ============================================================================
// Operator Management
// ============================================================================

// CreateOperator creates a new admin operator.
func (s *Service) CreateOperator(ctx context.Context, actorID uuid.UUID, identityID uuid.UUID, role string, permissions map[string]interface{}, ipAddress string) (*store.Operator, error) {
	normalizedRole, err := s.requireExistingRole(ctx, role)
	if err != nil {
		return nil, ErrInvalidRole
	}

	// Check actor has permission to create operators
	if err := s.EvaluateCapability(ctx, actorID, PermOperatorsCreate); err != nil {
		return nil, err
	}

	// Only super_admin can create super_admin
	if normalizedRole == "super_admin" {
		actor, err := s.store.GetOperator(ctx, actorID)
		if err != nil {
			return nil, err
		}
		if actor.Role != "super_admin" {
			return nil, ErrPermissionDenied
		}
	}

	if permissions == nil {
		permissions = make(map[string]interface{})
	}
	if err := validatePermissionOverrides(permissions); err != nil {
		return nil, err
	}

	op := &store.Operator{
		ID:          uuid.New(),
		IdentityID:  identityID,
		Role:        normalizedRole,
		Permissions: permissions,
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
	}

	if err := s.store.CreateOperator(ctx, op); err != nil {
		return nil, err
	}

	// Log the action
	s.logAction(ctx, &actorID, "create", "operator", op.ID.String(), map[string]interface{}{
		"identity_id": identityID.String(),
		"role":        normalizedRole,
	}, ipAddress)

	return op, nil
}

// GetOperator retrieves an operator by ID.
func (s *Service) GetOperator(ctx context.Context, actorID uuid.UUID, operatorID uuid.UUID) (*store.Operator, error) {
	// Check permission
	if err := s.EvaluateCapability(ctx, actorID, PermOperatorsRead); err != nil {
		return nil, err
	}

	return s.store.GetOperator(ctx, operatorID)
}

// GetOperatorByIdentityID retrieves an operator by identity ID.
func (s *Service) GetOperatorByIdentityID(ctx context.Context, identityID uuid.UUID) (*store.Operator, error) {
	return s.store.GetOperatorByIdentityID(ctx, identityID)
}

// UpdateOperator updates an existing operator.
func (s *Service) UpdateOperator(ctx context.Context, actorID uuid.UUID, operatorID uuid.UUID, role string, permissions map[string]interface{}, ipAddress string) (*store.Operator, error) {
	// Check permission
	if err := s.EvaluateCapability(ctx, actorID, PermOperatorsUpdate); err != nil {
		return nil, err
	}

	normalizedRole := ""
	if role != "" {
		var err error
		normalizedRole, err = s.requireExistingRole(ctx, role)
		if err != nil {
			return nil, ErrInvalidRole
		}
	}

	// Prevent self-demotion from super_admin
	if actorID == operatorID && normalizedRole != "" {
		actor, err := s.store.GetOperator(ctx, actorID)
		if err != nil {
			return nil, err
		}
		if actor.Role == "super_admin" && normalizedRole != "super_admin" {
			return nil, ErrSelfDemotion
		}
	}

	if permissions != nil {
		if err := validatePermissionOverrides(permissions); err != nil {
			return nil, err
		}
	}

	// Get existing operator
	op, err := s.store.GetOperator(ctx, operatorID)
	if err != nil {
		return nil, err
	}

	// Only super_admin can modify super_admin accounts
	if op.Role == "super_admin" || normalizedRole == "super_admin" {
		actor, err := s.store.GetOperator(ctx, actorID)
		if err != nil {
			return nil, err
		}
		if actor.Role != "super_admin" {
			return nil, ErrPermissionDenied
		}
	}

	// Update fields
	oldRole := op.Role
	if normalizedRole != "" {
		op.Role = normalizedRole
	}
	if permissions != nil {
		op.Permissions = permissions
	}
	op.UpdatedAt = time.Now().UTC()

	if err := s.store.UpdateOperator(ctx, op); err != nil {
		return nil, err
	}

	// Log the action
	s.logAction(ctx, &actorID, "update", "operator", op.ID.String(), map[string]interface{}{
		"old_role": oldRole,
		"new_role": op.Role,
	}, ipAddress)

	return op, nil
}

// DeleteOperator removes an operator.
func (s *Service) DeleteOperator(ctx context.Context, actorID uuid.UUID, operatorID uuid.UUID, ipAddress string) error {
	// Prevent self-deletion
	if actorID == operatorID {
		return ErrSelfDeletion
	}

	// Check permission
	if err := s.EvaluateCapability(ctx, actorID, PermOperatorsDelete); err != nil {
		return err
	}

	// Get operator to delete
	op, err := s.store.GetOperator(ctx, operatorID)
	if err != nil {
		return err
	}

	// Only super_admin can delete super_admin
	if op.Role == "super_admin" {
		actor, err := s.store.GetOperator(ctx, actorID)
		if err != nil {
			return err
		}
		if actor.Role != "super_admin" {
			return ErrPermissionDenied
		}
	}

	if err := s.store.DeleteOperator(ctx, operatorID); err != nil {
		return err
	}

	// Log the action
	s.logAction(ctx, &actorID, "delete", "operator", operatorID.String(), map[string]interface{}{
		"identity_id": op.IdentityID.String(),
		"role":        op.Role,
	}, ipAddress)

	return nil
}

// ListOperators retrieves all operators with pagination.
func (s *Service) ListOperators(ctx context.Context, actorID uuid.UUID, limit, offset int) ([]*store.Operator, int64, error) {
	// Check permission
	if err := s.EvaluateCapability(ctx, actorID, PermOperatorsRead); err != nil {
		return nil, 0, err
	}

	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}

	return s.store.ListOperators(ctx, store.ListOptions{
		Limit:  limit,
		Offset: offset,
		Sort:   "created_at DESC",
	})
}

// ============================================================================
// Role Management
// ============================================================================

// ListRoles retrieves all roles.
func (s *Service) ListRoles(ctx context.Context, actorID uuid.UUID, limit, offset int) ([]*store.Role, int64, error) {
	// Check permission
	if err := s.EvaluateCapability(ctx, actorID, PermRolesRead); err != nil {
		return nil, 0, err
	}

	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}

	return s.store.ListRoles(ctx, store.ListOptions{
		Limit:  limit,
		Offset: offset,
	})
}

// GetRole retrieves a role by name.
func (s *Service) GetRole(ctx context.Context, actorID uuid.UUID, name string) (*store.Role, error) {
	// Check permission
	if err := s.EvaluateCapability(ctx, actorID, PermRolesRead); err != nil {
		return nil, err
	}

	return s.store.GetRoleByName(ctx, strings.TrimSpace(strings.ToLower(name)))
}

// CreateRole creates a custom role.
func (s *Service) CreateRole(ctx context.Context, actorID uuid.UUID, name, description string, permissions []string, ipAddress string) (*store.Role, error) {
	if err := s.EvaluateCapability(ctx, actorID, PermRolesCreate); err != nil {
		return nil, err
	}

	normalizedName, err := normalizeRoleName(name)
	if err != nil {
		return nil, err
	}
	if _, reserved := SystemRoles[normalizedName]; reserved {
		return nil, ErrPermissionDenied
	}

	normalizedPerms, err := normalizePermissions(permissions)
	if err != nil {
		return nil, err
	}

	role := &store.Role{
		ID:          uuid.New(),
		Name:        normalizedName,
		Description: strings.TrimSpace(description),
		Permissions: normalizedPerms,
		IsSystem:    false,
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
	}

	if err := s.store.CreateRole(ctx, role); err != nil {
		return nil, err
	}

	s.logAction(ctx, &actorID, "create", "role", role.ID.String(), map[string]interface{}{
		"name":        role.Name,
		"permissions": role.Permissions,
	}, ipAddress)

	return role, nil
}

// UpdateRole updates an existing custom role.
func (s *Service) UpdateRole(ctx context.Context, actorID uuid.UUID, name string, description *string, permissions []string, ipAddress string) (*store.Role, error) {
	if err := s.EvaluateCapability(ctx, actorID, PermRolesUpdate); err != nil {
		return nil, err
	}

	normalizedName, err := normalizeRoleName(name)
	if err != nil {
		return nil, err
	}
	role, err := s.store.GetRoleByName(ctx, normalizedName)
	if err != nil {
		return nil, err
	}
	if role.IsSystem {
		return nil, ErrPermissionDenied
	}

	if description != nil {
		role.Description = strings.TrimSpace(*description)
	}
	if permissions != nil {
		normalizedPerms, err := normalizePermissions(permissions)
		if err != nil {
			return nil, err
		}
		role.Permissions = normalizedPerms
	}
	role.UpdatedAt = time.Now().UTC()

	if err := s.store.UpdateRole(ctx, role); err != nil {
		if errors.Is(err, store.ErrSystemRole) {
			return nil, ErrPermissionDenied
		}
		return nil, err
	}

	s.logAction(ctx, &actorID, "update", "role", role.ID.String(), map[string]interface{}{
		"name":        role.Name,
		"permissions": role.Permissions,
	}, ipAddress)

	return role, nil
}

// DeleteRole deletes a custom role if no operators are assigned.
func (s *Service) DeleteRole(ctx context.Context, actorID uuid.UUID, name string, ipAddress string) error {
	if err := s.EvaluateCapability(ctx, actorID, PermRolesDelete); err != nil {
		return err
	}

	normalizedName, err := normalizeRoleName(name)
	if err != nil {
		return err
	}
	role, err := s.store.GetRoleByName(ctx, normalizedName)
	if err != nil {
		return err
	}
	if role.IsSystem {
		return ErrPermissionDenied
	}

	assigned, err := s.store.CountOperatorsByRole(ctx, role.Name)
	if err != nil {
		return err
	}
	if assigned > 0 {
		return ErrRoleInUse
	}

	if err := s.store.DeleteRole(ctx, role.ID); err != nil {
		if errors.Is(err, store.ErrSystemRole) {
			return ErrPermissionDenied
		}
		return err
	}

	s.logAction(ctx, &actorID, "delete", "role", role.ID.String(), map[string]interface{}{
		"name": role.Name,
	}, ipAddress)
	return nil
}

// AvailablePermissions lists supported RBAC permissions for admin UI/API clients.
func (s *Service) AvailablePermissions() []string {
	return AvailablePermissions()
}

// ============================================================================
// Audit Log Operations
// ============================================================================

// ListAuditLogs retrieves audit log entries.
func (s *Service) ListAuditLogs(ctx context.Context, actorID uuid.UUID, filter store.AuditFilter, limit, offset int) ([]*store.AuditLogEntry, int64, error) {
	// Check permission
	if err := s.EvaluateCapability(ctx, actorID, PermAuditRead); err != nil {
		return nil, 0, err
	}

	if limit <= 0 {
		limit = 50
	}
	if limit > 500 {
		limit = 500
	}

	return s.store.ListAuditLogs(ctx, filter, store.ListOptions{
		Limit:  limit,
		Offset: offset,
		Sort:   "created_at DESC",
	})
}

// ============================================================================
// Helper Methods
// ============================================================================

func normalizeRoleName(name string) (string, error) {
	normalized := strings.TrimSpace(strings.ToLower(name))
	if !roleNamePattern.MatchString(normalized) {
		return "", ErrInvalidRoleName
	}
	return normalized, nil
}

func normalizePermissions(permissions []string) ([]string, error) {
	seen := make(map[string]struct{}, len(permissions))
	out := make([]string, 0, len(permissions))
	for _, permission := range permissions {
		p := strings.TrimSpace(permission)
		if !IsValidPermission(p) {
			return nil, ErrInvalidPermission
		}
		if _, exists := seen[p]; exists {
			continue
		}
		seen[p] = struct{}{}
		out = append(out, p)
	}
	slices.Sort(out)
	return out, nil
}

func validatePermissionOverrides(overrides map[string]interface{}) error {
	for permission, granted := range overrides {
		if !IsValidPermission(permission) {
			return ErrInvalidPermission
		}
		if _, ok := granted.(bool); !ok {
			return ErrInvalidPermission
		}
	}
	return nil
}

func (s *Service) requireExistingRole(ctx context.Context, role string) (string, error) {
	normalizedRole, err := normalizeRoleName(role)
	if err != nil {
		return "", err
	}

	if _, err := s.store.GetRoleByName(ctx, normalizedRole); err != nil {
		if errors.Is(err, store.ErrRoleNotFound) {
			return "", ErrInvalidRole
		}
		return "", err
	}

	return normalizedRole, nil
}

// logAction logs an action to the audit log (best-effort, doesn't return errors).
func (s *Service) logAction(ctx context.Context, operatorID *uuid.UUID, action, resourceType, resourceID string, details map[string]interface{}, ipAddress string) {
	if details == nil {
		details = make(map[string]interface{})
	}

	entry := &store.AuditLogEntry{
		ID:           uuid.New(),
		OperatorID:   operatorID,
		Action:       action,
		ResourceType: resourceType,
		ResourceID:   resourceID,
		Details:      details,
		IPAddress:    ipAddress,
		CreatedAt:    time.Now().UTC(),
	}

	// Best-effort logging - don't fail main operation
	_ = s.store.LogAction(ctx, entry)
}
