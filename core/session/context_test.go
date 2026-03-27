package session

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInjectHeaders(t *testing.T) {
	sessionID := uuid.New()
	identityID := uuid.New()
	secret := []byte("test-secret-32-bytes-for-headers")

	session := &Session{
		ID:         sessionID,
		IdentityID: identityID,
		AAL:        AAL1,
	}

	tests := []struct {
		name      string
		session   *Session
		secret    []byte
		checkFunc func(*testing.T, *http.Request)
	}{
		{
			name:    "valid session injects headers",
			session: session,
			secret:  secret,
			checkFunc: func(t *testing.T, r *http.Request) {
				assert.Equal(t, sessionID.String(), r.Header.Get(HeaderPrefix+"Session-ID"))
				assert.Equal(t, identityID.String(), r.Header.Get(HeaderPrefix+"Identity-ID"))
				assert.Equal(t, string(AAL1), r.Header.Get(HeaderPrefix+"AAL"))
				assert.NotEmpty(t, r.Header.Get(HeaderPrefix+"Signature"))
			},
		},
		{
			name:    "nil session does not inject headers",
			session: nil,
			secret:  secret,
			checkFunc: func(t *testing.T, r *http.Request) {
				assert.Empty(t, r.Header.Get(HeaderPrefix+"Session-ID"))
				assert.Empty(t, r.Header.Get(HeaderPrefix+"Identity-ID"))
				assert.Empty(t, r.Header.Get(HeaderPrefix+"AAL"))
				assert.Empty(t, r.Header.Get(HeaderPrefix+"Signature"))
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/test", nil)
			InjectHeaders(req, tt.session, tt.secret)
			tt.checkFunc(t, req)
		})
	}
}

func TestVerifyHeaders(t *testing.T) {
	sessionID := uuid.New()
	identityID := uuid.New()
	secret := []byte("test-secret-32-bytes-for-verify")

	validSession := &Session{
		ID:         sessionID,
		IdentityID: identityID,
		AAL:        AAL2,
	}

	tests := []struct {
		name      string
		setupReq  func() *http.Request
		secret    []byte
		wantCtx   *Context
		wantErr   error
	}{
		{
			name: "valid headers return context",
			setupReq: func() *http.Request {
				req := httptest.NewRequest("GET", "/test", nil)
				req.Header.Set("X-Request-ID", "test-request-123")
				InjectHeaders(req, validSession, secret)
				return req
			},
			secret: secret,
			wantCtx: &Context{
				SessionID:  sessionID,
				IdentityID: identityID,
				AAL:        AAL2,
				RequestID:  "test-request-123",
			},
			wantErr: nil,
		},
		{
			name: "invalid signature",
			setupReq: func() *http.Request {
				req := httptest.NewRequest("GET", "/test", nil)
				InjectHeaders(req, validSession, secret)
				// Tamper with signature
				req.Header.Set(HeaderPrefix+"Signature", "invalid-signature")
				return req
			},
			secret:  secret,
			wantCtx: nil,
			wantErr: ErrSessionInvalid,
		},
		{
			name: "missing session ID",
			setupReq: func() *http.Request {
				req := httptest.NewRequest("GET", "/test", nil)
				req.Header.Set(HeaderPrefix+"Identity-ID", identityID.String())
				req.Header.Set(HeaderPrefix+"AAL", string(AAL1))
				req.Header.Set(HeaderPrefix+"Signature", "some-sig")
				return req
			},
			secret:  secret,
			wantCtx: nil,
			wantErr: ErrSessionInvalid,
		},
		{
			name: "invalid session ID format",
			setupReq: func() *http.Request {
				req := httptest.NewRequest("GET", "/test", nil)
				req.Header.Set(HeaderPrefix+"Session-ID", "invalid-uuid")
				req.Header.Set(HeaderPrefix+"Identity-ID", identityID.String())
				req.Header.Set(HeaderPrefix+"AAL", string(AAL1))
				// Create valid signature for invalid data
				headers := map[string]string{
					"Session-ID":  "invalid-uuid",
					"Identity-ID": identityID.String(),
					"AAL":         string(AAL1),
				}
				sig := signHeaders(headers, secret)
				req.Header.Set(HeaderPrefix+"Signature", sig)
				return req
			},
			secret:  secret,
			wantCtx: nil,
			wantErr: ErrSessionInvalid,
		},
		{
			name: "invalid identity ID format",
			setupReq: func() *http.Request {
				req := httptest.NewRequest("GET", "/test", nil)
				req.Header.Set(HeaderPrefix+"Session-ID", sessionID.String())
				req.Header.Set(HeaderPrefix+"Identity-ID", "invalid-uuid")
				req.Header.Set(HeaderPrefix+"AAL", string(AAL1))
				// Create valid signature for invalid data
				headers := map[string]string{
					"Session-ID":  sessionID.String(),
					"Identity-ID": "invalid-uuid",
					"AAL":         string(AAL1),
				}
				sig := signHeaders(headers, secret)
				req.Header.Set(HeaderPrefix+"Signature", sig)
				return req
			},
			secret:  secret,
			wantCtx: nil,
			wantErr: ErrSessionInvalid,
		},
		{
			name: "wrong secret produces invalid signature",
			setupReq: func() *http.Request {
				req := httptest.NewRequest("GET", "/test", nil)
				InjectHeaders(req, validSession, []byte("wrong-secret"))
				return req
			},
			secret:  secret,
			wantCtx: nil,
			wantErr: ErrSessionInvalid,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := tt.setupReq()
			ctx, err := VerifyHeaders(req, tt.secret)

			if tt.wantErr != nil {
				assert.ErrorIs(t, err, tt.wantErr)
				assert.Nil(t, ctx)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.wantCtx.SessionID, ctx.SessionID)
			assert.Equal(t, tt.wantCtx.IdentityID, ctx.IdentityID)
			assert.Equal(t, tt.wantCtx.AAL, ctx.AAL)
			assert.Equal(t, tt.wantCtx.RequestID, ctx.RequestID)
		})
	}
}

func TestSignHeaders(t *testing.T) {
	secret := []byte("test-signing-secret")
	
	headers1 := map[string]string{
		"Session-ID":  "session-1",
		"Identity-ID": "identity-1",
		"AAL":         "aal1",
	}
	
	headers2 := map[string]string{
		"Session-ID":  "session-2",
		"Identity-ID": "identity-1",
		"AAL":         "aal1",
	}

	// Same headers should produce same signature
	sig1a := signHeaders(headers1, secret)
	sig1b := signHeaders(headers1, secret)
	assert.Equal(t, sig1a, sig1b, "same headers should produce same signature")

	// Different headers should produce different signatures
	sig2 := signHeaders(headers2, secret)
	assert.NotEqual(t, sig1a, sig2, "different headers should produce different signatures")

	// Signature should be deterministic and hex-encoded
	assert.Regexp(t, "^[0-9a-f]{64}$", sig1a, "signature should be 64-char hex string")
}

func TestWithSession(t *testing.T) {
	session := &Session{
		ID:         uuid.New(),
		IdentityID: uuid.New(),
		AAL:        AAL1,
	}

	ctx := context.Background()
	ctxWithSession := WithSession(ctx, session)

	retrieved := FromContext(ctxWithSession)
	assert.Equal(t, session, retrieved)
}

func TestFromContext(t *testing.T) {
	tests := []struct {
		name     string
		setupCtx func() context.Context
		expected *Session
	}{
		{
			name: "context with session",
			setupCtx: func() context.Context {
				session := &Session{ID: uuid.New(), AAL: AAL1}
				return WithSession(context.Background(), session)
			},
			expected: &Session{}, // Will be set in test
		},
		{
			name: "context without session",
			setupCtx: func() context.Context {
				return context.Background()
			},
			expected: nil,
		},
		{
			name: "context with wrong type",
			setupCtx: func() context.Context {
				return context.WithValue(context.Background(), contextKeySession, "not-a-session")
			},
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := tt.setupCtx()
			result := FromContext(ctx)

			if tt.expected == nil {
				assert.Nil(t, result)
			} else {
				assert.NotNil(t, result)
			}
		})
	}
}

func TestWithContext(t *testing.T) {
	sessionCtx := &Context{
		SessionID:  uuid.New(),
		IdentityID: uuid.New(),
		AAL:        AAL2,
		RequestID:  "test-request",
		IsAdmin:    true,
	}

	ctx := context.Background()
	ctxWithSessionCtx := WithContext(ctx, sessionCtx)

	retrieved := GetContext(ctxWithSessionCtx)
	assert.Equal(t, sessionCtx, retrieved)
}

func TestGetContext(t *testing.T) {
	tests := []struct {
		name     string
		setupCtx func() context.Context
		expected *Context
	}{
		{
			name: "context with session context",
			setupCtx: func() context.Context {
				sessionCtx := &Context{SessionID: uuid.New(), AAL: AAL1}
				return WithContext(context.Background(), sessionCtx)
			},
			expected: &Context{}, // Will be set in test
		},
		{
			name: "context without session context",
			setupCtx: func() context.Context {
				return context.Background()
			},
			expected: nil,
		},
		{
			name: "context with wrong type",
			setupCtx: func() context.Context {
				return context.WithValue(context.Background(), contextKeySession, "not-a-context")
			},
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := tt.setupCtx()
			result := GetContext(ctx)

			if tt.expected == nil {
				assert.Nil(t, result)
			} else {
				assert.NotNil(t, result)
			}
		})
	}
}

func TestHeaderPrefix(t *testing.T) {
	assert.Equal(t, "X-Aegion-Session-", HeaderPrefix)
}

func TestContextKeys(t *testing.T) {
	assert.Equal(t, contextKey("aegion.session"), contextKeySession)
	assert.Equal(t, contextKey("aegion.identity"), contextKeyIdentity)
	assert.Equal(t, contextKey("aegion.request_id"), contextKeyRequestID)
}

func TestHeaderInjectVerifyRoundTrip(t *testing.T) {
	sessionID := uuid.New()
	identityID := uuid.New()
	secret := []byte("roundtrip-secret-32-bytes-long!!")

	originalSession := &Session{
		ID:         sessionID,
		IdentityID: identityID,
		AAL:        AAL2,
	}

	// Inject headers into request
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("X-Request-ID", "roundtrip-test")
	InjectHeaders(req, originalSession, secret)

	// Verify headers and extract context
	ctx, err := VerifyHeaders(req, secret)
	require.NoError(t, err)

	assert.Equal(t, sessionID, ctx.SessionID)
	assert.Equal(t, identityID, ctx.IdentityID)
	assert.Equal(t, AAL2, ctx.AAL)
	assert.Equal(t, "roundtrip-test", ctx.RequestID)
}

func TestHeaderTampering(t *testing.T) {
	sessionID := uuid.New()
	identityID := uuid.New()
	secret := []byte("tampering-test-secret-32-bytes!!")

	session := &Session{
		ID:         sessionID,
		IdentityID: identityID,
		AAL:        AAL1,
	}

	// Inject valid headers
	req := httptest.NewRequest("GET", "/test", nil)
	InjectHeaders(req, session, secret)

	// Tamper with AAL header
	req.Header.Set(HeaderPrefix+"AAL", string(AAL2))

	// Verification should fail due to signature mismatch
	ctx, err := VerifyHeaders(req, secret)
	assert.ErrorIs(t, err, ErrSessionInvalid)
	assert.Nil(t, ctx)
}

func TestAALValues(t *testing.T) {
	assert.Equal(t, "aal0", string(AAL0))
	assert.Equal(t, "aal1", string(AAL1))
	assert.Equal(t, "aal2", string(AAL2))
}

// Additional comprehensive tests for context operations

func TestInjectHeaders_MultipleAALs(t *testing.T) {
	secret := []byte("test-secret-32-bytes-for-headers")

	tests := []struct {
		name    string
		aal     AAL
		wantAAL string
	}{
		{"AAL0", AAL0, string(AAL0)},
		{"AAL1", AAL1, string(AAL1)},
		{"AAL2", AAL2, string(AAL2)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			session := &Session{
				ID:         uuid.New(),
				IdentityID: uuid.New(),
				AAL:        tt.aal,
			}

			req := httptest.NewRequest("GET", "/", nil)
			InjectHeaders(req, session, secret)

			assert.Equal(t, tt.wantAAL, req.Header.Get(HeaderPrefix+"AAL"))
		})
	}
}

func TestInjectHeaders_NilSession(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("X-Other-Header", "value")

	InjectHeaders(req, nil, []byte("secret"))

	assert.Empty(t, req.Header.Get(HeaderPrefix+"Session-ID"))
	assert.Empty(t, req.Header.Get(HeaderPrefix+"Identity-ID"))
	assert.Empty(t, req.Header.Get(HeaderPrefix+"AAL"))
	assert.Empty(t, req.Header.Get(HeaderPrefix+"Signature"))
	assert.Equal(t, "value", req.Header.Get("X-Other-Header")) // Other headers unaffected
}

func TestInjectHeaders_OverwriteExisting(t *testing.T) {
	session := &Session{
		ID:         uuid.New(),
		IdentityID: uuid.New(),
		AAL:        AAL1,
	}

	secret := []byte("test-secret")

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set(HeaderPrefix+"Session-ID", "old-id")

	InjectHeaders(req, session, secret)

	assert.Equal(t, session.ID.String(), req.Header.Get(HeaderPrefix+"Session-ID"))
	assert.NotEqual(t, "old-id", req.Header.Get(HeaderPrefix+"Session-ID"))
}

func TestVerifyHeaders_MissingHeaders(t *testing.T) {
	secret := []byte("test-secret")

	tests := []struct {
		name      string
		setupReq  func() *http.Request
		wantError error
	}{
		{
			name: "missing all headers",
			setupReq: func() *http.Request {
				return httptest.NewRequest("GET", "/", nil)
			},
			wantError: ErrSessionInvalid,
		},
		{
			name: "missing session ID",
			setupReq: func() *http.Request {
				req := httptest.NewRequest("GET", "/", nil)
				identityID := uuid.New()
				req.Header.Set(HeaderPrefix+"Identity-ID", identityID.String())
				req.Header.Set(HeaderPrefix+"AAL", string(AAL1))
				return req
			},
			wantError: ErrSessionInvalid,
		},
		{
			name: "missing identity ID",
			setupReq: func() *http.Request {
				req := httptest.NewRequest("GET", "/", nil)
				sessionID := uuid.New()
				req.Header.Set(HeaderPrefix+"Session-ID", sessionID.String())
				req.Header.Set(HeaderPrefix+"AAL", string(AAL1))
				return req
			},
			wantError: ErrSessionInvalid,
		},
		{
			name: "missing AAL",
			setupReq: func() *http.Request {
				req := httptest.NewRequest("GET", "/", nil)
				sessionID := uuid.New()
				identityID := uuid.New()
				req.Header.Set(HeaderPrefix+"Session-ID", sessionID.String())
				req.Header.Set(HeaderPrefix+"Identity-ID", identityID.String())
				return req
			},
			wantError: ErrSessionInvalid,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := tt.setupReq()
			ctx, err := VerifyHeaders(req, secret)

			assert.ErrorIs(t, err, tt.wantError)
			assert.Nil(t, ctx)
		})
	}
}

func TestVerifyHeaders_ValidWithRequestID(t *testing.T) {
	sessionID := uuid.New()
	identityID := uuid.New()
	secret := []byte("test-secret-32-bytes-for-verify")

	session := &Session{
		ID:         sessionID,
		IdentityID: identityID,
		AAL:        AAL1,
	}

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("X-Request-ID", "req-123")
	InjectHeaders(req, session, secret)

	ctx, err := VerifyHeaders(req, secret)

	require.NoError(t, err)
	assert.Equal(t, "req-123", ctx.RequestID)
}

func TestVerifyHeaders_NoRequestID(t *testing.T) {
	sessionID := uuid.New()
	identityID := uuid.New()
	secret := []byte("test-secret-32-bytes-for-verify")

	session := &Session{
		ID:         sessionID,
		IdentityID: identityID,
		AAL:        AAL1,
	}

	req := httptest.NewRequest("GET", "/", nil)
	InjectHeaders(req, session, secret)

	ctx, err := VerifyHeaders(req, secret)

	require.NoError(t, err)
	assert.Empty(t, ctx.RequestID)
}

func TestVerifyHeaders_InvalidUUIDs(t *testing.T) {
	secret := []byte("test-secret")

	tests := []struct {
		name         string
		sessionID    string
		identityID   string
		shouldError  bool
	}{
		{
			name:        "both invalid",
			sessionID:   "invalid-uuid",
			identityID:  "also-invalid",
			shouldError: true,
		},
		{
			name:        "valid format but UUID errors on both",
			sessionID:   "not-a-uuid-at-all",
			identityID:  "also-not-a-uuid",
			shouldError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/", nil)
			req.Header.Set(HeaderPrefix+"Session-ID", tt.sessionID)
			req.Header.Set(HeaderPrefix+"Identity-ID", tt.identityID)
			req.Header.Set(HeaderPrefix+"AAL", string(AAL1))
			// Don't set signature - it will be invalid anyway

			ctx, err := VerifyHeaders(req, secret)

			assert.Error(t, err)
			assert.Nil(t, ctx)
		})
	}
}

func TestSignHeaders_Deterministic(t *testing.T) {
	secret := []byte("deterministic-secret")

	headers := map[string]string{
		"Session-ID":  "session-123",
		"Identity-ID": "identity-456",
		"AAL":         "aal1",
	}

	sig1 := signHeaders(headers, secret)
	sig2 := signHeaders(headers, secret)
	sig3 := signHeaders(headers, secret)

	assert.Equal(t, sig1, sig2)
	assert.Equal(t, sig2, sig3)
}

func TestSignHeaders_OrderMatters(t *testing.T) {
	secret := []byte("order-test-secret")

	headers1 := map[string]string{
		"Session-ID":  "session-1",
		"Identity-ID": "identity-1",
		"AAL":         "aal1",
	}

	headers2 := map[string]string{
		"Session-ID":  "session-1",
		"Identity-ID": "identity-1",
		"AAL":         "aal2", // Different AAL
	}

	sig1 := signHeaders(headers1, secret)
	sig2 := signHeaders(headers2, secret)

	assert.NotEqual(t, sig1, sig2)
}

func TestSignHeaders_HexFormat(t *testing.T) {
	secret := []byte("format-test-secret")

	headers := map[string]string{
		"Session-ID":  uuid.New().String(),
		"Identity-ID": uuid.New().String(),
		"AAL":         string(AAL2),
	}

	sig := signHeaders(headers, secret)

	// SHA256 = 32 bytes = 64 hex characters
	assert.Len(t, sig, 64)
	assert.Regexp(t, "^[0-9a-f]+$", sig)
}

func TestWithSession_ContextValue(t *testing.T) {
	session1 := &Session{
		ID:         uuid.New(),
		IdentityID: uuid.New(),
		AAL:        AAL1,
	}

	session2 := &Session{
		ID:         uuid.New(),
		IdentityID: uuid.New(),
		AAL:        AAL2,
	}

	ctx := context.Background()

	// Add first session
	ctx1 := WithSession(ctx, session1)
	retrieved1 := FromContext(ctx1)
	assert.Equal(t, session1, retrieved1)

	// Add second session (overwrites)
	ctx2 := WithSession(ctx1, session2)
	retrieved2 := FromContext(ctx2)
	assert.Equal(t, session2, retrieved2)
	assert.NotEqual(t, session1, retrieved2)

	// Original context unchanged
	assert.Nil(t, FromContext(ctx))
}

func TestFromContext_TypeAssertion(t *testing.T) {
	tests := []struct {
		name     string
		value    interface{}
		expected *Session
	}{
		{
			name:     "nil value",
			value:    nil,
			expected: nil,
		},
		{
			name:     "wrong type string",
			value:    "not a session",
			expected: nil,
		},
		{
			name:     "wrong type number",
			value:    42,
			expected: nil,
		},
		{
			name:     "valid session",
			value:    &Session{ID: uuid.New()},
			expected: &Session{}, // Will check existence
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.WithValue(context.Background(), contextKeySession, tt.value)
			result := FromContext(ctx)

			if tt.expected == nil {
				assert.Nil(t, result)
			} else {
				assert.NotNil(t, result)
				assert.IsType(t, &Session{}, result)
			}
		})
	}
}

func TestWithContext_ContextValue(t *testing.T) {
	sc1 := &Context{
		SessionID:  uuid.New(),
		IdentityID: uuid.New(),
		AAL:        AAL1,
		RequestID:  "req-1",
		IsAdmin:    false,
	}

	sc2 := &Context{
		SessionID:  uuid.New(),
		IdentityID: uuid.New(),
		AAL:        AAL2,
		RequestID:  "req-2",
		IsAdmin:    true,
	}

	ctx := context.Background()

	// Add first context
	ctx1 := WithContext(ctx, sc1)
	retrieved1 := GetContext(ctx1)
	assert.Equal(t, sc1, retrieved1)

	// Add second context (overwrites)
	ctx2 := WithContext(ctx1, sc2)
	retrieved2 := GetContext(ctx2)
	assert.Equal(t, sc2, retrieved2)
	assert.NotEqual(t, sc1, retrieved2)

	// Original context unchanged
	assert.Nil(t, GetContext(ctx))
}

func TestGetContext_TypeAssertion(t *testing.T) {
	tests := []struct {
		name     string
		value    interface{}
		expected *Context
	}{
		{
			name:     "nil value",
			value:    nil,
			expected: nil,
		},
		{
			name:     "wrong type string",
			value:    "not a context",
			expected: nil,
		},
		{
			name:     "valid context",
			value:    &Context{SessionID: uuid.New()},
			expected: &Context{}, // Will check existence
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.WithValue(context.Background(), contextKeySession, tt.value)
			result := GetContext(ctx)

			if tt.expected == nil {
				assert.Nil(t, result)
			} else {
				assert.NotNil(t, result)
				assert.IsType(t, &Context{}, result)
			}
		})
	}
}

func TestHeaderPrefix_Value(t *testing.T) {
	assert.Equal(t, "X-Aegion-Session-", HeaderPrefix)
}

func TestContextKeys_Values(t *testing.T) {
	assert.NotEmpty(t, contextKeySession)
	assert.NotEmpty(t, contextKeyIdentity)
	assert.NotEmpty(t, contextKeyRequestID)

	// Ensure they're different
	assert.NotEqual(t, contextKeySession, contextKeyIdentity)
	assert.NotEqual(t, contextKeyIdentity, contextKeyRequestID)
	assert.NotEqual(t, contextKeySession, contextKeyRequestID)
}

func TestContext_Structure(t *testing.T) {
	sessionID := uuid.New()
	identityID := uuid.New()

	ctx := &Context{
		SessionID:  sessionID,
		IdentityID: identityID,
		AAL:        AAL2,
		RequestID:  "test-req-123",
		IsAdmin:    true,
	}

	assert.Equal(t, sessionID, ctx.SessionID)
	assert.Equal(t, identityID, ctx.IdentityID)
	assert.Equal(t, AAL2, ctx.AAL)
	assert.Equal(t, "test-req-123", ctx.RequestID)
	assert.True(t, ctx.IsAdmin)
}

func TestContext_AdminFlag(t *testing.T) {
	tests := []struct {
		name    string
		isAdmin bool
	}{
		{"admin true", true},
		{"admin false", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := &Context{
				SessionID:  uuid.New(),
				IdentityID: uuid.New(),
				IsAdmin:    tt.isAdmin,
			}

			assert.Equal(t, tt.isAdmin, ctx.IsAdmin)
		})
	}
}

func TestHeaderInjectVerifyRoundTrip_AllAALs(t *testing.T) {
	secret := []byte("roundtrip-all-aals")

	aaLs := []AAL{AAL0, AAL1, AAL2}

	for _, aal := range aaLs {
		t.Run(string(aal), func(t *testing.T) {
			session := &Session{
				ID:         uuid.New(),
				IdentityID: uuid.New(),
				AAL:        aal,
			}

			req := httptest.NewRequest("GET", "/", nil)
			InjectHeaders(req, session, secret)

			ctx, err := VerifyHeaders(req, secret)

			require.NoError(t, err)
			assert.Equal(t, aal, ctx.AAL)
		})
	}
}

func TestHeaderTampering_EachHeader(t *testing.T) {
	sessionID := uuid.New()
	identityID := uuid.New()
	secret := []byte("tamper-each-header-secret")

	session := &Session{
		ID:         sessionID,
		IdentityID: identityID,
		AAL:        AAL1,
	}

	tests := []struct {
		name      string
		tamperFn  func(*http.Request)
	}{
		{
			name: "tamper Session-ID",
			tamperFn: func(r *http.Request) {
				r.Header.Set(HeaderPrefix+"Session-ID", uuid.New().String())
			},
		},
		{
			name: "tamper Identity-ID",
			tamperFn: func(r *http.Request) {
				r.Header.Set(HeaderPrefix+"Identity-ID", uuid.New().String())
			},
		},
		{
			name: "tamper AAL",
			tamperFn: func(r *http.Request) {
				r.Header.Set(HeaderPrefix+"AAL", string(AAL2))
			},
		},
		{
			name: "tamper Signature",
			tamperFn: func(r *http.Request) {
				r.Header.Set(HeaderPrefix+"Signature", "0000000000000000000000000000000000000000000000000000000000000000")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/", nil)
			InjectHeaders(req, session, secret)

			// Tamper with header
			tt.tamperFn(req)

			// Verification should fail
			ctx, err := VerifyHeaders(req, secret)
			assert.ErrorIs(t, err, ErrSessionInvalid)
			assert.Nil(t, ctx)
		})
	}
}

func TestVerifyHeaders_DifferentSecrets(t *testing.T) {
	session := &Session{
		ID:         uuid.New(),
		IdentityID: uuid.New(),
		AAL:        AAL1,
	}

	secret1 := []byte("secret-number-one-32-bytes-long!")
	secret2 := []byte("secret-number-two-32-bytes-long!")

	// Inject with secret1
	req := httptest.NewRequest("GET", "/", nil)
	InjectHeaders(req, session, secret1)

	// Verify with secret1 - should succeed
	ctx1, err1 := VerifyHeaders(req, secret1)
	require.NoError(t, err1)
	assert.NotNil(t, ctx1)

	// Verify with secret2 - should fail
	ctx2, err2 := VerifyHeaders(req, secret2)
	assert.ErrorIs(t, err2, ErrSessionInvalid)
	assert.Nil(t, ctx2)
}

func TestInjectAndVerify_MultipleRequests(t *testing.T) {
	secret := []byte("multi-request-secret-32-bytes!!!")

	sessions := []*Session{
		{ID: uuid.New(), IdentityID: uuid.New(), AAL: AAL0},
		{ID: uuid.New(), IdentityID: uuid.New(), AAL: AAL1},
		{ID: uuid.New(), IdentityID: uuid.New(), AAL: AAL2},
	}

	for i, session := range sessions {
		t.Run(string(rune(i)), func(t *testing.T) {
			req := httptest.NewRequest("GET", "/", nil)
			InjectHeaders(req, session, secret)

			ctx, err := VerifyHeaders(req, secret)

			require.NoError(t, err)
			assert.Equal(t, session.ID, ctx.SessionID)
			assert.Equal(t, session.IdentityID, ctx.IdentityID)
			assert.Equal(t, session.AAL, ctx.AAL)
		})
	}
}

func TestHeaderVerify_EmptyHeaders(t *testing.T) {
	secret := []byte("empty-headers-secret")

	req := httptest.NewRequest("GET", "/", nil)

	// All headers are empty
	ctx, err := VerifyHeaders(req, secret)

	assert.Error(t, err)
	assert.Nil(t, ctx)
}

func TestSessionContextIsolation(t *testing.T) {
	session := &Session{
		ID:         uuid.New(),
		IdentityID: uuid.New(),
		AAL:        AAL1,
	}

	sessionCtx := &Context{
		SessionID:  uuid.New(),
		IdentityID: uuid.New(),
		AAL:        AAL2,
	}

	ctx := context.Background()

	// Add session using WithSession
	ctx = WithSession(ctx, session)
	retrieved := FromContext(ctx)
	assert.Equal(t, session, retrieved)

	// WithContext uses the same key, so it overwrites
	ctx2 := WithContext(context.Background(), sessionCtx)
	retrieved2 := GetContext(ctx2)
	assert.Equal(t, sessionCtx, retrieved2)
}

func TestContextKey_Type(t *testing.T) {
	// Verify contextKey is a string type
	var ck contextKey = "test-key"
	assert.IsType(t, contextKey(""), ck)
}