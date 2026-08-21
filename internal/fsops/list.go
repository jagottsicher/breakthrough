//go:build unix

package fsops

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// EntryType classifies what kind of filesystem object an Entry is — more
// than a plain file/directory split, so the UI's type-prefix column (see
// ui.typeGlyph) can tell a directory from a directory symlink from a
// broken symlink from the rarer special files, the way `ls -F`/Midnight
// Commander's classify-by-prefix convention does.
type EntryType int

const (
	TypeFile EntryType = iota
	TypeDir
	TypeSymlinkFile   // valid symlink, resolves (transitively) to a file
	TypeSymlinkDir    // valid symlink, resolves (transitively) to a directory
	TypeSymlinkBroken // symlink whose target doesn't resolve
	TypeSocket
	TypeFIFO
	TypeCharDevice
	TypeBlockDevice
)

// Entry describes one item in a directory listing.
type Entry struct {
	Name string
	Type EntryType

	// IsDir says whether Enter/double-click should navigate into this
	// entry rather than treat it as a file — true for TypeDir and, since
	// a directory symlink behaves the same way to navigate into, also for
	// TypeSymlinkDir. This used to only reflect the entry itself (never
	// true for any symlink); now that ListDir resolves symlinks to
	// classify them anyway, there's no reason to still treat a directory
	// symlink as unnavigable.
	IsDir bool

	// LinkTarget is the entry's own raw, unresolved link text (whatever
	// os.Readlink returns — relative paths are not made absolute), set
	// only for the three symlink Types. Kept raw rather than resolved:
	// this is what the list shows inline next to the name (see
	// ui.typeGlyph's caller in addRow), matching `ls -l`'s own
	// "name -> target" convention, which shows the same raw text.
	LinkTarget string

	// Nlink is the entry's hard-link count. >1 on a TypeFile means this
	// content also exists under at least one other name somewhere on the
	// same filesystem — not otherwise discoverable without a full
	// filesystem scan, but the count alone is enough to flag it.
	Nlink uint64

	// MountPoint is true if this directory sits on a different
	// filesystem than the directory being listed — e.g. a separate
	// partition, an NFS share, or an fstab bind mount. Only meaningful
	// when Type is TypeDir or TypeSymlinkDir.
	MountPoint bool

	// Mode is the entry's own permission/mode bits (Lstat's, not a
	// symlink's target). Currently only used to detect the executable
	// bit for TypeFile — see ui.typeGlyph's '*', matching `ls -F`/
	// Midnight Commander's own convention for an executable file.
	Mode os.FileMode
}

// ListDir returns the entries of the directory at path, sorted with
// directories (including directory symlinks — see Entry.IsDir) first,
// then alphabetically (case-insensitive) within each group. It does not
// recurse.
func ListDir(path string) ([]Entry, error) {
	dirEntries, err := os.ReadDir(path)
	if err != nil {
		return nil, err
	}

	// Looked up once for the whole directory, not per entry — every
	// entry's own mount-point check (see describeEntry) compares against
	// this same device id, so there's no reason to re-stat path itself
	// once per entry.
	parentDev, haveParentDev := deviceOf(path)

	// keyedEntry pairs each Entry with its lowercase sort key, computed
	// once up front rather than inside the sort.Slice comparator below
	// (which runs O(n log n) times) — for a directory with tens of
	// thousands of entries, recomputing strings.ToLower per comparison
	// means millions of redundant allocations for no benefit. Sorting a
	// slice of this combined type (instead of Entry plus a parallel key
	// slice) keeps each key attached to its Entry through every swap;
	// sort.Slice only knows how to swap elements of the one slice it's
	// given, so a separate parallel slice would silently desync.
	type keyedEntry struct {
		Entry
		key string
	}

	keyed := make([]keyedEntry, len(dirEntries))
	for i, de := range dirEntries {
		name := de.Name()
		entryPath := filepath.Join(path, name)
		keyed[i] = keyedEntry{
			Entry: describeEntry(entryPath, name, parentDev, haveParentDev),
			key:   strings.ToLower(name),
		}
	}

	sort.Slice(keyed, func(i, j int) bool {
		if keyed[i].IsDir != keyed[j].IsDir {
			return keyed[i].IsDir // directories before files
		}
		return keyed[i].key < keyed[j].key
	})

	entries := make([]Entry, len(keyed))
	for i, k := range keyed {
		entries[i] = k.Entry
	}

	return entries, nil
}

// describeEntry classifies one directory entry at entryPath (see
// EntryType), given the device id of the directory being listed
// (parentDev/haveParentDev, from deviceOf) for the mount-point check.
func describeEntry(entryPath, name string, parentDev uint64, haveParentDev bool) Entry {
	fi, err := os.Lstat(entryPath)
	if err != nil {
		// Vanished between ReadDir and Lstat (a real race, however
		// unlikely) — report it as a plain file rather than fail the
		// whole listing over one entry.
		return Entry{Name: name, Type: TypeFile}
	}

	nlink := nlinkOf(fi)
	mode := fi.Mode()

	if mode&os.ModeSymlink != 0 {
		target, _ := os.Readlink(entryPath) // best-effort; empty on failure, which shouldn't happen right after a successful Lstat

		resolved, err := os.Stat(entryPath) // follows the whole chain
		if err != nil {
			return Entry{Name: name, Type: TypeSymlinkBroken, LinkTarget: target, Nlink: nlink, Mode: mode}
		}
		if resolved.IsDir() {
			mount := mountPointVia(entryPath, parentDev, haveParentDev)
			return Entry{Name: name, Type: TypeSymlinkDir, IsDir: true, LinkTarget: target, Nlink: nlink, MountPoint: mount, Mode: mode}
		}
		return Entry{Name: name, Type: TypeSymlinkFile, LinkTarget: target, Nlink: nlink, Mode: mode}
	}

	switch {
	case fi.IsDir():
		mount := mountPointVia(entryPath, parentDev, haveParentDev)
		return Entry{Name: name, Type: TypeDir, IsDir: true, Nlink: nlink, MountPoint: mount, Mode: mode}
	case mode&os.ModeSocket != 0:
		return Entry{Name: name, Type: TypeSocket, Nlink: nlink, Mode: mode}
	case mode&os.ModeNamedPipe != 0:
		return Entry{Name: name, Type: TypeFIFO, Nlink: nlink, Mode: mode}
	case mode&os.ModeDevice != 0 && mode&os.ModeCharDevice != 0:
		return Entry{Name: name, Type: TypeCharDevice, Nlink: nlink, Mode: mode}
	case mode&os.ModeDevice != 0:
		return Entry{Name: name, Type: TypeBlockDevice, Nlink: nlink, Mode: mode}
	default:
		return Entry{Name: name, Type: TypeFile, Nlink: nlink, Mode: mode}
	}
}
