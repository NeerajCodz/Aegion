// Package securefile reads deployment-mounted regular files with bounded,
// race-checked semantics.
package securefile

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

var (
	ErrPathNotAbsolute = errors.New("file path must be absolute")
	ErrNotRegularFile  = errors.New("path is not a regular file")
	ErrEmptyFile       = errors.New("file is empty")
	ErrFileTooLarge    = errors.New("file exceeds configured size limit")
	ErrFileChanged     = errors.New("file changed while being opened")
)

// ReadRegularFile reads one non-empty, absolute, regular file up to maxBytes.
// It rejects symlinks and verifies the opened file is the one that was checked,
// preventing a replacement between validation and opening the deployment mount.
func ReadRegularFile(path string, maxBytes int64) ([]byte, error) {
	if maxBytes <= 0 {
		return nil, errors.New("maximum file size must be positive")
	}

	cleanPath, err := cleanAbsolutePath(path)
	if err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(filepath.Dir(cleanPath))
	if err != nil {
		return nil, err
	}
	defer root.Close()

	fileName := filepath.Base(cleanPath)
	checked, err := inspectRegularFile(root, fileName, maxBytes)
	if err != nil {
		return nil, err
	}
	file, err := root.Open(fileName)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	opened, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !opened.Mode().IsRegular() {
		return nil, ErrNotRegularFile
	}
	if !os.SameFile(checked, opened) {
		return nil, ErrFileChanged
	}

	data, err := io.ReadAll(io.LimitReader(file, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxBytes {
		return nil, ErrFileTooLarge
	}
	if len(data) == 0 {
		return nil, ErrEmptyFile
	}
	return data, nil
}

func cleanAbsolutePath(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" || !filepath.IsAbs(path) {
		return "", ErrPathNotAbsolute
	}
	return filepath.Clean(path), nil
}

func inspectRegularFile(root *os.Root, fileName string, maxBytes int64) (os.FileInfo, error) {
	info, err := root.Lstat(fileName)
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return nil, ErrNotRegularFile
	}
	if info.Size() <= 0 {
		return nil, ErrEmptyFile
	}
	if info.Size() > maxBytes {
		return nil, fmt.Errorf("%w: %d > %d bytes", ErrFileTooLarge, info.Size(), maxBytes)
	}
	return info, nil
}
