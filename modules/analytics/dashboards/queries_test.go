package dashboards

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAdditionalDashboardQueryBuilderEscapesStrings(t *testing.T) {
	sql := NewQueryBuilder("SELECT * FROM analytics_events WHERE category = :category").
		WithParameter("category", "auth' OR '1'='1").
		Build()

	assert.Contains(t, sql, "auth'' OR ''1''=''1")
}

func TestAdditionalAggregateQueryIncludesFiltersAndGrouping(t *testing.T) {
	query := (&AggregateQuery{
		Table:      "analytics_events",
		Dimensions: []string{"category"},
		Metrics:    []string{"COUNT(*) as total"},
		Filters: map[string]interface{}{
			"event_type": "auth.login",
		},
		OrderBy: "total DESC",
		Limit:   5,
	}).Build()

	assert.Contains(t, query, "FROM analytics_events")
	assert.Contains(t, query, "event_type = 'auth.login'")
	assert.Contains(t, query, "GROUP BY category")
	assert.Contains(t, query, "LIMIT 5")
}

func TestAdditionalExportQueryHelpers(t *testing.T) {
	dashboardQueries := map[string]*DashboardQuery{
		"dash-1": {
			ID:   "dash-1",
			SQL:  "SELECT event_type, COUNT(*) AS total FROM analytics_events",
			Name: "events",
		},
	}

	csvSQL, err := ExportToCSV(&ExportQuery{
		DashboardID: "dash-1",
		TimeRange:   "24h",
		Filters: map[string]interface{}{
			"event_type": "auth.login",
		},
	}, dashboardQueries)
	require.NoError(t, err)
	assert.Contains(t, csvSQL, "SELECT * FROM (")
	assert.Contains(t, csvSQL, "event_type = 'auth.login'")
	assert.Contains(t, csvSQL, "created_at >= NOW() - INTERVAL '1 day'")

	jsonSQL, err := ExportToJSON(&ExportQuery{DashboardID: "dash-1"}, dashboardQueries)
	require.NoError(t, err)
	assert.Contains(t, jsonSQL, "json_agg")
}

func TestAdditionalExportQueryHelpersFallbackToCommonTemplate(t *testing.T) {
	sql, err := ExportToCSV(&ExportQuery{DashboardID: "top_events"}, nil)
	require.NoError(t, err)
	assert.Contains(t, sql, "FROM analytics_events")
	assert.Contains(t, sql, "LIMIT 1000")
}

func TestAdditionalExportQueryHelpersRejectUnknownDashboard(t *testing.T) {
	_, err := ExportToJSON(&ExportQuery{DashboardID: "missing-dashboard"}, nil)
	require.Error(t, err)
}
