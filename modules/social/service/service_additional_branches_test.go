package service

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/aegion/aegion/modules/social/store"
	"github.com/google/uuid"
)

type socialRepoStub struct {
	provider      *store.Provider
	state         store.AuthState
	linkResult    *store.IdentityLinkResult
	listErr       error
	getErr        error
	upsertErr     error
	deleteErr     error
	saveStateErr  error
	consumeErr    error
	resolveErr    error
	consumeState  store.AuthState
	listProviders []store.Provider
}

func (s *socialRepoStub) ListProviders(context.Context, bool) ([]store.Provider, error) {
	if s.listErr != nil {
		return nil, s.listErr
	}
	if s.listProviders != nil {
		return s.listProviders, nil
	}
	return []store.Provider{}, nil
}

func (s *socialRepoStub) GetProviderBySlug(context.Context, string) (*store.Provider, error) {
	if s.getErr != nil {
		return nil, s.getErr
	}
	if s.provider == nil {
		return nil, store.ErrProviderNotFound
	}
	p := *s.provider
	return &p, nil
}

func (s *socialRepoStub) UpsertProvider(context.Context, store.Provider) (*store.Provider, error) {
	if s.upsertErr != nil {
		return nil, s.upsertErr
	}
	if s.provider == nil {
		p := store.Provider{Slug: "stub", Enabled: true}
		s.provider = &p
	}
	p := *s.provider
	return &p, nil
}

func (s *socialRepoStub) DeleteProvider(context.Context, string) error { return s.deleteErr }

func (s *socialRepoStub) SaveState(_ context.Context, state store.AuthState) error {
	if s.saveStateErr != nil {
		return s.saveStateErr
	}
	s.state = state
	return nil
}

func (s *socialRepoStub) ConsumeState(context.Context, string) (store.AuthState, error) {
	if s.consumeErr != nil {
		return store.AuthState{}, s.consumeErr
	}
	if s.consumeState.ID != "" {
		return s.consumeState, nil
	}
	return s.state, nil
}

func (s *socialRepoStub) ResolveIdentity(context.Context, store.Provider, store.SocialProfile) (*store.IdentityLinkResult, error) {
	if s.resolveErr != nil {
		return nil, s.resolveErr
	}
	if s.linkResult != nil {
		return s.linkResult, nil
	}
	return &store.IdentityLinkResult{IdentityID: uuid.New(), Created: true, Linked: true}, nil
}

func TestSocialServiceAdditionalManagementAndLoadBranches(t *testing.T) {
	ctx := context.Background()
	boom := errors.New("boom")

	t.Run("list and get provider error branches", func(t *testing.T) {
		repo := &socialRepoStub{listErr: boom}
		svc := New(repo)
		if _, err := svc.ListProviders(ctx); !errors.Is(err, boom) {
			t.Fatalf("ListProviders(error) = %v", err)
		}
		if _, err := svc.ListConfiguredProviders(ctx, true); !errors.Is(err, boom) {
			t.Fatalf("ListConfiguredProviders(error) = %v", err)
		}

		repo = &socialRepoStub{getErr: boom}
		svc = New(repo)
		if _, err := svc.GetProvider(ctx, "google"); !errors.Is(err, boom) {
			t.Fatalf("GetProvider(error) = %v", err)
		}
	})

	t.Run("upsert and delete provider error branches", func(t *testing.T) {
		repo := &socialRepoStub{upsertErr: boom}
		svc := New(repo)
		if _, err := svc.UpsertProvider(ctx, ProviderUpsertRequest{Slug: "x"}); err == nil {
			t.Fatalf("UpsertProvider(build misconfig) expected error")
		}

		if _, err := svc.UpsertProvider(ctx, ProviderUpsertRequest{
			Slug:        "custom",
			DisplayName: "Custom",
			Protocol:    store.ProtocolOIDC,
			ClientID:    "cid",
			RedirectURI: "https://app.example.com/cb",
			Enabled:     true,
		}); !errors.Is(err, boom) {
			t.Fatalf("UpsertProvider(repo error) = %v", err)
		}

		repo = &socialRepoStub{deleteErr: boom}
		svc = New(repo)
		if err := svc.DeleteProvider(ctx, "custom"); !errors.Is(err, boom) {
			t.Fatalf("DeleteProvider(error) = %v", err)
		}
	})

	t.Run("ensure preset provider repo errors", func(t *testing.T) {
		repo := &socialRepoStub{getErr: boom}
		svc := New(repo)
		if err := svc.EnsurePresetProviders(ctx); !errors.Is(err, boom) {
			t.Fatalf("EnsurePresetProviders(get error) = %v", err)
		}

		repo = &socialRepoStub{
			getErr:    store.ErrProviderNotFound,
			upsertErr: boom,
		}
		svc = New(repo)
		if err := svc.EnsurePresetProviders(ctx); !errors.Is(err, boom) {
			t.Fatalf("EnsurePresetProviders(upsert error) = %v", err)
		}
	})

	t.Run("load provider unsupported/misconfig branches", func(t *testing.T) {
		repo := &socialRepoStub{}
		svc := New(repo)
		if _, _, err := svc.loadProvider(ctx, "missing"); !errors.Is(err, ErrProviderUnsupported) {
			t.Fatalf("loadProvider(missing) = %v", err)
		}

		repo = &socialRepoStub{provider: &store.Provider{Slug: "google", Enabled: false}}
		svc = New(repo)
		if _, _, err := svc.loadProvider(ctx, "google"); !errors.Is(err, ErrProviderUnsupported) {
			t.Fatalf("loadProvider(disabled) = %v", err)
		}

		repo = &socialRepoStub{provider: &store.Provider{
			Slug:       "google",
			Enabled:    true,
			Protocol:   store.ProtocolOIDC,
			ClientID:   "cid",
			RedirectURI:"https://app.example.com/cb",
		}}
		svc = New(repo)
		if _, _, err := svc.loadProvider(ctx, "google"); !errors.Is(err, ErrProviderMisconfig) {
			t.Fatalf("loadProvider(misconfig) = %v", err)
		}
	})
}

func TestSocialServiceAdditionalAuthBranches(t *testing.T) {
	ctx := context.Background()
	boom := errors.New("boom")

	t.Run("start auth save state and auth URL failures", func(t *testing.T) {
		repo := &socialRepoStub{provider: &store.Provider{
			Slug:              "google",
			Enabled:           true,
			Protocol:          store.ProtocolOAuth,
			AuthorizeEndpoint: "https://provider.example.com/authorize",
			TokenEndpoint:     "https://provider.example.com/token",
			UserInfoEndpoint:  "https://provider.example.com/userinfo",
			ClientID:          "cid",
			RedirectURI:       "https://app.example.com/cb",
			PKCEMethod:        store.PKCES256,
			AuthStyle:         store.AuthStyleClientSecretPost,
			ClaimSource:       store.ClaimSourceUserInfo,
			Scopes:            []string{"openid"},
		}}
		svc := New(repo)
		repo.saveStateErr = boom
		if _, err := svc.StartAuth(ctx, "google", "/after"); !errors.Is(err, boom) {
			t.Fatalf("StartAuth(save state error) = %v", err)
		}

		repo.saveStateErr = nil
		repo.provider.AuthorizeEndpoint = "://bad"
		if _, err := svc.StartAuth(ctx, "google", "/after"); err == nil {
			t.Fatalf("StartAuth(invalid authorize endpoint) expected error")
		}
	})

	t.Run("complete auth invalid and state branches", func(t *testing.T) {
		repo := &socialRepoStub{}
		svc := New(repo)
		if _, err := svc.CompleteAuth(ctx, " ", "state", "code"); !errors.Is(err, ErrInvalidCallback) {
			t.Fatalf("CompleteAuth(invalid input) = %v", err)
		}

		repo.consumeErr = store.ErrStateExpired
		if _, err := svc.CompleteAuth(ctx, "google", "state", "code"); !errors.Is(err, ErrInvalidState) {
			t.Fatalf("CompleteAuth(expired state) = %v", err)
		}
		repo.consumeErr = boom
		if _, err := svc.CompleteAuth(ctx, "google", "state", "code"); !errors.Is(err, boom) {
			t.Fatalf("CompleteAuth(consume error) = %v", err)
		}

		repo.consumeErr = nil
		repo.consumeState = store.AuthState{ID: "state", ProviderSlug: "other", RedirectTo: "/after", Nonce: "n"}
		if _, err := svc.CompleteAuth(ctx, "google", "state", "code"); !errors.Is(err, ErrInvalidState) {
			t.Fatalf("CompleteAuth(provider mismatch) = %v", err)
		}
	})

	t.Run("complete auth exchange, profile, and resolve branches", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/token":
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"access_token":"at-1"}`))
			case "/userinfo":
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"sub":"u1","email":"user@example.com","email_verified":true,"name":"User"}`))
			default:
				http.NotFound(w, r)
			}
		}))
		defer server.Close()

		baseProvider := &store.Provider{
			Slug:             "google",
			Enabled:          true,
			Protocol:         store.ProtocolOAuth,
			AuthorizeEndpoint: server.URL + "/authorize",
			TokenEndpoint:    server.URL + "/token",
			UserInfoEndpoint: server.URL + "/userinfo",
			ClientID:         "cid",
			ClientSecret:     "secret",
			RedirectURI:      "https://app.example.com/cb",
			PKCEMethod:       store.PKCES256,
			AuthStyle:        store.AuthStyleClientSecretPost,
			ClaimSource:      store.ClaimSourceUserInfo,
			ClaimMapping: store.ClaimMapping{
				Subject:       "sub",
				Email:         "email",
				EmailVerified: "email_verified",
				Name:          "name",
			},
			Scopes: []string{"openid", "email"},
		}

		repo := &socialRepoStub{
			provider:     baseProvider,
			consumeState: store.AuthState{ID: "state", ProviderSlug: "google", RedirectTo: "/after", Nonce: "n", PKCEVerifier: "verifier"},
		}
		svc := New(repo).WithHTTPClient(server.Client())

		repo.getErr = boom
		if _, err := svc.CompleteAuth(ctx, "google", "state", "code"); !errors.Is(err, boom) {
			t.Fatalf("CompleteAuth(load provider error) = %v", err)
		}
		repo.getErr = nil

		repo.provider.TokenEndpoint = server.URL + "/missing"
		if _, err := svc.CompleteAuth(ctx, "google", "state", "code"); err == nil {
			t.Fatalf("CompleteAuth(exchange code error) expected error")
		}

		repo.provider.TokenEndpoint = server.URL + "/token"
		repo.provider.ClaimSource = store.ClaimSourceIDToken
		if _, err := svc.CompleteAuth(ctx, "google", "state", "code"); !errors.Is(err, ErrInvalidCallback) {
			t.Fatalf("CompleteAuth(profile error) = %v", err)
		}

		repo.provider.ClaimSource = store.ClaimSourceUserInfo
		repo.resolveErr = boom
		if _, err := svc.CompleteAuth(ctx, "google", "state", "code"); !errors.Is(err, boom) {
			t.Fatalf("CompleteAuth(resolve identity error) = %v", err)
		}
	})
}

func TestSocialServiceAdditionalMergeAndAuthURLBranches(t *testing.T) {
	base := store.Provider{
		Slug:              "base",
		DisplayName:       "Base",
		Protocol:          store.ProtocolOIDC,
		Issuer:            "iss",
		DiscoveryURL:      "disc",
		AuthorizeEndpoint: "auth",
		TokenEndpoint:     "token",
		UserInfoEndpoint:  "userinfo",
		JWKSURI:           "jwks",
		Scopes:            []string{"openid"},
		ClaimMapping:      store.ClaimMapping{Subject: "sub"},
		ExtraAuthParams:   map[string]string{"prompt": "consent"},
		PKCEMethod:        store.PKCES256,
		AuthStyle:         store.AuthStyleClientSecretPost,
		ClaimSource:       store.ClaimSourceUserInfo,
		Enabled:           true,
		TrustEmailVerified:true,
		RedirectURI:       "https://app.example.com/cb",
		ClientID:          "cid",
		ClientSecret:      "secret",
	}
	override := store.Provider{
		Slug:              "override",
		DisplayName:       "Override",
		Protocol:          store.ProtocolOAuth,
		Issuer:            "iss2",
		DiscoveryURL:      "disc2",
		AuthorizeEndpoint: "auth2",
		TokenEndpoint:     "token2",
		UserInfoEndpoint:  "userinfo2",
		JWKSURI:           "jwks2",
		Scopes:            []string{"email"},
		ClaimMapping:      store.ClaimMapping{Subject: "subject", Email: "email", EmailVerified: "verified", Name: "name", Picture: "picture"},
		ExtraAuthParams:   map[string]string{"hd": "example.com"},
		PKCEMethod:        store.PKCEPlain,
		AuthStyle:         store.AuthStyleClientSecretBasic,
		ClaimSource:       store.ClaimSourceIDToken,
		Enabled:           true,
		TrustEmailVerified:true,
		RedirectURI:       "https://app.example.com/cb2",
		ClientID:          "cid2",
		ClientSecret:      "secret2",
	}
	merged := mergePreset(base, override)
	if merged.Slug != "override" || merged.DisplayName != "Override" || merged.Protocol != store.ProtocolOAuth ||
		merged.Issuer != "iss2" || merged.DiscoveryURL != "disc2" || merged.AuthorizeEndpoint != "auth2" ||
		merged.TokenEndpoint != "token2" || merged.UserInfoEndpoint != "userinfo2" || merged.JWKSURI != "jwks2" ||
		merged.PKCEMethod != store.PKCEPlain || merged.AuthStyle != store.AuthStyleClientSecretBasic ||
		merged.ClaimSource != store.ClaimSourceIDToken || merged.RedirectURI != "https://app.example.com/cb2" ||
		merged.ClientID != "cid2" || merged.ClientSecret != "secret2" || merged.ExtraAuthParams["hd"] != "example.com" {
		t.Fatalf("mergePreset() did not apply overrides as expected: %#v", merged)
	}

	value, err := authorizationURL(store.Provider{
		ClientID:   "cid",
		RedirectURI:"https://app.example.com/cb",
		Protocol:   store.ProtocolOIDC,
		PKCEMethod: store.PKCES256,
		ExtraAuthParams: map[string]string{
			"prompt": "login",
			" ":      "x",
			"empty":  " ",
		},
	}, resolvedProvider{
		AuthorizeEndpoint: "https://provider.example.com/authorize",
		Scopes:            []string{"openid"},
	}, "state", "nonce", "challenge")
	if err != nil {
		t.Fatalf("authorizationURL(extra params) error = %v", err)
	}
	if !strings.Contains(value, "prompt=login") || strings.Contains(value, "empty=") {
		t.Fatalf("authorizationURL(extra params) = %q", value)
	}
}

