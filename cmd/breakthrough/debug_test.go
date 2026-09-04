package main

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

// TestEnableDebugModeRedirectsStderr pins the core mechanism --debug
// relies on: a dup2 at the OS file descriptor level, not just
// reassigning Go's os.Stderr variable — verified directly here, not
// assumed, since the whole point (see enableDebugMode's own doc
// comment) is catching the Go runtime's own default panic printer,
// which writes to fd 2 directly and would never notice a plain
// os.Stderr reassignment.
//
// Carefully restores the real fd 2 afterward regardless of how the test
// itself turns out — leaving it redirected would silently swallow every
// later test's own stderr output for the rest of this whole test
// binary, not just this one test.
func TestEnableDebugModeRedirectsStderr(t *testing.T) {
	origStderr, err := unix.Dup(int(os.Stderr.Fd()))
	if err != nil {
		t.Fatalf("dup(2): %v", err)
	}
	defer func() {
		if err := unix.Dup2(origStderr, int(os.Stderr.Fd())); err != nil {
			// t.Fatal itself wouldn't even reach a real terminal any
			// more if this failed — panic is the only way left to make
			// this loud enough to notice.
			panic(fmt.Sprintf("failed to restore the real stderr after TestEnableDebugModeRedirectsStderr: %v", err))
		}
		_ = unix.Close(origStderr)
	}()

	dir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", dir)

	logPath, err := enableDebugMode()
	if err != nil {
		t.Fatalf("enableDebugMode: %v", err)
	}

	fmt.Fprintln(os.Stderr, "marker: this should land in the log file, not the terminal")

	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("reading debug log: %v", err)
	}
	got := string(data)
	if !strings.Contains(got, "marker: this should land in the log file") {
		t.Errorf("debug log is missing the marker line written after redirect, got:\n%s", got)
	}
	if !strings.Contains(got, "debug session started") {
		t.Errorf("debug log is missing its own session-start banner, got:\n%s", got)
	}
}
