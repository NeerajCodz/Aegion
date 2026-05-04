package store

import (
	"context"
	"time"
)

// AuthCode represents an OAuth2 authorization code.
type AuthCode struct {
	Code                string     `json:"code"`
	ClientID            string     `json:"client_id"`
	IdentityID          string     `json:"identity_id"`
	SessionID           string     `json:"session_id"`
	RedirectURI         string     `json:"redirect_uri"`
	Scopes              []string   `json:"scopes"`
	Audience            []string   `json:"audience,omitempty"`
	CodeChallenge       *string    `json:"code_challenge,omitempty"`
	CodeChallengeMethod *string    `json:"code_challenge_method,omitempty"`
	Nonce               *string    `json:"nonce,omitempty"`
	State               *string    `json:"state,omitempty"`
	ACR                 string     `json:"acr"`
	AMR                 []string   `json:"amr"`
	AuthTime            time.Time  `json:"auth_time"`
	RequestObject       *string    `json:"request_object,omitempty"`
	Used                bool       `json:"used"`
	UsedAt              *time.Time `json:"used_at,omitempty"`
	ExpiresAt           time.Time  `json:"expires_at"`
	CreatedAt           time.Time  `json:"created_at"`
}

// CreateAuthCode creates a new authorization code.
func (s *Store) CreateAuthCode(ctx context.Context, code *AuthCode) error {
	if code.Code == "" {
		var err error
		code.Code, err = GenerateAuthCode()
		if err != nil {
			return err
		}
	}
	now := nowUTC()
	code.CreatedAt = now
	if code.AuthTime.IsZero() {
		code.AuthTime = now
	}

	_, err := s.db.Exec(ctx, `
		INSERT INTO oa2_auth_codes (
			code, client_id, identity_id, session_id, redirect_uri, scopes, audience,
			code_challenge, code_challenge_method, nonce, state, acr, amr, auth_time,
			request_object, used, expires_at, created_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18)
	`,
		code.Code, code.ClientID, code.IdentityID, code.SessionID, code.RedirectURI,
		code.Scopes, code.Audience, code.CodeChallenge, code.CodeChallengeMethod,
		code.Nonce, code.State, code.ACR, code.AMR, code.AuthTime, code.RequestObject,
		code.Used, code.ExpiresAt, code.CreatedAt,
	)

	if isDuplicateKeyError(err) {
		return ErrAlreadyExists
	}
	return err
}

// GetAuthCode retrieves an authorization code.
func (s *Store) GetAuthCode(ctx context.Context, code string) (*AuthCode, error) {
	ac := &AuthCode{}

	err := s.db.QueryRow(ctx, `
		SELECT code, client_id, identity_id, session_id, redirect_uri, scopes, audience,
			code_challenge, code_challenge_method, nonce, state, acr, amr, auth_time,
			request_object, used, used_at, expires_at, created_at
		FROM oa2_auth_codes WHERE code = $1
	`, code).Scan(
		&ac.Code, &ac.ClientID, &ac.IdentityID, &ac.SessionID, &ac.RedirectURI,
		&ac.Scopes, &ac.Audience, &ac.CodeChallenge, &ac.CodeChallengeMethod,
		&ac.Nonce, &ac.State, &ac.ACR, &ac.AMR, &ac.AuthTime, &ac.RequestObject,
		&ac.Used, &ac.UsedAt, &ac.ExpiresAt, &ac.CreatedAt,
	)

	if err != nil {
		if err.Error() == "no rows in result set" {
			return nil, ErrNotFound
		}
		return nil, err
	}

	return ac, nil
}

// MarkAuthCodeUsed marks an authorization code as used.
func (s *Store) MarkAuthCodeUsed(ctx context.Context, code string) error {
	now := nowUTC()
	result, err := s.db.Exec(ctx, `
		UPDATE oa2_auth_codes SET used = true, used_at = $2 WHERE code = $1 AND used = false
	`, code, now)

	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return ErrCodeUsed
	}
	return nil
}

// DeleteAuthCode deletes an authorization code.
func (s *Store) DeleteAuthCode(ctx context.Context, code string) error {
	_, err := s.db.Exec(ctx, "DELETE FROM oa2_auth_codes WHERE code = $1", code)
	return err
}

// CleanupExpiredAuthCodes removes expired authorization codes.
func (s *Store) CleanupExpiredAuthCodes(ctx context.Context) (int64, error) {
	result, err := s.db.Exec(ctx, "DELETE FROM oa2_auth_codes WHERE expires_at < NOW()")
	if err != nil {
		return 0, err
	}
	return result.RowsAffected(), nil
}

// ValidateAuthCode checks if a code is valid and not expired/used.
func (ac *AuthCode) IsValid() error {
	if ac.Used {
		return ErrCodeUsed
	}
	if nowUTC().After(ac.ExpiresAt) {
		return ErrCodeExpired
	}
	return nil
}
