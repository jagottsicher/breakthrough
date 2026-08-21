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
	LinkTarget string // only set if IsSymlink; the raw, unresolved link text
	LinkBroken bool   // only meaningful if IsSymlink: the target doesn't resolve
	LinkIsDir  bool   // only meaningful if IsSymlink && !LinkBroken: the fully-resolved target is a directory
	Mode       os.FileMode
	Size       int64
	ModTime    time.Time
	Owner      string // falls back to the numeric uid (as a string) if unresolved
	Group      string // falls back to the numeric gid (as a string) if unresolved
	UID        int    // the raw uid/gid Owner/Group were resolved from — e.g. for centering the owner/group picker on the current value
	GID        int
	Nlink      uint64 // hard-link count of path itself (not a symlink's target)
	MountPoint bool   // true if this is a directory (or a symlink resolving to one) on a different filesystem than its parent
}

// Stat gathers Info for path. It uses Lstat, so a symlink is reported as
// itself (IsSymlink true, LinkTarget set, Size/Mode/Nlink describing the
// link, not its target) rather than being followed — except for
// LinkBroken/LinkIsDir/MountPoint, which specifically need to know what
// the link resolves to and say so explicitly in their own names.
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
		Nlink:   nlinkOf(fi),
	}

	checkMount := fi.IsDir()

	if fi.Mode()&os.ModeSymlink != 0 {
		info.IsSymlink = true
		if target, err := os.Readlink(path); err == nil {
			info.LinkTarget = target
		}
		if resolved, err := os.Stat(path); err != nil {
			info.LinkBroken = true
		} else {
			info.LinkIsDir = resolved.IsDir()
			checkMount = resolved.IsDir()
		}
	}

	if checkMount {
		info.MountPoint = isMountPoint(path)
	}

	info.Owner, info.Group, info.UID, info.GID = ownerGroup(fi)

	return info, nil
}

// ownerGroup resolves fi's owner/group names via the platform's syscall
// stat structure, falling back to the numeric id (as a string) if that
// data isn't available or the name lookup fails — e.g. a uid/gid with no
// matching passwd/group entry.
func ownerGroup(fi os.FileInfo) (owner, group string, uidNum, gidNum int) {
	stat, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return "", "", 0, 0
	}

	uidNum = int(stat.Uid)
	uid := strconv.FormatUint(uint64(stat.Uid), 10)
	owner = uid
	if u, err := user.LookupId(uid); err == nil {
		owner = u.Username
	}

	gidNum = int(stat.Gid)
	gid := strconv.FormatUint(uint64(stat.Gid), 10)
	group = gid
	if g, err := user.LookupGroupId(gid); err == nil {
		group = g.Name
	}

	return owner, group, uidNum, gidNum
}
