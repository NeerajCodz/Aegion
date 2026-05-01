package handler

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	tokenservice "github.com/aegion/aegion/modules/oauth2/service/token"
)

type stubIntrospectionService struct {
	resp    *tokenservice.IntrospectionResponse
	err     error
	lastReq *tokenservice.IntrospectionRequest
}

func (s *stubIntrospectionService) IntrospectToken(ctx context.Context, req *tokenservice.IntrospectionRequest) (*tokenservice.IntrospectionResponse, error) {
	s.lastReq = req
	if s.err != nil {
		return nil, s.err
	}
	return s.resp, nil
}

func TestRegisterRoutes(t *testing.T) {
	svc := &stubIntrospectionService{
		resp: &tokenservice.IntrospectionResponse{Active: true, ClientID: "client-1", Subject: "user-1"},
	}
	h := New(svc)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	t.Run("oauth2 introspection with basic auth", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/oauth2/introspect", strings.NewReader("token=opaque-token"))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("client-1:secret-1")))
		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected %d, got %d", http.StatusOK, rec.Code)
		}
		if svc.lastReq == nil || svc.lastReq.ClientID != "client-1" || svc.lastReq.ClientSecret != "secret-1" || svc.lastReq.Token != "opaque-token" {
			t.Fatalf("unexpected introspection request: %+v", svc.lastReq)
		}
		if rec.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("expected no-store header")
		}
	})

	t.Run("json introspection", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/v1/introspection/token", strings.NewReader(`{"token":"opaque-token","client_id":"client-2","client_secret":"secret-2"}`))
		req.Header.Set("Content-Type", "application/json")
		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected %d, got %d", http.StatusOK, rec.Code)
		}
		var body map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		if body["active"] != true {
			t.Fatalf("expected active response, got %+v", body)
		}
		if svc.lastReq == nil || svc.lastReq.ClientID != "client-2" || svc.lastReq.Token != "opaque-token" {
			t.Fatalf("unexpected json introspection request: %+v", svc.lastReq)
		}
	})

	t.Run("invalid client maps to oauth error", func(t *testing.T) {
		svc.err = tokenservice.ErrInvalidClient
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/oauth2/introspect", strings.NewReader("token=opaque-token&client_id=client-1"))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected %d, got %d", http.StatusUnauthorized, rec.Code)
		}
		if rec.Header().Get("WWW-Authenticate") == "" {
			t.Fatal("expected WWW-Authenticate header")
		}
		svc.err = nil
	})

	t.Run("json invalid body", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/v1/introspection/token", strings.NewReader(`{"token":"x","client_id":`))
		req.Header.Set("Content-Type", "application/json")
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected %d, got %d", http.StatusBadRequest, rec.Code)
		}
	})
}

func TestNewAndMethodHandling(t *testing.T) {
	first := New(&stubIntrospectionService{})
	second := New(&stubIntrospectionService{})
	if first == nil || second == nil {
		t.Fatal("New returned nil instance")
	}
	if first == second {
		t.Fatal("New returned shared instance")
	}

	mux := http.NewServeMux()
	first.RegisterRoutes(mux)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/oauth2/introspect", nil)
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected %d, got %d", http.StatusMethodNotAllowed, rec.Code)
	}
}

func TestWriteMappedIntrospectionJSONError(t *testing.T) {
	tests := []struct {
		name   string
		err    error
		status int
	}{
		{name: "invalid client", err: tokenservice.ErrInvalidClient, status: http.StatusUnauthorized},
		{name: "invalid request", err: tokenservice.ErrInvalidRequest, status: http.StatusBadRequest},
		{name: "default", err: errors.New("boom"), status: http.StatusInternalServerError},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			writeMappedIntrospectionJSONError(rec, tt.err)
			if rec.Code != tt.status {
				t.Fatalf("expected %d, got %d", tt.status, rec.Code)
			}
		})
	}
}
