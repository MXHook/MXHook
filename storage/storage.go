package storage

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"syscall"
)

var ErrIDCollision = errors.New("storage: message ID collision")

var validID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9:._-]{0,255}$`)

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

// Prepare creates and validates the durable base directory.
func (f *FilesystemStorer) Prepare() error {
	_, err := f.ensureBase()
	return err
}

// Store writes data under baseDir and returns the full path.
func (f *FilesystemStorer) Store(ctx context.Context, id string, data []byte) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if !validID.MatchString(id) {
		return "", errors.New("storage: invalid message ID")
	}
	base, err := f.ensureBase()
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256([]byte(id))
	name := fmt.Sprintf("%x.eml", digest)
	dir := filepath.Join(base, name[:2])
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("storage: mkdir shard: %w", err)
	}
	if err := os.Chmod(dir, 0o700); err != nil { //nolint:gosec // G302: directory mode 0o700 (stricter than the G301 directory limit); G302 applies the file threshold to os.Chmod
		return "", fmt.Errorf("storage: secure shard: %w", err)
	}
	// Persist the shard directory entry itself; fsyncing only the file and
	// its own directory leaves a crash window where the shard entry is lost.
	if err := syncDirectory(base); err != nil {
		return "", err
	}
	path := filepath.Join(dir, name)
	if existing, err := readRegular(path); err == nil {
		if bytes.Equal(existing, data) {
			return path, nil
		}
		return "", fmt.Errorf("%w: %s", ErrIDCollision, id)
	} else if !errors.Is(err, fs.ErrNotExist) {
		return "", err
	}

	tmp, err := os.CreateTemp(dir, ".mxhook-raw-*")
	if err != nil {
		return "", fmt.Errorf("storage: create temporary file: %w", err)
	}
	tmpPath := tmp.Name()
	defer func() { _ = os.Remove(tmpPath) }() // best-effort temp cleanup; file is gone after a successful publish
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close() // best-effort cleanup on error path; original error returned
		return "", fmt.Errorf("storage: secure temporary file: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close() // best-effort cleanup on error path; original error returned
		return "", fmt.Errorf("storage: write temporary file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close() // best-effort cleanup on error path; original error returned
		return "", fmt.Errorf("storage: sync temporary file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return "", fmt.Errorf("storage: close temporary file: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	publishErr := linkFile(tmpPath, path)
	if publishErr != nil && isLinkUnsupported(publishErr) {
		// Link-less filesystems (NFS, FUSE, overlay): reserve the name with
		// O_EXCL to keep collision detection, then rename into place.
		publishErr = publishWithoutLink(tmpPath, path)
	}
	if publishErr != nil {
		if errors.Is(publishErr, fs.ErrExist) {
			existing, readErr := readRegular(path)
			if readErr != nil {
				return "", readErr
			}
			if bytes.Equal(existing, data) {
				return path, nil
			}
			return "", fmt.Errorf("%w: %s", ErrIDCollision, id)
		}
		return "", fmt.Errorf("storage: publish raw file: %w", publishErr)
	}
	if err := syncDirectory(dir); err != nil {
		return "", err
	}
	return path, nil
}

func (f *FilesystemStorer) ensureBase() (string, error) {
	base, err := filepath.Abs(f.baseDir)
	if err != nil {
		return "", fmt.Errorf("storage: resolve base directory: %w", err)
	}
	if err := os.MkdirAll(base, 0o700); err != nil {
		return "", fmt.Errorf("storage: mkdir: %w", err)
	}
	if err := os.Chmod(base, 0o700); err != nil { //nolint:gosec // G302: directory mode 0o700 (stricter than the G301 directory limit); G302 applies the file threshold to os.Chmod
		return "", fmt.Errorf("storage: secure base directory: %w", err)
	}
	return base, nil
}

// Get reads and returns the file at the given path.
func (f *FilesystemStorer) Get(_ context.Context, path string) ([]byte, error) {
	root, relative, err := f.openInBase(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = root.Close() }() // read-only handle; close error not actionable
	info, err := root.Lstat(relative)
	if err != nil {
		return nil, fmt.Errorf("storage: read: %w", err)
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("storage: raw path is not a regular file")
	}
	data, err := root.ReadFile(relative)
	if err != nil {
		return nil, fmt.Errorf("storage: read: %w", err)
	}
	return data, nil
}

// Delete removes the file at the given path.
func (f *FilesystemStorer) Delete(_ context.Context, path string) error {
	root, relative, err := f.openInBase(path)
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }() // read-only handle; close error not actionable
	info, err := root.Lstat(relative)
	if err != nil {
		return fmt.Errorf("storage: inspect delete target: %w", err)
	}
	if !info.Mode().IsRegular() {
		return errors.New("storage: delete target is not a regular file")
	}
	if err := root.Remove(relative); err != nil {
		return fmt.Errorf("storage: delete: %w", err)
	}
	return nil
}

// openInBase resolves path to a base-relative name and opens the base as an
// os.Root, so every following access uses openat semantics: symlinks that
// resolve outside the base fail instead of escaping it.
func (f *FilesystemStorer) openInBase(path string) (*os.Root, string, error) {
	base, relative, err := f.resolve(path)
	if err != nil {
		return nil, "", err
	}
	root, err := os.OpenRoot(base)
	if err != nil {
		return nil, "", fmt.Errorf("storage: open base directory: %w", err)
	}
	return root, relative, nil
}

func (f *FilesystemStorer) resolve(path string) (base string, relative string, err error) {
	base, err = filepath.Abs(f.baseDir)
	if err != nil {
		return "", "", fmt.Errorf("storage: resolve base directory: %w", err)
	}
	target, err := filepath.Abs(path)
	if err != nil {
		return "", "", fmt.Errorf("storage: resolve path: %w", err)
	}
	relative, err = filepath.Rel(base, target)
	if err != nil || relative == "." || relative == ".." || filepath.IsAbs(relative) ||
		len(relative) >= 3 && relative[:3] == ".."+string(filepath.Separator) {
		return "", "", errors.New("storage: path escapes base directory")
	}
	return base, relative, nil
}

// linkFile is a seam so tests can simulate link-less filesystems.
var linkFile = os.Link

func isLinkUnsupported(err error) bool {
	return errors.Is(err, errors.ErrUnsupported) ||
		errors.Is(err, syscall.ENOTSUP) ||
		errors.Is(err, syscall.EPERM)
}

// publishWithoutLink reserves the final name with O_EXCL (preserving
// collision detection semantics) and renames the fsynced temporary file
// over the reservation.
func publishWithoutLink(tmpPath, path string) error {
	reserved, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600) //nolint:gosec // G304: path is baseDir/<sha256 shard>/<sha256>.eml, derived from a validated, hashed ID
	if err != nil {
		return fmt.Errorf("storage: reserve final name: %w", err)
	}
	if err := reserved.Close(); err != nil {
		return fmt.Errorf("storage: close reservation: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("storage: publish raw file: %w", err)
	}
	return nil
}

func readRegular(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, fmt.Errorf("storage: lstat raw file: %w", err)
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("storage: raw path is not a regular file")
	}
	data, err := os.ReadFile(path) //nolint:gosec // G304: path is baseDir/<sha256 shard>/<sha256>.eml, derived from a validated, hashed ID
	if err != nil {
		return nil, fmt.Errorf("storage: read raw file: %w", err)
	}
	return data, nil
}

func syncDirectory(path string) error {
	if runtime.GOOS == "windows" {
		return nil
	}
	dir, err := os.Open(path) //nolint:gosec // G304: path is the operator-configured raw directory or one of its hash shards
	if err != nil {
		return fmt.Errorf("storage: open directory for sync: %w", err)
	}
	defer func() { _ = dir.Close() }() // read-only handle; close error not actionable
	if err := dir.Sync(); err != nil {
		return fmt.Errorf("storage: sync directory: %w", err)
	}
	return nil
}
