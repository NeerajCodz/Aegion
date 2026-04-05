// Package service provides OAuth2 authorization flow logic.
package authorization

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Qypher/aegion/modules/oauth2/store"
)

var (
	ErrInvalidRequest       = errors.New("invalid_request")
	ErrUnauthorizedClient   = errors.New("unauthorized_client")
	ErrAccessDenied         = errors.New("access_denied")
	ErrUnsupportedResponseType = errors.New("unsupported_response_type")
	ErrInvalidScope         = errors.New("invalid_scope")
	ErrServerError          = errors.New("server_error")
	ErrPKCERequired         = errors.New("pkce_required")
	ErrInvalidPKCE          = errors.New("invalid_pkce")
)

// AuthorizationStore interface for authorization operations.
type AuthorizationStore interface {
	GetClient(ctx context.Context, id string) (*store.Client, error)
	CreateAuthCode(ctx context.Context, code *store.AuthCode) error
	GetAuthCode(ctx context.Context, code string) (*store.AuthCode, error)
	MarkAuthCodeUsed(ctx context.Context, code string) error
	CreateLoginChallenge(ctx context.Context, challenge *store.LoginChallenge) error
	GetLoginChallenge(ctx context.Context, id string) (*store.LoginChallenge, error)
	AcceptLoginChallenge(ctx context.Context, id, identityID, sessionID string) error
	CreateConsentChallenge(ctx context.Context, challenge *store.ConsentChallenge) error
	GetConsentChallenge(ctx context.Context, id string) (*store.ConsentChallenge, error)
	AcceptConsentChallenge(ctx context.Context, id string, grantedScopes, grantedAudience []string, remember bool, rememberFor *int) error
	RejectConsentChallenge(ctx context.Context, id, errorCode, errorDesc string) error
	GetConsentSession(ctx context.Context, clientID, identityID string) (*store.ConsentSession, error)
	CreateConsentSession(ctx context.Context, consent *store.ConsentSession) error
}

// AuthorizationService handles OAuth2 authorization code flow.
type AuthorizationService struct {
	store AuthorizationStore
}

// NewAuthorizationService creates a new authorization service.
func NewAuthorizationService(store AuthorizationStore) *AuthorizationService {
	return &AuthorizationService{store: store}
}

// AuthorizeRequest represents an authorization request.
type AuthorizeRequest struct {
	ClientID            string   `json:"client_id"`
	RedirectURI         string   `json:"redirect_uri"`
	ResponseType        string   `json:"response_type"`
	Scope               string   `json:"scope"`
	State               string   `json:"state,omitempty"`
	Nonce               string   `json:"nonce,omitempty"`
	CodeChallenge       string   `json:"code_challenge,omitempty"`
	CodeChallengeMethod string   `json:"code_challenge_method,omitempty"`
	Audience            []string `json:"audience,omitempty"`
	ACRValues           []string `json:"acr_values,omitempty"`
	MaxAge              *int     `json:"max_age,omitempty"`
	Prompt              string   `json:"prompt,omitempty"`
}

// LoginChallengeResponse represents a login challenge redirect.
type LoginChallengeResponse struct {
	LoginChallenge string `json:"login_challenge"`
	RedirectTo     string `json:"redirect_to"`
}

// ConsentChallengeResponse represents a consent challenge redirect.
type ConsentChallengeResponse struct {
	ConsentChallenge string `json:"consent_challenge"`
	RedirectTo       string `json:"redirect_to"`
}

// AuthorizationResponse represents a successful authorization.
type AuthorizationResponse struct {
	Code  string `json:"code"`
	State string `json:"state,omitempty"`
}

// StartAuthorization initiates the authorization flow.
func (s *AuthorizationService) StartAuthorization(ctx context.Context, req *AuthorizeRequest) (*LoginChallengeResponse, error) {
	// Validate client
	client, err := s.store.GetClient(ctx, req.ClientID)
	if err != nil {
		return nil, fmt.Errorf("%w: client not found", ErrUnauthorizedClient)
	}

	// Validate redirect URI
	if !client.ValidateRedirectURI(req.RedirectURI) {
		return nil, fmt.Errorf("%w: redirect_uri not registered", ErrInvalidRequest)
	}

	// Validate response type
	if req.ResponseType != "code" {
		return nil, fmt.Errorf("%w: only 'code' is supported", ErrUnsupportedResponseType)
	}

	// Parse and validate scopes
	scopes := parseScopes(req.Scope)
	if len(scopes) == 0 {
		scopes = []string{"openid"}
	}
	for _, scope := range scopes {
		if !client.HasScope(scope) {
			return nil, fmt.Errorf("%w: scope '%s' not allowed", ErrInvalidScope, scope)
		}
	}

	// Validate PKCE
	if client.RequirePKCE {
		if req.CodeChallenge == "" {
			return nil, fmt.Errorf("%w: client requires PKCE", ErrPKCERequired)
		}
		if req.CodeChallengeMethod != "S256" && req.CodeChallengeMethod != "plain" {
			return nil, fmt.Errorf("%w: invalid code_challenge_method", ErrInvalidPKCE)
		}
	}

	// Create login challenge
	challenge := &store.LoginChallenge{
		ID:                  store.GenerateLoginChallenge(),
		ClientID:            client.ID,
		RequestURL:          "", // TODO: capture full request URL
		RedirectURI:         req.RedirectURI,
		Scopes:              scopes,
		Audience:            req.Audience,
		ACRValues:           req.ACRValues,
		State:               &req.State,
		CodeChallenge:       &req.CodeChallenge,
		CodeChallengeMethod: &req.CodeChallengeMethod,
		Nonce:               &req.Nonce,
		Skip:                false,
		ExpiresAt:           time.Now().UTC().Add(10 * time.Minute),
	}

	if err := s.store.CreateLoginChallenge(ctx, challenge); err != nil {
		return nil, fmt.Errorf("%w: failed to create login challenge", ErrServerError)
	}

	// Return login challenge redirect
	return &LoginChallengeResponse{
		LoginChallenge: challenge.ID,
		RedirectTo:     fmt.Sprintf("/oauth2/login?challenge=%s", challenge.ID),
	}, nil
}

// AcceptLogin accepts a login challenge (called after user authentication).
func (s *AuthorizationService) AcceptLogin(ctx context.Context, challengeID, identityID, sessionID string) (*ConsentChallengeResponse, error) {
	challenge, err := s.store.GetLoginChallenge(ctx, challengeID)
	if err != nil {
		return nil, fmt.Errorf("%w: login challenge not found", ErrInvalidRequest)
	}

	if time.Now().UTC().After(challenge.ExpiresAt) {
		return nil, fmt.Errorf("%w: login challenge expired", ErrInvalidRequest)
	}

	// Mark login as accepted
	if err := s.store.AcceptLoginChallenge(ctx, challengeID, identityID, sessionID); err != nil {
		return nil, err
	}

	// Check if we can skip consent
	skipConsent := false
	if !challenge.Skip {
		// Check for existing consent session
		client, _ := s.store.GetClient(ctx, challenge.ClientID)
		if client != nil && !client.RequireConsent {
			skipConsent = true
		} else {
			// Check remembered consent
			consent, err := s.store.GetConsentSession(ctx, challenge.ClientID, identityID)
			if err == nil && consent != nil && consent.Remember {
				if consent.ExpiresAt == nil || time.Now().UTC().Before(*consent.ExpiresAt) {
					skipConsent = true
				}
			}
		}
	}

	// Create consent challenge
	consentChallenge := &store.ConsentChallenge{
		ID:                challengeID + "_consent",
		LoginChallengeID:  challengeID,
		ClientID:          challenge.ClientID,
		IdentityID:        identityID,
		SessionID:         sessionID,
		RequestURL:        challenge.RequestURL,
		RedirectURI:       challenge.RedirectURI,
		RequestedScopes:   challenge.Scopes,
		RequestedAudience: challenge.Audience,
		Skip:              skipConsent,
		ExpiresAt:         time.Now().UTC().Add(10 * time.Minute),
	}

	if err := s.store.CreateConsentChallenge(ctx, consentChallenge); err != nil {
		return nil, err
	}

	if skipConsent {
		// Auto-accept consent
		if err := s.store.AcceptConsentChallenge(ctx, consentChallenge.ID, challenge.Scopes, challenge.Audience, false, nil); err != nil {
			return nil, err
		}
	}

	return &ConsentChallengeResponse{
		ConsentChallenge: consentChallenge.ID,
		RedirectTo:       fmt.Sprintf("/oauth2/consent?challenge=%s", consentChallenge.ID),
	}, nil
}

// AcceptConsent accepts a consent challenge (called after user grants consent).
func (s *AuthorizationService) AcceptConsent(ctx context.Context, challengeID string, grantedScopes []string, remember bool, rememberFor *int) (*AuthorizationResponse, error) {
	challenge, err := s.store.GetConsentChallenge(ctx, challengeID)
	if err != nil {
		return nil, fmt.Errorf("%w: consent challenge not found", ErrInvalidRequest)
	}

	if time.Now().UTC().After(challenge.ExpiresAt) {
		return nil, fmt.Errorf("%w: consent challenge expired", ErrInvalidRequest)
	}

	if challenge.Handled {
		return nil, fmt.Errorf("%w: consent already handled", ErrInvalidRequest)
	}

	// Mark consent as accepted
	if err := s.store.AcceptConsentChallenge(ctx, challengeID, grantedScopes, challenge.RequestedAudience, remember, rememberFor); err != nil {
		return nil, err
	}

	// Create remembered consent session if requested
	if remember {
		var expiresAt *time.Time
		if rememberFor != nil && *rememberFor > 0 {
			exp := time.Now().UTC().Add(time.Duration(*rememberFor) * time.Second)
			expiresAt = &exp
		}

		consent := &store.ConsentSession{
			ID:         store.GenerateClientID(), // reuse generator
			ClientID:   challenge.ClientID,
			IdentityID: challenge.IdentityID,
			Scopes:     grantedScopes,
			Audience:   challenge.RequestedAudience,
			Remember:   true,
			RememberFor: rememberFor,
			ExpiresAt:  expiresAt,
		}
		_ = s.store.CreateConsentSession(ctx, consent)
	}

	// Fetch login challenge for PKCE data
	loginChallenge, err := s.store.GetLoginChallenge(ctx, challenge.LoginChallengeID)
	if err != nil {
		return nil, err
	}

	// Generate authorization code
	authCode := &store.AuthCode{
		Code:                store.GenerateAuthCode(),
		ClientID:            challenge.ClientID,
		IdentityID:          challenge.IdentityID,
		SessionID:           challenge.SessionID,
		RedirectURI:         challenge.RedirectURI,
		Scopes:              grantedScopes,
		Audience:            challenge.RequestedAudience,
		CodeChallenge:       loginChallenge.CodeChallenge,
		CodeChallengeMethod: loginChallenge.CodeChallengeMethod,
		Nonce:               loginChallenge.Nonce,
		State:               loginChallenge.State,
		ACR:                 "aal1", // TODO: determine from session
		AMR:                 []string{"pwd"}, // TODO: determine from session
		ExpiresAt:           time.Now().UTC().Add(10 * time.Minute),
	}

	if err := s.store.CreateAuthCode(ctx, authCode); err != nil {
		return nil, fmt.Errorf("%w: failed to create authorization code", ErrServerError)
	}

	state := ""
	if loginChallenge.State != nil {
		state = *loginChallenge.State
	}

	return &AuthorizationResponse{
		Code:  authCode.Code,
		State: state,
	}, nil
}

// RejectConsent rejects a consent challenge.
func (s *AuthorizationService) RejectConsent(ctx context.Context, challengeID, errorCode, errorDesc string) error {
	return s.store.RejectConsentChallenge(ctx, challengeID, errorCode, errorDesc)
}

// VerifyPKCE verifies a PKCE code verifier against the challenge.
func VerifyPKCE(codeVerifier, codeChallenge, method string) error {
	if method == "" {
		method = "plain"
	}

	var computed string
	switch method {
	case "plain":
		computed = codeVerifier
	case "S256":
		h := sha256.Sum256([]byte(codeVerifier))
		computed = base64.RawURLEncoding.EncodeToString(h[:])
	default:
		return fmt.Errorf("unsupported code_challenge_method: %s", method)
	}

	if computed != codeChallenge {
		return store.ErrPKCEMismatch
	}

	return nil
}

// parseScopes parses a space-separated scope string.
func parseScopes(scope string) []string {
	if scope == "" {
		return []string{}
	}
	parts := strings.Split(scope, " ")
	var scopes []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			scopes = append(scopes, p)
		}
	}
	return scopes
}
