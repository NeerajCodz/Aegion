package store

import (
	"context"
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

func TestIcebergStorageStubBehavior(t *testing.T) {
	ctx := context.Background()
	storage, err := NewIcebergStorage("rest", "warehouse", "catalog", "")
	require.NoError(t, err)

	require.NoError(t, storage.Initialize(ctx))

	path, err := storage.Write(ctx, "events", []byte("payload"))
	require.NoError(t, err)
	assert.Contains(t, path, "events.data_")

	_, err = storage.Read(ctx, path)
	assert.ErrorIs(t, err, ErrFailed)

	items, err := storage.List(ctx, "events")
	require.NoError(t, err)
	assert.Empty(t, items)

	assert.NoError(t, storage.Delete(ctx, path))
	assert.NoError(t, storage.Health(ctx))
	assert.NoError(t, storage.Close(ctx))
}
