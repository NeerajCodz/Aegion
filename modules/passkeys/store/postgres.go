package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PostgresStore struct {
	pool *pgxpool.Pool
}

func NewPostgres(pool *pgxpool.Pool) (*PostgresStore, error) {
	if pool == nil {
		return nil, errors.New("postgres pool is required")
	}
	return &PostgresStore{pool: pool}, nil
}

func (s *PostgresStore) SaveChallenge(challenge Challenge) {
	_, _ = s.pool.Exec(context.Background(), `
		INSERT INTO passkey_challenges (id, identity_id, purpose, expires_at, created_at)
		VALUES ($1, $2, $3, $4, NOW())
		ON CONFLICT (id) DO UPDATE SET
			identity_id = EXCLUDED.identity_id,
			purpose = EXCLUDED.purpose,
			expires_at = EXCLUDED.expires_at
	`, challenge.ID, challenge.IdentityID, challenge.Purpose, challenge.ExpiresAt.UTC())
}

func (s *PostgresStore) ConsumeChallenge(challengeID string) (Challenge, error) {
	tx, err := s.pool.Begin(context.Background())
	if err != nil {
		return Challenge{}, err
	}
	defer func() {
		_ = tx.Rollback(context.Background())
	}()

	var challenge Challenge
	err = tx.QueryRow(context.Background(), `
		SELECT id, identity_id, purpose, expires_at
		FROM passkey_challenges
		WHERE id = $1
	`, challengeID).Scan(&challenge.ID, &challenge.IdentityID, &challenge.Purpose, &challenge.ExpiresAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Challenge{}, ErrChallengeNotFound
		}
		return Challenge{}, err
	}
	if _, err := tx.Exec(context.Background(), `DELETE FROM passkey_challenges WHERE id = $1`, challengeID); err != nil {
		return Challenge{}, err
	}
	if err := tx.Commit(context.Background()); err != nil {
		return Challenge{}, err
	}
	if time.Now().UTC().After(challenge.ExpiresAt) {
		return Challenge{}, ErrChallengeExpired
	}
	return challenge, nil
}

func (s *PostgresStore) UpsertCredential(credential Credential) {
	_, _ = s.pool.Exec(context.Background(), `
		INSERT INTO passkey_credentials (id, identity_id, public_key, sign_count, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, NOW())
		ON CONFLICT (id) DO UPDATE SET
			identity_id = EXCLUDED.identity_id,
			public_key = EXCLUDED.public_key,
			sign_count = EXCLUDED.sign_count,
			updated_at = NOW()
	`, credential.ID, credential.IdentityID, credential.PublicKey, credential.SignCount, credential.CreatedAt.UTC())
}

func (s *PostgresStore) GetCredential(credentialID string) (Credential, error) {
	var credential Credential
	err := s.pool.QueryRow(context.Background(), `
		SELECT id, identity_id, public_key, sign_count, created_at
		FROM passkey_credentials
		WHERE id = $1
	`, credentialID).Scan(&credential.ID, &credential.IdentityID, &credential.PublicKey, &credential.SignCount, &credential.CreatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Credential{}, ErrCredentialNotFound
		}
		return Credential{}, err
	}
	return credential, nil
}

func (s *PostgresStore) UpdateCredentialSignCount(credentialID string, signCount uint32) error {
	result, err := s.pool.Exec(context.Background(), `
		UPDATE passkey_credentials
		SET sign_count = $2, updated_at = NOW()
		WHERE id = $1
	`, credentialID, signCount)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return ErrCredentialNotFound
	}
	return nil
}

func (s *PostgresStore) ListCredentialsByIdentity(identityID string) []Credential {
	rows, err := s.pool.Query(context.Background(), `
		SELECT id, identity_id, public_key, sign_count, created_at
		FROM passkey_credentials
		WHERE identity_id = $1
		ORDER BY created_at ASC
	`, identityID)
	if err != nil {
		return []Credential{}
	}
	defer rows.Close()

	credentials := make([]Credential, 0)
	for rows.Next() {
		var credential Credential
		if err := rows.Scan(&credential.ID, &credential.IdentityID, &credential.PublicKey, &credential.SignCount, &credential.CreatedAt); err != nil {
			return []Credential{}
		}
		credentials = append(credentials, credential)
	}
	return credentials
}
