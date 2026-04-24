package store

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIcebergStorageValidation(t *testing.T) {
	_, err := NewIcebergStorage("", "warehouse", "catalog", "")
	assert.ErrorIs(t, err, ErrInvalidArg)

	_, err = NewIcebergStorage("nessie", "warehouse", "catalog", "")
	assert.ErrorIs(t, err, ErrInvalidArg)

	storage, err := NewIcebergStorage("rest", "warehouse", "catalog", "")
	require.NoError(t, err)
	assert.NotNil(t, storage)
}

func TestIcebergStorageReadWriteLifecycle(t *testing.T) {
	ctx := context.Background()
	warehouse := filepath.Join(t.TempDir(), "warehouse")
	storage, err := NewIcebergStorage("rest", warehouse, "catalog", "")
	require.NoError(t, err)

	require.NoError(t, storage.Initialize(ctx))

	path, err := storage.Write(ctx, "events", []byte("payload"))
	require.NoError(t, err)
	assert.Contains(t, path, "events/data_")

	data, err := storage.Read(ctx, path)
	require.NoError(t, err)
	assert.Equal(t, []byte("payload"), data)

	items, err := storage.List(ctx, "events")
	require.NoError(t, err)
	require.Len(t, items, 1)
	assert.Equal(t, path, items[0])

	assert.NoError(t, storage.Delete(ctx, path))
	_, err = storage.Read(ctx, path)
	assert.ErrorIs(t, err, ErrNotFound)
	assert.NoError(t, storage.Health(ctx))
	assert.NoError(t, storage.Close(ctx))
}

func TestIcebergStorageRejectsTraversal(t *testing.T) {
	ctx := context.Background()
	storage, err := NewIcebergStorage("rest", filepath.Join(t.TempDir(), "warehouse"), "catalog", "")
	require.NoError(t, err)
	require.NoError(t, storage.Initialize(ctx))

	_, err = storage.Read(ctx, "../escape")
	assert.ErrorIs(t, err, ErrInvalidArg)

	err = storage.Delete(ctx, "../escape")
	assert.ErrorIs(t, err, ErrInvalidArg)
}
