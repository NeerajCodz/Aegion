package service

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	platformjwt "github.com/aegion/aegion/internal/platform/jwt"
	"github.com/aegion/aegion/modules/social/store"
)

type roundTripFunc func(req *http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func jsonResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func TestServiceManagementMethodsAndHelpers(t *testing.T) {
	ctx := context.Background()
	repo := store.New()
	svc := New(repo)

	if err := svc.EnsurePresetProviders(ctx); err != nil {
		t.Fatalf("EnsurePresetProviders() error = %v", err)
	}
	configured, err := svc.ListConfiguredProviders(ctx, true)
	if err != nil || len(configured) == 0 {
		t.Fatalf("ListConfiguredProviders(includeDisabled=true) len=%d err=%v", len(configured), err)
	}

	prov, err := svc.UpsertProvider(ctx, ProviderUpsertRequest{
		Preset:             "google",
		ClientID:           "google-client",
		ClientSecret:       "secret",
		RedirectURI:        "https://app.example.com/callback",
		Enabled:            true,
		TrustEmailVerified: true,
	})
	if err != nil {
		t.Fatalf("UpsertProvider() error = %v", err)
	}
	if prov.ClientSecret != "" {
		t.Fatalf("UpsertProvider() should return sanitized provider")
	}

	got, err := svc.GetProvider(ctx, "google")
	if err != nil {
		t.Fatalf("GetProvider() error = %v", err)
	}
	if got.Slug != "google" || got.ClientSecret != "" {
		t.Fatalf("GetProvider() unexpected provider: %#v", got)
	}
	list, err := svc.ListConfiguredProviders(ctx, false)
	if err != nil || len(list) == 0 {
		t.Fatalf("ListConfiguredProviders(includeDisabled=false) len=%d err=%v", len(list), err)
	}
	if err := svc.DeleteProvider(ctx, "google"); err != nil {
		t.Fatalf("DeleteProvider() error = %v", err)
	}
	if _, err := svc.GetProvider(ctx, "google"); !errors.Is(err, store.ErrProviderNotFound) {
		t.Fatalf("GetProvider(deleted) error = %v, want %v", err, store.ErrProviderNotFound)
	}

	p, err := svc.buildProvider(ProviderUpsertRequest{
		Slug:               " custom ",
		DisplayName:        "",
		Protocol:           "",
		PKCEMethod:         "",
		AuthStyle:          "",
		ClaimSource:        "",
		Enabled:            false,
		RedirectURI:        "",
		ClientID:           "",
		ClaimMapping:       store.ClaimMapping{},
		ExtraAuthParams:    nil,
		TrustEmailVerified: false,
	})
	if err != nil {
		t.Fatalf("buildProvider(defaults) error = %v", err)
	}
	if p.DisplayName == "" || p.Protocol == "" || p.PKCEMethod == "" || p.AuthStyle == "" || p.ClaimSource == "" || len(p.Scopes) == 0 {
		t.Fatalf("buildProvider(defaults) missing defaults: %#v", p)
	}
	if _, err := svc.buildProvider(ProviderUpsertRequest{Slug: "x", Enabled: true}); !errors.Is(err, ErrProviderMisconfig) {
		t.Fatalf("buildProvider(enabled without credentials) error = %v, want %v", err, ErrProviderMisconfig)
	}
	if _, err := svc.buildProvider(ProviderUpsertRequest{Preset: "does-not-exist", Slug: "x"}); err == nil {
		t.Fatalf("buildProvider(unknown preset) expected error")
	}

	merged := mergePreset(
		store.Provider{
			Slug:               "google",
			DisplayName:        "Google",
			Enabled:            false,
			TrustEmailVerified: false,
			ExtraAuthParams:    map[string]string{"prompt": "consent"},
			ClaimMapping:       store.ClaimMapping{Subject: "sub", Email: "email"},
		},
		store.Provider{
			DisplayName:        "Custom Google",
			Enabled:            true,
			TrustEmailVerified: true,
			ExtraAuthParams:    map[string]string{"hd": "example.com"},
			ClaimMapping:       store.ClaimMapping{Name: "name"},
		},
	)
	if merged.DisplayName != "Custom Google" || !merged.Enabled || !merged.TrustEmailVerified || merged.ExtraAuthParams["hd"] != "example.com" {
		t.Fatalf("mergePreset() unexpected merge: %#v", merged)
	}
	if m := mergeClaimMapping(store.ClaimMapping{Subject: "sub"}, store.ClaimMapping{Email: "email"}); m.Subject != "sub" || m.Email != "email" {
		t.Fatalf("mergeClaimMapping() unexpected value: %#v", m)
	}
	if got := normalizeRedirectTarget(" "); got != "/" {
		t.Fatalf("normalizeRedirectTarget(blank) = %q, want /", got)
	}
	if got := normalizeRedirectTarget("/after"); got != "/after" {
		t.Fatalf("normalizeRedirectTarget(non-blank) = %q, want /after", got)
	}
	if got := copyMap(nil); len(got) != 0 {
		t.Fatalf("copyMap(nil) len = %d, want 0", len(got))
	}
}

func TestOAuthJWTAndClaimHelpers(t *testing.T) {
	now := time.Now().UTC()
	kp, err := platformjwt.GenerateECKeyPair("kid-social")
	if err != nil {
		t.Fatalf("GenerateECKeyPair() error = %v", err)
	}
	token, err := platformjwt.Sign(platformjwt.Claims{
		Issuer:    "https://issuer.example.com",
		Audience:  "client-1",
		Subject:   "social-user-1",
		IssuedAt:  now.Unix(),
		ExpiresAt: now.Add(5 * time.Minute).Unix(),
		Custom: map[string]interface{}{
			"nonce":          "nonce-1",
			"email":          "user@example.com",
			"email_verified": true,
			"name":           "Example User",
			"picture":        "https://example.com/u.png",
		},
	}, kp.PrivateKey, kp.Algorithm, kp.KeyID)
	if err != nil {
		t.Fatalf("Sign() error = %v", err)
	}

	jwkJSON, err := platformjwt.ToJWK(kp.Algorithm, kp.KeyID, kp.PublicKey)
	if err != nil {
		t.Fatalf("ToJWK() error = %v", err)
	}
	var key jwk
	if err := json.Unmarshal([]byte(jwkJSON), &key); err != nil {
		t.Fatalf("Unmarshal JWK error = %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/jwks":
			_ = json.NewEncoder(w).Encode(jwksDocument{Keys: []jwk{key}})
		case "/userinfo":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"sub":            "social-user-1",
				"email":          "user@example.com",
				"email_verified": true,
				"name":           "Example User",
				"picture":        "https://example.com/u.png",
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	svc := New(store.New()).WithHTTPClient(server.Client())
	provider := store.Provider{
		Slug:       "oidc",
		ClientID:   "client-1",
		ClaimSource: store.ClaimSourceIDToken,
		ClaimMapping: store.ClaimMapping{
			Subject:       "sub",
			Email:         "email",
			EmailVerified: "email_verified",
			Name:          "name",
			Picture:       "picture",
		},
	}
	resolved := resolvedProvider{
		Issuer:           "https://issuer.example.com",
		JWKSURI:          server.URL + "/jwks",
		UserInfoEndpoint: server.URL + "/userinfo",
	}

	claims, err := svc.verifyAndDecodeIDToken(context.Background(), token, "client-1", "https://issuer.example.com", server.URL+"/jwks")
	if err != nil {
		t.Fatalf("verifyAndDecodeIDToken() error = %v", err)
	}
	if claims["sub"] != "social-user-1" {
		t.Fatalf("verifyAndDecodeIDToken() claims = %#v", claims)
	}

	profile, err := svc.profileFromIDToken(context.Background(), provider, resolved, store.AuthState{Nonce: "nonce-1"}, token)
	if err != nil {
		t.Fatalf("profileFromIDToken() error = %v", err)
	}
	if profile.ProviderUser != "social-user-1" || profile.Email != "user@example.com" {
		t.Fatalf("profileFromIDToken() unexpected profile: %#v", profile)
	}
	if _, err := svc.profileFromIDToken(context.Background(), provider, resolved, store.AuthState{Nonce: "wrong"}, token); err == nil {
		t.Fatalf("profileFromIDToken(nonce mismatch) expected error")
	}
	if _, err := svc.profileFromIDToken(context.Background(), provider, resolved, store.AuthState{}, " "); !errors.Is(err, ErrInvalidCallback) {
		t.Fatalf("profileFromIDToken(empty token) error = %v, want %v", err, ErrInvalidCallback)
	}

	claimsFromUserInfo, err := svc.fetchJSONClaims(context.Background(), http.MethodGet, server.URL+"/userinfo", "token")
	if err != nil || claimsFromUserInfo["sub"] != "social-user-1" {
		t.Fatalf("fetchJSONClaims() claims=%#v err=%v", claimsFromUserInfo, err)
	}
	if _, err := svc.fetchJSONClaims(context.Background(), http.MethodGet, "http://[::1", ""); err == nil {
		t.Fatalf("fetchJSONClaims(invalid endpoint) expected error")
	}
	if _, err := svc.fetchJWKS(context.Background(), server.URL+"/jwks"); err != nil {
		t.Fatalf("fetchJWKS() error = %v", err)
	}

	header := map[string]string{"alg": "HS256", "kid": "kid-1"}
	payload := map[string]interface{}{"sub": "1", "email": "e@example.com"}
	headerRaw, _ := json.Marshal(header)
	payloadRaw, _ := json.Marshal(payload)
	manualToken := base64.RawURLEncoding.EncodeToString(headerRaw) + "." + base64.RawURLEncoding.EncodeToString(payloadRaw) + ".sig"
	if _, err := parseJWTHeader(manualToken); err != nil {
		t.Fatalf("parseJWTHeader() error = %v", err)
	}
	if _, err := parseJWTPayload(manualToken); err != nil {
		t.Fatalf("parseJWTPayload() error = %v", err)
	}
	if _, err := parseJWTHeader("bad"); err == nil {
		t.Fatalf("parseJWTHeader(invalid) expected error")
	}
	if _, err := parseJWTPayload("bad"); err == nil {
		t.Fatalf("parseJWTPayload(invalid) expected error")
	}

	if _, err := decodeBase64URLInt("AQAB"); err != nil {
		t.Fatalf("decodeBase64URLInt() error = %v", err)
	}
	if _, err := decodeBase64URLInt("***"); err == nil {
		t.Fatalf("decodeBase64URLInt(invalid) expected error")
	}
	if _, err := jwkToVerifyKey(jwk{Kty: "EC", Crv: "P-256", X: "***", Y: "AQAB"}); err == nil {
		t.Fatalf("jwkToVerifyKey(invalid x) expected error")
	}
	if _, err := jwkToVerifyKey(jwk{Kty: "EC", Crv: "P-384"}); err == nil {
		t.Fatalf("jwkToVerifyKey(unsupported ec curve) expected error")
	}
	if _, err := jwkToVerifyKey(jwk{Kty: "RSA", N: base64.RawURLEncoding.EncodeToString([]byte{1, 2, 3}), E: "***"}); err == nil {
		t.Fatalf("jwkToVerifyKey(invalid rsa exponent) expected error")
	}
	if _, err := jwkToVerifyKey(jwk{Kty: "oct"}); err == nil {
		t.Fatalf("jwkToVerifyKey(unsupported kty) expected error")
	}

	if got := shortLeeway(); got != 30*time.Second {
		t.Fatalf("shortLeeway() = %v, want 30s", got)
	}
	if got := claimValue(map[string]interface{}{"id": float64(123)}, "sub|id"); got != "123" {
		t.Fatalf("claimValue(float mapping) = %q, want 123", got)
	}
	if got := claimValue(map[string]interface{}{"a": json.Number("45")}, "a"); got != "45" {
		t.Fatalf("claimValue(json number) = %q, want 45", got)
	}
	if got := claimBool(map[string]interface{}{"verified": "YES"}, "verified"); !got {
		t.Fatalf("claimBool(string true) expected true")
	}
	if got := claimBool(map[string]interface{}{"verified": false}, "verified"); got {
		t.Fatalf("claimBool(bool false) expected false")
	}
}

func TestOAuthFlowBranchesAndProviderLoading(t *testing.T) {
	ctx := context.Background()
	repo := store.New()

	svc := New(repo).WithHTTPClient(&http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			switch req.URL.String() {
			case "https://provider.example.com/.well-known/openid-configuration":
				return jsonResponse(200, `{"issuer":"https://provider.example.com","authorization_endpoint":"https://provider.example.com/authorize","token_endpoint":"https://provider.example.com/token","userinfo_endpoint":"https://provider.example.com/userinfo","jwks_uri":"https://provider.example.com/jwks","scopes_supported":["openid","email"]}`), nil
			case "https://provider.example.com/token":
				if strings.Contains(req.Header.Get("Authorization"), "Basic") {
					return jsonResponse(200, `{"access_token":"at-basic"}`), nil
				}
				return jsonResponse(200, `{"access_token":"at-post"}`), nil
			case "https://provider.example.com/userinfo":
				return jsonResponse(200, `{"sub":"u1","email":"user@example.com","email_verified":"true","name":"User","picture":"https://example.com/u.png"}`), nil
			case "https://api.github.com/user":
				return jsonResponse(200, `{"id":"42","login":"octocat"}`), nil
			case "https://api.github.com/user/emails":
				return jsonResponse(200, `[{"email":"octo@example.com","verified":true,"primary":true}]`), nil
			default:
				return jsonResponse(404, `{"error":"not found"}`), nil
			}
		}),
	})

	googleProvider := store.Provider{
		Slug:         "google",
		DisplayName:  "Google",
		Protocol:     store.ProtocolOIDC,
		DiscoveryURL: "https://provider.example.com/.well-known/openid-configuration",
		ClientID:     "cid",
		ClientSecret: "secret",
		RedirectURI:  "https://app.example.com/callback",
		Scopes:       []string{"openid", "email"},
		ClaimMapping: store.ClaimMapping{
			Subject:       "sub",
			Email:         "email",
			EmailVerified: "email_verified",
			Name:          "name",
			Picture:       "picture",
		},
		PKCEMethod: store.PKCES256,
		AuthStyle:  store.AuthStyleClientSecretPost,
		ClaimSource: store.ClaimSourceUserInfo,
		Enabled:     true,
	}
	if _, err := repo.UpsertProvider(ctx, googleProvider); err != nil {
		t.Fatalf("repo.UpsertProvider(google) error = %v", err)
	}

	_, resolved, err := svc.loadProvider(ctx, "google")
	if err != nil || resolved.TokenEndpoint == "" {
		t.Fatalf("loadProvider(google) resolved=%#v err=%v", resolved, err)
	}
	if _, err := svc.resolveProvider(ctx, store.Provider{Protocol: store.ProtocolOIDC}); !errors.Is(err, ErrProviderMisconfig) {
		t.Fatalf("resolveProvider(missing endpoints) error = %v, want %v", err, ErrProviderMisconfig)
	}
	if _, err := svc.fetchDiscovery(ctx, "https://provider.example.com/.well-known/openid-configuration"); err != nil {
		t.Fatalf("fetchDiscovery(first) error = %v", err)
	}
	if _, err := svc.fetchDiscovery(ctx, "https://provider.example.com/.well-known/openid-configuration"); err != nil {
		t.Fatalf("fetchDiscovery(cache hit) error = %v", err)
	}
	if _, err := svc.fetchDiscovery(ctx, "https://provider.example.com/missing"); err == nil {
		t.Fatalf("fetchDiscovery(non-2xx) expected error")
	}

	state := store.AuthState{ID: "state-1", ProviderSlug: "google", RedirectTo: "/x", Nonce: "n1", PKCEVerifier: "verifier"}
	payload, err := svc.exchangeCode(ctx, googleProvider, resolvedProvider{TokenEndpoint: "https://provider.example.com/token"}, state, "code")
	if err != nil || payload.AccessToken == "" {
		t.Fatalf("exchangeCode(post) payload=%#v err=%v", payload, err)
	}

	basicProvider := googleProvider
	basicProvider.AuthStyle = store.AuthStyleClientSecretBasic
	basicPayload, err := svc.exchangeCode(ctx, basicProvider, resolvedProvider{TokenEndpoint: "https://provider.example.com/token"}, state, "code")
	if err != nil || basicPayload.AccessToken == "" {
		t.Fatalf("exchangeCode(basic) payload=%#v err=%v", basicPayload, err)
	}
	if _, err := svc.exchangeCode(ctx, googleProvider, resolvedProvider{TokenEndpoint: "https://provider.example.com/missing"}, state, "code"); err == nil {
		t.Fatalf("exchangeCode(non-2xx) expected error")
	}
	if profile, err := svc.profileFromUserInfo(ctx, googleProvider, resolvedProvider{UserInfoEndpoint: "https://provider.example.com/userinfo"}, "at"); err != nil || profile.ProviderUser != "u1" {
		t.Fatalf("profileFromUserInfo() profile=%#v err=%v", profile, err)
	}
	if _, err := svc.profileFromUserInfo(ctx, googleProvider, resolvedProvider{}, ""); !errors.Is(err, ErrProviderMisconfig) {
		t.Fatalf("profileFromUserInfo(misconfig) error = %v, want %v", err, ErrProviderMisconfig)
	}

	githubProvider := store.Provider{
		Slug:       "github",
		ClaimSource: store.ClaimSourceGitHubUser,
		ClaimMapping: store.ClaimMapping{
			Subject: "id",
			Email:   "email",
			Name:    "name|login",
		},
	}
	githubProfile, err := svc.profileFromGitHub(ctx, githubProvider, resolvedProvider{UserInfoEndpoint: "https://api.github.com/user"}, "gh-token")
	if err != nil || githubProfile.Email != "octo@example.com" || !githubProfile.EmailVerified {
		t.Fatalf("profileFromGitHub() profile=%#v err=%v", githubProfile, err)
	}

	if p, err := svc.fetchProfile(ctx, githubProvider, resolvedProvider{UserInfoEndpoint: "https://api.github.com/user"}, state, &tokenPayload{AccessToken: "gh-token"}); err != nil || p.ProviderUser == "" {
		t.Fatalf("fetchProfile(github) profile=%#v err=%v", p, err)
	}
	if p, err := svc.fetchProfile(ctx, googleProvider, resolvedProvider{UserInfoEndpoint: "https://provider.example.com/userinfo"}, state, &tokenPayload{AccessToken: "at"}); err != nil || p.ProviderUser == "" {
		t.Fatalf("fetchProfile(userinfo) profile=%#v err=%v", p, err)
	}
	idTokenProvider := googleProvider
	idTokenProvider.ClaimSource = store.ClaimSourceIDToken
	if _, err := svc.fetchProfile(ctx, idTokenProvider, resolvedProvider{}, store.AuthState{}, &tokenPayload{IDToken: ""}); !errors.Is(err, ErrInvalidCallback) {
		t.Fatalf("fetchProfile(id token empty) error = %v, want %v", err, ErrInvalidCallback)
	}

	if _, err := authorizationURL(googleProvider, resolvedProvider{AuthorizeEndpoint: "://bad", Scopes: []string{"openid"}}, "s", "n", "c"); err == nil {
		t.Fatalf("authorizationURL(invalid endpoint) expected error")
	}
	urlValue, err := authorizationURL(googleProvider, resolvedProvider{AuthorizeEndpoint: "https://provider.example.com/authorize", Scopes: []string{"openid"}}, "state-1", "nonce-1", "challenge-1")
	if err != nil || !strings.Contains(urlValue, "state=state-1") || !strings.Contains(urlValue, "code_challenge=") {
		t.Fatalf("authorizationURL() value=%q err=%v", urlValue, err)
	}

	if _, _, err := svc.loadProvider(ctx, "missing"); !errors.Is(err, ErrProviderUnsupported) {
		t.Fatalf("loadProvider(missing) error = %v, want %v", err, ErrProviderUnsupported)
	}
	disabled := googleProvider
	disabled.Slug = "disabled"
	disabled.Enabled = false
	if _, err := repo.UpsertProvider(ctx, disabled); err != nil {
		t.Fatalf("repo.UpsertProvider(disabled) error = %v", err)
	}
	if _, _, err := svc.loadProvider(ctx, "disabled"); !errors.Is(err, ErrProviderUnsupported) {
		t.Fatalf("loadProvider(disabled) error = %v, want %v", err, ErrProviderUnsupported)
	}

	if _, err := randomHexToken(8); err != nil {
		t.Fatalf("randomHexToken() error = %v", err)
	}
	if _, err := randomPKCEVerifier(); err != nil {
		t.Fatalf("randomPKCEVerifier() error = %v", err)
	}

	profile := claimsToProfile(githubProvider, map[string]interface{}{"id": float64(7), "email": "x@example.com"})
	if profile.ProviderUser != "7" || !profile.EmailVerified {
		t.Fatalf("claimsToProfile(github fallback verify) = %#v", profile)
	}
}
