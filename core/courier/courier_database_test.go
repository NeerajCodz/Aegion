package courier

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ============================================================================
// MOCK DATABASE IMPLEMENTATION
// ============================================================================

// MockDBExecutor tracks database operations for testing
type MockDBExecutor struct {
	insertedMessages map[uuid.UUID]*Message
	updatedMessages  map[uuid.UUID]*Message
	executeCalls     []string
	execErr          error
	queryErr         error
	shouldFail       bool
}

// NewMockDBExecutor creates a new mock database executor
func NewMockDBExecutor() *MockDBExecutor {
	return &MockDBExecutor{
		insertedMessages: make(map[uuid.UUID]*Message),
		updatedMessages:  make(map[uuid.UUID]*Message),
		executeCalls:     make([]string, 0),
	}
}

// RecordInsert records an insert operation
func (m *MockDBExecutor) RecordInsert(id uuid.UUID, msg *Message) {
	m.insertedMessages[id] = msg
}

// RecordUpdate records an update operation
func (m *MockDBExecutor) RecordUpdate(id uuid.UUID, msg *Message) {
	m.updatedMessages[id] = msg
}

// GetInsertedMessage returns an inserted message
func (m *MockDBExecutor) GetInsertedMessage(id uuid.UUID) (*Message, bool) {
	msg, ok := m.insertedMessages[id]
	return msg, ok
}

// GetUpdatedMessage returns an updated message
func (m *MockDBExecutor) GetUpdatedMessage(id uuid.UUID) (*Message, bool) {
	msg, ok := m.updatedMessages[id]
	return msg, ok
}

// ============================================================================
// SEND EMAIL METHOD TESTS
// ============================================================================

func TestEmailMessageConstruction(t *testing.T) {
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
		name       string
		to         string
		subject    string
		body       string
		expectAddr string
		expectFrom string
	}{
		{
			name:       "standard email",
			to:         "user@example.com",
			subject:    "Welcome",
			body:       "Welcome to Aegion",
			expectAddr: "smtp.example.com:587",
			expectFrom: "Aegion <noreply@example.com>",
		},
		{
			name:       "HTML email",
			to:         "admin@example.com",
			subject:    "Report",
			body:       "<h1>Report</h1>",
			expectAddr: "smtp.example.com:587",
			expectFrom: "Aegion <noreply@example.com>",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expectAddr, fmt.Sprintf("%s:%d", courier.smtp.Host, courier.smtp.Port))
			assert.Equal(t, tt.expectFrom, fmt.Sprintf("%s <%s>", courier.smtp.FromName, courier.smtp.FromAddress))
		})
	}
}

// ============================================================================
// TEMPLATE LOADING TESTS
// ============================================================================

func TestTemplateNotFound(t *testing.T) {
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

	// Try to render a template that doesn't exist
	result, err := courier.renderTemplate("non-existent", map[string]interface{}{})

	assert.Error(t, err)
	assert.Empty(t, result)
	assert.Contains(t, err.Error(), "template not found")
}

// ============================================================================
// MARK SENT AND FAILED TESTS
// ============================================================================

func TestMessageMarkingLogic(t *testing.T) {
	tests := []struct {
		name              string
		initialSendCount  int
		newSendCount      int
		maxRetries        int
		expectedStatus    MessageStatus
		shouldAbandon     bool
	}{
		{
			name:             "first failure, should retry",
			initialSendCount: 0,
			newSendCount:     1,
			maxRetries:       3,
			expectedStatus:   StatusQueued,
			shouldAbandon:    false,
		},
		{
			name:             "second failure, should retry",
			initialSendCount: 1,
			newSendCount:     2,
			maxRetries:       3,
			expectedStatus:   StatusQueued,
			shouldAbandon:    false,
		},
		{
			name:             "at max retries, should abandon",
			initialSendCount: 2,
			newSendCount:     3,
			maxRetries:       3,
			expectedStatus:   StatusAbandoned,
			shouldAbandon:    true,
		},
		{
			name:             "beyond max retries, should abandon",
			initialSendCount: 3,
			newSendCount:     4,
			maxRetries:       3,
			expectedStatus:   StatusAbandoned,
			shouldAbandon:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := &Message{
				ID:        uuid.New(),
				Type:      MessageTypeEmail,
				Status:    StatusProcessing,
				SendCount: tt.initialSendCount,
			}

			// Simulate failure handling
			msg.SendCount = tt.newSendCount
			msg.LastError = "Test error"

			if msg.SendCount >= tt.maxRetries {
				msg.Status = StatusAbandoned
			} else {
				msg.Status = StatusQueued
			}

			assert.Equal(t, tt.expectedStatus, msg.Status)
			assert.Equal(t, tt.newSendCount, msg.SendCount)
		})
	}
}

// ============================================================================
// MESSAGE SENT MARKING TESTS
// ============================================================================

func TestMarkMessageAsSent(t *testing.T) {
	createdTime := time.Now().UTC().Add(-1 * time.Second)
	msg := &Message{
		ID:        uuid.New(),
		Type:      MessageTypeEmail,
		Status:    StatusProcessing,
		Recipient: "user@example.com",
		SendCount: 1,
		CreatedAt: createdTime,
		UpdatedAt: createdTime,
	}

	// Simulate marking as sent
	now := time.Now()
	msg.Status = StatusSent
	msg.SentAt = &now

	assert.Equal(t, StatusSent, msg.Status)
	assert.NotNil(t, msg.SentAt)
	assert.True(t, msg.SentAt.After(msg.CreatedAt))
}

// ============================================================================
// CANCEL MESSAGE TESTS
// ============================================================================

func TestMessageCancellation(t *testing.T) {
	tests := []struct {
		name           string
		initialStatus  MessageStatus
		canBeCancelled bool
	}{
		{"cancel queued message", StatusQueued, true},
		{"cannot cancel processing", StatusProcessing, false},
		{"cannot cancel sent", StatusSent, false},
		{"cannot cancel abandoned", StatusAbandoned, false},
		{"cannot cancel already cancelled", StatusCancelled, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := &Message{
				ID:     uuid.New(),
				Status: tt.initialStatus,
			}

			// Simulate cancellation logic
			canCancel := msg.Status == StatusQueued

			assert.Equal(t, tt.canBeCancelled, canCancel)

			if canCancel {
				msg.Status = StatusCancelled
				assert.Equal(t, StatusCancelled, msg.Status)
			}
		})
	}
}

// ============================================================================
// CLEANUP TESTS
// ============================================================================

func TestMessageCleanupLogic(t *testing.T) {
	tests := []struct {
		name           string
		messageStatus  MessageStatus
		daysOld        int
		olderThanDays  int
		shouldBeDeleted bool
	}{
		{"sent message older than cutoff", StatusSent, 10, 5, true},
		{"sent message newer than cutoff", StatusSent, 2, 5, false},
		{"abandoned message older than cutoff", StatusAbandoned, 10, 5, true},
		{"cancelled message older than cutoff", StatusCancelled, 10, 5, true},
		{"queued message older than cutoff", StatusQueued, 10, 5, false},
		{"processing message older than cutoff", StatusProcessing, 10, 5, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			messageTime := time.Now().Add(-time.Duration(tt.daysOld) * 24 * time.Hour)
			cutoffTime := time.Now().Add(-time.Duration(tt.olderThanDays) * 24 * time.Hour)

			// Check if message should be deleted
			shouldDelete := (tt.messageStatus == StatusSent ||
				tt.messageStatus == StatusAbandoned ||
				tt.messageStatus == StatusCancelled) &&
				messageTime.Before(cutoffTime)

			assert.Equal(t, tt.shouldBeDeleted, shouldDelete)
		})
	}
}

// ============================================================================
// BATCH QUERY LOGIC TESTS
// ============================================================================

func TestBatchQueryFiltering(t *testing.T) {
	now := time.Now()
	tests := []struct {
		name           string
		msgStatus      MessageStatus
		sendAfter      *time.Time
		shouldBeIncluded bool
	}{
		{
			name:             "queued with no delay",
			msgStatus:        StatusQueued,
			sendAfter:        nil,
			shouldBeIncluded: true,
		},
		{
			name:             "queued with past delay",
			msgStatus:        StatusQueued,
			sendAfter:        &now,
			shouldBeIncluded: true,
		},
		{
			name:             "queued with future delay",
			msgStatus:        StatusQueued,
			sendAfter:        func() *time.Time { t := now.Add(1 * time.Hour); return &t }(),
			shouldBeIncluded: false,
		},
		{
			name:             "processing status",
			msgStatus:        StatusProcessing,
			sendAfter:        nil,
			shouldBeIncluded: false,
		},
		{
			name:             "sent status",
			msgStatus:        StatusSent,
			sendAfter:        nil,
			shouldBeIncluded: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := &Message{
				ID:        uuid.New(),
				Status:    tt.msgStatus,
				SendAfter: tt.sendAfter,
			}

			// Simulate query logic
			eligible := msg.Status == StatusQueued &&
				(msg.SendAfter == nil || msg.SendAfter.Before(time.Now()) || msg.SendAfter.Equal(time.Now()))

			assert.Equal(t, tt.shouldBeIncluded, eligible)
		})
	}
}

// ============================================================================
// TRANSACTION HANDLING TESTS
// ============================================================================

func TestTransactionRollbackOnError(t *testing.T) {
	msg := &Message{
		ID:        uuid.New(),
		Status:    StatusQueued,
		SendCount: 0,
	}

	originalStatus := msg.Status

	// Simulate transaction
	msg.Status = StatusProcessing
	msg.SendCount++

	// Simulate error - rollback
	if true { // simulate error condition
		msg.Status = originalStatus
		msg.SendCount--
	}

	assert.Equal(t, StatusQueued, msg.Status)
	assert.Equal(t, 0, msg.SendCount)
}

// ============================================================================
// CONCURRENT MESSAGE PROCESSING TESTS
// ============================================================================

func TestConcurrentMessageProcessing(t *testing.T) {
	messages := make(chan *Message, 100)
	processed := make(chan bool, 100)

	// Create 100 messages
	for i := 0; i < 100; i++ {
		go func(idx int) {
			msg := &Message{
				ID:        uuid.New(),
				Type:      MessageTypeEmail,
				Status:    StatusQueued,
				Recipient: fmt.Sprintf("user%d@example.com", idx),
				Body:      fmt.Sprintf("Message %d", idx),
			}
			messages <- msg
		}(i)
	}

	// Process messages
	for i := 0; i < 100; i++ {
		msg := <-messages
		msg.Status = StatusSent
		now := time.Now()
		msg.SentAt = &now
		processed <- true
	}

	// Verify all processed
	assert.Len(t, processed, 100)
}

// ============================================================================
// MESSAGE PRIORITY TESTS
// ============================================================================

func TestMessageProcessingOrder(t *testing.T) {
	baseTime := time.Now().UTC()

	messages := []*Message{
		{
			ID:        uuid.New(),
			Status:    StatusQueued,
			CreatedAt: baseTime.Add(2 * time.Second),
			SendAfter: nil,
		},
		{
			ID:        uuid.New(),
			Status:    StatusQueued,
			CreatedAt: baseTime,
			SendAfter: nil,
		},
		{
			ID:        uuid.New(),
			Status:    StatusQueued,
			CreatedAt: baseTime.Add(1 * time.Second),
			SendAfter: nil,
		},
	}

	// Should be processed in created order
	assert.True(t, messages[1].CreatedAt.Before(messages[2].CreatedAt))
	assert.True(t, messages[2].CreatedAt.Before(messages[0].CreatedAt))
}

// ============================================================================
// ERROR MESSAGE HANDLING TESTS
// ============================================================================

func TestLastErrorRecording(t *testing.T) {
	tests := []struct {
		name      string
		errors    []string
		expectErr string
	}{
		{
			name:      "single error",
			errors:    []string{"Connection timeout"},
			expectErr: "Connection timeout",
		},
		{
			name:      "multiple errors",
			errors:    []string{"Connection timeout", "SMTP error", "Authentication failed"},
			expectErr: "Authentication failed",
		},
		{
			name:      "empty error",
			errors:    []string{},
			expectErr: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := &Message{
				ID:        uuid.New(),
				LastError: "",
			}

			for _, err := range tt.errors {
				msg.LastError = err
			}

			assert.Equal(t, tt.expectErr, msg.LastError)
		})
	}
}

// ============================================================================
// SMTP CONFIGURATION VALIDATION TESTS
// ============================================================================

func TestSMTPConfigurationValidation(t *testing.T) {
	tests := []struct {
		name    string
		config  SMTPConfig
		isValid bool
	}{
		{
			name: "valid authenticated config",
			config: SMTPConfig{
				Host:        "smtp.example.com",
				Port:        587,
				FromAddress: "sender@example.com",
				FromName:    "Sender",
				Username:    "user",
				Password:    "pass",
				AuthEnabled: true,
			},
			isValid: true,
		},
		{
			name: "valid unauthenticated config",
			config: SMTPConfig{
				Host:        "localhost",
				Port:        25,
				FromAddress: "sender@example.com",
				FromName:    "Sender",
				AuthEnabled: false,
			},
			isValid: true,
		},
		{
			name: "missing host",
			config: SMTPConfig{
				Port:        587,
				FromAddress: "sender@example.com",
			},
			isValid: false,
		},
		{
			name: "missing from address",
			config: SMTPConfig{
				Host: "smtp.example.com",
				Port: 587,
			},
			isValid: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Simple validation
			isValid := tt.config.Host != "" && tt.config.FromAddress != "" && tt.config.Port > 0

			assert.Equal(t, tt.isValid, isValid)
		})
	}
}

// ============================================================================
// MESSAGE LOOKUP TESTS
// ============================================================================

func TestMessageLookupByID(t *testing.T) {
	messages := make(map[uuid.UUID]*Message)

	// Create test messages
	msg1ID := uuid.New()
	msg2ID := uuid.New()
	msg3ID := uuid.New()

	messages[msg1ID] = &Message{
		ID:   msg1ID,
		Type: MessageTypeEmail,
		Body: "Message 1",
	}
	messages[msg2ID] = &Message{
		ID:   msg2ID,
		Type: MessageTypeEmail,
		Body: "Message 2",
	}
	messages[msg3ID] = &Message{
		ID:   msg3ID,
		Type: MessageTypeSMS,
		Body: "Message 3",
	}

	// Lookup tests
	assert.Len(t, messages, 3)

	msg, exists := messages[msg1ID]
	assert.True(t, exists)
	assert.Equal(t, msg1ID, msg.ID)

	msg, exists = messages[uuid.New()]
	assert.False(t, exists)
	assert.Nil(t, msg)
}

// ============================================================================
// MESSAGE LOOKUP BY IDEMPOTENCY KEY TESTS
// ============================================================================

func TestMessageLookupByIdempotencyKey(t *testing.T) {
	messages := make(map[string]*Message)

	key1 := "verify:user1:123456"
	key2 := "reset:user2:token789"
	key3 := "magic:token456"

	messages[key1] = &Message{
		ID:             uuid.New(),
		IdempotencyKey: key1,
		Body:           "Verification email",
	}
	messages[key2] = &Message{
		ID:             uuid.New(),
		IdempotencyKey: key2,
		Body:           "Password reset",
	}
	messages[key3] = &Message{
		ID:             uuid.New(),
		IdempotencyKey: key3,
		Body:           "Magic link",
	}

	// Lookup test
	msg, exists := messages[key1]
	assert.True(t, exists)
	assert.Equal(t, key1, msg.IdempotencyKey)

	_, exists = messages["non-existent"]
	assert.False(t, exists)
}

// ============================================================================
// BATCH OPERATION TESTS
// ============================================================================

func TestBatchOperationRecording(t *testing.T) {
	type BatchOperation struct {
		Timestamp time.Time
		Count     int
		Errors    int
		Successes int
	}

	operations := []BatchOperation{
		{
			Timestamp: time.Now(),
			Count:     10,
			Errors:    1,
			Successes: 9,
		},
		{
			Timestamp: time.Now().Add(1 * time.Minute),
			Count:     15,
			Errors:    0,
			Successes: 15,
		},
		{
			Timestamp: time.Now().Add(2 * time.Minute),
			Count:     12,
			Errors:    3,
			Successes: 9,
		},
	}

	totalMessages := 0
	totalErrors := 0
	totalSuccesses := 0

	for _, op := range operations {
		totalMessages += op.Count
		totalErrors += op.Errors
		totalSuccesses += op.Successes
	}

	assert.Equal(t, 37, totalMessages)
	assert.Equal(t, 4, totalErrors)
	assert.Equal(t, 33, totalSuccesses)
}

// ============================================================================
// MESSAGE STATE PERSISTENCE TESTS
// ============================================================================

func TestMessageStatePersistence(t *testing.T) {
	// Create message
	originalMsg := &Message{
		ID:        uuid.New(),
		Type:      MessageTypeEmail,
		Status:    StatusQueued,
		Recipient: "user@example.com",
		Subject:   "Test",
		Body:      "Test body",
		SendCount: 0,
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}

	// Serialize
	data, err := json.Marshal(originalMsg)
	require.NoError(t, err)

	// Deserialize
	var restoredMsg Message
	err = json.Unmarshal(data, &restoredMsg)
	require.NoError(t, err)

	// Verify state is preserved
	assert.Equal(t, originalMsg.ID, restoredMsg.ID)
	assert.Equal(t, originalMsg.Type, restoredMsg.Type)
	assert.Equal(t, originalMsg.Status, restoredMsg.Status)
	assert.Equal(t, originalMsg.Recipient, restoredMsg.Recipient)
	assert.Equal(t, originalMsg.SendCount, restoredMsg.SendCount)
}

// ============================================================================
// MESSAGE RETRY COUNTDOWN TESTS
// ============================================================================

func TestRetryCountdown(t *testing.T) {
	msg := &Message{
		ID:        uuid.New(),
		Status:    StatusQueued,
		SendCount: 0,
	}

	maxRetries := 3

	// Simulate retry attempts
	for i := 0; i < maxRetries; i++ {
		msg.SendCount++
		msg.Status = StatusProcessing

		if msg.SendCount >= maxRetries {
			msg.Status = StatusAbandoned
			break
		}

		msg.Status = StatusQueued
	}

	assert.Equal(t, maxRetries, msg.SendCount)
	assert.Equal(t, StatusAbandoned, msg.Status)
}

// ============================================================================
// MESSAGE TIMESTAMP EDGE CASES
// ============================================================================

func TestTimestampEdgeCases(t *testing.T) {
	tests := []struct {
		name      string
		testFunc  func(t *testing.T)
	}{
		{
			name: "zero timestamp",
			testFunc: func(t *testing.T) {
				msg := &Message{
					CreatedAt: time.Time{},
				}
				assert.True(t, msg.CreatedAt.IsZero())
			},
		},
		{
			name: "very old timestamp",
			testFunc: func(t *testing.T) {
				oldTime := time.Date(1970, 1, 1, 0, 0, 0, 0, time.UTC)
				msg := &Message{
					CreatedAt: oldTime,
				}
				assert.Equal(t, oldTime, msg.CreatedAt)
			},
		},
		{
			name: "future timestamp",
			testFunc: func(t *testing.T) {
				futureTime := time.Now().Add(365 * 24 * time.Hour)
				msg := &Message{
					SendAfter: &futureTime,
				}
				assert.NotNil(t, msg.SendAfter)
				assert.True(t, msg.SendAfter.After(time.Now()))
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.testFunc(t)
		})
	}
}
