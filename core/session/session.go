// Package session provides session management for Aegion.
package session

import (
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"strings"
	"time"

	platformcrypto "github.com/aegion/aegion/internal/platform/crypto"
	"github.com/aegion/aegion/internal/platform/secrettoken"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
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

	cleanupExpiredAfter  time.Duration
	cleanupInactiveAfter time.Duration

	execStmt   func(ctx context.Context, sql string, args ...interface{}) (pgconn.CommandTag, error)
	queryRowFn func(ctx context.Context, sql string, args ...interface{}) pgx.Row
	queryRows  func(ctx context.Context, sql string, args ...interface{}) (pgx.Rows, error)
	beginTx    func(ctx context.Context) (sessionTx, error)
	now        func() time.Time
}

var errTokenEntropyFailure = errors.New("failed to generate token entropy")
var errSessionDBUnavailable = errors.New("session manager database unavailable")
var readTokenRandom = func(b []byte) (int, error) {
	if err := platformcrypto.FillRandomBytes(b); err != nil {
		return 0, err
	}
	return len(b), nil
}

const sessionLookupPrefixLength = secrettoken.DefaultLookupPrefixLength

type sessionTx interface {
	Exec(ctx context.Context, sql string, args ...interface{}) (pgconn.CommandTag, error)
	QueryRow(ctx context.Context, sql string, args ...interface{}) pgx.Row
	Commit(ctx context.Context) error
	Rollback(ctx context.Context) error
}

type sessionErrorRow struct {
	err error
}

func (r sessionErrorRow) Scan(dest ...interface{}) error {
	return r.err
}

// ManagerConfig configures the session manager.
type ManagerConfig struct {
	DB           *pgxpool.Pool
	CookieSecret []byte
	CookieConfig CookieConfig
	Lifespan     time.Duration
	IdleTimeout  time.Duration

	// Cleanup configuration
	CleanupExpiredAfter  time.Duration // Delete sessions expired more than this (default: 7 days)
	CleanupInactiveAfter time.Duration // Delete inactive sessions after this (default: 1 day)
}

// NewManager creates a new session manager.
func NewManager(cfg ManagerConfig) *Manager {
	expiredAfter := cfg.CleanupExpiredAfter
	if expiredAfter == 0 {
		expiredAfter = 7 * 24 * time.Hour
	}
	inactiveAfter := cfg.CleanupInactiveAfter
	if inactiveAfter == 0 {
		inactiveAfter = 24 * time.Hour
	}

	m := &Manager{
		db:                   cfg.DB,
		cookieSecret:         cfg.CookieSecret,
		cookieConfig:         cfg.CookieConfig,
		lifespan:             cfg.Lifespan,
		idleTimeout:          cfg.IdleTimeout,
		cleanupExpiredAfter:  expiredAfter,
		cleanupInactiveAfter: inactiveAfter,
		now: func() time.Time {
			return time.Now().UTC()
		},
	}
	if cfg.DB != nil {
		m.execStmt = func(ctx context.Context, sql string, args ...interface{}) (pgconn.CommandTag, error) {
			return cfg.DB.Exec(ctx, sql, args...)
		}
		m.queryRowFn = func(ctx context.Context, sql string, args ...interface{}) pgx.Row {
			return cfg.DB.QueryRow(ctx, sql, args...)
		}
		m.queryRows = func(ctx context.Context, sql string, args ...interface{}) (pgx.Rows, error) {
			return cfg.DB.Query(ctx, sql, args...)
		}
		m.beginTx = func(ctx context.Context) (sessionTx, error) {
			tx, err := cfg.DB.Begin(ctx)
			if err != nil {
				return nil, err
			}
			return tx, nil
		}
	} else {
		m.execStmt = func(context.Context, string, ...interface{}) (pgconn.CommandTag, error) {
			return pgconn.CommandTag{}, errSessionDBUnavailable
		}
		m.queryRowFn = func(context.Context, string, ...interface{}) pgx.Row {
			return sessionErrorRow{err: errSessionDBUnavailable}
		}
		m.queryRows = func(context.Context, string, ...interface{}) (pgx.Rows, error) {
			return nil, errSessionDBUnavailable
		}
		m.beginTx = func(context.Context) (sessionTx, error) {
			return nil, errSessionDBUnavailable
		}
	}
	return m
}

// Create creates a new session for an identity.
func (m *Manager) Create(ctx context.Context, identityID uuid.UUID, method AuthMethod, device DeviceInfo) (*Session, error) {
	now := m.now()
	token, err := m.generateToken()
	if err != nil {
		return nil, err
	}
	logoutToken, err := m.generateToken()
	if err != nil {
		return nil, err
	}

	session := &Session{
		ID:              uuid.New(),
		Token:           token,
		IdentityID:      identityID,
		AAL:             methodToAAL(method),
		IssuedAt:        now,
		ExpiresAt:       now.Add(m.lifespan),
		AuthenticatedAt: now,
		LogoutToken:     logoutToken,
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
	tokenHash := secrettoken.Hash(session.Token)
	tokenPrefix := secrettoken.Prefix(session.Token, sessionLookupPrefixLength)
	logoutTokenHash := secrettoken.Hash(session.LogoutToken)
	logoutTokenPrefix := secrettoken.Prefix(session.LogoutToken, sessionLookupPrefixLength)

	_, err = m.execStmt(ctx, `
		INSERT INTO core_sessions (
			id, token, token_hash, token_prefix, identity_id, aal, issued_at, expires_at,
			authenticated_at, logout_token, logout_token_hash, logout_token_prefix, devices, active,
			is_impersonation, impersonator_id, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18)
	`,
		session.ID, nil, tokenHash, tokenPrefix, session.IdentityID, session.AAL,
		session.IssuedAt, session.ExpiresAt, session.AuthenticatedAt,
		nil, logoutTokenHash, logoutTokenPrefix, session.Devices, session.Active,
		session.IsImpersonation, session.ImpersonatorID,
		session.CreatedAt, session.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}

	// Insert auth method
	_, err = m.execStmt(ctx, `
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
	tokenHash := secrettoken.Hash(token)
	tokenPrefix := secrettoken.Prefix(token, sessionLookupPrefixLength)

	err := m.queryRowFn(ctx, `
		SELECT id, COALESCE(token, ''), identity_id, aal, issued_at, expires_at,
			   authenticated_at, COALESCE(logout_token, ''), devices, active,
			   is_impersonation, impersonator_id, created_at, updated_at
		FROM core_sessions
		WHERE active = TRUE
		  AND token_hash = $1
		  AND token_prefix = $2
		ORDER BY created_at DESC
		LIMIT 1
	`, tokenHash, tokenPrefix).Scan(
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
	session.Token = token

	// Check expiration
	if m.now().After(session.ExpiresAt) {
		return nil, ErrSessionExpired
	}

	// Load auth methods
	rows, err := m.queryRows(ctx, `
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

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return session, nil
}

// Revoke invalidates a session.
func (m *Manager) Revoke(ctx context.Context, sessionID uuid.UUID) error {
	_, err := m.execStmt(ctx, `
		UPDATE core_sessions
		SET active = FALSE, updated_at = NOW()
		WHERE id = $1
	`, sessionID)
	return err
}

// RevokeAllForIdentity revokes all sessions for an identity.
func (m *Manager) RevokeAllForIdentity(ctx context.Context, identityID uuid.UUID) error {
	_, err := m.execStmt(ctx, `
		UPDATE core_sessions
		SET active = FALSE, updated_at = NOW()
		WHERE identity_id = $1 AND active = TRUE
	`, identityID)
	return err
}

// Extend extends a session's expiration time.
func (m *Manager) Extend(ctx context.Context, sessionID uuid.UUID) error {
	newExpiry := m.now().Add(m.lifespan)
	_, err := m.execStmt(ctx, `
		UPDATE core_sessions
		SET expires_at = $1, updated_at = NOW()
		WHERE id = $2 AND active = TRUE
	`, newExpiry, sessionID)
	return err
}

// AddAuthMethod records an additional authentication method.
func (m *Manager) AddAuthMethod(ctx context.Context, sessionID uuid.UUID, method AuthMethod) error {
	now := m.now()
	aalContrib := methodToAAL(method)

	tx, err := m.beginTx(ctx)
	if err != nil {
		return err
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

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

	// #nosec G124 -- Attributes are runtime-configurable and validated for production in config.Validate.
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
	// #nosec G124 -- Attributes are runtime-configurable and validated for production in config.Validate.
	cookie := &http.Cookie{
		Name:     m.cookieConfig.Name,
		Value:    "",
		Path:     m.cookieConfig.Path,
		Domain:   m.cookieConfig.Domain,
		SameSite: m.cookieConfig.SameSite,
		Secure:   m.cookieConfig.Secure,
		MaxAge:   -1,
		Expires:  time.Unix(0, 0),
		HttpOnly: m.cookieConfig.HTTPOnly,
	}
	http.SetCookie(w, cookie)
}

// Cleanup removes expired sessions.
func (m *Manager) Cleanup(ctx context.Context) (int64, error) {
	expiredDays := int(m.cleanupExpiredAfter.Hours() / 24)
	inactiveHours := int(m.cleanupInactiveAfter.Hours())

	sql := `
		DELETE FROM core_sessions
		WHERE expires_at < NOW() - (INTERVAL '1 day' * $1)
		   OR (active = FALSE AND updated_at < NOW() - (INTERVAL '1 hour' * $2))
	`

	result, err := m.execStmt(ctx, sql, expiredDays, inactiveHours)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected(), nil
}

// Helper functions

func (m *Manager) generateToken() (string, error) {
	b := make([]byte, 32)
	if _, err := readTokenRandom(b); err != nil {
		return "", errTokenEntropyFailure
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func (m *Manager) signToken(token string) string {
	signed, err := platformcrypto.SignSessionCookieValue(m.cookieSecret, token, m.now())
	if err != nil {
		return ""
	}
	return signed
}

func (m *Manager) verifySignedToken(signed string) (string, error) {
	token, err := platformcrypto.VerifySessionCookieValue(m.cookieSecret, signed, m.lifespan, m.now())
	if err != nil {
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
