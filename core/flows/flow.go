// Package flows provides self-service flow management for Aegion identity platform.
// It handles login, registration, recovery, settings, and verification flows with
// CSRF protection, state management, and configurable TTL.
package flows

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"time"

	"github.com/google/uuid"
)

// Flow types
const (
	TypeLogin        FlowType = "login"
	TypeRegistration FlowType = "registration"
	TypeRecovery     FlowType = "recovery"
	TypeSettings     FlowType = "settings"
	TypeVerification FlowType = "verification"
)

// Flow states
const (
	StateActive    FlowState = "active"
	StateCompleted FlowState = "completed"
	StateFailed    FlowState = "failed"
	StateExpired   FlowState = "expired"
)

// Default TTL for flows
const DefaultTTL = 15 * time.Minute

// FlowType represents the type of self-service flow
type FlowType string

// Valid returns true if the flow type is valid
func (t FlowType) Valid() bool {
	switch t {
	case TypeLogin, TypeRegistration, TypeRecovery, TypeSettings, TypeVerification:
		return true
	default:
		return false
	}
}

// FlowState represents the current state of a flow
type FlowState string

// Valid returns true if the flow state is valid
func (s FlowState) Valid() bool {
	switch s {
	case StateActive, StateCompleted, StateFailed, StateExpired:
		return true
	default:
		return false
	}
}

// IsTerminal returns true if the flow state is terminal (cannot transition)
func (s FlowState) IsTerminal() bool {
	return s == StateCompleted || s == StateFailed || s == StateExpired
}

// Errors
var (
	ErrFlowNotFound     = errors.New("flow not found")
	ErrFlowExpired      = errors.New("flow has expired")
	ErrFlowCompleted    = errors.New("flow already completed")
	ErrFlowFailed       = errors.New("flow has failed")
	ErrInvalidCSRF      = errors.New("invalid CSRF token")
	ErrInvalidFlowType  = errors.New("invalid flow type")
	ErrInvalidFlowState = errors.New("invalid flow state")
)

// Flow represents a self-service flow instance
type Flow struct {
	ID         uuid.UUID  `json:"id"`
	Type       FlowType   `json:"type"`
	State      FlowState  `json:"state"`
	IdentityID *uuid.UUID `json:"identity_id,omitempty"`
	SessionID  *uuid.UUID `json:"session_id,omitempty"`
	RequestURL string     `json:"request_url"`
	ReturnTo   string     `json:"return_to,omitempty"`
	UI         *UIState   `json:"ui"`
	Context    FlowCtx    `json:"context"`
	IssuedAt   time.Time  `json:"issued_at"`
	ExpiresAt  time.Time  `json:"expires_at"`
	CSRFToken  string     `json:"csrf_token"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
}

// UIState represents the UI state for rendering forms
type UIState struct {
	Action   string `json:"action"`
	Method   string `json:"method"`
	Nodes    []Node `json:"nodes"`
	Messages []Msg  `json:"messages,omitempty"`
}

// FlowCtx holds arbitrary context data for the flow
type FlowCtx map[string]any

// NewFlow creates a new flow with the given type and TTL
func NewFlow(flowType FlowType, requestURL string, ttl time.Duration) (*Flow, error) {
	if !flowType.Valid() {
		return nil, ErrInvalidFlowType
	}

	if ttl <= 0 {
		ttl = DefaultTTL
	}

	csrf, err := GenerateCSRFToken()
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC()

	return &Flow{
		ID:         uuid.New(),
		Type:       flowType,
		State:      StateActive,
		RequestURL: requestURL,
		UI:         &UIState{Nodes: []Node{}},
		Context:    make(FlowCtx),
		IssuedAt:   now,
		ExpiresAt:  now.Add(ttl),
		CSRFToken:  csrf,
		CreatedAt:  now,
		UpdatedAt:  now,
	}, nil
}

// IsExpired returns true if the flow has expired
func (f *Flow) IsExpired() bool {
	return time.Now().UTC().After(f.ExpiresAt)
}

// IsActive returns true if the flow is in active state and not expired
func (f *Flow) IsActive() bool {
	return f.State == StateActive && !f.IsExpired()
}

// ValidateCSRF validates the provided CSRF token against the flow's token
func (f *Flow) ValidateCSRF(token string) error {
	if !ValidateCSRFToken(f.CSRFToken, token) {
		return ErrInvalidCSRF
	}
	return nil
}

// Complete marks the flow as completed
func (f *Flow) Complete() error {
	if f.State.IsTerminal() {
		return ErrFlowCompleted
	}
	f.State = StateCompleted
	f.UpdatedAt = time.Now().UTC()
	return nil
}

// Fail marks the flow as failed
func (f *Flow) Fail() {
	if !f.State.IsTerminal() {
		f.State = StateFailed
		f.UpdatedAt = time.Now().UTC()
	}
}

// Expire marks the flow as expired
func (f *Flow) Expire() {
	if !f.State.IsTerminal() {
		f.State = StateExpired
		f.UpdatedAt = time.Now().UTC()
	}
}

// SetIdentity sets the identity ID for the flow
func (f *Flow) SetIdentity(identityID uuid.UUID) {
	f.IdentityID = &identityID
	f.UpdatedAt = time.Now().UTC()
}

// SetSession sets the session ID for the flow
func (f *Flow) SetSession(sessionID uuid.UUID) {
	f.SessionID = &sessionID
	f.UpdatedAt = time.Now().UTC()
}

// SetReturnTo sets the return URL for the flow
func (f *Flow) SetReturnTo(returnTo string) {
	f.ReturnTo = returnTo
	f.UpdatedAt = time.Now().UTC()
}

// AddContext adds a key-value pair to the flow context
func (f *Flow) AddContext(key string, value any) {
	f.Context[key] = value
	f.UpdatedAt = time.Now().UTC()
}

// GetContext retrieves a value from the flow context
func (f *Flow) GetContext(key string) (any, bool) {
	v, ok := f.Context[key]
	return v, ok
}

// GenerateCSRFToken generates a cryptographically secure CSRF token
func GenerateCSRFToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// ValidateCSRFToken performs constant-time comparison of CSRF tokens
func ValidateCSRFToken(expected, actual string) bool {
	if len(expected) == 0 || len(actual) == 0 {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(expected), []byte(actual)) == 1
}

// Msg represents a message in the UI
type Msg struct {
	ID      string  `json:"id"`
	Type    MsgType `json:"type"`
	Text    string  `json:"text"`
	Context any     `json:"context,omitempty"`
}

// MsgType represents the type of message
type MsgType string

const (
	MsgTypeInfo    MsgType = "info"
	MsgTypeError   MsgType = "error"
	MsgTypeSuccess MsgType = "success"
	MsgTypeWarning MsgType = "warning"
)
