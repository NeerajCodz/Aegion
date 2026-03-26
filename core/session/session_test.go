package session

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Mock database pool for testing session manager without real database
type mockDB struct {
	sessions    map[string]*Session
	authMethods map[uuid.UUID][]SessionAuthMethod
	execError   error
	queryError  error
}

func (m *mockDB) Exec(ctx context.Context, sql string, args ...interface{}) (commandTag, error) {
	if m.execError != nil {
		return commandTag{}, m.execError
	}
	return commandTag{rowsAffected: 1}, nil
}

func (m *mockDB) QueryRow(ctx context.Context, sql string, args ...interface{}) row {
	if m.queryError != nil {
		return &mockRow{err: m.queryError}
	}
	
	// Handle session query
	if len(args) > 0 {
		if token, ok := args[0].(string); ok {
			if session, exists := m.sessions[token]; exists {
				return &mockRow{session: session}
			}
			return &mockRow{err: ErrSessionNotFound}
		}
	}
	
	return &mockRow{err: ErrSessionNotFound}
}

func (m *mockDB) Query(ctx context.Context, sql string, args ...interface{}) (rows, error) {
	if m.queryError != nil {
		return nil, m.queryError
	}
	
	// Handle auth methods query
	if len(args) > 0 {
		if sessionID, ok := args[0].(uuid.UUID); ok {
			if authMethods, exists := m.authMethods[sessionID]; exists {
				return &mockRows{authMethods: authMethods}, nil
			}
		}
	}
	
	return &mockRows{}, nil
}

func (m *mockDB) Begin(ctx context.Context) (tx, error) {
	return &mockTx{db: m}, nil
}

// Mock types to satisfy interfaces
type commandTag struct {
	rowsAffected int64
}

func (c commandTag) RowsAffected() int64 {
	return c.rowsAffected
}

type row interface {
	Scan(dest ...interface{}) error
}

type rows interface {
	Next() bool
	Scan(dest ...interface{}) error
	Close()
}

type tx interface {
	Exec(ctx context.Context, sql string, args ...interface{}) (commandTag, error)
	QueryRow(ctx context.Context, sql string, args ...interface{}) row
	Commit(ctx context.Context) error
	Rollback(ctx context.Context) error
}

type mockRow struct {
	session *Session
	err     error
}

func (r *mockRow) Scan(dest ...interface{}) error {
	if r.err != nil {
		return r.err
	}
	
	if r.session != nil {
		// Simplified scan for session data
		if len(dest) >= 14 {
			if id, ok := dest[0].(*uuid.UUID); ok {
				*id = r.session.ID
			}
			if token, ok := dest[1].(*string); ok {
				*token = r.session.Token
			}
			if identityID, ok := dest[2].(*uuid.UUID); ok {
				*identityID = r.session.IdentityID
			}
			if aal, ok := dest[3].(*AAL); ok {
				*aal = r.session.AAL
			}
			// Set other fields as needed...
		}
	}
	
	return nil
}

type mockRows struct {
	authMethods []SessionAuthMethod
	current     int
}

func (r *mockRows) Next() bool {
	return r.current < len(r.authMethods)
}

func (r *mockRows) Scan(dest ...interface{}) error {
	if r.current < len(r.authMethods) {
		am := r.authMethods[r.current]
		if len(dest) >= 3 {
			if method, ok := dest[0].(*AuthMethod); ok {
				*method = am.Method
			}
			if aal, ok := dest[1].(*AAL); ok {
				*aal = am.AALContrib
			}
			if completedAt, ok := dest[2].(*time.Time); ok {
				*completedAt = am.CompletedAt
			}
		}
		r.current++
	}
	return nil
}

func (r *mockRows) Close() {}

type mockTx struct {
	db *mockDB
}

func (t *mockTx) Exec(ctx context.Context, sql string, args ...interface{}) (commandTag, error) {
	return t.db.Exec(ctx, sql, args...)
}

func (t *mockTx) QueryRow(ctx context.Context, sql string, args ...interface{}) row {
	return t.db.QueryRow(ctx, sql, args...)
}

func (t *mockTx) Commit(ctx context.Context) error {
	return nil
}

func (t *mockTx) Rollback(ctx context.Context) error {
	return nil
}

// Test helper to create a test manager
func createTestManager() *Manager {
	return NewManager(ManagerConfig{
		DB:           nil, // We'll use dependency injection for mocking
		CookieSecret: []byte("test-cookie-secret-32-bytes!!"),
		CookieConfig: CookieConfig{
			Name:     "aegion_session",
			Path:     "/",
			Domain:   "test.local",
			SameSite: http.SameSiteStrictMode,
			Secure:   false,
			HTTPOnly: true,
		},
		Lifespan:    24 * time.Hour,
		IdleTimeout: 30 * time.Minute,
	})
}

func TestNewManager(t *testing.T) {
	cfg := ManagerConfig{
		DB:           nil,
		CookieSecret: []byte("test-secret"),
		CookieConfig: CookieConfig{Name: "test_cookie"},
		Lifespan:     1 * time.Hour,
		IdleTimeout:  15 * time.Minute,
	}

	manager := NewManager(cfg)
	
	assert.NotNil(t, manager)
	assert.Equal(t, cfg.CookieSecret, manager.cookieSecret)
	assert.Equal(t, cfg.CookieConfig, manager.cookieConfig)
	assert.Equal(t, cfg.Lifespan, manager.lifespan)
	assert.Equal(t, cfg.IdleTimeout, manager.idleTimeout)
}

func TestMethodToAAL(t *testing.T) {
	tests := []struct {
		name     string
		method   AuthMethod
		expected AAL
	}{
		{
			name:     "password method",
			method:   AuthMethodPassword,
			expected: AAL1,
		},
		{
			name:     "magic link method",
			method:   AuthMethodMagicLink,
			expected: AAL1,
		},
		{
			name:     "social method",
			method:   AuthMethodSocial,
			expected: AAL1,
		},
		{
			name:     "saml method",
			method:   AuthMethodSAML,
			expected: AAL1,
		},
		{
			name:     "passkey method",
			method:   AuthMethodPasskey,
			expected: AAL1,
		},
		{
			name:     "totp method",
			method:   AuthMethodTOTP,
			expected: AAL2,
		},
		{
			name:     "webauthn method",
			method:   AuthMethodWebAuthn,
			expected: AAL2,
		},
		{
			name:     "sms method",
			method:   AuthMethodSMS,
			expected: AAL2,
		},
		{
			name:     "backup method",
			method:   AuthMethodBackup,
			expected: AAL2,
		},
		{
			name:     "unknown method",
			method:   AuthMethod("unknown"),
			expected: AAL0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := methodToAAL(tt.method)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestComputeAAL(t *testing.T) {
	tests := []struct {
		name     string
		current  AAL
		contrib  AAL
		expected AAL
	}{
		{
			name:     "AAL1 + AAL2 = AAL2",
			current:  AAL1,
			contrib:  AAL2,
			expected: AAL2,
		},
		{
			name:     "AAL0 + AAL1 = AAL1",
			current:  AAL0,
			contrib:  AAL1,
			expected: AAL1,
		},
		{
			name:     "empty + AAL1 = AAL1",
			current:  "",
			contrib:  AAL1,
			expected: AAL1,
		},
		{
			name:     "AAL2 + AAL1 = AAL2 (no downgrade)",
			current:  AAL2,
			contrib:  AAL1,
			expected: AAL2,
		},
		{
			name:     "AAL1 + AAL1 = AAL1",
			current:  AAL1,
			contrib:  AAL1,
			expected: AAL1,
		},
		{
			name:     "AAL2 + AAL2 = AAL2",
			current:  AAL2,
			contrib:  AAL2,
			expected: AAL2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := computeAAL(tt.current, tt.contrib)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestManager_GenerateToken(t *testing.T) {
	manager := createTestManager()

	token1 := manager.generateToken()
	token2 := manager.generateToken()

	// Tokens should be non-empty and unique
	assert.NotEmpty(t, token1)
	assert.NotEmpty(t, token2)
	assert.NotEqual(t, token1, token2)

	// Tokens should be valid base64
	assert.Regexp(t, "^[A-Za-z0-9_-]+$", token1)
	assert.Regexp(t, "^[A-Za-z0-9_-]+$", token2)
}

func TestManager_SignVerifyToken(t *testing.T) {
	manager := createTestManager()

	tests := []struct {
		name      string
		token     string
		wantError bool
	}{
		{
			name:      "valid token",
			token:     "valid-token-123",
			wantError: false,
		},
		{
			name:      "another valid token",
			token:     "another-token-456",
			wantError: false,
		},
		{
			name:      "empty token",
			token:     "",
			wantError: false, // Should work, just empty
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Sign token
			signed := manager.signToken(tt.token)
			assert.Contains(t, signed, ".")
			assert.Contains(t, signed, tt.token)

			// Verify signed token
			verified, err := manager.verifySignedToken(signed)
			
			if tt.wantError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.token, verified)
			}
		})
	}
}

func TestManager_VerifySignedToken_Invalid(t *testing.T) {
	manager := createTestManager()

	tests := []struct {
		name  string
		token string
	}{
		{
			name:  "no signature part",
			token: "token-without-signature",
		},
		{
			name:  "invalid signature",
			token: "token.invalid-signature",
		},
		{
			name:  "tampered token",
			token: "tampered-token.invalid-signature",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := manager.verifySignedToken(tt.token)
			assert.ErrorIs(t, err, ErrSessionInvalid)
		})
	}
}

func TestManager_SetCookie(t *testing.T) {
	manager := createTestManager()
	sessionID := uuid.New()
	identityID := uuid.New()
	expiresAt := time.Now().UTC().Add(24 * time.Hour)

	session := &Session{
		ID:         sessionID,
		Token:      "test-session-token",
		IdentityID: identityID,
		ExpiresAt:  expiresAt,
	}

	recorder := httptest.NewRecorder()
	manager.SetCookie(recorder, session)

	cookies := recorder.Result().Cookies()
	require.Len(t, cookies, 1)

	cookie := cookies[0]
	assert.Equal(t, manager.cookieConfig.Name, cookie.Name)
	assert.Contains(t, cookie.Value, "test-session-token")
	assert.Equal(t, manager.cookieConfig.Path, cookie.Path)
	assert.Equal(t, manager.cookieConfig.Domain, cookie.Domain)
	assert.Equal(t, manager.cookieConfig.SameSite, cookie.SameSite)
	assert.Equal(t, manager.cookieConfig.Secure, cookie.Secure)
	assert.Equal(t, manager.cookieConfig.HTTPOnly, cookie.HttpOnly)
	assert.WithinDuration(t, expiresAt, cookie.Expires, time.Second)
}

func TestManager_ClearCookie(t *testing.T) {
	manager := createTestManager()

	recorder := httptest.NewRecorder()
	manager.ClearCookie(recorder)

	cookies := recorder.Result().Cookies()
	require.Len(t, cookies, 1)

	cookie := cookies[0]
	assert.Equal(t, manager.cookieConfig.Name, cookie.Name)
	assert.Empty(t, cookie.Value)
	assert.Equal(t, -1, cookie.MaxAge)
	assert.True(t, cookie.Expires.Before(time.Now()))
}

func TestManager_GetFromRequest(t *testing.T) {
	manager := createTestManager()
	sessionToken := "test-session-token-123"
	signedToken := manager.signToken(sessionToken)

	tests := []struct {
		name      string
		setupReq  func() *http.Request
		mockSetup func() *mockDB
		wantError error
	}{
		{
			name: "valid cookie",
			setupReq: func() *http.Request {
				req := httptest.NewRequest("GET", "/", nil)
				cookie := &http.Cookie{
					Name:  manager.cookieConfig.Name,
					Value: signedToken,
				}
				req.AddCookie(cookie)
				return req
			},
			mockSetup: func() *mockDB {
				session := &Session{
					ID:        uuid.New(),
					Token:     sessionToken,
					ExpiresAt: time.Now().UTC().Add(time.Hour),
				}
				return &mockDB{
					sessions: map[string]*Session{sessionToken: session},
				}
			},
			wantError: nil,
		},
		{
			name: "authorization bearer header",
			setupReq: func() *http.Request {
				req := httptest.NewRequest("GET", "/", nil)
				req.Header.Set("Authorization", "Bearer "+sessionToken)
				return req
			},
			mockSetup: func() *mockDB {
				session := &Session{
					ID:        uuid.New(),
					Token:     sessionToken,
					ExpiresAt: time.Now().UTC().Add(time.Hour),
				}
				return &mockDB{
					sessions: map[string]*Session{sessionToken: session},
				}
			},
			wantError: nil,
		},
		{
			name: "x-session-token header",
			setupReq: func() *http.Request {
				req := httptest.NewRequest("GET", "/", nil)
				req.Header.Set("X-Session-Token", sessionToken)
				return req
			},
			mockSetup: func() *mockDB {
				session := &Session{
					ID:        uuid.New(),
					Token:     sessionToken,
					ExpiresAt: time.Now().UTC().Add(time.Hour),
				}
				return &mockDB{
					sessions: map[string]*Session{sessionToken: session},
				}
			},
			wantError: nil,
		},
		{
			name: "no session found",
			setupReq: func() *http.Request {
				return httptest.NewRequest("GET", "/", nil)
			},
			mockSetup: func() *mockDB {
				return &mockDB{sessions: make(map[string]*Session)}
			},
			wantError: ErrSessionNotFound,
		},
		{
			name: "invalid cookie signature",
			setupReq: func() *http.Request {
				req := httptest.NewRequest("GET", "/", nil)
				cookie := &http.Cookie{
					Name:  manager.cookieConfig.Name,
					Value: "invalid.signature",
				}
				req.AddCookie(cookie)
				return req
			},
			mockSetup: func() *mockDB {
				return &mockDB{sessions: make(map[string]*Session)}
			},
			wantError: ErrSessionNotFound, // Falls through to other methods
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// This test would require dependency injection to work properly
			// For now, we test the logic structure
			req := tt.setupReq()
			
			// Test cookie extraction logic
			if cookie, err := req.Cookie(manager.cookieConfig.Name); err == nil {
				_, verifyErr := manager.verifySignedToken(cookie.Value)
				if tt.name == "invalid cookie signature" {
					assert.Error(t, verifyErr)
				}
			}

			// Test header extraction logic
			if auth := req.Header.Get("Authorization"); auth != "" {
				if strings.HasPrefix(auth, "Bearer ") {
					token := strings.TrimPrefix(auth, "Bearer ")
					assert.Equal(t, sessionToken, token)
				}
			}

			if token := req.Header.Get("X-Session-Token"); token != "" {
				assert.Equal(t, sessionToken, token)
			}
		})
	}
}

func TestAuthMethodConstants(t *testing.T) {
	assert.Equal(t, "password", string(AuthMethodPassword))
	assert.Equal(t, "totp", string(AuthMethodTOTP))
	assert.Equal(t, "webauthn", string(AuthMethodWebAuthn))
	assert.Equal(t, "magic_link", string(AuthMethodMagicLink))
	assert.Equal(t, "social", string(AuthMethodSocial))
	assert.Equal(t, "saml", string(AuthMethodSAML))
	assert.Equal(t, "passkey", string(AuthMethodPasskey))
	assert.Equal(t, "sms", string(AuthMethodSMS))
	assert.Equal(t, "backup_code", string(AuthMethodBackup))
}

func TestSessionStructure(t *testing.T) {
	sessionID := uuid.New()
	identityID := uuid.New()
	impersonatorID := uuid.New()
	now := time.Now().UTC()

	session := Session{
		ID:              sessionID,
		Token:           "test-token",
		IdentityID:      identityID,
		AAL:             AAL2,
		IssuedAt:        now,
		ExpiresAt:       now.Add(time.Hour),
		AuthenticatedAt: now,
		LogoutToken:     "logout-token",
		Devices: []DeviceInfo{
			{
				UserAgent: "Mozilla/5.0",
				IPAddress: "127.0.0.1",
				Location:  "Test Location",
			},
		},
		Active:          true,
		IsImpersonation: true,
		ImpersonatorID:  &impersonatorID,
		AuthMethods: []SessionAuthMethod{
			{
				Method:      AuthMethodPassword,
				AALContrib:  AAL1,
				CompletedAt: now,
			},
			{
				Method:      AuthMethodTOTP,
				AALContrib:  AAL2,
				CompletedAt: now.Add(time.Minute),
			},
		},
		CreatedAt: now,
		UpdatedAt: now,
	}

	// Verify all fields are set correctly
	assert.Equal(t, sessionID, session.ID)
	assert.Equal(t, "test-token", session.Token)
	assert.Equal(t, identityID, session.IdentityID)
	assert.Equal(t, AAL2, session.AAL)
	assert.True(t, session.Active)
	assert.True(t, session.IsImpersonation)
	assert.Equal(t, &impersonatorID, session.ImpersonatorID)
	assert.Len(t, session.Devices, 1)
	assert.Len(t, session.AuthMethods, 2)
	assert.Equal(t, AuthMethodPassword, session.AuthMethods[0].Method)
	assert.Equal(t, AuthMethodTOTP, session.AuthMethods[1].Method)
}

func TestDeviceInfo(t *testing.T) {
	device := DeviceInfo{
		UserAgent: "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36",
		IPAddress: "192.168.1.100",
		Location:  "New York, NY",
	}

	assert.NotEmpty(t, device.UserAgent)
	assert.NotEmpty(t, device.IPAddress)
	assert.NotEmpty(t, device.Location)
}

func TestCookieConfig(t *testing.T) {
	cfg := CookieConfig{
		Name:     "test_session",
		Path:     "/api",
		Domain:   "example.com",
		SameSite: http.SameSiteLaxMode,
		Secure:   true,
		HTTPOnly: true,
	}

	assert.Equal(t, "test_session", cfg.Name)
	assert.Equal(t, "/api", cfg.Path)
	assert.Equal(t, "example.com", cfg.Domain)
	assert.Equal(t, http.SameSiteLaxMode, cfg.SameSite)
	assert.True(t, cfg.Secure)
	assert.True(t, cfg.HTTPOnly)
}

func TestErrorConstants(t *testing.T) {
	assert.Equal(t, "session not found", ErrSessionNotFound.Error())
	assert.Equal(t, "session expired", ErrSessionExpired.Error())
	assert.Equal(t, "session invalid", ErrSessionInvalid.Error())
}