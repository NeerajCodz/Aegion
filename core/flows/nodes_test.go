package flows

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewInputNode(t *testing.T) {
	node := NewInputNode("email", InputTypeEmail, "Email Address", true)

	assert.Equal(t, NodeTypeInput, node.Type)
	assert.Equal(t, "default", node.Group)
	assert.Equal(t, "email", node.Attributes.Name)
	assert.Equal(t, InputTypeEmail, node.Attributes.Type)
	assert.Equal(t, true, node.Attributes.Required)
	assert.NotNil(t, node.Meta.Label)
	assert.Equal(t, "label.email", node.Meta.Label.ID)
	assert.Equal(t, "Email Address", node.Meta.Label.Text)
}

func TestNewHiddenNode(t *testing.T) {
	node := NewHiddenNode("csrf_token", "secret-token-123")

	assert.Equal(t, NodeTypeInput, node.Type)
	assert.Equal(t, "csrf_token", node.Attributes.Name)
	assert.Equal(t, InputTypeHidden, node.Attributes.Type)
	assert.Equal(t, "secret-token-123", node.Attributes.Value)
	assert.Equal(t, false, node.Attributes.Required)
}

func TestNewSubmitNode(t *testing.T) {
	node := NewSubmitNode("login", "Sign In")

	assert.Equal(t, NodeTypeSubmit, node.Type)
	assert.Equal(t, "login", node.Attributes.Name)
	assert.Equal(t, InputTypeSubmit, node.Attributes.Type)
	assert.Equal(t, "Sign In", node.Attributes.Value)
	assert.NotNil(t, node.Meta.Label)
	assert.Equal(t, "button.login", node.Meta.Label.ID)
	assert.Equal(t, "Sign In", node.Meta.Label.Text)
}

func TestNewTextNode(t *testing.T) {
	node := NewTextNode("intro", "Welcome to our service")

	assert.Equal(t, NodeTypeText, node.Type)
	assert.Equal(t, "intro", node.Attributes.ID)
	assert.Equal(t, "Welcome to our service", node.Attributes.NodeValue)
}

func TestNewErrorNode(t *testing.T) {
	node := NewErrorNode(ErrIDInvalidCredentials)

	assert.Equal(t, NodeTypeError, node.Type)
	assert.Len(t, node.Messages, 1)
	assert.Equal(t, ErrIDInvalidCredentials, node.Messages[0].ID)
	assert.Equal(t, MsgTypeError, node.Messages[0].Type)
	assert.Equal(t, "The credentials provided are invalid.", node.Messages[0].Text)
}

func TestNewInfoNode(t *testing.T) {
	node := NewInfoNode("info-box", "Please fill in all required fields")

	assert.Equal(t, NodeTypeInfo, node.Type)
	assert.Equal(t, "info-box", node.Attributes.ID)
	assert.Equal(t, "Please fill in all required fields", node.Attributes.NodeValue)
}

func TestNewAnchorNode(t *testing.T) {
	node := NewAnchorNode("forgot-password", "/recovery", "Forgot Password?")

	assert.Equal(t, NodeTypeAnchor, node.Type)
	assert.Equal(t, "forgot-password", node.Attributes.ID)
	assert.Equal(t, "/recovery", node.Attributes.Href)
	assert.Equal(t, "Forgot Password?", node.Attributes.NodeValue)
}

func TestGetErrorMessage(t *testing.T) {
	tests := []struct {
		name      string
		errID     string
		expected  string
	}{
		{
			name:     "invalid credentials",
			errID:    ErrIDInvalidCredentials,
			expected: "The credentials provided are invalid.",
		},
		{
			name:     "email taken",
			errID:    ErrIDEmailTaken,
			expected: "This email address is already registered.",
		},
		{
			name:     "flow expired",
			errID:    ErrIDFlowExpired,
			expected: "This action has expired. Please start again.",
		},
		{
			name:     "unknown error defaults to internal",
			errID:    "error.unknown",
			expected: ErrorMessages[ErrIDInternalError],
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := GetErrorMessage(tt.errID)
			assert.Equal(t, tt.expected, msg)
		})
	}
}

func TestGenerateLoginFormNodes(t *testing.T) {
	csrf := "test-csrf-token"
	nodes := GenerateLoginFormNodes(csrf)

	assert.Len(t, nodes, 4)

	// Check CSRF hidden input
	assert.Equal(t, NodeTypeInput, nodes[0].Type)
	assert.Equal(t, InputTypeHidden, nodes[0].Attributes.Type)
	assert.Equal(t, "csrf_token", nodes[0].Attributes.Name)
	assert.Equal(t, csrf, nodes[0].Attributes.Value)

	// Check email input
	assert.Equal(t, NodeTypeInput, nodes[1].Type)
	assert.Equal(t, InputTypeEmail, nodes[1].Attributes.Type)
	assert.Equal(t, "identifier", nodes[1].Attributes.Name)
	assert.True(t, nodes[1].Attributes.Required)

	// Check password input
	assert.Equal(t, NodeTypeInput, nodes[2].Type)
	assert.Equal(t, InputTypePassword, nodes[2].Attributes.Type)
	assert.Equal(t, "password", nodes[2].Attributes.Name)
	assert.True(t, nodes[2].Attributes.Required)

	// Check submit button
	assert.Equal(t, NodeTypeSubmit, nodes[3].Type)
	assert.Equal(t, "method", nodes[3].Attributes.Name)
}

func TestGenerateRegistrationFormNodes(t *testing.T) {
	csrf := "test-csrf-token"
	nodes := GenerateRegistrationFormNodes(csrf)

	assert.Len(t, nodes, 5)

	// Check CSRF hidden input
	assert.Equal(t, NodeTypeInput, nodes[0].Type)
	assert.Equal(t, InputTypeHidden, nodes[0].Attributes.Type)

	// Check email input
	assert.Equal(t, "email", nodes[1].Attributes.Name)
	assert.Equal(t, InputTypeEmail, nodes[1].Attributes.Type)

	// Check password input
	assert.Equal(t, "password", nodes[2].Attributes.Name)
	assert.Equal(t, InputTypePassword, nodes[2].Attributes.Type)

	// Check password confirm input
	assert.Equal(t, "password_confirm", nodes[3].Attributes.Name)
	assert.Equal(t, InputTypePassword, nodes[3].Attributes.Type)

	// Check submit button
	assert.Equal(t, NodeTypeSubmit, nodes[4].Type)
}

func TestGenerateRecoveryFormNodes(t *testing.T) {
	csrf := "test-csrf-token"
	nodes := GenerateRecoveryFormNodes(csrf)

	assert.Len(t, nodes, 3)

	// Check email input
	assert.Equal(t, "email", nodes[1].Attributes.Name)
	assert.Equal(t, InputTypeEmail, nodes[1].Attributes.Type)

	// Check submit button
	assert.Equal(t, NodeTypeSubmit, nodes[2].Type)
}

func TestGenerateSettingsFormNodes(t *testing.T) {
	csrf := "test-csrf-token"
	nodes := GenerateSettingsFormNodes(csrf)

	assert.Len(t, nodes, 5)

	// Check current password input
	assert.Equal(t, "current_password", nodes[1].Attributes.Name)
	assert.Equal(t, InputTypePassword, nodes[1].Attributes.Type)

	// Check new password input
	assert.Equal(t, "new_password", nodes[2].Attributes.Name)
	assert.Equal(t, InputTypePassword, nodes[2].Attributes.Type)

	// Check new password confirm input
	assert.Equal(t, "new_password_confirm", nodes[3].Attributes.Name)
	assert.Equal(t, InputTypePassword, nodes[3].Attributes.Type)
}

func TestGenerateVerificationFormNodes(t *testing.T) {
	csrf := "test-csrf-token"
	nodes := GenerateVerificationFormNodes(csrf)

	assert.Len(t, nodes, 3)

	// Check code input
	assert.Equal(t, "code", nodes[1].Attributes.Name)
	assert.Equal(t, InputTypeText, nodes[1].Attributes.Type)
}

func TestAddNodeToGroup(t *testing.T) {
	node := NewInputNode("email", InputTypeEmail, "Email", true)
	assert.Equal(t, "default", node.Group)

	updatedNode := AddNodeToGroup(node, "custom-group")
	assert.Equal(t, "custom-group", updatedNode.Group)
}

func TestWithAutocomplete(t *testing.T) {
	node := NewInputNode("email", InputTypeEmail, "Email", true)
	assert.Empty(t, node.Attributes.Autocomplete)

	updatedNode := WithAutocomplete(node, "email")
	assert.Equal(t, "email", updatedNode.Attributes.Autocomplete)
}

func TestWithPlaceholder(t *testing.T) {
	node := NewInputNode("email", InputTypeEmail, "Email", true)
	assert.Empty(t, node.Attributes.Placeholder)

	updatedNode := WithPlaceholder(node, "Enter your email address")
	assert.Equal(t, "Enter your email address", updatedNode.Attributes.Placeholder)
}

func TestWithPattern(t *testing.T) {
	node := NewInputNode("email", InputTypeEmail, "Email", true)
	assert.Empty(t, node.Attributes.Pattern)

	pattern := `^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}$`
	updatedNode := WithPattern(node, pattern)
	assert.Equal(t, pattern, updatedNode.Attributes.Pattern)
}

func TestWithMinLength(t *testing.T) {
	node := NewInputNode("password", InputTypePassword, "Password", true)
	assert.Equal(t, 0, node.Attributes.MinLength)

	updatedNode := WithMinLength(node, 8)
	assert.Equal(t, 8, updatedNode.Attributes.MinLength)
}

func TestWithMaxLength(t *testing.T) {
	node := NewInputNode("username", InputTypeText, "Username", true)
	assert.Equal(t, 0, node.Attributes.MaxLength)

	updatedNode := WithMaxLength(node, 50)
	assert.Equal(t, 50, updatedNode.Attributes.MaxLength)
}

func TestWithDisabled(t *testing.T) {
	node := NewInputNode("email", InputTypeEmail, "Email", true)
	assert.False(t, node.Attributes.Disabled)

	disabledNode := WithDisabled(node, true)
	assert.True(t, disabledNode.Attributes.Disabled)
}

func TestNodeAttributesChaining(t *testing.T) {
	node := NewInputNode("password", InputTypePassword, "Password", true)
	
	// Chain multiple modifiers
	node = WithPlaceholder(node, "At least 8 characters")
	node = WithMinLength(node, 8)
	node = WithMaxLength(node, 128)
	node = WithPattern(node, ".{8,}")
	node = WithAutocomplete(node, "current-password")

	assert.Equal(t, "At least 8 characters", node.Attributes.Placeholder)
	assert.Equal(t, 8, node.Attributes.MinLength)
	assert.Equal(t, 128, node.Attributes.MaxLength)
	assert.Equal(t, ".{8,}", node.Attributes.Pattern)
	assert.Equal(t, "current-password", node.Attributes.Autocomplete)
}

func TestErrorMessages(t *testing.T) {
	// Test all error message constants are defined
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
			assert.Contains(t, ErrorMessages, errID)
		})
	}
}

func TestInputTypesExtended(t *testing.T) {
	tests := []struct {
		name      string
		inputType InputType
		expected  string
	}{
		{"text", InputTypeText, "text"},
		{"password", InputTypePassword, "password"},
		{"email", InputTypeEmail, "email"},
		{"hidden", InputTypeHidden, "hidden"},
		{"submit", InputTypeSubmit, "submit"},
		{"button", InputTypeButton, "button"},
		{"checkbox", InputTypeCheckbox, "checkbox"},
		{"tel", InputTypeTel, "tel"},
		{"number", InputTypeNumber, "number"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, string(tt.inputType))
		})
	}
}

func TestNodeWithComplexAttributes(t *testing.T) {
	node := Node{
		Type:  NodeTypeInput,
		Group: "credentials",
		Attributes: NodeAttributes{
			Name:         "email",
			Type:         InputTypeEmail,
			Value:        "user@example.com",
			Required:     true,
			Disabled:     false,
			Pattern:      `^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}$`,
			Autocomplete: "email",
			Label:        "Email Address",
			Placeholder:  "Enter your email",
			MaxLength:    255,
			MinLength:    5,
			ID:           "email-input",
		},
		Meta: NodeMeta{
			Label: &TextMeta{
				ID:   "label.email",
				Text: "Email Address",
				Type: "text",
			},
			Description: "Your email address for login",
		},
	}

	assert.Equal(t, "email", node.Attributes.Name)
	assert.Equal(t, InputTypeEmail, node.Attributes.Type)
	assert.Equal(t, "user@example.com", node.Attributes.Value)
	assert.True(t, node.Attributes.Required)
	assert.NotEmpty(t, node.Attributes.Pattern)
	assert.Equal(t, 255, node.Attributes.MaxLength)
	assert.Equal(t, 5, node.Attributes.MinLength)
	assert.NotNil(t, node.Meta.Label)
	assert.Equal(t, "Your email address for login", node.Meta.Description)
}

func BenchmarkGenerateLoginFormNodes(b *testing.B) {
	csrf := "test-csrf-token-benchmark"
	for i := 0; i < b.N; i++ {
		GenerateLoginFormNodes(csrf)
	}
}

func BenchmarkGenerateRegistrationFormNodes(b *testing.B) {
	csrf := "test-csrf-token-benchmark"
	for i := 0; i < b.N; i++ {
		GenerateRegistrationFormNodes(csrf)
	}
}

func BenchmarkNewInputNode(b *testing.B) {
	for i := 0; i < b.N; i++ {
		NewInputNode("email", InputTypeEmail, "Email", true)
	}
}
