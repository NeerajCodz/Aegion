package flows

// NodeType represents the type of UI node
type NodeType string

const (
	NodeTypeInput  NodeType = "input"
	NodeTypeText   NodeType = "text"
	NodeTypeSubmit NodeType = "submit"
	NodeTypeError  NodeType = "error"
	NodeTypeInfo   NodeType = "info"
	NodeTypeScript NodeType = "script"
	NodeTypeImage  NodeType = "image"
	NodeTypeAnchor NodeType = "anchor"
)

// InputType represents the type of input field
type InputType string

const (
	InputTypeText     InputType = "text"
	InputTypePassword InputType = "password"
	InputTypeEmail    InputType = "email"
	InputTypeHidden   InputType = "hidden"
	InputTypeSubmit   InputType = "submit"
	InputTypeButton   InputType = "button"
	InputTypeCheckbox InputType = "checkbox"
	InputTypeTel      InputType = "tel"
	InputTypeNumber   InputType = "number"
)

// Node represents a UI node in a flow form
type Node struct {
	Type       NodeType       `json:"type"`
	Group      string         `json:"group"`
	Attributes NodeAttributes `json:"attributes"`
	Messages   []Msg          `json:"messages,omitempty"`
	Meta       NodeMeta       `json:"meta,omitempty"`
}

// NodeAttributes holds the attributes for a UI node
type NodeAttributes struct {
	Name        string    `json:"name,omitempty"`
	Type        InputType `json:"type,omitempty"`
	Value       any       `json:"value,omitempty"`
	Required    bool      `json:"required,omitempty"`
	Disabled    bool      `json:"disabled,omitempty"`
	Pattern     string    `json:"pattern,omitempty"`
	Autocomplete string   `json:"autocomplete,omitempty"`
	Label       string    `json:"label,omitempty"`
	Placeholder string    `json:"placeholder,omitempty"`
	MaxLength   int       `json:"maxlength,omitempty"`
	MinLength   int       `json:"minlength,omitempty"`
	Href        string    `json:"href,omitempty"`
	Src         string    `json:"src,omitempty"`
	Alt         string    `json:"alt,omitempty"`
	ID          string    `json:"id,omitempty"`
	OnClick     string    `json:"onclick,omitempty"`
	NodeValue   string    `json:"node_value,omitempty"`
}

// NodeMeta holds metadata about a UI node
type NodeMeta struct {
	Label       *TextMeta `json:"label,omitempty"`
	Description string    `json:"description,omitempty"`
}

// TextMeta holds text metadata
type TextMeta struct {
	ID   string `json:"id"`
	Text string `json:"text"`
	Type string `json:"type,omitempty"`
}

// Error message IDs for i18n
const (
	ErrIDInvalidCredentials  = "error.invalid_credentials"
	ErrIDAccountNotFound     = "error.account_not_found"
	ErrIDEmailTaken          = "error.email_taken"
	ErrIDUsernameTaken       = "error.username_taken"
	ErrIDPasswordTooWeak     = "error.password_too_weak"
	ErrIDPasswordMismatch    = "error.password_mismatch"
	ErrIDInvalidEmail        = "error.invalid_email"
	ErrIDSessionExpired      = "error.session_expired"
	ErrIDCSRFInvalid         = "error.csrf_invalid"
	ErrIDFlowExpired         = "error.flow_expired"
	ErrIDRecoveryCodeInvalid = "error.recovery_code_invalid"
	ErrIDTooManyAttempts     = "error.too_many_attempts"
	ErrIDInternalError       = "error.internal"
)

// ErrorMessages maps error IDs to default messages
var ErrorMessages = map[string]string{
	ErrIDInvalidCredentials:  "The credentials provided are invalid.",
	ErrIDAccountNotFound:     "No account found with this identifier.",
	ErrIDEmailTaken:          "This email address is already registered.",
	ErrIDUsernameTaken:       "This username is already taken.",
	ErrIDPasswordTooWeak:     "The password does not meet security requirements.",
	ErrIDPasswordMismatch:    "The passwords do not match.",
	ErrIDInvalidEmail:        "Please enter a valid email address.",
	ErrIDSessionExpired:      "Your session has expired. Please start again.",
	ErrIDCSRFInvalid:         "Security validation failed. Please try again.",
	ErrIDFlowExpired:         "This action has expired. Please start again.",
	ErrIDRecoveryCodeInvalid: "The recovery code is invalid or has expired.",
	ErrIDTooManyAttempts:     "Too many attempts. Please try again later.",
	ErrIDInternalError:       "An unexpected error occurred. Please try again.",
}

// GetErrorMessage returns the error message for a given error ID
func GetErrorMessage(errID string) string {
	if msg, ok := ErrorMessages[errID]; ok {
		return msg
	}
	return ErrorMessages[ErrIDInternalError]
}

// NewInputNode creates a new input node
func NewInputNode(name string, inputType InputType, label string, required bool) Node {
	return Node{
		Type:  NodeTypeInput,
		Group: "default",
		Attributes: NodeAttributes{
			Name:     name,
			Type:     inputType,
			Required: required,
		},
		Meta: NodeMeta{
			Label: &TextMeta{
				ID:   "label." + name,
				Text: label,
			},
		},
	}
}

// NewHiddenNode creates a new hidden input node
func NewHiddenNode(name string, value any) Node {
	return Node{
		Type:  NodeTypeInput,
		Group: "default",
		Attributes: NodeAttributes{
			Name:  name,
			Type:  InputTypeHidden,
			Value: value,
		},
	}
}

// NewSubmitNode creates a new submit button node
func NewSubmitNode(name, label string) Node {
	return Node{
		Type:  NodeTypeSubmit,
		Group: "default",
		Attributes: NodeAttributes{
			Name:  name,
			Type:  InputTypeSubmit,
			Value: label,
		},
		Meta: NodeMeta{
			Label: &TextMeta{
				ID:   "button." + name,
				Text: label,
			},
		},
	}
}

// NewTextNode creates a new text node
func NewTextNode(id, text string) Node {
	return Node{
		Type:  NodeTypeText,
		Group: "default",
		Attributes: NodeAttributes{
			ID:        id,
			NodeValue: text,
		},
	}
}

// NewErrorNode creates a new error node
func NewErrorNode(errID string) Node {
	return Node{
		Type:  NodeTypeError,
		Group: "default",
		Messages: []Msg{
			{
				ID:   errID,
				Type: MsgTypeError,
				Text: GetErrorMessage(errID),
			},
		},
	}
}

// NewInfoNode creates a new info node
func NewInfoNode(id, text string) Node {
	return Node{
		Type:  NodeTypeInfo,
		Group: "default",
		Attributes: NodeAttributes{
			ID:        id,
			NodeValue: text,
		},
	}
}

// NewAnchorNode creates a new anchor/link node
func NewAnchorNode(id, href, text string) Node {
	return Node{
		Type:  NodeTypeAnchor,
		Group: "default",
		Attributes: NodeAttributes{
			ID:        id,
			Href:      href,
			NodeValue: text,
		},
	}
}

// GenerateLoginFormNodes generates the form nodes for a login flow
func GenerateLoginFormNodes(csrfToken string) []Node {
	return []Node{
		NewHiddenNode("csrf_token", csrfToken),
		NewInputNode("identifier", InputTypeEmail, "Email or Username", true),
		NewInputNode("password", InputTypePassword, "Password", true),
		NewSubmitNode("method", "Sign In"),
	}
}

// GenerateRegistrationFormNodes generates the form nodes for a registration flow
func GenerateRegistrationFormNodes(csrfToken string) []Node {
	return []Node{
		NewHiddenNode("csrf_token", csrfToken),
		NewInputNode("email", InputTypeEmail, "Email", true),
		NewInputNode("password", InputTypePassword, "Password", true),
		NewInputNode("password_confirm", InputTypePassword, "Confirm Password", true),
		NewSubmitNode("method", "Create Account"),
	}
}

// GenerateRecoveryFormNodes generates the form nodes for a recovery flow
func GenerateRecoveryFormNodes(csrfToken string) []Node {
	return []Node{
		NewHiddenNode("csrf_token", csrfToken),
		NewInputNode("email", InputTypeEmail, "Email", true),
		NewSubmitNode("method", "Send Recovery Link"),
	}
}

// GenerateSettingsFormNodes generates the form nodes for a settings flow
func GenerateSettingsFormNodes(csrfToken string) []Node {
	return []Node{
		NewHiddenNode("csrf_token", csrfToken),
		NewInputNode("current_password", InputTypePassword, "Current Password", true),
		NewInputNode("new_password", InputTypePassword, "New Password", true),
		NewInputNode("new_password_confirm", InputTypePassword, "Confirm New Password", true),
		NewSubmitNode("method", "Update Password"),
	}
}

// GenerateVerificationFormNodes generates the form nodes for a verification flow
func GenerateVerificationFormNodes(csrfToken string) []Node {
	return []Node{
		NewHiddenNode("csrf_token", csrfToken),
		NewInputNode("code", InputTypeText, "Verification Code", true),
		NewSubmitNode("method", "Verify"),
	}
}

// AddNodeToGroup adds a node to a specific group
func AddNodeToGroup(node Node, group string) Node {
	node.Group = group
	return node
}

// WithAutocomplete sets the autocomplete attribute on a node
func WithAutocomplete(node Node, autocomplete string) Node {
	node.Attributes.Autocomplete = autocomplete
	return node
}

// WithPlaceholder sets the placeholder attribute on a node
func WithPlaceholder(node Node, placeholder string) Node {
	node.Attributes.Placeholder = placeholder
	return node
}

// WithPattern sets the pattern attribute on a node
func WithPattern(node Node, pattern string) Node {
	node.Attributes.Pattern = pattern
	return node
}

// WithMinLength sets the minlength attribute on a node
func WithMinLength(node Node, minLen int) Node {
	node.Attributes.MinLength = minLen
	return node
}

// WithMaxLength sets the maxlength attribute on a node
func WithMaxLength(node Node, maxLen int) Node {
	node.Attributes.MaxLength = maxLen
	return node
}

// WithDisabled sets the disabled attribute on a node
func WithDisabled(node Node, disabled bool) Node {
	node.Attributes.Disabled = disabled
	return node
}
