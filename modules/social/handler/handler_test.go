package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	platformcrypto "github.com/aegion/aegion/internal/platform/crypto"
	"github.com/aegion/aegion/modules/social/service"
	"github.com/aegion/aegion/modules/social/store"
)

type stubSocialService struct {
	providers       []store.Provider
	listErr         error
	startResp       *service.StartAuthResponse
	startErr        error
	callbackRes     *service.CallbackResult
	callbackErr     error
	configured      []store.Provider
	configuredErr   error
	getProviderResp *store.Provider
	getProviderErr  error
	upsertResp      *store.Provider
	upsertErr       error
	deleteErr       error
}

func (s *stubSocialService) ListProviders(ctx context.Context) ([]store.Provider, error) {
	return s.providers, s.listErr
}

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

func (s *stubSocialService) ListConfiguredProviders(ctx context.Context, includeDisabled bool) ([]store.Provider, error) {
	return s.configured, s.configuredErr
}

func (s *stubSocialService) GetProvider(ctx context.Context, slug string) (*store.Provider, error) {
	if s.getProviderErr != nil {
		return nil, s.getProviderErr
	}
	return s.getProviderResp, nil
}

func (s *stubSocialService) UpsertProvider(ctx context.Context, req service.ProviderUpsertRequest) (*store.Provider, error) {
	if s.upsertErr != nil {
		return nil, s.upsertErr
	}
	return s.upsertResp, nil
}

func (s *stubSocialService) DeleteProvider(ctx context.Context, slug string) error {
	return s.deleteErr
}

func TestRegisterRoutesAndHandlers(t *testing.T) {
	h := New(&stubSocialService{
		providers: []store.Provider{{Slug: "google", DisplayName: "Google"}},
		startResp: &service.StartAuthResponse{
			Provider: "google",
			AuthURL:  "https://accounts.google.com/o/oauth2/v2/auth?state=s1",
			State:    "s1",
		},
		callbackRes: &service.CallbackResult{
			Provider:   "google",
			IdentityID: "identity-1",
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
		req.Header.Set("Accept", "application/json")
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected %d, got %d", http.StatusOK, rec.Code)
		}
	})
}

func TestManagementRoutesRequireToken(t *testing.T) {
	h := New(&stubSocialService{})
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/social/admin/providers", nil)
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected %d, got %d", http.StatusServiceUnavailable, rec.Code)
	}
}

func TestManagementRoutes(t *testing.T) {
	provider := &store.Provider{Slug: "custom-oidc", DisplayName: "Custom OIDC"}
	h := New(&stubSocialService{
		configured:      []store.Provider{{Slug: "google"}},
		getProviderResp: provider,
		upsertResp:      provider,
	}, Config{ManagementToken: "secret-token"})
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	t.Run("list providers", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/v1/social/admin/providers", nil)
		req.Header.Set("Authorization", "Bearer secret-token")
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected %d, got %d", http.StatusOK, rec.Code)
		}
	})

	t.Run("upsert provider", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/v1/social/admin/providers", strings.NewReader(`{"slug":"custom-oidc","display_name":"Custom OIDC"}`))
		req.Header.Set("Authorization", "Bearer secret-token")
		req.Header.Set("Content-Type", "application/json")
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected %d, got %d", http.StatusOK, rec.Code)
		}
	})

	t.Run("get provider", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/v1/social/admin/providers/custom-oidc", nil)
		req.Header.Set("Authorization", "Bearer secret-token")
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected %d, got %d", http.StatusOK, rec.Code)
		}
		var body store.Provider
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("failed to parse provider body: %v", err)
		}
		if body.Slug != "custom-oidc" {
			t.Fatalf("unexpected provider slug: %s", body.Slug)
		}
	})
}

func TestManagementRoutesRequireCoreSignedIdentity(t *testing.T) {
	secret := []byte("0123456789abcdef0123456789abcdef")
	h := New(&stubSocialService{
		configured: []store.Provider{{Slug: "google"}},
	}, Config{
		ManagementToken:       "management-token",
		IdentitySigningSecret: secret,
	})
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	newRequest := func() *http.Request {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/social/admin/providers", nil)
		req.Header.Set("Authorization", "Bearer management-token")
		req.Header.Set("X-User-ID", "identity-1")
		req.Header.Set("X-User-Session-ID", "session-1")
		req.Header.Set("X-User-AAL", "aal2")
		return req
	}

	t.Run("rejects unsigned identity headers", func(t *testing.T) {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, newRequest())
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
		}
	})

	t.Run("accepts signed identity headers", func(t *testing.T) {
		req := newRequest()
		signature, err := platformcrypto.SignIdentityHeaders(
			secret,
			req.Header,
			[]string{"X-User-ID", "X-User-Session-ID", "X-User-AAL"},
			time.Now().UTC(),
		)
		if err != nil {
			t.Fatalf("sign identity headers: %v", err)
		}
		req.Header.Set("X-Aegion-Signature", signature)

		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
		}
	})

	t.Run("rejects tampered signed identity headers", func(t *testing.T) {
		req := newRequest()
		signature, err := platformcrypto.SignIdentityHeaders(
			secret,
			req.Header,
			[]string{"X-User-ID", "X-User-Session-ID", "X-User-AAL"},
			time.Now().UTC(),
		)
		if err != nil {
			t.Fatalf("sign identity headers: %v", err)
		}
		req.Header.Set("X-Aegion-Signature", signature)
		req.Header.Set("X-User-AAL", "aal3")

		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
		}
	})
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
