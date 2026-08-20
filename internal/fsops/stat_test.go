//go:build unix

package fsops

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestStatFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "file.txt")
	if err := os.WriteFile(path, []byte("hello"), 0o640); err != nil {
		t.Fatal(err)
	}

	info, err := Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}

	if info.Name != "file.txt" {
		t.Errorf("Name = %q, want %q", info.Name, "file.txt")
	}
	if info.Path != path {
		t.Errorf("Path = %q, want %q", info.Path, path)
	}
	if info.IsDir {
		t.Error("IsDir = true, want false")
	}
	if info.IsSymlink {
		t.Error("IsSymlink = true, want false")
	}
	if info.Size != 5 {
		t.Errorf("Size = %d, want 5", info.Size)
	}
	if info.Mode.Perm() != 0o640 {
		t.Errorf("Mode.Perm() = %o, want 640", info.Mode.Perm())
	}
	if info.Owner == "" {
		t.Error("Owner is empty, want the current user's name or numeric uid")
	}
	if info.Group == "" {
		t.Error("Group is empty, want the current group's name or numeric gid")
	}
	if time.Since(info.ModTime) > time.Minute {
		t.Errorf("ModTime = %v, expected roughly now", info.ModTime)
	}
}

func TestStatDir(t *testing.T) {
	dir := t.TempDir()

	info, err := Stat(dir)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}

	if !info.IsDir {
		t.Error("IsDir = false, want true")
	}
}

func TestStatSymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target.txt")
	link := filepath.Join(dir, "link.txt")

	if err := os.WriteFile(target, []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}

	info, err := Stat(link)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}

	if !info.IsSymlink {
		t.Error("IsSymlink = false, want true")
	}
	if info.IsDir {
		t.Error("IsDir = true, want false — Stat uses Lstat, it must not follow the link")
	}
	if info.LinkTarget != target {
		t.Errorf("LinkTarget = %q, want %q", info.LinkTarget, target)
	}
}

func TestStatNonExistent(t *testing.T) {
	if _, err := Stat(filepath.Join(t.TempDir(), "does-not-exist")); err == nil {
		t.Fatal("expected an error for a non-existent path, got nil")
	}
}
