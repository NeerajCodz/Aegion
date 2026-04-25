package rest

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAdditionalQueryBuilderBuildsEventQuery(t *testing.T) {
	db := &MockDatabase{queryResults: map[string][]map[string]interface{}{}}
	qb := NewQueryBuilder(db)

	sql, err := qb.BuildQuery(context.Background(), QueryRequest{
		Page:     2,
		PageSize: 25,
		Filters: map[string]interface{}{
			"category": "auth",
		},
	}, "events")
	require.NoError(t, err)
	assert.Contains(t, sql, "FROM events")
	assert.Contains(t, sql, "category")
	assert.Contains(t, sql, "LIMIT 25")
	assert.Contains(t, sql, "OFFSET 25")
}

func TestAdditionalHandlerSearchEventsRequiresQuery(t *testing.T) {
	handler := newTestHandler(&MockDatabase{queryResults: map[string][]map[string]interface{}{}})
	req := httptest.NewRequest(http.MethodPost, "/events/search", strings.NewReader(`{"page":1,"pageSize":10}`))
	w := httptest.NewRecorder()

	handler.SearchEvents(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestAdditionalHandlerExportEventsRejectsUnsupportedFormat(t *testing.T) {
	handler := newTestHandler(&MockDatabase{queryResults: map[string][]map[string]interface{}{}})
	req := httptest.NewRequest(http.MethodPost, "/events/export/xml", http.NoBody)
	req.SetPathValue("format", "xml")
	w := httptest.NewRecorder()

	handler.ExportEvents(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestAdditionalHandlerListEventsReturnsPagination(t *testing.T) {
	sql := "SELECT * FROM events ORDER BY created_at DESC LIMIT 100 OFFSET 0"
	db := &MockDatabase{
		queryResults: map[string][]map[string]interface{}{
			sql: {
				{"id": "1", "category": "auth"},
			},
		},
		countResult: 1,
	}

	handler := newTestHandler(db)
	req := httptest.NewRequest(http.MethodGet, "/events", nil)
	w := httptest.NewRecorder()

	handler.ListEvents(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	var resp Response
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	require.NotNil(t, resp.Pagination)
	assert.Equal(t, 1, resp.Pagination.Total)
}
