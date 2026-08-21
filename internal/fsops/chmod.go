package fsops

import (
	"fmt"
	"os"
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
