package rest

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/rs/zerolog"
)

// MockDatabase implements Database interface for testing
type MockDatabase struct {
	queryResults map[string][]map[string]interface{}
	queryErr     error
	countResult  int
	countErr     error
}

func (m *MockDatabase) Query(ctx context.Context, sql string) ([]map[string]interface{}, error) {
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
		Logger:  zerolog.Nop(),
		Config:  Config{DefaultPageSize: 100, MaxPageSize: 10000, QueryTimeoutSeconds: 300},
		Queries: NewQueryBuilder(db),
		Exports: NewExportBuilder(db),
		Cache:   NewCache(),
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
		Logger: zerolog.Nop(),
		DB:     db,
	})

	require.NoError(t, err)
	assert.NotNil(t, handler)
}

func TestInitialize_MissingDatabase(t *testing.T) {
	handler, err := Initialize(InitParams{
		Config: Config{},
		Logger: zerolog.Nop(),
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
