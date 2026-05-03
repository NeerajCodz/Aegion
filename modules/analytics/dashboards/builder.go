package dashboards

import (
	"fmt"
	"time"
)

// Builder provides a fluent interface for building dashboards.
type Builder struct {
	dashboard *Dashboard
}

// NewBuilder creates a new dashboard builder.
func NewBuilder(id string) *Builder {
	return &Builder{
		dashboard: &Dashboard{
			ID:              id,
			RefreshInterval: 30,
			Layout:          "grid-3col",
			Public:          false,
			Pinned:          false,
			Components:      []Component{},
			CreatedAt:       time.Now(),
			UpdatedAt:       time.Now(),
		},
	}
}

// Name sets the dashboard name.
func (b *Builder) Name(name string) *Builder {
	b.dashboard.Name = name
	return b
}

// Description sets the dashboard description.
func (b *Builder) Description(desc string) *Builder {
	b.dashboard.Description = desc
	return b
}

// Category sets the dashboard category.
func (b *Builder) Category(category string) *Builder {
	b.dashboard.Category = category
	return b
}

// IsDefault marks the dashboard as default.
func (b *Builder) IsDefault(isDefault bool) *Builder {
	b.dashboard.IsDefault = isDefault
	return b
}

// Layout sets the dashboard layout.
func (b *Builder) Layout(layout string) *Builder {
	b.dashboard.Layout = layout
	return b
}

// RefreshInterval sets the auto-refresh interval in seconds.
func (b *Builder) RefreshInterval(seconds int) *Builder {
	b.dashboard.RefreshInterval = seconds
	return b
}

// Public marks the dashboard as public.
func (b *Builder) Public(public bool) *Builder {
	b.dashboard.Public = public
	return b
}

// Pinned marks the dashboard as pinned.
func (b *Builder) Pinned(pinned bool) *Builder {
	b.dashboard.Pinned = pinned
	return b
}

// OwnerID sets the dashboard owner.
func (b *Builder) OwnerID(ownerID string) *Builder {
	b.dashboard.OwnerID = &ownerID
	return b
}

// AddComponent adds a component to the dashboard.
func (b *Builder) AddComponent(component Component) *Builder {
	if len(b.dashboard.Components) > 0 {
		component.GridRow = len(b.dashboard.Components)/3 + 1
		component.GridCol = (len(b.dashboard.Components) % 3) + 1
	} else {
		component.GridRow = 1
		component.GridCol = 1
	}

	if component.GridWidth == 0 {
		component.GridWidth = 1
	}
	if component.GridHeight == 0 {
		component.GridHeight = 1
	}

	b.dashboard.Components = append(b.dashboard.Components, component)
	return b
}

// AddTimeSeriesComponent adds a time series chart component.
func (b *Builder) AddTimeSeriesComponent(id, title, queryID string, metrics []string) *Builder {
	component := Component{
		ID:        id,
		Type:      "time_series",
		Title:     title,
		QueryID:   queryID,
		TimeRange: "1d",
		Metrics:   metrics,
		Config: map[string]interface{}{
			"chartType": "line",
		},
	}
	return b.AddComponent(component)
}

// AddPieChartComponent adds a pie chart component.
func (b *Builder) AddPieChartComponent(id, title, queryID string, metrics []string) *Builder {
	component := Component{
		ID:        id,
		Type:      "pie_chart",
		Title:     title,
		QueryID:   queryID,
		TimeRange: "1d",
		Metrics:   metrics,
	}
	return b.AddComponent(component)
}

// AddGaugeComponent adds a gauge component.
func (b *Builder) AddGaugeComponent(id, title, queryID string, metric string, min, max float64) *Builder {
	component := Component{
		ID:        id,
		Type:      "gauge",
		Title:     title,
		QueryID:   queryID,
		TimeRange: "1d",
		Metrics:   []string{metric},
		Config: map[string]interface{}{
			"min": min,
			"max": max,
		},
	}
	return b.AddComponent(component)
}

// AddTableComponent adds a table component.
func (b *Builder) AddTableComponent(id, title, queryID string) *Builder {
	component := Component{
		ID:        id,
		Type:      "table",
		Title:     title,
		QueryID:   queryID,
		TimeRange: "1d",
		Config: map[string]interface{}{
			"pageable":  true,
			"page_size": 10,
		},
	}
	return b.AddComponent(component)
}

// AddHistogramComponent adds a histogram component.
func (b *Builder) AddHistogramComponent(id, title, queryID string, metric string, buckets int) *Builder {
	component := Component{
		ID:        id,
		Type:      "histogram",
		Title:     title,
		QueryID:   queryID,
		TimeRange: "1d",
		Metrics:   []string{metric},
		Config: map[string]interface{}{
			"buckets": buckets,
		},
	}
	return b.AddComponent(component)
}

// AddMapComponent adds a geographic map component.
func (b *Builder) AddMapComponent(id, title, queryID string) *Builder {
	component := Component{
		ID:        id,
		Type:      "map",
		Title:     title,
		QueryID:   queryID,
		TimeRange: "1d",
		Config: map[string]interface{}{
			"mapType": "world",
		},
	}
	return b.AddComponent(component)
}

// AddHeatmapComponent adds a heatmap component.
func (b *Builder) AddHeatmapComponent(id, title, queryID string) *Builder {
	component := Component{
		ID:        id,
		Type:      "heatmap",
		Title:     title,
		QueryID:   queryID,
		TimeRange: "1d",
	}
	return b.AddComponent(component)
}

// AddLeaderboardComponent adds a leaderboard component.
func (b *Builder) AddLeaderboardComponent(id, title, queryID string, rankingMetric string) *Builder {
	component := Component{
		ID:        id,
		Type:      "leaderboard",
		Title:     title,
		QueryID:   queryID,
		TimeRange: "1d",
		Metrics:   []string{rankingMetric},
		Config: map[string]interface{}{
			"limit": 10,
		},
	}
	return b.AddComponent(component)
}

// AddFunnelComponent adds a funnel chart component.
func (b *Builder) AddFunnelComponent(id, title, queryID string, stages []string) *Builder {
	component := Component{
		ID:        id,
		Type:      "funnel",
		Title:     title,
		QueryID:   queryID,
		TimeRange: "1d",
		Metrics:   stages,
	}
	return b.AddComponent(component)
}

// AddBarChartComponent adds a bar chart component.
func (b *Builder) AddBarChartComponent(id, title, queryID string, metrics []string) *Builder {
	component := Component{
		ID:        id,
		Type:      "bar_chart",
		Title:     title,
		QueryID:   queryID,
		TimeRange: "1d",
		Metrics:   metrics,
	}
	return b.AddComponent(component)
}

// AddAlertBannerComponent adds an alert banner component.
func (b *Builder) AddAlertBannerComponent(id, title, queryID string) *Builder {
	component := Component{
		ID:        id,
		Type:      "alert_banner",
		Title:     title,
		QueryID:   queryID,
		TimeRange: "1h",
	}
	return b.AddComponent(component)
}

// AddCounterComponent adds a counter/KPI component.
func (b *Builder) AddCounterComponent(id, title, queryID string, metric string) *Builder {
	component := Component{
		ID:        id,
		Type:      "counter",
		Title:     title,
		QueryID:   queryID,
		TimeRange: "1d",
		Metrics:   []string{metric},
	}
	return b.AddComponent(component)
}

// AddTimelineComponent adds a timeline component.
func (b *Builder) AddTimelineComponent(id, title, queryID string) *Builder {
	component := Component{
		ID:        id,
		Type:      "timeline",
		Title:     title,
		QueryID:   queryID,
		TimeRange: "1d",
		Config: map[string]interface{}{
			"limit": 20,
		},
	}
	return b.AddComponent(component)
}

// Build returns the constructed dashboard.
func (b *Builder) Build() *Dashboard {
	b.dashboard.UpdatedAt = time.Now()
	return b.dashboard
}

// ComponentBuilder provides a fluent interface for building dashboard components.
type ComponentBuilder struct {
	component Component
}

// NewComponentBuilder creates a new component builder.
func NewComponentBuilder(id, componentType string) *ComponentBuilder {
	return &ComponentBuilder{
		component: Component{
			ID:         id,
			Type:       componentType,
			GridCol:    1,
			GridRow:    1,
			GridWidth:  1,
			GridHeight: 1,
			Config:     make(map[string]interface{}),
		},
	}
}

// Title sets the component title.
func (cb *ComponentBuilder) Title(title string) *ComponentBuilder {
	cb.component.Title = title
	return cb
}

// Description sets the component description.
func (cb *ComponentBuilder) Description(desc string) *ComponentBuilder {
	cb.component.Description = desc
	return cb
}

// QueryID sets the query ID.
func (cb *ComponentBuilder) QueryID(queryID string) *ComponentBuilder {
	cb.component.QueryID = queryID
	return cb
}

// TimeRange sets the time range.
func (cb *ComponentBuilder) TimeRange(timeRange string) *ComponentBuilder {
	cb.component.TimeRange = timeRange
	return cb
}

// Metrics sets the metrics list.
func (cb *ComponentBuilder) Metrics(metrics []string) *ComponentBuilder {
	cb.component.Metrics = metrics
	return cb
}

// GridPosition sets the grid position.
func (cb *ComponentBuilder) GridPosition(col, row, width, height int) *ComponentBuilder {
	cb.component.GridCol = col
	cb.component.GridRow = row
	cb.component.GridWidth = width
	cb.component.GridHeight = height
	return cb
}

// Config sets a config value.
func (cb *ComponentBuilder) Config(key string, value interface{}) *ComponentBuilder {
	cb.component.Config[key] = value
	return cb
}

// Configs sets multiple config values.
func (cb *ComponentBuilder) Configs(config map[string]interface{}) *ComponentBuilder {
	for k, v := range config {
		cb.component.Config[k] = v
	}
	return cb
}

// Build returns the constructed component.
func (cb *ComponentBuilder) Build() Component {
	return cb.component
}

// PrebuiltDashboardBuilder creates pre-built dashboards with sensible defaults.
type PrebuiltDashboardBuilder struct {
	dashboards map[string]*Dashboard
}

// NewPrebuiltDashboardBuilder creates a new pre-built dashboard builder.
func NewPrebuiltDashboardBuilder() *PrebuiltDashboardBuilder {
	return &PrebuiltDashboardBuilder{
		dashboards: PrebuiltDashboards(),
	}
}

// Get retrieves a pre-built dashboard by ID.
func (pb *PrebuiltDashboardBuilder) Get(id string) (*Dashboard, error) {
	if dashboard, ok := pb.dashboards[id]; ok {
		return dashboard, nil
	}
	return nil, fmt.Errorf("dashboard '%s' not found", id)
}

// GetAll returns all pre-built dashboards.
func (pb *PrebuiltDashboardBuilder) GetAll() map[string]*Dashboard {
	return pb.dashboards
}

// GetQuery retrieves a pre-built query by ID.
func (pb *PrebuiltDashboardBuilder) GetQuery(id string) (*DashboardQuery, error) {
	queries := PrebuiltQueries()
	if query, ok := queries[id]; ok {
		return query, nil
	}
	return nil, fmt.Errorf("query '%s' not found", id)
}

// GetAllQueries returns all pre-built queries.
func (pb *PrebuiltDashboardBuilder) GetAllQueries() map[string]*DashboardQuery {
	return PrebuiltQueries()
}

// List returns all dashboard definitions with optional filtering.
func (pb *PrebuiltDashboardBuilder) List(category *string) []*Dashboard {
	var result []*Dashboard
	for _, dashboard := range pb.dashboards {
		if category == nil || dashboard.Category == *category {
			result = append(result, dashboard)
		}
	}
	return result
}
