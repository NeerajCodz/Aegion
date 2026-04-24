package rbac

import (
	"testing"
)

func TestRoleValidation(t *testing.T) {
	tests := []struct {
		role  Role
		valid bool
	}{
		{RoleAdmin, true},
		{RoleAnalyst, true},
		{RoleViewer, true},
		{RoleUser, true},
		{Role("invalid"), false},
	}

	for _, tt := range tests {
		t.Run(string(tt.role), func(t *testing.T) {
			result := isValidRole(tt.role)
			if result != tt.valid {
				t.Errorf("isValidRole(%s) = %v, want %v", tt.role, result, tt.valid)
			}
		})
	}
}

func TestSetAndGetUserRole(t *testing.T) {
	manager := NewManager()

	err := manager.SetUserRole("user1", RoleAnalyst)
	if err != nil {
		t.Fatalf("SetUserRole failed: %v", err)
	}

	role, err := manager.GetUserRole("user1")
	if err != nil {
		t.Fatalf("GetUserRole failed: %v", err)
	}

	if role != RoleAnalyst {
		t.Errorf("GetUserRole returned %s, want %s", role, RoleAnalyst)
	}
}

func TestSetUserRoleInvalid(t *testing.T) {
	manager := NewManager()

	err := manager.SetUserRole("user1", Role("invalid"))
	if err == nil {
		t.Error("SetUserRole should fail with invalid role")
	}
}

func TestHasPermission(t *testing.T) {
	manager := NewManager()

	// Admin should have all permissions
	manager.SetUserRole("admin", RoleAdmin)
	has, _ := manager.HasPermission("admin", PermViewEvents)
	if !has {
		t.Error("Admin should have PermViewEvents")
	}

	// Viewer should have limited permissions
	manager.SetUserRole("viewer", RoleViewer)
	has, _ = manager.HasPermission("viewer", PermViewEvents)
	if !has {
		t.Error("Viewer should have PermViewEvents")
	}

	has, _ = manager.HasPermission("viewer", PermManageUsers)
	if has {
		t.Error("Viewer should not have PermManageUsers")
	}
}

func TestGrantAndRevokePermission(t *testing.T) {
	manager := NewManager()
	manager.SetUserRole("user1", RoleViewer)

	// Viewer doesn't have export permission
	has, _ := manager.HasPermission("user1", PermExportData)
	if has {
		t.Error("Viewer should not have PermExportData initially")
	}

	// Grant permission
	manager.GrantPermission("user1", PermExportData)
	has, _ = manager.HasPermission("user1", PermExportData)
	if !has {
		t.Error("User should have PermExportData after grant")
	}

	// Revoke permission
	manager.RevokePermission("user1", PermExportData)
	has, _ = manager.HasPermission("user1", PermExportData)
	if has {
		t.Error("User should not have PermExportData after revoke")
	}
}

func TestDashboardOwnership(t *testing.T) {
	manager := NewManager()

	manager.SetDashboardOwner("dash1", "user1")
	manager.SetUserRole("user1", RoleUser)
	manager.SetUserRole("user2", RoleUser)

	// Owner can access
	can, _ := manager.CanAccessDashboard("user1", "dash1")
	if !can {
		t.Error("Dashboard owner should be able to access")
	}

	// Non-owner cannot access
	can, _ = manager.CanAccessDashboard("user2", "dash1")
	if can {
		t.Error("Non-owner should not be able to access")
	}

	// Admin can access
	manager.SetUserRole("admin", RoleAdmin)
	can, _ = manager.CanAccessDashboard("admin", "dash1")
	if !can {
		t.Error("Admin should be able to access any dashboard")
	}
}

func TestDashboardModification(t *testing.T) {
	manager := NewManager()

	manager.SetDashboardOwner("dash1", "user1")
	manager.SetUserRole("user1", RoleUser)
	manager.SetUserRole("user2", RoleUser)

	// Owner can modify
	can, _ := manager.CanModifyDashboard("user1", "dash1")
	if !can {
		t.Error("Dashboard owner should be able to modify")
	}

	// Non-owner cannot modify
	can, _ = manager.CanModifyDashboard("user2", "dash1")
	if can {
		t.Error("Non-owner should not be able to modify")
	}
}

func TestWebhookOwnership(t *testing.T) {
	manager := NewManager()

	manager.SetWebhookOwner("hook1", "user1")
	manager.SetUserRole("user1", RoleUser)
	manager.SetUserRole("user2", RoleUser)

	// Owner can access
	can, _ := manager.CanAccessWebhook("user1", "hook1")
	if !can {
		t.Error("Webhook owner should be able to access")
	}

	// Non-owner cannot access
	can, _ = manager.CanAccessWebhook("user2", "hook1")
	if can {
		t.Error("Non-owner should not be able to access")
	}
}

func TestWebhookModification(t *testing.T) {
	manager := NewManager()

	manager.SetWebhookOwner("hook1", "user1")
	manager.SetUserRole("user1", RoleUser)
	manager.SetUserRole("user2", RoleUser)

	// Owner can modify
	can, _ := manager.CanModifyWebhook("user1", "hook1")
	if !can {
		t.Error("Webhook owner should be able to modify")
	}

	// Non-owner cannot modify
	can, _ = manager.CanModifyWebhook("user2", "hook1")
	if can {
		t.Error("Non-owner should not be able to modify")
	}
}

func TestDefaultRole(t *testing.T) {
	manager := NewManager()

	// User not in system should have default role
	role, _ := manager.GetUserRole("unknown_user")
	if role != RoleUser {
		t.Errorf("Unknown user should have default role %s, got %s", RoleUser, role)
	}
}
