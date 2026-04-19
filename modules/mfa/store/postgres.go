package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type pgDB interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Begin(ctx context.Context) (pgx.Tx, error)
}

type PostgresStore struct {
	pool pgDB
}

func NewPostgres(pool *pgxpool.Pool) (*PostgresStore, error) {
	if pool == nil {
		return nil, errors.New("postgres pool is required")
	}
	return &PostgresStore{pool: pool}, nil
}

func (s *PostgresStore) SaveEnrollment(enrollment Enrollment) error {
	_, err := s.pool.Exec(context.Background(), `
		INSERT INTO mfa_enrollments (id, identity_id, secret_ciphertext, expires_at, created_at)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (id) DO UPDATE SET
			identity_id = EXCLUDED.identity_id,
			secret_ciphertext = EXCLUDED.secret_ciphertext,
			expires_at = EXCLUDED.expires_at
	`, enrollment.ID, enrollment.IdentityID, enrollment.SecretCiphertext, enrollment.ExpiresAt.UTC(), enrollment.CreatedAt.UTC())
	return err
}

func (s *PostgresStore) GetEnrollment(enrollmentID string) (Enrollment, error) {
	var enrollment Enrollment
	err := s.pool.QueryRow(context.Background(), `
		SELECT id, identity_id, secret_ciphertext, expires_at, created_at
		FROM mfa_enrollments
		WHERE id = $1
	`, enrollmentID).Scan(&enrollment.ID, &enrollment.IdentityID, &enrollment.SecretCiphertext, &enrollment.ExpiresAt, &enrollment.CreatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Enrollment{}, ErrEnrollmentNotFound
		}
		return Enrollment{}, err
	}
	return enrollment, nil
}

func (s *PostgresStore) DeleteEnrollment(enrollmentID string) error {
	_, err := s.pool.Exec(context.Background(), `DELETE FROM mfa_enrollments WHERE id = $1`, enrollmentID)
	return err
}

func (s *PostgresStore) UpsertTOTPFactor(factor TOTPFactor) error {
	_, err := s.pool.Exec(context.Background(), `
		INSERT INTO mfa_totp_factors (identity_id, secret_ciphertext, enrolled_at, last_used_at, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (identity_id) DO UPDATE SET
			secret_ciphertext = EXCLUDED.secret_ciphertext,
			enrolled_at = EXCLUDED.enrolled_at,
			last_used_at = EXCLUDED.last_used_at,
			updated_at = EXCLUDED.updated_at
	`, factor.IdentityID, factor.SecretCiphertext, factor.EnrolledAt.UTC(), nullTime(factor.LastUsedAt), factor.CreatedAt.UTC(), factor.UpdatedAt.UTC())
	return err
}

func (s *PostgresStore) GetTOTPFactor(identityID string) (TOTPFactor, error) {
	var factor TOTPFactor
	err := s.pool.QueryRow(context.Background(), `
		SELECT identity_id, secret_ciphertext, enrolled_at, COALESCE(last_used_at, '0001-01-01T00:00:00Z'::timestamptz), created_at, updated_at
		FROM mfa_totp_factors
		WHERE identity_id = $1
	`, identityID).Scan(&factor.IdentityID, &factor.SecretCiphertext, &factor.EnrolledAt, &factor.LastUsedAt, &factor.CreatedAt, &factor.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return TOTPFactor{}, ErrTOTPFactorNotFound
		}
		return TOTPFactor{}, err
	}
	return factor, nil
}

func (s *PostgresStore) UpdateTOTPLastUsed(identityID string, usedAt time.Time) error {
	result, err := s.pool.Exec(context.Background(), `
		UPDATE mfa_totp_factors
		SET last_used_at = $2, updated_at = $2
		WHERE identity_id = $1
	`, identityID, usedAt.UTC())
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return ErrTOTPFactorNotFound
	}
	return nil
}

func (s *PostgresStore) ReplaceBackupCodes(identityID string, codes []BackupCode) error {
	tx, err := s.pool.Begin(context.Background())
	if err != nil {
		return err
	}
	defer func() {
		_ = tx.Rollback(context.Background())
	}()

	if _, err := tx.Exec(context.Background(), `DELETE FROM mfa_backup_codes WHERE identity_id = $1`, identityID); err != nil {
		return err
	}
	for _, code := range codes {
		if _, err := tx.Exec(context.Background(), `
			INSERT INTO mfa_backup_codes (id, identity_id, code_hash, batch_id, used_at, created_at)
			VALUES ($1, $2, $3, $4, $5, $6)
		`, code.ID, code.IdentityID, code.CodeHash, code.BatchID, nullTimePtr(code.UsedAt), code.CreatedAt.UTC()); err != nil {
			return err
		}
	}
	return tx.Commit(context.Background())
}

func (s *PostgresStore) ListBackupCodes(identityID string) ([]BackupCode, error) {
	rows, err := s.pool.Query(context.Background(), `
		SELECT id, identity_id, code_hash, batch_id, used_at, created_at
		FROM mfa_backup_codes
		WHERE identity_id = $1
		ORDER BY created_at ASC
	`, identityID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	codes := make([]BackupCode, 0)
	for rows.Next() {
		var code BackupCode
		if err := rows.Scan(&code.ID, &code.IdentityID, &code.CodeHash, &code.BatchID, &code.UsedAt, &code.CreatedAt); err != nil {
			return nil, err
		}
		codes = append(codes, code)
	}
	return codes, rows.Err()
}

func (s *PostgresStore) MarkBackupCodeUsed(identityID, codeID string, usedAt time.Time) error {
	result, err := s.pool.Exec(context.Background(), `
		UPDATE mfa_backup_codes
		SET used_at = $3
		WHERE identity_id = $1
		  AND id = $2
		  AND used_at IS NULL
	`, identityID, codeID, usedAt.UTC())
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return ErrBackupCodeNotFound
	}
	return nil
}

func (s *PostgresStore) SaveTrustedDevice(device TrustedDevice) error {
	_, err := s.pool.Exec(context.Background(), `
		INSERT INTO mfa_trusted_devices (
			id, identity_id, token_hash, token_prefix, label, expires_at, last_used_at, created_at, revoked_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`, device.ID, device.IdentityID, device.TokenHash, device.TokenPrefix, device.Label, device.ExpiresAt.UTC(), nullTimePtr(device.LastUsedAt), device.CreatedAt.UTC(), nullTimePtr(device.RevokedAt))
	return err
}

func (s *PostgresStore) GetTrustedDevice(identityID, tokenHash, tokenPrefix string) (TrustedDevice, error) {
	var device TrustedDevice
	err := s.pool.QueryRow(context.Background(), `
		SELECT id, identity_id, token_hash, token_prefix, COALESCE(label, ''), expires_at, last_used_at, created_at, revoked_at
		FROM mfa_trusted_devices
		WHERE identity_id = $1
		  AND token_hash = $2
		  AND token_prefix = $3
		  AND revoked_at IS NULL
		ORDER BY created_at DESC
		LIMIT 1
	`, identityID, tokenHash, tokenPrefix).Scan(
		&device.ID,
		&device.IdentityID,
		&device.TokenHash,
		&device.TokenPrefix,
		&device.Label,
		&device.ExpiresAt,
		&device.LastUsedAt,
		&device.CreatedAt,
		&device.RevokedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return TrustedDevice{}, ErrTrustedDeviceNotFound
		}
		return TrustedDevice{}, err
	}
	return device, nil
}

func (s *PostgresStore) TouchTrustedDevice(identityID, deviceID string, touchedAt time.Time) error {
	result, err := s.pool.Exec(context.Background(), `
		UPDATE mfa_trusted_devices
		SET last_used_at = $3
		WHERE identity_id = $1
		  AND id = $2
		  AND revoked_at IS NULL
	`, identityID, deviceID, touchedAt.UTC())
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return ErrTrustedDeviceNotFound
	}
	return nil
}

func (s *PostgresStore) DeleteTrustedDevice(identityID, deviceID string, revokedAt time.Time) error {
	result, err := s.pool.Exec(context.Background(), `
		UPDATE mfa_trusted_devices
		SET revoked_at = $3
		WHERE identity_id = $1
		  AND id = $2
		  AND revoked_at IS NULL
	`, identityID, deviceID, revokedAt.UTC())
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return ErrTrustedDeviceNotFound
	}
	return nil
}

func (s *PostgresStore) DeleteAllIdentityData(identityID string) error {
	tx, err := s.pool.Begin(context.Background())
	if err != nil {
		return err
	}
	defer func() {
		_ = tx.Rollback(context.Background())
	}()

	statements := []string{
		`DELETE FROM mfa_enrollments WHERE identity_id = $1`,
		`DELETE FROM mfa_backup_codes WHERE identity_id = $1`,
		`DELETE FROM mfa_totp_factors WHERE identity_id = $1`,
		`DELETE FROM mfa_trusted_devices WHERE identity_id = $1`,
	}
	for _, statement := range statements {
		if _, err := tx.Exec(context.Background(), statement, identityID); err != nil {
			return err
		}
	}
	return tx.Commit(context.Background())
}

func (s *PostgresStore) ListFactorsByIdentity(identityID string) ([]Factor, error) {
	rows, err := s.pool.Query(context.Background(), `
		SELECT identity_id, enrolled_at, COALESCE(last_used_at, '0001-01-01T00:00:00Z'::timestamptz)
		FROM mfa_totp_factors
		WHERE identity_id = $1
	`, identityID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	factors := make([]Factor, 0)
	for rows.Next() {
		var factor Factor
		var storedIdentityID string
		if err := rows.Scan(&storedIdentityID, &factor.EnrolledAt, &factor.LastUsedAt); err != nil {
			return nil, err
		}
		factor.ID = storedIdentityID + ":totp"
		factor.Method = "totp"
		factor.Verified = true
		factors = append(factors, factor)
	}
	return factors, rows.Err()
}

func nullTime(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value.UTC()
}

func nullTimePtr(value *time.Time) any {
	if value == nil || value.IsZero() {
		return nil
	}
	return value.UTC()
}
