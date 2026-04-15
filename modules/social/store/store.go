package store

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
)

var (
	ErrStateNotFound    = errors.New("state not found")
	ErrStateExpired     = errors.New("state expired")
	ErrProviderNotFound = errors.New("provider not found")
)

type Protocol string

const (
	ProtocolOIDC  Protocol = "oidc"
	ProtocolOAuth Protocol = "oauth2"
)

type AuthStyle string

const (
	AuthStyleClientSecretPost  AuthStyle = "client_secret_post"
	AuthStyleClientSecretBasic AuthStyle = "client_secret_basic"
)

type PKCEMethod string

const (
	PKCENone  PKCEMethod = "none"
	PKCES256  PKCEMethod = "S256"
	PKCEPlain PKCEMethod = "plain"
)

type ClaimSource string

const (
	ClaimSourceUserInfo   ClaimSource = "userinfo"
	ClaimSourceIDToken    ClaimSource = "id_token"
	ClaimSourceGitHubUser ClaimSource = "github_user"
)

type ClaimMapping struct {
	Subject       string `json:"subject"`
	Email         string `json:"email"`
	EmailVerified string `json:"email_verified"`
	Name          string `json:"name"`
	Picture       string `json:"picture"`
}

type Provider struct {
	ID                 uuid.UUID         `json:"id"`
	Slug               string            `json:"slug"`
	DisplayName        string            `json:"display_name"`
	Preset             string            `json:"preset"`
	Protocol           Protocol          `json:"protocol"`
	Issuer             string            `json:"issuer,omitempty"`
	DiscoveryURL       string            `json:"discovery_url,omitempty"`
	AuthorizeEndpoint  string            `json:"authorize_endpoint,omitempty"`
	TokenEndpoint      string            `json:"token_endpoint,omitempty"`
	UserInfoEndpoint   string            `json:"userinfo_endpoint,omitempty"`
	JWKSURI            string            `json:"jwks_uri,omitempty"`
	Scopes             []string          `json:"scopes,omitempty"`
	ClaimMapping       ClaimMapping      `json:"claim_mapping"`
	ExtraAuthParams    map[string]string `json:"extra_auth_params,omitempty"`
	PKCEMethod         PKCEMethod        `json:"pkce_method"`
	AuthStyle          AuthStyle         `json:"auth_style"`
	ClaimSource        ClaimSource       `json:"claim_source"`
	Enabled            bool              `json:"enabled"`
	TrustEmailVerified bool              `json:"trust_email_verified"`
	RedirectURI        string            `json:"redirect_uri"`
	ClientID           string            `json:"client_id,omitempty"`
	ClientSecret       string            `json:"-"`
	CreatedAt          time.Time         `json:"created_at"`
	UpdatedAt          time.Time         `json:"updated_at"`
}

func (p Provider) Sanitized() Provider {
	p.ClientSecret = ""
	return p
}

type AuthState struct {
	ID           string
	ProviderSlug string
	RedirectTo   string
	Nonce        string
	PKCEVerifier string
	ExpiresAt    time.Time
}

type SocialProfile struct {
	Provider      string                 `json:"provider"`
	ProviderUser  string                 `json:"provider_user"`
	Email         string                 `json:"email,omitempty"`
	EmailVerified bool                   `json:"email_verified"`
	Name          string                 `json:"name,omitempty"`
	PictureURL    string                 `json:"picture_url,omitempty"`
	RawClaims     map[string]interface{} `json:"raw_claims,omitempty"`
}

type IdentityLinkResult struct {
	IdentityID uuid.UUID `json:"identity_id"`
	Created    bool      `json:"created"`
	Linked     bool      `json:"linked"`
}

type Repository interface {
	ListProviders(ctx context.Context, includeDisabled bool) ([]Provider, error)
	GetProviderBySlug(ctx context.Context, slug string) (*Provider, error)
	UpsertProvider(ctx context.Context, provider Provider) (*Provider, error)
	DeleteProvider(ctx context.Context, slug string) error
	SaveState(ctx context.Context, state AuthState) error
	ConsumeState(ctx context.Context, stateID string) (AuthState, error)
	ResolveIdentity(ctx context.Context, provider Provider, profile SocialProfile) (*IdentityLinkResult, error)
}
