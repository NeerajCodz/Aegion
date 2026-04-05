package store

import (
	"context"
	"time"
)

// ConsentSession represents a remembered consent.
type ConsentSession struct {
	ID                string            `json:"id"`
	ClientID          string            `json:"client_id"`
	IdentityID        string            `json:"identity_id"`
	Scopes            []string          `json:"scopes"`
	Audience          []string          `json:"audience,omitempty"`
	Remember          bool              `json:"remember"`
	RememberFor       *int              `json:"remember_for,omitempty"`
	AccessTokenClaims map[string]any    `json:"access_token_claims,omitempty"`
	IDTokenClaims     map[string]any    `json:"id_token_claims,omitempty"`
	Handled           bool              `json:"handled"`
	GrantedAt         *time.Time        `json:"granted_at,omitempty"`
	ExpiresAt         *time.Time        `json:"expires_at,omitempty"`
	CreatedAt         time.Time         `json:"created_at"`
	UpdatedAt         time.Time         `json:"updated_at"`
}

// LoginChallenge represents a pending login during OAuth flow.
type LoginChallenge struct {
	ID                    string     `json:"id"`
	ClientID              string     `json:"client_id"`
	RequestURL            string     `json:"request_url"`
	RedirectURI           string     `json:"redirect_uri"`
	Scopes                []string   `json:"scopes"`
	Audience              []string   `json:"audience,omitempty"`
	ACRValues             []string   `json:"acr_values,omitempty"`
	State                 *string    `json:"state,omitempty"`
	CodeChallenge         *string    `json:"code_challenge,omitempty"`
	CodeChallengeMethod   *string    `json:"code_challenge_method,omitempty"`
	Nonce                 *string    `json:"nonce,omitempty"`
	Skip                  bool       `json:"skip"`
	IdentityID            *string    `json:"identity_id,omitempty"`
	SessionID             *string    `json:"session_id,omitempty"`
	AuthenticatedAt       *time.Time `json:"authenticated_at,omitempty"`
	ExpiresAt             time.Time  `json:"expires_at"`
	CreatedAt             time.Time  `json:"created_at"`
}

// ConsentChallenge represents a pending consent decision.
type ConsentChallenge struct {
	ID                  string            `json:"id"`
	LoginChallengeID    string            `json:"login_challenge_id"`
	ClientID            string            `json:"client_id"`
	IdentityID          string            `json:"identity_id"`
	SessionID           string            `json:"session_id"`
	RequestURL          string            `json:"request_url"`
	RedirectURI         string            `json:"redirect_uri"`
	RequestedScopes     []string          `json:"requested_scopes"`
	RequestedAudience   []string          `json:"requested_audience,omitempty"`
	Skip                bool              `json:"skip"`
	GrantedScopes       []string          `json:"granted_scopes,omitempty"`
	GrantedAudience     []string          `json:"granted_audience,omitempty"`
	Remember            *bool             `json:"remember,omitempty"`
	RememberFor         *int              `json:"remember_for,omitempty"`
	AccessTokenClaims   map[string]any    `json:"access_token_claims,omitempty"`
	IDTokenClaims       map[string]any    `json:"id_token_claims,omitempty"`
	Handled             bool              `json:"handled"`
	HandledAt           *time.Time        `json:"handled_at,omitempty"`
	Rejected            bool              `json:"rejected"`
	Error               *string           `json:"error,omitempty"`
	ErrorDescription    *string           `json:"error_description,omitempty"`
	ExpiresAt           time.Time         `json:"expires_at"`
	CreatedAt           time.Time         `json:"created_at"`
}

// CreateConsentSession creates or updates a consent session.
func (s *Store) CreateConsentSession(ctx context.Context, consent *ConsentSession) error {
	now := nowUTC()
	consent.CreatedAt = now
	consent.UpdatedAt = now

	_, err := s.db.Exec(ctx, `
		INSERT INTO oa2_consent_sessions (
			id, client_id, identity_id, scopes, audience, remember, remember_for,
			access_token_claims, id_token_claims, handled, granted_at, expires_at,
			created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
		ON CONFLICT (client_id, identity_id) DO UPDATE SET
			scopes = EXCLUDED.scopes,
			audience = EXCLUDED.audience,
			remember = EXCLUDED.remember,
			remember_for = EXCLUDED.remember_for,
			access_token_claims = EXCLUDED.access_token_claims,
			id_token_claims = EXCLUDED.id_token_claims,
			granted_at = EXCLUDED.granted_at,
			expires_at = EXCLUDED.expires_at,
			updated_at = EXCLUDED.updated_at
	`,
		consent.ID, consent.ClientID, consent.IdentityID, consent.Scopes,
		consent.Audience, consent.Remember, consent.RememberFor,
		consent.AccessTokenClaims, consent.IDTokenClaims, consent.Handled,
		consent.GrantedAt, consent.ExpiresAt, consent.CreatedAt, consent.UpdatedAt,
	)

	return err
}

// GetConsentSession retrieves a consent session.
func (s *Store) GetConsentSession(ctx context.Context, clientID, identityID string) (*ConsentSession, error) {
	consent := &ConsentSession{}

	err := s.db.QueryRow(ctx, `
		SELECT id, client_id, identity_id, scopes, audience, remember, remember_for,
			access_token_claims, id_token_claims, handled, granted_at, expires_at,
			created_at, updated_at
		FROM oa2_consent_sessions WHERE client_id = $1 AND identity_id = $2
	`, clientID, identityID).Scan(
		&consent.ID, &consent.ClientID, &consent.IdentityID, &consent.Scopes,
		&consent.Audience, &consent.Remember, &consent.RememberFor,
		&consent.AccessTokenClaims, &consent.IDTokenClaims, &consent.Handled,
		&consent.GrantedAt, &consent.ExpiresAt, &consent.CreatedAt, &consent.UpdatedAt,
	)

	if err != nil {
		if err.Error() == "no rows in result set" {
			return nil, ErrNotFound
		}
		return nil, err
	}

	return consent, nil
}

// DeleteConsentSession deletes a consent session.
func (s *Store) DeleteConsentSession(ctx context.Context, clientID, identityID string) error {
	_, err := s.db.Exec(ctx, `
		DELETE FROM oa2_consent_sessions WHERE client_id = $1 AND identity_id = $2
	`, clientID, identityID)
	return err
}

// CreateLoginChallenge creates a new login challenge.
func (s *Store) CreateLoginChallenge(ctx context.Context, challenge *LoginChallenge) error {
	if challenge.ID == "" {
		challenge.ID = GenerateLoginChallenge()
	}
	challenge.CreatedAt = nowUTC()

	_, err := s.db.Exec(ctx, `
		INSERT INTO oa2_login_challenges (
			id, client_id, request_url, redirect_uri, scopes, audience, acr_values,
			state, code_challenge, code_challenge_method, nonce, skip, identity_id,
			session_id, authenticated_at, expires_at, created_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17)
	`,
		challenge.ID, challenge.ClientID, challenge.RequestURL, challenge.RedirectURI,
		challenge.Scopes, challenge.Audience, challenge.ACRValues, challenge.State,
		challenge.CodeChallenge, challenge.CodeChallengeMethod, challenge.Nonce,
		challenge.Skip, challenge.IdentityID, challenge.SessionID,
		challenge.AuthenticatedAt, challenge.ExpiresAt, challenge.CreatedAt,
	)

	return err
}

// GetLoginChallenge retrieves a login challenge.
func (s *Store) GetLoginChallenge(ctx context.Context, id string) (*LoginChallenge, error) {
	challenge := &LoginChallenge{}

	err := s.db.QueryRow(ctx, `
		SELECT id, client_id, request_url, redirect_uri, scopes, audience, acr_values,
			state, code_challenge, code_challenge_method, nonce, skip, identity_id,
			session_id, authenticated_at, expires_at, created_at
		FROM oa2_login_challenges WHERE id = $1
	`, id).Scan(
		&challenge.ID, &challenge.ClientID, &challenge.RequestURL, &challenge.RedirectURI,
		&challenge.Scopes, &challenge.Audience, &challenge.ACRValues, &challenge.State,
		&challenge.CodeChallenge, &challenge.CodeChallengeMethod, &challenge.Nonce,
		&challenge.Skip, &challenge.IdentityID, &challenge.SessionID,
		&challenge.AuthenticatedAt, &challenge.ExpiresAt, &challenge.CreatedAt,
	)

	if err != nil {
		if err.Error() == "no rows in result set" {
			return nil, ErrNotFound
		}
		return nil, err
	}

	return challenge, nil
}

// AcceptLoginChallenge marks a login challenge as authenticated.
func (s *Store) AcceptLoginChallenge(ctx context.Context, id, identityID, sessionID string) error {
	now := nowUTC()
	result, err := s.db.Exec(ctx, `
		UPDATE oa2_login_challenges SET
			identity_id = $2,
			session_id = $3,
			authenticated_at = $4
		WHERE id = $1
	`, id, identityID, sessionID, now)

	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// CreateConsentChallenge creates a new consent challenge.
func (s *Store) CreateConsentChallenge(ctx context.Context, challenge *ConsentChallenge) error {
	if challenge.ID == "" {
		challenge.ID = GenerateConsentChallenge()
	}
	challenge.CreatedAt = nowUTC()

	_, err := s.db.Exec(ctx, `
		INSERT INTO oa2_consent_challenges (
			id, login_challenge_id, client_id, identity_id, session_id, request_url,
			redirect_uri, requested_scopes, requested_audience, skip, granted_scopes,
			granted_audience, remember, remember_for, access_token_claims, id_token_claims,
			handled, handled_at, rejected, error, error_description, expires_at, created_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21, $22, $23)
	`,
		challenge.ID, challenge.LoginChallengeID, challenge.ClientID, challenge.IdentityID,
		challenge.SessionID, challenge.RequestURL, challenge.RedirectURI,
		challenge.RequestedScopes, challenge.RequestedAudience, challenge.Skip,
		challenge.GrantedScopes, challenge.GrantedAudience, challenge.Remember,
		challenge.RememberFor, challenge.AccessTokenClaims, challenge.IDTokenClaims,
		challenge.Handled, challenge.HandledAt, challenge.Rejected, challenge.Error,
		challenge.ErrorDescription, challenge.ExpiresAt, challenge.CreatedAt,
	)

	return err
}

// GetConsentChallenge retrieves a consent challenge.
func (s *Store) GetConsentChallenge(ctx context.Context, id string) (*ConsentChallenge, error) {
	challenge := &ConsentChallenge{}

	err := s.db.QueryRow(ctx, `
		SELECT id, login_challenge_id, client_id, identity_id, session_id, request_url,
			redirect_uri, requested_scopes, requested_audience, skip, granted_scopes,
			granted_audience, remember, remember_for, access_token_claims, id_token_claims,
			handled, handled_at, rejected, error, error_description, expires_at, created_at
		FROM oa2_consent_challenges WHERE id = $1
	`, id).Scan(
		&challenge.ID, &challenge.LoginChallengeID, &challenge.ClientID, &challenge.IdentityID,
		&challenge.SessionID, &challenge.RequestURL, &challenge.RedirectURI,
		&challenge.RequestedScopes, &challenge.RequestedAudience, &challenge.Skip,
		&challenge.GrantedScopes, &challenge.GrantedAudience, &challenge.Remember,
		&challenge.RememberFor, &challenge.AccessTokenClaims, &challenge.IDTokenClaims,
		&challenge.Handled, &challenge.HandledAt, &challenge.Rejected, &challenge.Error,
		&challenge.ErrorDescription, &challenge.ExpiresAt, &challenge.CreatedAt,
	)

	if err != nil {
		if err.Error() == "no rows in result set" {
			return nil, ErrNotFound
		}
		return nil, err
	}

	return challenge, nil
}

// AcceptConsentChallenge marks a consent challenge as granted.
func (s *Store) AcceptConsentChallenge(ctx context.Context, id string, grantedScopes, grantedAudience []string, remember bool, rememberFor *int) error {
	now := nowUTC()
	result, err := s.db.Exec(ctx, `
		UPDATE oa2_consent_challenges SET
			granted_scopes = $2,
			granted_audience = $3,
			remember = $4,
			remember_for = $5,
			handled = true,
			handled_at = $6
		WHERE id = $1
	`, id, grantedScopes, grantedAudience, remember, rememberFor, now)

	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// RejectConsentChallenge marks a consent challenge as rejected.
func (s *Store) RejectConsentChallenge(ctx context.Context, id, errorCode, errorDesc string) error {
	now := nowUTC()
	result, err := s.db.Exec(ctx, `
		UPDATE oa2_consent_challenges SET
			rejected = true,
			error = $2,
			error_description = $3,
			handled = true,
			handled_at = $4
		WHERE id = $1
	`, id, errorCode, errorDesc, now)

	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// CleanupExpiredChallenges removes expired challenges.
func (s *Store) CleanupExpiredChallenges(ctx context.Context) (int64, error) {
	var total int64

	result, err := s.db.Exec(ctx, "DELETE FROM oa2_login_challenges WHERE expires_at < NOW()")
	if err != nil {
		return 0, err
	}
	total += result.RowsAffected()

	result, err = s.db.Exec(ctx, "DELETE FROM oa2_consent_challenges WHERE expires_at < NOW()")
	if err != nil {
		return total, err
	}
	total += result.RowsAffected()

	return total, nil
}
