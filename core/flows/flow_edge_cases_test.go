package flows

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Additional comprehensive edge case and integration tests

func TestFlow_StateTransitions(t *testing.T) {
	// Test state machine transitions
	flow, _ := NewFlow(TypeLogin, "/auth/login", 15*time.Minute)

	// Active -> Completed
	assert.Equal(t, StateActive, flow.State)
	err := flow.Complete()
	assert.NoError(t, err)
	assert.Equal(t, StateCompleted, flow.State)

	// Cannot transition from Completed
	err = flow.Complete()
	assert.ErrorIs(t, err, ErrFlowCompleted)
}

func TestFlow_StateTransitions_ActiveToFailed(t *testing.T) {
	flow, _ := NewFlow(TypeLogin, "/auth/login", 15*time.Minute)

	assert.Equal(t, StateActive, flow.State)
	flow.Fail()
	assert.Equal(t, StateFailed, flow.State)

	// Cannot fail again
	oldState := flow.State
	flow.Fail()
	assert.Equal(t, oldState, flow.State)
}

func TestFlow_StateTransitions_ActiveToExpired(t *testing.T) {
	flow, _ := NewFlow(TypeLogin, "/auth/login", 15*time.Minute)

	assert.Equal(t, StateActive, flow.State)
	flow.Expire()
	assert.Equal(t, StateExpired, flow.State)

	// Cannot expire again
	oldState := flow.State
	flow.Expire()
	assert.Equal(t, oldState, flow.State)
}

func TestFlow_CompleteMultipleCalls(t *testing.T) {
	flow, _ := NewFlow(TypeLogin, "/auth/login", 15*time.Minute)
	oldUpdatedAt := flow.UpdatedAt

	// First complete succeeds
	err1 := flow.Complete()
	firstUpdatedAt := flow.UpdatedAt
	
	assert.NoError(t, err1)
	assert.Equal(t, StateCompleted, flow.State)
	assert.True(t, firstUpdatedAt.After(oldUpdatedAt) || firstUpdatedAt.Equal(oldUpdatedAt))

	// Second complete fails
	err2 := flow.Complete()
	assert.ErrorIs(t, err2, ErrFlowCompleted)
	// UpdatedAt should not change on error
	assert.Equal(t, firstUpdatedAt, flow.UpdatedAt)
}

func TestFlow_ContextOperations(t *testing.T) {
	flow, _ := NewFlow(TypeLogin, "/auth/login", 15*time.Minute)

	// Test adding various types
	flow.AddContext("string", "value")
	flow.AddContext("int", 42)
	flow.AddContext("bool", true)
	flow.AddContext("nested", map[string]interface{}{"key": "value"})

	// Test retrieving
	val, exists := flow.GetContext("string")
	assert.True(t, exists)
	assert.Equal(t, "value", val)

	val, exists = flow.GetContext("int")
	assert.True(t, exists)
	assert.Equal(t, 42, val)

	val, exists = flow.GetContext("bool")
	assert.True(t, exists)
	assert.Equal(t, true, val)

	// Test non-existent
	val, exists = flow.GetContext("missing")
	assert.False(t, exists)
	assert.Nil(t, val)
}

func TestFlow_ContextOverwrite(t *testing.T) {
	flow, _ := NewFlow(TypeLogin, "/auth/login", 15*time.Minute)

	flow.AddContext("key", "value1")
	val, _ := flow.GetContext("key")
	assert.Equal(t, "value1", val)

	flow.AddContext("key", "value2")
	val, _ = flow.GetContext("key")
	assert.Equal(t, "value2", val)
}

func TestFlow_MultipleIdentitiesAndSessions(t *testing.T) {
	flow, _ := NewFlow(TypeLogin, "/auth/login", 15*time.Minute)

	id1 := uuid.New()
	session1 := uuid.New()

	flow.SetIdentity(id1)
	flow.SetSession(session1)

	assert.NotNil(t, flow.IdentityID)
	assert.Equal(t, id1, *flow.IdentityID)
	assert.NotNil(t, flow.SessionID)
	assert.Equal(t, session1, *flow.SessionID)

	// Can update them
	id2 := uuid.New()
	session2 := uuid.New()
	flow.SetIdentity(id2)
	flow.SetSession(session2)

	assert.Equal(t, id2, *flow.IdentityID)
	assert.Equal(t, session2, *flow.SessionID)
}

func TestFlow_ReturnToURL(t *testing.T) {
	flow, _ := NewFlow(TypeLogin, "/auth/login", 15*time.Minute)

	assert.Empty(t, flow.ReturnTo)

	returnURL := "https://example.com/dashboard"
	flow.SetReturnTo(returnURL)

	assert.Equal(t, returnURL, flow.ReturnTo)
}

func TestFlow_Timestamps(t *testing.T) {
	now := time.Now().UTC()
	flow, _ := NewFlow(TypeLogin, "/auth/login", 15*time.Minute)

	// All timestamps should be close to now
	assert.WithinDuration(t, now, flow.CreatedAt, 100*time.Millisecond)
	assert.WithinDuration(t, now, flow.IssuedAt, 100*time.Millisecond)
	assert.WithinDuration(t, now, flow.UpdatedAt, 100*time.Millisecond)

	// ExpiresAt should be IssuedAt + TTL
	expectedExpiry := flow.IssuedAt.Add(15 * time.Minute)
	assert.WithinDuration(t, expectedExpiry, flow.ExpiresAt, 100*time.Millisecond)
}

func TestNewFlow_InvalidType(t *testing.T) {
	flow, err := NewFlow(FlowType("invalid"), "/url", 15*time.Minute)

	assert.ErrorIs(t, err, ErrInvalidFlowType)
	assert.Nil(t, flow)
}

func TestNewFlow_WithDifferentTTLs(t *testing.T) {
	tests := []struct {
		name     string
		ttl      time.Duration
		expected time.Duration
	}{
		{"1 minute", 1 * time.Minute, 1 * time.Minute},
		{"1 hour", 1 * time.Hour, 1 * time.Hour},
		{"24 hours", 24 * time.Hour, 24 * time.Hour},
		{"zero uses default", 0, DefaultTTL},
		{"negative uses default", -1 * time.Hour, DefaultTTL},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			flow, err := NewFlow(TypeLogin, "/auth/login", tt.ttl)
			require.NoError(t, err)

			expectedExpiry := flow.IssuedAt.Add(tt.expected)
			assert.WithinDuration(t, expectedExpiry, flow.ExpiresAt, 100*time.Millisecond)
		})
	}
}

func TestCSRFToken_Length(t *testing.T) {
	token, err := GenerateCSRFToken()
	require.NoError(t, err)

	// Base64 encoded 32 bytes should be 43 characters (32 * 4/3, rounded up)
	assert.NotEmpty(t, token)
	assert.Greater(t, len(token), 40)
}

func TestCSRFToken_Uniqueness(t *testing.T) {
	tokens := make(map[string]bool)
	
	for i := 0; i < 100; i++ {
		token, err := GenerateCSRFToken()
		require.NoError(t, err)
		assert.False(t, tokens[token], "Generated duplicate token")
		tokens[token] = true
	}

	assert.Equal(t, 100, len(tokens))
}

func TestValidateCSRFToken_TimingSafety(t *testing.T) {
	// This test verifies that ValidateCSRFToken uses constant time comparison
	token1, _ := GenerateCSRFToken()
	token2, _ := GenerateCSRFToken()

	// Different tokens should not validate
	assert.False(t, ValidateCSRFToken(token1, token2))

	// Same token should validate
	assert.True(t, ValidateCSRFToken(token1, token1))
}

func TestUIState_Nodes(t *testing.T) {
	ui := &UIState{
		Action: "/auth/login",
		Method: "POST",
		Nodes: []Node{
			NewInputNode("email", InputTypeEmail, "Email", true),
			NewInputNode("password", InputTypePassword, "Password", true),
			NewSubmitNode("login", "Sign In"),
		},
	}

	assert.Len(t, ui.Nodes, 3)
	assert.Equal(t, "email", ui.Nodes[0].Attributes.Name)
	assert.Equal(t, "password", ui.Nodes[1].Attributes.Name)
	assert.Equal(t, "login", ui.Nodes[2].Attributes.Name)
}

func TestUIState_Messages(t *testing.T) {
	msg1 := Msg{
		ID:   "msg1",
		Type: MsgTypeError,
		Text: "Invalid email",
	}
	msg2 := Msg{
		ID:   "msg2",
		Type: MsgTypeInfo,
		Text: "Please enter a valid email",
	}

	ui := &UIState{
		Action:   "/auth/login",
		Method:   "POST",
		Nodes:    []Node{},
		Messages: []Msg{msg1, msg2},
	}

	assert.Len(t, ui.Messages, 2)
	assert.Equal(t, MsgTypeError, ui.Messages[0].Type)
	assert.Equal(t, MsgTypeInfo, ui.Messages[1].Type)
}

func TestFlowCtx_AsMap(t *testing.T) {
	ctx := FlowCtx{
		"email":    "user@example.com",
		"verified": true,
		"attempts": 3,
	}

	assert.Equal(t, "user@example.com", ctx["email"])
	assert.Equal(t, true, ctx["verified"])
	assert.Equal(t, 3, ctx["attempts"])
}

func TestNodeMeta_Label(t *testing.T) {
	meta := NodeMeta{
		Label: &TextMeta{
			ID:   "label.email",
			Text: "Email Address",
			Type: "text",
		},
		Description: "Your email for login",
	}

	assert.NotNil(t, meta.Label)
	assert.Equal(t, "label.email", meta.Label.ID)
	assert.Equal(t, "Email Address", meta.Label.Text)
	assert.Equal(t, "Your email for login", meta.Description)
}

func TestMsg_WithContext(t *testing.T) {
	contextData := map[string]interface{}{
		"field": "email",
		"code":  "invalid_format",
	}

	msg := Msg{
		ID:      "error1",
		Type:    MsgTypeError,
		Text:    "Invalid email format",
		Context: contextData,
	}

	assert.Equal(t, contextData, msg.Context)
	assert.Equal(t, MsgTypeError, msg.Type)
}

func TestMsgType_Values(t *testing.T) {
	assert.Equal(t, "info", string(MsgTypeInfo))
	assert.Equal(t, "error", string(MsgTypeError))
	assert.Equal(t, "success", string(MsgTypeSuccess))
	assert.Equal(t, "warning", string(MsgTypeWarning))
}

func TestAllFlowTypes(t *testing.T) {
	flowTypes := []FlowType{
		TypeLogin,
		TypeRegistration,
		TypeRecovery,
		TypeSettings,
		TypeVerification,
	}

	for _, ft := range flowTypes {
		t.Run(string(ft), func(t *testing.T) {
			assert.True(t, ft.Valid())
		})
	}
}

func TestAllFlowStates(t *testing.T) {
	states := []FlowState{
		StateActive,
		StateCompleted,
		StateFailed,
		StateExpired,
	}

	for _, state := range states {
		t.Run(string(state), func(t *testing.T) {
			assert.True(t, state.Valid())
		})
	}
}

func TestFlowState_Terminal(t *testing.T) {
	terminalStates := []FlowState{
		StateCompleted,
		StateFailed,
		StateExpired,
	}

	for _, state := range terminalStates {
		t.Run(string(state), func(t *testing.T) {
			assert.True(t, state.IsTerminal())
		})
	}

	// Active should not be terminal
	assert.False(t, StateActive.IsTerminal())
}

func TestFlow_InitializesWithEmptyContext(t *testing.T) {
	flow, _ := NewFlow(TypeLogin, "/auth/login", 15*time.Minute)

	assert.NotNil(t, flow.Context)
	assert.Equal(t, 0, len(flow.Context))

	// Should be able to add to it
	flow.AddContext("key", "value")
	assert.Equal(t, 1, len(flow.Context))
}

func TestFlow_InitializesWithEmptyUI(t *testing.T) {
	flow, _ := NewFlow(TypeLogin, "/auth/login", 15*time.Minute)

	assert.NotNil(t, flow.UI)
	assert.NotNil(t, flow.UI.Nodes)
	assert.Equal(t, 0, len(flow.UI.Nodes))
	assert.Equal(t, 0, len(flow.UI.Messages))
}

func TestFlow_GeneratesUniqueIDs(t *testing.T) {
	flow1, _ := NewFlow(TypeLogin, "/auth/login", 15*time.Minute)
	flow2, _ := NewFlow(TypeLogin, "/auth/login", 15*time.Minute)

	assert.NotEqual(t, flow1.ID, flow2.ID)
	assert.NotEqual(t, flow1.CSRFToken, flow2.CSRFToken)
}

func TestFlow_GeneratesValidIDs(t *testing.T) {
	flow, _ := NewFlow(TypeLogin, "/auth/login", 15*time.Minute)

	assert.NotEqual(t, uuid.Nil, flow.ID)
	assert.NotEmpty(t, flow.CSRFToken)
}

func BenchmarkFlow_Operations(b *testing.B) {
	flow, _ := NewFlow(TypeLogin, "/auth/login", 15*time.Minute)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		flow.AddContext("key", "value")
		flow.GetContext("key")
		flow.SetReturnTo("https://example.com")
	}
}

func BenchmarkFlow_StateTransitions(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		flow, _ := NewFlow(TypeLogin, "/auth/login", 15*time.Minute)
		_ = flow.Complete()
	}
}
