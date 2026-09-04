package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"
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
func DebugDir() string {
	dir := os.Getenv("XDG_STATE_HOME")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		dir = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(dir, "breakthrough")
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
