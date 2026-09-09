package fsops

import (
	"os"
	"path/filepath"
	"testing"
)

// TestMoveRefusesWhenDestinationIsSourceItself pins the same Overlaps
// guard Copy's own TestCopyRefusesWhenDestinationIsSourceItself pins,
// for Move: without it, the os.Rename fast path itself would silently
// no-op (renaming a path to itself trivially "succeeds" — nothing
// actually moved, nothing to report either), reporting success back for
// an operation that did nothing at all.
func TestMoveRefusesWhenDestinationIsSourceItself(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "file.txt")
	if err := os.WriteFile(src, []byte("irreplaceable"), 0o640); err != nil {
		t.Fatal(err)
	}

	if err := Move(src, src, MoveOptions{Force: true}); err == nil {
		t.Fatal("Move(src, src, ...) should refuse, not silently no-op")
	}

	got, err := os.ReadFile(src)
	if err != nil {
		t.Fatalf("the file is gone after the refused Move: %v", err)
	}
	if string(got) != "irreplaceable" {
		t.Errorf("file content = %q, want it untouched: %q", got, "irreplaceable")
	}
}

// TestMoveRefusesMovingDirectoryIntoOwnSubdirectory mirrors Copy's own
// TestCopyRefusesCopyingDirectoryIntoOwnSubdirectory for Move — the
// EXDEV fallback goes through Copy internally (already guarded), but
// this pins that the same-filesystem os.Rename fast path refuses too,
// with a clear error, rather than relying on the OS's own less specific
// EINVAL to happen to prevent damage.
func TestMoveRefusesMovingDirectoryIntoOwnSubdirectory(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "foo")
	sub := filepath.Join(src, "bar")
	if err := os.MkdirAll(sub, 0o750); err != nil {
		t.Fatal(err)
	}

	dst := filepath.Join(sub, "foo")
	if err := Move(src, dst, MoveOptions{}); err == nil {
		t.Fatal("Move(foo, foo/bar/foo, ...) should refuse")
	}

	if _, err := os.Stat(src); err != nil {
		t.Errorf("src should still exist after the refused Move: %v", err)
	}
	if _, err := os.Stat(dst); !os.IsNotExist(err) {
		t.Errorf("dst should never have been created at all, stat err = %v", err)
	}
}

func TestMoveSameFilesystem(t *testing.T) {
	dir := t.TempDir() // TempDir is a single filesystem, so this exercises the os.Rename fast path
	src := filepath.Join(dir, "src.txt")
	if err := os.WriteFile(src, []byte("hello"), 0o640); err != nil {
		t.Fatal(err)
	}

	dst := filepath.Join(dir, "dst.txt")
	if err := Move(src, dst, MoveOptions{}); err != nil {
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

	if err := Move(src, dst, MoveOptions{}); err == nil {
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

	if err := Move(src, dst, MoveOptions{Force: true}); err != nil {
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
	if err := Move(src, dst, MoveOptions{OnFile: func(path string) { reported = append(reported, path) }}); err != nil {
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
	if err := Move(src, dst, MoveOptions{}); err != nil {
		t.Fatalf("Move: %v", err)
	}
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Errorf("src should be gone after Move, stat err = %v", err)
	}
	if got, err := os.ReadFile(filepath.Join(dst, "sub", "a.txt")); err != nil || string(got) != "a" {
		t.Errorf("dst/sub/a.txt = %q, %v, want %q, nil", got, err, "a")
	}
}

// TestMoveMergeIntoNonEmptyDirectoryFallsBackToCopy pins the ENOTEMPTY
// fallback Move's own doc comment describes: os.Rename itself can only
// ever replace an existing directory outright, refusing a non-empty one
// entirely (POSIX rename(2)) — MergeInto onto one falls back to the
// same file-by-file Copy pass the EXDEV case already uses, so a
// dst-only file with no matching source entry still survives, exactly
// like a same-filesystem MergeInto Copy would.
func TestMoveMergeIntoNonEmptyDirectoryFallsBackToCopy(t *testing.T) {
	dir := t.TempDir() // single filesystem — this is not an EXDEV case
	src := filepath.Join(dir, "src")
	if err := os.MkdirAll(src, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "a.txt"), []byte("new"), 0o640); err != nil {
		t.Fatal(err)
	}

	dst := filepath.Join(dir, "dst")
	if err := os.MkdirAll(dst, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dst, "extra.txt"), []byte("keep me"), 0o640); err != nil {
		t.Fatal(err)
	}

	if err := Move(src, dst, MoveOptions{Force: true, Mode: MergeInto}); err != nil {
		t.Fatalf("Move: %v", err)
	}

	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Errorf("src should be gone after Move, stat err = %v", err)
	}
	if got, err := os.ReadFile(filepath.Join(dst, "a.txt")); err != nil || string(got) != "new" {
		t.Errorf("dst/a.txt = %q, %v, want %q", got, err, "new")
	}
	if got, err := os.ReadFile(filepath.Join(dst, "extra.txt")); err != nil || string(got) != "keep me" {
		t.Errorf("dst/extra.txt = %q, %v, want it left untouched by a merge", got, err)
	}
}

// TestMoveReplaceEntirelyNonEmptyDirectoryWipesFirst mirrors the above
// for ReplaceEntirely: dst is removed before os.Rename is even
// attempted (see Move's own doc comment), so it never even reaches
// ENOTEMPTY — the extra file must not survive.
func TestMoveReplaceEntirelyNonEmptyDirectoryWipesFirst(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	if err := os.MkdirAll(src, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "a.txt"), []byte("new"), 0o640); err != nil {
		t.Fatal(err)
	}

	dst := filepath.Join(dir, "dst")
	if err := os.MkdirAll(dst, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dst, "extra.txt"), []byte("should be gone"), 0o640); err != nil {
		t.Fatal(err)
	}

	if err := Move(src, dst, MoveOptions{Force: true, Mode: ReplaceEntirely}); err != nil {
		t.Fatalf("Move: %v", err)
	}

	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Errorf("src should be gone after Move, stat err = %v", err)
	}
	if got, err := os.ReadFile(filepath.Join(dst, "a.txt")); err != nil || string(got) != "new" {
		t.Errorf("dst/a.txt = %q, %v, want %q", got, err, "new")
	}
	if _, err := os.Stat(filepath.Join(dst, "extra.txt")); !os.IsNotExist(err) {
		t.Errorf("dst/extra.txt should be gone after ReplaceEntirely, stat err = %v", err)
	}
}
