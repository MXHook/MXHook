//go:build aix || darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris

package maildir

import (
	"errors"
	"fmt"
	"os"
)

// linkUnlinkNoReplace emulates a no-replace rename: hard-linking fails with
// EEXIST when the target already exists, then the source link is removed.
//
// Crash window: a crash between the link and the unlink leaves the entry
// hard-linked at both paths (for a claim: in both new/ and cur/). The claimed
// copy is processed normally; the stale new/ copy then fails its claim with
// EEXIST every scan until removed by an operator.
//
// Directories cannot be hard-linked (EPERM), so they move via a
// check-then-rename that is not atomic: a concurrent writer could create the
// target between the check and the rename and be overwritten. Directories
// only travel this path on their way to quarantine, so the window is
// acceptable.
func linkUnlinkNoReplace(source, target string) error {
	info, err := os.Lstat(source)
	if err != nil {
		return fmt.Errorf("lstat source: %w", err)
	}
	if info.IsDir() {
		if _, err := os.Lstat(target); err == nil {
			return &os.LinkError{Op: "rename", Old: source, New: target, Err: os.ErrExist}
		} else if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("lstat target: %w", err)
		}
		if err := os.Rename(source, target); err != nil {
			return fmt.Errorf("rename directory: %w", err)
		}
		return nil
	}
	if err := os.Link(source, target); err != nil {
		return fmt.Errorf("link: %w", err)
	}
	if err := os.Remove(source); err != nil {
		removeErr := os.Remove(target)
		return errors.Join(err, removeErr)
	}
	return nil
}
