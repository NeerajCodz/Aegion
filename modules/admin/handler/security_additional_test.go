package handler

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	adminstore "github.com/aegion/aegion/modules/admin/store"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestSecurityHandlerAdditionalBranches(t *testing.T) {
	operator := &adminstore.Operator{ID: uuid.New()}

	t.Run("list ip bans error branches", func(t *testing.T) {
		h := New(&fakeService{store: &fakeStore{}})

		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/admin/security/ip-bans", nil)
		h.ListIPBans(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("unauthorized expected %d got %d", http.StatusUnauthorized, rec.Code)
		}

		h.db = &fakeDB{queryFn: func(context.Context, string, ...any) (pgx.Rows, error) { return nil, errors.New("query failed") }}
		rec = httptest.NewRecorder()
		req = httptest.NewRequest(http.MethodGet, "/admin/security/ip-bans", nil)
		req = req.WithContext(context.WithValue(req.Context(), contextKeyOperator, operator))
		h.ListIPBans(rec, req)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("query error expected %d got %d", http.StatusInternalServerError, rec.Code)
		}

		h.db = &fakeDB{queryFn: func(context.Context, string, ...any) (pgx.Rows, error) {
			return &fakeRows{data: [][]any{{"bad"}}}, nil
		}}
		rec = httptest.NewRecorder()
		req = httptest.NewRequest(http.MethodGet, "/admin/security/ip-bans", nil)
		req = req.WithContext(context.WithValue(req.Context(), contextKeyOperator, operator))
		h.ListIPBans(rec, req)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("scan error expected %d got %d", http.StatusInternalServerError, rec.Code)
		}

		h.db = &fakeDB{queryFn: func(context.Context, string, ...any) (pgx.Rows, error) {
			return &fakeRows{err: errors.New("rows failed")}, nil
		}}
		rec = httptest.NewRecorder()
		req = httptest.NewRequest(http.MethodGet, "/admin/security/ip-bans", nil)
		req = req.WithContext(context.WithValue(req.Context(), contextKeyOperator, operator))
		h.ListIPBans(rec, req)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("rows err expected %d got %d", http.StatusInternalServerError, rec.Code)
		}
	})

	t.Run("upsert ip ban error branches", func(t *testing.T) {
		h := New(&fakeService{store: &fakeStore{}})

		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/admin/security/ip-bans", strings.NewReader(`{"cidr":"10.0.0.1/32","reason":"x"}`))
		req.Header.Set("Content-Type", "application/json")
		h.UpsertIPBan(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("unauthorized expected %d got %d", http.StatusUnauthorized, rec.Code)
		}

		rec = httptest.NewRecorder()
		req = httptest.NewRequest(http.MethodPost, "/admin/security/ip-bans", strings.NewReader(`{`))
		req.Header.Set("Content-Type", "application/json")
		req = req.WithContext(context.WithValue(req.Context(), contextKeyOperator, operator))
		h.UpsertIPBan(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("invalid body expected %d got %d", http.StatusBadRequest, rec.Code)
		}

		rec = httptest.NewRecorder()
		req = httptest.NewRequest(http.MethodPost, "/admin/security/ip-bans", strings.NewReader(`{"cidr":"bad","reason":"x"}`))
		req.Header.Set("Content-Type", "application/json")
		req = req.WithContext(context.WithValue(req.Context(), contextKeyOperator, operator))
		h.UpsertIPBan(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("invalid cidr expected %d got %d", http.StatusBadRequest, rec.Code)
		}

		rec = httptest.NewRecorder()
		req = httptest.NewRequest(http.MethodPost, "/admin/security/ip-bans", strings.NewReader(`{"cidr":"10.0.0.1/32","reason":" "}`))
		req.Header.Set("Content-Type", "application/json")
		req = req.WithContext(context.WithValue(req.Context(), contextKeyOperator, operator))
		h.UpsertIPBan(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("missing reason expected %d got %d", http.StatusBadRequest, rec.Code)
		}

		h.db = &fakeDB{queryRowFn: func(context.Context, string, ...any) pgx.Row {
			return fakeRow{err: errors.New("insert failed")}
		}}
		rec = httptest.NewRecorder()
		req = httptest.NewRequest(http.MethodPost, "/admin/security/ip-bans", strings.NewReader(`{"cidr":"10.0.0.1/32","reason":"abuse","expires_at":"2030-01-01T00:00:00Z"}`))
		req.Header.Set("Content-Type", "application/json")
		req = req.WithContext(context.WithValue(req.Context(), contextKeyOperator, operator))
		h.UpsertIPBan(rec, req)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("query row error expected %d got %d", http.StatusInternalServerError, rec.Code)
		}
	})

	t.Run("delete ip ban error branches", func(t *testing.T) {
		h := New(&fakeService{store: &fakeStore{}})

		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodDelete, "/admin/security/ip-bans/id", nil)
		h.DeleteIPBan(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("unauthorized expected %d got %d", http.StatusUnauthorized, rec.Code)
		}

		rec = httptest.NewRecorder()
		req = httptest.NewRequest(http.MethodDelete, "/admin/security/ip-bans/bad", nil)
		req = req.WithContext(context.WithValue(req.Context(), contextKeyOperator, operator))
		req = withRouteParam(req, "id", "bad")
		h.DeleteIPBan(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("invalid id expected %d got %d", http.StatusBadRequest, rec.Code)
		}

		h.db = &fakeDB{execFn: func(context.Context, string, ...any) (pgconn.CommandTag, error) {
			return pgconn.CommandTag{}, errors.New("delete failed")
		}}
		id := uuid.New()
		rec = httptest.NewRecorder()
		req = httptest.NewRequest(http.MethodDelete, "/admin/security/ip-bans/"+id.String(), nil)
		req = req.WithContext(context.WithValue(req.Context(), contextKeyOperator, operator))
		req = withRouteParam(req, "id", id.String())
		h.DeleteIPBan(rec, req)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("delete error expected %d got %d", http.StatusInternalServerError, rec.Code)
		}

		h.db = &fakeDB{execFn: func(context.Context, string, ...any) (pgconn.CommandTag, error) {
			return pgconn.NewCommandTag("DELETE 0"), nil
		}}
		rec = httptest.NewRecorder()
		req = httptest.NewRequest(http.MethodDelete, "/admin/security/ip-bans/"+id.String(), nil)
		req = req.WithContext(context.WithValue(req.Context(), contextKeyOperator, operator))
		req = withRouteParam(req, "id", id.String())
		h.DeleteIPBan(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("not found expected %d got %d", http.StatusNotFound, rec.Code)
		}
	})

	t.Run("normalize cidr branches", func(t *testing.T) {
		if _, err := normalizeCIDR(""); err == nil {
			t.Fatal("normalizeCIDR(empty) expected error")
		}
		if got, err := normalizeCIDR("192.0.2.1"); err != nil || got != "192.0.2.1/32" {
			t.Fatalf("normalizeCIDR(ipv4) got=%q err=%v", got, err)
		}
		if got, err := normalizeCIDR("2001:db8::1"); err != nil || got != "2001:db8::1/128" {
			t.Fatalf("normalizeCIDR(ipv6) got=%q err=%v", got, err)
		}
		if got, err := normalizeCIDR("198.51.100.0/24"); err != nil || got != "198.51.100.0/24" {
			t.Fatalf("normalizeCIDR(prefix) got=%q err=%v", got, err)
		}
		if _, err := normalizeCIDR("not-an-ip"); err == nil {
			t.Fatal("normalizeCIDR(invalid) expected error")
		}
		if _, err := normalizeCIDR(time.Now().UTC().Format(time.RFC3339)); err == nil {
			t.Fatal("normalizeCIDR(timestamp) expected error")
		}
	})
}
