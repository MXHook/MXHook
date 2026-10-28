//go:build !aix && !darwin && !dragonfly && !freebsd && !linux && !netbsd && !openbsd && !solaris

package maildir

import (
	"errors"
	"os"
)

type lockHandle interface {
	Close() error
}

func openOwnershipLock(string) (lockHandle, error) {
	return nil, errors.New("maildir: ingestion requires a supported Unix-like operating system")
}

func openClaimedFile(string, os.FileInfo) (*os.File, error) {
	return nil, errors.New("maildir: ingestion requires a supported Unix-like operating system")
}

func validatePrivateDirectory(string) error {
	return errors.New("maildir: ingestion requires a supported Unix-like operating system")
}

func ensurePrivateDirectory(string) error {
	return errors.New("maildir: ingestion requires a supported Unix-like operating system")
}

func readOwnershipMarker(string) ([]byte, error) {
	return nil, errors.New("maildir: ingestion requires a supported Unix-like operating system")
}

func renameNoReplace(string, string) error {
	return errors.New("maildir: ingestion requires a supported Unix-like operating system")
}
