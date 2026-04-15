package courier

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestQueueSMS_WithSeams(t *testing.T) {
	c := newTestCourier(3)
	execCalls := 0
	c.execStmt = func(_ context.Context, _ string, args ...interface{}) (pgconn.CommandTag, error) {
		execCalls++
		if args[1] != MessageTypeSMS {
			t.Fatalf("expected sms message type, got %v", args[1])
		}
		if args[3] != "+1234567890" {
			t.Fatalf("unexpected recipient: %v", args[3])
		}
		return pgconn.NewCommandTag("INSERT 0 1"), nil
	}

	msg, err := c.QueueSMS(context.Background(), "+1234567890", "Your code is 123456", WithSource("mfa"))
	if err != nil {
		t.Fatalf("QueueSMS returned error: %v", err)
	}
	if msg.Type != MessageTypeSMS {
		t.Fatalf("expected sms type, got %s", msg.Type)
	}
	if msg.SourceModule != "mfa" {
		t.Fatalf("expected source module to be applied")
	}
	if execCalls != 1 {
		t.Fatalf("expected one insert call, got %d", execCalls)
	}
}

func TestQueueSMS_RejectsInvalidPayload(t *testing.T) {
	c := New(Config{})
	if _, err := c.QueueSMS(context.Background(), "", "hello"); err == nil {
		t.Fatalf("expected empty recipient validation error")
	}
	if _, err := c.QueueSMS(context.Background(), "+1234567890", "   "); err == nil {
		t.Fatalf("expected empty body validation error")
	}
}

func TestRenderTemplate_LoadsFromDatabase(t *testing.T) {
	c := newTestCourier(3)
	queryCalls := 0
	rows := &stubCourierRows{
		rows: [][]interface{}{
			{"Hello {{.Name}}", "Welcome {{.Name}}"},
		},
	}
	c.queryRows = func(_ context.Context, _ string, args ...interface{}) (courierRows, error) {
		queryCalls++
		if len(args) != 1 || args[0] != "welcome" {
			t.Fatalf("unexpected template lookup args: %v", args)
		}
		return rows, nil
	}

	body, err := c.renderTemplate("welcome", map[string]interface{}{"Name": "Alice"})
	if err != nil {
		t.Fatalf("renderTemplate failed: %v", err)
	}
	if body != "Welcome Alice" {
		t.Fatalf("unexpected rendered body: %q", body)
	}
	subject, err := c.renderSubjectTemplate("welcome", map[string]interface{}{"Name": "Alice"})
	if err != nil {
		t.Fatalf("renderSubjectTemplate failed: %v", err)
	}
	if subject != "Hello Alice" {
		t.Fatalf("unexpected rendered subject: %q", subject)
	}

	_, err = c.renderTemplate("welcome", map[string]interface{}{"Name": "Bob"})
	if err != nil {
		t.Fatalf("expected cached template to render: %v", err)
	}
	if queryCalls != 1 {
		t.Fatalf("expected template lookup to be cached, got %d calls", queryCalls)
	}
}

func TestRenderTemplate_DBFailurePropagates(t *testing.T) {
	c := newTestCourier(3)
	c.queryRows = func(context.Context, string, ...interface{}) (courierRows, error) {
		return nil, errors.New("db unavailable")
	}
	if _, err := c.renderTemplate("welcome", map[string]interface{}{}); err == nil || err.Error() != "db unavailable" {
		t.Fatalf("expected db failure, got %v", err)
	}
}

func TestSendSMS_HTTPGateway(t *testing.T) {
	var gotMethod string
	var gotContentType string
	var gotBody map[string]string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotContentType = r.Header.Get("Content-Type")
		defer r.Body.Close()
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("failed to decode sms payload: %v", err)
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()

	c := New(Config{
		SMS: SMSConfig{
			Enabled: true,
			URL:     server.URL,
			Method:  http.MethodPost,
			Headers: map[string]string{
				"X-Provider-Key": "test-key",
			},
			Timeout: 2 * time.Second,
		},
	})

	if err := c.sendSMS("+1234567890", "Your code is 123456"); err != nil {
		t.Fatalf("sendSMS failed: %v", err)
	}
	if gotMethod != http.MethodPost {
		t.Fatalf("unexpected method: %s", gotMethod)
	}
	if gotContentType != "application/json" {
		t.Fatalf("unexpected content type: %s", gotContentType)
	}
	if gotBody["to"] != "+1234567890" || gotBody["body"] != "Your code is 123456" {
		t.Fatalf("unexpected sms payload: %#v", gotBody)
	}
}

func TestSendSMS_CustomTemplateAndFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte("upstream failed"))
	}))
	defer server.Close()

	c := New(Config{
		SMS: SMSConfig{
			Enabled:      true,
			URL:          server.URL,
			Method:       http.MethodPost,
			BodyTemplate: `{"phone":"{{.To}}","message":"{{.Body}}"}`,
			Timeout:      2 * time.Second,
		},
	})

	if err := c.sendSMS("+1234567890", "Hello"); err == nil || err.Error() == "" {
		t.Fatalf("expected upstream failure")
	}
}

func TestProcessQueue_RendersTemplateSubjectFromDatabase(t *testing.T) {
	c := newTestCourier(3)
	messageID := uuid.New()
	rows := &stubCourierRows{
		rows: [][]interface{}{
			{messageID, MessageTypeEmail, "user@example.com", "", "fallback", "welcome", []byte(`{"Name":"Alice"}`), 0},
		},
	}
	templateRows := &stubCourierRows{
		rows: [][]interface{}{
			{"Hi {{.Name}}", "Welcome {{.Name}}"},
		},
	}
	c.queryRows = func(_ context.Context, query string, args ...interface{}) (courierRows, error) {
		if len(args) == 1 {
			switch v := args[0].(type) {
			case int:
				return rows, nil
			case string:
				if v == "welcome" {
					return templateRows, nil
				}
			}
		}
		t.Fatalf("unexpected query args: %v", args)
		return nil, nil
	}
	c.sendEmailFn = func(_ string, subject, body string) error {
		if subject != "Hi Alice" {
			t.Fatalf("expected rendered subject, got %q", subject)
		}
		if body != "Welcome Alice" {
			t.Fatalf("expected rendered body, got %q", body)
		}
		return nil
	}
	c.execStmt = func(context.Context, string, ...interface{}) (pgconn.CommandTag, error) {
		return pgconn.NewCommandTag("UPDATE 1"), nil
	}

	processed, err := c.ProcessQueue(context.Background(), 1)
	if err != nil {
		t.Fatalf("ProcessQueue failed: %v", err)
	}
	if processed != 1 {
		t.Fatalf("expected one processed message, got %d", processed)
	}
}
