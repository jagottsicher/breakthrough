//go:build unix

package fsops

import (
	"fmt"
	"os"
	"os/user"
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
		if uid, err = resolveUID(ownerPart); err != nil {
			return -1, -1, err
		}
	}
	if hasGroup && groupPart != "" {
		if gid, err = resolveGID(groupPart); err != nil {
			return -1, -1, err
		}
	}

	return uid, gid, nil
}

func resolveUID(s string) (int, error) {
	if u, err := user.Lookup(s); err == nil {
		return strconv.Atoi(u.Uid)
	}
	if uid, err := strconv.Atoi(s); err == nil {
		return uid, nil
	}
	return 0, fmt.Errorf("fsops: unknown user %q", s)
}

func resolveGID(s string) (int, error) {
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
