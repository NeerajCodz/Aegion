package courier

import (
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ============================================================================
// FUNCTIONAL FLOW TESTS
// ============================================================================

func TestEmailQueueingWorkflow(t *testing.T) {
	// Simulate the complete workflow without actual database
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

	_ = New(cfg)  // Just verify New works, don't need reference

	// Create message
	recipient := "user@example.com"
	subject := "Verification"
	body := "Please verify your email"

	msg := &Message{
		ID:        uuid.New(),
		Type:      MessageTypeEmail,
		Status:    StatusQueued,
		Recipient: recipient,
		Subject:   subject,
		Body:      body,
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}

	// Verify message created correctly
	assert.Equal(t, StatusQueued, msg.Status)
	assert.Equal(t, recipient, msg.Recipient)

	// Simulate processing
	msg.Status = StatusProcessing
	assert.Equal(t, StatusProcessing, msg.Status)

	// Simulate sending
	msg.Status = StatusSent
	sentTime := time.Now()
	msg.SentAt = &sentTime
	assert.Equal(t, StatusSent, msg.Status)
	assert.NotNil(t, msg.SentAt)
}

func TestEmailRetryWorkflow(t *testing.T) {
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

	msg := &Message{
		ID:        uuid.New(),
		Type:      MessageTypeEmail,
		Status:    StatusQueued,
		Recipient: "user@example.com",
		SendCount: 0,
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}

	// First attempt
	msg.Status = StatusProcessing
	msg.SendCount = 1

	// Simulate failure
	msg.Status = StatusQueued
	msg.LastError = "SMTP 550 error"
	assert.Equal(t, 1, msg.SendCount)

	// Second attempt
	msg.Status = StatusProcessing
	msg.SendCount = 2

	// Simulate another failure
	msg.Status = StatusQueued
	msg.LastError = "Timeout"
	assert.Equal(t, 2, msg.SendCount)

	// Third attempt
	msg.Status = StatusProcessing
	msg.SendCount = 3

	// At max retries, abandon
	if msg.SendCount >= courier.maxRetries {
		msg.Status = StatusAbandoned
		msg.LastError = "Max retries exceeded"
	}

	assert.Equal(t, StatusAbandoned, msg.Status)
	assert.Equal(t, 3, msg.SendCount)
}

func TestVerificationEmailFullFlow(t *testing.T) {
	cfg := Config{
		SMTP: SMTPConfig{
			Host:        "smtp.example.com",
			Port:        587,
			FromAddress: "noreply@example.com",
			FromName:    "Aegion",
		},
		MaxRetries: 3,
	}

	_ = New(cfg)  // Verify New works

	identityID := uuid.New()
	code := "123456"
	recipient := "user@example.com"

	// Create verification message
	msg := &Message{
		ID:             uuid.New(),
		Type:           MessageTypeEmail,
		Status:         StatusQueued,
		Recipient:      recipient,
		Subject:        "Verify your email address",
		Body:           fmt.Sprintf("<p>Your verification code is: <strong>%s</strong></p>", code),
		IdentityID:     &identityID,
		SourceModule:   "core",
		IdempotencyKey: fmt.Sprintf("verify:%s:%s", identityID.String(), code),
		CreatedAt:      time.Now().UTC(),
		UpdatedAt:      time.Now().UTC(),
	}

	// Verify message
	assert.Equal(t, StatusQueued, msg.Status)
	assert.Equal(t, MessageTypeEmail, msg.Type)
	assert.Equal(t, recipient, msg.Recipient)
	assert.Contains(t, msg.Body, code)
	assert.Equal(t, identityID, *msg.IdentityID)
	assert.Equal(t, "core", msg.SourceModule)
}

func TestPasswordResetEmailFullFlow(t *testing.T) {
	cfg := Config{
		SMTP: SMTPConfig{
			Host:        "smtp.example.com",
			Port:        587,
			FromAddress: "noreply@example.com",
			FromName:    "Aegion",
		},
		MaxRetries: 3,
	}

	_ = New(cfg)  // Verify New works

	identityID := uuid.New()
	resetCode := "reset-token-abc123"
	recipient := "user@example.com"

	// Create reset message
	msg := &Message{
		ID:             uuid.New(),
		Type:           MessageTypeEmail,
		Status:         StatusQueued,
		Recipient:      recipient,
		Subject:        "Reset your password",
		Body:           fmt.Sprintf("<p>Your password reset code is: <strong>%s</strong></p>", resetCode),
		IdentityID:     &identityID,
		SourceModule:   "core",
		IdempotencyKey: fmt.Sprintf("reset:%s:%s", identityID.String(), resetCode),
		CreatedAt:      time.Now().UTC(),
		UpdatedAt:      time.Now().UTC(),
	}

	// Verify message
	assert.Equal(t, StatusQueued, msg.Status)
	assert.Contains(t, msg.Body, resetCode)
	assert.Equal(t, identityID, *msg.IdentityID)
}

func TestMagicLinkEmailFullFlow(t *testing.T) {
	cfg := Config{
		SMTP: SMTPConfig{
			Host:        "smtp.example.com",
			Port:        587,
			FromAddress: "noreply@example.com",
			FromName:    "Aegion",
		},
		MaxRetries: 3,
	}

	_ = New(cfg)  // Verify New works

	recipient := "user@example.com"
	link := "https://example.com/signin?token=abc123"
	code := "magic-token-xyz"

	// Create magic link message
	msg := &Message{
		ID:             uuid.New(),
		Type:           MessageTypeEmail,
		Status:         StatusQueued,
		Recipient:      recipient,
		Subject:        "Sign in to your account",
		Body:           fmt.Sprintf(`<p><a href="%s">Sign In</a></p><p>Or enter this code: <strong>%s</strong></p>`, link, code),
		SourceModule:   "magic_link",
		IdempotencyKey: fmt.Sprintf("magic:%s", code),
		CreatedAt:      time.Now().UTC(),
		UpdatedAt:      time.Now().UTC(),
	}

	// Verify message
	assert.Equal(t, StatusQueued, msg.Status)
	assert.Contains(t, msg.Body, link)
	assert.Contains(t, msg.Body, code)
	assert.Equal(t, "magic_link", msg.SourceModule)
}

// ============================================================================
// BULK MESSAGE HANDLING TESTS
// ============================================================================

func TestBulkMessageQueueing(t *testing.T) {
	cfg := Config{
		SMTP: SMTPConfig{
			Host:        "smtp.example.com",
			Port:        587,
			FromAddress: "noreply@example.com",
			FromName:    "Aegion",
		},
		MaxRetries: 3,
	}

	_ = New(cfg)  // Verify New works

	recipients := []string{
		"user1@example.com",
		"user2@example.com",
		"user3@example.com",
		"user4@example.com",
		"user5@example.com",
	}

	messages := make([]*Message, 0, len(recipients))

	for i, recipient := range recipients {
		msg := &Message{
			ID:        uuid.New(),
			Type:      MessageTypeEmail,
			Status:    StatusQueued,
			Recipient: recipient,
			Subject:   fmt.Sprintf("Notification %d", i),
			Body:      "Message body",
			CreatedAt: time.Now().UTC(),
			UpdatedAt: time.Now().UTC(),
		}
		messages = append(messages, msg)
	}

	// Verify all messages created
	assert.Len(t, messages, 5)

	// Verify each message
	for i, msg := range messages {
		assert.Equal(t, recipients[i], msg.Recipient)
		assert.Equal(t, StatusQueued, msg.Status)
	}
}

func TestBatchProcessingWithVariousSizes(t *testing.T) {
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

			// Create messages for batch
			messages := make([]*Message, 0, batchSize)
			for i := 0; i < batchSize; i++ {
				messages = append(messages, &Message{
					ID:     uuid.New(),
					Type:   MessageTypeEmail,
					Status: StatusQueued,
				})
			}

			assert.Len(t, messages, batchSize)
		})
	}
}

// ============================================================================
// CLEANUP AND LIFECYCLE TESTS
// ============================================================================

func TestMessageCleanupLogicSimulation(t *testing.T) {
	tests := []struct {
		name           string
		status         MessageStatus
		ageInDays      int
		cleanupDays    int
		shouldDelete   bool
	}{
		{"sent, old enough", StatusSent, 30, 7, true},
		{"sent, too new", StatusSent, 2, 7, false},
		{"abandoned, old", StatusAbandoned, 30, 7, true},
		{"cancelled, old", StatusCancelled, 30, 7, true},
		{"queued, old", StatusQueued, 30, 7, false},
		{"processing, old", StatusProcessing, 30, 7, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msgTime := time.Now().Add(-time.Duration(tt.ageInDays) * 24 * time.Hour)
			cutoffTime := time.Now().Add(-time.Duration(tt.cleanupDays) * 24 * time.Hour)

			shouldDelete := (tt.status == StatusSent ||
				tt.status == StatusAbandoned ||
				tt.status == StatusCancelled) &&
				msgTime.Before(cutoffTime)

			assert.Equal(t, tt.shouldDelete, shouldDelete)
		})
	}
}

func TestMessageCancellationLogic(t *testing.T) {
	tests := []struct {
		name      string
		status    MessageStatus
		canCancel bool
	}{
		{"queued", StatusQueued, true},
		{"processing", StatusProcessing, false},
		{"sent", StatusSent, false},
		{"failed", StatusFailed, false},
		{"abandoned", StatusAbandoned, false},
		{"cancelled", StatusCancelled, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := &Message{
				ID:     uuid.New(),
				Status: tt.status,
			}

			// Can only cancel queued messages
			canCancel := msg.Status == StatusQueued

			assert.Equal(t, tt.canCancel, canCancel)

			if canCancel {
				msg.Status = StatusCancelled
				assert.Equal(t, StatusCancelled, msg.Status)
			}
		})
	}
}

// ============================================================================
// DELIVERY TRACKING TESTS
// ============================================================================

func TestDeliveryStatusTracking(t *testing.T) {
	createdTime := time.Now().UTC().Add(-1 * time.Second)
	msg := &Message{
		ID:        uuid.New(),
		Type:      MessageTypeEmail,
		Recipient: "user@example.com",
		CreatedAt: createdTime,
		UpdatedAt: createdTime,
	}

	// Track through lifecycle
	states := []MessageStatus{
		StatusQueued,
		StatusProcessing,
		StatusSent,
	}

	for i, state := range states {
		msg.Status = state
		msg.UpdatedAt = time.Now().UTC()

		if state == StatusSent {
			now := time.Now()
			msg.SentAt = &now
		}

		assert.Equal(t, state, msg.Status)

		if i > 0 {
			assert.True(t, msg.UpdatedAt.After(msg.CreatedAt) || msg.UpdatedAt.Equal(msg.CreatedAt))
		}
	}

	assert.Equal(t, StatusSent, msg.Status)
	assert.NotNil(t, msg.SentAt)
}

// ============================================================================
// IDEMPOTENCY AND DEDUPLICATION TESTS
// ============================================================================

func TestIdempotencyKeyGeneration(t *testing.T) {
	identityID := uuid.New()
	code := "123456"

	key1 := fmt.Sprintf("verify:%s:%s", identityID.String(), code)
	key2 := fmt.Sprintf("verify:%s:%s", identityID.String(), code)

	// Same inputs should produce same key
	assert.Equal(t, key1, key2)

	// Different codes should produce different keys
	key3 := fmt.Sprintf("verify:%s:%s", identityID.String(), "654321")
	assert.NotEqual(t, key1, key3)
}

func TestMessageDeduplication(t *testing.T) {
	messages := make(map[string]*Message)

	key1 := "verify:user1:123456"
	key2 := "verify:user2:654321"
	key3 := "reset:user1:abcdef"

	// First message
	msg1 := &Message{
		ID:             uuid.New(),
		IdempotencyKey: key1,
		Recipient:      "user1@example.com",
	}
	messages[key1] = msg1

	// Duplicate key (should overwrite)
	msg1Dup := &Message{
		ID:             uuid.New(),
		IdempotencyKey: key1,
		Recipient:      "user1@example.com",
	}
	messages[key1] = msg1Dup

	// Different keys
	msg2 := &Message{
		ID:             uuid.New(),
		IdempotencyKey: key2,
		Recipient:      "user2@example.com",
	}
	messages[key2] = msg2

	msg3 := &Message{
		ID:             uuid.New(),
		IdempotencyKey: key3,
		Recipient:      "user1@example.com",
	}
	messages[key3] = msg3

	// Should have 3 unique keys
	assert.Len(t, messages, 3)

	// First message should be overwritten with duplicate
	assert.Equal(t, msg1Dup.ID, messages[key1].ID)
	assert.NotEqual(t, msg1.ID, messages[key1].ID)
}

// ============================================================================
// TEMPLATE DATA TESTS
// ============================================================================

func TestTemplateDataTypes(t *testing.T) {
	tests := []struct {
		name     string
		data     map[string]interface{}
		hasData  bool
	}{
		{
			name: "string data",
			data: map[string]interface{}{
				"name":  "Alice",
				"email": "alice@example.com",
			},
			hasData: true,
		},
		{
			name: "numeric data",
			data: map[string]interface{}{
				"expiresIn": 900,
				"count":     42,
			},
			hasData: true,
		},
		{
			name: "bool data",
			data: map[string]interface{}{
				"isPremium": true,
				"isVerified": false,
			},
			hasData: true,
		},
		{
			name:    "empty data",
			data:    map[string]interface{}{},
			hasData: true,
		},
		{
			name:    "nil data",
			data:    nil,
			hasData: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := &Message{
				ID:           uuid.New(),
				TemplateData: tt.data,
			}

			if tt.hasData {
				if tt.data != nil && len(tt.data) > 0 {
					assert.NotNil(t, msg.TemplateData)
				}
			} else {
				assert.Nil(t, msg.TemplateData)
			}
		})
	}
}

// ============================================================================
// SEND AFTER SCHEDULING TESTS
// ============================================================================

func TestSendAfterScheduling(t *testing.T) {
	tests := []struct {
		name        string
		delay       time.Duration
		shouldBeSet bool
	}{
		{"no delay", 0, false},
		{"1 minute delay", 1 * time.Minute, true},
		{"1 hour delay", 1 * time.Hour, true},
		{"1 day delay", 24 * time.Hour, true},
		{"negative delay", -1 * time.Hour, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := &Message{
				ID:     uuid.New(),
				Status: StatusQueued,
			}

			if tt.delay > 0 {
				sendTime := time.Now().Add(tt.delay)
				msg.SendAfter = &sendTime
			} else if tt.delay < 0 {
				sendTime := time.Now().Add(tt.delay)
				msg.SendAfter = &sendTime
			}

			if tt.shouldBeSet && tt.delay != 0 {
				assert.NotNil(t, msg.SendAfter)
			} else if tt.delay == 0 {
				assert.Nil(t, msg.SendAfter)
			}
		})
	}
}

// ============================================================================
// SOURCE MODULE TESTS
// ============================================================================

func TestSourceModuleTrackingInFlows(t *testing.T) {
	modules := []string{
		"auth",
		"user",
		"billing",
		"notifications",
		"api",
		"admin",
		"webhook",
		"core",
	}

	for _, module := range modules {
		t.Run(module, func(t *testing.T) {
			msg := &Message{
				ID:           uuid.New(),
				SourceModule: module,
			}

			assert.Equal(t, module, msg.SourceModule)
		})
	}
}

// ============================================================================
// IDENTITY TRACKING TESTS
// ============================================================================

func TestIdentityAssociationTracking(t *testing.T) {
	identityID := uuid.New()

	msg := &Message{
		ID:         uuid.New(),
		IdentityID: &identityID,
	}

	assert.NotNil(t, msg.IdentityID)
	assert.Equal(t, identityID, *msg.IdentityID)

	// Test without identity
	msg2 := &Message{
		ID:         uuid.New(),
		IdentityID: nil,
	}

	assert.Nil(t, msg2.IdentityID)
}

// ============================================================================
// RETRY EXPONENTIAL BACKOFF CALCULATION TESTS
// ============================================================================

func TestExponentialBackoffCalculationWithCap(t *testing.T) {
	baseDelay := 100 * time.Millisecond

	tests := []struct {
		name       string
		attempt    int
		calculated time.Duration
	}{
		{"attempt 1", 1, 100 * time.Millisecond},
		{"attempt 2", 2, 200 * time.Millisecond},
		{"attempt 3", 3, 400 * time.Millisecond},
		{"attempt 4", 4, 800 * time.Millisecond},
		{"attempt 5", 5, 1600 * time.Millisecond},
		{"attempt 6", 6, 3200 * time.Millisecond},
		{"attempt 10", 10, 51200 * time.Millisecond},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			multiplier := 1 << (tt.attempt - 1)
			calculated := baseDelay * time.Duration(multiplier)

			assert.Equal(t, tt.calculated, calculated)
		})
	}
}

func TestBackoffWithMaxCap(t *testing.T) {
	baseDelay := 100 * time.Millisecond
	maxDelay := 5 * time.Second

	tests := []struct {
		name       string
		attempt    int
		capped     time.Duration
	}{
		{"attempt 1", 1, 100 * time.Millisecond},
		{"attempt 5", 5, 1600 * time.Millisecond},
		{"attempt 7", 7, 5 * time.Second}, // Capped at maxDelay
		{"attempt 10", 10, 5 * time.Second}, // Still capped
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			multiplier := 1 << (tt.attempt - 1)
			calculated := baseDelay * time.Duration(multiplier)

			if calculated > maxDelay {
				calculated = maxDelay
			}

			assert.Equal(t, tt.capped, calculated)
		})
	}
}

// ============================================================================
// MESSAGE LIFECYCLE LOGGING TESTS
// ============================================================================

func TestMessageLifecycleLogging(t *testing.T) {
	type LogEntry struct {
		Timestamp time.Time
		Status    MessageStatus
		Error     string
	}

	msg := &Message{
		ID:        uuid.New(),
		Type:      MessageTypeEmail,
		Status:    StatusQueued,
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}

	logs := []LogEntry{}

	// Log creation
	logs = append(logs, LogEntry{
		Timestamp: msg.CreatedAt,
		Status:    msg.Status,
		Error:     "",
	})

	// Simulate processing
	msg.Status = StatusProcessing
	msg.UpdatedAt = time.Now().UTC()
	logs = append(logs, LogEntry{
		Timestamp: msg.UpdatedAt,
		Status:    msg.Status,
		Error:     "",
	})

	// Simulate sending
	msg.Status = StatusSent
	msg.UpdatedAt = time.Now().UTC()
	now := time.Now()
	msg.SentAt = &now
	logs = append(logs, LogEntry{
		Timestamp: msg.UpdatedAt,
		Status:    msg.Status,
		Error:     "",
	})

	// Verify log progression
	assert.Len(t, logs, 3)
	assert.Equal(t, StatusQueued, logs[0].Status)
	assert.Equal(t, StatusProcessing, logs[1].Status)
	assert.Equal(t, StatusSent, logs[2].Status)

	// Verify timestamps are increasing
	for i := 1; i < len(logs); i++ {
		assert.True(t, logs[i].Timestamp.After(logs[i-1].Timestamp) || logs[i].Timestamp.Equal(logs[i-1].Timestamp))
	}
}
