// Package oidc provides OpenID Connect discovery and endpoints.
package oidc

import (
	"context"
	"encoding/json"
	"fmt"
)

// DiscoveryDocument represents the OpenID Connect discovery metadata.
type DiscoveryDocument struct {
	Issuer                                     string   `json:"issuer"`
	AuthorizationEndpoint                      string   `json:"authorization_endpoint"`
	TokenEndpoint                              string   `json:"token_endpoint"`
	UserinfoEndpoint                           string   `json:"userinfo_endpoint"`
	JwksURI                                    string   `json:"jwks_uri"`
	RegistrationEndpoint                       string   `json:"registration_endpoint,omitempty"`
	ScopesSupported                            []string `json:"scopes_supported"`
	ResponseTypesSupported                     []string `json:"response_types_supported"`
	ResponseModesSupported                     []string `json:"response_modes_supported,omitempty"`
	GrantTypesSupported                        []string `json:"grant_types_supported"`
	ACRValuesSupported                         []string `json:"acr_values_supported,omitempty"`
	SubjectTypesSupported                      []string `json:"subject_types_supported"`
	IDTokenSigningAlgValuesSupported           []string `json:"id_token_signing_alg_values_supported"`
	IDTokenEncryptionAlgValuesSupported        []string `json:"id_token_encryption_alg_values_supported,omitempty"`
	IDTokenEncryptionEncValuesSupported        []string `json:"id_token_encryption_enc_values_supported,omitempty"`
	UserinfoSigningAlgValuesSupported          []string `json:"userinfo_signing_alg_values_supported,omitempty"`
	UserinfoEncryptionAlgValuesSupported       []string `json:"userinfo_encryption_alg_values_supported,omitempty"`
	UserinfoEncryptionEncValuesSupported       []string `json:"userinfo_encryption_enc_values_supported,omitempty"`
	RequestObjectSigningAlgValuesSupported     []string `json:"request_object_signing_alg_values_supported,omitempty"`
	RequestObjectEncryptionAlgValuesSupported  []string `json:"request_object_encryption_alg_values_supported,omitempty"`
	RequestObjectEncryptionEncValuesSupported  []string `json:"request_object_encryption_enc_values_supported,omitempty"`
	TokenEndpointAuthMethodsSupported          []string `json:"token_endpoint_auth_methods_supported"`
	TokenEndpointAuthSigningAlgValuesSupported []string `json:"token_endpoint_auth_signing_alg_values_supported,omitempty"`
	DisplayValuesSupported                     []string `json:"display_values_supported,omitempty"`
	ClaimTypesSupported                        []string `json:"claim_types_supported,omitempty"`
	ClaimsSupported                            []string `json:"claims_supported,omitempty"`
	ServiceDocumentation                       string   `json:"service_documentation,omitempty"`
	ClaimsLocalesSupported                     []string `json:"claims_locales_supported,omitempty"`
	UILocalesSupported                         []string `json:"ui_locales_supported,omitempty"`
	ClaimsParameterSupported                   bool     `json:"claims_parameter_supported"`
	RequestParameterSupported                  bool     `json:"request_parameter_supported"`
	RequestURIParameterSupported               bool     `json:"request_uri_parameter_supported"`
	RequireRequestURIRegistration              bool     `json:"require_request_uri_registration"`
	OpPolicyURI                                string   `json:"op_policy_uri,omitempty"`
	OpTosURI                                   string   `json:"op_tos_uri,omitempty"`
	RevocationEndpoint                         string   `json:"revocation_endpoint,omitempty"`
	RevocationEndpointAuthMethodsSupported     []string `json:"revocation_endpoint_auth_methods_supported,omitempty"`
	IntrospectionEndpoint                      string   `json:"introspection_endpoint,omitempty"`
	IntrospectionEndpointAuthMethodsSupported  []string `json:"introspection_endpoint_auth_methods_supported,omitempty"`
	CodeChallengeMethodsSupported              []string `json:"code_challenge_methods_supported,omitempty"`
	DeviceAuthorizationEndpoint                string   `json:"device_authorization_endpoint,omitempty"`
	EndSessionEndpoint                         string   `json:"end_session_endpoint,omitempty"`
}

// DiscoveryService handles OIDC discovery.
type DiscoveryService struct {
	issuer  string
	baseURL string
}

// NewDiscoveryService creates a new discovery service.
func NewDiscoveryService(issuer, baseURL string) *DiscoveryService {
	return &DiscoveryService{
		issuer:  issuer,
		baseURL: baseURL,
	}
}

// GetDiscoveryDocument returns the OIDC discovery metadata.
func (s *DiscoveryService) GetDiscoveryDocument(ctx context.Context) (*DiscoveryDocument, error) {
	doc := &DiscoveryDocument{
		Issuer:                issuer(s.issuer),
		AuthorizationEndpoint: fmt.Sprintf("%s/oauth2/authorize", s.baseURL),
		TokenEndpoint:         fmt.Sprintf("%s/oauth2/token", s.baseURL),
		UserinfoEndpoint:      fmt.Sprintf("%s/oidc/userinfo", s.baseURL),
		JwksURI:               fmt.Sprintf("%s/.well-known/jwks.json", s.baseURL),
		RevocationEndpoint:    fmt.Sprintf("%s/oauth2/revoke", s.baseURL),
		IntrospectionEndpoint: fmt.Sprintf("%s/oauth2/introspect", s.baseURL),
		DeviceAuthorizationEndpoint: fmt.Sprintf("%s/oauth2/device/authorize", s.baseURL),
		EndSessionEndpoint:    fmt.Sprintf("%s/oauth2/logout", s.baseURL),

		ScopesSupported: []string{
			"openid",
			"profile",
			"email",
			"address",
			"phone",
			"offline_access",
		},
		ResponseTypesSupported: []string{
			"code",
			"token",
			"id_token",
			"code token",
			"code id_token",
			"token id_token",
			"code token id_token",
		},
		ResponseModesSupported: []string{
			"query",
			"fragment",
			"form_post",
		},
		GrantTypesSupported: []string{
			"authorization_code",
			"refresh_token",
			"client_credentials",
			"urn:ietf:params:oauth:grant-type:device_code",
			"urn:ietf:params:oauth:grant-type:jwt-bearer",
		},
		SubjectTypesSupported: []string{
			"public",
			"pairwise",
		},
		IDTokenSigningAlgValuesSupported: []string{
			"RS256",
			"RS384",
			"RS512",
			"ES256",
			"ES384",
			"ES512",
			"PS256",
			"PS384",
			"PS512",
		},
		TokenEndpointAuthMethodsSupported: []string{
			"none",
			"client_secret_basic",
			"client_secret_post",
			"client_secret_jwt",
			"private_key_jwt",
		},
		TokenEndpointAuthSigningAlgValuesSupported: []string{
			"RS256", "RS384", "RS512",
			"ES256", "ES384", "ES512",
			"PS256", "PS384", "PS512",
		},
		RevocationEndpointAuthMethodsSupported: []string{
			"none",
			"client_secret_basic",
			"client_secret_post",
			"client_secret_jwt",
			"private_key_jwt",
		},
		IntrospectionEndpointAuthMethodsSupported: []string{
			"client_secret_basic",
			"client_secret_post",
			"client_secret_jwt",
			"private_key_jwt",
		},
		ClaimTypesSupported: []string{
			"normal",
		},
		ClaimsSupported: []string{
			"sub",
			"iss",
			"aud",
			"exp",
			"iat",
			"auth_time",
			"nonce",
			"acr",
			"amr",
			"azp",
			"at_hash",
			"c_hash",
			"name",
			"given_name",
			"family_name",
			"middle_name",
			"nickname",
			"preferred_username",
			"profile",
			"picture",
			"website",
			"email",
			"email_verified",
			"gender",
			"birthdate",
			"zoneinfo",
			"locale",
			"phone_number",
			"phone_number_verified",
			"address",
			"updated_at",
		},
		CodeChallengeMethodsSupported: []string{
			"S256",
			"plain",
		},
		ClaimsParameterSupported:      false,
		RequestParameterSupported:     false,
		RequestURIParameterSupported:  false,
		RequireRequestURIRegistration: false,
	}

	return doc, nil
}

// MarshalDiscoveryDocument marshals the discovery document to JSON.
func (s *DiscoveryService) MarshalDiscoveryDocument(ctx context.Context) ([]byte, error) {
	doc, err := s.GetDiscoveryDocument(ctx)
	if err != nil {
		return nil, err
	}
	return json.Marshal(doc)
}

// issuer ensures the issuer URL does not have a trailing slash per OIDC spec.
func issuer(iss string) string {
	if len(iss) > 0 && iss[len(iss)-1] == '/' {
		return iss[:len(iss)-1]
	}
	return iss
}
