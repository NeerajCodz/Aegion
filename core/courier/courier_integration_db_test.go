// +build !windows

package courier

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

// ============================================================================
// DATABASE SETUP
// ============================================================================

// setupTestDB creates a test PostgreSQL container and returns connection pool
func setupTestDB(ctx context.Context) (*pgxpool.Pool, testcontainers.Container, error) {
	container, err := postgres.RunContainer(
		ctx,
		testcontainers.WithImage("postgres:15"),
		postgres.WithDatabase("courier_test"),
		postgres.WithUsername("postgres"),
		postgres.WithPassword("password"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(10 * time.Second),
		),
	)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to start postgres container: %w", err)
	}

	// Get connection string
	connStr, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get connection string: %w", err)
	}

	// Create connection pool
	pool, err := pgxpool.New(ctx, connStr)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create connection pool: %w", err)
	}

	// Create tables
	err = createTestTables(ctx, pool)
	if err != nil {
		pool.Close()
		return nil, nil, fmt.Errorf("failed to create tables: %w", err)
	}

	return pool, container, nil
}

// createTestTables creates the courier message table for testing
func createTestTables(ctx context.Context, pool *pgxpool.Pool) error {
	schema := `
	CREATE TABLE IF NOT EXISTS core_courier_messages (
		id UUID PRIMARY KEY,
		type VARCHAR(50) NOT NULL,
		status VARCHAR(50) NOT NULL,
		recipient VARCHAR(255) NOT NULL,
		subject TEXT,
		body TEXT NOT NULL,
		template_id VARCHAR(255),
		template_data JSONB,
		idempotency_key VARCHAR(255) UNIQUE,
		send_after TIMESTAMP,
		identity_id UUID,
		source_module VARCHAR(100),
		send_count INT DEFAULT 0,
		last_error TEXT,
		sent_at TIMESTAMP,
		created_at TIMESTAMP NOT NULL,
		updated_at TIMESTAMP NOT NULL
	);

	CREATE INDEX IF NOT EXISTS idx_status ON core_courier_messages(status);
	CREATE INDEX IF NOT EXISTS idx_send_after ON core_courier_messages(send_after);
	CREATE INDEX IF NOT EXISTS idx_created_at ON core_courier_messages(created_at);
	CREATE INDEX IF NOT EXISTS idx_identity_id ON core_courier_messages(identity_id);
	`

	_, err := pool.Exec(ctx, schema)
	return err
}

// ============================================================================
// QUEUE EMAIL INTEGRATION TESTS
// ============================================================================

func TestQueueEmailIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test")
	}

	ctx := context.Background()
	pool, container, err := setupTestDB(ctx)
	require.NoError(t, err)
	defer func() {
		pool.Close()
		container.Terminate(ctx)
	}()

	cfg := Config{
		DB: pool,
		SMTP: SMTPConfig{
			Host:        "smtp.example.com",
			Port:        587,
			FromAddress: "noreply@example.com",
			FromName:    "Aegion",
		},
		MaxRetries: 3,
	}

	courier := New(cfg)

	tests := []struct {
		name      string
		recipient string
		subject   string
		body      string
		opts      []QueueOption
	}{
		{
			name:      "simple email",
			recipient: "user@example.com",
			subject:   "Welcome",
			body:      "Welcome to Aegion",
			opts:      []QueueOption{},
		},
		{
			name:      "email with options",
			recipient: "admin@example.com",
			subject:   "Admin Alert",
			body:      "Alert message",
			opts: []QueueOption{
				WithIdempotencyKey("admin-alert-123"),
				WithSource("alerts"),
			},
		},
		{
			name:      "email with identity",
			recipient: "user2@example.com",
			subject:   "Verify Email",
			body:      "Please verify",
			opts: []QueueOption{
				WithIdentity(uuid.New()),
				WithIdempotencyKey("verify-user2"),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg, err := courier.QueueEmail(ctx, tt.recipient, tt.subject, tt.body, tt.opts...)
			require.NoError(t, err)
			require.NotNil(t, msg)

			assert.Equal(t, MessageTypeEmail, msg.Type)
			assert.Equal(t, StatusQueued, msg.Status)
			assert.Equal(t, tt.recipient, msg.Recipient)
			assert.Equal(t, tt.subject, msg.Subject)
			assert.Equal(t, tt.body, msg.Body)
			assert.NotNil(t, msg.ID)
			assert.False(t, msg.CreatedAt.IsZero())
		})
	}
}

// ============================================================================
// VERIFICATION EMAIL INTEGRATION TESTS
// ============================================================================

func TestSendVerificationEmailIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test")
	}

	ctx := context.Background()
	pool, container, err := setupTestDB(ctx)
	require.NoError(t, err)
	defer func() {
		pool.Close()
		container.Terminate(ctx)
	}()

	cfg := Config{
		DB: pool,
		SMTP: SMTPConfig{
			Host:        "smtp.example.com",
			Port:        587,
			FromAddress: "noreply@example.com",
			FromName:    "Aegion",
		},
		MaxRetries: 3,
	}

	courier := New(cfg)
	identityID := uuid.New()
	to := "user@example.com"
	code := "123456"

	msg, err := courier.SendVerificationEmail(ctx, to, code, identityID)
	require.NoError(t, err)
	require.NotNil(t, msg)

	assert.Equal(t, MessageTypeEmail, msg.Type)
	assert.Equal(t, StatusQueued, msg.Status)
	assert.Equal(t, to, msg.Recipient)
	assert.Equal(t, "Verify your email address", msg.Subject)
	assert.Contains(t, msg.Body, code)
	assert.NotNil(t, msg.IdentityID)
	assert.Equal(t, identityID, *msg.IdentityID)
}

// ============================================================================
// PASSWORD RESET EMAIL INTEGRATION TESTS
// ============================================================================

func TestSendPasswordResetEmailIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test")
	}

	ctx := context.Background()
	pool, container, err := setupTestDB(ctx)
	require.NoError(t, err)
	defer func() {
		pool.Close()
		container.Terminate(ctx)
	}()

	cfg := Config{
		DB: pool,
		SMTP: SMTPConfig{
			Host:        "smtp.example.com",
			Port:        587,
			FromAddress: "noreply@example.com",
			FromName:    "Aegion",
		},
		MaxRetries: 3,
	}

	courier := New(cfg)
	identityID := uuid.New()
	to := "user@example.com"
	resetCode := "reset-token-abc123"

	msg, err := courier.SendPasswordResetEmail(ctx, to, resetCode, identityID)
	require.NoError(t, err)
	require.NotNil(t, msg)

	assert.Equal(t, MessageTypeEmail, msg.Type)
	assert.Equal(t, StatusQueued, msg.Status)
	assert.Equal(t, to, msg.Recipient)
	assert.Equal(t, "Reset your password", msg.Subject)
	assert.Contains(t, msg.Body, resetCode)
	assert.NotNil(t, msg.IdentityID)
}

// ============================================================================
// MAGIC LINK EMAIL INTEGRATION TESTS
// ============================================================================

func TestSendMagicLinkEmailIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test")
	}

	ctx := context.Background()
	pool, container, err := setupTestDB(ctx)
	require.NoError(t, err)
	defer func() {
		pool.Close()
		container.Terminate(ctx)
	}()

	cfg := Config{
		DB: pool,
		SMTP: SMTPConfig{
			Host:        "smtp.example.com",
			Port:        587,
			FromAddress: "noreply@example.com",
			FromName:    "Aegion",
		},
		MaxRetries: 3,
	}

	courier := New(cfg)
	to := "user@example.com"
	link := "https://example.com/signin?token=abc123xyz"
	code := "magic-code-123"

	msg, err := courier.SendMagicLinkEmail(ctx, to, link, code)
	require.NoError(t, err)
	require.NotNil(t, msg)

	assert.Equal(t, MessageTypeEmail, msg.Type)
	assert.Equal(t, StatusQueued, msg.Status)
	assert.Equal(t, to, msg.Recipient)
	assert.Equal(t, "Sign in to your account", msg.Subject)
	assert.Contains(t, msg.Body, link)
	assert.Contains(t, msg.Body, code)
}

// ============================================================================
// CANCEL MESSAGE INTEGRATION TESTS
// ============================================================================

func TestCancelMessageIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test")
	}

	ctx := context.Background()
	pool, container, err := setupTestDB(ctx)
	require.NoError(t, err)
	defer func() {
		pool.Close()
		container.Terminate(ctx)
	}()

	cfg := Config{
		DB: pool,
		SMTP: SMTPConfig{
			Host:        "smtp.example.com",
			Port:        587,
			FromAddress: "noreply@example.com",
			FromName:    "Aegion",
		},
		MaxRetries: 3,
	}

	courier := New(cfg)

	// Queue an email
	msg, err := courier.QueueEmail(ctx, "user@example.com", "Test", "Body")
	require.NoError(t, err)
	require.NotNil(t, msg)

	msgID := msg.ID

	// Cancel the message
	err = courier.Cancel(ctx, msgID)
	require.NoError(t, err)

	// Verify it was cancelled by checking the database
	var status string
	err = pool.QueryRow(ctx, "SELECT status FROM core_courier_messages WHERE id = $1", msgID).Scan(&status)
	require.NoError(t, err)
	assert.Equal(t, string(StatusCancelled), status)
}

// ============================================================================
// CLEANUP INTEGRATION TESTS
// ============================================================================

func TestCleanupIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test")
	}

	ctx := context.Background()
	pool, container, err := setupTestDB(ctx)
	require.NoError(t, err)
	defer func() {
		pool.Close()
		container.Terminate(ctx)
	}()

	cfg := Config{
		DB: pool,
		SMTP: SMTPConfig{
			Host:        "smtp.example.com",
			Port:        587,
			FromAddress: "noreply@example.com",
			FromName:    "Aegion",
		},
		MaxRetries: 3,
	}

	courier := New(cfg)

	// Create old messages that should be cleaned up
	for i := 0; i < 5; i++ {
		msg, err := courier.QueueEmail(ctx, fmt.Sprintf("user%d@example.com", i), "Test", "Body")
		require.NoError(t, err)

		// Manually mark as sent with old timestamp
		oldTime := time.Now().Add(-30 * 24 * time.Hour)
		_, err = pool.Exec(ctx, `
			UPDATE core_courier_messages
			SET status = 'sent', sent_at = $2, updated_at = $3
			WHERE id = $1
		`, msg.ID, oldTime, oldTime)
		require.NoError(t, err)
	}

	// Create recent messages that should NOT be cleaned up
	for i := 0; i < 3; i++ {
		_, err := courier.QueueEmail(ctx, fmt.Sprintf("recent%d@example.com", i), "Test", "Body")
		require.NoError(t, err)
	}

	// Cleanup messages older than 7 days
	deleted, err := courier.Cleanup(ctx, 7*24*time.Hour)
	require.NoError(t, err)

	// Should have deleted the 5 old sent messages
	assert.Equal(t, int64(5), deleted)

	// Verify recent messages still exist
	var count int
	err = pool.QueryRow(ctx, "SELECT COUNT(*) FROM core_courier_messages WHERE status = 'queued'").Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 3, count)
}

// ============================================================================
// BATCH PROCESSING INTEGRATION TESTS
// ============================================================================

func TestProcessQueueBatchIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test")
	}

	ctx := context.Background()
	pool, container, err := setupTestDB(ctx)
	require.NoError(t, err)
	defer func() {
		pool.Close()
		container.Terminate(ctx)
	}()

	cfg := Config{
		DB: pool,
		SMTP: SMTPConfig{
			Host:        "smtp.example.com",
			Port:        587,
			FromAddress: "noreply@example.com",
			FromName:    "Aegion",
		},
		MaxRetries: 3,
	}

	courier := New(cfg)

	// Queue 20 messages
	for i := 0; i < 20; i++ {
		_, err := courier.QueueEmail(ctx, fmt.Sprintf("user%d@example.com", i), "Test", "Body")
		require.NoError(t, err)
	}

	// Verify messages are queued
	var queuedCount int
	err = pool.QueryRow(ctx, "SELECT COUNT(*) FROM core_courier_messages WHERE status = 'queued'").Scan(&queuedCount)
	require.NoError(t, err)
	assert.Equal(t, 20, queuedCount)
}

// ============================================================================
// IDEMPOTENCY INTEGRATION TESTS
// ============================================================================

func TestIdempotencyKeyIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test")
	}

	ctx := context.Background()
	pool, container, err := setupTestDB(ctx)
	require.NoError(t, err)
	defer func() {
		pool.Close()
		container.Terminate(ctx)
	}()

	cfg := Config{
		DB: pool,
		SMTP: SMTPConfig{
			Host:        "smtp.example.com",
			Port:        587,
			FromAddress: "noreply@example.com",
			FromName:    "Aegion",
		},
		MaxRetries: 3,
	}

	courier := New(cfg)
	idempotencyKey := "unique-send-key-" + uuid.New().String()

	// Send first message
	msg1, err := courier.QueueEmail(ctx, "user@example.com", "Test", "Body",
		WithIdempotencyKey(idempotencyKey))
	require.NoError(t, err)
	require.NotNil(t, msg1)

	// Try to send duplicate (should be ignored due to ON CONFLICT)
	_, err = courier.QueueEmail(ctx, "user@example.com", "Test", "Body",
		WithIdempotencyKey(idempotencyKey))
	require.NoError(t, err)

	// Count messages with this idempotency key
	var count int
	err = pool.QueryRow(ctx, "SELECT COUNT(*) FROM core_courier_messages WHERE idempotency_key = $1", idempotencyKey).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 1, count, "Should only have one message due to idempotency")
}

// ============================================================================
// TEMPLATE DATA INTEGRATION TESTS
// ============================================================================

func TestTemplateDataPersistenceIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test")
	}

	ctx := context.Background()
	pool, container, err := setupTestDB(ctx)
	require.NoError(t, err)
	defer func() {
		pool.Close()
		container.Terminate(ctx)
	}()

	cfg := Config{
		DB: pool,
		SMTP: SMTPConfig{
			Host:        "smtp.example.com",
			Port:        587,
			FromAddress: "noreply@example.com",
			FromName:    "Aegion",
		},
		MaxRetries: 3,
	}

	courier := New(cfg)

	templateData := map[string]interface{}{
		"name":       "John Doe",
		"code":       "123456",
		"expiresIn":  15,
		"link":       "https://example.com/verify",
	}

	msg, err := courier.QueueEmail(ctx, "user@example.com", "Test", "Body",
		WithTemplate("verification-email", templateData))
	require.NoError(t, err)
	require.NotNil(t, msg)

	// Retrieve and verify template data was persisted
	var storedData []byte
	err = pool.QueryRow(ctx, "SELECT template_data FROM core_courier_messages WHERE id = $1", msg.ID).Scan(&storedData)
	require.NoError(t, err)

	var retrieved map[string]interface{}
	err = json.Unmarshal(storedData, &retrieved)
	require.NoError(t, err)

	assert.Equal(t, "John Doe", retrieved["name"])
	assert.Equal(t, "123456", retrieved["code"])
	assert.Equal(t, float64(15), retrieved["expiresIn"])
}

// ============================================================================
// DELAYED SEND INTEGRATION TESTS
// ============================================================================

func TestDelayedSendIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test")
	}

	ctx := context.Background()
	pool, container, err := setupTestDB(ctx)
	require.NoError(t, err)
	defer func() {
		pool.Close()
		container.Terminate(ctx)
	}()

	cfg := Config{
		DB: pool,
		SMTP: SMTPConfig{
			Host:        "smtp.example.com",
			Port:        587,
			FromAddress: "noreply@example.com",
			FromName:    "Aegion",
		},
		MaxRetries: 3,
	}

	courier := New(cfg)

	// Send with delay
	sendAfter := time.Now().Add(1 * time.Hour)
	msg, err := courier.QueueEmail(ctx, "user@example.com", "Test", "Body",
		WithSendAfter(sendAfter))
	require.NoError(t, err)
	require.NotNil(t, msg)

	assert.NotNil(t, msg.SendAfter)
	assert.True(t, msg.SendAfter.After(time.Now()))

	// Verify in database
	var storedSendAfter *time.Time
	err = pool.QueryRow(ctx, "SELECT send_after FROM core_courier_messages WHERE id = $1", msg.ID).Scan(&storedSendAfter)
	require.NoError(t, err)
	require.NotNil(t, storedSendAfter)
}

// ============================================================================
// SOURCE MODULE TRACKING INTEGRATION TESTS
// ============================================================================

func TestSourceModuleTrackingIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test")
	}

	ctx := context.Background()
	pool, container, err := setupTestDB(ctx)
	require.NoError(t, err)
	defer func() {
		pool.Close()
		container.Terminate(ctx)
	}()

	cfg := Config{
		DB: pool,
		SMTP: SMTPConfig{
			Host:        "smtp.example.com",
			Port:        587,
			FromAddress: "noreply@example.com",
			FromName:    "Aegion",
		},
		MaxRetries: 3,
	}

	courier := New(cfg)

	modules := []string{"auth", "user", "billing", "notifications"}

	for _, module := range modules {
		msg, err := courier.QueueEmail(ctx, "user@example.com", "Test", "Body",
			WithSource(module))
		require.NoError(t, err)

		assert.Equal(t, module, msg.SourceModule)

		// Verify in database
		var storedModule string
		err = pool.QueryRow(ctx, "SELECT source_module FROM core_courier_messages WHERE id = $1", msg.ID).Scan(&storedModule)
		require.NoError(t, err)
		assert.Equal(t, module, storedModule)
	}
}

// ============================================================================
// MULTIPLE MESSAGE TYPES INTEGRATION TESTS
// ============================================================================

func TestMultipleMessageTypesIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test")
	}

	ctx := context.Background()
	pool, container, err := setupTestDB(ctx)
	require.NoError(t, err)
	defer func() {
		pool.Close()
		container.Terminate(ctx)
	}()

	cfg := Config{
		DB: pool,
		SMTP: SMTPConfig{
			Host:        "smtp.example.com",
			Port:        587,
			FromAddress: "noreply@example.com",
			FromName:    "Aegion",
		},
		MaxRetries: 3,
	}

	courier := New(cfg)

	// Queue email
	emailMsg, err := courier.QueueEmail(ctx, "user@example.com", "Test Email", "Email body")
	require.NoError(t, err)
	assert.Equal(t, MessageTypeEmail, emailMsg.Type)

	// Verify both messages in database
	var emailCount int
	err = pool.QueryRow(ctx, "SELECT COUNT(*) FROM core_courier_messages WHERE type = 'email'").Scan(&emailCount)
	require.NoError(t, err)
	assert.Equal(t, 1, emailCount)
}

// ============================================================================
// COURIER INITIALIZATION INTEGRATION TESTS
// ============================================================================

func TestCourierInitializationIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test")
	}

	ctx := context.Background()
	pool, container, err := setupTestDB(ctx)
	require.NoError(t, err)
	defer func() {
		pool.Close()
		container.Terminate(ctx)
	}()

	tests := []struct {
		name           string
		maxRetries     int
		expectedRetries int
	}{
		{"default retries", 0, 3},
		{"custom retries 5", 5, 5},
		{"custom retries 1", 1, 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := Config{
				DB: pool,
				SMTP: SMTPConfig{
					Host:        "smtp.example.com",
					Port:        587,
					FromAddress: "noreply@example.com",
					FromName:    "Aegion",
				},
				MaxRetries: tt.maxRetries,
			}

			courier := New(cfg)
			assert.Equal(t, tt.expectedRetries, courier.maxRetries)
		})
	}
}
