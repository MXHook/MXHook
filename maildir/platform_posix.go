//go:build aix || darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris

package maildir

import (
	"errors"
	"fmt"
	"io"
	"os"

	"golang.org/x/sys/unix"
)

func validatePrivateDirectory(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("maildir: inspect %s: %w", path, err)
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("maildir: %s is not a real directory", path)
	}
	if info.Mode().Perm()&0o022 != 0 {
		return fmt.Errorf("maildir: %s is group/world-writable", path)
	}
	return nil
}

func ensurePrivateDirectory(path string) error {
	if err := os.Mkdir(path, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
		return fmt.Errorf("maildir: create quarantine: %w", err)
	}
	return validatePrivateDirectory(path)
}

func readOwnershipMarker(path string) ([]byte, error) {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, fmt.Errorf("maildir: open ownership marker: %w", err)
	}
	file := os.NewFile(uintptr(fd), path)
	defer func() { _ = file.Close() }() // read-only handle; close error not actionable
	info, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("maildir: stat ownership marker: %w", err)
	}
	if !info.Mode().IsRegular() || info.Size() > 1024 {
		return nil, errors.New("maildir: ownership marker must be a small regular file")
	}
	marker, err := io.ReadAll(io.LimitReader(file, 1025))
	if err != nil {
		return nil, fmt.Errorf("maildir: read ownership marker: %w", err)
	}
	return marker, nil
}

type lockHandle interface {
	Close() error
}

type posixLock struct {
	file *os.File
}

func openOwnershipLock(path string) (lockHandle, error) {
	fd, err := unix.Open(path, unix.O_RDWR|unix.O_CREAT|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0o600)
	if err != nil {
		return nil, fmt.Errorf("maildir: open ownership lock: %w", err)
	}
	file := os.NewFile(uintptr(fd), path)
	if err := unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB); err != nil {
		_ = file.Close()
		if errors.Is(err, unix.EWOULDBLOCK) || errors.Is(err, unix.EAGAIN) {
			return nil, errors.New("maildir: already locked by another MXHook process")
		}
		return nil, fmt.Errorf("maildir: lock ownership: %w", err)
	}
	return &posixLock{file: file}, nil
}

func (l *posixLock) Close() error {
	unlockErr := unix.Flock(int(l.file.Fd()), unix.LOCK_UN)
	closeErr := l.file.Close()
	return errors.Join(unlockErr, closeErr)
}

func openClaimedFile(path string, expected os.FileInfo) (*os.File, error) {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, fmt.Errorf("open claimed file: %w", err)
	}
	file := os.NewFile(uintptr(fd), path)
	opened, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("stat claimed file: %w", err)
	}
	if !opened.Mode().IsRegular() || !os.SameFile(expected, opened) {
		_ = file.Close()
		return nil, errors.New("claimed file changed before open")
	}
	return file, nil
}
