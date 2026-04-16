package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func newOAuth2Handler() *Handler {
	return New(&fakeService{store: &fakeStore{}})
}

func withOAuth2RouteParam(req *http.Request, key, value string) *http.Request {
	routeCtx := chi.NewRouteContext()
	routeCtx.URLParams.Add(key, value)
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, routeCtx))
}

func oauth2JSONBody(t *testing.T, value any) *bytes.Buffer {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal oauth2 request body: %v", err)
	}
	return bytes.NewBuffer(raw)
}

func oauth2ClientRowValues(clientID, authMethod string) []any {
	secretHash := "$argon2id$v=19$m=65536,t=3,p=2$example"
	description := "Test Client"
	ownerID := "owner-1"
	now := time.Now().UTC()

	return []any{
		clientID,
		secretHash,
		"Example Client",
		description,
		nil,
		nil,
		nil,
		nil,
		[]string{"https://app.example.com/callback"},
		[]string{"https://app.example.com/logout"},
		[]string{"authorization_code", "refresh_token"},
		[]string{"code"},
		[]string{"openid", "email"},
		[]string{"api://example"},
		authMethod,
		nil,
		[]byte("{}"),
		nil,
		"public",
		"RS256",
		"opaque",
		900,
		2592000,
		3600,
		600,
		true,
		false,
		true,
		[]byte(`{"team":"identity"}`),
		ownerID,
		now,
		now,
	}
}

func TestOAuth2EndpointsCoverage(t *testing.T) {
	t.Run("get client and rotate secret", func(t *testing.T) {
		h := newOAuth2Handler()
		h.db = &fakeDB{
			queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
				switch {
				case strings.Contains(sql, "FROM oa2_clients WHERE id = $1"):
					return fakeRow{vals: oauth2ClientRowValues("client-1", "client_secret_basic")}
				default:
					return fakeRow{err: pgx.ErrNoRows}
				}
			},
			execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
				return pgconn.NewCommandTag("UPDATE 1"), nil
			},
		}

		rec := httptest.NewRecorder()
		req := withOperator(withOAuth2RouteParam(httptest.NewRequest(http.MethodGet, "/admin/oauth2/clients/client-1", nil), "id", "client-1"))
		h.GetOAuth2Client(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected %d, got %d body=%s", http.StatusOK, rec.Code, rec.Body.String())
		}

		rec = httptest.NewRecorder()
		req = withOperator(withOAuth2RouteParam(httptest.NewRequest(http.MethodPost, "/admin/oauth2/clients/client-1/rotate-secret", nil), "id", "client-1"))
		h.RotateOAuth2ClientSecret(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected %d, got %d body=%s", http.StatusOK, rec.Code, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), `"id":"client-1"`) {
			t.Fatalf("expected rotate response for client-1, got %s", rec.Body.String())
		}
	})

	t.Run("update and delete client", func(t *testing.T) {
		h := newOAuth2Handler()
		h.db = &fakeDB{
			queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
				switch {
				case strings.Contains(sql, "FROM oa2_clients WHERE id = $1"):
					return fakeRow{vals: oauth2ClientRowValues("client-2", "client_secret_basic")}
				default:
					return fakeRow{err: pgx.ErrNoRows}
				}
			},
			execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
				switch {
				case strings.Contains(sql, "UPDATE oa2_clients SET"):
					return pgconn.NewCommandTag("UPDATE 1"), nil
				case strings.Contains(sql, "DELETE FROM oa2_clients"):
					return pgconn.NewCommandTag("DELETE 1"), nil
				default:
					return pgconn.NewCommandTag("UPDATE 1"), nil
				}
			},
		}

		updateReq := withOperator(withOAuth2RouteParam(httptest.NewRequest(http.MethodPatch, "/admin/oauth2/clients/client-2", oauth2JSONBody(t, map[string]any{
			"name":                         "Updated Client",
			"redirect_uris":                []string{"https://app.example.com/callback"},
			"post_logout_redirect_uris":    []string{"https://app.example.com/logout"},
			"grant_types":                  []string{"authorization_code"},
			"response_types":               []string{"code"},
			"scopes":                       []string{"openid"},
			"token_endpoint_auth_method":   "none",
			"subject_type":                 "public",
			"access_token_strategy":        "opaque",
			"id_token_signed_response_alg": "RS256",
		})), "id", "client-2"))
		updateReq.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		h.UpdateOAuth2Client(rec, updateReq)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected %d, got %d body=%s", http.StatusOK, rec.Code, rec.Body.String())
		}

		rec = httptest.NewRecorder()
		deleteReq := withOperator(withOAuth2RouteParam(httptest.NewRequest(http.MethodDelete, "/admin/oauth2/clients/client-2", nil), "id", "client-2"))
		h.DeleteOAuth2Client(rec, deleteReq)
		if rec.Code != http.StatusNoContent {
			t.Fatalf("expected %d, got %d body=%s", http.StatusNoContent, rec.Code, rec.Body.String())
		}
	})

	t.Run("list tokens and reject unsupported token type", func(t *testing.T) {
		h := newOAuth2Handler()
		now := time.Now().UTC()
		h.db = &fakeDB{
			queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
				return fakeRow{vals: []any{int64(1)}}
			},
			queryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
				return &fakeRows{data: [][]any{
					{
						"access_token",
						"at-1",
						"client-1",
						"identity-1",
						"session-1",
						[]string{"openid"},
						[]string{"api://example"},
						"active",
						now.Add(time.Hour),
						now,
						[]byte(`{"source":"test"}`),
					},
				}}, nil
			},
		}

		rec := httptest.NewRecorder()
		req := withOperator(httptest.NewRequest(http.MethodGet, "/admin/oauth2/tokens", nil))
		h.ListOAuth2Tokens(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected %d, got %d body=%s", http.StatusOK, rec.Code, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), `"token_type":"access_token"`) {
			t.Fatalf("expected access token payload, got %s", rec.Body.String())
		}

		rec = httptest.NewRecorder()
		req = withOperator(httptest.NewRequest(http.MethodGet, "/admin/oauth2/tokens?token_type=unknown", nil))
		h.ListOAuth2Tokens(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected %d, got %d", http.StatusBadRequest, rec.Code)
		}
	})

	t.Run("revoke token branches", func(t *testing.T) {
		h := newOAuth2Handler()
		now := time.Now().UTC()
		h.db = &fakeDB{
			queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
				switch {
				case strings.Contains(sql, "FROM oa2_access_tokens WHERE jti = $1"):
					return fakeRow{err: pgx.ErrNoRows}
				case strings.Contains(sql, "FROM oa2_refresh_tokens WHERE id = $1"):
					return fakeRow{err: pgx.ErrNoRows}
				case strings.Contains(sql, "FROM oa2_id_tokens"):
					return fakeRow{vals: []any{"client-1", "identity-1", now.Add(5 * time.Minute)}}
				default:
					return fakeRow{err: pgx.ErrNoRows}
				}
			},
			execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
				return pgconn.NewCommandTag("UPDATE 1"), nil
			},
		}

		rec := httptest.NewRecorder()
		req := withOperator(httptest.NewRequest(http.MethodPost, "/admin/oauth2/tokens/revoke", oauth2JSONBody(t, map[string]any{
			"token_type": "access_token",
			"id":         "missing-access",
		})))
		req.Header.Set("Content-Type", "application/json")
		h.RevokeOAuth2Token(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected %d, got %d", http.StatusNotFound, rec.Code)
		}

		rec = httptest.NewRecorder()
		req = withOperator(httptest.NewRequest(http.MethodPost, "/admin/oauth2/tokens/revoke", oauth2JSONBody(t, map[string]any{
			"token_type": "refresh_token",
			"id":         "missing-refresh",
		})))
		req.Header.Set("Content-Type", "application/json")
		h.RevokeOAuth2Token(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected %d, got %d", http.StatusNotFound, rec.Code)
		}

		rec = httptest.NewRecorder()
		req = withOperator(httptest.NewRequest(http.MethodPost, "/admin/oauth2/tokens/revoke", oauth2JSONBody(t, map[string]any{
			"token_type": "id_token",
			"id":         "idtok-1",
			"reason":     "manual",
		})))
		req.Header.Set("Content-Type", "application/json")
		h.RevokeOAuth2Token(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected %d, got %d body=%s", http.StatusOK, rec.Code, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), `"token_type":"id_token"`) {
			t.Fatalf("expected id_token revoke response, got %s", rec.Body.String())
		}

		rec = httptest.NewRecorder()
		req = withOperator(httptest.NewRequest(http.MethodPost, "/admin/oauth2/tokens/revoke", oauth2JSONBody(t, map[string]any{
			"token_type": "unknown",
			"id":         "x",
		})))
		req.Header.Set("Content-Type", "application/json")
		h.RevokeOAuth2Token(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected %d, got %d", http.StatusBadRequest, rec.Code)
		}
	})
}
