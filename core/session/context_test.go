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