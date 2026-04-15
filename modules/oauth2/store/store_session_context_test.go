package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

func TestGetSessionAuthContextBranches(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC().Round(0)

	t.Run("empty session ID returns not found", func(t *testing.T) {
		s := NewWithDB(&mockDB{})
		_, err := s.GetSessionAuthContext(ctx, "   ")
		if !errors.Is(err, ErrNotFound) {
			t.Fatalf("expected ErrNotFound, got %v", err)
		}
	})

	t.Run("query row no rows maps to not found", func(t *testing.T) {
		s := NewWithDB(&mockDB{
			queryRowFn: func(context.Context, string, ...any) pgx.Row {
				return mockRow{err: pgx.ErrNoRows}
			},
		})
		_, err := s.GetSessionAuthContext(ctx, "session-1")
		if !errors.Is(err, ErrNotFound) {
			t.Fatalf("expected ErrNotFound, got %v", err)
		}
	})

	t.Run("query methods failure propagates", func(t *testing.T) {
		s := NewWithDB(&mockDB{
			queryRowFn: func(context.Context, string, ...any) pgx.Row {
				return mockRow{values: []any{"aal2", now}}
			},
			queryFn: func(context.Context, string, ...any) (pgx.Rows, error) {
				return nil, errors.New("query methods failed")
			},
		})
		_, err := s.GetSessionAuthContext(ctx, "session-1")
		if err == nil || err.Error() != "query methods failed" {
			t.Fatalf("expected query methods error, got %v", err)
		}
	})

	t.Run("rows scan and rows err propagation", func(t *testing.T) {
		s := NewWithDB(&mockDB{
			queryRowFn: func(context.Context, string, ...any) pgx.Row {
				return mockRow{values: []any{"aal2", now}}
			},
			queryFn: func(context.Context, string, ...any) (pgx.Rows, error) {
				return &mockRows{rows: [][]any{{"password"}}, scanErr: errors.New("scan failed")}, nil
			},
		})
		if _, err := s.GetSessionAuthContext(ctx, "session-1"); err == nil || err.Error() != "scan failed" {
			t.Fatalf("expected scan failed error, got %v", err)
		}

		s = NewWithDB(&mockDB{
			queryRowFn: func(context.Context, string, ...any) pgx.Row {
				return mockRow{values: []any{"aal1", now}}
			},
			queryFn: func(context.Context, string, ...any) (pgx.Rows, error) {
				return &mockRows{rows: [][]any{{"password"}}, err: errors.New("rows failed")}, nil
			},
		})
		if _, err := s.GetSessionAuthContext(ctx, "session-1"); err == nil || err.Error() != "rows failed" {
			t.Fatalf("expected rows failed error, got %v", err)
		}
	})

	t.Run("success returns methods in order", func(t *testing.T) {
		s := NewWithDB(&mockDB{
			queryRowFn: func(context.Context, string, ...any) pgx.Row {
				return mockRow{values: []any{"aal2", now}}
			},
			queryFn: func(context.Context, string, ...any) (pgx.Rows, error) {
				return &mockRows{rows: [][]any{{"password"}, {"totp"}}}, nil
			},
		})
		got, err := s.GetSessionAuthContext(ctx, "session-1")
		if err != nil {
			t.Fatalf("GetSessionAuthContext returned error: %v", err)
		}
		if got.AAL != "aal2" || !got.AuthenticatedAt.Equal(now) {
			t.Fatalf("unexpected auth context: %#v", got)
		}
		if len(got.Methods) != 2 || got.Methods[0] != "password" || got.Methods[1] != "totp" {
			t.Fatalf("unexpected methods: %#v", got.Methods)
		}
	})
}

func TestGetAccessTokenBySignatureBranches(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC().Round(0)

	t.Run("no rows maps to not found", func(t *testing.T) {
		s := NewWithDB(&mockDB{
			queryRowFn: func(context.Context, string, ...any) pgx.Row {
				return mockRow{err: errors.New("no rows in result set")}
			},
		})
		_, err := s.GetAccessTokenBySignature(ctx, "sig-1")
		if !errors.Is(err, ErrNotFound) {
			t.Fatalf("expected ErrNotFound, got %v", err)
		}
	})

	t.Run("generic query error propagates", func(t *testing.T) {
		s := NewWithDB(&mockDB{
			queryRowFn: func(context.Context, string, ...any) pgx.Row {
				return mockRow{err: errors.New("db error")}
			},
		})
		_, err := s.GetAccessTokenBySignature(ctx, "sig-1")
		if err == nil || err.Error() != "db error" {
			t.Fatalf("expected db error, got %v", err)
		}
	})

	t.Run("success decodes claims", func(t *testing.T) {
		signature := "sig-1"
		s := NewWithDB(&mockDB{
			queryRowFn: func(context.Context, string, ...any) pgx.Row {
				return mockRow{values: []any{
					"at-1", &signature, "client-1", "identity-1", "session-1", []string{"openid"}, []string{"api"},
					"https://issuer.example.com", "subject-1", []byte(`{"role":"admin"}`), false, (*time.Time)(nil), now.Add(time.Minute), now,
				}}
			},
		})
		token, err := s.GetAccessTokenBySignature(ctx, "sig-1")
		if err != nil {
			t.Fatalf("GetAccessTokenBySignature returned error: %v", err)
		}
		if token.JTI != "at-1" || token.ExtraClaims["role"] != "admin" {
			t.Fatalf("unexpected token result: %#v", token)
		}
	})
}
