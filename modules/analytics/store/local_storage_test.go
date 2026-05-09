package store

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLocalStorageNamespaceTraversalBlocked(t *testing.T) {
	basePath := t.TempDir()
	storage := NewLocalStorage(basePath)
	ctx := context.Background()

	_, err := storage.Write(ctx, "../../escaped", []byte("payload"))
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrInvalidArg)

	targetPath := filepath.Join(filepath.Dir(basePath), "escaped")
	_, statErr := os.Stat(targetPath)
	assert.ErrorIs(t, statErr, os.ErrNotExist)

	_, err = storage.List(ctx, "../../escaped")
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrInvalidArg)
}

func TestLocalStorageNamespaceValidSubpath(t *testing.T) {
	basePath := t.TempDir()
	storage := NewLocalStorage(basePath)
	ctx := context.Background()

	path, err := storage.Write(ctx, "events/2026", []byte("payload"))
	require.NoError(t, err)
	assert.Contains(t, path, filepath.Join("events", "2026")+string(filepath.Separator))

	paths, err := storage.List(ctx, "events/2026")
	require.NoError(t, err)
	require.Len(t, paths, 1)
	assert.Equal(t, path, paths[0])
}
