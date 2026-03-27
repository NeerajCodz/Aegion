package proxy

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"

	"github.com/aegion/aegion/core/session"
)

// TestAuthMiddleware_InjectHeaders_Complete tests complete header injection
func TestAuthMiddleware_InjectHeaders_Complete(t *testing.T) {
	identityID := uuid.New()
	sessionID := uuid.New()
	authTime := time.Now().UTC()
	expiresAt := authTime.Add(24 * time.Hour)

	sess := &session.Session{
		ID:              sessionID,
		IdentityID:      identityID,
		Active:          true,
		AuthenticatedAt: authTime,
		ExpiresAt:       expiresAt,
		AAL:             session.AAL2,
	}

	am := NewAuthMiddleware(nil, zerolog.New(zerolog.NewTestWriter(t)), false)

	req := httptest.NewRequest("GET", "/api/test", nil)
	am.InjectHeaders(req, sess)

	assert.Equal(t, sessionID.String(), req.Header.Get("X-Aegion-Session-ID"))
	assert.Equal(t, identityID.String(), req.Header.Get("X-Aegion-Identity-ID"))
	assert.Equal(t, string(session.AAL2), req.Header.Get("X-Aegion-AAL"))
	assert.Equal(t, authTime.Format(time.RFC3339), req.Header.Get("X-Aegion-Authenticated-At"))
	assert.Equal(t, expiresAt.Format(time.RFC3339), req.Header.Get("X-Aegion-Expires-At"))
	assert.Equal(t, "", req.Header.Get("X-Aegion-Impersonation"))
}

// TestAuthMiddleware_InjectHeaders_NilSession tests nil session handling
func TestAuthMiddleware_InjectHeaders_NilSession(t *testing.T) {
	am := NewAuthMiddleware(nil, zerolog.New(zerolog.NewTestWriter(t)), false)

	req := httptest.NewRequest("GET", "/api/test", nil)
	am.InjectHeaders(req, nil)

	// Should not panic and should not set any headers
	assert.Equal(t, "", req.Header.Get("X-Aegion-Session-ID"))
	assert.Equal(t, "", req.Header.Get("X-Aegion-Identity-ID"))
}

// TestAuthMiddleware_InjectHeaders_WithImpersonation tests impersonation header injection
func TestAuthMiddleware_InjectHeaders_WithImpersonation(t *testing.T) {
	identityID := uuid.New()
	sessionID := uuid.New()
	impersonatorID := uuid.New()
	authTime := time.Now().UTC()
	expiresAt := authTime.Add(24 * time.Hour)

	sess := &session.Session{
		ID:              sessionID,
		IdentityID:      identityID,
		Active:          true,
		AuthenticatedAt: authTime,
		ExpiresAt:       expiresAt,
		AAL:             session.AAL1,
		IsImpersonation: true,
		ImpersonatorID:  &impersonatorID,
	}

	am := NewAuthMiddleware(nil, zerolog.New(zerolog.NewTestWriter(t)), false)

	req := httptest.NewRequest("GET", "/api/test", nil)
	am.InjectHeaders(req, sess)

	assert.Equal(t, "true", req.Header.Get("X-Aegion-Impersonation"))
	assert.Equal(t, impersonatorID.String(), req.Header.Get("X-Aegion-Impersonator-ID"))
}

// TestAuthMiddleware_InjectHeaders_WithAuthMethods tests auth methods header injection
func TestAuthMiddleware_InjectHeaders_WithAuthMethods(t *testing.T) {
	identityID := uuid.New()
	sessionID := uuid.New()
	authTime := time.Now().UTC()
	expiresAt := authTime.Add(24 * time.Hour)

	sess := &session.Session{
		ID:              sessionID,
		IdentityID:      identityID,
		Active:          true,
		AuthenticatedAt: authTime,
		ExpiresAt:       expiresAt,
		AAL:             session.AAL1,
		AuthMethods: []session.SessionAuthMethod{
			{Method: session.AuthMethodPassword},
			{Method: session.AuthMethodTOTP},
		},
	}

	am := NewAuthMiddleware(nil, zerolog.New(zerolog.NewTestWriter(t)), false)

	req := httptest.NewRequest("GET", "/api/test", nil)
	am.InjectHeaders(req, sess)

	authMethodsHeader := req.Header.Get("X-Aegion-Auth-Methods")
	assert.Contains(t, authMethodsHeader, string(session.AuthMethodPassword))
	assert.Contains(t, authMethodsHeader, string(session.AuthMethodTOTP))
}

// TestAuthMiddleware_InjectHeaders_WithDevices tests device header injection
func TestAuthMiddleware_InjectHeaders_WithDevices(t *testing.T) {
	identityID := uuid.New()
	sessionID := uuid.New()
	authTime := time.Now().UTC()
	expiresAt := authTime.Add(24 * time.Hour)

	sess := &session.Session{
		ID:              sessionID,
		IdentityID:      identityID,
		Active:          true,
		AuthenticatedAt: authTime,
		ExpiresAt:       expiresAt,
		AAL:             session.AAL1,
		Devices: []session.DeviceInfo{
			{
				IPAddress: "192.168.1.1",
				UserAgent: "Mozilla/5.0 (Windows NT 10.0; Win64; x64)",
			},
			{
				IPAddress: "10.0.0.1",
				UserAgent: "curl/7.68.0",
			},
		},
	}

	am := NewAuthMiddleware(nil, zerolog.New(zerolog.NewTestWriter(t)), false)

	req := httptest.NewRequest("GET", "/api/test", nil)
	am.InjectHeaders(req, sess)

	// Should use the latest device (index 1)
	assert.Equal(t, "10.0.0.1", req.Header.Get("X-Aegion-Device-IP"))
	assert.Equal(t, "curl/7.68.0", req.Header.Get("X-Aegion-Device-UA"))
}

// TestAuthMiddleware_InjectHeaders_WithDevices_EmptyFields tests device with empty fields
func TestAuthMiddleware_InjectHeaders_WithDevices_EmptyFields(t *testing.T) {
	identityID := uuid.New()
	sessionID := uuid.New()
	authTime := time.Now().UTC()
	expiresAt := authTime.Add(24 * time.Hour)

	sess := &session.Session{
		ID:              sessionID,
		IdentityID:      identityID,
		Active:          true,
		AuthenticatedAt: authTime,
		ExpiresAt:       expiresAt,
		AAL:             session.AAL1,
		Devices: []session.DeviceInfo{
			{
				IPAddress: "",
				UserAgent: "",
			},
		},
	}

	am := NewAuthMiddleware(nil, zerolog.New(zerolog.NewTestWriter(t)), false)

	req := httptest.NewRequest("GET", "/api/test", nil)
	am.InjectHeaders(req, sess)

	// Should not set empty headers
	assert.Equal(t, "", req.Header.Get("X-Aegion-Device-IP"))
	assert.Equal(t, "", req.Header.Get("X-Aegion-Device-UA"))
}

// TestAuthMiddleware_HandleAuthError_SessionNotFound tests error handling for missing session
func TestAuthMiddleware_HandleAuthError_SessionNotFound(t *testing.T) {
	am := NewAuthMiddleware(nil, zerolog.New(zerolog.NewTestWriter(t)), false)

	req := httptest.NewRequest("GET", "/api/test", nil)
	req = req.WithContext(context.WithValue(req.Context(), "request_id", "test-123"))
	w := httptest.NewRecorder()

	am.handleAuthError(w, req, session.ErrSessionNotFound)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
	assert.Equal(t, "application/json", w.Header().Get("Content-Type"))
	assert.Equal(t, "no-cache, no-store, must-revalidate", w.Header().Get("Cache-Control"))
	assert.Equal(t, "no-cache", w.Header().Get("Pragma"))
	assert.Equal(t, "0", w.Header().Get("Expires"))
}

// TestAuthMiddleware_HandleAuthError_SessionExpired tests error handling for expired session
func TestAuthMiddleware_HandleAuthError_SessionExpired(t *testing.T) {
	am := NewAuthMiddleware(nil, zerolog.New(zerolog.NewTestWriter(t)), false)

	req := httptest.NewRequest("GET", "/api/test", nil)
	req = req.WithContext(context.WithValue(req.Context(), "request_id", "test-123"))
	w := httptest.NewRecorder()

	am.handleAuthError(w, req, session.ErrSessionExpired)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
	assert.Equal(t, "reauthenticate", w.Header().Get("X-Aegion-Action"))
}

// TestAuthMiddleware_HandleAuthError_SessionInvalid tests error handling for invalid session
func TestAuthMiddleware_HandleAuthError_SessionInvalid(t *testing.T) {
	am := NewAuthMiddleware(nil, zerolog.New(zerolog.NewTestWriter(t)), false)

	req := httptest.NewRequest("GET", "/api/test", nil)
	req = req.WithContext(context.WithValue(req.Context(), "request_id", "test-123"))
	w := httptest.NewRecorder()

	am.handleAuthError(w, req, session.ErrSessionInvalid)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

// TestRequireAAL_Sufficient tests AAL requirement middleware with sufficient AAL
func TestRequireAAL_Sufficient(t *testing.T) {
	sess := &session.Session{
		ID:  uuid.New(),
		AAL: session.AAL2,
	}

	handlerCalled := false
	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handlerCalled = true
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest("GET", "/api/test", nil)
	req = req.WithContext(session.WithSession(req.Context(), sess))
	w := httptest.NewRecorder()

	middleware := RequireAAL(session.AAL1)
	middleware(nextHandler).ServeHTTP(w, req)

	assert.True(t, handlerCalled)
	assert.Equal(t, http.StatusOK, w.Code)
}

// TestRequireAAL_Insufficient tests AAL requirement middleware with insufficient AAL
func TestRequireAAL_Insufficient(t *testing.T) {
	sess := &session.Session{
		ID:  uuid.New(),
		AAL: session.AAL0,
	}

	handlerCalled := false
	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handlerCalled = true
	})

	req := httptest.NewRequest("GET", "/api/test", nil)
	req = req.WithContext(session.WithSession(req.Context(), sess))
	w := httptest.NewRecorder()

	middleware := RequireAAL(session.AAL2)
	middleware(nextHandler).ServeHTTP(w, req)

	assert.False(t, handlerCalled)
	assert.Equal(t, http.StatusForbidden, w.Code)
}

// TestRequireAAL_NoSession tests AAL requirement middleware without session
func TestRequireAAL_NoSession(t *testing.T) {
	handlerCalled := false
	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handlerCalled = true
	})

	req := httptest.NewRequest("GET", "/api/test", nil)
	w := httptest.NewRecorder()

	middleware := RequireAAL(session.AAL1)
	middleware(nextHandler).ServeHTTP(w, req)

	assert.False(t, handlerCalled)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

// TestRequireCapabilities_NoSession tests capability requirement middleware without session
func TestRequireCapabilities_NoSession(t *testing.T) {
	handlerCalled := false
	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handlerCalled = true
	})

	req := httptest.NewRequest("GET", "/api/test", nil)
	w := httptest.NewRecorder()

	middleware := RequireCapabilities("admin")
	middleware(nextHandler).ServeHTTP(w, req)

	assert.False(t, handlerCalled)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

// TestGetRequestIDFromContext_Success tests successful request ID extraction
func TestGetRequestIDFromContext_Success(t *testing.T) {
	ctx := context.WithValue(context.Background(), "request_id", "test-123")
	id := getRequestIDFromContext(ctx)
	assert.Equal(t, "test-123", id)
}

// TestGetRequestIDFromContext_Empty tests empty context
func TestGetRequestIDFromContext_Empty(t *testing.T) {
	ctx := context.Background()
	id := getRequestIDFromContext(ctx)
	assert.Equal(t, "", id)
}

// TestGetRequestIDFromContext_TypeAssertion tests type assertion failure
func TestGetRequestIDFromContext_TypeAssertion(t *testing.T) {
	ctx := context.WithValue(context.Background(), "request_id", 123) // Wrong type
	id := getRequestIDFromContext(ctx)
	assert.Equal(t, "", id)
}

// TestJoinStrings_Empty tests empty slice
func TestJoinStrings_Empty(t *testing.T) {
	result := joinStrings([]string{}, ",")
	assert.Equal(t, "", result)
}

// TestJoinStrings_Single tests single element
func TestJoinStrings_Single(t *testing.T) {
	result := joinStrings([]string{"password"}, ",")
	assert.Equal(t, "password", result)
}

// TestJoinStrings_Multiple tests multiple elements
func TestJoinStrings_Multiple(t *testing.T) {
	result := joinStrings([]string{"password", "mfa", "webauthn"}, ",")
	assert.Equal(t, "password,mfa,webauthn", result)
}

// TestJoinStrings_CustomSeparator tests custom separator
func TestJoinStrings_CustomSeparator(t *testing.T) {
	result := joinStrings([]string{"password", "mfa"}, ";")
	assert.Equal(t, "password;mfa", result)
}

// TestWriteErrorResponse tests error response writing
func TestWriteErrorResponse(t *testing.T) {
	w := httptest.NewRecorder()
	writeErrorResponse(w, http.StatusForbidden, "Access denied")

	assert.Equal(t, http.StatusForbidden, w.Code)
	assert.Equal(t, "application/json", w.Header().Get("Content-Type"))
	assert.Contains(t, w.Body.String(), "Access denied")
	assert.Contains(t, w.Body.String(), "403")
}

// TestWriteErrorResponse_Unauthorized tests error response for unauthorized
func TestWriteErrorResponse_Unauthorized(t *testing.T) {
	w := httptest.NewRecorder()
	writeErrorResponse(w, http.StatusUnauthorized, "Authentication required")

	assert.Equal(t, http.StatusUnauthorized, w.Code)
	assert.Contains(t, w.Body.String(), "Authentication required")
	assert.Contains(t, w.Body.String(), "401")
}

// TestNewAuthMiddleware tests the NewAuthMiddleware constructor
func TestNewAuthMiddleware(t *testing.T) {
	logger := zerolog.New(zerolog.NewTestWriter(t))

	// Test with optional=false
	am := NewAuthMiddleware(nil, logger, false)
	assert.NotNil(t, am)
	assert.False(t, am.optional)

	// Test with optional=true
	am = NewAuthMiddleware(nil, logger, true)
	assert.NotNil(t, am)
	assert.True(t, am.optional)
}
