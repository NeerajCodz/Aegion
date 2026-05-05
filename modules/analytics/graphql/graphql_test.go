package graphql

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/aegion/aegion/internal/platform/logger"
	"github.com/aegion/aegion/modules/analytics"
	"github.com/aegion/aegion/modules/analytics/rbac"
	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// MockStore implements the Store interface for testing.
type MockStore struct {
	events             []*analytics.Event
	dashboards         []*analytics.Dashboard
	queries            []*analytics.Query
	metrics            []*analytics.Metric
	webhooks           []*analytics.Webhook
	shouldFailEventGet bool
	shouldFailDashGet  bool
}

func NewMockStore() *MockStore {
	return &MockStore{
		events:     make([]*analytics.Event, 0),
		dashboards: make([]*analytics.Dashboard, 0),
		queries:    make([]*analytics.Query, 0),
		metrics:    make([]*analytics.Metric, 0),
		webhooks:   make([]*analytics.Webhook, 0),
	}
}

// Events
func (ms *MockStore) GetEvent(ctx context.Context, id string) (*analytics.Event, error) {
	if ms.shouldFailEventGet {
		return nil, assert.AnError
	}
	for _, e := range ms.events {
		if e.ID == id {
			return e, nil
		}
	}
	return nil, nil
}

func (ms *MockStore) ListEvents(ctx context.Context, filter *EventFilter, limit int, offset int) ([]*analytics.Event, int, error) {
	return ms.events, len(ms.events), nil
}

func (ms *MockStore) CountEvents(ctx context.Context) (int, error) {
	return len(ms.events), nil
}

// Dashboards
func (ms *MockStore) CreateDashboard(ctx context.Context, dashboard *analytics.Dashboard) (*analytics.Dashboard, error) {
	ms.dashboards = append(ms.dashboards, dashboard)
	return dashboard, nil
}

func (ms *MockStore) GetDashboard(ctx context.Context, id string) (*analytics.Dashboard, error) {
	if ms.shouldFailDashGet {
		return nil, assert.AnError
	}
	for _, d := range ms.dashboards {
		if d.ID == id {
			return d, nil
		}
	}
	return nil, nil
}

func (ms *MockStore) ListDashboards(ctx context.Context, ownerID *string, public *bool) ([]*analytics.Dashboard, error) {
	var filtered []*analytics.Dashboard
	for _, d := range ms.dashboards {
		if ownerID != nil && d.OwnerID != *ownerID {
			continue
		}
		if public != nil && d.Public != *public {
			continue
		}
		filtered = append(filtered, d)
	}
	return filtered, nil
}

func (ms *MockStore) UpdateDashboard(ctx context.Context, dashboard *analytics.Dashboard) (*analytics.Dashboard, error) {
	for i, d := range ms.dashboards {
		if d.ID == dashboard.ID {
			ms.dashboards[i] = dashboard
			return dashboard, nil
		}
	}
	return nil, assert.AnError
}

func (ms *MockStore) DeleteDashboard(ctx context.Context, id string) error {
	for i, d := range ms.dashboards {
		if d.ID == id {
			ms.dashboards = append(ms.dashboards[:i], ms.dashboards[i+1:]...)
			return nil
		}
	}
	return assert.AnError
}

// Queries
func (ms *MockStore) CreateQuery(ctx context.Context, query *analytics.Query) (*analytics.Query, error) {
	ms.queries = append(ms.queries, query)
	return query, nil
}

func (ms *MockStore) GetQuery(ctx context.Context, id string) (*analytics.Query, error) {
	for _, q := range ms.queries {
		if q.ID == id {
			return q, nil
		}
	}
	return nil, nil
}

func (ms *MockStore) ListQueries(ctx context.Context, ownerID *string) ([]*analytics.Query, error) {
	var filtered []*analytics.Query
	for _, q := range ms.queries {
		if ownerID != nil && q.OwnerID != *ownerID {
			continue
		}
		filtered = append(filtered, q)
	}
	return filtered, nil
}

func (ms *MockStore) DeleteQuery(ctx context.Context, id string) error {
	for i, q := range ms.queries {
		if q.ID == id {
			ms.queries = append(ms.queries[:i], ms.queries[i+1:]...)
			return nil
		}
	}
	return assert.AnError
}

// Metrics
func (ms *MockStore) ListMetrics(ctx context.Context, category *string) ([]*analytics.Metric, error) {
	var filtered []*analytics.Metric
	for _, m := range ms.metrics {
		if category != nil && m.Category != *category {
			continue
		}
		filtered = append(filtered, m)
	}
	return filtered, nil
}

// Webhooks
func (ms *MockStore) CreateWebhook(ctx context.Context, webhook *analytics.Webhook) (*analytics.Webhook, error) {
	ms.webhooks = append(ms.webhooks, webhook)
	return webhook, nil
}

// Health
func (ms *MockStore) GetHealth(ctx context.Context) (*analytics.HealthStatus, error) {
	return &analytics.HealthStatus{
		DuckDB:     true,
		Storage:    true,
		Migrations: true,
		Status:     "healthy",
	}, nil
}

// Execute SQL
func (ms *MockStore) ExecuteSQL(ctx context.Context, sql string, timeout time.Duration) ([]map[string]interface{}, error) {
	return []map[string]interface{}{}, nil
}

// Tests

func TestResolverEvents(t *testing.T) {
	lgr := logger.TestLoggerDebug()
	store := NewMockStore()

	// Add test event
	event := &analytics.Event{
		ID:        "1",
		Category:  "auth",
		EventType: "login",
		Data:      map[string]interface{}{"user": "test"},
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	store.events = append(store.events, event)

	resolver := NewResolver(lgr, store)

	// Test query
	ctx := withGraphQLRole("test-user", rbac.RoleUser)
	conn, err := resolver.Events(ctx, nil, nil, nil, nil)

	assert.NoError(t, err)
	assert.NotNil(t, conn)
	assert.Equal(t, 1, conn.TotalCount)
	assert.Equal(t, 1, len(conn.Edges))
	assert.Equal(t, "1", conn.Edges[0].Node.ID)
}

func TestResolverEvent(t *testing.T) {
	lgr := logger.TestLoggerDebug()
	store := NewMockStore()

	event := &analytics.Event{
		ID:        "1",
		Category:  "auth",
		EventType: "login",
		Data:      map[string]interface{}{},
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	store.events = append(store.events, event)

	resolver := NewResolver(lgr, store)
	ctx := withGraphQLRole("test-user", rbac.RoleUser)

	node, err := resolver.Event(ctx, "1")
	assert.NoError(t, err)
	assert.NotNil(t, node)
	assert.Equal(t, "1", node.ID)
	assert.Equal(t, "auth", node.Category)
}

func TestResolverCreateDashboard(t *testing.T) {
	lgr := logger.TestLoggerDebug()
	store := NewMockStore()
	resolver := NewResolver(lgr, store)

	ctx := withGraphQLRole("test-user", rbac.RoleAnalyst)

	input := &CreateDashboardInput{
		Name:   "Test Dashboard",
		Config: map[string]interface{}{"title": "Test"},
		Public: boolPtr(false),
	}

	payload, err := resolver.CreateDashboard(ctx, input)
	assert.NoError(t, err)
	assert.NotNil(t, payload)
	assert.NotNil(t, payload.Dashboard)
	assert.Equal(t, "Test Dashboard", payload.Dashboard.Name)
	assert.Equal(t, "test-user", payload.Dashboard.OwnerID)
	assert.Len(t, store.dashboards, 1)
}

func TestResolverUpdateDashboard(t *testing.T) {
	lgr := logger.TestLoggerDebug()
	store := NewMockStore()
	resolver := NewResolver(lgr, store)

	// Create initial dashboard
	dashboard := &analytics.Dashboard{
		ID:      "1",
		Name:    "Old Name",
		Config:  map[string]interface{}{},
		OwnerID: "test-user",
		Public:  false,
	}
	store.dashboards = append(store.dashboards, dashboard)

	ctx := withGraphQLRole("test-user", rbac.RoleAnalyst)

	newName := "New Name"
	input := &UpdateDashboardInput{
		Name: &newName,
	}

	payload, err := resolver.UpdateDashboard(ctx, "1", input)
	assert.NoError(t, err)
	assert.NotNil(t, payload)
	assert.NotNil(t, payload.Dashboard)
	assert.Equal(t, "New Name", payload.Dashboard.Name)
}

func TestResolverDeleteDashboard(t *testing.T) {
	lgr := logger.TestLoggerDebug()
	store := NewMockStore()
	resolver := NewResolver(lgr, store)

	dashboard := &analytics.Dashboard{
		ID:      "1",
		Name:    "Test",
		Config:  map[string]interface{}{},
		OwnerID: "test-user",
		Public:  false,
	}
	store.dashboards = append(store.dashboards, dashboard)

	ctx := withGraphQLRole("test-user", rbac.RoleAnalyst)

	payload, err := resolver.DeleteDashboard(ctx, "1")
	assert.NoError(t, err)
	assert.NotNil(t, payload)
	assert.True(t, payload.Success)
	assert.Len(t, store.dashboards, 0)
}

func TestResolverSaveQuery(t *testing.T) {
	lgr := logger.TestLoggerDebug()
	store := NewMockStore()
	resolver := NewResolver(lgr, store)

	ctx := withGraphQLRole("test-user", rbac.RoleAnalyst)

	desc := "A test query"
	input := &SaveQueryInput{
		Name:        "Test Query",
		Description: &desc,
		SQL:         "SELECT * FROM events",
		IsPublic:    boolPtr(false),
	}

	payload, err := resolver.SaveQuery(ctx, input)
	assert.NoError(t, err)
	assert.NotNil(t, payload)
	assert.NotNil(t, payload.Query)
	assert.Equal(t, "Test Query", payload.Query.Name)
	assert.Equal(t, "test-user", payload.Query.OwnerID)
	assert.Len(t, store.queries, 1)
}

func TestResolverHealth(t *testing.T) {
	lgr := logger.TestLoggerDebug()
	store := NewMockStore()
	resolver := NewResolver(lgr, store)

	ctx := context.Background()

	health, err := resolver.Health(ctx)
	assert.NoError(t, err)
	assert.NotNil(t, health)
	assert.True(t, health.IsHealthy)
	assert.True(t, health.DuckDB)
	assert.True(t, health.Storage)
	assert.True(t, health.Migrations)
}

func TestResolverStats(t *testing.T) {
	lgr := logger.TestLoggerDebug()
	store := NewMockStore()
	resolver := NewResolver(lgr, store)

	// Add test event
	store.events = append(store.events, &analytics.Event{
		ID:        "1",
		CreatedAt: time.Now(),
	})

	ctx := context.Background()

	stats, err := resolver.Stats(ctx)
	assert.NoError(t, err)
	assert.NotNil(t, stats)
	assert.Equal(t, 1, stats.EventsTotal)
	assert.GreaterOrEqual(t, stats.Uptime, 0)
}

func TestResolverDashboardForbidsPrivateDashboardForOtherUser(t *testing.T) {
	lgr := logger.TestLoggerDebug()
	store := NewMockStore()
	resolver := NewResolver(lgr, store)

	store.dashboards = append(store.dashboards, &analytics.Dashboard{
		ID:      "private-dash",
		Name:    "Private",
		Config:  map[string]interface{}{},
		OwnerID: "owner-1",
		Public:  false,
	})

	_, err := resolver.Dashboard(withGraphQLRole("viewer-1", rbac.RoleViewer), "private-dash")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "forbidden")
}

func TestResolverQueryForbidsAccessToAnotherUsersSavedQuery(t *testing.T) {
	lgr := logger.TestLoggerDebug()
	store := NewMockStore()
	resolver := NewResolver(lgr, store)

	store.queries = append(store.queries, &analytics.Query{
		ID:      "query-1",
		Name:    "Owned Query",
		SQL:     "SELECT 1",
		OwnerID: "owner-1",
	})

	_, err := resolver.Query(withGraphQLRole("analyst-2", rbac.RoleAnalyst), "query-1")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "forbidden")
}

func TestRateLimiter(t *testing.T) {
	limiter := NewSimpleRateLimiter(3)

	ctx := context.Background()
	clientID := "test-client"

	// Should allow first 3 requests
	assert.True(t, limiter.AllowRequest(ctx, clientID))
	assert.Equal(t, 2, limiter.GetRemaining(clientID))

	assert.True(t, limiter.AllowRequest(ctx, clientID))
	assert.Equal(t, 1, limiter.GetRemaining(clientID))

	assert.True(t, limiter.AllowRequest(ctx, clientID))
	assert.Equal(t, 0, limiter.GetRemaining(clientID))

	// Should deny 4th request
	assert.False(t, limiter.AllowRequest(ctx, clientID))
}

func withGraphQLRole(userID string, role rbac.Role) context.Context {
	ctx := context.WithValue(context.Background(), "userID", userID)
	manager := rbac.NewManager()
	_ = manager.SetUserRole(userID, role)
	return rbac.WithManager(ctx, manager)
}

func TestComplexityAnalyzer(t *testing.T) {
	analyzer := NewSimpleComplexityAnalyzer(5)

	// Test depth validation
	depth, err := analyzer.ValidateDepth("{ { { } } }", 5)
	assert.NoError(t, err)
	assert.Equal(t, 3, depth)

	// Test depth exceeded
	depth, err = analyzer.ValidateDepth("{ { { { { { } } } } } }", 5)
	assert.Error(t, err)
	assert.Equal(t, 6, depth)

	// Test complexity calculation
	complexity, err := analyzer.AnalyzeComplexity("{ field(arg: 1) { nested } }")
	assert.NoError(t, err)
	assert.Greater(t, complexity, 0)
}

func TestDirectiveRegistry(t *testing.T) {
	lgr := logger.TestLoggerDebug()
	registry := NewDirectiveRegistry(lgr)

	// Should register directive
	handler := func(ctx context.Context, next func() error, dirCtx *DirectiveContext) error {
		return next()
	}
	registry.RegisterDirective("test", handler)

	// Should retrieve directive
	retrieved, ok := registry.GetDirective("test")
	assert.True(t, ok)
	assert.NotNil(t, retrieved)

	// Should not find non-existent directive
	_, ok = registry.GetDirective("nonexistent")
	assert.False(t, ok)
}

func TestDirectiveParserParsesSchemaDefinitions(t *testing.T) {
	parser := NewDirectiveParser(logger.TestLogger())

	directives := parser.ParseDirectives(SchemaDefinition)

	auth, ok := directives["auth"].(map[string]interface{})
	require.True(t, ok)
	args, ok := auth["arguments"].(map[string]interface{})
	require.True(t, ok)
	requiredArg, ok := args["required"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "Boolean", requiredArg["type"])
	assert.Contains(t, auth["locations"], "FIELD_DEFINITION")
}

func TestDirectiveValidatorValidatesRegisteredDirectives(t *testing.T) {
	registry := NewDirectiveRegistry(logger.TestLogger())
	RegisterBuiltInDirectives(registry)
	validator := NewDirectiveValidator(registry, logger.TestLogger())

	require.NoError(t, validator.ValidateDirectiveUsage(`query { health @cache(ttl: 30) }`))
	require.NoError(t, validator.ValidateDirectiveUsage(`query { health @deprecated(reason: "old") }`))
	assert.Error(t, validator.ValidateDirectiveUsage(`query { health @unknown }`))
	assert.Error(t, validator.ValidateDirectiveUsage(`query { health @cache(ttl: nope) }`))
}

func TestSimpleCache(t *testing.T) {
	cache := NewSimpleCache()

	// Should not find key initially
	_, found := cache.Get("key1")
	assert.False(t, found)

	// Should store and retrieve value
	cache.Set("key1", "value1", 1*time.Second)
	val, found := cache.Get("key1")
	assert.True(t, found)
	assert.Equal(t, "value1", val)

	// Should expire after TTL
	time.Sleep(1100 * time.Millisecond)
	_, found = cache.Get("key1")
	assert.False(t, found)

	// Should clear cache
	cache.Set("key2", "value2", 10*time.Second)
	cache.Clear()
	_, found = cache.Get("key2")
	assert.False(t, found)
}

func TestDefaultConfig(t *testing.T) {
	config := DefaultConfig()

	assert.True(t, config.Enabled)
	assert.Equal(t, "/graphql", config.Endpoint)
	assert.True(t, config.EnablePlayground)
	assert.True(t, config.EnableIntrospection)
	assert.Equal(t, 10, config.MaxQueryDepth)
	assert.Equal(t, 1000, config.MaxQueryComplexity)
	assert.Equal(t, 30, config.QueryTimeoutSeconds)
	assert.Equal(t, 100, config.RateLimitPerMinute)
}

func TestServerRegisterRoutesServeMux(t *testing.T) {
	lgr := logger.TestLogger()
	store := NewMockStore()
	resolver := NewResolver(lgr, store)
	server := NewServer(lgr, resolver, NewSchemaBuilder(resolver), NewSimpleQueryExecutor(resolver, lgr))
	mux := http.NewServeMux()

	require.NoError(t, server.RegisterRoutes(mux, "/graphql"))

	playgroundReq := httptest.NewRequest(http.MethodGet, "/graphql/playground", nil)
	playgroundReq.Header.Set("Authorization", "Bearer user-1:session-token")
	playgroundResp := httptest.NewRecorder()
	mux.ServeHTTP(playgroundResp, playgroundReq)
	assert.Equal(t, http.StatusOK, playgroundResp.Code)

	unauthorizedReq := httptest.NewRequest(http.MethodGet, "/graphql/playground", nil)
	unauthorizedResp := httptest.NewRecorder()
	mux.ServeHTTP(unauthorizedResp, unauthorizedReq)
	assert.Equal(t, http.StatusUnauthorized, unauthorizedResp.Code)

	healthReq := httptest.NewRequest(http.MethodGet, "/graphql/health", nil)
	healthResp := httptest.NewRecorder()
	mux.ServeHTTP(healthResp, healthReq)
	assert.Equal(t, http.StatusOK, healthResp.Code)
}

func TestServerRegisterRoutesChiRouter(t *testing.T) {
	lgr := logger.TestLogger()
	store := NewMockStore()
	resolver := NewResolver(lgr, store)
	server := NewServer(lgr, resolver, NewSchemaBuilder(resolver), NewSimpleQueryExecutor(resolver, lgr))
	router := chi.NewRouter()

	require.NoError(t, server.RegisterRoutes(router, "/graphql"))

	req := httptest.NewRequest(http.MethodGet, "/graphql/introspection", nil)
	req.Header.Set("Authorization", "Bearer user-1:session-token")
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)
	assert.Equal(t, http.StatusOK, resp.Code)

	var payload map[string]interface{}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&payload))
	assert.Contains(t, payload, "data")
}

func TestServerRegisterRoutesRejectsUnsupportedRouter(t *testing.T) {
	lgr := logger.TestLogger()
	store := NewMockStore()
	resolver := NewResolver(lgr, store)
	server := NewServer(lgr, resolver, NewSchemaBuilder(resolver), NewSimpleQueryExecutor(resolver, lgr))

	assert.Error(t, server.RegisterRoutes(struct{}{}, "/graphql"))
}

func TestConfigValidation(t *testing.T) {
	config := DefaultConfig()
	assert.NoError(t, config.Validate())

	config.MaxQueryDepth = 0
	assert.Error(t, config.Validate())

	config = DefaultConfig()
	config.Endpoint = ""
	assert.Error(t, config.Validate())
}

// Helper functions

func boolPtr(b bool) *bool {
	return &b
}
