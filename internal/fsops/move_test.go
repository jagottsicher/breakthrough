package fsops

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMoveSameFilesystem(t *testing.T) {
	dir := t.TempDir() // TempDir is a single filesystem, so this exercises the os.Rename fast path
	src := filepath.Join(dir, "src.txt")
	if err := os.WriteFile(src, []byte("hello"), 0o640); err != nil {
		t.Fatal(err)
	}

	dst := filepath.Join(dir, "dst.txt")
	if err := Move(src, dst, false, nil); err != nil {
		t.Fatalf("Move: %v", err)
	}

	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Errorf("src should be gone after Move, stat err = %v", err)
	}
	got, err := os.ReadFile(dst)
	if err != nil || string(got) != "hello" {
		t.Errorf("dst = %q, %v, want %q, nil", got, err, "hello")
	}
}

func TestMoveRefusesExistingDestByDefault(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.txt")
	dst := filepath.Join(dir, "dst.txt")
	if err := os.WriteFile(src, []byte("new"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dst, []byte("old"), 0o640); err != nil {
		t.Fatal(err)
	}

	if err := Move(src, dst, false, nil); err == nil {
		t.Fatal("Move should refuse to overwrite an existing dst without force")
	}
	if _, err := os.Stat(src); err != nil {
		t.Errorf("src should be untouched after a refused Move: %v", err)
	}
}

func TestMoveForceOverwritesExistingDest(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.txt")
	dst := filepath.Join(dir, "dst.txt")
	if err := os.WriteFile(src, []byte("new"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dst, []byte("old"), 0o640); err != nil {
		t.Fatal(err)
	}

	if err := Move(src, dst, true, nil); err != nil {
		t.Fatalf("Move with force: %v", err)
	}
	got, err := os.ReadFile(dst)
	if err != nil || string(got) != "new" {
		t.Errorf("dst = %q, %v, want %q, nil", got, err, "new")
	}
}

// TestMoveReportsSrcViaOnFile pins onFile's own contract for the
// os.Rename fast path (see Move's own doc comment): called once for
// src itself before attempting the rename — the only signal available
// for it at all, since a same-filesystem rename never touches
// individual files underneath a directory the way the EXDEV fallback's
// own Copy call does.
func TestMoveReportsSrcViaOnFile(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.txt")
	if err := os.WriteFile(src, []byte("hello"), 0o640); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(dir, "dst.txt")

	var reported []string
	if err := Move(src, dst, false, func(path string) { reported = append(reported, path) }); err != nil {
		t.Fatalf("Move: %v", err)
	}

	if len(reported) != 1 || reported[0] != src {
		t.Errorf("onFile reported %v, want exactly [%q]", reported, src)
	}
}

func TestMoveDirectory(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	if err := os.MkdirAll(filepath.Join(src, "sub"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "sub", "a.txt"), []byte("a"), 0o640); err != nil {
		t.Fatal(err)
	}

	dst := filepath.Join(dir, "dst")
	if err := Move(src, dst, false, nil); err != nil {
		t.Fatalf("Move: %v", err)
	}
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Errorf("src should be gone after Move, stat err = %v", err)
	}
	if got, err := os.ReadFile(filepath.Join(dst, "sub", "a.txt")); err != nil || string(got) != "a" {
		t.Errorf("dst/sub/a.txt = %q, %v, want %q, nil", got, err, "a")
	}
}
