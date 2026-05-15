package graphql

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/aegion/aegion/internal/xlog"
	"github.com/aegion/aegion/modules/analytics"
	"github.com/aegion/aegion/modules/analytics/rbac"
)

// Resolver handles GraphQL query, mutation, and subscription resolving.
type Resolver struct {
	logger    *xlog.Logger
	store     Store
	startTime time.Time
	// Subscription channels
	eventSubs       map[string]chan *EventNode
	metricSubs      map[string]chan *MetricNode
	dashboardSubs   map[string]chan *DashboardNode
	subscriptionsMu sync.RWMutex
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
func NewResolver(logger *xlog.Logger, store Store) *Resolver {
	return &Resolver{
		logger:        logger,
		store:         store,
		startTime:     time.Now(),
		eventSubs:     make(map[string]chan *EventNode),
		metricSubs:    make(map[string]chan *MetricNode),
		dashboardSubs: make(map[string]chan *DashboardNode),
	}
}

// ==================== Query Resolvers ====================

// Events resolves the events query with filtering and pagination.
func (r *Resolver) Events(ctx context.Context, filter *EventFilter, first *int, after *string, sort *SortInput) (*EventConnection, error) {
	if _, err := requireGraphQLPermission(ctx, rbac.PermViewEvents); err != nil {
		return nil, err
	}

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
		_, _ = fmt.Sscanf(*after, "%d", &offset)
	}

	events, total, err := r.store.ListEvents(ctx, filter, limit, offset)
	if err != nil {
		r.logger.ErrorContext(ctx, "failed to list events", "error", err)
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
	if _, err := requireGraphQLPermission(ctx, rbac.PermViewEvents); err != nil {
		return nil, err
	}

	event, err := r.store.GetEvent(ctx, id)
	if err != nil {
		r.logger.ErrorContext(ctx, "failed to get event", "error", err, "id", id)
		return nil, fmt.Errorf("failed to get event: %w", err)
	}
	if event == nil {
		return nil, errors.New("event not found")
	}
	return eventToNode(event), nil
}

// Dashboards resolves dashboards query with optional filtering.
func (r *Resolver) Dashboards(ctx context.Context, isDefault *bool, public *bool) ([]*DashboardNode, error) {
	ownerID, err := requireGraphQLPermission(ctx, rbac.PermViewDashboards)
	if err != nil {
		return nil, err
	}

	var dashboards []*analytics.Dashboard

	if public != nil && *public {
		dashboards, err = r.store.ListDashboards(ctx, nil, public)
	} else {
		dashboards, err = r.store.ListDashboards(ctx, &ownerID, nil)
	}

	if err != nil {
		r.logger.ErrorContext(ctx, "failed to list dashboards", "error", err)
		return nil, fmt.Errorf("failed to list dashboards: %w", err)
	}

	nodes := make([]*DashboardNode, len(dashboards))
	filtered := make([]*DashboardNode, 0, len(dashboards))
	for _, d := range dashboards {
		allowed, err := canReadDashboard(ctx, ownerID, d)
		if err != nil {
			return nil, err
		}
		if !allowed {
			continue
		}
		if isDefault != nil {
			if configDefault, ok := d.Config["is_default"].(bool); ok && configDefault != *isDefault {
				continue
			}
		}
		filtered = append(filtered, dashboardToNode(d))
	}
	copy(nodes, filtered)
	return filtered, nil
}

// Dashboard resolves a single dashboard by ID.
func (r *Resolver) Dashboard(ctx context.Context, id string) (*DashboardNode, error) {
	ownerID, err := requireGraphQLPermission(ctx, rbac.PermViewDashboards)
	if err != nil {
		return nil, err
	}

	dashboard, err := r.store.GetDashboard(ctx, id)
	if err != nil {
		r.logger.ErrorContext(ctx, "failed to get dashboard", "error", err, "id", id)
		return nil, fmt.Errorf("failed to get dashboard: %w", err)
	}
	if dashboard == nil {
		return nil, errors.New("dashboard not found")
	}
	allowed, err := canReadDashboard(ctx, ownerID, dashboard)
	if err != nil {
		return nil, err
	}
	if !allowed {
		return nil, errors.New("forbidden: dashboard access denied")
	}
	return dashboardToNode(dashboard), nil
}

// Queries resolves saved queries.
func (r *Resolver) Queries(ctx context.Context, limit *int, offset *int) ([]*SavedQueryNode, error) {
	ownerID, err := requireGraphQLPermission(ctx, rbac.PermModifyQueries)
	if err != nil {
		return nil, err
	}

	queries, err := r.store.ListQueries(ctx, &ownerID)
	if err != nil {
		r.logger.ErrorContext(ctx, "failed to list queries", "error", err)
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
	ownerID, err := requireGraphQLPermission(ctx, rbac.PermModifyQueries)
	if err != nil {
		return nil, err
	}

	query, err := r.store.GetQuery(ctx, id)
	if err != nil {
		r.logger.ErrorContext(ctx, "failed to get query", "error", err, "id", id)
		return nil, fmt.Errorf("failed to get query: %w", err)
	}
	if query == nil {
		return nil, errors.New("query not found")
	}
	if query.OwnerID != ownerID && !isGraphQLAdmin(ctx, ownerID) {
		return nil, errors.New("forbidden: query access denied")
	}
	return queryToNode(query), nil
}

// Health resolves the health status query.
func (r *Resolver) Health(ctx context.Context) (*HealthStatusNode, error) {
	health, err := r.store.GetHealth(ctx)
	if err != nil {
		r.logger.ErrorContext(ctx, "failed to get health status", "error", err)
		return nil, fmt.Errorf("failed to get health status: %w", err)
	}
	return healthToNode(health), nil
}

// Stats resolves the system statistics query.
func (r *Resolver) Stats(ctx context.Context) (*SystemStatsNode, error) {
	eventCount, err := r.store.CountEvents(ctx)
	if err != nil {
		r.logger.ErrorContext(ctx, "failed to count events", "error", err)
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
	if _, err := requireGraphQLPermission(ctx, rbac.PermViewDashboards); err != nil {
		return nil, err
	}

	metrics, err := r.store.ListMetrics(ctx, category)
	if err != nil {
		r.logger.ErrorContext(ctx, "failed to list metrics", "error", err)
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
	ownerID, err := requireGraphQLPermission(ctx, rbac.PermManageDashboards)
	if err != nil {
		return &CreateDashboardPayload{
			Errors: []*ErrorNode{{Message: err.Error()}},
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
		r.logger.ErrorContext(ctx, "failed to create dashboard", "error", err)
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
	ownerID, err := requireGraphQLPermission(ctx, rbac.PermManageDashboards)
	if err != nil {
		return &UpdateDashboardPayload{
			Errors: []*ErrorNode{{Message: err.Error()}},
		}, nil
	}

	dashboard, err := r.store.GetDashboard(ctx, id)
	if err != nil || dashboard == nil {
		return &UpdateDashboardPayload{
			Errors: []*ErrorNode{{Message: "dashboard not found"}},
		}, nil
	}

	// Check authorization
	if dashboard.OwnerID != ownerID && !isGraphQLAdmin(ctx, ownerID) {
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
		r.logger.ErrorContext(ctx, "failed to update dashboard", "error", err)
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
	ownerID, err := requireGraphQLPermission(ctx, rbac.PermManageDashboards)
	if err != nil {
		return &DeleteDashboardPayload{
			Errors: []*ErrorNode{{Message: err.Error()}},
		}, nil
	}

	dashboard, err := r.store.GetDashboard(ctx, id)
	if err != nil || dashboard == nil {
		return &DeleteDashboardPayload{
			Errors: []*ErrorNode{{Message: "dashboard not found"}},
		}, nil
	}

	// Check authorization
	if dashboard.OwnerID != ownerID && !isGraphQLAdmin(ctx, ownerID) {
		return &DeleteDashboardPayload{
			Errors: []*ErrorNode{{Message: "unauthorized: cannot delete dashboard owned by another user"}},
		}, nil
	}

	if err := r.store.DeleteDashboard(ctx, id); err != nil {
		r.logger.ErrorContext(ctx, "failed to delete dashboard", "error", err)
		return &DeleteDashboardPayload{
			Errors: []*ErrorNode{{Message: fmt.Sprintf("failed to delete dashboard: %v", err)}},
		}, nil
	}

	return &DeleteDashboardPayload{Success: true}, nil
}

// SaveQuery resolves the saveQuery mutation.
func (r *Resolver) SaveQuery(ctx context.Context, input *SaveQueryInput) (*SaveQueryPayload, error) {
	ownerID, err := requireGraphQLPermission(ctx, rbac.PermModifyQueries)
	if err != nil {
		return &SaveQueryPayload{
			Errors: []*ErrorNode{{Message: err.Error()}},
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
		r.logger.ErrorContext(ctx, "failed to save query", "error", err)
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
	ownerID, err := requireGraphQLPermission(ctx, rbac.PermModifyQueries)
	if err != nil {
		return &DeleteQueryPayload{
			Errors: []*ErrorNode{{Message: err.Error()}},
		}, nil
	}

	query, err := r.store.GetQuery(ctx, id)
	if err != nil || query == nil {
		return &DeleteQueryPayload{
			Errors: []*ErrorNode{{Message: "query not found"}},
		}, nil
	}

	// Check authorization
	if query.OwnerID != ownerID && !isGraphQLAdmin(ctx, ownerID) {
		return &DeleteQueryPayload{
			Errors: []*ErrorNode{{Message: "unauthorized: cannot delete query owned by another user"}},
		}, nil
	}

	if err := r.store.DeleteQuery(ctx, id); err != nil {
		r.logger.ErrorContext(ctx, "failed to delete query", "error", err)
		return &DeleteQueryPayload{
			Errors: []*ErrorNode{{Message: fmt.Sprintf("failed to delete query: %v", err)}},
		}, nil
	}

	return &DeleteQueryPayload{Success: true}, nil
}

// CreateReport resolves the createReport mutation (stub).
func (r *Resolver) CreateReport(ctx context.Context, input *CreateReportInput) (*CreateReportPayload, error) {
	if _, err := requireGraphQLPermission(ctx, rbac.PermExportData); err != nil {
		return &CreateReportPayload{
			Errors: []*ErrorNode{{Message: err.Error()}},
		}, nil
	}

	reportURL := "/reports/sample-report.pdf"
	return &CreateReportPayload{
		ReportURL: &reportURL,
	}, nil
}

// CreateWebhook resolves the createWebhook mutation.
func (r *Resolver) CreateWebhook(ctx context.Context, input *CreateWebhookInput) (*CreateWebhookPayload, error) {
	ownerID, err := requireGraphQLPermission(ctx, rbac.PermManageWebhooks)
	if err != nil {
		return &CreateWebhookPayload{
			Errors: []*ErrorNode{{Message: err.Error()}},
		}, nil
	}

	active := true
	if input.Active != nil {
		active = *input.Active
	}

	webhook := &analytics.Webhook{
		ID:         generateID(),
		URL:        input.URL,
		EventTypes: []string{input.EventType},
		Secret:     generateID(), // Generate random secret
		Active:     active,
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}

	created, err := r.store.CreateWebhook(ctx, webhook)
	if err != nil {
		r.logger.ErrorContext(ctx, "failed to create webhook", "error", err)
		return &CreateWebhookPayload{
			Errors: []*ErrorNode{{Message: fmt.Sprintf("failed to create webhook: %v", err)}},
		}, nil
	}
	manager := rbac.FromContext(ctx)
	_ = manager.SetWebhookOwner(created.ID, ownerID)

	return &CreateWebhookPayload{
		Webhook: webhookToNode(created),
	}, nil
}

// ExecuteQuery resolves the executeQuery mutation.
func (r *Resolver) ExecuteQuery(ctx context.Context, sql string, timeout *int) (*ExecuteQueryPayload, error) {
	_ = sql
	_ = timeout

	return &ExecuteQueryPayload{
		Errors: []*ErrorNode{{Message: "executeQuery is disabled for security reasons"}},
	}, nil
}

// ==================== Subscription Resolvers ====================

// OnNewEvent subscribes to new events matching a filter.
func (r *Resolver) OnNewEvent(ctx context.Context, filter *EventFilter) (<-chan *EventNode, error) {
	ch := make(chan *EventNode, 10)
	subID := generateID()
	r.subscriptionsMu.Lock()
	r.eventSubs[subID] = ch
	r.subscriptionsMu.Unlock()

	go func() {
		<-ctx.Done()
		r.subscriptionsMu.Lock()
		delete(r.eventSubs, subID)
		r.subscriptionsMu.Unlock()
		close(ch)
	}()

	return ch, nil
}

// OnMetricUpdate subscribes to metric updates.
func (r *Resolver) OnMetricUpdate(ctx context.Context, category *string) (<-chan *MetricNode, error) {
	ch := make(chan *MetricNode, 10)
	subID := generateID()
	r.subscriptionsMu.Lock()
	r.metricSubs[subID] = ch
	r.subscriptionsMu.Unlock()

	go func() {
		<-ctx.Done()
		r.subscriptionsMu.Lock()
		delete(r.metricSubs, subID)
		r.subscriptionsMu.Unlock()
		close(ch)
	}()

	return ch, nil
}

// OnDashboardChange subscribes to dashboard changes.
func (r *Resolver) OnDashboardChange(ctx context.Context, dashboardID string) (<-chan *DashboardNode, error) {
	ch := make(chan *DashboardNode, 10)
	subID := generateID()
	r.subscriptionsMu.Lock()
	r.dashboardSubs[subID] = ch
	r.subscriptionsMu.Unlock()

	go func() {
		<-ctx.Done()
		r.subscriptionsMu.Lock()
		delete(r.dashboardSubs, subID)
		r.subscriptionsMu.Unlock()
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
	eventType := ""
	if len(w.EventTypes) > 0 {
		eventType = w.EventTypes[0]
	}

	return &WebhookNode{
		ID:        w.ID,
		URL:       w.URL,
		EventType: eventType,
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

func requireGraphQLPermission(ctx context.Context, permission rbac.Permission) (string, error) {
	userID, ok := ctx.Value("userID").(string)
	if !ok || userID == "" {
		return "", errors.New("unauthorized: user not found in context")
	}

	manager := rbac.FromContext(ctx)
	allowed, err := manager.HasPermission(userID, permission)
	if err != nil {
		return "", fmt.Errorf("forbidden: failed to resolve permission %s: %w", permission, err)
	}
	if !allowed {
		return "", fmt.Errorf("forbidden: missing permission %s", permission)
	}

	return userID, nil
}

func canReadDashboard(ctx context.Context, userID string, dashboard *analytics.Dashboard) (bool, error) {
	if dashboard == nil {
		return false, nil
	}
	if dashboard.Public || dashboard.OwnerID == userID {
		return true, nil
	}

	manager := rbac.FromContext(ctx)
	return manager.CanAccessDashboard(userID, dashboard.ID)
}

func isGraphQLAdmin(ctx context.Context, userID string) bool {
	manager := rbac.FromContext(ctx)
	role, err := manager.GetUserRole(userID)
	return err == nil && role == rbac.RoleAdmin
}
