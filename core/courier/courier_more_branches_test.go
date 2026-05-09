package courier

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestSanitizeSMTPHeaderValueBranches(t *testing.T) {
	if _, err := sanitizeSMTPHeaderValue("bad\nvalue", "subject", true); err == nil {
		t.Fatal("sanitizeSMTPHeaderValue(newline) expected error")
	}
	if _, err := sanitizeSMTPHeaderValue("   ", "subject", true); err == nil {
		t.Fatal("sanitizeSMTPHeaderValue(required empty) expected error")
	}
	if got, err := sanitizeSMTPHeaderValue(" ok ", "subject", false); err != nil || got != "ok" {
		t.Fatalf("sanitizeSMTPHeaderValue(trim) got=%q err=%v", got, err)
	}
}

func TestSendSMSAdditionalValidationBranches(t *testing.T) {
	c := New(Config{SMS: SMSConfig{Enabled: true, URL: "://invalid", Method: http.MethodPost, Timeout: time.Second}})
	if err := c.sendSMS("+1234567890", "body"); err == nil || !strings.Contains(err.Error(), "url is invalid") {
		t.Fatalf("sendSMS(invalid url) = %v", err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }))
	defer srv.Close()

	c = New(Config{SMS: SMSConfig{Enabled: true, URL: srv.URL, Method: "BAD\nMETHOD", Timeout: time.Second}})
	if err := c.sendSMS("+1234567890", "body"); err == nil {
		t.Fatal("sendSMS(invalid method) expected error")
	}

	c = New(Config{SMS: SMSConfig{
		Enabled: true,
		URL:     srv.URL,
		Method:  http.MethodPost,
		Headers: map[string]string{"Bad\nName": "x"},
		Timeout: time.Second,
	}})
	if err := c.sendSMS("+1234567890", "body"); err == nil || !strings.Contains(err.Error(), "sms header name") {
		t.Fatalf("sendSMS(invalid header name) = %v", err)
	}

	c = New(Config{SMS: SMSConfig{
		Enabled: true,
		URL:     srv.URL,
		Method:  http.MethodPost,
		Headers: map[string]string{"X-Key": "bad\nvalue"},
		Timeout: time.Second,
	}})
	if err := c.sendSMS("+1234567890", "body"); err == nil || !strings.Contains(err.Error(), "sms header value") {
		t.Fatalf("sendSMS(invalid header value) = %v", err)
	}

	gotContentType := ""
	srvWithHeaders := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotContentType = r.Header.Get("Content-Type")
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srvWithHeaders.Close()

	c = New(Config{SMS: SMSConfig{
		Enabled: true,
		URL:     srvWithHeaders.URL,
		Method:  http.MethodPost,
		Headers: map[string]string{"Content-Type": "application/custom+json"},
		Timeout: time.Second,
	}})
	if err := c.sendSMS("+1234567890", "body"); err != nil {
		t.Fatalf("sendSMS(custom content type) = %v", err)
	}
	if gotContentType != "application/custom+json" {
		t.Fatalf("expected custom content type, got %q", gotContentType)
	}
}

func TestRenderSMSPayloadAdditionalBranches(t *testing.T) {
	c := New(Config{SMS: SMSConfig{Enabled: true, URL: "https://sms.example.com", Method: http.MethodPost}})

	c.sms.BodyTemplate = "{{"
	if _, err := c.renderSMSPayload("+123", "hello"); err == nil || !strings.Contains(err.Error(), "invalid sms body template") {
		t.Fatalf("renderSMSPayload(parse error) = %v", err)
	}

	c.sms.BodyTemplate = `{"message":"{{.Missing}}"}`
	if _, err := c.renderSMSPayload("+123", "hello"); err == nil || !strings.Contains(err.Error(), "invalid sms payload rendering") {
		t.Fatalf("renderSMSPayload(execute error) = %v", err)
	}

	c.sms.BodyTemplate = `{"to":"{{.To}}","body":"{{.Body}}"}`
	payload, err := c.renderSMSPayload(`+15551234567","body":"ignored","to":"+19998887777`, `code "\ 123`)
	if err != nil {
		t.Fatalf("renderSMSPayload(escaped values) error = %v", err)
	}
	var got map[string]string
	if err := json.Unmarshal(payload, &got); err != nil {
		t.Fatalf("renderSMSPayload output invalid json: %v payload=%s", err, string(payload))
	}
	if got["to"] != `+15551234567","body":"ignored","to":"+19998887777` {
		t.Fatalf("unexpected to field: %q", got["to"])
	}
	if got["body"] != `code "\ 123` {
		t.Fatalf("unexpected body field: %q", got["body"])
	}
}

func TestTemplateLoadingAdditionalBranches(t *testing.T) {
	c := newTestCourier(3)

	c.queryRows = func(context.Context, string, ...interface{}) (courierRows, error) {
		return &stubCourierRows{rows: [][]interface{}{}, err: errors.New("rows failed")}, nil
	}
	if _, err := c.renderSubjectTemplate("welcome", map[string]interface{}{}); err == nil || err.Error() != "rows failed" {
		t.Fatalf("renderSubjectTemplate(rows err) = %v", err)
	}

	c = newTestCourier(3)
	c.queryRows = func(context.Context, string, ...interface{}) (courierRows, error) {
		return &stubCourierRows{rows: [][]interface{}{{nil, "Hello {{.Name}}"}}}, nil
	}
	if _, err := c.renderSubjectTemplate("welcome", map[string]interface{}{"Name": "Alice"}); err == nil || !strings.Contains(err.Error(), "template subject not found") {
		t.Fatalf("renderSubjectTemplate(missing subject) = %v", err)
	}

	c = newTestCourier(3)
	c.queryRows = func(context.Context, string, ...interface{}) (courierRows, error) {
		return &stubCourierRows{rows: [][]interface{}{{nil, "{{"}}}, nil
	}
	if _, err := c.renderTemplate("broken-body", map[string]interface{}{}); err == nil || !strings.Contains(err.Error(), "invalid template body") {
		t.Fatalf("renderTemplate(invalid body) = %v", err)
	}

	c = newTestCourier(3)
	c.queryRows = func(context.Context, string, ...interface{}) (courierRows, error) {
		subject := "{{"
		return &stubCourierRows{rows: [][]interface{}{{subject, "Body"}}}, nil
	}
	if _, err := c.renderSubjectTemplate("broken-subject", map[string]interface{}{}); err == nil || !strings.Contains(err.Error(), "invalid template subject") {
		t.Fatalf("renderSubjectTemplate(invalid subject) = %v", err)
	}

	c = newTestCourier(3)
	c.queryRows = func(context.Context, string, ...interface{}) (courierRows, error) {
		return &stubCourierRows{rows: [][]interface{}{{nil, "Body"}}, scanErr: map[int]error{0: errors.New("scan failed")}}, nil
	}
	if _, err := c.renderTemplate("scan-fail", map[string]interface{}{}); err == nil || err.Error() != "scan failed" {
		t.Fatalf("renderTemplate(scan error) = %v", err)
	}
}
