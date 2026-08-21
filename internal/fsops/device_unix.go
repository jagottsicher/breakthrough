//go:build unix

package fsops

import (
	"os"
	"path/filepath"
	"syscall"
)

// nlinkOf returns fi's hard-link count, or 0 if the platform's stat
// structure isn't available (shouldn't happen on any unix target this
// project builds for). The underlying field's width varies by platform
// (e.g. uint16 on Darwin, uint64 on Linux), hence the explicit
// conversion rather than assuming one.
func nlinkOf(fi os.FileInfo) uint64 {
	stat, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return 0
	}
	return uint64(stat.Nlink)
}

// deviceOf returns path's filesystem device id, following symlinks, or
// ok=false if path can't be stat'd or the platform's stat structure isn't
// available.
func deviceOf(path string) (dev uint64, ok bool) {
	fi, err := os.Stat(path)
	if err != nil {
		return 0, false
	}
	stat, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, false
	}
	return uint64(stat.Dev), true
}

// mountPointVia reports whether entryPath's device differs from
// parentDev — the shared comparison behind isMountPoint (which looks up
// parentDev itself, for a single check) and ListDir (which looks up its
// listed directory's device once up front, for every entry in it, rather
// than re-deriving it per entry). haveParentDev=false (parentDev
// couldn't be determined) and any failure to stat entryPath both report
// false — not a mount point — rather than risk a false positive.
func mountPointVia(entryPath string, parentDev uint64, haveParentDev bool) bool {
	if !haveParentDev {
		return false
	}
	entryDev, ok := deviceOf(entryPath)
	if !ok {
		return false
	}
	return entryDev != parentDev
}

// isMountPoint reports whether path (a directory, or a symlink resolving
// to one) sits on a different filesystem than its own parent directory —
// e.g. a separate partition, an NFS share, or an fstab bind mount. Used
// by Stat, which only ever checks one path at a time; ListDir uses
// mountPointVia directly instead, to share one parent lookup across every
// entry in the directory rather than repeating it per entry.
func isMountPoint(path string) bool {
	parentDev, ok := deviceOf(filepath.Dir(path))
	return mountPointVia(path, parentDev, ok)
}
