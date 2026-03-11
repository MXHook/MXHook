package storage

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
)

// Storer is the interface for storing and retrieving raw email data.
type Storer interface {
	Store(ctx context.Context, id string, data []byte) (path string, err error)
	Get(ctx context.Context, path string) ([]byte, error)
	Delete(ctx context.Context, path string) error
}

// FilesystemStorer stores emails as files on the local filesystem.
type FilesystemStorer struct {
	baseDir string
}

// NewFilesystemStorer returns a Storer backed by the local filesystem.
func NewFilesystemStorer(baseDir string) *FilesystemStorer {
	return &FilesystemStorer{baseDir: baseDir}
}

// Store writes data to baseDir/<id>.eml and returns the full path.
func (f *FilesystemStorer) Store(_ context.Context, id string, data []byte) (string, error) {
	if err := os.MkdirAll(f.baseDir, 0o755); err != nil {
		return "", fmt.Errorf("storage: mkdir: %w", err)
	}
	path := filepath.Join(f.baseDir, id+".eml")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return "", fmt.Errorf("storage: write: %w", err)
	}
	return path, nil
}

// Get reads and returns the file at the given path.
func (f *FilesystemStorer) Get(_ context.Context, path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("storage: read: %w", err)
	}
	return data, nil
}

// Delete removes the file at the given path.
func (f *FilesystemStorer) Delete(_ context.Context, path string) error {
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("storage: delete: %w", err)
	}
	return nil
}
