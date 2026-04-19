package handler

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

type valuesErrRows struct {
	*fakeRows
}

func (r *valuesErrRows) Values() ([]any, error) {
	return nil, errors.New("values failed")
}

func TestIntegrationsAdditionalValueAndSimulationBranches(t *testing.T) {
	t.Run("listGenericRows handles rows.Values failure", func(t *testing.T) {
		h := newIntegrationsHandler()
		h.db = &fakeDB{
			queryFn: func(context.Context, string, ...any) (pgx.Rows, error) {
				return &valuesErrRows{
					fakeRows: &fakeRows{data: [][]any{{"id-1"}}},
				}, nil
			},
		}

		rec := httptest.NewRecorder()
		req := withOperator(httptest.NewRequest(http.MethodGet, "/admin/integrations/generic", nil))
		h.listGenericRows(rec, req, "SELECT 1", []string{"id"}, "items")
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("expected 500 for values error, got %d", rec.Code)
		}
	})

	t.Run("simulate proxy route defaults empty path to root", func(t *testing.T) {
		h := newIntegrationsHandler()
		now := time.Now().UTC()
		h.db = &fakeDB{
			queryFn: func(context.Context, string, ...any) (pgx.Rows, error) {
				return &fakeRows{data: [][]any{{
					"root-route", "/", []byte(`["GET"]`), false, "", []byte(`[]`), []byte(`{}`), "api", 10, []byte(`{}`), []byte(`{}`), true, "root", now, now,
				}}}, nil
			},
		}

		rec := httptest.NewRecorder()
		req := withOperator(httptest.NewRequest(http.MethodPost, "/admin/integrations/proxy/simulate", strings.NewReader(`{"path":"","method":""}`)))
		req.Header.Set("Content-Type", "application/json")
		h.SimulateProxyRoute(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), `"matched":true`) {
			t.Fatalf("expected route match response, got %s", rec.Body.String())
		}
	})
}
