package scim

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// MockStore is a mock implementation of the SCIM Store interface.
type MockStore struct {
	mock.Mock
}

func (m *MockStore) GetUserByID(ctx context.Context, id string) (*SCIMUser, error) {
	args := m.Called(ctx, id)
	return args.Get(0).(*SCIMUser), args.Error(1)
}

func (m *MockStore) GetUserByUserName(ctx context.Context, userName string) (*SCIMUser, error) {
	args := m.Called(ctx, userName)
	return args.Get(0).(*SCIMUser), args.Error(1)
}

func (m *MockStore) GetUserByExternalID(ctx context.Context, externalID string) (*SCIMUser, error) {
	args := m.Called(ctx, externalID)
	return args.Get(0).(*SCIMUser), args.Error(1)
}

func (m *MockStore) ListUsers(ctx context.Context, filter *Filter, sortBy string, sortOrder SortOrder, startIndex, count int) ([]*SCIMUser, int, error) {
	args := m.Called(ctx, filter, sortBy, sortOrder, startIndex, count)
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
	return args.Get(0).(*SCIMUser), args.Error(1)
}

func (m *MockStore) DeleteUser(ctx context.Context, id string) error {
	args := m.Called(ctx, id)
	return args.Error(0)
}

func (m *MockStore) GetGroupByID(ctx context.Context, id string) (*SCIMGroup, error) {
	args := m.Called(ctx, id)
	return args.Get(0).(*SCIMGroup), args.Error(1)
}

func (m *MockStore) GetGroupByDisplayName(ctx context.Context, displayName string) (*SCIMGroup, error) {
	args := m.Called(ctx, displayName)
	return args.Get(0).(*SCIMGroup), args.Error(1)
}

func (m *MockStore) ListGroups(ctx context.Context, filter *Filter, sortBy string, sortOrder SortOrder, startIndex, count int) ([]*SCIMGroup, int, error) {
	args := m.Called(ctx, filter, sortBy, sortOrder, startIndex, count)
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
	return args.Get(0).(*SCIMGroup), args.Error(1)
}

func (m *MockStore) DeleteGroup(ctx context.Context, id string) error {
	args := m.Called(ctx, id)
	return args.Error(0)
}

func (m *MockStore) GetSCIMMapping(ctx context.Context, id uuid.UUID) (*SCIMMapping, error) {
	args := m.Called(ctx, id)
	return args.Get(0).(*SCIMMapping), args.Error(1)
}

func (m *MockStore) ListSCIMMappings(ctx context.Context) ([]*SCIMMapping, error) {
	args := m.Called(ctx)
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
	return args.Get(0).(*SCIMToken), args.Error(1)
}

func (m *MockStore) ListSCIMTokens(ctx context.Context) ([]*SCIMToken, error) {
	args := m.Called(ctx)
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

// Test Service Creation
func TestNewService(t *testing.T) {
	mockStore := &MockStore{}
	
	service := NewService(mockStore, nil)
	
	assert.NotNil(t, service)
	assert.Equal(t, mockStore, service.store)
}

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
}

// Test Schemas
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
	
	// Check Group schema
	groupSchema := schemas[1]
	assert.Equal(t, SchemaGroup, groupSchema.ID)
	assert.Equal(t, "Group", groupSchema.Name)
	assert.NotEmpty(t, groupSchema.Attributes)
}

// Test User Operations
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
	
	mockStore.On("CreateUser", ctx, mock.AnythingOfType("*scim.SCIMUser")).Return(nil)
	
	createdUser, err := service.CreateUser(ctx, user)
	
	assert.NoError(t, err)
	assert.NotNil(t, createdUser)
	assert.NotEmpty(t, createdUser.ID)
	assert.Contains(t, createdUser.Schemas, SchemaUser)
	assert.NotNil(t, createdUser.Meta.Created)
	mockStore.AssertExpectations(t)
}

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
}

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
	assert.Equal(t, users, response.Resources)
	mockStore.AssertExpectations(t)
}

// Test Group Operations
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
	
	mockStore.On("CreateGroup", ctx, mock.AnythingOfType("*scim.SCIMGroup")).Return(nil)
	
	createdGroup, err := service.CreateGroup(ctx, group)
	
	assert.NoError(t, err)
	assert.NotNil(t, createdGroup)
	assert.NotEmpty(t, createdGroup.ID)
	assert.Contains(t, createdGroup.Schemas, SchemaGroup)
	assert.NotNil(t, createdGroup.Meta.Created)
	mockStore.AssertExpectations(t)
}

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
}

// Test Token Management
func TestCreateSCIMToken(t *testing.T) {
	mockStore := &MockStore{}
	service := NewService(mockStore, nil)
	ctx := context.Background()
	createdBy := uuid.New()
	
	mockStore.On("CreateSCIMToken", ctx, mock.AnythingOfType("*scim.SCIMToken")).Return(nil)
	
	token, tokenString, err := service.CreateSCIMToken(ctx, "Test Token", "Test token description", []string{"users:read", "groups:read"}, nil, createdBy)
	
	assert.NoError(t, err)
	assert.NotNil(t, token)
	assert.NotEmpty(t, tokenString)
	assert.True(t, strings.HasPrefix(tokenString, "aegion_scim_"))
	assert.Equal(t, "Test Token", token.Name)
	assert.Equal(t, []string{"users:read", "groups:read"}, token.Permissions)
	assert.Equal(t, createdBy, token.CreatedBy)
	mockStore.AssertExpectations(t)
}

func TestValidateToken(t *testing.T) {
	mockStore := &MockStore{}
	service := NewService(mockStore, nil)
	ctx := context.Background()
	
	// Create a token first (we don't need to mock this since it's just building the struct)
	createdToken := &SCIMToken{
		ID:          uuid.New(),
		Name:        "Test Token",
		Description: "Test description",
		Permissions: []string{"users:read"},
		CreatedBy:   uuid.New(),
		Active:      true,
		CreatedAt:   time.Now(),
	}
	
	// Generate a test token string
	tokenString := "aegion_scim_abcdefghijklmnopqrstuvwx"
	
	// Mock getting token by prefix
	mockStore.On("GetSCIMTokenByPrefix", ctx, "abcdefghijkl").Return(createdToken, nil)
	mockStore.On("UpdateSCIMTokenLastUsed", mock.Anything, createdToken.ID).Return(nil)
	
	validatedToken, err := service.ValidateToken(ctx, tokenString)
	
	assert.Error(t, err) // Should error because hash won't match
	assert.Nil(t, validatedToken)
	
	// Don't assert expectations since the goroutine may not complete
}

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
	
	// Give time for the goroutine to complete
	time.Sleep(10 * time.Millisecond)
}

func TestValidateTokenInvalidFormat(t *testing.T) {
	mockStore := &MockStore{}
	service := NewService(mockStore, nil)
	ctx := context.Background()
	
	_, err := service.ValidateToken(ctx, "invalid_token")
	
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid token format")
}

func TestHasPermission(t *testing.T) {
	token := &SCIMToken{
		Permissions: []string{"users:read", "groups:*"},
	}
	service := &Service{}
	
	assert.True(t, service.HasPermission(token, "users:read"))
	assert.True(t, service.HasPermission(token, "groups:read"))
	assert.True(t, service.HasPermission(token, "groups:write"))
	assert.False(t, service.HasPermission(token, "users:write"))
}

func TestHasPermissionWildcard(t *testing.T) {
	token := &SCIMToken{
		Permissions: []string{"*"},
	}
	service := &Service{}
	
	assert.True(t, service.HasPermission(token, "users:read"))
	assert.True(t, service.HasPermission(token, "groups:write"))
	assert.True(t, service.HasPermission(token, "anything"))
}

// Test Filter Parsing
func TestParseFilter(t *testing.T) {
	service := &Service{}
	
	filter, err := service.parseFilter(`userName eq "john.doe"`)
	
	assert.NoError(t, err)
	assert.Equal(t, "userName", filter.Attribute)
	assert.Equal(t, "eq", filter.Operator)
	assert.Equal(t, "john.doe", filter.Value)
}

func TestParseFilterInvalid(t *testing.T) {
	service := &Service{}
	
	_, err := service.parseFilter("invalid filter")
	
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid filter format")
}

// Benchmark tests
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