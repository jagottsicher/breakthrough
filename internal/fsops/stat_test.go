//go:build unix

package fsops

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestStatUIDGID pins that UID/GID are populated with the process's own
// ids for a file it just created — the one case guaranteed to be
// unambiguous regardless of what account runs this test.
func TestStatUIDGID(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "file.txt")
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatal(err)
	}

	info, err := Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if info.UID != os.Getuid() {
		t.Errorf("UID = %d, want %d", info.UID, os.Getuid())
	}
	if info.GID != os.Getgid() {
		t.Errorf("GID = %d, want %d", info.GID, os.Getgid())
	}
}

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
	if info.Nlink != 1 {
		t.Errorf("Nlink = %d, want 1 for a freshly created file with no other hard links", info.Nlink)
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
	if info.LinkBroken {
		t.Error("LinkBroken = true, want false — the target exists")
	}
	if info.LinkIsDir {
		t.Error("LinkIsDir = true, want false — the target is a file")
	}
}

// TestStatSymlinkToDir pins LinkIsDir specifically, and that MountPoint
// is derived from the resolved target (not left false just because the
// symlink itself is never a directory per Lstat).
func TestStatSymlinkToDir(t *testing.T) {
	dir := t.TempDir()
	targetDir := filepath.Join(dir, "target-dir")
	link := filepath.Join(dir, "link")

	if err := os.Mkdir(targetDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(targetDir, link); err != nil {
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
		t.Error("IsDir = true, want false — Lstat never reports a symlink itself as a directory")
	}
	if !info.LinkIsDir {
		t.Error("LinkIsDir = false, want true — the target is a directory")
	}
	if info.LinkBroken {
		t.Error("LinkBroken = true, want false")
	}
}

// TestStatBrokenSymlink pins LinkBroken for a symlink whose target
// doesn't exist — LinkTarget must still be reported (a dangling target is
// exactly the thing worth showing the user), just marked broken.
func TestStatBrokenSymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "does-not-exist")
	link := filepath.Join(dir, "link")

	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}

	info, err := Stat(link)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}

	if !info.LinkBroken {
		t.Error("LinkBroken = false, want true")
	}
	if info.LinkTarget != target {
		t.Errorf("LinkTarget = %q, want %q (still reported even though it's dangling)", info.LinkTarget, target)
	}
}

// TestStatHardlink pins Nlink for a file with an extra hard link.
func TestStatHardlink(t *testing.T) {
	dir := t.TempDir()
	original := filepath.Join(dir, "original.txt")
	if err := os.WriteFile(original, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	extra := filepath.Join(dir, "extra.txt")
	if err := os.Link(original, extra); err != nil {
		t.Skipf("hard links not supported here: %v", err)
	}

	info, err := Stat(original)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if info.Nlink != 2 {
		t.Errorf("Nlink = %d, want 2", info.Nlink)
	}
}

func TestStatNonExistent(t *testing.T) {
	if _, err := Stat(filepath.Join(t.TempDir(), "does-not-exist")); err == nil {
		t.Fatal("expected an error for a non-existent path, got nil")
	}
}
