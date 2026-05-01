package store

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockStorageBackend struct {
	mu              sync.RWMutex
	data            map[string][]byte
	initializeErr   error
	writeErr        error
	readErr         error
	deleteErr       error
	listErr         error
	healthErr       error
	closeErr        error
	initializeCalls int
}

func newMockStorageBackend() *mockStorageBackend {
	return &mockStorageBackend{data: make(map[string][]byte)}
}

func (m *mockStorageBackend) Initialize(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.initializeCalls++
	return m.initializeErr
}

func (m *mockStorageBackend) Write(ctx context.Context, namespace string, data []byte) (string, error) {
	if namespace == "" {
		return "", ErrInvalidArg
	}
	if m.writeErr != nil {
		return "", m.writeErr
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	path := fmt.Sprintf("%s/file_%d.dat", namespace, len(m.data)+1)
	m.data[path] = append([]byte(nil), data...)
	return path, nil
}

func (m *mockStorageBackend) Read(ctx context.Context, path string) ([]byte, error) {
	if path == "" {
		return nil, ErrInvalidArg
	}
	if m.readErr != nil {
		return nil, m.readErr
	}

	m.mu.RLock()
	defer m.mu.RUnlock()
	data, ok := m.data[path]
	if !ok {
		return nil, ErrNotFound
	}
	return append([]byte(nil), data...), nil
}

func (m *mockStorageBackend) Delete(ctx context.Context, path string) error {
	if path == "" {
		return ErrInvalidArg
	}
	if m.deleteErr != nil {
		return m.deleteErr
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.data, path)
	return nil
}

func (m *mockStorageBackend) List(ctx context.Context, namespace string) ([]string, error) {
	if namespace == "" {
		return nil, ErrInvalidArg
	}
	if m.listErr != nil {
		return nil, m.listErr
	}

	m.mu.RLock()
	defer m.mu.RUnlock()
	paths := make([]string, 0, len(m.data))
	for path := range m.data {
		if strings.HasPrefix(path, namespace+"/") {
			paths = append(paths, path)
		}
	}
	return paths, nil
}

func (m *mockStorageBackend) Health(ctx context.Context) error { return m.healthErr }
func (m *mockStorageBackend) Close(ctx context.Context) error  { return m.closeErr }

func TestMockStorageBackendCRUD(t *testing.T) {
	backend := newMockStorageBackend()
	ctx := context.Background()

	require.NoError(t, backend.Initialize(ctx))
	assert.Equal(t, 1, backend.initializeCalls)

	path, err := backend.Write(ctx, "events", []byte("payload"))
	require.NoError(t, err)
	assert.Contains(t, path, "events/")

	data, err := backend.Read(ctx, path)
	require.NoError(t, err)
	assert.Equal(t, []byte("payload"), data)

	list, err := backend.List(ctx, "events")
	require.NoError(t, err)
	assert.Len(t, list, 1)

	require.NoError(t, backend.Delete(ctx, path))
	_, err = backend.Read(ctx, path)
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestMockStorageBackendConcurrentWrites(t *testing.T) {
	backend := newMockStorageBackend()
	ctx := context.Background()

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := backend.Write(ctx, "events", []byte("test"))
			assert.NoError(t, err)
		}()
	}
	wg.Wait()

	list, err := backend.List(ctx, "events")
	require.NoError(t, err)
	assert.Len(t, list, 10)
}

func TestMockStorageBackendErrorPaths(t *testing.T) {
	backend := newMockStorageBackend()
	backend.healthErr = ErrFailed
	backend.closeErr = ErrFailed

	assert.ErrorIs(t, backend.Health(context.Background()), ErrFailed)
	assert.ErrorIs(t, backend.Close(context.Background()), ErrFailed)

	_, err := backend.Write(context.Background(), "", []byte("payload"))
	assert.ErrorIs(t, err, ErrInvalidArg)
}
