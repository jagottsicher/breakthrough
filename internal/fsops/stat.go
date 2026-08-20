//go:build unix

package fsops

import (
	"os"
	"os/user"
	"strconv"
	"syscall"
	"time"
)

// Info describes everything breakthrough currently knows how to show
// about one file or directory — roughly what `ls -halF` prints for a
// single entry, gathered natively via Lstat/syscall instead of shelling
// out to and parsing ls (see the Phase 1 design discussion: generic
// command-output parsing doesn't scale and isn't needed for this).
type Info struct {
	Name       string
	Path       string
	IsDir      bool
	IsSymlink  bool
	LinkTarget string // only set if IsSymlink
	Mode       os.FileMode
	Size       int64
	ModTime    time.Time
	Owner      string // falls back to the numeric uid (as a string) if unresolved
	Group      string // falls back to the numeric gid (as a string) if unresolved
}

// Stat gathers Info for path. It uses Lstat, so a symlink is reported as
// itself (IsSymlink true, LinkTarget set) rather than being followed.
func Stat(path string) (Info, error) {
	fi, err := os.Lstat(path)
	if err != nil {
		return Info{}, err
	}

	info := Info{
		Name:    fi.Name(),
		Path:    path,
		IsDir:   fi.IsDir(),
		Mode:    fi.Mode(),
		Size:    fi.Size(),
		ModTime: fi.ModTime(),
	}

	if fi.Mode()&os.ModeSymlink != 0 {
		info.IsSymlink = true
		if target, err := os.Readlink(path); err == nil {
			info.LinkTarget = target
		}
	}

	info.Owner, info.Group = ownerGroup(fi)

	return info, nil
}

// ownerGroup resolves fi's owner/group names via the platform's syscall
// stat structure, falling back to the numeric id (as a string) if that
// data isn't available or the name lookup fails — e.g. a uid/gid with no
// matching passwd/group entry.
func ownerGroup(fi os.FileInfo) (owner, group string) {
	stat, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return "", ""
	}

	uid := strconv.FormatUint(uint64(stat.Uid), 10)
	owner = uid
	if u, err := user.LookupId(uid); err == nil {
		owner = u.Username
	}

	gid := strconv.FormatUint(uint64(stat.Gid), 10)
	group = gid
	if g, err := user.LookupGroupId(gid); err == nil {
		group = g.Name
	}

	return owner, group
}
