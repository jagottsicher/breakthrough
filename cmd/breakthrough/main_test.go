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
