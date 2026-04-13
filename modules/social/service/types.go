package service

import (
	"errors"

	"github.com/aegion/aegion/modules/social/store"
)

var (
	ErrProviderUnsupported = errors.New("provider is not configured")
	ErrInvalidState        = errors.New("invalid social state")
	ErrInvalidCallback     = errors.New("invalid callback payload")
	ErrProviderMisconfig   = errors.New("provider is misconfigured")
)

type StartAuthResponse struct {
	Provider string `json:"provider"`
	AuthURL  string `json:"auth_url"`
	State    string `json:"state"`
}

type CallbackResult struct {
	Provider        string              `json:"provider"`
	RedirectTo      string              `json:"redirect_to,omitempty"`
	IdentityID      string              `json:"identity_id,omitempty"`
	CreatedIdentity bool                `json:"created_identity"`
	Linked          bool                `json:"linked"`
	Profile         store.SocialProfile `json:"profile"`
}

type ProviderUpsertRequest struct {
	Slug               string             `json:"slug"`
	DisplayName        string             `json:"display_name"`
	Preset             string             `json:"preset"`
	Protocol           store.Protocol     `json:"protocol"`
	Issuer             string             `json:"issuer"`
	DiscoveryURL       string             `json:"discovery_url"`
	AuthorizeEndpoint  string             `json:"authorize_endpoint"`
	TokenEndpoint      string             `json:"token_endpoint"`
	UserInfoEndpoint   string             `json:"userinfo_endpoint"`
	JWKSURI            string             `json:"jwks_uri"`
	Scopes             []string           `json:"scopes"`
	ClaimMapping       store.ClaimMapping `json:"claim_mapping"`
	ExtraAuthParams    map[string]string  `json:"extra_auth_params"`
	PKCEMethod         store.PKCEMethod   `json:"pkce_method"`
	AuthStyle          store.AuthStyle    `json:"auth_style"`
	ClaimSource        store.ClaimSource  `json:"claim_source"`
	Enabled            bool               `json:"enabled"`
	TrustEmailVerified bool               `json:"trust_email_verified"`
	RedirectURI        string             `json:"redirect_uri"`
	ClientID           string             `json:"client_id"`
	ClientSecret       string             `json:"client_secret"`
}
