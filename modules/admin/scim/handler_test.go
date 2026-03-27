package scim

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
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

// Test Authentication Middleware
func TestAuthMiddleware(t *testing.T) {
	handler := NewHandler(nil)
	
	// Test missing authorization header
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
	
	// Test invalid bearer token format
	req = httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Basic invalid")
	rr = httptest.NewRecorder()
	
	middleware.ServeHTTP(rr, req)
	
	assert.Equal(t, http.StatusUnauthorized, rr.Code)
	assert.False(t, nextCalled)
}

// Test Permission Middleware  
func TestRequirePermissionMiddleware(t *testing.T) {
	handler := NewHandler(nil)
	
	// Test with no token in context
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

// Test User Creation Endpoint
func TestHandlerCreateUser(t *testing.T) {
	handler := NewHandler(nil)
	// For simplicity, we'll just test the error path with invalid JSON
	
	req := httptest.NewRequest("POST", "/scim/v2/Users", bytes.NewBufferString("invalid json"))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	
	handler.CreateUser(rr, req)
	
	assert.Equal(t, http.StatusBadRequest, rr.Code)
	assert.Equal(t, "application/scim+json", rr.Header().Get("Content-Type"))
	
	var response ErrorResponse
	err := json.Unmarshal(rr.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.Equal(t, "invalidSyntax", response.ScimType)
}

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

// Test URL Parameter Parsing
func TestURLParameterHandling(t *testing.T) {
	handler := NewHandler(nil)
	handler.service = &Service{} // Use real service for schema test
	
	// Test with URL parameters in GetSchema
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

// Test Invalid JSON Handling
func TestInvalidJSONHandling(t *testing.T) {
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