// Package session provides session context injection for module communication.
package session

import (
	"context"
	"net/http"
	"time"

	platformcrypto "github.com/aegion/aegion/internal/platform/crypto"
	"github.com/google/uuid"
)

// HeaderPrefix is the prefix for session context headers.
const HeaderPrefix = "X-Aegion-Session-"

// Context keys for session data.
type contextKey string

const (
	contextKeySession   contextKey = "aegion.session"
	contextKeyIdentity  contextKey = "aegion.identity"
	contextKeyRequestID contextKey = "aegion.request_id"
)

// Context provides session context to handlers.
type Context struct {
	SessionID  uuid.UUID
	IdentityID uuid.UUID
	AAL        AAL
	RequestID  string
	IsAdmin    bool
}

// InjectHeaders adds signed session context headers for module communication.
func InjectHeaders(r *http.Request, session *Session, secret []byte) {
	if session == nil {
		return
	}

	headers := map[string]string{
		"Session-ID":  session.ID.String(),
		"Identity-ID": session.IdentityID.String(),
		"AAL":         string(session.AAL),
	}

	for key, value := range headers {
		fullKey := HeaderPrefix + key
		r.Header.Set(fullKey, value)
	}

	// Add signature header for verification
	sig, err := platformcrypto.SignSessionContextHeaders(secret, headers, time.Now().UTC())
	if err != nil {
		return
	}
	r.Header.Set(HeaderPrefix+"Signature", sig)
}

// VerifyHeaders verifies the signature of session context headers.
func VerifyHeaders(r *http.Request, secret []byte) (*Context, error) {
	headers := map[string]string{
		"Session-ID":  r.Header.Get(HeaderPrefix + "Session-ID"),
		"Identity-ID": r.Header.Get(HeaderPrefix + "Identity-ID"),
		"AAL":         r.Header.Get(HeaderPrefix + "AAL"),
	}

	// Verify signature
	actualSig := r.Header.Get(HeaderPrefix + "Signature")
	if !platformcrypto.VerifySessionContextHeaders(secret, headers, actualSig, time.Now().UTC()) {
		return nil, ErrSessionInvalid
	}

	sessionID, err := uuid.Parse(headers["Session-ID"])
	if err != nil {
		return nil, ErrSessionInvalid
	}

	identityID, err := uuid.Parse(headers["Identity-ID"])
	if err != nil {
		return nil, ErrSessionInvalid
	}

	return &Context{
		SessionID:  sessionID,
		IdentityID: identityID,
		AAL:        AAL(headers["AAL"]),
		RequestID:  r.Header.Get("X-Request-ID"),
	}, nil
}

// signHeaders creates an HMAC signature for session headers.
func signHeaders(headers map[string]string, secret []byte) string {
	sig, err := platformcrypto.SignSessionContextHeaders(secret, headers, time.Unix(0, 0).UTC())
	if err != nil {
		return ""
	}
	return sig
}

// WithSession adds session to context.
func WithSession(ctx context.Context, session *Session) context.Context {
	return context.WithValue(ctx, contextKeySession, session)
}

// FromContext retrieves session from context.
func FromContext(ctx context.Context) *Session {
	if session, ok := ctx.Value(contextKeySession).(*Session); ok {
		return session
	}
	return nil
}

// WithContext adds session context to request context.
func WithContext(ctx context.Context, sc *Context) context.Context {
	return context.WithValue(ctx, contextKeySession, sc)
}

// GetContext retrieves session context from request context.
func GetContext(ctx context.Context) *Context {
	if sc, ok := ctx.Value(contextKeySession).(*Context); ok {
		return sc
	}
	return nil
}
