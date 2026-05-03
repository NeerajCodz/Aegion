package dashboards

import (
	"context"
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/aegion/aegion/internal/platform/logger"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestManagerExecuteQueryUsesCache(t *testing.T) {
	db := openDashboardTestDB(t)
	manager := NewManager(db, logger.TestLogger(), DashboardConfig{})

	first, err := manager.ExecuteQuery(context.Background(), "query-1", &DashboardQuery{
		ID:       "query-1",
		Name:     "cached",
		SQL:      "SELECT 'ok' AS status",
		CacheTTL: 60,
	})
	require.NoError(t, err)
	require.Len(t, first.Data, 1)
	assert.False(t, first.FromCache)

	second, err := manager.ExecuteQuery(context.Background(), "query-1", &DashboardQuery{
		ID:       "query-1",
		Name:     "cached",
		SQL:      "SELECT 'different' AS status",
		CacheTTL: 60,
	})
	require.NoError(t, err)
	require.Len(t, second.Data, 1)
	assert.True(t, second.FromCache)
	require.NotNil(t, second.CachedAt)
	assert.Equal(t, first.Data[0]["status"], second.Data[0]["status"])
}

func TestListDashboardsParsesStoredConfig(t *testing.T) {
	db := openDashboardTestDB(t)
	manager := NewManager(db, logger.TestLogger(), DashboardConfig{})

	configJSON, err := marshalJSON(map[string]interface{}{
		"category":         "security",
		"is_default":       false,
		"layout":           "grid-4col",
		"refresh_interval": 45,
		"components": []Component{
			{
				ID:         "component-1",
				Type:       "table",
				Title:      "Security Events",
				Description: "Latest auth failures",
				QueryID:    "security_failures",
				TimeRange:  "7d",
				Metrics:    []string{"count"},
				Config: map[string]interface{}{
					"page_size": 25,
				},
				GridCol:    2,
				GridRow:    3,
				GridWidth:  4,
				GridHeight: 2,
			},
		},
		"config": map[string]interface{}{
			"theme": "dark",
		},
	})
	require.NoError(t, err)

	_, err = db.Exec(`
		INSERT INTO analytics_dashboards (id, name, description, config, owner_id, public, pinned, created_at, updated_at)
		VALUES ('dash-1', 'Security Dashboard', 'Security overview', ?, 'owner-1', 1, 1, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
	`, configJSON)
	require.NoError(t, err)

	ownerID := "owner-1"
	dashboards, err := manager.ListDashboards(context.Background(), &ownerID, true)
	require.NoError(t, err)
	require.Len(t, dashboards, 1)

	dashboard := dashboards[0]
	assert.Equal(t, "security", dashboard.Category)
	assert.Equal(t, "grid-4col", dashboard.Layout)
	assert.Equal(t, 45, dashboard.RefreshInterval)
	assert.True(t, dashboard.Public)
	assert.True(t, dashboard.Pinned)
	require.Len(t, dashboard.Components, 1)
	assert.Equal(t, "component-1", dashboard.Components[0].ID)
	assert.EqualValues(t, 25, dashboard.Components[0].Config["page_size"])
	assert.Equal(t, "dark", dashboard.Config["theme"])
}

func openDashboardTestDB(t *testing.T) *sql.DB {
	t.Helper()

	db, err := sql.Open("sqlite", "file::memory:?cache=shared")
	require.NoError(t, err)

	t.Cleanup(func() {
		require.NoError(t, db.Close())
	})

	_, err = db.Exec(`
		CREATE TABLE analytics_dashboards (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			description TEXT,
			config TEXT NOT NULL,
			owner_id TEXT,
			public BOOLEAN NOT NULL DEFAULT FALSE,
			pinned BOOLEAN NOT NULL DEFAULT FALSE,
			created_at TIMESTAMP NOT NULL,
			updated_at TIMESTAMP NOT NULL
		)
	`)
	require.NoError(t, err)

	return db
}
