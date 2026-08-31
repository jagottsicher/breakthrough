package fsops

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
)

// ParseMode parses s as an octal permission string the way chmod(1)
// accepts it, e.g. "755" or "0644". It deliberately only accepts the
// standard 9 permission bits (0-0777), not chmod's symbolic form
// (u+x, go-w, ...) or the setuid/setgid/sticky digit — both are later
// enhancements, not needed for a first working version. The setuid/
// setgid/sticky bits specifically are left out rather than approximated:
// os.FileMode represents them at different bit positions than their
// traditional octal values (os.ModeSetuid, not literal 04000), so
// accepting a 4-digit value here without that translation would silently
// set the wrong thing.
func ParseMode(s string) (os.FileMode, error) {
	n, err := strconv.ParseUint(s, 8, 32)
	if err != nil {
		return 0, fmt.Errorf("fsops: invalid permission %q, expected octal like 755", s)
	}
	if n > 0o777 {
		return 0, fmt.Errorf("fsops: invalid permission %q: setuid/setgid/sticky bits aren't supported yet, use 3 octal digits (0-777)", s)
	}
	return os.FileMode(n), nil
}

// Chmod sets path's permission bits to mode (see ParseMode).
func Chmod(path string, mode os.FileMode) error {
	return os.Chmod(path, mode)
}

// ChmodDirsRecursive sets mode on path and every directory nested beneath
// it, leaving regular files and everything else untouched — exactly the
// set find(1)'s own -type d matches while walking path, applied the same
// way `find path -type d -exec chmod mode {} +` would. See
// ChmodFilesRecursive for the "-type f" counterpart, and ChownRecursive
// for this same walk-and-apply shape used for Owner/Group instead of
// permissions.
//
// Like ChownRecursive, this stops at the first error instead of
// continuing past it — whatever already got chmod'd stays changed, no
// automatic rollback.
func ChmodDirsRecursive(path string, mode os.FileMode) error {
	return filepath.WalkDir(path, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			return nil
		}
		return Chmod(p, mode)
	})
}

// ChmodFilesRecursive sets mode on every regular file nested beneath
// path, leaving directories and anything else (symlinks, sockets, ...)
// untouched — find(1)'s own -type f, applied the same way
// `find path -type f -exec chmod mode {} +` would. A symlink is left
// alone even if it points at a regular file, matching find's own
// default (no -L) of never following one during the walk in the first
// place. See ChmodDirsRecursive for the "-type d" counterpart.
func ChmodFilesRecursive(path string, mode os.FileMode) error {
	return filepath.WalkDir(path, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		return Chmod(p, mode)
	})
}
