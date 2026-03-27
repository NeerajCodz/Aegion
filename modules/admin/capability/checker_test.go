package capability

import (
	"context"
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