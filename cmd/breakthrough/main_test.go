package main

import (
	"os"
	"syscall"
	"testing"
	"time"
)

// TestStartDir pins the command-line argument contract: an explicit path
// argument (as in "breakthrough /var/log") wins over the working
// directory, which is only the fallback when no argument was given.
func TestStartDir(t *testing.T) {
	orig := os.Args
	defer func() { os.Args = orig }()

	os.Args = []string{"breakthrough", "/var/log"}
	got, err := startDir()
	if err != nil {
		t.Fatalf("startDir: %v", err)
	}
	if got != "/var/log" {
		t.Errorf("startDir() = %q, want %q", got, "/var/log")
	}

	os.Args = []string{"breakthrough"}
	want, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd: %v", err)
	}
	got, err = startDir()
	if err != nil {
		t.Fatalf("startDir: %v", err)
	}
	if got != want {
		t.Errorf("startDir() = %q, want cwd %q", got, want)
	}
}

// TestStartDirSkipsFlags pins that a flag like --debug is never
// mistaken for the path argument — "breakthrough --debug" must still
// fall back to the working directory (not try to open a directory
// literally named "--debug"), and "breakthrough --debug /var/log" (or
// the flag and path in either order) must still find the real path.
func TestStartDirSkipsFlags(t *testing.T) {
	orig := os.Args
	defer func() { os.Args = orig }()

	want, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd: %v", err)
	}

	os.Args = []string{"breakthrough", "--debug"}
	got, err := startDir()
	if err != nil {
		t.Fatalf("startDir: %v", err)
	}
	if got != want {
		t.Errorf("startDir() with only --debug = %q, want cwd %q", got, want)
	}

	os.Args = []string{"breakthrough", "--debug", "/var/log"}
	got, err = startDir()
	if err != nil {
		t.Fatalf("startDir: %v", err)
	}
	if got != "/var/log" {
		t.Errorf("startDir() with --debug before the path = %q, want %q", got, "/var/log")
	}

	os.Args = []string{"breakthrough", "/var/log", "--debug"}
	got, err = startDir()
	if err != nil {
		t.Fatalf("startDir: %v", err)
	}
	if got != "/var/log" {
		t.Errorf("startDir() with --debug after the path = %q, want %q", got, "/var/log")
	}
}

// TestInstallSignalHandlerCallsStopOnSIGHUP pins installSignalHandler's
// own whole point: SIGHUP delivered to this process runs the given stop
// callback — verified against a real signal sent to the real running
// test process, not a fake or a mocked signal.Notify, since a real
// SIGHUP is quite literally the whole scenario this exists to handle
// (see its own doc comment on why an unhandled one would otherwise kill
// this process outright, with no chance for any cleanup to run at all).
// Leaves the process's own signal.Notify registration for SIGHUP/
// SIGTERM/SIGINT in place afterward rather than unwinding it — the same
// as production leaves it for the rest of the real application's own
// life, and harmless for the remainder of this short-lived test binary,
// which never sends any of the other two to itself.
func TestInstallSignalHandlerCallsStopOnSIGHUP(t *testing.T) {
	stopped := make(chan struct{}, 1)
	installSignalHandler(func() { stopped <- struct{}{} })

	if err := syscall.Kill(syscall.Getpid(), syscall.SIGHUP); err != nil {
		t.Fatalf("Kill(SIGHUP): %v", err)
	}

	select {
	case <-stopped:
	case <-time.After(2 * time.Second):
		t.Fatal("stop was not called within 2s of sending this process its own SIGHUP")
	}
}
