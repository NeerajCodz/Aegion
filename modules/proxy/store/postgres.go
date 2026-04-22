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

func (s *PostgresStore) ListUpstreams(ctx context.Context) ([]Upstream, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, name, url, health_check, health_check_expected_body, timeout, max_connections, headers, circuit_breaker, enabled, created_at, updated_at
		FROM proxy_upstreams
		ORDER BY name ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	upstreams := make([]Upstream, 0)
	for rows.Next() {
		upstream, err := scanUpstream(rows)
		if err != nil {
			return nil, err
		}
		upstreams = append(upstreams, upstream)
	}
	return upstreams, rows.Err()
}

func (s *PostgresStore) GetUpstreamByName(ctx context.Context, name string) (*Upstream, error) {
	upstream, err := scanUpstream(s.pool.QueryRow(ctx, `
		SELECT id, name, url, health_check, health_check_expected_body, timeout, max_connections, headers, circuit_breaker, enabled, created_at, updated_at
		FROM proxy_upstreams
		WHERE name = $1
	`, normalizeName(name)))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrUpstreamNotFound
		}
		return nil, err
	}
	return &upstream, nil
}

func (s *PostgresStore) UpsertUpstream(ctx context.Context, upstream Upstream) (*Upstream, error) {
	headersJSON, err := json.Marshal(cloneStringMap(upstream.Headers))
	if err != nil {
		return nil, err
	}
	cbJSON, err := json.Marshal(normalizeCircuitBreaker(upstream.CircuitBreaker))
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
		INSERT INTO proxy_upstreams (
			id, name, url, health_check, health_check_expected_body, timeout, max_connections, headers, circuit_breaker, enabled, created_at, updated_at
		) VALUES (
			COALESCE(NULLIF($1::text, '')::uuid, gen_random_uuid()),
			$2, $3, $4, $5, $6, $7, $8::jsonb, $9::jsonb, $10, $11, $12
		)
		ON CONFLICT (name) DO UPDATE SET
			url = EXCLUDED.url,
			health_check = EXCLUDED.health_check,
			health_check_expected_body = EXCLUDED.health_check_expected_body,
			timeout = EXCLUDED.timeout,
			max_connections = EXCLUDED.max_connections,
			headers = EXCLUDED.headers,
			circuit_breaker = EXCLUDED.circuit_breaker,
			enabled = EXCLUDED.enabled,
			updated_at = EXCLUDED.updated_at
		RETURNING id, created_at, updated_at
	`,
		uuidText(upstream.ID),
		normalizeName(upstream.Name),
		strings.TrimSpace(upstream.URL),
		strings.TrimSpace(upstream.HealthCheck),
		strings.TrimSpace(upstream.HealthCheckExpectedBody),
		strings.TrimSpace(upstream.Timeout),
		upstream.MaxConnections,
		headersJSON,
		cbJSON,
		upstream.Enabled,
		now,
		now,
	).Scan(&id, &createdAt, &updatedAt)
	if err != nil {
		return nil, err
	}

	upstream.ID = id
	upstream.CreatedAt = createdAt
	upstream.UpdatedAt = updatedAt
	return &upstream, nil
}

func (s *PostgresStore) DeleteUpstream(ctx context.Context, name string) error {
	name = normalizeName(name)
	var routeCount int
	if err := s.pool.QueryRow(ctx, `SELECT COUNT(*) FROM proxy_routes WHERE target = $1`, name).Scan(&routeCount); err != nil {
		return err
	}
	if routeCount > 0 {
		return ErrUpstreamInUse
	}

	result, err := s.pool.Exec(ctx, `DELETE FROM proxy_upstreams WHERE name = $1`, name)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return ErrUpstreamNotFound
	}
	return nil
}

func (s *PostgresStore) ListRoutes(ctx context.Context) ([]Route, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, path, methods, require_auth, required_aal, capabilities, rate_limit, target, priority, headers, rewrite, enabled, description, created_at, updated_at
		FROM proxy_routes
		ORDER BY priority DESC, id ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	routes := make([]Route, 0)
	for rows.Next() {
		route, err := scanRoute(rows)
		if err != nil {
			return nil, err
		}
		routes = append(routes, route)
	}
	return routes, rows.Err()
}

func (s *PostgresStore) GetRouteByID(ctx context.Context, id string) (*Route, error) {
	route, err := scanRoute(s.pool.QueryRow(ctx, `
		SELECT id, path, methods, require_auth, required_aal, capabilities, rate_limit, target, priority, headers, rewrite, enabled, description, created_at, updated_at
		FROM proxy_routes
		WHERE id = $1
	`, strings.TrimSpace(id)))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrRouteNotFound
		}
		return nil, err
	}
	return &route, nil
}

func (s *PostgresStore) UpsertRoute(ctx context.Context, route Route) (*Route, error) {
	methodsJSON, err := json.Marshal(append([]string(nil), route.Methods...))
	if err != nil {
		return nil, err
	}
	capabilitiesJSON, err := json.Marshal(append([]string(nil), route.Capabilities...))
	if err != nil {
		return nil, err
	}
	rateJSON, err := json.Marshal(normalizeRateLimit(route.RateLimit))
	if err != nil {
		return nil, err
	}
	headersJSON, err := json.Marshal(cloneStringMap(route.Headers))
	if err != nil {
		return nil, err
	}
	rewriteJSON, err := json.Marshal(normalizeRewrite(route.Rewrite))
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	var (
		createdAt time.Time
		updatedAt time.Time
	)
	err = s.pool.QueryRow(ctx, `
		INSERT INTO proxy_routes (
			id, path, methods, require_auth, required_aal, capabilities, rate_limit, target, priority, headers, rewrite, enabled, description, created_at, updated_at
		) VALUES (
			$1, $2, $3::jsonb, $4, $5, $6::jsonb, $7::jsonb, $8, $9, $10::jsonb, $11::jsonb, $12, $13, $14, $15
		)
		ON CONFLICT (id) DO UPDATE SET
			path = EXCLUDED.path,
			methods = EXCLUDED.methods,
			require_auth = EXCLUDED.require_auth,
			required_aal = EXCLUDED.required_aal,
			capabilities = EXCLUDED.capabilities,
			rate_limit = EXCLUDED.rate_limit,
			target = EXCLUDED.target,
			priority = EXCLUDED.priority,
			headers = EXCLUDED.headers,
			rewrite = EXCLUDED.rewrite,
			enabled = EXCLUDED.enabled,
			description = EXCLUDED.description,
			updated_at = EXCLUDED.updated_at
		RETURNING created_at, updated_at
	`,
		strings.TrimSpace(route.ID),
		strings.TrimSpace(route.Path),
		methodsJSON,
		route.RequireAuth,
		strings.TrimSpace(route.RequiredAAL),
		capabilitiesJSON,
		rateJSON,
		normalizeName(route.Target),
		route.Priority,
		headersJSON,
		rewriteJSON,
		route.Enabled,
		strings.TrimSpace(route.Description),
		now,
		now,
	).Scan(&createdAt, &updatedAt)
	if err != nil {
		return nil, err
	}

	route.CreatedAt = createdAt
	route.UpdatedAt = updatedAt
	return &route, nil
}

func (s *PostgresStore) DeleteRoute(ctx context.Context, id string) error {
	result, err := s.pool.Exec(ctx, `DELETE FROM proxy_routes WHERE id = $1`, strings.TrimSpace(id))
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return ErrRouteNotFound
	}
	return nil
}

func scanUpstream(scanner interface{ Scan(dest ...any) error }) (Upstream, error) {
	var (
		upstream Upstream
		headers  []byte
		cb       []byte
	)
	err := scanner.Scan(
		&upstream.ID,
		&upstream.Name,
		&upstream.URL,
		&upstream.HealthCheck,
		&upstream.HealthCheckExpectedBody,
		&upstream.Timeout,
		&upstream.MaxConnections,
		&headers,
		&cb,
		&upstream.Enabled,
		&upstream.CreatedAt,
		&upstream.UpdatedAt,
	)
	if err != nil {
		return Upstream{}, err
	}
	if len(headers) == 0 {
		upstream.Headers = map[string]string{}
	} else if err := json.Unmarshal(headers, &upstream.Headers); err != nil {
		return Upstream{}, err
	}
	var circuitBreaker CircuitBreaker
	if len(cb) > 0 {
		if err := json.Unmarshal(cb, &circuitBreaker); err != nil {
			return Upstream{}, err
		}
		if circuitBreaker != (CircuitBreaker{}) {
			upstream.CircuitBreaker = &circuitBreaker
		}
	}
	return upstream, nil
}

func scanRoute(scanner interface{ Scan(dest ...any) error }) (Route, error) {
	var (
		route        Route
		methods      []byte
		capabilities []byte
		rateLimit    []byte
		headers      []byte
		rewrite      []byte
	)
	err := scanner.Scan(
		&route.ID,
		&route.Path,
		&methods,
		&route.RequireAuth,
		&route.RequiredAAL,
		&capabilities,
		&rateLimit,
		&route.Target,
		&route.Priority,
		&headers,
		&rewrite,
		&route.Enabled,
		&route.Description,
		&route.CreatedAt,
		&route.UpdatedAt,
	)
	if err != nil {
		return Route{}, err
	}
	if len(methods) == 0 {
		route.Methods = []string{}
	} else if err := json.Unmarshal(methods, &route.Methods); err != nil {
		return Route{}, err
	}
	if len(capabilities) == 0 {
		route.Capabilities = []string{}
	} else if err := json.Unmarshal(capabilities, &route.Capabilities); err != nil {
		return Route{}, err
	}
	if len(headers) == 0 {
		route.Headers = map[string]string{}
	} else if err := json.Unmarshal(headers, &route.Headers); err != nil {
		return Route{}, err
	}
	var storedRate RateLimit
	if len(rateLimit) > 0 {
		if err := json.Unmarshal(rateLimit, &storedRate); err != nil {
			return Route{}, err
		}
		if storedRate != (RateLimit{}) {
			route.RateLimit = &storedRate
		}
	}
	var storedRewrite Rewrite
	if len(rewrite) > 0 {
		if err := json.Unmarshal(rewrite, &storedRewrite); err != nil {
			return Route{}, err
		}
		if storedRewrite != (Rewrite{}) {
			route.Rewrite = &storedRewrite
		}
	}
	return route, nil
}

func normalizeCircuitBreaker(in *CircuitBreaker) interface{} {
	if in == nil {
		return map[string]any{}
	}
	return in
}

func normalizeRateLimit(in *RateLimit) interface{} {
	if in == nil {
		return map[string]any{}
	}
	return in
}

func normalizeRewrite(in *Rewrite) interface{} {
	if in == nil {
		return map[string]any{}
	}
	return in
}

func uuidText(id uuid.UUID) string {
	if id == uuid.Nil {
		return ""
	}
	return id.String()
}
