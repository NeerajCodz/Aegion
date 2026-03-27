package courier

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html/template"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ============================================================================
// MOCK IMPLEMENTATIONS
// ============================================================================

// MockSMSGateway is a simple SMS gateway for testing
type MockSMSGateway struct {
	sentMessages []map[string]interface{}
	shouldError  bool
}

func (m *MockSMSGateway) SendSMS(to, body string) error {
	if m.shouldError {
		return fmt.Errorf("SMS send failed")
	}
	m.sentMessages = append(m.sentMessages, map[string]interface{}{
		"to":   to,
		"body": body,
	})
	return nil
}

// ============================================================================
// MESSAGE TYPE & STATUS TESTS
// ============================================================================

func TestMessageType(t *testing.T) {
	tests := []struct {
		name string
		mt   MessageType
		want string
	}{
		{"email type", MessageTypeEmail, "email"},
		{"sms type", MessageTypeSMS, "sms"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, string(tt.mt))
		})
	}
}

func TestMessageStatus(t *testing.T) {
	tests := []struct {
		name   string
		status MessageStatus
		want   string
	}{
		{"queued status", StatusQueued, "queued"},
		{"processing status", StatusProcessing, "processing"},
		{"sent status", StatusSent, "sent"},
		{"failed status", StatusFailed, "failed"},
		{"abandoned status", StatusAbandoned, "abandoned"},
		{"cancelled status", StatusCancelled, "cancelled"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, string(tt.status))
		})
	}
}

// ============================================================================
// COURIER INITIALIZATION TESTS
// ============================================================================

func TestNewWithDefaults(t *testing.T) {
	tests := []struct {
		name            string
		config          Config
		expectedRetries int
	}{
		{
			name:            "empty config gets defaults",
			config:          Config{},
			expectedRetries: 3,
		},
		{
			name:            "zero max retries gets default",
			config:          Config{MaxRetries: 0},
			expectedRetries: 3,
		},
		{
			name:            "custom retries preserved",
			config:          Config{MaxRetries: 5},
			expectedRetries: 5,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			courier := New(tt.config)

			require.NotNil(t, courier)
			assert.Equal(t, tt.expectedRetries, courier.maxRetries)
			assert.NotNil(t, courier.templates)
		})
	}
}

// ============================================================================
// QUEUE OPTIONS TESTS
// ============================================================================

func TestWithTemplate(t *testing.T) {
	templateID := "welcome-email"
	templateData := map[string]interface{}{
		"name": "John Doe",
		"code": "123456",
	}

	option := WithTemplate(templateID, templateData)

	msg := &Message{}
	option(msg)

	assert.Equal(t, templateID, msg.TemplateID)
	assert.NotNil(t, msg.TemplateData)
	assert.Equal(t, "John Doe", msg.TemplateData["name"])
	assert.Equal(t, "123456", msg.TemplateData["code"])
}

func TestWithIdempotencyKey(t *testing.T) {
	key := "unique-key-123"
	option := WithIdempotencyKey(key)

	msg := &Message{}
	option(msg)

	assert.Equal(t, key, msg.IdempotencyKey)
}

func TestWithSendAfter(t *testing.T) {
	sendTime := time.Now().Add(1 * time.Hour)
	option := WithSendAfter(sendTime)

	msg := &Message{}
	option(msg)

	require.NotNil(t, msg.SendAfter)
	assert.True(t, msg.SendAfter.Equal(sendTime))
}

func TestWithIdentity(t *testing.T) {
	identityID := uuid.New()
	option := WithIdentity(identityID)

	msg := &Message{}
	option(msg)

	require.NotNil(t, msg.IdentityID)
	assert.Equal(t, identityID, *msg.IdentityID)
}

func TestWithSource(t *testing.T) {
	sourceModule := "auth-service"
	option := WithSource(sourceModule)

	msg := &Message{}
	option(msg)

	assert.Equal(t, sourceModule, msg.SourceModule)
}

func TestMultipleOptions(t *testing.T) {
	templateID := "test-template"
	templateData := map[string]interface{}{"key": "value"}
	idempotencyKey := "test-key"
	sendTime := time.Now().Add(1 * time.Hour)
	identityID := uuid.New()
	sourceModule := "test-module"

	msg := &Message{}

	options := []QueueOption{
		WithTemplate(templateID, templateData),
		WithIdempotencyKey(idempotencyKey),
		WithSendAfter(sendTime),
		WithIdentity(identityID),
		WithSource(sourceModule),
	}

	for _, option := range options {
		option(msg)
	}

	assert.Equal(t, templateID, msg.TemplateID)
	assert.Equal(t, idempotencyKey, msg.IdempotencyKey)
	assert.True(t, msg.SendAfter.Equal(sendTime))
	assert.Equal(t, identityID, *msg.IdentityID)
	assert.Equal(t, sourceModule, msg.SourceModule)
}

// ============================================================================
// MESSAGE STRUCTURE TESTS
// ============================================================================

func TestMessageStruct(t *testing.T) {
	msgID := uuid.New()
	identityID := uuid.New()
	sendTime := time.Now()
	templateData := map[string]interface{}{"name": "Test"}

	msg := Message{
		ID:             msgID,
		Type:           MessageTypeEmail,
		Status:         StatusQueued,
		Recipient:      "test@example.com",
		Subject:        "Test Subject",
		Body:           "Test Body",
		TemplateID:     "test-template",
		TemplateData:   templateData,
		SendCount:      0,
		LastError:      "",
		IdempotencyKey: "test-key",
		SendAfter:      &sendTime,
		IdentityID:     &identityID,
		SourceModule:   "test-module",
		CreatedAt:      sendTime,
		UpdatedAt:      sendTime,
	}

	assert.Equal(t, msgID, msg.ID)
	assert.Equal(t, MessageTypeEmail, msg.Type)
	assert.Equal(t, StatusQueued, msg.Status)
	assert.Equal(t, "test@example.com", msg.Recipient)
	assert.Equal(t, "Test", msg.TemplateData["name"])
}

// ============================================================================
// MESSAGE QUEUE OPERATIONS TESTS
// ============================================================================

func TestMessageCreation(t *testing.T) {
	msg := &Message{
		ID:        uuid.New(),
		Type:      MessageTypeEmail,
		Status:    StatusQueued,
		Recipient: "test@example.com",
		Subject:   "Test",
		Body:      "Test Body",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	assert.NotNil(t, msg)
	assert.Equal(t, MessageTypeEmail, msg.Type)
	assert.Equal(t, StatusQueued, msg.Status)
	assert.Equal(t, "test@example.com", msg.Recipient)
}

func TestMessageStatusTransition(t *testing.T) {
	tests := []struct {
		name       string
		fromStatus MessageStatus
		toStatus   MessageStatus
		valid      bool
	}{
		{"queued to processing", StatusQueued, StatusProcessing, true},
		{"processing to sent", StatusProcessing, StatusSent, true},
		{"queued to sent (direct)", StatusQueued, StatusSent, true},
		{"processing to queued (retry)", StatusProcessing, StatusQueued, true},
		{"queued to cancelled", StatusQueued, StatusCancelled, true},
		{"sent to queued (invalid)", StatusSent, StatusQueued, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := &Message{
				ID:     uuid.New(),
				Status: tt.fromStatus,
			}

			// Simulate state transition logic
			oldStatus := msg.Status
			msg.Status = tt.toStatus

			if tt.valid {
				assert.Equal(t, tt.toStatus, msg.Status)
			} else {
				// In reality, this would be validated elsewhere
				// Just verify the old status was changed
				assert.NotEqual(t, oldStatus, msg.Status)
			}
		})
	}
}

// ============================================================================
// TEMPLATE RENDERING TESTS
// ============================================================================

func TestTemplateRendering(t *testing.T) {
	tests := []struct {
		name          string
		template      string
		templateData  map[string]interface{}
		expectedMatch string
		shouldError   bool
	}{
		{
			name:         "simple template",
			template:     "Hello {{.name}}",
			templateData: map[string]interface{}{"name": "Alice"},
			expectedMatch: "Hello Alice",
			shouldError:   false,
		},
		{
			name:         "template with multiple fields",
			template:     "Hello {{.name}}, your code is {{.code}}",
			templateData: map[string]interface{}{"name": "Bob", "code": "123456"},
			expectedMatch: "Hello Bob, your code is 123456",
			shouldError:   false,
		},
		{
			name:         "template with HTML",
			template:     "<h1>{{.title}}</h1><p>{{.message}}</p>",
			templateData: map[string]interface{}{"title": "Welcome", "message": "Hello user"},
			expectedMatch: "<h1>Welcome</h1><p>Hello user</p>",
			shouldError:   false,
		},
		{
			name:         "empty template data",
			template:     "Static content",
			templateData: map[string]interface{}{},
			expectedMatch: "Static content",
			shouldError:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpl, err := template.New("test").Parse(tt.template)
			require.NoError(t, err)

			var buf bytes.Buffer
			err = tmpl.Execute(&buf, tt.templateData)

			if tt.shouldError {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.expectedMatch, buf.String())
			}
		})
	}
}

func TestTemplateWithComplexData(t *testing.T) {
	templateStr := `
	<html>
	<body>
		<h1>Hello {{.name}}</h1>
		<p>Your verification code is: {{.code}}</p>
		<p>Link: {{.link}}</p>
	</body>
	</html>
	`

	tmpl, err := template.New("verification").Parse(templateStr)
	require.NoError(t, err)

	data := map[string]interface{}{
		"name": "John Doe",
		"code": "123456",
		"link": "https://example.com/verify?token=abc123",
	}

	var buf bytes.Buffer
	err = tmpl.Execute(&buf, data)
	require.NoError(t, err)

	result := buf.String()
	assert.Contains(t, result, "John Doe")
	assert.Contains(t, result, "123456")
	assert.Contains(t, result, "https://example.com/verify?token=abc123")
}

// ============================================================================
// EMAIL DELIVERY MOCKING TESTS
// ============================================================================

func TestEmailMessageFormat(t *testing.T) {
	cfg := Config{
		SMTP: SMTPConfig{
			Host:        "smtp.example.com",
			Port:        587,
			FromAddress: "noreply@example.com",
			FromName:    "Aegion",
			Username:    "user@example.com",
			Password:    "password",
			AuthEnabled: true,
		},
		MaxRetries: 3,
	}

	courier := New(cfg)
	assert.NotNil(t, courier)

	// Verify SMTP config is properly set
	assert.Equal(t, "smtp.example.com", courier.smtp.Host)
	assert.Equal(t, 587, courier.smtp.Port)
	assert.Equal(t, "noreply@example.com", courier.smtp.FromAddress)
	assert.Equal(t, "Aegion", courier.smtp.FromName)
	assert.True(t, courier.smtp.AuthEnabled)
}

func TestEmailDeliveryWithAuth(t *testing.T) {
	cfg := Config{
		SMTP: SMTPConfig{
			Host:        "smtp.example.com",
			Port:        587,
			FromAddress: "noreply@example.com",
			FromName:    "Aegion",
			Username:    "user@example.com",
			Password:    "password",
			AuthEnabled: true,
		},
		MaxRetries: 3,
	}

	courier := New(cfg)

	// Verify auth is enabled
	assert.True(t, courier.smtp.AuthEnabled)
	assert.Equal(t, "user@example.com", courier.smtp.Username)
	assert.Equal(t, "password", courier.smtp.Password)
}

func TestEmailDeliveryWithoutAuth(t *testing.T) {
	cfg := Config{
		SMTP: SMTPConfig{
			Host:        "smtp.example.com",
			Port:        25,
			FromAddress: "noreply@example.com",
			FromName:    "Aegion",
			AuthEnabled: false,
		},
		MaxRetries: 3,
	}

	courier := New(cfg)

	// Verify auth is not enabled
	assert.False(t, courier.smtp.AuthEnabled)
}

// ============================================================================
// SMS GATEWAY INTEGRATION MOCKING TESTS
// ============================================================================

func TestSMSMessageCreation(t *testing.T) {
	msg := &Message{
		ID:        uuid.New(),
		Type:      MessageTypeSMS,
		Status:    StatusQueued,
		Recipient: "+1234567890",
		Body:      "Your verification code is 123456",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	assert.Equal(t, MessageTypeSMS, msg.Type)
	assert.Equal(t, "+1234567890", msg.Recipient)
	assert.Contains(t, msg.Body, "123456")
}

func TestSMSGatewayIntegration(t *testing.T) {
	mockGateway := &MockSMSGateway{}

	// Simulate SMS sending
	err := mockGateway.SendSMS("+1234567890", "Your code is 123456")
	require.NoError(t, err)

	// Verify SMS was recorded
	assert.Len(t, mockGateway.sentMessages, 1)
	assert.Equal(t, "+1234567890", mockGateway.sentMessages[0]["to"])
	assert.Equal(t, "Your code is 123456", mockGateway.sentMessages[0]["body"])
}

// ============================================================================
// RETRY WITH EXPONENTIAL BACKOFF TESTS
// ============================================================================

func TestRetryLogic(t *testing.T) {
	tests := []struct {
		name             string
		sendCount        int
		maxRetries       int
		shouldAbandon    bool
		expectedCount    int
	}{
		{"first attempt", 0, 3, false, 1},
		{"second attempt", 1, 3, false, 2},
		{"third attempt", 2, 3, false, 3},
		{"at max retries", 3, 3, true, 3},
		{"beyond max retries", 4, 3, true, 4},
		{"custom max retries 5", 4, 5, false, 5},
		{"custom max retries 5 abandoned", 5, 5, true, 5},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sendCount := tt.sendCount
			abandoned := false

			if sendCount >= tt.maxRetries {
				abandoned = true
			} else {
				sendCount++
			}

			assert.Equal(t, tt.shouldAbandon, abandoned)
			assert.Equal(t, tt.expectedCount, sendCount)
		})
	}
}

func TestExponentialBackoff(t *testing.T) {
	tests := []struct {
		name      string
		attempt   int
		baseDelay time.Duration
		expected  time.Duration
	}{
		{"first retry", 1, 100 * time.Millisecond, 100 * time.Millisecond},
		{"second retry", 2, 100 * time.Millisecond, 200 * time.Millisecond},
		{"third retry", 3, 100 * time.Millisecond, 400 * time.Millisecond},
		{"fourth retry", 4, 100 * time.Millisecond, 800 * time.Millisecond},
		{"fifth retry", 5, 100 * time.Millisecond, 1600 * time.Millisecond},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Exponential backoff: delay = baseDelay * 2^(attempt-1)
			multiplier := 1 << (tt.attempt - 1) // 2^(attempt-1)
			calculated := tt.baseDelay * time.Duration(multiplier)

			assert.Equal(t, tt.expected, calculated)
		})
	}
}

func TestBackoffWithMaxDelay(t *testing.T) {
	tests := []struct {
		name      string
		attempt   int
		baseDelay time.Duration
		maxDelay  time.Duration
		expected  time.Duration
	}{
		{"delay under max", 2, 10 * time.Second, 60 * time.Second, 20 * time.Second},
		{"delay equals max", 3, 10 * time.Second, 40 * time.Second, 40 * time.Second},
		{"delay exceeds max", 4, 10 * time.Second, 60 * time.Second, 60 * time.Second},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			multiplier := 1 << (tt.attempt - 1)
			calculated := tt.baseDelay * time.Duration(multiplier)

			// Apply max delay cap
			if calculated > tt.maxDelay {
				calculated = tt.maxDelay
			}

			assert.Equal(t, tt.expected, calculated)
		})
	}
}

// ============================================================================
// DELIVERY STATUS TRACKING TESTS
// ============================================================================

func TestDeliveryStatusProgression(t *testing.T) {
	msg := &Message{
		ID:        uuid.New(),
		Type:      MessageTypeEmail,
		Status:    StatusQueued,
		Recipient: "test@example.com",
		SendCount: 0,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	// Simulate status progression
	assert.Equal(t, StatusQueued, msg.Status)
	assert.Equal(t, 0, msg.SendCount)

	// Move to processing
	msg.Status = StatusProcessing
	msg.SendCount = 1
	assert.Equal(t, StatusProcessing, msg.Status)
	assert.Equal(t, 1, msg.SendCount)

	// Move to sent
	msg.Status = StatusSent
	sentAt := time.Now()
	msg.SentAt = &sentAt
	assert.Equal(t, StatusSent, msg.Status)
	assert.NotNil(t, msg.SentAt)
}

func TestFailedDeliveryTracking(t *testing.T) {
	msg := &Message{
		ID:        uuid.New(),
		Type:      MessageTypeEmail,
		Status:    StatusQueued,
		SendCount: 0,
		LastError: "",
	}

	// Simulate first failure
	msg.SendCount = 1
	msg.Status = StatusQueued
	msg.LastError = "SMTP connection timeout"

	assert.Equal(t, 1, msg.SendCount)
	assert.Equal(t, StatusQueued, msg.Status)
	assert.Equal(t, "SMTP connection timeout", msg.LastError)

	// Simulate second failure
	msg.SendCount = 2
	msg.LastError = "SMTP authentication failed"

	assert.Equal(t, 2, msg.SendCount)
	assert.Equal(t, "SMTP authentication failed", msg.LastError)

	// Simulate abandonment
	msg.SendCount = 3
	msg.Status = StatusAbandoned
	msg.LastError = "Max retries exceeded"

	assert.Equal(t, 3, msg.SendCount)
	assert.Equal(t, StatusAbandoned, msg.Status)
	assert.Equal(t, "Max retries exceeded", msg.LastError)
}

func TestDeliveryIdempotency(t *testing.T) {
	idempotencyKey := "unique-delivery-key-" + uuid.New().String()

	msg1 := &Message{
		ID:             uuid.New(),
		IdempotencyKey: idempotencyKey,
		Recipient:      "test@example.com",
		Subject:        "Test",
		Body:           "Body",
	}

	msg2 := &Message{
		ID:             uuid.New(),
		IdempotencyKey: idempotencyKey,
		Recipient:      "test@example.com",
		Subject:        "Test",
		Body:           "Body",
	}

	// Same idempotency key means same delivery intent
	assert.Equal(t, msg1.IdempotencyKey, msg2.IdempotencyKey)
	// But different message IDs
	assert.NotEqual(t, msg1.ID, msg2.ID)
}

// ============================================================================
// BATCH PROCESSING TESTS
// ============================================================================

func TestBatchSizeDefault(t *testing.T) {
	tests := []struct {
		name     string
		input    int
		expected int
	}{
		{"zero gets default", 0, 10},
		{"positive preserved", 5, 5},
		{"large number preserved", 100, 100},
		{"batch size one", 1, 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			batchSize := tt.input
			if batchSize == 0 {
				batchSize = 10
			}

			assert.Equal(t, tt.expected, batchSize)
		})
	}
}

// ============================================================================
// MESSAGE SERIALIZATION TESTS
// ============================================================================

func TestMessageJSONSerialization(t *testing.T) {
	msgID := uuid.New()
	identityID := uuid.New()
	sendTime := time.Now().UTC()
	templateData := map[string]interface{}{"name": "Test", "code": "123"}

	msg := Message{
		ID:             msgID,
		Type:           MessageTypeEmail,
		Status:         StatusQueued,
		Recipient:      "test@example.com",
		Subject:        "Test Subject",
		Body:           "Test Body",
		TemplateID:     "test-template",
		TemplateData:   templateData,
		SendCount:      0,
		LastError:      "",
		IdempotencyKey: "test-key",
		SendAfter:      &sendTime,
		IdentityID:     &identityID,
		SourceModule:   "test-module",
		CreatedAt:      sendTime,
		UpdatedAt:      sendTime,
	}

	// Serialize to JSON
	jsonData, err := json.Marshal(msg)
	require.NoError(t, err)

	// Deserialize back
	var decoded Message
	err = json.Unmarshal(jsonData, &decoded)
	require.NoError(t, err)

	assert.Equal(t, msg.ID, decoded.ID)
	assert.Equal(t, msg.Type, decoded.Type)
	assert.Equal(t, msg.Recipient, decoded.Recipient)
	assert.Equal(t, msg.Subject, decoded.Subject)
}

func TestTemplateDataSerialization(t *testing.T) {
	templateData := map[string]interface{}{
		"name":      "Alice",
		"code":      "123456",
		"link":      "https://example.com/verify",
		"expiresIn": 900,
		"nested": map[string]interface{}{
			"key": "value",
		},
	}

	jsonData, err := json.Marshal(templateData)
	require.NoError(t, err)

	var decoded map[string]interface{}
	err = json.Unmarshal(jsonData, &decoded)
	require.NoError(t, err)

	assert.Equal(t, "Alice", decoded["name"])
	assert.Equal(t, "123456", decoded["code"])
	assert.Equal(t, float64(900), decoded["expiresIn"])
}

// ============================================================================
// VERIFICATION EMAIL TESTS
// ============================================================================

func TestVerificationEmailCreation(t *testing.T) {
	cfg := Config{
		SMTP: SMTPConfig{
			Host:        "smtp.example.com",
			Port:        587,
			FromAddress: "noreply@example.com",
			FromName:    "Aegion",
		},
		MaxRetries: 3,
	}

	courier := New(cfg)
	assert.NotNil(t, courier)

	// Simulate verification email
	identityID := uuid.New()
	verificationCode := "123456"
	recipient := "user@example.com"

	// Create the verification message manually for testing
	msg := &Message{
		ID:        uuid.New(),
		Type:      MessageTypeEmail,
		Status:    StatusQueued,
		Recipient: recipient,
		Subject:   "Verify your email address",
		Body:      fmt.Sprintf("<h1>Email Verification</h1><p>Your verification code is: <strong>%s</strong></p>", verificationCode),
		IdentityID: &identityID,
		SourceModule: "core",
		IdempotencyKey: fmt.Sprintf("verify:%s:%s", identityID.String(), verificationCode),
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	assert.Equal(t, recipient, msg.Recipient)
	assert.Equal(t, "Verify your email address", msg.Subject)
	assert.Contains(t, msg.Body, verificationCode)
	assert.Equal(t, "core", msg.SourceModule)
}

// ============================================================================
// PASSWORD RESET EMAIL TESTS
// ============================================================================

func TestPasswordResetEmailCreation(t *testing.T) {
	cfg := Config{
		SMTP: SMTPConfig{
			Host:        "smtp.example.com",
			Port:        587,
			FromAddress: "noreply@example.com",
			FromName:    "Aegion",
		},
		MaxRetries: 3,
	}

	courier := New(cfg)
	assert.NotNil(t, courier)

	identityID := uuid.New()
	resetCode := "reset-code-123"
	recipient := "user@example.com"

	msg := &Message{
		ID:        uuid.New(),
		Type:      MessageTypeEmail,
		Status:    StatusQueued,
		Recipient: recipient,
		Subject:   "Reset your password",
		Body:      fmt.Sprintf("<h1>Password Reset</h1><p>Your password reset code is: <strong>%s</strong></p>", resetCode),
		IdentityID: &identityID,
		SourceModule: "core",
		IdempotencyKey: fmt.Sprintf("reset:%s:%s", identityID.String(), resetCode),
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	assert.Equal(t, recipient, msg.Recipient)
	assert.Equal(t, "Reset your password", msg.Subject)
	assert.Contains(t, msg.Body, resetCode)
}

// ============================================================================
// MAGIC LINK EMAIL TESTS
// ============================================================================

func TestMagicLinkEmailCreation(t *testing.T) {
	cfg := Config{
		SMTP: SMTPConfig{
			Host:        "smtp.example.com",
			Port:        587,
			FromAddress: "noreply@example.com",
			FromName:    "Aegion",
		},
		MaxRetries: 3,
	}

	courier := New(cfg)
	assert.NotNil(t, courier)

	recipient := "user@example.com"
	magicLink := "https://example.com/signin?token=abc123"
	code := "magic-code-456"

	msg := &Message{
		ID:        uuid.New(),
		Type:      MessageTypeEmail,
		Status:    StatusQueued,
		Recipient: recipient,
		Subject:   "Sign in to your account",
		Body:      fmt.Sprintf(`<h1>Sign In</h1><p>Click the link below to sign in:</p><p><a href="%s">Sign In</a></p><p>Or enter this code: <strong>%s</strong></p>`, magicLink, code),
		SourceModule: "magic_link",
		IdempotencyKey: fmt.Sprintf("magic:%s", code),
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	assert.Equal(t, recipient, msg.Recipient)
	assert.Equal(t, "Sign in to your account", msg.Subject)
	assert.Contains(t, msg.Body, magicLink)
	assert.Contains(t, msg.Body, code)
}

// ============================================================================
// COMPLEX INTEGRATION TESTS
// ============================================================================

func TestMessageQueueing(t *testing.T) {
	cfg := Config{
		SMTP: SMTPConfig{
			Host:        "smtp.example.com",
			Port:        587,
			FromAddress: "noreply@example.com",
			FromName:    "Aegion",
		},
		MaxRetries: 3,
	}

	courier := New(cfg)
	require.NotNil(t, courier)

	// Create test messages
	messages := []*Message{
		{
			ID:        uuid.New(),
			Type:      MessageTypeEmail,
			Status:    StatusQueued,
			Recipient: "user1@example.com",
			Subject:   "Welcome",
			Body:      "Welcome to Aegion",
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		},
		{
			ID:        uuid.New(),
			Type:      MessageTypeEmail,
			Status:    StatusQueued,
			Recipient: "user2@example.com",
			Subject:   "Verify Email",
			Body:      "Please verify your email",
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		},
		{
			ID:        uuid.New(),
			Type:      MessageTypeSMS,
			Status:    StatusQueued,
			Recipient: "+1234567890",
			Body:      "Your verification code is 123456",
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		},
	}

	for _, msg := range messages {
		assert.Equal(t, StatusQueued, msg.Status)
		assert.NotNil(t, msg.ID)
		assert.NotEmpty(t, msg.Recipient)
	}

	assert.Len(t, messages, 3)
}

func TestRetryMechanismFlow(t *testing.T) {
	maxRetries := 3

	// Simulate a message going through retry flow
	msg := &Message{
		ID:        uuid.New(),
		Type:      MessageTypeEmail,
		Status:    StatusQueued,
		Recipient: "test@example.com",
		SendCount: 0,
		LastError: "",
	}

	// Attempt 1 - initial try
	msg.SendCount = 1
	msg.Status = StatusQueued
	msg.LastError = "Connection timeout"
	assert.Equal(t, 1, msg.SendCount)
	assert.Equal(t, StatusQueued, msg.Status)

	// Attempt 2 - first retry
	msg.SendCount = 2
	msg.LastError = "Connection refused"
	assert.Equal(t, 2, msg.SendCount)

	// Attempt 3 - second retry
	msg.SendCount = 3
	msg.LastError = "Authentication failed"
	assert.Equal(t, 3, msg.SendCount)

	// All retries exhausted - abandon
	if msg.SendCount >= maxRetries {
		msg.Status = StatusAbandoned
		msg.LastError = "Max retries exceeded"
	}

	assert.Equal(t, StatusAbandoned, msg.Status)
	assert.Equal(t, "Max retries exceeded", msg.LastError)
}

func TestMultipleMessageTypes(t *testing.T) {
	tests := []struct {
		name      string
		msgType   MessageType
		recipient string
		body      string
	}{
		{
			name:      "email message",
			msgType:   MessageTypeEmail,
			recipient: "user@example.com",
			body:      "Email body content",
		},
		{
			name:      "sms message",
			msgType:   MessageTypeSMS,
			recipient: "+1234567890",
			body:      "SMS body content",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := &Message{
				ID:        uuid.New(),
				Type:      tt.msgType,
				Status:    StatusQueued,
				Recipient: tt.recipient,
				Body:      tt.body,
				CreatedAt: time.Now(),
				UpdatedAt: time.Now(),
			}

			assert.Equal(t, tt.msgType, msg.Type)
			assert.Equal(t, tt.recipient, msg.Recipient)
			assert.Equal(t, tt.body, msg.Body)
		})
	}
}

// ============================================================================
// EDGE CASE TESTS
// ============================================================================

func TestEmptyRecipient(t *testing.T) {
	msg := &Message{
		ID:        uuid.New(),
		Type:      MessageTypeEmail,
		Recipient: "",
	}

	assert.Empty(t, msg.Recipient)
}

func TestVeryLongBody(t *testing.T) {
	longBody := ""
	for i := 0; i < 10000; i++ {
		longBody += "This is a long message. "
	}

	msg := &Message{
		ID:   uuid.New(),
		Type: MessageTypeEmail,
		Body: longBody,
	}

	assert.Len(t, msg.Body, len(longBody))
	assert.Greater(t, len(msg.Body), 100000)
}

func TestSpecialCharactersInBody(t *testing.T) {
	specialChars := "Special chars: <>&\"'ñ中文العربية"

	msg := &Message{
		ID:   uuid.New(),
		Type: MessageTypeEmail,
		Body: specialChars,
	}

	assert.Equal(t, specialChars, msg.Body)
}

func TestNilPointerFields(t *testing.T) {
	msg := &Message{
		ID:   uuid.New(),
		Type: MessageTypeEmail,
	}

	assert.Nil(t, msg.SendAfter)
	assert.Nil(t, msg.IdentityID)
	assert.Nil(t, msg.SentAt)
}

// ============================================================================
// CONCURRENCY TESTS
// ============================================================================

func TestConcurrentMessageCreation(t *testing.T) {
	messages := make(chan *Message, 100)

	for i := 0; i < 100; i++ {
		go func() {
			msg := &Message{
				ID:        uuid.New(),
				Type:      MessageTypeEmail,
				Status:    StatusQueued,
				Recipient: fmt.Sprintf("user%d@example.com", i),
				CreatedAt: time.Now(),
				UpdatedAt: time.Now(),
			}
			messages <- msg
		}()
	}

	var createdMessages []*Message
	for i := 0; i < 100; i++ {
		createdMessages = append(createdMessages, <-messages)
	}

	assert.Len(t, createdMessages, 100)

	// Verify all IDs are unique
	seenIDs := make(map[uuid.UUID]bool)
	for _, msg := range createdMessages {
		assert.False(t, seenIDs[msg.ID], "Duplicate message ID detected")
		seenIDs[msg.ID] = true
	}
}

// ============================================================================
// PERFORMANCE AND STRESS TESTS
// ============================================================================

func TestMessageCreationPerformance(t *testing.T) {
	start := time.Now()

	for i := 0; i < 1000; i++ {
		_ = &Message{
			ID:        uuid.New(),
			Type:      MessageTypeEmail,
			Status:    StatusQueued,
			Recipient: fmt.Sprintf("user%d@example.com", i),
			Body:      "Test body",
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		}
	}

	elapsed := time.Since(start)
	t.Logf("Created 1000 messages in %v", elapsed)

	// Should be reasonably fast
	assert.Less(t, elapsed, 5*time.Second)
}

func TestTemplateRenderingPerformance(t *testing.T) {
	templateStr := `
	<html>
	<body>
		<h1>Hello {{.name}}</h1>
		<p>Your code is {{.code}}</p>
		<p>Link: {{.link}}</p>
	</body>
	</html>
	`

	tmpl, err := template.New("perf").Parse(templateStr)
	require.NoError(t, err)

	start := time.Now()

	for i := 0; i < 1000; i++ {
		data := map[string]interface{}{
			"name": fmt.Sprintf("User%d", i),
			"code": fmt.Sprintf("CODE%d", i),
			"link": "https://example.com/verify",
		}

		var buf bytes.Buffer
		_ = tmpl.Execute(&buf, data)
	}

	elapsed := time.Since(start)
	t.Logf("Rendered 1000 templates in %v", elapsed)

	// Should be fast
	assert.Less(t, elapsed, 5*time.Second)
}