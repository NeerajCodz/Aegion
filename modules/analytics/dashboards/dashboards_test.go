package dashboards

import (
	"testing"
	"time"
)

func TestPrebuiltDashboards(t *testing.T) {
	dashboards := PrebuiltDashboards()

	if len(dashboards) != 5 {
		t.Errorf("expected 5 dashboards, got %d", len(dashboards))
	}

	expectedDashboards := map[string]string{
		"auth-dashboard":     "Authentication Dashboard",
		"activity-dashboard": "User Activity Dashboard",
		"sessions-dashboard": "Session Analytics Dashboard",
		"security-dashboard": "Security Dashboard",
		"health-dashboard":   "System Health Dashboard",
	}

	for id, expectedName := range expectedDashboards {
		dashboard, ok := dashboards[id]
		if !ok {
			t.Errorf("dashboard %s not found", id)
			continue
		}

		if dashboard.Name != expectedName {
			t.Errorf("dashboard %s: expected name '%s', got '%s'", id, expectedName, dashboard.Name)
		}

		if !dashboard.IsDefault {
			t.Errorf("dashboard %s should be marked as default", id)
		}

		if len(dashboard.Components) == 0 {
			t.Errorf("dashboard %s has no components", id)
		}

		// Verify refresh interval is set
		if dashboard.RefreshInterval <= 0 {
			t.Errorf("dashboard %s has invalid refresh interval: %d", id, dashboard.RefreshInterval)
		}
	}
}

func TestPrebuiltQueries(t *testing.T) {
	queries := PrebuiltQueries()

	if len(queries) != 50 {
		t.Errorf("expected 50 queries, got %d", len(queries))
	}

	// Verify some key queries exist
	keyQueries := []string{
		"auth_login_success_rate",
		"activity_new_signups",
		"session_current_active",
		"security_suspicious_activities",
		"health_api_latency",
	}

	for _, queryID := range keyQueries {
		if _, ok := queries[queryID]; !ok {
			t.Errorf("query %s not found", queryID)
		}
	}

	// Verify query structure
	for id, query := range queries {
		if query.ID != id {
			t.Errorf("query %s has mismatched ID: %s", id, query.ID)
		}

		if query.Name == "" {
			t.Errorf("query %s has empty name", id)
		}

		if query.SQL == "" {
			t.Errorf("query %s has empty SQL", id)
		}

		if query.Category == "" {
			t.Errorf("query %s has empty category", id)
		}

		if query.CacheTTL <= 0 {
			t.Errorf("query %s has invalid cache TTL: %d", id, query.CacheTTL)
		}
	}
}

func TestDashboardBuilder(t *testing.T) {
	dashboard := NewBuilder("test-dashboard").
		Name("Test Dashboard").
		Description("A test dashboard").
		Category("test").
		IsDefault(false).
		Layout("grid-3col").
		RefreshInterval(60).
		Public(false).
		Pinned(true).
		AddTimeSeriesComponent("metric1", "Test Metric", "test_query", []string{"value"}).
		Build()

	if dashboard.ID != "test-dashboard" {
		t.Errorf("expected ID 'test-dashboard', got '%s'", dashboard.ID)
	}

	if dashboard.Name != "Test Dashboard" {
		t.Errorf("expected name 'Test Dashboard', got '%s'", dashboard.Name)
	}

	if dashboard.RefreshInterval != 60 {
		t.Errorf("expected refresh interval 60, got %d", dashboard.RefreshInterval)
	}

	if len(dashboard.Components) != 1 {
		t.Errorf("expected 1 component, got %d", len(dashboard.Components))
	}

	component := dashboard.Components[0]
	if component.ID != "metric1" {
		t.Errorf("expected component ID 'metric1', got '%s'", component.ID)
	}

	if component.Type != "time_series" {
		t.Errorf("expected component type 'time_series', got '%s'", component.Type)
	}
}

func TestComponentBuilder(t *testing.T) {
	component := NewComponentBuilder("gauge1", "gauge").
		Title("Test Gauge").
		Description("A test gauge").
		QueryID("test_query").
		TimeRange("1d").
		Metrics([]string{"value"}).
		GridPosition(1, 1, 2, 2).
		Config("min", 0).
		Config("max", 100).
		Build()

	if component.ID != "gauge1" {
		t.Errorf("expected ID 'gauge1', got '%s'", component.ID)
	}

	if component.Type != "gauge" {
		t.Errorf("expected type 'gauge', got '%s'", component.Type)
	}

	if component.GridWidth != 2 {
		t.Errorf("expected grid width 2, got %d", component.GridWidth)
	}

	if minVal, ok := component.Config["min"]; !ok || minVal != 0 {
		t.Error("config 'min' not set correctly")
	}
}

func TestPrebuiltDashboardBuilder(t *testing.T) {
	builder := NewPrebuiltDashboardBuilder()

	// Test Get
	authDash, err := builder.Get("auth-dashboard")
	if err != nil {
		t.Errorf("failed to get auth-dashboard: %v", err)
	}

	if authDash.Name != "Authentication Dashboard" {
		t.Errorf("expected 'Authentication Dashboard', got '%s'", authDash.Name)
	}

	// Test Get non-existent
	_, err = builder.Get("non-existent")
	if err == nil {
		t.Error("should return error for non-existent dashboard")
	}

	// Test GetAll
	all := builder.GetAll()
	if len(all) != 5 {
		t.Errorf("expected 5 dashboards, got %d", len(all))
	}

	// Test GetQuery
	query, err := builder.GetQuery("auth_login_success_rate")
	if err != nil {
		t.Errorf("failed to get query: %v", err)
	}

	if query.Name != "Login Success Rate" {
		t.Errorf("expected 'Login Success Rate', got '%s'", query.Name)
	}

	// Test List by category
	authDashboards := builder.List(func() *string { s := "authentication"; return &s }())
	found := false
	for _, d := range authDashboards {
		if d.ID == "auth-dashboard" {
			found = true
			break
		}
	}
	if !found {
		t.Error("auth-dashboard not found in authentication category list")
	}
}

func TestQueryBuilder(t *testing.T) {
	builder := NewQueryBuilder("SELECT * FROM analytics_events WHERE category = :category")
	builder.WithParameter("category", "test")

	sql := builder.Build()
	if !contains(sql, "category = 'test'") {
		t.Errorf("query not properly substituted: %s", sql)
	}
}

func TestAggregateQuery(t *testing.T) {
	aq := &AggregateQuery{
		Table:      "analytics_events",
		Dimensions: []string{"category", "event_type"},
		Metrics:    []string{"COUNT(*) as count"},
		OrderBy:    "count DESC",
		Limit:      10,
	}

	sql := aq.Build()
	if !contains(sql, "GROUP BY") {
		t.Error("aggregate query missing GROUP BY")
	}
	if !contains(sql, "LIMIT 10") {
		t.Error("aggregate query missing LIMIT")
	}
}

func TestDashboardModels(t *testing.T) {
	now := time.Now()

	dashboard := &Dashboard{
		ID:              "test",
		Name:            "Test",
		Category:        "test",
		IsDefault:       false,
		Layout:          "grid-3col",
		RefreshInterval: 30,
		Public:          false,
		Pinned:          false,
		CreatedAt:       now,
		UpdatedAt:       now,
	}

	if dashboard.ID != "test" {
		t.Error("dashboard ID not set")
	}

	component := &Component{
		ID:         "comp1",
		Type:       "gauge",
		Title:      "Test",
		QueryID:    "query1",
		TimeRange:  "1d",
		GridCol:    1,
		GridRow:    1,
		GridWidth:  1,
		GridHeight: 1,
	}

	if component.Type != "gauge" {
		t.Error("component type not set")
	}

	query := &DashboardQuery{
		ID:       "q1",
		Name:     "Test Query",
		SQL:      "SELECT 1",
		Category: "test",
		CacheTTL: 300,
	}

	if query.CacheTTL != 300 {
		t.Error("query cache TTL not set")
	}

	share := &DashboardShare{
		ID:          "share1",
		DashboardID: "dash1",
		Token:       "token123",
		ReadOnly:    true,
		CreatedAt:   now,
	}

	if !share.ReadOnly {
		t.Error("share read_only flag not set")
	}

	alert := &AlertThreshold{
		ID:            "alert1",
		DashboardID:   "dash1",
		MetricName:    "error_rate",
		Operator:      "gt",
		Threshold:     5.0,
		SeverityLevel: "critical",
		Enabled:       true,
	}

	if alert.Threshold != 5.0 {
		t.Error("alert threshold not set")
	}
}

// Helper function
func contains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
