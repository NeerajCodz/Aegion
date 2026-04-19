package handler

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestOAuth2HandlersUnauthorizedBranches(t *testing.T) {
	h := newOAuth2Handler()

	cases := []struct {
		name string
		run  func(http.ResponseWriter, *http.Request)
		req  *http.Request
	}{
		{
			name: "list clients",
			run:  h.ListOAuth2Clients,
			req:  httptest.NewRequest(http.MethodGet, "/admin/oauth2/clients", nil),
		},
		{
			name: "get client",
			run:  h.GetOAuth2Client,
			req:  withOAuth2RouteParam(httptest.NewRequest(http.MethodGet, "/admin/oauth2/clients/client-1", nil), "id", "client-1"),
		},
		{
			name: "create client",
			run:  h.CreateOAuth2Client,
			req:  httptest.NewRequest(http.MethodPost, "/admin/oauth2/clients", nil),
		},
		{
			name: "update client",
			run:  h.UpdateOAuth2Client,
			req:  withOAuth2RouteParam(httptest.NewRequest(http.MethodPatch, "/admin/oauth2/clients/client-1", nil), "id", "client-1"),
		},
		{
			name: "delete client",
			run:  h.DeleteOAuth2Client,
			req:  withOAuth2RouteParam(httptest.NewRequest(http.MethodDelete, "/admin/oauth2/clients/client-1", nil), "id", "client-1"),
		},
		{
			name: "rotate secret",
			run:  h.RotateOAuth2ClientSecret,
			req:  withOAuth2RouteParam(httptest.NewRequest(http.MethodPost, "/admin/oauth2/clients/client-1/rotate-secret", nil), "id", "client-1"),
		},
		{
			name: "list tokens",
			run:  h.ListOAuth2Tokens,
			req:  httptest.NewRequest(http.MethodGet, "/admin/oauth2/tokens", nil),
		},
		{
			name: "revoke token",
			run:  h.RevokeOAuth2Token,
			req:  httptest.NewRequest(http.MethodPost, "/admin/oauth2/tokens/revoke", nil),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			tc.run(rec, tc.req)
			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("expected %d, got %d body=%s", http.StatusUnauthorized, rec.Code, rec.Body.String())
			}
		})
	}
}

func TestListOAuth2ClientsAdditionalBranches(t *testing.T) {
	now := time.Now().UTC()
	row := []any{
		"client-1",
		"Example Client",
		"description",
		[]string{"https://app.example.com/callback"},
		[]string{"authorization_code"},
		[]string{"code"},
		[]string{"openid"},
		"client_secret_basic",
		true,
		false,
		true,
		now,
		now,
	}

	t.Run("query error", func(t *testing.T) {
		h := newOAuth2Handler()
		h.db = &fakeDB{
			queryFn: func(context.Context, string, ...any) (pgx.Rows, error) {
				return nil, errors.New("query failed")
			},
		}

		rec := httptest.NewRecorder()
		req := withOperator(httptest.NewRequest(http.MethodGet, "/admin/oauth2/clients", nil))
		h.ListOAuth2Clients(rec, req)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("expected %d, got %d", http.StatusInternalServerError, rec.Code)
		}
	})

	t.Run("scan error", func(t *testing.T) {
		h := newOAuth2Handler()
		h.db = &fakeDB{
			queryFn: func(context.Context, string, ...any) (pgx.Rows, error) {
				return &fakeRows{data: [][]any{{"only-id"}}}, nil
			},
		}

		rec := httptest.NewRecorder()
		req := withOperator(httptest.NewRequest(http.MethodGet, "/admin/oauth2/clients", nil))
		h.ListOAuth2Clients(rec, req)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("expected %d, got %d", http.StatusInternalServerError, rec.Code)
		}
	})

	t.Run("rows error", func(t *testing.T) {
		h := newOAuth2Handler()
		h.db = &fakeDB{
			queryFn: func(context.Context, string, ...any) (pgx.Rows, error) {
				return &fakeRows{err: errors.New("rows failed")}, nil
			},
		}

		rec := httptest.NewRecorder()
		req := withOperator(httptest.NewRequest(http.MethodGet, "/admin/oauth2/clients", nil))
		h.ListOAuth2Clients(rec, req)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("expected %d, got %d", http.StatusInternalServerError, rec.Code)
		}
	})

	t.Run("count error", func(t *testing.T) {
		h := newOAuth2Handler()
		h.db = &fakeDB{
			queryFn: func(context.Context, string, ...any) (pgx.Rows, error) {
				return &fakeRows{data: [][]any{row}}, nil
			},
			queryRowFn: func(context.Context, string, ...any) pgx.Row {
				return fakeRow{err: errors.New("count failed")}
			},
		}

		rec := httptest.NewRecorder()
		req := withOperator(httptest.NewRequest(http.MethodGet, "/admin/oauth2/clients", nil))
		h.ListOAuth2Clients(rec, req)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("expected %d, got %d", http.StatusInternalServerError, rec.Code)
		}
	})

	t.Run("success with owner filter", func(t *testing.T) {
		h := newOAuth2Handler()
		h.db = &fakeDB{
			queryFn: func(_ context.Context, sql string, args ...any) (pgx.Rows, error) {
				if !strings.Contains(sql, "WHERE owner_id = $1") {
					t.Fatalf("expected owner filter query, got %q", sql)
				}
				if len(args) != 3 || args[0] != "owner-1" {
					t.Fatalf("unexpected owner query args: %#v", args)
				}
				return &fakeRows{data: [][]any{row}}, nil
			},
			queryRowFn: func(context.Context, string, ...any) pgx.Row {
				return fakeRow{vals: []any{int64(1)}}
			},
		}

		rec := httptest.NewRecorder()
		req := withOperator(httptest.NewRequest(http.MethodGet, "/admin/oauth2/clients?owner_id=owner-1&page=1&per_page=10", nil))
		h.ListOAuth2Clients(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected %d, got %d body=%s", http.StatusOK, rec.Code, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), `"id":"client-1"`) {
			t.Fatalf("expected client in response, got %s", rec.Body.String())
		}
	})
}

func TestCreateOAuth2ClientAdditionalBranches(t *testing.T) {
	t.Run("invalid body and invalid request", func(t *testing.T) {
		h := newOAuth2Handler()

		rec := httptest.NewRecorder()
		req := withOperator(httptest.NewRequest(http.MethodPost, "/admin/oauth2/clients", bytes.NewBufferString("{")))
		req.Header.Set("Content-Type", "application/json")
		h.CreateOAuth2Client(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected %d, got %d", http.StatusBadRequest, rec.Code)
		}

		rec = httptest.NewRecorder()
		req = withOperator(httptest.NewRequest(http.MethodPost, "/admin/oauth2/clients", oauth2JSONBody(t, map[string]any{})))
		req.Header.Set("Content-Type", "application/json")
		h.CreateOAuth2Client(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected %d, got %d", http.StatusBadRequest, rec.Code)
		}
	})

	t.Run("duplicate client returns conflict", func(t *testing.T) {
		h := newOAuth2Handler()
		h.db = &fakeDB{
			execFn: func(context.Context, string, ...any) (pgconn.CommandTag, error) {
				return pgconn.CommandTag{}, &pgconn.PgError{Code: "23505"}
			},
		}

		rec := httptest.NewRecorder()
		req := withOperator(httptest.NewRequest(http.MethodPost, "/admin/oauth2/clients", oauth2JSONBody(t, map[string]any{
			"name":          "Example App",
			"redirect_uris": []string{"https://app.example.com/callback"},
		})))
		req.Header.Set("Content-Type", "application/json")
		h.CreateOAuth2Client(rec, req)
		if rec.Code != http.StatusConflict {
			t.Fatalf("expected %d, got %d body=%s", http.StatusConflict, rec.Code, rec.Body.String())
		}
	})

	t.Run("success returns generated client secret", func(t *testing.T) {
		h := newOAuth2Handler()
		h.db = &fakeDB{
			execFn: func(context.Context, string, ...any) (pgconn.CommandTag, error) {
				return pgconn.NewCommandTag("INSERT 1"), nil
			},
		}

		rec := httptest.NewRecorder()
		req := withOperator(httptest.NewRequest(http.MethodPost, "/admin/oauth2/clients", oauth2JSONBody(t, map[string]any{
			"name":          "Example App",
			"redirect_uris": []string{"https://app.example.com/callback"},
		})))
		req.Header.Set("Content-Type", "application/json")
		h.CreateOAuth2Client(rec, req)
		if rec.Code != http.StatusCreated {
			t.Fatalf("expected %d, got %d body=%s", http.StatusCreated, rec.Code, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), `"client_secret":"`) {
			t.Fatalf("expected generated client_secret in response, got %s", rec.Body.String())
		}
	})
}

func TestRotateOAuth2ClientSecretPublicClientBranch(t *testing.T) {
	h := newOAuth2Handler()
	h.db = &fakeDB{
		queryRowFn: func(context.Context, string, ...any) pgx.Row {
			return fakeRow{vals: oauth2ClientRowValues("public-client", "none")}
		},
	}

	rec := httptest.NewRecorder()
	req := withOperator(withOAuth2RouteParam(httptest.NewRequest(http.MethodPost, "/admin/oauth2/clients/public-client/rotate-secret", nil), "id", "public-client"))
	h.RotateOAuth2ClientSecret(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected %d, got %d body=%s", http.StatusBadRequest, rec.Code, rec.Body.String())
	}
}

func TestOAuth2HandlersAdditionalEndpointBranches(t *testing.T) {
	t.Run("get client bad request and not found/internal", func(t *testing.T) {
		h := newOAuth2Handler()
		h.db = &fakeDB{}

		rec := httptest.NewRecorder()
		req := withOperator(httptest.NewRequest(http.MethodGet, "/admin/oauth2/clients", nil))
		h.GetOAuth2Client(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected %d, got %d", http.StatusBadRequest, rec.Code)
		}

		rec = httptest.NewRecorder()
		req = withOperator(withOAuth2RouteParam(httptest.NewRequest(http.MethodGet, "/admin/oauth2/clients/missing", nil), "id", "missing"))
		h.GetOAuth2Client(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected %d, got %d", http.StatusNotFound, rec.Code)
		}

		h.db = &fakeDB{
			queryRowFn: func(context.Context, string, ...any) pgx.Row {
				return fakeRow{err: errors.New("boom")}
			},
		}
		rec = httptest.NewRecorder()
		h.GetOAuth2Client(rec, req)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("expected %d, got %d", http.StatusInternalServerError, rec.Code)
		}
	})

	t.Run("update client failure branches", func(t *testing.T) {
		h := newOAuth2Handler()
		h.db = &fakeDB{}

		rec := httptest.NewRecorder()
		req := withOperator(httptest.NewRequest(http.MethodPatch, "/admin/oauth2/clients", nil))
		h.UpdateOAuth2Client(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected %d, got %d", http.StatusBadRequest, rec.Code)
		}

		req = withOperator(withOAuth2RouteParam(httptest.NewRequest(http.MethodPatch, "/admin/oauth2/clients/missing", nil), "id", "missing"))
		rec = httptest.NewRecorder()
		h.UpdateOAuth2Client(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected %d, got %d", http.StatusNotFound, rec.Code)
		}

		h.db = &fakeDB{
			queryRowFn: func(context.Context, string, ...any) pgx.Row {
				return fakeRow{err: errors.New("load failed")}
			},
		}
		rec = httptest.NewRecorder()
		h.UpdateOAuth2Client(rec, req)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("expected %d, got %d", http.StatusInternalServerError, rec.Code)
		}

		h.db = &fakeDB{
			queryRowFn: func(context.Context, string, ...any) pgx.Row {
				return fakeRow{vals: oauth2ClientRowValues("client-u1", "client_secret_basic")}
			},
		}
		rec = httptest.NewRecorder()
		req = withOperator(withOAuth2RouteParam(httptest.NewRequest(http.MethodPatch, "/admin/oauth2/clients/client-u1", bytes.NewBufferString("{")), "id", "client-u1"))
		req.Header.Set("Content-Type", "application/json")
		h.UpdateOAuth2Client(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected %d, got %d", http.StatusBadRequest, rec.Code)
		}

		rec = httptest.NewRecorder()
		req = withOperator(withOAuth2RouteParam(httptest.NewRequest(http.MethodPatch, "/admin/oauth2/clients/client-u1", oauth2JSONBody(t, map[string]any{
			"redirect_uris": []string{"relative/path"},
		})), "id", "client-u1"))
		req.Header.Set("Content-Type", "application/json")
		h.UpdateOAuth2Client(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected %d, got %d", http.StatusBadRequest, rec.Code)
		}

		h.db = &fakeDB{
			queryRowFn: func(context.Context, string, ...any) pgx.Row {
				return fakeRow{vals: oauth2ClientRowValues("client-u2", "client_secret_basic")}
			},
			execFn: func(context.Context, string, ...any) (pgconn.CommandTag, error) {
				return pgconn.CommandTag{}, errors.New("update failed")
			},
		}
		rec = httptest.NewRecorder()
		req = withOperator(withOAuth2RouteParam(httptest.NewRequest(http.MethodPatch, "/admin/oauth2/clients/client-u2", oauth2JSONBody(t, map[string]any{
			"name":          "Updated",
			"redirect_uris": []string{"https://app.example.com/callback"},
		})), "id", "client-u2"))
		req.Header.Set("Content-Type", "application/json")
		h.UpdateOAuth2Client(rec, req)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("expected %d, got %d", http.StatusInternalServerError, rec.Code)
		}

		execCalls := 0
		h.db = &fakeDB{
			queryRowFn: func(context.Context, string, ...any) pgx.Row {
				return fakeRow{vals: oauth2ClientRowValues("client-u3", "client_secret_basic")}
			},
			execFn: func(context.Context, string, ...any) (pgconn.CommandTag, error) {
				execCalls++
				if execCalls == 1 {
					return pgconn.NewCommandTag("UPDATE 1"), nil
				}
				return pgconn.CommandTag{}, errors.New("secret rotate failed")
			},
		}
		rec = httptest.NewRecorder()
		req = withOperator(withOAuth2RouteParam(httptest.NewRequest(http.MethodPatch, "/admin/oauth2/clients/client-u3", oauth2JSONBody(t, map[string]any{
			"name":          "Updated",
			"redirect_uris": []string{"https://app.example.com/callback"},
			"client_secret": "new-secret",
		})), "id", "client-u3"))
		req.Header.Set("Content-Type", "application/json")
		h.UpdateOAuth2Client(rec, req)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("expected %d, got %d", http.StatusInternalServerError, rec.Code)
		}
	})

	t.Run("delete and rotate secret error branches", func(t *testing.T) {
		h := newOAuth2Handler()
		h.db = &fakeDB{}

		rec := httptest.NewRecorder()
		req := withOperator(httptest.NewRequest(http.MethodDelete, "/admin/oauth2/clients", nil))
		h.DeleteOAuth2Client(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected %d, got %d", http.StatusBadRequest, rec.Code)
		}

		req = withOperator(withOAuth2RouteParam(httptest.NewRequest(http.MethodDelete, "/admin/oauth2/clients/missing", nil), "id", "missing"))
		rec = httptest.NewRecorder()
		h.DeleteOAuth2Client(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected %d, got %d", http.StatusNotFound, rec.Code)
		}

		h.db = &fakeDB{
			queryRowFn: func(context.Context, string, ...any) pgx.Row {
				return fakeRow{vals: oauth2ClientRowValues("client-d1", "client_secret_basic")}
			},
			execFn: func(context.Context, string, ...any) (pgconn.CommandTag, error) {
				return pgconn.CommandTag{}, errors.New("delete failed")
			},
		}
		rec = httptest.NewRecorder()
		req = withOperator(withOAuth2RouteParam(httptest.NewRequest(http.MethodDelete, "/admin/oauth2/clients/client-d1", nil), "id", "client-d1"))
		h.DeleteOAuth2Client(rec, req)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("expected %d, got %d", http.StatusInternalServerError, rec.Code)
		}

		rec = httptest.NewRecorder()
		req = withOperator(httptest.NewRequest(http.MethodPost, "/admin/oauth2/clients/rotate-secret", nil))
		h.RotateOAuth2ClientSecret(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected %d, got %d", http.StatusBadRequest, rec.Code)
		}

		h.db = &fakeDB{
			queryRowFn: func(context.Context, string, ...any) pgx.Row {
				return fakeRow{vals: oauth2ClientRowValues("client-r1", "client_secret_basic")}
			},
			execFn: func(context.Context, string, ...any) (pgconn.CommandTag, error) {
				return pgconn.CommandTag{}, errors.New("rotate failed")
			},
		}
		rec = httptest.NewRecorder()
		req = withOperator(withOAuth2RouteParam(httptest.NewRequest(http.MethodPost, "/admin/oauth2/clients/client-r1/rotate-secret", nil), "id", "client-r1"))
		h.RotateOAuth2ClientSecret(rec, req)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("expected %d, got %d", http.StatusInternalServerError, rec.Code)
		}
	})
}

func TestOAuth2TokenHandlersAdditionalBranches(t *testing.T) {
	now := time.Now().UTC()
	accessTokenRow := []any{
		"at-1",
		"sig-1",
		"client-1",
		"identity-1",
		"session-1",
		[]string{"openid"},
		[]string{"api://example"},
		"https://issuer.example.com",
		"identity-1",
		[]byte(`{}`),
		false,
		(*time.Time)(nil),
		now.Add(time.Hour),
		now,
	}

	t.Run("list tokens count/query branches", func(t *testing.T) {
		h := newOAuth2Handler()
		h.db = &fakeDB{
			queryRowFn: func(context.Context, string, ...any) pgx.Row {
				return fakeRow{err: errors.New("count failed")}
			},
		}

		rec := httptest.NewRecorder()
		req := withOperator(httptest.NewRequest(http.MethodGet, "/admin/oauth2/tokens", nil))
		h.ListOAuth2Tokens(rec, req)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("expected %d, got %d", http.StatusInternalServerError, rec.Code)
		}

		h.db = &fakeDB{
			queryRowFn: func(context.Context, string, ...any) pgx.Row {
				return fakeRow{vals: []any{int64(1)}}
			},
			queryFn: func(context.Context, string, ...any) (pgx.Rows, error) {
				return nil, errors.New("query failed")
			},
		}
		rec = httptest.NewRecorder()
		h.ListOAuth2Tokens(rec, req)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("expected %d, got %d", http.StatusInternalServerError, rec.Code)
		}
	})

	t.Run("revoke token invalid payload and internal branches", func(t *testing.T) {
		h := newOAuth2Handler()

		rec := httptest.NewRecorder()
		req := withOperator(httptest.NewRequest(http.MethodPost, "/admin/oauth2/tokens/revoke", bytes.NewBufferString("{")))
		req.Header.Set("Content-Type", "application/json")
		h.RevokeOAuth2Token(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected %d, got %d", http.StatusBadRequest, rec.Code)
		}

		rec = httptest.NewRecorder()
		req = withOperator(httptest.NewRequest(http.MethodPost, "/admin/oauth2/tokens/revoke", oauth2JSONBody(t, map[string]any{
			"token_type": "access_token",
		})))
		req.Header.Set("Content-Type", "application/json")
		h.RevokeOAuth2Token(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected %d, got %d", http.StatusBadRequest, rec.Code)
		}

		h.db = &fakeDB{
			queryRowFn: func(context.Context, string, ...any) pgx.Row {
				return fakeRow{err: errors.New("load failed")}
			},
		}
		rec = httptest.NewRecorder()
		req = withOperator(httptest.NewRequest(http.MethodPost, "/admin/oauth2/tokens/revoke", oauth2JSONBody(t, map[string]any{
			"token_type": "access_token",
			"id":         "at-1",
		})))
		req.Header.Set("Content-Type", "application/json")
		h.RevokeOAuth2Token(rec, req)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("expected %d, got %d", http.StatusInternalServerError, rec.Code)
		}

		h.db = &fakeDB{
			queryRowFn: func(context.Context, string, ...any) pgx.Row {
				return fakeRow{vals: accessTokenRow}
			},
			execFn: func(context.Context, string, ...any) (pgconn.CommandTag, error) {
				return pgconn.CommandTag{}, errors.New("revoke failed")
			},
		}
		rec = httptest.NewRecorder()
		req = withOperator(httptest.NewRequest(http.MethodPost, "/admin/oauth2/tokens/revoke", oauth2JSONBody(t, map[string]any{
			"token_type": "access_token",
			"id":         "at-1",
		})))
		req.Header.Set("Content-Type", "application/json")
		h.RevokeOAuth2Token(rec, req)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("expected %d, got %d", http.StatusInternalServerError, rec.Code)
		}
	})
}
