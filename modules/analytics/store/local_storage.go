package store

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// LocalStorage implements StorageBackend for local filesystem storage.
type LocalStorage struct {
	basePath string
}

// NewLocalStorage creates a new local storage backend.
func NewLocalStorage(basePath string) *LocalStorage {
	return &LocalStorage{
		basePath: basePath,
	}
}

// Initialize creates the base path if it doesn't exist.
func (l *LocalStorage) Initialize(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	if err := os.MkdirAll(l.basePath, 0755); err != nil {
		return fmt.Errorf("failed to create base path: %w", err)
	}

	return nil
}

// Write stores data to the local filesystem.
func (l *LocalStorage) Write(ctx context.Context, namespace string, data []byte) (string, error) {
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	default:
	}

	if namespace == "" {
		return "", fmt.Errorf("namespace cannot be empty: %w", ErrInvalidArg)
	}

	// Create namespace directory
	namespacePath := filepath.Join(l.basePath, namespace)
	if err := os.MkdirAll(namespacePath, 0755); err != nil {
		return "", fmt.Errorf("failed to create namespace directory: %w", err)
	}

	// Generate a unique filename based on timestamp and random suffix
	filename := fmt.Sprintf("data_%d.dat", time.Now().UnixNano())
	filePath := filepath.Join(namespacePath, filename)

	// Write the data to file
	if err := os.WriteFile(filePath, data, 0644); err != nil {
		return "", fmt.Errorf("failed to write data: %w", err)
	}

	// Return relative path from base
	relPath, err := filepath.Rel(l.basePath, filePath)
	if err != nil {
		return "", fmt.Errorf("failed to compute relative path: %w", err)
	}

	return relPath, nil
}

// Read retrieves data from the local filesystem.
func (l *LocalStorage) Read(ctx context.Context, path string) ([]byte, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	if path == "" {
		return nil, fmt.Errorf("path cannot be empty: %w", ErrInvalidArg)
	}

	// Prevent directory traversal attacks
	if strings.Contains(path, "..") {
		return nil, fmt.Errorf("invalid path: %w", ErrInvalidArg)
	}

	filePath := filepath.Join(l.basePath, path)

	// Verify the resolved path is still within basePath
	absPath, err := filepath.Abs(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve path: %w", err)
	}

	absBase, err := filepath.Abs(l.basePath)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve base path: %w", err)
	}

	if !strings.HasPrefix(absPath, absBase+string(filepath.Separator)) && absPath != absBase {
		return nil, fmt.Errorf("path escapes base directory: %w", ErrInvalidArg)
	}

	data, err := os.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("file not found: %w", ErrNotFound)
		}
		return nil, fmt.Errorf("failed to read data: %w", err)
	}

	return data, nil
}

// Delete removes a file from local storage.
func (l *LocalStorage) Delete(ctx context.Context, path string) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	if path == "" {
		return fmt.Errorf("path cannot be empty: %w", ErrInvalidArg)
	}

	if strings.Contains(path, "..") {
		return fmt.Errorf("invalid path: %w", ErrInvalidArg)
	}

	filePath := filepath.Join(l.basePath, path)

	// Verify path is within basePath
	absPath, err := filepath.Abs(filePath)
	if err != nil {
		return fmt.Errorf("failed to resolve path: %w", err)
	}

	absBase, err := filepath.Abs(l.basePath)
	if err != nil {
		return fmt.Errorf("failed to resolve base path: %w", err)
	}

	if !strings.HasPrefix(absPath, absBase+string(filepath.Separator)) && absPath != absBase {
		return fmt.Errorf("path escapes base directory: %w", ErrInvalidArg)
	}

	if err := os.Remove(filePath); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("file not found: %w", ErrNotFound)
		}
		return fmt.Errorf("failed to delete file: %w", err)
	}

	return nil
}

// List returns all files in a namespace directory.
func (l *LocalStorage) List(ctx context.Context, namespace string) ([]string, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	if namespace == "" {
		return nil, fmt.Errorf("namespace cannot be empty: %w", ErrInvalidArg)
	}

	namespacePath := filepath.Join(l.basePath, namespace)

	entries, err := os.ReadDir(namespacePath)
	if err != nil {
		if os.IsNotExist(err) {
			return []string{}, nil // Empty namespace
		}
		return nil, fmt.Errorf("failed to list directory: %w", err)
	}

	var paths []string
	for _, entry := range entries {
		if !entry.IsDir() {
			relPath := filepath.Join(namespace, entry.Name())
			paths = append(paths, relPath)
		}
	}

	return paths, nil
}

// Health checks if the storage backend is accessible.
func (l *LocalStorage) Health(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	// Try to access the base path
	_, err := os.Stat(l.basePath)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("base path does not exist: %w", ErrFailed)
		}
		return fmt.Errorf("failed to access base path: %w", err)
	}

	return nil
}

// Close is a no-op for local storage.
func (l *LocalStorage) Close(ctx context.Context) error {
	return nil
}
