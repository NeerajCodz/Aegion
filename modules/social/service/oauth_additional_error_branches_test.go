package service

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/aegion/aegion/modules/social/store"
)

func TestSocialOAuthAdditionalErrorBranches(t *testing.T) {
	ctx := context.Background()

	t.Run("resolve provider handles discovery request, transport and decode failures", func(t *testing.T) {
		svc := New(store.New()).WithHTTPClient(&http.Client{
			Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				switch req.URL.String() {
				case "https://provider.example.com/discovery":
					return nil, errors.New("discovery network failure")
				case "https://provider.example.com/discovery-bad-json":
					return jsonResponse(http.StatusOK, "{"), nil
				default:
					return jsonResponse(http.StatusNotFound, `{"error":"not found"}`), nil
				}
			}),
		})

		if _, err := svc.resolveProvider(ctx, store.Provider{
			Protocol:     store.ProtocolOIDC,
			DiscoveryURL: "://bad",
		}); err == nil {
			t.Fatal("expected invalid discovery url error")
		}

		if _, err := svc.resolveProvider(ctx, store.Provider{
			Protocol:     store.ProtocolOIDC,
			DiscoveryURL: "https://provider.example.com/discovery",
		}); err == nil {
			t.Fatal("expected discovery transport error")
		}

		if _, err := svc.resolveProvider(ctx, store.Provider{
			Protocol:     store.ProtocolOIDC,
			DiscoveryURL: "https://provider.example.com/discovery-bad-json",
		}); err == nil {
			t.Fatal("expected discovery decode error")
		}
	})

	t.Run("resolve provider applies discovery scopes and default scopes fallback", func(t *testing.T) {
		svc := New(store.New()).WithHTTPClient(&http.Client{
			Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				switch req.URL.String() {
				case "https://provider.example.com/with-scopes":
					return jsonResponse(http.StatusOK, `{
						"issuer":"https://issuer.example.com",
						"authorization_endpoint":"https://provider.example.com/authorize",
						"token_endpoint":"https://provider.example.com/token",
						"scopes_supported":["openid","email"]
					}`), nil
				case "https://provider.example.com/without-scopes":
					return jsonResponse(http.StatusOK, `{
						"issuer":"https://issuer.example.com",
						"authorization_endpoint":"https://provider.example.com/authorize",
						"token_endpoint":"https://provider.example.com/token"
					}`), nil
				default:
					return jsonResponse(http.StatusNotFound, `{"error":"not found"}`), nil
				}
			}),
		})

		withDiscoveryScopes, err := svc.resolveProvider(ctx, store.Provider{
			Protocol:     store.ProtocolOIDC,
			DiscoveryURL: "https://provider.example.com/with-scopes",
		})
		if err != nil {
			t.Fatalf("resolveProvider(with-scopes) error = %v", err)
		}
		if len(withDiscoveryScopes.Scopes) != 2 {
			t.Fatalf("expected discovery scopes, got %#v", withDiscoveryScopes.Scopes)
		}

		withDefaultScopes, err := svc.resolveProvider(ctx, store.Provider{
			Protocol:     store.ProtocolOIDC,
			DiscoveryURL: "https://provider.example.com/without-scopes",
		})
		if err != nil {
			t.Fatalf("resolveProvider(without-scopes) error = %v", err)
		}
		if len(withDefaultScopes.Scopes) != 3 {
			t.Fatalf("expected default scopes fallback, got %#v", withDefaultScopes.Scopes)
		}
	})

	t.Run("exchange code returns request creation, transport, and decode errors", func(t *testing.T) {
		svc := New(store.New())
		_, err := svc.exchangeCode(ctx, store.Provider{}, resolvedProvider{TokenEndpoint: "://bad"}, store.AuthState{}, "code")
		if err == nil {
			t.Fatal("expected exchangeCode request creation error")
		}

		svc = New(store.New()).WithHTTPClient(&http.Client{
			Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				if req.URL.String() == "https://provider.example.com/token-do-error" {
					return nil, errors.New("token endpoint unavailable")
				}
				if req.URL.String() == "https://provider.example.com/token-bad-json" {
					return jsonResponse(http.StatusOK, "{"), nil
				}
				return jsonResponse(http.StatusNotFound, `{"error":"not found"}`), nil
			}),
		})

		_, err = svc.exchangeCode(ctx, store.Provider{}, resolvedProvider{TokenEndpoint: "https://provider.example.com/token-do-error"}, store.AuthState{}, "code")
		if err == nil {
			t.Fatal("expected exchangeCode transport error")
		}

		_, err = svc.exchangeCode(ctx, store.Provider{}, resolvedProvider{TokenEndpoint: "https://provider.example.com/token-bad-json"}, store.AuthState{}, "code")
		if err == nil {
			t.Fatal("expected exchangeCode decode error")
		}
	})

	t.Run("profile helpers and json claims propagate fetch and verify errors", func(t *testing.T) {
		svc := New(store.New()).WithHTTPClient(&http.Client{
			Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				switch req.URL.String() {
				case "https://provider.example.com/userinfo-do-error":
					return nil, errors.New("userinfo unreachable")
				case "https://provider.example.com/userinfo-status":
					return jsonResponse(http.StatusBadGateway, `{"error":"bad gateway"}`), nil
				case "https://provider.example.com/userinfo-bad-json":
					return jsonResponse(http.StatusOK, "{"), nil
				default:
					return jsonResponse(http.StatusNotFound, `{"error":"not found"}`), nil
				}
			}),
		})

		provider := store.Provider{ClaimMapping: store.ClaimMapping{Subject: "sub", Email: "email"}}
		if _, err := svc.profileFromUserInfo(ctx, provider, resolvedProvider{UserInfoEndpoint: "https://provider.example.com/userinfo-do-error"}, "token"); err == nil {
			t.Fatal("expected profileFromUserInfo fetch error")
		}
		if _, err := svc.profileFromGitHub(ctx, provider, resolvedProvider{UserInfoEndpoint: "https://provider.example.com/userinfo-do-error"}, "token"); err == nil {
			t.Fatal("expected profileFromGitHub fetch error")
		}
		if _, err := svc.profileFromIDToken(ctx, store.Provider{ClientID: "cid"}, resolvedProvider{}, store.AuthState{}, "not-a-jwt"); err == nil {
			t.Fatal("expected profileFromIDToken verify error")
		}

		if _, err := svc.fetchJSONClaims(ctx, http.MethodGet, "https://provider.example.com/userinfo-do-error", "token"); err == nil {
			t.Fatal("expected fetchJSONClaims transport error")
		}
		if _, err := svc.fetchJSONClaims(ctx, http.MethodGet, "https://provider.example.com/userinfo-status", "token"); err == nil {
			t.Fatal("expected fetchJSONClaims status error")
		}
		if _, err := svc.fetchJSONClaims(ctx, http.MethodGet, "https://provider.example.com/userinfo-bad-json", "token"); err == nil {
			t.Fatal("expected fetchJSONClaims decode error")
		}
	})

	t.Run("claimsToProfile falls back to sub or id mapping", func(t *testing.T) {
		profile := claimsToProfile(store.Provider{
			Slug:        "github",
			ClaimSource: store.ClaimSourceGitHubUser,
			ClaimMapping: store.ClaimMapping{
				Subject:       "sub",
				Email:         "email",
				EmailVerified: "email_verified",
			},
		}, map[string]interface{}{
			"id":    "fallback-id",
			"email": "user@example.com",
		})

		if profile.ProviderUser != "fallback-id" {
			t.Fatalf("expected fallback provider user id, got %#v", profile.ProviderUser)
		}
	})
}
