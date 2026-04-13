package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/aegion/aegion/modules/social/store"
)

var (
	ErrProviderUnsupported = errors.New("provider is not configured")
	ErrInvalidState        = errors.New("invalid social state")
	ErrInvalidCallback     = errors.New("invalid callback payload")
)

type ProviderConfig struct {
	ClientID     string
	ClientSecret string
	RedirectURI  string
}

type Config struct {
	Google   ProviderConfig
	GitHub   ProviderConfig
	StateTTL time.Duration
}

type OAuthEndpoints struct {
	GoogleAuthorize string
	GoogleToken     string
	GoogleUserInfo  string
	GitHubAuthorize string
	GitHubToken     string
	GitHubUser      string
}

type StateStore interface {
	SaveState(state store.AuthState)
	ConsumeState(stateID string) (store.AuthState, error)
	UpsertProfile(profile store.SocialProfile)
}

type StartAuthResponse struct {
	Provider string `json:"provider"`
	AuthURL  string `json:"auth_url"`
	State    string `json:"state"`
}

type CallbackResult struct {
	Provider   string              `json:"provider"`
	RedirectTo string              `json:"redirect_to,omitempty"`
	Profile    store.SocialProfile `json:"profile"`
}

// Service contains social login business logic.
type Service struct {
	store     StateStore
	cfg       Config
	endpoints OAuthEndpoints
	client    *http.Client
}

// New creates a new social service.
func New(stateStore StateStore, cfg Config) *Service {
	if cfg.StateTTL <= 0 {
		cfg.StateTTL = 10 * time.Minute
	}
	return &Service{
		store: stateStore,
		cfg:   cfg,
		endpoints: OAuthEndpoints{
			GoogleAuthorize: "https://accounts.google.com/o/oauth2/v2/auth",
			GoogleToken:     "https://oauth2.googleapis.com/token",
			GoogleUserInfo:  "https://openidconnect.googleapis.com/v1/userinfo",
			GitHubAuthorize: "https://github.com/login/oauth/authorize",
			GitHubToken:     "https://github.com/login/oauth/access_token",
			GitHubUser:      "https://api.github.com/user",
		},
		client: &http.Client{Timeout: 10 * time.Second},
	}
}

// WithHTTPClient overrides outbound HTTP client (tests).
func (s *Service) WithHTTPClient(client *http.Client) *Service {
	if client != nil {
		s.client = client
	}
	return s
}

// WithEndpoints overrides provider endpoints (tests).
func (s *Service) WithEndpoints(endpoints OAuthEndpoints) *Service {
	s.endpoints = endpoints
	return s
}

func (s *Service) ListProviders() []string {
	providers := make([]string, 0, 2)
	if isProviderEnabled(s.cfg.Google) {
		providers = append(providers, "google")
	}
	if isProviderEnabled(s.cfg.GitHub) {
		providers = append(providers, "github")
	}
	return providers
}

func (s *Service) StartAuth(ctx context.Context, provider, redirectTo string) (*StartAuthResponse, error) {
	provider = strings.ToLower(strings.TrimSpace(provider))
	cfg, err := s.providerConfig(provider)
	if err != nil {
		return nil, err
	}

	stateID, err := randomToken(32)
	if err != nil {
		return nil, err
	}
	state := store.AuthState{
		ID:         stateID,
		Provider:   provider,
		RedirectTo: normalizeRedirectTarget(redirectTo),
		ExpiresAt:  time.Now().UTC().Add(s.cfg.StateTTL),
	}
	s.store.SaveState(state)

	authURL, err := s.authorizationURL(provider, cfg, stateID)
	if err != nil {
		return nil, err
	}
	return &StartAuthResponse{
		Provider: provider,
		AuthURL:  authURL,
		State:    stateID,
	}, nil
}

func (s *Service) CompleteAuth(ctx context.Context, provider, stateID, code string) (*CallbackResult, error) {
	provider = strings.ToLower(strings.TrimSpace(provider))
	code = strings.TrimSpace(code)
	if code == "" || stateID == "" {
		return nil, ErrInvalidCallback
	}

	cfg, err := s.providerConfig(provider)
	if err != nil {
		return nil, err
	}
	state, err := s.store.ConsumeState(stateID)
	if err != nil || state.Provider != provider {
		return nil, ErrInvalidState
	}

	accessToken, err := s.exchangeCode(ctx, provider, cfg, code)
	if err != nil {
		return nil, err
	}
	profile, err := s.fetchProfile(ctx, provider, accessToken)
	if err != nil {
		return nil, err
	}
	profile.Provider = provider
	s.store.UpsertProfile(profile)

	return &CallbackResult{
		Provider:   provider,
		RedirectTo: state.RedirectTo,
		Profile:    profile,
	}, nil
}

func (s *Service) providerConfig(provider string) (ProviderConfig, error) {
	switch provider {
	case "google":
		if !isProviderEnabled(s.cfg.Google) {
			return ProviderConfig{}, ErrProviderUnsupported
		}
		return s.cfg.Google, nil
	case "github":
		if !isProviderEnabled(s.cfg.GitHub) {
			return ProviderConfig{}, ErrProviderUnsupported
		}
		return s.cfg.GitHub, nil
	default:
		return ProviderConfig{}, ErrProviderUnsupported
	}
}

func isProviderEnabled(cfg ProviderConfig) bool {
	return strings.TrimSpace(cfg.ClientID) != "" &&
		strings.TrimSpace(cfg.ClientSecret) != "" &&
		strings.TrimSpace(cfg.RedirectURI) != ""
}

func normalizeRedirectTarget(target string) string {
	target = strings.TrimSpace(target)
	if target == "" {
		return "/"
	}
	return target
}

func (s *Service) authorizationURL(provider string, cfg ProviderConfig, state string) (string, error) {
	base := s.endpoints.GoogleAuthorize
	scope := "openid email profile"
	if provider == "github" {
		base = s.endpoints.GitHubAuthorize
		scope = "read:user user:email"
	}

	u, err := url.Parse(base)
	if err != nil {
		return "", err
	}
	query := u.Query()
	query.Set("response_type", "code")
	query.Set("client_id", cfg.ClientID)
	query.Set("redirect_uri", cfg.RedirectURI)
	query.Set("scope", scope)
	query.Set("state", state)
	u.RawQuery = query.Encode()
	return u.String(), nil
}

func (s *Service) exchangeCode(ctx context.Context, provider string, cfg ProviderConfig, code string) (string, error) {
	endpoint := s.endpoints.GoogleToken
	if provider == "github" {
		endpoint = s.endpoints.GitHubToken
	}
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("redirect_uri", cfg.RedirectURI)
	form.Set("client_id", cfg.ClientID)
	form.Set("client_secret", cfg.ClientSecret)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("token exchange failed with status %d", resp.StatusCode)
	}

	var payload struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return "", err
	}
	if strings.TrimSpace(payload.AccessToken) == "" {
		return "", errors.New("token exchange returned empty access token")
	}
	return payload.AccessToken, nil
}

func (s *Service) fetchProfile(ctx context.Context, provider, accessToken string) (store.SocialProfile, error) {
	endpoint := s.endpoints.GoogleUserInfo
	if provider == "github" {
		endpoint = s.endpoints.GitHubUser
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return store.SocialProfile{}, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "aegion-social")

	resp, err := s.client.Do(req)
	if err != nil {
		return store.SocialProfile{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return store.SocialProfile{}, fmt.Errorf("profile fetch failed with status %d", resp.StatusCode)
	}

	if provider == "github" {
		var payload struct {
			ID        int64  `json:"id"`
			Email     string `json:"email"`
			Name      string `json:"name"`
			AvatarURL string `json:"avatar_url"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
			return store.SocialProfile{}, err
		}
		return store.SocialProfile{
			ProviderUser:  fmt.Sprintf("%d", payload.ID),
			Email:         payload.Email,
			EmailVerified: payload.Email != "",
			Name:          payload.Name,
			PictureURL:    payload.AvatarURL,
		}, nil
	}

	var payload struct {
		Sub           string `json:"sub"`
		Email         string `json:"email"`
		EmailVerified bool   `json:"email_verified"`
		Name          string `json:"name"`
		Picture       string `json:"picture"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return store.SocialProfile{}, err
	}
	return store.SocialProfile{
		ProviderUser:  payload.Sub,
		Email:         payload.Email,
		EmailVerified: payload.EmailVerified,
		Name:          payload.Name,
		PictureURL:    payload.Picture,
	}, nil
}

func randomToken(bytesLen int) (string, error) {
	buf := make([]byte, bytesLen)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}
