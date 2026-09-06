package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/jagottsicher/breakthrough/internal/session"
)

// DebugDir is where this app's own crash/debug logs live —
// $XDG_STATE_HOME/breakthrough, falling back to
// ~/.local/state/breakthrough if that's unset. The XDG State directory
// is the right one for exactly this kind of thing (logs, not
// configuration or cache) — matching this project's own existing
// $XDG_CONFIG_HOME convention for settings (see internal/config).
// Exported so cmd/breakthrough's own --debug flag (see its own doc
// comment) can put its stderr-redirect log in the same place as
// crashLogPath below, without duplicating this same resolution logic a
// second time. Best-effort: "" if even the fallback can't be resolved
// (no $HOME either).
//
// A thin alias for session.StateDir rather than its own copy of that
// resolution: the saved tab layout (see session.SaveTabs) needs the very
// same directory, and two independently maintained versions of "where
// does this app's own state live" is exactly the kind of thing that
// silently drifts apart later.
func DebugDir() string {
	return session.StateDir()
}

// crashLogPath is where safeGo's own recovered panics get logged — see
// DebugDir's own doc comment for how that directory is resolved.
// Best-effort: "" if DebugDir can't be resolved, in which case logCrash
// just skips writing to a file — a recovered panic is still reported
// through the error overlay regardless (see safeGo).
func crashLogPath() string {
	dir := DebugDir()
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, "crash.log")
}

// logCrash appends one crash record to crashLogPath — a timestamp, name
// (see safeGo's own doc comment), the recovered panic value, and a full
// stack trace of every currently running goroutine (runtime.Stack's own
// "all" flag, not just the panicking one). Best-effort and silent about
// its own failure: a crash log write failing is not itself something
// worth crashing over, especially not from inside a panic recovery path
// already.
func logCrash(name string, rec any, stack []byte) {
	path := crashLogPath()
	if path == "" {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer func() { _ = f.Close() }()
	_, _ = fmt.Fprintf(f, "=== %s: panic in %q: %v ===\n%s\n\n", time.Now().Format(time.RFC3339), name, rec, stack) // a crash log write failing is a best-effort no-op — see this func's own doc comment
}

// allStacks dumps every currently running goroutine's own stack —
// runtime.Stack(buf, true), grown until the whole dump fits, since
// there's no way to know the right buffer size up front.
func allStacks() []byte {
	buf := make([]byte, 1<<16)
	for {
		n := runtime.Stack(buf, true)
		if n < len(buf) {
			return buf[:n]
		}
		buf = make([]byte, 2*len(buf))
	}
}

// safeGo runs fn in its own goroutine, recovering any panic inside it
// instead of letting it take the whole process down — a real,
// hand-verified failure mode this fixes: a panic in a goroutine this
// app spawns itself (background hash computation, a search/sed/tool-
// window worker, the status clock, ...) runs entirely outside
// tview.Application.Run's own call stack, so tview's own top-level
// recover — which restores the terminal (screen.Fini()) before
// re-panicking, verified directly against its own application.go — never
// even sees it: the Go runtime kills the *whole process* immediately, on
// whichever goroutine actually panicked, without restoring the terminal
// at all. Confirmed by hand with a small reproduction: the terminal is
// left showing stale, frozen TUI content overlapping the shell prompt
// afterward, with the real panic message and full stack easy to miss
// entirely in that mess even though they were fully printed to stderr —
// which matches a real, reported crash that came back as only one,
// seemingly truncated stack frame.
//
// Recovering here converts that into a graceful, in-app error instead:
// the full stack still gets logged (to crashLogPath, for exactly the
// case above, where whatever briefly appeared on a broken terminal
// wasn't enough to diagnose from), and onPanic (if not nil) runs via
// QueueUpdateDraw — safe from any goroutine, unlike touching UI state
// directly here — to reset whatever "in progress" flag fn's own caller
// owns (e.g. cancelHashComputation), so the UI doesn't end up stuck
// believing a background task is still running forever, before
// showError reports it to the user.
//
// name identifies which of this app's own background tasks it was, in
// the log and in the error message — not the file/line a stack trace
// already has, just enough for a human skimming either one to know
// where to start looking.
func (r *Root) safeGo(name string, onPanic func(), fn func()) {
	go func() {
		defer func() {
			if rec := recover(); rec != nil {
				stack := allStacks()
				logCrash(name, rec, stack)
				msg := fmt.Sprintf("%s: internal error: %v", name, rec)
				if path := crashLogPath(); path != "" {
					msg += fmt.Sprintf(" (details written to %s)", path)
				}
				r.app.QueueUpdateDraw(func() {
					if onPanic != nil {
						onPanic()
					}
					r.showError(fmt.Errorf("%s", msg))
				})
			}
		}()
		fn()
	}()
}

// ReportPanic writes a panic and every goroutine's stack to the crash
// log, and reports where it went ("" if no log could be written).
//
// The exported counterpart to what safeGo already does for the
// goroutines it owns — for the one place safeGo cannot reach: the main
// goroutine. A panic there (in a key handler, a mouse handler, a draw
// callback, tview's own event loop) unwinds through
// tview.Application.Run, which restores the terminal and re-panics, and
// the traceback then goes to stderr — where, on a terminal that has just
// been switched back out of the alternate screen buffer, it is very
// often scrolled away or wiped before anyone can read it.
//
// The practical consequence was that a real, repeatable crash left
// behind no evidence at all: no crash.log, because nothing recovered it,
// and nothing on screen either. Reported by a user hitting it regularly
// while browsing with the Details sidebar open in split view — a crash
// nobody could act on because nobody could see it.
//
// Deliberately does not attempt to keep the application alive. A panic
// means some invariant this program believed in is already false, and a
// file manager that carries on regardless is exactly the kind of program
// that then does something irreversible to somebody's files. The job
// here is to leave a usable report and stop.
func ReportPanic(name string, rec any) string {
	logCrash(name, rec, allStacks())
	return crashLogPath()
}

// CrashLogPath is where ReportPanic writes, for a caller that wants to
// name it before anything has gone wrong.
func CrashLogPath() string { return crashLogPath() }
