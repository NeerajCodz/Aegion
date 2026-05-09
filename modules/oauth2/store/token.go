package store

import (
	"context"
	"encoding/json"
	"time"
)

// AccessToken represents an OAuth2 access token.
type AccessToken struct {
	JTI         string         `json:"jti"`
	Signature   *string        `json:"-"` // for opaque tokens
	ClientID    string         `json:"client_id"`
	IdentityID  string         `json:"identity_id"`
	SessionID   string         `json:"session_id"`
	Scopes      []string       `json:"scopes"`
	Audience    []string       `json:"audience"`
	Issuer      string         `json:"issuer"`
	Subject     string         `json:"subject"`
	ExtraClaims map[string]any `json:"extra_claims,omitempty"`
	Revoked     bool           `json:"revoked"`
	RevokedAt   *time.Time     `json:"revoked_at,omitempty"`
	ExpiresAt   time.Time      `json:"expires_at"`
	CreatedAt   time.Time      `json:"created_at"`
}

// RefreshToken represents an OAuth2 refresh token.
type RefreshToken struct {
	ID                   string         `json:"id"`
	FamilyID             string         `json:"family_id"`
	ClientID             string         `json:"client_id"`
	IdentityID           string         `json:"identity_id"`
	SessionID            string         `json:"session_id"`
	Scopes               []string       `json:"scopes"`
	Audience             []string       `json:"audience,omitempty"`
	Active               bool           `json:"active"`
	Used                 bool           `json:"used"`
	UsedAt               *time.Time     `json:"used_at,omitempty"`
	SuccessorID          *string        `json:"successor_id,omitempty"`
	GracePeriodExpiresAt *time.Time     `json:"grace_period_expires_at,omitempty"`
	FirstUsedAt          *time.Time     `json:"first_used_at,omitempty"`
	AccessTokenJTI       *string        `json:"access_token_jti,omitempty"`
	ExtraClaims          map[string]any `json:"extra_claims,omitempty"`
	ExpiresAt            time.Time      `json:"expires_at"`
	CreatedAt            time.Time      `json:"created_at"`
}

// IDToken represents an OIDC ID token.
type IDToken struct {
	JTI         string         `json:"jti"`
	ClientID    string         `json:"client_id"`
	IdentityID  string         `json:"identity_id"`
	SessionID   string         `json:"session_id"`
	Nonce       *string        `json:"nonce,omitempty"`
	ATHash      *string        `json:"at_hash,omitempty"`
	CHash       *string        `json:"c_hash,omitempty"`
	ACR         string         `json:"acr"`
	AMR         []string       `json:"amr"`
	AuthTime    time.Time      `json:"auth_time"`
	ExtraClaims map[string]any `json:"extra_claims,omitempty"`
	Revoked     bool           `json:"revoked"`
	ExpiresAt   time.Time      `json:"expires_at"`
	CreatedAt   time.Time      `json:"created_at"`
}

// CreateAccessToken stores a new access token.
func (s *Store) CreateAccessToken(ctx context.Context, token *AccessToken) error {
	if token.JTI == "" {
		var err error
		token.JTI, err = GenerateAccessTokenJTI()
		if err != nil {
			return err
		}
	}
	token.CreatedAt = nowUTC()

	claims, _ := json.Marshal(token.ExtraClaims)

	_, err := s.db.Exec(ctx, `
		INSERT INTO oa2_access_tokens (
			jti, signature, client_id, identity_id, session_id, scopes, audience,
			issuer, subject, extra_claims, revoked, expires_at, created_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
	`,
		token.JTI, token.Signature, token.ClientID, token.IdentityID, token.SessionID,
		token.Scopes, token.Audience, token.Issuer, token.Subject, claims,
		token.Revoked, token.ExpiresAt, token.CreatedAt,
	)

	return err
}

// GetAccessToken retrieves an access token by JTI.
func (s *Store) GetAccessToken(ctx context.Context, jti string) (*AccessToken, error) {
	token := &AccessToken{}
	var claims []byte

	err := s.db.QueryRow(ctx, `
		SELECT jti, signature, client_id, identity_id, session_id, scopes, audience,
			issuer, subject, extra_claims, revoked, revoked_at, expires_at, created_at
		FROM oa2_access_tokens WHERE jti = $1
	`, jti).Scan(
		&token.JTI, &token.Signature, &token.ClientID, &token.IdentityID, &token.SessionID,
		&token.Scopes, &token.Audience, &token.Issuer, &token.Subject, &claims,
		&token.Revoked, &token.RevokedAt, &token.ExpiresAt, &token.CreatedAt,
	)

	if err != nil {
		if err.Error() == "no rows in result set" {
			return nil, ErrNotFound
		}
		return nil, err
	}

	if len(claims) > 0 {
		_ = json.Unmarshal(claims, &token.ExtraClaims)
	}

	return token, nil
}

// GetAccessTokenBySignature retrieves an access token by token signature fingerprint.
func (s *Store) GetAccessTokenBySignature(ctx context.Context, signature string) (*AccessToken, error) {
	token := &AccessToken{}
	var claims []byte

	err := s.db.QueryRow(ctx, `
		SELECT jti, signature, client_id, identity_id, session_id, scopes, audience,
			issuer, subject, extra_claims, revoked, revoked_at, expires_at, created_at
		FROM oa2_access_tokens WHERE signature = $1
	`, signature).Scan(
		&token.JTI, &token.Signature, &token.ClientID, &token.IdentityID, &token.SessionID,
		&token.Scopes, &token.Audience, &token.Issuer, &token.Subject, &claims,
		&token.Revoked, &token.RevokedAt, &token.ExpiresAt, &token.CreatedAt,
	)

	if err != nil {
		if err.Error() == "no rows in result set" {
			return nil, ErrNotFound
		}
		return nil, err
	}

	if len(claims) > 0 {
		_ = json.Unmarshal(claims, &token.ExtraClaims)
	}

	return token, nil
}

// RevokeAccessToken marks an access token as revoked.
func (s *Store) RevokeAccessToken(ctx context.Context, jti string) error {
	now := nowUTC()
	result, err := s.db.Exec(ctx, `
		UPDATE oa2_access_tokens SET revoked = true, revoked_at = $2 WHERE jti = $1
	`, jti, now)

	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// RevokeAccessTokensBySession revokes all access tokens for a session.
func (s *Store) RevokeAccessTokensBySession(ctx context.Context, sessionID string) (int64, error) {
	now := nowUTC()
	result, err := s.db.Exec(ctx, `
		UPDATE oa2_access_tokens SET revoked = true, revoked_at = $2
		WHERE session_id = $1 AND revoked = false
	`, sessionID, now)

	if err != nil {
		return 0, err
	}
	return result.RowsAffected(), nil
}

// CreateRefreshToken stores a new refresh token.
func (s *Store) CreateRefreshToken(ctx context.Context, token *RefreshToken) error {
	if token.ID == "" {
		var err error
		token.ID, err = GenerateRefreshToken()
		if err != nil {
			return err
		}
	}
	if token.FamilyID == "" {
		var err error
		token.FamilyID, err = GenerateRefreshTokenFamily()
		if err != nil {
			return err
		}
	}
	token.CreatedAt = nowUTC()

	claims, _ := json.Marshal(token.ExtraClaims)

	_, err := s.db.Exec(ctx, `
		INSERT INTO oa2_refresh_tokens (
			id, family_id, client_id, identity_id, session_id, scopes, audience,
			active, used, successor_id, grace_period_expires_at, access_token_jti,
			extra_claims, expires_at, created_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
	`,
		token.ID, token.FamilyID, token.ClientID, token.IdentityID, token.SessionID,
		token.Scopes, token.Audience, token.Active, token.Used, token.SuccessorID,
		token.GracePeriodExpiresAt, token.AccessTokenJTI, claims,
		token.ExpiresAt, token.CreatedAt,
	)

	return err
}

// GetRefreshToken retrieves a refresh token by ID.
func (s *Store) GetRefreshToken(ctx context.Context, id string) (*RefreshToken, error) {
	token := &RefreshToken{}
	var claims []byte

	err := s.db.QueryRow(ctx, `
		SELECT id, family_id, client_id, identity_id, session_id, scopes, audience,
			active, used, used_at, successor_id, grace_period_expires_at, first_used_at,
			access_token_jti, extra_claims, expires_at, created_at
		FROM oa2_refresh_tokens WHERE id = $1
	`, id).Scan(
		&token.ID, &token.FamilyID, &token.ClientID, &token.IdentityID, &token.SessionID,
		&token.Scopes, &token.Audience, &token.Active, &token.Used, &token.UsedAt,
		&token.SuccessorID, &token.GracePeriodExpiresAt, &token.FirstUsedAt,
		&token.AccessTokenJTI, &claims, &token.ExpiresAt, &token.CreatedAt,
	)

	if err != nil {
		if err.Error() == "no rows in result set" {
			return nil, ErrNotFound
		}
		return nil, err
	}

	if len(claims) > 0 {
		_ = json.Unmarshal(claims, &token.ExtraClaims)
	}

	return token, nil
}

// MarkRefreshTokenUsed marks a refresh token as used and sets successor.
func (s *Store) MarkRefreshTokenUsed(ctx context.Context, id, successorID string, gracePeriod time.Duration) error {
	now := nowUTC()
	var gracePeriodExpires *time.Time
	if gracePeriod > 0 {
		exp := now.Add(gracePeriod)
		gracePeriodExpires = &exp
	}

	result, err := s.db.Exec(ctx, `
		UPDATE oa2_refresh_tokens SET
			used = true,
			used_at = $2,
			successor_id = $3,
			grace_period_expires_at = $4,
			first_used_at = COALESCE(first_used_at, $2)
		WHERE id = $1 AND active = true AND used = false
	`, id, now, successorID, gracePeriodExpires)

	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return ErrTokenInactive
	}
	return nil
}

// InvalidateRefreshTokenFamily marks all tokens in a family as inactive (replay detected).
func (s *Store) InvalidateRefreshTokenFamily(ctx context.Context, familyID string) (int64, error) {
	result, err := s.db.Exec(ctx, `
		UPDATE oa2_refresh_tokens SET active = false WHERE family_id = $1 AND active = true
	`, familyID)

	if err != nil {
		return 0, err
	}
	return result.RowsAffected(), nil
}

// RevokeRefreshTokensBySession revokes all refresh tokens for a session.
func (s *Store) RevokeRefreshTokensBySession(ctx context.Context, sessionID string) (int64, error) {
	result, err := s.db.Exec(ctx, `
		UPDATE oa2_refresh_tokens SET active = false WHERE session_id = $1 AND active = true
	`, sessionID)

	if err != nil {
		return 0, err
	}
	return result.RowsAffected(), nil
}

// CreateIDToken stores a new ID token.
func (s *Store) CreateIDToken(ctx context.Context, token *IDToken) error {
	if token.JTI == "" {
		var err error
		token.JTI, err = GenerateIDTokenJTI()
		if err != nil {
			return err
		}
	}
	token.CreatedAt = nowUTC()

	claims, _ := json.Marshal(token.ExtraClaims)

	_, err := s.db.Exec(ctx, `
		INSERT INTO oa2_id_tokens (
			jti, client_id, identity_id, session_id, nonce, at_hash, c_hash,
			acr, amr, auth_time, extra_claims, revoked, expires_at, created_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
	`,
		token.JTI, token.ClientID, token.IdentityID, token.SessionID, token.Nonce,
		token.ATHash, token.CHash, token.ACR, token.AMR, token.AuthTime,
		claims, token.Revoked, token.ExpiresAt, token.CreatedAt,
	)

	return err
}

// CleanupExpiredTokens removes expired tokens.
func (s *Store) CleanupExpiredTokens(ctx context.Context) (int64, error) {
	var total int64

	// Access tokens
	result, err := s.db.Exec(ctx, "DELETE FROM oa2_access_tokens WHERE expires_at < NOW()")
	if err != nil {
		return 0, err
	}
	total += result.RowsAffected()

	// Refresh tokens
	result, err = s.db.Exec(ctx, "DELETE FROM oa2_refresh_tokens WHERE expires_at < NOW()")
	if err != nil {
		return total, err
	}
	total += result.RowsAffected()

	// ID tokens
	result, err = s.db.Exec(ctx, "DELETE FROM oa2_id_tokens WHERE expires_at < NOW()")
	if err != nil {
		return total, err
	}
	total += result.RowsAffected()

	return total, nil
}

// IsValid checks if an access token is valid.
func (t *AccessToken) IsValid() error {
	if t.Revoked {
		return ErrTokenRevoked
	}
	if nowUTC().After(t.ExpiresAt) {
		return ErrTokenExpired
	}
	return nil
}

// IsValid checks if a refresh token is valid.
func (t *RefreshToken) IsValid() error {
	if !t.Active {
		return ErrTokenInactive
	}
	if t.Used {
		return ErrTokenInactive
	}
	if nowUTC().After(t.ExpiresAt) {
		return ErrTokenExpired
	}
	return nil
}
