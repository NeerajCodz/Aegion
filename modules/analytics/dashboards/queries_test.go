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
	csvSQL, err := ExportToCSV(&ExportQuery{DashboardID: "dash-1"}, nil)
	require.NoError(t, err)
	assert.Contains(t, csvSQL, "dash-1")

	jsonSQL, err := ExportToJSON(&ExportQuery{DashboardID: "dash-1"}, nil)
	require.NoError(t, err)
	assert.Contains(t, jsonSQL, "row_to_json")
}
