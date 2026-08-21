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

	got, err := Hash(path)
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

	hashA, err := Hash(a)
	if err != nil {
		t.Fatal(err)
	}
	hashB, err := Hash(b)
	if err != nil {
		t.Fatal(err)
	}

	if hashA == hashB {
		t.Error("differently-content files should not hash the same")
	}
}

func TestHashRejectsDirectory(t *testing.T) {
	dir := t.TempDir()
	if _, err := Hash(dir); err == nil {
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

	got, err := Hash(link)
	if err != nil {
		t.Fatalf("Hash(link): %v", err)
	}
	if got.MD5 != "5eb63bbbe01eeed093cb22bb8f5acdc3" {
		t.Errorf("Hash(link).MD5 = %q, want the target's own MD5", got.MD5)
	}
}
