package security

import (
	"testing"

	"github.com/aegion/aegion/modules/analytics/rbac"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRBAC_AdminCanAccessAll verifies admin has all permissions
func TestRBAC_AdminCanAccessAll(t *testing.T) {
	manager := rbac.NewManager()

	// Set user as admin
	err := manager.SetUserRole("admin_user", rbac.RoleAdmin)
	require.NoError(t, err)

	// Admin should be able to:
	// - View events
	isAllowed, err := manager.HasPermission("admin_user", rbac.PermViewEvents)
	require.NoError(t, err)
	assert.True(t, isAllowed, "admin should have view_events permission")

	// - Export data
	isAllowed, err = manager.HasPermission("admin_user", rbac.PermExportData)
	require.NoError(t, err)
	assert.True(t, isAllowed, "admin should have export_data permission")

	// - Manage webhooks
	isAllowed, err = manager.HasPermission("admin_user", rbac.PermManageWebhooks)
	require.NoError(t, err)
	assert.True(t, isAllowed, "admin should have manage_webhooks permission")

	// - Manage dashboards
	isAllowed, err = manager.HasPermission("admin_user", rbac.PermManageDashboards)
	require.NoError(t, err)
	assert.True(t, isAllowed, "admin should have manage_dashboards permission")

	// - Manage audit
	isAllowed, err = manager.HasPermission("admin_user", rbac.PermManageAudit)
	require.NoError(t, err)
	assert.True(t, isAllowed, "admin should have manage_audit permission")

	// - Manage users
	isAllowed, err = manager.HasPermission("admin_user", rbac.PermManageUsers)
	require.NoError(t, err)
	assert.True(t, isAllowed, "admin should have manage_users permission")

	// - Manage roles
	isAllowed, err = manager.HasPermission("admin_user", rbac.PermManageRoles)
	require.NoError(t, err)
	assert.True(t, isAllowed, "admin should have manage_roles permission")

	// - View audit logs
	isAllowed, err = manager.HasPermission("admin_user", rbac.PermViewAudit)
	require.NoError(t, err)
	assert.True(t, isAllowed, "admin should have view_audit permission")

	// - View dashboards
	isAllowed, err = manager.HasPermission("admin_user", rbac.PermViewDashboards)
	require.NoError(t, err)
	assert.True(t, isAllowed, "admin should have view_dashboards permission")

	// - Modify queries
	isAllowed, err = manager.HasPermission("admin_user", rbac.PermModifyQueries)
	require.NoError(t, err)
	assert.True(t, isAllowed, "admin should have modify_queries permission")

	t.Logf("✓ Admin RBAC verified: all permissions granted")
}

// TestRBAC_AnalystCanQueryNotDelete verifies analyst can query but not delete
func TestRBAC_AnalystCanQueryNotDelete(t *testing.T) {
	manager := rbac.NewManager()

	// Set user as analyst
	err := manager.SetUserRole("analyst_user", rbac.RoleAnalyst)
	require.NoError(t, err)

	// Analyst should be able to:
	// - View events
	isAllowed, err := manager.HasPermission("analyst_user", rbac.PermViewEvents)
	require.NoError(t, err)
	assert.True(t, isAllowed, "analyst should have view_events permission")

	// - Export data
	isAllowed, err = manager.HasPermission("analyst_user", rbac.PermExportData)
	require.NoError(t, err)
	assert.True(t, isAllowed, "analyst should have export_data permission")

	// - Manage dashboards
	isAllowed, err = manager.HasPermission("analyst_user", rbac.PermManageDashboards)
	require.NoError(t, err)
	assert.True(t, isAllowed, "analyst should have manage_dashboards permission")

	// - Manage webhooks
	isAllowed, err = manager.HasPermission("analyst_user", rbac.PermManageWebhooks)
	require.NoError(t, err)
	assert.True(t, isAllowed, "analyst should have manage_webhooks permission")

	// - Modify queries
	isAllowed, err = manager.HasPermission("analyst_user", rbac.PermModifyQueries)
	require.NoError(t, err)
	assert.True(t, isAllowed, "analyst should have modify_queries permission")

	// Analyst should NOT be able to:
	// - Manage users
	isAllowed, err = manager.HasPermission("analyst_user", rbac.PermManageUsers)
	require.NoError(t, err)
	assert.False(t, isAllowed, "analyst should NOT have manage_users permission")

	// - Manage roles
	isAllowed, err = manager.HasPermission("analyst_user", rbac.PermManageRoles)
	require.NoError(t, err)
	assert.False(t, isAllowed, "analyst should NOT have manage_roles permission")

	// - Manage audit (write)
	isAllowed, err = manager.HasPermission("analyst_user", rbac.PermManageAudit)
	require.NoError(t, err)
	assert.False(t, isAllowed, "analyst should NOT have manage_audit permission")

	t.Logf("✓ Analyst RBAC verified: can query but not manage")
}

// TestRBAC_ViewerReadOnly verifies viewer has read-only access
func TestRBAC_ViewerReadOnly(t *testing.T) {
	manager := rbac.NewManager()

	// Set user as viewer
	err := manager.SetUserRole("viewer_user", rbac.RoleViewer)
	require.NoError(t, err)

	// Viewer should be able to:
	// - View events (read-only)
	isAllowed, err := manager.HasPermission("viewer_user", rbac.PermViewEvents)
	require.NoError(t, err)
	assert.True(t, isAllowed, "viewer should have view_events permission")

	// - View dashboards (read-only)
	isAllowed, err = manager.HasPermission("viewer_user", rbac.PermViewDashboards)
	require.NoError(t, err)
	assert.True(t, isAllowed, "viewer should have view_dashboards permission")

	// - View audit (read-only)
	isAllowed, err = manager.HasPermission("viewer_user", rbac.PermViewAudit)
	require.NoError(t, err)
	assert.True(t, isAllowed, "viewer should have view_audit permission")

	// Viewer should NOT be able to:
	// - Export data
	isAllowed, err = manager.HasPermission("viewer_user", rbac.PermExportData)
	require.NoError(t, err)
	assert.False(t, isAllowed, "viewer should NOT have export_data permission")

	// - Manage dashboards
	isAllowed, err = manager.HasPermission("viewer_user", rbac.PermManageDashboards)
	require.NoError(t, err)
	assert.False(t, isAllowed, "viewer should NOT have manage_dashboards permission")

	// - Manage webhooks
	isAllowed, err = manager.HasPermission("viewer_user", rbac.PermManageWebhooks)
	require.NoError(t, err)
	assert.False(t, isAllowed, "viewer should NOT have manage_webhooks permission")

	// - Modify queries
	isAllowed, err = manager.HasPermission("viewer_user", rbac.PermModifyQueries)
	require.NoError(t, err)
	assert.False(t, isAllowed, "viewer should NOT have modify_queries permission")

	// - Manage audit (write)
	isAllowed, err = manager.HasPermission("viewer_user", rbac.PermManageAudit)
	require.NoError(t, err)
	assert.False(t, isAllowed, "viewer should NOT have manage_audit permission")

	// - Manage users
	isAllowed, err = manager.HasPermission("viewer_user", rbac.PermManageUsers)
	require.NoError(t, err)
	assert.False(t, isAllowed, "viewer should NOT have manage_users permission")

	// - Manage roles
	isAllowed, err = manager.HasPermission("viewer_user", rbac.PermManageRoles)
	require.NoError(t, err)
	assert.False(t, isAllowed, "viewer should NOT have manage_roles permission")

	t.Logf("✓ Viewer RBAC verified: read-only access only")
}

// TestRBAC_UnauthenticatedRejected verifies unauthenticated users are rejected
func TestRBAC_UnauthenticatedRejected(t *testing.T) {
	manager := rbac.NewManager()

	// Unauthenticated user (no role set) should have NO permissions
	isAllowed, err := manager.HasPermission("unknown_user", rbac.PermViewEvents)
	require.NoError(t, err)
	assert.False(t, isAllowed, "unauthenticated user should NOT have view_events permission")

	isAllowed, err = manager.HasPermission("unknown_user", rbac.PermViewDashboards)
	require.NoError(t, err)
	assert.False(t, isAllowed, "unauthenticated user should NOT have view_dashboards permission")

	isAllowed, err = manager.HasPermission("unknown_user", rbac.PermExportData)
	require.NoError(t, err)
	assert.False(t, isAllowed, "unauthenticated user should NOT have export_data permission")

	t.Logf("✓ Unauthenticated access rejected: no permissions granted")
}

// TestRBAC_RoleSwitching verifies users can change roles
func TestRBAC_RoleSwitching(t *testing.T) {
	manager := rbac.NewManager()

	userID := "switchable_user"

	// Start as viewer
	err := manager.SetUserRole(userID, rbac.RoleViewer)
	require.NoError(t, err)

	isAllowed, err := manager.HasPermission(userID, rbac.PermExportData)
	require.NoError(t, err)
	assert.False(t, isAllowed, "viewer should not have export permission")

	// Upgrade to analyst
	err = manager.SetUserRole(userID, rbac.RoleAnalyst)
	require.NoError(t, err)

	isAllowed, err = manager.HasPermission(userID, rbac.PermExportData)
	require.NoError(t, err)
	assert.True(t, isAllowed, "analyst should have export permission")

	// Upgrade to admin
	err = manager.SetUserRole(userID, rbac.RoleAdmin)
	require.NoError(t, err)

	isAllowed, err = manager.HasPermission(userID, rbac.PermManageUsers)
	require.NoError(t, err)
	assert.True(t, isAllowed, "admin should have manage_users permission")

	// Downgrade to viewer
	err = manager.SetUserRole(userID, rbac.RoleViewer)
	require.NoError(t, err)

	isAllowed, err = manager.HasPermission(userID, rbac.PermManageUsers)
	require.NoError(t, err)
	assert.False(t, isAllowed, "viewer should not have manage_users permission after downgrade")

	t.Logf("✓ Role switching verified: viewer → analyst → admin → viewer")
}

// TestRBAC_CustomPermissions verifies custom permissions can be added
func TestRBAC_CustomPermissions(t *testing.T) {
	manager := rbac.NewManager()

	userID := "custom_user"

	// Start with viewer role (limited permissions)
	err := manager.SetUserRole(userID, rbac.RoleViewer)
	require.NoError(t, err)

	isAllowed, err := manager.HasPermission(userID, rbac.PermExportData)
	require.NoError(t, err)
	assert.False(t, isAllowed, "viewer should not have export permission by default")

	// Add custom permission
	err = manager.GrantPermission(userID, rbac.PermExportData)
	require.NoError(t, err)

	isAllowed, err = manager.HasPermission(userID, rbac.PermExportData)
	require.NoError(t, err)
	assert.True(t, isAllowed, "viewer should have export permission after grant")

	// Remove custom permission
	err = manager.RevokePermission(userID, rbac.PermExportData)
	require.NoError(t, err)

	isAllowed, err = manager.HasPermission(userID, rbac.PermExportData)
	require.NoError(t, err)
	assert.False(t, isAllowed, "viewer should not have export permission after revoke")

	t.Logf("✓ Custom permissions verified: grant/revoke works")
}
