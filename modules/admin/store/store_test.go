package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	bcrypt "github.com/aegion/aegion/internal/platform/bcryptcompat"
)

type fakeDB struct {
	queryRowFn func(ctx context.Context, sql string, args ...any) pgx.Row
	queryFn    func(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	execFn     func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
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

func (f *fakeDB) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	if f.execFn != nil {
		return f.execFn(ctx, sql, args...)
	}
	return pgconn.NewCommandTag("UPDATE 1"), nil
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
		if err := assignScanDest(dest[i], r.vals[i]); err != nil {
			return err
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

func (r *fakeRows) Err() error {
	return r.err
}

func (r *fakeRows) CommandTag() pgconn.CommandTag {
	return pgconn.NewCommandTag("SELECT 0")
}

func (r *fakeRows) FieldDescriptions() []pgconn.FieldDescription {
	return nil
}

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
		if err := assignScanDest(dest[i], row[i]); err != nil {
			return err
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

func (r *fakeRows) RawValues() [][]byte {
	return nil
}

func (r *fakeRows) Conn() *pgx.Conn {
	return nil
}

func assignScanDest(dest any, val any) error {
	switch d := dest.(type) {
	case *string:
		v, ok := val.(string)
		if !ok {
			return fmt.Errorf("expected string value, got %T", val)
		}
		*d = v
		return nil
	case *bool:
		v, ok := val.(bool)
		if !ok {
			return fmt.Errorf("expected bool value, got %T", val)
		}
		*d = v
		return nil
	case *int64:
		switch v := val.(type) {
		case int:
			*d = int64(v)
		case int64:
			*d = v
		default:
			return fmt.Errorf("expected int64 value, got %T", val)
		}
		return nil
	case *int:
		switch v := val.(type) {
		case int:
			*d = v
		case int64:
			*d = int(v)
		default:
			return fmt.Errorf("expected int value, got %T", val)
		}
		return nil
	case *uuid.UUID:
		v, ok := val.(uuid.UUID)
		if !ok {
			return fmt.Errorf("expected uuid value, got %T", val)
		}
		*d = v
		return nil
	case **uuid.UUID:
		if val == nil {
			*d = nil
			return nil
		}
		switch v := val.(type) {
		case uuid.UUID:
			cp := v
			*d = &cp
		case *uuid.UUID:
			*d = v
		default:
			return fmt.Errorf("expected uuid/*uuid value, got %T", val)
		}
		return nil
	case *[]byte:
		switch v := val.(type) {
		case []byte:
			*d = append((*d)[:0], v...)
		case string:
			*d = []byte(v)
		default:
			return fmt.Errorf("expected []byte/string value, got %T", val)
		}
		return nil
	case **string:
		if val == nil {
			*d = nil
			return nil
		}
		switch v := val.(type) {
		case string:
			cp := v
			*d = &cp
		case *string:
			*d = v
		default:
			return fmt.Errorf("expected string/*string value, got %T", val)
		}
		return nil
	case *time.Time:
		switch v := val.(type) {
		case time.Time:
			*d = v
		case *time.Time:
			if v == nil {
				return errors.New("expected non-nil *time.Time value")
			}
			*d = *v
		default:
			return fmt.Errorf("expected time value, got %T", val)
		}
		return nil
	case **time.Time:
		if val == nil {
			*d = nil
			return nil
		}
		switch v := val.(type) {
		case time.Time:
			cp := v
			*d = &cp
		case *time.Time:
			*d = v
		default:
			return fmt.Errorf("expected time/*time value, got %T", val)
		}
		return nil
	default:
		return fmt.Errorf("unsupported scan destination %T", dest)
	}
}

func TestNewStore(t *testing.T) {
	pool := new(pgxpool.Pool)
	storeWithPool := New(pool)
	assert.Same(t, pool, storeWithPool.DB())
	assert.Same(t, pool, storeWithPool.db)

	storeWithoutPool := New(nil)
	assert.Nil(t, storeWithoutPool.DB())
	assert.Nil(t, storeWithoutPool.db)
}

func TestCreateOperator(t *testing.T) {
	op := &Operator{
		ID:         uuid.New(),
		IdentityID: uuid.New(),
		Role:       "admin",
		Permissions: map[string]interface{}{
			"operators.read": true,
		},
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}

	t.Run("success", func(t *testing.T) {
		called := false
		s := &Store{
			db: &fakeDB{
				execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
					called = true
					require.Len(t, args, 6)
					var perms map[string]interface{}
					require.NoError(t, json.Unmarshal(args[3].([]byte), &perms))
					assert.Equal(t, true, perms["operators.read"])
					return pgconn.NewCommandTag("INSERT 1"), nil
				},
			},
		}

		err := s.CreateOperator(context.Background(), op)
		require.NoError(t, err)
		assert.True(t, called)
	})

	t.Run("duplicate operator", func(t *testing.T) {
		s := &Store{
			db: &fakeDB{
				execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
					return pgconn.CommandTag{}, errors.New("duplicate key value violates unique constraint")
				},
			},
		}

		err := s.CreateOperator(context.Background(), op)
		require.ErrorIs(t, err, ErrDuplicateOperator)
	})

	t.Run("marshal error", func(t *testing.T) {
		opBad := *op
		opBad.Permissions = map[string]interface{}{"bad": func() {}}

		s := &Store{db: &fakeDB{}}
		err := s.CreateOperator(context.Background(), &opBad)
		require.Error(t, err)
	})
}

func TestGetOperator(t *testing.T) {
	opID := uuid.New()
	identityID := uuid.New()
	now := time.Now().UTC()

	t.Run("success", func(t *testing.T) {
		s := &Store{
			db: &fakeDB{
				queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
					return fakeRow{
						vals: []any{
							opID,
							identityID,
							"admin",
							[]byte(`{"operators.write":true}`),
							now,
							now,
						},
					}
				},
			},
		}

		op, err := s.GetOperator(context.Background(), opID)
		require.NoError(t, err)
		assert.Equal(t, opID, op.ID)
		assert.Equal(t, identityID, op.IdentityID)
		assert.Equal(t, true, op.Permissions["operators.write"])
	})

	t.Run("invalid permissions json falls back", func(t *testing.T) {
		s := &Store{
			db: &fakeDB{
				queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
					return fakeRow{
						vals: []any{opID, identityID, "admin", []byte("{"), now, now},
					}
				},
			},
		}

		op, err := s.GetOperator(context.Background(), opID)
		require.NoError(t, err)
		require.NotNil(t, op.Permissions)
		assert.Empty(t, op.Permissions)
	})

	t.Run("not found", func(t *testing.T) {
		s := &Store{
			db: &fakeDB{
				queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
					return fakeRow{err: pgx.ErrNoRows}
				},
			},
		}
		_, err := s.GetOperator(context.Background(), opID)
		require.ErrorIs(t, err, ErrOperatorNotFound)
	})
}

func TestGetOperatorByIdentityIDNotFound(t *testing.T) {
	s := &Store{
		db: &fakeDB{
			queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
				return fakeRow{err: pgx.ErrNoRows}
			},
		},
	}

	_, err := s.GetOperatorByIdentityID(context.Background(), uuid.New())
	require.ErrorIs(t, err, ErrOperatorNotFound)
}

func TestAuthenticateOperatorByEmail(t *testing.T) {
	now := time.Now().UTC()
	opID := uuid.New()
	identityID := uuid.New()
	hash, err := bcrypt.GenerateFromPassword([]byte("super-secret"), bcrypt.MinCost)
	require.NoError(t, err)

	t.Run("invalid input", func(t *testing.T) {
		s := &Store{db: &fakeDB{}}
		_, err := s.AuthenticateOperatorByEmail(context.Background(), " ", "pwd")
		require.ErrorIs(t, err, ErrInvalidCredentials)

		_, err = s.AuthenticateOperatorByEmail(context.Background(), "admin@example.com", "")
		require.ErrorIs(t, err, ErrInvalidCredentials)
	})

	t.Run("unknown user", func(t *testing.T) {
		s := &Store{
			db: &fakeDB{
				queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
					return fakeRow{err: pgx.ErrNoRows}
				},
			},
		}
		_, err := s.AuthenticateOperatorByEmail(context.Background(), "admin@example.com", "pwd")
		require.ErrorIs(t, err, ErrInvalidCredentials)
	})

	t.Run("wrong password", func(t *testing.T) {
		s := &Store{
			db: &fakeDB{
				queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
					return fakeRow{
						vals: []any{
							opID,
							identityID,
							"admin",
							[]byte(`{"operators.read":true}`),
							now,
							now,
							string(hash),
						},
					}
				},
			},
		}
		_, err := s.AuthenticateOperatorByEmail(context.Background(), "admin@example.com", "wrong")
		require.ErrorIs(t, err, ErrInvalidCredentials)
	})

	t.Run("success normalizes email and handles invalid json", func(t *testing.T) {
		var gotEmail any
		s := &Store{
			db: &fakeDB{
				queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
					gotEmail = args[0]
					return fakeRow{
						vals: []any{
							opID,
							identityID,
							"admin",
							[]byte("{"),
							now,
							now,
							string(hash),
						},
					}
				},
			},
		}

		op, err := s.AuthenticateOperatorByEmail(context.Background(), "  ADMIN@Example.com ", "super-secret")
		require.NoError(t, err)
		assert.Equal(t, "admin@example.com", gotEmail)
		assert.NotNil(t, op.Permissions)
		assert.Empty(t, op.Permissions)
	})
}

func TestGetIdentityProfile(t *testing.T) {
	identityID := uuid.New()
	lastLogin := time.Now().UTC()

	t.Run("success", func(t *testing.T) {
		s := &Store{
			db: &fakeDB{
				queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
					return fakeRow{
						vals: []any{"user@example.com", "User", "active", lastLogin},
					}
				},
			},
		}

		profile, err := s.GetIdentityProfile(context.Background(), identityID)
		require.NoError(t, err)
		assert.Equal(t, "user@example.com", profile.Email)
		assert.Equal(t, "User", profile.Name)
		assert.Equal(t, "active", profile.State)
		require.NotNil(t, profile.LastLoginAt)
		assert.Equal(t, lastLogin, *profile.LastLoginAt)
	})

	t.Run("identity not found", func(t *testing.T) {
		s := &Store{
			db: &fakeDB{
				queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
					return fakeRow{err: pgx.ErrNoRows}
				},
			},
		}
		_, err := s.GetIdentityProfile(context.Background(), identityID)
		require.ErrorIs(t, err, ErrIdentityNotFound)
	})
}

func TestUpdateOperator(t *testing.T) {
	op := &Operator{
		ID:         uuid.New(),
		IdentityID: uuid.New(),
		Role:       "admin",
		Permissions: map[string]interface{}{
			"operators.read": true,
		},
	}

	t.Run("marshal error", func(t *testing.T) {
		opBad := *op
		opBad.Permissions = map[string]interface{}{"bad": func() {}}
		s := &Store{db: &fakeDB{}}

		err := s.UpdateOperator(context.Background(), &opBad)
		require.Error(t, err)
	})

	t.Run("exec error", func(t *testing.T) {
		s := &Store{
			db: &fakeDB{
				execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
					return pgconn.CommandTag{}, errors.New("db down")
				},
			},
		}

		err := s.UpdateOperator(context.Background(), op)
		require.EqualError(t, err, "db down")
	})

	t.Run("not found", func(t *testing.T) {
		s := &Store{
			db: &fakeDB{
				execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
					return pgconn.NewCommandTag("UPDATE 0"), nil
				},
			},
		}
		err := s.UpdateOperator(context.Background(), op)
		require.ErrorIs(t, err, ErrOperatorNotFound)
	})

	t.Run("success", func(t *testing.T) {
		s := &Store{
			db: &fakeDB{
				execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
					return pgconn.NewCommandTag("UPDATE 1"), nil
				},
			},
		}
		err := s.UpdateOperator(context.Background(), op)
		require.NoError(t, err)
	})
}

func TestDeleteOperator(t *testing.T) {
	opID := uuid.New()

	t.Run("not found", func(t *testing.T) {
		s := &Store{
			db: &fakeDB{
				execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
					return pgconn.NewCommandTag("DELETE 0"), nil
				},
			},
		}
		err := s.DeleteOperator(context.Background(), opID)
		require.ErrorIs(t, err, ErrOperatorNotFound)
	})

	t.Run("success", func(t *testing.T) {
		s := &Store{
			db: &fakeDB{
				execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
					return pgconn.NewCommandTag("DELETE 1"), nil
				},
			},
		}
		err := s.DeleteOperator(context.Background(), opID)
		require.NoError(t, err)
	})
}

func TestCountOperatorsByRole(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		s := &Store{
			db: &fakeDB{
				queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
					assert.Equal(t, "SELECT COUNT(*) FROM adm_operators WHERE role = $1", sql)
					require.Len(t, args, 1)
					assert.Equal(t, "admin", args[0])
					return fakeRow{vals: []any{int64(3)}}
				},
			},
		}

		count, err := s.CountOperatorsByRole(context.Background(), "admin")
		require.NoError(t, err)
		assert.Equal(t, int64(3), count)
	})

	t.Run("query error", func(t *testing.T) {
		s := &Store{
			db: &fakeDB{
				queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
					return fakeRow{err: errors.New("count failed")}
				},
			},
		}

		count, err := s.CountOperatorsByRole(context.Background(), "admin")
		require.EqualError(t, err, "count failed")
		assert.Equal(t, int64(0), count)
	})
}

func TestListOperators(t *testing.T) {
	opID := uuid.New()
	identityID := uuid.New()
	now := time.Now().UTC()

	t.Run("success with defaults", func(t *testing.T) {
		var listArgs []any
		s := &Store{
			db: &fakeDB{
				queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
					return fakeRow{vals: []any{int64(1)}}
				},
				queryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
					listArgs = args
					assert.Contains(t, sql, "ORDER BY created_at DESC")
					return &fakeRows{
						data: [][]any{
							{opID, identityID, "admin", []byte("{"), now, now},
						},
					}, nil
				},
			},
		}

		items, total, err := s.ListOperators(context.Background(), ListOptions{})
		require.NoError(t, err)
		assert.Equal(t, int64(1), total)
		require.Len(t, items, 1)
		assert.Equal(t, 10, listArgs[0])
		assert.Equal(t, 0, listArgs[1])
		assert.NotNil(t, items[0].Permissions)
		assert.Empty(t, items[0].Permissions)
	})

	t.Run("count error", func(t *testing.T) {
		s := &Store{
			db: &fakeDB{
				queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
					return fakeRow{err: errors.New("count failed")}
				},
			},
		}
		_, _, err := s.ListOperators(context.Background(), ListOptions{})
		require.EqualError(t, err, "count failed")
	})

	t.Run("list query error", func(t *testing.T) {
		s := &Store{
			db: &fakeDB{
				queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
					return fakeRow{vals: []any{int64(1)}}
				},
				queryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
					return nil, errors.New("list failed")
				},
			},
		}
		_, _, err := s.ListOperators(context.Background(), ListOptions{})
		require.EqualError(t, err, "list failed")
	})
}

func TestRoleStoreMethods(t *testing.T) {
	role := &Role{
		ID:          uuid.New(),
		Name:        "admin",
		Description: "Administrator",
		Permissions: []string{"identities.read"},
		IsSystem:    false,
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
	}

	t.Run("create role duplicate", func(t *testing.T) {
		s := &Store{
			db: &fakeDB{
				execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
					return pgconn.CommandTag{}, errors.New("23505 duplicate")
				},
			},
		}
		err := s.CreateRole(context.Background(), role)
		require.ErrorIs(t, err, ErrDuplicateRole)
	})

	t.Run("get role success and fallback perms", func(t *testing.T) {
		s := &Store{
			db: &fakeDB{
				queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
					return fakeRow{
						vals: []any{
							role.ID,
							role.Name,
							role.Description,
							[]byte("{"),
							false,
							role.CreatedAt,
							role.UpdatedAt,
						},
					}
				},
			},
		}
		got, err := s.GetRole(context.Background(), role.ID)
		require.NoError(t, err)
		assert.Equal(t, []string{}, got.Permissions)
	})

	t.Run("get role by name not found", func(t *testing.T) {
		s := &Store{
			db: &fakeDB{
				queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
					return fakeRow{err: pgx.ErrNoRows}
				},
			},
		}
		_, err := s.GetRoleByName(context.Background(), "missing")
		require.ErrorIs(t, err, ErrRoleNotFound)
	})

	t.Run("update role blocked for system role", func(t *testing.T) {
		s := &Store{
			db: &fakeDB{
				queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
					return fakeRow{vals: []any{true}}
				},
			},
		}
		err := s.UpdateRole(context.Background(), role)
		require.ErrorIs(t, err, ErrSystemRole)
	})

	t.Run("update role duplicate", func(t *testing.T) {
		s := &Store{
			db: &fakeDB{
				queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
					return fakeRow{vals: []any{false}}
				},
				execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
					return pgconn.CommandTag{}, errors.New("duplicate key")
				},
			},
		}
		err := s.UpdateRole(context.Background(), role)
		require.ErrorIs(t, err, ErrDuplicateRole)
	})

	t.Run("update role not found", func(t *testing.T) {
		s := &Store{
			db: &fakeDB{
				queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
					return fakeRow{vals: []any{false}}
				},
				execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
					return pgconn.NewCommandTag("UPDATE 0"), nil
				},
			},
		}
		err := s.UpdateRole(context.Background(), role)
		require.ErrorIs(t, err, ErrRoleNotFound)
	})

	t.Run("update role success", func(t *testing.T) {
		s := &Store{
			db: &fakeDB{
				queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
					return fakeRow{vals: []any{false}}
				},
				execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
					return pgconn.NewCommandTag("UPDATE 1"), nil
				},
			},
		}
		err := s.UpdateRole(context.Background(), role)
		require.NoError(t, err)
	})

	t.Run("delete role system", func(t *testing.T) {
		s := &Store{
			db: &fakeDB{
				queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
					return fakeRow{vals: []any{true}}
				},
			},
		}
		err := s.DeleteRole(context.Background(), role.ID)
		require.ErrorIs(t, err, ErrSystemRole)
	})

	t.Run("delete role not found", func(t *testing.T) {
		s := &Store{
			db: &fakeDB{
				queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
					return fakeRow{vals: []any{false}}
				},
				execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
					return pgconn.NewCommandTag("DELETE 0"), nil
				},
			},
		}
		err := s.DeleteRole(context.Background(), role.ID)
		require.ErrorIs(t, err, ErrRoleNotFound)
	})

	t.Run("list roles success", func(t *testing.T) {
		s := &Store{
			db: &fakeDB{
				queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
					return fakeRow{vals: []any{int64(1)}}
				},
				queryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
					assert.Contains(t, sql, "ORDER BY is_system DESC, name ASC")
					return &fakeRows{
						data: [][]any{
							{
								role.ID,
								role.Name,
								role.Description,
								[]byte(`["identities.read"]`),
								false,
								role.CreatedAt,
								role.UpdatedAt,
							},
						},
					}, nil
				},
			},
		}

		items, total, err := s.ListRoles(context.Background(), ListOptions{})
		require.NoError(t, err)
		assert.Equal(t, int64(1), total)
		require.Len(t, items, 1)
		assert.Equal(t, []string{"identities.read"}, items[0].Permissions)
	})
}

func TestLogAction(t *testing.T) {
	entry := &AuditLogEntry{
		ID:           uuid.New(),
		Action:       "operator.created",
		ResourceType: "operator",
		ResourceID:   uuid.New().String(),
		Details: map[string]interface{}{
			"role": "admin",
		},
		IPAddress: "127.0.0.1",
		CreatedAt: time.Now().UTC(),
	}

	t.Run("success", func(t *testing.T) {
		called := false
		s := &Store{
			db: &fakeDB{
				execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
					called = true
					return pgconn.NewCommandTag("INSERT 1"), nil
				},
			},
		}
		err := s.LogAction(context.Background(), entry)
		require.NoError(t, err)
		assert.True(t, called)
	})

	t.Run("marshal error", func(t *testing.T) {
		bad := *entry
		bad.Details = map[string]interface{}{"bad": func() {}}
		s := &Store{db: &fakeDB{}}
		err := s.LogAction(context.Background(), &bad)
		require.Error(t, err)
	})
}

func TestListAuditLogs(t *testing.T) {
	operatorID := uuid.New()
	now := time.Now().UTC()
	start := now.Add(-time.Hour)
	end := now.Add(time.Hour)
	var countQuery string
	var listQuery string

	s := &Store{
		db: &fakeDB{
			queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
				countQuery = sql
				require.Len(t, args, 6)
				return fakeRow{vals: []any{int64(1)}}
			},
			queryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
				listQuery = sql
				require.Len(t, args, 8)
				assert.Equal(t, 50, args[6])
				assert.Equal(t, 0, args[7])
				return &fakeRows{
					data: [][]any{
						{
							uuid.New(),
							operatorID,
							"operator.created",
							"operator",
							uuid.New().String(),
							[]byte("{"),
							"127.0.0.1",
							now,
						},
					},
				}, nil
			},
		},
	}

	filter := AuditFilter{
		OperatorID:   &operatorID,
		Action:       "operator.created",
		ResourceType: "operator",
		ResourceID:   "r1",
		StartTime:    &start,
		EndTime:      &end,
	}

	items, total, err := s.ListAuditLogs(context.Background(), filter, ListOptions{})
	require.NoError(t, err)
	assert.Equal(t, int64(1), total)
	require.Len(t, items, 1)
	assert.NotNil(t, items[0].Details)
	assert.Empty(t, items[0].Details)
	assert.Contains(t, countQuery, "operator_id = $1")
	assert.Contains(t, countQuery, "created_at <= $6")
	assert.Contains(t, listQuery, "ORDER BY created_at DESC")
}

func TestListAuditLogsCountError(t *testing.T) {
	s := &Store{
		db: &fakeDB{
			queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
				return fakeRow{err: errors.New("count failed")}
			},
		},
	}
	_, _, err := s.ListAuditLogs(context.Background(), AuditFilter{}, ListOptions{})
	require.EqualError(t, err, "count failed")
}

func TestAPIKeyStoreMethods(t *testing.T) {
	operatorID := uuid.New()
	key := &APIKey{
		ID:         uuid.New(),
		OperatorID: operatorID,
		Name:       "automation",
		KeyHash:    HashAPIKeyToken("aegion_example"),
		KeyPrefix:  "123456789012",
		Permissions: map[string]interface{}{
			"identities.read": true,
		},
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}

	t.Run("create api key marshal error", func(t *testing.T) {
		bad := *key
		bad.Permissions = map[string]interface{}{"bad": func() {}}

		s := &Store{db: &fakeDB{}}
		err := s.CreateAPIKey(context.Background(), &bad)
		require.Error(t, err)
	})

	t.Run("create and fetch api key", func(t *testing.T) {
		now := time.Now().UTC()
		s := &Store{
			db: &fakeDB{
				execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
					return pgconn.NewCommandTag("INSERT 1"), nil
				},
				queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
					return fakeRow{
						vals: []any{
							key.ID,
							key.OperatorID,
							key.Name,
							key.KeyHash,
							key.KeyPrefix,
							[]byte("{"),
							now,
							(*time.Time)(nil),
							key.CreatedAt,
							key.UpdatedAt,
						},
					}
				},
			},
		}

		err := s.CreateAPIKey(context.Background(), key)
		require.NoError(t, err)

		got, err := s.GetAPIKey(context.Background(), key.ID)
		require.NoError(t, err)
		assert.Equal(t, key.ID, got.ID)
		assert.NotNil(t, got.Permissions)
		assert.Empty(t, got.Permissions)
	})

	t.Run("get api key by prefix not found", func(t *testing.T) {
		s := &Store{
			db: &fakeDB{
				queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
					return fakeRow{err: pgx.ErrNoRows}
				},
			},
		}
		_, err := s.GetAPIKeyByPrefix(context.Background(), "missing")
		require.ErrorIs(t, err, ErrAPIKeyNotFound)
	})

	t.Run("update last used and delete not found", func(t *testing.T) {
		s := &Store{
			db: &fakeDB{
				execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
					if len(args) > 0 && args[0] == key.ID {
						if contains(sql, "DELETE") {
							return pgconn.NewCommandTag("DELETE 0"), nil
						}
						return pgconn.NewCommandTag("UPDATE 1"), nil
					}
					return pgconn.CommandTag{}, errors.New("unexpected args")
				},
			},
		}

		err := s.UpdateAPIKeyLastUsed(context.Background(), key.ID)
		require.NoError(t, err)

		err = s.DeleteAPIKey(context.Background(), key.ID)
		require.ErrorIs(t, err, ErrAPIKeyNotFound)
	})

	t.Run("list api keys", func(t *testing.T) {
		now := time.Now().UTC()
		s := &Store{
			db: &fakeDB{
				queryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
					return &fakeRows{
						data: [][]any{
							{
								key.ID,
								key.OperatorID,
								key.Name,
								key.KeyHash,
								key.KeyPrefix,
								[]byte(`{"identities.read":true}`),
								now,
								(*time.Time)(nil),
								key.CreatedAt,
								key.UpdatedAt,
							},
						},
					}, nil
				},
			},
		}

		keys, err := s.ListAPIKeysByOperator(context.Background(), operatorID)
		require.NoError(t, err)
		require.Len(t, keys, 1)
		assert.Equal(t, true, keys[0].Permissions["identities.read"])
	})
}

func TestHashAndValidateAPIKeyToken(t *testing.T) {
	token := "aegion_examplekey01_not_a_real_token"
	hash := HashAPIKeyToken(token)

	assert.NotEmpty(t, hash)
	assert.True(t, ValidateAPIKeyToken(token, hash))
	assert.False(t, ValidateAPIKeyToken("wrong", hash))
	assert.False(t, ValidateAPIKeyToken("", hash))
	assert.False(t, ValidateAPIKeyToken(token, ""))
	assert.False(t, ValidateAPIKeyToken(token, hash+"x"))
}

func TestHelpers(t *testing.T) {
	assert.True(t, isDuplicateKeyError(errors.New("duplicate key value")))
	assert.True(t, isDuplicateKeyError(errors.New("sqlstate 23505")))
	assert.False(t, isDuplicateKeyError(nil))
	assert.False(t, isDuplicateKeyError(errors.New("other error")))

	assert.True(t, contains("abcdef", "bcd"))
	assert.False(t, contains("abcdef", "gh"))
	assert.True(t, contains("abc", ""))

	assert.Equal(t, "0", itoa(0))
	assert.Equal(t, "9", itoa(9))
	assert.Equal(t, "10", itoa(10))
	assert.Equal(t, "12345", itoa(12345))

	allowedSorts := map[string]string{
		"created_at desc": "created_at DESC",
		"created_at asc":  "created_at ASC",
	}
	assert.Equal(t, "created_at DESC", safeSortExpression("created_at desc", "fallback", allowedSorts))
	assert.Equal(t, "fallback", safeSortExpression("bogus", "fallback", allowedSorts))
	assert.Equal(t, "fallback", safeSortExpression("   ", "fallback", allowedSorts))
}

func TestGetOperatorByIdentityIDErrors(t *testing.T) {
	t.Run("database error", func(t *testing.T) {
		s := &Store{
			db: &fakeDB{
				queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
					return fakeRow{err: errors.New("connection lost")}
				},
			},
		}
		_, err := s.GetOperatorByIdentityID(context.Background(), uuid.New())
		require.EqualError(t, err, "connection lost")
	})

	t.Run("invalid permissions json fallback", func(t *testing.T) {
		id := uuid.New()
		identityID := uuid.New()
		now := time.Now()
		s := &Store{
			db: &fakeDB{
				queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
					return fakeRow{
						vals: []any{id, identityID, "admin", []byte("{invalid"), now, now},
					}
				},
			},
		}
		op, err := s.GetOperatorByIdentityID(context.Background(), identityID)
		require.NoError(t, err)
		require.NotNil(t, op)
		assert.Equal(t, id, op.ID)
		assert.NotNil(t, op.Permissions)
		assert.Empty(t, op.Permissions)
	})
}

func TestCreateRoleErrors(t *testing.T) {
	t.Run("database error", func(t *testing.T) {
		s := &Store{
			db: &fakeDB{
				execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
					return pgconn.CommandTag{}, errors.New("database offline")
				},
			},
		}
		role := &Role{
			ID:          uuid.New(),
			Name:        "test",
			Description: "Test role",
			Permissions: []string{"perm1"},
			IsSystem:    false,
			CreatedAt:   time.Now(),
			UpdatedAt:   time.Now(),
		}
		err := s.CreateRole(context.Background(), role)
		require.EqualError(t, err, "database offline")
	})

	t.Run("success", func(t *testing.T) {
		s := &Store{
			db: &fakeDB{
				execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
					return pgconn.NewCommandTag("INSERT 1"), nil
				},
			},
		}
		role := &Role{
			ID:          uuid.New(),
			Name:        "test",
			Permissions: []string{"perm1"},
			CreatedAt:   time.Now(),
			UpdatedAt:   time.Now(),
		}
		err := s.CreateRole(context.Background(), role)
		require.NoError(t, err)
	})
}

func TestGetRoleByNameErrors(t *testing.T) {
	t.Run("database error", func(t *testing.T) {
		s := &Store{
			db: &fakeDB{
				queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
					return fakeRow{err: errors.New("db timeout")}
				},
			},
		}
		_, err := s.GetRoleByName(context.Background(), "admin")
		require.EqualError(t, err, "db timeout")
	})

	t.Run("invalid permissions json fallback", func(t *testing.T) {
		id := uuid.New()
		now := time.Now()
		s := &Store{
			db: &fakeDB{
				queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
					return fakeRow{
						vals: []any{id, "admin", "Admin role", []byte("corrupted"), false, now, now},
					}
				},
			},
		}
		role, err := s.GetRoleByName(context.Background(), "admin")
		require.NoError(t, err)
		require.NotNil(t, role)
		assert.Equal(t, "admin", role.Name)
		assert.Empty(t, role.Permissions)
	})
}

func TestDeleteRoleErrors(t *testing.T) {
	t.Run("database error on initial query", func(t *testing.T) {
		s := &Store{
			db: &fakeDB{
				queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
					return fakeRow{err: errors.New("db connection failed")}
				},
			},
		}
		err := s.DeleteRole(context.Background(), uuid.New())
		require.EqualError(t, err, "db connection failed")
	})

	t.Run("role not found on initial query", func(t *testing.T) {
		s := &Store{
			db: &fakeDB{
				queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
					return fakeRow{err: pgx.ErrNoRows}
				},
			},
		}
		err := s.DeleteRole(context.Background(), uuid.New())
		require.ErrorIs(t, err, ErrRoleNotFound)
	})

	t.Run("system role cannot be deleted", func(t *testing.T) {
		s := &Store{
			db: &fakeDB{
				queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
					return fakeRow{vals: []any{true}} // is_system = true
				},
			},
		}
		err := s.DeleteRole(context.Background(), uuid.New())
		require.ErrorIs(t, err, ErrSystemRole)
	})

	t.Run("exec error", func(t *testing.T) {
		s := &Store{
			db: &fakeDB{
				queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
					return fakeRow{vals: []any{false}} // is_system = false
				},
				execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
					return pgconn.CommandTag{}, errors.New("exec failed")
				},
			},
		}
		err := s.DeleteRole(context.Background(), uuid.New())
		require.EqualError(t, err, "exec failed")
	})

	t.Run("success", func(t *testing.T) {
		s := &Store{
			db: &fakeDB{
				queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
					return fakeRow{vals: []any{false}} // is_system = false
				},
				execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
					return pgconn.NewCommandTag("DELETE 1"), nil
				},
			},
		}
		err := s.DeleteRole(context.Background(), uuid.New())
		require.NoError(t, err)
	})
}

func TestGetAPIKeyErrors(t *testing.T) {
	t.Run("database error", func(t *testing.T) {
		s := &Store{
			db: &fakeDB{
				queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
					return fakeRow{err: errors.New("query failed")}
				},
			},
		}
		_, err := s.GetAPIKey(context.Background(), uuid.New())
		require.EqualError(t, err, "query failed")
	})

	t.Run("invalid permissions json fallback", func(t *testing.T) {
		id := uuid.New()
		opID := uuid.New()
		now := time.Now()
		s := &Store{
			db: &fakeDB{
				queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
					// GetAPIKey scans: id, operator_id, name, key_hash, key_prefix, permissions, last_used_at, expires_at, created_at, updated_at
					return fakeRow{
						vals: []any{id, opID, "test", "hash123", "prefix_", []byte("{malformed"), &now, &now, now, now},
					}
				},
			},
		}
		key, err := s.GetAPIKey(context.Background(), id)
		require.NoError(t, err)
		require.NotNil(t, key)
		assert.Equal(t, "test", key.Name)
		assert.Empty(t, key.Permissions)
	})
}

func TestGetAPIKeyByPrefixErrors(t *testing.T) {
	t.Run("database error", func(t *testing.T) {
		s := &Store{
			db: &fakeDB{
				queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
					return fakeRow{err: errors.New("connection timeout")}
				},
			},
		}
		_, err := s.GetAPIKeyByPrefix(context.Background(), "prefix_")
		require.EqualError(t, err, "connection timeout")
	})

	t.Run("invalid permissions json fallback", func(t *testing.T) {
		id := uuid.New()
		opID := uuid.New()
		now := time.Now()
		s := &Store{
			db: &fakeDB{
				queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
					// Same 10 fields as GetAPIKey
					return fakeRow{
						vals: []any{id, opID, "test", "hash123", "prefix_", []byte("not-json"), &now, &now, now, now},
					}
				},
			},
		}
		key, err := s.GetAPIKeyByPrefix(context.Background(), "prefix_")
		require.NoError(t, err)
		require.NotNil(t, key)
		assert.Empty(t, key.Permissions)
	})
}

func TestStore_AdditionalErrorBranches(t *testing.T) {
	now := time.Now().UTC()
	ctx := context.Background()

	t.Run("create/get/auth/profile/delete operator generic errors", func(t *testing.T) {
		op := &Operator{
			ID:          uuid.New(),
			IdentityID:  uuid.New(),
			Role:        "admin",
			Permissions: map[string]interface{}{"manage": true},
			CreatedAt:   now,
			UpdatedAt:   now,
		}

		s := &Store{
			db: &fakeDB{
				execFn: func(context.Context, string, ...any) (pgconn.CommandTag, error) {
					return pgconn.CommandTag{}, errors.New("write failed")
				},
				queryRowFn: func(context.Context, string, ...any) pgx.Row {
					return fakeRow{err: errors.New("query failed")}
				},
			},
		}

		require.EqualError(t, s.CreateOperator(ctx, op), "write failed")
		_, err := s.GetOperator(ctx, op.ID)
		require.EqualError(t, err, "query failed")
		_, err = s.AuthenticateOperatorByEmail(ctx, "admin@example.com", "password")
		require.EqualError(t, err, "query failed")
		_, err = s.GetIdentityProfile(ctx, op.IdentityID)
		require.EqualError(t, err, "query failed")
		require.EqualError(t, s.DeleteOperator(ctx, op.ID), "write failed")
	})

	t.Run("list operators scan error", func(t *testing.T) {
		s := &Store{
			db: &fakeDB{
				queryRowFn: func(context.Context, string, ...any) pgx.Row {
					return fakeRow{vals: []any{int64(1)}}
				},
				queryFn: func(context.Context, string, ...any) (pgx.Rows, error) {
					return &fakeRows{data: [][]any{{"bad-row"}}}, nil
				},
			},
		}

		_, _, err := s.ListOperators(ctx, ListOptions{Limit: 1})
		require.Error(t, err)
	})

	t.Run("get role no rows and generic query error", func(t *testing.T) {
		notFound := &Store{db: &fakeDB{
			queryRowFn: func(context.Context, string, ...any) pgx.Row {
				return fakeRow{err: pgx.ErrNoRows}
			},
		}}
		_, err := notFound.GetRole(ctx, uuid.New())
		require.ErrorIs(t, err, ErrRoleNotFound)

		dbErr := &Store{db: &fakeDB{
			queryRowFn: func(context.Context, string, ...any) pgx.Row {
				return fakeRow{err: errors.New("role lookup failed")}
			},
		}}
		_, err = dbErr.GetRole(ctx, uuid.New())
		require.EqualError(t, err, "role lookup failed")
	})

	t.Run("update role extra errors", func(t *testing.T) {
		role := &Role{
			ID:          uuid.New(),
			Name:        "custom",
			Description: "Custom role",
			Permissions: []string{"read"},
		}

		notFound := &Store{db: &fakeDB{
			queryRowFn: func(context.Context, string, ...any) pgx.Row {
				return fakeRow{err: pgx.ErrNoRows}
			},
		}}
		require.ErrorIs(t, notFound.UpdateRole(ctx, role), ErrRoleNotFound)

		readErr := &Store{db: &fakeDB{
			queryRowFn: func(context.Context, string, ...any) pgx.Row {
				return fakeRow{err: errors.New("query failed")}
			},
		}}
		require.EqualError(t, readErr.UpdateRole(ctx, role), "query failed")

		execErr := &Store{db: &fakeDB{
			queryRowFn: func(context.Context, string, ...any) pgx.Row {
				return fakeRow{vals: []any{false}}
			},
			execFn: func(context.Context, string, ...any) (pgconn.CommandTag, error) {
				return pgconn.CommandTag{}, errors.New("update failed")
			},
		}}
		require.EqualError(t, execErr.UpdateRole(ctx, role), "update failed")

		zeroRows := &Store{db: &fakeDB{
			queryRowFn: func(context.Context, string, ...any) pgx.Row {
				return fakeRow{vals: []any{false}}
			},
			execFn: func(context.Context, string, ...any) (pgconn.CommandTag, error) {
				return pgconn.NewCommandTag("UPDATE 0"), nil
			},
		}}
		require.ErrorIs(t, zeroRows.UpdateRole(ctx, role), ErrRoleNotFound)
	})

	t.Run("list roles count/query/scan/error fallback branches", func(t *testing.T) {
		countErrStore := &Store{db: &fakeDB{
			queryRowFn: func(context.Context, string, ...any) pgx.Row {
				return fakeRow{err: errors.New("count failed")}
			},
		}}
		_, _, err := countErrStore.ListRoles(ctx, ListOptions{})
		require.EqualError(t, err, "count failed")

		queryErrStore := &Store{db: &fakeDB{
			queryRowFn: func(context.Context, string, ...any) pgx.Row {
				return fakeRow{vals: []any{int64(1)}}
			},
			queryFn: func(context.Context, string, ...any) (pgx.Rows, error) {
				return nil, errors.New("query failed")
			},
		}}
		_, _, err = queryErrStore.ListRoles(ctx, ListOptions{})
		require.EqualError(t, err, "query failed")

		scanErrStore := &Store{db: &fakeDB{
			queryRowFn: func(context.Context, string, ...any) pgx.Row {
				return fakeRow{vals: []any{int64(1)}}
			},
			queryFn: func(context.Context, string, ...any) (pgx.Rows, error) {
				return &fakeRows{data: [][]any{{"bad-row"}}}, nil
			},
		}}
		_, _, err = scanErrStore.ListRoles(ctx, ListOptions{})
		require.Error(t, err)

		id := uuid.New()
		unmarshalFallbackStore := &Store{db: &fakeDB{
			queryRowFn: func(context.Context, string, ...any) pgx.Row {
				return fakeRow{vals: []any{int64(1)}}
			},
			queryFn: func(context.Context, string, ...any) (pgx.Rows, error) {
				return &fakeRows{data: [][]any{
					{id, "admin", "Admin", []byte("{bad-json"), true, now, now},
				}}, nil
			},
		}}
		roles, total, err := unmarshalFallbackStore.ListRoles(ctx, ListOptions{})
		require.NoError(t, err)
		require.Equal(t, int64(1), total)
		require.Len(t, roles, 1)
		assert.Empty(t, roles[0].Permissions)
	})

	t.Run("list audit logs query and scan errors", func(t *testing.T) {
		queryErrStore := &Store{db: &fakeDB{
			queryRowFn: func(context.Context, string, ...any) pgx.Row {
				return fakeRow{vals: []any{int64(0)}}
			},
			queryFn: func(context.Context, string, ...any) (pgx.Rows, error) {
				return nil, errors.New("query failed")
			},
		}}
		_, _, err := queryErrStore.ListAuditLogs(ctx, AuditFilter{}, ListOptions{})
		require.EqualError(t, err, "query failed")

		scanErrStore := &Store{db: &fakeDB{
			queryRowFn: func(context.Context, string, ...any) pgx.Row {
				return fakeRow{vals: []any{int64(1)}}
			},
			queryFn: func(context.Context, string, ...any) (pgx.Rows, error) {
				return &fakeRows{data: [][]any{{"bad-row"}}}, nil
			},
		}}
		_, _, err = scanErrStore.ListAuditLogs(ctx, AuditFilter{}, ListOptions{})
		require.Error(t, err)
	})

	t.Run("api key not found and scan error branches", func(t *testing.T) {
		notFoundStore := &Store{db: &fakeDB{
			queryRowFn: func(context.Context, string, ...any) pgx.Row {
				return fakeRow{err: pgx.ErrNoRows}
			},
		}}
		_, err := notFoundStore.GetAPIKey(ctx, uuid.New())
		require.ErrorIs(t, err, ErrAPIKeyNotFound)

		scanErrStore := &Store{db: &fakeDB{
			queryFn: func(context.Context, string, ...any) (pgx.Rows, error) {
				return &fakeRows{data: [][]any{{"bad-row"}}}, nil
			},
		}}
		_, err = scanErrStore.ListAPIKeysByOperator(ctx, uuid.New())
		require.Error(t, err)
	})
}

func TestDeleteAPIKeyErrors(t *testing.T) {
	t.Run("database error", func(t *testing.T) {
		s := &Store{
			db: &fakeDB{
				execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
					return pgconn.CommandTag{}, errors.New("delete failed")
				},
			},
		}
		err := s.DeleteAPIKey(context.Background(), uuid.New())
		require.EqualError(t, err, "delete failed")
	})

	t.Run("api key not found", func(t *testing.T) {
		s := &Store{
			db: &fakeDB{
				execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
					return pgconn.NewCommandTag("DELETE 0"), nil
				},
			},
		}
		err := s.DeleteAPIKey(context.Background(), uuid.New())
		require.ErrorIs(t, err, ErrAPIKeyNotFound)
	})

	t.Run("success", func(t *testing.T) {
		s := &Store{
			db: &fakeDB{
				execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
					return pgconn.NewCommandTag("DELETE 1"), nil
				},
			},
		}
		err := s.DeleteAPIKey(context.Background(), uuid.New())
		require.NoError(t, err)
	})
}

func TestListAPIKeysByOperatorErrors(t *testing.T) {
	t.Run("query error", func(t *testing.T) {
		s := &Store{
			db: &fakeDB{
				queryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
					return nil, errors.New("query failed")
				},
			},
		}
		_, err := s.ListAPIKeysByOperator(context.Background(), uuid.New())
		require.EqualError(t, err, "query failed")
	})

	t.Run("rows error during iteration", func(t *testing.T) {
		s := &Store{
			db: &fakeDB{
				queryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
					return &fakeRows{
						data: [][]any{},
						err:  errors.New("iteration failed"),
					}, nil
				},
			},
		}
		_, err := s.ListAPIKeysByOperator(context.Background(), uuid.New())
		require.EqualError(t, err, "iteration failed")
	})

	t.Run("invalid permissions json fallback", func(t *testing.T) {
		id := uuid.New()
		opID := uuid.New()
		now := time.Now()
		s := &Store{
			db: &fakeDB{
				queryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
					return &fakeRows{
						data: [][]any{
							// ListAPIKeysByOperator scans: id, operator_id, name, key_hash, key_prefix, permissions, last_used_at, expires_at, created_at, updated_at
							{id, opID, "test", "hash123", "prefix_", []byte("bad-json"), &now, &now, now, now},
						},
					}, nil
				},
			},
		}
		keys, err := s.ListAPIKeysByOperator(context.Background(), opID)
		require.NoError(t, err)
		require.Len(t, keys, 1)
		assert.Empty(t, keys[0].Permissions)
	})
}
