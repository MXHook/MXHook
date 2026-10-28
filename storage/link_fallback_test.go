package storage

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestStoreFallsBackWhenLinkUnsupported(t *testing.T) {
	original := linkFile
	linkFile = func(string, string) error { return syscall.ENOTSUP }
	t.Cleanup(func() { linkFile = original })

	storer := NewFilesystemStorer(t.TempDir())
	ctx := context.Background()
	path, err := storer.Store(ctx, "fallback", []byte("payload"))
	if err != nil {
		t.Fatalf("Store on link-less filesystem: %v", err)
	}
	got, err := storer.Get(ctx, path)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if string(got) != "payload" {
		t.Fatalf("stored content = %q, want payload", got)
	}

	// Idempotent same-content store still succeeds.
	again, err := storer.Store(ctx, "fallback", []byte("payload"))
	if err != nil {
		t.Fatalf("idempotent Store: %v", err)
	}
	if again != path {
		t.Fatalf("idempotent Store path = %q, want %q", again, path)
	}

	// Collision detection must survive the fallback.
	if _, err := storer.Store(ctx, "fallback", []byte("different")); !errors.Is(err, ErrIDCollision) {
		t.Fatalf("collision error = %v, want ErrIDCollision", err)
	}
}

func TestStoreFallsBackWhenLinkPermissionDenied(t *testing.T) {
	original := linkFile
	linkFile = func(string, string) error { return &os.LinkError{Op: "link", Err: syscall.EPERM} }
	t.Cleanup(func() { linkFile = original })

	storer := NewFilesystemStorer(t.TempDir())
	path, err := storer.Store(context.Background(), "eperm", []byte("payload"))
	if err != nil {
		t.Fatalf("Store with EPERM link: %v", err)
	}
	got, err := storer.Get(context.Background(), path)
	if err != nil || string(got) != "payload" {
		t.Fatalf("Get = %q, %v; want payload", got, err)
	}
}

func TestPublishWithoutLinkDetectsExistingTarget(t *testing.T) {
	dir := t.TempDir()
	tmp := filepath.Join(dir, "tmp")
	if err := os.WriteFile(tmp, []byte("new"), 0o600); err != nil {
		t.Fatalf("write tmp: %v", err)
	}
	target := filepath.Join(dir, "target")
	if err := os.WriteFile(target, []byte("existing"), 0o600); err != nil {
		t.Fatalf("write target: %v", err)
	}
	if err := publishWithoutLink(tmp, target); !errors.Is(err, fs.ErrExist) {
		t.Fatalf("publishWithoutLink over existing target = %v, want fs.ErrExist", err)
	}
	existing, err := os.ReadFile(target) //nolint:gosec // G304: test reads a path under t.TempDir()
	if err != nil || string(existing) != "existing" {
		t.Fatalf("target content = %q, %v; must be untouched", existing, err)
	}
}
