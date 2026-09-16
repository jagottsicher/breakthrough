package fsops

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCreateFile(t *testing.T) {
	dir := t.TempDir()

	got, err := CreateFile(dir, "new.txt")
	if err != nil {
		t.Fatalf("CreateFile: %v", err)
	}

	want := filepath.Join(dir, "new.txt")
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	info, err := os.Stat(want)
	if err != nil {
		t.Fatalf("new file not found: %v", err)
	}
	if info.Size() != 0 {
		t.Errorf("new file size = %d, want 0", info.Size())
	}
}

func TestCreateFileEmptyName(t *testing.T) {
	if _, err := CreateFile(t.TempDir(), ""); err == nil {
		t.Fatal("expected an error for an empty name, got nil")
	}
}

func TestCreateFilePathSeparator(t *testing.T) {
	if _, err := CreateFile(t.TempDir(), "sub"+string(os.PathSeparator)+"new.txt"); err == nil {
		t.Fatal("expected an error for a name containing a path separator, got nil")
	}
}

func TestCreateFileDestinationExists(t *testing.T) {
	dir := t.TempDir()
	existing := filepath.Join(dir, "existing.txt")
	if err := os.WriteFile(existing, []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := CreateFile(dir, "existing.txt"); err == nil {
		t.Fatal("expected an error when the destination already exists, got nil")
	}
	data, err := os.ReadFile(existing)
	if err != nil {
		t.Fatalf("existing file should be untouched: %v", err)
	}
	if string(data) != "hi" {
		t.Errorf("existing file was overwritten: got %q, want %q", data, "hi")
	}
}

func TestCreateDir(t *testing.T) {
	dir := t.TempDir()

	got, err := CreateDir(dir, "newdir")
	if err != nil {
		t.Fatalf("CreateDir: %v", err)
	}

	want := filepath.Join(dir, "newdir")
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	info, err := os.Stat(want)
	if err != nil {
		t.Fatalf("new directory not found: %v", err)
	}
	if !info.IsDir() {
		t.Errorf("new entry is not a directory")
	}
}

func TestCreateDirEmptyName(t *testing.T) {
	if _, err := CreateDir(t.TempDir(), ""); err == nil {
		t.Fatal("expected an error for an empty name, got nil")
	}
}

func TestCreateDirPathSeparator(t *testing.T) {
	if _, err := CreateDir(t.TempDir(), "sub"+string(os.PathSeparator)+"newdir"); err == nil {
		t.Fatal("expected an error for a name containing a path separator, got nil")
	}
}

func TestCreateDirDestinationExists(t *testing.T) {
	dir := t.TempDir()
	existing := filepath.Join(dir, "existing")
	if err := os.Mkdir(existing, 0o755); err != nil {
		t.Fatal(err)
	}

	if _, err := CreateDir(dir, "existing"); err == nil {
		t.Fatal("expected an error when the destination already exists, got nil")
	}
}
