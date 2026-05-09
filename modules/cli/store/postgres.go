package store

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

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

func (s *PostgresStore) SaveRun(ctx context.Context, run CommandRun) error {
	argsJSON, err := json.Marshal(run.Arguments)
	if err != nil {
		return err
	}
	resultJSON, err := json.Marshal(run.Result)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, `
		INSERT INTO cli_command_runs (id, command_name, arguments, result, success, error_message, executed_at)
		VALUES ($1, $2, $3::jsonb, $4::jsonb, $5, $6, $7)
	`, run.ID, run.Command, argsJSON, resultJSON, run.Success, strings.TrimSpace(run.Error), run.ExecutedAt.UTC())
	return err
}

func (s *PostgresStore) ListRuns(ctx context.Context, limit int) ([]CommandRun, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id, command_name, arguments, result, success, error_message, executed_at
		FROM cli_command_runs
		ORDER BY executed_at DESC
		LIMIT $1
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	runs := make([]CommandRun, 0)
	for rows.Next() {
		run, err := scanRun(rows)
		if err != nil {
			return nil, err
		}
		runs = append(runs, run)
	}
	return runs, rows.Err()
}

func (s *PostgresStore) GetRun(ctx context.Context, id string) (*CommandRun, error) {
	run, err := scanRun(s.pool.QueryRow(ctx, `
		SELECT id, command_name, arguments, result, success, error_message, executed_at
		FROM cli_command_runs
		WHERE id = $1
	`, strings.TrimSpace(id)))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrRunNotFound
		}
		return nil, err
	}
	return &run, nil
}

func (s *PostgresStore) StatusSummary(ctx context.Context) (map[string]any, error) {
	return map[string]any{
		"database":            "connected",
		"identities_total":    s.countIfExists(ctx, "core_identities", "SELECT COUNT(*) FROM core_identities WHERE deleted_at IS NULL"),
		"sessions_active":     s.countIfExists(ctx, "core_sessions", "SELECT COUNT(*) FROM core_sessions WHERE active = TRUE AND expires_at > NOW()"),
		"oauth2_clients":      s.countIfExists(ctx, "oauth2_clients", "SELECT COUNT(*) FROM oauth2_clients"),
		"social_providers":    s.countIfExists(ctx, "soc_providers", "SELECT COUNT(*) FROM soc_providers WHERE enabled = TRUE"),
		"proxy_upstreams":     s.countIfExists(ctx, "proxy_upstreams", "SELECT COUNT(*) FROM proxy_upstreams WHERE enabled = TRUE"),
		"proxy_routes":        s.countIfExists(ctx, "proxy_routes", "SELECT COUNT(*) FROM proxy_routes WHERE enabled = TRUE"),
		"recent_command_runs": s.countIfExists(ctx, "cli_command_runs", "SELECT COUNT(*) FROM cli_command_runs"),
	}, nil
}

func (s *PostgresStore) RuntimeConfig(ctx context.Context) (map[string]any, error) {
	proxyConfig := s.systemConfig(ctx, "proxy.settings")
	if identitySigningSecret, ok := proxyConfig["identity_signing_secret"]; ok {
		if secret, ok := identitySigningSecret.(string); ok {
			proxyConfig["identity_signing_secret_set"] = strings.TrimSpace(secret) != ""
		} else {
			proxyConfig["identity_signing_secret_set"] = true
		}
		delete(proxyConfig, "identity_signing_secret")
	}

	return map[string]any{
		"database": "connected",
		"policy":   s.systemConfig(ctx, "policy.settings"),
		"proxy":    proxyConfig,
	}, nil
}

func (s *PostgresStore) CourierSummary(ctx context.Context) (map[string]any, error) {
	if !s.tableExists(ctx, "core_courier_messages") {
		return map[string]any{
			"database":      "connected",
			"queued":        0,
			"processing":    0,
			"delivered":     0,
			"failed":        0,
			"cancelled":     0,
			"retriable":     0,
			"last_activity": "",
		}, nil
	}

	summary := map[string]any{
		"database": "connected",
	}
	for _, status := range []string{"queued", "processing", "delivered", "failed", "cancelled"} {
		var count int64
		if err := s.pool.QueryRow(ctx, `SELECT COUNT(*) FROM core_courier_messages WHERE status = $1`, status).Scan(&count); err == nil {
			summary[status] = count
		}
	}
	var retriable int64
	_ = s.pool.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM core_courier_messages
		WHERE status = 'failed' AND next_retry_at IS NOT NULL AND next_retry_at > NOW()
	`).Scan(&retriable)
	summary["retriable"] = retriable

	var lastActivity time.Time
	if err := s.pool.QueryRow(ctx, `SELECT COALESCE(MAX(updated_at), NOW()) FROM core_courier_messages`).Scan(&lastActivity); err == nil {
		summary["last_activity"] = lastActivity.UTC().Format(time.RFC3339)
	}
	return summary, nil
}

func (s *PostgresStore) countIfExists(ctx context.Context, tableName, query string) int64 {
	if !s.tableExists(ctx, tableName) {
		return 0
	}
	var count int64
	if err := s.pool.QueryRow(ctx, query).Scan(&count); err != nil {
		return 0
	}
	return count
}

func (s *PostgresStore) tableExists(ctx context.Context, tableName string) bool {
	var exists bool
	err := s.pool.QueryRow(ctx, `SELECT to_regclass($1) IS NOT NULL`, "public."+strings.TrimSpace(tableName)).Scan(&exists)
	return err == nil && exists
}

func (s *PostgresStore) systemConfig(ctx context.Context, key string) map[string]any {
	if !s.tableExists(ctx, "core_system_config") {
		return map[string]any{}
	}
	var raw []byte
	if err := s.pool.QueryRow(ctx, `SELECT value::jsonb FROM core_system_config WHERE key = $1`, key).Scan(&raw); err != nil {
		return map[string]any{}
	}
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return map[string]any{}
	}
	return decoded
}

func scanRun(scanner interface{ Scan(dest ...any) error }) (CommandRun, error) {
	var (
		run        CommandRun
		argsJSON   []byte
		resultJSON []byte
	)
	err := scanner.Scan(&run.ID, &run.Command, &argsJSON, &resultJSON, &run.Success, &run.Error, &run.ExecutedAt)
	if err != nil {
		return CommandRun{}, err
	}
	if len(argsJSON) > 0 {
		if err := json.Unmarshal(argsJSON, &run.Arguments); err != nil {
			return CommandRun{}, err
		}
	}
	if len(resultJSON) > 0 {
		if err := json.Unmarshal(resultJSON, &run.Result); err != nil {
			return CommandRun{}, err
		}
	}
	if run.Arguments == nil {
		run.Arguments = map[string]any{}
	}
	if run.Result == nil {
		run.Result = map[string]any{}
	}
	return run, nil
}
