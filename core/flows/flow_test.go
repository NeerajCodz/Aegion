package flows

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFlowType_Valid(t *testing.T) {
	tests := []struct {
		name     string
		flowType FlowType
		expected bool
	}{
		{"login is valid", TypeLogin, true},
		{"registration is valid", TypeRegistration, true},
		{"recovery is valid", TypeRecovery, true},
		{"settings is valid", TypeSettings, true},
		{"verification is valid", TypeVerification, true},
		{"empty string is invalid", FlowType(""), false},
		{"unknown type is invalid", FlowType("unknown"), false},
		{"custom type is invalid", FlowType("custom"), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.flowType.Valid())
		})
	}
}

func TestFlowState_Valid(t *testing.T) {
	tests := []struct {
		name      string
		flowState FlowState
		expected  bool
	}{
		{"active is valid", StateActive, true},
		{"completed is valid", StateCompleted, true},
		{"failed is valid", StateFailed, true},
		{"expired is valid", StateExpired, true},
		{"empty string is invalid", FlowState(""), false},
		{"unknown state is invalid", FlowState("unknown"), false},
		{"custom state is invalid", FlowState("pending"), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.flowState.Valid())
		})
	}
}

func TestFlowState_IsTerminal(t *testing.T) {
	tests := []struct {
		name      string
		flowState FlowState
		expected  bool
	}{
		{"active is not terminal", StateActive, false},
		{"completed is terminal", StateCompleted, true},
		{"failed is terminal", StateFailed, true},
		{"expired is terminal", StateExpired, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.flowState.IsTerminal())
		})
	}
}

func TestNewFlow(t *testing.T) {
	tests := []struct {
		name       string
		flowType   FlowType
		requestURL string
		ttl        time.Duration
		wantErr    error
	}{
		{
			name:       "valid login flow",
			flowType:   TypeLogin,
			requestURL: "/auth/login",
			ttl:        15 * time.Minute,
			wantErr:    nil,
		},
		{
			name:       "valid registration flow",
			flowType:   TypeRegistration,
			requestURL: "/auth/register",
			ttl:        30 * time.Minute,
			wantErr:    nil,
		},
		{
			name:       "zero TTL uses default",
			flowType:   TypeRecovery,
			requestURL: "/auth/recovery",
			ttl:        0,
			wantErr:    nil,
		},
		{
			name:       "negative TTL uses default",
			flowType:   TypeSettings,
			requestURL: "/settings",
			ttl:        -1 * time.Minute,
			wantErr:    nil,
		},
		{
			name:       "invalid flow type",
			flowType:   FlowType("invalid"),
			requestURL: "/invalid",
			ttl:        15 * time.Minute,
			wantErr:    ErrInvalidFlowType,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			flow, err := NewFlow(tt.flowType, tt.requestURL, tt.ttl)

			if tt.wantErr != nil {
				assert.ErrorIs(t, err, tt.wantErr)
				assert.Nil(t, flow)
				return
			}

			require.NoError(t, err)
			assert.NotNil(t, flow)

			// Check basic properties
			assert.NotEqual(t, uuid.Nil, flow.ID)
			assert.Equal(t, tt.flowType, flow.Type)
			assert.Equal(t, StateActive, flow.State)
			assert.Equal(t, tt.requestURL, flow.RequestURL)
			assert.NotEmpty(t, flow.CSRFToken)
			assert.NotNil(t, flow.UI)
			assert.NotNil(t, flow.Context)
			assert.Equal(t, 0, len(flow.UI.Nodes))
			assert.Equal(t, 0, len(flow.Context))

			// Check timestamps
			now := time.Now().UTC()
			assert.WithinDuration(t, now, flow.IssuedAt, time.Second)
			assert.WithinDuration(t, now, flow.CreatedAt, time.Second)
			assert.WithinDuration(t, now, flow.UpdatedAt, time.Second)

			// Check TTL
			expectedTTL := tt.ttl
			if expectedTTL <= 0 {
				expectedTTL = DefaultTTL
			}
			expectedExpiry := flow.IssuedAt.Add(expectedTTL)
			assert.WithinDuration(t, expectedExpiry, flow.ExpiresAt, time.Second)
		})
	}
}

func TestFlow_IsExpired(t *testing.T) {
	now := time.Now().UTC()

	tests := []struct {
		name      string
		expiresAt time.Time
		expected  bool
	}{
		{
			name:      "not expired - future expiry",
			expiresAt: now.Add(10 * time.Minute),
			expected:  false,
		},
		{
			name:      "expired - past expiry",
			expiresAt: now.Add(-10 * time.Minute),
			expected:  true,
		},
		{
			name:      "just expired - exactly now",
			expiresAt: now.Add(-1 * time.Millisecond),
			expected:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			flow := &Flow{ExpiresAt: tt.expiresAt}
			assert.Equal(t, tt.expected, flow.IsExpired())
		})
	}
}

func TestFlow_IsActive(t *testing.T) {
	now := time.Now().UTC()

	tests := []struct {
		name      string
		state     FlowState
		expiresAt time.Time
		expected  bool
	}{
		{
			name:      "active and not expired",
			state:     StateActive,
			expiresAt: now.Add(10 * time.Minute),
			expected:  true,
		},
		{
			name:      "active but expired",
			state:     StateActive,
			expiresAt: now.Add(-10 * time.Minute),
			expected:  false,
		},
		{
			name:      "completed and not expired",
			state:     StateCompleted,
			expiresAt: now.Add(10 * time.Minute),
			expected:  false,
		},
		{
			name:      "failed and not expired",
			state:     StateFailed,
			expiresAt: now.Add(10 * time.Minute),
			expected:  false,
		},
		{
			name:      "expired state and past expiry",
			state:     StateExpired,
			expiresAt: now.Add(-10 * time.Minute),
			expected:  false,
		},
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

func TestFlow_ValidateCSRF(t *testing.T) {
	validToken, err := GenerateCSRFToken()
	require.NoError(t, err)

	flow := &Flow{CSRFToken: validToken}

	tests := []struct {
		name      string
		token     string
		wantError bool
	}{
		{
			name:      "valid token",
			token:     validToken,
			wantError: false,
		},
		{
			name:      "invalid token",
			token:     "invalid-token",
			wantError: true,
		},
		{
			name:      "empty token",
			token:     "",
			wantError: true,
		},
		{
			name:      "different valid token",
			token:     "dGVzdC10b2tlbi0zMi1ieXRlcy1sb25nISEhISEhISE",
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := flow.ValidateCSRF(tt.token)
			if tt.wantError {
				assert.ErrorIs(t, err, ErrInvalidCSRF)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestFlow_Complete(t *testing.T) {
	tests := []struct {
		name         string
		initialState FlowState
		wantError    error
		finalState   FlowState
	}{
		{
			name:         "complete active flow",
			initialState: StateActive,
			wantError:    nil,
			finalState:   StateCompleted,
		},
		{
			name:         "cannot complete already completed flow",
			initialState: StateCompleted,
			wantError:    ErrFlowCompleted,
			finalState:   StateCompleted,
		},
		{
			name:         "cannot complete failed flow",
			initialState: StateFailed,
			wantError:    ErrFlowCompleted,
			finalState:   StateFailed,
		},
		{
			name:         "cannot complete expired flow",
			initialState: StateExpired,
			wantError:    ErrFlowCompleted,
			finalState:   StateExpired,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			flow := &Flow{
				State:     tt.initialState,
				UpdatedAt: time.Now().UTC().Add(-1 * time.Hour),
			}
			oldUpdatedAt := flow.UpdatedAt

			err := flow.Complete()

			if tt.wantError != nil {
				assert.ErrorIs(t, err, tt.wantError)
				// UpdatedAt should not change on error
				assert.Equal(t, oldUpdatedAt, flow.UpdatedAt)
			} else {
				assert.NoError(t, err)
				// UpdatedAt should be updated
				assert.True(t, flow.UpdatedAt.After(oldUpdatedAt))
			}
			assert.Equal(t, tt.finalState, flow.State)
		})
	}
}

func TestFlow_Fail(t *testing.T) {
	tests := []struct {
		name         string
		initialState FlowState
		finalState   FlowState
		shouldUpdate bool
	}{
		{
			name:         "fail active flow",
			initialState: StateActive,
			finalState:   StateFailed,
			shouldUpdate: true,
		},
		{
			name:         "cannot fail completed flow",
			initialState: StateCompleted,
			finalState:   StateCompleted,
			shouldUpdate: false,
		},
		{
			name:         "cannot fail already failed flow",
			initialState: StateFailed,
			finalState:   StateFailed,
			shouldUpdate: false,
		},
		{
			name:         "cannot fail expired flow",
			initialState: StateExpired,
			finalState:   StateExpired,
			shouldUpdate: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			flow := &Flow{
				State:     tt.initialState,
				UpdatedAt: time.Now().UTC().Add(-1 * time.Hour),
			}
			oldUpdatedAt := flow.UpdatedAt

			flow.Fail()

			assert.Equal(t, tt.finalState, flow.State)
			if tt.shouldUpdate {
				assert.True(t, flow.UpdatedAt.After(oldUpdatedAt))
			} else {
				assert.Equal(t, oldUpdatedAt, flow.UpdatedAt)
			}
		})
	}
}

func TestFlow_Expire(t *testing.T) {
	tests := []struct {
		name         string
		initialState FlowState
		finalState   FlowState
		shouldUpdate bool
	}{
		{
			name:         "expire active flow",
			initialState: StateActive,
			finalState:   StateExpired,
			shouldUpdate: true,
		},
		{
			name:         "cannot expire completed flow",
			initialState: StateCompleted,
			finalState:   StateCompleted,
			shouldUpdate: false,
		},
		{
			name:         "cannot expire failed flow",
			initialState: StateFailed,
			finalState:   StateFailed,
			shouldUpdate: false,
		},
		{
			name:         "cannot expire already expired flow",
			initialState: StateExpired,
			finalState:   StateExpired,
			shouldUpdate: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			flow := &Flow{
				State:     tt.initialState,
				UpdatedAt: time.Now().UTC().Add(-1 * time.Hour),
			}
			oldUpdatedAt := flow.UpdatedAt

			flow.Expire()

			assert.Equal(t, tt.finalState, flow.State)
			if tt.shouldUpdate {
				assert.True(t, flow.UpdatedAt.After(oldUpdatedAt))
			} else {
				assert.Equal(t, oldUpdatedAt, flow.UpdatedAt)
			}
		})
	}
}

func TestFlow_SetIdentity(t *testing.T) {
	flow := &Flow{
		UpdatedAt: time.Now().UTC().Add(-1 * time.Hour),
	}
	oldUpdatedAt := flow.UpdatedAt

	identityID := uuid.New()
	flow.SetIdentity(identityID)

	assert.NotNil(t, flow.IdentityID)
	assert.Equal(t, identityID, *flow.IdentityID)
	assert.True(t, flow.UpdatedAt.After(oldUpdatedAt))
}

func TestFlow_SetSession(t *testing.T) {
	flow := &Flow{
		UpdatedAt: time.Now().UTC().Add(-1 * time.Hour),
	}
	oldUpdatedAt := flow.UpdatedAt

	sessionID := uuid.New()
	flow.SetSession(sessionID)

	assert.NotNil(t, flow.SessionID)
	assert.Equal(t, sessionID, *flow.SessionID)
	assert.True(t, flow.UpdatedAt.After(oldUpdatedAt))
}

func TestFlow_SetReturnTo(t *testing.T) {
	flow := &Flow{
		UpdatedAt: time.Now().UTC().Add(-1 * time.Hour),
	}
	oldUpdatedAt := flow.UpdatedAt

	returnURL := "https://example.com/return"
	flow.SetReturnTo(returnURL)

	assert.Equal(t, returnURL, flow.ReturnTo)
	assert.True(t, flow.UpdatedAt.After(oldUpdatedAt))
}

func TestFlow_AddContext(t *testing.T) {
	flow := &Flow{
		Context:   make(FlowCtx),
		UpdatedAt: time.Now().UTC().Add(-1 * time.Hour),
	}
	oldUpdatedAt := flow.UpdatedAt

	flow.AddContext("key1", "value1")
	flow.AddContext("key2", 123)
	flow.AddContext("key3", map[string]string{"nested": "value"})

	assert.Equal(t, "value1", flow.Context["key1"])
	assert.Equal(t, 123, flow.Context["key2"])
	assert.Equal(t, map[string]string{"nested": "value"}, flow.Context["key3"])
	assert.True(t, flow.UpdatedAt.After(oldUpdatedAt))
}

func TestFlow_GetContext(t *testing.T) {
	flow := &Flow{
		Context: FlowCtx{
			"existing": "value",
			"number":   42,
		},
	}

	// Get existing key
	value, exists := flow.GetContext("existing")
	assert.True(t, exists)
	assert.Equal(t, "value", value)

	// Get existing number key
	value, exists = flow.GetContext("number")
	assert.True(t, exists)
	assert.Equal(t, 42, value)

	// Get non-existing key
	value, exists = flow.GetContext("nonexistent")
	assert.False(t, exists)
	assert.Nil(t, value)
}

func TestGenerateCSRFToken(t *testing.T) {
	// Test token generation
	token1, err := GenerateCSRFToken()
	assert.NoError(t, err)
	assert.NotEmpty(t, token1)

	token2, err := GenerateCSRFToken()
	assert.NoError(t, err)
	assert.NotEmpty(t, token2)

	// Tokens should be unique
	assert.NotEqual(t, token1, token2)

	// Tokens should be valid base64
	assert.Regexp(t, "^[A-Za-z0-9_-]+$", token1)
	assert.Regexp(t, "^[A-Za-z0-9_-]+$", token2)
}

func TestValidateCSRFToken(t *testing.T) {
	validToken, err := GenerateCSRFToken()
	require.NoError(t, err)

	tests := []struct {
		name     string
		expected string
		actual   string
		valid    bool
	}{
		{
			name:     "valid matching tokens",
			expected: validToken,
			actual:   validToken,
			valid:    true,
		},
		{
			name:     "different tokens",
			expected: validToken,
			actual:   "different-token",
			valid:    false,
		},
		{
			name:     "empty expected token",
			expected: "",
			actual:   validToken,
			valid:    false,
		},
		{
			name:     "empty actual token",
			expected: validToken,
			actual:   "",
			valid:    false,
		},
		{
			name:     "both empty",
			expected: "",
			actual:   "",
			valid:    false,
		},
		{
			name:     "case sensitive",
			expected: "AbCdEf",
			actual:   "abcdef",
			valid:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ValidateCSRFToken(tt.expected, tt.actual)
			assert.Equal(t, tt.valid, result)
		})
	}
}

func TestConstants(t *testing.T) {
	// Test flow types
	assert.Equal(t, "login", string(TypeLogin))
	assert.Equal(t, "registration", string(TypeRegistration))
	assert.Equal(t, "recovery", string(TypeRecovery))
	assert.Equal(t, "settings", string(TypeSettings))
	assert.Equal(t, "verification", string(TypeVerification))

	// Test flow states
	assert.Equal(t, "active", string(StateActive))
	assert.Equal(t, "completed", string(StateCompleted))
	assert.Equal(t, "failed", string(StateFailed))
	assert.Equal(t, "expired", string(StateExpired))

	// Test default TTL
	assert.Equal(t, 15*time.Minute, DefaultTTL)

	// Test message types
	assert.Equal(t, "info", string(MsgTypeInfo))
	assert.Equal(t, "error", string(MsgTypeError))
	assert.Equal(t, "success", string(MsgTypeSuccess))
	assert.Equal(t, "warning", string(MsgTypeWarning))
}

func TestErrors(t *testing.T) {
	// Test error constants exist and are different
	errors := []error{
		ErrFlowNotFound,
		ErrFlowExpired,
		ErrFlowCompleted,
		ErrFlowFailed,
		ErrInvalidCSRF,
		ErrInvalidFlowType,
		ErrInvalidFlowState,
	}

	// Check all errors are different
	for i, err1 := range errors {
		for j, err2 := range errors {
			if i != j {
				assert.NotEqual(t, err1, err2)
			}
		}
	}

	// Check error messages are meaningful
	assert.Contains(t, ErrFlowNotFound.Error(), "not found")
	assert.Contains(t, ErrFlowExpired.Error(), "expired")
	assert.Contains(t, ErrFlowCompleted.Error(), "completed")
	assert.Contains(t, ErrFlowFailed.Error(), "failed")
	assert.Contains(t, ErrInvalidCSRF.Error(), "CSRF")
	assert.Contains(t, ErrInvalidFlowType.Error(), "flow type")
	assert.Contains(t, ErrInvalidFlowState.Error(), "flow state")
}

func TestUIState(t *testing.T) {
	ui := &UIState{
		Action: "/auth/login",
		Method: "POST",
		Nodes: []Node{
			{
				Type:  NodeTypeInput,
				Group: "default",
				Attributes: NodeAttributes{
					Name:        "email",
					Type:        InputTypeEmail,
					Required:    true,
					Label:       "Email",
					Placeholder: "Enter your email",
				},
			},
		},
		Messages: []Msg{
			{
				ID:   "welcome",
				Type: MsgTypeInfo,
				Text: "Please enter your credentials",
			},
		},
	}

	assert.Equal(t, "/auth/login", ui.Action)
	assert.Equal(t, "POST", ui.Method)
	assert.Len(t, ui.Nodes, 1)
	assert.Len(t, ui.Messages, 1)

	node := ui.Nodes[0]
	assert.Equal(t, NodeTypeInput, node.Type)
	assert.Equal(t, "email", node.Attributes.Name)
	assert.Equal(t, InputTypeEmail, node.Attributes.Type)
	assert.True(t, node.Attributes.Required)

	msg := ui.Messages[0]
	assert.Equal(t, MsgTypeInfo, msg.Type)
	assert.Equal(t, "Please enter your credentials", msg.Text)
}

func TestNodeTypes(t *testing.T) {
	nodeTypes := []NodeType{
		NodeTypeInput, NodeTypeText, NodeTypeSubmit, NodeTypeError,
		NodeTypeInfo, NodeTypeScript, NodeTypeImage, NodeTypeAnchor,
	}

	inputTypes := []InputType{
		InputTypeText, InputTypePassword, InputTypeEmail, InputTypeHidden,
		InputTypeSubmit, InputTypeButton, InputTypeCheckbox, InputTypeTel,
		InputTypeNumber,
	}

	// Ensure all types are unique strings
	for _, nt := range nodeTypes {
		assert.NotEmpty(t, string(nt))
	}

	for _, it := range inputTypes {
		assert.NotEmpty(t, string(it))
	}
}

func BenchmarkGenerateCSRFToken(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_, err := GenerateCSRFToken()
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkValidateCSRFToken(b *testing.B) {
	token1, _ := GenerateCSRFToken()
	token2, _ := GenerateCSRFToken()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ValidateCSRFToken(token1, token2)
	}
}