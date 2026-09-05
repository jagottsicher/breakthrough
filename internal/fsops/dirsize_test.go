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

	bytes, measured, ok := DirSize(dir)
	if !ok {
		t.Fatal("DirSize reported failure on a real, readable directory")
	}
	if bytes <= 0 {
		t.Errorf("DirSize = %d, want a positive byte count", bytes)
	}
	// A plain directory measures itself: the resolved path is the one
	// passed in (EvalSymlinks also cleans it, so compare against the
	// cleaned form).
	if want, _ := filepath.EvalSymlinks(dir); measured != want {
		t.Errorf("measured = %q, want %q", measured, want)
	}
}

// TestDirSizeOnNonexistentPathFails pins the other end: a path du can't
// even see reports failure rather than a bogus zero-as-success.
func TestDirSizeOnNonexistentPathFails(t *testing.T) {
	requireDu(t)

	_, _, ok := DirSize(filepath.Join(t.TempDir(), "does-not-exist"))
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

	bytes, _, ok := DirSize(dir)
	if !ok {
		t.Fatal("DirSize failed outright over one unreadable subdirectory, want the partial total")
	}
	if bytes <= 0 {
		t.Errorf("DirSize = %d, want a positive byte count for what was readable", bytes)
	}
}

// TestDirSizeFollowsASymlinkToItsTarget pins the behaviour a real
// request asked for: measuring a directory symlink must report the
// target's own size, not the handful of bytes the link itself occupies
// — `du` on a symlink reports the latter, which is never the answer
// being asked for.
func TestDirSizeFollowsASymlinkToItsTarget(t *testing.T) {
	requireDu(t)

	root := t.TempDir()
	target := filepath.Join(root, "target")
	if err := os.Mkdir(target, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(target, "big.bin"), make([]byte, 64*1024), 0o600); err != nil {
		t.Fatalf("writing fixture: %v", err)
	}
	link := filepath.Join(root, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	viaLink, measured, ok := DirSize(link)
	if !ok {
		t.Fatal("DirSize on a directory symlink reported failure")
	}
	viaTarget, _, ok := DirSize(target)
	if !ok {
		t.Fatal("DirSize on the target reported failure")
	}

	if viaLink != viaTarget {
		t.Errorf("through the link = %d, directly = %d — they must agree", viaLink, viaTarget)
	}
	if want, _ := filepath.EvalSymlinks(target); measured != want {
		t.Errorf("measured = %q, want the resolved target %q", measured, want)
	}
}

// TestDirSizeFollowsAWholeSymlinkChain pins the multi-hop case the same
// request named explicitly: a link to a link to a directory still
// reports the directory at the end of the chain.
func TestDirSizeFollowsAWholeSymlinkChain(t *testing.T) {
	requireDu(t)

	root := t.TempDir()
	target := filepath.Join(root, "target")
	if err := os.Mkdir(target, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(target, "big.bin"), make([]byte, 64*1024), 0o600); err != nil {
		t.Fatalf("writing fixture: %v", err)
	}
	first := filepath.Join(root, "first")
	second := filepath.Join(root, "second")
	if err := os.Symlink(target, first); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	if err := os.Symlink(first, second); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	viaChain, measured, ok := DirSize(second)
	if !ok {
		t.Fatal("DirSize through a two-hop symlink chain reported failure")
	}
	viaTarget, _, _ := DirSize(target)

	if viaChain != viaTarget {
		t.Errorf("through the chain = %d, directly = %d — they must agree", viaChain, viaTarget)
	}
	if want, _ := filepath.EvalSymlinks(target); measured != want {
		t.Errorf("measured = %q, want the far end of the chain %q", measured, want)
	}
}

// TestDirSizeOnABrokenSymlinkFails pins that an unresolvable link
// reports failure rather than quietly measuring the link itself and
// returning a confidently wrong handful of bytes.
func TestDirSizeOnABrokenSymlinkFails(t *testing.T) {
	requireDu(t)

	root := t.TempDir()
	link := filepath.Join(root, "broken")
	if err := os.Symlink(filepath.Join(root, "nothing-here"), link); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	if _, _, ok := DirSize(link); ok {
		t.Error("DirSize on a broken symlink reported success")
	}
}
