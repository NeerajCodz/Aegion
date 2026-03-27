package authtoken

import (
	"bytes"
	"context"
	"crypto/rand"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMiddleware(t *testing.T) {
	secret := make([]byte, 32)
	_, err := rand.Read(secret)
	require.NoError(t, err)

	gen, err := NewGenerator(GeneratorConfig{
		Secret: secret,
		TTL:    5 * time.Minute,
	})
	require.NoError(t, err)

	// Create a test handler that checks if module ID is in context
	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		moduleID := ModuleIDFromContext(r.Context())
		w.Header().Set("X-Module-ID", moduleID)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("success"))
	})

	tests := []struct {
		name           string
		setupRequest   func() *http.Request
		config         MiddlewareConfig
		expectedStatus int
		expectedModule string
		expectBody     string
	}{
		{
			name: "valid token in header",
			setupRequest: func() *http.Request {
				token, err := gen.Generate("password")
				require.NoError(t, err)
				req := httptest.NewRequest("GET", "/test", nil)
				req.Header.Set(HeaderInternalToken, token)
				return req
			},
			config: MiddlewareConfig{
				Generator: gen,
			},
			expectedStatus: http.StatusOK,
			expectedModule: "password",
			expectBody:     "success",
		},
		{
			name: "missing token",
			setupRequest: func() *http.Request {
				return httptest.NewRequest("GET", "/test", nil)
			},
			config: MiddlewareConfig{
				Generator: gen,
			},
			expectedStatus: http.StatusUnauthorized,
			expectedModule: "",
			expectBody:     "missing internal auth token\n",
		},
		{
			name: "invalid token format",
			setupRequest: func() *http.Request {
				req := httptest.NewRequest("GET", "/test", nil)
				req.Header.Set(HeaderInternalToken, "invalid.token.format")
				return req
			},
			config: MiddlewareConfig{
				Generator: gen,
			},
			expectedStatus: http.StatusUnauthorized,
			expectedModule: "",
			expectBody:     "invalid internal auth token\n",
		},
		{
			name: "empty token",
			setupRequest: func() *http.Request {
				req := httptest.NewRequest("GET", "/test", nil)
				req.Header.Set(HeaderInternalToken, "")
				return req
			},
			config: MiddlewareConfig{
				Generator: gen,
			},
			expectedStatus: http.StatusUnauthorized,
			expectedModule: "",
			expectBody:     "missing internal auth token\n",
		},
		{
			name: "skip path - no token required",
			setupRequest: func() *http.Request {
				return httptest.NewRequest("GET", "/health", nil)
			},
			config: MiddlewareConfig{
				Generator: gen,
				SkipPaths: []string{"/health", "/metrics"},
			},
			expectedStatus: http.StatusOK,
			expectedModule: "",
			expectBody:     "success",
		},
		{
			name: "skip path with token - still skipped",
			setupRequest: func() *http.Request {
				token, err := gen.Generate("test")
				require.NoError(t, err)
				req := httptest.NewRequest("GET", "/metrics", nil)
				req.Header.Set(HeaderInternalToken, token)
				return req
			},
			config: MiddlewareConfig{
				Generator: gen,
				SkipPaths: []string{"/health", "/metrics"},
			},
			expectedStatus: http.StatusOK,
			expectedModule: "",
			expectBody:     "success",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			middleware := Middleware(tt.config)
			handler := middleware(testHandler)

			req := tt.setupRequest()
			recorder := httptest.NewRecorder()

			handler.ServeHTTP(recorder, req)

			assert.Equal(t, tt.expectedStatus, recorder.Code)
			assert.Equal(t, tt.expectBody, recorder.Body.String())
			assert.Equal(t, tt.expectedModule, recorder.Header().Get("X-Module-ID"))
		})
	}
}

func TestMiddleware_WithLogger(t *testing.T) {
	secret := make([]byte, 32)
	_, err := rand.Read(secret)
	require.NoError(t, err)

	gen, err := NewGenerator(GeneratorConfig{
		Secret: secret,
		TTL:    5 * time.Minute,
	})
	require.NoError(t, err)

	// Create logger that captures output
	var logOutput bytes.Buffer
	logger := zerolog.New(&logOutput).With().Timestamp().Logger()

	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	middleware := Middleware(MiddlewareConfig{
		Generator: gen,
		Logger:    &logger,
	})
	handler := middleware(testHandler)

	tests := []struct {
		name          string
		setupRequest  func() *http.Request
		expectLogLine bool
	}{
		{
			name: "valid token - debug log",
			setupRequest: func() *http.Request {
				token, err := gen.Generate("test-module")
				require.NoError(t, err)
				req := httptest.NewRequest("GET", "/test", nil)
				req.Header.Set(HeaderInternalToken, token)
				return req
			},
			expectLogLine: true,
		},
		{
			name: "missing token - warn log",
			setupRequest: func() *http.Request {
				return httptest.NewRequest("GET", "/test", nil)
			},
			expectLogLine: true,
		},
		{
			name: "invalid token - warn log",
			setupRequest: func() *http.Request {
				req := httptest.NewRequest("GET", "/test", nil)
				req.Header.Set(HeaderInternalToken, "invalid")
				return req
			},
			expectLogLine: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset log output
			logOutput.Reset()

			req := tt.setupRequest()
			recorder := httptest.NewRecorder()

			handler.ServeHTTP(recorder, req)

			if tt.expectLogLine {
				assert.Greater(t, logOutput.Len(), 0, "expected log output")
			}
		})
	}
}

func TestModuleIDFromContext(t *testing.T) {
	tests := []struct {
		name     string
		setupCtx func() context.Context
		expected string
	}{
		{
			name: "context with module ID",
			setupCtx: func() context.Context {
				return context.WithValue(context.Background(), ContextKeyModuleID, "test-module")
			},
			expected: "test-module",
		},
		{
			name: "context without module ID",
			setupCtx: func() context.Context {
				return context.Background()
			},
			expected: "",
		},
		{
			name: "context with wrong type",
			setupCtx: func() context.Context {
				return context.WithValue(context.Background(), ContextKeyModuleID, 123)
			},
			expected: "",
		},
		{
			name: "context with nil value",
			setupCtx: func() context.Context {
				return context.WithValue(context.Background(), ContextKeyModuleID, nil)
			},
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := tt.setupCtx()
			result := ModuleIDFromContext(ctx)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestRequireModuleID(t *testing.T) {
	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("success"))
	})

	middleware := RequireModuleID("password", "magic_link")
	handler := middleware(testHandler)

	tests := []struct {
		name           string
		setupRequest   func() *http.Request
		expectedStatus int
		expectBody     string
	}{
		{
			name: "allowed module ID",
			setupRequest: func() *http.Request {
				ctx := context.WithValue(context.Background(), ContextKeyModuleID, "password")
				return httptest.NewRequest("GET", "/test", nil).WithContext(ctx)
			},
			expectedStatus: http.StatusOK,
			expectBody:     "success",
		},
		{
			name: "another allowed module ID",
			setupRequest: func() *http.Request {
				ctx := context.WithValue(context.Background(), ContextKeyModuleID, "magic_link")
				return httptest.NewRequest("GET", "/test", nil).WithContext(ctx)
			},
			expectedStatus: http.StatusOK,
			expectBody:     "success",
		},
		{
			name: "forbidden module ID",
			setupRequest: func() *http.Request {
				ctx := context.WithValue(context.Background(), ContextKeyModuleID, "admin")
				return httptest.NewRequest("GET", "/test", nil).WithContext(ctx)
			},
			expectedStatus: http.StatusForbidden,
			expectBody:     "forbidden\n",
		},
		{
			name: "no module ID in context",
			setupRequest: func() *http.Request {
				return httptest.NewRequest("GET", "/test", nil)
			},
			expectedStatus: http.StatusForbidden,
			expectBody:     "forbidden\n",
		},
		{
			name: "empty module ID",
			setupRequest: func() *http.Request {
				ctx := context.WithValue(context.Background(), ContextKeyModuleID, "")
				return httptest.NewRequest("GET", "/test", nil).WithContext(ctx)
			},
			expectedStatus: http.StatusForbidden,
			expectBody:     "forbidden\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := tt.setupRequest()
			recorder := httptest.NewRecorder()

			handler.ServeHTTP(recorder, req)

			assert.Equal(t, tt.expectedStatus, recorder.Code)
			assert.Equal(t, tt.expectBody, recorder.Body.String())
		})
	}
}

func TestRequireModuleID_EmptyAllowedList(t *testing.T) {
	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	middleware := RequireModuleID() // No allowed modules
	handler := middleware(testHandler)

	ctx := context.WithValue(context.Background(), ContextKeyModuleID, "any-module")
	req := httptest.NewRequest("GET", "/test", nil).WithContext(ctx)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	assert.Equal(t, http.StatusForbidden, recorder.Code)
}

func TestMiddleware_Integration(t *testing.T) {
	secret := make([]byte, 32)
	_, err := rand.Read(secret)
	require.NoError(t, err)

	gen, err := NewGenerator(GeneratorConfig{
		Secret: secret,
		TTL:    5 * time.Minute,
	})
	require.NoError(t, err)

	// Create a handler chain: auth middleware + module restriction + final handler
	finalHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		moduleID := ModuleIDFromContext(r.Context())
		w.Header().Set("X-Module", moduleID)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("authenticated and authorized"))
	})

	authMiddleware := Middleware(MiddlewareConfig{
		Generator: gen,
		SkipPaths: []string{"/health"},
	})
	
	// Create a conditional module middleware that skips health endpoints
	moduleMiddleware := func(next http.Handler) http.Handler {
		requireModuleMiddleware := RequireModuleID("password", "admin")
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Skip module check for health endpoint
			if r.URL.Path == "/health" {
				next.ServeHTTP(w, r)
				return
			}
			requireModuleMiddleware(next).ServeHTTP(w, r)
		})
	}
	
	handler := authMiddleware(moduleMiddleware(finalHandler))

	tests := []struct {
		name           string
		path           string
		moduleID       string
		setupAuth      bool
		expectedStatus int
		expectedModule string
	}{
		{
			name:           "valid password module",
			path:           "/api/password",
			moduleID:       "password",
			setupAuth:      true,
			expectedStatus: http.StatusOK,
			expectedModule: "password",
		},
		{
			name:           "valid admin module",
			path:           "/api/admin",
			moduleID:       "admin",
			setupAuth:      true,
			expectedStatus: http.StatusOK,
			expectedModule: "admin",
		},
		{
			name:           "invalid module - forbidden",
			path:           "/api/test",
			moduleID:       "magic_link",
			setupAuth:      true,
			expectedStatus: http.StatusForbidden,
			expectedModule: "",
		},
		{
			name:           "no auth token - unauthorized",
			path:           "/api/password",
			moduleID:       "",
			setupAuth:      false,
			expectedStatus: http.StatusUnauthorized,
			expectedModule: "",
		},
		{
			name:           "health endpoint - bypass all middleware",
			path:           "/health",
			moduleID:       "",
			setupAuth:      false,
			expectedStatus: http.StatusOK,
			expectedModule: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", tt.path, nil)

			if tt.setupAuth {
				token, err := gen.Generate(tt.moduleID)
				require.NoError(t, err)
				req.Header.Set(HeaderInternalToken, token)
			}

			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, req)

			assert.Equal(t, tt.expectedStatus, recorder.Code)
			if tt.expectedModule != "" {
				assert.Equal(t, tt.expectedModule, recorder.Header().Get("X-Module"))
			}
		})
	}
}

func TestHeaderConstants(t *testing.T) {
	assert.Equal(t, "X-Aegion-Internal-Token", HeaderInternalToken)
}

func TestContextKeyConstants(t *testing.T) {
	assert.Equal(t, contextKey("aegion_module_id"), ContextKeyModuleID)
}

func BenchmarkMiddleware(b *testing.B) {
	secret := make([]byte, 32)
	_, err := rand.Read(secret)
	require.NoError(b, err)

	gen, err := NewGenerator(GeneratorConfig{
		Secret: secret,
		TTL:    5 * time.Minute,
	})
	require.NoError(b, err)

	token, err := gen.Generate("benchmark-module")
	require.NoError(b, err)

	middleware := Middleware(MiddlewareConfig{
		Generator: gen,
	})

	handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set(HeaderInternalToken, token)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, req)
	}
}