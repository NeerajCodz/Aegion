// Package session provides session management for Aegion.
package session

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrSessionNotFound = errors.New("session not found")
	ErrSessionExpired  = errors.New("session expired")
	ErrSessionInvalid  = errors.New("session invalid")
)

// AAL represents the Authentication Assurance Level.
type AAL string

const (
	AAL0 AAL = "aal0" // Anonymous/unauthenticated
	AAL1 AAL = "aal1" // Single factor (password, magic link)
	AAL2 AAL = "aal2" // Multi-factor (TOTP, WebAuthn, etc.)
)

// AuthMethod represents an authentication method used in a session.
type AuthMethod string

const (
	AuthMethodPassword  AuthMethod = "password"
	AuthMethodTOTP      AuthMethod = "totp"
	AuthMethodWebAuthn  AuthMethod = "webauthn"
	AuthMethodMagicLink AuthMethod = "magic_link"
	AuthMethodSocial    AuthMethod = "social"
	AuthMethodSAML      AuthMethod = "saml"
	AuthMethodPasskey   AuthMethod = "passkey"
	AuthMethodSMS       AuthMethod = "sms"
	AuthMethodBackup    AuthMethod = "backup_code"
)

// Session represents an authenticated session.
type Session struct {
	ID              uuid.UUID           `json:"id"`
	Token           string              `json:"-"`
	IdentityID      uuid.UUID           `json:"identity_id"`
	AAL             AAL                 `json:"aal"`
	IssuedAt        time.Time           `json:"issued_at"`
	ExpiresAt       time.Time           `json:"expires_at"`
	AuthenticatedAt time.Time           `json:"authenticated_at"`
	LogoutToken     string              `json:"-"`
	Devices         []DeviceInfo        `json:"devices"`
	Active          bool                `json:"active"`
	IsImpersonation bool                `json:"is_impersonation,omitempty"`
	ImpersonatorID  *uuid.UUID          `json:"impersonator_id,omitempty"`
	AuthMethods     []SessionAuthMethod `json:"authentication_methods"`
	CreatedAt       time.Time           `json:"created_at"`
	UpdatedAt       time.Time           `json:"updated_at"`
}

// SessionAuthMethod records an authentication method used.
type SessionAuthMethod struct {
	Method      AuthMethod `json:"method"`
	AALContrib  AAL        `json:"aal_contributed"`
	CompletedAt time.Time  `json:"completed_at"`
}

// DeviceInfo contains device fingerprint information.
type DeviceInfo struct {
	UserAgent string `json:"user_agent"`
	IPAddress string `json:"ip_address"`
	Location  string `json:"location,omitempty"`
}

// CookieConfig holds cookie settings.
type CookieConfig struct {
	Name     string
	Path     string
	Domain   string
	SameSite http.SameSite
	Secure   bool
	HTTPOnly bool
}

// Manager handles session operations.
type Manager struct {
	db           *pgxpool.Pool
	cookieSecret []byte
	cookieConfig CookieConfig
	lifespan     time.Duration
	idleTimeout  time.Duration
}

// ManagerConfig configures the session manager.
type ManagerConfig struct {
	DB           *pgxpool.Pool
	CookieSecret []byte
	CookieConfig CookieConfig
	Lifespan     time.Duration
	IdleTimeout  time.Duration
}

// NewManager creates a new session manager.
func NewManager(cfg ManagerConfig) *Manager {
	return &Manager{
		db:           cfg.DB,
		cookieSecret: cfg.CookieSecret,
		cookieConfig: cfg.CookieConfig,
		lifespan:     cfg.Lifespan,
		idleTimeout:  cfg.IdleTimeout,
	}
}

// Create creates a new session for an identity.
func (m *Manager) Create(ctx context.Context, identityID uuid.UUID, method AuthMethod, device DeviceInfo) (*Session, error) {
	now := time.Now().UTC()

	session := &Session{
		ID:              uuid.New(),
		Token:           m.generateToken(),
		IdentityID:      identityID,
		AAL:             methodToAAL(method),
		IssuedAt:        now,
		ExpiresAt:       now.Add(m.lifespan),
		AuthenticatedAt: now,
		LogoutToken:     m.generateToken(),
		Devices:         []DeviceInfo{device},
		Active:          true,
		AuthMethods: []SessionAuthMethod{
			{
				Method:      method,
				AALContrib:  methodToAAL(method),
				CompletedAt: now,
			},
		},
		CreatedAt: now,
		UpdatedAt: now,
	}

	// Insert session
	_, err := m.db.Exec(ctx, `
		INSERT INTO core_sessions (
			id, token, identity_id, aal, issued_at, expires_at,
			authenticated_at, logout_token, devices, active,
			is_impersonation, impersonator_id, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
	`,
		session.ID, session.Token, session.IdentityID, session.AAL,
		session.IssuedAt, session.ExpiresAt, session.AuthenticatedAt,
		session.LogoutToken, session.Devices, session.Active,
		session.IsImpersonation, session.ImpersonatorID,
		session.CreatedAt, session.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}

	// Insert auth method
	_, err = m.db.Exec(ctx, `
		INSERT INTO core_session_auth_methods (session_id, method, aal_contributed, completed_at)
		VALUES ($1, $2, $3, $4)
	`, session.ID, method, methodToAAL(method), now)
	if err != nil {
		return nil, err
	}

	return session, nil
}

// Get retrieves a session by token.
func (m *Manager) Get(ctx context.Context, token string) (*Session, error) {
	session := &Session{}

	err := m.db.QueryRow(ctx, `
		SELECT id, token, identity_id, aal, issued_at, expires_at,
			   authenticated_at, logout_token, devices, active,
			   is_impersonation, impersonator_id, created_at, updated_at
		FROM core_sessions
		WHERE token = $1 AND active = TRUE
	`, token).Scan(
		&session.ID, &session.Token, &session.IdentityID, &session.AAL,
		&session.IssuedAt, &session.ExpiresAt, &session.AuthenticatedAt,
		&session.LogoutToken, &session.Devices, &session.Active,
		&session.IsImpersonation, &session.ImpersonatorID,
		&session.CreatedAt, &session.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrSessionNotFound
		}
		return nil, err
	}

	// Check expiration
	if time.Now().UTC().After(session.ExpiresAt) {
		return nil, ErrSessionExpired
	}

	// Load auth methods
	rows, err := m.db.Query(ctx, `
		SELECT method, aal_contributed, completed_at
		FROM core_session_auth_methods
		WHERE session_id = $1
		ORDER BY completed_at
	`, session.ID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var am SessionAuthMethod
		if err := rows.Scan(&am.Method, &am.AALContrib, &am.CompletedAt); err != nil {
			return nil, err
		}
		session.AuthMethods = append(session.AuthMethods, am)
	}

	return session, nil
}

// Revoke invalidates a session.
func (m *Manager) Revoke(ctx context.Context, sessionID uuid.UUID) error {
	_, err := m.db.Exec(ctx, `
		UPDATE core_sessions
		SET active = FALSE, updated_at = NOW()
		WHERE id = $1
	`, sessionID)
	return err
}

// RevokeAllForIdentity revokes all sessions for an identity.
func (m *Manager) RevokeAllForIdentity(ctx context.Context, identityID uuid.UUID) error {
	_, err := m.db.Exec(ctx, `
		UPDATE core_sessions
		SET active = FALSE, updated_at = NOW()
		WHERE identity_id = $1 AND active = TRUE
	`, identityID)
	return err
}

// Extend extends a session's expiration time.
func (m *Manager) Extend(ctx context.Context, sessionID uuid.UUID) error {
	newExpiry := time.Now().UTC().Add(m.lifespan)
	_, err := m.db.Exec(ctx, `
		UPDATE core_sessions
		SET expires_at = $1, updated_at = NOW()
		WHERE id = $2 AND active = TRUE
	`, newExpiry, sessionID)
	return err
}

// AddAuthMethod records an additional authentication method.
func (m *Manager) AddAuthMethod(ctx context.Context, sessionID uuid.UUID, method AuthMethod) error {
	now := time.Now().UTC()
	aalContrib := methodToAAL(method)

	tx, err := m.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	// Insert auth method
	_, err = tx.Exec(ctx, `
		INSERT INTO core_session_auth_methods (session_id, method, aal_contributed, completed_at)
		VALUES ($1, $2, $3, $4)
	`, sessionID, method, aalContrib, now)
	if err != nil {
		return err
	}

	// Update session AAL if this method contributes higher
	var currentAAL AAL
	err = tx.QueryRow(ctx, "SELECT aal FROM core_sessions WHERE id = $1", sessionID).Scan(&currentAAL)
	if err != nil {
		return err
	}

	newAAL := computeAAL(currentAAL, aalContrib)
	if newAAL != currentAAL {
		_, err = tx.Exec(ctx, `
			UPDATE core_sessions
			SET aal = $1, authenticated_at = $2, updated_at = NOW()
			WHERE id = $3
		`, newAAL, now, sessionID)
		if err != nil {
			return err
		}
	}

	return tx.Commit(ctx)
}

// GetFromRequest extracts and validates a session from an HTTP request.
func (m *Manager) GetFromRequest(ctx context.Context, r *http.Request) (*Session, error) {
	// Try cookie first
	cookie, err := r.Cookie(m.cookieConfig.Name)
	if err == nil && cookie.Value != "" {
		token, err := m.verifySignedToken(cookie.Value)
		if err == nil {
			return m.Get(ctx, token)
		}
	}

	// Try Authorization header
	auth := r.Header.Get("Authorization")
	if strings.HasPrefix(auth, "Bearer ") {
		token := strings.TrimPrefix(auth, "Bearer ")
		return m.Get(ctx, token)
	}

	// Try X-Session-Token header
	if token := r.Header.Get("X-Session-Token"); token != "" {
		return m.Get(ctx, token)
	}

	return nil, ErrSessionNotFound
}

// SetCookie sets the session cookie on a response.
func (m *Manager) SetCookie(w http.ResponseWriter, session *Session) {
	signedToken := m.signToken(session.Token)

	cookie := &http.Cookie{
		Name:     m.cookieConfig.Name,
		Value:    signedToken,
		Path:     m.cookieConfig.Path,
		Domain:   m.cookieConfig.Domain,
		SameSite: m.cookieConfig.SameSite,
		Secure:   m.cookieConfig.Secure,
		HttpOnly: m.cookieConfig.HTTPOnly,
		Expires:  session.ExpiresAt,
	}
	http.SetCookie(w, cookie)
}

// ClearCookie removes the session cookie.
func (m *Manager) ClearCookie(w http.ResponseWriter) {
	cookie := &http.Cookie{
		Name:     m.cookieConfig.Name,
		Value:    "",
		Path:     m.cookieConfig.Path,
		Domain:   m.cookieConfig.Domain,
		MaxAge:   -1,
		Expires:  time.Unix(0, 0),
		HttpOnly: m.cookieConfig.HTTPOnly,
	}
	http.SetCookie(w, cookie)
}

// Cleanup removes expired sessions.
func (m *Manager) Cleanup(ctx context.Context) (int64, error) {
	result, err := m.db.Exec(ctx, `
		DELETE FROM core_sessions
		WHERE expires_at < NOW() - INTERVAL '7 days'
		   OR (active = FALSE AND updated_at < NOW() - INTERVAL '1 day')
	`)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected(), nil
}

// Helper functions

func (m *Manager) generateToken() string {
	b := make([]byte, 32)
	rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

func (m *Manager) signToken(token string) string {
	mac := hmac.New(sha256.New, m.cookieSecret)
	mac.Write([]byte(token))
	sig := hex.EncodeToString(mac.Sum(nil))
	return token + "." + sig
}

func (m *Manager) verifySignedToken(signed string) (string, error) {
	parts := strings.SplitN(signed, ".", 2)
	if len(parts) != 2 {
		return "", ErrSessionInvalid
	}

	token, sig := parts[0], parts[1]

	mac := hmac.New(sha256.New, m.cookieSecret)
	mac.Write([]byte(token))
	expected := hex.EncodeToString(mac.Sum(nil))

	if !hmac.Equal([]byte(sig), []byte(expected)) {
		return "", ErrSessionInvalid
	}

	return token, nil
}

func methodToAAL(method AuthMethod) AAL {
	switch method {
	case AuthMethodPassword, AuthMethodMagicLink, AuthMethodSocial, AuthMethodSAML, AuthMethodPasskey:
		return AAL1
	case AuthMethodTOTP, AuthMethodWebAuthn, AuthMethodSMS, AuthMethodBackup:
		return AAL2
	default:
		return AAL0
	}
}

func computeAAL(current, contrib AAL) AAL {
	// AAL2 requires a combination of factors
	if current == AAL1 && contrib == AAL2 {
		return AAL2
	}
	if current == AAL0 || current == "" {
		return contrib
	}
	return current
}
