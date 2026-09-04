package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
	"time"

	"github.com/jagottsicher/breakthrough/internal/ui"
	"golang.org/x/sys/unix"
)

// enableDebugMode is --debug's own action (see main) — redirects this
// process's own stderr (fd 2, not just Go's os.Stderr variable) to a
// durable log file for the rest of the process's lifetime, and turns on
// runtime/debug's own SetTraceback("all") so an uncaught panic — one
// tview's own Application.Run doesn't catch either, an even rarer case
// than the goroutine panics safeGo already recovers from (see
// internal/ui/crash.go) — dumps every goroutine's own stack, not just
// the panicking one, if it ever actually happens.
//
// A real dup2 at the OS file descriptor level, not just reassigning
// os.Stderr: the Go runtime's own default panic printer writes directly
// to fd 2 with a raw syscall, entirely bypassing os.Stderr (which the
// runtime can't depend on still working correctly during a crash) —
// verified by hand, not guessed: a plain "os.Stderr = f" reassignment
// never actually caught a real panic's own output in testing, dup2 did.
//
// Uses golang.org/x/sys/unix rather than the standard syscall package
// because the standard library doesn't define Dup2 for every platform
// this project targets — linux/arm64's kernel has no dup2 syscall at
// all, only dup3, so syscall.Dup2 simply doesn't exist there (confirmed
// against a real linux/arm64 build, not assumed). x/sys/unix provides
// Dup2 uniformly across all of them, transparently backed by dup3 where
// that's the only option.
//
// Everything written to stderr from this point on — this app's own
// error messages, and critically any crash — goes to the log file
// instead of the visible terminal for the rest of the run. That's a
// deliberate trade, specific to --debug: the terminal is in tview's own
// alternate-screen/raw mode for this app's entire lifetime regardless,
// so nothing would show up live on it either way — the log file is the
// only place a --debug crash's own diagnostics were ever going to be
// readable from.
func enableDebugMode() (logPath string, err error) {
	dir := ui.DebugDir()
	if dir == "" {
		return "", fmt.Errorf("could not determine a debug log directory ($XDG_STATE_HOME and $HOME are both unset)")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}

	logPath = filepath.Join(dir, "debug.log")
	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return "", err
	}
	if err := unix.Dup2(int(f.Fd()), int(os.Stderr.Fd())); err != nil {
		_ = f.Close()
		return "", fmt.Errorf("redirecting stderr: %w", err)
	}
	_ = f.Close() // fd 2 now points at the same open file description; this *os.File handle itself is no longer needed

	debug.SetTraceback("all")
	fmt.Fprintf(os.Stderr, "\n=== breakthrough %s debug session started %s ===\n", version, time.Now().Format(time.RFC3339))
	return logPath, nil
}
