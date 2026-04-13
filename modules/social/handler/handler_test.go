package handler

import (
	"context"
	"errors"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/aegion/aegion/modules/social/service"
	"github.com/aegion/aegion/modules/social/store"
)

type stubSocialService struct {
	providers   []string
	startResp   *service.StartAuthResponse
	startErr    error
	callbackRes *service.CallbackResult
	callbackErr error
}

func (s *stubSocialService) ListProviders() []string { return s.providers }
func (s *stubSocialService) StartAuth(ctx context.Context, provider, redirectTo string) (*service.StartAuthResponse, error) {
	if s.startErr != nil {
		return nil, s.startErr
	}
	return s.startResp, nil
}
func (s *stubSocialService) CompleteAuth(ctx context.Context, provider, stateID, code string) (*service.CallbackResult, error) {
	if s.callbackErr != nil {
		return nil, s.callbackErr
	}
	return s.callbackRes, nil
}

func TestRegisterRoutesAndHandlers(t *testing.T) {
	h := New(&stubSocialService{
		providers: []string{"google", "github"},
		startResp: &service.StartAuthResponse{
			Provider: "google",
			AuthURL:  "https://accounts.google.com/o/oauth2/v2/auth?state=s1",
			State:    "s1",
		},
		callbackRes: &service.CallbackResult{
			Provider: "google",
			Profile: store.SocialProfile{
				ProviderUser: "google-user-1",
				Email:        "user@example.com",
			},
		},
	})
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	t.Run("providers endpoint", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/v1/social/providers", nil)
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected %d, got %d", http.StatusOK, rec.Code)
		}
		var body map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("failed parsing body: %v", err)
		}
		if _, ok := body["providers"]; !ok {
			t.Fatal("expected providers in response")
		}
	})

	t.Run("start endpoint", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/v1/social/google/start", strings.NewReader(`{"redirect_to":"/home"}`))
		req.Header.Set("Content-Type", "application/json")
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected %d, got %d", http.StatusOK, rec.Code)
		}
	})

	t.Run("callback endpoint", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/v1/social/google/callback?state=s1&code=c1", nil)
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected %d, got %d", http.StatusOK, rec.Code)
		}
	})
}

func TestStartRouteProviderErrors(t *testing.T) {
	h := New(&stubSocialService{startErr: service.ErrProviderUnsupported})
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/social/github/start", strings.NewReader(`{"redirect_to":"/"}`))
	req.Header.Set("Content-Type", "application/json")
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected %d, got %d", http.StatusBadRequest, rec.Code)
	}
}

func TestCallbackRouteErrors(t *testing.T) {
	h := New(&stubSocialService{callbackErr: errors.New("provider down")})
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/social/google/callback?state=s1&code=c1", nil)
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("expected %d, got %d", http.StatusBadGateway, rec.Code)
	}
}
