package storage_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/mxhook/mxhook/storage"
)

func TestFilesystemStorerRejectsTraversalID(t *testing.T) {
	parent := t.TempDir()
	base := filepath.Join(parent, "raw")
	storer := storage.NewFilesystemStorer(base)
	if _, err := storer.Store(context.Background(), "../escape", []byte("secret")); err == nil {
		t.Fatal("Store accepted a traversal ID")
	}
	if _, err := os.Stat(filepath.Join(parent, "escape.eml")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("outside file exists: %v", err)
	}
}

func TestFilesystemStorerGetRejectsTraversalPath(t *testing.T) {
	parent := t.TempDir()
	base := filepath.Join(parent, "raw")
	storer := storage.NewFilesystemStorer(base)
	if err := storer.Prepare(); err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	victim := filepath.Join(parent, "victim.eml")
	if err := os.WriteFile(victim, []byte("outside data"), 0o600); err != nil {
		t.Fatalf("write victim: %v", err)
	}
	if _, err := storer.Get(context.Background(), filepath.Join(base, "..", "victim.eml")); err == nil {
		t.Fatal("Get accepted a traversal path")
	}
}

func TestFilesystemStorerDeleteRejectsTraversalPath(t *testing.T) {
	parent := t.TempDir()
	base := filepath.Join(parent, "raw")
	storer := storage.NewFilesystemStorer(base)
	if err := storer.Prepare(); err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	victim := filepath.Join(parent, "victim.eml")
	if err := os.WriteFile(victim, []byte("outside data"), 0o600); err != nil {
		t.Fatalf("write victim: %v", err)
	}
	if err := storer.Delete(context.Background(), filepath.Join(base, "..", "victim.eml")); err == nil {
		t.Fatal("Delete accepted a traversal path")
	}
	if _, err := os.Stat(victim); err != nil {
		t.Fatalf("outside file was removed: %v", err)
	}
}

func symlinkEscapeFixture(t *testing.T) (storer *storage.FilesystemStorer, base, victim string) {
	t.Helper()
	parent := t.TempDir()
	base = filepath.Join(parent, "raw")
	storer = storage.NewFilesystemStorer(base)
	if err := storer.Prepare(); err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	outside := filepath.Join(parent, "outside")
	if err := os.MkdirAll(outside, 0o700); err != nil {
		t.Fatalf("mkdir outside: %v", err)
	}
	victim = filepath.Join(outside, "victim.eml")
	if err := os.WriteFile(victim, []byte("outside data"), 0o600); err != nil {
		t.Fatalf("write victim: %v", err)
	}
	if err := os.Symlink(outside, filepath.Join(base, "aa")); err != nil {
		t.Fatalf("symlink shard: %v", err)
	}
	return storer, base, victim
}

func TestFilesystemStorerGetRejectsSymlinkedDirectoryEscape(t *testing.T) {
	storer, base, _ := symlinkEscapeFixture(t)
	if _, err := storer.Get(context.Background(), filepath.Join(base, "aa", "victim.eml")); err == nil {
		t.Fatal("Get followed a symlinked directory outside the base")
	}
}

func TestFilesystemStorerDeleteRejectsSymlinkedDirectoryEscape(t *testing.T) {
	storer, base, victim := symlinkEscapeFixture(t)
	if err := storer.Delete(context.Background(), filepath.Join(base, "aa", "victim.eml")); err == nil {
		t.Fatal("Delete followed a symlinked directory outside the base")
	}
	if _, err := os.Stat(victim); err != nil {
		t.Fatalf("outside file was removed: %v", err)
	}
}

func TestFilesystemStorerGetRejectsSymlinkedFile(t *testing.T) {
	storer, base, victim := symlinkEscapeFixture(t)
	link := filepath.Join(base, "direct.eml")
	if err := os.Symlink(victim, link); err != nil {
		t.Fatalf("symlink file: %v", err)
	}
	if _, err := storer.Get(context.Background(), link); err == nil {
		t.Fatal("Get followed a symlinked file outside the base")
	}
}

func TestFilesystemStorerDeleteRejectsSymlinkedFile(t *testing.T) {
	storer, base, victim := symlinkEscapeFixture(t)
	link := filepath.Join(base, "direct.eml")
	if err := os.Symlink(victim, link); err != nil {
		t.Fatalf("symlink file: %v", err)
	}
	if err := storer.Delete(context.Background(), link); err == nil {
		t.Fatal("Delete accepted a symlinked file")
	}
	if _, err := os.Stat(victim); err != nil {
		t.Fatalf("outside file was removed: %v", err)
	}
}

func TestFilesystemStorerRejectsDifferentContentForSameID(t *testing.T) {
	storer := storage.NewFilesystemStorer(t.TempDir())
	ctx := context.Background()
	firstPath, err := storer.Store(ctx, "same", []byte("first"))
	if err != nil {
		t.Fatalf("first Store: %v", err)
	}
	if _, err := storer.Store(ctx, "same", []byte("second")); err == nil {
		t.Fatal("second Store accepted different content")
	}
	got, err := storer.Get(ctx, firstPath)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if string(got) != "first" {
		t.Fatalf("stored content = %q, want first", got)
	}
}

func TestFilesystemStorerUsesPrivateModes(t *testing.T) {
	base := filepath.Join(t.TempDir(), "raw")
	storer := storage.NewFilesystemStorer(base)
	path, err := storer.Store(context.Background(), "private", []byte("secret"))
	if err != nil {
		t.Fatalf("Store: %v", err)
	}
	dirInfo, err := os.Stat(base)
	if err != nil {
		t.Fatalf("stat base: %v", err)
	}
	if got := dirInfo.Mode().Perm(); got != 0o700 {
		t.Fatalf("base mode = %o, want 700", got)
	}
	fileInfo, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat raw file: %v", err)
	}
	if got := fileInfo.Mode().Perm(); got != 0o600 {
		t.Fatalf("file mode = %o, want 600", got)
	}
}
