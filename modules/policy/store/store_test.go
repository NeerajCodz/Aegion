package store

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeDB struct {
	queryRowFn func(ctx context.Context, sql string, args ...any) pgx.Row
	queryFn    func(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

func (f *fakeDB) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	if f.queryRowFn != nil {
		return f.queryRowFn(ctx, sql, args...)
	}
	return fakeRow{err: pgx.ErrNoRows}
}

func (f *fakeDB) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	if f.queryFn != nil {
		return f.queryFn(ctx, sql, args...)
	}
	return &fakeRows{}, nil
}

type fakeRow struct {
	vals []any
	err  error
}

func (r fakeRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	if len(dest) != len(r.vals) {
		return fmt.Errorf("scan mismatch: expected %d destinations, got %d", len(r.vals), len(dest))
	}
	for i := range dest {
		switch d := dest[i].(type) {
		case *string:
			v, ok := r.vals[i].(string)
			if !ok {
				return fmt.Errorf("expected string value, got %T", r.vals[i])
			}
			*d = v
		default:
			return fmt.Errorf("unsupported scan destination %T", dest[i])
		}
	}
	return nil
}

type fakeRows struct {
	data [][]any
	idx  int
	err  error
}

func (r *fakeRows) Close() {}

func (r *fakeRows) Err() error { return r.err }

func (r *fakeRows) CommandTag() pgconn.CommandTag { return pgconn.NewCommandTag("SELECT 0") }

func (r *fakeRows) FieldDescriptions() []pgconn.FieldDescription { return nil }

func (r *fakeRows) Next() bool {
	if r.idx >= len(r.data) {
		return false
	}
	r.idx++
	return true
}

func (r *fakeRows) Scan(dest ...any) error {
	if r.idx == 0 || r.idx > len(r.data) {
		return errors.New("scan called without current row")
	}
	row := r.data[r.idx-1]
	if len(dest) != len(row) {
		return fmt.Errorf("scan mismatch: expected %d destinations, got %d", len(row), len(dest))
	}
	for i := range dest {
		switch d := dest[i].(type) {
		case *string:
			v, ok := row[i].(string)
			if !ok {
				return fmt.Errorf("expected string value, got %T", row[i])
			}
			*d = v
		case *int:
			v, ok := row[i].(int)
			if !ok {
				return fmt.Errorf("expected int value, got %T", row[i])
			}
			*d = v
		case *bool:
			v, ok := row[i].(bool)
			if !ok {
				return fmt.Errorf("expected bool value, got %T", row[i])
			}
			*d = v
		default:
			return fmt.Errorf("unsupported scan destination %T", dest[i])
		}
	}
	return nil
}

func (r *fakeRows) Values() ([]any, error) {
	if r.idx == 0 || r.idx > len(r.data) {
		return nil, errors.New("values called without current row")
	}
	return r.data[r.idx-1], nil
}

func (r *fakeRows) RawValues() [][]byte { return nil }

func (r *fakeRows) Conn() *pgx.Conn { return nil }

func TestStore_ListRoleIDsByIdentity(t *testing.T) {
	st := NewWithDB(&fakeDB{
		queryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			assert.Contains(t, sql, "FROM pol_role_assignments")
			assert.Equal(t, []any{"id-1"}, args)
			return &fakeRows{data: [][]any{{"role-1"}, {"role-2"}}}, nil
		},
	})

	roles, err := st.ListRoleIDsByIdentity(context.Background(), "id-1")
	require.NoError(t, err)
	assert.Equal(t, []string{"role-1", "role-2"}, roles)
}

func TestStore_ListPermissionsByRoleIDs(t *testing.T) {
	t.Run("empty role list", func(t *testing.T) {
		st := NewWithDB(&fakeDB{})
		perms, err := st.ListPermissionsByRoleIDs(context.Background(), nil)
		require.NoError(t, err)
		assert.Empty(t, perms)
	})

	t.Run("returns permissions", func(t *testing.T) {
		st := NewWithDB(&fakeDB{
			queryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
				assert.Contains(t, sql, "FROM pol_permissions")
				return &fakeRows{data: [][]any{{"documents", "read"}, {"*", "*"}}}, nil
			},
		})

		perms, err := st.ListPermissionsByRoleIDs(context.Background(), []string{"role-1"})
		require.NoError(t, err)
		require.Len(t, perms, 2)
		assert.Equal(t, Permission{ResourceType: "documents", Action: "read"}, perms[0])
		assert.Equal(t, Permission{ResourceType: "*", Action: "*"}, perms[1])
	})
}

func TestStore_GetRoleIDByName(t *testing.T) {
	t.Run("returns role id", func(t *testing.T) {
		st := NewWithDB(&fakeDB{
			queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
				assert.Contains(t, sql, "FROM pol_roles")
				return fakeRow{vals: []any{"role-1"}}
			},
		})

		roleID, err := st.GetRoleIDByName(context.Background(), "admin")
		require.NoError(t, err)
		assert.Equal(t, "role-1", roleID)
	})

	t.Run("not found", func(t *testing.T) {
		st := NewWithDB(&fakeDB{
			queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
				return fakeRow{err: pgx.ErrNoRows}
			},
		})

		_, err := st.GetRoleIDByName(context.Background(), "missing")
		assert.ErrorIs(t, err, ErrNotFound)
	})
}

func TestStore_ListABACRules(t *testing.T) {
	t.Run("returns enabled rules ordered by priority", func(t *testing.T) {
		st := NewWithDB(&fakeDB{
			queryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
				assert.Contains(t, sql, "FROM pol_abac_rules")
				assert.Contains(t, sql, "WHERE enabled = TRUE")
				assert.Empty(t, args)
				return &fakeRows{data: [][]any{
					{"deny_external_writes", "ip == 10.0.0.1", 10, "deny", true},
					{"allow_reader", "resource_type == documents", 20, "allow", true},
				}}, nil
			},
		})

		rules, err := st.ListABACRules(context.Background())
		require.NoError(t, err)
		require.Len(t, rules, 2)
		assert.Equal(t, ABACRule{Name: "deny_external_writes", Expression: "ip == 10.0.0.1", Priority: 10, Effect: "deny", Enabled: true}, rules[0])
		assert.Equal(t, ABACRule{Name: "allow_reader", Expression: "resource_type == documents", Priority: 20, Effect: "allow", Enabled: true}, rules[1])
	})

	t.Run("query error", func(t *testing.T) {
		st := NewWithDB(&fakeDB{
			queryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
				return nil, errors.New("db unavailable")
			},
		})

		_, err := st.ListABACRules(context.Background())
		assert.ErrorContains(t, err, "db unavailable")
	})
}
