//go:build linux || darwin

package maildir

import (
	"errors"

	"golang.org/x/sys/unix"
)

// renameNoReplace atomically moves source to target without overwriting an
// existing target. Filesystems without no-replace rename support (older Linux
// kernels, NFS before 4.2, SMB, some FUSE and bind mounts) report EINVAL,
// ENOSYS, or ENOTSUP; those fall back to a link+unlink sequence that keeps the
// no-overwrite guarantee.
func renameNoReplace(source, target string) error {
	err := renameNoReplaceNative(source, target)
	if err == nil || !renameNoReplaceUnsupported(err) {
		return err
	}
	return linkUnlinkNoReplace(source, target)
}

func renameNoReplaceUnsupported(err error) bool {
	return errors.Is(err, unix.EINVAL) || errors.Is(err, unix.ENOSYS) || errors.Is(err, unix.ENOTSUP)
}
