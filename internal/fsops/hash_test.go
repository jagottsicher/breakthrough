package fsops

import (
	"os"
	"path/filepath"
	"testing"
)

func TestHash(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(path, []byte("hello world"), 0o640); err != nil {
		t.Fatal(err)
	}

	got, err := Hash(path, nil)
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}

	// Exact digests rather than just length: these are well-known, stable
	// values for the literal bytes "hello world".
	if got.MD5 != "5eb63bbbe01eeed093cb22bb8f5acdc3" {
		t.Errorf("MD5 = %q, want %q", got.MD5, "5eb63bbbe01eeed093cb22bb8f5acdc3")
	}
	if got.SHA1 != "2aae6c35c94fcfb415dbe95f408b9ce91ee846ed" {
		t.Errorf("SHA1 = %q, want %q", got.SHA1, "2aae6c35c94fcfb415dbe95f408b9ce91ee846ed")
	}
	if got.SHA256 != "b94d27b9934d3e08a52e52d7da7dabfac484efe37a5380ee9088f7ace2efcde9" {
		t.Errorf("SHA256 = %q, want %q", got.SHA256, "b94d27b9934d3e08a52e52d7da7dabfac484efe37a5380ee9088f7ace2efcde9")
	}
}

func TestHashDiffersByContent(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.txt")
	b := filepath.Join(dir, "b.txt")
	if err := os.WriteFile(a, []byte("content A"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(b, []byte("content B"), 0o640); err != nil {
		t.Fatal(err)
	}

	hashA, err := Hash(a, nil)
	if err != nil {
		t.Fatal(err)
	}
	hashB, err := Hash(b, nil)
	if err != nil {
		t.Fatal(err)
	}

	if hashA == hashB {
		t.Error("differently-content files should not hash the same")
	}
}

func TestHashRejectsDirectory(t *testing.T) {
	dir := t.TempDir()
	if _, err := Hash(dir, nil); err == nil {
		t.Error("Hash should refuse a directory")
	}
}

func TestHashFollowsSymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target.txt")
	if err := os.WriteFile(target, []byte("hello world"), 0o640); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link.txt")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}

	got, err := Hash(link, nil)
	if err != nil {
		t.Fatalf("Hash(link): %v", err)
	}
	if got.MD5 != "5eb63bbbe01eeed093cb22bb8f5acdc3" {
		t.Errorf("Hash(link).MD5 = %q, want the target's own MD5", got.MD5)
	}
}

// TestHashReportsProgress pins onProgress's own contract: called after
// every underlying Read with the running total of bytes streamed so
// far, ending exactly at the file's size.
func TestHashReportsProgress(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.txt")
	content := []byte("hello world")
	if err := os.WriteFile(path, content, 0o640); err != nil {
		t.Fatal(err)
	}

	var calls []int64
	if _, err := Hash(path, func(readBytes int64) {
		calls = append(calls, readBytes)
	}); err != nil {
		t.Fatalf("Hash: %v", err)
	}

	if len(calls) == 0 {
		t.Fatal("onProgress should have been called at least once for a non-empty file")
	}
	last := calls[len(calls)-1]
	if last != int64(len(content)) {
		t.Errorf("final onProgress read count = %d, want %d (the file's own size)", last, len(content))
	}
	for i := 1; i < len(calls); i++ {
		if calls[i] < calls[i-1] {
			t.Errorf("onProgress calls should never decrease, got %v", calls)
			break
		}
	}
}

// TestHashNilProgressIsOptional pins that onProgress is genuinely
// optional — passing nil (as every other test in this file does) must
// not panic, even though Hash calls it internally whenever it's set.
func TestHashNilProgressIsOptional(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(path, []byte("hello world"), 0o640); err != nil {
		t.Fatal(err)
	}

	if _, err := Hash(path, nil); err != nil {
		t.Fatalf("Hash with nil onProgress: %v", err)
	}
}
