package service

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/aegion/aegion/modules/social/store"
)

func makeJWTForTests(header map[string]any, payload map[string]any) string {
	h, _ := json.Marshal(header)
	p, _ := json.Marshal(payload)
	return base64.RawURLEncoding.EncodeToString(h) + "." + base64.RawURLEncoding.EncodeToString(p) + ".sig"
}

func TestSocialServiceOAuthJWTMoreBranches(t *testing.T) {
	ctx := context.Background()

	svc := New(store.New()).WithHTTPClient(&http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			switch req.URL.Path {
			case "/jwks":
				return jsonResponse(http.StatusOK, `{"keys":[{"kid":"k2","kty":"EC","alg":"ES256","crv":"P-256","x":"AQAB","y":"AQAB"}]}`), nil
			case "/token-empty":
				return jsonResponse(http.StatusOK, `{"token_type":"Bearer"}`), nil
			default:
				return jsonResponse(http.StatusBadRequest, `{`), nil
			}
		}),
	})

	token := makeJWTForTests(map[string]any{"alg": "ES256", "kid": "k1"}, map[string]any{"sub": "u1"})
	if _, err := svc.verifyAndDecodeIDToken(ctx, token, "cid", "iss", "https://provider.example.com/jwks"); err == nil || !strings.Contains(err.Error(), "provider jwk not found") {
		t.Fatalf("verifyAndDecodeIDToken(jwk miss) = %v", err)
	}

	if _, err := svc.fetchJWKS(ctx, "://bad-uri"); err == nil {
		t.Fatal("fetchJWKS(invalid uri) expected error")
	}
	if _, err := svc.fetchJWKS(ctx, "https://provider.example.com/not-jwks"); err == nil {
		t.Fatal("fetchJWKS(decode error) expected error")
	}

	if _, err := parseJWTHeader("**.a.b"); err == nil {
		t.Fatal("parseJWTHeader(base64 error) expected error")
	}
	if _, err := parseJWTPayload("a.**.b"); err == nil {
		t.Fatal("parseJWTPayload(base64 error) expected error")
	}

	provider := store.Provider{
		Slug:             "google",
		RedirectURI:      "https://app.example.com/cb",
		ClientID:         "cid",
		ClientSecret:     "secret",
		AuthStyle:        store.AuthStyleClientSecretPost,
		PKCEMethod:       store.PKCENone,
		ClaimSource:      store.ClaimSourceUserInfo,
		ClaimMapping:     store.ClaimMapping{Subject: "sub", Email: "email"},
		TokenEndpoint:    "https://provider.example.com/token-empty",
		UserInfoEndpoint: "https://provider.example.com/user",
	}
	if _, err := svc.exchangeCode(ctx, provider, resolvedProvider{TokenEndpoint: "https://provider.example.com/token-empty"}, store.AuthState{}, "code"); err != ErrInvalidCallback {
		t.Fatalf("exchangeCode(empty token payload) = %v", err)
	}

	svc = New(store.New()).WithHTTPClient(&http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			switch req.URL.String() {
			case "https://api.github.com/user":
				return jsonResponse(http.StatusOK, `{"id":"42","login":"octo"}`), nil
			case "https://api.github.com/user/emails":
				return jsonResponse(http.StatusOK, `[{"email":"secondary@example.com","verified":true,"primary":false}]`), nil
			default:
				return jsonResponse(http.StatusNotFound, `{"error":"not found"}`), nil
			}
		}),
	})
	githubProvider := store.Provider{
		Slug:        "github",
		ClaimSource: store.ClaimSourceGitHubUser,
		ClaimMapping: store.ClaimMapping{
			Subject: "id",
			Email:   "email",
			Name:    "name|login",
		},
	}
	profile, err := svc.profileFromGitHub(ctx, githubProvider, resolvedProvider{UserInfoEndpoint: "https://api.github.com/user"}, "token")
	if err != nil || profile.Email != "secondary@example.com" || !profile.EmailVerified {
		t.Fatalf("profileFromGitHub(verified fallback email) profile=%#v err=%v", profile, err)
	}
}
