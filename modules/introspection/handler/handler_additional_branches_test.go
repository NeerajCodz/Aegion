package handler

import (
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	tokenservice "github.com/aegion/aegion/modules/oauth2/service/token"
)

func TestIntrospectionHandlerAdditionalBranches(t *testing.T) {
	t.Run("register routes with nil mux", func(t *testing.T) {
		h := New(&stubIntrospectionService{})
		h.RegisterRoutes(nil)
	})

	t.Run("rfc introspection error branches", func(t *testing.T) {
		h := New(nil)

		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/oauth2/introspect", strings.NewReader("token=x"))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		h.handleRFC7662Introspection(rec, req)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("nil service expected %d got %d", http.StatusInternalServerError, rec.Code)
		}

		svc := &stubIntrospectionService{err: tokenservice.ErrInvalidRequest}
		h = New(svc)
		rec = httptest.NewRecorder()
		req = httptest.NewRequest(http.MethodPost, "/oauth2/introspect", strings.NewReader("token=x"))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		h.handleRFC7662Introspection(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("invalid request expected %d got %d", http.StatusBadRequest, rec.Code)
		}

		svc.err = errors.New("boom")
		rec = httptest.NewRecorder()
		req = httptest.NewRequest(http.MethodPost, "/oauth2/introspect", strings.NewReader("token=x"))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		h.handleRFC7662Introspection(rec, req)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("default error expected %d got %d", http.StatusInternalServerError, rec.Code)
		}
	})

	t.Run("json introspection branches", func(t *testing.T) {
		h := New(nil)
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/v1/introspection/token", strings.NewReader(`{"token":"x"}`))
		req.Header.Set("Content-Type", "application/json")
		h.handleJSONIntrospection(rec, req)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("nil service expected %d got %d", http.StatusInternalServerError, rec.Code)
		}

		svc := &stubIntrospectionService{err: tokenservice.ErrInvalidRequest}
		h = New(svc)
		rec = httptest.NewRecorder()
		req = httptest.NewRequest(http.MethodPost, "/api/v1/introspection/token", strings.NewReader(`{"token":"opaque","client_id":"cid","client_secret":"sec"}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("basic-id:basic-secret")))
		h.handleJSONIntrospection(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("invalid request expected %d got %d", http.StatusBadRequest, rec.Code)
		}
		if svc.lastReq == nil || svc.lastReq.ClientID != "basic-id" || svc.lastReq.ClientSecret != "basic-secret" {
			t.Fatalf("expected basic auth override, got %#v", svc.lastReq)
		}

		svc.err = tokenservice.ErrInvalidClient
		rec = httptest.NewRecorder()
		req = httptest.NewRequest(http.MethodPost, "/api/v1/introspection/token", strings.NewReader(`{"token":"opaque"}`))
		req.Header.Set("Content-Type", "application/json")
		h.handleJSONIntrospection(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("invalid client expected %d got %d", http.StatusUnauthorized, rec.Code)
		}
	})

	t.Run("credential helpers and json decoder branches", func(t *testing.T) {
		if id, secret := extractClientCredentials(nil); id != "" || secret != "" {
			t.Fatalf("extractClientCredentials(nil) = %q/%q", id, secret)
		}

		req := httptest.NewRequest(http.MethodPost, "/oauth2/introspect", strings.NewReader("client_id=form-id&client_secret=form-secret"))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		_ = req.ParseForm()
		id, secret := extractClientCredentials(req)
		if id != "form-id" || secret != "form-secret" {
			t.Fatalf("extractClientCredentials(form) = %q/%q", id, secret)
		}

		if _, _, ok := extractBasicAuthCredentials(httptest.NewRequest(http.MethodGet, "/", nil)); ok {
			t.Fatal("extractBasicAuthCredentials(no auth) expected false")
		}

		rec := httptest.NewRecorder()
		req = httptest.NewRequest(http.MethodPost, "/api/v1/introspection/token", strings.NewReader(`{"token":"x"} {}`))
		req.Header.Set("Content-Type", "application/json")
		var payload map[string]any
		if err := decodeJSONBody(rec, req, &payload); err == nil {
			t.Fatal("decodeJSONBody(extra json) expected error")
		}
	})
}

