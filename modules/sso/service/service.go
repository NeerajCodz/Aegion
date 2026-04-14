package service

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	platformcrypto "github.com/aegion/aegion/internal/platform/crypto"
	"github.com/aegion/aegion/modules/sso/store"
)

var (
	ErrConnectionDisabled = errors.New("sso connection is disabled")
	ErrInvalidRelayState  = errors.New("invalid relay state")
	ErrMissingStateSecret = errors.New("sso state secret is required")
)

const relayStateKind = "sso_state"

type StartResponse struct {
	Connection  string `json:"connection"`
	RedirectURL string `json:"redirect_url"`
	RelayState  string `json:"relay_state"`
}

type CallbackResult struct {
	Connection   string                 `json:"connection"`
	Subject      string                 `json:"subject"`
	Email        string                 `json:"email,omitempty"`
	DisplayName  string                 `json:"display_name,omitempty"`
	RedirectTo   string                 `json:"redirect_to,omitempty"`
	JITProvision bool                   `json:"jit_provision"`
	Attributes   map[string]interface{} `json:"attributes,omitempty"`
}

type ConnectionUpsertRequest struct {
	Slug              string                 `json:"slug"`
	DisplayName       string                 `json:"display_name"`
	EntityID          string                 `json:"entity_id"`
	SSOURL            string                 `json:"sso_url"`
	CertificatePEM    string                 `json:"certificate_pem"`
	MetadataURL       string                 `json:"metadata_url"`
	Domains           []string               `json:"domains"`
	AttributeMapping  store.AttributeMapping `json:"attribute_mapping"`
	JITProvisioning   bool                   `json:"jit_provisioning"`
	DefaultRedirectTo string                 `json:"default_redirect_to"`
	ExtraAuthnContext map[string]string      `json:"extra_authn_context"`
	Enabled           bool                   `json:"enabled"`
}

type callbackState struct {
	Connection string `json:"connection"`
	RedirectTo string `json:"redirect_to"`
	IssuedAt   int64  `json:"issued_at"`
}

type Service struct {
	repo        store.Repository
	stateSecret []byte
	now         func() time.Time
}

func New(repo store.Repository, stateSecret []byte) *Service {
	if repo == nil {
		repo = store.New()
	}
	return &Service{
		repo:        repo,
		stateSecret: append([]byte(nil), stateSecret...),
		now: func() time.Time {
			return time.Now().UTC()
		},
	}
}

func (s *Service) ListConnections(ctx context.Context) ([]store.Connection, error) {
	return s.repo.ListConnections(ctx, false)
}

func (s *Service) ListConfiguredConnections(ctx context.Context, includeDisabled bool) ([]store.Connection, error) {
	return s.repo.ListConnections(ctx, includeDisabled)
}

func (s *Service) GetConnection(ctx context.Context, slug string) (*store.Connection, error) {
	return s.repo.GetConnectionBySlug(ctx, slug)
}

func (s *Service) GetConnectionForDomain(ctx context.Context, domain string) (*store.Connection, error) {
	return s.repo.GetConnectionByDomain(ctx, domain)
}

func (s *Service) UpsertConnection(ctx context.Context, req ConnectionUpsertRequest) (*store.Connection, error) {
	connection, err := normalizeConnection(req)
	if err != nil {
		return nil, err
	}
	return s.repo.UpsertConnection(ctx, connection)
}

func (s *Service) DeleteConnection(ctx context.Context, slug string) error {
	return s.repo.DeleteConnection(ctx, slug)
}

func (s *Service) StartAuth(ctx context.Context, slug, redirectTo string) (*StartResponse, error) {
	connection, err := s.repo.GetConnectionBySlug(ctx, slug)
	if err != nil {
		return nil, err
	}
	if !connection.Enabled {
		return nil, ErrConnectionDisabled
	}
	relayState, err := s.signRelayState(callbackState{
		Connection: connection.Slug,
		RedirectTo: normalizeRedirect(firstNonEmpty(redirectTo, connection.DefaultRedirectTo)),
		IssuedAt:   s.now().Unix(),
	})
	if err != nil {
		return nil, err
	}
	redirectURL, err := buildRedirectURL(*connection, relayState)
	if err != nil {
		return nil, err
	}
	return &StartResponse{
		Connection:  connection.Slug,
		RedirectURL: redirectURL,
		RelayState:  relayState,
	}, nil
}

func (s *Service) CompleteAuth(ctx context.Context, slug, relayState, subject, email, displayName string, attributes map[string]interface{}) (*CallbackResult, error) {
	connection, err := s.repo.GetConnectionBySlug(ctx, slug)
	if err != nil {
		return nil, err
	}
	if !connection.Enabled {
		return nil, ErrConnectionDisabled
	}
	state, err := s.verifyRelayState(relayState)
	if err != nil {
		return nil, err
	}
	if state.Connection != connection.Slug {
		return nil, ErrInvalidRelayState
	}
	resolvedAttributes := map[string]interface{}{}
	for key, value := range attributes {
		resolvedAttributes[key] = value
	}
	if strings.TrimSpace(subject) == "" && connection.AttributeMapping.Subject != "" {
		subject = stringValue(resolvedAttributes[connection.AttributeMapping.Subject])
	}
	if strings.TrimSpace(email) == "" && connection.AttributeMapping.Email != "" {
		email = stringValue(resolvedAttributes[connection.AttributeMapping.Email])
	}
	if strings.TrimSpace(displayName) == "" && connection.AttributeMapping.DisplayName != "" {
		displayName = stringValue(resolvedAttributes[connection.AttributeMapping.DisplayName])
	}
	if strings.TrimSpace(subject) == "" {
		return nil, ErrInvalidRelayState
	}
	return &CallbackResult{
		Connection:   connection.Slug,
		Subject:      strings.TrimSpace(subject),
		Email:        strings.ToLower(strings.TrimSpace(email)),
		DisplayName:  strings.TrimSpace(displayName),
		RedirectTo:   normalizeRedirect(firstNonEmpty(state.RedirectTo, connection.DefaultRedirectTo)),
		JITProvision: connection.JITProvisioning,
		Attributes:   resolvedAttributes,
	}, nil
}

func normalizeConnection(req ConnectionUpsertRequest) (store.Connection, error) {
	now := time.Now().UTC()
	connection := store.Connection{
		Slug:              strings.ToLower(strings.TrimSpace(req.Slug)),
		DisplayName:       strings.TrimSpace(req.DisplayName),
		EntityID:          strings.TrimSpace(req.EntityID),
		SSOURL:            strings.TrimSpace(req.SSOURL),
		CertificatePEM:    strings.TrimSpace(req.CertificatePEM),
		MetadataURL:       strings.TrimSpace(req.MetadataURL),
		Domains:           normalizeDomains(req.Domains),
		AttributeMapping:  req.AttributeMapping,
		JITProvisioning:   req.JITProvisioning,
		DefaultRedirectTo: normalizeRedirect(req.DefaultRedirectTo),
		ExtraAuthnContext: normalizeExtra(req.ExtraAuthnContext),
		Enabled:           req.Enabled,
		CreatedAt:         now,
		UpdatedAt:         now,
	}
	if connection.Slug == "" || connection.DisplayName == "" || connection.EntityID == "" || connection.SSOURL == "" {
		return store.Connection{}, store.ErrConnectionNotFound
	}
	parsed, err := url.Parse(connection.SSOURL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return store.Connection{}, store.ErrConnectionNotFound
	}
	if connection.AttributeMapping.Subject == "" {
		connection.AttributeMapping.Subject = "subject"
	}
	if connection.AttributeMapping.Email == "" {
		connection.AttributeMapping.Email = "email"
	}
	if connection.AttributeMapping.DisplayName == "" {
		connection.AttributeMapping.DisplayName = "display_name"
	}
	return connection, nil
}

func buildRedirectURL(connection store.Connection, relayState string) (string, error) {
	parsed, err := url.Parse(connection.SSOURL)
	if err != nil {
		return "", err
	}
	query := parsed.Query()
	query.Set("SAMLRequest", base64.RawURLEncoding.EncodeToString([]byte(connection.EntityID)))
	query.Set("RelayState", relayState)
	for key, value := range connection.ExtraAuthnContext {
		if strings.TrimSpace(key) == "" || strings.TrimSpace(value) == "" {
			continue
		}
		query.Set(key, value)
	}
	parsed.RawQuery = query.Encode()
	return parsed.String(), nil
}

func (s *Service) signRelayState(state callbackState) (string, error) {
	if len(s.stateSecret) == 0 {
		return "", ErrMissingStateSecret
	}
	payload, err := json.Marshal(state)
	if err != nil {
		return "", err
	}
	envelope, err := platformcrypto.SignEnvelope(relayStateKind, s.stateSecret, payload, s.now())
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(payload) + "." + envelope, nil
}

func (s *Service) verifyRelayState(value string) (*callbackState, error) {
	if len(s.stateSecret) == 0 {
		return nil, ErrMissingStateSecret
	}
	parts := strings.SplitN(strings.TrimSpace(value), ".", 2)
	if len(parts) != 2 {
		return nil, ErrInvalidRelayState
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, ErrInvalidRelayState
	}
	if !platformcrypto.VerifyEnvelope(relayStateKind, s.stateSecret, payload, parts[1], 15*time.Minute, s.now()) {
		return nil, ErrInvalidRelayState
	}
	var state callbackState
	if err := json.Unmarshal(payload, &state); err != nil {
		return nil, ErrInvalidRelayState
	}
	return &state, nil
}

func normalizeDomains(values []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(value, "@")))
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
	}
	return out
}

func normalizeExtra(in map[string]string) map[string]string {
	out := make(map[string]string)
	for key, value := range in {
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if key == "" || value == "" {
			continue
		}
		out[key] = value
	}
	return out
}

func normalizeRedirect(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "/"
	}
	return value
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func stringValue(value interface{}) string {
	switch v := value.(type) {
	case string:
		return strings.TrimSpace(v)
	case fmt.Stringer:
		return strings.TrimSpace(v.String())
	default:
		return ""
	}
}
