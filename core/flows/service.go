package flows

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// AuthMethod represents an available authentication method
type AuthMethod struct {
	Method   string `json:"method"`
	Provider string `json:"provider,omitempty"`
	Config   any    `json:"config,omitempty"`
}

// Config holds the service configuration
type Config struct {
	LoginTTL        time.Duration
	RegistrationTTL time.Duration
	RecoveryTTL     time.Duration
	SettingsTTL     time.Duration
	VerificationTTL time.Duration
	DefaultMethods  []AuthMethod
}

// DefaultConfig returns a configuration with sensible defaults
func DefaultConfig() Config {
	return Config{
		LoginTTL:        15 * time.Minute,
		RegistrationTTL: 30 * time.Minute,
		RecoveryTTL:     15 * time.Minute,
		SettingsTTL:     30 * time.Minute,
		VerificationTTL: 24 * time.Hour,
		DefaultMethods: []AuthMethod{
			{Method: "password"},
		},
	}
}

// Service manages self-service flows
type Service struct {
	store  FlowStore
	config Config
}

// NewService creates a new flow service
func NewService(store FlowStore, config Config) *Service {
	if config.LoginTTL <= 0 {
		config.LoginTTL = DefaultTTL
	}
	if config.RegistrationTTL <= 0 {
		config.RegistrationTTL = DefaultTTL
	}
	if config.RecoveryTTL <= 0 {
		config.RecoveryTTL = DefaultTTL
	}
	if config.SettingsTTL <= 0 {
		config.SettingsTTL = DefaultTTL
	}
	if config.VerificationTTL <= 0 {
		config.VerificationTTL = DefaultTTL
	}

	return &Service{
		store:  store,
		config: config,
	}
}

// CreateLoginFlow creates a new login flow
func (s *Service) CreateLoginFlow(ctx context.Context, requestURL string) (*Flow, error) {
	flow, err := NewFlow(TypeLogin, requestURL, s.config.LoginTTL)
	if err != nil {
		return nil, err
	}

	flow.UI = s.generateLoginUI(flow)

	if err := s.store.Create(ctx, flow); err != nil {
		return nil, err
	}

	return flow, nil
}

// CreateRegistrationFlow creates a new registration flow
func (s *Service) CreateRegistrationFlow(ctx context.Context, requestURL string) (*Flow, error) {
	flow, err := NewFlow(TypeRegistration, requestURL, s.config.RegistrationTTL)
	if err != nil {
		return nil, err
	}

	flow.UI = s.generateRegistrationUI(flow)

	if err := s.store.Create(ctx, flow); err != nil {
		return nil, err
	}

	return flow, nil
}

// CreateRecoveryFlow creates a new account recovery flow
func (s *Service) CreateRecoveryFlow(ctx context.Context, requestURL string) (*Flow, error) {
	flow, err := NewFlow(TypeRecovery, requestURL, s.config.RecoveryTTL)
	if err != nil {
		return nil, err
	}

	flow.UI = s.generateRecoveryUI(flow)

	if err := s.store.Create(ctx, flow); err != nil {
		return nil, err
	}

	return flow, nil
}

// CreateSettingsFlow creates a new settings flow for an authenticated user
func (s *Service) CreateSettingsFlow(ctx context.Context, requestURL string, identityID uuid.UUID) (*Flow, error) {
	flow, err := NewFlow(TypeSettings, requestURL, s.config.SettingsTTL)
	if err != nil {
		return nil, err
	}

	flow.SetIdentity(identityID)
	flow.UI = s.generateSettingsUI(flow)

	if err := s.store.Create(ctx, flow); err != nil {
		return nil, err
	}

	return flow, nil
}

// CreateVerificationFlow creates a new verification flow
func (s *Service) CreateVerificationFlow(ctx context.Context, requestURL string, identityID *uuid.UUID) (*Flow, error) {
	flow, err := NewFlow(TypeVerification, requestURL, s.config.VerificationTTL)
	if err != nil {
		return nil, err
	}

	if identityID != nil {
		flow.SetIdentity(*identityID)
	}

	flow.UI = s.generateVerificationUI(flow)

	if err := s.store.Create(ctx, flow); err != nil {
		return nil, err
	}

	return flow, nil
}

// GetFlow retrieves a flow by ID and validates it
func (s *Service) GetFlow(ctx context.Context, id uuid.UUID) (*Flow, error) {
	flow, err := s.store.Get(ctx, id)
	if err != nil {
		return nil, err
	}

	if err := s.checkFlowState(flow); err != nil {
		return nil, err
	}

	return flow, nil
}

// ValidateFlow validates a flow's state, expiry, and CSRF token
func (s *Service) ValidateFlow(ctx context.Context, id uuid.UUID, csrfToken string) (*Flow, error) {
	flow, err := s.store.Get(ctx, id)
	if err != nil {
		return nil, err
	}

	if err := s.checkFlowState(flow); err != nil {
		return nil, err
	}

	if err := flow.ValidateCSRF(csrfToken); err != nil {
		return nil, err
	}

	return flow, nil
}

// ValidateFlowByCSRF retrieves and validates a flow using its CSRF token
func (s *Service) ValidateFlowByCSRF(ctx context.Context, csrfToken string) (*Flow, error) {
	flow, err := s.store.GetByCSRF(ctx, csrfToken)
	if err != nil {
		return nil, err
	}

	if err := s.checkFlowState(flow); err != nil {
		return nil, err
	}

	return flow, nil
}

// CompleteFlow marks a flow as completed
func (s *Service) CompleteFlow(ctx context.Context, id uuid.UUID) error {
	flow, err := s.store.Get(ctx, id)
	if err != nil {
		return err
	}

	if err := flow.Complete(); err != nil {
		return err
	}

	return s.store.Update(ctx, flow)
}

// FailFlow marks a flow as failed with an optional error message
func (s *Service) FailFlow(ctx context.Context, id uuid.UUID, errMsg string) error {
	flow, err := s.store.Get(ctx, id)
	if err != nil {
		return err
	}

	flow.Fail()

	if errMsg != "" {
		flow.UI.Messages = append(flow.UI.Messages, Msg{
			ID:   "flow_error",
			Type: MsgTypeError,
			Text: errMsg,
		})
	}

	return s.store.Update(ctx, flow)
}

// UpdateFlowUI updates the UI state of a flow
func (s *Service) UpdateFlowUI(ctx context.Context, id uuid.UUID, ui *UIState) error {
	flow, err := s.store.Get(ctx, id)
	if err != nil {
		return err
	}

	flow.UI = ui
	return s.store.Update(ctx, flow)
}

// AddFlowMessage adds a message to the flow's UI
func (s *Service) AddFlowMessage(ctx context.Context, id uuid.UUID, msg Msg) error {
	flow, err := s.store.Get(ctx, id)
	if err != nil {
		return err
	}

	flow.UI.Messages = append(flow.UI.Messages, msg)
	return s.store.Update(ctx, flow)
}

// GetFlowMethods returns the available authentication methods for a flow type
func (s *Service) GetFlowMethods(flowType FlowType) []AuthMethod {
	switch flowType {
	case TypeLogin:
		return s.config.DefaultMethods
	case TypeRegistration:
		return s.config.DefaultMethods
	case TypeRecovery:
		return []AuthMethod{
			{Method: "link"},
			{Method: "code"},
		}
	case TypeSettings:
		return []AuthMethod{
			{Method: "password"},
			{Method: "profile"},
		}
	case TypeVerification:
		return []AuthMethod{
			{Method: "link"},
			{Method: "code"},
		}
	default:
		return nil
	}
}

// Cleanup removes expired and terminal flows
func (s *Service) Cleanup(ctx context.Context) (int64, error) {
	return s.store.DeleteExpired(ctx)
}

// checkFlowState validates the current state of a flow
func (s *Service) checkFlowState(flow *Flow) error {
	if flow.IsExpired() {
		if flow.State == StateActive {
			flow.Expire()
		}
		return ErrFlowExpired
	}

	switch flow.State {
	case StateCompleted:
		return ErrFlowCompleted
	case StateFailed:
		return ErrFlowFailed
	case StateExpired:
		return ErrFlowExpired
	}

	return nil
}

// generateLoginUI creates the UI nodes for a login flow
func (s *Service) generateLoginUI(flow *Flow) *UIState {
	nodes := GenerateLoginFormNodes(flow.CSRFToken)
	return &UIState{
		Action: flow.RequestURL,
		Method: "POST",
		Nodes:  nodes,
	}
}

// generateRegistrationUI creates the UI nodes for a registration flow
func (s *Service) generateRegistrationUI(flow *Flow) *UIState {
	nodes := GenerateRegistrationFormNodes(flow.CSRFToken)
	return &UIState{
		Action: flow.RequestURL,
		Method: "POST",
		Nodes:  nodes,
	}
}

// generateRecoveryUI creates the UI nodes for a recovery flow
func (s *Service) generateRecoveryUI(flow *Flow) *UIState {
	nodes := GenerateRecoveryFormNodes(flow.CSRFToken)
	return &UIState{
		Action: flow.RequestURL,
		Method: "POST",
		Nodes:  nodes,
	}
}

// generateSettingsUI creates the UI nodes for a settings flow
func (s *Service) generateSettingsUI(flow *Flow) *UIState {
	nodes := GenerateSettingsFormNodes(flow.CSRFToken)
	return &UIState{
		Action: flow.RequestURL,
		Method: "POST",
		Nodes:  nodes,
	}
}

// generateVerificationUI creates the UI nodes for a verification flow
func (s *Service) generateVerificationUI(flow *Flow) *UIState {
	nodes := GenerateVerificationFormNodes(flow.CSRFToken)
	return &UIState{
		Action: flow.RequestURL,
		Method: "POST",
		Nodes:  nodes,
	}
}
