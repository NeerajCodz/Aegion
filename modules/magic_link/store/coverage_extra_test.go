package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

func TestNew_UsesDefaultCodeConfig(t *testing.T) {
	s := New(nil)
	if s == nil {
		t.Fatalf("expected non-nil store")
		return
	}
	if s.codeLength != 6 {
		t.Fatalf("expected default code length 6, got %d", s.codeLength)
	}
	if s.codeCharset != "0123456789" {
		t.Fatalf("expected default code charset, got %q", s.codeCharset)
	}
}

func TestGetByToken_PropagatesQueryError(t *testing.T) {
	st := NewWithDB(&fakeDB{
		queryRowFn: func(context.Context, string, ...interface{}) pgx.Row {
			return &fakeRow{err: errors.New("query failed")}
		},
	})

	_, err := st.GetByToken(context.Background(), "token")
	if err == nil || err.Error() != "query failed" {
		t.Fatalf("expected propagated query error, got %v", err)
	}
}

func TestCreate_PropagatesGenerateCodeError(t *testing.T) {
	origRandomIntN := randomIntN
	randomIntN = func(int) (int, error) { return 0, errors.New("entropy source failed") }
	t.Cleanup(func() { randomIntN = origRandomIntN })

	st := NewWithDB(&fakeDB{})

	_, err := st.Create(context.Background(), "user@example.com", CodeTypeLogin, nil, time.Minute)
	if err == nil {
		t.Fatalf("expected create to fail when entropy source fails")
	}
}
