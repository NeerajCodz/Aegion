// Package service provides OAuth2 authorization flow logic.
package authorization

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	platformcrypto "github.com/aegion/aegion/internal/platform/crypto"
	"github.com/aegion/aegion/modules/oauth2/store"
)

var (
	ErrInvalidRequest          = errors.New("invalid_request")
	ErrUnauthorizedClient      = errors.New("unauthorized_client")
	ErrAccessDenied            = errors.New("access_denied")
	ErrUnsupportedResponseType = errors.New("unsupported_response_type")
	ErrInvalidScope            = errors.New("invalid_scope")
	ErrServerError             = errors.New("server_error")
	ErrPKCERequired            = errors.New("pkce_required")
	ErrInvalidPKCE             = errors.New("invalid_pkce")
)

// AuthorizationStore interface for authorization operations.
type AuthorizationStore interface {
	GetClient(ctx context.Context, id string) (*store.Client, error)
	CreateAuthCode(ctx context.Context, code *store.AuthCode) error
	GetAuthCode(ctx context.Context, code string) (*store.AuthCode, error)
	MarkAuthCodeUsed(ctx context.Context, code string) error
	GetSessionAuthContext(ctx context.Context, sessionID string) (*store.SessionAuthContext, error)
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
	RequestURL          string   `json:"request_url,omitempty"`
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
	if req == nil {
		return nil, fmt.Errorf("%w: request is required", ErrInvalidRequest)
	}
	if strings.TrimSpace(req.ClientID) == "" {
		return nil, fmt.Errorf("%w: client_id is required", ErrInvalidRequest)
	}
	if strings.TrimSpace(req.RedirectURI) == "" {
		return nil, fmt.Errorf("%w: redirect_uri is required", ErrInvalidRequest)
	}
	if strings.TrimSpace(req.ResponseType) == "" {
		return nil, fmt.Errorf("%w: response_type is required", ErrInvalidRequest)
	}

	// Validate client
	client, err := s.store.GetClient(ctx, req.ClientID)
	if err != nil {
		return nil, fmt.Errorf("%w: client not found", ErrUnauthorizedClient)
	}

	if len(client.GrantTypes) > 0 && !client.HasGrantType("authorization_code") {
		return nil, fmt.Errorf("%w: client does not allow authorization_code", ErrUnauthorizedClient)
	}

	// Validate redirect URI
	if !client.ValidateRedirectURI(req.RedirectURI) {
		return nil, fmt.Errorf("%w: redirect_uri not registered", ErrInvalidRequest)
	}

	// Validate response type
	if req.ResponseType != "code" {
		return nil, fmt.Errorf("%w: only 'code' is supported", ErrUnsupportedResponseType)
	}
	if len(client.ResponseTypes) > 0 && !supportsValue(client.ResponseTypes, req.ResponseType) {
		return nil, fmt.Errorf("%w: response_type '%s' not allowed", ErrUnauthorizedClient, req.ResponseType)
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

	requestURL := strings.TrimSpace(req.RequestURL)
	if requestURL == "" {
		requestURL = req.RedirectURI
	}

	// Generate login challenge ID
	challengeID, err := store.GenerateLoginChallenge()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to generate login challenge", ErrServerError)
	}

	// Create login challenge
	challenge := &store.LoginChallenge{
		ID:                  challengeID,
		ClientID:            client.ID,
		RequestURL:          requestURL,
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
				if (consent.ExpiresAt == nil || time.Now().UTC().Before(*consent.ExpiresAt)) &&
					isSubset(challenge.Scopes, consent.Scopes) &&
					isSubset(challenge.Audience, consent.Audience) {
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

	if !isSubset(grantedScopes, challenge.RequestedScopes) {
		return nil, fmt.Errorf("%w: granted scopes exceed requested scopes", ErrInvalidScope)
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

		consentID, err := store.GenerateClientID()
		if err != nil {
			return nil, fmt.Errorf("%w: failed to generate consent ID", ErrServerError)
		}

		consent := &store.ConsentSession{
			ID:          consentID, // reuse generator
			ClientID:    challenge.ClientID,
			IdentityID:  challenge.IdentityID,
			Scopes:      grantedScopes,
			Audience:    challenge.RequestedAudience,
			Remember:    true,
			RememberFor: rememberFor,
			ExpiresAt:   expiresAt,
		}
		_ = s.store.CreateConsentSession(ctx, consent)
	}

	// Fetch login challenge for PKCE data
	loginChallenge, err := s.store.GetLoginChallenge(ctx, challenge.LoginChallengeID)
	if err != nil {
		return nil, err
	}

	acr, amr, authTime := deriveAuthContext(loginChallenge)
	sessionAuthContext, err := s.store.GetSessionAuthContext(ctx, challenge.SessionID)
	switch {
	case err == nil:
		acr, amr, authTime = deriveAuthContextFromSession(sessionAuthContext, authTime)
	case errors.Is(err, store.ErrNotFound):
		// Continue with login challenge context/defaults for compatibility.
	default:
		return nil, fmt.Errorf("%w: failed to resolve session auth context", ErrServerError)
	}

	// Generate authorization code
	authCodeValue, err := store.GenerateAuthCode()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to generate auth code", ErrServerError)
	}

	authCode := &store.AuthCode{
		Code:                authCodeValue,
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
		ACR:                 acr,
		AMR:                 amr,
		AuthTime:            authTime,
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
	ok, err := platformcrypto.VerifyPKCE(codeVerifier, codeChallenge, method)
	if err != nil {
		return fmt.Errorf("unsupported code_challenge_method: %s", method)
	}
	if !ok {
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

func supportsValue(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}

func isSubset(values []string, allowed []string) bool {
	allowedSet := make(map[string]struct{}, len(allowed))
	for _, value := range allowed {
		allowedSet[value] = struct{}{}
	}
	for _, value := range values {
		if _, ok := allowedSet[value]; !ok {
			return false
		}
	}
	return true
}

func deriveAuthContext(loginChallenge *store.LoginChallenge) (string, []string, time.Time) {
	authTime := time.Now().UTC()
	if loginChallenge != nil && loginChallenge.AuthenticatedAt != nil {
		authTime = loginChallenge.AuthenticatedAt.UTC()
	}
	return "aal1", []string{"pwd"}, authTime
}

func deriveAuthContextFromSession(ctx *store.SessionAuthContext, fallbackAuthTime time.Time) (string, []string, time.Time) {
	if ctx == nil {
		return "aal1", []string{"pwd"}, fallbackAuthTime
	}

	acr := "aal1"
	if normalized := normalizeACR(ctx.AAL); normalized != "" {
		acr = normalized
	}

	amr := dedupeAMR(ctx.Methods)
	if len(amr) == 0 {
		amr = []string{"pwd"}
	}

	authTime := fallbackAuthTime
	if !ctx.AuthenticatedAt.IsZero() {
		authTime = ctx.AuthenticatedAt.UTC()
	}

	return acr, amr, authTime
}

func normalizeACR(aal string) string {
	switch strings.ToLower(strings.TrimSpace(aal)) {
	case "aal0":
		return "aal0"
	case "aal1":
		return "aal1"
	case "aal2":
		return "aal2"
	default:
		return ""
	}
}

func dedupeAMR(methods []string) []string {
	if len(methods) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(methods))
	amr := make([]string, 0, len(methods))
	for _, method := range methods {
		value := mapMethodToAMR(method)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		amr = append(amr, value)
	}
	return amr
}

func mapMethodToAMR(method string) string {
	switch strings.ToLower(strings.TrimSpace(method)) {
	case "password":
		return "pwd"
	case "totp", "magic_link", "sms", "backup_code":
		return "otp"
	case "webauthn", "passkey":
		return "hwk"
	case "social", "saml":
		return "federated"
	default:
		return ""
	}
}
