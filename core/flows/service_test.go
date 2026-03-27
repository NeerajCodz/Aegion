package flows

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// MockFlowStore is a mock implementation of FlowStore for testing
type MockFlowStore struct {
	flows          map[uuid.UUID]*Flow
	flowsByCSRF    map[string]*Flow
	flowsByIdentity map[string][]*Flow
	createCalls    int
	getCalls       int
	updateCalls    int
	deleteCalls    int
}

func NewMockFlowStore() *MockFlowStore {
	return &MockFlowStore{
		flows:          make(map[uuid.UUID]*Flow),
		flowsByCSRF:    make(map[string]*Flow),
		flowsByIdentity: make(map[string][]*Flow),
	}
}

func (m *MockFlowStore) Create(ctx context.Context, flow *Flow) error {
	m.createCalls++
	m.flows[flow.ID] = flow
	m.flowsByCSRF[flow.CSRFToken] = flow
	return nil
}

func (m *MockFlowStore) Get(ctx context.Context, id uuid.UUID) (*Flow, error) {
	m.getCalls++
	if flow, ok := m.flows[id]; ok {
		return flow, nil
	}
	return nil, ErrFlowNotFound
}

func (m *MockFlowStore) GetByCSRF(ctx context.Context, csrfToken string) (*Flow, error) {
	if flow, ok := m.flowsByCSRF[csrfToken]; ok {
		return flow, nil
	}
	return nil, ErrFlowNotFound
}

func (m *MockFlowStore) Update(ctx context.Context, flow *Flow) error {
	m.updateCalls++
	m.flows[flow.ID] = flow
	return nil
}

func (m *MockFlowStore) Delete(ctx context.Context, id uuid.UUID) error {
	m.deleteCalls++
	delete(m.flows, id)
	return nil
}

func (m *MockFlowStore) DeleteExpired(ctx context.Context) (int64, error) {
	count := int64(0)
	for id, flow := range m.flows {
		if flow.IsExpired() || flow.State.IsTerminal() {
			delete(m.flows, id)
			count++
		}
	}
	return count, nil
}

func (m *MockFlowStore) ListByIdentity(ctx context.Context, identityID uuid.UUID, flowType FlowType) ([]*Flow, error) {
	key := identityID.String() + ":" + string(flowType)
	return m.flowsByIdentity[key], nil
}

func TestNewService(t *testing.T) {
	store := NewMockFlowStore()
	config := DefaultConfig()

	service := NewService(store, config)

	assert.NotNil(t, service)
	assert.Equal(t, 15*time.Minute, config.LoginTTL)
	assert.Equal(t, 30*time.Minute, config.RegistrationTTL)
}

func TestNewService_DefaultsZeroTTL(t *testing.T) {
	store := NewMockFlowStore()
	config := Config{}

	service := NewService(store, config)

	assert.NotNil(t, service)
	// The service will use the defaults internally
	// We can verify this by creating a flow and checking its expiry
	ctx := context.Background()
	flow, _ := service.CreateLoginFlow(ctx, "/auth/login")
	
	// Flow should have been created with default TTL
	assert.NotNil(t, flow)
	now := time.Now().UTC()
	expectedExpiry := now.Add(DefaultTTL)
	// Allow 1 second margin
	assert.True(t, flow.ExpiresAt.After(expectedExpiry.Add(-1*time.Second)))
	assert.True(t, flow.ExpiresAt.Before(expectedExpiry.Add(1*time.Second)))
}

func TestService_CreateLoginFlow(t *testing.T) {
	store := NewMockFlowStore()
	config := DefaultConfig()
	service := NewService(store, config)

	ctx := context.Background()
	flow, err := service.CreateLoginFlow(ctx, "/auth/login")

	require.NoError(t, err)
	assert.NotNil(t, flow)
	assert.Equal(t, TypeLogin, flow.Type)
	assert.Equal(t, StateActive, flow.State)
	assert.NotNil(t, flow.UI)
	assert.Greater(t, len(flow.UI.Nodes), 0)
	assert.Equal(t, 1, store.createCalls)
}

func TestService_CreateRegistrationFlow(t *testing.T) {
	store := NewMockFlowStore()
	config := DefaultConfig()
	service := NewService(store, config)

	ctx := context.Background()
	flow, err := service.CreateRegistrationFlow(ctx, "/auth/register")

	require.NoError(t, err)
	assert.NotNil(t, flow)
	assert.Equal(t, TypeRegistration, flow.Type)
	assert.Equal(t, StateActive, flow.State)
	assert.NotNil(t, flow.UI)
	assert.Greater(t, len(flow.UI.Nodes), 0)
}

func TestService_CreateRecoveryFlow(t *testing.T) {
	store := NewMockFlowStore()
	config := DefaultConfig()
	service := NewService(store, config)

	ctx := context.Background()
	flow, err := service.CreateRecoveryFlow(ctx, "/auth/recovery")

	require.NoError(t, err)
	assert.NotNil(t, flow)
	assert.Equal(t, TypeRecovery, flow.Type)
	assert.Equal(t, StateActive, flow.State)
}

func TestService_CreateSettingsFlow(t *testing.T) {
	store := NewMockFlowStore()
	config := DefaultConfig()
	service := NewService(store, config)

	ctx := context.Background()
	identityID := uuid.New()
	flow, err := service.CreateSettingsFlow(ctx, "/settings", identityID)

	require.NoError(t, err)
	assert.NotNil(t, flow)
	assert.Equal(t, TypeSettings, flow.Type)
	assert.Equal(t, StateActive, flow.State)
	assert.NotNil(t, flow.IdentityID)
	assert.Equal(t, identityID, *flow.IdentityID)
}

func TestService_CreateVerificationFlow_WithIdentity(t *testing.T) {
	store := NewMockFlowStore()
	config := DefaultConfig()
	service := NewService(store, config)

	ctx := context.Background()
	identityID := uuid.New()
	flow, err := service.CreateVerificationFlow(ctx, "/verify", &identityID)

	require.NoError(t, err)
	assert.NotNil(t, flow)
	assert.Equal(t, TypeVerification, flow.Type)
	assert.NotNil(t, flow.IdentityID)
	assert.Equal(t, identityID, *flow.IdentityID)
}

func TestService_CreateVerificationFlow_WithoutIdentity(t *testing.T) {
	store := NewMockFlowStore()
	config := DefaultConfig()
	service := NewService(store, config)

	ctx := context.Background()
	flow, err := service.CreateVerificationFlow(ctx, "/verify", nil)

	require.NoError(t, err)
	assert.NotNil(t, flow)
	assert.Equal(t, TypeVerification, flow.Type)
	assert.Nil(t, flow.IdentityID)
}

func TestService_GetFlow(t *testing.T) {
	store := NewMockFlowStore()
	config := DefaultConfig()
	service := NewService(store, config)

	ctx := context.Background()
	createdFlow, _ := service.CreateLoginFlow(ctx, "/auth/login")

	flow, err := service.GetFlow(ctx, createdFlow.ID)

	require.NoError(t, err)
	assert.NotNil(t, flow)
	assert.Equal(t, createdFlow.ID, flow.ID)
	assert.Equal(t, TypeLogin, flow.Type)
}

func TestService_GetFlow_NotFound(t *testing.T) {
	store := NewMockFlowStore()
	config := DefaultConfig()
	service := NewService(store, config)

	ctx := context.Background()
	flow, err := service.GetFlow(ctx, uuid.New())

	assert.ErrorIs(t, err, ErrFlowNotFound)
	assert.Nil(t, flow)
}

func TestService_GetFlow_Expired(t *testing.T) {
	store := NewMockFlowStore()
	config := DefaultConfig()
	config.LoginTTL = 1 * time.Millisecond
	service := NewService(store, config)

	ctx := context.Background()
	createdFlow, _ := service.CreateLoginFlow(ctx, "/auth/login")

	// Wait for flow to expire
	time.Sleep(10 * time.Millisecond)

	flow, err := service.GetFlow(ctx, createdFlow.ID)

	assert.ErrorIs(t, err, ErrFlowExpired)
	assert.Nil(t, flow)
	// Flow state should be marked as expired
	storedFlow, _ := store.Get(ctx, createdFlow.ID)
	assert.Equal(t, StateExpired, storedFlow.State)
}

func TestService_ValidateFlow(t *testing.T) {
	store := NewMockFlowStore()
	config := DefaultConfig()
	service := NewService(store, config)

	ctx := context.Background()
	createdFlow, _ := service.CreateLoginFlow(ctx, "/auth/login")

	retrievedFlow, err := service.ValidateFlow(ctx, createdFlow.ID, createdFlow.CSRFToken)

	require.NoError(t, err)
	assert.NotNil(t, retrievedFlow)
	assert.Equal(t, createdFlow.ID, retrievedFlow.ID)
}

func TestService_ValidateFlow_InvalidCSRF(t *testing.T) {
	store := NewMockFlowStore()
	config := DefaultConfig()
	service := NewService(store, config)

	ctx := context.Background()
	createdFlow, _ := service.CreateLoginFlow(ctx, "/auth/login")

	flow, err := service.ValidateFlow(ctx, createdFlow.ID, "invalid-csrf")

	assert.ErrorIs(t, err, ErrInvalidCSRF)
	assert.Nil(t, flow)
}

func TestService_ValidateFlowByCSRF(t *testing.T) {
	store := NewMockFlowStore()
	config := DefaultConfig()
	service := NewService(store, config)

	ctx := context.Background()
	createdFlow, _ := service.CreateLoginFlow(ctx, "/auth/login")

	flow, err := service.ValidateFlowByCSRF(ctx, createdFlow.CSRFToken)

	require.NoError(t, err)
	assert.NotNil(t, flow)
	assert.Equal(t, createdFlow.ID, flow.ID)
}

func TestService_ValidateFlowByCSRF_NotFound(t *testing.T) {
	store := NewMockFlowStore()
	config := DefaultConfig()
	service := NewService(store, config)

	ctx := context.Background()
	flow, err := service.ValidateFlowByCSRF(ctx, "invalid-csrf-token")

	assert.ErrorIs(t, err, ErrFlowNotFound)
	assert.Nil(t, flow)
}

func TestService_CompleteFlow(t *testing.T) {
	store := NewMockFlowStore()
	config := DefaultConfig()
	service := NewService(store, config)

	ctx := context.Background()
	createdFlow, _ := service.CreateLoginFlow(ctx, "/auth/login")

	err := service.CompleteFlow(ctx, createdFlow.ID)

	require.NoError(t, err)
	updatedFlow, _ := store.Get(ctx, createdFlow.ID)
	assert.Equal(t, StateCompleted, updatedFlow.State)
}

func TestService_CompleteFlow_NotFound(t *testing.T) {
	store := NewMockFlowStore()
	config := DefaultConfig()
	service := NewService(store, config)

	ctx := context.Background()
	err := service.CompleteFlow(ctx, uuid.New())

	assert.ErrorIs(t, err, ErrFlowNotFound)
}

func TestService_FailFlow(t *testing.T) {
	store := NewMockFlowStore()
	config := DefaultConfig()
	service := NewService(store, config)

	ctx := context.Background()
	createdFlow, _ := service.CreateLoginFlow(ctx, "/auth/login")

	err := service.FailFlow(ctx, createdFlow.ID, "Authentication failed")

	require.NoError(t, err)
	updatedFlow, _ := store.Get(ctx, createdFlow.ID)
	assert.Equal(t, StateFailed, updatedFlow.State)
	assert.Len(t, updatedFlow.UI.Messages, 1)
	assert.Equal(t, "Authentication failed", updatedFlow.UI.Messages[0].Text)
}

func TestService_FailFlow_EmptyMessage(t *testing.T) {
	store := NewMockFlowStore()
	config := DefaultConfig()
	service := NewService(store, config)

	ctx := context.Background()
	createdFlow, _ := service.CreateLoginFlow(ctx, "/auth/login")

	err := service.FailFlow(ctx, createdFlow.ID, "")

	require.NoError(t, err)
	updatedFlow, _ := store.Get(ctx, createdFlow.ID)
	assert.Equal(t, StateFailed, updatedFlow.State)
	assert.Len(t, updatedFlow.UI.Messages, 0)
}

func TestService_UpdateFlowUI(t *testing.T) {
	store := NewMockFlowStore()
	config := DefaultConfig()
	service := NewService(store, config)

	ctx := context.Background()
	createdFlow, _ := service.CreateLoginFlow(ctx, "/auth/login")

	newUI := &UIState{
		Action: "/auth/login",
		Method: "POST",
		Nodes: []Node{
			NewInputNode("email", InputTypeEmail, "Email", true),
		},
	}

	err := service.UpdateFlowUI(ctx, createdFlow.ID, newUI)

	require.NoError(t, err)
	updatedFlow, _ := store.Get(ctx, createdFlow.ID)
	assert.Len(t, updatedFlow.UI.Nodes, 1)
	assert.Equal(t, "email", updatedFlow.UI.Nodes[0].Attributes.Name)
}

func TestService_AddFlowMessage(t *testing.T) {
	store := NewMockFlowStore()
	config := DefaultConfig()
	service := NewService(store, config)

	ctx := context.Background()
	createdFlow, _ := service.CreateLoginFlow(ctx, "/auth/login")

	msg := Msg{
		ID:   "info-1",
		Type: MsgTypeInfo,
		Text: "Please verify your email",
	}

	err := service.AddFlowMessage(ctx, createdFlow.ID, msg)

	require.NoError(t, err)
	updatedFlow, _ := store.Get(ctx, createdFlow.ID)
	assert.Len(t, updatedFlow.UI.Messages, 1)
	assert.Equal(t, "Please verify your email", updatedFlow.UI.Messages[0].Text)
}

func TestService_GetFlowMethods_Login(t *testing.T) {
	store := NewMockFlowStore()
	config := DefaultConfig()
	service := NewService(store, config)

	methods := service.GetFlowMethods(TypeLogin)

	assert.NotEmpty(t, methods)
	assert.Equal(t, "password", methods[0].Method)
}

func TestService_GetFlowMethods_Registration(t *testing.T) {
	store := NewMockFlowStore()
	config := DefaultConfig()
	service := NewService(store, config)

	methods := service.GetFlowMethods(TypeRegistration)

	assert.NotEmpty(t, methods)
	assert.Equal(t, "password", methods[0].Method)
}

func TestService_GetFlowMethods_Recovery(t *testing.T) {
	store := NewMockFlowStore()
	config := DefaultConfig()
	service := NewService(store, config)

	methods := service.GetFlowMethods(TypeRecovery)

	assert.Len(t, methods, 2)
	assert.Equal(t, "link", methods[0].Method)
	assert.Equal(t, "code", methods[1].Method)
}

func TestService_GetFlowMethods_Settings(t *testing.T) {
	store := NewMockFlowStore()
	config := DefaultConfig()
	service := NewService(store, config)

	methods := service.GetFlowMethods(TypeSettings)

	assert.Len(t, methods, 2)
	assert.Equal(t, "password", methods[0].Method)
	assert.Equal(t, "profile", methods[1].Method)
}

func TestService_GetFlowMethods_Verification(t *testing.T) {
	store := NewMockFlowStore()
	config := DefaultConfig()
	service := NewService(store, config)

	methods := service.GetFlowMethods(TypeVerification)

	assert.Len(t, methods, 2)
	assert.Equal(t, "link", methods[0].Method)
	assert.Equal(t, "code", methods[1].Method)
}

func TestService_GetFlowMethods_Invalid(t *testing.T) {
	store := NewMockFlowStore()
	config := DefaultConfig()
	service := NewService(store, config)

	methods := service.GetFlowMethods(FlowType("invalid"))

	assert.Nil(t, methods)
}

func TestService_Cleanup(t *testing.T) {
	store := NewMockFlowStore()
	config := Config{
		LoginTTL:        1 * time.Millisecond,
		RegistrationTTL: DefaultTTL,
		RecoveryTTL:     DefaultTTL,
		SettingsTTL:     DefaultTTL,
		VerificationTTL: DefaultTTL,
	}
	service := NewService(store, config)

	ctx := context.Background()
	
	// Create a flow that will expire
	_, _ = service.CreateLoginFlow(ctx, "/auth/login")

	// Wait for expiry
	time.Sleep(10 * time.Millisecond)

	// Cleanup should remove expired flows
	deleted, err := service.Cleanup(ctx)

	require.NoError(t, err)
	assert.Greater(t, deleted, int64(0))
}

func TestService_CreateFlowUI_Login(t *testing.T) {
	store := NewMockFlowStore()
	config := DefaultConfig()
	service := NewService(store, config)

	ctx := context.Background()
	flow, _ := service.CreateLoginFlow(ctx, "/auth/login")

	assert.Equal(t, "/auth/login", flow.UI.Action)
	assert.Equal(t, "POST", flow.UI.Method)
	assert.NotEmpty(t, flow.UI.Nodes)
}

func TestService_CreateFlowUI_Registration(t *testing.T) {
	store := NewMockFlowStore()
	config := DefaultConfig()
	service := NewService(store, config)

	ctx := context.Background()
	flow, _ := service.CreateRegistrationFlow(ctx, "/auth/register")

	assert.Equal(t, "/auth/register", flow.UI.Action)
	assert.Equal(t, "POST", flow.UI.Method)
	assert.NotEmpty(t, flow.UI.Nodes)
}

func TestService_CreateFlowUI_Settings(t *testing.T) {
	store := NewMockFlowStore()
	config := DefaultConfig()
	service := NewService(store, config)

	ctx := context.Background()
	identityID := uuid.New()
	flow, _ := service.CreateSettingsFlow(ctx, "/settings", identityID)

	assert.Equal(t, "/settings", flow.UI.Action)
	assert.Equal(t, "POST", flow.UI.Method)
	assert.NotEmpty(t, flow.UI.Nodes)
}

func TestDefaultConfig(t *testing.T) {
	config := DefaultConfig()

	assert.Equal(t, 15*time.Minute, config.LoginTTL)
	assert.Equal(t, 30*time.Minute, config.RegistrationTTL)
	assert.Equal(t, 15*time.Minute, config.RecoveryTTL)
	assert.Equal(t, 30*time.Minute, config.SettingsTTL)
	assert.Equal(t, 24*time.Hour, config.VerificationTTL)
	assert.NotEmpty(t, config.DefaultMethods)
	assert.Equal(t, "password", config.DefaultMethods[0].Method)
}

func BenchmarkService_CreateLoginFlow(b *testing.B) {
	store := NewMockFlowStore()
	config := DefaultConfig()
	service := NewService(store, config)
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		service.CreateLoginFlow(ctx, "/auth/login")
	}
}

func BenchmarkService_ValidateFlow(b *testing.B) {
	store := NewMockFlowStore()
	config := DefaultConfig()
	service := NewService(store, config)
	ctx := context.Background()

	flow, _ := service.CreateLoginFlow(ctx, "/auth/login")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		service.ValidateFlow(ctx, flow.ID, flow.CSRFToken)
	}
}

func BenchmarkService_GetFlowMethods(b *testing.B) {
	store := NewMockFlowStore()
	config := DefaultConfig()
	service := NewService(store, config)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		service.GetFlowMethods(TypeLogin)
	}
}
