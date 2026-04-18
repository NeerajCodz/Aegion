package handler

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	oauth2store "github.com/aegion/aegion/modules/oauth2/store"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func oauth2AccessTokenStoreRowValues(id string) []any {
	now := time.Now().UTC()
	return []any{
		id,
		nil,
		"client-1",
		"identity-1",
		"session-1",
		[]string{"openid"},
		[]string{"api://example"},
		"https://issuer.example.com",
		"subject-1",
		[]byte(`{}`),
		false,
		nil,
		now.Add(time.Hour),
		now,
	}
}

func oauth2RefreshTokenStoreRowValues(id string) []any {
	now := time.Now().UTC()
	return []any{
		id,
		"family-1",
		"client-1",
		"identity-1",
		"session-1",
		[]string{"openid"},
		[]string{"api://example"},
		true,
		false,
		nil,
		nil,
		nil,
		nil,
		nil,
		[]byte(`{}`),
		now.Add(2 * time.Hour),
		now,
	}
}

func TestOAuth2HandlersUncoveredBranches(t *testing.T) {
	t.Run("create client internal create error", func(t *testing.T) {
		h := newOAuth2Handler()
		h.db = &fakeDB{
			execFn: func(context.Context, string, ...any) (pgconn.CommandTag, error) {
				return pgconn.CommandTag{}, errors.New("create failed")
			},
		}

		rec := httptest.NewRecorder()
		req := withOperator(httptest.NewRequest(http.MethodPost, "/admin/oauth2/clients", oauth2JSONBody(t, map[string]any{
			"name":          "Example App",
			"redirect_uris": []string{"https://app.example.com/callback"},
		})))
		req.Header.Set("Content-Type", "application/json")
		h.CreateOAuth2Client(rec, req)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("expected %d, got %d body=%s", http.StatusInternalServerError, rec.Code, rec.Body.String())
		}
	})

	t.Run("update client not found and success with secret response", func(t *testing.T) {
		h := newOAuth2Handler()
		h.db = &fakeDB{
			queryRowFn: func(context.Context, string, ...any) pgx.Row {
				return fakeRow{vals: oauth2ClientRowValues("client-u-nf", "client_secret_basic")}
			},
			execFn: func(context.Context, string, ...any) (pgconn.CommandTag, error) {
				return pgconn.NewCommandTag("UPDATE 0"), nil
			},
		}

		rec := httptest.NewRecorder()
		req := withOperator(withOAuth2RouteParam(httptest.NewRequest(http.MethodPatch, "/admin/oauth2/clients/client-u-nf", oauth2JSONBody(t, map[string]any{
			"name":          "Updated",
			"redirect_uris": []string{"https://app.example.com/callback"},
		})), "id", "client-u-nf"))
		req.Header.Set("Content-Type", "application/json")
		h.UpdateOAuth2Client(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected %d, got %d body=%s", http.StatusNotFound, rec.Code, rec.Body.String())
		}

		execCalls := 0
		h = newOAuth2Handler()
		h.db = &fakeDB{
			queryRowFn: func(context.Context, string, ...any) pgx.Row {
				return fakeRow{vals: oauth2ClientRowValues("client-u-secret", "client_secret_basic")}
			},
			execFn: func(_ context.Context, sql string, _ ...any) (pgconn.CommandTag, error) {
				execCalls++
				if strings.Contains(sql, "UPDATE oa2_clients SET secret_hash") {
					return pgconn.NewCommandTag("UPDATE 1"), nil
				}
				return pgconn.NewCommandTag("UPDATE 1"), nil
			},
		}
		rec = httptest.NewRecorder()
		req = withOperator(withOAuth2RouteParam(httptest.NewRequest(http.MethodPatch, "/admin/oauth2/clients/client-u-secret", oauth2JSONBody(t, map[string]any{
			"name":          "Updated Secret",
			"redirect_uris": []string{"https://app.example.com/callback"},
			"client_secret": "new-secret",
		})), "id", "client-u-secret"))
		req.Header.Set("Content-Type", "application/json")
		h.UpdateOAuth2Client(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected %d, got %d body=%s", http.StatusOK, rec.Code, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), `"client_secret":"new-secret"`) {
			t.Fatalf("expected updated client_secret in response, got %s", rec.Body.String())
		}
		if execCalls < 2 {
			t.Fatalf("expected update + secret-update calls, got %d", execCalls)
		}
	})

	t.Run("delete and rotate get-client error branches", func(t *testing.T) {
		h := newOAuth2Handler()
		h.db = &fakeDB{
			queryRowFn: func(context.Context, string, ...any) pgx.Row {
				return fakeRow{err: errors.New("load failed")}
			},
		}
		rec := httptest.NewRecorder()
		req := withOperator(withOAuth2RouteParam(httptest.NewRequest(http.MethodDelete, "/admin/oauth2/clients/client-d-int", nil), "id", "client-d-int"))
		h.DeleteOAuth2Client(rec, req)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("expected %d, got %d", http.StatusInternalServerError, rec.Code)
		}

		h = newOAuth2Handler()
		h.db = &fakeDB{
			queryRowFn: func(context.Context, string, ...any) pgx.Row {
				return fakeRow{vals: oauth2ClientRowValues("client-d-nf", "client_secret_basic")}
			},
			execFn: func(context.Context, string, ...any) (pgconn.CommandTag, error) {
				return pgconn.NewCommandTag("DELETE 0"), nil
			},
		}
		rec = httptest.NewRecorder()
		req = withOperator(withOAuth2RouteParam(httptest.NewRequest(http.MethodDelete, "/admin/oauth2/clients/client-d-nf", nil), "id", "client-d-nf"))
		h.DeleteOAuth2Client(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected %d, got %d", http.StatusNotFound, rec.Code)
		}

		h = newOAuth2Handler()
		h.db = &fakeDB{
			queryRowFn: func(context.Context, string, ...any) pgx.Row {
				return fakeRow{err: pgx.ErrNoRows}
			},
		}
		rec = httptest.NewRecorder()
		req = withOperator(withOAuth2RouteParam(httptest.NewRequest(http.MethodPost, "/admin/oauth2/clients/client-r-nf/rotate-secret", nil), "id", "client-r-nf"))
		h.RotateOAuth2ClientSecret(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected %d, got %d", http.StatusNotFound, rec.Code)
		}

		h = newOAuth2Handler()
		h.db = &fakeDB{
			queryRowFn: func(context.Context, string, ...any) pgx.Row {
				return fakeRow{err: errors.New("load failed")}
			},
		}
		rec = httptest.NewRecorder()
		req = withOperator(withOAuth2RouteParam(httptest.NewRequest(http.MethodPost, "/admin/oauth2/clients/client-r-int/rotate-secret", nil), "id", "client-r-int"))
		h.RotateOAuth2ClientSecret(rec, req)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("expected %d, got %d", http.StatusInternalServerError, rec.Code)
		}
	})

	t.Run("list tokens filters and scan/rows errors", func(t *testing.T) {
		h := newOAuth2Handler()
		now := time.Now().UTC()
		h.db = &fakeDB{
			queryRowFn: func(context.Context, string, ...any) pgx.Row {
				return fakeRow{vals: []any{int64(1)}}
			},
			queryFn: func(_ context.Context, sql string, args ...any) (pgx.Rows, error) {
				if !strings.Contains(sql, "client_id = $1") || !strings.Contains(sql, "identity_id = $2") {
					t.Fatalf("expected filtered token query, got %q", sql)
				}
				if len(args) != 4 || args[0] != "client-1" || args[1] != "identity-1" {
					t.Fatalf("unexpected query args: %#v", args)
				}
				return &fakeRows{data: [][]any{
					{"access_token", "tok-1", "client-1", "identity-1", "session-1", []string{"openid"}, []string{"api://example"}, "active", now.Add(time.Hour), now, []byte(`{"source":"test"}`)},
				}}, nil
			},
		}
		rec := httptest.NewRecorder()
		req := withOperator(httptest.NewRequest(http.MethodGet, "/admin/oauth2/tokens?token_type=access_token&client_id=client-1&identity_id=identity-1", nil))
		h.ListOAuth2Tokens(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected %d, got %d body=%s", http.StatusOK, rec.Code, rec.Body.String())
		}

		h = newOAuth2Handler()
		h.db = &fakeDB{
			queryRowFn: func(context.Context, string, ...any) pgx.Row {
				return fakeRow{vals: []any{int64(1)}}
			},
			queryFn: func(context.Context, string, ...any) (pgx.Rows, error) {
				return &fakeRows{data: [][]any{{"access_token"}}}, nil
			},
		}
		rec = httptest.NewRecorder()
		req = withOperator(httptest.NewRequest(http.MethodGet, "/admin/oauth2/tokens?token_type=access_token", nil))
		h.ListOAuth2Tokens(rec, req)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("expected %d, got %d", http.StatusInternalServerError, rec.Code)
		}

		h = newOAuth2Handler()
		h.db = &fakeDB{
			queryRowFn: func(context.Context, string, ...any) pgx.Row {
				return fakeRow{vals: []any{int64(1)}}
			},
			queryFn: func(context.Context, string, ...any) (pgx.Rows, error) {
				return &fakeRows{err: errors.New("rows err")}, nil
			},
		}
		rec = httptest.NewRecorder()
		req = withOperator(httptest.NewRequest(http.MethodGet, "/admin/oauth2/tokens?token_type=access_token", nil))
		h.ListOAuth2Tokens(rec, req)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("expected %d, got %d", http.StatusInternalServerError, rec.Code)
		}
	})

	t.Run("revoke token additional branch coverage", func(t *testing.T) {
		h := newOAuth2Handler()
		h.db = &fakeDB{
			queryRowFn: func(_ context.Context, sql string, _ ...any) pgx.Row {
				switch {
				case strings.Contains(sql, "FROM oa2_access_tokens WHERE jti = $1"):
					return fakeRow{vals: oauth2AccessTokenStoreRowValues("at-1")}
				default:
					return fakeRow{err: pgx.ErrNoRows}
				}
			},
			execFn: func(_ context.Context, sql string, _ ...any) (pgconn.CommandTag, error) {
				if strings.Contains(sql, "UPDATE oa2_access_tokens SET revoked = true") {
					return pgconn.CommandTag{}, errors.New("revoke access failed")
				}
				return pgconn.NewCommandTag("UPDATE 1"), nil
			},
		}
		rec := httptest.NewRecorder()
		req := withOperator(httptest.NewRequest(http.MethodPost, "/admin/oauth2/tokens/revoke", oauth2JSONBody(t, map[string]any{
			"token_type": "access_token",
			"id":         "at-1",
		})))
		req.Header.Set("Content-Type", "application/json")
		h.RevokeOAuth2Token(rec, req)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("expected %d, got %d", http.StatusInternalServerError, rec.Code)
		}

		h = newOAuth2Handler()
		h.db = &fakeDB{
			queryRowFn: func(_ context.Context, sql string, _ ...any) pgx.Row {
				switch {
				case strings.Contains(sql, "FROM oa2_refresh_tokens WHERE id = $1"):
					return fakeRow{err: errors.New("refresh load failed")}
				default:
					return fakeRow{err: pgx.ErrNoRows}
				}
			},
		}
		rec = httptest.NewRecorder()
		req = withOperator(httptest.NewRequest(http.MethodPost, "/admin/oauth2/tokens/revoke", oauth2JSONBody(t, map[string]any{
			"token_type": "refresh_token",
			"id":         "rt-int",
		})))
		req.Header.Set("Content-Type", "application/json")
		h.RevokeOAuth2Token(rec, req)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("expected %d, got %d", http.StatusInternalServerError, rec.Code)
		}

		h = newOAuth2Handler()
		h.db = &fakeDB{
			queryRowFn: func(_ context.Context, sql string, _ ...any) pgx.Row {
				switch {
				case strings.Contains(sql, "FROM oa2_refresh_tokens WHERE id = $1"):
					return fakeRow{vals: oauth2RefreshTokenStoreRowValues("rt-err")}
				default:
					return fakeRow{err: pgx.ErrNoRows}
				}
			},
			execFn: func(_ context.Context, sql string, _ ...any) (pgconn.CommandTag, error) {
				if strings.Contains(sql, "UPDATE oa2_refresh_tokens SET active = false") {
					return pgconn.CommandTag{}, errors.New("refresh revoke failed")
				}
				return pgconn.NewCommandTag("UPDATE 1"), nil
			},
		}
		rec = httptest.NewRecorder()
		req = withOperator(httptest.NewRequest(http.MethodPost, "/admin/oauth2/tokens/revoke", oauth2JSONBody(t, map[string]any{
			"token_type": "refresh_token",
			"id":         "rt-err",
		})))
		req.Header.Set("Content-Type", "application/json")
		h.RevokeOAuth2Token(rec, req)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("expected %d, got %d", http.StatusInternalServerError, rec.Code)
		}

		h = newOAuth2Handler()
		h.db = &fakeDB{
			queryRowFn: func(_ context.Context, sql string, _ ...any) pgx.Row {
				switch {
				case strings.Contains(sql, "FROM oa2_refresh_tokens WHERE id = $1"):
					return fakeRow{vals: oauth2RefreshTokenStoreRowValues("rt-ok")}
				default:
					return fakeRow{err: pgx.ErrNoRows}
				}
			},
			execFn: func(_ context.Context, sql string, _ ...any) (pgconn.CommandTag, error) {
				switch {
				case strings.Contains(sql, "UPDATE oa2_refresh_tokens SET active = false"):
					return pgconn.NewCommandTag("UPDATE 1"), nil
				case strings.Contains(sql, "INSERT INTO oa2_token_revocations"):
					return pgconn.NewCommandTag("INSERT 1"), nil
				default:
					return pgconn.NewCommandTag("UPDATE 1"), nil
				}
			},
		}
		rec = httptest.NewRecorder()
		req = withOperator(httptest.NewRequest(http.MethodPost, "/admin/oauth2/tokens/revoke", oauth2JSONBody(t, map[string]any{
			"token_type": "refresh_token",
			"id":         "rt-ok",
			"reason":     "manual",
		})))
		req.Header.Set("Content-Type", "application/json")
		h.RevokeOAuth2Token(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected %d, got %d body=%s", http.StatusOK, rec.Code, rec.Body.String())
		}

		h = newOAuth2Handler()
		h.db = &fakeDB{
			queryRowFn: func(_ context.Context, sql string, _ ...any) pgx.Row {
				switch {
				case strings.Contains(sql, "FROM oa2_id_tokens"):
					return fakeRow{err: pgx.ErrNoRows}
				default:
					return fakeRow{err: pgx.ErrNoRows}
				}
			},
		}
		rec = httptest.NewRecorder()
		req = withOperator(httptest.NewRequest(http.MethodPost, "/admin/oauth2/tokens/revoke", oauth2JSONBody(t, map[string]any{
			"token_type": "id_token",
			"id":         "id-missing",
		})))
		req.Header.Set("Content-Type", "application/json")
		h.RevokeOAuth2Token(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected %d, got %d", http.StatusNotFound, rec.Code)
		}

		h = newOAuth2Handler()
		h.db = &fakeDB{
			queryRowFn: func(_ context.Context, sql string, _ ...any) pgx.Row {
				switch {
				case strings.Contains(sql, "FROM oa2_id_tokens"):
					return fakeRow{err: errors.New("id load failed")}
				default:
					return fakeRow{err: pgx.ErrNoRows}
				}
			},
		}
		rec = httptest.NewRecorder()
		req = withOperator(httptest.NewRequest(http.MethodPost, "/admin/oauth2/tokens/revoke", oauth2JSONBody(t, map[string]any{
			"token_type": "id_token",
			"id":         "id-int",
		})))
		req.Header.Set("Content-Type", "application/json")
		h.RevokeOAuth2Token(rec, req)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("expected %d, got %d", http.StatusInternalServerError, rec.Code)
		}

		h = newOAuth2Handler()
		h.db = &fakeDB{
			queryRowFn: func(_ context.Context, sql string, _ ...any) pgx.Row {
				switch {
				case strings.Contains(sql, "FROM oa2_id_tokens"):
					return fakeRow{vals: []any{"client-1", "identity-1", time.Now().UTC().Add(time.Hour)}}
				default:
					return fakeRow{err: pgx.ErrNoRows}
				}
			},
			execFn: func(_ context.Context, sql string, _ ...any) (pgconn.CommandTag, error) {
				if strings.Contains(sql, "UPDATE oa2_id_tokens SET revoked = true") {
					return pgconn.CommandTag{}, errors.New("id revoke failed")
				}
				return pgconn.NewCommandTag("UPDATE 1"), nil
			},
		}
		rec = httptest.NewRecorder()
		req = withOperator(httptest.NewRequest(http.MethodPost, "/admin/oauth2/tokens/revoke", oauth2JSONBody(t, map[string]any{
			"token_type": "id_token",
			"id":         "id-exec",
		})))
		req.Header.Set("Content-Type", "application/json")
		h.RevokeOAuth2Token(rec, req)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("expected %d, got %d", http.StatusInternalServerError, rec.Code)
		}
	})
}

func TestOAuth2ClientBuilderAdditionalBranches(t *testing.T) {
	existingNoRedirect := &oauth2store.Client{
		ID:                      "existing-no-redirect",
		Name:                    "Existing",
		RedirectURIs:            []string{},
		TokenEndpointAuthMethod: "none",
		SubjectType:             "public",
		AccessTokenStrategy:     "opaque",
		AccessTokenTTL:          900,
		RefreshTokenTTL:         900,
		IDTokenTTL:              900,
		AuthCodeTTL:             900,
	}
	if _, _, err := buildOAuth2ClientFromRequest(existingNoRedirect, OAuth2ClientRequest{Name: "Existing"}); err == nil {
		t.Fatal("expected missing redirect URIs to fail")
	}

	base := OAuth2ClientRequest{
		Name:         "Web App",
		RedirectURIs: []string{"https://app.example.com/callback"},
	}
	if _, _, err := buildOAuth2ClientFromRequest(nil, OAuth2ClientRequest{
		Name:                    "Web App",
		RedirectURIs:            []string{"https://app.example.com/callback"},
		PostLogoutRedirectURIs:  []string{"relative/path"},
		TokenEndpointAuthMethod: "none",
	}); err == nil {
		t.Fatal("expected invalid post_logout_redirect_uris to fail")
	}
	if _, _, err := buildOAuth2ClientFromRequest(nil, OAuth2ClientRequest{
		Name:         "Web App",
		RedirectURIs: []string{"https://app.example.com/callback"},
		GrantTypes:   []string{"invalid_grant"},
	}); err == nil {
		t.Fatal("expected invalid grant_types to fail")
	}
	if _, _, err := buildOAuth2ClientFromRequest(nil, OAuth2ClientRequest{
		Name:          "Web App",
		RedirectURIs:  []string{"https://app.example.com/callback"},
		ResponseTypes: []string{"invalid_response"},
	}); err == nil {
		t.Fatal("expected invalid response_types to fail")
	}

	req := base
	req.TokenEndpointAuthMethod = "private_key_jwt"
	req.JWKS = []byte(`{"keys":[{"kty":"RSA","kid":"one"}]}`)
	client, _, err := buildOAuth2ClientFromRequest(nil, req)
	if err != nil {
		t.Fatalf("buildOAuth2ClientFromRequest(jwks copy) error = %v", err)
	}
	if len(client.JWKS) == 0 {
		t.Fatalf("expected non-empty client jwks, got %#v", client.JWKS)
	}
	req.JWKS[0] = 'X'
	if client.JWKS[0] == 'X' {
		t.Fatal("expected JWKS bytes to be copied")
	}

	defaulted, err := normalizeStringSet([]string{" ", ""}, allowedOAuth2GrantTypes, []string{"authorization_code"}, true)
	if err != nil || len(defaulted) != 1 || defaulted[0] != "authorization_code" {
		t.Fatalf("normalizeStringSet(empty values -> defaults) = %#v err=%v", defaulted, err)
	}
	generic := normalizeGenericStringSet([]string{" ", "openid"}, nil)
	if len(generic) != 1 || generic[0] != "openid" {
		t.Fatalf("normalizeGenericStringSet(trim empty values) = %#v", generic)
	}
}

