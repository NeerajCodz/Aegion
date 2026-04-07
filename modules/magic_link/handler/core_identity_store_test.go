package handler

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeIdentityRow struct {
	scanFn func(dest ...interface{}) error
}

func (r fakeIdentityRow) Scan(dest ...interface{}) error {
	if r.scanFn != nil {
		return r.scanFn(dest...)
	}
	return nil
}

type fakeIdentityDB struct {
	queryRowFn func(ctx context.Context, sql string, args ...interface{}) pgx.Row
	execFn     func(ctx context.Context, sql string, args ...interface{}) (pgconn.CommandTag, error)
}

func (f *fakeIdentityDB) QueryRow(ctx context.Context, sql string, args ...interface{}) pgx.Row {
	if f.queryRowFn != nil {
		return f.queryRowFn(ctx, sql, args...)
	}
	return fakeIdentityRow{}
}

func (f *fakeIdentityDB) Exec(ctx context.Context, sql string, args ...interface{}) (pgconn.CommandTag, error) {
	if f.execFn != nil {
		return f.execFn(ctx, sql, args...)
	}
	return pgconn.CommandTag{}, nil
}

func TestCoreIdentityStore_GetIdentityByEmail(t *testing.T) {
	t.Run("returns nil for empty input", func(t *testing.T) {
		store := NewCoreIdentityStore(&fakeIdentityDB{})
		identityID, err := store.GetIdentityByEmail(context.Background(), "   ")
		require.NoError(t, err)
		assert.Nil(t, identityID)
	})

	t.Run("returns identity when found", func(t *testing.T) {
		expectedID := uuid.New()
		store := NewCoreIdentityStore(&fakeIdentityDB{
			queryRowFn: func(_ context.Context, _ string, args ...interface{}) pgx.Row {
				assert.Equal(t, "user@example.com", args[0])
				return fakeIdentityRow{
					scanFn: func(dest ...interface{}) error {
						*(dest[0].(*uuid.UUID)) = expectedID
						return nil
					},
				}
			},
		})

		identityID, err := store.GetIdentityByEmail(context.Background(), "user@example.com")
		require.NoError(t, err)
		require.NotNil(t, identityID)
		assert.Equal(t, expectedID, *identityID)
	})

	t.Run("returns nil when missing", func(t *testing.T) {
		store := NewCoreIdentityStore(&fakeIdentityDB{
			queryRowFn: func(_ context.Context, _ string, _ ...interface{}) pgx.Row {
				return fakeIdentityRow{
					scanFn: func(dest ...interface{}) error {
						return pgx.ErrNoRows
					},
				}
			},
		})

		identityID, err := store.GetIdentityByEmail(context.Background(), "missing@example.com")
		require.NoError(t, err)
		assert.Nil(t, identityID)
	})

	t.Run("propagates query errors", func(t *testing.T) {
		store := NewCoreIdentityStore(&fakeIdentityDB{
			queryRowFn: func(_ context.Context, _ string, _ ...interface{}) pgx.Row {
				return fakeIdentityRow{
					scanFn: func(dest ...interface{}) error {
						return errors.New("query failed")
					},
				}
			},
		})

		identityID, err := store.GetIdentityByEmail(context.Background(), "user@example.com")
		assert.Nil(t, identityID)
		assert.EqualError(t, err, "query failed")
	})
}

func TestCoreIdentityStore_MarkEmailVerified(t *testing.T) {
	t.Run("no-op for empty email", func(t *testing.T) {
		called := false
		store := NewCoreIdentityStore(&fakeIdentityDB{
			execFn: func(_ context.Context, _ string, _ ...interface{}) (pgconn.CommandTag, error) {
				called = true
				return pgconn.CommandTag{}, nil
			},
		})

		err := store.MarkEmailVerified(context.Background(), uuid.New(), " ")
		require.NoError(t, err)
		assert.False(t, called)
	})

	t.Run("updates verification flag", func(t *testing.T) {
		identityID := uuid.New()
		store := NewCoreIdentityStore(&fakeIdentityDB{
			execFn: func(_ context.Context, _ string, args ...interface{}) (pgconn.CommandTag, error) {
				assert.Equal(t, identityID, args[0])
				assert.Equal(t, "user@example.com", args[1])
				return pgconn.CommandTag{}, nil
			},
		})

		err := store.MarkEmailVerified(context.Background(), identityID, "user@example.com")
		require.NoError(t, err)
	})

	t.Run("propagates update errors", func(t *testing.T) {
		store := NewCoreIdentityStore(&fakeIdentityDB{
			execFn: func(_ context.Context, _ string, _ ...interface{}) (pgconn.CommandTag, error) {
				return pgconn.CommandTag{}, errors.New("update failed")
			},
		})

		err := store.MarkEmailVerified(context.Background(), uuid.New(), "user@example.com")
		assert.EqualError(t, err, "update failed")
	})
}
