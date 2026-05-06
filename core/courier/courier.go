// Package courier provides email and SMS delivery for Aegion.
package courier

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"net/http"
	"net/smtp"
	"net/url"
	"strings"
	texttemplate "text/template"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
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

// SMSConfig holds dynamic HTTP SMS gateway configuration.
type SMSConfig struct {
	Enabled      bool
	URL          string
	Method       string
	Headers      map[string]string
	BodyTemplate string
	Timeout      time.Duration
}

// Courier handles message delivery.
type Courier struct {
	db          *pgxpool.Pool
	smtp        SMTPConfig
	sms         SMSConfig
	templates   map[string]*template.Template
	subjects    map[string]*template.Template
	maxRetries  int
	codeExpiry  time.Duration
	linkExpiry  time.Duration
	execStmt    func(ctx context.Context, query string, args ...interface{}) (pgconn.CommandTag, error)
	queryRows   func(ctx context.Context, query string, args ...interface{}) (courierRows, error)
	sendEmailFn func(to, subject, body string) error
	sendSMSFn   func(to, body string) error
	now         func() time.Time
}

type courierRows interface {
	Close()
	Err() error
	Next() bool
	Scan(dest ...interface{}) error
}

var errCourierDBUnavailable = errors.New("courier database is not configured")

// Config holds courier configuration.
type Config struct {
	DB         *pgxpool.Pool
	SMTP       SMTPConfig
	SMS        SMSConfig
	MaxRetries int
	CodeExpiry time.Duration // Expiry for verification codes (default: 15 minutes)
	LinkExpiry time.Duration // Expiry for magic links (default: 15 minutes)
}

// New creates a new courier.
func New(cfg Config) *Courier {
	if cfg.MaxRetries == 0 {
		cfg.MaxRetries = 3
	}
	if cfg.CodeExpiry == 0 {
		cfg.CodeExpiry = 15 * time.Minute
	}
	if cfg.LinkExpiry == 0 {
		cfg.LinkExpiry = 15 * time.Minute
	}
	if cfg.SMS.Method == "" {
		cfg.SMS.Method = http.MethodPost
	}
	if cfg.SMS.Timeout == 0 {
		cfg.SMS.Timeout = 10 * time.Second
	}

	c := &Courier{
		db:         cfg.DB,
		smtp:       cfg.SMTP,
		sms:        cfg.SMS,
		templates:  make(map[string]*template.Template),
		subjects:   make(map[string]*template.Template),
		maxRetries: cfg.MaxRetries,
		codeExpiry: cfg.CodeExpiry,
		linkExpiry: cfg.LinkExpiry,
	}
	c.execStmt = func(ctx context.Context, query string, args ...interface{}) (pgconn.CommandTag, error) {
		if c.db == nil {
			return pgconn.CommandTag{}, errCourierDBUnavailable
		}
		return c.db.Exec(ctx, query, args...)
	}
	c.queryRows = func(ctx context.Context, query string, args ...interface{}) (courierRows, error) {
		if c.db == nil {
			return nil, errCourierDBUnavailable
		}
		return c.db.Query(ctx, query, args...)
	}
	c.sendEmailFn = c.sendEmail
	c.sendSMSFn = c.sendSMS
	c.now = func() time.Time { return time.Now().UTC() }
	return c
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
		CreatedAt: c.now(),
		UpdatedAt: c.now(),
	}
	if err := c.prepareMessage(msg); err != nil {
		return nil, err
	}

	for _, opt := range opts {
		opt(msg)
	}

	return c.queueMessage(ctx, msg)
}

// QueueSMS queues an SMS for delivery.
func (c *Courier) QueueSMS(ctx context.Context, recipient, body string, opts ...QueueOption) (*Message, error) {
	msg := &Message{
		ID:        uuid.New(),
		Type:      MessageTypeSMS,
		Status:    StatusQueued,
		Recipient: recipient,
		Body:      body,
		CreatedAt: c.now(),
		UpdatedAt: c.now(),
	}
	if err := c.prepareMessage(msg); err != nil {
		return nil, err
	}

	for _, opt := range opts {
		opt(msg)
	}

	return c.queueMessage(ctx, msg)
}

func (c *Courier) prepareMessage(msg *Message) error {
	normalizedRecipient, err := sanitizeDeliveryValue(msg.Recipient, "recipient", true)
	if err != nil {
		return err
	}
	normalizedSubject, err := sanitizeDeliveryValue(msg.Subject, "subject", false)
	if err != nil {
		return err
	}
	msg.Recipient = normalizedRecipient
	msg.Subject = normalizedSubject
	msg.Body = strings.TrimSpace(msg.Body)
	if msg.Body == "" {
		return fmt.Errorf("invalid body: value is required")
	}
	return nil
}

func (c *Courier) queueMessage(ctx context.Context, msg *Message) (*Message, error) {
	templateDataJSON, _ := json.Marshal(msg.TemplateData)

	_, err := c.execStmt(ctx, `
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
	rows, err := c.queryRows(ctx, `
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
			if err := json.Unmarshal(templateDataJSON, &templateData); err != nil {
				sendErr := fmt.Errorf("invalid template data: %w", err)
				_ = c.markFailed(ctx, id, sendCount, sendErr)
				processed++
				continue
			}
			rendered, err := c.renderTemplate(*templateID, templateData)
			if err == nil {
				body = rendered
			}
			if msgType == MessageTypeEmail && strings.TrimSpace(subject) == "" {
				renderedSubject, err := c.renderSubjectTemplate(*templateID, templateData)
				if err == nil {
					subject = renderedSubject
				}
			}
		}

		// Send message
		var sendErr error
		switch msgType {
		case MessageTypeEmail:
			sendErr = c.sendEmailFn(recipient, subject, body)
		case MessageTypeSMS:
			sendErr = c.sendSMSFn(recipient, body)
		}

		if sendErr != nil {
			if err := c.markFailed(ctx, id, sendCount, sendErr); err != nil {
				return processed, err
			}
		} else {
			if err := c.markSent(ctx, id); err != nil {
				return processed, err
			}
		}

		processed++
	}

	return processed, rows.Err()
}

// sendEmail sends an email via SMTP.
func (c *Courier) sendEmail(to, subject, body string) error {
	normalizedTo, err := sanitizeDeliveryValue(to, "recipient", true)
	if err != nil {
		return err
	}
	normalizedSubject, err := sanitizeDeliveryValue(subject, "subject", false)
	if err != nil {
		return err
	}
	normalizedFromAddress, err := sanitizeDeliveryValue(c.smtp.FromAddress, "from address", true)
	if err != nil {
		return err
	}
	normalizedFromName, err := sanitizeDeliveryValue(c.smtp.FromName, "from name", true)
	if err != nil {
		return err
	}
	from := fmt.Sprintf("%s <%s>", normalizedFromName, normalizedFromAddress)

	msg := fmt.Sprintf("From: %s\r\n"+
		"To: %s\r\n"+
		"Subject: %s\r\n"+
		"MIME-Version: 1.0\r\n"+
		"Content-Type: text/html; charset=\"utf-8\"\r\n"+
		"\r\n"+
		"%s",
		from, normalizedTo, normalizedSubject, body)

	addr := fmt.Sprintf("%s:%d", c.smtp.Host, c.smtp.Port)

	var auth smtp.Auth
	if c.smtp.AuthEnabled {
		auth = smtp.PlainAuth("", c.smtp.Username, c.smtp.Password, c.smtp.Host)
	}

	return smtp.SendMail(addr, auth, normalizedFromAddress, []string{normalizedTo}, []byte(msg))
}

func sanitizeDeliveryValue(value, field string, required bool) (string, error) {
	normalized := strings.TrimSpace(value)
	if strings.ContainsAny(normalized, "\r\n") {
		return "", fmt.Errorf("invalid %s: newline characters are not allowed", field)
	}
	if required && normalized == "" {
		return "", fmt.Errorf("invalid %s: value is required", field)
	}
	return normalized, nil
}

func sanitizeSMTPHeaderValue(value, field string, required bool) (string, error) {
	return sanitizeDeliveryValue(value, field, required)
}

// sendSMS sends an SMS using a generic configurable HTTP gateway.
func (c *Courier) sendSMS(to, body string) error {
	normalizedTo, err := sanitizeDeliveryValue(to, "recipient", true)
	if err != nil {
		return err
	}
	trimmedBody := strings.TrimSpace(body)
	if trimmedBody == "" {
		return fmt.Errorf("invalid body: value is required")
	}
	if !c.sms.Enabled {
		return fmt.Errorf("sms delivery is not configured")
	}
	parsedURL, err := url.Parse(strings.TrimSpace(c.sms.URL))
	if err != nil || parsedURL.Scheme == "" || parsedURL.Host == "" {
		return fmt.Errorf("sms delivery url is invalid")
	}

	payload, err := c.renderSMSPayload(normalizedTo, trimmedBody)
	if err != nil {
		return err
	}
	req, err := http.NewRequest(strings.ToUpper(strings.TrimSpace(c.sms.Method)), parsedURL.String(), bytes.NewReader(payload))
	if err != nil {
		return err
	}

	contentTypeSet := false
	for key, value := range c.sms.Headers {
		normalizedKey, err := sanitizeDeliveryValue(key, "sms header name", true)
		if err != nil {
			return err
		}
		normalizedValue, err := sanitizeDeliveryValue(value, "sms header value", true)
		if err != nil {
			return err
		}
		req.Header.Set(normalizedKey, normalizedValue)
		if strings.EqualFold(normalizedKey, "Content-Type") {
			contentTypeSet = true
		}
	}
	if !contentTypeSet {
		req.Header.Set("Content-Type", "application/json")
	}

	client := &http.Client{Timeout: c.sms.Timeout}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		responseBody, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return fmt.Errorf("sms delivery failed: status %d: %s", resp.StatusCode, strings.TrimSpace(string(responseBody)))
	}
	return nil
}

func (c *Courier) renderSMSPayload(to, body string) ([]byte, error) {
	if strings.TrimSpace(c.sms.BodyTemplate) == "" {
		return json.Marshal(map[string]string{
			"to":   to,
			"body": body,
		})
	}

	tmpl, err := texttemplate.New("sms-body").Option("missingkey=error").Parse(c.sms.BodyTemplate)
	if err != nil {
		return nil, fmt.Errorf("invalid sms body template: %w", err)
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, map[string]string{
		"To":   escapeJSONStringForTemplate(to),
		"Body": escapeJSONStringForTemplate(body),
	}); err != nil {
		return nil, fmt.Errorf("invalid sms payload rendering: %w", err)
	}
	return buf.Bytes(), nil
}

func escapeJSONStringForTemplate(value string) string {
	encoded, err := json.Marshal(value)
	if err != nil || len(encoded) < 2 {
		return value
	}
	return string(encoded[1 : len(encoded)-1])
}

// renderTemplate renders a message template.
func (c *Courier) renderTemplate(templateID string, data map[string]interface{}) (string, error) {
	tmpl, ok := c.templates[templateID]
	if !ok {
		if err := c.loadTemplate(templateID); err != nil {
			return "", err
		}
		tmpl = c.templates[templateID]
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", err
	}

	return buf.String(), nil
}

func (c *Courier) renderSubjectTemplate(templateID string, data map[string]interface{}) (string, error) {
	if _, ok := c.subjects[templateID]; !ok {
		if err := c.loadTemplate(templateID); err != nil {
			return "", err
		}
	}
	tmpl := c.subjects[templateID]
	if tmpl == nil {
		return "", fmt.Errorf("template subject not found: %s", templateID)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", err
	}
	return strings.TrimSpace(buf.String()), nil
}

func (c *Courier) loadTemplate(templateID string) error {
	rows, err := c.queryRows(context.Background(), `
		SELECT subject, body
		FROM core_courier_templates
		WHERE name = $1
		LIMIT 1
	`, templateID)
	if err != nil {
		if errors.Is(err, errCourierDBUnavailable) {
			return fmt.Errorf("template not found: %s", templateID)
		}
		return err
	}
	defer rows.Close()

	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return err
		}
		return fmt.Errorf("template not found: %s", templateID)
	}

	var subject *string
	var body string
	if err := rows.Scan(&subject, &body); err != nil {
		return err
	}

	bodyTemplate, err := template.New(templateID).Option("missingkey=zero").Parse(body)
	if err != nil {
		return fmt.Errorf("invalid template body %q: %w", templateID, err)
	}
	c.templates[templateID] = bodyTemplate

	if subject != nil && strings.TrimSpace(*subject) != "" {
		subjectTemplate, err := template.New(templateID + "-subject").Option("missingkey=zero").Parse(*subject)
		if err != nil {
			return fmt.Errorf("invalid template subject %q: %w", templateID, err)
		}
		c.subjects[templateID] = subjectTemplate
	}

	return nil
}

// markSent marks a message as sent.
func (c *Courier) markSent(ctx context.Context, id uuid.UUID) error {
	_, err := c.execStmt(ctx, `
		UPDATE core_courier_messages
		SET status = 'sent', sent_at = NOW(), updated_at = NOW()
		WHERE id = $1
	`, id)
	return err
}

// markFailed marks a message as failed.
func (c *Courier) markFailed(ctx context.Context, id uuid.UUID, sendCount int, err error) error {
	sendCount++

	if sendCount >= c.maxRetries {
		_, dbErr := c.execStmt(ctx, `
			UPDATE core_courier_messages
			SET status = 'abandoned', send_count = $2, last_error = $3, updated_at = NOW()
			WHERE id = $1
		`, id, sendCount, err.Error())
		return dbErr
	} else {
		_, dbErr := c.execStmt(ctx, `
			UPDATE core_courier_messages
			SET status = 'queued', send_count = $2, last_error = $3, updated_at = NOW()
			WHERE id = $1
		`, id, sendCount, err.Error())
		return dbErr
	}
}

// Cancel cancels a pending message.
func (c *Courier) Cancel(ctx context.Context, id uuid.UUID) error {
	result, err := c.execStmt(ctx, `
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
	cutoff := c.now().Add(-olderThan)

	result, err := c.execStmt(ctx, `
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
			<p>This code will expire in %s.</p>
		</body>
		</html>
	`, code, c.codeExpiry)

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
			<p>This code will expire in %s.</p>
			<p>If you did not request this, please ignore this email.</p>
		</body>
		</html>
	`, code, c.codeExpiry)

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
			<p>This link will expire in %s.</p>
		</body>
		</html>
	`, link, code, c.linkExpiry)

	return c.QueueEmail(ctx, to, subject, body,
		WithSource("magic_link"),
		WithIdempotencyKey(fmt.Sprintf("magic:%s", code)),
	)
}
