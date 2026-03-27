package flows

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
)

// Tests for PostgreSQL store implementations (testing the code paths without DB)

func TestPostgresFlowStore_Initialization(t *testing.T) {
	store := NewPostgresFlowStore(nil)
	assert.NotNil(t, store)
}

func TestPostgresContinuityStore_Initialization(t *testing.T) {
	store := NewPostgresContinuityStore(nil)
	assert.NotNil(t, store)
}

// Tests for JSON marshaling/unmarshaling (used by store)

func TestFlow_JSONMarshal(t *testing.T) {
	flow, _ := NewFlow(TypeLogin, "/auth/login", 0)
	flow.AddContext("key", "value")

	data, err := json.Marshal(flow)
	assert.NoError(t, err)
	assert.NotEmpty(t, data)

	var recovered Flow
	err = json.Unmarshal(data, &recovered)
	assert.NoError(t, err)
	assert.Equal(t, flow.ID, recovered.ID)
	assert.Equal(t, flow.Type, recovered.Type)
	assert.Equal(t, flow.State, recovered.State)
	assert.Equal(t, flow.CSRFToken, recovered.CSRFToken)
}

func TestUIState_JSONMarshal(t *testing.T) {
	ui := &UIState{
		Action: "/auth/login",
		Method: "POST",
		Nodes: []Node{
			NewInputNode("email", InputTypeEmail, "Email", true),
		},
		Messages: []Msg{
			{
				ID:   "msg1",
				Type: MsgTypeInfo,
				Text: "Welcome",
			},
		},
	}

	data, err := json.Marshal(ui)
	assert.NoError(t, err)

	var recovered UIState
	err = json.Unmarshal(data, &recovered)
	assert.NoError(t, err)
	assert.Equal(t, ui.Action, recovered.Action)
	assert.Equal(t, ui.Method, recovered.Method)
	assert.Len(t, recovered.Nodes, 1)
	assert.Len(t, recovered.Messages, 1)
}

func TestFlowCtx_JSONMarshal(t *testing.T) {
	ctx := FlowCtx{
		"email":    "user@example.com",
		"verified": true,
		"attempts": 3,
	}

	data, err := json.Marshal(ctx)
	assert.NoError(t, err)

	var recovered FlowCtx
	err = json.Unmarshal(data, &recovered)
	assert.NoError(t, err)
	assert.Equal(t, "user@example.com", recovered["email"])
	assert.Equal(t, true, recovered["verified"])
	assert.Equal(t, float64(3), recovered["attempts"]) // JSON unmarshals numbers as float64
}

func TestContinuityContainer_JSONMarshal(t *testing.T) {
	container := NewContinuityContainer("test", 0)
	container.Set("key", "value")
	container.Set("count", 42)

	data, err := json.Marshal(container)
	assert.NoError(t, err)

	var recovered ContinuityContainer
	err = json.Unmarshal(data, &recovered)
	assert.NoError(t, err)
	assert.Equal(t, container.ID, recovered.ID)
	assert.Equal(t, container.Name, recovered.Name)
	assert.NotNil(t, recovered.Payload)
}

func TestPayload_JSONMarshal(t *testing.T) {
	payload := Payload{
		"string": "value",
		"int":    42,
		"bool":   true,
		"nested": map[string]interface{}{
			"key": "value",
		},
	}

	data, err := json.Marshal(payload)
	assert.NoError(t, err)

	var recovered Payload
	err = json.Unmarshal(data, &recovered)
	assert.NoError(t, err)
	assert.Equal(t, "value", recovered["string"])
	assert.Equal(t, float64(42), recovered["int"])
	assert.Equal(t, true, recovered["bool"])
}

// Tests for Node structures
func TestNode_JSONMarshal(t *testing.T) {
	node := NewInputNode("email", InputTypeEmail, "Email Address", true)
	node = WithPlaceholder(node, "user@example.com")
	node = WithAutocomplete(node, "email")

	data, err := json.Marshal(node)
	assert.NoError(t, err)

	var recovered Node
	err = json.Unmarshal(data, &recovered)
	assert.NoError(t, err)
	assert.Equal(t, node.Type, recovered.Type)
	assert.Equal(t, node.Attributes.Name, recovered.Attributes.Name)
	assert.Equal(t, node.Attributes.Placeholder, recovered.Attributes.Placeholder)
}

// Type compatibility and conversions
func TestFlowType_StringConversion(t *testing.T) {
	types := []FlowType{
		TypeLogin,
		TypeRegistration,
		TypeRecovery,
		TypeSettings,
		TypeVerification,
	}

	for _, ft := range types {
		// Convert to string and back
		str := string(ft)
		assert.NotEmpty(t, str)

		// Verify it's a valid type
		recovered := FlowType(str)
		assert.True(t, recovered.Valid())
	}
}

func TestFlowState_StringConversion(t *testing.T) {
	states := []FlowState{
		StateActive,
		StateCompleted,
		StateFailed,
		StateExpired,
	}

	for _, s := range states {
		// Convert to string and back
		str := string(s)
		assert.NotEmpty(t, str)

		// Verify it's a valid state
		recovered := FlowState(str)
		assert.True(t, recovered.Valid())
	}
}

func TestNodeType_StringConversion(t *testing.T) {
	types := []NodeType{
		NodeTypeInput,
		NodeTypeText,
		NodeTypeSubmit,
		NodeTypeError,
		NodeTypeInfo,
		NodeTypeScript,
		NodeTypeImage,
		NodeTypeAnchor,
	}

	for _, nt := range types {
		str := string(nt)
		assert.NotEmpty(t, str)
	}
}

func TestInputType_StringConversion(t *testing.T) {
	types := []InputType{
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

	for _, it := range types {
		str := string(it)
		assert.NotEmpty(t, str)
	}
}

// Tests for all valid/invalid combinations
func TestFlowType_Invalid(t *testing.T) {
	invalid := []FlowType{
		FlowType(""),
		FlowType("invalid"),
		FlowType("LOGIN"),  // case sensitive
		FlowType("login "), // with space
	}

	for _, ft := range invalid {
		assert.False(t, ft.Valid(), "FlowType %s should be invalid", string(ft))
	}
}

func TestFlowState_Invalid(t *testing.T) {
	invalid := []FlowState{
		FlowState(""),
		FlowState("invalid"),
		FlowState("ACTIVE"),  // case sensitive
		FlowState("pending"), // not a valid state
	}

	for _, s := range invalid {
		assert.False(t, s.Valid(), "FlowState %s should be invalid", string(s))
	}
}

// Tests for all error constants
func TestAllErrorConstants(t *testing.T) {
	errors := []error{
		ErrFlowNotFound,
		ErrFlowExpired,
		ErrFlowCompleted,
		ErrFlowFailed,
		ErrInvalidCSRF,
		ErrInvalidFlowType,
		ErrInvalidFlowState,
		ErrContainerNotFound,
		ErrContainerExpired,
	}

	for _, err := range errors {
		assert.NotNil(t, err)
		assert.NotEmpty(t, err.Error())
	}
}

// Tests for constant values
func TestConstants_Values(t *testing.T) {
	// Flow types
	assert.Equal(t, "login", string(TypeLogin))
	assert.Equal(t, "registration", string(TypeRegistration))
	assert.Equal(t, "recovery", string(TypeRecovery))
	assert.Equal(t, "settings", string(TypeSettings))
	assert.Equal(t, "verification", string(TypeVerification))

	// Flow states
	assert.Equal(t, "active", string(StateActive))
	assert.Equal(t, "completed", string(StateCompleted))
	assert.Equal(t, "failed", string(StateFailed))
	assert.Equal(t, "expired", string(StateExpired))

	// Node types
	assert.Equal(t, "input", string(NodeTypeInput))
	assert.Equal(t, "text", string(NodeTypeText))
	assert.Equal(t, "submit", string(NodeTypeSubmit))
	assert.Equal(t, "error", string(NodeTypeError))
	assert.Equal(t, "info", string(NodeTypeInfo))
	assert.Equal(t, "script", string(NodeTypeScript))
	assert.Equal(t, "image", string(NodeTypeImage))
	assert.Equal(t, "anchor", string(NodeTypeAnchor))

	// Input types
	assert.Equal(t, "text", string(InputTypeText))
	assert.Equal(t, "password", string(InputTypePassword))
	assert.Equal(t, "email", string(InputTypeEmail))
	assert.Equal(t, "hidden", string(InputTypeHidden))
	assert.Equal(t, "submit", string(InputTypeSubmit))
	assert.Equal(t, "button", string(InputTypeButton))
	assert.Equal(t, "checkbox", string(InputTypeCheckbox))
	assert.Equal(t, "tel", string(InputTypeTel))
	assert.Equal(t, "number", string(InputTypeNumber))

	// Message types
	assert.Equal(t, "info", string(MsgTypeInfo))
	assert.Equal(t, "error", string(MsgTypeError))
	assert.Equal(t, "success", string(MsgTypeSuccess))
	assert.Equal(t, "warning", string(MsgTypeWarning))
}

// Tests for TTL constants
func TestTTLConstants(t *testing.T) {
	assert.Equal(t, 15.0, DefaultTTL.Minutes()) // 15 minutes
	assert.Equal(t, 15.0, DefaultContinuityTTL.Minutes()) // 15 minutes
}

// Tests for nil and edge case scenarios
func TestFlow_NilIdentityAndSession(t *testing.T) {
	flow, _ := NewFlow(TypeLogin, "/auth/login", 0)

	assert.Nil(t, flow.IdentityID)
	assert.Nil(t, flow.SessionID)

	// Should not panic when reading nil pointers
	_, exists := flow.GetContext("any")
	assert.False(t, exists)
}

func TestFlow_EmptyRequestURL(t *testing.T) {
	flow, _ := NewFlow(TypeLogin, "", 0)

	assert.Equal(t, "", flow.RequestURL)
}

func TestFlow_EmptyReturnTo(t *testing.T) {
	flow, _ := NewFlow(TypeLogin, "/auth/login", 0)

	assert.Equal(t, "", flow.ReturnTo)
}

// UUID generation tests
func TestFlow_UniqueUUIDs(t *testing.T) {
	ids := make(map[uuid.UUID]bool)

	for i := 0; i < 100; i++ {
		flow, _ := NewFlow(TypeLogin, "/auth/login", 0)
		assert.False(t, ids[flow.ID], "Generated duplicate UUID")
		ids[flow.ID] = true
	}

	assert.Equal(t, 100, len(ids))
}

func TestContinuityContainer_UniqueUUIDs(t *testing.T) {
	ids := make(map[uuid.UUID]bool)

	for i := 0; i < 100; i++ {
		container := NewContinuityContainer("test", 0)
		assert.False(t, ids[container.ID], "Generated duplicate UUID")
		ids[container.ID] = true
	}

	assert.Equal(t, 100, len(ids))
}

// NodeAttributes edge cases
func TestNodeAttributes_AllZero(t *testing.T) {
	attrs := NodeAttributes{}

	assert.Empty(t, attrs.Name)
	assert.Empty(t, attrs.Type)
	assert.Nil(t, attrs.Value)
	assert.False(t, attrs.Required)
	assert.False(t, attrs.Disabled)
	assert.Equal(t, 0, attrs.MaxLength)
	assert.Equal(t, 0, attrs.MinLength)
}

func TestNodeAttributes_FullPopulation(t *testing.T) {
	attrs := NodeAttributes{
		Name:         "test",
		Type:         InputTypeEmail,
		Value:        "test@example.com",
		Required:     true,
		Disabled:     false,
		Pattern:      ".+@.+",
		Autocomplete: "email",
		Label:        "Test Label",
		Placeholder:  "Test Placeholder",
		MaxLength:    100,
		MinLength:    1,
		Href:         "https://example.com",
		Src:          "image.png",
		Alt:          "Test Image",
		ID:           "test-id",
		OnClick:      "handleClick()",
		NodeValue:    "test-value",
	}

	assert.Equal(t, "test", attrs.Name)
	assert.Equal(t, InputTypeEmail, attrs.Type)
	assert.Equal(t, "test@example.com", attrs.Value)
	assert.True(t, attrs.Required)
	assert.False(t, attrs.Disabled)
	assert.Equal(t, ".+@.+", attrs.Pattern)
	assert.Equal(t, "email", attrs.Autocomplete)
	assert.Equal(t, "Test Label", attrs.Label)
	assert.Equal(t, "Test Placeholder", attrs.Placeholder)
	assert.Equal(t, 100, attrs.MaxLength)
	assert.Equal(t, 1, attrs.MinLength)
	assert.Equal(t, "https://example.com", attrs.Href)
	assert.Equal(t, "image.png", attrs.Src)
	assert.Equal(t, "Test Image", attrs.Alt)
	assert.Equal(t, "test-id", attrs.ID)
	assert.Equal(t, "handleClick()", attrs.OnClick)
	assert.Equal(t, "test-value", attrs.NodeValue)
}

func BenchmarkFlow_JSONOperations(b *testing.B) {
	flow, _ := NewFlow(TypeLogin, "/auth/login", 0)
	flow.AddContext("key", "value")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		data, _ := json.Marshal(flow)
		var recovered Flow
		json.Unmarshal(data, &recovered)
	}
}

func BenchmarkContinuityContainer_JSONOperations(b *testing.B) {
	container := NewContinuityContainer("test", 0)
	container.Set("key", "value")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		data, _ := json.Marshal(container)
		var recovered ContinuityContainer
		json.Unmarshal(data, &recovered)
	}
}
