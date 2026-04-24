package rbac

import (
	"context"
	"errors"
	"sync"
)

// Role represents a user role in the system
type Role string

// Permission represents a specific action permission
type Permission string

const (
	// Roles
	RoleAdmin   Role = "admin"
	RoleAnalyst Role = "analyst"
	RoleViewer  Role = "viewer"
	RoleUser    Role = "user"

	// Permissions
	PermViewEvents         Permission = "view_events"
	PermExportData         Permission = "export_data"
	PermManageWebhooks     Permission = "manage_webhooks"
	PermManageDashboards   Permission = "manage_dashboards"
	PermManageAudit        Permission = "manage_audit"
	PermManageUsers        Permission = "manage_users"
	PermManageRoles        Permission = "manage_roles"
	PermViewAudit          Permission = "view_audit"
	PermViewDashboards     Permission = "view_dashboards"
	PermModifyQueries      Permission = "modify_queries"
)

var (
	ErrUnauthorized     = errors.New("unauthorized")
	ErrForbidden        = errors.New("forbidden")
	ErrInvalidRole      = errors.New("invalid role")
	ErrInvalidPermission = errors.New("invalid permission")
)

// RolePermissionMap defines permissions for each role
var rolePermissionMap = map[Role][]Permission{
	RoleAdmin: {
		PermViewEvents,
		PermExportData,
		PermManageWebhooks,
		PermManageDashboards,
		PermManageAudit,
		PermManageUsers,
		PermManageRoles,
		PermViewAudit,
		PermViewDashboards,
		PermModifyQueries,
	},
	RoleAnalyst: {
		PermViewEvents,
		PermExportData,
		PermManageWebhooks,
		PermManageDashboards,
		PermViewAudit,
		PermViewDashboards,
		PermModifyQueries,
	},
	RoleViewer: {
		PermViewEvents,
		PermViewDashboards,
		PermViewAudit,
	},
	RoleUser: {
		PermViewEvents,
		PermViewDashboards,
	},
}

// Manager handles role and permission management
type Manager struct {
	mu                 sync.RWMutex
	userRoles          map[string]Role          // userID -> Role
	userCustomPerms    map[string][]Permission  // userID -> custom permissions
	resourceOwnership  map[string]string        // resourceID -> ownerUserID
	dashboardOwnership map[string]string        // dashboardID -> ownerUserID
	webhookOwnership   map[string]string        // webhookID -> ownerUserID
}

// NewManager creates a new RBAC manager
func NewManager() *Manager {
	return &Manager{
		userRoles:          make(map[string]Role),
		userCustomPerms:    make(map[string][]Permission),
		resourceOwnership:  make(map[string]string),
		dashboardOwnership: make(map[string]string),
		webhookOwnership:   make(map[string]string),
	}
}

// SetUserRole sets the role for a user
func (m *Manager) SetUserRole(userID string, role Role) error {
	if !isValidRole(role) {
		return ErrInvalidRole
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	m.userRoles[userID] = role
	// Clear custom permissions when role is set
	delete(m.userCustomPerms, userID)

	return nil
}

// GetUserRole returns the role for a user
func (m *Manager) GetUserRole(userID string) (Role, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	role, exists := m.userRoles[userID]
	if !exists {
		return RoleUser, nil // Default role
	}

	return role, nil
}

// HasPermission checks if a user has a specific permission
func (m *Manager) HasPermission(userID string, perm Permission) (bool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Check custom permissions first
	if customPerms, ok := m.userCustomPerms[userID]; ok {
		for _, p := range customPerms {
			if p == perm {
				return true, nil
			}
		}
	}

	// Check role-based permissions
	role := m.userRoles[userID]
	if role == "" {
		role = RoleUser // Default role
	}

	if !isValidRole(role) {
		return false, ErrInvalidRole
	}

	permissions := rolePermissionMap[role]
	for _, p := range permissions {
		if p == perm {
			return true, nil
		}
	}

	return false, nil
}

// GrantPermission grants a specific permission to a user (overrides role)
func (m *Manager) GrantPermission(userID string, perm Permission) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, ok := m.userCustomPerms[userID]; !ok {
		m.userCustomPerms[userID] = []Permission{}
	}

	// Check if already granted
	for _, p := range m.userCustomPerms[userID] {
		if p == perm {
			return nil
		}
	}

	m.userCustomPerms[userID] = append(m.userCustomPerms[userID], perm)
	return nil
}

// RevokePermission revokes a specific permission from a user
func (m *Manager) RevokePermission(userID string, perm Permission) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	customPerms, ok := m.userCustomPerms[userID]
	if !ok {
		return nil
	}

	newPerms := []Permission{}
	for _, p := range customPerms {
		if p != perm {
			newPerms = append(newPerms, p)
		}
	}

	m.userCustomPerms[userID] = newPerms
	return nil
}

// SetDashboardOwner sets the owner of a dashboard
func (m *Manager) SetDashboardOwner(dashboardID, userID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.dashboardOwnership[dashboardID] = userID
	return nil
}

// CanAccessDashboard checks if a user can access a dashboard
func (m *Manager) CanAccessDashboard(userID, dashboardID string) (bool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Admin can access all dashboards
	if role, ok := m.userRoles[userID]; ok && role == RoleAdmin {
		return true, nil
	}

	// Check ownership
	if owner, ok := m.dashboardOwnership[dashboardID]; ok && owner == userID {
		return true, nil
	}

	// Check if dashboard is shared (would be in separate structure in full implementation)
	return false, nil
}

// CanModifyDashboard checks if a user can modify a dashboard
func (m *Manager) CanModifyDashboard(userID, dashboardID string) (bool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Admin can modify all dashboards
	if role, ok := m.userRoles[userID]; ok && role == RoleAdmin {
		return true, nil
	}

	// Only owner can modify
	if owner, ok := m.dashboardOwnership[dashboardID]; ok && owner == userID {
		return true, nil
	}

	return false, nil
}

// SetWebhookOwner sets the owner of a webhook
func (m *Manager) SetWebhookOwner(webhookID, userID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.webhookOwnership[webhookID] = userID
	return nil
}

// CanAccessWebhook checks if a user can access a webhook
func (m *Manager) CanAccessWebhook(userID, webhookID string) (bool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Admin can access all webhooks
	if role, ok := m.userRoles[userID]; ok && role == RoleAdmin {
		return true, nil
	}

	// Check ownership
	if owner, ok := m.webhookOwnership[webhookID]; ok && owner == userID {
		return true, nil
	}

	return false, nil
}

// CanModifyWebhook checks if a user can modify a webhook
func (m *Manager) CanModifyWebhook(userID, webhookID string) (bool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Admin can modify all webhooks
	if role, ok := m.userRoles[userID]; ok && role == RoleAdmin {
		return true, nil
	}

	// Only owner can modify
	if owner, ok := m.webhookOwnership[webhookID]; ok && owner == userID {
		return true, nil
	}

	return false, nil
}

// Helper function to validate role
func isValidRole(role Role) bool {
	switch role {
	case RoleAdmin, RoleAnalyst, RoleViewer, RoleUser:
		return true
	default:
		return false
	}
}

// Context key for RBAC manager
type contextKey string

const rbacManagerContextKey contextKey = "rbac_manager"

// FromContext retrieves the RBAC manager from context
func FromContext(ctx context.Context) *Manager {
	manager, ok := ctx.Value(rbacManagerContextKey).(*Manager)
	if !ok {
		return NewManager()
	}
	return manager
}

// WithManager adds the RBAC manager to context
func WithManager(ctx context.Context, manager *Manager) context.Context {
	return context.WithValue(ctx, rbacManagerContextKey, manager)
}
