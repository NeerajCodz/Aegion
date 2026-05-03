package dashboards

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"
)

// Manager handles dashboard operations including CRUD, metrics computation, and real-time updates.
type Manager struct {
	db       *sql.DB
	cache    map[string]*QueryResult
	cacheMu  sync.RWMutex
	cacheTTL map[string]time.Time
	cacheAt  map[string]time.Time
	logger   *slog.Logger
	config   DashboardConfig
}

// NewManager creates a new dashboard manager.
func NewManager(db *sql.DB, logger *slog.Logger, config DashboardConfig) *Manager {
	if logger == nil {
		logger = slog.Default()
	}
	return &Manager{
		db:       db,
		cache:    make(map[string]*QueryResult),
		cacheTTL: make(map[string]time.Time),
		cacheAt:  make(map[string]time.Time),
		logger:   logger.With("component", "dashboard-manager"),
		config:   config,
	}
}

// CreateDashboard creates a new custom dashboard.
func (m *Manager) CreateDashboard(ctx context.Context, dashboard *Dashboard) (*Dashboard, error) {
	if dashboard.ID == "" {
		dashboard.ID = generateID()
	}
	if dashboard.OwnerID == nil {
		return nil, fmt.Errorf("owner_id is required for custom dashboards")
	}

	dashboard.CreatedAt = time.Now()
	dashboard.UpdatedAt = time.Now()

	query := `
		INSERT INTO analytics_dashboards (id, name, description, config, owner_id, public, pinned, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		RETURNING id, name, description, config, owner_id, public, pinned, created_at, updated_at
	`

	configJSON, err := marshalJSON(buildDashboardConfigPayload(dashboard))
	if err != nil {
		return nil, fmt.Errorf("failed to marshal config: %w", err)
	}

	err = m.db.QueryRowContext(ctx, query,
		dashboard.ID,
		dashboard.Name,
		dashboard.Description,
		configJSON,
		dashboard.OwnerID,
		dashboard.Public,
		dashboard.Pinned,
		dashboard.CreatedAt,
		dashboard.UpdatedAt,
	).Scan(
		&dashboard.ID,
		&dashboard.Name,
		&dashboard.Description,
		&configJSON,
		&dashboard.OwnerID,
		&dashboard.Public,
		&dashboard.Pinned,
		&dashboard.CreatedAt,
		&dashboard.UpdatedAt,
	)
	if err != nil {
		m.logger.ErrorContext(ctx, "failed to create dashboard", "error", err)
		return nil, fmt.Errorf("failed to create dashboard: %w", err)
	}

	m.logger.InfoContext(ctx, "dashboard created", "id", dashboard.ID, "name", dashboard.Name)
	return dashboard, nil
}

// GetDashboard retrieves a dashboard by ID.
func (m *Manager) GetDashboard(ctx context.Context, id string) (*Dashboard, error) {
	dashboard := &Dashboard{}

	query := `
		SELECT id, name, description, config, owner_id, public, pinned, created_at, updated_at
		FROM analytics_dashboards
		WHERE id = $1
	`

	var configJSON string
	err := m.db.QueryRowContext(ctx, query, id).Scan(
		&dashboard.ID,
		&dashboard.Name,
		&dashboard.Description,
		&configJSON,
		&dashboard.OwnerID,
		&dashboard.Public,
		&dashboard.Pinned,
		&dashboard.CreatedAt,
		&dashboard.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("dashboard not found")
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get dashboard: %w", err)
	}

	config, err := unmarshalJSON(configJSON)
	if err != nil {
		return nil, fmt.Errorf("failed to parse dashboard config: %w", err)
	}
	applyDashboardConfig(dashboard, config)

	return dashboard, nil
}

// UpdateDashboard updates an existing dashboard.
func (m *Manager) UpdateDashboard(ctx context.Context, id string, updates map[string]interface{}) (*Dashboard, error) {
	dashboard, err := m.GetDashboard(ctx, id)
	if err != nil {
		return nil, err
	}

	// Apply updates
	if name, ok := updates["name"].(string); ok {
		dashboard.Name = name
	}
	if desc, ok := updates["description"].(string); ok {
		dashboard.Description = desc
	}
	if public, ok := updates["public"].(bool); ok {
		dashboard.Public = public
	}
	if pinned, ok := updates["pinned"].(bool); ok {
		dashboard.Pinned = pinned
	}

	dashboard.UpdatedAt = time.Now()

	configJSON, err := marshalJSON(buildDashboardConfigPayload(dashboard))
	if err != nil {
		return nil, fmt.Errorf("failed to marshal dashboard config: %w", err)
	}

	query := `
		UPDATE analytics_dashboards
		SET name = $1, description = $2, config = $3, public = $4, pinned = $5, updated_at = $6
		WHERE id = $7
	`

	_, err = m.db.ExecContext(ctx, query,
		dashboard.Name,
		dashboard.Description,
		configJSON,
		dashboard.Public,
		dashboard.Pinned,
		dashboard.UpdatedAt,
		id,
	)
	if err != nil {
		m.logger.ErrorContext(ctx, "failed to update dashboard", "error", err, "id", id)
		return nil, fmt.Errorf("failed to update dashboard: %w", err)
	}

	m.logger.InfoContext(ctx, "dashboard updated", "id", id)
	return dashboard, nil
}

// DeleteDashboard deletes a dashboard.
func (m *Manager) DeleteDashboard(ctx context.Context, id string) error {
	result, err := m.db.ExecContext(ctx, "DELETE FROM analytics_dashboards WHERE id = $1", id)
	if err != nil {
		return fmt.Errorf("failed to delete dashboard: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to check deletion result: %w", err)
	}

	if rows == 0 {
		return fmt.Errorf("dashboard not found")
	}

	m.logger.InfoContext(ctx, "dashboard deleted", "id", id)
	return nil
}

// ListDashboards lists dashboards with optional filtering.
func (m *Manager) ListDashboards(ctx context.Context, ownerID *string, includeDefault bool) ([]*Dashboard, error) {
	var dashboards []*Dashboard

	query := "SELECT id, name, description, config, owner_id, public, pinned, created_at, updated_at FROM analytics_dashboards WHERE 1=1"
	args := []interface{}{}

	if ownerID != nil {
		query += fmt.Sprintf(" AND (owner_id = $%d OR owner_id IS NULL)", len(args)+1)
		args = append(args, *ownerID)
	}

	rows, err := m.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query dashboards: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		dashboard := &Dashboard{}
		var configJSON string

		err := rows.Scan(
			&dashboard.ID,
			&dashboard.Name,
			&dashboard.Description,
			&configJSON,
			&dashboard.OwnerID,
			&dashboard.Public,
			&dashboard.Pinned,
			&dashboard.CreatedAt,
			&dashboard.UpdatedAt,
		)
		if err != nil {
			m.logger.ErrorContext(ctx, "failed to scan dashboard row", "error", err)
			continue
		}

		config, err := unmarshalJSON(configJSON)
		if err != nil {
			m.logger.ErrorContext(ctx, "failed to parse dashboard config", "error", err, "dashboard_id", dashboard.ID)
			continue
		}
		applyDashboardConfig(dashboard, config)

		if includeDefault || !dashboard.IsDefault {
			dashboards = append(dashboards, dashboard)
		}
	}

	return dashboards, rows.Err()
}

// ExecuteQuery executes a dashboard query with caching.
func (m *Manager) ExecuteQuery(ctx context.Context, queryID string, query *DashboardQuery) (*QueryResult, error) {
	startTime := time.Now()

	// Check cache
	m.cacheMu.RLock()
	if cached, ok := m.cache[queryID]; ok {
		if ttl, exists := m.cacheTTL[queryID]; exists && time.Now().Before(ttl) {
			cachedAt := m.cacheAt[queryID]
			m.cacheMu.RUnlock()
			return cloneQueryResult(cached, true, &cachedAt), nil
		}
	}
	m.cacheMu.RUnlock()

	// Execute query
	rows, err := m.db.QueryContext(ctx, query.SQL)
	if err != nil {
		m.logger.ErrorContext(ctx, "failed to execute query", "error", err, "query_id", queryID)
		return nil, fmt.Errorf("failed to execute query: %w", err)
	}
	defer rows.Close()

	columns, err := rows.Columns()
	if err != nil {
		return nil, fmt.Errorf("failed to get columns: %w", err)
	}

	var data []map[string]interface{}
	for rows.Next() {
		values := make([]interface{}, len(columns))
		valuePtrs := make([]interface{}, len(columns))
		for i := range columns {
			valuePtrs[i] = &values[i]
		}

		err := rows.Scan(valuePtrs...)
		if err != nil {
			m.logger.ErrorContext(ctx, "failed to scan row", "error", err)
			continue
		}

		entry := make(map[string]interface{})
		for i, col := range columns {
			entry[col] = values[i]
		}
		data = append(data, entry)
	}

	executionTime := int(time.Since(startTime).Milliseconds())

	result := &QueryResult{
		QueryID:       queryID,
		Data:          data,
		Columns:       columns,
		RowCount:      len(data),
		ExecutionTime: executionTime,
		FromCache:     false,
	}

	// Cache result
	if query.CacheTTL > 0 {
		m.cacheMu.Lock()
		cachedAt := time.Now()
		m.cache[queryID] = cloneQueryResult(result, false, &cachedAt)
		m.cacheTTL[queryID] = time.Now().Add(time.Duration(query.CacheTTL) * time.Second)
		m.cacheAt[queryID] = cachedAt
		m.cacheMu.Unlock()
	}

	m.logger.DebugContext(ctx, "query executed", "query_id", queryID, "rows", len(data), "duration_ms", executionTime)
	return result, rows.Err()
}

// CreateShare creates a shareable link for a dashboard.
func (m *Manager) CreateShare(ctx context.Context, dashboardID string, expiresIn *time.Duration, readOnly bool) (*DashboardShare, error) {
	share := &DashboardShare{
		ID:          generateID(),
		DashboardID: dashboardID,
		Token:       generateToken(32),
		ReadOnly:    readOnly,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}

	if expiresIn != nil {
		expiresAt := time.Now().Add(*expiresIn)
		share.ExpiresAt = &expiresAt
	}

	query := `
		INSERT INTO analytics_dashboard_shares (id, dashboard_id, token, expires_at, read_only, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`

	_, err := m.db.ExecContext(ctx, query,
		share.ID,
		share.DashboardID,
		share.Token,
		share.ExpiresAt,
		share.ReadOnly,
		share.CreatedAt,
		share.UpdatedAt,
	)
	if err != nil {
		m.logger.ErrorContext(ctx, "failed to create share", "error", err)
		return nil, fmt.Errorf("failed to create share: %w", err)
	}

	m.logger.InfoContext(ctx, "dashboard share created", "dashboard_id", dashboardID, "token", share.Token)
	return share, nil
}

// GetShareByToken retrieves a share by token.
func (m *Manager) GetShareByToken(ctx context.Context, token string) (*DashboardShare, error) {
	share := &DashboardShare{}

	query := `
		SELECT id, dashboard_id, token, expires_at, read_only, created_at, updated_at
		FROM analytics_dashboard_shares
		WHERE token = $1 AND (expires_at IS NULL OR expires_at > NOW())
	`

	err := m.db.QueryRowContext(ctx, query, token).Scan(
		&share.ID,
		&share.DashboardID,
		&share.Token,
		&share.ExpiresAt,
		&share.ReadOnly,
		&share.CreatedAt,
		&share.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("share not found or expired")
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get share: %w", err)
	}

	return share, nil
}

// SaveAlert creates or updates an alert threshold.
func (m *Manager) SaveAlert(ctx context.Context, alert *AlertThreshold) (*AlertThreshold, error) {
	if alert.ID == "" {
		alert.ID = generateID()
	}

	alert.CreatedAt = time.Now()
	alert.UpdatedAt = time.Now()

	query := `
		INSERT INTO analytics_dashboard_alerts (id, dashboard_id, metric_name, operator, threshold, severity_level, enabled, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT (id) DO UPDATE SET
			operator = $4, threshold = $5, severity_level = $6, enabled = $7, updated_at = $9
		RETURNING id, dashboard_id, metric_name, operator, threshold, severity_level, enabled, created_at, updated_at
	`

	err := m.db.QueryRowContext(ctx, query,
		alert.ID,
		alert.DashboardID,
		alert.MetricName,
		alert.Operator,
		alert.Threshold,
		alert.SeverityLevel,
		alert.Enabled,
		alert.CreatedAt,
		alert.UpdatedAt,
	).Scan(
		&alert.ID,
		&alert.DashboardID,
		&alert.MetricName,
		&alert.Operator,
		&alert.Threshold,
		&alert.SeverityLevel,
		&alert.Enabled,
		&alert.CreatedAt,
		&alert.UpdatedAt,
	)
	if err != nil {
		m.logger.ErrorContext(ctx, "failed to save alert", "error", err)
		return nil, fmt.Errorf("failed to save alert: %w", err)
	}

	m.logger.InfoContext(ctx, "alert saved", "alert_id", alert.ID, "metric", alert.MetricName)
	return alert, nil
}

// ClearCache clears the query cache.
func (m *Manager) ClearCache() {
	m.cacheMu.Lock()
	defer m.cacheMu.Unlock()

	m.cache = make(map[string]*QueryResult)
	m.cacheTTL = make(map[string]time.Time)
	m.cacheAt = make(map[string]time.Time)
	m.logger.Info("dashboard cache cleared")
}

// Helper functions

func generateID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func generateToken(length int) string {
	b := make([]byte, length)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func marshalJSON(v interface{}) (string, error) {
	body, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return string(body), nil
}

func unmarshalJSON(s string) (map[string]interface{}, error) {
	if strings.TrimSpace(s) == "" {
		return map[string]interface{}{}, nil
	}

	var result map[string]interface{}
	if err := json.Unmarshal([]byte(s), &result); err != nil {
		return nil, err
	}
	if result == nil {
		return map[string]interface{}{}, nil
	}
	return result, nil
}

func parseComponents(data []interface{}) []Component {
	var components []Component
	for _, item := range data {
		if m, ok := item.(map[string]interface{}); ok {
			c := Component{
				ID:          stringValue(m["id"]),
				Type:        stringValue(m["type"]),
				Title:       stringValue(m["title"]),
				Description: stringValue(m["description"]),
				QueryID:     stringValue(m["query_id"]),
				TimeRange:   stringValue(m["time_range"]),
				GridCol:     intValue(m["grid_col"]),
				GridRow:     intValue(m["grid_row"]),
				GridWidth:   intValue(m["grid_width"]),
				GridHeight:  intValue(m["grid_height"]),
				Config:      mapValue(m["config"]),
			}
			if metrics, ok := m["metrics"].([]interface{}); ok {
				c.Metrics = stringSlice(metrics)
			}
			components = append(components, c)
		}
	}
	return components
}

func buildDashboardConfigPayload(dashboard *Dashboard) map[string]interface{} {
	return map[string]interface{}{
		"category":         dashboard.Category,
		"is_default":       dashboard.IsDefault,
		"layout":           dashboard.Layout,
		"refresh_interval": dashboard.RefreshInterval,
		"components":       dashboard.Components,
		"config":           dashboard.Config,
	}
}

func applyDashboardConfig(dashboard *Dashboard, config map[string]interface{}) {
	dashboard.Category = stringValue(config["category"])
	dashboard.IsDefault = boolValue(config["is_default"])
	dashboard.Layout = stringValue(config["layout"])
	if refreshInterval := intValue(config["refresh_interval"]); refreshInterval > 0 {
		dashboard.RefreshInterval = refreshInterval
	}
	if components, ok := config["components"].([]interface{}); ok {
		dashboard.Components = parseComponents(components)
	}
	if cfg, ok := config["config"].(map[string]interface{}); ok {
		dashboard.Config = cfg
	}
}

func cloneQueryResult(result *QueryResult, fromCache bool, cachedAt *time.Time) *QueryResult {
	if result == nil {
		return nil
	}

	cloned := &QueryResult{
		QueryID:       result.QueryID,
		Columns:       append([]string(nil), result.Columns...),
		RowCount:      result.RowCount,
		ExecutionTime: result.ExecutionTime,
		FromCache:     fromCache,
	}
	if cachedAt != nil {
		cloned.CachedAt = cachedAt
	}
	if len(result.Data) > 0 {
		cloned.Data = make([]map[string]interface{}, 0, len(result.Data))
		for _, row := range result.Data {
			clonedRow := make(map[string]interface{}, len(row))
			for key, value := range row {
				clonedRow[key] = value
			}
			cloned.Data = append(cloned.Data, clonedRow)
		}
	}

	return cloned
}

func stringValue(value interface{}) string {
	if value == nil {
		return ""
	}
	return fmt.Sprintf("%v", value)
}

func intValue(value interface{}) int {
	switch typed := value.(type) {
	case int:
		return typed
	case int32:
		return int(typed)
	case int64:
		return int(typed)
	case float32:
		return int(typed)
	case float64:
		return int(typed)
	default:
		return 0
	}
}

func boolValue(value interface{}) bool {
	typed, ok := value.(bool)
	return ok && typed
}

func mapValue(value interface{}) map[string]interface{} {
	typed, ok := value.(map[string]interface{})
	if !ok || typed == nil {
		return map[string]interface{}{}
	}
	return typed
}

func stringSlice(values []interface{}) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		result = append(result, stringValue(value))
	}
	return result
}
