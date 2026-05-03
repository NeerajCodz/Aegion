package rest

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	analytics "github.com/aegion/aegion/modules/analytics"
	"github.com/aegion/aegion/modules/analytics/webhooks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/aegion/aegion/internal/platform/logger"
)

// MockDatabase implements Database interface for testing
type MockDatabase struct {
	queryResults map[string][]map[string]interface{}
	queryFn      func(ctx context.Context, sql string) ([]map[string]interface{}, error)
	queryErr     error
	countResult  int
	countErr     error
}

func (m *MockDatabase) Query(ctx context.Context, sql string) ([]map[string]interface{}, error) {
	if m.queryFn != nil {
		return m.queryFn(ctx, sql)
	}
	if m.queryErr != nil {
		return nil, m.queryErr
	}

	// Simple mock: return predefined results
	if results, ok := m.queryResults[sql]; ok {
		return results, nil
	}

	return []map[string]interface{}{}, nil
}

func (m *MockDatabase) Count(ctx context.Context, sql string) (int, error) {
	if m.countErr != nil {
		return 0, m.countErr
	}
	return m.countResult, nil
}

func newTestHandler(db Database) *Handler {
	return NewHandler(HandlerDeps{
		Logger:  logger.TestLogger(),
		Config:  Config{DefaultPageSize: 100, MaxPageSize: 10000, QueryTimeoutSeconds: 300},
		Queries: NewQueryBuilder(db),
		Exports: NewExportBuilder(db),
		Cache:   NewCache(),
	})
}

type mockWebhookManager struct {
	registerFn           func(ctx context.Context, userID string, req *webhooks.WebhookRequest) (*analytics.Webhook, error)
	updateFn             func(ctx context.Context, userID, webhookID string, req *webhooks.WebhookRequest) (*analytics.Webhook, error)
	deleteFn             func(ctx context.Context, userID, webhookID string) error
	listFn               func(ctx context.Context, userID string) ([]*analytics.Webhook, error)
	getFn                func(ctx context.Context, webhookID string) (*analytics.Webhook, error)
	testFn               func(ctx context.Context, webhookID string) (string, error)
	getHistoryFn         func(ctx context.Context, webhookID string, limit int) ([]*analytics.WebhookDelivery, error)
	getDeliveryFn        func(ctx context.Context, deliveryID string) (*analytics.WebhookDelivery, error)
	replayFn             func(ctx context.Context, deliveryID string) error
}

func (m *mockWebhookManager) RegisterWebhook(ctx context.Context, userID string, req *webhooks.WebhookRequest) (*analytics.Webhook, error) {
	return m.registerFn(ctx, userID, req)
}

func (m *mockWebhookManager) UpdateWebhook(ctx context.Context, userID, webhookID string, req *webhooks.WebhookRequest) (*analytics.Webhook, error) {
	return m.updateFn(ctx, userID, webhookID, req)
}

func (m *mockWebhookManager) DeleteWebhook(ctx context.Context, userID, webhookID string) error {
	return m.deleteFn(ctx, userID, webhookID)
}

func (m *mockWebhookManager) ListWebhooks(ctx context.Context, userID string) ([]*analytics.Webhook, error) {
	return m.listFn(ctx, userID)
}

func (m *mockWebhookManager) GetWebhook(ctx context.Context, webhookID string) (*analytics.Webhook, error) {
	return m.getFn(ctx, webhookID)
}

func (m *mockWebhookManager) TestWebhook(ctx context.Context, webhookID string) (string, error) {
	return m.testFn(ctx, webhookID)
}

func (m *mockWebhookManager) GetDeliveryHistory(ctx context.Context, webhookID string, limit int) ([]*analytics.WebhookDelivery, error) {
	return m.getHistoryFn(ctx, webhookID, limit)
}

func (m *mockWebhookManager) GetDelivery(ctx context.Context, deliveryID string) (*analytics.WebhookDelivery, error) {
	return m.getDeliveryFn(ctx, deliveryID)
}

func (m *mockWebhookManager) ReplayEvent(ctx context.Context, deliveryID string) error {
	return m.replayFn(ctx, deliveryID)
}

func newWebhookTestHandler(manager WebhookManager) *Handler {
	return NewHandler(HandlerDeps{
		Logger:         logger.TestLogger(),
		Config:         Config{DefaultPageSize: 100, MaxPageSize: 10000, QueryTimeoutSeconds: 300},
		Queries:        NewQueryBuilder(&MockDatabase{}),
		Exports:        NewExportBuilder(&MockDatabase{}),
		Cache:          NewCache(),
		WebhookManager: manager,
	})
}

func TestQueryBuilder_BuildQuery_Basic(t *testing.T) {
	db := &MockDatabase{
		queryResults: make(map[string][]map[string]interface{}),
	}

	qb := NewQueryBuilder(db)

	req := QueryRequest{
		Fields:   []string{"id", "name"},
		PageSize: 100,
		Page:     1,
	}

	sql, err := qb.BuildQuery(context.Background(), req, "events")
	require.NoError(t, err)
	assert.Contains(t, sql, "SELECT * FROM events")
	assert.Contains(t, sql, "LIMIT 100")
	assert.Contains(t, sql, "OFFSET 0")
}

func TestQueryBuilder_BuildQuery_WithFilters(t *testing.T) {
	db := &MockDatabase{
		queryResults: make(map[string][]map[string]interface{}),
	}

	qb := NewQueryBuilder(db)

	req := QueryRequest{
		Filters: map[string]interface{}{
			"category": "login",
		},
		PageSize: 50,
		Page:     2,
	}

	sql, err := qb.BuildQuery(context.Background(), req, "events")
	require.NoError(t, err)
	assert.Contains(t, sql, "WHERE")
	assert.Contains(t, sql, "category")
	assert.Contains(t, sql, "LIMIT 50")
	assert.Contains(t, sql, "OFFSET 50")
}

func TestQueryBuilder_BuildQuery_WithOperators(t *testing.T) {
	db := &MockDatabase{
		queryResults: make(map[string][]map[string]interface{}),
	}

	qb := NewQueryBuilder(db)

	req := QueryRequest{
		Filters: map[string]interface{}{
			"count": map[string]interface{}{
				"$gt": 10,
			},
		},
		PageSize: 100,
		Page:     1,
	}

	sql, err := qb.BuildQuery(context.Background(), req, "events")
	require.NoError(t, err)
	assert.Contains(t, sql, "count > 10")
}

func TestQueryBuilder_BuildQuery_WithTimeRange(t *testing.T) {
	db := &MockDatabase{
		queryResults: make(map[string][]map[string]interface{}),
	}

	qb := NewQueryBuilder(db)

	req := QueryRequest{
		TimeRange: &TimeRange{
			Value: 24,
			Unit:  "h",
		},
		PageSize: 100,
		Page:     1,
	}

	sql, err := qb.BuildQuery(context.Background(), req, "events")
	require.NoError(t, err)
	assert.Contains(t, sql, "created_at")
}

func TestQueryBuilder_BuildQuery_WithSorting(t *testing.T) {
	db := &MockDatabase{
		queryResults: make(map[string][]map[string]interface{}),
	}

	qb := NewQueryBuilder(db)

	req := QueryRequest{
		Sort: []SortField{
			{Field: "created_at", Direction: "desc"},
		},
		PageSize: 100,
		Page:     1,
	}

	sql, err := qb.BuildQuery(context.Background(), req, "events")
	require.NoError(t, err)
	assert.Contains(t, sql, "ORDER BY created_at DESC")
}

func TestHandlerListEvents_Success(t *testing.T) {
	db := &MockDatabase{
		queryResults: map[string][]map[string]interface{}{
			"SELECT * FROM events ORDER BY created_at DESC LIMIT 100 OFFSET 0": {
				{"id": "1", "name": "event1"},
				{"id": "2", "name": "event2"},
			},
		},
		countResult: 2,
	}

	h := newTestHandler(db)

	req := httptest.NewRequest("GET", "/events", nil)
	req = req.WithContext(withUserID(req.Context(), "user1"))
	w := httptest.NewRecorder()

	h.ListEvents(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp Response
	err := json.NewDecoder(w.Body).Decode(&resp)
	require.NoError(t, err)

	assert.NotNil(t, resp.Data)
	assert.NotNil(t, resp.Meta)
}

func TestHandlerHealth_Success(t *testing.T) {
	db := &MockDatabase{}
	h := newTestHandler(db)

	req := httptest.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()

	h.Health(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp Response
	err := json.NewDecoder(w.Body).Decode(&resp)
	require.NoError(t, err)

	health, ok := resp.Data.(map[string]interface{})
	assert.True(t, ok)
	assert.NotEmpty(t, health)
}

func TestHandlerStats_Success(t *testing.T) {
	db := &MockDatabase{}
	h := newTestHandler(db)

	req := httptest.NewRequest("GET", "/stats", nil)
	w := httptest.NewRecorder()

	h.Stats(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestHandlerExportFormats_Success(t *testing.T) {
	db := &MockDatabase{}
	h := newTestHandler(db)

	req := httptest.NewRequest("GET", "/export-formats", nil)
	w := httptest.NewRecorder()

	h.ExportFormats(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp Response
	err := json.NewDecoder(w.Body).Decode(&resp)
	require.NoError(t, err)

	assert.NotNil(t, resp.Data)
}

func TestValidatorQueryRequest_Valid(t *testing.T) {
	v := NewValidator()

	req := QueryRequest{
		PageSize: 100,
		Page:     1,
	}

	err := v.ValidateQueryRequest(req)
	assert.NoError(t, err)
}

func TestValidatorQueryRequest_InvalidPageSize(t *testing.T) {
	v := NewValidator()

	req := QueryRequest{
		PageSize: -1,
		Page:     1,
	}

	err := v.ValidateQueryRequest(req)
	assert.Error(t, err)
}

func TestValidatorDashboardRequest_Valid(t *testing.T) {
	v := NewValidator()

	req := DashboardRequest{
		Name:   "Test Dashboard",
		Config: map[string]interface{}{"test": "value"},
	}

	err := v.ValidateDashboardRequest(req)
	assert.NoError(t, err)
}

func TestValidatorDashboardRequest_MissingName(t *testing.T) {
	v := NewValidator()

	req := DashboardRequest{
		Config: map[string]interface{}{"test": "value"},
	}

	err := v.ValidateDashboardRequest(req)
	assert.Error(t, err)
}

func TestValidatorQuerySaveRequest_Valid(t *testing.T) {
	v := NewValidator()

	req := QuerySaveRequest{
		Name: "Test Query",
		SQL:  "SELECT * FROM events",
	}

	err := v.ValidateQuerySaveRequest(req)
	assert.NoError(t, err)
}

func TestValidatorQuerySaveRequest_InvalidSQL(t *testing.T) {
	v := NewValidator()

	req := QuerySaveRequest{
		Name: "Test Query",
		SQL:  "DROP TABLE events",
	}

	err := v.ValidateQuerySaveRequest(req)
	assert.Error(t, err)
}

func TestCache_GetSet(t *testing.T) {
	cache := NewCache()

	err := cache.Set(context.Background(), "key1", "value1", 5*time.Minute)
	require.NoError(t, err)

	val, found, err := cache.Get(context.Background(), "key1")
	require.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, "value1", val)
}

func TestCache_Expiration(t *testing.T) {
	cache := NewCache()

	err := cache.Set(context.Background(), "key1", "value1", 1*time.Millisecond)
	require.NoError(t, err)

	time.Sleep(10 * time.Millisecond)

	val, found, err := cache.Get(context.Background(), "key1")
	require.NoError(t, err)
	assert.False(t, found)
	assert.Nil(t, val)
}

func TestCache_Delete(t *testing.T) {
	cache := NewCache()

	err := cache.Set(context.Background(), "key1", "value1", 5*time.Minute)
	require.NoError(t, err)

	err = cache.Delete(context.Background(), "key1")
	require.NoError(t, err)

	val, found, err := cache.Get(context.Background(), "key1")
	require.NoError(t, err)
	assert.False(t, found)
	assert.Nil(t, val)
}

func TestRateLimiter_Allow(t *testing.T) {
	limiter := NewRateLimiter(2)

	// First request should be allowed
	assert.True(t, limiter.Allow("user1"))

	// Second request should be allowed
	assert.True(t, limiter.Allow("user1"))

	// Third request should be denied
	assert.False(t, limiter.Allow("user1"))

	// Different user should have separate limit
	assert.True(t, limiter.Allow("user2"))
}

func TestSanitizeInput(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"normal text", "normal text"},
		{"text'with'quotes", "text'with'quotes"},
		{"text\nwith\nnewlines", "text with newlines"},
	}

	for _, tt := range tests {
		result := SanitizeInput(tt.input)
		assert.Equal(t, tt.expected, result)
	}
}

func TestExportBuilder_CSV(t *testing.T) {
	db := &MockDatabase{
		queryResults: map[string][]map[string]interface{}{
			"SELECT * FROM events": {
				{"id": "1", "name": "event1"},
				{"id": "2", "name": "event2"},
			},
		},
	}

	eb := NewExportBuilder(db)

	w := httptest.NewRecorder()
	err := eb.ExportCSV(context.Background(), "SELECT * FROM events", w)
	require.NoError(t, err)

	body := w.Body.String()
	assert.Contains(t, body, "id")
	assert.Contains(t, body, "name")
	assert.Contains(t, body, "event1")
}

func TestExportBuilder_JSON(t *testing.T) {
	db := &MockDatabase{
		queryResults: map[string][]map[string]interface{}{
			"SELECT * FROM events": {
				{"id": "1", "name": "event1"},
			},
		},
	}

	eb := NewExportBuilder(db)

	w := httptest.NewRecorder()
	err := eb.ExportJSON(context.Background(), "SELECT * FROM events", w)
	require.NoError(t, err)

	body := w.Body.String()
	assert.Contains(t, body, "id")
	assert.Contains(t, body, "event1")
}

func TestInitialize_Success(t *testing.T) {
	db := &MockDatabase{}

	handler, err := Initialize(InitParams{
		Config: Config{
			BasePath:              "/api/v1/analytics",
			RateLimit:             1000,
			QueryTimeoutSeconds:   300,
			ResultCacheTTLMinutes: 15,
			MaxPageSize:           10000,
			DefaultPageSize:       100,
		},
		Logger: logger.TestLogger(),
		DB:     db,
	})

	require.NoError(t, err)
	assert.NotNil(t, handler)
}

func TestInitialize_MissingDatabase(t *testing.T) {
	handler, err := Initialize(InitParams{
		Config: Config{},
		Logger: logger.TestLogger(),
		DB:     nil,
	})

	assert.Error(t, err)
	assert.Nil(t, handler)
}

func TestHealthCheck_Success(t *testing.T) {
	db := &MockDatabase{}
	handler := newTestHandler(db)

	err := HealthCheck(context.Background(), handler)
	assert.NoError(t, err)
}

func TestHealthCheck_NilHandler(t *testing.T) {
	err := HealthCheck(context.Background(), nil)
	assert.Error(t, err)
}

func TestHandlerCreateDashboard_PersistsAnalyticsDashboardsRow(t *testing.T) {
	db := &MockDatabase{
		queryFn: func(ctx context.Context, sql string) ([]map[string]interface{}, error) {
			assert.Contains(t, sql, "INSERT INTO analytics_dashboards")
			assert.Contains(t, sql, `"layout":"grid"`)
			assert.Contains(t, sql, "'user1'")
			return []map[string]interface{}{
				{
					"id":          "dash-1",
					"name":        "Ops Dashboard",
					"description": "Operations",
					"config":      `{"layout":"grid"}`,
					"owner_id":    "user1",
					"public":      true,
					"pinned":      false,
				},
			}, nil
		},
	}
	h := newTestHandler(db)

	req := httptest.NewRequest(http.MethodPost, "/dashboards", strings.NewReader(`{"name":"Ops Dashboard","description":"Operations","config":{"layout":"grid"},"public":true}`))
	req = req.WithContext(withUserID(req.Context(), "user1"))
	w := httptest.NewRecorder()

	h.CreateDashboard(w, req)

	assert.Equal(t, http.StatusCreated, w.Code)
}

func TestHandlerUpdateDashboard_UsesAnalyticsDashboardsTable(t *testing.T) {
	call := 0
	db := &MockDatabase{
		queryFn: func(ctx context.Context, sql string) ([]map[string]interface{}, error) {
			call++
			if call == 1 {
				assert.Contains(t, sql, "SELECT owner_id FROM analytics_dashboards")
				return []map[string]interface{}{{"owner_id": "user1"}}, nil
			}
			assert.Contains(t, sql, "UPDATE analytics_dashboards")
			assert.Contains(t, sql, `"layout":"stacked"`)
			return []map[string]interface{}{
				{"id": "dash-1", "name": "Updated Dashboard", "owner_id": "user1", "public": false},
			}, nil
		},
	}
	h := newTestHandler(db)

	req := httptest.NewRequest(http.MethodPut, "/dashboards/dash-1", strings.NewReader(`{"name":"Updated Dashboard","description":"","config":{"layout":"stacked"},"public":false}`))
	req.SetPathValue("id", "dash-1")
	req = req.WithContext(withUserID(req.Context(), "user1"))
	w := httptest.NewRecorder()

	h.UpdateDashboard(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, 2, call)
}

func TestHandlerDeleteDashboard_DeletesFromAnalyticsDashboards(t *testing.T) {
	call := 0
	db := &MockDatabase{
		queryFn: func(ctx context.Context, sql string) ([]map[string]interface{}, error) {
			call++
			if call == 1 {
				assert.Contains(t, sql, "SELECT owner_id FROM analytics_dashboards")
				return []map[string]interface{}{{"owner_id": "user1"}}, nil
			}
			assert.Contains(t, sql, "DELETE FROM analytics_dashboards")
			return []map[string]interface{}{{"id": "dash-1"}}, nil
		},
	}
	h := newTestHandler(db)

	req := httptest.NewRequest(http.MethodDelete, "/dashboards/dash-1", nil)
	req.SetPathValue("id", "dash-1")
	req = req.WithContext(withUserID(req.Context(), "user1"))
	w := httptest.NewRecorder()

	h.DeleteDashboard(w, req)

	assert.Equal(t, http.StatusNoContent, w.Code)
	assert.Equal(t, 2, call)
}

func TestHandlerSaveQuery_PersistsAnalyticsQueriesRow(t *testing.T) {
	db := &MockDatabase{
		queryFn: func(ctx context.Context, sql string) ([]map[string]interface{}, error) {
			assert.Contains(t, sql, "INSERT INTO analytics_queries")
			assert.Contains(t, sql, "SELECT count(*) FROM analytics_events")
			return []map[string]interface{}{
				{"id": "query-1", "name": "Total Events", "owner_id": "user1"},
			}, nil
		},
	}
	h := newTestHandler(db)

	req := httptest.NewRequest(http.MethodPost, "/queries", strings.NewReader(`{"name":"Total Events","description":"All events","sql":"SELECT count(*) FROM analytics_events"}`))
	req = req.WithContext(withUserID(req.Context(), "user1"))
	w := httptest.NewRecorder()

	h.SaveQuery(w, req)

	assert.Equal(t, http.StatusCreated, w.Code)
}

func TestHandlerDeleteQuery_DeletesFromAnalyticsQueries(t *testing.T) {
	call := 0
	db := &MockDatabase{
		queryFn: func(ctx context.Context, sql string) ([]map[string]interface{}, error) {
			call++
			if call == 1 {
				assert.Contains(t, sql, "SELECT owner_id FROM analytics_queries")
				return []map[string]interface{}{{"owner_id": "user1"}}, nil
			}
			assert.Contains(t, sql, "DELETE FROM analytics_queries")
			return []map[string]interface{}{{"id": "query-1"}}, nil
		},
	}
	h := newTestHandler(db)

	req := httptest.NewRequest(http.MethodDelete, "/queries/query-1", nil)
	req.SetPathValue("id", "query-1")
	req = req.WithContext(withUserID(req.Context(), "user1"))
	w := httptest.NewRecorder()

	h.DeleteQuery(w, req)

	assert.Equal(t, http.StatusNoContent, w.Code)
	assert.Equal(t, 2, call)
}

func TestRegisterWebhook_Success(t *testing.T) {
	manager := &mockWebhookManager{
		registerFn: func(ctx context.Context, userID string, req *webhooks.WebhookRequest) (*analytics.Webhook, error) {
			require.Equal(t, "user_1", userID)
			require.Equal(t, "https://example.test/hook", req.URL)
			return &analytics.Webhook{
				ID:         "wh_1",
				UserID:     userID,
				URL:        req.URL,
				EventTypes: req.EventFilter.EventTypes,
				Active:     true,
				CreatedAt:  time.Now(),
				UpdatedAt:  time.Now(),
			}, nil
		},
	}
	handler := newWebhookTestHandler(manager)

	req := httptest.NewRequest(http.MethodPost, "/webhooks", strings.NewReader(`{"url":"https://example.test/hook","event_filter":{"event_types":["auth.login"]}}`))
	req = req.WithContext(withUserID(req.Context(), "user_1"))
	w := httptest.NewRecorder()

	handler.RegisterWebhook(w, req)

	require.Equal(t, http.StatusCreated, w.Code)
	assert.Contains(t, w.Body.String(), `"id":"wh_1"`)
}

func TestListWebhooks_Success(t *testing.T) {
	manager := &mockWebhookManager{
		listFn: func(ctx context.Context, userID string) ([]*analytics.Webhook, error) {
			require.Equal(t, "user_1", userID)
			return []*analytics.Webhook{
				{ID: "wh_1", UserID: userID, URL: "https://example.test/1", EventTypes: []string{"auth.login"}, Active: true, CreatedAt: time.Now(), UpdatedAt: time.Now()},
			}, nil
		},
	}
	handler := newWebhookTestHandler(manager)

	req := httptest.NewRequest(http.MethodGet, "/webhooks", nil)
	req = req.WithContext(withUserID(req.Context(), "user_1"))
	w := httptest.NewRecorder()

	handler.ListWebhooks(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), `"total":1`)
}

func TestGetWebhook_ForbiddenForOtherOwner(t *testing.T) {
	manager := &mockWebhookManager{
		getFn: func(ctx context.Context, webhookID string) (*analytics.Webhook, error) {
			return &analytics.Webhook{ID: webhookID, UserID: "other_user", URL: "https://example.test", Active: true, CreatedAt: time.Now(), UpdatedAt: time.Now()}, nil
		},
	}
	handler := newWebhookTestHandler(manager)

	req := httptest.NewRequest(http.MethodGet, "/webhooks/wh_1", nil)
	req.SetPathValue("id", "wh_1")
	req = req.WithContext(withUserID(req.Context(), "user_1"))
	w := httptest.NewRecorder()

	handler.GetWebhook(w, req)

	require.Equal(t, http.StatusForbidden, w.Code)
}

func TestTestWebhook_Success(t *testing.T) {
	manager := &mockWebhookManager{
		getFn: func(ctx context.Context, webhookID string) (*analytics.Webhook, error) {
			return &analytics.Webhook{ID: webhookID, UserID: "user_1", URL: "https://example.test", Active: true, CreatedAt: time.Now(), UpdatedAt: time.Now()}, nil
		},
		testFn: func(ctx context.Context, webhookID string) (string, error) {
			return "delivery_1", nil
		},
	}
	handler := newWebhookTestHandler(manager)

	req := httptest.NewRequest(http.MethodPost, "/webhooks/wh_1/test", nil)
	req.SetPathValue("id", "wh_1")
	req = req.WithContext(withUserID(req.Context(), "user_1"))
	w := httptest.NewRecorder()

	handler.TestWebhook(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), `"delivery_id":"delivery_1"`)
}

func TestReplayDelivery_Success(t *testing.T) {
	manager := &mockWebhookManager{
		getDeliveryFn: func(ctx context.Context, deliveryID string) (*analytics.WebhookDelivery, error) {
			return &analytics.WebhookDelivery{ID: deliveryID, WebhookID: "wh_1"}, nil
		},
		getFn: func(ctx context.Context, webhookID string) (*analytics.Webhook, error) {
			return &analytics.Webhook{ID: webhookID, UserID: "user_1", URL: "https://example.test", Active: true, CreatedAt: time.Now(), UpdatedAt: time.Now()}, nil
		},
		replayFn: func(ctx context.Context, deliveryID string) error {
			require.Equal(t, "delivery_1", deliveryID)
			return nil
		},
	}
	handler := newWebhookTestHandler(manager)

	req := httptest.NewRequest(http.MethodPost, "/webhooks/deliveries/delivery_1/replay", nil)
	req.SetPathValue("id", "delivery_1")
	req = req.WithContext(withUserID(req.Context(), "user_1"))
	w := httptest.NewRecorder()

	handler.ReplayDelivery(w, req)

	require.Equal(t, http.StatusAccepted, w.Code)
	assert.Contains(t, w.Body.String(), `"status":"replay_queued"`)
}

func TestWriteWebhookError_MapsNotFound(t *testing.T) {
	w := httptest.NewRecorder()
	handler := newWebhookTestHandler(&mockWebhookManager{})

	writeWebhookError(w, handler, webhooks.ErrWebhookNotFound)

	require.Equal(t, http.StatusNotFound, w.Code)
}

func TestRegisterWebhook_RequiresManager(t *testing.T) {
	handler := newWebhookTestHandler(nil)
	req := httptest.NewRequest(http.MethodPost, "/webhooks", strings.NewReader(`{}`))
	req = req.WithContext(withUserID(req.Context(), "user_1"))
	w := httptest.NewRecorder()

	handler.RegisterWebhook(w, req)

	require.Equal(t, http.StatusServiceUnavailable, w.Code)
}

func TestReplayDelivery_MapsMissingDelivery(t *testing.T) {
	manager := &mockWebhookManager{
		getDeliveryFn: func(ctx context.Context, deliveryID string) (*analytics.WebhookDelivery, error) {
			return nil, webhooks.ErrDeliveryNotFound
		},
	}
	handler := newWebhookTestHandler(manager)

	req := httptest.NewRequest(http.MethodPost, "/webhooks/deliveries/missing/replay", nil)
	req.SetPathValue("id", "missing")
	req = req.WithContext(withUserID(req.Context(), "user_1"))
	w := httptest.NewRecorder()

	handler.ReplayDelivery(w, req)

	require.Equal(t, http.StatusNotFound, w.Code)
}

func TestWriteWebhookError_Default(t *testing.T) {
	w := httptest.NewRecorder()
	handler := newWebhookTestHandler(&mockWebhookManager{})

	writeWebhookError(w, handler, errors.New("boom"))

	require.Equal(t, http.StatusInternalServerError, w.Code)
}

