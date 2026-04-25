package store

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLocalStorageReadWriteDelete(t *testing.T) {
	ctx := context.Background()
	basePath := t.TempDir()
	storage := NewLocalStorage(basePath)

	require.NoError(t, storage.Initialize(ctx))

	path, err := storage.Write(ctx, "events", []byte("hello"))
	require.NoError(t, err)
	assert.Contains(t, path, "events")

	data, err := storage.Read(ctx, path)
	require.NoError(t, err)
	assert.Equal(t, []byte("hello"), data)

	items, err := storage.List(ctx, "events")
	require.NoError(t, err)
	assert.Len(t, items, 1)

	require.NoError(t, storage.Delete(ctx, path))
	_, err = storage.Read(ctx, path)
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestLocalStorageRejectsTraversal(t *testing.T) {
	storage := NewLocalStorage(t.TempDir())
	ctx := context.Background()

	_, err := storage.Read(ctx, "../secret")
	assert.ErrorIs(t, err, ErrInvalidArg)

	err = storage.Delete(ctx, "../secret")
	assert.ErrorIs(t, err, ErrInvalidArg)
}

func TestS3StorageConstructorValidation(t *testing.T) {
	_, err := NewS3Storage("", "us-east-1", "", "", false, "", "")
	assert.ErrorIs(t, err, ErrInvalidArg)

	_, err = NewS3Storage("bucket", "", "", "", false, "", "")
	assert.ErrorIs(t, err, ErrInvalidArg)

	storage, err := NewS3Storage("bucket", "us-east-1", "analytics", "", false, "", "")
	require.NoError(t, err)
	assert.Equal(t, "analytics/events/data_0.dat", storage.buildKey("events"))
}
