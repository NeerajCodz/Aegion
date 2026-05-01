// Package oidc provides JWKS endpoint.
package oidc

import (
	"context"
	"encoding/json"
)

// JWK represents a JSON Web Key.
type JWK struct {
	KTY string   `json:"kty"`           // Key type (RSA, EC, oct)
	USE string   `json:"use,omitempty"` // Public key use (sig, enc)
	KID string   `json:"kid"`           // Key ID
	ALG string   `json:"alg,omitempty"` // Algorithm
	N   string   `json:"n,omitempty"`   // RSA modulus
	E   string   `json:"e,omitempty"`   // RSA exponent
	X   string   `json:"x,omitempty"`   // EC x coordinate
	Y   string   `json:"y,omitempty"`   // EC y coordinate
	CRV string   `json:"crv,omitempty"` // EC curve
	X5C []string `json:"x5c,omitempty"` // X.509 certificate chain
	X5T string   `json:"x5t,omitempty"` // X.509 SHA-1 thumbprint
}

// JWKS represents a JSON Web Key Set.
type JWKS struct {
	Keys []JWK `json:"keys"`
}

// JWKSProvider interface for fetching public keys.
type JWKSProvider interface {
	GetPublicKeys(ctx context.Context) ([]JWK, error)
}

// JWKSService handles JWKS endpoint.
type JWKSService struct {
	provider JWKSProvider
}

// NewJWKSService creates a new JWKS service.
func NewJWKSService(provider JWKSProvider) *JWKSService {
	return &JWKSService{provider: provider}
}

// GetJWKS returns the JSON Web Key Set.
func (s *JWKSService) GetJWKS(ctx context.Context) (*JWKS, error) {
	keys, err := s.provider.GetPublicKeys(ctx)
	if err != nil {
		return nil, err
	}
	return &JWKS{Keys: keys}, nil
}

// MarshalJWKS marshals the JWKS to JSON.
func (s *JWKSService) MarshalJWKS(ctx context.Context) ([]byte, error) {
	jwks, err := s.GetJWKS(ctx)
	if err != nil {
		return nil, err
	}
	return json.Marshal(jwks)
}

// MockJWKSProvider is a mock JWKS provider for testing.
type MockJWKSProvider struct {
	Keys []JWK
}

func (m *MockJWKSProvider) GetPublicKeys(ctx context.Context) ([]JWK, error) {
	if m.Keys != nil {
		return m.Keys, nil
	}
	// Return a mock RSA key
	return []JWK{
		{
			KTY: "RSA",
			USE: "sig",
			KID: "key-1",
			ALG: "RS256",
			N:   "mock-modulus",
			E:   "AQAB",
		},
	}, nil
}
