package service

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	platformcrypto "github.com/aegion/aegion/internal/platform/crypto"
	"github.com/aegion/aegion/modules/social/providers/catalog"
	"github.com/aegion/aegion/modules/social/store"
)

type Service struct {
	repo   store.Repository
	client *http.Client
	now    func() time.Time

	discoveryMu sync.Mutex
	discovery   map[string]discoveryDocument
}

func New(repo store.Repository) *Service {
	return &Service{
		repo:      repo,
		client:    &http.Client{Timeout: 10 * time.Second},
		now:       func() time.Time { return time.Now().UTC() },
		discovery: make(map[string]discoveryDocument),
	}
}

func (s *Service) WithHTTPClient(client *http.Client) *Service {
	if client != nil {
		s.client = client
	}
	return s
}

func (s *Service) ListProviders(ctx context.Context) ([]store.Provider, error) {
	providers, err := s.repo.ListProviders(ctx, false)
	if err != nil {
		return nil, err
	}
	out := make([]store.Provider, 0, len(providers))
	for _, provider := range providers {
		out = append(out, provider.Sanitized())
	}
	return out, nil
}

func (s *Service) ListConfiguredProviders(ctx context.Context, includeDisabled bool) ([]store.Provider, error) {
	providers, err := s.repo.ListProviders(ctx, includeDisabled)
	if err != nil {
		return nil, err
	}
	out := make([]store.Provider, 0, len(providers))
	for _, provider := range providers {
		out = append(out, provider.Sanitized())
	}
	return out, nil
}

func (s *Service) GetProvider(ctx context.Context, slug string) (*store.Provider, error) {
	provider, err := s.repo.GetProviderBySlug(ctx, slug)
	if err != nil {
		return nil, err
	}
	safe := provider.Sanitized()
	return &safe, nil
}

func (s *Service) UpsertProvider(ctx context.Context, req ProviderUpsertRequest) (*store.Provider, error) {
	provider, err := s.buildProvider(req)
	if err != nil {
		return nil, err
	}
	saved, err := s.repo.UpsertProvider(ctx, provider)
	if err != nil {
		return nil, err
	}
	safe := saved.Sanitized()
	return &safe, nil
}

func (s *Service) DeleteProvider(ctx context.Context, slug string) error {
	return s.repo.DeleteProvider(ctx, slug)
}

func (s *Service) EnsurePresetProviders(ctx context.Context) error {
	for _, preset := range catalog.All() {
		existing, err := s.repo.GetProviderBySlug(ctx, preset.Slug)
		switch {
		case err == nil && existing != nil:
			continue
		case err != nil && err != store.ErrProviderNotFound:
			return err
		}
		if _, err := s.repo.UpsertProvider(ctx, preset); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) StartAuth(ctx context.Context, providerSlug, redirectTo string) (*StartAuthResponse, error) {
	provider, resolved, err := s.loadProvider(ctx, providerSlug)
	if err != nil {
		return nil, err
	}

	stateID, err := randomHexToken(32)
	if err != nil {
		return nil, err
	}
	nonce, err := randomHexToken(16)
	if err != nil {
		return nil, err
	}

	pkceVerifier := ""
	pkceChallenge := ""
	if provider.PKCEMethod != store.PKCENone {
		pkceVerifier, err = randomPKCEVerifier()
		if err != nil {
			return nil, err
		}
		pkceChallenge, err = platformcrypto.PKCEChallenge(pkceVerifier, string(provider.PKCEMethod))
		if err != nil {
			return nil, err
		}
	}

	if err := s.repo.SaveState(ctx, store.AuthState{
		ID:           stateID,
		ProviderSlug: provider.Slug,
		RedirectTo:   normalizeRedirectTarget(redirectTo),
		Nonce:        nonce,
		PKCEVerifier: pkceVerifier,
		ExpiresAt:    s.now().Add(10 * time.Minute),
	}); err != nil {
		return nil, err
	}

	authURL, err := authorizationURL(provider, resolved, stateID, nonce, pkceChallenge)
	if err != nil {
		return nil, err
	}

	return &StartAuthResponse{
		Provider: provider.Slug,
		AuthURL:  authURL,
		State:    stateID,
	}, nil
}

func (s *Service) CompleteAuth(ctx context.Context, providerSlug, stateID, code string) (*CallbackResult, error) {
	providerSlug = strings.ToLower(strings.TrimSpace(providerSlug))
	stateID = strings.TrimSpace(stateID)
	code = strings.TrimSpace(code)
	if providerSlug == "" || stateID == "" || code == "" {
		return nil, ErrInvalidCallback
	}

	state, err := s.repo.ConsumeState(ctx, stateID)
	if err != nil {
		if errors.Is(err, store.ErrStateExpired) || errors.Is(err, store.ErrStateNotFound) {
			return nil, ErrInvalidState
		}
		return nil, err
	}
	if state.ProviderSlug != providerSlug {
		return nil, ErrInvalidState
	}

	provider, resolved, err := s.loadProvider(ctx, providerSlug)
	if err != nil {
		return nil, err
	}

	tokenResponse, err := s.exchangeCode(ctx, provider, resolved, state, code)
	if err != nil {
		return nil, err
	}
	profile, err := s.fetchProfile(ctx, provider, resolved, state, tokenResponse)
	if err != nil {
		return nil, err
	}
	link, err := s.repo.ResolveIdentity(ctx, provider, profile)
	if err != nil {
		return nil, err
	}

	return &CallbackResult{
		Provider:        provider.Slug,
		RedirectTo:      state.RedirectTo,
		IdentityID:      link.IdentityID.String(),
		CreatedIdentity: link.Created,
		Linked:          link.Linked,
		Profile:         profile,
	}, nil
}

func (s *Service) buildProvider(req ProviderUpsertRequest) (store.Provider, error) {
	provider := store.Provider{
		Slug:               strings.ToLower(strings.TrimSpace(req.Slug)),
		DisplayName:        strings.TrimSpace(req.DisplayName),
		Preset:             strings.ToLower(strings.TrimSpace(req.Preset)),
		Protocol:           req.Protocol,
		Issuer:             strings.TrimSpace(req.Issuer),
		DiscoveryURL:       strings.TrimSpace(req.DiscoveryURL),
		AuthorizeEndpoint:  strings.TrimSpace(req.AuthorizeEndpoint),
		TokenEndpoint:      strings.TrimSpace(req.TokenEndpoint),
		UserInfoEndpoint:   strings.TrimSpace(req.UserInfoEndpoint),
		JWKSURI:            strings.TrimSpace(req.JWKSURI),
		Scopes:             append([]string(nil), req.Scopes...),
		ClaimMapping:       req.ClaimMapping,
		ExtraAuthParams:    copyMap(req.ExtraAuthParams),
		PKCEMethod:         req.PKCEMethod,
		AuthStyle:          req.AuthStyle,
		ClaimSource:        req.ClaimSource,
		Enabled:            req.Enabled,
		TrustEmailVerified: req.TrustEmailVerified,
		RedirectURI:        strings.TrimSpace(req.RedirectURI),
		ClientID:           strings.TrimSpace(req.ClientID),
		ClientSecret:       strings.TrimSpace(req.ClientSecret),
	}

	if provider.Preset != "" && provider.Preset != "custom" {
		preset, err := catalog.Lookup(provider.Preset)
		if err != nil {
			return store.Provider{}, err
		}
		provider = mergePreset(preset, provider)
	}
	if provider.Slug == "" {
		return store.Provider{}, ErrProviderMisconfig
	}
	if provider.DisplayName == "" {
		provider.DisplayName = strings.Title(provider.Slug)
	}
	if provider.Protocol == "" {
		provider.Protocol = store.ProtocolOIDC
	}
	if provider.PKCEMethod == "" {
		provider.PKCEMethod = store.PKCES256
	}
	if provider.AuthStyle == "" {
		provider.AuthStyle = store.AuthStyleClientSecretPost
	}
	if provider.ClaimSource == "" {
		provider.ClaimSource = store.ClaimSourceUserInfo
	}
	if len(provider.Scopes) == 0 {
		provider.Scopes = []string{"openid", "email", "profile"}
	}
	if provider.Enabled && (provider.ClientID == "" || provider.RedirectURI == "") {
		return store.Provider{}, ErrProviderMisconfig
	}
	return provider, nil
}

func (s *Service) loadProvider(ctx context.Context, slug string) (store.Provider, resolvedProvider, error) {
	provider, err := s.repo.GetProviderBySlug(ctx, slug)
	if err != nil {
		if errors.Is(err, store.ErrProviderNotFound) {
			return store.Provider{}, resolvedProvider{}, ErrProviderUnsupported
		}
		return store.Provider{}, resolvedProvider{}, err
	}
	if !provider.Enabled {
		return store.Provider{}, resolvedProvider{}, ErrProviderUnsupported
	}
	resolved, err := s.resolveProvider(ctx, *provider)
	if err != nil {
		return store.Provider{}, resolvedProvider{}, err
	}
	return *provider, resolved, nil
}

func mergePreset(preset, override store.Provider) store.Provider {
	merged := preset
	if override.Slug != "" {
		merged.Slug = override.Slug
	}
	if override.DisplayName != "" {
		merged.DisplayName = override.DisplayName
	}
	if override.Protocol != "" {
		merged.Protocol = override.Protocol
	}
	if override.Issuer != "" {
		merged.Issuer = override.Issuer
	}
	if override.DiscoveryURL != "" {
		merged.DiscoveryURL = override.DiscoveryURL
	}
	if override.AuthorizeEndpoint != "" {
		merged.AuthorizeEndpoint = override.AuthorizeEndpoint
	}
	if override.TokenEndpoint != "" {
		merged.TokenEndpoint = override.TokenEndpoint
	}
	if override.UserInfoEndpoint != "" {
		merged.UserInfoEndpoint = override.UserInfoEndpoint
	}
	if override.JWKSURI != "" {
		merged.JWKSURI = override.JWKSURI
	}
	if len(override.Scopes) > 0 {
		merged.Scopes = override.Scopes
	}
	merged.ClaimMapping = mergeClaimMapping(preset.ClaimMapping, override.ClaimMapping)
	merged.ExtraAuthParams = copyMap(preset.ExtraAuthParams)
	for key, value := range override.ExtraAuthParams {
		merged.ExtraAuthParams[key] = value
	}
	if override.PKCEMethod != "" {
		merged.PKCEMethod = override.PKCEMethod
	}
	if override.AuthStyle != "" {
		merged.AuthStyle = override.AuthStyle
	}
	if override.ClaimSource != "" {
		merged.ClaimSource = override.ClaimSource
	}
	merged.Enabled = preset.Enabled || override.Enabled
	merged.TrustEmailVerified = preset.TrustEmailVerified || override.TrustEmailVerified
	if override.RedirectURI != "" {
		merged.RedirectURI = override.RedirectURI
	}
	if override.ClientID != "" {
		merged.ClientID = override.ClientID
	}
	if override.ClientSecret != "" {
		merged.ClientSecret = override.ClientSecret
	}
	return merged
}

func mergeClaimMapping(base, override store.ClaimMapping) store.ClaimMapping {
	merged := base
	if override.Subject != "" {
		merged.Subject = override.Subject
	}
	if override.Email != "" {
		merged.Email = override.Email
	}
	if override.EmailVerified != "" {
		merged.EmailVerified = override.EmailVerified
	}
	if override.Name != "" {
		merged.Name = override.Name
	}
	if override.Picture != "" {
		merged.Picture = override.Picture
	}
	return merged
}

func normalizeRedirectTarget(target string) string {
	target = strings.TrimSpace(target)
	if target == "" {
		return "/"
	}
	return target
}

func copyMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return map[string]string{}
	}
	out := make(map[string]string, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func authorizationURL(provider store.Provider, resolved resolvedProvider, state, nonce, pkceChallenge string) (string, error) {
	base, err := url.Parse(resolved.AuthorizeEndpoint)
	if err != nil {
		return "", err
	}
	query := base.Query()
	query.Set("response_type", "code")
	query.Set("client_id", provider.ClientID)
	query.Set("redirect_uri", provider.RedirectURI)
	query.Set("scope", strings.Join(resolved.Scopes, " "))
	query.Set("state", state)
	if provider.Protocol == store.ProtocolOIDC || provider.ClaimSource == store.ClaimSourceIDToken {
		query.Set("nonce", nonce)
	}
	if provider.PKCEMethod != store.PKCENone && pkceChallenge != "" {
		query.Set("code_challenge", pkceChallenge)
		query.Set("code_challenge_method", string(provider.PKCEMethod))
	}
	for key, value := range provider.ExtraAuthParams {
		if strings.TrimSpace(key) == "" || strings.TrimSpace(value) == "" {
			continue
		}
		query.Set(strings.TrimSpace(key), strings.TrimSpace(value))
	}
	base.RawQuery = query.Encode()
	return base.String(), nil
}

func randomHexToken(bytesLen int) (string, error) {
	buf, err := platformcrypto.RandomBytes(bytesLen)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", buf), nil
}

func randomPKCEVerifier() (string, error) {
	buf, err := platformcrypto.RandomBytes(48)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}
