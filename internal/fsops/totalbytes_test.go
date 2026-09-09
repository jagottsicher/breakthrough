package fsops

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// TestTotalBytesSumsFilesRecursively pins TotalBytes' own basic
// contract: a plain file counted directly, a directory walked
// recursively and summed, several top-level paths added together.
func TestTotalBytesSumsFilesRecursively(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(file, []byte("12345"), 0o640); err != nil { // 5 bytes
		t.Fatal(err)
	}
	sub := filepath.Join(dir, "sub")
	if err := os.MkdirAll(sub, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "b.txt"), []byte("1234567890"), 0o640); err != nil { // 10 bytes
		t.Fatal(err)
	}

	got := TotalBytes(context.Background(), []string{file, sub})
	if got != 15 {
		t.Errorf("TotalBytes = %d, want 15 (5 + 10)", got)
	}
}

// TestTotalBytesSkipsSymlinks pins the documented contract: a symlink
// anywhere in the walk, including a top-level path that is one itself,
// contributes 0 — recreating a link is never a byte stream, so it
// shouldn't inflate the total a real non-following Copy would actually
// need to move.
func TestTotalBytesSkipsSymlinks(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target.txt")
	if err := os.WriteFile(target, []byte("this content is not counted via the link"), 0o640); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}

	// Only the link itself is passed in, not target directly.
	if got := TotalBytes(context.Background(), []string{link}); got != 0 {
		t.Errorf("TotalBytes with only a symlink = %d, want 0", got)
	}

	// A symlink nested inside a directory contributes 0 too, while its
	// sibling regular file still counts normally.
	nested := filepath.Join(dir, "nested")
	if err := os.MkdirAll(nested, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nested, "real.txt"), []byte("abc"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(nested, "link.txt")); err != nil {
		t.Fatal(err)
	}
	if got := TotalBytes(context.Background(), []string{nested}); got != 3 {
		t.Errorf("TotalBytes with a directory containing a symlink = %d, want 3 (only the real file)", got)
	}
}

// TestTotalBytesBestEffortOnUnreadablePath pins the documented
// best-effort contract: a path that doesn't exist at all (the simplest
// stand-in for "can't be stat'd") contributes 0 rather than the whole
// call failing or panicking — this feeds a progress estimate, not a
// correctness check.
func TestTotalBytesBestEffortOnUnreadablePath(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "real.txt")
	if err := os.WriteFile(real, []byte("1234"), 0o640); err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(dir, "does-not-exist")

	got := TotalBytes(context.Background(), []string{real, missing})
	if got != 4 {
		t.Errorf("TotalBytes with one missing path = %d, want 4 (the one real file, missing path contributes 0)", got)
	}
}

// TestTotalBytesStopsOnCancellation pins the early-return contract: a
// cancelled context stops the walk rather than continuing to size a
// tree nobody will read the result of. Doesn't assert an exact partial
// total (that depends on os.ReadDir's own unspecified ordering) — only
// that a pre-cancelled context returns immediately with nothing at all
// counted, the one deterministic case.
func TestTotalBytesStopsOnCancellation(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("12345"), 0o640); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already cancelled before TotalBytes is ever called

	if got := TotalBytes(ctx, []string{dir}); got != 0 {
		t.Errorf("TotalBytes with an already-cancelled context = %d, want 0", got)
	}
}
