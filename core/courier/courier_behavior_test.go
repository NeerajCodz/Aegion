package courier

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/smtp"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
)

type stubCourierRows struct {
	rows    [][]interface{}
	scanErr map[int]error
	err     error
	nextIdx int
	closed  bool
}

func (s *stubCourierRows) Close()     { s.closed = true }
func (s *stubCourierRows) Err() error { return s.err }
func (s *stubCourierRows) Next() bool {
	return s.nextIdx < len(s.rows)
}
func (s *stubCourierRows) Scan(dest ...interface{}) error {
	idx := s.nextIdx
	s.nextIdx++
	if err, ok := s.scanErr[idx]; ok {
		return err
	}
	row := s.rows[idx]
	for i := range dest {
		switch d := dest[i].(type) {
		case *uuid.UUID:
			*d = row[i].(uuid.UUID)
		case *MessageType:
			*d = row[i].(MessageType)
		case *string:
			*d = row[i].(string)
		case **string:
			if row[i] == nil {
				*d = nil
			} else {
				v := row[i].(string)
				*d = &v
			}
		case *[]byte:
			*d = row[i].([]byte)
		case *int:
			*d = row[i].(int)
		default:
			return errors.New("unsupported scan destination")
		}
	}
	return nil
}

func newTestCourier(maxRetries int) *Courier {
	c := New(Config{MaxRetries: maxRetries})
	c.now = func() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) }
	return c
}

func TestQueueEmailAndOptions_WithSeams(t *testing.T) {
	c := newTestCourier(3)
	identityID := uuid.New()
	sendAfter := c.now().Add(30 * time.Minute)

	execCalls := 0
	var recordedArgs []interface{}
	c.execStmt = func(_ context.Context, _ string, args ...interface{}) (pgconn.CommandTag, error) {
		execCalls++
		recordedArgs = args
		return pgconn.NewCommandTag("INSERT 0 1"), nil
	}

	msg, err := c.QueueEmail(
		context.Background(),
		"user@example.com",
		"Subject",
		"Body",
		WithTemplate("welcome", map[string]interface{}{"name": "Alice"}),
		WithIdempotencyKey("idem-123"),
		WithSendAfter(sendAfter),
		WithIdentity(identityID),
		WithSource("core"),
	)
	if err != nil {
		t.Fatalf("QueueEmail returned error: %v", err)
	}
	if msg.Type != MessageTypeEmail || msg.Status != StatusQueued {
		t.Fatalf("unexpected queued message type/status: %s/%s", msg.Type, msg.Status)
	}
	if execCalls != 1 {
		t.Fatalf("expected one insert call, got %d", execCalls)
	}
	if len(recordedArgs) < 14 {
		t.Fatalf("expected insert args payload")
	}
	if recordedArgs[8] != "idem-123" {
		t.Fatalf("expected idempotency key to be persisted")
	}
	if recordedArgs[11] != "core" {
		t.Fatalf("expected source module to be persisted")
	}
}

func TestQueueEmail_DBError(t *testing.T) {
	c := newTestCourier(3)
	c.execStmt = func(context.Context, string, ...interface{}) (pgconn.CommandTag, error) {
		return pgconn.CommandTag{}, errors.New("insert failed")
	}

	_, err := c.QueueEmail(context.Background(), "user@example.com", "Subject", "Body")
	if err == nil || err.Error() != "insert failed" {
		t.Fatalf("expected insert failed error, got %v", err)
	}
}

func TestProcessQueue_FlowCoverage(t *testing.T) {
	t.Run("query error", func(t *testing.T) {
		c := newTestCourier(3)
		c.queryRows = func(context.Context, string, ...interface{}) (courierRows, error) {
			return nil, errors.New("query failed")
		}
		processed, err := c.ProcessQueue(context.Background(), 5)
		if err == nil || err.Error() != "query failed" {
			t.Fatalf("expected query failed, got %v", err)
		}
		if processed != 0 {
			t.Fatalf("expected 0 processed on query error")
		}
	})

	t.Run("successful email and sms send", func(t *testing.T) {
		c := newTestCourier(3)
		idEmail := uuid.New()
		idSMS := uuid.New()
		rows := &stubCourierRows{
			rows: [][]interface{}{
				{idEmail, MessageTypeEmail, "user@example.com", "Welcome", "raw body", nil, []byte(`{}`), 0},
				{idSMS, MessageTypeSMS, "+10000000000", "", "otp", nil, []byte(`{}`), 1},
			},
		}
		c.queryRows = func(context.Context, string, ...interface{}) (courierRows, error) { return rows, nil }
		c.sendEmailFn = func(to, subject, body string) error {
			if to == "" || subject == "" || body == "" {
				t.Fatalf("expected populated email payload")
			}
			return nil
		}
		c.sendSMSFn = func(to, body string) error {
			if to == "" || body == "" {
				t.Fatalf("expected populated sms payload")
			}
			return nil
		}

		updateCalls := 0
		c.execStmt = func(context.Context, string, ...interface{}) (pgconn.CommandTag, error) {
			updateCalls++
			return pgconn.NewCommandTag("UPDATE 1"), nil
		}

		processed, err := c.ProcessQueue(context.Background(), 2)
		if err != nil {
			t.Fatalf("ProcessQueue returned error: %v", err)
		}
		if processed != 2 {
			t.Fatalf("expected 2 processed, got %d", processed)
		}
		if updateCalls != 2 {
			t.Fatalf("expected markSent twice, got %d", updateCalls)
		}
		if !rows.closed {
			t.Fatalf("expected rows to be closed")
		}
	})

	t.Run("template data invalid marks failed", func(t *testing.T) {
		c := newTestCourier(3)
		id := uuid.New()
		tmpl := "welcome"
		rows := &stubCourierRows{
			rows: [][]interface{}{
				{id, MessageTypeEmail, "user@example.com", "Welcome", "raw", tmpl, []byte("{invalid"), 0},
			},
		}
		c.queryRows = func(context.Context, string, ...interface{}) (courierRows, error) { return rows, nil }
		failCalls := 0
		c.execStmt = func(context.Context, string, ...interface{}) (pgconn.CommandTag, error) {
			failCalls++
			return pgconn.NewCommandTag("UPDATE 1"), nil
		}

		processed, err := c.ProcessQueue(context.Background(), 1)
		if err != nil {
			t.Fatalf("expected nil error, got %v", err)
		}
		if processed != 1 {
			t.Fatalf("expected 1 processed, got %d", processed)
		}
		if failCalls != 1 {
			t.Fatalf("expected one markFailed call, got %d", failCalls)
		}
	})

	t.Run("send failure then db update failure", func(t *testing.T) {
		c := newTestCourier(3)
		id := uuid.New()
		rows := &stubCourierRows{
			rows: [][]interface{}{
				{id, MessageTypeEmail, "user@example.com", "Welcome", "body", nil, []byte(`{}`), 0},
			},
		}
		c.queryRows = func(context.Context, string, ...interface{}) (courierRows, error) { return rows, nil }
		c.sendEmailFn = func(string, string, string) error { return errors.New("smtp down") }
		c.execStmt = func(context.Context, string, ...interface{}) (pgconn.CommandTag, error) {
			return pgconn.CommandTag{}, errors.New("update failed")
		}

		processed, err := c.ProcessQueue(context.Background(), 1)
		if err == nil || err.Error() != "update failed" {
			t.Fatalf("expected update failed error, got %v", err)
		}
		if processed != 0 {
			t.Fatalf("expected 0 processed before hard DB failure, got %d", processed)
		}
	})

	t.Run("rows scan error is skipped", func(t *testing.T) {
		c := newTestCourier(3)
		id := uuid.New()
		rows := &stubCourierRows{
			rows: [][]interface{}{
				{id, MessageTypeEmail, "user@example.com", "Welcome", "body", nil, []byte(`{}`), 0},
			},
			scanErr: map[int]error{0: errors.New("scan failed")},
		}
		c.queryRows = func(context.Context, string, ...interface{}) (courierRows, error) { return rows, nil }

		processed, err := c.ProcessQueue(context.Background(), 1)
		if err != nil {
			t.Fatalf("expected nil error, got %v", err)
		}
		if processed != 0 {
			t.Fatalf("expected 0 processed when scan fails, got %d", processed)
		}
	})

	t.Run("rows err returned at end", func(t *testing.T) {
		c := newTestCourier(3)
		rows := &stubCourierRows{
			rows: [][]interface{}{},
			err:  errors.New("cursor err"),
		}
		c.queryRows = func(context.Context, string, ...interface{}) (courierRows, error) { return rows, nil }

		processed, err := c.ProcessQueue(context.Background(), 1)
		if err == nil || err.Error() != "cursor err" {
			t.Fatalf("expected cursor err, got %v", err)
		}
		if processed != 0 {
			t.Fatalf("expected 0 processed, got %d", processed)
		}
	})
}

func TestMarkStateTransitions_CancelAndCleanup(t *testing.T) {
	c := newTestCourier(2)
	execCalls := 0
	lastTag := pgconn.NewCommandTag("UPDATE 1")
	c.execStmt = func(_ context.Context, _ string, args ...interface{}) (pgconn.CommandTag, error) {
		execCalls++
		if len(args) == 0 {
			t.Fatalf("expected args")
		}
		return lastTag, nil
	}

	if err := c.markSent(context.Background(), uuid.New()); err != nil {
		t.Fatalf("markSent failed: %v", err)
	}
	if err := c.markFailed(context.Background(), uuid.New(), 0, errors.New("temporary")); err != nil {
		t.Fatalf("markFailed (retry) failed: %v", err)
	}
	if err := c.markFailed(context.Background(), uuid.New(), 1, errors.New("fatal")); err != nil {
		t.Fatalf("markFailed (abandon) failed: %v", err)
	}

	if execCalls != 3 {
		t.Fatalf("expected 3 exec calls, got %d", execCalls)
	}

	lastTag = pgconn.NewCommandTag("UPDATE 0")
	if err := c.Cancel(context.Background(), uuid.New()); err == nil {
		t.Fatalf("expected cancel not found error")
	}

	lastTag = pgconn.NewCommandTag("UPDATE 1")
	if err := c.Cancel(context.Background(), uuid.New()); err != nil {
		t.Fatalf("cancel should succeed when row exists: %v", err)
	}

	c.execStmt = func(context.Context, string, ...interface{}) (pgconn.CommandTag, error) {
		return pgconn.NewCommandTag("DELETE 5"), nil
	}
	deleted, err := c.Cleanup(context.Background(), time.Hour)
	if err != nil {
		t.Fatalf("cleanup failed: %v", err)
	}
	if deleted != 5 {
		t.Fatalf("expected 5 rows deleted, got %d", deleted)
	}
}

func TestEmailHelperMethods_QueueEmailCalls(t *testing.T) {
	c := newTestCourier(3)
	var observed []map[string]interface{}
	c.execStmt = func(_ context.Context, _ string, args ...interface{}) (pgconn.CommandTag, error) {
		templateJSON, _ := args[7].([]byte)
		var data map[string]interface{}
		_ = json.Unmarshal(templateJSON, &data)
		observed = append(observed, map[string]interface{}{
			"recipient": args[3],
			"subject":   args[4],
			"source":    args[11],
			"idem":      args[8],
			"data":      data,
		})
		return pgconn.NewCommandTag("INSERT 0 1"), nil
	}

	identityID := uuid.New()
	if _, err := c.SendVerificationEmail(context.Background(), "user@example.com", "123456", identityID); err != nil {
		t.Fatalf("SendVerificationEmail failed: %v", err)
	}
	if _, err := c.SendPasswordResetEmail(context.Background(), "user@example.com", "654321", identityID); err != nil {
		t.Fatalf("SendPasswordResetEmail failed: %v", err)
	}
	if _, err := c.SendMagicLinkEmail(context.Background(), "user@example.com", "https://example.com/magic", "ABCDEF"); err != nil {
		t.Fatalf("SendMagicLinkEmail failed: %v", err)
	}

	if len(observed) != 3 {
		t.Fatalf("expected 3 queued helper emails, got %d", len(observed))
	}
	if observed[0]["source"] != "core" || observed[1]["source"] != "core" || observed[2]["source"] != "magic_link" {
		t.Fatalf("unexpected helper source mapping: %#v", observed)
	}
}

func TestCourierDefaults_DBUnavailableSentinel(t *testing.T) {
	c := New(Config{})
	if _, err := c.execStmt(context.Background(), "select 1"); !errors.Is(err, errCourierDBUnavailable) {
		t.Fatalf("expected errCourierDBUnavailable from execStmt, got %v", err)
	}
	if _, err := c.queryRows(context.Background(), "select 1"); !errors.Is(err, errCourierDBUnavailable) {
		t.Fatalf("expected errCourierDBUnavailable from queryRows, got %v", err)
	}
}

type stubSMTPServer struct {
	listener net.Listener
	messages chan string
}

func startStubSMTPServer(t *testing.T) *stubSMTPServer {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen for smtp stub: %v", err)
	}
	s := &stubSMTPServer{
		listener: ln,
		messages: make(chan string, 4),
	}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go handleSMTPConn(conn, s.messages)
		}
	}()
	return s
}

func handleSMTPConn(conn net.Conn, out chan<- string) {
	defer func() {
		_ = conn.Close()
	}()
	_, _ = conn.Write([]byte("220 localhost Simple SMTP\r\n"))
	buf := make([]byte, 4096)
	inData := false
	var payload string
	for {
		n, err := conn.Read(buf)
		if err != nil {
			return
		}
		chunk := string(buf[:n])
		lines := splitCRLF(chunk)
		for _, line := range lines {
			if line == "" {
				continue
			}
			if inData {
				if line == "." {
					inData = false
					select {
					case out <- payload:
					default:
					}
					payload = ""
					_, _ = conn.Write([]byte("250 Ok\r\n"))
					continue
				}
				payload += line + "\n"
				continue
			}
			switch {
			case line == "QUIT":
				_, _ = conn.Write([]byte("221 Bye\r\n"))
				return
			case line == "DATA":
				inData = true
				_, _ = conn.Write([]byte("354 End data with <CR><LF>.<CR><LF>\r\n"))
			case line == "RSET" || line == "NOOP" || len(line) >= 4 && (line[:4] == "EHLO" || line[:4] == "HELO" || line[:4] == "MAIL" || line[:4] == "RCPT"):
				_, _ = conn.Write([]byte("250 Ok\r\n"))
			default:
				_, _ = conn.Write([]byte("250 Ok\r\n"))
			}
		}
	}
}

func splitCRLF(s string) []string {
	out := []string{}
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			line := s[start:i]
			if len(line) > 0 && line[len(line)-1] == '\r' {
				line = line[:len(line)-1]
			}
			out = append(out, line)
			start = i + 1
		}
	}
	if start < len(s) {
		line := s[start:]
		if len(line) > 0 && line[len(line)-1] == '\r' {
			line = line[:len(line)-1]
		}
		out = append(out, line)
	}
	return out
}

func TestSendEmailLiveSMTP(t *testing.T) {
	smtpStub := startStubSMTPServer(t)
	defer func() {
		_ = smtpStub.listener.Close()
	}()

	addr := smtpStub.listener.Addr().(*net.TCPAddr)
	c := New(Config{
		SMTP: SMTPConfig{
			Host:        "127.0.0.1",
			Port:        addr.Port,
			FromAddress: "noreply@example.com",
			FromName:    "Aegion",
			AuthEnabled: false,
		},
		MaxRetries: 3,
	})

	err := c.sendEmail("user@example.com", "Hello", "<p>Body</p>")
	if err != nil {
		t.Fatalf("expected smtp send to succeed, got %v", err)
	}

	select {
	case msg := <-smtpStub.messages:
		if msg == "" {
			t.Fatalf("expected smtp payload")
		}
		if !strings.Contains(msg, "Subject: Hello") {
			t.Fatalf("expected subject in smtp payload: %s", msg)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for smtp payload")
	}
}

func TestSendEmailFailureBranch(t *testing.T) {
	c := New(Config{
		SMTP: SMTPConfig{
			Host:        "127.0.0.1",
			Port:        1,
			FromAddress: "noreply@example.com",
			FromName:    "Aegion",
			AuthEnabled: false,
		},
		MaxRetries: 3,
	})
	if err := c.sendEmail("user@example.com", "Subj", "Body"); err == nil {
		t.Fatalf("expected sendEmail failure on invalid smtp endpoint")
	}
}

func TestQueueEmail_DBUnavailableViaMethods(t *testing.T) {
	c := New(Config{})
	if _, err := c.QueueEmail(context.Background(), "user@example.com", "s", "b"); !errors.Is(err, errCourierDBUnavailable) {
		t.Fatalf("expected db unavailable error from QueueEmail, got %v", err)
	}
}

func TestCancelAndCleanupDBErrorBranches(t *testing.T) {
	c := newTestCourier(3)
	c.execStmt = func(context.Context, string, ...interface{}) (pgconn.CommandTag, error) {
		return pgconn.CommandTag{}, errors.New("db down")
	}
	if err := c.Cancel(context.Background(), uuid.New()); err == nil || err.Error() != "db down" {
		t.Fatalf("expected db error from Cancel, got %v", err)
	}
	if _, err := c.Cleanup(context.Background(), time.Hour); err == nil || err.Error() != "db down" {
		t.Fatalf("expected db error from Cleanup, got %v", err)
	}
}

func TestCourierNewDefaultsAndSMTPAuthConfig(t *testing.T) {
	c := New(Config{
		SMTP: SMTPConfig{
			Host:        "smtp.example.com",
			Port:        587,
			FromAddress: "noreply@example.com",
			FromName:    "Aegion",
		},
		MaxRetries: 0,
	})
	if c.maxRetries != 3 {
		t.Fatalf("expected default maxRetries to be 3, got %d", c.maxRetries)
	}
	addr := fmt.Sprintf("%s:%d", c.smtp.Host, c.smtp.Port)
	if addr != "smtp.example.com:587" {
		t.Fatalf("unexpected smtp addr: %s", addr)
	}
	_ = smtp.PlainAuth("", "u", "p", "smtp.example.com")
}
