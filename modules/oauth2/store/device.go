package store

import (
	"context"
	"time"
)

// DeviceCode represents a device authorization grant.
type DeviceCode struct {
	DeviceCode              string     `json:"device_code"`
	UserCode                string     `json:"user_code"`
	ClientID                string     `json:"client_id"`
	Scopes                  []string   `json:"scopes"`
	Audience                []string   `json:"audience,omitempty"`
	VerificationURI         string     `json:"verification_uri"`
	VerificationURIComplete *string    `json:"verification_uri_complete,omitempty"`
	Interval                int        `json:"interval"`
	IdentityID              *string    `json:"identity_id,omitempty"`
	SessionID               *string    `json:"session_id,omitempty"`
	Status                  string     `json:"status"` // pending, approved, denied, expired
	ApprovedAt              *time.Time `json:"approved_at,omitempty"`
	ExpiresAt               time.Time  `json:"expires_at"`
	LastPollAt              *time.Time `json:"last_poll_at,omitempty"`
	CreatedAt               time.Time  `json:"created_at"`
}

// CreateDeviceCode creates a new device code.
func (s *Store) CreateDeviceCode(ctx context.Context, dc *DeviceCode) error {
	if dc.DeviceCode == "" {
		dc.DeviceCode = GenerateDeviceCode()
	}
	if dc.UserCode == "" {
		dc.UserCode = GenerateUserCode()
	}
	dc.CreatedAt = nowUTC()
	if dc.Status == "" {
		dc.Status = "pending"
	}
	if dc.Interval == 0 {
		dc.Interval = 5
	}

	_, err := s.db.Exec(ctx, `
		INSERT INTO oa2_device_codes (
			device_code, user_code, client_id, scopes, audience, verification_uri,
			verification_uri_complete, interval, identity_id, session_id, status,
			approved_at, expires_at, last_poll_at, created_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
	`,
		dc.DeviceCode, dc.UserCode, dc.ClientID, dc.Scopes, dc.Audience,
		dc.VerificationURI, dc.VerificationURIComplete, dc.Interval,
		dc.IdentityID, dc.SessionID, dc.Status, dc.ApprovedAt,
		dc.ExpiresAt, dc.LastPollAt, dc.CreatedAt,
	)

	if isDuplicateKeyError(err) {
		return ErrAlreadyExists
	}
	return err
}

// GetDeviceCodeByDeviceCode retrieves a device code by device_code.
func (s *Store) GetDeviceCodeByDeviceCode(ctx context.Context, deviceCode string) (*DeviceCode, error) {
	dc := &DeviceCode{}

	err := s.db.QueryRow(ctx, `
		SELECT device_code, user_code, client_id, scopes, audience, verification_uri,
			verification_uri_complete, interval, identity_id, session_id, status,
			approved_at, expires_at, last_poll_at, created_at
		FROM oa2_device_codes WHERE device_code = $1
	`, deviceCode).Scan(
		&dc.DeviceCode, &dc.UserCode, &dc.ClientID, &dc.Scopes, &dc.Audience,
		&dc.VerificationURI, &dc.VerificationURIComplete, &dc.Interval,
		&dc.IdentityID, &dc.SessionID, &dc.Status, &dc.ApprovedAt,
		&dc.ExpiresAt, &dc.LastPollAt, &dc.CreatedAt,
	)

	if err != nil {
		if err.Error() == "no rows in result set" {
			return nil, ErrNotFound
		}
		return nil, err
	}

	return dc, nil
}

// GetDeviceCodeByUserCode retrieves a device code by user_code.
func (s *Store) GetDeviceCodeByUserCode(ctx context.Context, userCode string) (*DeviceCode, error) {
	dc := &DeviceCode{}

	err := s.db.QueryRow(ctx, `
		SELECT device_code, user_code, client_id, scopes, audience, verification_uri,
			verification_uri_complete, interval, identity_id, session_id, status,
			approved_at, expires_at, last_poll_at, created_at
		FROM oa2_device_codes WHERE user_code = $1
	`, userCode).Scan(
		&dc.DeviceCode, &dc.UserCode, &dc.ClientID, &dc.Scopes, &dc.Audience,
		&dc.VerificationURI, &dc.VerificationURIComplete, &dc.Interval,
		&dc.IdentityID, &dc.SessionID, &dc.Status, &dc.ApprovedAt,
		&dc.ExpiresAt, &dc.LastPollAt, &dc.CreatedAt,
	)

	if err != nil {
		if err.Error() == "no rows in result set" {
			return nil, ErrNotFound
		}
		return nil, err
	}

	return dc, nil
}

// UpdateDeviceCodePoll updates the last poll time.
func (s *Store) UpdateDeviceCodePoll(ctx context.Context, deviceCode string) error {
	now := nowUTC()
	result, err := s.db.Exec(ctx, `
		UPDATE oa2_device_codes SET last_poll_at = $2 WHERE device_code = $1
	`, deviceCode, now)

	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ApproveDeviceCode approves a device code.
func (s *Store) ApproveDeviceCode(ctx context.Context, userCode, identityID, sessionID string) error {
	now := nowUTC()
	result, err := s.db.Exec(ctx, `
		UPDATE oa2_device_codes SET
			identity_id = $2,
			session_id = $3,
			status = 'approved',
			approved_at = $4
		WHERE user_code = $1 AND status = 'pending'
	`, userCode, identityID, sessionID, now)

	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// DenyDeviceCode denies a device code.
func (s *Store) DenyDeviceCode(ctx context.Context, userCode string) error {
	result, err := s.db.Exec(ctx, `
		UPDATE oa2_device_codes SET status = 'denied' WHERE user_code = $1 AND status = 'pending'
	`, userCode)

	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteDeviceCode deletes a device code.
func (s *Store) DeleteDeviceCode(ctx context.Context, deviceCode string) error {
	_, err := s.db.Exec(ctx, "DELETE FROM oa2_device_codes WHERE device_code = $1", deviceCode)
	return err
}

// CleanupExpiredDeviceCodes removes expired device codes.
func (s *Store) CleanupExpiredDeviceCodes(ctx context.Context) (int64, error) {
	result, err := s.db.Exec(ctx, "DELETE FROM oa2_device_codes WHERE expires_at < NOW()")
	if err != nil {
		return 0, err
	}
	return result.RowsAffected(), nil
}

// JWTAssertion represents a JWT bearer assertion for replay protection.
type JWTAssertion struct {
	JTI       string    `json:"jti"`
	ClientID  string    `json:"client_id"`
	Issuer    string    `json:"issuer"`
	Subject   string    `json:"subject"`
	Audience  string    `json:"audience"`
	Used      bool      `json:"used"`
	UsedAt    *time.Time `json:"used_at,omitempty"`
	ExpiresAt time.Time `json:"expires_at"`
	CreatedAt time.Time `json:"created_at"`
}

// CreateJWTAssertion stores a JWT assertion for replay detection.
func (s *Store) CreateJWTAssertion(ctx context.Context, assertion *JWTAssertion) error {
	assertion.CreatedAt = nowUTC()

	_, err := s.db.Exec(ctx, `
		INSERT INTO oa2_jwt_assertions (
			jti, client_id, issuer, subject, audience, used, expires_at, created_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`,
		assertion.JTI, assertion.ClientID, assertion.Issuer, assertion.Subject,
		assertion.Audience, assertion.Used, assertion.ExpiresAt, assertion.CreatedAt,
	)

	if isDuplicateKeyError(err) {
		return ErrAlreadyExists // replay detected
	}
	return err
}

// MarkJWTAssertionUsed marks an assertion as used.
func (s *Store) MarkJWTAssertionUsed(ctx context.Context, jti string) error {
	now := nowUTC()
	result, err := s.db.Exec(ctx, `
		UPDATE oa2_jwt_assertions SET used = true, used_at = $2 WHERE jti = $1
	`, jti, now)

	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// CleanupExpiredJWTAssertions removes expired assertions.
func (s *Store) CleanupExpiredJWTAssertions(ctx context.Context) (int64, error) {
	result, err := s.db.Exec(ctx, "DELETE FROM oa2_jwt_assertions WHERE expires_at < NOW()")
	if err != nil {
		return 0, err
	}
	return result.RowsAffected(), nil
}
