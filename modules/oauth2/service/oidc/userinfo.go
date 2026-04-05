// Package oidc provides UserInfo endpoint.
package oidc

import (
	"context"
	"errors"
	"time"
)

var (
	ErrInvalidToken      = errors.New("invalid_token")
	ErrInsufficientScope = errors.New("insufficient_scope")
)

// UserInfoClaims represents standard OIDC UserInfo claims.
type UserInfoClaims struct {
	Sub               string  `json:"sub"`
	Name              *string `json:"name,omitempty"`
	GivenName         *string `json:"given_name,omitempty"`
	FamilyName        *string `json:"family_name,omitempty"`
	MiddleName        *string `json:"middle_name,omitempty"`
	Nickname          *string `json:"nickname,omitempty"`
	PreferredUsername *string `json:"preferred_username,omitempty"`
	Profile           *string `json:"profile,omitempty"`
	Picture           *string `json:"picture,omitempty"`
	Website           *string `json:"website,omitempty"`
	Email             *string `json:"email,omitempty"`
	EmailVerified     *bool   `json:"email_verified,omitempty"`
	Gender            *string `json:"gender,omitempty"`
	Birthdate         *string `json:"birthdate,omitempty"`
	Zoneinfo          *string `json:"zoneinfo,omitempty"`
	Locale            *string `json:"locale,omitempty"`
	PhoneNumber       *string `json:"phone_number,omitempty"`
	PhoneNumberVerified *bool `json:"phone_number_verified,omitempty"`
	Address           *Address `json:"address,omitempty"`
	UpdatedAt         *int64  `json:"updated_at,omitempty"`
}

// Address represents the address claim.
type Address struct {
	Formatted     *string `json:"formatted,omitempty"`
	StreetAddress *string `json:"street_address,omitempty"`
	Locality      *string `json:"locality,omitempty"`
	Region        *string `json:"region,omitempty"`
	PostalCode    *string `json:"postal_code,omitempty"`
	Country       *string `json:"country,omitempty"`
}

// AccessToken represents a validated access token.
type AccessToken struct {
	JTI        string
	IdentityID string
	ClientID   string
	Scopes     []string
	ExpiresAt  time.Time
}

// TokenValidator interface for validating access tokens.
type TokenValidator interface {
	ValidateAccessToken(ctx context.Context, token string) (*AccessToken, error)
}

// UserInfoProvider interface for fetching user profile data.
type UserInfoProvider interface {
	GetUserInfo(ctx context.Context, identityID string, scopes []string) (*UserInfoClaims, error)
}

// UserInfoService handles UserInfo endpoint.
type UserInfoService struct {
	validator TokenValidator
	provider  UserInfoProvider
}

// NewUserInfoService creates a new UserInfo service.
func NewUserInfoService(validator TokenValidator, provider UserInfoProvider) *UserInfoService {
	return &UserInfoService{
		validator: validator,
		provider:  provider,
	}
}

// GetUserInfo returns UserInfo claims for a valid access token.
func (s *UserInfoService) GetUserInfo(ctx context.Context, accessToken string) (*UserInfoClaims, error) {
	// Validate access token
	token, err := s.validator.ValidateAccessToken(ctx, accessToken)
	if err != nil {
		return nil, ErrInvalidToken
	}

	// Check for openid scope
	if !hasScope(token.Scopes, "openid") {
		return nil, ErrInsufficientScope
	}

	// Fetch user info
	claims, err := s.provider.GetUserInfo(ctx, token.IdentityID, token.Scopes)
	if err != nil {
		return nil, err
	}

	return claims, nil
}

// hasScope checks if a scope is in the list.
func hasScope(scopes []string, scope string) bool {
	for _, s := range scopes {
		if s == scope {
			return true
		}
	}
	return false
}

// MockTokenValidator is a mock token validator for testing.
type MockTokenValidator struct {
	Token *AccessToken
	Err   error
}

func (m *MockTokenValidator) ValidateAccessToken(ctx context.Context, token string) (*AccessToken, error) {
	if m.Err != nil {
		return nil, m.Err
	}
	if m.Token != nil {
		return m.Token, nil
	}
	return &AccessToken{
		JTI:        "mock-jti",
		IdentityID: "identity-123",
		ClientID:   "client-456",
		Scopes:     []string{"openid", "profile", "email"},
		ExpiresAt:  time.Now().Add(15 * time.Minute),
	}, nil
}

// MockUserInfoProvider is a mock user info provider for testing.
type MockUserInfoProvider struct {
	Claims *UserInfoClaims
	Err    error
}

func (m *MockUserInfoProvider) GetUserInfo(ctx context.Context, identityID string, scopes []string) (*UserInfoClaims, error) {
	if m.Err != nil {
		return nil, m.Err
	}
	if m.Claims != nil {
		return m.Claims, nil
	}
	email := "user@example.com"
	emailVerified := true
	name := "John Doe"
	return &UserInfoClaims{
		Sub:           identityID,
		Email:         &email,
		EmailVerified: &emailVerified,
		Name:          &name,
	}, nil
}
