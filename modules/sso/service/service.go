package service

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	platformcrypto "github.com/aegion/aegion/internal/platform/crypto"
	"github.com/aegion/aegion/modules/sso/store"
)

var (
	ErrConnectionDisabled  = errors.New("sso connection is disabled")
	ErrInvalidRelayState   = errors.New("invalid relay state")
	ErrMissingStateSecret  = errors.New("sso state secret is required")
	ErrInvalidSAMLResponse = errors.New("invalid sso response")
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
	RequestID  string `json:"request_id"`
	IssuedAt   int64  `json:"issued_at"`
}

type Service struct {
	repo        store.Repository
	stateSecret []byte
	now         func() time.Time
	httpClient  *http.Client
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
		httpClient: &http.Client{Timeout: 10 * time.Second},
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
	connection, err := s.normalizeConnection(ctx, req)
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
	state := callbackState{
		Connection: connection.Slug,
		RedirectTo: normalizeRedirect(firstNonEmpty(redirectTo, connection.DefaultRedirectTo)),
		RequestID:  newSAMLRequestID(),
		IssuedAt:   s.now().Unix(),
	}
	relayState, err := s.signRelayState(state)
	if err != nil {
		return nil, err
	}
	redirectURL, err := buildRedirectURL(*connection, relayState, state.RequestID)
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
	if rawSAML := stringValue(resolvedAttributes["_saml_response"]); rawSAML != "" {
		// Reject raw SAML assertions until XML signature validation is implemented.
		// Parsing untrusted assertions enables authentication bypass.
		return nil, ErrInvalidSAMLResponse
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

func (s *Service) normalizeConnection(ctx context.Context, req ConnectionUpsertRequest) (store.Connection, error) {
	if metadataURL := strings.TrimSpace(req.MetadataURL); metadataURL != "" {
		metadata, err := s.fetchMetadata(ctx, metadataURL)
		if err != nil {
			return store.Connection{}, err
		}
		if strings.TrimSpace(req.EntityID) == "" {
			req.EntityID = metadata.EntityID
		}
		if strings.TrimSpace(req.SSOURL) == "" {
			req.SSOURL = metadata.SSOURL
		}
		if strings.TrimSpace(req.CertificatePEM) == "" {
			req.CertificatePEM = metadata.CertificatePEM
		}
	}
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

type metadataEntityDescriptor struct {
	XMLName          xml.Name `xml:"EntityDescriptor"`
	EntityID         string   `xml:"entityID,attr"`
	IDPSSODescriptor struct {
		SingleSignOnServices []struct {
			Binding  string `xml:"Binding,attr"`
			Location string `xml:"Location,attr"`
		} `xml:"SingleSignOnService"`
		KeyDescriptors []struct {
			KeyInfo struct {
				X509Data struct {
					Certificate string `xml:"X509Certificate"`
				} `xml:"X509Data"`
			} `xml:"KeyInfo"`
		} `xml:"KeyDescriptor"`
	} `xml:"IDPSSODescriptor"`
}

type fetchedMetadata struct {
	EntityID       string
	SSOURL         string
	CertificatePEM string
}

func (s *Service) fetchMetadata(ctx context.Context, metadataURL string) (*fetchedMetadata, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, metadataURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, ErrConnectionDisabled
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	var entity metadataEntityDescriptor
	if err := xml.Unmarshal(body, &entity); err != nil {
		return nil, err
	}
	result := &fetchedMetadata{
		EntityID: strings.TrimSpace(entity.EntityID),
	}
	for _, service := range entity.IDPSSODescriptor.SingleSignOnServices {
		if strings.TrimSpace(service.Location) != "" {
			result.SSOURL = strings.TrimSpace(service.Location)
			break
		}
	}
	for _, key := range entity.IDPSSODescriptor.KeyDescriptors {
		cert := compactCertificate(key.KeyInfo.X509Data.Certificate)
		if cert != "" {
			result.CertificatePEM = "-----BEGIN CERTIFICATE-----\n" + cert + "\n-----END CERTIFICATE-----"
			break
		}
	}
	return result, nil
}

func buildRedirectURL(connection store.Connection, relayState, requestID string) (string, error) {
	parsed, err := url.Parse(connection.SSOURL)
	if err != nil {
		return "", err
	}
	query := parsed.Query()
	authnRequest, err := buildAuthnRequest(connection, requestID)
	if err != nil {
		return "", err
	}
	query.Set("SAMLRequest", base64.StdEncoding.EncodeToString([]byte(authnRequest)))
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

type authnRequestEnvelope struct {
	XMLName      xml.Name `xml:"samlp:AuthnRequest"`
	XmlnsSamlp   string   `xml:"xmlns:samlp,attr"`
	XmlnsSaml    string   `xml:"xmlns:saml,attr"`
	ID           string   `xml:"ID,attr"`
	Version      string   `xml:"Version,attr"`
	IssueInstant string   `xml:"IssueInstant,attr"`
	Destination  string   `xml:"Destination,attr,omitempty"`
	Issuer       struct {
		Value string `xml:",chardata"`
	} `xml:"saml:Issuer"`
}

func buildAuthnRequest(connection store.Connection, requestID string) (string, error) {
	if strings.TrimSpace(requestID) == "" {
		requestID = newSAMLRequestID()
	}
	env := authnRequestEnvelope{
		XmlnsSamlp:   "urn:oasis:names:tc:SAML:2.0:protocol",
		XmlnsSaml:    "urn:oasis:names:tc:SAML:2.0:assertion",
		ID:           requestID,
		Version:      "2.0",
		IssueInstant: time.Now().UTC().Format(time.RFC3339Nano),
		Destination:  strings.TrimSpace(connection.SSOURL),
	}
	env.Issuer.Value = "urn:aegion:sp:" + connection.Slug
	raw, err := xml.Marshal(env)
	if err != nil {
		return "", err
	}
	return xml.Header + string(raw), nil
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

type samlResponseEnvelope struct {
	XMLName      xml.Name `xml:"Response"`
	ID           string   `xml:"ID,attr"`
	InResponseTo string   `xml:"InResponseTo,attr"`
	Destination  string   `xml:"Destination,attr"`
	IssueInstant string   `xml:"IssueInstant,attr"`
	Issuer       string   `xml:"Issuer"`
	Status       struct {
		StatusCode struct {
			Value string `xml:"Value,attr"`
		} `xml:"StatusCode"`
	} `xml:"Status"`
	Assertion struct {
		Issuer  string `xml:"Issuer"`
		Subject struct {
			NameID              string `xml:"NameID"`
			SubjectConfirmation struct {
				SubjectConfirmationData struct {
					InResponseTo string `xml:"InResponseTo,attr"`
					NotOnOrAfter string `xml:"NotOnOrAfter,attr"`
					Recipient    string `xml:"Recipient,attr"`
				} `xml:"SubjectConfirmationData"`
			} `xml:"SubjectConfirmation"`
		} `xml:"Subject"`
		Conditions struct {
			NotBefore    string `xml:"NotBefore,attr"`
			NotOnOrAfter string `xml:"NotOnOrAfter,attr"`
		} `xml:"Conditions"`
		AttributeStatement struct {
			Attributes []struct {
				Name   string `xml:"Name,attr"`
				Values []struct {
					Value string `xml:",chardata"`
				} `xml:"AttributeValue"`
			} `xml:"Attribute"`
		} `xml:"AttributeStatement"`
	} `xml:"Assertion"`
}

type parsedSAMLResponse struct {
	Subject     string
	Email       string
	DisplayName string
	Attributes  map[string]interface{}
}

func parseSAMLResponse(raw string, connection *store.Connection, expectedRequestID string, now time.Time) (*parsedSAMLResponse, error) {
	decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(raw))
	if err != nil {
		decoded, err = base64.RawStdEncoding.DecodeString(strings.TrimSpace(raw))
		if err != nil {
			return nil, ErrInvalidSAMLResponse
		}
	}
	var envelope samlResponseEnvelope
	if err := xml.Unmarshal(decoded, &envelope); err != nil {
		return nil, ErrInvalidSAMLResponse
	}
	if status := strings.TrimSpace(envelope.Status.StatusCode.Value); status != "" &&
		status != "urn:oasis:names:tc:SAML:2.0:status:Success" {
		return nil, ErrInvalidSAMLResponse
	}
	if !matchesRequestID(expectedRequestID, envelope.InResponseTo, envelope.Assertion.Subject.SubjectConfirmation.SubjectConfirmationData.InResponseTo) {
		return nil, ErrInvalidSAMLResponse
	}
	if !matchesIssuer(connection, envelope.Issuer, envelope.Assertion.Issuer) {
		return nil, ErrInvalidSAMLResponse
	}
	if !validTimeWindow(now, envelope.Assertion.Conditions.NotBefore, envelope.Assertion.Conditions.NotOnOrAfter) {
		return nil, ErrInvalidSAMLResponse
	}
	if !validNotOnOrAfter(now, envelope.Assertion.Subject.SubjectConfirmation.SubjectConfirmationData.NotOnOrAfter) {
		return nil, ErrInvalidSAMLResponse
	}
	result := &parsedSAMLResponse{
		Subject:    strings.TrimSpace(envelope.Assertion.Subject.NameID),
		Attributes: map[string]interface{}{},
	}
	for _, attr := range envelope.Assertion.AttributeStatement.Attributes {
		key := strings.TrimSpace(attr.Name)
		if key == "" || len(attr.Values) == 0 {
			continue
		}
		value := strings.TrimSpace(attr.Values[0].Value)
		if value == "" {
			continue
		}
		result.Attributes[key] = value
	}
	if connection != nil {
		result.Email = stringValue(result.Attributes[connection.AttributeMapping.Email])
		result.DisplayName = stringValue(result.Attributes[connection.AttributeMapping.DisplayName])
		if result.Subject == "" {
			result.Subject = stringValue(result.Attributes[connection.AttributeMapping.Subject])
		}
	}
	if result.Subject == "" {
		return nil, ErrInvalidSAMLResponse
	}
	return result, nil
}

func newSAMLRequestID() string {
	return "_" + fmt.Sprintf("%d", time.Now().UTC().UnixNano())
}

func matchesRequestID(expected string, values ...string) bool {
	expected = strings.TrimSpace(expected)
	if expected == "" {
		return true
	}
	for _, value := range values {
		if strings.TrimSpace(value) == expected {
			return true
		}
	}
	return false
}

func matchesIssuer(connection *store.Connection, issuers ...string) bool {
	if connection == nil || strings.TrimSpace(connection.EntityID) == "" {
		return true
	}
	expected := strings.TrimSpace(connection.EntityID)
	for _, issuer := range issuers {
		if strings.TrimSpace(issuer) == expected {
			return true
		}
	}
	return false
}

func validTimeWindow(now time.Time, notBeforeRaw, notOnOrAfterRaw string) bool {
	const skew = 2 * time.Minute
	if notBefore, ok := parseSAMLTimestamp(notBeforeRaw); ok && now.Add(skew).Before(notBefore) {
		return false
	}
	if notOnOrAfter, ok := parseSAMLTimestamp(notOnOrAfterRaw); ok && !now.Before(notOnOrAfter.Add(skew)) {
		return false
	}
	return true
}

func validNotOnOrAfter(now time.Time, value string) bool {
	const skew = 2 * time.Minute
	t, ok := parseSAMLTimestamp(value)
	if !ok {
		return true
	}
	return now.Before(t.Add(skew))
}

func parseSAMLTimestamp(value string) (time.Time, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, false
	}
	layouts := []string{time.RFC3339Nano, time.RFC3339, "2006-01-02T15:04:05.999Z", "2006-01-02T15:04:05Z"}
	for _, layout := range layouts {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed.UTC(), true
		}
	}
	return time.Time{}, false
}

func compactCertificate(value string) string {
	value = strings.TrimSpace(value)
	value = strings.ReplaceAll(value, "\n", "")
	value = strings.ReplaceAll(value, "\r", "")
	return value
}
