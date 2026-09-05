package fsops

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// requireDu skips the test on a system without a real du binary — this
// package targets Linux/macOS/BSD (see its own package doc), all of
// which have one, but a stripped-down container image running the test
// suite might not.
func requireDu(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("du"); err != nil {
		t.Skip("du not available in this environment")
	}
}

// TestDirSizeReportsARealDirectory pins the basic happy path against a
// real, small, fully-readable directory tree — the exact byte total
// doesn't matter (block-size rounding on the real filesystem this test
// runs on makes an exact expected value unportable), only that it comes
// back positive and ok.
func TestDirSizeReportsARealDirectory(t *testing.T) {
	requireDu(t)

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hello, world"), 0o600); err != nil {
		t.Fatalf("writing fixture: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b.txt"), make([]byte, 8192), 0o600); err != nil {
		t.Fatalf("writing fixture: %v", err)
	}

	bytes, ok := DirSize(dir)
	if !ok {
		t.Fatal("DirSize reported failure on a real, readable directory")
	}
	if bytes <= 0 {
		t.Errorf("DirSize = %d, want a positive byte count", bytes)
	}
}

// TestDirSizeOnNonexistentPathFails pins the other end: a path du can't
// even see reports failure rather than a bogus zero-as-success.
func TestDirSizeOnNonexistentPathFails(t *testing.T) {
	requireDu(t)

	_, ok := DirSize(filepath.Join(t.TempDir(), "does-not-exist"))
	if ok {
		t.Error("DirSize on a nonexistent path reported success")
	}
}

// TestDirSizeSurvivesAnUnreadableSubdirectory pins the core reasoning in
// DirSize's own doc comment: du exits non-zero the moment it hits even
// one permission-denied subdirectory, but that must not discard the
// valid total it still printed for everything else — the common case in
// a real home directory, not an edge case.
func TestDirSizeSurvivesAnUnreadableSubdirectory(t *testing.T) {
	requireDu(t)
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions — can't create an actually-unreadable subdirectory")
	}

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "readable.txt"), []byte("hello"), 0o600); err != nil {
		t.Fatalf("writing fixture: %v", err)
	}
	blocked := filepath.Join(dir, "blocked")
	if err := os.Mkdir(blocked, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(blocked, "hidden.txt"), []byte("secret"), 0o600); err != nil {
		t.Fatalf("writing fixture: %v", err)
	}
	if err := os.Chmod(blocked, 0o000); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	defer func() { _ = os.Chmod(blocked, 0o700) }() // let t.TempDir() clean up afterward

	bytes, ok := DirSize(dir)
	if !ok {
		t.Fatal("DirSize failed outright over one unreadable subdirectory, want the partial total")
	}
	if bytes <= 0 {
		t.Errorf("DirSize = %d, want a positive byte count for what was readable", bytes)
	}
}
