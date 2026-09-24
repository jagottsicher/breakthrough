package activitylog

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/jagottsicher/breakthrough/internal/session"
)

// SystemLogDir is the real, syslog-style location ResolvePath tries
// first — reviewable with the same tools (tail -f, grep, logrotate) as
// any other system log, matching what a sysadmin running breakthrough
// as root or via sudo (this project's own actual target audience —
// see the project's own doc comment) would already expect. A var, not
// a const, and exported — the same "swappable for a test" idiom this
// project's own internal/firewall (runUFWStatus and friends) already
// uses, so a test, in this package or internal/ui, can point this at a
// temp directory (or a deliberately unwritable one) instead of ever
// touching the real /var/log, regardless of whether the test process
// itself happens to be running as root.
var SystemLogDir = "/var/log/breakthrough"

// logFileName is the one file this package ever writes, under
// whichever directory ResolvePath settles on.
const logFileName = "breakthrough.log"

// ResolvePath decides where the activity log file actually lives:
// SystemLogDir if it already exists (or can be created) and is
// genuinely writable, or — the far more common case for an ordinary,
// unprivileged user — the same per-user directory this app's own
// crash log already uses (see internal/session.StateDir), reported via
// fellBack so a caller can mention the fallback once rather than stay
// silent about where the user's own logs actually ended up.
func ResolvePath() (path string, fellBack bool, err error) {
	if p, ok := tryLogDir(SystemLogDir); ok {
		return p, false, nil
	}
	dir := session.StateDir()
	if dir == "" {
		return "", true, fmt.Errorf("activitylog: neither %s nor a per-user state directory is available", SystemLogDir)
	}
	if p, ok := tryLogDir(dir); ok {
		return p, true, nil
	}
	return "", true, fmt.Errorf("activitylog: %s is not writable", dir)
}

// tryLogDir reports whether dir can actually hold the log file:
// creating it first if it doesn't exist yet (0755, matching this app's
// other self-created directories, e.g. the Trash), then proving it's
// genuinely writable by opening — and immediately removing — a real
// probe file. Permission bits alone are checked nowhere here: they can
// report a directory as writable when it genuinely isn't (an ACL, a
// read-only bind mount, SELinux), which would otherwise turn "logging
// is on" into a silent no-op the first time something actually tries
// to log.
func tryLogDir(dir string) (path string, ok bool) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", false
	}
	probe := filepath.Join(dir, ".breakthrough-writable-probe")
	f, err := os.OpenFile(probe, os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return "", false
	}
	_ = f.Close()
	_ = os.Remove(probe)
	return filepath.Join(dir, logFileName), true
}

// Writer is an activity log file already open for appending — the one
// real side effect this package performs, kept as small and separate
// from Logger's own filtering logic as possible so either half can be
// tested without the other.
type Writer struct {
	f *os.File
}

// OpenWriter opens (creating if necessary) the file at path for
// appending — one real log file, never truncated or rotated by this
// package itself: log rotation is exactly what a real system's own
// logrotate already does for anything under /var/log, and duplicating
// that here would just be a second, competing implementation of
// something the OS already provides for free once the file lives in a
// real log directory.
func OpenWriter(path string) (*Writer, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, err
	}
	return &Writer{f: f}, nil
}

// WriteEntry appends e as one real line — see Entry.Format's own doc
// comment for the exact shape.
func (w *Writer) WriteEntry(e Entry) error {
	_, err := w.f.WriteString(e.Format())
	return err
}

// Close closes the underlying file — safe to call on a nil *Writer (a
// no-op), the same "nil-safe, no special-casing needed at the call
// site" convention Logger's own methods already follow.
func (w *Writer) Close() error {
	if w == nil || w.f == nil {
		return nil
	}
	return w.f.Close()
}
