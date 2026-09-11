package fsops

import (
	"os"
	"path/filepath"
	"testing"
	"time"
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

// TestMoveLeavesSourceIntactWhenTheCopyFallbackFailsPartway pins the
// user's own explicit safety requirement: if a file is skipped or
// cannot be moved for any reason, the source's own folder structure
// must stay intact — including whatever else in it was *not* the cause
// of the failure. Verified here through Move's own Copy-based fallback
// (a merge-mode conflict, the same trigger
// TestMoveMergeIntoNonEmptyDirectoryFallsBackToCopy already uses, since
// EXDEV itself needs a genuine second filesystem to simulate) rather
// than the fast os.Rename path, which is atomic and has no partial
// state to leave behind in the first place.
//
// b.txt is made unreadable so copyDir's own fail-fast walk (see its own
// doc comment: it returns on the very first error rather than skipping
// past it) stops there — os.ReadDir returns entries in name order, so
// a.txt is always attempted, and copied successfully, first. Move must
// then still report the failure without ever calling os.RemoveAll(src)
// at all: not just b.txt, but a.txt too (already safely copied to dst
// by that point) must still be exactly where it started.
func TestMoveLeavesSourceIntactWhenTheCopyFallbackFailsPartway(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root: permission checks never fail, this test doesn't apply")
	}

	base := t.TempDir()
	src := filepath.Join(base, "src")
	if err := os.Mkdir(src, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "a.txt"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "b.txt"), []byte("b"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Join(src, "b.txt"), 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(filepath.Join(src, "b.txt"), 0o644) }) // so t.TempDir's own cleanup can remove it afterward

	// A conflicting, non-empty dst forces the MergeInto fallback rather
	// than the fast os.Rename path.
	dst := filepath.Join(base, "dst")
	if err := os.Mkdir(dst, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dst, "already-there.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	err := Move(src, dst, MoveOptions{Force: true, Mode: MergeInto})
	if err == nil {
		t.Fatal("Move should have failed once copying b.txt hit a permission error")
	}

	if _, statErr := os.Stat(filepath.Join(src, "a.txt")); statErr != nil {
		t.Errorf("src/a.txt should still exist after a failed Move (it was already safely copied to dst), stat err = %v", statErr)
	}
	if _, statErr := os.Stat(filepath.Join(src, "b.txt")); statErr != nil {
		t.Errorf("src/b.txt should still exist after a failed Move, stat err = %v", statErr)
	}
}

// TestMoveMergeFallbackPreservesModTime pins the user's own explicit
// request end to end through Move itself, not just Copy directly:
// dates (and, by the same mechanism, permissions and best-effort
// ownership — see preserveMetadata's own doc comment in copy.go) must
// survive a move that has to fall back to its Copy-based path, exactly
// as the fast os.Rename path already preserves them for free by being
// the same inode throughout.
func TestMoveMergeFallbackPreservesModTime(t *testing.T) {
	base := t.TempDir()
	src := filepath.Join(base, "src")
	if err := os.Mkdir(src, 0o755); err != nil {
		t.Fatal(err)
	}
	srcFile := filepath.Join(src, "a.txt")
	if err := os.WriteFile(srcFile, []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	want := time.Date(2018, 4, 4, 4, 4, 4, 0, time.UTC)
	if err := os.Chtimes(srcFile, want, want); err != nil {
		t.Fatal(err)
	}

	dst := filepath.Join(base, "dst")
	if err := os.Mkdir(dst, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dst, "already-there.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := Move(src, dst, MoveOptions{Force: true, Mode: MergeInto}); err != nil {
		t.Fatalf("Move: %v", err)
	}

	fi, err := os.Stat(filepath.Join(dst, "a.txt"))
	if err != nil {
		t.Fatalf("Stat(dst/a.txt): %v", err)
	}
	if !fi.ModTime().Equal(want) {
		t.Errorf("dst/a.txt ModTime = %v, want %v (preserved through Move's own Copy-based fallback)", fi.ModTime(), want)
	}
}

// TestMoveMergeFallbackSkipAttributesLeavesModTimeAlone is
// TestMoveMergeFallbackPreservesModTime's own opt-out counterpart:
// SkipAttributes threaded from MoveOptions into the Copy fallback's own
// CopyOptions must actually take effect there, not just on a direct
// Copy call.
func TestMoveMergeFallbackSkipAttributesLeavesModTimeAlone(t *testing.T) {
	base := t.TempDir()
	src := filepath.Join(base, "src")
	if err := os.Mkdir(src, 0o755); err != nil {
		t.Fatal(err)
	}
	srcFile := filepath.Join(src, "a.txt")
	if err := os.WriteFile(srcFile, []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	old := time.Date(2018, 4, 4, 4, 4, 4, 0, time.UTC)
	if err := os.Chtimes(srcFile, old, old); err != nil {
		t.Fatal(err)
	}

	dst := filepath.Join(base, "dst")
	if err := os.Mkdir(dst, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dst, "already-there.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	before := time.Now().Add(-time.Second) // a window wide enough for filesystem mtime granularity
	if err := Move(src, dst, MoveOptions{Force: true, Mode: MergeInto, SkipAttributes: true}); err != nil {
		t.Fatalf("Move: %v", err)
	}

	fi, err := os.Stat(filepath.Join(dst, "a.txt"))
	if err != nil {
		t.Fatalf("Stat(dst/a.txt): %v", err)
	}
	if fi.ModTime().Equal(old) {
		t.Errorf("dst/a.txt ModTime = %v, want unequal to src's own %v — SkipAttributes should have reached Move's own Copy-based fallback too", fi.ModTime(), old)
	}
	if fi.ModTime().Before(before) {
		t.Errorf("dst/a.txt ModTime = %v, want at or after %v (its own real creation time)", fi.ModTime(), before)
	}
}

// TestMoveMergeFallbackStableSymlinksRewritesAbsoluteInternalLink pins
// StableSymlinks' own equivalent threading, using an absolute internal
// link — the shape that's meaningfully rewritten regardless of tree
// structure (see stableSymlinkTarget's own doc comment in copy.go for
// why a plain relative link wouldn't discriminate this case at all).
func TestMoveMergeFallbackStableSymlinksRewritesAbsoluteInternalLink(t *testing.T) {
	base := t.TempDir()
	src := filepath.Join(base, "src")
	if err := os.Mkdir(src, 0o755); err != nil {
		t.Fatal(err)
	}
	real := filepath.Join(src, "real.txt")
	if err := os.WriteFile(real, []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, filepath.Join(src, "link.txt")); err != nil {
		t.Fatal(err)
	}

	dst := filepath.Join(base, "dst")
	if err := os.Mkdir(dst, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dst, "already-there.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := Move(src, dst, MoveOptions{Force: true, Mode: MergeInto, StableSymlinks: true}); err != nil {
		t.Fatalf("Move: %v", err)
	}

	got, err := os.Readlink(filepath.Join(dst, "link.txt"))
	if err != nil {
		t.Fatalf("Readlink: %v", err)
	}
	wantTarget := filepath.Join(dst, "real.txt")
	if got != wantTarget {
		t.Errorf("moved link target = %q, want %q — StableSymlinks should have reached Move's own Copy-based fallback too", got, wantTarget)
	}
}

// TestMoveFollowingSymlinksOnATopLevelSymlinkRemovesOnlyTheLink pins the
// user's own explicit safety requirement: cutting a symlink with
// dereferencing on must never touch whatever the link points to, no
// matter how far away that lives — "remote" is simulated here as an
// entirely separate sibling directory (the same shape an NFS/EFS mount
// point would have: some other path this function never descends into
// to remove anything), so a bug that accidentally removed the *resolved*
// path instead of the literal symlink path would delete this sibling
// and fail the test loudly, rather than the danger only ever showing up
// against a real network mount.
func TestMoveFollowingSymlinksOnATopLevelSymlinkRemovesOnlyTheLink(t *testing.T) {
	base := t.TempDir()
	remote := filepath.Join(base, "remote-share") // stands in for an NFS/EFS mount
	if err := os.MkdirAll(remote, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(remote, "data.txt"), []byte("remote content"), 0o640); err != nil {
		t.Fatal(err)
	}

	link := filepath.Join(base, "link-to-remote")
	if err := os.Symlink(remote, link); err != nil {
		t.Fatal(err)
	}

	dst := filepath.Join(base, "materialized")
	if err := MoveFollowingSymlinks(link, dst, MoveOptions{}); err != nil {
		t.Fatalf("MoveFollowingSymlinks: %v", err)
	}

	if fi, err := os.Lstat(dst); err != nil || !fi.IsDir() || fi.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("dst should be a real directory, not a symlink (mode %v, err %v)", fi.Mode(), err)
	}
	if got, err := os.ReadFile(filepath.Join(dst, "data.txt")); err != nil || string(got) != "remote content" {
		t.Errorf("dst/data.txt = %q, %v, want %q", got, err, "remote content")
	}
	if _, err := os.Lstat(link); !os.IsNotExist(err) {
		t.Errorf("the original symlink should be gone, Lstat err = %v", err)
	}

	// The actual point of this test: the "remote" target itself must
	// still be there, completely untouched, not silently wiped out by a
	// removal step that resolved the link instead of using its own
	// literal path.
	if got, err := os.ReadFile(filepath.Join(remote, "data.txt")); err != nil || string(got) != "remote content" {
		t.Fatalf("the symlink's own target must survive untouched, got %q, %v, want %q", got, err, "remote content")
	}
}

// TestMoveFollowingSymlinksOnARealDirectoryRemovesOnlyTheNestedLink
// pins the same guarantee one level down: a real (non-symlink) directory
// that is itself being cut, but merely *contains* a symlink somewhere
// inside it pointing at a "remote" share, must still only ever lose that
// nested symlink's own entry when its source is removed — os.RemoveAll
// never follows a symlink while descending, so the remote target must
// come through unscathed here too.
func TestMoveFollowingSymlinksOnARealDirectoryRemovesOnlyTheNestedLink(t *testing.T) {
	base := t.TempDir()
	remote := filepath.Join(base, "remote-share")
	if err := os.MkdirAll(remote, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(remote, "data.txt"), []byte("remote content"), 0o640); err != nil {
		t.Fatal(err)
	}

	src := filepath.Join(base, "src")
	if err := os.MkdirAll(src, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "plain.txt"), []byte("plain"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(remote, filepath.Join(src, "nested-link")); err != nil {
		t.Fatal(err)
	}

	dst := filepath.Join(base, "dst")
	if err := MoveFollowingSymlinks(src, dst, MoveOptions{}); err != nil {
		t.Fatalf("MoveFollowingSymlinks: %v", err)
	}

	if got, err := os.ReadFile(filepath.Join(dst, "plain.txt")); err != nil || string(got) != "plain" {
		t.Errorf("dst/plain.txt = %q, %v, want %q", got, err, "plain")
	}
	if got, err := os.ReadFile(filepath.Join(dst, "nested-link", "data.txt")); err != nil || string(got) != "remote content" {
		t.Errorf("dst/nested-link should be a real, materialized copy of the remote content, got %q, %v", got, err)
	}
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Errorf("src should be entirely gone after a successful cut, stat err = %v", err)
	}

	// Again the actual point: the remote share itself must be untouched.
	if got, err := os.ReadFile(filepath.Join(remote, "data.txt")); err != nil || string(got) != "remote content" {
		t.Fatalf("the nested symlink's own target must survive untouched, got %q, %v, want %q", got, err, "remote content")
	}
}

// TestMoveFollowingSymlinksOnAFileSymlinkRemovesOnlyTheLink is the
// single-file counterpart: a symlink to a plain file, not a directory.
func TestMoveFollowingSymlinksOnAFileSymlinkRemovesOnlyTheLink(t *testing.T) {
	base := t.TempDir()
	target := filepath.Join(base, "target.txt")
	if err := os.WriteFile(target, []byte("original"), 0o640); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(base, "link.txt")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}

	dst := filepath.Join(base, "dst.txt")
	if err := MoveFollowingSymlinks(link, dst, MoveOptions{}); err != nil {
		t.Fatalf("MoveFollowingSymlinks: %v", err)
	}

	if fi, err := os.Lstat(dst); err != nil || fi.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("dst should be a real file, not a symlink (mode %v, err %v)", fi.Mode(), err)
	}
	if _, err := os.Lstat(link); !os.IsNotExist(err) {
		t.Errorf("the original symlink should be gone, Lstat err = %v", err)
	}
	if got, err := os.ReadFile(target); err != nil || string(got) != "original" {
		t.Fatalf("the symlink's own target file must survive untouched, got %q, %v", got, err)
	}
}

// TestMoveFollowingSymlinksLeavesSourceUntouchedWhenCopyFails pins the
// same ordering Move's own EXDEV/merge fallback already guarantees:
// nothing is ever removed from src unless the new copy at dst is
// already safely in place. Forced here via an existing, non-forced dst
// — the cheapest way to make the Copy step fail without needing a real
// EXDEV condition.
func TestMoveFollowingSymlinksLeavesSourceUntouchedWhenCopyFails(t *testing.T) {
	base := t.TempDir()
	target := filepath.Join(base, "target.txt")
	if err := os.WriteFile(target, []byte("original"), 0o640); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(base, "link.txt")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(base, "dst.txt")
	if err := os.WriteFile(dst, []byte("already here"), 0o640); err != nil {
		t.Fatal(err)
	}

	if err := MoveFollowingSymlinks(link, dst, MoveOptions{}); err == nil {
		t.Fatal("MoveFollowingSymlinks should have refused — dst already exists and Force is false")
	}

	if _, err := os.Lstat(link); err != nil {
		t.Errorf("the original symlink should still be there after a failed copy, Lstat err = %v", err)
	}
	if got, err := os.ReadFile(target); err != nil || string(got) != "original" {
		t.Errorf("the symlink's own target must be untouched, got %q, %v", got, err)
	}
}
