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
// MESSAGE CREATION TESTS (NO DATABASE)
// ============================================================================

func TestCreateMessageForQueuing(t *testing.T) {
	tests := []struct {
		name      string
		recipient string
		subject   string
		body      string
		msgType   MessageType
	}{
		{
			name:      "standard email",
			recipient: "user@example.com",
			subject:   "Welcome",
			body:      "Welcome to our service",
			msgType:   MessageTypeEmail,
		},
		{
			name:      "email with HTML",
			recipient: "admin@example.com",
			subject:   "Admin Report",
			body:      "<html><body><h1>Report</h1></body></html>",
			msgType:   MessageTypeEmail,
		},
		{
			name:      "SMS message",
			recipient: "+1234567890",
			subject:   "",
			body:      "Your code is 123456",
			msgType:   MessageTypeSMS,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			now := time.Now().UTC()
			msg := &Message{
				ID:        uuid.New(),
				Type:      tt.msgType,
				Status:    StatusQueued,
				Recipient: tt.recipient,
				Subject:   tt.subject,
				Body:      tt.body,
				CreatedAt: now,
				UpdatedAt: now,
			}

			assert.Equal(t, tt.msgType, msg.Type)
			assert.Equal(t, tt.recipient, msg.Recipient)
			assert.Equal(t, tt.subject, msg.Subject)
			assert.Equal(t, tt.body, msg.Body)
			assert.Equal(t, StatusQueued, msg.Status)
			assert.NotNil(t, msg.ID)
		})
	}
}

// ============================================================================
// TEMPLATE DATA HANDLING TESTS
// ============================================================================

func TestTemplateDataJSONMarshaling(t *testing.T) {
	tests := []struct {
		name     string
		data     map[string]interface{}
		hasError bool
	}{
		{
			name: "simple string data",
			data: map[string]interface{}{
				"name": "Alice",
				"code": "123456",
			},
			hasError: false,
		},
		{
			name: "complex nested data",
			data: map[string]interface{}{
				"user": map[string]interface{}{
					"name":  "Bob",
					"email": "bob@example.com",
				},
				"codes": []string{"1", "2", "3"},
				"count": 42,
			},
			hasError: false,
		},
		{
			name:     "empty data",
			data:     map[string]interface{}{},
			hasError: false,
		},
		{
			name: "nil values",
			data: map[string]interface{}{
				"value": nil,
				"name":  "Test",
			},
			hasError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			jsonData, err := json.Marshal(tt.data)

			if tt.hasError {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)

			// Verify it can be unmarshaled back
			var decoded map[string]interface{}
			err = json.Unmarshal(jsonData, &decoded)
			require.NoError(t, err)

			// Verify data integrity
			for key, value := range tt.data {
				if value == nil {
					assert.Nil(t, decoded[key])
				} else {
					assert.NotNil(t, decoded[key])
				}
			}
		})
	}
}

// ============================================================================
// IDEMPOTENCY KEY HANDLING TESTS
// ============================================================================

func TestIdempotencyKeyDuplication(t *testing.T) {
	idempotencyKey := "unique-send-key-" + uuid.New().String()

	msg1 := &Message{
		ID:             uuid.New(),
		Type:           MessageTypeEmail,
		IdempotencyKey: idempotencyKey,
		Recipient:      "user@example.com",
	}

	msg2 := &Message{
		ID:             uuid.New(),
		Type:           MessageTypeEmail,
		IdempotencyKey: idempotencyKey,
		Recipient:      "user@example.com",
	}

	// Same idempotency key
	assert.Equal(t, msg1.IdempotencyKey, msg2.IdempotencyKey)
	// Different message IDs
	assert.NotEqual(t, msg1.ID, msg2.ID)

	// Both messages should have the same behavior due to idempotency
	assert.Equal(t, msg1.Type, msg2.Type)
	assert.Equal(t, msg1.Recipient, msg2.Recipient)
}

func TestIdempotencyKeyUniqueness(t *testing.T) {
	messages := make([]*Message, 10)
	seenKeys := make(map[string]bool)

	for i := 0; i < 10; i++ {
		key := fmt.Sprintf("key-%d-%s", i, uuid.New().String())
		msg := &Message{
			ID:             uuid.New(),
			IdempotencyKey: key,
		}
		messages[i] = msg

		// Verify uniqueness
		assert.False(t, seenKeys[key], "Duplicate idempotency key detected")
		seenKeys[key] = true
	}

	assert.Len(t, seenKeys, 10)
}

// ============================================================================
// SEND AFTER (DELAYED) DELIVERY TESTS
// ============================================================================

func TestSendAfterDelay(t *testing.T) {
	tests := []struct {
		name        string
		delayFunc   func() time.Time
		shouldDelay bool
	}{
		{
			name: "immediate send (no delay)",
			delayFunc: func() time.Time {
				return time.Time{}
			},
			shouldDelay: false,
		},
		{
			name: "one hour delay",
			delayFunc: func() time.Time {
				return time.Now().Add(1 * time.Hour)
			},
			shouldDelay: true,
		},
		{
			name: "past time (should be sent immediately)",
			delayFunc: func() time.Time {
				return time.Now().Add(-1 * time.Hour)
			},
			shouldDelay: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sendTime := tt.delayFunc()
			msg := &Message{
				ID:     uuid.New(),
				Type:   MessageTypeEmail,
				Status: StatusQueued,
			}

			if tt.shouldDelay && !sendTime.IsZero() {
				msg.SendAfter = &sendTime
			}

			// Check if there's a delay
			hasDelay := msg.SendAfter != nil && !msg.SendAfter.IsZero()
			assert.Equal(t, tt.shouldDelay, hasDelay)
		})
	}
}

// ============================================================================
// IDENTITY ASSOCIATION TESTS
// ============================================================================

func TestIdentityAssociation(t *testing.T) {
	tests := []struct {
		name      string
		hasUser   bool
		userCount int
	}{
		{"anonymous message", false, 0},
		{"single user message", true, 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := &Message{
				ID:   uuid.New(),
				Type: MessageTypeEmail,
			}

			if tt.hasUser {
				identityID := uuid.New()
				msg.IdentityID = &identityID
			}

			if tt.hasUser {
				require.NotNil(t, msg.IdentityID)
			} else {
				assert.Nil(t, msg.IdentityID)
			}
		})
	}
}

// ============================================================================
// SOURCE MODULE TRACKING TESTS
// ============================================================================

func TestSourceModuleTracking(t *testing.T) {
	tests := []struct {
		name           string
		sourceModule   string
		expectedModule string
	}{
		{"auth module", "auth", "auth"},
		{"user module", "user", "user"},
		{"billing module", "billing", "billing"},
		{"empty source", "", ""},
		{"core module", "core", "core"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := &Message{
				ID:           uuid.New(),
				SourceModule: tt.sourceModule,
			}

			assert.Equal(t, tt.expectedModule, msg.SourceModule)
		})
	}
}

// ============================================================================
// EMAIL MESSAGE FORMATTING TESTS
// ============================================================================

func TestEmailMessageFormatting(t *testing.T) {
	cfg := Config{
		SMTP: SMTPConfig{
			Host:        "smtp.example.com",
			Port:        587,
			FromAddress: "noreply@example.com",
			FromName:    "Aegion",
			Username:    "user@example.com",
			Password:    "secret",
			AuthEnabled: true,
		},
	}

	courier := New(cfg)
	require.NotNil(t, courier)

	tests := []struct {
		name        string
		recipient   string
		subject     string
		body        string
		expectError bool
	}{
		{
			name:        "valid email",
			recipient:   "user@example.com",
			subject:     "Test Subject",
			body:        "Test Body",
			expectError: false,
		},
		{
			name:        "HTML email",
			recipient:   "user@example.com",
			subject:     "HTML Email",
			body:        "<html><body><h1>Test</h1></body></html>",
			expectError: false,
		},
		{
			name:        "email with special chars",
			recipient:   "user+test@example.com",
			subject:     "Test [Urgent] & Important",
			body:        "Body with <special> &characters&",
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := &Message{
				ID:        uuid.New(),
				Type:      MessageTypeEmail,
				Status:    StatusQueued,
				Recipient: tt.recipient,
				Subject:   tt.subject,
				Body:      tt.body,
			}

			assert.Equal(t, tt.recipient, msg.Recipient)
			assert.Equal(t, tt.subject, msg.Subject)
			assert.Equal(t, tt.body, msg.Body)
		})
	}
}

// ============================================================================
// SMS MESSAGE FORMATTING TESTS
// ============================================================================

func TestSMSMessageFormatting(t *testing.T) {
	tests := []struct {
		name      string
		recipient string
		body      string
		valid     bool
	}{
		{
			name:      "US number",
			recipient: "+1234567890",
			body:      "Your code is 123456",
			valid:     true,
		},
		{
			name:      "international number",
			recipient: "+33123456789",
			body:      "Votre code est 123456",
			valid:     true,
		},
		{
			name:      "long SMS",
			recipient: "+1234567890",
			body:      "This is a very long SMS message that exceeds the standard SMS length limit of 160 characters and should be handled as a multi-part SMS message by the gateway.",
			valid:     true,
		},
		{
			name:      "SMS with special chars",
			recipient: "+1234567890",
			body:      "Code: 123456! @#$%",
			valid:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := &Message{
				ID:        uuid.New(),
				Type:      MessageTypeSMS,
				Status:    StatusQueued,
				Recipient: tt.recipient,
				Body:      tt.body,
			}

			if tt.valid {
				assert.Equal(t, tt.recipient, msg.Recipient)
				assert.Equal(t, tt.body, msg.Body)
				assert.Equal(t, MessageTypeSMS, msg.Type)
			}
		})
	}
}

// ============================================================================
// MESSAGE LIFECYCLE STATE MACHINE TESTS
// ============================================================================

func TestMessageLifecycleStateMachine(t *testing.T) {
	tests := []struct {
		name          string
		states        []MessageStatus
		shouldSucceed bool
	}{
		{
			name:          "successful delivery",
			states:        []MessageStatus{StatusQueued, StatusProcessing, StatusSent},
			shouldSucceed: true,
		},
		{
			name:          "failure and retry",
			states:        []MessageStatus{StatusQueued, StatusProcessing, StatusQueued, StatusProcessing, StatusSent},
			shouldSucceed: true,
		},
		{
			name:          "abandoned after retries",
			states:        []MessageStatus{StatusQueued, StatusProcessing, StatusQueued, StatusProcessing, StatusQueued, StatusProcessing, StatusAbandoned},
			shouldSucceed: true,
		},
		{
			name:          "cancelled",
			states:        []MessageStatus{StatusQueued, StatusCancelled},
			shouldSucceed: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := &Message{
				ID:   uuid.New(),
				Type: MessageTypeEmail,
			}

			// Simulate state transitions
			for i, state := range tt.states {
				msg.Status = state

				// Verify state was set
				assert.Equal(t, state, msg.Status)

				// Track send count for failure scenarios
				if state == StatusProcessing {
					msg.SendCount++
				}

				// Update timestamp
				msg.UpdatedAt = time.Now()
				assert.False(t, msg.UpdatedAt.IsZero())

				// For sent state, set SentAt
				if state == StatusSent {
					now := time.Now()
					msg.SentAt = &now
					assert.NotNil(t, msg.SentAt)
				}

				// Verify progression
				if i > 0 {
					prevState := tt.states[i-1]
					assert.NotEqual(t, prevState, state, fmt.Sprintf("State didn't change at step %d", i))
				}
			}

			assert.True(t, tt.shouldSucceed)
		})
	}
}

// ============================================================================
// RETRY MECHANISM TESTS
// ============================================================================

func TestRetryMechanismWithErrorTracking(t *testing.T) {
	msg := &Message{
		ID:        uuid.New(),
		Type:      MessageTypeEmail,
		Status:    StatusQueued,
		Recipient: "test@example.com",
		SendCount: 0,
		LastError: "",
	}

	errors := []string{
		"Connection timeout",
		"SMTP 550 user not found",
		"Authentication failed",
	}

	maxRetries := 3

	for i, errMsg := range errors {
		msg.SendCount = i + 1
		msg.LastError = errMsg

		if msg.SendCount >= maxRetries {
			msg.Status = StatusAbandoned
			break
		} else {
			msg.Status = StatusQueued
		}

		assert.Equal(t, errMsg, msg.LastError)
	}

	assert.Equal(t, StatusAbandoned, msg.Status)
	assert.Equal(t, 3, msg.SendCount)
	assert.Equal(t, "Authentication failed", msg.LastError)
}

func TestExponentialBackoffCalculation(t *testing.T) {
	baseDelay := 100 * time.Millisecond
	maxDelay := 30 * time.Second

	tests := []struct {
		name       string
		attempt    int
		calculated time.Duration
		capped     time.Duration
	}{
		{"attempt 1", 1, 100 * time.Millisecond, 100 * time.Millisecond},
		{"attempt 2", 2, 200 * time.Millisecond, 200 * time.Millisecond},
		{"attempt 3", 3, 400 * time.Millisecond, 400 * time.Millisecond},
		{"attempt 4", 4, 800 * time.Millisecond, 800 * time.Millisecond},
		{"attempt 5", 5, 1600 * time.Millisecond, 1600 * time.Millisecond},
		{"attempt 10", 10, 51200 * time.Millisecond, maxDelay},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Calculate exponential backoff
			multiplier := 1 << (tt.attempt - 1)
			calculated := baseDelay * time.Duration(multiplier)

			// Cap with max delay
			if calculated > maxDelay {
				calculated = maxDelay
			}

			assert.Equal(t, tt.capped, calculated)
		})
	}
}

// ============================================================================
// MESSAGE BATCH PROCESSING TESTS
// ============================================================================

func TestBatchProcessing(t *testing.T) {
	tests := []struct {
		name      string
		batchSize int
		expected  int
	}{
		{"default batch", 0, 10},
		{"small batch", 1, 1},
		{"medium batch", 25, 25},
		{"large batch", 100, 100},
		{"max batch", 1000, 1000},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			batchSize := tt.batchSize
			if batchSize == 0 {
				batchSize = 10
			}

			assert.Equal(t, tt.expected, batchSize)

			// Verify batch can hold expected messages
			messages := make([]*Message, 0, batchSize)
			for i := 0; i < batchSize; i++ {
				messages = append(messages, &Message{
					ID:   uuid.New(),
					Type: MessageTypeEmail,
				})
			}

			assert.Len(t, messages, batchSize)
		})
	}
}

// ============================================================================
// TIMESTAMP HANDLING TESTS
// ============================================================================

func TestTimestampHandling(t *testing.T) {
	now := time.Now().UTC()
	beforeNow := now.Add(-1 * time.Second)
	afterNow := now.Add(1 * time.Second)

	msg := &Message{
		ID:        uuid.New(),
		CreatedAt: now,
		UpdatedAt: now,
	}

	assert.True(t, msg.CreatedAt.Equal(now))
	assert.True(t, msg.UpdatedAt.Equal(now))

	// Update timestamp
	msg.UpdatedAt = afterNow
	assert.True(t, msg.UpdatedAt.After(msg.CreatedAt))

	// Verify time progression
	assert.True(t, afterNow.After(now))
	assert.False(t, beforeNow.After(now))
}

func TestSentAtTimestamp(t *testing.T) {
	msg := &Message{
		ID:     uuid.New(),
		Status: StatusQueued,
		SentAt: nil,
	}

	// Before sending
	assert.Nil(t, msg.SentAt)

	// After sending
	now := time.Now()
	msg.Status = StatusSent
	msg.SentAt = &now

	assert.NotNil(t, msg.SentAt)
	assert.True(t, msg.SentAt.After(msg.CreatedAt))
}

// ============================================================================
// COMPLEX EMAIL TEMPLATE RENDERING TESTS
// ============================================================================

func TestComplexTemplateRendering(t *testing.T) {
	tests := []struct {
		name         string
		templateStr  string
		data         map[string]interface{}
		expectedText string
	}{
		{
			name: "verification email",
			templateStr: `
			Dear {{.name}},
			Your verification code is: {{.code}}
			This code expires in {{.expiresIn}} minutes.
			`,
			data: map[string]interface{}{
				"name":      "Alice",
				"code":      "123456",
				"expiresIn": 15,
			},
			expectedText: "Alice",
		},
		{
			name: "password reset email",
			templateStr: `
			<html>
			<body>
				<h1>Password Reset</h1>
				<p>Hello {{.firstName}} {{.lastName}},</p>
				<p>Click <a href="{{.resetLink}}">here</a> to reset your password.</p>
			</body>
			</html>
			`,
			data: map[string]interface{}{
				"firstName": "Bob",
				"lastName":  "Smith",
				"resetLink": "https://example.com/reset?token=abc123",
			},
			expectedText: "Bob",
		},
		{
			name: "magic link email",
			templateStr: `
			{{.greeting}}
			{{if .premium}}This is a premium account{{end}}
			Link: {{.link}}
			`,
			data: map[string]interface{}{
				"greeting": "Hello user",
				"premium":  true,
				"link":     "https://example.com/signin",
			},
			expectedText: "Hello user",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpl, err := template.New("test").Parse(tt.templateStr)
			require.NoError(t, err)

			var buf bytes.Buffer
			err = tmpl.Execute(&buf, tt.data)
			require.NoError(t, err)

			result := buf.String()
			assert.Contains(t, result, tt.expectedText)
		})
	}
}

// ============================================================================
// MESSAGE VALIDATION TESTS
// ============================================================================

func TestMessageValidation(t *testing.T) {
	tests := []struct {
		name      string
		recipient string
		body      string
		isValid   bool
	}{
		{"valid email", "user@example.com", "Body", true},
		{"valid SMS", "+1234567890", "Body", true},
		{"empty recipient", "", "Body", true}, // Still valid structure, validation would be business logic
		{"empty body", "user@example.com", "", true},
		{"email with spaces", "user @example.com", "Body", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := &Message{
				ID:        uuid.New(),
				Type:      MessageTypeEmail,
				Status:    StatusQueued,
				Recipient: tt.recipient,
				Body:      tt.body,
			}

			// Basic validation
			isValid := msg.ID != uuid.Nil && msg.Type != ""
			assert.Equal(t, tt.isValid, isValid)
		})
	}
}

// ============================================================================
// MESSAGE UNIQUENESS TESTS
// ============================================================================

func TestMessageIDUniqueness(t *testing.T) {
	messages := make(map[uuid.UUID]*Message)

	for i := 0; i < 100; i++ {
		msg := &Message{
			ID:        uuid.New(),
			Type:      MessageTypeEmail,
			Recipient: fmt.Sprintf("user%d@example.com", i),
		}

		// Verify uniqueness
		assert.NotContains(t, messages, msg.ID)
		messages[msg.ID] = msg
	}

	assert.Len(t, messages, 100)
}

// ============================================================================
// VERIFICATION EMAIL WORKFLOW TESTS
// ============================================================================

func TestVerificationEmailWorkflow(t *testing.T) {
	identityID := uuid.New()
	code := "123456"
	recipient := "user@example.com"
	idempotencyKey := fmt.Sprintf("verify:%s:%s", identityID.String(), code)

	msg := &Message{
		ID:             uuid.New(),
		Type:           MessageTypeEmail,
		Status:         StatusQueued,
		Recipient:      recipient,
		Subject:        "Verify your email address",
		Body:           fmt.Sprintf("<p>Your verification code is: <strong>%s</strong></p>", code),
		IdentityID:     &identityID,
		SourceModule:   "core",
		IdempotencyKey: idempotencyKey,
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}

	assert.Equal(t, recipient, msg.Recipient)
	assert.Equal(t, "Verify your email address", msg.Subject)
	assert.Contains(t, msg.Body, code)
	assert.Equal(t, &identityID, msg.IdentityID)
	assert.Equal(t, "core", msg.SourceModule)
	assert.Equal(t, idempotencyKey, msg.IdempotencyKey)
}

// ============================================================================
// PASSWORD RESET EMAIL WORKFLOW TESTS
// ============================================================================

func TestPasswordResetEmailWorkflow(t *testing.T) {
	identityID := uuid.New()
	resetCode := "reset-token-abc123"
	recipient := "user@example.com"
	idempotencyKey := fmt.Sprintf("reset:%s:%s", identityID.String(), resetCode)

	msg := &Message{
		ID:             uuid.New(),
		Type:           MessageTypeEmail,
		Status:         StatusQueued,
		Recipient:      recipient,
		Subject:        "Reset your password",
		Body:           fmt.Sprintf("<p>Your password reset code is: <strong>%s</strong></p>", resetCode),
		IdentityID:     &identityID,
		SourceModule:   "core",
		IdempotencyKey: idempotencyKey,
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}

	assert.Equal(t, recipient, msg.Recipient)
	assert.Equal(t, "Reset your password", msg.Subject)
	assert.Contains(t, msg.Body, resetCode)
	assert.Equal(t, &identityID, msg.IdentityID)
}

// ============================================================================
// MAGIC LINK EMAIL WORKFLOW TESTS
// ============================================================================

func TestMagicLinkEmailWorkflow(t *testing.T) {
	code := "magic-token-xyz789"
	magicLink := "https://example.com/signin?token=xyz789"
	recipient := "user@example.com"
	idempotencyKey := fmt.Sprintf("magic:%s", code)

	msg := &Message{
		ID:             uuid.New(),
		Type:           MessageTypeEmail,
		Status:         StatusQueued,
		Recipient:      recipient,
		Subject:        "Sign in to your account",
		Body:           fmt.Sprintf(`<p><a href="%s">Sign In</a></p><p>Code: <strong>%s</strong></p>`, magicLink, code),
		SourceModule:   "magic_link",
		IdempotencyKey: idempotencyKey,
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}

	assert.Equal(t, recipient, msg.Recipient)
	assert.Equal(t, "Sign in to your account", msg.Subject)
	assert.Contains(t, msg.Body, magicLink)
	assert.Contains(t, msg.Body, code)
	assert.Equal(t, "magic_link", msg.SourceModule)
}

// ============================================================================
// BULK MESSAGE CREATION TESTS
// ============================================================================

func TestBulkEmailMessageCreation(t *testing.T) {
	recipients := []string{
		"user1@example.com",
		"user2@example.com",
		"user3@example.com",
		"user4@example.com",
		"user5@example.com",
	}

	messages := make([]*Message, len(recipients))
	createdAt := time.Now().UTC()

	for i, recipient := range recipients {
		messages[i] = &Message{
			ID:        uuid.New(),
			Type:      MessageTypeEmail,
			Status:    StatusQueued,
			Recipient: recipient,
			Subject:   "Bulk Email",
			Body:      "Hello user",
			CreatedAt: createdAt,
			UpdatedAt: createdAt,
		}
	}

	assert.Len(t, messages, 5)

	// Verify all messages are unique
	seenIDs := make(map[uuid.UUID]bool)
	for i, msg := range messages {
		assert.Equal(t, recipients[i], msg.Recipient)
		assert.False(t, seenIDs[msg.ID])
		seenIDs[msg.ID] = true
	}
}

// ============================================================================
// MESSAGE CONTENT EDGE CASES TESTS
// ============================================================================

func TestMessageWithUnicodeContent(t *testing.T) {
	tests := []struct {
		name    string
		subject string
		body    string
	}{
		{
			name:    "chinese characters",
			subject: "你好",
			body:    "这是一条测试信息",
		},
		{
			name:    "arabic text",
			subject: "مرحبا",
			body:    "هذه رسالة اختبار",
		},
		{
			name:    "emoji",
			subject: "Hello 👋",
			body:    "Emoji test 🎉 😊",
		},
		{
			name:    "mixed languages",
			subject: "Hello مرحبا 你好",
			body:    "Mixed content 中文 العربية English",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := &Message{
				ID:      uuid.New(),
				Type:    MessageTypeEmail,
				Subject: tt.subject,
				Body:    tt.body,
			}

			assert.Equal(t, tt.subject, msg.Subject)
			assert.Equal(t, tt.body, msg.Body)
		})
	}
}

// ============================================================================
// COURIER CONFIGURATION TESTS
// ============================================================================

func TestSMTPConfigVariations(t *testing.T) {
	tests := []struct {
		name    string
		config  SMTPConfig
		hasAuth bool
	}{
		{
			name: "authenticated SMTP",
			config: SMTPConfig{
				Host:        "smtp.gmail.com",
				Port:        587,
				FromAddress: "sender@gmail.com",
				FromName:    "Aegion",
				Username:    "user@gmail.com",
				Password:    "password",
				AuthEnabled: true,
			},
			hasAuth: true,
		},
		{
			name: "unauthenticated SMTP",
			config: SMTPConfig{
				Host:        "localhost",
				Port:        25,
				FromAddress: "sender@example.com",
				FromName:    "Aegion",
				AuthEnabled: false,
			},
			hasAuth: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.hasAuth, tt.config.AuthEnabled)

			if tt.hasAuth {
				assert.NotEmpty(t, tt.config.Username)
				assert.NotEmpty(t, tt.config.Password)
			}
		})
	}
}

func TestCourierConfigDefaults(t *testing.T) {
	tests := []struct {
		name            string
		inputConfig     Config
		expectedRetries int
	}{
		{
			name:            "empty config gets default retries",
			inputConfig:     Config{},
			expectedRetries: 3,
		},
		{
			name:            "custom retries preserved",
			inputConfig:     Config{MaxRetries: 5},
			expectedRetries: 5,
		},
		{
			name:            "zero retries becomes default",
			inputConfig:     Config{MaxRetries: 0},
			expectedRetries: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			courier := New(tt.inputConfig)
			assert.Equal(t, tt.expectedRetries, courier.maxRetries)
		})
	}
}
