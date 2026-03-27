package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// MockStore implements store interface for testing
type MockStore struct {
	mock.Mock
}

func (m *MockStore) CreateOperator(ctx context.Context, op interface{}) error {
	args := m.Called(ctx, op)
	return args.Error(0)
}

func (m *MockStore) GetOperator(ctx context.Context, id string) (interface{}, error) {
	args := m.Called(ctx, id)
	return args.Get(0), args.Error(1)
}

func (m *MockStore) GetOperatorByIdentityID(ctx context.Context, identityID string) (interface{}, error) {
	args := m.Called(ctx, identityID)
	return args.Get(0), args.Error(1)
}

func (m *MockStore) UpdateOperator(ctx context.Context, id, role string, permissions []string) error {
	args := m.Called(ctx, id, role, permissions)
	return args.Error(0)
}

func (m *MockStore) DeleteOperator(ctx context.Context, id string) error {
	args := m.Called(ctx, id)
	return args.Error(0)
}

func (m *MockStore) ListOperators(ctx context.Context, limit, offset int) ([]interface{}, int, error) {
	args := m.Called(ctx, limit, offset)
	return args.Get(0).([]interface{}), args.Int(1), args.Error(2)
}

func (m *MockStore) CountOperators(ctx context.Context) (int, error) {
	args := m.Called(ctx)
	return args.Int(0), args.Error(1)
}

func (m *MockStore) LogAudit(ctx context.Context, log interface{}) error {
	args := m.Called(ctx, log)
	return args.Error(0)
}

func (m *MockStore) ListAuditLogs(ctx context.Context, filter interface{}, limit, offset int) ([]interface{}, int, error) {
	args := m.Called(ctx, filter, limit, offset)
	return args.Get(0).([]interface{}), args.Int(1), args.Error(2)
}

func (m *MockStore) GetRole(ctx context.Context, name string) (interface{}, error) {
	args := m.Called(ctx, name)
	return args.Get(0), args.Error(1)
}

func (m *MockStore) ListRoles(ctx context.Context, limit, offset int) ([]interface{}, int, error) {
	args := m.Called(ctx, limit, offset)
	return args.Get(0).([]interface{}), args.Int(1), args.Error(2)
}

func TestNew(t *testing.T) {
	store := &MockStore{}
	config := Config{BootstrapEnabled: true}
	
	service := New(store, config)
	
	assert.NotNil(t, service)
	assert.Equal(t, store, service.store)
	assert.Equal(t, config, service.config)
}

func TestValidRoles(t *testing.T) {
	expectedRoles := []string{"super_admin", "admin", "operator", "viewer"}
	
	assert.Equal(t, expectedRoles, ValidRoles)
	assert.Contains(t, ValidRoles, "super_admin")
	assert.Contains(t, ValidRoles, "admin")
	assert.Contains(t, ValidRoles, "operator")
	assert.Contains(t, ValidRoles, "viewer")
}

func TestDefaultRolePermissions(t *testing.T) {
	// Test that default role permissions are correctly defined
	assert.Contains(t, DefaultRolePermissions, "super_admin")
	assert.Contains(t, DefaultRolePermissions, "admin")
	assert.Contains(t, DefaultRolePermissions, "operator")
	assert.Contains(t, DefaultRolePermissions, "viewer")
	
	// Super admin should have wildcard permission
	superAdminPerms := DefaultRolePermissions["super_admin"]
	assert.Contains(t, superAdminPerms, "*")
	
	// Other roles should have specific permissions
	adminPerms := DefaultRolePermissions["admin"]
	assert.Contains(t, adminPerms, "identities:*")
	assert.Contains(t, adminPerms, "sessions:*")
	assert.Contains(t, adminPerms, "config:read")
	
	operatorPerms := DefaultRolePermissions["operator"]
	assert.Contains(t, operatorPerms, "identities:read")
	assert.Contains(t, operatorPerms, "identities:update")
	
	viewerPerms := DefaultRolePermissions["viewer"]
	assert.Contains(t, viewerPerms, "identities:read")
	assert.Contains(t, viewerPerms, "sessions:read")
}

func TestService_CanBootstrap(t *testing.T) {
	tests := []struct {
		name        string
		setupMocks  func(*MockStore)
		config      Config
		expectCan   bool
		expectErr   error
	}{
		{
			name: "can bootstrap - no operators exist",
			setupMocks: func(store *MockStore) {
				store.On("CountOperators", mock.Anything).Return(0, nil)
			},
			config:    Config{BootstrapEnabled: true},
			expectCan: true,
			expectErr: nil,
		},
		{
			name: "cannot bootstrap - operators exist",
			setupMocks: func(store *MockStore) {
				store.On("CountOperators", mock.Anything).Return(1, nil)
			},
			config:    Config{BootstrapEnabled: true},
			expectCan: false,
			expectErr: nil,
		},
		{
			name:       "cannot bootstrap - disabled",
			setupMocks: func(store *MockStore) {},
			config:     Config{BootstrapEnabled: false},
			expectCan:  false,
			expectErr:  nil,
		},
		{
			name: "error checking operators",
			setupMocks: func(store *MockStore) {
				store.On("CountOperators", mock.Anything).Return(0, errors.New("database error"))
			},
			config:    Config{BootstrapEnabled: true},
			expectCan: false,
			expectErr: errors.New("database error"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := &MockStore{}
			service := &Service{
				store:  store,
				config: tt.config,
			}

			tt.setupMocks(store)

			ctx := context.Background()
			canBootstrap, err := service.CanBootstrap(ctx)

			assert.Equal(t, tt.expectCan, canBootstrap)
			if tt.expectErr != nil {
				assert.Error(t, err)
				assert.Equal(t, tt.expectErr.Error(), err.Error())
			} else {
				assert.NoError(t, err)
			}

			store.AssertExpectations(t)
		})
	}
}

func TestService_Bootstrap(t *testing.T) {
	identityID := uuid.New().String()
	ipAddress := "192.168.1.100"

	tests := []struct {
		name       string
		setupMocks func(*MockStore)
		expectErr  error
	}{
		{
			name: "successful bootstrap",
			setupMocks: func(store *MockStore) {
				store.On("CountOperators", mock.Anything).Return(0, nil)
				store.On("CreateOperator", mock.Anything, mock.MatchedBy(func(op interface{}) bool {
					// Verify operator has super_admin role
					return true
				})).Return(nil)
				store.On("LogAudit", mock.Anything, mock.Anything).Return(nil)
			},
			expectErr: nil,
		},
		{
			name: "bootstrap not allowed - operators exist",
			setupMocks: func(store *MockStore) {
				store.On("CountOperators", mock.Anything).Return(1, nil)
			},
			expectErr: ErrBootstrapNotAllowed,
		},
		{
			name: "create operator fails",
			setupMocks: func(store *MockStore) {
				store.On("CountOperators", mock.Anything).Return(0, nil)
				store.On("CreateOperator", mock.Anything, mock.Anything).Return(errors.New("create failed"))
			},
			expectErr: errors.New("create failed"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := &MockStore{}
			service := &Service{
				store:  store,
				config: Config{BootstrapEnabled: true},
			}

			tt.setupMocks(store)

			ctx := context.Background()
			err := service.Bootstrap(ctx, identityID, ipAddress)

			if tt.expectErr != nil {
				assert.Error(t, err)
				assert.Equal(t, tt.expectErr.Error(), err.Error())
			} else {
				assert.NoError(t, err)
			}

			store.AssertExpectations(t)
		})
	}
}

func TestService_CreateOperator(t *testing.T) {
	actorID := uuid.New().String()
	identityID := uuid.New().String()
	ipAddress := "192.168.1.100"

	// Mock actor (super admin)
	superAdminActor := map[string]interface{}{
		"id":          actorID,
		"role":        "super_admin",
		"permissions": []string{"*"},
	}

	// Mock actor (admin)
	adminActor := map[string]interface{}{
		"id":          actorID,
		"role":        "admin",
		"permissions": []string{"operators:create"},
	}

	tests := []struct {
		name        string
		role        string
		permissions []string
		setupMocks  func(*MockStore)
		expectErr   error
	}{
		{
			name:        "successful creation by super admin",
			role:        "admin",
			permissions: []string{"identities:read"},
			setupMocks: func(store *MockStore) {
				store.On("GetOperator", mock.Anything, actorID).Return(superAdminActor, nil)
				store.On("CreateOperator", mock.Anything, mock.Anything).Return(nil)
				store.On("LogAudit", mock.Anything, mock.Anything).Return(nil)
			},
			expectErr: nil,
		},
		{
			name:        "invalid role",
			role:        "invalid_role",
			permissions: []string{},
			setupMocks: func(store *MockStore) {
				store.On("GetOperator", mock.Anything, actorID).Return(superAdminActor, nil)
			},
			expectErr: ErrInvalidRole,
		},
		{
			name:        "admin trying to create super admin",
			role:        "super_admin",
			permissions: []string{},
			setupMocks: func(store *MockStore) {
				store.On("GetOperator", mock.Anything, actorID).Return(adminActor, nil)
			},
			expectErr: ErrPermissionDenied,
		},
		{
			name:        "actor not found",
			role:        "admin",
			permissions: []string{},
			setupMocks: func(store *MockStore) {
				store.On("GetOperator", mock.Anything, actorID).Return(nil, ErrOperatorNotFound)
			},
			expectErr: ErrOperatorNotFound,
		},
		{
			name:        "insufficient permissions",
			role:        "admin",
			permissions: []string{},
			setupMocks: func(store *MockStore) {
				// Actor without create permissions
				actor := map[string]interface{}{
					"id":          actorID,
					"role":        "viewer",
					"permissions": []string{"identities:read"},
				}
				store.On("GetOperator", mock.Anything, actorID).Return(actor, nil)
			},
			expectErr: ErrPermissionDenied,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := &MockStore{}
			service := New(store)

			tt.setupMocks(store)

			ctx := context.Background()
			err := service.CreateOperator(ctx, actorID, identityID, tt.role, tt.permissions, ipAddress)

			if tt.expectErr != nil {
				assert.Error(t, err)
				assert.True(t, errors.Is(err, tt.expectErr) || err.Error() == tt.expectErr.Error())
			} else {
				assert.NoError(t, err)
			}

			store.AssertExpectations(t)
		})
	}
}

func TestService_UpdateOperator(t *testing.T) {
	actorID := uuid.New().String()
	targetID := uuid.New().String()
	ipAddress := "192.168.1.100"

	superAdminActor := map[string]interface{}{
		"id":          actorID,
		"role":        "super_admin",
		"permissions": []string{"*"},
	}

	superAdminTarget := map[string]interface{}{
		"id":          targetID,
		"role":        "super_admin",
		"permissions": []string{"*"},
	}

	adminTarget := map[string]interface{}{
		"id":          targetID,
		"role":        "admin",
		"permissions": []string{"operators:update"},
	}

	tests := []struct {
		name        string
		targetID    string
		role        string
		permissions []string
		setupMocks  func(*MockStore)
		expectErr   error
	}{
		{
			name:        "successful update",
			targetID:    targetID,
			role:        "admin",
			permissions: []string{"identities:read"},
			setupMocks: func(store *MockStore) {
				store.On("GetOperator", mock.Anything, actorID).Return(superAdminActor, nil)
				store.On("GetOperator", mock.Anything, targetID).Return(adminTarget, nil)
				store.On("UpdateOperator", mock.Anything, targetID, "admin", []string{"identities:read"}).Return(nil)
				store.On("LogAudit", mock.Anything, mock.Anything).Return(nil)
			},
			expectErr: nil,
		},
		{
			name:        "self demotion from super admin",
			targetID:    actorID, // Same as actor (self)
			role:        "admin",
			permissions: []string{},
			setupMocks: func(store *MockStore) {
				store.On("GetOperator", mock.Anything, actorID).Return(superAdminActor, nil)
				store.On("GetOperator", mock.Anything, actorID).Return(superAdminActor, nil)
			},
			expectErr: ErrSelfDemotion,
		},
		{
			name:        "invalid role",
			targetID:    targetID,
			role:        "invalid_role",
			permissions: []string{},
			setupMocks: func(store *MockStore) {
				store.On("GetOperator", mock.Anything, actorID).Return(superAdminActor, nil)
				store.On("GetOperator", mock.Anything, targetID).Return(adminTarget, nil)
			},
			expectErr: ErrInvalidRole,
		},
		{
			name:        "non-super admin trying to modify super admin",
			targetID:    targetID,
			role:        "admin",
			permissions: []string{},
			setupMocks: func(store *MockStore) {
				// Actor is admin, target is super admin
				actor := map[string]interface{}{
					"id":          actorID,
					"role":        "admin",
					"permissions": []string{"operators:update"},
				}
				store.On("GetOperator", mock.Anything, actorID).Return(actor, nil)
				store.On("GetOperator", mock.Anything, targetID).Return(superAdminTarget, nil)
			},
			expectErr: ErrPermissionDenied,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := &MockStore{}
			service := New(store)

			tt.setupMocks(store)

			ctx := context.Background()
			err := service.UpdateOperator(ctx, actorID, tt.targetID, tt.role, tt.permissions, ipAddress)

			if tt.expectErr != nil {
				assert.Error(t, err)
				assert.True(t, errors.Is(err, tt.expectErr) || err.Error() == tt.expectErr.Error())
			} else {
				assert.NoError(t, err)
			}

			store.AssertExpectations(t)
		})
	}
}

func TestService_DeleteOperator(t *testing.T) {
	actorID := uuid.New().String()
	targetID := uuid.New().String()
	ipAddress := "192.168.1.100"

	superAdminActor := map[string]interface{}{
		"id":          actorID,
		"role":        "super_admin",
		"permissions": []string{"*"},
	}

	tests := []struct {
		name       string
		targetID   string
		setupMocks func(*MockStore)
		expectErr  error
	}{
		{
			name:     "successful deletion",
			targetID: targetID,
			setupMocks: func(store *MockStore) {
				target := map[string]interface{}{
					"id":          targetID,
					"role":        "admin",
					"permissions": []string{},
				}
				store.On("GetOperator", mock.Anything, actorID).Return(superAdminActor, nil)
				store.On("GetOperator", mock.Anything, targetID).Return(target, nil)
				store.On("DeleteOperator", mock.Anything, targetID).Return(nil)
				store.On("LogAudit", mock.Anything, mock.Anything).Return(nil)
			},
			expectErr: nil,
		},
		{
			name:     "self deletion",
			targetID: actorID, // Same as actor
			setupMocks: func(store *MockStore) {
				store.On("GetOperator", mock.Anything, actorID).Return(superAdminActor, nil)
				store.On("GetOperator", mock.Anything, actorID).Return(superAdminActor, nil)
			},
			expectErr: ErrSelfDeletion,
		},
		{
			name:     "target not found",
			targetID: targetID,
			setupMocks: func(store *MockStore) {
				store.On("GetOperator", mock.Anything, actorID).Return(superAdminActor, nil)
				store.On("GetOperator", mock.Anything, targetID).Return(nil, ErrOperatorNotFound)
			},
			expectErr: ErrOperatorNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := &MockStore{}
			service := New(store)

			tt.setupMocks(store)

			ctx := context.Background()
			err := service.DeleteOperator(ctx, actorID, tt.targetID, ipAddress)

			if tt.expectErr != nil {
				assert.Error(t, err)
				assert.True(t, errors.Is(err, tt.expectErr) || err.Error() == tt.expectErr.Error())
			} else {
				assert.NoError(t, err)
			}

			store.AssertExpectations(t)
		})
	}
}

func TestService_ListOperators(t *testing.T) {
	actorID := uuid.New().String()
	
	actor := map[string]interface{}{
		"id":          actorID,
		"role":        "admin",
		"permissions": []string{"operators:read"},
	}

	operators := []interface{}{
		map[string]interface{}{"id": "op1", "role": "admin"},
		map[string]interface{}{"id": "op2", "role": "operator"},
	}

	tests := []struct {
		name       string
		limit      int
		offset     int
		setupMocks func(*MockStore)
		expectErr  error
	}{
		{
			name:   "successful listing",
			limit:  10,
			offset: 0,
			setupMocks: func(store *MockStore) {
				store.On("GetOperator", mock.Anything, actorID).Return(actor, nil)
				store.On("ListOperators", mock.Anything, 10, 0).Return(operators, 2, nil)
			},
			expectErr: nil,
		},
		{
			name:   "limit too high",
			limit:  200, // Should be capped at 100
			offset: 0,
			setupMocks: func(store *MockStore) {
				store.On("GetOperator", mock.Anything, actorID).Return(actor, nil)
				store.On("ListOperators", mock.Anything, 100, 0).Return(operators, 2, nil)
			},
			expectErr: nil,
		},
		{
			name:   "insufficient permissions",
			limit:  10,
			offset: 0,
			setupMocks: func(store *MockStore) {
				// Actor without read permissions
				unauthorizedActor := map[string]interface{}{
					"id":          actorID,
					"role":        "viewer", // viewer has read permissions
					"permissions": []string{"identities:read"}, // but not operators:read
				}
				store.On("GetOperator", mock.Anything, actorID).Return(unauthorizedActor, nil)
			},
			expectErr: ErrPermissionDenied,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := &MockStore{}
			service := New(store)

			tt.setupMocks(store)

			ctx := context.Background()
			result, total, err := service.ListOperators(ctx, actorID, tt.limit, tt.offset)

			if tt.expectErr != nil {
				assert.Error(t, err)
				assert.Nil(t, result)
				assert.Equal(t, 0, total)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, result)
				assert.Equal(t, 2, total)
			}

			store.AssertExpectations(t)
		})
	}
}

// Test permission checking logic
func TestService_matchPermission(t *testing.T) {
	tests := []struct {
		name        string
		permission  string
		required    string
		shouldMatch bool
	}{
		{
			name:        "exact match",
			permission:  "identities:read",
			required:    "identities:read",
			shouldMatch: true,
		},
		{
			name:        "wildcard permission",
			permission:  "*",
			required:    "identities:read",
			shouldMatch: true,
		},
		{
			name:        "category wildcard",
			permission:  "identities:*",
			required:    "identities:read",
			shouldMatch: true,
		},
		{
			name:        "category wildcard mismatch",
			permission:  "sessions:*",
			required:    "identities:read",
			shouldMatch: false,
		},
		{
			name:        "no match",
			permission:  "identities:write",
			required:    "identities:read",
			shouldMatch: false,
		},
		{
			name:        "partial match fails",
			permission:  "identities:r",
			required:    "identities:read",
			shouldMatch: false,
		},
	}

	service := New(&MockStore{})

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			matches := service.matchPermission(tt.permission, tt.required)
			assert.Equal(t, tt.shouldMatch, matches)
		})
	}
}

func TestService_hasPermission(t *testing.T) {
	tests := []struct {
		name        string
		role        string
		permissions []string
		required    string
		shouldHave  bool
	}{
		{
			name:        "super admin has all permissions",
			role:        "super_admin",
			permissions: []string{}, // Individual permissions don't matter for super admin
			required:    "anything:action",
			shouldHave:  true,
		},
		{
			name:        "direct permission match",
			role:        "operator",
			permissions: []string{"identities:read", "sessions:read"},
			required:    "identities:read",
			shouldHave:  true,
		},
		{
			name:        "wildcard permission",
			role:        "admin",
			permissions: []string{"identities:*"},
			required:    "identities:create",
			shouldHave:  true,
		},
		{
			name:        "role default permission",
			role:        "admin",
			permissions: []string{}, // Use role defaults
			required:    "identities:read", // Admin role has identities:*
			shouldHave:  true,
		},
		{
			name:        "no permission",
			role:        "viewer",
			permissions: []string{"identities:read"},
			required:    "identities:write",
			shouldHave:  false,
		},
	}

	service := New(&MockStore{})

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hasPermission := service.hasPermission(tt.role, tt.permissions, tt.required)
			assert.Equal(t, tt.shouldHave, hasPermission)
		})
	}
}

func TestService_EvaluateCapability(t *testing.T) {
	operatorID := uuid.New().String()
	
	operator := map[string]interface{}{
		"id":          operatorID,
		"role":        "admin",
		"permissions": []string{"identities:read", "sessions:*"},
	}

	tests := []struct {
		name       string
		permission string
		setupMocks func(*MockStore)
		expectCan  bool
		expectErr  error
	}{
		{
			name:       "has permission",
			permission: "identities:read",
			setupMocks: func(store *MockStore) {
				store.On("GetOperator", mock.Anything, operatorID).Return(operator, nil)
			},
			expectCan: true,
			expectErr: nil,
		},
		{
			name:       "has wildcard permission",
			permission: "sessions:delete",
			setupMocks: func(store *MockStore) {
				store.On("GetOperator", mock.Anything, operatorID).Return(operator, nil)
			},
			expectCan: true,
			expectErr: nil,
		},
		{
			name:       "no permission",
			permission: "operators:create",
			setupMocks: func(store *MockStore) {
				store.On("GetOperator", mock.Anything, operatorID).Return(operator, nil)
			},
			expectCan: false,
			expectErr: nil,
		},
		{
			name:       "operator not found",
			permission: "identities:read",
			setupMocks: func(store *MockStore) {
				store.On("GetOperator", mock.Anything, operatorID).Return(nil, ErrOperatorNotFound)
			},
			expectCan: false,
			expectErr: ErrOperatorNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := &MockStore{}
			service := New(store)

			tt.setupMocks(store)

			ctx := context.Background()
			canPerform, err := service.EvaluateCapability(ctx, operatorID, tt.permission)

			assert.Equal(t, tt.expectCan, canPerform)
			if tt.expectErr != nil {
				assert.Error(t, err)
				assert.True(t, errors.Is(err, tt.expectErr) || err.Error() == tt.expectErr.Error())
			} else {
				assert.NoError(t, err)
			}

			store.AssertExpectations(t)
		})
	}
}

func TestService_EdgeCases(t *testing.T) {
	t.Run("empty role validation", func(t *testing.T) {
		service := New(&MockStore{})
		
		// Empty role should be invalid
		for _, role := range []string{"", "  ", "ADMIN", "Super_Admin"} {
			valid := false
			for _, validRole := range ValidRoles {
				if role == validRole {
					valid = true
					break
				}
			}
			assert.False(t, valid, "Role '%s' should be invalid", role)
		}
	})

	t.Run("permission format validation", func(t *testing.T) {
		service := New(&MockStore{})
		
		tests := []struct {
			permission string
			valid      bool
		}{
			{"identities:read", true},
			{"*", true},
			{"identities:*", true},
			{"", false},
			{"identities", false}, // Missing action
			{":read", false}, // Missing resource
			{"identities:", false}, // Missing action
			{"identities:read:extra", false}, // Too many parts
		}

		for _, tt := range tests {
			parts := strings.Split(tt.permission, ":")
			isValid := tt.permission == "*" || (len(parts) == 2 && parts[0] != "" && parts[1] != "")
			assert.Equal(t, tt.valid, isValid, "Permission '%s' validity", tt.permission)
		}
	})

	t.Run("operator struct validation", func(t *testing.T) {
		now := time.Now()
		
		operator := &Operator{
			ID:          uuid.New().String(),
			IdentityID:  uuid.New().String(),
			Role:        "admin",
			Permissions: []string{"identities:read"},
			CreatedAt:   now,
			UpdatedAt:   now,
		}

		// Validate required fields
		assert.NotEmpty(t, operator.ID)
		assert.NotEmpty(t, operator.IdentityID)
		assert.Contains(t, ValidRoles, operator.Role)
		assert.NotEmpty(t, operator.Permissions)
		assert.False(t, operator.CreatedAt.IsZero())
		assert.False(t, operator.UpdatedAt.IsZero())
	})
}

// Test concurrent operations
func TestService_ConcurrentCapabilityChecks(t *testing.T) {
	store := &MockStore{}
	service := New(store)

	operatorID := uuid.New().String()
	operator := map[string]interface{}{
		"id":          operatorID,
		"role":        "admin",
		"permissions": []string{"identities:*"},
	}

	// Setup mock for multiple concurrent calls
	store.On("GetOperator", mock.Anything, operatorID).Return(operator, nil).Times(10)

	done := make(chan bool, 10)
	
	for i := 0; i < 10; i++ {
		go func() {
			ctx := context.Background()
			canPerform, err := service.EvaluateCapability(ctx, operatorID, "identities:read")
			
			assert.NoError(t, err)
			assert.True(t, canPerform)
			done <- true
		}()
	}

	// Wait for all goroutines to complete
	for i := 0; i < 10; i++ {
		<-done
	}

	store.AssertExpectations(t)
}

// Benchmark permission checking
func BenchmarkMatchPermission(b *testing.B) {
	service := New(&MockStore{})
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		service.matchPermission("identities:*", "identities:read")
	}
}

func BenchmarkHasPermission(b *testing.B) {
	service := New(&MockStore{})
	permissions := []string{"identities:*", "sessions:read", "config:read"}
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		service.hasPermission("admin", permissions, "identities:create")
	}
}