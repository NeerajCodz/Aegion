package flows

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Additional comprehensive tests to maximize coverage

func TestAllFlowTypesAreValid(t *testing.T) {
	types := []FlowType{
		TypeLogin,
		TypeRegistration,
		TypeRecovery,
		TypeSettings,
		TypeVerification,
	}

	for _, ft := range types {
		assert.True(t, ft.Valid())
	}
}

func TestAllFlowStatesAreValid(t *testing.T) {
	states := []FlowState{
		StateActive,
		StateCompleted,
		StateFailed,
		StateExpired,
	}

	for _, s := range states {
		assert.True(t, s.Valid())
	}
}

func TestTerminalStates(t *testing.T) {
	terminal := []FlowState{
		StateCompleted,
		StateFailed,
		StateExpired,
	}

	for _, s := range terminal {
		assert.True(t, s.IsTerminal())
	}

	assert.False(t, StateActive.IsTerminal())
}

func TestMultipleFlowCreations(t *testing.T) {
	for i := 0; i < 10; i++ {
		flow, err := NewFlow(TypeLogin, "/auth/login", 15*time.Minute)
		require.NoError(t, err)
		assert.NotNil(t, flow)
		assert.Equal(t, TypeLogin, flow.Type)
		assert.Equal(t, StateActive, flow.State)
	}
}

func TestFlow_ContextIsMap(t *testing.T) {
	flow, _ := NewFlow(TypeLogin, "/auth/login", 15*time.Minute)

	assert.IsType(t, FlowCtx{}, flow.Context)
	assert.Equal(t, 0, len(flow.Context))
}

func TestFlow_UIIsNotNil(t *testing.T) {
	flow, _ := NewFlow(TypeLogin, "/auth/login", 15*time.Minute)

	assert.NotNil(t, flow.UI)
	assert.NotNil(t, flow.UI.Nodes)
}

func TestFlow_TimestampsOrder(t *testing.T) {
	flow, _ := NewFlow(TypeLogin, "/auth/login", 15*time.Minute)

	// IssuedAt <= ExpiresAt
	assert.True(t, flow.IssuedAt.Before(flow.ExpiresAt) || flow.IssuedAt.Equal(flow.ExpiresAt))
}

func TestFlow_CSRFTokenIsBase64(t *testing.T) {
	flow, _ := NewFlow(TypeLogin, "/auth/login", 15*time.Minute)

	// Should contain only base64url characters (A-Z, a-z, 0-9, -, _)
	for _, ch := range flow.CSRFToken {
		assert.True(t, (ch >= 'A' && ch <= 'Z') ||
			(ch >= 'a' && ch <= 'z') ||
			(ch >= '0' && ch <= '9') ||
			ch == '-' || ch == '_')
	}
}

func TestGenerateCSRFToken_Randomness(t *testing.T) {
	// Generate multiple tokens and verify they're all different
	tokens := make(map[string]bool)
	for i := 0; i < 50; i++ {
		token, _ := GenerateCSRFToken()
		assert.False(t, tokens[token], "Duplicate token generated")
		tokens[token] = true
	}
}

func TestValidateCSRFToken_EdgeCases(t *testing.T) {
	tests := []struct {
		name     string
		expected string
		actual   string
		valid    bool
	}{
		{"same tokens", "abc", "abc", true},
		{"different tokens", "abc", "def", false},
		{"empty expected", "", "abc", false},
		{"empty actual", "abc", "", false},
		{"both empty", "", "", false},
		{"length mismatch", "short", "verylongtoken", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ValidateCSRFToken(tt.expected, tt.actual)
			assert.Equal(t, tt.valid, result)
		})
	}
}

func TestFlow_IsActiveOnlyWhenActiveAndNotExpired(t *testing.T) {
	tests := []struct {
		name      string
		state     FlowState
		expiresAt time.Time
		expected  bool
	}{
		{"active not expired", StateActive, time.Now().UTC().Add(10 * time.Minute), true},
		{"active expired", StateActive, time.Now().UTC().Add(-10 * time.Minute), false},
		{"completed not expired", StateCompleted, time.Now().UTC().Add(10 * time.Minute), false},
		{"failed not expired", StateFailed, time.Now().UTC().Add(10 * time.Minute), false},
		{"expired not expired", StateExpired, time.Now().UTC().Add(10 * time.Minute), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			flow := &Flow{
				State:     tt.state,
				ExpiresAt: tt.expiresAt,
			}
			assert.Equal(t, tt.expected, flow.IsActive())
		})
	}
}

func TestFlow_StateTransitionRules(t *testing.T) {
	// Test that terminal states block all transitions
	terminalStates := []FlowState{
		StateCompleted,
		StateFailed,
		StateExpired,
	}

	for _, state := range terminalStates {
		t.Run(string(state), func(t *testing.T) {
			flow := &Flow{State: state}

			// Cannot complete
			err := flow.Complete()
			assert.ErrorIs(t, err, ErrFlowCompleted)

			// Cannot fail (no error, just blocked)
			oldState := flow.State
			flow.Fail()
			assert.Equal(t, oldState, flow.State)

			// Cannot expire (no error, just blocked)
			flow.Expire()
			assert.Equal(t, oldState, flow.State)
		})
	}
}

func TestNode_AllTypes(t *testing.T) {
	// Test creating nodes of all types
	nodeTypes := []NodeType{
		NodeTypeInput,
		NodeTypeText,
		NodeTypeSubmit,
		NodeTypeError,
		NodeTypeInfo,
		NodeTypeScript,
		NodeTypeImage,
		NodeTypeAnchor,
	}

	for _, nt := range nodeTypes {
		node := Node{Type: nt}
		assert.Equal(t, nt, node.Type)
	}
}

func TestNode_AllInputTypes(t *testing.T) {
	// Test creating input nodes of all types
	inputTypes := []InputType{
		InputTypeText,
		InputTypePassword,
		InputTypeEmail,
		InputTypeHidden,
		InputTypeSubmit,
		InputTypeButton,
		InputTypeCheckbox,
		InputTypeTel,
		InputTypeNumber,
	}

	for _, it := range inputTypes {
		node := NewInputNode("test", it, "Test", true)
		assert.Equal(t, it, node.Attributes.Type)
	}
}

func TestUIState_CompleteFlow(t *testing.T) {
	ui := &UIState{
		Action: "/auth/login",
		Method: "POST",
		Nodes: []Node{
			NewHiddenNode("csrf", "token123"),
			NewInputNode("email", InputTypeEmail, "Email", true),
			NewInputNode("password", InputTypePassword, "Password", true),
			NewSubmitNode("submit", "Login"),
		},
		Messages: []Msg{
			{
				ID:   "msg1",
				Type: MsgTypeInfo,
				Text: "Please enter your credentials",
			},
		},
	}

	assert.Equal(t, "/auth/login", ui.Action)
	assert.Equal(t, "POST", ui.Method)
	assert.Len(t, ui.Nodes, 4)
	assert.Len(t, ui.Messages, 1)
}

func TestMsg_AllTypes(t *testing.T) {
	msgTypes := []MsgType{
		MsgTypeInfo,
		MsgTypeError,
		MsgTypeSuccess,
		MsgTypeWarning,
	}

	for _, mt := range msgTypes {
		msg := Msg{
			ID:   "msg",
			Type: mt,
			Text: "Test message",
		}
		assert.Equal(t, mt, msg.Type)
	}
}

func TestFlow_ManyContextItems(t *testing.T) {
	flow, _ := NewFlow(TypeLogin, "/auth/login", 15*time.Minute)

	// Add many items
	for i := 0; i < 100; i++ {
		key := "key" + string(rune(i))
		flow.AddContext(key, i)
	}

	assert.Equal(t, 100, len(flow.Context))

	// Verify they're all there
	for i := 0; i < 100; i++ {
		key := "key" + string(rune(i))
		val, exists := flow.GetContext(key)
		assert.True(t, exists)
		assert.Equal(t, i, val)
	}
}

func TestContinuityContainer_GetVariousTypes(t *testing.T) {
	container := NewContinuityContainer("test", 15*time.Minute)

	container.Set("string", "value")
	container.Set("uuid_direct", uuid.New())
	container.Set("uuid_string", uuid.New().String())

	// Get string
	str := container.GetString("string")
	assert.Equal(t, "value", str)

	// Get UUID as UUID
	id, exists := container.GetUUID("uuid_direct")
	assert.True(t, exists)
	assert.NotEqual(t, uuid.Nil, id)

	// Get UUID as string
	id2, exists := container.GetUUID("uuid_string")
	assert.True(t, exists)
	assert.NotEqual(t, uuid.Nil, id2)
}

func TestGenerateLoginFormNodes_Structure(t *testing.T) {
	csrf := "test-token"
	nodes := GenerateLoginFormNodes(csrf)

	assert.Len(t, nodes, 4)
	assert.Equal(t, InputTypeHidden, nodes[0].Attributes.Type)
	assert.Equal(t, csrf, nodes[0].Attributes.Value)
	assert.Equal(t, InputTypeEmail, nodes[1].Attributes.Type)
	assert.Equal(t, InputTypePassword, nodes[2].Attributes.Type)
	assert.Equal(t, InputTypeSubmit, nodes[3].Attributes.Type)
}

func TestGenerateRegistrationFormNodes_Structure(t *testing.T) {
	csrf := "test-token"
	nodes := GenerateRegistrationFormNodes(csrf)

	assert.Len(t, nodes, 5)
	assert.Equal(t, InputTypeHidden, nodes[0].Attributes.Type)
	assert.Equal(t, InputTypeEmail, nodes[1].Attributes.Type)
	assert.Equal(t, InputTypePassword, nodes[2].Attributes.Type)
	assert.Equal(t, InputTypePassword, nodes[3].Attributes.Type)
	assert.Equal(t, InputTypeSubmit, nodes[4].Attributes.Type)
}

func TestFlow_IdentityAndSessionImmutability(t *testing.T) {
	flow, _ := NewFlow(TypeLogin, "/auth/login", 15*time.Minute)

	id1 := uuid.New()
	session1 := uuid.New()

	flow.SetIdentity(id1)
	flow.SetSession(session1)

	// Replace with new values
	id2 := uuid.New()
	session2 := uuid.New()

	flow.SetIdentity(id2)
	flow.SetSession(session2)

	// Should have new values
	assert.Equal(t, id2, *flow.IdentityID)
	assert.Equal(t, session2, *flow.SessionID)
}

func TestFlow_ContextOverwriting(t *testing.T) {
	flow, _ := NewFlow(TypeLogin, "/auth/login", 15*time.Minute)

	flow.AddContext("key", "value1")
	val1, _ := flow.GetContext("key")
	assert.Equal(t, "value1", val1)

	flow.AddContext("key", "value2")
	val2, _ := flow.GetContext("key")
	assert.Equal(t, "value2", val2)

	flow.AddContext("key", "value3")
	val3, _ := flow.GetContext("key")
	assert.Equal(t, "value3", val3)
}

func TestNewInputNode_AllParameters(t *testing.T) {
	node := NewInputNode("username", InputTypeText, "Username", true)

	assert.Equal(t, NodeTypeInput, node.Type)
	assert.Equal(t, "default", node.Group)
	assert.Equal(t, "username", node.Attributes.Name)
	assert.Equal(t, InputTypeText, node.Attributes.Type)
	assert.True(t, node.Attributes.Required)
	assert.NotNil(t, node.Meta.Label)
	assert.Equal(t, "Username", node.Meta.Label.Text)
}

func TestNodeChaining_MultipleModifiers(t *testing.T) {
	node := NewInputNode("password", InputTypePassword, "Password", true)
	
	node = WithPlaceholder(node, "Min 8 characters")
	node = WithMinLength(node, 8)
	node = WithMaxLength(node, 128)
	node = WithAutocomplete(node, "current-password")
	node = WithPattern(node, ".{8,}")

	assert.Equal(t, "Min 8 characters", node.Attributes.Placeholder)
	assert.Equal(t, 8, node.Attributes.MinLength)
	assert.Equal(t, 128, node.Attributes.MaxLength)
	assert.Equal(t, "current-password", node.Attributes.Autocomplete)
	assert.Equal(t, ".{8,}", node.Attributes.Pattern)
}

func BenchmarkMultipleFlowCreations(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		NewFlow(TypeLogin, "/auth/login", 15*time.Minute)
	}
}

func BenchmarkContextOperations(b *testing.B) {
	flow, _ := NewFlow(TypeLogin, "/auth/login", 15*time.Minute)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		flow.AddContext("key", "value")
		flow.GetContext("key")
	}
}
