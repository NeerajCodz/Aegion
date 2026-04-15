package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/aegion/aegion/modules/social/store"
)

func TestListProviders(t *testing.T) {
	svc := New(store.New())
	if _, err := svc.UpsertProvider(context.Background(), ProviderUpsertRequest{
		Preset:             "google",
		ClientID:           "google-client",
		ClientSecret:       "google-secret",
		RedirectURI:        "https://app.example.com/social/google/callback",
		Enabled:            true,
		TrustEmailVerified: true,
	}); err != nil {
		t.Fatalf("UpsertProvider failed: %v", err)
	}
	if _, err := svc.UpsertProvider(context.Background(), ProviderUpsertRequest{
		Preset:             "github",
		ClientID:           "github-client",
		ClientSecret:       "github-secret",
		RedirectURI:        "https://app.example.com/social/github/callback",
		Enabled:            true,
		TrustEmailVerified: true,
	}); err != nil {
		t.Fatalf("UpsertProvider failed: %v", err)
	}

	providers, err := svc.ListProviders(context.Background())
	if err != nil {
		t.Fatalf("ListProviders failed: %v", err)
	}
	if len(providers) != 2 {
		t.Fatalf("expected 2 providers, got %d", len(providers))
	}
	if providers[0].ClientSecret != "" {
		t.Fatal("expected provider secrets to be redacted")
	}
}

func TestStartAuth(t *testing.T) {
	var upstreamURL string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/openid-configuration":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"issuer":                 upstreamURL,
				"authorization_endpoint": upstreamURL + "/authorize",
				"token_endpoint":         upstreamURL + "/token",
				"userinfo_endpoint":      upstreamURL + "/userinfo",
				"jwks_uri":               upstreamURL + "/jwks",
			})
		default:
			http.NotFound(w, r)
		}
	}))
	upstreamURL = upstream.URL
	defer upstream.Close()

	svc := New(store.New()).WithHTTPClient(upstream.Client())
	if _, err := svc.UpsertProvider(context.Background(), ProviderUpsertRequest{
		Preset:             "google",
		DiscoveryURL:       upstream.URL + "/.well-known/openid-configuration",
		ClientID:           "google-client",
		ClientSecret:       "google-secret",
		RedirectURI:        "https://app.example.com/social/google/callback",
		Enabled:            true,
		TrustEmailVerified: true,
	}); err != nil {
		t.Fatalf("UpsertProvider failed: %v", err)
	}

	resp, err := svc.StartAuth(context.Background(), "google", "/dashboard")
	if err != nil {
		t.Fatalf("StartAuth failed: %v", err)
	}
	if resp.Provider != "google" {
		t.Fatalf("unexpected provider: %s", resp.Provider)
	}
	if !strings.Contains(resp.AuthURL, "/authorize") {
		t.Fatalf("expected authorize endpoint, got %s", resp.AuthURL)
	}
	if !strings.Contains(resp.AuthURL, "code_challenge=") {
		t.Fatalf("expected PKCE challenge in auth url, got %s", resp.AuthURL)
	}
}

func TestCompleteAuthOIDCUserInfo(t *testing.T) {
	var upstreamURL string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/openid-configuration":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"issuer":                 upstreamURL,
				"authorization_endpoint": upstreamURL + "/authorize",
				"token_endpoint":         upstreamURL + "/token",
				"userinfo_endpoint":      upstreamURL + "/userinfo",
				"jwks_uri":               upstreamURL + "/jwks",
			})
		case "/token":
			_ = json.NewEncoder(w).Encode(map[string]string{"access_token": "token-1"})
		case "/userinfo":
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
	upstreamURL = upstream.URL
	defer upstream.Close()

	svc := New(store.New()).WithHTTPClient(upstream.Client())
	if _, err := svc.UpsertProvider(context.Background(), ProviderUpsertRequest{
		Preset:             "google",
		DiscoveryURL:       upstream.URL + "/.well-known/openid-configuration",
		ClientID:           "google-client",
		ClientSecret:       "google-secret",
		RedirectURI:        "https://app.example.com/social/google/callback",
		Enabled:            true,
		TrustEmailVerified: true,
	}); err != nil {
		t.Fatalf("UpsertProvider failed: %v", err)
	}

	start, err := svc.StartAuth(context.Background(), "google", "/after-login")
	if err != nil {
		t.Fatalf("StartAuth failed: %v", err)
	}
	result, err := svc.CompleteAuth(context.Background(), "google", start.State, "code-1")
	if err != nil {
		t.Fatalf("CompleteAuth failed: %v", err)
	}
	if result.Profile.ProviderUser != "google-user-1" {
		t.Fatalf("unexpected provider user: %s", result.Profile.ProviderUser)
	}
	if result.IdentityID == "" {
		t.Fatal("expected linked identity id")
	}
	if result.RedirectTo != "/after-login" {
		t.Fatalf("unexpected redirect target: %s", result.RedirectTo)
	}
}
