package fsops

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCopyFile(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.txt")
	if err := os.WriteFile(src, []byte("hello"), 0o640); err != nil {
		t.Fatal(err)
	}

	dst := filepath.Join(dir, "dst.txt")
	if err := Copy(src, dst, false); err != nil {
		t.Fatalf("Copy: %v", err)
	}

	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("ReadFile(dst): %v", err)
	}
	if string(got) != "hello" {
		t.Errorf("dst content = %q, want %q", got, "hello")
	}

	// The original must be untouched — Copy, unlike Move, never removes
	// src.
	if _, err := os.Stat(src); err != nil {
		t.Errorf("src should still exist after Copy: %v", err)
	}
}

func TestCopyRefusesExistingDestByDefault(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.txt")
	dst := filepath.Join(dir, "dst.txt")
	if err := os.WriteFile(src, []byte("new"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dst, []byte("old"), 0o640); err != nil {
		t.Fatal(err)
	}

	if err := Copy(src, dst, false); err == nil {
		t.Fatal("Copy should refuse to overwrite an existing dst without force")
	}

	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "old" {
		t.Errorf("dst content = %q, want unchanged %q", got, "old")
	}
}

func TestCopyForceOverwritesExistingDest(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.txt")
	dst := filepath.Join(dir, "dst.txt")
	if err := os.WriteFile(src, []byte("new"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dst, []byte("old-and-longer"), 0o640); err != nil {
		t.Fatal(err)
	}

	if err := Copy(src, dst, true); err != nil {
		t.Fatalf("Copy with force: %v", err)
	}

	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "new" {
		t.Errorf("dst content = %q, want %q (no leftover bytes from the longer old file)", got, "new")
	}
}

func TestCopyDirRecursive(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	if err := os.MkdirAll(filepath.Join(src, "sub"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "a.txt"), []byte("a"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "sub", "b.txt"), []byte("b"), 0o640); err != nil {
		t.Fatal(err)
	}

	dst := filepath.Join(dir, "dst")
	if err := Copy(src, dst, false); err != nil {
		t.Fatalf("Copy: %v", err)
	}

	if got, err := os.ReadFile(filepath.Join(dst, "a.txt")); err != nil || string(got) != "a" {
		t.Errorf("dst/a.txt = %q, %v, want %q, nil", got, err, "a")
	}
	if got, err := os.ReadFile(filepath.Join(dst, "sub", "b.txt")); err != nil || string(got) != "b" {
		t.Errorf("dst/sub/b.txt = %q, %v, want %q, nil", got, err, "b")
	}
}

func TestCopySymlinkRecreatesLinkNotTarget(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target.txt")
	if err := os.WriteFile(target, []byte("real"), 0o640); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link.txt")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}

	dst := filepath.Join(dir, "link-copy.txt")
	if err := Copy(link, dst, false); err != nil {
		t.Fatalf("Copy: %v", err)
	}

	got, err := os.Readlink(dst)
	if err != nil {
		t.Fatalf("dst should be a symlink: %v", err)
	}
	if got != target {
		t.Errorf("copied link points to %q, want %q", got, target)
	}
}
