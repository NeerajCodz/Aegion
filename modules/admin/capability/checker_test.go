package capability

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// MockStore is a mock implementation of the capability Store interface.
type MockStore struct {
	mock.Mock
}

func (m *MockStore) GetRole(ctx context.Context, id string) (*Role, error) {
	args := m.Called(ctx, id)
	return args.Get(0).(*Role), args.Error(1)
}

func (m *MockStore) GetRoles(ctx context.Context, ids []string) ([]*Role, error) {
	args := m.Called(ctx, ids)
	return args.Get(0).([]*Role), args.Error(1)
}

func (m *MockStore) ListRoles(ctx context.Context) ([]*Role, error) {
	args := m.Called(ctx)
	return args.Get(0).([]*Role), args.Error(1)
}

func (m *MockStore) CreateRole(ctx context.Context, role *Role) error {
	args := m.Called(ctx, role)
	return args.Error(0)
}

func (m *MockStore) UpdateRole(ctx context.Context, role *Role) error {
	args := m.Called(ctx, role)
	return args.Error(0)
}

func (m *MockStore) DeleteRole(ctx context.Context, id string) error {
	args := m.Called(ctx, id)
	return args.Error(0)
}

func (m *MockStore) GetAdminIdentity(ctx context.Context, identityID uuid.UUID) (*AdminIdentity, error) {
	args := m.Called(ctx, identityID)
	return args.Get(0).(*AdminIdentity), args.Error(1)
}

func (m *MockStore) CreateAdminIdentity(ctx context.Context, admin *AdminIdentity) error {
	args := m.Called(ctx, admin)
	return args.Error(0)
}

func (m *MockStore) UpdateAdminIdentity(ctx context.Context, admin *AdminIdentity) error {
	args := m.Called(ctx, admin)
	return args.Error(0)
}

func (m *MockStore) DeleteAdminIdentity(ctx context.Context, identityID uuid.UUID) error {
	args := m.Called(ctx, identityID)
	return args.Error(0)
}

func (m *MockStore) ListAdminIdentities(ctx context.Context) ([]*AdminIdentity, error) {
	args := m.Called(ctx)
	return args.Get(0).([]*AdminIdentity), args.Error(1)
}

// Test Checker Creation
func TestNewChecker(t *testing.T) {
	mockStore := &MockStore{}
	
	checker := NewChecker(mockStore)
	
	assert.NotNil(t, checker)
	assert.Equal(t, mockStore, checker.store)
}

// Test Capability Matching
func TestMatchesCapability(t *testing.T) {
	checker := &Checker{}
	
	// Test exact match
	assert.True(t, checker.matchesCapability(CapUsersRead, CapUsersRead))
	
	// Test global wildcard
	assert.True(t, checker.matchesCapability(CapAll, CapUsersRead))
	assert.True(t, checker.matchesCapability(CapAll, CapSystemConfig))
	
	// Test domain wildcard
	assert.True(t, checker.matchesCapability(CapUsersAll, CapUsersRead))
	assert.True(t, checker.matchesCapability(CapUsersAll, CapUsersCreate))
	assert.True(t, checker.matchesCapability(CapUsersAll, CapUsersDelete))
	
	// Test non-matching
	assert.False(t, checker.matchesCapability(CapUsersRead, CapUsersCreate))
	assert.False(t, checker.matchesCapability(CapUsersAll, CapSessionsRead))
}

// Test Capability Evaluation
func TestEvaluateCapability(t *testing.T) {
	mockStore := &MockStore{}
	checker := NewChecker(mockStore)
	ctx := context.Background()
	
	// Admin with explicit deny
	admin := &AdminIdentity{
		IdentityID: uuid.New(),
		Roles:      []string{"admin"},
		Grants:     []Capability{CapUsersRead},
		Denies:     []Capability{CapUsersDelete},
	}
	
	// Mock role
	adminRole := &Role{
		ID:   "admin",
		Name: "Administrator",
		Capabilities: []Capability{
			CapUsersAll,
			CapSessionsAll,
		},
	}
	
	mockStore.On("GetRoles", ctx, []string{"admin"}).Return([]*Role{adminRole}, nil)
	
	// Test explicit deny overrides everything
	assert.False(t, checker.evaluateCapability(ctx, admin, CapUsersDelete))
	
	// Test explicit grant
	assert.True(t, checker.evaluateCapability(ctx, admin, CapUsersRead))
	
	// Test role-based capability
	assert.True(t, checker.evaluateCapability(ctx, admin, CapUsersCreate))
	assert.True(t, checker.evaluateCapability(ctx, admin, CapSessionsRead))
	
	// Test denied capability
	assert.False(t, checker.evaluateCapability(ctx, admin, CapSystemConfig))
	
	mockStore.AssertExpectations(t)
}

func TestHasCapability(t *testing.T) {
	mockStore := &MockStore{}
	checker := NewChecker(mockStore)
	ctx := context.Background()
	identityID := uuid.New()
	
	admin := &AdminIdentity{
		IdentityID: identityID,
		Roles:      []string{"user_manager"},
		Grants:     []Capability{},
		Denies:     []Capability{},
	}
	
	userManagerRole := &Role{
		ID:   "user_manager",
		Name: "User Manager",
		Capabilities: []Capability{
			CapUsersAll,
			CapSessionsAll,
		},
	}
	
	mockStore.On("GetAdminIdentity", ctx, identityID).Return(admin, nil)
	mockStore.On("GetRoles", ctx, []string{"user_manager"}).Return([]*Role{userManagerRole}, nil)
	
	assert.True(t, checker.HasCapability(ctx, identityID, CapUsersRead))
	assert.True(t, checker.HasCapability(ctx, identityID, CapSessionsRevoke))
	assert.False(t, checker.HasCapability(ctx, identityID, CapSystemConfig))
	
	mockStore.AssertExpectations(t)
}

// Test Effective Capabilities
func TestGetEffectiveCapabilities(t *testing.T) {
	mockStore := &MockStore{}
	checker := NewChecker(mockStore)
	ctx := context.Background()
	identityID := uuid.New()
	
	admin := &AdminIdentity{
		IdentityID: identityID,
		Roles:      []string{"user_manager"},
		Grants:     []Capability{CapSystemAudit},
		Denies:     []Capability{CapUsersDelete},
	}
	
	userManagerRole := &Role{
		ID:   "user_manager",
		Name: "User Manager",
		Capabilities: []Capability{
			CapUsersAll,
			CapSessionsRead,
		},
	}
	
	mockStore.On("GetAdminIdentity", ctx, identityID).Return(admin, nil)
	mockStore.On("GetRoles", ctx, []string{"user_manager"}).Return([]*Role{userManagerRole}, nil)
	
	capabilities, err := checker.GetEffectiveCapabilities(ctx, identityID)
	
	assert.NoError(t, err)
	assert.NotEmpty(t, capabilities)
	
	// Should include role-based capabilities (expanded from users.*)
	assert.Contains(t, capabilities, CapUsersRead)
	assert.Contains(t, capabilities, CapUsersCreate)
	assert.Contains(t, capabilities, CapUsersUpdate)
	assert.Contains(t, capabilities, CapUsersSuspend)
	
	// Should include session capabilities
	assert.Contains(t, capabilities, CapSessionsRead)
	
	// Should include explicit grants
	assert.Contains(t, capabilities, CapSystemAudit)
	
	// Should NOT include denied capabilities
	assert.NotContains(t, capabilities, CapUsersDelete)
	
	mockStore.AssertExpectations(t)
}

func TestGetEffectiveCapabilitiesGlobalWildcard(t *testing.T) {
	mockStore := &MockStore{}
	checker := NewChecker(mockStore)
	ctx := context.Background()
	identityID := uuid.New()
	
	admin := &AdminIdentity{
		IdentityID: identityID,
		Roles:      []string{"super_admin"},
		Grants:     []Capability{},
		Denies:     []Capability{},
	}
	
	superAdminRole := &Role{
		ID:   "super_admin",
		Name: "Super Administrator",
		Capabilities: []Capability{
			CapAll,
		},
	}
	
	mockStore.On("GetAdminIdentity", ctx, identityID).Return(admin, nil)
	mockStore.On("GetRoles", ctx, []string{"super_admin"}).Return([]*Role{superAdminRole}, nil)
	
	capabilities, err := checker.GetEffectiveCapabilities(ctx, identityID)
	
	assert.NoError(t, err)
	assert.Equal(t, len(AllCapabilityInfo), len(capabilities))
	
	mockStore.AssertExpectations(t)
}

// Test Role Management
func TestCreateRole(t *testing.T) {
	mockStore := &MockStore{}
	checker := NewChecker(mockStore)
	ctx := context.Background()
	
	role := &Role{
		ID:          "custom_role",
		Name:        "Custom Role",
		Description: "A custom role for testing",
		Capabilities: []Capability{
			CapUsersRead,
			CapSessionsRead,
		},
	}
	
	mockStore.On("CreateRole", ctx, role).Return(nil)
	
	err := checker.CreateRole(ctx, role)
	
	assert.NoError(t, err)
	mockStore.AssertExpectations(t)
}

func TestCreateRoleInvalid(t *testing.T) {
	mockStore := &MockStore{}
	checker := NewChecker(mockStore)
	ctx := context.Background()
	
	// Test missing ID
	role := &Role{
		Name: "Invalid Role",
	}
	
	err := checker.CreateRole(ctx, role)
	assert.ErrorIs(t, err, ErrInvalidRole)
	
	// Test missing name
	role = &Role{
		ID: "invalid",
	}
	
	err = checker.CreateRole(ctx, role)
	assert.ErrorIs(t, err, ErrInvalidRole)
	
	// Test invalid capability
	role = &Role{
		ID:   "invalid",
		Name: "Invalid Role",
		Capabilities: []Capability{
			"invalid.capability",
		},
	}
	
	err = checker.CreateRole(ctx, role)
	assert.ErrorIs(t, err, ErrInvalidCapability)
}

func TestUpdateRole(t *testing.T) {
	mockStore := &MockStore{}
	checker := NewChecker(mockStore)
	ctx := context.Background()
	
	existingRole := &Role{
		ID:       "custom_role",
		IsSystem: false,
	}
	
	updatedRole := &Role{
		ID:          "custom_role",
		Name:        "Updated Role",
		Description: "Updated description",
		Capabilities: []Capability{
			CapUsersRead,
			CapUsersCreate,
		},
	}
	
	mockStore.On("GetRole", ctx, "custom_role").Return(existingRole, nil)
	mockStore.On("UpdateRole", ctx, updatedRole).Return(nil)
	
	err := checker.UpdateRole(ctx, updatedRole)
	
	assert.NoError(t, err)
	mockStore.AssertExpectations(t)
}

func TestUpdateSystemRole(t *testing.T) {
	mockStore := &MockStore{}
	checker := NewChecker(mockStore)
	ctx := context.Background()
	
	systemRole := &Role{
		ID:       "admin",
		IsSystem: true,
	}
	
	updatedRole := &Role{
		ID:   "admin",
		Name: "Updated Admin",
	}
	
	mockStore.On("GetRole", ctx, "admin").Return(systemRole, nil)
	
	err := checker.UpdateRole(ctx, updatedRole)
	
	assert.ErrorIs(t, err, ErrCannotModifySystemRole)
	mockStore.AssertExpectations(t)
}

func TestDeleteRole(t *testing.T) {
	mockStore := &MockStore{}
	checker := NewChecker(mockStore)
	ctx := context.Background()
	
	customRole := &Role{
		ID:       "custom_role",
		IsSystem: false,
	}
	
	mockStore.On("GetRole", ctx, "custom_role").Return(customRole, nil)
	mockStore.On("DeleteRole", ctx, "custom_role").Return(nil)
	
	err := checker.DeleteRole(ctx, "custom_role")
	
	assert.NoError(t, err)
	mockStore.AssertExpectations(t)
}

func TestDeleteSystemRole(t *testing.T) {
	mockStore := &MockStore{}
	checker := NewChecker(mockStore)
	ctx := context.Background()
	
	systemRole := &Role{
		ID:       "admin",
		IsSystem: true,
	}
	
	mockStore.On("GetRole", ctx, "admin").Return(systemRole, nil)
	
	err := checker.DeleteRole(ctx, "admin")
	
	assert.ErrorIs(t, err, ErrCannotDeleteSystemRole)
	mockStore.AssertExpectations(t)
}

// Test Admin Identity Management
func TestGrantCapabilities(t *testing.T) {
	mockStore := &MockStore{}
	checker := NewChecker(mockStore)
	ctx := context.Background()
	identityID := uuid.New()
	
	admin := &AdminIdentity{
		IdentityID: identityID,
		Grants:     []Capability{CapUsersRead},
	}
	
	mockStore.On("GetAdminIdentity", ctx, identityID).Return(admin, nil)
	mockStore.On("UpdateAdminIdentity", ctx, mock.MatchedBy(func(a *AdminIdentity) bool {
		return len(a.Grants) == 2 && 
			   contains(a.Grants, CapUsersRead) && 
			   contains(a.Grants, CapUsersCreate)
	})).Return(nil)
	
	err := checker.GrantCapabilities(ctx, identityID, []Capability{CapUsersCreate})
	
	assert.NoError(t, err)
	mockStore.AssertExpectations(t)
}

func TestRevokeCapabilities(t *testing.T) {
	mockStore := &MockStore{}
	checker := NewChecker(mockStore)
	ctx := context.Background()
	identityID := uuid.New()
	
	admin := &AdminIdentity{
		IdentityID: identityID,
		Grants:     []Capability{CapUsersRead, CapUsersCreate},
	}
	
	mockStore.On("GetAdminIdentity", ctx, identityID).Return(admin, nil)
	mockStore.On("UpdateAdminIdentity", ctx, mock.MatchedBy(func(a *AdminIdentity) bool {
		return len(a.Grants) == 1 && contains(a.Grants, CapUsersRead)
	})).Return(nil)
	
	err := checker.RevokeCapabilities(ctx, identityID, []Capability{CapUsersCreate})
	
	assert.NoError(t, err)
	mockStore.AssertExpectations(t)
}

func TestAssignRoles(t *testing.T) {
	mockStore := &MockStore{}
	checker := NewChecker(mockStore)
	ctx := context.Background()
	identityID := uuid.New()
	
	admin := &AdminIdentity{
		IdentityID: identityID,
		Roles:      []string{"viewer"},
	}
	
	roles := []*Role{
		{ID: "user_manager", Name: "User Manager"},
	}
	
	mockStore.On("GetAdminIdentity", ctx, identityID).Return(admin, nil)
	mockStore.On("GetRoles", ctx, []string{"user_manager"}).Return(roles, nil)
	mockStore.On("UpdateAdminIdentity", ctx, mock.MatchedBy(func(a *AdminIdentity) bool {
		return len(a.Roles) == 2 && 
			   contains(a.Roles, "viewer") && 
			   contains(a.Roles, "user_manager")
	})).Return(nil)
	
	err := checker.AssignRoles(ctx, identityID, []string{"user_manager"})
	
	assert.NoError(t, err)
	mockStore.AssertExpectations(t)
}

// Test RemoveRoles
func TestRemoveRoles(t *testing.T) {
	mockStore := &MockStore{}
	checker := NewChecker(mockStore)
	ctx := context.Background()
	identityID := uuid.New()
	
	admin := &AdminIdentity{
		IdentityID: identityID,
		Roles:      []string{"user_manager", "auditor"},
	}
	
	mockStore.On("GetAdminIdentity", ctx, identityID).Return(admin, nil)
	mockStore.On("UpdateAdminIdentity", ctx, mock.MatchedBy(func(a *AdminIdentity) bool {
		return len(a.Roles) == 1 && contains(a.Roles, "auditor")
	})).Return(nil)
	
	err := checker.RemoveRoles(ctx, identityID, []string{"user_manager"})
	
	assert.NoError(t, err)
	mockStore.AssertExpectations(t)
}

// Test DenyCapabilities
func TestDenyCapabilities(t *testing.T) {
	mockStore := &MockStore{}
	checker := NewChecker(mockStore)
	ctx := context.Background()
	identityID := uuid.New()
	
	admin := &AdminIdentity{
		IdentityID: identityID,
		Denies:     []Capability{CapUsersDelete},
	}
	
	mockStore.On("GetAdminIdentity", ctx, identityID).Return(admin, nil)
	mockStore.On("UpdateAdminIdentity", ctx, mock.MatchedBy(func(a *AdminIdentity) bool {
		return len(a.Denies) == 2 && 
			   contains(a.Denies, CapUsersDelete) && 
			   contains(a.Denies, CapSystemConfig)
	})).Return(nil)
	
	err := checker.DenyCapabilities(ctx, identityID, []Capability{CapSystemConfig})
	
	assert.NoError(t, err)
	mockStore.AssertExpectations(t)
}

// Test Validation
func TestIsValidCapability(t *testing.T) {
	checker := &Checker{}
	
	// Test defined capabilities
	assert.True(t, checker.isValidCapability(CapUsersRead))
	assert.True(t, checker.isValidCapability(CapAll))
	
	// Test wildcards
	assert.True(t, checker.isValidCapability(CapUsersAll))
	assert.True(t, checker.isValidCapability(CapSystemAll))
	
	// Test invalid
	assert.False(t, checker.isValidCapability("invalid.capability"))
	assert.False(t, checker.isValidCapability("nonexistent.*"))
}

// Test Default Roles
func TestDefaultRoles(t *testing.T) {
	// Test that default roles are properly defined
	assert.NotEmpty(t, DefaultRoles)
	
	// Test super admin role
	superAdmin := DefaultRoles["super_admin"]
	assert.NotNil(t, superAdmin)
	assert.Equal(t, "Super Administrator", superAdmin.Name)
	assert.True(t, superAdmin.IsSystem)
	assert.Contains(t, superAdmin.Capabilities, CapAll)
	
	// Test admin role
	admin := DefaultRoles["admin"]
	assert.NotNil(t, admin)
	assert.Equal(t, "Administrator", admin.Name)
	assert.True(t, admin.IsSystem)
	assert.Contains(t, admin.Capabilities, CapUsersAll)
	
	// Test user manager role
	userManager := DefaultRoles["user_manager"]
	assert.NotNil(t, userManager)
	assert.Equal(t, "User Manager", userManager.Name)
	assert.True(t, userManager.IsSystem)
	assert.Contains(t, userManager.Capabilities, CapUsersAll)
}

// Test Capability Info
func TestAllCapabilityInfo(t *testing.T) {
	assert.NotEmpty(t, AllCapabilityInfo)
	
	// Test that all defined capabilities have info
	assert.Contains(t, AllCapabilityInfo, CapUsersRead)
	assert.Contains(t, AllCapabilityInfo, CapAll)
	
	// Test capability info structure
	info := AllCapabilityInfo[CapUsersRead]
	assert.Equal(t, CapUsersRead, info.Name)
	assert.Equal(t, "users", info.Domain)
	assert.NotEmpty(t, info.Description)
	assert.False(t, info.IsWildcard)
	
	// Test wildcard capability info
	wildcardInfo := AllCapabilityInfo[CapUsersAll]
	assert.True(t, wildcardInfo.IsWildcard)
}

// Helper functions
func contains[T comparable](slice []T, item T) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

// Benchmark tests
func BenchmarkHasCapability(b *testing.B) {
	mockStore := &MockStore{}
	checker := NewChecker(mockStore)
	ctx := context.Background()
	identityID := uuid.New()
	
	admin := &AdminIdentity{
		IdentityID: identityID,
		Roles:      []string{"admin"},
	}
	
	adminRole := &Role{
		ID:           "admin",
		Capabilities: []Capability{CapUsersAll},
	}
	
	mockStore.On("GetAdminIdentity", ctx, identityID).Return(admin, nil)
	mockStore.On("GetRoles", ctx, []string{"admin"}).Return([]*Role{adminRole}, nil)
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		checker.HasCapability(ctx, identityID, CapUsersRead)
	}
}

// ============================================================================
// COMPREHENSIVE EDGE CASE TESTS
// ============================================================================

// Test wildcard matching edge cases
func TestMatchesCapabilityWildcardEdgeCases(t *testing.T) {
	checker := &Checker{}
	
	// Test multiple domain wildcards matching the same specific capability
	assert.True(t, checker.matchesCapability(CapOAuth2All, CapOAuth2ClientsRead))
	assert.True(t, checker.matchesCapability(CapOAuth2All, CapOAuth2ClientsManage))
	assert.True(t, checker.matchesCapability(CapOAuth2All, CapOAuth2TokensRead))
	assert.True(t, checker.matchesCapability(CapOAuth2All, CapOAuth2TokensRevoke))
	
	// Test SCIM wildcards with nested structure
	assert.True(t, checker.matchesCapability(CapSCIMAll, CapSCIMUsersRead))
	assert.True(t, checker.matchesCapability(CapSCIMAll, CapSCIMUsersWrite))
	assert.True(t, checker.matchesCapability(CapSCIMAll, CapSCIMGroupsRead))
	assert.True(t, checker.matchesCapability(CapSCIMAll, CapSCIMGroupsWrite))
	assert.True(t, checker.matchesCapability(CapSCIMAll, CapSCIMTokensManage))
	
	// Test non-matching domain wildcards
	assert.False(t, checker.matchesCapability(CapUsersAll, CapOAuth2ClientsRead))
	assert.False(t, checker.matchesCapability(CapSessionsAll, CapMFARead))
	assert.False(t, checker.matchesCapability(CapPolicyAll, CapSystemConfig))
	assert.False(t, checker.matchesCapability(CapAdminTeamAll, CapUsersRead))
	
	// Test wildcard not matching different domains
	assert.False(t, checker.matchesCapability(CapUsersAll, CapSystemAudit))
	assert.False(t, checker.matchesCapability(CapMFAAll, CapUsersRead))
}

// Test permission layering: role + override + deny
func TestPermissionLayeringComplexScenarios(t *testing.T) {
	mockStore := &MockStore{}
	checker := NewChecker(mockStore)
	ctx := context.Background()
	
	// Scenario 1: Super admin with specific deny
	// Super admin has *, but explicitly deny system.config
	admin := &AdminIdentity{
		IdentityID: uuid.New(),
		Roles:      []string{"super_admin"},
		Grants:     []Capability{},
		Denies:     []Capability{CapSystemConfig},
	}
	
	superAdminRole := &Role{
		ID:   "super_admin",
		Name: "Super Administrator",
		Capabilities: []Capability{CapAll},
	}
	
	mockStore.On("GetRoles", ctx, []string{"super_admin"}).Return([]*Role{superAdminRole}, nil)
	
	// Should not have the denied capability
	assert.False(t, checker.evaluateCapability(ctx, admin, CapSystemConfig))
	// But should have others
	assert.True(t, checker.evaluateCapability(ctx, admin, CapUsersRead))
	assert.True(t, checker.evaluateCapability(ctx, admin, CapSystemAudit))
	
	mockStore.ExpectedCalls = nil
	
	// Scenario 2: Multiple roles + wildcard deny
	// User manager role has users.* and sessions.*
	// But explicitly deny users.delete
	admin2 := &AdminIdentity{
		IdentityID: uuid.New(),
		Roles:      []string{"user_manager"},
		Grants:     []Capability{},
		Denies:     []Capability{CapUsersDelete},
	}
	
	userManagerRole := &Role{
		ID:   "user_manager",
		Name: "User Manager",
		Capabilities: []Capability{CapUsersAll, CapSessionsAll},
	}
	
	mockStore.On("GetRoles", ctx, []string{"user_manager"}).Return([]*Role{userManagerRole}, nil)
	
	// Should not have denied capability
	assert.False(t, checker.evaluateCapability(ctx, admin2, CapUsersDelete))
	// But should have others from the same domain
	assert.True(t, checker.evaluateCapability(ctx, admin2, CapUsersRead))
	assert.True(t, checker.evaluateCapability(ctx, admin2, CapUsersCreate))
	// And from other role
	assert.True(t, checker.evaluateCapability(ctx, admin2, CapSessionsRead))
}

// Test deny overrides grant
func TestDenyOverridesGrant(t *testing.T) {
	mockStore := &MockStore{}
	checker := NewChecker(mockStore)
	ctx := context.Background()
	
	// Admin with explicit grant but also explicit deny (deny should win)
	admin := &AdminIdentity{
		IdentityID: uuid.New(),
		Roles:      []string{},
		Grants:     []Capability{CapUsersDelete},
		Denies:     []Capability{CapUsersDelete},
	}
	
	// Deny should override grant
	assert.False(t, checker.evaluateCapability(ctx, admin, CapUsersDelete))
}

// Test wildcard deny overrides specific grant
func TestWildcardDenyOverridesSpecificGrant(t *testing.T) {
	mockStore := &MockStore{}
	checker := NewChecker(mockStore)
	ctx := context.Background()
	
	// Admin with explicit grant for specific capability
	// But wildcard deny on the entire domain
	admin := &AdminIdentity{
		IdentityID: uuid.New(),
		Roles:      []string{},
		Grants:     []Capability{CapUsersRead, CapUsersCreate},
		Denies:     []Capability{CapUsersAll}, // Deny entire users domain
	}
	
	// Wildcard deny should override specific grants
	assert.False(t, checker.evaluateCapability(ctx, admin, CapUsersRead))
	assert.False(t, checker.evaluateCapability(ctx, admin, CapUsersCreate))
}

// Test deny wildcard overrides role with specific capability
func TestDenyWildcardOverridesRoleCapability(t *testing.T) {
	mockStore := &MockStore{}
	checker := NewChecker(mockStore)
	ctx := context.Background()
	
	// Admin has admin role (users.*) but deny wildcard on admin_team
	admin := &AdminIdentity{
		IdentityID: uuid.New(),
		Roles:      []string{"admin"},
		Grants:     []Capability{},
		Denies:     []Capability{CapAdminTeamAll},
	}
	
	adminRole := &Role{
		ID:   "admin",
		Name: "Administrator",
		Capabilities: []Capability{
			CapUsersAll,
			CapSessionsAll,
			CapAdminTeamRead,
		},
	}
	
	mockStore.On("GetRoles", ctx, []string{"admin"}).Return([]*Role{adminRole}, nil)
	
	// Users capabilities should still be available
	assert.True(t, checker.evaluateCapability(ctx, admin, CapUsersRead))
	// But admin_team should be denied
	assert.False(t, checker.evaluateCapability(ctx, admin, CapAdminTeamRead))
}

// Test no capabilities (empty admin)
func TestEvaluateCapabilityEmptyAdmin(t *testing.T) {
	mockStore := &MockStore{}
	checker := NewChecker(mockStore)
	ctx := context.Background()
	
	// Admin with no roles, grants, or denies
	admin := &AdminIdentity{
		IdentityID: uuid.New(),
		Roles:      []string{},
		Grants:     []Capability{},
		Denies:     []Capability{},
	}
	
	mockStore.On("GetRoles", ctx, []string{}).Return([]*Role{}, nil)
	
	// Should not have any capability
	assert.False(t, checker.evaluateCapability(ctx, admin, CapUsersRead))
	assert.False(t, checker.evaluateCapability(ctx, admin, CapSystemConfig))
	assert.False(t, checker.evaluateCapability(ctx, admin, CapAll))
}

// Test GetEffectiveCapabilities with wildcard deny
func TestGetEffectiveCapabilitiesWithWildcardDeny(t *testing.T) {
	mockStore := &MockStore{}
	checker := NewChecker(mockStore)
	ctx := context.Background()
	identityID := uuid.New()
	
	// Admin with admin role but deny entire users domain
	admin := &AdminIdentity{
		IdentityID: identityID,
		Roles:      []string{"admin"},
		Grants:     []Capability{},
		Denies:     []Capability{CapUsersAll},
	}
	
	adminRole := &Role{
		ID:   "admin",
		Name: "Administrator",
		Capabilities: []Capability{
			CapUsersAll,
			CapSessionsAll,
			CapMFAAll,
		},
	}
	
	mockStore.On("GetAdminIdentity", ctx, identityID).Return(admin, nil)
	mockStore.On("GetRoles", ctx, []string{"admin"}).Return([]*Role{adminRole}, nil)
	
	capabilities, err := checker.GetEffectiveCapabilities(ctx, identityID)
	
	assert.NoError(t, err)
	
	// Users capabilities should be removed
	assert.NotContains(t, capabilities, CapUsersRead)
	assert.NotContains(t, capabilities, CapUsersCreate)
	assert.NotContains(t, capabilities, CapUsersDelete)
	
	// But others should be present
	assert.Contains(t, capabilities, CapSessionsRead)
	assert.Contains(t, capabilities, CapMFAManage)
	
	mockStore.AssertExpectations(t)
}

// Test GetEffectiveCapabilities with wildcard grant
func TestGetEffectiveCapabilitiesWithWildcardGrant(t *testing.T) {
	mockStore := &MockStore{}
	checker := NewChecker(mockStore)
	ctx := context.Background()
	identityID := uuid.New()
	
	// Admin with no roles but wildcard grant on users domain
	admin := &AdminIdentity{
		IdentityID: identityID,
		Roles:      []string{},
		Grants:     []Capability{CapUsersAll, CapSessionsRead},
		Denies:     []Capability{},
	}
	
	mockStore.On("GetAdminIdentity", ctx, identityID).Return(admin, nil)
	mockStore.On("GetRoles", ctx, []string{}).Return([]*Role{}, nil)
	
	capabilities, err := checker.GetEffectiveCapabilities(ctx, identityID)
	
	assert.NoError(t, err)
	
	// All users capabilities should be present
	assert.Contains(t, capabilities, CapUsersRead)
	assert.Contains(t, capabilities, CapUsersCreate)
	assert.Contains(t, capabilities, CapUsersDelete)
	
	// And the specific session capability
	assert.Contains(t, capabilities, CapSessionsRead)
	
	mockStore.AssertExpectations(t)
}

// Test GetEffectiveCapabilities with global wildcard deny
func TestGetEffectiveCapabilitiesWithGlobalDeny(t *testing.T) {
	mockStore := &MockStore{}
	checker := NewChecker(mockStore)
	ctx := context.Background()
	identityID := uuid.New()
	
	// Admin with admin role and global deny
	admin := &AdminIdentity{
		IdentityID: identityID,
		Roles:      []string{"admin"},
		Grants:     []Capability{},
		Denies:     []Capability{CapAll},
	}
	
	adminRole := &Role{
		ID:   "admin",
		Name: "Administrator",
		Capabilities: []Capability{CapUsersAll, CapSessionsAll},
	}
	
	mockStore.On("GetAdminIdentity", ctx, identityID).Return(admin, nil)
	mockStore.On("GetRoles", ctx, []string{"admin"}).Return([]*Role{adminRole}, nil)
	
	capabilities, err := checker.GetEffectiveCapabilities(ctx, identityID)
	
	assert.NoError(t, err)
	// Should be empty due to global deny
	assert.Empty(t, capabilities)
	
	mockStore.AssertExpectations(t)
}

// Test error handling: store failure during HasCapability
func TestHasCapabilityStoreError(t *testing.T) {
	mockStore := &MockStore{}
	checker := NewChecker(mockStore)
	ctx := context.Background()
	identityID := uuid.New()
	
	mockStore.On("GetAdminIdentity", ctx, identityID).Return((*AdminIdentity)(nil), ErrAdminIdentityNotFound)
	
	result := checker.HasCapability(ctx, identityID, CapUsersRead)
	
	assert.False(t, result)
	mockStore.AssertExpectations(t)
}

// Test error handling: store failure during GetEffectiveCapabilities
func TestGetEffectiveCapabilitiesStoreError(t *testing.T) {
	mockStore := &MockStore{}
	checker := NewChecker(mockStore)
	ctx := context.Background()
	identityID := uuid.New()
	
	admin := &AdminIdentity{
		IdentityID: identityID,
		Roles:      []string{"admin"},
	}
	
	mockStore.On("GetAdminIdentity", ctx, identityID).Return(admin, nil)
	mockStore.On("GetRoles", ctx, []string{"admin"}).Return(([]*Role)(nil), ErrRoleNotFound)
	
	capabilities, err := checker.GetEffectiveCapabilities(ctx, identityID)
	
	assert.Error(t, err)
	assert.Nil(t, capabilities)
	mockStore.AssertExpectations(t)
}

// Test GrantCapabilities with invalid capability (should skip)
func TestGrantCapabilitiesInvalidCapability(t *testing.T) {
	mockStore := &MockStore{}
	checker := NewChecker(mockStore)
	ctx := context.Background()
	identityID := uuid.New()
	
	admin := &AdminIdentity{
		IdentityID: identityID,
		Grants:     []Capability{},
	}
	
	mockStore.On("GetAdminIdentity", ctx, identityID).Return(admin, nil)
	// Only valid capabilities should be added
	mockStore.On("UpdateAdminIdentity", ctx, mock.MatchedBy(func(a *AdminIdentity) bool {
		return len(a.Grants) == 1 && contains(a.Grants, CapUsersRead)
	})).Return(nil)
	
	// Try to grant one valid and one invalid
	err := checker.GrantCapabilities(ctx, identityID, []Capability{
		CapUsersRead,
		"invalid.capability",
	})
	
	assert.NoError(t, err)
	// Only valid capability should be granted
	mockStore.AssertExpectations(t)
}

// Test DenyCapabilities with invalid capability (should skip)
func TestDenyCapabilitiesInvalidCapability(t *testing.T) {
	mockStore := &MockStore{}
	checker := NewChecker(mockStore)
	ctx := context.Background()
	identityID := uuid.New()
	
	admin := &AdminIdentity{
		IdentityID: identityID,
		Denies:     []Capability{},
	}
	
	mockStore.On("GetAdminIdentity", ctx, identityID).Return(admin, nil)
	// Only valid capabilities should be added
	mockStore.On("UpdateAdminIdentity", ctx, mock.MatchedBy(func(a *AdminIdentity) bool {
		return len(a.Denies) == 1 && contains(a.Denies, CapUsersDelete)
	})).Return(nil)
	
	// Try to deny one valid and one invalid
	err := checker.DenyCapabilities(ctx, identityID, []Capability{
		CapUsersDelete,
		"invalid.capability",
	})
	
	assert.NoError(t, err)
	// Only valid capability should be denied
	mockStore.AssertExpectations(t)
}

// Test RemoveRoles non-existent role
func TestRemoveRolesNonExistent(t *testing.T) {
	mockStore := &MockStore{}
	checker := NewChecker(mockStore)
	ctx := context.Background()
	identityID := uuid.New()
	
	admin := &AdminIdentity{
		IdentityID: identityID,
		Roles:      []string{"user_manager"},
	}
	
	mockStore.On("GetAdminIdentity", ctx, identityID).Return(admin, nil)
	// Trying to remove a role that doesn't exist should not change anything
	mockStore.On("UpdateAdminIdentity", ctx, mock.MatchedBy(func(a *AdminIdentity) bool {
		return len(a.Roles) == 1 && contains(a.Roles, "user_manager")
	})).Return(nil)
	
	err := checker.RemoveRoles(ctx, identityID, []string{"nonexistent"})
	
	assert.NoError(t, err)
	mockStore.AssertExpectations(t)
}

// Test RevokeCapabilities non-existent capability
func TestRevokeCapabilitiesNonExistent(t *testing.T) {
	mockStore := &MockStore{}
	checker := NewChecker(mockStore)
	ctx := context.Background()
	identityID := uuid.New()
	
	admin := &AdminIdentity{
		IdentityID: identityID,
		Grants:     []Capability{CapUsersRead},
	}
	
	mockStore.On("GetAdminIdentity", ctx, identityID).Return(admin, nil)
	// Trying to revoke a capability that doesn't exist should not change anything
	mockStore.On("UpdateAdminIdentity", ctx, mock.MatchedBy(func(a *AdminIdentity) bool {
		return len(a.Grants) == 1 && contains(a.Grants, CapUsersRead)
	})).Return(nil)
	
	err := checker.RevokeCapabilities(ctx, identityID, []Capability{CapUsersDelete})
	
	assert.NoError(t, err)
	mockStore.AssertExpectations(t)
}

// Test multiple denies on same capability
func TestMultipleDeniesOnSameCapability(t *testing.T) {
	mockStore := &MockStore{}
	checker := NewChecker(mockStore)
	ctx := context.Background()
	identityID := uuid.New()
	
	admin := &AdminIdentity{
		IdentityID: identityID,
		Denies:     []Capability{CapUsersDelete},
	}
	
	mockStore.On("GetAdminIdentity", ctx, identityID).Return(admin, nil)
	// Adding the same deny multiple times should result in single entry
	mockStore.On("UpdateAdminIdentity", ctx, mock.MatchedBy(func(a *AdminIdentity) bool {
		// Should have exactly 1 deny (deduped)
		count := 0
		for _, d := range a.Denies {
			if d == CapUsersDelete {
				count++
			}
		}
		return count == 1
	})).Return(nil)
	
	err := checker.DenyCapabilities(ctx, identityID, []Capability{CapUsersDelete})
	
	assert.NoError(t, err)
	mockStore.AssertExpectations(t)
}

// Test AssignRoles with duplicate role
func TestAssignRolesDuplicate(t *testing.T) {
	mockStore := &MockStore{}
	checker := NewChecker(mockStore)
	ctx := context.Background()
	identityID := uuid.New()
	
	admin := &AdminIdentity{
		IdentityID: identityID,
		Roles:      []string{"user_manager"},
	}
	
	roles := []*Role{
		{ID: "user_manager", Name: "User Manager"},
	}
	
	mockStore.On("GetAdminIdentity", ctx, identityID).Return(admin, nil)
	mockStore.On("GetRoles", ctx, []string{"user_manager"}).Return(roles, nil)
	// Adding same role should result in single entry
	mockStore.On("UpdateAdminIdentity", ctx, mock.MatchedBy(func(a *AdminIdentity) bool {
		count := 0
		for _, r := range a.Roles {
			if r == "user_manager" {
				count++
			}
		}
		return count == 1
	})).Return(nil)
	
	err := checker.AssignRoles(ctx, identityID, []string{"user_manager"})
	
	assert.NoError(t, err)
	mockStore.AssertExpectations(t)
}

// Test GrantCapabilities with duplicate grant
func TestGrantCapabilitiesDuplicate(t *testing.T) {
	mockStore := &MockStore{}
	checker := NewChecker(mockStore)
	ctx := context.Background()
	identityID := uuid.New()
	
	admin := &AdminIdentity{
		IdentityID: identityID,
		Grants:     []Capability{CapUsersRead},
	}
	
	mockStore.On("GetAdminIdentity", ctx, identityID).Return(admin, nil)
	// Adding same grant should result in single entry
	mockStore.On("UpdateAdminIdentity", ctx, mock.MatchedBy(func(a *AdminIdentity) bool {
		count := 0
		for _, g := range a.Grants {
			if g == CapUsersRead {
				count++
			}
		}
		return count == 1
	})).Return(nil)
	
	err := checker.GrantCapabilities(ctx, identityID, []Capability{CapUsersRead})
	
	assert.NoError(t, err)
	mockStore.AssertExpectations(t)
}

// Test isValidCapability for all domains
func TestIsValidCapabilityAllDomains(t *testing.T) {
	checker := &Checker{}
	
	// Test all valid specific capabilities
	validCaps := []Capability{
		// Users
		CapUsersRead, CapUsersCreate, CapUsersUpdate, CapUsersDelete, CapUsersSuspend,
		// Sessions
		CapSessionsRead, CapSessionsRevoke,
		// MFA
		CapMFARead, CapMFAManage,
		// OAuth2
		CapOAuth2ClientsRead, CapOAuth2ClientsManage, CapOAuth2TokensRead, CapOAuth2TokensRevoke,
		// Policy
		CapPolicyRead, CapPolicyManage,
		// System
		CapSystemConfig, CapSystemAudit, CapSystemHealth,
		// Admin Team
		CapAdminTeamRead, CapAdminTeamManage,
		// SCIM
		CapSCIMUsersRead, CapSCIMUsersWrite, CapSCIMGroupsRead, CapSCIMGroupsWrite, CapSCIMTokensManage,
		// Global
		CapAll,
	}
	
	for _, cap := range validCaps {
		assert.True(t, checker.isValidCapability(cap), "should be valid: %s", cap)
	}
	
	// Test all valid wildcards
	validWildcards := []Capability{
		CapUsersAll, CapSessionsAll, CapMFAAll, CapOAuth2All, CapPolicyAll,
		CapSystemAll, CapAdminTeamAll, CapSCIMAll, CapAll,
	}
	
	for _, cap := range validWildcards {
		assert.True(t, checker.isValidCapability(cap), "should be valid: %s", cap)
	}
	
	// Test invalid capabilities
	invalidCaps := []Capability{
		"invalid", "invalid.read", "users.read.extra", "nonexistent.*",
		"", " ", "users. read", "users .read",
	}
	
	for _, cap := range invalidCaps {
		assert.False(t, checker.isValidCapability(cap), "should be invalid: %s", cap)
	}
}

// Test HTTP middleware: RequireCapability success
func TestRequireCapabilityMiddlewareSuccess(t *testing.T) {
	mockStore := &MockStore{}
	checker := NewChecker(mockStore)
	ctx := context.Background()
	identityID := uuid.New()
	
	admin := &AdminIdentity{
		IdentityID: identityID,
		Roles:      []string{"user_manager"},
		Grants:     []Capability{},
		Denies:     []Capability{},
	}
	
	userManagerRole := &Role{
		ID:   "user_manager",
		Name: "User Manager",
		Capabilities: []Capability{CapUsersAll},
	}
	
	mockStore.On("GetAdminIdentity", mock.Anything, identityID).Return(admin, nil)
	mockStore.On("GetRoles", mock.Anything, []string{"user_manager"}).Return([]*Role{userManagerRole}, nil)
	
	// Create test handler
	handlerCalled := false
	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handlerCalled = true
		w.WriteHeader(http.StatusOK)
	})
	
	// Apply middleware
	middleware := checker.RequireCapability(CapUsersRead)
	wrappedHandler := middleware(testHandler)
	
	// Create request with identity in context
	req := httptest.NewRequest("GET", "/test", nil)
	req = req.WithContext(SetIdentityIDInContext(ctx, identityID))
	w := httptest.NewRecorder()
	
	wrappedHandler.ServeHTTP(w, req)
	
	assert.True(t, handlerCalled)
	assert.Equal(t, http.StatusOK, w.Code)
	mockStore.AssertExpectations(t)
}

// Test HTTP middleware: RequireCapability forbidden
func TestRequireCapabilityMiddlewareForbidden(t *testing.T) {
	mockStore := &MockStore{}
	checker := NewChecker(mockStore)
	ctx := context.Background()
	identityID := uuid.New()
	
	admin := &AdminIdentity{
		IdentityID: identityID,
		Roles:      []string{"auditor"},
		Grants:     []Capability{},
		Denies:     []Capability{},
	}
	
	auditorRole := &Role{
		ID:   "auditor",
		Name: "Auditor",
		Capabilities: []Capability{CapUsersRead},
	}
	
	mockStore.On("GetAdminIdentity", mock.Anything, identityID).Return(admin, nil)
	mockStore.On("GetRoles", mock.Anything, []string{"auditor"}).Return([]*Role{auditorRole}, nil)
	
	handlerCalled := false
	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handlerCalled = true
	})
	
	middleware := checker.RequireCapability(CapUsersDelete)
	wrappedHandler := middleware(testHandler)
	
	req := httptest.NewRequest("GET", "/test", nil)
	req = req.WithContext(SetIdentityIDInContext(ctx, identityID))
	w := httptest.NewRecorder()
	
	wrappedHandler.ServeHTTP(w, req)
	
	assert.False(t, handlerCalled)
	assert.Equal(t, http.StatusForbidden, w.Code)
	mockStore.AssertExpectations(t)
}

// Test HTTP middleware: RequireCapability unauthenticated
func TestRequireCapabilityMiddlewareUnauthenticated(t *testing.T) {
	mockStore := &MockStore{}
	checker := NewChecker(mockStore)
	
	handlerCalled := false
	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handlerCalled = true
	})
	
	middleware := checker.RequireCapability(CapUsersRead)
	wrappedHandler := middleware(testHandler)
	
	req := httptest.NewRequest("GET", "/test", nil)
	// No identity in context
	w := httptest.NewRecorder()
	
	wrappedHandler.ServeHTTP(w, req)
	
	assert.False(t, handlerCalled)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

// Test HTTP middleware: RequireAnyCapability with one match
func TestRequireAnyCapabilityMiddlewareOneMatch(t *testing.T) {
	mockStore := &MockStore{}
	checker := NewChecker(mockStore)
	ctx := context.Background()
	identityID := uuid.New()
	
	admin := &AdminIdentity{
		IdentityID: identityID,
		Roles:      []string{"user_manager"},
	}
	
	userManagerRole := &Role{
		ID:   "user_manager",
		Capabilities: []Capability{CapUsersAll},
	}
	
	mockStore.On("GetAdminIdentity", mock.Anything, identityID).Return(admin, nil)
	mockStore.On("GetRoles", mock.Anything, []string{"user_manager"}).Return([]*Role{userManagerRole}, nil)
	
	handlerCalled := false
	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handlerCalled = true
		w.WriteHeader(http.StatusOK)
	})
	
	middleware := checker.RequireAnyCapability(CapSystemConfig, CapUsersRead)
	wrappedHandler := middleware(testHandler)
	
	req := httptest.NewRequest("GET", "/test", nil)
	req = req.WithContext(SetIdentityIDInContext(ctx, identityID))
	w := httptest.NewRecorder()
	
	wrappedHandler.ServeHTTP(w, req)
	
	assert.True(t, handlerCalled)
	assert.Equal(t, http.StatusOK, w.Code)
	mockStore.AssertExpectations(t)
}

// Test HTTP middleware: RequireAnyCapability with no match
func TestRequireAnyCapabilityMiddlewareNoMatch(t *testing.T) {
	mockStore := &MockStore{}
	checker := NewChecker(mockStore)
	ctx := context.Background()
	identityID := uuid.New()
	
	admin := &AdminIdentity{
		IdentityID: identityID,
		Roles:      []string{"auditor"},
	}
	
	auditorRole := &Role{
		ID:   "auditor",
		Capabilities: []Capability{CapUsersRead},
	}
	
	mockStore.On("GetAdminIdentity", mock.Anything, identityID).Return(admin, nil)
	mockStore.On("GetRoles", mock.Anything, []string{"auditor"}).Return([]*Role{auditorRole}, nil)
	
	handlerCalled := false
	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handlerCalled = true
	})
	
	middleware := checker.RequireAnyCapability(CapUsersDelete, CapSystemConfig)
	wrappedHandler := middleware(testHandler)
	
	req := httptest.NewRequest("GET", "/test", nil)
	req = req.WithContext(SetIdentityIDInContext(ctx, identityID))
	w := httptest.NewRecorder()
	
	wrappedHandler.ServeHTTP(w, req)
	
	assert.False(t, handlerCalled)
	assert.Equal(t, http.StatusForbidden, w.Code)
	mockStore.AssertExpectations(t)
}

// Test HTTP middleware: RequireAllCapabilities success
func TestRequireAllCapabilitiesMiddlewareSuccess(t *testing.T) {
	mockStore := &MockStore{}
	checker := NewChecker(mockStore)
	ctx := context.Background()
	identityID := uuid.New()
	
	admin := &AdminIdentity{
		IdentityID: identityID,
		Roles:      []string{"admin"},
	}
	
	adminRole := &Role{
		ID:   "admin",
		Capabilities: []Capability{CapUsersAll, CapSessionsAll, CapMFAAll},
	}
	
	mockStore.On("GetAdminIdentity", mock.Anything, identityID).Return(admin, nil)
	mockStore.On("GetRoles", mock.Anything, []string{"admin"}).Return([]*Role{adminRole}, nil)
	
	handlerCalled := false
	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handlerCalled = true
		w.WriteHeader(http.StatusOK)
	})
	
	middleware := checker.RequireAllCapabilities(CapUsersRead, CapSessionsRead)
	wrappedHandler := middleware(testHandler)
	
	req := httptest.NewRequest("GET", "/test", nil)
	req = req.WithContext(SetIdentityIDInContext(ctx, identityID))
	w := httptest.NewRecorder()
	
	wrappedHandler.ServeHTTP(w, req)
	
	assert.True(t, handlerCalled)
	assert.Equal(t, http.StatusOK, w.Code)
	mockStore.AssertExpectations(t)
}

// Test HTTP middleware: RequireAllCapabilities partial match
func TestRequireAllCapabilitiesMiddlewarePartialMatch(t *testing.T) {
	mockStore := &MockStore{}
	checker := NewChecker(mockStore)
	ctx := context.Background()
	identityID := uuid.New()
	
	admin := &AdminIdentity{
		IdentityID: identityID,
		Roles:      []string{"user_manager"},
	}
	
	userManagerRole := &Role{
		ID:   "user_manager",
		Capabilities: []Capability{CapUsersAll, CapSessionsAll},
	}
	
	mockStore.On("GetAdminIdentity", mock.Anything, identityID).Return(admin, nil)
	mockStore.On("GetRoles", mock.Anything, []string{"user_manager"}).Return([]*Role{userManagerRole}, nil)
	
	handlerCalled := false
	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handlerCalled = true
	})
	
	// Require users AND system capabilities - should fail
	middleware := checker.RequireAllCapabilities(CapUsersRead, CapSystemConfig)
	wrappedHandler := middleware(testHandler)
	
	req := httptest.NewRequest("GET", "/test", nil)
	req = req.WithContext(SetIdentityIDInContext(ctx, identityID))
	w := httptest.NewRecorder()
	
	wrappedHandler.ServeHTTP(w, req)
	
	assert.False(t, handlerCalled)
	assert.Equal(t, http.StatusForbidden, w.Code)
	mockStore.AssertExpectations(t)
}

// Test context utilities
func TestContextUtilities(t *testing.T) {
	ctx := context.Background()
	identityID := uuid.New()
	
	// Test SetIdentityIDInContext and GetIdentityIDFromContext
	ctxWithID := SetIdentityIDInContext(ctx, identityID)
	retrievedID, ok := GetIdentityIDFromContext(ctxWithID)
	
	assert.True(t, ok)
	assert.Equal(t, identityID, retrievedID)
	
	// Test with empty context
	_, ok = GetIdentityIDFromContext(context.Background())
	assert.False(t, ok)
}

// Test GetEffectiveCapabilities with multiple roles and mixed grants/denies
func TestGetEffectiveCapabilitiesComplex(t *testing.T) {
	mockStore := &MockStore{}
	checker := NewChecker(mockStore)
	ctx := context.Background()
	identityID := uuid.New()
	
	// Complex scenario: user_manager + auditor roles + some grants + some denies
	admin := &AdminIdentity{
		IdentityID: identityID,
		Roles:      []string{"user_manager", "security_manager"},
		Grants:     []Capability{CapPolicyRead},
		Denies:     []Capability{CapUsersDelete},
	}
	
	userManagerRole := &Role{
		ID:   "user_manager",
		Capabilities: []Capability{CapUsersAll, CapSessionsAll},
	}
	
	securityManagerRole := &Role{
		ID:   "security_manager",
		Capabilities: []Capability{CapMFAAll, CapOAuth2All},
	}
	
	mockStore.On("GetAdminIdentity", ctx, identityID).Return(admin, nil)
	mockStore.On("GetRoles", ctx, []string{"user_manager", "security_manager"}).Return([]*Role{userManagerRole, securityManagerRole}, nil)
	
	capabilities, err := checker.GetEffectiveCapabilities(ctx, identityID)
	
	assert.NoError(t, err)
	
	// From user_manager role
	assert.Contains(t, capabilities, CapUsersRead)
	assert.Contains(t, capabilities, CapUsersCreate)
	assert.Contains(t, capabilities, CapSessionsRead)
	
	// From security_manager role
	assert.Contains(t, capabilities, CapMFARead)
	assert.Contains(t, capabilities, CapOAuth2ClientsRead)
	
	// From grants
	assert.Contains(t, capabilities, CapPolicyRead)
	
	// NOT from denies
	assert.NotContains(t, capabilities, CapUsersDelete)
	
	mockStore.AssertExpectations(t)
}

// Test RevokeCapabilities with empty array
func TestRevokeCapabilitiesEmpty(t *testing.T) {
	mockStore := &MockStore{}
	checker := NewChecker(mockStore)
	ctx := context.Background()
	identityID := uuid.New()
	
	admin := &AdminIdentity{
		IdentityID: identityID,
		Grants:     []Capability{CapUsersRead},
	}
	
	mockStore.On("GetAdminIdentity", ctx, identityID).Return(admin, nil)
	mockStore.On("UpdateAdminIdentity", ctx, mock.MatchedBy(func(a *AdminIdentity) bool {
		return len(a.Grants) == 1 && contains(a.Grants, CapUsersRead)
	})).Return(nil)
	
	err := checker.RevokeCapabilities(ctx, identityID, []Capability{})
	
	assert.NoError(t, err)
	mockStore.AssertExpectations(t)
}

// Test RemoveRoles with empty array
func TestRemoveRolesEmpty(t *testing.T) {
	mockStore := &MockStore{}
	checker := NewChecker(mockStore)
	ctx := context.Background()
	identityID := uuid.New()
	
	admin := &AdminIdentity{
		IdentityID: identityID,
		Roles:      []string{"user_manager"},
	}
	
	mockStore.On("GetAdminIdentity", ctx, identityID).Return(admin, nil)
	mockStore.On("UpdateAdminIdentity", ctx, mock.MatchedBy(func(a *AdminIdentity) bool {
		return len(a.Roles) == 1 && contains(a.Roles, "user_manager")
	})).Return(nil)
	
	err := checker.RemoveRoles(ctx, identityID, []string{})
	
	assert.NoError(t, err)
	mockStore.AssertExpectations(t)
}

// Test error handling: store failure during role operations
func TestCreateRoleStoreError(t *testing.T) {
	mockStore := &MockStore{}
	checker := NewChecker(mockStore)
	ctx := context.Background()
	
	role := &Role{
		ID:          "custom_role",
		Name:        "Custom Role",
		Capabilities: []Capability{CapUsersRead},
	}
	
	mockStore.On("CreateRole", ctx, role).Return(ErrInvalidRole)
	
	err := checker.CreateRole(ctx, role)
	
	assert.Error(t, err)
	mockStore.AssertExpectations(t)
}

// Test error handling: store failure during UpdateRole
func TestUpdateRoleStoreError(t *testing.T) {
	mockStore := &MockStore{}
	checker := NewChecker(mockStore)
	ctx := context.Background()
	
	customRole := &Role{
		ID:       "custom_role",
		IsSystem: false,
	}
	
	updatedRole := &Role{
		ID:   "custom_role",
		Name: "Updated",
	}
	
	mockStore.On("GetRole", ctx, "custom_role").Return(customRole, nil)
	mockStore.On("UpdateRole", ctx, updatedRole).Return(ErrInvalidRole)
	
	err := checker.UpdateRole(ctx, updatedRole)
	
	assert.Error(t, err)
	mockStore.AssertExpectations(t)
}

// Test error handling: store failure during DeleteRole
func TestDeleteRoleStoreError(t *testing.T) {
	mockStore := &MockStore{}
	checker := NewChecker(mockStore)
	ctx := context.Background()
	
	customRole := &Role{
		ID:       "custom_role",
		IsSystem: false,
	}
	
	mockStore.On("GetRole", ctx, "custom_role").Return(customRole, nil)
	mockStore.On("DeleteRole", ctx, "custom_role").Return(ErrInvalidRole)
	
	err := checker.DeleteRole(ctx, "custom_role")
	
	assert.Error(t, err)
	mockStore.AssertExpectations(t)
}

// Test error handling: role not found during UpdateRole
func TestUpdateRoleNotFound(t *testing.T) {
	mockStore := &MockStore{}
	checker := NewChecker(mockStore)
	ctx := context.Background()
	
	updatedRole := &Role{
		ID:   "nonexistent",
		Name: "Updated",
	}
	
	mockStore.On("GetRole", ctx, "nonexistent").Return((*Role)(nil), ErrRoleNotFound)
	
	err := checker.UpdateRole(ctx, updatedRole)
	
	assert.Error(t, err)
	mockStore.AssertExpectations(t)
}

// Test error handling: role not found during DeleteRole
func TestDeleteRoleNotFound(t *testing.T) {
	mockStore := &MockStore{}
	checker := NewChecker(mockStore)
	ctx := context.Background()
	
	mockStore.On("GetRole", ctx, "nonexistent").Return((*Role)(nil), ErrRoleNotFound)
	
	err := checker.DeleteRole(ctx, "nonexistent")
	
	assert.Error(t, err)
	mockStore.AssertExpectations(t)
}

// Test error handling: store failure during GrantCapabilities
func TestGrantCapabilitiesStoreError(t *testing.T) {
	mockStore := &MockStore{}
	checker := NewChecker(mockStore)
	ctx := context.Background()
	identityID := uuid.New()
	
	mockStore.On("GetAdminIdentity", ctx, identityID).Return((*AdminIdentity)(nil), ErrAdminIdentityNotFound)
	
	err := checker.GrantCapabilities(ctx, identityID, []Capability{CapUsersRead})
	
	assert.Error(t, err)
	mockStore.AssertExpectations(t)
}

// Test error handling: store failure during RevokeCapabilities
func TestRevokeCapabilitiesStoreError(t *testing.T) {
	mockStore := &MockStore{}
	checker := NewChecker(mockStore)
	ctx := context.Background()
	identityID := uuid.New()
	
	mockStore.On("GetAdminIdentity", ctx, identityID).Return((*AdminIdentity)(nil), ErrAdminIdentityNotFound)
	
	err := checker.RevokeCapabilities(ctx, identityID, []Capability{CapUsersRead})
	
	assert.Error(t, err)
	mockStore.AssertExpectations(t)
}

// Test error handling: store failure during DenyCapabilities
func TestDenyCapabilitiesStoreError(t *testing.T) {
	mockStore := &MockStore{}
	checker := NewChecker(mockStore)
	ctx := context.Background()
	identityID := uuid.New()
	
	mockStore.On("GetAdminIdentity", ctx, identityID).Return((*AdminIdentity)(nil), ErrAdminIdentityNotFound)
	
	err := checker.DenyCapabilities(ctx, identityID, []Capability{CapUsersRead})
	
	assert.Error(t, err)
	mockStore.AssertExpectations(t)
}

// Test error handling: store failure during AssignRoles
func TestAssignRolesStoreErrorGetAdmin(t *testing.T) {
	mockStore := &MockStore{}
	checker := NewChecker(mockStore)
	ctx := context.Background()
	identityID := uuid.New()
	
	mockStore.On("GetAdminIdentity", ctx, identityID).Return((*AdminIdentity)(nil), ErrAdminIdentityNotFound)
	
	err := checker.AssignRoles(ctx, identityID, []string{"user_manager"})
	
	assert.Error(t, err)
	mockStore.AssertExpectations(t)
}

// Test error handling: store failure during AssignRoles (role not found)
func TestAssignRolesStoreErrorGetRoles(t *testing.T) {
	mockStore := &MockStore{}
	checker := NewChecker(mockStore)
	ctx := context.Background()
	identityID := uuid.New()
	
	admin := &AdminIdentity{
		IdentityID: identityID,
		Roles:      []string{},
	}
	
	mockStore.On("GetAdminIdentity", ctx, identityID).Return(admin, nil)
	mockStore.On("GetRoles", ctx, []string{"user_manager"}).Return(([]*Role)(nil), ErrRoleNotFound)
	
	err := checker.AssignRoles(ctx, identityID, []string{"user_manager"})
	
	assert.Error(t, err)
	mockStore.AssertExpectations(t)
}

// Test error handling: store failure during AssignRoles (update fails)
func TestAssignRolesStoreErrorUpdate(t *testing.T) {
	mockStore := &MockStore{}
	checker := NewChecker(mockStore)
	ctx := context.Background()
	identityID := uuid.New()
	
	admin := &AdminIdentity{
		IdentityID: identityID,
		Roles:      []string{},
	}
	
	userManagerRole := &Role{
		ID:   "user_manager",
		Name: "User Manager",
	}
	
	mockStore.On("GetAdminIdentity", ctx, identityID).Return(admin, nil)
	mockStore.On("GetRoles", ctx, []string{"user_manager"}).Return([]*Role{userManagerRole}, nil)
	mockStore.On("UpdateAdminIdentity", ctx, mock.Anything).Return(ErrPermissionDenied)
	
	err := checker.AssignRoles(ctx, identityID, []string{"user_manager"})
	
	assert.Error(t, err)
	mockStore.AssertExpectations(t)
}

// Test error handling: store failure during RemoveRoles
func TestRemoveRolesStoreError(t *testing.T) {
	mockStore := &MockStore{}
	checker := NewChecker(mockStore)
	ctx := context.Background()
	identityID := uuid.New()
	
	mockStore.On("GetAdminIdentity", ctx, identityID).Return((*AdminIdentity)(nil), ErrAdminIdentityNotFound)
	
	err := checker.RemoveRoles(ctx, identityID, []string{"user_manager"})
	
	assert.Error(t, err)
	mockStore.AssertExpectations(t)
}

// Test role with no capabilities
func TestCreateRoleWithNoCapabilities(t *testing.T) {
	mockStore := &MockStore{}
	checker := NewChecker(mockStore)
	ctx := context.Background()
	
	role := &Role{
		ID:           "empty_role",
		Name:         "Empty Role",
		Capabilities: []Capability{},
	}
	
	mockStore.On("CreateRole", ctx, role).Return(nil)
	
	err := checker.CreateRole(ctx, role)
	
	assert.NoError(t, err)
	mockStore.AssertExpectations(t)
}

// Test UpdateRole with all fields
func TestUpdateRoleAllFields(t *testing.T) {
	mockStore := &MockStore{}
	checker := NewChecker(mockStore)
	ctx := context.Background()
	
	existingRole := &Role{
		ID:       "custom_role",
		IsSystem: false,
	}
	
	updatedRole := &Role{
		ID:          "custom_role",
		Name:        "Updated Role",
		Description: "Updated description",
		Capabilities: []Capability{
			CapUsersRead,
			CapUsersCreate,
			CapSessionsRead,
		},
	}
	
	mockStore.On("GetRole", ctx, "custom_role").Return(existingRole, nil)
	mockStore.On("UpdateRole", ctx, updatedRole).Return(nil)
	
	err := checker.UpdateRole(ctx, updatedRole)
	
	assert.NoError(t, err)
	mockStore.AssertExpectations(t)
}

// Test GrantCapabilities with multiple valid capabilities
func TestGrantCapabilitiesMultiple(t *testing.T) {
	mockStore := &MockStore{}
	checker := NewChecker(mockStore)
	ctx := context.Background()
	identityID := uuid.New()
	
	admin := &AdminIdentity{
		IdentityID: identityID,
		Grants:     []Capability{},
	}
	
	mockStore.On("GetAdminIdentity", ctx, identityID).Return(admin, nil)
	mockStore.On("UpdateAdminIdentity", ctx, mock.MatchedBy(func(a *AdminIdentity) bool {
		return len(a.Grants) == 3 &&
			   contains(a.Grants, CapUsersRead) &&
			   contains(a.Grants, CapSessionsRead) &&
			   contains(a.Grants, CapSystemAudit)
	})).Return(nil)
	
	err := checker.GrantCapabilities(ctx, identityID, []Capability{
		CapUsersRead,
		CapSessionsRead,
		CapSystemAudit,
	})
	
	assert.NoError(t, err)
	mockStore.AssertExpectations(t)
}

// Test RevokeCapabilities with multiple capabilities
func TestRevokeCapabilitiesMultiple(t *testing.T) {
	mockStore := &MockStore{}
	checker := NewChecker(mockStore)
	ctx := context.Background()
	identityID := uuid.New()
	
	admin := &AdminIdentity{
		IdentityID: identityID,
		Grants:     []Capability{CapUsersRead, CapUsersCreate, CapUsersDelete, CapSystemAudit},
	}
	
	mockStore.On("GetAdminIdentity", ctx, identityID).Return(admin, nil)
	mockStore.On("UpdateAdminIdentity", ctx, mock.MatchedBy(func(a *AdminIdentity) bool {
		return len(a.Grants) == 2 &&
			   contains(a.Grants, CapUsersRead) &&
			   contains(a.Grants, CapSystemAudit)
	})).Return(nil)
	
	err := checker.RevokeCapabilities(ctx, identityID, []Capability{
		CapUsersCreate,
		CapUsersDelete,
	})
	
	assert.NoError(t, err)
	mockStore.AssertExpectations(t)
}

// Test DenyCapabilities with multiple capabilities
func TestDenyCapabilitiesMultiple(t *testing.T) {
	mockStore := &MockStore{}
	checker := NewChecker(mockStore)
	ctx := context.Background()
	identityID := uuid.New()
	
	admin := &AdminIdentity{
		IdentityID: identityID,
		Denies:     []Capability{},
	}
	
	mockStore.On("GetAdminIdentity", ctx, identityID).Return(admin, nil)
	mockStore.On("UpdateAdminIdentity", ctx, mock.MatchedBy(func(a *AdminIdentity) bool {
		return len(a.Denies) == 3 &&
			   contains(a.Denies, CapUsersDelete) &&
			   contains(a.Denies, CapSystemConfig) &&
			   contains(a.Denies, CapAdminTeamManage)
	})).Return(nil)
	
	err := checker.DenyCapabilities(ctx, identityID, []Capability{
		CapUsersDelete,
		CapSystemConfig,
		CapAdminTeamManage,
	})
	
	assert.NoError(t, err)
	mockStore.AssertExpectations(t)
}

// Test AssignRoles with multiple roles
func TestAssignRolesMultiple(t *testing.T) {
	mockStore := &MockStore{}
	checker := NewChecker(mockStore)
	ctx := context.Background()
	identityID := uuid.New()
	
	admin := &AdminIdentity{
		IdentityID: identityID,
		Roles:      []string{},
	}
	
	roles := []*Role{
		{ID: "user_manager", Name: "User Manager"},
		{ID: "auditor", Name: "Auditor"},
	}
	
	mockStore.On("GetAdminIdentity", ctx, identityID).Return(admin, nil)
	mockStore.On("GetRoles", ctx, []string{"user_manager", "auditor"}).Return(roles, nil)
	mockStore.On("UpdateAdminIdentity", ctx, mock.MatchedBy(func(a *AdminIdentity) bool {
		return len(a.Roles) == 2 &&
			   contains(a.Roles, "user_manager") &&
			   contains(a.Roles, "auditor")
	})).Return(nil)
	
	err := checker.AssignRoles(ctx, identityID, []string{"user_manager", "auditor"})
	
	assert.NoError(t, err)
	mockStore.AssertExpectations(t)
}

// Test RemoveRoles with multiple roles
func TestRemoveRolesMultiple(t *testing.T) {
	mockStore := &MockStore{}
	checker := NewChecker(mockStore)
	ctx := context.Background()
	identityID := uuid.New()
	
	admin := &AdminIdentity{
		IdentityID: identityID,
		Roles:      []string{"user_manager", "auditor", "security_manager"},
	}
	
	mockStore.On("GetAdminIdentity", ctx, identityID).Return(admin, nil)
	mockStore.On("UpdateAdminIdentity", ctx, mock.MatchedBy(func(a *AdminIdentity) bool {
		return len(a.Roles) == 1 && contains(a.Roles, "security_manager")
	})).Return(nil)
	
	err := checker.RemoveRoles(ctx, identityID, []string{"user_manager", "auditor"})
	
	assert.NoError(t, err)
	mockStore.AssertExpectations(t)
}

// Test HasCapability with multiple roles returning the capability
func TestHasCapabilityFromMultipleRoles(t *testing.T) {
	mockStore := &MockStore{}
	checker := NewChecker(mockStore)
	ctx := context.Background()
	identityID := uuid.New()
	
	admin := &AdminIdentity{
		IdentityID: identityID,
		Roles:      []string{"user_manager", "security_manager"},
	}
	
	// Both roles have some capabilities
	userManagerRole := &Role{
		ID:   "user_manager",
		Capabilities: []Capability{CapUsersAll, CapSessionsAll},
	}
	
	securityManagerRole := &Role{
		ID:   "security_manager",
		Capabilities: []Capability{CapMFAAll, CapOAuth2All},
	}
	
	mockStore.On("GetAdminIdentity", ctx, identityID).Return(admin, nil)
	mockStore.On("GetRoles", ctx, []string{"user_manager", "security_manager"}).Return([]*Role{userManagerRole, securityManagerRole}, nil)
	
	// Should have capability from first role
	assert.True(t, checker.HasCapability(ctx, identityID, CapUsersRead))
	// Should have capability from second role
	assert.True(t, checker.HasCapability(ctx, identityID, CapMFAManage))
	
	mockStore.AssertExpectations(t)
}

// Test GetEffectiveCapabilities with error during update
func TestGrantCapabilitiesUpdateError(t *testing.T) {
	mockStore := &MockStore{}
	checker := NewChecker(mockStore)
	ctx := context.Background()
	identityID := uuid.New()
	
	admin := &AdminIdentity{
		IdentityID: identityID,
		Grants:     []Capability{},
	}
	
	mockStore.On("GetAdminIdentity", ctx, identityID).Return(admin, nil)
	mockStore.On("UpdateAdminIdentity", ctx, mock.Anything).Return(ErrPermissionDenied)
	
	err := checker.GrantCapabilities(ctx, identityID, []Capability{CapUsersRead})
	
	assert.Error(t, err)
	mockStore.AssertExpectations(t)
}

// Test RevokeCapabilities update error
func TestRevokeCapabilitiesUpdateError(t *testing.T) {
	mockStore := &MockStore{}
	checker := NewChecker(mockStore)
	ctx := context.Background()
	identityID := uuid.New()
	
	admin := &AdminIdentity{
		IdentityID: identityID,
		Grants:     []Capability{CapUsersRead},
	}
	
	mockStore.On("GetAdminIdentity", ctx, identityID).Return(admin, nil)
	mockStore.On("UpdateAdminIdentity", ctx, mock.Anything).Return(ErrPermissionDenied)
	
	err := checker.RevokeCapabilities(ctx, identityID, []Capability{CapUsersRead})
	
	assert.Error(t, err)
	mockStore.AssertExpectations(t)
}

// Test DenyCapabilities update error
func TestDenyCapabilitiesUpdateError(t *testing.T) {
	mockStore := &MockStore{}
	checker := NewChecker(mockStore)
	ctx := context.Background()
	identityID := uuid.New()
	
	admin := &AdminIdentity{
		IdentityID: identityID,
		Denies:     []Capability{},
	}
	
	mockStore.On("GetAdminIdentity", ctx, identityID).Return(admin, nil)
	mockStore.On("UpdateAdminIdentity", ctx, mock.Anything).Return(ErrPermissionDenied)
	
	err := checker.DenyCapabilities(ctx, identityID, []Capability{CapUsersDelete})
	
	assert.Error(t, err)
	mockStore.AssertExpectations(t)
}