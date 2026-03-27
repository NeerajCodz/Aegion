package scim

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// NoOpStore is a simple store that does nothing
type NoOpStore struct{}

func (n *NoOpStore) GetUserByID(ctx context.Context, id string) (*SCIMUser, error) {
	return nil, nil
}

func (n *NoOpStore) GetUserByUserName(ctx context.Context, userName string) (*SCIMUser, error) {
	return nil, nil
}

func (n *NoOpStore) GetUserByExternalID(ctx context.Context, externalID string) (*SCIMUser, error) {
	return nil, nil
}

func (n *NoOpStore) ListUsers(ctx context.Context, filter *Filter, sortBy string, sortOrder SortOrder, startIndex, count int) ([]*SCIMUser, int, error) {
	return nil, 0, nil
}

func (n *NoOpStore) CreateUser(ctx context.Context, user *SCIMUser) error {
	return nil
}

func (n *NoOpStore) UpdateUser(ctx context.Context, user *SCIMUser) error {
	return nil
}

func (n *NoOpStore) PatchUser(ctx context.Context, id string, operations []PatchOperation) (*SCIMUser, error) {
	return nil, nil
}

func (n *NoOpStore) DeleteUser(ctx context.Context, id string) error {
	return nil
}

func (n *NoOpStore) GetGroupByID(ctx context.Context, id string) (*SCIMGroup, error) {
	return nil, nil
}

func (n *NoOpStore) GetGroupByDisplayName(ctx context.Context, displayName string) (*SCIMGroup, error) {
	return nil, nil
}

func (n *NoOpStore) ListGroups(ctx context.Context, filter *Filter, sortBy string, sortOrder SortOrder, startIndex, count int) ([]*SCIMGroup, int, error) {
	return nil, 0, nil
}

func (n *NoOpStore) CreateGroup(ctx context.Context, group *SCIMGroup) error {
	return nil
}

func (n *NoOpStore) UpdateGroup(ctx context.Context, group *SCIMGroup) error {
	return nil
}

func (n *NoOpStore) PatchGroup(ctx context.Context, id string, operations []PatchOperation) (*SCIMGroup, error) {
	return nil, nil
}

func (n *NoOpStore) DeleteGroup(ctx context.Context, id string) error {
	return nil
}

func (n *NoOpStore) GetSCIMMapping(ctx context.Context, id uuid.UUID) (*SCIMMapping, error) {
	return nil, nil
}

func (n *NoOpStore) ListSCIMMappings(ctx context.Context) ([]*SCIMMapping, error) {
	return nil, nil
}

func (n *NoOpStore) CreateSCIMMapping(ctx context.Context, mapping *SCIMMapping) error {
	return nil
}

func (n *NoOpStore) UpdateSCIMMapping(ctx context.Context, mapping *SCIMMapping) error {
	return nil
}

func (n *NoOpStore) DeleteSCIMMapping(ctx context.Context, id uuid.UUID) error {
	return nil
}

func (n *NoOpStore) GetSCIMTokenByPrefix(ctx context.Context, prefix string) (*SCIMToken, error) {
	return nil, nil
}

func (n *NoOpStore) ListSCIMTokens(ctx context.Context) ([]*SCIMToken, error) {
	return nil, nil
}

func (n *NoOpStore) CreateSCIMToken(ctx context.Context, token *SCIMToken) error {
	return nil
}

func (n *NoOpStore) UpdateSCIMTokenLastUsed(ctx context.Context, id uuid.UUID) error {
	return nil
}

func (n *NoOpStore) DeleteSCIMToken(ctx context.Context, id uuid.UUID) error {
	return nil
}
type MockStore struct {
	mock.Mock
}

func (m *MockStore) GetUserByID(ctx context.Context, id string) (*SCIMUser, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*SCIMUser), args.Error(1)
}

func (m *MockStore) GetUserByUserName(ctx context.Context, userName string) (*SCIMUser, error) {
	args := m.Called(ctx, userName)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*SCIMUser), args.Error(1)
}

func (m *MockStore) GetUserByExternalID(ctx context.Context, externalID string) (*SCIMUser, error) {
	args := m.Called(ctx, externalID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*SCIMUser), args.Error(1)
}

func (m *MockStore) ListUsers(ctx context.Context, filter *Filter, sortBy string, sortOrder SortOrder, startIndex, count int) ([]*SCIMUser, int, error) {
	args := m.Called(ctx, filter, sortBy, sortOrder, startIndex, count)
	if args.Get(0) == nil {
		return nil, args.Int(1), args.Error(2)
	}
	return args.Get(0).([]*SCIMUser), args.Int(1), args.Error(2)
}

func (m *MockStore) CreateUser(ctx context.Context, user *SCIMUser) error {
	args := m.Called(ctx, user)
	return args.Error(0)
}

func (m *MockStore) UpdateUser(ctx context.Context, user *SCIMUser) error {
	args := m.Called(ctx, user)
	return args.Error(0)
}

func (m *MockStore) PatchUser(ctx context.Context, id string, operations []PatchOperation) (*SCIMUser, error) {
	args := m.Called(ctx, id, operations)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*SCIMUser), args.Error(1)
}

func (m *MockStore) DeleteUser(ctx context.Context, id string) error {
	args := m.Called(ctx, id)
	return args.Error(0)
}

func (m *MockStore) GetGroupByID(ctx context.Context, id string) (*SCIMGroup, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*SCIMGroup), args.Error(1)
}

func (m *MockStore) GetGroupByDisplayName(ctx context.Context, displayName string) (*SCIMGroup, error) {
	args := m.Called(ctx, displayName)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*SCIMGroup), args.Error(1)
}

func (m *MockStore) ListGroups(ctx context.Context, filter *Filter, sortBy string, sortOrder SortOrder, startIndex, count int) ([]*SCIMGroup, int, error) {
	args := m.Called(ctx, filter, sortBy, sortOrder, startIndex, count)
	if args.Get(0) == nil {
		return nil, args.Int(1), args.Error(2)
	}
	return args.Get(0).([]*SCIMGroup), args.Int(1), args.Error(2)
}

func (m *MockStore) CreateGroup(ctx context.Context, group *SCIMGroup) error {
	args := m.Called(ctx, group)
	return args.Error(0)
}

func (m *MockStore) UpdateGroup(ctx context.Context, group *SCIMGroup) error {
	args := m.Called(ctx, group)
	return args.Error(0)
}

func (m *MockStore) PatchGroup(ctx context.Context, id string, operations []PatchOperation) (*SCIMGroup, error) {
	args := m.Called(ctx, id, operations)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*SCIMGroup), args.Error(1)
}

func (m *MockStore) DeleteGroup(ctx context.Context, id string) error {
	args := m.Called(ctx, id)
	return args.Error(0)
}

func (m *MockStore) GetSCIMMapping(ctx context.Context, id uuid.UUID) (*SCIMMapping, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*SCIMMapping), args.Error(1)
}

func (m *MockStore) ListSCIMMappings(ctx context.Context) ([]*SCIMMapping, error) {
	args := m.Called(ctx)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*SCIMMapping), args.Error(1)
}

func (m *MockStore) CreateSCIMMapping(ctx context.Context, mapping *SCIMMapping) error {
	args := m.Called(ctx, mapping)
	return args.Error(0)
}

func (m *MockStore) UpdateSCIMMapping(ctx context.Context, mapping *SCIMMapping) error {
	args := m.Called(ctx, mapping)
	return args.Error(0)
}

func (m *MockStore) DeleteSCIMMapping(ctx context.Context, id uuid.UUID) error {
	args := m.Called(ctx, id)
	return args.Error(0)
}

func (m *MockStore) GetSCIMTokenByPrefix(ctx context.Context, prefix string) (*SCIMToken, error) {
	args := m.Called(ctx, prefix)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*SCIMToken), args.Error(1)
}

func (m *MockStore) ListSCIMTokens(ctx context.Context) ([]*SCIMToken, error) {
	args := m.Called(ctx)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*SCIMToken), args.Error(1)
}

func (m *MockStore) CreateSCIMToken(ctx context.Context, token *SCIMToken) error {
	args := m.Called(ctx, token)
	return args.Error(0)
}

func (m *MockStore) UpdateSCIMTokenLastUsed(ctx context.Context, id uuid.UUID) error {
	args := m.Called(ctx, id)
	return args.Error(0)
}

func (m *MockStore) DeleteSCIMToken(ctx context.Context, id uuid.UUID) error {
	args := m.Called(ctx, id)
	return args.Error(0)
}

// ============== SERVICE CREATION TESTS ==============

// Test Service Creation
func TestNewService(t *testing.T) {
	mockStore := &MockStore{}

	service := NewService(mockStore, nil)

	assert.NotNil(t, service)
	assert.Equal(t, mockStore, service.store)
}

// ============== SERVICE PROVIDER CONFIG TESTS ==============

// Test Service Provider Config
func TestGetServiceProviderConfig(t *testing.T) {
	mockStore := &MockStore{}
	service := NewService(mockStore, nil)

	config := service.GetServiceProviderConfig()

	assert.NotNil(t, config)
	assert.Contains(t, config.Schemas, "urn:ietf:params:scim:schemas:core:2.0:ServiceProviderConfig")
	assert.True(t, config.Patch.Supported)
	assert.False(t, config.Bulk.Supported)
	assert.True(t, config.Filter.Supported)
	assert.Equal(t, 1000, config.Filter.MaxResults)
	assert.True(t, config.Sort.Supported)
	assert.False(t, config.ETag.Supported)
	assert.False(t, config.ChangePassword.Supported)
}

// ============== SCHEMA TESTS ==============

// Test Get Schemas
func TestGetSchemas(t *testing.T) {
	mockStore := &MockStore{}
	service := NewService(mockStore, nil)

	schemas := service.GetSchemas()

	assert.Len(t, schemas, 2)

	// Check User schema
	userSchema := schemas[0]
	assert.Equal(t, SchemaUser, userSchema.ID)
	assert.Equal(t, "User", userSchema.Name)
	assert.NotEmpty(t, userSchema.Attributes)
	assert.Equal(t, "User Account", userSchema.Description)

	// Check Group schema
	groupSchema := schemas[1]
	assert.Equal(t, SchemaGroup, groupSchema.ID)
	assert.Equal(t, "Group", groupSchema.Name)
	assert.NotEmpty(t, groupSchema.Attributes)
	assert.Equal(t, "Group", groupSchema.Description)
}

// Test User Schema Structure
func TestUserSchemaAttributes(t *testing.T) {
	mockStore := &MockStore{}
	service := NewService(mockStore, nil)

	schemas := service.GetSchemas()
	userSchema := schemas[0]

	// Check for required attributes
	attributeNames := make(map[string]bool)
	for _, attr := range userSchema.Attributes {
		attributeNames[attr.Name] = true
	}

	assert.True(t, attributeNames["userName"])
	assert.True(t, attributeNames["name"])
	assert.True(t, attributeNames["displayName"])
	assert.True(t, attributeNames["emails"])
	assert.True(t, attributeNames["active"])
	assert.True(t, attributeNames["groups"])
}

// Test Group Schema Structure
func TestGroupSchemaAttributes(t *testing.T) {
	mockStore := &MockStore{}
	service := NewService(mockStore, nil)

	schemas := service.GetSchemas()
	groupSchema := schemas[1]

	// Check for required attributes
	attributeNames := make(map[string]bool)
	for _, attr := range groupSchema.Attributes {
		attributeNames[attr.Name] = true
	}

	assert.True(t, attributeNames["displayName"])
	assert.True(t, attributeNames["members"])
}

// ============== USER OPERATION TESTS ==============

// Test Create User
func TestCreateUser(t *testing.T) {
	mockStore := &MockStore{}
	service := NewService(mockStore, nil)
	ctx := context.Background()

	user := &SCIMUser{
		UserName: "john.doe",
		Name: &Name{
			GivenName:  "John",
			FamilyName: "Doe",
		},
		Emails: []Email{
			{Value: "john.doe@example.com", Primary: true},
		},
		Active: true,
	}

	mockStore.On("CreateUser", ctx, mock.MatchedBy(func(u *SCIMUser) bool {
		return u.UserName == "john.doe" && u.ID != ""
	})).Return(nil)

	createdUser, err := service.CreateUser(ctx, user)

	assert.NoError(t, err)
	assert.NotNil(t, createdUser)
	assert.NotEmpty(t, createdUser.ID)
	assert.Contains(t, createdUser.Schemas, SchemaUser)
	assert.NotNil(t, createdUser.Meta.Created)
	assert.NotNil(t, createdUser.Meta.LastModified)
	assert.Equal(t, "User", createdUser.Meta.ResourceType)
	assert.Equal(t, "1", createdUser.Meta.Version)
	mockStore.AssertExpectations(t)
}

// Test Create User Missing UserName
func TestCreateUserMissingUserName(t *testing.T) {
	mockStore := &MockStore{}
	service := NewService(mockStore, nil)
	ctx := context.Background()

	user := &SCIMUser{
		Name: &Name{
			GivenName:  "John",
			FamilyName: "Doe",
		},
		Active: true,
	}

	_, err := service.CreateUser(ctx, user)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "userName is required")
	mockStore.AssertNotCalled(t, "CreateUser")
}

// Test Get User
func TestGetUser(t *testing.T) {
	mockStore := &MockStore{}
	service := NewService(mockStore, nil)
	ctx := context.Background()
	userID := uuid.New().String()

	expectedUser := &SCIMUser{
		ID:       userID,
		UserName: "john.doe",
		Active:   true,
	}

	mockStore.On("GetUserByID", ctx, userID).Return(expectedUser, nil)

	user, err := service.GetUser(ctx, userID)

	assert.NoError(t, err)
	assert.Equal(t, expectedUser, user)
	mockStore.AssertExpectations(t)
}

// Test Get User Not Found
func TestGetUserNotFound(t *testing.T) {
	mockStore := &MockStore{}
	service := NewService(mockStore, nil)
	ctx := context.Background()
	userID := uuid.New().String()

	mockStore.On("GetUserByID", ctx, userID).Return(nil, fmt.Errorf("not found"))

	user, err := service.GetUser(ctx, userID)

	assert.Error(t, err)
	assert.Nil(t, user)
	mockStore.AssertExpectations(t)
}

// Test List Users
func TestListUsers(t *testing.T) {
	mockStore := &MockStore{}
	service := NewService(mockStore, nil)
	ctx := context.Background()

	users := []*SCIMUser{
		{ID: "1", UserName: "user1", Active: true},
		{ID: "2", UserName: "user2", Active: true},
	}

	mockStore.On("ListUsers", ctx, (*Filter)(nil), "", SortAscending, 1, 20).Return(users, 2, nil)

	response, err := service.ListUsers(ctx, "", "", SortAscending, 1, 20)

	assert.NoError(t, err)
	assert.NotNil(t, response)
	assert.Equal(t, 2, response.TotalResults)
	assert.Equal(t, 2, response.ItemsPerPage)
	assert.Equal(t, 1, response.StartIndex)
	assert.Equal(t, SchemaListResponse, response.Schemas[0])
	assert.Equal(t, users, response.Resources)
	mockStore.AssertExpectations(t)
}

// Test List Users Pagination
func TestListUsersPagination(t *testing.T) {
	mockStore := &MockStore{}
	service := NewService(mockStore, nil)
	ctx := context.Background()

	users := []*SCIMUser{
		{ID: "3", UserName: "user3", Active: true},
	}

	// Note: count=5 gets passed to store as-is (no default applied since count > 0)
	mockStore.On("ListUsers", ctx, mock.Anything, "", SortAscending, 2, 5).Return(users, 10, nil)

	response, err := service.ListUsers(ctx, "", "", SortAscending, 2, 5)

	assert.NoError(t, err)
	assert.Equal(t, 10, response.TotalResults)
	assert.Equal(t, 2, response.StartIndex)
	assert.Equal(t, 1, response.ItemsPerPage) // ItemsPerPage is len(users) which is 1
}

// Test List Users With Count Limit
func TestListUsersCountLimit(t *testing.T) {
	mockStore := &MockStore{}
	service := NewService(mockStore, nil)
	ctx := context.Background()

	users := []*SCIMUser{}
	// Should clamp count to 1000
	mockStore.On("ListUsers", ctx, mock.Anything, "", SortAscending, 1, 1000).Return(users, 0, nil)

	response, err := service.ListUsers(ctx, "", "", SortAscending, 1, 5000)

	assert.NoError(t, err)
	assert.Equal(t, 0, response.TotalResults)
}

// Test List Users Pagination Defaults
func TestListUsersDefaults(t *testing.T) {
	mockStore := &MockStore{}
	service := NewService(mockStore, nil)
	ctx := context.Background()

	users := []*SCIMUser{}
	// Should set defaults: startIndex=1, count=20
	mockStore.On("ListUsers", ctx, mock.Anything, "", SortAscending, 1, 20).Return(users, 0, nil)

	_, err := service.ListUsers(ctx, "", "", SortAscending, 0, 0)

	assert.NoError(t, err)
	mockStore.AssertExpectations(t)
}

// Test Update User
func TestUpdateUser(t *testing.T) {
	mockStore := &MockStore{}
	service := NewService(mockStore, nil)
	ctx := context.Background()
	userID := uuid.New().String()

	now := time.Now().UTC()
	existingUser := &SCIMUser{
		ID:       userID,
		UserName: "john",
		Active:   false,
		Meta: Meta{
			Created:      &now,
			LastModified: &now,
		},
	}

	updatedUserData := &SCIMUser{
		UserName: "john.updated",
		Active:   true,
	}

	mockStore.On("GetUserByID", ctx, userID).Return(existingUser, nil)
	mockStore.On("UpdateUser", ctx, mock.MatchedBy(func(u *SCIMUser) bool {
		return u.ID == userID && u.UserName == "john.updated"
	})).Return(nil)

	updatedUser, err := service.UpdateUser(ctx, userID, updatedUserData)

	assert.NoError(t, err)
	assert.NotNil(t, updatedUser)
	assert.Equal(t, userID, updatedUser.ID)
	assert.Contains(t, updatedUser.Schemas, SchemaUser)
	mockStore.AssertExpectations(t)
}

// Test Update User Not Found
func TestUpdateUserNotFound(t *testing.T) {
	mockStore := &MockStore{}
	service := NewService(mockStore, nil)
	ctx := context.Background()
	userID := uuid.New().String()

	updatedUserData := &SCIMUser{
		UserName: "john.updated",
		Active:   true,
	}

	mockStore.On("GetUserByID", ctx, userID).Return(nil, fmt.Errorf("not found"))

	_, err := service.UpdateUser(ctx, userID, updatedUserData)

	assert.Error(t, err)
	mockStore.AssertExpectations(t)
}

// Test Patch User
func TestPatchUser(t *testing.T) {
	mockStore := &MockStore{}
	service := NewService(mockStore, nil)
	ctx := context.Background()
	userID := uuid.New().String()

	operations := []PatchOperation{
		{
			Op:    "replace",
			Path:  "active",
			Value: false,
		},
	}

	patchedUser := &SCIMUser{
		ID:       userID,
		UserName: "testuser",
		Active:   false,
	}

	mockStore.On("PatchUser", ctx, userID, operations).Return(patchedUser, nil)

	result, err := service.PatchUser(ctx, userID, operations)

	assert.NoError(t, err)
	assert.Equal(t, patchedUser, result)
	mockStore.AssertExpectations(t)
}

// Test Delete User
func TestDeleteUser(t *testing.T) {
	mockStore := &MockStore{}
	service := NewService(mockStore, nil)
	ctx := context.Background()
	userID := uuid.New().String()

	mockStore.On("DeleteUser", ctx, userID).Return(nil)

	err := service.DeleteUser(ctx, userID)

	assert.NoError(t, err)
	mockStore.AssertExpectations(t)
}

// Test Delete User Error
func TestDeleteUserError(t *testing.T) {
	mockStore := &MockStore{}
	service := NewService(mockStore, nil)
	ctx := context.Background()
	userID := uuid.New().String()

	mockStore.On("DeleteUser", ctx, userID).Return(fmt.Errorf("deletion failed"))

	err := service.DeleteUser(ctx, userID)

	assert.Error(t, err)
	mockStore.AssertExpectations(t)
}

// ============== GROUP OPERATION TESTS ==============

// Test Create Group
func TestCreateGroup(t *testing.T) {
	mockStore := &MockStore{}
	service := NewService(mockStore, nil)
	ctx := context.Background()

	group := &SCIMGroup{
		DisplayName: "Developers",
		Members: []Member{
			{Value: "user1", Type: "User"},
		},
	}

	mockStore.On("CreateGroup", ctx, mock.MatchedBy(func(g *SCIMGroup) bool {
		return g.DisplayName == "Developers" && g.ID != ""
	})).Return(nil)

	createdGroup, err := service.CreateGroup(ctx, group)

	assert.NoError(t, err)
	assert.NotNil(t, createdGroup)
	assert.NotEmpty(t, createdGroup.ID)
	assert.Contains(t, createdGroup.Schemas, SchemaGroup)
	assert.NotNil(t, createdGroup.Meta.Created)
	assert.Equal(t, "Group", createdGroup.Meta.ResourceType)
	mockStore.AssertExpectations(t)
}

// Test Create Group Missing DisplayName
func TestCreateGroupMissingDisplayName(t *testing.T) {
	mockStore := &MockStore{}
	service := NewService(mockStore, nil)
	ctx := context.Background()

	group := &SCIMGroup{
		Members: []Member{
			{Value: "user1", Type: "User"},
		},
	}

	_, err := service.CreateGroup(ctx, group)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "displayName is required")
	mockStore.AssertNotCalled(t, "CreateGroup")
}

// Test Get Group
func TestGetGroup(t *testing.T) {
	mockStore := &MockStore{}
	service := NewService(mockStore, nil)
	ctx := context.Background()
	groupID := uuid.New().String()

	expectedGroup := &SCIMGroup{
		ID:          groupID,
		DisplayName: "Developers",
	}

	mockStore.On("GetGroupByID", ctx, groupID).Return(expectedGroup, nil)

	group, err := service.GetGroup(ctx, groupID)

	assert.NoError(t, err)
	assert.Equal(t, expectedGroup, group)
	mockStore.AssertExpectations(t)
}

// Test List Groups
func TestListGroups(t *testing.T) {
	mockStore := &MockStore{}
	service := NewService(mockStore, nil)
	ctx := context.Background()

	groups := []*SCIMGroup{
		{ID: "g1", DisplayName: "Developers"},
		{ID: "g2", DisplayName: "QA"},
	}

	mockStore.On("ListGroups", ctx, (*Filter)(nil), "", SortAscending, 1, 20).Return(groups, 2, nil)

	response, err := service.ListGroups(ctx, "", "", SortAscending, 1, 20)

	assert.NoError(t, err)
	assert.NotNil(t, response)
	assert.Equal(t, 2, response.TotalResults)
	assert.Equal(t, SchemaListResponse, response.Schemas[0])
	assert.Equal(t, groups, response.Resources)
	mockStore.AssertExpectations(t)
}

// Test Update Group
func TestUpdateGroup(t *testing.T) {
	mockStore := &MockStore{}
	service := NewService(mockStore, nil)
	ctx := context.Background()
	groupID := uuid.New().String()

	now := time.Now().UTC()
	existingGroup := &SCIMGroup{
		ID:          groupID,
		DisplayName: "OldName",
		Meta: Meta{
			Created:      &now,
			LastModified: &now,
		},
	}

	updatedGroupData := &SCIMGroup{
		DisplayName: "NewName",
	}

	mockStore.On("GetGroupByID", ctx, groupID).Return(existingGroup, nil)
	mockStore.On("UpdateGroup", ctx, mock.MatchedBy(func(g *SCIMGroup) bool {
		return g.ID == groupID && g.DisplayName == "NewName"
	})).Return(nil)

	updatedGroup, err := service.UpdateGroup(ctx, groupID, updatedGroupData)

	assert.NoError(t, err)
	assert.NotNil(t, updatedGroup)
	assert.Equal(t, groupID, updatedGroup.ID)
	mockStore.AssertExpectations(t)
}

// Test Patch Group
func TestPatchGroup(t *testing.T) {
	mockStore := &MockStore{}
	service := NewService(mockStore, nil)
	ctx := context.Background()
	groupID := uuid.New().String()

	operations := []PatchOperation{
		{
			Op:    "replace",
			Path:  "displayName",
			Value: "UpdatedGroup",
		},
	}

	patchedGroup := &SCIMGroup{
		ID:          groupID,
		DisplayName: "UpdatedGroup",
	}

	mockStore.On("PatchGroup", ctx, groupID, operations).Return(patchedGroup, nil)

	result, err := service.PatchGroup(ctx, groupID, operations)

	assert.NoError(t, err)
	assert.Equal(t, patchedGroup, result)
	mockStore.AssertExpectations(t)
}

// Test Delete Group
func TestDeleteGroup(t *testing.T) {
	mockStore := &MockStore{}
	service := NewService(mockStore, nil)
	ctx := context.Background()
	groupID := uuid.New().String()

	mockStore.On("DeleteGroup", ctx, groupID).Return(nil)

	err := service.DeleteGroup(ctx, groupID)

	assert.NoError(t, err)
	mockStore.AssertExpectations(t)
}

// ============== TOKEN MANAGEMENT TESTS ==============

// Test Create SCIM Token
func TestCreateSCIMToken(t *testing.T) {
	mockStore := &MockStore{}
	service := NewService(mockStore, nil)
	ctx := context.Background()
	createdBy := uuid.New()

	mockStore.On("CreateSCIMToken", ctx, mock.MatchedBy(func(token *SCIMToken) bool {
		return token.Name == "Test Token" && token.Prefix != ""
	})).Return(nil)

	token, tokenString, err := service.CreateSCIMToken(ctx, "Test Token", "Test token description", []string{"users:read", "groups:read"}, nil, createdBy)

	assert.NoError(t, err)
	assert.NotNil(t, token)
	assert.NotEmpty(t, tokenString)
	assert.True(t, strings.HasPrefix(tokenString, "aegion_scim_"))
	assert.Equal(t, "Test Token", token.Name)
	assert.Equal(t, "Test token description", token.Description)
	assert.Equal(t, []string{"users:read", "groups:read"}, token.Permissions)
	assert.Equal(t, createdBy, token.CreatedBy)
	assert.True(t, token.Active)
	assert.NotEmpty(t, token.Prefix)
	assert.NotEmpty(t, token.TokenHash)
	mockStore.AssertExpectations(t)
}

// Test Create Token with Expiration
func TestCreateSCIMTokenWithExpiration(t *testing.T) {
	mockStore := &MockStore{}
	service := NewService(mockStore, nil)
	ctx := context.Background()
	createdBy := uuid.New()
	expiresAt := time.Now().Add(24 * time.Hour).UTC()

	mockStore.On("CreateSCIMToken", ctx, mock.MatchedBy(func(token *SCIMToken) bool {
		return token.ExpiresAt != nil && token.ExpiresAt.Equal(expiresAt)
	})).Return(nil)

	token, _, err := service.CreateSCIMToken(ctx, "Expiring Token", "", []string{}, &expiresAt, createdBy)

	assert.NoError(t, err)
	assert.NotNil(t, token.ExpiresAt)
	mockStore.AssertExpectations(t)
}

// Test Validate Token Success
func TestValidateTokenSuccess(t *testing.T) {
	mockStore := &MockStore{}
	service := NewService(mockStore, nil)
	ctx := context.Background()

	// Create a real token first to get the actual hash
	mockStore.On("CreateSCIMToken", ctx, mock.AnythingOfType("*scim.SCIMToken")).Return(nil)
	createdToken, tokenString, err := service.CreateSCIMToken(ctx, "Test Token", "Test description", []string{"users:read"}, nil, uuid.New())
	assert.NoError(t, err)

	// Now mock the validation path
	mockStore.On("GetSCIMTokenByPrefix", ctx, tokenString[12:24]).Return(createdToken, nil)
	mockStore.On("UpdateSCIMTokenLastUsed", mock.Anything, createdToken.ID).Return(nil)

	validatedToken, err := service.ValidateToken(ctx, tokenString)

	assert.NoError(t, err)
	assert.Equal(t, createdToken.ID, validatedToken.ID)
	assert.Equal(t, createdToken.Permissions, validatedToken.Permissions)

	// Give time for the goroutine to complete
	time.Sleep(10 * time.Millisecond)
}

// Test Validate Token Invalid Format
func TestValidateTokenInvalidFormat(t *testing.T) {
	mockStore := &MockStore{}
	service := NewService(mockStore, nil)
	ctx := context.Background()

	_, err := service.ValidateToken(ctx, "invalid_token")

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid token format")
	mockStore.AssertNotCalled(t, "GetSCIMTokenByPrefix")
}

// Test Validate Token Invalid Length
func TestValidateTokenInvalidLength(t *testing.T) {
	mockStore := &MockStore{}
	service := NewService(mockStore, nil)
	ctx := context.Background()

	_, err := service.ValidateToken(ctx, "aegion_scim_short")

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid token length")
	mockStore.AssertNotCalled(t, "GetSCIMTokenByPrefix")
}

// Test Validate Token Not Found
func TestValidateTokenNotFound(t *testing.T) {
	mockStore := &MockStore{}
	service := NewService(mockStore, nil)
	ctx := context.Background()

	tokenString := "aegion_scim_1234567890abcdefghijklmn"
	prefix := tokenString[12:24] // "1234567890ab"

	mockStore.On("GetSCIMTokenByPrefix", ctx, prefix).Return(nil, fmt.Errorf("not found"))

	_, err := service.ValidateToken(ctx, tokenString)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "token not found")
}

// Test Validate Inactive Token
func TestValidateInactiveToken(t *testing.T) {
	mockStore := &MockStore{}
	service := NewService(mockStore, nil)
	ctx := context.Background()

	inactiveToken := &SCIMToken{
		ID:     uuid.New(),
		Active: false,
	}

	mockStore.On("GetSCIMTokenByPrefix", ctx, mock.Anything).Return(inactiveToken, nil)

	_, err := service.ValidateToken(ctx, "aegion_scim_1234567890abcdefghijklmn")

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "token is inactive")
}

// Test Validate Expired Token
func TestValidateExpiredToken(t *testing.T) {
	mockStore := &MockStore{}
	service := NewService(mockStore, nil)
	ctx := context.Background()

	expiredTime := time.Now().UTC().Add(-1 * time.Hour)
	expiredToken := &SCIMToken{
		ID:        uuid.New(),
		Active:    true,
		ExpiresAt: &expiredTime,
	}

	mockStore.On("GetSCIMTokenByPrefix", ctx, mock.Anything).Return(expiredToken, nil)

	_, err := service.ValidateToken(ctx, "aegion_scim_1234567890abcdefghijklmn")

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "token has expired")
}

// Test Has Permission - Exact Match
func TestHasPermissionExactMatch(t *testing.T) {
	token := &SCIMToken{
		Permissions: []string{"users:read", "groups:write"},
	}
	service := &Service{}

	assert.True(t, service.HasPermission(token, "users:read"))
	assert.True(t, service.HasPermission(token, "groups:write"))
	assert.False(t, service.HasPermission(token, "users:write"))
}

// Test Has Permission - Wildcard All
func TestHasPermissionWildcardAll(t *testing.T) {
	token := &SCIMToken{
		Permissions: []string{"*"},
	}
	service := &Service{}

	assert.True(t, service.HasPermission(token, "users:read"))
	assert.True(t, service.HasPermission(token, "groups:write"))
	assert.True(t, service.HasPermission(token, "anything:else"))
}

// Test Has Permission - Wildcard Prefix
func TestHasPermissionWildcardPrefix(t *testing.T) {
	token := &SCIMToken{
		Permissions: []string{"users:*", "groups:read"},
	}
	service := &Service{}

	assert.True(t, service.HasPermission(token, "users:read"))
	assert.True(t, service.HasPermission(token, "users:write"))
	assert.True(t, service.HasPermission(token, "groups:read"))
	assert.False(t, service.HasPermission(token, "groups:write"))
}

// Test Has Permission - No Permission
func TestHasPermissionNone(t *testing.T) {
	token := &SCIMToken{
		Permissions: []string{},
	}
	service := &Service{}

	assert.False(t, service.HasPermission(token, "users:read"))
}

// ============== FILTER PARSING TESTS ==============

// Test Parse Filter - Simple Equality
func TestParseFilterSimpleEquality(t *testing.T) {
	service := &Service{}

	filter, err := service.parseFilter(`userName eq "john.doe"`)

	assert.NoError(t, err)
	assert.Equal(t, "userName", filter.Attribute)
	assert.Equal(t, "eq", filter.Operator)
	assert.Equal(t, "john.doe", filter.Value)
}

// Test Parse Filter - Different Operator
func TestParseFilterDifferentOperator(t *testing.T) {
	service := &Service{}

	filter, err := service.parseFilter(`active eq true`)

	assert.NoError(t, err)
	assert.Equal(t, "active", filter.Attribute)
	assert.Equal(t, "eq", filter.Operator)
	assert.Equal(t, "true", filter.Value)
}

// Test Parse Filter - Single Quotes
func TestParseFilterSingleQuotes(t *testing.T) {
	service := &Service{}

	filter, err := service.parseFilter(`name eq 'test'`)

	assert.NoError(t, err)
	assert.Equal(t, "name", filter.Attribute)
	assert.Equal(t, "eq", filter.Operator)
	assert.Equal(t, "test", filter.Value)
}

// Test Parse Filter - Invalid Format
func TestParseFilterInvalidFormat(t *testing.T) {
	service := &Service{}

	_, err := service.parseFilter("invalid filter")

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid filter format")
}

// Test Parse Filter - Too Many Parts
func TestParseFilterTooManyParts(t *testing.T) {
	service := &Service{}

	_, err := service.parseFilter(`userName eq "john" extra`)

	assert.Error(t, err)
}

// Test Parse Filter - Empty String
func TestParseFilterEmptyString(t *testing.T) {
	service := &Service{}

	_, err := service.parseFilter("")

	assert.Error(t, err)
}

// ============== CONTEXT TESTS ==============

// Test Context With SCIM Token
func TestContextWithSCIMToken(t *testing.T) {
	ctx := context.Background()
	token := &SCIMToken{
		ID:   uuid.New(),
		Name: "test-token",
	}

	ctxWithToken := contextWithSCIMToken(ctx, token)
	retrievedToken := scimTokenFromContext(ctxWithToken)

	assert.NotNil(t, retrievedToken)
	assert.Equal(t, token.ID, retrievedToken.ID)
	assert.Equal(t, token.Name, retrievedToken.Name)
}

// Test Context Token Not Present
func TestContextTokenNotPresent(t *testing.T) {
	ctx := context.Background()
	retrievedToken := scimTokenFromContext(ctx)

	assert.Nil(t, retrievedToken)
}

// ============== BENCHMARK TESTS ==============

// Benchmark Create User
func BenchmarkCreateUser(b *testing.B) {
	mockStore := &MockStore{}
	service := NewService(mockStore, nil)
	ctx := context.Background()

	mockStore.On("CreateUser", ctx, mock.AnythingOfType("*scim.SCIMUser")).Return(nil)

	user := &SCIMUser{
		UserName: "benchmark.user",
		Active:   true,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = service.CreateUser(ctx, user)
	}
}

// Benchmark List Users
func BenchmarkListUsers(b *testing.B) {
	mockStore := &MockStore{}
	service := NewService(mockStore, nil)
	ctx := context.Background()

	users := []*SCIMUser{
		{ID: "1", UserName: "user1", Active: true},
		{ID: "2", UserName: "user2", Active: true},
	}

	mockStore.On("ListUsers", ctx, mock.Anything, "", SortAscending, 1, 20).Return(users, 2, nil)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = service.ListUsers(ctx, "", "", SortAscending, 1, 20)
	}
}

// Benchmark Validate Token
func BenchmarkValidateToken(b *testing.B) {
	mockStore := &MockStore{}
	service := NewService(mockStore, nil)
	ctx := context.Background()

	mockStore.On("CreateSCIMToken", ctx, mock.AnythingOfType("*scim.SCIMToken")).Return(nil)
	token, tokenString, _ := service.CreateSCIMToken(ctx, "bench", "", []string{}, nil, uuid.New())
	mockStore.On("GetSCIMTokenByPrefix", ctx, tokenString[12:24]).Return(token, nil)
	mockStore.On("UpdateSCIMTokenLastUsed", mock.Anything, token.ID).Return(nil)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = service.ValidateToken(ctx, tokenString)
	}
}

// ============== ADDITIONAL EDGE CASE TESTS ==============

// Test List Users with Filter Error
func TestListUsersFilterError(t *testing.T) {
	store := &NoOpStore{}
	service := NewService(store, nil)
	ctx := context.Background()

	// Invalid filter format - should return error (only 2 parts instead of 3)
	_, err := service.ListUsers(ctx, "invalidfilter", "", SortAscending, 1, 20)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid filter")
}

// Test List Groups with Filter Error
func TestListGroupsFilterError(t *testing.T) {
	store := &NoOpStore{}
	service := NewService(store, nil)
	ctx := context.Background()

	// Invalid filter format - should return error (only 1 part instead of 3)
	_, err := service.ListGroups(ctx, "invalid", "", SortAscending, 1, 20)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid filter")
}

// Test List Users Store Error
func TestListUsersStoreError(t *testing.T) {
	mockStore := &MockStore{}
	service := NewService(mockStore, nil)
	ctx := context.Background()

	mockStore.On("ListUsers", ctx, mock.Anything, "", SortAscending, 1, 20).
		Return(nil, 0, fmt.Errorf("database error"))

	_, err := service.ListUsers(ctx, "", "", SortAscending, 1, 20)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "database error")
}

// Test List Groups Store Error
func TestListGroupsStoreError(t *testing.T) {
	mockStore := &MockStore{}
	service := NewService(mockStore, nil)
	ctx := context.Background()

	mockStore.On("ListGroups", ctx, mock.Anything, "", SortAscending, 1, 20).
		Return(nil, 0, fmt.Errorf("database error"))

	_, err := service.ListGroups(ctx, "", "", SortAscending, 1, 20)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "database error")
}

// Test Update Group Not Found
func TestUpdateGroupNotFound(t *testing.T) {
	mockStore := &MockStore{}
	service := NewService(mockStore, nil)
	ctx := context.Background()
	groupID := uuid.New().String()

	updatedGroupData := &SCIMGroup{
		DisplayName: "NewName",
	}

	mockStore.On("GetGroupByID", ctx, groupID).Return(nil, fmt.Errorf("not found"))

	_, err := service.UpdateGroup(ctx, groupID, updatedGroupData)

	assert.Error(t, err)
	mockStore.AssertExpectations(t)
}

// Test Update Group Store Error
func TestUpdateGroupStoreError(t *testing.T) {
	mockStore := &MockStore{}
	service := NewService(mockStore, nil)
	ctx := context.Background()
	groupID := uuid.New().String()

	now := time.Now().UTC()
	existingGroup := &SCIMGroup{
		ID:          groupID,
		DisplayName: "OldName",
		Meta: Meta{
			Created:      &now,
			LastModified: &now,
		},
	}

	updatedGroupData := &SCIMGroup{
		DisplayName: "NewName",
	}

	mockStore.On("GetGroupByID", ctx, groupID).Return(existingGroup, nil)
	mockStore.On("UpdateGroup", ctx, mock.Anything).Return(fmt.Errorf("database error"))

	_, err := service.UpdateGroup(ctx, groupID, updatedGroupData)

	assert.Error(t, err)
	mockStore.AssertExpectations(t)
}

// Test Create User Store Error
func TestCreateUserStoreError(t *testing.T) {
	mockStore := &MockStore{}
	service := NewService(mockStore, nil)
	ctx := context.Background()

	user := &SCIMUser{
		UserName: "john.doe",
		Active:   true,
	}

	mockStore.On("CreateUser", ctx, mock.Anything).Return(fmt.Errorf("database error"))

	_, err := service.CreateUser(ctx, user)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "database error")
}

// Test Create Group Store Error
func TestCreateGroupStoreError(t *testing.T) {
	mockStore := &MockStore{}
	service := NewService(mockStore, nil)
	ctx := context.Background()

	group := &SCIMGroup{
		DisplayName: "Developers",
	}

	mockStore.On("CreateGroup", ctx, mock.Anything).Return(fmt.Errorf("database error"))

	_, err := service.CreateGroup(ctx, group)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "database error")
}

// Test Update User Store Error
func TestUpdateUserStoreError(t *testing.T) {
	mockStore := &MockStore{}
	service := NewService(mockStore, nil)
	ctx := context.Background()
	userID := uuid.New().String()

	now := time.Now().UTC()
	existingUser := &SCIMUser{
		ID:       userID,
		UserName: "john",
		Active:   false,
		Meta: Meta{
			Created:      &now,
			LastModified: &now,
		},
	}

	updatedUserData := &SCIMUser{
		UserName: "john.updated",
		Active:   true,
	}

	mockStore.On("GetUserByID", ctx, userID).Return(existingUser, nil)
	mockStore.On("UpdateUser", ctx, mock.Anything).Return(fmt.Errorf("database error"))

	_, err := service.UpdateUser(ctx, userID, updatedUserData)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "database error")
}

// Test Parse Filter with Complex Expressions
func TestParseFilterVariations(t *testing.T) {
	service := &Service{}

	tests := []struct {
		filter    string
		attribute string
		operator  string
		value     string
		hasError  bool
	}{
		{`email eq "test@example.com"`, "email", "eq", "test@example.com", false},
		{`active eq true`, "active", "eq", "true", false},
		{`id ne "123"`, "id", "ne", "123", false},
		{`invalid`, "", "", "", true},
		{`one two three four`, "", "", "", true},
	}

	for _, tt := range tests {
		filter, err := service.parseFilter(tt.filter)

		if tt.hasError {
			assert.Error(t, err)
		} else {
			assert.NoError(t, err)
			assert.Equal(t, tt.attribute, filter.Attribute)
			assert.Equal(t, tt.operator, filter.Operator)
			assert.Equal(t, tt.value, filter.Value)
		}
	}
}

// Test Schema Meta Information
func TestSchemaMetaInformation(t *testing.T) {
	mockStore := &MockStore{}
	service := NewService(mockStore, nil)

	schemas := service.GetSchemas()

	userSchema := schemas[0]
	assert.Equal(t, "Schema", userSchema.Meta.ResourceType)
	assert.Contains(t, userSchema.Meta.Location, SchemaUser)

	groupSchema := schemas[1]
	assert.Equal(t, "Schema", groupSchema.Meta.ResourceType)
	assert.Contains(t, groupSchema.Meta.Location, SchemaGroup)
}

// Test User Schema Attribute Details
func TestUserSchemaAttributeDetails(t *testing.T) {
	mockStore := &MockStore{}
	service := NewService(mockStore, nil)

	schemas := service.GetSchemas()
	userSchema := schemas[0]

	// Find userName attribute
	var userNameAttr *Attribute
	for i, attr := range userSchema.Attributes {
		if attr.Name == "userName" {
			userNameAttr = &userSchema.Attributes[i]
			break
		}
	}

	require.NotNil(t, userNameAttr)
	assert.True(t, userNameAttr.Required)
	assert.Equal(t, "string", userNameAttr.Type)
	assert.Equal(t, "readWrite", userNameAttr.Mutability)
	assert.Equal(t, "server", userNameAttr.Uniqueness)
}

// Test Group Schema Attribute Details
func TestGroupSchemaAttributeDetails(t *testing.T) {
	mockStore := &MockStore{}
	service := NewService(mockStore, nil)

	schemas := service.GetSchemas()
	groupSchema := schemas[1]

	// Find displayName attribute
	var displayNameAttr *Attribute
	for i, attr := range groupSchema.Attributes {
		if attr.Name == "displayName" {
			displayNameAttr = &groupSchema.Attributes[i]
			break
		}
	}

	require.NotNil(t, displayNameAttr)
	assert.True(t, displayNameAttr.Required)
	assert.Equal(t, "string", displayNameAttr.Type)
}

// Test Service Provider Config Details
func TestServiceProviderConfigDetails(t *testing.T) {
	mockStore := &MockStore{}
	service := NewService(mockStore, nil)

	config := service.GetServiceProviderConfig()

	// Check authentication schemes
	assert.Len(t, config.AuthenticationSchemes, 1)
	assert.Equal(t, "httpbearer", config.AuthenticationSchemes[0].Type)
	assert.True(t, config.AuthenticationSchemes[0].Primary)

	// Check meta information
	assert.Equal(t, "ServiceProviderConfig", config.Meta.ResourceType)
	assert.Contains(t, config.Meta.Location, "ServiceProviderConfig")

	// Verify documentation URI
	assert.NotEmpty(t, config.DocumentationURI)
}

// Test Has Permission Empty Token Permissions
func TestHasPermissionEmptyPermissions(t *testing.T) {
	token := &SCIMToken{
		Permissions: []string{},
	}
	service := &Service{}

	assert.False(t, service.HasPermission(token, "users:read"))
	assert.False(t, service.HasPermission(token, "users:write"))
}

// Test Has Permission Nil Token
func TestHasPermissionNilPermissions(t *testing.T) {
	token := &SCIMToken{
		Permissions: nil,
	}
	service := &Service{}

	assert.False(t, service.HasPermission(token, "users:read"))
}

// Test Create Token Storage
func TestCreateTokenStorageCall(t *testing.T) {
	mockStore := &MockStore{}
	service := NewService(mockStore, nil)
	ctx := context.Background()
	createdBy := uuid.New()

	mockStore.On("CreateSCIMToken", ctx, mock.MatchedBy(func(token *SCIMToken) bool {
		// Verify token structure
		return token.Name == "token" &&
			token.Description == "desc" &&
			len(token.Permissions) == 1 &&
			token.Permissions[0] == "read" &&
			token.CreatedBy == createdBy
	})).Return(nil)

	token, tokenString, err := service.CreateSCIMToken(ctx, "token", "desc", []string{"read"}, nil, createdBy)

	assert.NoError(t, err)
	assert.NotNil(t, token)
	assert.NotEmpty(t, tokenString)
	mockStore.AssertCalled(t, "CreateSCIMToken", ctx, mock.Anything)
}