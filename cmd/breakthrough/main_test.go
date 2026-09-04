package main

import (
	"os"
	"testing"
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
