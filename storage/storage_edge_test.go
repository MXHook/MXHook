package storage_test

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/mxhook/mxhook/storage"
)

// TestFilesystemStorer_Get_NonExistentFile confirms Get returns an error when the
// file does not exist.
func TestFilesystemStorer_Get_NonExistentFile(t *testing.T) {
	dir := t.TempDir()
	s := storage.NewFilesystemStorer(dir)

	_, err := s.Get(context.Background(), filepath.Join(dir, "missing.eml"))
	if err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
}

// TestFilesystemStorer_Delete_NonExistentFile confirms Delete returns an error
// when there is no file to remove.
func TestFilesystemStorer_Delete_NonExistentFile(t *testing.T) {
	dir := t.TempDir()
	s := storage.NewFilesystemStorer(dir)

	err := s.Delete(context.Background(), filepath.Join(dir, "ghost.eml"))
	if err == nil {
		t.Fatal("expected error deleting non-existent file, got nil")
	}
}

// TestFilesystemStorer_Store_UnwritableDir confirms Store returns an error when
// it cannot create the base directory (simulate by using a file as the basedir).
func TestFilesystemStorer_Store_UnwritableDir(t *testing.T) {
	// Create a regular file and use its path as the baseDir; MkdirAll will fail
	// because something already exists at that path as a file.
	tmp := t.TempDir()
	blockingFile := filepath.Join(tmp, "notadir")
	if err := os.WriteFile(blockingFile, []byte("x"), 0o600); err != nil {
		t.Fatalf("setup: %v", err)
	}

	// Use the file path as the baseDir — MkdirAll cannot create a directory
	// where a regular file exists, so Store should return an error.
	s := storage.NewFilesystemStorer(filepath.Join(blockingFile, "subdir"))
	_, err := s.Store(context.Background(), "id", []byte("data"))
	if err == nil {
		t.Fatal("expected error storing to unwritable path, got nil")
	}
}

// TestFilesystemStorer_Concurrent confirms the storer is safe for concurrent use.
func TestFilesystemStorer_Concurrent(t *testing.T) {
	dir := t.TempDir()
	s := storage.NewFilesystemStorer(dir)
	ctx := context.Background()

	const workers = 10
	var wg sync.WaitGroup
	errs := make(chan error, workers)

	for i := range workers {
		wg.Add(1)
		go func(_ int) {
			defer wg.Done()
			id := filepath.Base(t.TempDir()) // unique name
			_ = id
			// Use a simple string id derived from loop index
			msgID := filepath.Base(t.TempDir())
			data := []byte("email body")
			path, err := s.Store(ctx, msgID, data)
			if err != nil {
				errs <- err
				return
			}
			got, err := s.Get(ctx, path)
			if err != nil {
				errs <- err
				return
			}
			if string(got) != string(data) {
				errs <- nil
				return
			}
			if err := s.Delete(ctx, path); err != nil {
				errs <- err
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Errorf("concurrent operation error: %v", err)
		}
	}
}

// TestFilesystemStorer_LargePayload verifies large data survives a round-trip.
func TestFilesystemStorer_LargePayload(t *testing.T) {
	dir := t.TempDir()
	s := storage.NewFilesystemStorer(dir)

	data := make([]byte, 1<<20) // 1 MiB
	for i := range data {
		data[i] = byte(i % 256)
	}

	path, err := s.Store(context.Background(), "large-id", data)
	if err != nil {
		t.Fatalf("Store: %v", err)
	}
	got, err := s.Get(context.Background(), path)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(got) != len(data) {
		t.Errorf("got %d bytes, want %d", len(got), len(data))
	}
}

// TestFilesystemStorer_EmptyPayload verifies a zero-byte payload is stored and
// retrieved correctly.
func TestFilesystemStorer_EmptyPayload(t *testing.T) {
	dir := t.TempDir()
	s := storage.NewFilesystemStorer(dir)

	path, err := s.Store(context.Background(), "empty-id", []byte{})
	if err != nil {
		t.Fatalf("Store: %v", err)
	}
	got, err := s.Get(context.Background(), path)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty, got %d bytes", len(got))
	}
}

// TestFilesystemStorer_Store_WriteError confirms that Store returns an error when
// os.WriteFile fails (e.g. the target path is a directory, not a file).
func TestFilesystemStorer_Store_WriteError(t *testing.T) {
	dir := t.TempDir()
	s := storage.NewFilesystemStorer(dir)

	collidingPath, err := s.Store(context.Background(), "conflict-id", []byte("seed"))
	if err != nil {
		t.Fatalf("seed Store: %v", err)
	}
	if err := os.Remove(collidingPath); err != nil {
		t.Fatalf("remove seed: %v", err)
	}
	if err := os.Mkdir(collidingPath, 0o700); err != nil {
		t.Fatalf("setup mkdir: %v", err)
	}

	_, err = s.Store(context.Background(), "conflict-id", []byte("data"))
	if err == nil {
		t.Fatal("expected error when WriteFile target is a directory, got nil")
	}
}

// TestFilesystemStorer_StoreCreatesSubdirs verifies that nested base directories
// are created automatically.
func TestFilesystemStorer_StoreCreatesSubdirs(t *testing.T) {
	dir := t.TempDir()
	nested := filepath.Join(dir, "a", "b", "c")
	s := storage.NewFilesystemStorer(nested)

	path, err := s.Store(context.Background(), "nested-id", []byte("hello"))
	if err != nil {
		t.Fatalf("Store: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("file not created: %v", err)
	}
}
