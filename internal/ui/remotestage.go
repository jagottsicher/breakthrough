// remotestage.go holds the one thing Look, Edit, and remote archive
// browsing all turn out to need in common: a real local path standing
// in for a file that only exists on the other end of a connection.
// None of viewer.Load, an external $EDITOR/$PAGER, or internal/archive
// has any notion of a remotefs.Client — each of them only ever knows
// how to open a real path on this machine — so a remote file has to be
// staged into one before any of that existing, unmodified local logic
// can run against it at all.
package ui

import (
	"io"
	"os"
	"path"

	"github.com/jagottsicher/breakthrough/internal/remotefs"
)

// downloadRemoteToTemp streams remotePath's own content into a fresh
// local temp file and reports its path. The temp file's own name keeps
// remotePath's extension (via os.CreateTemp's own "*" placement,
// substituted in the middle rather than dropped at the end) since
// several things downstream key off it: viewer.Sniff/Load falls back
// to an extension check for a format it can't otherwise decode, syntax
// highlighting picks a language by extension, and an external editor
// or pager typically does too.
//
// cleanup removes the temp file and is always non-nil, even on error —
// every caller defers it (or calls it explicitly once done), the same
// "always safe to call, no nil-check needed" contract this project's
// other optional-cleanup functions already follow.
func downloadRemoteToTemp(remote remotefs.Client, remotePath string) (localPath string, cleanup func(), err error) {
	noop := func() {}

	rc, err := remote.Open(remotePath)
	if err != nil {
		return "", noop, err
	}
	defer func() { _ = rc.Close() }()

	f, err := os.CreateTemp("", "breakthrough-remote-*-"+path.Base(remotePath))
	if err != nil {
		return "", noop, err
	}
	cleanup = func() { _ = os.Remove(f.Name()) }

	if _, err := io.Copy(f, rc); err != nil {
		_ = f.Close()
		cleanup()
		return "", noop, err
	}
	if err := f.Close(); err != nil {
		cleanup()
		return "", noop, err
	}
	return f.Name(), cleanup, nil
}

// cleanupViewerRemoteTempFile removes whatever temp file Look staged a
// remote PDF into, if any — a no-op if there isn't one right now (the
// ordinary, by far most common case: no Look open at all, or a local/
// non-PDF remote Look, neither of which ever sets viewerRemoteTempFile
// in the first place — see its own doc comment on the Root struct).
func (r *Root) cleanupViewerRemoteTempFile() {
	if r.viewerRemoteTempFile == "" {
		return
	}
	_ = os.Remove(r.viewerRemoteTempFile)
	r.viewerRemoteTempFile = ""
}
