package graphql

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/aegion/aegion/modules/analytics"
	"github.com/rs/zerolog"
)

// Resolver handles GraphQL query, mutation, and subscription resolving.
type Resolver struct {
	logger    zerolog.Logger
	store     Store
	startTime time.Time
	// Subscription channels
	eventSubs       map[string]chan *EventNode
	metricSubs      map[string]chan *MetricNode
	dashboardSubs   map[string]chan *DashboardNode
	subscriptionsMu interface{} // Use sync.RWMutex in real implementation
}

// Store defines the interface for data access.
type Store interface {
	// Events
	GetEvent(ctx context.Context, id string) (*analytics.Event, error)
	ListEvents(ctx context.Context, filter *EventFilter, limit int, offset int) ([]*analytics.Event, int, error)
	CountEvents(ctx context.Context) (int, error)

	// Dashboards
	CreateDashboard(ctx context.Context, dashboard *analytics.Dashboard) (*analytics.Dashboard, error)
	GetDashboard(ctx context.Context, id string) (*analytics.Dashboard, error)
	ListDashboards(ctx context.Context, ownerID *string, public *bool) ([]*analytics.Dashboard, error)
	UpdateDashboard(ctx context.Context, dashboard *analytics.Dashboard) (*analytics.Dashboard, error)
	DeleteDashboard(ctx context.Context, id string) error

	// Queries
	CreateQuery(ctx context.Context, query *analytics.Query) (*analytics.Query, error)
	GetQuery(ctx context.Context, id string) (*analytics.Query, error)
	ListQueries(ctx context.Context, ownerID *string) ([]*analytics.Query, error)
	DeleteQuery(ctx context.Context, id string) error

	// Metrics
	ListMetrics(ctx context.Context, category *string) ([]*analytics.Metric, error)

	// Webhooks
	CreateWebhook(ctx context.Context, webhook *analytics.Webhook) (*analytics.Webhook, error)

	// Health
	GetHealth(ctx context.Context) (*analytics.HealthStatus, error)

	// Execute SQL
	ExecuteSQL(ctx context.Context, sql string, timeout time.Duration) ([]map[string]interface{}, error)
}

// NewResolver creates a new GraphQL resolver.
func NewResolver(logger zerolog.Logger, store Store) *Resolver {
	return &Resolver{
		logger:       logger,
		store:        store,
		startTime:    time.Now(),
		eventSubs:    make(map[string]chan *EventNode),
		metricSubs:   make(map[string]chan *MetricNode),
		dashboardSubs: make(map[string]chan *DashboardNode),
	}
}

// ==================== Query Resolvers ====================

// Events resolves the events query with filtering and pagination.
func (r *Resolver) Events(ctx context.Context, filter *EventFilter, first *int, after *string, sort *SortInput) (*EventConnection, error) {
	limit := 100
	if first != nil && *first > 0 {
		if *first > 1000 {
			limit = 1000
		} else {
			limit = *first
		}
	}

	offset := 0
	if after != nil && *after != "" {
		// Parse cursor - for simplicity, use numeric offset
		fmt.Sscanf(*after, "%d", &offset)
	}

	events, total, err := r.store.ListEvents(ctx, filter, limit, offset)
	if err != nil {
		r.logger.Error().Err(err).Msg("failed to list events")
		return nil, fmt.Errorf("failed to list events: %w", err)
	}

	edges := make([]*EventEdge, len(events))
	for i, event := range events {
		edges[i] = &EventEdge{
			Cursor: fmt.Sprintf("%d", offset+i),
			Node:   eventToNode(event),
		}
	}

	hasNext := offset+limit < total
	var endCursor *string
	if len(edges) > 0 {
		c := fmt.Sprintf("%d", offset+len(edges)-1)
		endCursor = &c
	}

	return &EventConnection{
		Edges: edges,
		PageInfo: &PageInfo{
			HasNextPage:     hasNext,
			HasPreviousPage: offset > 0,
			EndCursor:       endCursor,
			TotalCount:      total,
		},
		TotalCount: total,
	}, nil
}

// Event resolves a single event by ID.
func (r *Resolver) Event(ctx context.Context, id string) (*EventNode, error) {
	event, err := r.store.GetEvent(ctx, id)
	if err != nil {
		r.logger.Error().Err(err).Str("id", id).Msg("failed to get event")
		return nil, fmt.Errorf("failed to get event: %w", err)
	}
	if event == nil {
		return nil, errors.New("event not found")
	}
	return eventToNode(event), nil
}

// Dashboards resolves dashboards query with optional filtering.
func (r *Resolver) Dashboards(ctx context.Context, isDefault *bool, public *bool) ([]*DashboardNode, error) {
	// Get current user from context (implement auth context in middleware)
	ownerID, ok := ctx.Value("userID").(string)
	if !ok {
		return nil, errors.New("unauthorized: user not found in context")
	}

	var dashboards []*analytics.Dashboard
	var err error

	if public != nil && *public {
		dashboards, err = r.store.ListDashboards(ctx, nil, public)
	} else {
		dashboards, err = r.store.ListDashboards(ctx, &ownerID, nil)
	}

	if err != nil {
		r.logger.Error().Err(err).Msg("failed to list dashboards")
		return nil, fmt.Errorf("failed to list dashboards: %w", err)
	}

	nodes := make([]*DashboardNode, len(dashboards))
	for i, d := range dashboards {
		nodes[i] = dashboardToNode(d)
	}
	return nodes, nil
}

// Dashboard resolves a single dashboard by ID.
func (r *Resolver) Dashboard(ctx context.Context, id string) (*DashboardNode, error) {
	dashboard, err := r.store.GetDashboard(ctx, id)
	if err != nil {
		r.logger.Error().Err(err).Str("id", id).Msg("failed to get dashboard")
		return nil, fmt.Errorf("failed to get dashboard: %w", err)
	}
	if dashboard == nil {
		return nil, errors.New("dashboard not found")
	}
	return dashboardToNode(dashboard), nil
}

// Queries resolves saved queries.
func (r *Resolver) Queries(ctx context.Context, limit *int, offset *int) ([]*SavedQueryNode, error) {
	ownerID, ok := ctx.Value("userID").(string)
	if !ok {
		return nil, errors.New("unauthorized: user not found in context")
	}

	queries, err := r.store.ListQueries(ctx, &ownerID)
	if err != nil {
		r.logger.Error().Err(err).Msg("failed to list queries")
		return nil, fmt.Errorf("failed to list queries: %w", err)
	}

	nodes := make([]*SavedQueryNode, len(queries))
	for i, q := range queries {
		nodes[i] = queryToNode(q)
	}
	return nodes, nil
}

// Query resolves a single saved query by ID.
func (r *Resolver) Query(ctx context.Context, id string) (*SavedQueryNode, error) {
	query, err := r.store.GetQuery(ctx, id)
	if err != nil {
		r.logger.Error().Err(err).Str("id", id).Msg("failed to get query")
		return nil, fmt.Errorf("failed to get query: %w", err)
	}
	if query == nil {
		return nil, errors.New("query not found")
	}
	return queryToNode(query), nil
}

// Health resolves the health status query.
func (r *Resolver) Health(ctx context.Context) (*HealthStatusNode, error) {
	health, err := r.store.GetHealth(ctx)
	if err != nil {
		r.logger.Error().Err(err).Msg("failed to get health status")
		return nil, fmt.Errorf("failed to get health status: %w", err)
	}
	return healthToNode(health), nil
}

// Stats resolves the system statistics query.
func (r *Resolver) Stats(ctx context.Context) (*SystemStatsNode, error) {
	eventCount, err := r.store.CountEvents(ctx)
	if err != nil {
		r.logger.Error().Err(err).Msg("failed to count events")
		eventCount = 0
	}

	uptime := int(time.Since(r.startTime).Seconds())

	return &SystemStatsNode{
		EventsTotal:     eventCount,
		DashboardsTotal: 0,
		QueriesTotal:    0,
		QueryTimeAvgMs:  0.0,
		CacheHitRate:    0.0,
		Uptime:          uptime,
	}, nil
}

// Metrics resolves the metrics query.
func (r *Resolver) Metrics(ctx context.Context, category *string, timeRange *TimeRangeInput) ([]*MetricNode, error) {
	metrics, err := r.store.ListMetrics(ctx, category)
	if err != nil {
		r.logger.Error().Err(err).Msg("failed to list metrics")
		return nil, fmt.Errorf("failed to list metrics: %w", err)
	}

	nodes := make([]*MetricNode, len(metrics))
	for i, m := range metrics {
		nodes[i] = metricToNode(m)
	}
	return nodes, nil
}

// ==================== Mutation Resolvers ====================

// CreateDashboard resolves the createDashboard mutation.
func (r *Resolver) CreateDashboard(ctx context.Context, input *CreateDashboardInput) (*CreateDashboardPayload, error) {
	ownerID, ok := ctx.Value("userID").(string)
	if !ok {
		return &CreateDashboardPayload{
			Errors: []*ErrorNode{{Message: "unauthorized: user not found in context"}},
		}, nil
	}

	public := false
	if input.Public != nil {
		public = *input.Public
	}

	desc := ""
	if input.Description != nil {
		desc = *input.Description
	}

	dashboard := &analytics.Dashboard{
		ID:          generateID(),
		Name:        input.Name,
		Description: desc,
		Config:      input.Config,
		OwnerID:     ownerID,
		Public:      public,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}

	created, err := r.store.CreateDashboard(ctx, dashboard)
	if err != nil {
		r.logger.Error().Err(err).Msg("failed to create dashboard")
		return &CreateDashboardPayload{
			Errors: []*ErrorNode{{Message: fmt.Sprintf("failed to create dashboard: %v", err)}},
		}, nil
	}

	return &CreateDashboardPayload{
		Dashboard: dashboardToNode(created),
	}, nil
}

// UpdateDashboard resolves the updateDashboard mutation.
func (r *Resolver) UpdateDashboard(ctx context.Context, id string, input *UpdateDashboardInput) (*UpdateDashboardPayload, error) {
	ownerID, ok := ctx.Value("userID").(string)
	if !ok {
		return &UpdateDashboardPayload{
			Errors: []*ErrorNode{{Message: "unauthorized: user not found in context"}},
		}, nil
	}

	dashboard, err := r.store.GetDashboard(ctx, id)
	if err != nil || dashboard == nil {
		return &UpdateDashboardPayload{
			Errors: []*ErrorNode{{Message: "dashboard not found"}},
		}, nil
	}

	// Check authorization
	if dashboard.OwnerID != ownerID {
		return &UpdateDashboardPayload{
			Errors: []*ErrorNode{{Message: "unauthorized: cannot update dashboard owned by another user"}},
		}, nil
	}

	if input.Name != nil {
		dashboard.Name = *input.Name
	}
	if input.Description != nil {
		dashboard.Description = *input.Description
	}
	if input.Config != nil {
		dashboard.Config = input.Config
	}
	if input.Public != nil {
		dashboard.Public = *input.Public
	}
	dashboard.UpdatedAt = time.Now()

	updated, err := r.store.UpdateDashboard(ctx, dashboard)
	if err != nil {
		r.logger.Error().Err(err).Msg("failed to update dashboard")
		return &UpdateDashboardPayload{
			Errors: []*ErrorNode{{Message: fmt.Sprintf("failed to update dashboard: %v", err)}},
		}, nil
	}

	return &UpdateDashboardPayload{
		Dashboard: dashboardToNode(updated),
	}, nil
}

// DeleteDashboard resolves the deleteDashboard mutation.
func (r *Resolver) DeleteDashboard(ctx context.Context, id string) (*DeleteDashboardPayload, error) {
	ownerID, ok := ctx.Value("userID").(string)
	if !ok {
		return &DeleteDashboardPayload{
			Errors: []*ErrorNode{{Message: "unauthorized: user not found in context"}},
		}, nil
	}

	dashboard, err := r.store.GetDashboard(ctx, id)
	if err != nil || dashboard == nil {
		return &DeleteDashboardPayload{
			Errors: []*ErrorNode{{Message: "dashboard not found"}},
		}, nil
	}

	// Check authorization
	if dashboard.OwnerID != ownerID {
		return &DeleteDashboardPayload{
			Errors: []*ErrorNode{{Message: "unauthorized: cannot delete dashboard owned by another user"}},
		}, nil
	}

	if err := r.store.DeleteDashboard(ctx, id); err != nil {
		r.logger.Error().Err(err).Msg("failed to delete dashboard")
		return &DeleteDashboardPayload{
			Errors: []*ErrorNode{{Message: fmt.Sprintf("failed to delete dashboard: %v", err)}},
		}, nil
	}

	return &DeleteDashboardPayload{Success: true}, nil
}

// SaveQuery resolves the saveQuery mutation.
func (r *Resolver) SaveQuery(ctx context.Context, input *SaveQueryInput) (*SaveQueryPayload, error) {
	ownerID, ok := ctx.Value("userID").(string)
	if !ok {
		return &SaveQueryPayload{
			Errors: []*ErrorNode{{Message: "unauthorized: user not found in context"}},
		}, nil
	}

	desc := ""
	if input.Description != nil {
		desc = *input.Description
	}

	query := &analytics.Query{
		ID:          generateID(),
		Name:        input.Name,
		Description: desc,
		SQL:         input.SQL,
		OwnerID:     ownerID,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}

	created, err := r.store.CreateQuery(ctx, query)
	if err != nil {
		r.logger.Error().Err(err).Msg("failed to save query")
		return &SaveQueryPayload{
			Errors: []*ErrorNode{{Message: fmt.Sprintf("failed to save query: %v", err)}},
		}, nil
	}

	return &SaveQueryPayload{
		Query: queryToNode(created),
	}, nil
}

// DeleteQuery resolves the deleteQuery mutation.
func (r *Resolver) DeleteQuery(ctx context.Context, id string) (*DeleteQueryPayload, error) {
	ownerID, ok := ctx.Value("userID").(string)
	if !ok {
		return &DeleteQueryPayload{
			Errors: []*ErrorNode{{Message: "unauthorized: user not found in context"}},
		}, nil
	}

	query, err := r.store.GetQuery(ctx, id)
	if err != nil || query == nil {
		return &DeleteQueryPayload{
			Errors: []*ErrorNode{{Message: "query not found"}},
		}, nil
	}

	// Check authorization
	if query.OwnerID != ownerID {
		return &DeleteQueryPayload{
			Errors: []*ErrorNode{{Message: "unauthorized: cannot delete query owned by another user"}},
		}, nil
	}

	if err := r.store.DeleteQuery(ctx, id); err != nil {
		r.logger.Error().Err(err).Msg("failed to delete query")
		return &DeleteQueryPayload{
			Errors: []*ErrorNode{{Message: fmt.Sprintf("failed to delete query: %v", err)}},
		}, nil
	}

	return &DeleteQueryPayload{Success: true}, nil
}

// CreateReport resolves the createReport mutation (stub).
func (r *Resolver) CreateReport(ctx context.Context, input *CreateReportInput) (*CreateReportPayload, error) {
	reportURL := "/reports/sample-report.pdf"
	return &CreateReportPayload{
		ReportURL: &reportURL,
	}, nil
}

// CreateWebhook resolves the createWebhook mutation.
func (r *Resolver) CreateWebhook(ctx context.Context, input *CreateWebhookInput) (*CreateWebhookPayload, error) {
	active := true
	if input.Active != nil {
		active = *input.Active
	}

	webhook := &analytics.Webhook{
		ID:        generateID(),
		URL:       input.URL,
		EventType: input.EventType,
		Secret:    generateID(), // Generate random secret
		Active:    active,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	created, err := r.store.CreateWebhook(ctx, webhook)
	if err != nil {
		r.logger.Error().Err(err).Msg("failed to create webhook")
		return &CreateWebhookPayload{
			Errors: []*ErrorNode{{Message: fmt.Sprintf("failed to create webhook: %v", err)}},
		}, nil
	}

	return &CreateWebhookPayload{
		Webhook: webhookToNode(created),
	}, nil
}

// ExecuteQuery resolves the executeQuery mutation.
func (r *Resolver) ExecuteQuery(ctx context.Context, sql string, timeout *int) (*ExecuteQueryPayload, error) {
	// Check authorization
	if _, ok := ctx.Value("userID").(string); !ok {
		return &ExecuteQueryPayload{
			Errors: []*ErrorNode{{Message: "unauthorized: user not found in context"}},
		}, nil
	}

	timeoutDuration := 30 * time.Second
	if timeout != nil {
		timeoutDuration = time.Duration(*timeout) * time.Second
	}

	start := time.Now()
	rows, err := r.store.ExecuteSQL(ctx, sql, timeoutDuration)
	executionTimeMs := int(time.Since(start).Milliseconds())

	if err != nil {
		r.logger.Error().Err(err).Msg("failed to execute query")
		return &ExecuteQueryPayload{
			Errors: []*ErrorNode{{Message: fmt.Sprintf("failed to execute query: %v", err)}},
		}, nil
	}

	return &ExecuteQueryPayload{
		Rows:            rows,
		RowCount:        len(rows),
		ExecutionTimeMs: executionTimeMs,
	}, nil
}

// ==================== Subscription Resolvers ====================

// OnNewEvent subscribes to new events matching a filter.
func (r *Resolver) OnNewEvent(ctx context.Context, filter *EventFilter) (<-chan *EventNode, error) {
	ch := make(chan *EventNode, 10)
	subID := generateID()
	r.eventSubs[subID] = ch

	go func() {
		<-ctx.Done()
		delete(r.eventSubs, subID)
		close(ch)
	}()

	return ch, nil
}

// OnMetricUpdate subscribes to metric updates.
func (r *Resolver) OnMetricUpdate(ctx context.Context, category *string) (<-chan *MetricNode, error) {
	ch := make(chan *MetricNode, 10)
	subID := generateID()
	r.metricSubs[subID] = ch

	go func() {
		<-ctx.Done()
		delete(r.metricSubs, subID)
		close(ch)
	}()

	return ch, nil
}

// OnDashboardChange subscribes to dashboard changes.
func (r *Resolver) OnDashboardChange(ctx context.Context, dashboardID string) (<-chan *DashboardNode, error) {
	ch := make(chan *DashboardNode, 10)
	subID := generateID()
	r.dashboardSubs[subID] = ch

	go func() {
		<-ctx.Done()
		delete(r.dashboardSubs, subID)
		close(ch)
	}()

	return ch, nil
}

// ==================== Helper Functions ====================

func eventToNode(e *analytics.Event) *EventNode {
	return &EventNode{
		ID:        e.ID,
		Category:  e.Category,
		EventType: e.EventType,
		Data:      e.Data,
		UserID:    e.UserID,
		SessionID: e.SessionID,
		CreatedAt: e.CreatedAt,
		UpdatedAt: e.UpdatedAt,
	}
}

func dashboardToNode(d *analytics.Dashboard) *DashboardNode {
	var desc *string
	if d.Description != "" {
		desc = &d.Description
	}
	return &DashboardNode{
		ID:          d.ID,
		Name:        d.Name,
		Description: desc,
		Config:      d.Config,
		OwnerID:     d.OwnerID,
		Public:      d.Public,
		CreatedAt:   d.CreatedAt,
		UpdatedAt:   d.UpdatedAt,
	}
}

func queryToNode(q *analytics.Query) *SavedQueryNode {
	var desc *string
	if q.Description != "" {
		desc = &q.Description
	}
	return &SavedQueryNode{
		ID:          q.ID,
		Name:        q.Name,
		Description: desc,
		SQL:         q.SQL,
		OwnerID:     q.OwnerID,
		CreatedAt:   q.CreatedAt,
		UpdatedAt:   q.UpdatedAt,
	}
}

func metricToNode(m *analytics.Metric) *MetricNode {
	unit := m.Unit
	return &MetricNode{
		ID:        m.ID,
		Name:      m.Name,
		Category:  m.Category,
		Value:     m.Value,
		Unit:      &unit,
		CreatedAt: m.CreatedAt,
		UpdatedAt: m.UpdatedAt,
	}
}

func healthToNode(h *analytics.HealthStatus) *HealthStatusNode {
	return &HealthStatusNode{
		IsHealthy:  h.Status == "healthy",
		DuckDB:     h.DuckDB,
		Storage:    h.Storage,
		Migrations: h.Migrations,
	}
}

func webhookToNode(w *analytics.Webhook) *WebhookNode {
	return &WebhookNode{
		ID:        w.ID,
		URL:       w.URL,
		EventType: w.EventType,
		Active:    w.Active,
		CreatedAt: w.CreatedAt,
		UpdatedAt: w.UpdatedAt,
	}
}

func stringPtr(s *string) *string {
	return s
}

func generateID() string {
	return fmt.Sprintf("%d", time.Now().UnixNano())
}
