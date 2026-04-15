package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/aegion/aegion/modules/social/store"
)

type resolvedProvider struct {
	Issuer            string
	AuthorizeEndpoint string
	TokenEndpoint     string
	UserInfoEndpoint  string
	JWKSURI           string
	Scopes            []string
}

type discoveryDocument struct {
	Issuer                string   `json:"issuer"`
	AuthorizationEndpoint string   `json:"authorization_endpoint"`
	TokenEndpoint         string   `json:"token_endpoint"`
	UserInfoEndpoint      string   `json:"userinfo_endpoint"`
	JWKSURI               string   `json:"jwks_uri"`
	ScopesSupported       []string `json:"scopes_supported"`
}

type tokenPayload struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	IDToken     string `json:"id_token"`
}

func (s *Service) resolveProvider(ctx context.Context, provider store.Provider) (resolvedProvider, error) {
	resolved := resolvedProvider{
		Issuer:            strings.TrimSpace(provider.Issuer),
		AuthorizeEndpoint: strings.TrimSpace(provider.AuthorizeEndpoint),
		TokenEndpoint:     strings.TrimSpace(provider.TokenEndpoint),
		UserInfoEndpoint:  strings.TrimSpace(provider.UserInfoEndpoint),
		JWKSURI:           strings.TrimSpace(provider.JWKSURI),
		Scopes:            append([]string(nil), provider.Scopes...),
	}

	if provider.Protocol == store.ProtocolOIDC && strings.TrimSpace(provider.DiscoveryURL) != "" {
		doc, err := s.fetchDiscovery(ctx, provider.DiscoveryURL)
		if err != nil {
			return resolvedProvider{}, err
		}
		if resolved.Issuer == "" {
			resolved.Issuer = doc.Issuer
		}
		if resolved.AuthorizeEndpoint == "" {
			resolved.AuthorizeEndpoint = doc.AuthorizationEndpoint
		}
		if resolved.TokenEndpoint == "" {
			resolved.TokenEndpoint = doc.TokenEndpoint
		}
		if resolved.UserInfoEndpoint == "" {
			resolved.UserInfoEndpoint = doc.UserInfoEndpoint
		}
		if resolved.JWKSURI == "" {
			resolved.JWKSURI = doc.JWKSURI
		}
		if len(resolved.Scopes) == 0 {
			resolved.Scopes = doc.ScopesSupported
		}
	}

	if strings.TrimSpace(resolved.AuthorizeEndpoint) == "" || strings.TrimSpace(resolved.TokenEndpoint) == "" {
		return resolvedProvider{}, ErrProviderMisconfig
	}
	if len(resolved.Scopes) == 0 {
		resolved.Scopes = []string{"openid", "email", "profile"}
	}
	return resolved, nil
}

func (s *Service) fetchDiscovery(ctx context.Context, discoveryURL string) (discoveryDocument, error) {
	discoveryURL = strings.TrimSpace(discoveryURL)
	s.discoveryMu.Lock()
	if doc, ok := s.discovery[discoveryURL]; ok {
		s.discoveryMu.Unlock()
		return doc, nil
	}
	s.discoveryMu.Unlock()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, discoveryURL, nil)
	if err != nil {
		return discoveryDocument{}, err
	}
	req.Header.Set("Accept", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		return discoveryDocument{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return discoveryDocument{}, fmt.Errorf("discovery failed with status %d", resp.StatusCode)
	}

	var doc discoveryDocument
	if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
		return discoveryDocument{}, err
	}

	s.discoveryMu.Lock()
	s.discovery[discoveryURL] = doc
	s.discoveryMu.Unlock()
	return doc, nil
}

func (s *Service) exchangeCode(ctx context.Context, provider store.Provider, resolved resolvedProvider, state store.AuthState, code string) (*tokenPayload, error) {
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("redirect_uri", provider.RedirectURI)
	form.Set("client_id", provider.ClientID)
	if provider.AuthStyle == store.AuthStyleClientSecretPost && provider.ClientSecret != "" {
		form.Set("client_secret", provider.ClientSecret)
	}
	if provider.PKCEMethod != store.PKCENone && state.PKCEVerifier != "" {
		form.Set("code_verifier", state.PKCEVerifier)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, resolved.TokenEndpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	if provider.AuthStyle == store.AuthStyleClientSecretBasic && provider.ClientSecret != "" {
		req.SetBasicAuth(provider.ClientID, provider.ClientSecret)
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("token exchange failed with status %d", resp.StatusCode)
	}

	var payload tokenPayload
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, err
	}
	if strings.TrimSpace(payload.AccessToken) == "" && strings.TrimSpace(payload.IDToken) == "" {
		return nil, ErrInvalidCallback
	}
	return &payload, nil
}

func (s *Service) fetchProfile(ctx context.Context, provider store.Provider, resolved resolvedProvider, state store.AuthState, tokenResponse *tokenPayload) (store.SocialProfile, error) {
	switch provider.ClaimSource {
	case store.ClaimSourceIDToken:
		return s.profileFromIDToken(ctx, provider, resolved, state, tokenResponse.IDToken)
	case store.ClaimSourceGitHubUser:
		return s.profileFromGitHub(ctx, provider, resolved, tokenResponse.AccessToken)
	default:
		return s.profileFromUserInfo(ctx, provider, resolved, tokenResponse.AccessToken)
	}
}

func (s *Service) profileFromUserInfo(ctx context.Context, provider store.Provider, resolved resolvedProvider, accessToken string) (store.SocialProfile, error) {
	if strings.TrimSpace(resolved.UserInfoEndpoint) == "" || strings.TrimSpace(accessToken) == "" {
		return store.SocialProfile{}, ErrProviderMisconfig
	}

	claims, err := s.fetchJSONClaims(ctx, http.MethodGet, resolved.UserInfoEndpoint, accessToken)
	if err != nil {
		return store.SocialProfile{}, err
	}
	return claimsToProfile(provider, claims), nil
}

func (s *Service) profileFromGitHub(ctx context.Context, provider store.Provider, resolved resolvedProvider, accessToken string) (store.SocialProfile, error) {
	claims, err := s.fetchJSONClaims(ctx, http.MethodGet, resolved.UserInfoEndpoint, accessToken)
	if err != nil {
		return store.SocialProfile{}, err
	}

	if strings.TrimSpace(claimValue(claims, provider.ClaimMapping.Email)) == "" {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.github.com/user/emails", nil)
		if err == nil {
			req.Header.Set("Authorization", "Bearer "+accessToken)
			req.Header.Set("Accept", "application/json")
			req.Header.Set("User-Agent", "aegion-social")
			resp, err := s.client.Do(req)
			if err == nil {
				defer resp.Body.Close()
				if resp.StatusCode >= 200 && resp.StatusCode < 300 {
					var payload []map[string]interface{}
					if err := json.NewDecoder(resp.Body).Decode(&payload); err == nil {
						for _, item := range payload {
							verified, _ := item["verified"].(bool)
							primary, _ := item["primary"].(bool)
							email, _ := item["email"].(string)
							if verified && primary && email != "" {
								claims["email"] = email
								claims["email_verified"] = true
								break
							}
						}
						if strings.TrimSpace(claimValue(claims, provider.ClaimMapping.Email)) == "" {
							for _, item := range payload {
								verified, _ := item["verified"].(bool)
								email, _ := item["email"].(string)
								if verified && email != "" {
									claims["email"] = email
									claims["email_verified"] = true
									break
								}
							}
						}
					}
				}
			}
		}
	}

	return claimsToProfile(provider, claims), nil
}

func (s *Service) profileFromIDToken(ctx context.Context, provider store.Provider, resolved resolvedProvider, state store.AuthState, idToken string) (store.SocialProfile, error) {
	if strings.TrimSpace(idToken) == "" {
		return store.SocialProfile{}, ErrInvalidCallback
	}

	claims, err := s.verifyAndDecodeIDToken(ctx, idToken, provider.ClientID, resolved.Issuer, resolved.JWKSURI)
	if err != nil {
		return store.SocialProfile{}, err
	}
	if expected := strings.TrimSpace(state.Nonce); expected != "" {
		if strings.TrimSpace(claimValue(claims, "nonce")) != expected {
			return store.SocialProfile{}, errors.New("id token nonce mismatch")
		}
	}
	return claimsToProfile(provider, claims), nil
}

func (s *Service) fetchJSONClaims(ctx context.Context, method, endpoint, accessToken string) (map[string]interface{}, error) {
	req, err := http.NewRequestWithContext(ctx, method, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "aegion-social")
	if accessToken != "" {
		req.Header.Set("Authorization", "Bearer "+accessToken)
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("profile fetch failed with status %d", resp.StatusCode)
	}

	var claims map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&claims); err != nil {
		return nil, err
	}
	return claims, nil
}

func claimsToProfile(provider store.Provider, claims map[string]interface{}) store.SocialProfile {
	profile := store.SocialProfile{
		Provider:      provider.Slug,
		ProviderUser:  claimValue(claims, provider.ClaimMapping.Subject),
		Email:         strings.ToLower(claimValue(claims, provider.ClaimMapping.Email)),
		EmailVerified: claimBool(claims, provider.ClaimMapping.EmailVerified),
		Name:          claimValue(claims, provider.ClaimMapping.Name),
		PictureURL:    claimValue(claims, provider.ClaimMapping.Picture),
		RawClaims:     claims,
	}
	if profile.ProviderUser == "" {
		profile.ProviderUser = claimValue(claims, "sub|id")
	}
	if !profile.EmailVerified && strings.TrimSpace(profile.Email) != "" && provider.ClaimSource == store.ClaimSourceGitHubUser {
		profile.EmailVerified = true
	}
	return profile
}

func claimValue(claims map[string]interface{}, mapping string) string {
	for _, key := range strings.Split(mapping, "|") {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		value, ok := claims[key]
		if !ok || value == nil {
			continue
		}
		switch typed := value.(type) {
		case string:
			if strings.TrimSpace(typed) != "" {
				return strings.TrimSpace(typed)
			}
		case float64:
			return strings.TrimSpace(fmt.Sprintf("%.0f", typed))
		case json.Number:
			return typed.String()
		}
	}
	return ""
}

func claimBool(claims map[string]interface{}, mapping string) bool {
	for _, key := range strings.Split(mapping, "|") {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		value, ok := claims[key]
		if !ok || value == nil {
			continue
		}
		switch typed := value.(type) {
		case bool:
			return typed
		case string:
			switch strings.ToLower(strings.TrimSpace(typed)) {
			case "true", "1", "yes":
				return true
			}
		}
	}
	return false
}

func shortLeeway() time.Duration {
	return 30 * time.Second
}
