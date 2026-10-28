//go:build darwin

package maildir

import "golang.org/x/sys/unix"

// renameNoReplaceNative is a variable to allow tests to exercise the
// unsupported-filesystem fallback dispatch without an unsupported filesystem.
var renameNoReplaceNative = func(source, target string) error {
	return unix.RenameatxNp(unix.AT_FDCWD, source, unix.AT_FDCWD, target, unix.RENAME_EXCL)
}
