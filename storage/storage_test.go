package storage_test

import (
	"context"
	"os"
	"testing"

	"github.com/mxhook/mxhook/storage"
)

func TestFilesystemStorer_ImplementsStorer(_ *testing.T) {
	var _ storage.Storer = (*storage.FilesystemStorer)(nil)
}

func TestFilesystemStorer_StoreAndGet(t *testing.T) {
	dir := t.TempDir()
	s := storage.NewFilesystemStorer(dir)

	data := []byte("raw email content")
	path, err := s.Store(context.Background(), "test-id", data)
	if err != nil {
		t.Fatalf("Store: %v", err)
	}

	got, err := s.Get(context.Background(), path)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if string(got) != string(data) {
		t.Fatalf("got %q, want %q", got, data)
	}

	if err := s.Delete(context.Background(), path); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("file should be deleted")
	}
}
