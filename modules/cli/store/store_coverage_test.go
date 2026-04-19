package store

import (
	"context"
	"errors"
	"testing"
	"time"
)

type testScanRun struct {
	values []any
	err    error
}

func (s testScanRun) Scan(dest ...any) error {
	if s.err != nil {
		return s.err
	}
	for i := range dest {
		switch d := dest[i].(type) {
		case *string:
			*d = s.values[i].(string)
		case *[]byte:
			*d = s.values[i].([]byte)
		case *bool:
			*d = s.values[i].(bool)
		case *time.Time:
			*d = s.values[i].(time.Time)
		default:
			return errors.New("unsupported destination type")
		}
	}
	return nil
}

func TestMemoryStoreAdditionalBranches(t *testing.T) {
	ctx := context.Background()
	s := New()
	now := time.Now().UTC().Round(0)

	if err := s.SaveRun(ctx, CommandRun{
		ID:         "run-older",
		Command:    "status.summary",
		Arguments:  map[string]any{"limit": 10},
		Result:     map[string]any{"ok": true},
		Success:    true,
		ExecutedAt: now.Add(-time.Minute),
	}); err != nil {
		t.Fatalf("SaveRun(run-older) error = %v", err)
	}
	if err := s.SaveRun(ctx, CommandRun{
		ID:         "run-newer",
		Command:    "runtime.config",
		Arguments:  map[string]any{"section": "proxy"},
		Result:     map[string]any{"proxy": map[string]any{"enabled": true}},
		Success:    false,
		Error:      "boom",
		ExecutedAt: now,
	}); err != nil {
		t.Fatalf("SaveRun(run-newer) error = %v", err)
	}

	limited, err := s.ListRuns(ctx, 1)
	if err != nil {
		t.Fatalf("ListRuns(limit=1) error = %v", err)
	}
	if len(limited) != 1 || limited[0].ID != "run-newer" {
		t.Fatalf("ListRuns(limit=1) = %#v, want run-newer only", limited)
	}

	all, err := s.ListRuns(ctx, 0)
	if err != nil {
		t.Fatalf("ListRuns(limit=0) error = %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("ListRuns(limit=0) len = %d, want 2", len(all))
	}

	got, err := s.GetRun(ctx, " run-newer ")
	if err != nil {
		t.Fatalf("GetRun(existing) error = %v", err)
	}
	got.Arguments["section"] = "mutated"
	gotAgain, err := s.GetRun(ctx, "run-newer")
	if err != nil {
		t.Fatalf("GetRun(second) error = %v", err)
	}
	if gotAgain.Arguments["section"] == "mutated" {
		t.Fatalf("GetRun() should return cloned maps")
	}

	if _, err := s.GetRun(ctx, "missing"); !errors.Is(err, ErrRunNotFound) {
		t.Fatalf("GetRun(missing) error = %v, want %v", err, ErrRunNotFound)
	}

	status, err := s.StatusSummary(ctx)
	if err != nil {
		t.Fatalf("StatusSummary() error = %v", err)
	}
	if status["database"] != "unavailable" || status["recent_command_runs"] != len(s.runs) {
		t.Fatalf("StatusSummary() unexpected payload: %#v", status)
	}

	runtime, err := s.RuntimeConfig(ctx)
	if err != nil {
		t.Fatalf("RuntimeConfig() error = %v", err)
	}
	if runtime["database"] != "unavailable" {
		t.Fatalf("RuntimeConfig() unexpected payload: %#v", runtime)
	}

	courier, err := s.CourierSummary(ctx)
	if err != nil {
		t.Fatalf("CourierSummary() error = %v", err)
	}
	if courier["database"] != "unavailable" || courier["queued"] != 0 {
		t.Fatalf("CourierSummary() unexpected payload: %#v", courier)
	}
}

func TestCloneHelpers(t *testing.T) {
	if got := cloneMap(nil); len(got) != 0 {
		t.Fatalf("cloneMap(nil) len = %d, want 0", len(got))
	}

	run := CommandRun{
		ID:        "run-1",
		Arguments: map[string]any{"a": "1"},
		Result:    map[string]any{"b": "2"},
	}
	cloned := cloneRun(run)
	cloned.Arguments["a"] = "mutated"
	cloned.Result["b"] = "mutated"
	if run.Arguments["a"] != "1" || run.Result["b"] != "2" {
		t.Fatalf("cloneRun should deep-clone map fields")
	}
}

func TestScanRunBranches(t *testing.T) {
	now := time.Now().UTC().Round(0)
	validValues := []any{
		"run-1",
		"status.summary",
		[]byte(`{"limit":10}`),
		[]byte(`{"count":1}`),
		true,
		"",
		now,
	}

	t.Run("scan error", func(t *testing.T) {
		_, err := scanRun(testScanRun{err: errors.New("scan failed")})
		if err == nil || err.Error() != "scan failed" {
			t.Fatalf("scanRun(scan error) = %v, want scan failed", err)
		}
	})
	t.Run("arguments json error", func(t *testing.T) {
		values := append([]any(nil), validValues...)
		values[2] = []byte(`{`)
		_, err := scanRun(testScanRun{values: values})
		if err == nil {
			t.Fatalf("scanRun(arguments json error) expected error")
		}
	})
	t.Run("result json error", func(t *testing.T) {
		values := append([]any(nil), validValues...)
		values[3] = []byte(`{`)
		_, err := scanRun(testScanRun{values: values})
		if err == nil {
			t.Fatalf("scanRun(result json error) expected error")
		}
	})
	t.Run("success normalizes nil maps", func(t *testing.T) {
		values := append([]any(nil), validValues...)
		values[2] = []byte(``)
		values[3] = []byte(``)
		got, err := scanRun(testScanRun{values: values})
		if err != nil {
			t.Fatalf("scanRun(success) error = %v", err)
		}
		if got.ID != "run-1" || got.Command != "status.summary" {
			t.Fatalf("scanRun(success) unexpected run: %#v", got)
		}
		if got.Arguments == nil || len(got.Arguments) != 0 || got.Result == nil || len(got.Result) != 0 {
			t.Fatalf("scanRun(success) expected empty non-nil maps, got args=%#v result=%#v", got.Arguments, got.Result)
		}
	})
}

func TestNewPostgresValidation(t *testing.T) {
	if _, err := NewPostgres(nil); err == nil {
		t.Fatalf("NewPostgres(nil) expected error")
	}
}
