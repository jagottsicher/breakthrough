package fsops

import (
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// DirSize runs `du -sk` against dir and returns its own total, in
// bytes, alongside the path actually measured.
//
// dir is resolved through any symlinks first (see filepath.EvalSymlinks,
// which follows an entire chain, not just the first hop), and measured
// is that resolved path — which differs from dir exactly when dir was a
// symlink, and is what the caller should show so the number is never
// attributed to the wrong directory. Resolving matters because `du` on a
// symlink reports the link itself, a handful of bytes, rather than
// anything about what it points at: for a directory symlink, that
// answer is never the one being asked for. Per the user's own explicit
// request, including the multi-hop case a chain of links produces.
//
// A mount point needs no equivalent handling: du descends into a mounted
// filesystem by default (it's --one-file-system that would stop it, and
// that's deliberately not passed), so the files actually stored there
// are already counted.
//
// Symlinks *inside* the tree are still not followed — that's du's own
// default, and matches what `du -hs` reports at a shell prompt, which is
// the number this is meant to reproduce.
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
func DirSize(dir string) (bytes int64, measured string, ok bool) {
	// A broken link, or one pointing outside anything readable, can't be
	// measured at all — report failure rather than falling back to
	// measuring the link itself, which would produce a confidently wrong
	// handful of bytes.
	measured, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return 0, dir, false
	}

	out, err := exec.Command("du", "-sk", measured).Output()
	if err != nil {
		if _, isExitErr := err.(*exec.ExitError); !isExitErr {
			// du didn't even run (not on PATH, permission to exec it
			// denied, ...) — no partial stdout exists to fall back to.
			return 0, measured, false
		}
		// A non-zero exit with du specifically: fall through and try to
		// parse whatever it did manage to write to stdout — see this
		// func's own doc comment.
	}

	fields := strings.Fields(string(out))
	if len(fields) == 0 {
		return 0, measured, false
	}
	kb, err := strconv.ParseInt(fields[0], 10, 64)
	if err != nil {
		return 0, measured, false
	}
	return kb * 1024, measured, true
}
