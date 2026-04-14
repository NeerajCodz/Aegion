// Package service provides admin capability evaluation.
package service

import (
	"context"
	"sort"
	"strings"

	"github.com/google/uuid"
)

var permissionCatalog = []string{
	PermAll,
	PermIdentitiesRead,
	PermIdentitiesWrite,
	PermIdentitiesCreate,
	PermIdentitiesUpdate,
	PermIdentitiesDelete,
	PermIdentitiesAll,
	PermSessionsRead,
	PermSessionsRevoke,
	PermSessionsDelete,
	PermSessionsAll,
	PermConfigRead,
	PermConfigWrite,
	PermConfigUpdate,
	PermConfigAll,
	PermOperatorsRead,
	PermOperatorsCreate,
	PermOperatorsUpdate,
	PermOperatorsDelete,
	PermOperatorsAll,
	PermRolesRead,
	PermRolesCreate,
	PermRolesUpdate,
	PermRolesDelete,
	PermRolesAll,
	PermAPIKeysRead,
	PermAPIKeysCreate,
	PermAPIKeysDelete,
	PermAPIKeysAll,
	PermAuditRead,
	PermAuditAll,
	PermSchemasRead,
	PermSchemasCreate,
	PermSchemasUpdate,
	PermSchemasDelete,
	PermSchemasAll,
	PermFlowsRead,
	PermFlowsCreate,
	PermFlowsUpdate,
	PermFlowsDelete,
	PermFlowsAll,
	PermSecurityRead,
	PermSecurityCreate,
	PermSecurityUpdate,
	PermSecurityDelete,
	PermSecurityAll,
}

var validPermissionSet = func() map[string]struct{} {
	set := make(map[string]struct{}, len(permissionCatalog))
	for _, permission := range permissionCatalog {
		set[permission] = struct{}{}
	}
	return set
}()

// Permission constants for admin operations.
const (
	// Wildcard permission - grants all access
	PermAll = "*"

	// Identity permissions
	PermIdentitiesRead   = "identities:read"
	PermIdentitiesWrite  = "identities:write"
	PermIdentitiesCreate = "identities:create"
	PermIdentitiesUpdate = "identities:update"
	PermIdentitiesDelete = "identities:delete"
	PermIdentitiesAll    = "identities:*"

	// Session permissions
	PermSessionsRead   = "sessions:read"
	PermSessionsRevoke = "sessions:revoke"
	PermSessionsDelete = "sessions:delete"
	PermSessionsAll    = "sessions:*"

	// Config permissions
	PermConfigRead   = "config:read"
	PermConfigWrite  = "config:write"
	PermConfigUpdate = "config:update"
	PermConfigAll    = "config:*"

	// Operator permissions (admin user management)
	PermOperatorsRead   = "operators:read"
	PermOperatorsCreate = "operators:create"
	PermOperatorsUpdate = "operators:update"
	PermOperatorsDelete = "operators:delete"
	PermOperatorsAll    = "operators:*"

	// Role permissions
	PermRolesRead   = "roles:read"
	PermRolesCreate = "roles:create"
	PermRolesUpdate = "roles:update"
	PermRolesDelete = "roles:delete"
	PermRolesAll    = "roles:*"

	// API Key permissions
	PermAPIKeysRead   = "api_keys:read"
	PermAPIKeysCreate = "api_keys:create"
	PermAPIKeysDelete = "api_keys:delete"
	PermAPIKeysAll    = "api_keys:*"

	// Audit permissions
	PermAuditRead = "audit:read"
	PermAuditAll  = "audit:*"

	// Schema permissions
	PermSchemasRead   = "schemas:read"
	PermSchemasCreate = "schemas:create"
	PermSchemasUpdate = "schemas:update"
	PermSchemasDelete = "schemas:delete"
	PermSchemasAll    = "schemas:*"

	// Flow permissions
	PermFlowsRead   = "flows:read"
	PermFlowsCreate = "flows:create"
	PermFlowsUpdate = "flows:update"
	PermFlowsDelete = "flows:delete"
	PermFlowsAll    = "flows:*"

	// Security controls permissions
	PermSecurityRead   = "security:read"
	PermSecurityCreate = "security:create"
	PermSecurityUpdate = "security:update"
	PermSecurityDelete = "security:delete"
	PermSecurityAll    = "security:*"
)

// DefaultRolePermissions defines the default permissions for each role.
var DefaultRolePermissions = map[string][]string{
	"super_admin": {PermAll},
	"admin": {
		PermIdentitiesAll,
		PermSessionsAll,
		PermConfigRead,
		PermConfigUpdate,
		PermAuditRead,
		PermOperatorsRead,
		PermRolesRead,
		PermSecurityAll,
	},
	"operator": {
		PermIdentitiesRead,
		PermIdentitiesUpdate,
		PermSessionsRead,
		PermSessionsDelete,
		PermAuditRead,
		PermSecurityRead,
	},
	"viewer": {
		PermIdentitiesRead,
		PermSessionsRead,
		PermConfigRead,
		PermAuditRead,
	},
}

// AvailablePermissions returns the complete set of assignable permissions.
func AvailablePermissions() []string {
	out := make([]string, 0, len(permissionCatalog))
	out = append(out, permissionCatalog...)
	sort.Strings(out)
	return out
}

// IsValidPermission returns true when permission is recognized by the RBAC model.
func IsValidPermission(permission string) bool {
	permission = strings.TrimSpace(permission)
	if permission == "" {
		return false
	}
	_, ok := validPermissionSet[permission]
	return ok
}

// EvaluateCapability checks if an operator has a specific permission.
// Returns nil if the operator has the permission, or an error if not.
func (s *Service) EvaluateCapability(ctx context.Context, operatorID uuid.UUID, permission string) error {
	op, err := s.store.GetOperator(ctx, operatorID)
	if err != nil {
		return ErrUnauthorized
	}

	// Check if operator has the permission
	if s.hasPermission(op.Role, op.Permissions, permission) {
		return nil
	}

	return ErrPermissionDenied
}

// EvaluateCapabilityByIdentity checks if an identity (via their operator record) has a permission.
func (s *Service) EvaluateCapabilityByIdentity(ctx context.Context, identityID uuid.UUID, permission string) error {
	op, err := s.store.GetOperatorByIdentityID(ctx, identityID)
	if err != nil {
		return ErrUnauthorized
	}

	// Check if operator has the permission
	if s.hasPermission(op.Role, op.Permissions, permission) {
		return nil
	}

	return ErrPermissionDenied
}

// HasCapability is a convenience method that returns a boolean instead of error.
func (s *Service) HasCapability(ctx context.Context, operatorID uuid.UUID, permission string) bool {
	return s.EvaluateCapability(ctx, operatorID, permission) == nil
}

// GetEffectivePermissions returns all effective permissions for an operator.
func (s *Service) GetEffectivePermissions(ctx context.Context, operatorID uuid.UUID) ([]string, error) {
	op, err := s.store.GetOperator(ctx, operatorID)
	if err != nil {
		return nil, err
	}

	perms := make(map[string]bool)

	// First, get role-based permissions from database
	role, err := s.store.GetRoleByName(ctx, op.Role)
	if err == nil && role != nil {
		for _, p := range role.Permissions {
			perms[p] = true
		}
	} else {
		// Fall back to default permissions if role not found in DB
		for _, p := range DefaultRolePermissions[op.Role] {
			perms[p] = true
		}
	}

	// Apply individual overrides
	for perm, granted := range op.Permissions {
		if g, ok := granted.(bool); ok {
			if g {
				perms[perm] = true
			} else {
				delete(perms, perm)
			}
		}
	}

	// Convert to slice
	result := make([]string, 0, len(perms))
	for p := range perms {
		result = append(result, p)
	}

	return result, nil
}

// hasPermission checks if the operator has the specified permission.
func (s *Service) hasPermission(role string, overrides map[string]interface{}, permission string) bool {
	// Explicit deny override always wins (exact and wildcard matches).
	if granted, ok := overrides[permission]; ok {
		if g, ok := granted.(bool); ok {
			return g
		}
	}
	for overridePerm, granted := range overrides {
		g, ok := granted.(bool)
		if !ok || g {
			continue
		}
		if matchPermission(overridePerm, permission) {
			return false
		}
	}

	// Get role permissions - try database first
	var rolePerms []string
	if cachedRole, err := s.store.GetRoleByName(context.Background(), role); err == nil && cachedRole != nil {
		rolePerms = cachedRole.Permissions
	} else {
		rolePerms = DefaultRolePermissions[role]
	}

	// Check if any role permission grants access
	for _, rolePerm := range rolePerms {
		if matchPermission(rolePerm, permission) {
			return true
		}
	}

	// Check individual overrides (explicit grant)
	for overridePerm, granted := range overrides {
		if g, ok := granted.(bool); ok && g {
			if matchPermission(overridePerm, permission) {
				return true
			}
		}
	}

	return false
}

// matchPermission checks if a granted permission matches the required permission.
// Supports wildcards like "identities:*" and "*".
func matchPermission(granted, required string) bool {
	// Exact match
	if granted == required {
		return true
	}

	// Global wildcard
	if granted == PermAll {
		return true
	}

	// Category wildcard (e.g., "identities:*" matches "identities:read")
	if strings.HasSuffix(granted, ":*") {
		prefix := strings.TrimSuffix(granted, "*")
		if strings.HasPrefix(required, prefix) {
			return true
		}
	}

	return false
}

// RequireCapability is a helper that returns an error when the capability check fails.
func (s *Service) RequireCapability(ctx context.Context, operatorID uuid.UUID, permission string) error {
	return s.EvaluateCapability(ctx, operatorID, permission)
}
