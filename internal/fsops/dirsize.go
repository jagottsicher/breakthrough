package fsops

import (
	"os/exec"
	"strconv"
	"strings"
)

// DirSize runs `du -sk dir` and returns its own total, in bytes.
//
// Deliberately not `du -sh`: -h's own human-readable formatting is
// locale-dependent, the exact same reason FetchDiskUsage requests `df
// -k`/`df -i` instead of `df -h` — see its own doc comment. Requesting
// raw 1024-byte blocks via -k and letting internal/ui's own humanSize
// format the result instead sidesteps that, and gets an exact byte count
// for free rather than needing to reverse a rounded, unit-suffixed
// string.
//
// A non-zero exit from du is deliberately not treated as failure on its
// own: du still exits non-zero the moment it hits even one unreadable
// subdirectory along the way (a permission-denied entry somewhere deep
// in a real home directory is common, not exceptional), but it still
// prints a valid total for everything it *could* read on its way out —
// exactly the common case this is for. Discarding that total over one
// unreadable subdirectory, the way dfLastLine's own all-or-nothing
// handling does for df, would make this fail constantly on exactly the
// directory trees a user is most likely to actually want a size for.
// Only a du that produced no parseable output at all — it never started
// (not installed), or crashed before printing anything — is reported as
// failure here.
func DirSize(dir string) (bytes int64, ok bool) {
	out, err := exec.Command("du", "-sk", dir).Output()
	if err != nil {
		if _, isExitErr := err.(*exec.ExitError); !isExitErr {
			// du didn't even run (not on PATH, permission to exec it
			// denied, ...) — no partial stdout exists to fall back to.
			return 0, false
		}
		// A non-zero exit with du specifically: fall through and try to
		// parse whatever it did manage to write to stdout — see this
		// func's own doc comment.
	}

	fields := strings.Fields(string(out))
	if len(fields) == 0 {
		return 0, false
	}
	kb, err := strconv.ParseInt(fields[0], 10, 64)
	if err != nil {
		return 0, false
	}
	return kb * 1024, true
}
