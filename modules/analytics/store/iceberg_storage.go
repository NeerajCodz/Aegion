package store

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// IcebergStorage implements a file-backed warehouse for analytics archival data.
// It provides the StorageBackend contract locally while preserving Iceberg-oriented
// concepts such as catalog, warehouse, namespace, and metadata.
type IcebergStorage struct {
	catalogType   string
	warehousePath string
	catalogName   string
	nessieURI     string
	rootPath      string
}

type icebergCatalogMetadata struct {
	CatalogType   string    `json:"catalog_type"`
	CatalogName   string    `json:"catalog_name"`
	NessieURI     string    `json:"nessie_uri,omitempty"`
	WarehousePath string    `json:"warehouse_path"`
	InitializedAt time.Time `json:"initialized_at"`
}

type icebergObjectMetadata struct {
	Namespace string    `json:"namespace"`
	Path      string    `json:"path"`
	SizeBytes int       `json:"size_bytes"`
	CreatedAt time.Time `json:"created_at"`
}

// NewIcebergStorage creates a new Iceberg storage backend.
func NewIcebergStorage(catalogType, warehousePath, catalogName, nessieURI string) (*IcebergStorage, error) {
	if catalogType == "" {
		return nil, fmt.Errorf("catalog_type cannot be empty: %w", ErrInvalidArg)
	}

	if warehousePath == "" {
		return nil, fmt.Errorf("warehouse_path cannot be empty: %w", ErrInvalidArg)
	}

	if catalogName == "" {
		return nil, fmt.Errorf("catalog_name cannot be empty: %w", ErrInvalidArg)
	}

	validTypes := []string{"nessie", "dynamodb", "rest", "hive"}
	valid := false
	for _, t := range validTypes {
		if catalogType == t {
			valid = true
			break
		}
	}

	if !valid {
		return nil, fmt.Errorf("invalid catalog_type: must be one of nessie, dynamodb, rest, or hive: %w", ErrInvalidArg)
	}

	if catalogType == "nessie" && nessieURI == "" {
		return nil, fmt.Errorf("nessie_uri is required when catalog_type is nessie: %w", ErrInvalidArg)
	}

	rootPath := filepath.Join(warehousePath, catalogName)

	return &IcebergStorage{
		catalogType:   catalogType,
		warehousePath: warehousePath,
		catalogName:   catalogName,
		nessieURI:     nessieURI,
		rootPath:      rootPath,
	}, nil
}

// Initialize prepares the Iceberg catalog.
func (i *IcebergStorage) Initialize(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	if err := os.MkdirAll(i.rootPath, 0o755); err != nil {
		return fmt.Errorf("failed to create iceberg warehouse: %w", err)
	}

	metadata := icebergCatalogMetadata{
		CatalogType:   i.catalogType,
		CatalogName:   i.catalogName,
		NessieURI:     i.nessieURI,
		WarehousePath: i.warehousePath,
		InitializedAt: time.Now().UTC(),
	}

	metadataPath := filepath.Join(i.rootPath, "_catalog.json")
	body, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to encode iceberg metadata: %w", err)
	}

	if err := os.WriteFile(metadataPath, body, 0o644); err != nil {
		return fmt.Errorf("failed to write iceberg metadata: %w", err)
	}

	return nil
}

// Write stores data into a namespace within the warehouse.
func (i *IcebergStorage) Write(ctx context.Context, namespace string, data []byte) (string, error) {
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	default:
	}

	if namespace == "" {
		return "", fmt.Errorf("namespace cannot be empty: %w", ErrInvalidArg)
	}

	namespacePath, err := i.namespacePath(namespace)
	if err != nil {
		return "", err
	}

	if err := os.MkdirAll(namespacePath, 0o755); err != nil {
		return "", fmt.Errorf("failed to create namespace directory: %w", err)
	}

	filename := fmt.Sprintf("data_%d.iceberg", time.Now().UnixNano())
	filePath := filepath.Join(namespacePath, filename)
	if err := os.WriteFile(filePath, data, 0o644); err != nil {
		return "", fmt.Errorf("failed to write iceberg object: %w", err)
	}

	relPath, err := filepath.Rel(i.rootPath, filePath)
	if err != nil {
		return "", fmt.Errorf("failed to compute iceberg relative path: %w", err)
	}

	if err := i.writeObjectMetadata(relPath, namespace, len(data)); err != nil {
		return "", err
	}

	return filepath.ToSlash(relPath), nil
}

// Read retrieves data from the warehouse.
func (i *IcebergStorage) Read(ctx context.Context, path string) ([]byte, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	if path == "" {
		return nil, fmt.Errorf("path cannot be empty: %w", ErrInvalidArg)
	}

	filePath, err := i.resolvePath(path)
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("iceberg object not found: %w", ErrNotFound)
		}
		return nil, fmt.Errorf("failed to read iceberg object: %w", err)
	}

	return data, nil
}

// Delete removes an object from the warehouse.
func (i *IcebergStorage) Delete(ctx context.Context, path string) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	if path == "" {
		return fmt.Errorf("path cannot be empty: %w", ErrInvalidArg)
	}

	filePath, err := i.resolvePath(path)
	if err != nil {
		return err
	}

	if err := os.Remove(filePath); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("iceberg object not found: %w", ErrNotFound)
		}
		return fmt.Errorf("failed to delete iceberg object: %w", err)
	}

	metadataPath := filePath + ".meta.json"
	if err := os.Remove(metadataPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to delete iceberg metadata: %w", err)
	}

	return nil
}

// List returns all stored objects in a namespace.
func (i *IcebergStorage) List(ctx context.Context, namespace string) ([]string, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	if namespace == "" {
		return nil, fmt.Errorf("namespace cannot be empty: %w", ErrInvalidArg)
	}

	namespacePath, err := i.namespacePath(namespace)
	if err != nil {
		return nil, err
	}

	entries, err := os.ReadDir(namespacePath)
	if err != nil {
		if os.IsNotExist(err) {
			return []string{}, nil
		}
		return nil, fmt.Errorf("failed to list iceberg namespace: %w", err)
	}

	paths := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || strings.HasSuffix(entry.Name(), ".meta.json") {
			continue
		}
		paths = append(paths, filepath.ToSlash(filepath.Join(namespace, entry.Name())))
	}

	return paths, nil
}

// Health checks if the warehouse is accessible.
func (i *IcebergStorage) Health(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	info, err := os.Stat(i.rootPath)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("iceberg warehouse not initialized: %w", ErrFailed)
		}
		return fmt.Errorf("failed to stat iceberg warehouse: %w", err)
	}

	if !info.IsDir() {
		return fmt.Errorf("iceberg warehouse path is not a directory: %w", ErrFailed)
	}

	return nil
}

// Close releases resources used by the backend.
func (i *IcebergStorage) Close(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return nil
	}
}

func (i *IcebergStorage) namespacePath(namespace string) (string, error) {
	if strings.Contains(namespace, "..") {
		return "", fmt.Errorf("invalid namespace: %w", ErrInvalidArg)
	}
	return filepath.Join(i.rootPath, filepath.FromSlash(namespace)), nil
}

func (i *IcebergStorage) resolvePath(path string) (string, error) {
	if strings.Contains(path, "..") {
		return "", fmt.Errorf("invalid path: %w", ErrInvalidArg)
	}

	filePath := filepath.Join(i.rootPath, filepath.FromSlash(path))
	absPath, err := filepath.Abs(filePath)
	if err != nil {
		return "", fmt.Errorf("failed to resolve path: %w", err)
	}

	absRoot, err := filepath.Abs(i.rootPath)
	if err != nil {
		return "", fmt.Errorf("failed to resolve root path: %w", err)
	}

	if !strings.HasPrefix(absPath, absRoot) {
		return "", fmt.Errorf("path escapes warehouse root: %w", ErrInvalidArg)
	}

	return filePath, nil
}

func (i *IcebergStorage) writeObjectMetadata(relPath, namespace string, size int) error {
	metadata := icebergObjectMetadata{
		Namespace: namespace,
		Path:      filepath.ToSlash(relPath),
		SizeBytes: size,
		CreatedAt: time.Now().UTC(),
	}

	body, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to encode iceberg object metadata: %w", err)
	}

	filePath, err := i.resolvePath(relPath)
	if err != nil {
		return err
	}

	if err := os.WriteFile(filePath+".meta.json", body, 0o644); err != nil {
		return fmt.Errorf("failed to write iceberg object metadata: %w", err)
	}

	return nil
}
