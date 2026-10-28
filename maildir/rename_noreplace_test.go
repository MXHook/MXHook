//go:build linux || darwin

package maildir

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

func stubNativeRename(t *testing.T, stub func(source, target string) error) {
	t.Helper()
	original := renameNoReplaceNative
	renameNoReplaceNative = stub
	t.Cleanup(func() { renameNoReplaceNative = original })
}

func writeRenameFixture(t *testing.T) (source, target string) {
	t.Helper()
	dir := t.TempDir()
	source = filepath.Join(dir, "source")
	target = filepath.Join(dir, "target")
	if err := os.WriteFile(source, []byte("source bytes"), 0o600); err != nil {
		t.Fatalf("write source: %v", err)
	}
	return source, target
}

func TestRenameNoReplaceFallsBackWhenNoReplaceUnsupported(t *testing.T) {
	for _, errno := range []unix.Errno{unix.EINVAL, unix.ENOSYS, unix.ENOTSUP} {
		t.Run(errno.Error(), func(t *testing.T) {
			stubNativeRename(t, func(string, string) error { return errno })
			source, target := writeRenameFixture(t)

			if err := renameNoReplace(source, target); err != nil {
				t.Fatalf("renameNoReplace did not fall back on %v: %v", errno, err)
			}
			if _, err := os.Lstat(source); !errors.Is(err, os.ErrNotExist) {
				t.Error("source still present after fallback rename")
			}
			if got, err := os.ReadFile(target); err != nil || string(got) != "source bytes" { //nolint:gosec // G304: test reads a path under t.TempDir()
				t.Errorf("target = (%q, %v), want source bytes", got, err)
			}
		})
	}
}

func TestRenameNoReplaceFallbackStillRefusesExistingTarget(t *testing.T) {
	stubNativeRename(t, func(string, string) error { return unix.EINVAL })
	source, target := writeRenameFixture(t)
	if err := os.WriteFile(target, []byte("target bytes"), 0o600); err != nil {
		t.Fatalf("write target: %v", err)
	}

	if err := renameNoReplace(source, target); err == nil {
		t.Fatal("fallback rename overwrote an existing target")
	}
	if got, _ := os.ReadFile(source); string(got) != "source bytes" { //nolint:gosec // G304: test reads a path under t.TempDir()
		t.Errorf("source changed: %q", got)
	}
	if got, _ := os.ReadFile(target); string(got) != "target bytes" { //nolint:gosec // G304: test reads a path under t.TempDir()
		t.Errorf("target changed: %q", got)
	}
}

func TestRenameNoReplaceDoesNotFallBackOnOtherErrors(t *testing.T) {
	stubNativeRename(t, func(string, string) error { return unix.EEXIST })
	source, target := writeRenameFixture(t)

	if err := renameNoReplace(source, target); !errors.Is(err, unix.EEXIST) {
		t.Fatalf("err = %v, want EEXIST passed through", err)
	}
	if _, err := os.Lstat(source); err != nil {
		t.Errorf("source missing after non-fallback error: %v", err)
	}
	if _, err := os.Lstat(target); !errors.Is(err, os.ErrNotExist) {
		t.Error("target created despite non-fallback error")
	}
}

func TestLinkUnlinkNoReplaceMovesDirectories(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "srcdir")
	if err := os.Mkdir(source, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "inner"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, "dstdir")

	if err := linkUnlinkNoReplace(source, target); err != nil {
		t.Fatalf("linkUnlinkNoReplace on directory: %v", err)
	}
	if _, err := os.Lstat(source); !errors.Is(err, os.ErrNotExist) {
		t.Error("source directory still present")
	}
	if _, err := os.Stat(filepath.Join(target, "inner")); err != nil {
		t.Errorf("moved directory content missing: %v", err)
	}
}

func TestLinkUnlinkNoReplaceRefusesExistingTargetDirectory(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "srcdir")
	target := filepath.Join(dir, "dstdir")
	for _, path := range []string{source, target} {
		if err := os.Mkdir(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}

	if err := linkUnlinkNoReplace(source, target); !errors.Is(err, os.ErrExist) {
		t.Fatalf("err = %v, want os.ErrExist", err)
	}
	if _, err := os.Lstat(source); err != nil {
		t.Errorf("source directory missing after refused move: %v", err)
	}
}

func TestRenameNoReplaceUsesNativeRenameOnSuccess(t *testing.T) {
	calls := 0
	stubNativeRename(t, func(source, target string) error {
		calls++
		return os.Rename(source, target)
	})
	source, target := writeRenameFixture(t)

	if err := renameNoReplace(source, target); err != nil {
		t.Fatalf("renameNoReplace: %v", err)
	}
	if calls != 1 {
		t.Errorf("native rename calls = %d, want 1", calls)
	}
	if _, err := os.Lstat(target); err != nil {
		t.Errorf("target missing after native rename: %v", err)
	}
}
