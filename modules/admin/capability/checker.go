// Package capability provides permission checking logic.
package capability

import (
	"context"
	"net/http"
	"strings"

	"github.com/google/uuid"
)

// Store defines the interface for capability persistence operations.
type Store interface {
	// Role operations
	GetRole(ctx context.Context, id string) (*Role, error)
	GetRoles(ctx context.Context, ids []string) ([]*Role, error)
	ListRoles(ctx context.Context) ([]*Role, error)
	CreateRole(ctx context.Context, role *Role) error
	UpdateRole(ctx context.Context, role *Role) error
	DeleteRole(ctx context.Context, id string) error

	// Admin identity operations
	GetAdminIdentity(ctx context.Context, identityID uuid.UUID) (*AdminIdentity, error)
	CreateAdminIdentity(ctx context.Context, admin *AdminIdentity) error
	UpdateAdminIdentity(ctx context.Context, admin *AdminIdentity) error
	DeleteAdminIdentity(ctx context.Context, identityID uuid.UUID) error
	ListAdminIdentities(ctx context.Context) ([]*AdminIdentity, error)
}

// Checker provides capability evaluation.
type Checker struct {
	store Store
}

// NewChecker creates a new capability checker.
func NewChecker(store Store) *Checker {
	return &Checker{store: store}
}

// HasCapability checks if an admin identity has a specific capability.
// This implements the Discord-style permission model:
// 1. Check explicit denies first (they always win)
// 2. Check explicit grants
// 3. Check role-based capabilities
func (c *Checker) HasCapability(ctx context.Context, identityID uuid.UUID, cap Capability) bool {
	admin, err := c.store.GetAdminIdentity(ctx, identityID)
	if err != nil {
		return false
	}

	return c.evaluateCapability(ctx, admin, cap)
}

// evaluateCapability performs the actual capability evaluation.
func (c *Checker) evaluateCapability(ctx context.Context, admin *AdminIdentity, cap Capability) bool {
	// Step 1: Check explicit denies - they override everything
	if c.hasExplicitDeny(admin.Denies, cap) {
		return false
	}

	// Step 2: Check explicit grants
	if c.hasExplicitGrant(admin.Grants, cap) {
		return true
	}

	// Step 3: Check role-based capabilities
	if c.hasRoleCapability(ctx, admin.Roles, cap) {
		return true
	}

	return false
}

// hasExplicitDeny checks if a capability is explicitly denied.
func (c *Checker) hasExplicitDeny(denies []Capability, cap Capability) bool {
	for _, deny := range denies {
		if c.matchesCapability(deny, cap) {
			return true
		}
	}
	return false
}

// hasExplicitGrant checks if a capability is explicitly granted.
func (c *Checker) hasExplicitGrant(grants []Capability, cap Capability) bool {
	for _, grant := range grants {
		if c.matchesCapability(grant, cap) {
			return true
		}
	}
	return false
}

// hasRoleCapability checks if any assigned role grants the capability.
func (c *Checker) hasRoleCapability(ctx context.Context, roleIDs []string, cap Capability) bool {
	roles, err := c.store.GetRoles(ctx, roleIDs)
	if err != nil {
		return false
	}

	for _, role := range roles {
		for _, roleCap := range role.Capabilities {
			if c.matchesCapability(roleCap, cap) {
				return true
			}
		}
	}

	return false
}

// matchesCapability checks if a granted capability matches the required one.
// Supports wildcard matching (e.g., "users.*" matches "users.read").
func (c *Checker) matchesCapability(granted, required Capability) bool {
	// Exact match
	if granted == required {
		return true
	}

	// Global wildcard
	if granted == CapAll {
		return true
	}

	// Domain wildcard (e.g., "users.*" matches "users.read")
	grantedStr := string(granted)
	requiredStr := string(required)

	if strings.HasSuffix(grantedStr, ".*") {
		prefix := strings.TrimSuffix(grantedStr, "*")
		if strings.HasPrefix(requiredStr, prefix) {
			return true
		}
	}

	return false
}

// GetEffectiveCapabilities returns all effective capabilities for an admin identity.
func (c *Checker) GetEffectiveCapabilities(ctx context.Context, identityID uuid.UUID) ([]Capability, error) {
	admin, err := c.store.GetAdminIdentity(ctx, identityID)
	if err != nil {
		return nil, err
	}

	capabilitySet := make(map[Capability]bool)

	// Collect capabilities from roles
	roles, err := c.store.GetRoles(ctx, admin.Roles)
	if err != nil {
		return nil, err
	}

	for _, role := range roles {
		for _, cap := range role.Capabilities {
			if cap == CapAll {
				// If they have global wildcard, they have everything
				result := make([]Capability, 0, len(AllCapabilityInfo))
				for cap := range AllCapabilityInfo {
					result = append(result, cap)
				}
				return result, nil
			}
			
			// Expand wildcard capabilities
			if strings.HasSuffix(string(cap), ".*") {
				domain := strings.TrimSuffix(string(cap), ".*")
				if domainCaps, exists := AllCapabilities[domain]; exists {
					for _, domainCap := range domainCaps {
						if !strings.HasSuffix(string(domainCap), ".*") {
							capabilitySet[domainCap] = true
						}
					}
				}
			} else {
				capabilitySet[cap] = true
			}
		}
	}

	// Apply explicit grants
	for _, grant := range admin.Grants {
		if grant == CapAll {
			// If they have global wildcard, they have everything
			result := make([]Capability, 0, len(AllCapabilityInfo))
			for cap := range AllCapabilityInfo {
				result = append(result, cap)
			}
			return result, nil
		}
		
		// Expand wildcard capabilities
		if strings.HasSuffix(string(grant), ".*") {
			domain := strings.TrimSuffix(string(grant), ".*")
			if domainCaps, exists := AllCapabilities[domain]; exists {
				for _, domainCap := range domainCaps {
					if !strings.HasSuffix(string(domainCap), ".*") {
						capabilitySet[domainCap] = true
					}
				}
			}
		} else {
			capabilitySet[grant] = true
		}
	}

	// Remove explicitly denied capabilities
	for _, deny := range admin.Denies {
		if deny == CapAll {
			// If everything is denied, return empty
			return []Capability{}, nil
		}
		
		// Remove wildcard denials
		if strings.HasSuffix(string(deny), ".*") {
			domain := strings.TrimSuffix(string(deny), ".*")
			if domainCaps, exists := AllCapabilities[domain]; exists {
				for _, domainCap := range domainCaps {
					delete(capabilitySet, domainCap)
				}
			}
		} else {
			delete(capabilitySet, deny)
		}
	}

	// Convert to slice
	result := make([]Capability, 0, len(capabilitySet))
	for cap := range capabilitySet {
		result = append(result, cap)
	}

	return result, nil
}

// RequireCapability returns HTTP middleware that checks for a specific capability.
func (c *Checker) RequireCapability(cap Capability) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Extract identity ID from context (set by auth middleware)
			identityID, ok := GetIdentityIDFromContext(r.Context())
			if !ok {
				http.Error(w, "Authentication required", http.StatusUnauthorized)
				return
			}

			// Check capability
			if !c.HasCapability(r.Context(), identityID, cap) {
				http.Error(w, "Insufficient permissions", http.StatusForbidden)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// RequireAnyCapability returns HTTP middleware that checks for any of the specified capabilities.
func (c *Checker) RequireAnyCapability(caps ...Capability) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Extract identity ID from context (set by auth middleware)
			identityID, ok := GetIdentityIDFromContext(r.Context())
			if !ok {
				http.Error(w, "Authentication required", http.StatusUnauthorized)
				return
			}

			// Check if user has any of the required capabilities
			for _, cap := range caps {
				if c.HasCapability(r.Context(), identityID, cap) {
					next.ServeHTTP(w, r)
					return
				}
			}

			http.Error(w, "Insufficient permissions", http.StatusForbidden)
		})
	}
}

// RequireAllCapabilities returns HTTP middleware that checks for all specified capabilities.
func (c *Checker) RequireAllCapabilities(caps ...Capability) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Extract identity ID from context (set by auth middleware)
			identityID, ok := GetIdentityIDFromContext(r.Context())
			if !ok {
				http.Error(w, "Authentication required", http.StatusUnauthorized)
				return
			}

			// Check if user has all required capabilities
			for _, cap := range caps {
				if !c.HasCapability(r.Context(), identityID, cap) {
					http.Error(w, "Insufficient permissions", http.StatusForbidden)
					return
				}
			}

			next.ServeHTTP(w, r)
		})
	}
}

// Role management operations

// CreateRole creates a new role.
func (c *Checker) CreateRole(ctx context.Context, role *Role) error {
	// Validate role
	if role.ID == "" {
		return ErrInvalidRole
	}
	if role.Name == "" {
		return ErrInvalidRole
	}

	// Validate capabilities
	for _, cap := range role.Capabilities {
		if !c.isValidCapability(cap) {
			return ErrInvalidCapability
		}
	}

	return c.store.CreateRole(ctx, role)
}

// UpdateRole updates an existing role.
func (c *Checker) UpdateRole(ctx context.Context, role *Role) error {
	// Cannot modify system roles
	existing, err := c.store.GetRole(ctx, role.ID)
	if err != nil {
		return err
	}
	if existing.IsSystem {
		return ErrCannotModifySystemRole
	}

	// Validate capabilities
	for _, cap := range role.Capabilities {
		if !c.isValidCapability(cap) {
			return ErrInvalidCapability
		}
	}

	return c.store.UpdateRole(ctx, role)
}

// DeleteRole deletes a role.
func (c *Checker) DeleteRole(ctx context.Context, roleID string) error {
	// Cannot delete system roles
	existing, err := c.store.GetRole(ctx, roleID)
	if err != nil {
		return err
	}
	if existing.IsSystem {
		return ErrCannotDeleteSystemRole
	}

	return c.store.DeleteRole(ctx, roleID)
}

// isValidCapability checks if a capability is valid.
func (c *Checker) isValidCapability(cap Capability) bool {
	// Check if it's a defined capability
	if _, exists := AllCapabilityInfo[cap]; exists {
		return true
	}

	// Check if it's a valid wildcard
	capStr := string(cap)
	if strings.HasSuffix(capStr, ".*") {
		domain := strings.TrimSuffix(capStr, ".*")
		if _, exists := AllCapabilities[domain]; exists {
			return true
		}
	}

	return false
}

// Admin identity management

// GrantCapabilities grants capabilities to an admin identity.
func (c *Checker) GrantCapabilities(ctx context.Context, identityID uuid.UUID, caps []Capability) error {
	admin, err := c.store.GetAdminIdentity(ctx, identityID)
	if err != nil {
		return err
	}

	// Add new grants
	grantSet := make(map[Capability]bool)
	for _, existing := range admin.Grants {
		grantSet[existing] = true
	}
	for _, cap := range caps {
		if c.isValidCapability(cap) {
			grantSet[cap] = true
		}
	}

	// Convert back to slice
	admin.Grants = make([]Capability, 0, len(grantSet))
	for cap := range grantSet {
		admin.Grants = append(admin.Grants, cap)
	}

	return c.store.UpdateAdminIdentity(ctx, admin)
}

// RevokeCapabilities revokes capabilities from an admin identity.
func (c *Checker) RevokeCapabilities(ctx context.Context, identityID uuid.UUID, caps []Capability) error {
	admin, err := c.store.GetAdminIdentity(ctx, identityID)
	if err != nil {
		return err
	}

	// Remove from grants
	newGrants := make([]Capability, 0)
	for _, existing := range admin.Grants {
		keep := true
		for _, revoke := range caps {
			if existing == revoke {
				keep = false
				break
			}
		}
		if keep {
			newGrants = append(newGrants, existing)
		}
	}
	admin.Grants = newGrants

	return c.store.UpdateAdminIdentity(ctx, admin)
}

// DenyCapabilities explicitly denies capabilities to an admin identity.
func (c *Checker) DenyCapabilities(ctx context.Context, identityID uuid.UUID, caps []Capability) error {
	admin, err := c.store.GetAdminIdentity(ctx, identityID)
	if err != nil {
		return err
	}

	// Add new denies
	denySet := make(map[Capability]bool)
	for _, existing := range admin.Denies {
		denySet[existing] = true
	}
	for _, cap := range caps {
		if c.isValidCapability(cap) {
			denySet[cap] = true
		}
	}

	// Convert back to slice
	admin.Denies = make([]Capability, 0, len(denySet))
	for cap := range denySet {
		admin.Denies = append(admin.Denies, cap)
	}

	return c.store.UpdateAdminIdentity(ctx, admin)
}

// AssignRoles assigns roles to an admin identity.
func (c *Checker) AssignRoles(ctx context.Context, identityID uuid.UUID, roleIDs []string) error {
	admin, err := c.store.GetAdminIdentity(ctx, identityID)
	if err != nil {
		return err
	}

	// Validate roles exist
	_, err = c.store.GetRoles(ctx, roleIDs)
	if err != nil {
		return err
	}

	// Add new roles
	roleSet := make(map[string]bool)
	for _, existing := range admin.Roles {
		roleSet[existing] = true
	}
	for _, roleID := range roleIDs {
		roleSet[roleID] = true
	}

	// Convert back to slice
	admin.Roles = make([]string, 0, len(roleSet))
	for roleID := range roleSet {
		admin.Roles = append(admin.Roles, roleID)
	}

	return c.store.UpdateAdminIdentity(ctx, admin)
}

// RemoveRoles removes roles from an admin identity.
func (c *Checker) RemoveRoles(ctx context.Context, identityID uuid.UUID, roleIDs []string) error {
	admin, err := c.store.GetAdminIdentity(ctx, identityID)
	if err != nil {
		return err
	}

	// Remove roles
	newRoles := make([]string, 0)
	for _, existing := range admin.Roles {
		keep := true
		for _, remove := range roleIDs {
			if existing == remove {
				keep = false
				break
			}
		}
		if keep {
			newRoles = append(newRoles, existing)
		}
	}
	admin.Roles = newRoles

	return c.store.UpdateAdminIdentity(ctx, admin)
}