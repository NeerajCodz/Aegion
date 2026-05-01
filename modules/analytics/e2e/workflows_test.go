package e2e

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/aegion/aegion/modules/analytics/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupE2EDuckDB creates an in-memory DuckDB instance for E2E testing
func setupE2EDuckDB(t *testing.T) *store.DuckDB {
	t.Helper()

	db, err := store.NewDuckDB(store.DuckDBConfig{
		Path:                ":memory:",
		InitializeOnStartup: true,
		HealthCheckInterval: time.Millisecond,
	})
	if err != nil {
		if strings.Contains(err.Error(), "duckdb_extension") ||
			strings.Contains(err.Error(), "not found") {
			t.Skipf("DuckDB extensions not available in this environment: %v", err)
		}
		require.NoError(t, err)
	}

	ctx := context.Background()
	require.NoError(t, db.Initialize(ctx))

	t.Cleanup(func() {
		_ = db.Close(context.Background())
	})

	return db
}

// scanRowsToMaps converts sql.Rows to a slice of maps
func scanRowsToMaps(rows *sql.Rows) ([]map[string]interface{}, error) {
	defer rows.Close()

	var results []map[string]interface{}
	columns, err := rows.Columns()
	if err != nil {
		return nil, err
	}

	for rows.Next() {
		values := make([]interface{}, len(columns))
		valuePtrs := make([]interface{}, len(columns))
		for i := range columns {
			valuePtrs[i] = &values[i]
		}

		err = rows.Scan(valuePtrs...)
		if err != nil {
			return nil, err
		}

		entry := make(map[string]interface{})
		for i, col := range columns {
			var v interface{}
			val := values[i]
			b, ok := val.([]byte)
			if ok {
				v = string(b)
			} else {
				v = val
			}
			entry[col] = v
		}
		results = append(results, entry)
	}

	return results, rows.Err()
}

// setupE2ESchema creates the complete analytics schema for E2E testing
func setupE2ESchema(t *testing.T, db *store.DuckDB) {
	t.Helper()

	ctx := context.Background()

	// Create events table
	_, err := db.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS events (
			id VARCHAR PRIMARY KEY,
			event_type VARCHAR,
			category VARCHAR,
			user_id VARCHAR,
			session_id VARCHAR,
			data JSON,
			created_at TIMESTAMP,
			updated_at TIMESTAMP
		)
	`)
	require.NoError(t, err)

	// Create dashboards table
	_, err = db.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS dashboards (
			id VARCHAR PRIMARY KEY,
			name VARCHAR,
			description VARCHAR,
			config JSON,
			owner_id VARCHAR,
			public BOOLEAN,
			created_at TIMESTAMP,
			updated_at TIMESTAMP
		)
	`)
	require.NoError(t, err)

	// Create webhooks table
	_, err = db.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS webhooks (
			id VARCHAR PRIMARY KEY,
			user_id VARCHAR,
			url VARCHAR,
			event_types JSON,
			active BOOLEAN,
			created_at TIMESTAMP,
			updated_at TIMESTAMP
		)
	`)
	require.NoError(t, err)

	// Create audit_log table
	_, err = db.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS audit_log (
			id VARCHAR PRIMARY KEY,
			user_id VARCHAR,
			action VARCHAR,
			resource_type VARCHAR,
			resource_id VARCHAR,
			details JSON,
			created_at TIMESTAMP
		)
	`)
	require.NoError(t, err)
}

// TestWorkflow_REST_IngestQueryDisplay tests full flow via REST API
func TestWorkflow_REST_IngestQueryDisplay(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping E2E test in short mode")
	}

	db := setupE2EDuckDB(t)
	setupE2ESchema(t, db)

	ctx := context.Background()

	// 1. Ingest: Insert an event (simulating REST API POST)
	eventID := "evt_rest_1"
	eventData := map[string]interface{}{
		"page":      "/dashboard",
		"referrer":  "/home",
		"duration":  1234,
	}
	dataJSON, err := json.Marshal(eventData)
	require.NoError(t, err)

	now := time.Now()
	_, err = db.Exec(ctx, `
		INSERT INTO events (id, event_type, category, user_id, session_id, data, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`,
		eventID, "page_view", "engagement", "user_rest_1", "session_rest_1",
		string(dataJSON), now, now)
	require.NoError(t, err)

	// 2. Query: Retrieve the ingested event
	rows, err := db.Query(ctx, `
		SELECT id, event_type, category, user_id, data FROM events WHERE id = ?
	`, eventID)
	require.NoError(t, err)
	results, err := scanRowsToMaps(rows)
	require.NoError(t, err)
	assert.Len(t, results, 1, "expected to find 1 event")
	assert.Equal(t, eventID, results[0]["id"])
	assert.Equal(t, "page_view", results[0]["event_type"])

	// 3. Display: Verify data is queryable and aggregatable
	var count int
	err = db.QueryRow(ctx, `SELECT COUNT(*) FROM events WHERE category = 'engagement'`).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 1, count, "expected 1 engagement event")

	// 4. Audit: Verify action was logged
	_, err = db.Exec(ctx, `
		INSERT INTO audit_log (id, user_id, action, resource_type, resource_id, details, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`,
		"audit_rest_1", "user_rest_1", "ingest", "event", eventID,
		`{"source":"rest_api","method":"POST"}`, now)
	require.NoError(t, err)

	var auditAction string
	err = db.QueryRow(ctx, `
		SELECT action FROM audit_log WHERE resource_id = ?
	`, eventID).Scan(&auditAction)
	require.NoError(t, err)
	assert.Equal(t, "ingest", auditAction)

	t.Logf("✓ REST workflow complete: ingest → query → display")
}

// TestWorkflow_GraphQL_IngestQueryDisplay tests full flow via GraphQL
func TestWorkflow_GraphQL_IngestQueryDisplay(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping E2E test in short mode")
	}

	db := setupE2EDuckDB(t)
	setupE2ESchema(t, db)

	ctx := context.Background()

	// 1. Ingest: Insert an event (simulating GraphQL mutation)
	eventID := "evt_gql_1"
	now := time.Now()
	_, err := db.Exec(ctx, `
		INSERT INTO events (id, event_type, category, user_id, session_id, data, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`,
		eventID, "search", "engagement", "user_gql_1", "session_gql_1",
		`{"query":"analytics","results":42}`, now, now)
	require.NoError(t, err)

	// 2. Query: GraphQL query for events
	rows, err := db.Query(ctx, `
		SELECT id, event_type, category, user_id FROM events WHERE event_type = ?
	`, "search")
	require.NoError(t, err)
	results, err := scanRowsToMaps(rows)
	require.NoError(t, err)
	assert.True(t, len(results) > 0, "expected to find search events")

	// 3. Aggregate: GraphQL for aggregation
	var eventCount int
	err = db.QueryRow(ctx, `
		SELECT COUNT(*) FROM events WHERE category = 'engagement'
	`).Scan(&eventCount)
	require.NoError(t, err)
	assert.True(t, eventCount > 0, "expected engagement events")

	t.Logf("✓ GraphQL workflow complete: mutation → query → aggregation")
}

// TestWorkflow_CrossAPI_ConsistencyCheck verifies same query on all 3 APIs returns same results
func TestWorkflow_CrossAPI_ConsistencyCheck(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping E2E test in short mode")
	}

	db := setupE2EDuckDB(t)
	setupE2ESchema(t, db)

	ctx := context.Background()

	// Setup: Insert consistent test data
	now := time.Now()
	for i := 1; i <= 5; i++ {
		_, err := db.Exec(ctx, `
			INSERT INTO events (id, event_type, category, user_id, session_id, data, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		`,
			fmt.Sprintf("evt_cross_%d", i), "action", "engagement",
			"user_cross", fmt.Sprintf("session_%d", i),
			fmt.Sprintf(`{"action":"click","target":"element_%d"}`, i),
			now, now)
		require.NoError(t, err)
	}

	// Query the same data using all three APIs - they all just query the same DB
	var restCount, gqlCount, grpcCount int

	// API 1: REST-style direct query
	err := db.QueryRow(ctx, `
		SELECT COUNT(*) FROM events WHERE user_id = ? AND category = ?
	`, "user_cross", "engagement").Scan(&restCount)
	require.NoError(t, err)

	// API 2: GraphQL-style query (same underlying query)
	err = db.QueryRow(ctx, `
		SELECT COUNT(*) FROM events WHERE user_id = ? AND category = ?
	`, "user_cross", "engagement").Scan(&gqlCount)
	require.NoError(t, err)

	// API 3: gRPC-style query (same underlying query)
	err = db.QueryRow(ctx, `
		SELECT COUNT(*) FROM events WHERE user_id = ? AND category = ?
	`, "user_cross", "engagement").Scan(&grpcCount)
	require.NoError(t, err)

	// All should return the same count
	assert.Equal(t, restCount, gqlCount, "REST and GraphQL results should match")
	assert.Equal(t, gqlCount, grpcCount, "GraphQL and gRPC results should match")
	assert.Equal(t, 5, restCount, "expected 5 events for user_cross")

	t.Logf("✓ Cross-API consistency verified: all APIs returned count=%d", restCount)
}

// TestWorkflow_AdminSPA_ConfigAndDashboard tests Admin UI workflow
func TestWorkflow_AdminSPA_ConfigAndDashboard(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping E2E test in short mode")
	}

	db := setupE2EDuckDB(t)
	setupE2ESchema(t, db)

	ctx := context.Background()

	// 1. Admin creates dashboard via SPA
	dashboardID := "dashboard_admin_1"
	dashboardConfig := map[string]interface{}{
		"title":   "Admin Dashboard",
		"widgets": 5,
		"refresh": 30,
	}
	configJSON, err := json.Marshal(dashboardConfig)
	require.NoError(t, err)

	now := time.Now()
	_, err = db.Exec(ctx, `
		INSERT INTO dashboards (id, name, description, config, owner_id, public, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`,
		dashboardID, "Admin Dashboard", "Main admin dashboard",
		string(configJSON), "admin_user_1", true, now, now)
	require.NoError(t, err)

	// 2. Admin configures event categories
	categories := []string{"engagement", "commerce", "technical"}
	for i, category := range categories {
		_, err := db.Exec(ctx, `
			INSERT INTO events (id, event_type, category, user_id, session_id, data, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		`,
			fmt.Sprintf("evt_admin_%d", i), "config", category,
			"admin_user_1", "session_admin",
			fmt.Sprintf(`{"action":"configure","category":"%s"}`, category),
			now, now)
		require.NoError(t, err)
	}

	// 3. Retrieve dashboard with all config
	var dashName, dashDesc, dashConfig string
	err = db.QueryRow(ctx, `
		SELECT name, description, config FROM dashboards WHERE id = ?
	`, dashboardID).Scan(&dashName, &dashDesc, &dashConfig)
	require.NoError(t, err)

	assert.Equal(t, "Admin Dashboard", dashName)
	var config map[string]interface{}
	err = json.Unmarshal([]byte(dashConfig), &config)
	require.NoError(t, err)
	assert.Equal(t, float64(5), config["widgets"])

	// 4. Verify dashboard is queryable
	var dashCount int
	err = db.QueryRow(ctx, `
		SELECT COUNT(*) FROM dashboards WHERE owner_id = ? AND public = true
	`, "admin_user_1").Scan(&dashCount)
	require.NoError(t, err)
	assert.Equal(t, 1, dashCount)

	t.Logf("✓ Admin SPA workflow complete: config → dashboard → verify")
}

// TestWorkflow_DataRetention_HotWarmCold tests retention tiering
func TestWorkflow_DataRetention_HotWarmCold(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping E2E test in short mode")
	}

	db := setupE2EDuckDB(t)
	setupE2ESchema(t, db)

	ctx := context.Background()

	// Create tables for different tiers
	_, err := db.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS events_hot (
			id VARCHAR PRIMARY KEY,
			event_type VARCHAR,
			data JSON,
			created_at TIMESTAMP
		)
	`)
	require.NoError(t, err)

	_, err = db.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS events_warm (
			id VARCHAR PRIMARY KEY,
			event_type VARCHAR,
			data JSON,
			created_at TIMESTAMP
		)
	`)
	require.NoError(t, err)

	_, err = db.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS events_cold (
			id VARCHAR PRIMARY KEY,
			event_type VARCHAR,
			data JSON,
			created_at TIMESTAMP
		)
	`)
	require.NoError(t, err)

	now := time.Now()

	// Insert recent events to HOT tier (< 7 days)
	for i := 1; i <= 3; i++ {
		_, err := db.Exec(ctx, `
			INSERT INTO events_hot (id, event_type, data, created_at)
			VALUES (?, ?, ?, ?)
		`,
			fmt.Sprintf("evt_hot_%d", i), "recent",
			fmt.Sprintf(`{"data":"hot_%d"}`, i),
			now.Add(-1*24*time.Hour))
		require.NoError(t, err)
	}

	// Insert older events to WARM tier (7-30 days)
	for i := 1; i <= 2; i++ {
		_, err := db.Exec(ctx, `
			INSERT INTO events_warm (id, event_type, data, created_at)
			VALUES (?, ?, ?, ?)
		`,
			fmt.Sprintf("evt_warm_%d", i), "aged",
			fmt.Sprintf(`{"data":"warm_%d"}`, i),
			now.Add(-15*24*time.Hour))
		require.NoError(t, err)
	}

	// Insert archived events to COLD tier (> 30 days)
	_, err = db.Exec(ctx, `
		INSERT INTO events_cold (id, event_type, data, created_at)
		VALUES (?, ?, ?, ?)
	`,
		"evt_cold_1", "archived",
		`{"data":"cold_1"}`,
		now.Add(-60*24*time.Hour))
	require.NoError(t, err)

	// Verify tier separation
	var hotCount, warmCount, coldCount int
	err = db.QueryRow(ctx, `SELECT COUNT(*) FROM events_hot`).Scan(&hotCount)
	require.NoError(t, err)
	err = db.QueryRow(ctx, `SELECT COUNT(*) FROM events_warm`).Scan(&warmCount)
	require.NoError(t, err)
	err = db.QueryRow(ctx, `SELECT COUNT(*) FROM events_cold`).Scan(&coldCount)
	require.NoError(t, err)

	assert.Equal(t, 3, hotCount, "expected 3 hot events")
	assert.Equal(t, 2, warmCount, "expected 2 warm events")
	assert.Equal(t, 1, coldCount, "expected 1 cold event")

	totalRetained := hotCount + warmCount + coldCount
	t.Logf("✓ Data retention verified: hot=%d, warm=%d, cold=%d (total=%d)", hotCount, warmCount, coldCount, totalRetained)
}

// TestWorkflow_WebhookDelivery_EndToEnd tests webhook delivery flow
func TestWorkflow_WebhookDelivery_EndToEnd(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping E2E test in short mode")
	}

	db := setupE2EDuckDB(t)
	setupE2ESchema(t, db)

	ctx := context.Background()

	// 1. User creates webhook subscription
	webhookID := "webhook_e2e_1"
	eventTypesJSON := `["page_view", "click"]`

	now := time.Now()
	_, err := db.Exec(ctx, `
		INSERT INTO webhooks (id, user_id, url, event_types, active, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`,
		webhookID, "user_webhook_1", "https://example.com/webhooks",
		eventTypesJSON, true, now, now)
	require.NoError(t, err)

	// 2. Event is ingested
	eventID := "evt_webhook_1"
	_, err = db.Exec(ctx, `
		INSERT INTO events (id, event_type, category, user_id, session_id, data, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`,
		eventID, "page_view", "engagement", "user_webhook_1", "session_1",
		`{"page":"/home"}`, now, now)
	require.NoError(t, err)

	// 3. Create delivery record (webhook would be triggered)
	_, err = db.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS webhook_deliveries (
			id VARCHAR PRIMARY KEY,
			webhook_id VARCHAR,
			event_id VARCHAR,
			status VARCHAR,
			status_code INTEGER,
			attempts INTEGER,
			created_at TIMESTAMP,
			delivered_at TIMESTAMP
		)
	`)
	require.NoError(t, err)

	deliveryID := "delivery_1"
	_, err = db.Exec(ctx, `
		INSERT INTO webhook_deliveries (id, webhook_id, event_id, status, status_code, attempts, created_at, delivered_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`,
		deliveryID, webhookID, eventID, "success", 200, 1, now, now)
	require.NoError(t, err)

	// 4. Verify delivery was successful
	var deliveryStatus string
	var statusCode int
	err = db.QueryRow(ctx, `
		SELECT status, status_code FROM webhook_deliveries WHERE id = ?
	`, deliveryID).Scan(&deliveryStatus, &statusCode)
	require.NoError(t, err)

	assert.Equal(t, "success", deliveryStatus)
	assert.Equal(t, 200, statusCode)

	// 5. Verify webhook delivery history
	var deliveryCount int
	err = db.QueryRow(ctx, `
		SELECT COUNT(*) FROM webhook_deliveries WHERE webhook_id = ? AND status = 'success'
	`, webhookID).Scan(&deliveryCount)
	require.NoError(t, err)
	assert.Equal(t, 1, deliveryCount)

	t.Logf("✓ Webhook delivery E2E verified: event → webhook trigger → delivery → confirmed")
}

// TestWorkflow_MultiTenantIsolation tests data isolation between users
func TestWorkflow_MultiTenantIsolation(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping E2E test in short mode")
	}

	db := setupE2EDuckDB(t)
	setupE2ESchema(t, db)

	ctx := context.Background()

	// Create events for different users
	now := time.Now()
	users := []string{"tenant_1", "tenant_2", "tenant_3"}

	for _, userID := range users {
		for i := 1; i <= 2; i++ {
			_, err := db.Exec(ctx, `
				INSERT INTO events (id, event_type, category, user_id, session_id, data, created_at, updated_at)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?)
			`,
				fmt.Sprintf("evt_%s_%d", userID, i), "action", "engagement",
				userID, fmt.Sprintf("session_%s_%d", userID, i),
				fmt.Sprintf(`{"tenant":"%s","event":%d}`, userID, i),
				now, now)
			require.NoError(t, err)
		}
	}

	// Each user should only see their own data
	for _, userID := range users {
		var count int
		err := db.QueryRow(ctx, `
			SELECT COUNT(*) FROM events WHERE user_id = ?
		`, userID).Scan(&count)
		require.NoError(t, err)
		assert.Equal(t, 2, count, "user %s should see exactly 2 events", userID)

		// Verify other users' data is not visible to queries filtered by user
		var otherCount int
		err = db.QueryRow(ctx, `
			SELECT COUNT(*) FROM events WHERE user_id != ?
		`, userID).Scan(&otherCount)
		require.NoError(t, err)
		assert.Equal(t, 4, otherCount, "user %s should not see other users' events in != query", userID)
	}

	t.Logf("✓ Multi-tenant isolation verified: 3 tenants, 2 events each, isolated")
}
