// Package courier provides email and SMS delivery for Aegion.
package courier

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"net/smtp"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// MessageType represents the type of message.
type MessageType string

const (
	MessageTypeEmail MessageType = "email"
	MessageTypeSMS   MessageType = "sms"
)

// MessageStatus represents the delivery status.
type MessageStatus string

const (
	StatusQueued     MessageStatus = "queued"
	StatusProcessing MessageStatus = "processing"
	StatusSent       MessageStatus = "sent"
	StatusFailed     MessageStatus = "failed"
	StatusAbandoned  MessageStatus = "abandoned"
	StatusCancelled  MessageStatus = "cancelled"
)

// Message represents a courier message.
type Message struct {
	ID             uuid.UUID              `json:"id"`
	Type           MessageType            `json:"type"`
	Status         MessageStatus          `json:"status"`
	Recipient      string                 `json:"recipient"`
	Subject        string                 `json:"subject,omitempty"`
	Body           string                 `json:"body"`
	TemplateID     string                 `json:"template_id,omitempty"`
	TemplateData   map[string]interface{} `json:"template_data,omitempty"`
	SendCount      int                    `json:"send_count"`
	LastError      string                 `json:"last_error,omitempty"`
	IdempotencyKey string                 `json:"idempotency_key,omitempty"`
	SendAfter      *time.Time             `json:"send_after,omitempty"`
	SentAt         *time.Time             `json:"sent_at,omitempty"`
	IdentityID     *uuid.UUID             `json:"identity_id,omitempty"`
	SourceModule   string                 `json:"source_module,omitempty"`
	CreatedAt      time.Time              `json:"created_at"`
	UpdatedAt      time.Time              `json:"updated_at"`
}

// SMTPConfig holds SMTP configuration.
type SMTPConfig struct {
	Host        string
	Port        int
	FromAddress string
	FromName    string
	Username    string
	Password    string
	AuthEnabled bool
}

// Courier handles message delivery.
type Courier struct {
	db         *pgxpool.Pool
	smtp       SMTPConfig
	templates  map[string]*template.Template
	maxRetries int
}

// Config holds courier configuration.
type Config struct {
	DB         *pgxpool.Pool
	SMTP       SMTPConfig
	MaxRetries int
}

// New creates a new courier.
func New(cfg Config) *Courier {
	if cfg.MaxRetries == 0 {
		cfg.MaxRetries = 3
	}

	return &Courier{
		db:         cfg.DB,
		smtp:       cfg.SMTP,
		templates:  make(map[string]*template.Template),
		maxRetries: cfg.MaxRetries,
	}
}

// QueueEmail queues an email for delivery.
func (c *Courier) QueueEmail(ctx context.Context, recipient, subject, body string, opts ...QueueOption) (*Message, error) {
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

	for _, opt := range opts {
		opt(msg)
	}

	templateDataJSON, _ := json.Marshal(msg.TemplateData)

	_, err := c.db.Exec(ctx, `
		INSERT INTO core_courier_messages (
			id, type, status, recipient, subject, body,
			template_id, template_data, idempotency_key,
			send_after, identity_id, source_module, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
		ON CONFLICT (idempotency_key) DO NOTHING
	`,
		msg.ID, msg.Type, msg.Status, msg.Recipient, msg.Subject, msg.Body,
		msg.TemplateID, templateDataJSON, msg.IdempotencyKey,
		msg.SendAfter, msg.IdentityID, msg.SourceModule, msg.CreatedAt, msg.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}

	return msg, nil
}

// QueueOption configures a queued message.
type QueueOption func(*Message)

// WithTemplate sets the template for the message.
func WithTemplate(templateID string, data map[string]interface{}) QueueOption {
	return func(m *Message) {
		m.TemplateID = templateID
		m.TemplateData = data
	}
}

// WithIdempotencyKey sets the idempotency key.
func WithIdempotencyKey(key string) QueueOption {
	return func(m *Message) {
		m.IdempotencyKey = key
	}
}

// WithSendAfter delays the message.
func WithSendAfter(t time.Time) QueueOption {
	return func(m *Message) {
		m.SendAfter = &t
	}
}

// WithIdentity associates the message with an identity.
func WithIdentity(identityID uuid.UUID) QueueOption {
	return func(m *Message) {
		m.IdentityID = &identityID
	}
}

// WithSource sets the source module.
func WithSource(module string) QueueOption {
	return func(m *Message) {
		m.SourceModule = module
	}
}

// ProcessQueue processes pending messages.
func (c *Courier) ProcessQueue(ctx context.Context, batchSize int) (int, error) {
	if batchSize == 0 {
		batchSize = 10
	}

	// Get pending messages
	rows, err := c.db.Query(ctx, `
		UPDATE core_courier_messages
		SET status = 'processing', updated_at = NOW()
		WHERE id IN (
			SELECT id FROM core_courier_messages
			WHERE status = 'queued'
			  AND (send_after IS NULL OR send_after <= NOW())
			ORDER BY created_at
			LIMIT $1
			FOR UPDATE SKIP LOCKED
		)
		RETURNING id, type, recipient, subject, body, template_id, template_data, send_count
	`, batchSize)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	var processed int
	for rows.Next() {
		var (
			id               uuid.UUID
			msgType          MessageType
			recipient        string
			subject          string
			body             string
			templateID       *string
			templateDataJSON []byte
			sendCount        int
		)

		err := rows.Scan(&id, &msgType, &recipient, &subject, &body, &templateID, &templateDataJSON, &sendCount)
		if err != nil {
			continue
		}

		// Render template if needed
		if templateID != nil && *templateID != "" {
			var templateData map[string]interface{}
			json.Unmarshal(templateDataJSON, &templateData)
			rendered, err := c.renderTemplate(*templateID, templateData)
			if err == nil {
				body = rendered
			}
		}

		// Send message
		var sendErr error
		switch msgType {
		case MessageTypeEmail:
			sendErr = c.sendEmail(recipient, subject, body)
		case MessageTypeSMS:
			sendErr = c.sendSMS(recipient, body)
		}

		if sendErr != nil {
			c.markFailed(ctx, id, sendCount, sendErr)
		} else {
			c.markSent(ctx, id)
		}

		processed++
	}

	return processed, rows.Err()
}

// sendEmail sends an email via SMTP.
func (c *Courier) sendEmail(to, subject, body string) error {
	from := fmt.Sprintf("%s <%s>", c.smtp.FromName, c.smtp.FromAddress)
	
	msg := fmt.Sprintf("From: %s\r\n"+
		"To: %s\r\n"+
		"Subject: %s\r\n"+
		"MIME-Version: 1.0\r\n"+
		"Content-Type: text/html; charset=\"utf-8\"\r\n"+
		"\r\n"+
		"%s",
		from, to, subject, body)

	addr := fmt.Sprintf("%s:%d", c.smtp.Host, c.smtp.Port)

	var auth smtp.Auth
	if c.smtp.AuthEnabled {
		auth = smtp.PlainAuth("", c.smtp.Username, c.smtp.Password, c.smtp.Host)
	}

	return smtp.SendMail(addr, auth, c.smtp.FromAddress, []string{to}, []byte(msg))
}

// sendSMS sends an SMS (placeholder - would integrate with SMS gateway).
func (c *Courier) sendSMS(to, body string) error {
	// TODO: Integrate with SMS gateway (Twilio, AWS SNS, etc.)
	return fmt.Errorf("SMS delivery not implemented")
}

// renderTemplate renders a message template.
func (c *Courier) renderTemplate(templateID string, data map[string]interface{}) (string, error) {
	// Load template from database if not cached
	tmpl, ok := c.templates[templateID]
	if !ok {
		// TODO: Load from core_courier_templates table
		return "", fmt.Errorf("template not found: %s", templateID)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", err
	}

	return buf.String(), nil
}

// markSent marks a message as sent.
func (c *Courier) markSent(ctx context.Context, id uuid.UUID) {
	c.db.Exec(ctx, `
		UPDATE core_courier_messages
		SET status = 'sent', sent_at = NOW(), updated_at = NOW()
		WHERE id = $1
	`, id)
}

// markFailed marks a message as failed.
func (c *Courier) markFailed(ctx context.Context, id uuid.UUID, sendCount int, err error) {
	sendCount++
	
	if sendCount >= c.maxRetries {
		c.db.Exec(ctx, `
			UPDATE core_courier_messages
			SET status = 'abandoned', send_count = $2, last_error = $3, updated_at = NOW()
			WHERE id = $1
		`, id, sendCount, err.Error())
	} else {
		c.db.Exec(ctx, `
			UPDATE core_courier_messages
			SET status = 'queued', send_count = $2, last_error = $3, updated_at = NOW()
			WHERE id = $1
		`, id, sendCount, err.Error())
	}
}

// Cancel cancels a pending message.
func (c *Courier) Cancel(ctx context.Context, id uuid.UUID) error {
	result, err := c.db.Exec(ctx, `
		UPDATE core_courier_messages
		SET status = 'cancelled', updated_at = NOW()
		WHERE id = $1 AND status = 'queued'
	`, id)
	if err != nil {
		return err
	}

	if result.RowsAffected() == 0 {
		return fmt.Errorf("message not found or not cancelable")
	}

	return nil
}

// Cleanup removes old sent/abandoned messages.
func (c *Courier) Cleanup(ctx context.Context, olderThan time.Duration) (int64, error) {
	cutoff := time.Now().Add(-olderThan)

	result, err := c.db.Exec(ctx, `
		DELETE FROM core_courier_messages
		WHERE status IN ('sent', 'abandoned', 'cancelled')
		  AND updated_at < $1
	`, cutoff)
	if err != nil {
		return 0, err
	}

	return result.RowsAffected(), nil
}

// SendVerificationEmail sends a verification email.
func (c *Courier) SendVerificationEmail(ctx context.Context, to string, code string, identityID uuid.UUID) (*Message, error) {
	subject := "Verify your email address"
	body := fmt.Sprintf(`
		<html>
		<body>
			<h1>Email Verification</h1>
			<p>Your verification code is: <strong>%s</strong></p>
			<p>This code will expire in 15 minutes.</p>
		</body>
		</html>
	`, code)

	return c.QueueEmail(ctx, to, subject, body,
		WithIdentity(identityID),
		WithSource("core"),
		WithIdempotencyKey(fmt.Sprintf("verify:%s:%s", identityID.String(), code)),
	)
}

// SendPasswordResetEmail sends a password reset email.
func (c *Courier) SendPasswordResetEmail(ctx context.Context, to string, code string, identityID uuid.UUID) (*Message, error) {
	subject := "Reset your password"
	body := fmt.Sprintf(`
		<html>
		<body>
			<h1>Password Reset</h1>
			<p>Your password reset code is: <strong>%s</strong></p>
			<p>This code will expire in 15 minutes.</p>
			<p>If you did not request this, please ignore this email.</p>
		</body>
		</html>
	`, code)

	return c.QueueEmail(ctx, to, subject, body,
		WithIdentity(identityID),
		WithSource("core"),
		WithIdempotencyKey(fmt.Sprintf("reset:%s:%s", identityID.String(), code)),
	)
}

// SendMagicLinkEmail sends a magic link email.
func (c *Courier) SendMagicLinkEmail(ctx context.Context, to string, link string, code string) (*Message, error) {
	subject := "Sign in to your account"
	body := fmt.Sprintf(`
		<html>
		<body>
			<h1>Sign In</h1>
			<p>Click the link below to sign in:</p>
			<p><a href="%s">Sign In</a></p>
			<p>Or enter this code: <strong>%s</strong></p>
			<p>This link will expire in 15 minutes.</p>
		</body>
		</html>
	`, link, code)

	return c.QueueEmail(ctx, to, subject, body,
		WithSource("magic_link"),
		WithIdempotencyKey(fmt.Sprintf("magic:%s", code)),
	)
}
