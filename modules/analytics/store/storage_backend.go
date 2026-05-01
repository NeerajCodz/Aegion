package store

import (
	"context"
	"errors"
)

// StorageBackend is the interface that all storage backends must implement.
type StorageBackend interface {
	// Initialize prepares the storage backend for use.
	Initialize(ctx context.Context) error

	// Write stores a batch of data to the storage backend.
	Write(ctx context.Context, namespace string, data []byte) (path string, err error)

	// Read retrieves data from the storage backend.
	Read(ctx context.Context, path string) ([]byte, error)

	// Delete removes data from the storage backend.
	Delete(ctx context.Context, path string) error

	// List returns a list of all objects in a given namespace.
	List(ctx context.Context, namespace string) ([]string, error)

	// Health checks if the storage backend is operational.
	Health(ctx context.Context) error

	// Close releases any resources held by the storage backend.
	Close(ctx context.Context) error
}

// Common errors for storage backends.
var (
	ErrNotFound   = errors.New("not found")
	ErrUnknown    = errors.New("unknown storage error")
	ErrInvalidArg = errors.New("invalid argument")
	ErrFailed     = errors.New("storage operation failed")
)
