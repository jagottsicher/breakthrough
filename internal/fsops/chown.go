//go:build unix

package fsops

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
)

// ParseOwnerGroup parses s in chown(1)'s own "owner[:group]" or ":group"
// syntax. Each half accepts either a name (resolved via os/user) or a
// bare numeric id — the same fallback Stat's ownerGroup uses in reverse
// when a name can't be looked up. The returned uid/gid is -1 for whichever
// half wasn't given, matching os.Chown's own "leave this one unchanged"
// convention, so ParseOwnerGroup's result can be passed straight to
// Chown.
func ParseOwnerGroup(s string) (uid, gid int, err error) {
	uid, gid = -1, -1

	ownerPart, groupPart, hasGroup := strings.Cut(s, ":")
	if ownerPart == "" && (!hasGroup || groupPart == "") {
		return -1, -1, fmt.Errorf("fsops: empty owner/group")
	}

	if ownerPart != "" {
		if uid, err = ResolveUID(ownerPart); err != nil {
			return -1, -1, err
		}
	}
	if hasGroup && groupPart != "" {
		if gid, err = ResolveGID(groupPart); err != nil {
			return -1, -1, err
		}
	}

	return uid, gid, nil
}

// ResolveUID resolves s — a username (via os/user) or a bare numeric
// uid — to a uid. Exported for callers that only have one half of
// chown(1)'s "owner[:group]" syntax to resolve, e.g. the Properties
// overlay's separately-editable Owner field (see ui.Root's
// openOwnerGroupPicker) — ParseOwnerGroup itself is built on top of this
// and ResolveGID.
func ResolveUID(s string) (int, error) {
	if u, err := user.Lookup(s); err == nil {
		return strconv.Atoi(u.Uid)
	}
	if uid, err := strconv.Atoi(s); err == nil {
		return uid, nil
	}
	return 0, fmt.Errorf("fsops: unknown user %q", s)
}

// ResolveGID is ResolveUID's own counterpart for group names/gids.
func ResolveGID(s string) (int, error) {
	if g, err := user.LookupGroup(s); err == nil {
		return strconv.Atoi(g.Gid)
	}
	if gid, err := strconv.Atoi(s); err == nil {
		return gid, nil
	}
	return 0, fmt.Errorf("fsops: unknown group %q", s)
}

// Chown changes path's owner and/or group (see ParseOwnerGroup). uid or
// gid of -1 leaves that half unchanged.
func Chown(path string, uid, gid int) error {
	return os.Chown(path, uid, gid)
}

// ChownRecursive is Chown, applied to path itself and everything inside
// it — the Properties overlay's own "apply to this folder's whole tree"
// toggle for Owner/Group, per the user's own explicit request, rather
// than just the directory entry itself. Stops at the first error rather
// than continuing past it: whatever already succeeded stays changed,
// the same "no automatic rollback" approach savePropertiesEdit's own
// doc comment already documents for a save that fails partway through.
// A symlink is never descended into as if it were a directory (WalkDir's
// own traversal only ever recurses into a real subdirectory), but the
// symlink entry itself still gets a plain Chown call like everything
// else here — which, like a single, non-recursive Chown/os.Chown call
// on a symlink, follows it and changes its target's ownership, not the
// link's own. A dangling symlink nested anywhere in the tree therefore
// makes the whole walk fail once it's reached, the same as chown(1)
// itself would on a broken link's target.
func ChownRecursive(path string, uid, gid int) error {
	return filepath.WalkDir(path, func(p string, _ os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		return Chown(p, uid, gid)
	})
}
