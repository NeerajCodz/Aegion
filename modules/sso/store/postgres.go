package store

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type pgDB interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
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

func (s *PostgresStore) ListConnections(ctx context.Context, includeDisabled bool) ([]Connection, error) {
	query := `
		SELECT id, slug, display_name, entity_id, sso_url, certificate_pem, metadata_url, domains, attribute_mapping, jit_provisioning, default_redirect_to, extra_authn_context, enabled, created_at, updated_at
		FROM sso_connections
	`
	if !includeDisabled {
		query += ` WHERE enabled = TRUE`
	}
	query += ` ORDER BY display_name ASC, slug ASC`
	rows, err := s.pool.Query(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	connections := make([]Connection, 0)
	for rows.Next() {
		connection, err := scanConnection(rows)
		if err != nil {
			return nil, err
		}
		connections = append(connections, connection)
	}
	return connections, rows.Err()
}

func (s *PostgresStore) GetConnectionBySlug(ctx context.Context, slug string) (*Connection, error) {
	connection, err := scanConnection(s.pool.QueryRow(ctx, `
		SELECT id, slug, display_name, entity_id, sso_url, certificate_pem, metadata_url, domains, attribute_mapping, jit_provisioning, default_redirect_to, extra_authn_context, enabled, created_at, updated_at
		FROM sso_connections
		WHERE slug = $1
	`, normalizeSlug(slug)))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrConnectionNotFound
		}
		return nil, err
	}
	return &connection, nil
}

func (s *PostgresStore) GetConnectionByDomain(ctx context.Context, domain string) (*Connection, error) {
	domain = normalizeDomain(domain)
	connection, err := scanConnection(s.pool.QueryRow(ctx, `
		SELECT id, slug, display_name, entity_id, sso_url, certificate_pem, metadata_url, domains, attribute_mapping, jit_provisioning, default_redirect_to, extra_authn_context, enabled, created_at, updated_at
		FROM sso_connections
		WHERE domains ? $1
		ORDER BY display_name ASC
		LIMIT 1
	`, domain))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrConnectionNotFound
		}
		return nil, err
	}
	return &connection, nil
}

func (s *PostgresStore) UpsertConnection(ctx context.Context, connection Connection) (*Connection, error) {
	domainsJSON, err := json.Marshal(connection.Domains)
	if err != nil {
		return nil, err
	}
	mappingJSON, err := json.Marshal(connection.AttributeMapping)
	if err != nil {
		return nil, err
	}
	extraJSON, err := json.Marshal(connection.ExtraAuthnContext)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	var (
		id        uuid.UUID
		createdAt time.Time
		updatedAt time.Time
	)
	err = s.pool.QueryRow(ctx, `
		INSERT INTO sso_connections (
			id, slug, display_name, entity_id, sso_url, certificate_pem, metadata_url, domains, attribute_mapping, jit_provisioning, default_redirect_to, extra_authn_context, enabled, created_at, updated_at
		) VALUES (
			COALESCE(NULLIF($1::text, '')::uuid, gen_random_uuid()), $2, $3, $4, $5, $6, $7, $8::jsonb, $9::jsonb, $10, $11, $12::jsonb, $13, $14, $15
		)
		ON CONFLICT (slug) DO UPDATE SET
			display_name = EXCLUDED.display_name,
			entity_id = EXCLUDED.entity_id,
			sso_url = EXCLUDED.sso_url,
			certificate_pem = EXCLUDED.certificate_pem,
			metadata_url = EXCLUDED.metadata_url,
			domains = EXCLUDED.domains,
			attribute_mapping = EXCLUDED.attribute_mapping,
			jit_provisioning = EXCLUDED.jit_provisioning,
			default_redirect_to = EXCLUDED.default_redirect_to,
			extra_authn_context = EXCLUDED.extra_authn_context,
			enabled = EXCLUDED.enabled,
			updated_at = EXCLUDED.updated_at
		RETURNING id, created_at, updated_at
	`,
		uuidText(connection.ID),
		normalizeSlug(connection.Slug),
		strings.TrimSpace(connection.DisplayName),
		strings.TrimSpace(connection.EntityID),
		strings.TrimSpace(connection.SSOURL),
		strings.TrimSpace(connection.CertificatePEM),
		strings.TrimSpace(connection.MetadataURL),
		domainsJSON,
		mappingJSON,
		connection.JITProvisioning,
		strings.TrimSpace(connection.DefaultRedirectTo),
		extraJSON,
		connection.Enabled,
		now,
		now,
	).Scan(&id, &createdAt, &updatedAt)
	if err != nil {
		return nil, err
	}
	connection.ID = id
	connection.CreatedAt = createdAt
	connection.UpdatedAt = updatedAt
	return &connection, nil
}

func (s *PostgresStore) DeleteConnection(ctx context.Context, slug string) error {
	result, err := s.pool.Exec(ctx, `DELETE FROM sso_connections WHERE slug = $1`, normalizeSlug(slug))
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return ErrConnectionNotFound
	}
	return nil
}

func (s *PostgresStore) CreateAuthRequest(ctx context.Context, requestID, connectionSlug string, expiresAt time.Time) error {
	requestID = strings.TrimSpace(requestID)
	connectionSlug = normalizeSlug(connectionSlug)
	if requestID == "" || connectionSlug == "" || !expiresAt.After(time.Now().UTC()) {
		return ErrAuthRequestConflict
	}
	result, err := s.pool.Exec(ctx, `
		WITH pruned AS (
			DELETE FROM sso_auth_requests
			WHERE expires_at <= CURRENT_TIMESTAMP
		)
		INSERT INTO sso_auth_requests (request_id, connection_slug, expires_at)
		VALUES ($1, $2, $3)
		ON CONFLICT (request_id) DO NOTHING
	`, requestID, connectionSlug, expiresAt.UTC())
	if err != nil {
		return err
	}
	if result.RowsAffected() != 1 {
		return ErrAuthRequestConflict
	}
	return nil
}

func (s *PostgresStore) ConsumeAuthRequest(ctx context.Context, requestID, connectionSlug string, now time.Time) (bool, error) {
	requestID = strings.TrimSpace(requestID)
	connectionSlug = normalizeSlug(connectionSlug)
	if requestID == "" || connectionSlug == "" {
		return false, nil
	}
	var consumed bool
	err := s.pool.QueryRow(ctx, `
		UPDATE sso_auth_requests
		SET consumed_at = $3
		WHERE request_id = $1
			AND connection_slug = $2
			AND consumed_at IS NULL
			AND expires_at > $3
		RETURNING TRUE
	`, requestID, connectionSlug, now.UTC()).Scan(&consumed)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return consumed, nil
}

func scanConnection(scanner interface{ Scan(dest ...any) error }) (Connection, error) {
	var (
		connection  Connection
		domainsJSON []byte
		mappingJSON []byte
		extraJSON   []byte
	)
	err := scanner.Scan(
		&connection.ID,
		&connection.Slug,
		&connection.DisplayName,
		&connection.EntityID,
		&connection.SSOURL,
		&connection.CertificatePEM,
		&connection.MetadataURL,
		&domainsJSON,
		&mappingJSON,
		&connection.JITProvisioning,
		&connection.DefaultRedirectTo,
		&extraJSON,
		&connection.Enabled,
		&connection.CreatedAt,
		&connection.UpdatedAt,
	)
	if err != nil {
		return Connection{}, err
	}
	if err := json.Unmarshal(domainsJSON, &connection.Domains); err != nil {
		return Connection{}, err
	}
	if err := json.Unmarshal(mappingJSON, &connection.AttributeMapping); err != nil {
		return Connection{}, err
	}
	if err := json.Unmarshal(extraJSON, &connection.ExtraAuthnContext); err != nil {
		return Connection{}, err
	}
	if connection.Domains == nil {
		connection.Domains = []string{}
	}
	if connection.ExtraAuthnContext == nil {
		connection.ExtraAuthnContext = map[string]string{}
	}
	return connection, nil
}

func uuidText(id uuid.UUID) string {
	if id == uuid.Nil {
		return ""
	}
	return id.String()
}
