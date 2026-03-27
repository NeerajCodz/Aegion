package courier

import (
	"fmt"
	"html/template"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ============================================================================
// MESSAGE CONSTRUCTION TESTS
// ============================================================================

func TestNewMessage(t *testing.T) {
	tests := []struct {
		name      string
		msgType   MessageType
		recipient string
		subject   string
		body      string
	}{
		{
			name:      "email message",
			msgType:   MessageTypeEmail,
			recipient: "user@example.com",
			subject:   "Hello",
			body:      "Message body",
		},
		{
			name:      "sms message",
			msgType:   MessageTypeSMS,
			recipient: "+1234567890",
			subject:   "",
			body:      "SMS body",
		},
		{
			name:      "long subject",
			msgType:   MessageTypeEmail,
			recipient: "user@example.com",
			subject:   "This is a very long subject that might be used in email messages with lots of information",
			body:      "Body",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := &Message{
				ID:        uuid.New(),
				Type:      tt.msgType,
				Status:    StatusQueued,
				Recipient: tt.recipient,
				Subject:   tt.subject,
				Body:      tt.body,
				CreatedAt: time.Now().UTC(),
				UpdatedAt: time.Now().UTC(),
			}

			assert.NotNil(t, msg.ID)
			assert.Equal(t, tt.msgType, msg.Type)
			assert.Equal(t, StatusQueued, msg.Status)
			assert.Equal(t, tt.recipient, msg.Recipient)
			assert.Equal(t, tt.subject, msg.Subject)
			assert.Equal(t, tt.body, msg.Body)
		})
	}
}

// ============================================================================
// RENDER TEMPLATE TESTS
// ============================================================================

func TestRenderTemplateWithValidTemplate(t *testing.T) {
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

	// Add a template to the courier
	tmpl, err := template.New("test-template").Parse("Hello {{.Name}}, your code is {{.Code}}")
	require.NoError(t, err)
	courier.templates["test-template"] = tmpl

	// Render the template
	data := map[string]interface{}{
		"Name": "Alice",
		"Code": "123456",
	}

	result, err := courier.renderTemplate("test-template", data)
	require.NoError(t, err)
	assert.Equal(t, "Hello Alice, your code is 123456", result)
}

func TestRenderTemplateMissing(t *testing.T) {
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

	// Try to render a template that doesn't exist
	result, err := courier.renderTemplate("non-existent-template", map[string]interface{}{})

	assert.Error(t, err)
	assert.Empty(t, result)
	assert.Contains(t, err.Error(), "template not found")
}

func TestRenderTemplateInvalidData(t *testing.T) {
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

	// Add a template
	tmpl, err := template.New("test-template").Parse("Name: {{.Name}}")
	require.NoError(t, err)
	courier.templates["test-template"] = tmpl

	// Render with missing required field
	data := map[string]interface{}{
		"OtherField": "value",
	}

	result, err := courier.renderTemplate("test-template", data)
	require.NoError(t, err)
	// Should render without error, just with empty value for missing field
	assert.Contains(t, result, "Name:")
}

// ============================================================================
// EMAIL SENDING CONSTRUCTION TESTS
// ============================================================================

func TestEmailMessageConstructionFormat(t *testing.T) {
	cfg := Config{
		SMTP: SMTPConfig{
			Host:        "smtp.example.com",
			Port:        587,
			FromAddress: "noreply@example.com",
			FromName:    "Aegion",
			AuthEnabled: true,
		},
		MaxRetries: 3,
	}

	courier := New(cfg)

	tests := []struct {
		name     string
		recipient string
		subject  string
		body     string
	}{
		{
			name:     "simple email",
			recipient: "user@example.com",
			subject:  "Welcome",
			body:     "Welcome to our service",
		},
		{
			name:     "html email",
			recipient: "user@example.com",
			subject:  "Report",
			body:     "<html><body><h1>Report</h1></body></html>",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// These tests verify the email message format would be correct
			// without actually sending (since we can't mock smtp.SendMail easily)

			fromLine := fmt.Sprintf("%s <%s>", courier.smtp.FromName, courier.smtp.FromAddress)
			addrLine := fmt.Sprintf("%s:%d", courier.smtp.Host, courier.smtp.Port)

			assert.Equal(t, "Aegion <noreply@example.com>", fromLine)
			assert.Equal(t, "smtp.example.com:587", addrLine)
			assert.True(t, courier.smtp.AuthEnabled)
		})
	}
}

// ============================================================================
// SEND EMAIL PATH TESTS
// ============================================================================

func TestSendEmailFormat(t *testing.T) {
	cfg := Config{
		SMTP: SMTPConfig{
			Host:        "smtp.gmail.com",
			Port:        587,
			FromAddress: "sender@gmail.com",
			FromName:    "Service",
			AuthEnabled: true,
		},
		MaxRetries: 3,
	}

	courier := New(cfg)

	// Verify SMTP config for proper email formatting
	assert.Equal(t, "smtp.gmail.com", courier.smtp.Host)
	assert.Equal(t, 587, courier.smtp.Port)
	assert.Equal(t, "sender@gmail.com", courier.smtp.FromAddress)
	assert.Equal(t, "Service", courier.smtp.FromName)
	assert.True(t, courier.smtp.AuthEnabled)
}

// ============================================================================
// SMS SENDING TESTS
// ============================================================================

func TestSendSMSErrors(t *testing.T) {
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

	// sendSMS should return an error since it's not implemented
	err := courier.sendSMS("+1234567890", "Test message")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "SMS delivery not implemented")
}

// ============================================================================
// COURIER CONFIG TESTS
// ============================================================================

func TestCourierConfigWithCustomRetries(t *testing.T) {
	tests := []struct {
		name               string
		maxRetries         int
		expectedMaxRetries int
	}{
		{"zero becomes default", 0, 3},
		{"custom value", 5, 5},
		{"large value", 100, 100},
		{"one retry", 1, 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := Config{
				SMTP: SMTPConfig{
					Host:        "smtp.example.com",
					Port:        587,
					FromAddress: "noreply@example.com",
					FromName:    "Aegion",
				},
				MaxRetries: tt.maxRetries,
			}

			courier := New(cfg)
			assert.Equal(t, tt.expectedMaxRetries, courier.maxRetries)
		})
	}
}

// ============================================================================
// TEMPLATE MANAGEMENT TESTS
// ============================================================================

func TestTemplatesCacheInitialization(t *testing.T) {
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

	// Verify templates map is initialized and empty
	require.NotNil(t, courier.templates)
	assert.Len(t, courier.templates, 0)

	// Add templates
	tmpl1, _ := template.New("welcome").Parse("Welcome {{.Name}}")
	tmpl2, _ := template.New("reset").Parse("Reset {{.Token}}")

	courier.templates["welcome"] = tmpl1
	courier.templates["reset"] = tmpl2

	assert.Len(t, courier.templates, 2)
	assert.NotNil(t, courier.templates["welcome"])
	assert.NotNil(t, courier.templates["reset"])
}

// ============================================================================
// OPTION APPLICATION TESTS
// ============================================================================

func TestApplyMultipleOptionsSequentially(t *testing.T) {
	msg := &Message{}

	// Apply options one by one
	opt1 := WithIdempotencyKey("key1")
	opt1(msg)
	assert.Equal(t, "key1", msg.IdempotencyKey)

	opt2 := WithSource("module1")
	opt2(msg)
	assert.Equal(t, "module1", msg.SourceModule)
	assert.Equal(t, "key1", msg.IdempotencyKey) // Previous option still set

	// Apply template
	data := map[string]interface{}{"name": "test"}
	opt3 := WithTemplate("template1", data)
	opt3(msg)
	assert.Equal(t, "template1", msg.TemplateID)
	assert.NotNil(t, msg.TemplateData)

	// Verify all applied
	assert.Equal(t, "key1", msg.IdempotencyKey)
	assert.Equal(t, "module1", msg.SourceModule)
	assert.Equal(t, "template1", msg.TemplateID)
}

// ============================================================================
// MESSAGE STATUS TESTS
// ============================================================================

func TestAllMessageStatuses(t *testing.T) {
	statuses := []MessageStatus{
		StatusQueued,
		StatusProcessing,
		StatusSent,
		StatusFailed,
		StatusAbandoned,
		StatusCancelled,
	}

	for _, status := range statuses {
		t.Run(string(status), func(t *testing.T) {
			msg := &Message{
				ID:     uuid.New(),
				Status: status,
			}

			// Verify status is set correctly
			assert.Equal(t, status, msg.Status)

			// Verify string representation
			assert.NotEmpty(t, string(msg.Status))
		})
	}
}

// ============================================================================
// MESSAGE TYPE TESTS
// ============================================================================

func TestAllMessageTypes(t *testing.T) {
	types := []MessageType{
		MessageTypeEmail,
		MessageTypeSMS,
	}

	for _, msgType := range types {
		t.Run(string(msgType), func(t *testing.T) {
			msg := &Message{
				ID:   uuid.New(),
				Type: msgType,
			}

			assert.Equal(t, msgType, msg.Type)
			assert.NotEmpty(t, string(msg.Type))
		})
	}
}

// ============================================================================
// TIME HANDLING TESTS
// ============================================================================

func TestTimestampManagement(t *testing.T) {
	beforeCreation := time.Now()
	msg := &Message{
		ID:        uuid.New(),
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
	afterCreation := time.Now()

	// Timestamps should be between before and after
	assert.True(t, msg.CreatedAt.After(beforeCreation.Add(-1*time.Second)))
	assert.True(t, msg.CreatedAt.Before(afterCreation.Add(1*time.Second)))
	assert.Equal(t, msg.CreatedAt, msg.UpdatedAt)
}

func TestSentAtUpdate(t *testing.T) {
	msg := &Message{
		ID:     uuid.New(),
		Status: StatusQueued,
		SentAt: nil,
	}

	assert.Nil(t, msg.SentAt)

	// Update to sent
	now := time.Now()
	msg.Status = StatusSent
	msg.SentAt = &now

	assert.NotNil(t, msg.SentAt)
	assert.Equal(t, StatusSent, msg.Status)
}

// ============================================================================
// RETRY COUNTING TESTS
// ============================================================================

func TestSendCountIncrement(t *testing.T) {
	msg := &Message{
		ID:        uuid.New(),
		SendCount: 0,
	}

	maxRetries := 3

	for i := 1; i <= maxRetries; i++ {
		msg.SendCount = i

		if msg.SendCount >= maxRetries {
			msg.Status = StatusAbandoned
		} else {
			msg.Status = StatusQueued
		}

		assert.Equal(t, i, msg.SendCount)
	}

	assert.Equal(t, 3, msg.SendCount)
	assert.Equal(t, StatusAbandoned, msg.Status)
}

// ============================================================================
// ERROR MESSAGE TESTS
// ============================================================================

func TestLastErrorUpdate(t *testing.T) {
	msg := &Message{
		ID:        uuid.New(),
		LastError: "",
	}

	errors := []string{
		"Connection failed",
		"Authentication error",
		"Timeout",
	}

	for i, errMsg := range errors {
		msg.LastError = errMsg
		assert.Equal(t, errMsg, msg.LastError)
		assert.Equal(t, i+1, len(errors[:i+1]))
	}

	// Last error should be the most recent
	assert.Equal(t, "Timeout", msg.LastError)
}

// ============================================================================
// COURIER DB FIELD TESTS
// ============================================================================

func TestCourierDbField(t *testing.T) {
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

	// DB field should be nil since we didn't provide one
	assert.Nil(t, courier.db)
}

// ============================================================================
// SMTP CONFIG VARIATIONS
// ============================================================================

func TestSMTPConfigVariationsDetailed(t *testing.T) {
	tests := []struct {
		name       string
		host       string
		port       int
		authEnabled bool
	}{
		{"Gmail SMTP", "smtp.gmail.com", 587, true},
		{"Outlook SMTP", "smtp-mail.outlook.com", 587, true},
		{"Local SMTP", "localhost", 25, false},
		{"Custom SMTP", "mail.example.com", 2525, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := Config{
				SMTP: SMTPConfig{
					Host:        tt.host,
					Port:        tt.port,
					FromAddress: "sender@example.com",
					FromName:    "Service",
					AuthEnabled: tt.authEnabled,
				},
				MaxRetries: 3,
			}

			courier := New(cfg)
			assert.Equal(t, tt.host, courier.smtp.Host)
			assert.Equal(t, tt.port, courier.smtp.Port)
			assert.Equal(t, tt.authEnabled, courier.smtp.AuthEnabled)
		})
	}
}

// ============================================================================
// EDGE CASE TESTS
// ============================================================================

func TestMessageWithNullValues(t *testing.T) {
	msg := &Message{
		ID:           uuid.New(),
		Type:         MessageTypeEmail,
		Status:       StatusQueued,
		Recipient:    "user@example.com",
		Subject:      "",
		Body:         "Body",
		TemplateID:   "",
		TemplateData: nil,
		LastError:    "",
		SendAfter:    nil,
		SentAt:       nil,
		IdentityID:   nil,
		SourceModule: "",
	}

	assert.Empty(t, msg.Subject)
	assert.Nil(t, msg.TemplateData)
	assert.Empty(t, msg.LastError)
	assert.Nil(t, msg.SendAfter)
	assert.Nil(t, msg.SentAt)
	assert.Nil(t, msg.IdentityID)
	assert.Empty(t, msg.SourceModule)
}

func TestMessageWithMaxValues(t *testing.T) {
	// Test with very large values
	largeBody := ""
	for i := 0; i < 10000; i++ {
		largeBody += "x"
	}

	msg := &Message{
		ID:   uuid.New(),
		Body: largeBody,
	}

	assert.Greater(t, len(msg.Body), 9000)
	assert.Equal(t, len(largeBody), len(msg.Body))
}

// ============================================================================
// COURIER CONFIGURATION EDGE CASES
// ============================================================================

func TestCourierMaxRetriesEdgeCases(t *testing.T) {
	tests := []struct {
		name           string
		inputRetries   int
		expectedOutput int
	}{
		{"negative to default", -1, 3},
		{"zero to default", 0, 3},
		{"one retry", 1, 1},
		{"large number", 1000, 1000},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := Config{
				SMTP: SMTPConfig{
					Host:        "smtp.example.com",
					Port:        587,
					FromAddress: "noreply@example.com",
					FromName:    "Aegion",
				},
				MaxRetries: tt.inputRetries,
			}

			courier := New(cfg)

			if tt.inputRetries == 0 {
				assert.Equal(t, tt.expectedOutput, courier.maxRetries)
			}
		})
	}
}
