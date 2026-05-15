package xlog

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestEventLifecycleEmitsRequiredFields(t *testing.T) {
	var buf bytes.Buffer
	l := New(Config{
		ServiceName:    "test-service",
		ServiceVersion: "1.2.3",
		Environment:    "test",
		Sinks:          []Sink{NewJSONSink(&buf)},
		Clock:          fixedClock(time.Unix(10, 0), time.Unix(10, int64(25*time.Millisecond))),
	})

	err := l.Start(context.Background(), "checkout.completed", WithKind(KindRequest)).
		Set("user.id", "user_123").
		Success().
		Emit()
	if err != nil {
		t.Fatalf("Emit() error = %v", err)
	}

	var record map[string]any
	if err := json.Unmarshal(buf.Bytes(), &record); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	for _, key := range []string{"event.name", "event.kind", "event.outcome", "timestamp", "duration_ms", "service.name", "service.version", "environment"} {
		if _, ok := record[key]; !ok {
			t.Fatalf("missing required key %q in %#v", key, record)
		}
	}
	if record["event.outcome"] != string(OutcomeSuccess) {
		t.Fatalf("unexpected outcome %v", record["event.outcome"])
	}
}

func TestEventRedactsSensitiveFields(t *testing.T) {
	var got Record
	l := New(Config{
		ServiceName:    "svc",
		ServiceVersion: "dev",
		Environment:    "test",
		Sinks: []Sink{SinkFunc(func(_ context.Context, r Record) error {
			got = r
			return nil
		})},
	})
	if err := l.Start(context.Background(), "auth.login", WithKind(KindSecurity)).
		Set("auth.token", "secret").
		Set("user.email", "person@example.com").
		Success().
		Emit(); err != nil {
		t.Fatalf("Emit() error = %v", err)
	}
	if got.Fields["auth.token"] != "[REDACTED]" {
		t.Fatalf("token was not redacted: %#v", got.Fields["auth.token"])
	}
	if got.Fields["user.email"] == "person@example.com" {
		t.Fatalf("email was not hashed")
	}
}

func TestEventEmitsOnce(t *testing.T) {
	count := 0
	l := New(Config{
		ServiceName:    "svc",
		ServiceVersion: "dev",
		Environment:    "test",
		Sinks: []Sink{SinkFunc(func(context.Context, Record) error {
			count++
			return nil
		})},
	})
	event := l.Start(context.Background(), "job.run", WithKind(KindJob)).Success()
	_ = event.Emit()
	_ = event.Emit()
	if count != 1 {
		t.Fatalf("expected one emit, got %d", count)
	}
}

func TestHTTPMiddlewareOutcome(t *testing.T) {
	var got Record
	l := New(Config{
		ServiceName:    "svc",
		ServiceVersion: "dev",
		Environment:    "test",
		Sinks: []Sink{SinkFunc(func(_ context.Context, r Record) error {
			got = r
			return nil
		})},
	})
	handler := l.HTTPMiddleware("http.request")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		Set(r.Context(), "user.id", "user_1")
		w.WriteHeader(http.StatusCreated)
	}))
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/v1/test", nil))
	if got.Fields["event.outcome"] != string(OutcomeSuccess) {
		t.Fatalf("unexpected outcome %#v", got.Fields)
	}
	if got.Fields["http.status_code"] != http.StatusCreated {
		t.Fatalf("unexpected status %#v", got.Fields["http.status_code"])
	}
}

func TestRunCapturesError(t *testing.T) {
	var got Record
	l := New(Config{
		ServiceName:    "svc",
		ServiceVersion: "dev",
		Environment:    "test",
		Sinks: []Sink{SinkFunc(func(_ context.Context, r Record) error {
			got = r
			return nil
		})},
	})
	wantErr := errors.New("boom")
	err := l.Start(context.Background(), "job.failed", WithKind(KindJob)).Run(func(context.Context, *Event) error {
		return wantErr
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("Run() error = %v", err)
	}
	if got.Fields["event.outcome"] != string(OutcomeError) {
		t.Fatalf("unexpected outcome %#v", got.Fields)
	}
}

func fixedClock(times ...time.Time) func() time.Time {
	i := 0
	return func() time.Time {
		if i >= len(times) {
			return times[len(times)-1]
		}
		t := times[i]
		i++
		return t
	}
}
