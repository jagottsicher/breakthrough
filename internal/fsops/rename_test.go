package fsops

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRename(t *testing.T) {
	dir := t.TempDir()
	old := filepath.Join(dir, "old.txt")
	if err := os.WriteFile(old, []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}

	newPath, err := Rename(old, "new.txt")
	if err != nil {
		t.Fatalf("Rename: %v", err)
	}

	want := filepath.Join(dir, "new.txt")
	if newPath != want {
		t.Errorf("newPath = %q, want %q", newPath, want)
	}
	if _, err := os.Stat(want); err != nil {
		t.Errorf("renamed file not found: %v", err)
	}
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Errorf("old file still exists (err = %v)", err)
	}
}

func TestRenameEmptyName(t *testing.T) {
	dir := t.TempDir()
	old := filepath.Join(dir, "old.txt")
	if err := os.WriteFile(old, nil, 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := Rename(old, ""); err == nil {
		t.Fatal("expected an error for an empty new name, got nil")
	}
}

func TestRenamePathSeparator(t *testing.T) {
	dir := t.TempDir()
	old := filepath.Join(dir, "old.txt")
	if err := os.WriteFile(old, nil, 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := Rename(old, "sub"+string(os.PathSeparator)+"new.txt"); err == nil {
		t.Fatal("expected an error for a new name containing a path separator, got nil")
	}
}

func TestRenameDestinationExists(t *testing.T) {
	dir := t.TempDir()
	old := filepath.Join(dir, "old.txt")
	existing := filepath.Join(dir, "existing.txt")
	if err := os.WriteFile(old, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(existing, nil, 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := Rename(old, "existing.txt"); err == nil {
		t.Fatal("expected an error when the destination already exists, got nil")
	}
	if _, err := os.Stat(old); err != nil {
		t.Errorf("original file should still exist after a refused rename: %v", err)
	}
}
