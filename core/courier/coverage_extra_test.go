package courier

import (
	"context"
	"errors"
	"html/template"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestProcessQueue_DefaultBatchAndTemplateRenderSuccess(t *testing.T) {
	c := newTestCourier(3)

	tmpl, err := template.New("welcome").Parse("Hello {{.Name}}")
	if err != nil {
		t.Fatalf("template parse failed: %v", err)
	}
	c.templates["welcome"] = tmpl

	id := uuid.New()
	templateID := "welcome"
	rows := &stubCourierRows{
		rows: [][]interface{}{
			{id, MessageTypeEmail, "user@example.com", "Subject", "fallback", templateID, []byte(`{"Name":"Alice"}`), 0},
		},
	}

	c.queryRows = func(_ context.Context, _ string, args ...interface{}) (courierRows, error) {
		if len(args) != 1 || args[0].(int) != 10 {
			t.Fatalf("expected default batch size of 10, got args=%v", args)
		}
		return rows, nil
	}
	c.sendEmailFn = func(_ string, _ string, body string) error {
		if body != "Hello Alice" {
			t.Fatalf("expected rendered template body, got %q", body)
		}
		return nil
	}
	c.execStmt = func(context.Context, string, ...interface{}) (pgconn.CommandTag, error) {
		return pgconn.NewCommandTag("UPDATE 1"), nil
	}

	processed, err := c.ProcessQueue(context.Background(), 0)
	if err != nil {
		t.Fatalf("ProcessQueue failed: %v", err)
	}
	if processed != 1 {
		t.Fatalf("expected 1 processed message, got %d", processed)
	}
}

func TestProcessQueue_MarkSentError(t *testing.T) {
	c := newTestCourier(3)

	id := uuid.New()
	rows := &stubCourierRows{
		rows: [][]interface{}{
			{id, MessageTypeEmail, "user@example.com", "Subject", "body", nil, []byte(`{}`), 0},
		},
	}
	c.queryRows = func(context.Context, string, ...interface{}) (courierRows, error) { return rows, nil }
	c.sendEmailFn = func(string, string, string) error { return nil }
	c.execStmt = func(context.Context, string, ...interface{}) (pgconn.CommandTag, error) {
		return pgconn.CommandTag{}, errors.New("mark sent failed")
	}

	if _, err := c.ProcessQueue(context.Background(), 1); err == nil || err.Error() != "mark sent failed" {
		t.Fatalf("expected mark sent failure, got %v", err)
	}
}

func TestRenderTemplate_ExecuteError(t *testing.T) {
	c := newTestCourier(3)

	tmpl := template.Must(template.New("bad").
		Funcs(template.FuncMap{
			"fail": func() (string, error) { return "", errors.New("boom") },
		}).
		Parse(`{{fail}}`))

	c.templates["bad"] = tmpl
	if _, err := c.renderTemplate("bad", map[string]interface{}{}); err == nil {
		t.Fatalf("expected template execute error")
	}
}

func TestSendEmail_AuthEnabledPath(t *testing.T) {
	c := New(Config{
		SMTP: SMTPConfig{
			Host:        "127.0.0.1",
			Port:        1, // deliberately unreachable
			FromAddress: "sender@example.com",
			FromName:    "Sender",
			AuthEnabled: true,
			Username:    "user",
			Password:    "pass",
		},
		MaxRetries: 3,
	})

	if err := c.sendEmail("user@example.com", "subject", "body"); err == nil {
		t.Fatalf("expected SMTP send failure on unreachable endpoint")
	}
}
