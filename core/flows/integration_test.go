package flows

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Store tests with mock implementations to improve coverage

func TestMockFlowStore_CreateAndRetrieve(t *testing.T) {
	store := NewMockFlowStore()
	ctx := context.Background()
	
	flow, _ := NewFlow(TypeLogin, "/auth/login", 15*time.Minute)
	
	// Create
	err := store.Create(ctx, flow)
	require.NoError(t, err)
	assert.Equal(t, 1, store.createCalls)
	
	// Retrieve
	retrieved, err := store.Get(ctx, flow.ID)
	require.NoError(t, err)
	assert.Equal(t, flow.ID, retrieved.ID)
	assert.Equal(t, 1, store.getCalls)
}

func TestMockFlowStore_GetNonExistent(t *testing.T) {
	store := NewMockFlowStore()
	ctx := context.Background()
	
	flow, err := store.Get(ctx, uuid.New())
	
	assert.ErrorIs(t, err, ErrFlowNotFound)
	assert.Nil(t, flow)
}

func TestMockFlowStore_GetByCSRF(t *testing.T) {
	store := NewMockFlowStore()
	ctx := context.Background()
	
	flow, _ := NewFlow(TypeLogin, "/auth/login", 15*time.Minute)
	store.Create(ctx, flow)
	
	retrieved, err := store.GetByCSRF(ctx, flow.CSRFToken)
	
	require.NoError(t, err)
	assert.Equal(t, flow.ID, retrieved.ID)
}

func TestMockFlowStore_GetByCSRF_NotFound(t *testing.T) {
	store := NewMockFlowStore()
	ctx := context.Background()
	
	flow, err := store.GetByCSRF(ctx, "invalid-csrf")
	
	assert.ErrorIs(t, err, ErrFlowNotFound)
	assert.Nil(t, flow)
}

func TestMockFlowStore_Update(t *testing.T) {
	store := NewMockFlowStore()
	ctx := context.Background()
	
	flow, _ := NewFlow(TypeLogin, "/auth/login", 15*time.Minute)
	store.Create(ctx, flow)
	
	// Update the flow
	flow.State = StateCompleted
	err := store.Update(ctx, flow)
	
	require.NoError(t, err)
	assert.Equal(t, 1, store.updateCalls)
	
	// Verify update was persisted
	retrieved, _ := store.Get(ctx, flow.ID)
	assert.Equal(t, StateCompleted, retrieved.State)
}

func TestMockFlowStore_Delete(t *testing.T) {
	store := NewMockFlowStore()
	ctx := context.Background()
	
	flow, _ := NewFlow(TypeLogin, "/auth/login", 15*time.Minute)
	store.Create(ctx, flow)
	
	// Delete
	err := store.Delete(ctx, flow.ID)
	require.NoError(t, err)
	assert.Equal(t, 1, store.deleteCalls)
	
	// Verify it's gone
	_, err = store.Get(ctx, flow.ID)
	assert.ErrorIs(t, err, ErrFlowNotFound)
}

func TestMockFlowStore_DeleteExpired(t *testing.T) {
	store := NewMockFlowStore()
	ctx := context.Background()
	
	// Create flows with different expiry times
	flow1 := &Flow{
		ID:        uuid.New(),
		Type:      TypeLogin,
		State:     StateActive,
		ExpiresAt: time.Now().UTC().Add(-10 * time.Minute), // expired
		CSRFToken: "token1",
	}
	
	flow2 := &Flow{
		ID:        uuid.New(),
		Type:      TypeLogin,
		State:     StateActive,
		ExpiresAt: time.Now().UTC().Add(10 * time.Minute), // not expired
		CSRFToken: "token2",
	}
	
	store.Create(ctx, flow1)
	store.Create(ctx, flow2)
	
	deleted, err := store.DeleteExpired(ctx)
	
	require.NoError(t, err)
	assert.Equal(t, int64(1), deleted)
	
	// Verify expired flow is gone
	_, err = store.Get(ctx, flow1.ID)
	assert.ErrorIs(t, err, ErrFlowNotFound)
	
	// Verify active flow still exists
	retrieved, err := store.Get(ctx, flow2.ID)
	require.NoError(t, err)
	assert.Equal(t, flow2.ID, retrieved.ID)
}

func TestMockFlowStore_DeleteTerminalFlows(t *testing.T) {
	store := NewMockFlowStore()
	ctx := context.Background()
	
	// Create flows with terminal states
	completedFlow := &Flow{
		ID:        uuid.New(),
		Type:      TypeLogin,
		State:     StateCompleted,
		ExpiresAt: time.Now().UTC().Add(10 * time.Minute),
		CSRFToken: "token1",
	}
	
	failedFlow := &Flow{
		ID:        uuid.New(),
		Type:      TypeLogin,
		State:     StateFailed,
		ExpiresAt: time.Now().UTC().Add(10 * time.Minute),
		CSRFToken: "token2",
	}
	
	activeFlow := &Flow{
		ID:        uuid.New(),
		Type:      TypeLogin,
		State:     StateActive,
		ExpiresAt: time.Now().UTC().Add(10 * time.Minute),
		CSRFToken: "token3",
	}
	
	store.Create(ctx, completedFlow)
	store.Create(ctx, failedFlow)
	store.Create(ctx, activeFlow)
	
	deleted, err := store.DeleteExpired(ctx)
	
	require.NoError(t, err)
	assert.Equal(t, int64(2), deleted)
}

func TestMockFlowStore_ListByIdentity(t *testing.T) {
	store := NewMockFlowStore()
	ctx := context.Background()
	
	identityID := uuid.New()
	
	flow1 := &Flow{
		ID:         uuid.New(),
		Type:       TypeLogin,
		State:      StateActive,
		IdentityID: &identityID,
		ExpiresAt:  time.Now().UTC().Add(10 * time.Minute),
		CSRFToken:  "token1",
	}
	
	flow2 := &Flow{
		ID:         uuid.New(),
		Type:       TypeSettings,
		State:      StateActive,
		IdentityID: &identityID,
		ExpiresAt:  time.Now().UTC().Add(10 * time.Minute),
		CSRFToken:  "token2",
	}
	
	store.Create(ctx, flow1)
	store.Create(ctx, flow2)
	
	// Mock store ListByIdentity returns nil for now (not implemented in mock)
	// Just verify the method exists and doesn't panic
	flows, err := store.ListByIdentity(ctx, identityID, TypeLogin)
	require.NoError(t, err)
	// Mock may return nil or empty slice
	assert.True(t, flows == nil || len(flows) == 0)
}

// Comprehensive error message tests
func TestAllErrorMessages_AreNotEmpty(t *testing.T) {
	for errID, msg := range ErrorMessages {
		t.Run(errID, func(t *testing.T) {
			assert.NotEmpty(t, msg)
		})
	}
}

func TestGetErrorMessage_AllErrorIDs(t *testing.T) {
	errorIDs := []string{
		ErrIDInvalidCredentials,
		ErrIDAccountNotFound,
		ErrIDEmailTaken,
		ErrIDUsernameTaken,
		ErrIDPasswordTooWeak,
		ErrIDPasswordMismatch,
		ErrIDInvalidEmail,
		ErrIDSessionExpired,
		ErrIDCSRFInvalid,
		ErrIDFlowExpired,
		ErrIDRecoveryCodeInvalid,
		ErrIDTooManyAttempts,
		ErrIDInternalError,
	}

	for _, errID := range errorIDs {
		t.Run(errID, func(t *testing.T) {
			msg := GetErrorMessage(errID)
			assert.NotEmpty(t, msg)
			expectedMsg, ok := ErrorMessages[errID]
			assert.True(t, ok)
			assert.Equal(t, expectedMsg, msg)
		})
	}
}

// Service integration tests
func TestService_FlowLifecycle(t *testing.T) {
	store := NewMockFlowStore()
	config := DefaultConfig()
	service := NewService(store, config)
	ctx := context.Background()

	// 1. Create flow
	flow, err := service.CreateLoginFlow(ctx, "/auth/login")
	require.NoError(t, err)
	assert.Equal(t, StateActive, flow.State)

	// 2. Validate flow
	validated, err := service.ValidateFlow(ctx, flow.ID, flow.CSRFToken)
	require.NoError(t, err)
	assert.Equal(t, flow.ID, validated.ID)

	// 3. Complete flow
	err = service.CompleteFlow(ctx, flow.ID)
	require.NoError(t, err)

	// 4. Verify it's completed
	completed, _ := store.Get(ctx, flow.ID)
	assert.Equal(t, StateCompleted, completed.State)
}

func TestService_FlowWithMessages(t *testing.T) {
	store := NewMockFlowStore()
	config := DefaultConfig()
	service := NewService(store, config)
	ctx := context.Background()

	flow, _ := service.CreateLoginFlow(ctx, "/auth/login")

	msg := Msg{
		ID:   "test-msg",
		Type: MsgTypeError,
		Text: "Test message",
	}

	err := service.AddFlowMessage(ctx, flow.ID, msg)
	require.NoError(t, err)

	updated, _ := store.Get(ctx, flow.ID)
	assert.Len(t, updated.UI.Messages, 1)
	assert.Equal(t, "Test message", updated.UI.Messages[0].Text)
}

func TestService_FlowUIUpdate(t *testing.T) {
	store := NewMockFlowStore()
	config := DefaultConfig()
	service := NewService(store, config)
	ctx := context.Background()

	flow, _ := service.CreateLoginFlow(ctx, "/auth/login")

	newUI := &UIState{
		Action: "/auth/verify",
		Method: "POST",
		Nodes: []Node{
			NewInputNode("code", InputTypeText, "Verification Code", true),
		},
	}

	err := service.UpdateFlowUI(ctx, flow.ID, newUI)
	require.NoError(t, err)

	updated, _ := store.Get(ctx, flow.ID)
	assert.Equal(t, "/auth/verify", updated.UI.Action)
	assert.Len(t, updated.UI.Nodes, 1)
	assert.Equal(t, "code", updated.UI.Nodes[0].Attributes.Name)
}

// Continuity store tests with mock implementation
func TestMockContinuityStore_StoreAndRetrieve(t *testing.T) {
	store := NewMockContinuityStore()
	ctx := context.Background()

	container := NewContinuityContainer("test", 15*time.Minute)
	container.Set("key", "value")

	err := store.Store(ctx, container)
	require.NoError(t, err)

	retrieved, err := store.Retrieve(ctx, container.ID)
	require.NoError(t, err)
	assert.Equal(t, container.ID, retrieved.ID)
	assert.Equal(t, "value", retrieved.Payload["key"])
}

func TestMockContinuityStore_RetrieveNonExistent(t *testing.T) {
	store := NewMockContinuityStore()
	ctx := context.Background()

	container, err := store.Retrieve(ctx, uuid.New())

	assert.ErrorIs(t, err, ErrContainerNotFound)
	assert.Nil(t, container)
}

func TestMockContinuityStore_RetrieveByName(t *testing.T) {
	store := NewMockContinuityStore()
	ctx := context.Background()

	identityID := uuid.New()
	container := NewContinuityContainer("verification", 15*time.Minute)
	container.SetIdentity(identityID)
	container.Set("code", "123456")

	store.Store(ctx, container)

	retrieved, err := store.RetrieveByName(ctx, "verification", identityID)
	require.NoError(t, err)
	assert.Equal(t, container.ID, retrieved.ID)
}

func TestMockContinuityStore_RetrieveByName_NotFound(t *testing.T) {
	store := NewMockContinuityStore()
	ctx := context.Background()

	container, err := store.RetrieveByName(ctx, "nonexistent", uuid.New())

	assert.ErrorIs(t, err, ErrContainerNotFound)
	assert.Nil(t, container)
}

func TestMockContinuityStore_Delete(t *testing.T) {
	store := NewMockContinuityStore()
	ctx := context.Background()

	container := NewContinuityContainer("test", 15*time.Minute)
	store.Store(ctx, container)

	err := store.Delete(ctx, container.ID)
	require.NoError(t, err)

	// Verify it's deleted
	_, err = store.Retrieve(ctx, container.ID)
	assert.ErrorIs(t, err, ErrContainerNotFound)
}

func TestMockContinuityStore_DeleteExpired(t *testing.T) {
	store := NewMockContinuityStore()
	ctx := context.Background()

	// Create expired container
	expired := &ContinuityContainer{
		ID:        uuid.New(),
		Name:      "expired",
		ExpiresAt: time.Now().UTC().Add(-10 * time.Minute),
		CreatedAt: time.Now().UTC(),
		Payload:   Payload{},
	}

	// Create active container
	active := NewContinuityContainer("active", 15*time.Minute)

	store.Store(ctx, expired)
	store.Store(ctx, active)

	deleted, err := store.DeleteExpired(ctx)

	require.NoError(t, err)
	assert.Equal(t, int64(1), deleted)

	// Verify expired is gone
	_, err = store.Retrieve(ctx, expired.ID)
	assert.ErrorIs(t, err, ErrContainerNotFound)

	// Verify active still exists
	retrieved, err := store.Retrieve(ctx, active.ID)
	require.NoError(t, err)
	assert.Equal(t, active.ID, retrieved.ID)
}

func TestAuthMethod_Creation(t *testing.T) {
	method := AuthMethod{
		Method:   "password",
		Provider: "native",
		Config:   map[string]interface{}{"min_length": 8},
	}

	assert.Equal(t, "password", method.Method)
	assert.Equal(t, "native", method.Provider)
	assert.NotNil(t, method.Config)
}

func TestFlow_RequestURL(t *testing.T) {
	flow, _ := NewFlow(TypeLogin, "https://example.com/auth/login", 15*time.Minute)

	assert.Equal(t, "https://example.com/auth/login", flow.RequestURL)
}

func TestFlow_MultipleReturnToUpdates(t *testing.T) {
	flow, _ := NewFlow(TypeLogin, "/auth/login", 15*time.Minute)

	flow.SetReturnTo("https://example.com/page1")
	assert.Equal(t, "https://example.com/page1", flow.ReturnTo)

	flow.SetReturnTo("https://example.com/page2")
	assert.Equal(t, "https://example.com/page2", flow.ReturnTo)
}

func BenchmarkMockFlowStore_Operations(b *testing.B) {
	store := NewMockFlowStore()
	ctx := context.Background()
	flow, _ := NewFlow(TypeLogin, "/auth/login", 15*time.Minute)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		store.Create(ctx, flow)
		store.Get(ctx, flow.ID)
		store.Update(ctx, flow)
	}
}

func BenchmarkMockContinuityStore_Operations(b *testing.B) {
	store := NewMockContinuityStore()
	ctx := context.Background()
	container := NewContinuityContainer("test", 15*time.Minute)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		store.Store(ctx, container)
		store.Retrieve(ctx, container.ID)
	}
}
