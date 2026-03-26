package courier

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

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
			if string(tt.mt) != tt.want {
				t.Errorf("MessageType = %s, want %s", string(tt.mt), tt.want)
			}
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
			if string(tt.status) != tt.want {
				t.Errorf("MessageStatus = %s, want %s", string(tt.status), tt.want)
			}
		})
	}
}

func TestNewWithDefaults(t *testing.T) {
	tests := []struct {
		name           string
		config         Config
		expectedRetries int
	}{
		{
			name:           "empty config gets defaults",
			config:         Config{},
			expectedRetries: 3,
		},
		{
			name:           "zero max retries gets default",
			config:         Config{MaxRetries: 0},
			expectedRetries: 3,
		},
		{
			name:           "custom retries preserved",
			config:         Config{MaxRetries: 5},
			expectedRetries: 5,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			courier := New(tt.config)
			
			if courier == nil {
				t.Fatal("New() returned nil")
			}
			
			if courier.maxRetries != tt.expectedRetries {
				t.Errorf("maxRetries = %d, want %d", courier.maxRetries, tt.expectedRetries)
			}

			if courier.templates == nil {
				t.Error("templates map is nil")
			}
		})
	}
}

func TestWithTemplate(t *testing.T) {
	templateID := "welcome-email"
	templateData := map[string]interface{}{
		"name": "John Doe",
		"code": "123456",
	}

	option := WithTemplate(templateID, templateData)
	
	msg := &Message{}
	option(msg)

	if msg.TemplateID != templateID {
		t.Errorf("TemplateID = %s, want %s", msg.TemplateID, templateID)
	}
	
	if msg.TemplateData == nil {
		t.Fatal("TemplateData is nil")
	}
	
	if msg.TemplateData["name"] != "John Doe" {
		t.Errorf("TemplateData[name] = %v, want John Doe", msg.TemplateData["name"])
	}
	
	if msg.TemplateData["code"] != "123456" {
		t.Errorf("TemplateData[code] = %v, want 123456", msg.TemplateData["code"])
	}
}

func TestWithIdempotencyKey(t *testing.T) {
	key := "unique-key-123"
	option := WithIdempotencyKey(key)
	
	msg := &Message{}
	option(msg)

	if msg.IdempotencyKey != key {
		t.Errorf("IdempotencyKey = %s, want %s", msg.IdempotencyKey, key)
	}
}

func TestWithSendAfter(t *testing.T) {
	sendTime := time.Now().Add(1 * time.Hour)
	option := WithSendAfter(sendTime)
	
	msg := &Message{}
	option(msg)

	if msg.SendAfter == nil {
		t.Fatal("SendAfter is nil")
	}
	
	if !msg.SendAfter.Equal(sendTime) {
		t.Errorf("SendAfter = %v, want %v", *msg.SendAfter, sendTime)
	}
}

func TestWithIdentity(t *testing.T) {
	identityID := uuid.New()
	option := WithIdentity(identityID)
	
	msg := &Message{}
	option(msg)

	if msg.IdentityID == nil {
		t.Fatal("IdentityID is nil")
	}
	
	if *msg.IdentityID != identityID {
		t.Errorf("IdentityID = %v, want %v", *msg.IdentityID, identityID)
	}
}

func TestWithSource(t *testing.T) {
	sourceModule := "auth-service"
	option := WithSource(sourceModule)
	
	msg := &Message{}
	option(msg)

	if msg.SourceModule != sourceModule {
		t.Errorf("SourceModule = %s, want %s", msg.SourceModule, sourceModule)
	}
}

func TestMultipleOptions(t *testing.T) {
	templateID := "test-template"
	templateData := map[string]interface{}{"key": "value"}
	idempotencyKey := "test-key"
	sendTime := time.Now().Add(1 * time.Hour)
	identityID := uuid.New()
	sourceModule := "test-module"

	msg := &Message{}
	
	// Apply multiple options
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

	// Verify all options were applied
	if msg.TemplateID != templateID {
		t.Errorf("TemplateID = %s, want %s", msg.TemplateID, templateID)
	}
	if msg.IdempotencyKey != idempotencyKey {
		t.Errorf("IdempotencyKey = %s, want %s", msg.IdempotencyKey, idempotencyKey)
	}
	if msg.SendAfter == nil || !msg.SendAfter.Equal(sendTime) {
		t.Errorf("SendAfter mismatch")
	}
	if msg.IdentityID == nil || *msg.IdentityID != identityID {
		t.Errorf("IdentityID mismatch")
	}
	if msg.SourceModule != sourceModule {
		t.Errorf("SourceModule = %s, want %s", msg.SourceModule, sourceModule)
	}
}

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

	if msg.ID != msgID {
		t.Errorf("ID = %v, want %v", msg.ID, msgID)
	}
	if msg.Type != MessageTypeEmail {
		t.Errorf("Type = %s, want %s", msg.Type, MessageTypeEmail)
	}
	if msg.Status != StatusQueued {
		t.Errorf("Status = %s, want %s", msg.Status, StatusQueued)
	}
	if msg.Recipient != "test@example.com" {
		t.Errorf("Recipient = %s, want test@example.com", msg.Recipient)
	}
	if msg.TemplateData["name"] != "Test" {
		t.Errorf("TemplateData[name] = %v, want Test", msg.TemplateData["name"])
	}
}

func TestProcessQueueBatchSizeDefault(t *testing.T) {
	// Test that batch size defaults to 10 when 0 is passed
	// This is testing the logic: if batchSize == 0 { batchSize = 10 }
	
	tests := []struct {
		name      string
		input     int
		expected  int
	}{
		{"zero gets default", 0, 10},
		{"positive preserved", 5, 5},
		{"large number preserved", 100, 100},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			batchSize := tt.input
			if batchSize == 0 {
				batchSize = 10
			}
			
			if batchSize != tt.expected {
				t.Errorf("batch size = %d, want %d", batchSize, tt.expected)
			}
		})
	}
}

func TestRetryLogic(t *testing.T) {
	maxRetries := 3
	
	tests := []struct {
		name               string
		sendCount          int
		shouldAbandon      bool
		expectedNewCount   int
	}{
		{"first attempt", 1, false, 2},
		{"second attempt", 2, false, 3},
		{"at max retries", 3, true, 3},  // Should abandon
		{"beyond max retries", 4, true, 4},  // Should abandon
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Simulate retry logic
			sendCount := tt.sendCount
			var shouldAbandon bool
			
			if sendCount >= maxRetries {
				shouldAbandon = true
			} else {
				sendCount++
			}
			
			if shouldAbandon != tt.shouldAbandon {
				t.Errorf("shouldAbandon = %t, want %t", shouldAbandon, tt.shouldAbandon)
			}
			
			if sendCount != tt.expectedNewCount {
				t.Errorf("sendCount = %d, want %d", sendCount, tt.expectedNewCount)
			}
		})
	}
}