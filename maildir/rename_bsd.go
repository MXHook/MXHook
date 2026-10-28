//go:build aix || dragonfly || freebsd || netbsd || openbsd || solaris

package maildir

func renameNoReplace(source, target string) error {
	return linkUnlinkNoReplace(source, target)
}
