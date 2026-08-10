package graphql

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	analytics "github.com/aegion/aegion/modules/analytics"
	analyticsstore "github.com/aegion/aegion/modules/analytics/store"
)

// DuckDBStore is the production Store implementation. It uses only bound
// parameters and the analytics tables created by the module migrator.
type DuckDBStore struct {
	db      *analyticsstore.DuckDB
	backend analyticsstore.StorageBackend
	schema  func(context.Context) error
}

// NewDuckDBStore creates a Store backed by the durable analytics database.
// backend and schema are included in health checks so GraphQL readiness cannot
// outlive either durable dependency.
func NewDuckDBStore(db *analyticsstore.DuckDB, backend analyticsstore.StorageBackend, schema func(context.Context) error) (*DuckDBStore, error) {
	if db == nil {
		return nil, errors.New("analytics DuckDB is required")
	}
	if backend == nil {
		return nil, errors.New("analytics storage backend is required")
	}
	if schema == nil {
		return nil, errors.New("analytics schema check is required")
	}
	return &DuckDBStore{db: db, backend: backend, schema: schema}, nil
}

func (s *DuckDBStore) GetEvent(ctx context.Context, id string) (*analytics.Event, error) {
	row := s.db.QueryRow(ctx, `SELECT id, category, event_type, data, user_id, session_id, created_at, updated_at FROM analytics_events WHERE id = ?`, id)
	return scanEvent(row)
}

func (s *DuckDBStore) ListEvents(ctx context.Context, filter *EventFilter, limit, offset int) ([]*analytics.Event, int, error) {
	if limit < 1 || limit > 1000 {
		return nil, 0, fmt.Errorf("event limit must be between 1 and 1000")
	}
	if offset < 0 {
		return nil, 0, errors.New("event offset must not be negative")
	}
	where, args, err := eventWhere(filter)
	if err != nil {
		return nil, 0, err
	}
	countSQL := `SELECT COUNT(*) FROM analytics_events` + where
	var total int
	if err := s.db.QueryRow(ctx, countSQL, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count analytics events: %w", err)
	}
	rows, err := s.db.Query(ctx, `SELECT id, category, event_type, data, user_id, session_id, created_at, updated_at FROM analytics_events`+where+` ORDER BY created_at DESC, id DESC LIMIT ? OFFSET ?`, append(args, limit, offset)...)
	if err != nil {
		return nil, 0, fmt.Errorf("list analytics events: %w", err)
	}
	defer rows.Close()
	events := make([]*analytics.Event, 0, limit)
	for rows.Next() {
		event, err := scanEvent(rows)
		if err != nil {
			return nil, 0, err
		}
		events = append(events, event)
	}
	return events, total, rows.Err()
}

func (s *DuckDBStore) CountEvents(ctx context.Context) (int, error) {
	var count int
	if err := s.db.QueryRow(ctx, `SELECT COUNT(*) FROM analytics_events`).Scan(&count); err != nil {
		return 0, fmt.Errorf("count analytics events: %w", err)
	}
	return count, nil
}

func (s *DuckDBStore) CreateDashboard(ctx context.Context, dashboard *analytics.Dashboard) (*analytics.Dashboard, error) {
	if dashboard == nil || strings.TrimSpace(dashboard.ID) == "" || strings.TrimSpace(dashboard.OwnerID) == "" || strings.TrimSpace(dashboard.Name) == "" {
		return nil, errors.New("dashboard id, owner, and name are required")
	}
	config, err := json.Marshal(nonNilMap(dashboard.Config))
	if err != nil {
		return nil, fmt.Errorf("encode dashboard config: %w", err)
	}
	if _, err := s.db.Exec(ctx, `INSERT INTO analytics_dashboards (id, name, description, config, owner_id, public, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, dashboard.ID, dashboard.Name, dashboard.Description, string(config), dashboard.OwnerID, dashboard.Public, dashboard.CreatedAt.UTC(), dashboard.UpdatedAt.UTC()); err != nil {
		return nil, fmt.Errorf("create dashboard: %w", err)
	}
	return s.GetDashboard(ctx, dashboard.ID)
}

func (s *DuckDBStore) GetDashboard(ctx context.Context, id string) (*analytics.Dashboard, error) {
	return scanDashboard(s.db.QueryRow(ctx, `SELECT id, name, description, config, owner_id, public, created_at, updated_at FROM analytics_dashboards WHERE id = ?`, id))
}

func (s *DuckDBStore) ListDashboards(ctx context.Context, ownerID *string, public *bool) ([]*analytics.Dashboard, error) {
	clauses, args := make([]string, 0, 2), make([]interface{}, 0, 2)
	if ownerID != nil {
		clauses, args = append(clauses, "owner_id = ?"), append(args, *ownerID)
	}
	if public != nil {
		clauses, args = append(clauses, "public = ?"), append(args, *public)
	}
	query := `SELECT id, name, description, config, owner_id, public, created_at, updated_at FROM analytics_dashboards`
	if len(clauses) > 0 {
		query += " WHERE " + strings.Join(clauses, " AND ")
	}
	query += " ORDER BY updated_at DESC, id DESC"
	rows, err := s.db.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list dashboards: %w", err)
	}
	defer rows.Close()
	items := make([]*analytics.Dashboard, 0)
	for rows.Next() {
		item, err := scanDashboard(rows)
		if err != nil { return nil, err }
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *DuckDBStore) UpdateDashboard(ctx context.Context, dashboard *analytics.Dashboard) (*analytics.Dashboard, error) {
	if dashboard == nil || strings.TrimSpace(dashboard.ID) == "" || strings.TrimSpace(dashboard.OwnerID) == "" || strings.TrimSpace(dashboard.Name) == "" {
		return nil, errors.New("dashboard id, owner, and name are required")
	}
	config, err := json.Marshal(nonNilMap(dashboard.Config))
	if err != nil { return nil, fmt.Errorf("encode dashboard config: %w", err) }
	result, err := s.db.Exec(ctx, `UPDATE analytics_dashboards SET name = ?, description = ?, config = ?, public = ?, updated_at = ? WHERE id = ?`, dashboard.Name, dashboard.Description, string(config), dashboard.Public, dashboard.UpdatedAt.UTC(), dashboard.ID)
	if err != nil { return nil, fmt.Errorf("update dashboard: %w", err) }
	changed, err := result.RowsAffected()
	if err != nil { return nil, fmt.Errorf("check dashboard update: %w", err) }
	if changed != 1 { return nil, nil }
	return s.GetDashboard(ctx, dashboard.ID)
}

func (s *DuckDBStore) DeleteDashboard(ctx context.Context, id string) error {
	result, err := s.db.Exec(ctx, `DELETE FROM analytics_dashboards WHERE id = ?`, id)
	if err != nil { return fmt.Errorf("delete dashboard: %w", err) }
	changed, err := result.RowsAffected()
	if err != nil { return fmt.Errorf("check dashboard deletion: %w", err) }
	if changed != 1 { return errors.New("dashboard not found") }
	return nil
}

func (s *DuckDBStore) CreateQuery(ctx context.Context, query *analytics.Query) (*analytics.Query, error) {
	if query == nil || strings.TrimSpace(query.ID) == "" || strings.TrimSpace(query.OwnerID) == "" || strings.TrimSpace(query.Name) == "" || strings.TrimSpace(query.SQL) == "" {
		return nil, errors.New("query id, owner, name, and sql are required")
	}
	if _, err := s.db.Exec(ctx, `INSERT INTO analytics_queries (id, name, description, sql, owner_id, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?)`, query.ID, query.Name, query.Description, query.SQL, query.OwnerID, query.CreatedAt.UTC(), query.UpdatedAt.UTC()); err != nil {
		return nil, fmt.Errorf("create query: %w", err)
	}
	return s.GetQuery(ctx, query.ID)
}

func (s *DuckDBStore) GetQuery(ctx context.Context, id string) (*analytics.Query, error) {
	return scanQuery(s.db.QueryRow(ctx, `SELECT id, name, description, sql, owner_id, created_at, updated_at FROM analytics_queries WHERE id = ?`, id))
}

func (s *DuckDBStore) ListQueries(ctx context.Context, ownerID *string) ([]*analytics.Query, error) {
	query, args := `SELECT id, name, description, sql, owner_id, created_at, updated_at FROM analytics_queries`, []interface{}(nil)
	if ownerID != nil { query, args = query+` WHERE owner_id = ?`, []interface{}{*ownerID} }
	rows, err := s.db.Query(ctx, query+` ORDER BY updated_at DESC, id DESC`, args...)
	if err != nil { return nil, fmt.Errorf("list queries: %w", err) }
	defer rows.Close()
	items := make([]*analytics.Query, 0)
	for rows.Next() {
		item, err := scanQuery(rows)
		if err != nil { return nil, err }
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *DuckDBStore) DeleteQuery(ctx context.Context, id string) error {
	result, err := s.db.Exec(ctx, `DELETE FROM analytics_queries WHERE id = ?`, id)
	if err != nil { return fmt.Errorf("delete query: %w", err) }
	changed, err := result.RowsAffected()
	if err != nil { return fmt.Errorf("check query deletion: %w", err) }
	if changed != 1 { return errors.New("query not found") }
	return nil
}

func (s *DuckDBStore) ListMetrics(ctx context.Context, category *string) ([]*analytics.Metric, error) {
	query, args := `SELECT id, name, category, value, unit, created_at, updated_at FROM analytics_metrics`, []interface{}(nil)
	if category != nil { query, args = query+` WHERE category = ?`, []interface{}{*category} }
	rows, err := s.db.Query(ctx, query+` ORDER BY created_at DESC, id DESC`, args...)
	if err != nil { return nil, fmt.Errorf("list metrics: %w", err) }
	defer rows.Close()
	items := make([]*analytics.Metric, 0)
	for rows.Next() {
		var item analytics.Metric
		var unit sql.NullString
		if err := rows.Scan(&item.ID, &item.Name, &item.Category, &item.Value, &unit, &item.CreatedAt, &item.UpdatedAt); err != nil { return nil, fmt.Errorf("scan metric: %w", err) }
		item.Unit = unit.String
		items = append(items, &item)
	}
	return items, rows.Err()
}

func (s *DuckDBStore) CreateWebhook(ctx context.Context, webhook *analytics.Webhook) (*analytics.Webhook, error) {
	if webhook == nil || strings.TrimSpace(webhook.ID) == "" || strings.TrimSpace(webhook.URL) == "" || len(webhook.EventTypes) != 1 || strings.TrimSpace(webhook.EventTypes[0]) == "" || strings.TrimSpace(webhook.Secret) == "" {
		return nil, errors.New("webhook id, url, one event type, and secret are required")
	}
	if _, err := s.db.Exec(ctx, `INSERT INTO analytics_webhooks (id, url, event_type, secret, active, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?)`, webhook.ID, webhook.URL, webhook.EventTypes[0], webhook.Secret, webhook.Active, webhook.CreatedAt.UTC(), webhook.UpdatedAt.UTC()); err != nil {
		return nil, fmt.Errorf("create webhook: %w", err)
	}
	return webhook, nil
}

func (s *DuckDBStore) GetHealth(ctx context.Context) (*analytics.HealthStatus, error) {
	duckHealthy := s.db.Health(ctx) == nil
	storageHealthy := s.backend.Health(ctx) == nil
	migrationHealthy := s.schema(ctx) == nil
	status := "healthy"
	if !duckHealthy || !storageHealthy || !migrationHealthy { status = "degraded" }
	return &analytics.HealthStatus{DuckDB: duckHealthy, Storage: storageHealthy, Migrations: migrationHealthy, LastCheckTime: time.Now().UTC(), Status: status}, nil
}

// ExecuteSQL exists for the Store contract but is intentionally not exposed by
// the GraphQL schema. The resolver keeps arbitrary SQL execution disabled.
func (s *DuckDBStore) ExecuteSQL(ctx context.Context, statement string, timeout time.Duration) ([]map[string]interface{}, error) {
	if timeout <= 0 || timeout > 30*time.Second { return nil, errors.New("sql timeout must be between 1ns and 30s") }
	if !strings.HasPrefix(strings.ToUpper(strings.TrimSpace(statement)), "SELECT") || strings.Contains(statement, ";") {
		return nil, errors.New("only one SELECT statement is allowed")
	}
	queryCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return s.db.ExecuteSQL(queryCtx, statement)
}

type rowScanner interface { Scan(...interface{}) error }

func scanEvent(row rowScanner) (*analytics.Event, error) {
	var item analytics.Event
	var rawData []byte
	var userID, sessionID sql.NullString
	if err := row.Scan(&item.ID, &item.Category, &item.EventType, &rawData, &userID, &sessionID, &item.CreatedAt, &item.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) { return nil, nil }
		return nil, fmt.Errorf("scan analytics event: %w", err)
	}
	if len(rawData) == 0 { rawData = []byte(`{}`) }
	if err := json.Unmarshal(rawData, &item.Data); err != nil { return nil, fmt.Errorf("decode event data: %w", err) }
	if userID.Valid { value := userID.String; item.UserID = &value }
	if sessionID.Valid { value := sessionID.String; item.SessionID = &value }
	return &item, nil
}

func scanDashboard(row rowScanner) (*analytics.Dashboard, error) {
	var item analytics.Dashboard
	var rawConfig []byte
	if err := row.Scan(&item.ID, &item.Name, &item.Description, &rawConfig, &item.OwnerID, &item.Public, &item.CreatedAt, &item.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) { return nil, nil }
		return nil, fmt.Errorf("scan dashboard: %w", err)
	}
	if len(rawConfig) == 0 { rawConfig = []byte(`{}`) }
	if err := json.Unmarshal(rawConfig, &item.Config); err != nil { return nil, fmt.Errorf("decode dashboard config: %w", err) }
	return &item, nil
}

func scanQuery(row rowScanner) (*analytics.Query, error) {
	var item analytics.Query
	if err := row.Scan(&item.ID, &item.Name, &item.Description, &item.SQL, &item.OwnerID, &item.CreatedAt, &item.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) { return nil, nil }
		return nil, fmt.Errorf("scan saved query: %w", err)
	}
	return &item, nil
}

func eventWhere(filter *EventFilter) (string, []interface{}, error) {
	if filter == nil { return "", nil, nil }
	clauses, args := make([]string, 0, 7), make([]interface{}, 0, 7)
	add := func(column string, value *string) { if value != nil { clauses, args = append(clauses, column+" = ?"), append(args, *value) } }
	add("event_type", filter.EventType); add("category", filter.Category); add("user_id", filter.UserID)
	if filter.After != nil {
		value, err := parseGraphQLTime(*filter.After); if err != nil { return "", nil, fmt.Errorf("invalid event after filter: %w", err) }
		clauses, args = append(clauses, "created_at >= ?"), append(args, value)
	}
	if filter.Before != nil {
		value, err := parseGraphQLTime(*filter.Before); if err != nil { return "", nil, fmt.Errorf("invalid event before filter: %w", err) }
		clauses, args = append(clauses, "created_at <= ?"), append(args, value)
	}
	if tr := filter.TimeRange; tr != nil {
		if tr.Start != nil { clauses, args = append(clauses, "created_at >= ?"), append(args, tr.Start.UTC()) }
		if tr.End != nil { clauses, args = append(clauses, "created_at <= ?"), append(args, tr.End.UTC()) }
		if tr.Unit != nil || tr.Value != nil {
			if tr.Unit == nil || tr.Value == nil || *tr.Value <= 0 { return "", nil, errors.New("time range unit and positive value are required together") }
			duration, err := graphQLDuration(*tr.Unit, *tr.Value); if err != nil { return "", nil, err }
			clauses, args = append(clauses, "created_at >= ?"), append(args, time.Now().UTC().Add(-duration))
		}
	}
	if len(clauses) == 0 { return "", args, nil }
	return " WHERE " + strings.Join(clauses, " AND "), args, nil
}

func parseGraphQLTime(value string) (time.Time, error) { return time.Parse(time.RFC3339, strings.TrimSpace(value)) }
func graphQLDuration(unit TimeUnit, value int) (time.Duration, error) {
	if value > 3660 { return 0, errors.New("time range value exceeds 3660") }
	switch unit {
	case TimeUnitHour: return time.Duration(value) * time.Hour, nil
	case TimeUnitDay: return time.Duration(value) * 24 * time.Hour, nil
	case TimeUnitWeek: return time.Duration(value) * 7 * 24 * time.Hour, nil
	case TimeUnitMonth: return time.Duration(value) * 30 * 24 * time.Hour, nil
	case TimeUnitYear: return time.Duration(value) * 365 * 24 * time.Hour, nil
	default: return 0, errors.New("invalid time range unit")
	}
}
func nonNilMap(value map[string]interface{}) map[string]interface{} { if value == nil { return map[string]interface{}{} }; return value }
