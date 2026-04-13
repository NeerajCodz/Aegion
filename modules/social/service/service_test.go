package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/aegion/aegion/modules/social/store"
)

func testService(t *testing.T) *Service {
	t.Helper()
	return New(store.New(), Config{
		Google: ProviderConfig{
			ClientID:     "google-client",
			ClientSecret: "google-secret",
			RedirectURI:  "https://app.example.com/social/google/callback",
		},
		GitHub: ProviderConfig{
			ClientID:     "github-client",
			ClientSecret: "github-secret",
			RedirectURI:  "https://app.example.com/social/github/callback",
		},
		StateTTL: time.Minute,
	})
}

func TestListProviders(t *testing.T) {
	svc := testService(t)
	providers := svc.ListProviders()
	if len(providers) != 2 {
		t.Fatalf("expected 2 providers, got %d", len(providers))
	}
}

func TestStartAuth(t *testing.T) {
	svc := testService(t)
	resp, err := svc.StartAuth(context.Background(), "google", "/dashboard")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if resp.Provider != "google" {
		t.Fatalf("unexpected provider: %s", resp.Provider)
	}
	if !strings.Contains(resp.AuthURL, "accounts.google.com") {
		t.Fatalf("expected google auth url, got %s", resp.AuthURL)
	}
	if resp.State == "" {
		t.Fatal("expected generated state")
	}
}

func TestCompleteAuth(t *testing.T) {
	tokenCalled := false
	userCalled := false
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			tokenCalled = true
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]string{"access_token": "token-1"})
		case "/userinfo":
			userCalled = true
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"sub":            "google-user-1",
				"email":          "user@example.com",
				"email_verified": true,
				"name":           "Google User",
				"picture":        "https://example.com/u.png",
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()

	svc := testService(t).WithEndpoints(OAuthEndpoints{
		GoogleAuthorize: upstream.URL + "/authorize",
		GoogleToken:     upstream.URL + "/token",
		GoogleUserInfo:  upstream.URL + "/userinfo",
		GitHubAuthorize: upstream.URL + "/gh-authorize",
		GitHubToken:     upstream.URL + "/gh-token",
		GitHubUser:      upstream.URL + "/gh-user",
	})

	start, err := svc.StartAuth(context.Background(), "google", "/after-login")
	if err != nil {
		t.Fatalf("start auth failed: %v", err)
	}
	result, err := svc.CompleteAuth(context.Background(), "google", start.State, "code-1")
	if err != nil {
		t.Fatalf("complete auth failed: %v", err)
	}
	if !tokenCalled || !userCalled {
		t.Fatal("expected token exchange and userinfo calls")
	}
	if result.Profile.ProviderUser != "google-user-1" {
		t.Fatalf("unexpected provider user: %s", result.Profile.ProviderUser)
	}
	if result.RedirectTo != "/after-login" {
		t.Fatalf("unexpected redirect target: %s", result.RedirectTo)
	}
}
