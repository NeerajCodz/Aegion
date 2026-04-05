// Package device implements RFC 8628 Device Authorization Grant.
package device

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/Qypher/aegion/modules/oauth2/store"
)

var (
	ErrSlowDown       = errors.New("slow_down")
	ErrAuthorizationPending = errors.New("authorization_pending")
	ErrExpiredToken   = errors.New("expired_token")
	ErrAccessDenied   = errors.New("access_denied")
	ErrInvalidClient  = errors.New("invalid_client")
	ErrInvalidGrant   = errors.New("invalid_grant")
)

// DeviceStore interface for device flow operations.
type DeviceStore interface {
	GetClient(ctx context.Context, id string) (*store.Client, error)
	CreateDeviceCode(ctx context.Context, dc *store.DeviceCode) error
	GetDeviceCode(ctx context.Context, deviceCode string) (*store.DeviceCode, error)
	GetDeviceCodeByUserCode(ctx context.Context, userCode string) (*store.DeviceCode, error)
	MarkDeviceCodeApproved(ctx context.Context, deviceCode, identityID string, scopes []string) error
	MarkDeviceCodeDenied(ctx context.Context, deviceCode string) error
	MarkDeviceCodeUsed(ctx context.Context, deviceCode string) error
}

// DeviceService handles device authorization flow.
type DeviceService struct {
	store                DeviceStore
	deviceCodeTTL        time.Duration
	pollingInterval      int // seconds
	verificationURI      string
	verificationURIComplete bool
}

// NewDeviceService creates a new device service.
func NewDeviceService(store DeviceStore, deviceCodeTTL time.Duration, pollingInterval int, verificationURI string) *DeviceService {
	return &DeviceService{
		store:           store,
		deviceCodeTTL:   deviceCodeTTL,
		pollingInterval: pollingInterval,
		verificationURI: verificationURI,
		verificationURIComplete: true,
	}
}

// DeviceAuthorizationRequest represents a device authorization request.
type DeviceAuthorizationRequest struct {
	ClientID string
	Scope    string
}

// DeviceAuthorizationResponse represents a device authorization response.
type DeviceAuthorizationResponse struct {
	DeviceCode              string `json:"device_code"`
	UserCode                string `json:"user_code"`
	VerificationURI         string `json:"verification_uri"`
	VerificationURIComplete string `json:"verification_uri_complete,omitempty"`
	ExpiresIn               int    `json:"expires_in"`
	Interval                int    `json:"interval,omitempty"`
}

// RequestDeviceAuthorization initiates a device authorization flow.
func (s *DeviceService) RequestDeviceAuthorization(ctx context.Context, req *DeviceAuthorizationRequest) (*DeviceAuthorizationResponse, error) {
	// Validate client
	client, err := s.store.GetClient(ctx, req.ClientID)
	if err != nil {
		return nil, ErrInvalidClient
	}

	// Parse scopes
	scopes := parseScopes(req.Scope)

	// Generate device code and user code
	deviceCode := store.GenerateDeviceCode()
	userCode := generateUserCode()

	// Create device code record
	dc := &store.DeviceCode{
		DeviceCode:  deviceCode,
		UserCode:    userCode,
		ClientID:    client.ID,
		Scopes:      scopes,
		ExpiresAt:   time.Now().UTC().Add(s.deviceCodeTTL),
		Interval:    &s.pollingInterval,
	}

	if err := s.store.CreateDeviceCode(ctx, dc); err != nil {
		return nil, err
	}

	// Build response
	resp := &DeviceAuthorizationResponse{
		DeviceCode:      deviceCode,
		UserCode:        userCode,
		VerificationURI: s.verificationURI,
		ExpiresIn:       int(s.deviceCodeTTL.Seconds()),
		Interval:        s.pollingInterval,
	}

	if s.verificationURIComplete {
		resp.VerificationURIComplete = fmt.Sprintf("%s?user_code=%s", s.verificationURI, userCode)
	}

	return resp, nil
}

// DeviceTokenRequest represents a device token polling request.
type DeviceTokenRequest struct {
	GrantType  string
	DeviceCode string
	ClientID   string
}

// PollDeviceToken polls for device authorization completion.
// Returns ErrAuthorizationPending if user hasn't authorized yet.
// Returns ErrSlowDown if client is polling too fast.
func (s *DeviceService) PollDeviceToken(ctx context.Context, req *DeviceTokenRequest) (*store.DeviceCode, error) {
	// Retrieve device code
	dc, err := s.store.GetDeviceCode(ctx, req.DeviceCode)
	if err != nil {
		return nil, ErrInvalidGrant
	}

	// Verify client
	if dc.ClientID != req.ClientID {
		return nil, ErrInvalidClient
	}

	// Check expiration
	if time.Now().UTC().After(dc.ExpiresAt) {
		return nil, ErrExpiredToken
	}

	// Check if denied
	if dc.DeniedAt != nil {
		return nil, ErrAccessDenied
	}

	// Check if approved
	if dc.ApprovedAt == nil {
		// Check polling rate
		if dc.LastPolledAt != nil {
			interval := s.pollingInterval
			if dc.Interval != nil {
				interval = *dc.Interval
			}
			elapsed := time.Since(*dc.LastPolledAt)
			if elapsed < time.Duration(interval)*time.Second {
				return nil, ErrSlowDown
			}
		}
		return nil, ErrAuthorizationPending
	}

	// Check if already used
	if dc.Used {
		return nil, ErrInvalidGrant
	}

	return dc, nil
}

// ApproveDeviceAuthorization approves a device authorization request.
func (s *DeviceService) ApproveDeviceAuthorization(ctx context.Context, userCode, identityID string, scopes []string) error {
	// Get device code by user code
	dc, err := s.store.GetDeviceCodeByUserCode(ctx, userCode)
	if err != nil {
		return err
	}

	// Check expiration
	if time.Now().UTC().After(dc.ExpiresAt) {
		return ErrExpiredToken
	}

	// Check if already approved/denied
	if dc.ApprovedAt != nil || dc.DeniedAt != nil {
		return ErrInvalidGrant
	}

	// Mark as approved
	return s.store.MarkDeviceCodeApproved(ctx, dc.DeviceCode, identityID, scopes)
}

// DenyDeviceAuthorization denies a device authorization request.
func (s *DeviceService) DenyDeviceAuthorization(ctx context.Context, userCode string) error {
	// Get device code by user code
	dc, err := s.store.GetDeviceCodeByUserCode(ctx, userCode)
	if err != nil {
		return err
	}

	return s.store.MarkDeviceCodeDenied(ctx, dc.DeviceCode)
}

// generateUserCode generates an 8-character user code in format XXXX-XXXX.
// Uses uppercase consonants only to avoid profanity and visual confusion.
func generateUserCode() string {
	const charset = "BCDFGHJKLMNPQRSTVWXYZ" // No vowels to avoid profanity
	const codeLength = 8

	code := make([]byte, codeLength)
	for i := 0; i < codeLength; i++ {
		n, _ := rand.Int(rand.Reader, big.NewInt(int64(len(charset))))
		code[i] = charset[n.Int64()]
	}

	// Format as XXXX-XXXX
	return fmt.Sprintf("%s-%s", string(code[:4]), string(code[4:]))
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
