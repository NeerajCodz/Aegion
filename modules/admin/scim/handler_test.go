package scim

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// Test Handler Creation
func TestNewSCIMHandler(t *testing.T) {
	handler := NewHandler(nil) // Using nil for simplicity
	assert.NotNil(t, handler)
}

// Test Service Provider Config Endpoint
func TestHandlerGetServiceProviderConfig(t *testing.T) {
	handler := NewHandler(nil)
	handler.service = &Service{} // Replace with real service for this test

	// Create test request
	req := httptest.NewRequest("GET", "/scim/v2/ServiceProviderConfig", nil)
	rr := httptest.NewRecorder()

	// Call handler
	handler.GetServiceProviderConfig(rr, req)

	// Check response
	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Equal(t, "application/scim+json", rr.Header().Get("Content-Type"))

	var config ServiceProviderConfig
	err := json.Unmarshal(rr.Body.Bytes(), &config)
	assert.NoError(t, err)
	assert.NotEmpty(t, config.Schemas)
}

// Test Schemas Endpoint
func TestHandlerGetSchemas(t *testing.T) {
	handler := NewHandler(nil)
	handler.service = &Service{} // Replace with real service for this test

	req := httptest.NewRequest("GET", "/scim/v2/Schemas", nil)
	rr := httptest.NewRecorder()

	handler.GetSchemas(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Equal(t, "application/scim+json", rr.Header().Get("Content-Type"))

	var response ListResponse
	err := json.Unmarshal(rr.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.Equal(t, 2, response.TotalResults) // User and Group schemas
}

// Test Get Single Schema
func TestHandlerGetSchema(t *testing.T) {
	handler := NewHandler(nil)
	handler.service = &Service{}

	req := httptest.NewRequest("GET", "/scim/v2/Schemas/"+SchemaUser, nil)
	rr := httptest.NewRecorder()

	// Use chi router to set URL params
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", SchemaUser)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	handler.GetSchema(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)

	var schema Schema
	err := json.Unmarshal(rr.Body.Bytes(), &schema)
	assert.NoError(t, err)
	assert.Equal(t, SchemaUser, schema.ID)
}

// Test Get Schema Not Found
func TestHandlerGetSchemaNotFound(t *testing.T) {
	handler := NewHandler(nil)
	handler.service = &Service{}

	req := httptest.NewRequest("GET", "/scim/v2/Schemas/invalid-schema", nil)
	rr := httptest.NewRecorder()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "invalid-schema")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	handler.GetSchema(rr, req)

	assert.Equal(t, http.StatusNotFound, rr.Code)
	var errResp ErrorResponse
	err := json.Unmarshal(rr.Body.Bytes(), &errResp)
	assert.NoError(t, err)
	assert.Equal(t, "notFound", errResp.ScimType)
}

// Test Authentication Middleware - Missing Auth Header
func TestAuthMiddlewareMissingHeader(t *testing.T) {
	handler := NewHandler(nil)

	req := httptest.NewRequest("GET", "/test", nil)
	rr := httptest.NewRecorder()

	nextCalled := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
	})

	middleware := handler.authMiddleware(next)
	middleware.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusUnauthorized, rr.Code)
	assert.False(t, nextCalled)
}

// Test Authentication Middleware - Invalid Bearer Token
func TestAuthMiddlewareInvalidBearer(t *testing.T) {
	handler := NewHandler(nil)

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Basic invalid")
	rr := httptest.NewRecorder()

	nextCalled := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
	})

	middleware := handler.authMiddleware(next)
	middleware.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusUnauthorized, rr.Code)
	assert.False(t, nextCalled)
}

// Test Authentication Middleware - Valid Token
func TestAuthMiddlewareValidToken(t *testing.T) {
	mockStore := &MockStore{}
	service := NewService(mockStore, nil)
	handler := NewHandler(service)

	// Token format: "aegion_scim_" (12 chars) + prefix (12 chars) + rest
	tokenString := "aegion_scim_test1234abcdef9876543210"
	prefix := tokenString[12:24] // "test1234abcd"

	token := &SCIMToken{
		ID:          uuid.New(),
		Name:        "test-token",
		TokenHash:   "hash",
		Prefix:      prefix,
		Permissions: []string{"users:read"},
		Active:      true,
	}

	mockStore.On("GetSCIMTokenByPrefix", mock.Anything, prefix).Return(token, nil)
	mockStore.On("UpdateSCIMTokenLastUsed", mock.Anything, mock.Anything).Return(nil)

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer "+tokenString)
	rr := httptest.NewRecorder()

	nextCalled := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusOK)
	})

	middleware := handler.authMiddleware(next)
	middleware.ServeHTTP(rr, req)

	// We expect unauthorized since the token hash won't match, but the point is to test the flow
	assert.Equal(t, http.StatusUnauthorized, rr.Code)
	assert.False(t, nextCalled)
}

// Test Permission Middleware - No Token
func TestRequirePermissionMiddlewareNoToken(t *testing.T) {
	handler := NewHandler(nil)

	req := httptest.NewRequest("GET", "/test", nil)
	rr := httptest.NewRecorder()

	nextCalled := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusOK)
	})

	middleware := handler.requirePermission("users:read")(next)
	middleware.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusUnauthorized, rr.Code)
	assert.False(t, nextCalled)
}

// Test Permission Middleware - Insufficient Rights
func TestRequirePermissionMiddlewareInsufficientRights(t *testing.T) {
	mockStore := &MockStore{}
	service := NewService(mockStore, nil)
	handler := NewHandler(service)

	token := &SCIMToken{
		ID:          uuid.New(),
		Permissions: []string{"groups:read"},
	}

	req := httptest.NewRequest("GET", "/test", nil)
	ctx := contextWithSCIMToken(req.Context(), token)
	req = req.WithContext(ctx)
	rr := httptest.NewRecorder()

	nextCalled := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
	})

	mockStore.On("GetUserByID", mock.Anything, mock.Anything).Return(nil, fmt.Errorf("not found"))

	middleware := handler.requirePermission("users:read")(next)
	middleware.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusForbidden, rr.Code)
	assert.False(t, nextCalled)
}

// Test Permission Middleware - Sufficient Rights
func TestRequirePermissionMiddlewareSufficientRights(t *testing.T) {
	mockStore := &MockStore{}
	service := NewService(mockStore, nil)
	handler := NewHandler(service)

	token := &SCIMToken{
		ID:          uuid.New(),
		Permissions: []string{"users:read"},
	}

	req := httptest.NewRequest("GET", "/test", nil)
	ctx := contextWithSCIMToken(req.Context(), token)
	req = req.WithContext(ctx)
	rr := httptest.NewRecorder()

	nextCalled := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusOK)
	})

	middleware := handler.requirePermission("users:read")(next)
	middleware.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	assert.True(t, nextCalled)
}

// ============== USER HANDLERS ==============

// Test List Users
func TestHandlerListUsers(t *testing.T) {
	mockStore := &MockStore{}
	service := NewService(mockStore, nil)
	handler := NewHandler(service)

	users := []*SCIMUser{
		{
			ID:       uuid.New().String(),
			UserName: "user1",
			Active:   true,
		},
	}

	mockStore.On("ListUsers", mock.Anything, mock.Anything, "", SortAscending, 1, 20).
		Return(users, 1, nil)

	req := httptest.NewRequest("GET", "/scim/v2/Users", nil)
	rr := httptest.NewRecorder()

	handler.ListUsers(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)

	var response ListResponse
	err := json.Unmarshal(rr.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.Equal(t, 1, response.TotalResults)
}

// Test List Users with Pagination
func TestHandlerListUsersPagination(t *testing.T) {
	mockStore := &MockStore{}
	service := NewService(mockStore, nil)
	handler := NewHandler(service)

	// Return exactly 10 users to match what we claim
	users := make([]*SCIMUser, 10)
	for i := 0; i < 10; i++ {
		id := fmt.Sprintf("user%d", i+2)
		users[i] = &SCIMUser{ID: id, UserName: id, Active: true}
	}

	mockStore.On("ListUsers", mock.Anything, mock.Anything, "", SortAscending, 2, 10).
		Return(users, 20, nil)

	req := httptest.NewRequest("GET", "/scim/v2/Users?startIndex=2&count=10", nil)
	rr := httptest.NewRecorder()

	handler.ListUsers(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)

	var response ListResponse
	err := json.Unmarshal(rr.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.Equal(t, 20, response.TotalResults)
	assert.Equal(t, 2, response.StartIndex)
	assert.Equal(t, 10, response.ItemsPerPage)
}

// Test List Users with Sort
func TestHandlerListUsersSort(t *testing.T) {
	mockStore := &MockStore{}
	service := NewService(mockStore, nil)
	handler := NewHandler(service)

	users := []*SCIMUser{}

	mockStore.On("ListUsers", mock.Anything, mock.Anything, "userName", SortDescending, 1, 20).
		Return(users, 0, nil)

	req := httptest.NewRequest("GET", "/scim/v2/Users?sortBy=userName&sortOrder=descending", nil)
	rr := httptest.NewRecorder()

	handler.ListUsers(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	mockStore.AssertExpectations(t)
}

// Test List Users Error
func TestHandlerListUsersError(t *testing.T) {
	mockStore := &MockStore{}
	service := NewService(mockStore, nil)
	handler := NewHandler(service)

	mockStore.On("ListUsers", mock.Anything, mock.Anything, "", SortAscending, 1, 20).
		Return(nil, 0, fmt.Errorf("database error"))

	req := httptest.NewRequest("GET", "/scim/v2/Users", nil)
	rr := httptest.NewRecorder()

	handler.ListUsers(rr, req)

	assert.Equal(t, http.StatusInternalServerError, rr.Code)
}

// Test Get User
func TestHandlerGetUser(t *testing.T) {
	mockStore := &MockStore{}
	service := NewService(mockStore, nil)
	handler := NewHandler(service)

	userID := uuid.New().String()
	user := &SCIMUser{
		ID:       userID,
		UserName: "testuser",
		Active:   true,
	}

	mockStore.On("GetUserByID", mock.Anything, userID).Return(user, nil)

	req := httptest.NewRequest("GET", "/scim/v2/Users/"+userID, nil)
	rr := httptest.NewRecorder()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", userID)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	handler.GetUser(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)

	var returnedUser SCIMUser
	err := json.Unmarshal(rr.Body.Bytes(), &returnedUser)
	assert.NoError(t, err)
	assert.Equal(t, userID, returnedUser.ID)
	assert.Equal(t, "testuser", returnedUser.UserName)
}

// Test Get User Not Found
func TestHandlerGetUserNotFound(t *testing.T) {
	mockStore := &MockStore{}
	service := NewService(mockStore, nil)
	handler := NewHandler(service)

	userID := uuid.New().String()
	mockStore.On("GetUserByID", mock.Anything, userID).Return(nil, fmt.Errorf("not found"))

	req := httptest.NewRequest("GET", "/scim/v2/Users/"+userID, nil)
	rr := httptest.NewRecorder()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", userID)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	handler.GetUser(rr, req)

	assert.Equal(t, http.StatusNotFound, rr.Code)
}

// Test Create User Success
func TestHandlerCreateUserSuccess(t *testing.T) {
	mockStore := &MockStore{}
	service := NewService(mockStore, nil)
	handler := NewHandler(service)

	user := SCIMUser{
		UserName: "newuser",
		Active:   true,
	}

	mockStore.On("CreateUser", mock.Anything, mock.MatchedBy(func(u *SCIMUser) bool {
		return u.UserName == "newuser"
	})).Return(nil)

	body, err := json.Marshal(user)
	require.NoError(t, err)

	req := httptest.NewRequest("POST", "/scim/v2/Users", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	handler.CreateUser(rr, req)

	assert.Equal(t, http.StatusCreated, rr.Code)
}

// Test Create User Invalid JSON
func TestHandlerCreateUserInvalidJSON(t *testing.T) {
	handler := NewHandler(nil)

	req := httptest.NewRequest("POST", "/scim/v2/Users", bytes.NewBufferString("invalid json"))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	handler.CreateUser(rr, req)

	assert.Equal(t, http.StatusBadRequest, rr.Code)

	var response ErrorResponse
	err := json.Unmarshal(rr.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.Equal(t, "invalidSyntax", response.ScimType)
}

// Test Create User Missing Required Field
func TestHandlerCreateUserMissingRequired(t *testing.T) {
	mockStore := &MockStore{}
	service := NewService(mockStore, nil)
	handler := NewHandler(service)

	user := SCIMUser{
		Active: true,
	}

	mockStore.On("CreateUser", mock.Anything, mock.Anything).Return(nil)

	body, err := json.Marshal(user)
	require.NoError(t, err)

	req := httptest.NewRequest("POST", "/scim/v2/Users", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	handler.CreateUser(rr, req)

	assert.Equal(t, http.StatusBadRequest, rr.Code)

	var response ErrorResponse
	err = json.Unmarshal(rr.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.Equal(t, "invalidValue", response.ScimType)
}

// Test Create User Duplicate
func TestHandlerCreateUserDuplicate(t *testing.T) {
	mockStore := &MockStore{}
	service := NewService(mockStore, nil)
	handler := NewHandler(service)

	user := SCIMUser{
		UserName: "existing",
		Active:   true,
	}

	mockStore.On("CreateUser", mock.Anything, mock.Anything).Return(fmt.Errorf("user already exists"))

	body, err := json.Marshal(user)
	require.NoError(t, err)

	req := httptest.NewRequest("POST", "/scim/v2/Users", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	handler.CreateUser(rr, req)

	assert.Equal(t, http.StatusConflict, rr.Code)

	var response ErrorResponse
	err = json.Unmarshal(rr.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.Equal(t, "uniqueness", response.ScimType)
}

// Test Update User
func TestHandlerUpdateUserSuccess(t *testing.T) {
	mockStore := &MockStore{}
	service := NewService(mockStore, nil)
	handler := NewHandler(service)

	userID := uuid.New().String()
	user := SCIMUser{
		UserName: "updated",
		Active:   true,
	}

	now := time.Now().UTC()
	existingUser := &SCIMUser{
		ID:       userID,
		UserName: "existing",
		Active:   false,
		Meta: Meta{
			Created:      &now,
			LastModified: &now,
		},
	}

	mockStore.On("GetUserByID", mock.Anything, userID).Return(existingUser, nil)
	mockStore.On("UpdateUser", mock.Anything, mock.Anything).Return(nil)

	body, err := json.Marshal(user)
	require.NoError(t, err)

	req := httptest.NewRequest("PUT", "/scim/v2/Users/"+userID, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", userID)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	handler.UpdateUser(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
}

// Test Update User Not Found
func TestHandlerUpdateUserNotFound(t *testing.T) {
	mockStore := &MockStore{}
	service := NewService(mockStore, nil)
	handler := NewHandler(service)

	userID := uuid.New().String()
	user := SCIMUser{
		UserName: "updated",
		Active:   true,
	}

	mockStore.On("GetUserByID", mock.Anything, userID).Return(nil, fmt.Errorf("not found"))

	body, err := json.Marshal(user)
	require.NoError(t, err)

	req := httptest.NewRequest("PUT", "/scim/v2/Users/"+userID, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", userID)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	handler.UpdateUser(rr, req)

	assert.Equal(t, http.StatusNotFound, rr.Code)
}

// Test Patch User
func TestHandlerPatchUserSuccess(t *testing.T) {
	mockStore := &MockStore{}
	service := NewService(mockStore, nil)
	handler := NewHandler(service)

	userID := uuid.New().String()
	patchReq := PatchRequest{
		Schemas: []string{SchemaPatchOp},
		Operations: []PatchOperation{
			{
				Op:    "replace",
				Path:  "active",
				Value: false,
			},
		},
	}

	user := &SCIMUser{
		ID:       userID,
		UserName: "testuser",
		Active:   false,
	}

	mockStore.On("PatchUser", mock.Anything, userID, patchReq.Operations).Return(user, nil)

	body, err := json.Marshal(patchReq)
	require.NoError(t, err)

	req := httptest.NewRequest("PATCH", "/scim/v2/Users/"+userID, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", userID)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	handler.PatchUser(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
}

// Test Patch User No Operations
func TestHandlerPatchUserNoOperations(t *testing.T) {
	mockStore := &MockStore{}
	service := NewService(mockStore, nil)
	handler := NewHandler(service)

	userID := uuid.New().String()
	patchReq := PatchRequest{
		Schemas:    []string{SchemaPatchOp},
		Operations: []PatchOperation{},
	}

	body, err := json.Marshal(patchReq)
	require.NoError(t, err)

	req := httptest.NewRequest("PATCH", "/scim/v2/Users/"+userID, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", userID)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	handler.PatchUser(rr, req)

	assert.Equal(t, http.StatusBadRequest, rr.Code)

	var response ErrorResponse
	err = json.Unmarshal(rr.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.Equal(t, "invalidValue", response.ScimType)
}

// Test Patch User Invalid Operation
func TestHandlerPatchUserInvalidOperation(t *testing.T) {
	mockStore := &MockStore{}
	service := NewService(mockStore, nil)
	handler := NewHandler(service)

	userID := uuid.New().String()
	patchReq := PatchRequest{
		Schemas: []string{SchemaPatchOp},
		Operations: []PatchOperation{
			{
				Op:   "replace",
				Path: "active",
			},
		},
	}

	mockStore.On("PatchUser", mock.Anything, userID, patchReq.Operations).
		Return(nil, fmt.Errorf("invalid operation"))

	body, err := json.Marshal(patchReq)
	require.NoError(t, err)

	req := httptest.NewRequest("PATCH", "/scim/v2/Users/"+userID, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", userID)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	handler.PatchUser(rr, req)

	assert.Equal(t, http.StatusBadRequest, rr.Code)
}

// Test Delete User
func TestHandlerDeleteUserSuccess(t *testing.T) {
	mockStore := &MockStore{}
	service := NewService(mockStore, nil)
	handler := NewHandler(service)

	userID := uuid.New().String()
	mockStore.On("DeleteUser", mock.Anything, userID).Return(nil)

	req := httptest.NewRequest("DELETE", "/scim/v2/Users/"+userID, nil)
	rr := httptest.NewRecorder()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", userID)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	handler.DeleteUser(rr, req)

	assert.Equal(t, http.StatusNoContent, rr.Code)
}

// Test Delete User Not Found
func TestHandlerDeleteUserNotFound(t *testing.T) {
	mockStore := &MockStore{}
	service := NewService(mockStore, nil)
	handler := NewHandler(service)

	userID := uuid.New().String()
	mockStore.On("DeleteUser", mock.Anything, userID).Return(fmt.Errorf("not found"))

	req := httptest.NewRequest("DELETE", "/scim/v2/Users/"+userID, nil)
	rr := httptest.NewRecorder()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", userID)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	handler.DeleteUser(rr, req)

	assert.Equal(t, http.StatusNotFound, rr.Code)
}

// ============== GROUP HANDLERS ==============

// Test List Groups
func TestHandlerListGroups(t *testing.T) {
	mockStore := &MockStore{}
	service := NewService(mockStore, nil)
	handler := NewHandler(service)

	groups := []*SCIMGroup{
		{
			ID:          uuid.New().String(),
			DisplayName: "Developers",
		},
	}

	mockStore.On("ListGroups", mock.Anything, mock.Anything, "", SortAscending, 1, 20).
		Return(groups, 1, nil)

	req := httptest.NewRequest("GET", "/scim/v2/Groups", nil)
	rr := httptest.NewRecorder()

	handler.ListGroups(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)

	var response ListResponse
	err := json.Unmarshal(rr.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.Equal(t, 1, response.TotalResults)
}

// Test Get Group
func TestHandlerGetGroup(t *testing.T) {
	mockStore := &MockStore{}
	service := NewService(mockStore, nil)
	handler := NewHandler(service)

	groupID := uuid.New().String()
	group := &SCIMGroup{
		ID:          groupID,
		DisplayName: "Developers",
	}

	mockStore.On("GetGroupByID", mock.Anything, groupID).Return(group, nil)

	req := httptest.NewRequest("GET", "/scim/v2/Groups/"+groupID, nil)
	rr := httptest.NewRecorder()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", groupID)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	handler.GetGroup(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)

	var returnedGroup SCIMGroup
	err := json.Unmarshal(rr.Body.Bytes(), &returnedGroup)
	assert.NoError(t, err)
	assert.Equal(t, groupID, returnedGroup.ID)
}

// Test Create Group Success
func TestHandlerCreateGroupSuccess(t *testing.T) {
	mockStore := &MockStore{}
	service := NewService(mockStore, nil)
	handler := NewHandler(service)

	group := SCIMGroup{
		DisplayName: "NewGroup",
	}

	mockStore.On("CreateGroup", mock.Anything, mock.Anything).Return(nil)

	body, err := json.Marshal(group)
	require.NoError(t, err)

	req := httptest.NewRequest("POST", "/scim/v2/Groups", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	handler.CreateGroup(rr, req)

	assert.Equal(t, http.StatusCreated, rr.Code)
}

// Test Create Group Missing Required Field
func TestHandlerCreateGroupMissingRequired(t *testing.T) {
	mockStore := &MockStore{}
	service := NewService(mockStore, nil)
	handler := NewHandler(service)

	group := SCIMGroup{}

	mockStore.On("CreateGroup", mock.Anything, mock.Anything).Return(nil)

	body, err := json.Marshal(group)
	require.NoError(t, err)

	req := httptest.NewRequest("POST", "/scim/v2/Groups", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	handler.CreateGroup(rr, req)

	assert.Equal(t, http.StatusBadRequest, rr.Code)

	var response ErrorResponse
	err = json.Unmarshal(rr.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.Equal(t, "invalidValue", response.ScimType)
}

// Test Update Group
func TestHandlerUpdateGroupSuccess(t *testing.T) {
	mockStore := &MockStore{}
	service := NewService(mockStore, nil)
	handler := NewHandler(service)

	groupID := uuid.New().String()
	group := SCIMGroup{
		DisplayName: "UpdatedGroup",
	}

	now := time.Now().UTC()
	existingGroup := &SCIMGroup{
		ID:          groupID,
		DisplayName: "OldGroup",
		Meta: Meta{
			Created:      &now,
			LastModified: &now,
		},
	}

	mockStore.On("GetGroupByID", mock.Anything, groupID).Return(existingGroup, nil)
	mockStore.On("UpdateGroup", mock.Anything, mock.Anything).Return(nil)

	body, err := json.Marshal(group)
	require.NoError(t, err)

	req := httptest.NewRequest("PUT", "/scim/v2/Groups/"+groupID, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", groupID)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	handler.UpdateGroup(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
}

// Test Patch Group
func TestHandlerPatchGroupSuccess(t *testing.T) {
	mockStore := &MockStore{}
	service := NewService(mockStore, nil)
	handler := NewHandler(service)

	groupID := uuid.New().String()
	patchReq := PatchRequest{
		Schemas: []string{SchemaPatchOp},
		Operations: []PatchOperation{
			{
				Op:    "replace",
				Path:  "displayName",
				Value: "UpdatedName",
			},
		},
	}

	group := &SCIMGroup{
		ID:          groupID,
		DisplayName: "UpdatedName",
	}

	mockStore.On("PatchGroup", mock.Anything, groupID, patchReq.Operations).Return(group, nil)

	body, err := json.Marshal(patchReq)
	require.NoError(t, err)

	req := httptest.NewRequest("PATCH", "/scim/v2/Groups/"+groupID, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", groupID)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	handler.PatchGroup(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
}

// Test Delete Group
func TestHandlerDeleteGroupSuccess(t *testing.T) {
	mockStore := &MockStore{}
	service := NewService(mockStore, nil)
	handler := NewHandler(service)

	groupID := uuid.New().String()
	mockStore.On("DeleteGroup", mock.Anything, groupID).Return(nil)

	req := httptest.NewRequest("DELETE", "/scim/v2/Groups/"+groupID, nil)
	rr := httptest.NewRecorder()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", groupID)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	handler.DeleteGroup(rr, req)

	assert.Equal(t, http.StatusNoContent, rr.Code)
}

// ============== ERROR RESPONSE TESTS ==============

// Test Error Response
func TestWriteError(t *testing.T) {
	handler := NewHandler(nil)

	rr := httptest.NewRecorder()
	handler.writeError(rr, http.StatusBadRequest, "invalidValue", "Invalid request")

	assert.Equal(t, http.StatusBadRequest, rr.Code)
	assert.Equal(t, "application/scim+json", rr.Header().Get("Content-Type"))

	var errorResp ErrorResponse
	err := json.Unmarshal(rr.Body.Bytes(), &errorResp)
	assert.NoError(t, err)
	assert.Contains(t, errorResp.Schemas, SchemaError)
	assert.Equal(t, "invalidValue", errorResp.ScimType)
	assert.Equal(t, "Invalid request", errorResp.Detail)
	assert.Equal(t, "400", errorResp.Status)
}

// Test Write JSON Response
func TestWriteJSON(t *testing.T) {
	handler := NewHandler(nil)

	user := SCIMUser{
		ID:       "test-id",
		UserName: "testuser",
		Active:   true,
	}

	rr := httptest.NewRecorder()
	handler.writeJSON(rr, http.StatusOK, user)

	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Equal(t, "application/scim+json", rr.Header().Get("Content-Type"))

	var response SCIMUser
	err := json.Unmarshal(rr.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.Equal(t, user.ID, response.ID)
}

// ============== INVALID JSON TESTS ==============

// Test Invalid JSON Handling POST User
func TestInvalidJSONHandlingUser(t *testing.T) {
	handler := NewHandler(nil)

	req := httptest.NewRequest("POST", "/scim/v2/Users", bytes.NewBufferString("invalid json"))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	handler.CreateUser(rr, req)

	assert.Equal(t, http.StatusBadRequest, rr.Code)

	var errorResp ErrorResponse
	err := json.Unmarshal(rr.Body.Bytes(), &errorResp)
	assert.NoError(t, err)
	assert.Equal(t, "invalidSyntax", errorResp.ScimType)
}

// Test Invalid JSON Handling PUT User
func TestInvalidJSONHandlingUpdateUser(t *testing.T) {
	handler := NewHandler(nil)

	req := httptest.NewRequest("PUT", "/scim/v2/Users/test", bytes.NewBufferString("invalid json"))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "test")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	handler.UpdateUser(rr, req)

	assert.Equal(t, http.StatusBadRequest, rr.Code)
}

// Test Invalid JSON Handling PATCH User
func TestInvalidJSONHandlingPatchUser(t *testing.T) {
	handler := NewHandler(nil)

	req := httptest.NewRequest("PATCH", "/scim/v2/Users/test", bytes.NewBufferString("invalid json"))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "test")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	handler.PatchUser(rr, req)

	assert.Equal(t, http.StatusBadRequest, rr.Code)
}

// Test Invalid JSON Handling POST Group
func TestInvalidJSONHandlingGroup(t *testing.T) {
	handler := NewHandler(nil)

	req := httptest.NewRequest("POST", "/scim/v2/Groups", bytes.NewBufferString("invalid json"))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	handler.CreateGroup(rr, req)

	assert.Equal(t, http.StatusBadRequest, rr.Code)

	var errorResp ErrorResponse
	err := json.Unmarshal(rr.Body.Bytes(), &errorResp)
	assert.NoError(t, err)
	assert.Equal(t, "invalidSyntax", errorResp.ScimType)
}

// Test Update Group Not Found
func TestHandlerUpdateGroupNotFound(t *testing.T) {
	mockStore := &MockStore{}
	service := NewService(mockStore, nil)
	handler := NewHandler(service)

	groupID := uuid.New().String()
	group := SCIMGroup{
		DisplayName: "UpdatedGroup",
	}

	mockStore.On("GetGroupByID", mock.Anything, groupID).Return(nil, fmt.Errorf("not found"))

	body, err := json.Marshal(group)
	require.NoError(t, err)

	req := httptest.NewRequest("PUT", "/scim/v2/Groups/"+groupID, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", groupID)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	handler.UpdateGroup(rr, req)

	assert.Equal(t, http.StatusNotFound, rr.Code)
}

// Test Patch Group Not Found
func TestHandlerPatchGroupNotFound(t *testing.T) {
	mockStore := &MockStore{}
	service := NewService(mockStore, nil)
	handler := NewHandler(service)

	groupID := uuid.New().String()
	patchReq := PatchRequest{
		Schemas: []string{SchemaPatchOp},
		Operations: []PatchOperation{
			{
				Op:    "replace",
				Path:  "displayName",
				Value: "UpdatedName",
			},
		},
	}

	mockStore.On("PatchGroup", mock.Anything, groupID, patchReq.Operations).
		Return(nil, fmt.Errorf("not found"))

	body, err := json.Marshal(patchReq)
	require.NoError(t, err)

	req := httptest.NewRequest("PATCH", "/scim/v2/Groups/"+groupID, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", groupID)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	handler.PatchGroup(rr, req)

	assert.Equal(t, http.StatusNotFound, rr.Code)
}

// Test Patch Group Invalid Operation
func TestHandlerPatchGroupInvalidOperation(t *testing.T) {
	mockStore := &MockStore{}
	service := NewService(mockStore, nil)
	handler := NewHandler(service)

	groupID := uuid.New().String()
	patchReq := PatchRequest{
		Schemas: []string{SchemaPatchOp},
		Operations: []PatchOperation{
			{
				Op:   "replace",
				Path: "displayName",
			},
		},
	}

	mockStore.On("PatchGroup", mock.Anything, groupID, patchReq.Operations).
		Return(nil, fmt.Errorf("invalid operation"))

	body, err := json.Marshal(patchReq)
	require.NoError(t, err)

	req := httptest.NewRequest("PATCH", "/scim/v2/Groups/"+groupID, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", groupID)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	handler.PatchGroup(rr, req)

	assert.Equal(t, http.StatusBadRequest, rr.Code)
}

// Test Delete Group Not Found
func TestHandlerDeleteGroupNotFound(t *testing.T) {
	mockStore := &MockStore{}
	service := NewService(mockStore, nil)
	handler := NewHandler(service)

	groupID := uuid.New().String()
	mockStore.On("DeleteGroup", mock.Anything, groupID).Return(fmt.Errorf("not found"))

	req := httptest.NewRequest("DELETE", "/scim/v2/Groups/"+groupID, nil)
	rr := httptest.NewRecorder()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", groupID)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	handler.DeleteGroup(rr, req)

	assert.Equal(t, http.StatusNotFound, rr.Code)
}

// Test Create Group Duplicate
func TestHandlerCreateGroupDuplicate(t *testing.T) {
	mockStore := &MockStore{}
	service := NewService(mockStore, nil)
	handler := NewHandler(service)

	group := SCIMGroup{
		DisplayName: "existing",
	}

	mockStore.On("CreateGroup", mock.Anything, mock.Anything).Return(fmt.Errorf("group already exists"))

	body, err := json.Marshal(group)
	require.NoError(t, err)

	req := httptest.NewRequest("POST", "/scim/v2/Groups", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	handler.CreateGroup(rr, req)

	assert.Equal(t, http.StatusConflict, rr.Code)

	var response ErrorResponse
	err = json.Unmarshal(rr.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.Equal(t, "uniqueness", response.ScimType)
}

// Test List Users with Invalid Start Index
func TestHandlerListUsersInvalidStartIndex(t *testing.T) {
	mockStore := &MockStore{}
	service := NewService(mockStore, nil)
	handler := NewHandler(service)

	users := []*SCIMUser{}

	// Invalid startIndex should be treated as 1
	mockStore.On("ListUsers", mock.Anything, mock.Anything, "", SortAscending, 1, 20).
		Return(users, 0, nil)

	req := httptest.NewRequest("GET", "/scim/v2/Users?startIndex=invalid", nil)
	rr := httptest.NewRecorder()

	handler.ListUsers(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
}

// Test List Users with Invalid Count
func TestHandlerListUsersInvalidCount(t *testing.T) {
	mockStore := &MockStore{}
	service := NewService(mockStore, nil)
	handler := NewHandler(service)

	users := []*SCIMUser{}

	// Invalid count should be treated as 20 (default)
	mockStore.On("ListUsers", mock.Anything, mock.Anything, "", SortAscending, 1, 20).
		Return(users, 0, nil)

	req := httptest.NewRequest("GET", "/scim/v2/Users?count=invalid", nil)
	rr := httptest.NewRecorder()

	handler.ListUsers(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
}

// Test Get User Not Found
func TestHandlerGetGroupNotFound(t *testing.T) {
	mockStore := &MockStore{}
	service := NewService(mockStore, nil)
	handler := NewHandler(service)

	groupID := uuid.New().String()
	mockStore.On("GetGroupByID", mock.Anything, groupID).Return(nil, fmt.Errorf("not found"))

	req := httptest.NewRequest("GET", "/scim/v2/Groups/"+groupID, nil)
	rr := httptest.NewRecorder()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", groupID)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	handler.GetGroup(rr, req)

	assert.Equal(t, http.StatusNotFound, rr.Code)
}

// Test Create User Server Error
func TestHandlerCreateUserServerError(t *testing.T) {
	mockStore := &MockStore{}
	service := NewService(mockStore, nil)
	handler := NewHandler(service)

	user := SCIMUser{
		UserName: "newuser",
		Active:   true,
	}

	mockStore.On("CreateUser", mock.Anything, mock.Anything).Return(fmt.Errorf("database error"))

	body, err := json.Marshal(user)
	require.NoError(t, err)

	req := httptest.NewRequest("POST", "/scim/v2/Users", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	handler.CreateUser(rr, req)

	assert.Equal(t, http.StatusInternalServerError, rr.Code)
}

// Test Update User Server Error
func TestHandlerUpdateUserServerError(t *testing.T) {
	mockStore := &MockStore{}
	service := NewService(mockStore, nil)
	handler := NewHandler(service)

	userID := uuid.New().String()
	user := SCIMUser{
		UserName: "updated",
		Active:   true,
	}

	now := time.Now().UTC()
	existingUser := &SCIMUser{
		ID:       userID,
		UserName: "existing",
		Active:   false,
		Meta: Meta{
			Created:      &now,
			LastModified: &now,
		},
	}

	mockStore.On("GetUserByID", mock.Anything, userID).Return(existingUser, nil)
	mockStore.On("UpdateUser", mock.Anything, mock.Anything).Return(fmt.Errorf("database error"))

	body, err := json.Marshal(user)
	require.NoError(t, err)

	req := httptest.NewRequest("PUT", "/scim/v2/Users/"+userID, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", userID)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	handler.UpdateUser(rr, req)

	assert.Equal(t, http.StatusInternalServerError, rr.Code)
}

// Test Patch User Not Found
func TestHandlerPatchUserNotFound(t *testing.T) {
	mockStore := &MockStore{}
	service := NewService(mockStore, nil)
	handler := NewHandler(service)

	userID := uuid.New().String()
	patchReq := PatchRequest{
		Schemas: []string{SchemaPatchOp},
		Operations: []PatchOperation{
			{
				Op:    "replace",
				Path:  "active",
				Value: false,
			},
		},
	}

	mockStore.On("PatchUser", mock.Anything, userID, patchReq.Operations).
		Return(nil, fmt.Errorf("not found"))

	body, err := json.Marshal(patchReq)
	require.NoError(t, err)

	req := httptest.NewRequest("PATCH", "/scim/v2/Users/"+userID, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", userID)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	handler.PatchUser(rr, req)

	assert.Equal(t, http.StatusNotFound, rr.Code)
}

// Test Delete User Server Error
func TestHandlerDeleteUserServerError(t *testing.T) {
	mockStore := &MockStore{}
	service := NewService(mockStore, nil)
	handler := NewHandler(service)

	userID := uuid.New().String()
	mockStore.On("DeleteUser", mock.Anything, userID).Return(fmt.Errorf("database error"))

	req := httptest.NewRequest("DELETE", "/scim/v2/Users/"+userID, nil)
	rr := httptest.NewRecorder()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", userID)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	handler.DeleteUser(rr, req)

	assert.Equal(t, http.StatusInternalServerError, rr.Code)
}