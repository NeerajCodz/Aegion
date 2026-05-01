package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	platformcrypto "github.com/aegion/aegion/internal/platform/crypto"
	"github.com/google/uuid"
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
	pool      pgDB
	cipherKey []byte
}

func NewPostgres(pool *pgxpool.Pool, cipherKey []byte) (*PostgresStore, error) {
	if pool == nil {
		return nil, errors.New("postgres pool is required")
	}
	if len(cipherKey) != platformcrypto.KeySize {
		return nil, platformcrypto.ErrInvalidKeyLength
	}
	keyCopy := make([]byte, len(cipherKey))
	copy(keyCopy, cipherKey)
	return &PostgresStore{pool: pool, cipherKey: keyCopy}, nil
}

func (s *PostgresStore) ListProviders(ctx context.Context, includeDisabled bool) ([]Provider, error) {
	query := `
		SELECT
			p.id,
			p.slug,
			p.display_name,
			p.preset,
			p.protocol,
			COALESCE(p.issuer, ''),
			COALESCE(p.discovery_url, ''),
			COALESCE(p.authorize_endpoint, ''),
			COALESCE(p.token_endpoint, ''),
			COALESCE(p.userinfo_endpoint, ''),
			COALESCE(p.jwks_uri, ''),
			p.scopes,
			p.claim_mapping,
			p.extra_auth_params,
			p.pkce_method,
			p.auth_style,
			p.claim_source,
			p.enabled,
			p.trust_email_verified,
			p.redirect_uri,
			COALESCE(c.client_id, ''),
			COALESCE(c.client_secret_ciphertext, ''),
			p.created_at,
			p.updated_at
		FROM soc_providers p
		LEFT JOIN soc_provider_credentials c ON c.provider_id = p.id
	`
	args := []interface{}{}
	if !includeDisabled {
		query += ` WHERE p.enabled = TRUE`
	}
	query += ` ORDER BY p.display_name ASC, p.slug ASC`

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var providers []Provider
	for rows.Next() {
		provider, err := s.scanProvider(rows)
		if err != nil {
			return nil, err
		}
		providers = append(providers, provider)
	}
	return providers, rows.Err()
}

func (s *PostgresStore) GetProviderBySlug(ctx context.Context, slug string) (*Provider, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT
			p.id,
			p.slug,
			p.display_name,
			p.preset,
			p.protocol,
			COALESCE(p.issuer, ''),
			COALESCE(p.discovery_url, ''),
			COALESCE(p.authorize_endpoint, ''),
			COALESCE(p.token_endpoint, ''),
			COALESCE(p.userinfo_endpoint, ''),
			COALESCE(p.jwks_uri, ''),
			p.scopes,
			p.claim_mapping,
			p.extra_auth_params,
			p.pkce_method,
			p.auth_style,
			p.claim_source,
			p.enabled,
			p.trust_email_verified,
			p.redirect_uri,
			COALESCE(c.client_id, ''),
			COALESCE(c.client_secret_ciphertext, ''),
			p.created_at,
			p.updated_at
		FROM soc_providers p
		LEFT JOIN soc_provider_credentials c ON c.provider_id = p.id
		WHERE p.slug = $1
	`, normalizeSlug(slug))

	provider, err := s.scanProvider(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrProviderNotFound
		}
		return nil, err
	}
	return &provider, nil
}

func (s *PostgresStore) UpsertProvider(ctx context.Context, provider Provider) (*Provider, error) {
	provider.Slug = normalizeSlug(provider.Slug)
	if provider.Slug == "" {
		return nil, ErrProviderNotFound
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	scopesJSON, err := json.Marshal(normalizeScopes(provider.Scopes))
	if err != nil {
		return nil, err
	}
	claimJSON, err := json.Marshal(provider.ClaimMapping)
	if err != nil {
		return nil, err
	}
	extraJSON, err := json.Marshal(provider.ExtraAuthParams)
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	var (
		providerID uuid.UUID
		createdAt  time.Time
		updatedAt  time.Time
	)
	err = tx.QueryRow(ctx, `
		INSERT INTO soc_providers (
			id, slug, display_name, preset, protocol, issuer, discovery_url,
			authorize_endpoint, token_endpoint, userinfo_endpoint, jwks_uri,
			scopes, claim_mapping, extra_auth_params, pkce_method, auth_style,
			claim_source, enabled, trust_email_verified, redirect_uri, created_at, updated_at
		) VALUES (
			COALESCE(NULLIF($1::text, '')::uuid, gen_random_uuid()), $2, $3, $4, $5, NULLIF($6, ''),
			NULLIF($7, ''), NULLIF($8, ''), NULLIF($9, ''), NULLIF($10, ''),
			NULLIF($11, ''), $12::jsonb, $13::jsonb, $14::jsonb, $15, $16, $17,
			$18, $19, $20, $21, $22
		)
		ON CONFLICT (slug) DO UPDATE SET
			display_name = EXCLUDED.display_name,
			preset = EXCLUDED.preset,
			protocol = EXCLUDED.protocol,
			issuer = EXCLUDED.issuer,
			discovery_url = EXCLUDED.discovery_url,
			authorize_endpoint = EXCLUDED.authorize_endpoint,
			token_endpoint = EXCLUDED.token_endpoint,
			userinfo_endpoint = EXCLUDED.userinfo_endpoint,
			jwks_uri = EXCLUDED.jwks_uri,
			scopes = EXCLUDED.scopes,
			claim_mapping = EXCLUDED.claim_mapping,
			extra_auth_params = EXCLUDED.extra_auth_params,
			pkce_method = EXCLUDED.pkce_method,
			auth_style = EXCLUDED.auth_style,
			claim_source = EXCLUDED.claim_source,
			enabled = EXCLUDED.enabled,
			trust_email_verified = EXCLUDED.trust_email_verified,
			redirect_uri = EXCLUDED.redirect_uri,
			updated_at = EXCLUDED.updated_at
		RETURNING id, created_at, updated_at
	`,
		uuidText(provider.ID),
		provider.Slug,
		strings.TrimSpace(provider.DisplayName),
		strings.TrimSpace(provider.Preset),
		string(provider.Protocol),
		strings.TrimSpace(provider.Issuer),
		strings.TrimSpace(provider.DiscoveryURL),
		strings.TrimSpace(provider.AuthorizeEndpoint),
		strings.TrimSpace(provider.TokenEndpoint),
		strings.TrimSpace(provider.UserInfoEndpoint),
		strings.TrimSpace(provider.JWKSURI),
		scopesJSON,
		claimJSON,
		extraJSON,
		string(provider.PKCEMethod),
		string(provider.AuthStyle),
		string(provider.ClaimSource),
		provider.Enabled,
		provider.TrustEmailVerified,
		strings.TrimSpace(provider.RedirectURI),
		now,
		now,
	).Scan(&providerID, &createdAt, &updatedAt)
	if err != nil {
		return nil, err
	}

	clientSecretCiphertext := ""
	if strings.TrimSpace(provider.ClientSecret) != "" {
		clientSecretCiphertext, err = s.encryptSecret(provider.Slug, provider.ClientSecret)
		if err != nil {
			return nil, err
		}
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO soc_provider_credentials (
			provider_id, client_id, client_secret_ciphertext, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (provider_id) DO UPDATE SET
			client_id = EXCLUDED.client_id,
			client_secret_ciphertext = CASE
				WHEN EXCLUDED.client_secret_ciphertext <> '' THEN EXCLUDED.client_secret_ciphertext
				ELSE soc_provider_credentials.client_secret_ciphertext
			END,
			updated_at = EXCLUDED.updated_at
	`, providerID, strings.TrimSpace(provider.ClientID), clientSecretCiphertext, now, now)
	if err != nil {
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}

	return s.GetProviderBySlug(ctx, provider.Slug)
}

func (s *PostgresStore) DeleteProvider(ctx context.Context, slug string) error {
	result, err := s.pool.Exec(ctx, `DELETE FROM soc_providers WHERE slug = $1`, normalizeSlug(slug))
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return ErrProviderNotFound
	}
	return nil
}

func (s *PostgresStore) SaveState(ctx context.Context, state AuthState) error {
	verifierCiphertext := ""
	var err error
	if strings.TrimSpace(state.PKCEVerifier) != "" {
		verifierCiphertext, err = platformcrypto.EncryptField(s.cipherKey, []byte(state.PKCEVerifier), []byte(state.ID))
		if err != nil {
			return err
		}
	}

	_, err = s.pool.Exec(ctx, `
		INSERT INTO soc_auth_states (
			id, provider_slug, redirect_to, nonce, pkce_verifier_ciphertext, expires_at, created_at
		) VALUES ($1, $2, $3, $4, $5, $6, NOW())
	`, state.ID, normalizeSlug(state.ProviderSlug), strings.TrimSpace(state.RedirectTo), strings.TrimSpace(state.Nonce), verifierCiphertext, state.ExpiresAt.UTC())
	return err
}

func (s *PostgresStore) ConsumeState(ctx context.Context, stateID string) (AuthState, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return AuthState{}, err
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	var (
		state                  AuthState
		pkceVerifierCiphertext string
	)
	err = tx.QueryRow(ctx, `
		SELECT id, provider_slug, COALESCE(redirect_to, ''), COALESCE(nonce, ''), COALESCE(pkce_verifier_ciphertext, ''), expires_at
		FROM soc_auth_states
		WHERE id = $1
	`, stateID).Scan(&state.ID, &state.ProviderSlug, &state.RedirectTo, &state.Nonce, &pkceVerifierCiphertext, &state.ExpiresAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return AuthState{}, ErrStateNotFound
		}
		return AuthState{}, err
	}

	if _, err := tx.Exec(ctx, `DELETE FROM soc_auth_states WHERE id = $1`, stateID); err != nil {
		return AuthState{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return AuthState{}, err
	}
	if time.Now().UTC().After(state.ExpiresAt) {
		return AuthState{}, ErrStateExpired
	}
	if pkceVerifierCiphertext != "" {
		plaintext, err := platformcrypto.DecryptField(s.cipherKey, pkceVerifierCiphertext, []byte(state.ID))
		if err != nil {
			return AuthState{}, err
		}
		state.PKCEVerifier = string(plaintext)
	}
	return state, nil
}

func (s *PostgresStore) ResolveIdentity(ctx context.Context, provider Provider, profile SocialProfile) (*IdentityLinkResult, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	now := time.Now().UTC()
	rawClaimsJSON, err := json.Marshal(profile.RawClaims)
	if err != nil {
		return nil, err
	}

	if existingIdentityID, ok, err := s.identityByLink(ctx, tx, provider.Slug, profile.ProviderUser); err != nil {
		return nil, err
	} else if ok {
		if err := s.refreshLink(ctx, tx, provider, existingIdentityID, profile, rawClaimsJSON, now); err != nil {
			return nil, err
		}
		if s.shouldTrustEmail(provider, profile) {
			if err := s.upsertPrimaryEmail(ctx, tx, existingIdentityID, profile.Email, true); err != nil {
				return nil, err
			}
		}
		if err := tx.Commit(ctx); err != nil {
			return nil, err
		}
		return &IdentityLinkResult{IdentityID: existingIdentityID, Linked: true}, nil
	}

	identityID, created, err := s.lookupOrCreateIdentity(ctx, tx, provider, profile)
	if err != nil {
		return nil, err
	}
	if err := s.refreshLink(ctx, tx, provider, identityID, profile, rawClaimsJSON, now); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &IdentityLinkResult{
		IdentityID: identityID,
		Created:    created,
		Linked:     true,
	}, nil
}

func (s *PostgresStore) lookupOrCreateIdentity(ctx context.Context, tx pgx.Tx, provider Provider, profile SocialProfile) (uuid.UUID, bool, error) {
	if email := strings.ToLower(strings.TrimSpace(profile.Email)); email != "" {
		var identityID uuid.UUID
		err := tx.QueryRow(ctx, `
			SELECT i.id
			FROM core_identities i
			JOIN core_identity_addresses a ON a.identity_id = i.id
			WHERE a.type = 'email'
			  AND LOWER(a.value) = LOWER($1)
			  AND i.deleted_at IS NULL
			ORDER BY a.verified DESC, a.is_primary DESC, a.created_at ASC
			LIMIT 1
		`, email).Scan(&identityID)
		switch {
		case err == nil:
			if s.shouldTrustEmail(provider, profile) {
				if err := s.upsertPrimaryEmail(ctx, tx, identityID, email, true); err != nil {
					return uuid.Nil, false, err
				}
			}
			return identityID, false, nil
		case err != nil && !errors.Is(err, pgx.ErrNoRows):
			return uuid.Nil, false, err
		}
	}

	var schemaID uuid.UUID
	if err := tx.QueryRow(ctx, `
		SELECT id
		FROM core_identity_schemas
		ORDER BY is_default DESC, created_at ASC
		LIMIT 1
	`).Scan(&schemaID); err != nil {
		return uuid.Nil, false, err
	}

	identityID := uuid.New()
	traits := map[string]interface{}{}
	if email := strings.ToLower(strings.TrimSpace(profile.Email)); email != "" {
		traits["email"] = email
	}
	if name := strings.TrimSpace(profile.Name); name != "" {
		traits["display_name"] = name
	}
	traitsJSON, err := json.Marshal(traits)
	if err != nil {
		return uuid.Nil, false, err
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO core_identities (id, schema_id, traits, state, created_at, updated_at)
		VALUES ($1, $2, $3::jsonb, 'active', NOW(), NOW())
	`, identityID, schemaID, traitsJSON); err != nil {
		return uuid.Nil, false, err
	}
	if email := strings.ToLower(strings.TrimSpace(profile.Email)); email != "" {
		if err := s.upsertPrimaryEmail(ctx, tx, identityID, email, s.shouldTrustEmail(provider, profile)); err != nil {
			return uuid.Nil, false, err
		}
	}

	return identityID, true, nil
}

func (s *PostgresStore) refreshLink(ctx context.Context, tx pgx.Tx, provider Provider, identityID uuid.UUID, profile SocialProfile, rawClaimsJSON []byte, now time.Time) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO soc_identity_links (
			id, provider_slug, identity_id, provider_subject, email, email_verified,
			display_name, picture_url, raw_claims, linked_at, last_login_at, created_at, updated_at
		) VALUES (
			gen_random_uuid(), $1, $2, $3, NULLIF($4, ''), $5,
			NULLIF($6, ''), NULLIF($7, ''), $8::jsonb, $9, $9, $9, $9
		)
		ON CONFLICT (provider_slug, provider_subject) DO UPDATE SET
			identity_id = EXCLUDED.identity_id,
			email = EXCLUDED.email,
			email_verified = EXCLUDED.email_verified,
			display_name = EXCLUDED.display_name,
			picture_url = EXCLUDED.picture_url,
			raw_claims = EXCLUDED.raw_claims,
			last_login_at = EXCLUDED.last_login_at,
			updated_at = EXCLUDED.updated_at
	`, provider.Slug, identityID, profile.ProviderUser, strings.ToLower(strings.TrimSpace(profile.Email)), profile.EmailVerified, strings.TrimSpace(profile.Name), strings.TrimSpace(profile.PictureURL), rawClaimsJSON, now)
	return err
}

func (s *PostgresStore) identityByLink(ctx context.Context, tx pgx.Tx, slug, subject string) (uuid.UUID, bool, error) {
	var identityID uuid.UUID
	err := tx.QueryRow(ctx, `
		SELECT identity_id
		FROM soc_identity_links
		WHERE provider_slug = $1 AND provider_subject = $2
	`, normalizeSlug(slug), strings.TrimSpace(subject)).Scan(&identityID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return uuid.Nil, false, nil
		}
		return uuid.Nil, false, err
	}
	return identityID, true, nil
}

func (s *PostgresStore) upsertPrimaryEmail(ctx context.Context, tx pgx.Tx, identityID uuid.UUID, email string, verified bool) error {
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" {
		return nil
	}

	result, err := tx.Exec(ctx, `
		UPDATE core_identity_addresses
		SET value = $1,
			verified = $2,
			verified_at = CASE WHEN $2 THEN COALESCE(verified_at, NOW()) ELSE NULL END,
			updated_at = NOW()
		WHERE identity_id = $3
		  AND type = 'email'
		  AND is_primary = TRUE
	`, email, verified, identityID)
	if err != nil {
		return err
	}
	if result.RowsAffected() > 0 {
		return nil
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO core_identity_addresses (
			id, identity_id, type, value, is_primary, verified, verified_at, created_at, updated_at
		) VALUES (
			$1, $2, 'email', $3, TRUE, $4, CASE WHEN $4 THEN NOW() ELSE NULL END, NOW(), NOW()
		)
	`, uuid.New(), identityID, email, verified)
	return err
}

func (s *PostgresStore) shouldTrustEmail(provider Provider, profile SocialProfile) bool {
	return provider.TrustEmailVerified && profile.EmailVerified && strings.TrimSpace(profile.Email) != ""
}

func (s *PostgresStore) encryptSecret(slug, secret string) (string, error) {
	return platformcrypto.EncryptField(s.cipherKey, []byte(secret), []byte("social-provider:"+normalizeSlug(slug)))
}

func (s *PostgresStore) decryptSecret(slug, ciphertext string) (string, error) {
	if strings.TrimSpace(ciphertext) == "" {
		return "", nil
	}
	plaintext, err := platformcrypto.DecryptField(s.cipherKey, ciphertext, []byte("social-provider:"+normalizeSlug(slug)))
	if err != nil {
		return "", err
	}
	return string(plaintext), nil
}

func (s *PostgresStore) scanProvider(scanner interface{ Scan(dest ...any) error }) (Provider, error) {
	var (
		provider               Provider
		scopesJSON             []byte
		claimJSON              []byte
		extraJSON              []byte
		clientSecretCiphertext string
	)
	err := scanner.Scan(
		&provider.ID,
		&provider.Slug,
		&provider.DisplayName,
		&provider.Preset,
		&provider.Protocol,
		&provider.Issuer,
		&provider.DiscoveryURL,
		&provider.AuthorizeEndpoint,
		&provider.TokenEndpoint,
		&provider.UserInfoEndpoint,
		&provider.JWKSURI,
		&scopesJSON,
		&claimJSON,
		&extraJSON,
		&provider.PKCEMethod,
		&provider.AuthStyle,
		&provider.ClaimSource,
		&provider.Enabled,
		&provider.TrustEmailVerified,
		&provider.RedirectURI,
		&provider.ClientID,
		&clientSecretCiphertext,
		&provider.CreatedAt,
		&provider.UpdatedAt,
	)
	if err != nil {
		return Provider{}, err
	}

	if err := json.Unmarshal(scopesJSON, &provider.Scopes); err != nil {
		return Provider{}, err
	}
	if len(provider.Scopes) == 0 {
		provider.Scopes = []string{}
	}
	if err := json.Unmarshal(claimJSON, &provider.ClaimMapping); err != nil {
		return Provider{}, err
	}
	if err := json.Unmarshal(extraJSON, &provider.ExtraAuthParams); err != nil {
		return Provider{}, err
	}
	if provider.ExtraAuthParams == nil {
		provider.ExtraAuthParams = map[string]string{}
	}
	provider.ClientSecret, err = s.decryptSecret(provider.Slug, clientSecretCiphertext)
	if err != nil {
		return Provider{}, fmt.Errorf("decrypt provider secret: %w", err)
	}
	return provider, nil
}

func normalizeScopes(scopes []string) []string {
	out := make([]string, 0, len(scopes))
	seen := map[string]struct{}{}
	for _, scope := range scopes {
		trimmed := strings.TrimSpace(scope)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
	}
	return out
}

func uuidText(id uuid.UUID) string {
	if id == uuid.Nil {
		return ""
	}
	return id.String()
}
