package fsops

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// TestOverlaps pins its own exact contract: src itself and anything
// underneath it overlap (dst == src, and any depth of descendant);
// a sibling, the parent, or anywhere else entirely does not.
func TestOverlaps(t *testing.T) {
	tests := []struct {
		src, dst string
		want     bool
	}{
		{"/a/b", "/a/b", true},          // exact match
		{"/a/b", "/a/b/c", true},        // direct child
		{"/a/b", "/a/b/c/d/e", true},    // deep descendant
		{"/a/b", "/a/b-sibling", false}, // shares a prefix as a string, but isn't a real path descendant
		{"/a/b", "/a/c", false},         // sibling
		{"/a/b", "/a", false},           // parent
		{"/a/b", "/x/y", false},         // unrelated
		{"/a/b/", "/a/b/c", true},       // trailing slash on src shouldn't matter
		{"/a/b", "/a/b/../b/c", true},   // dst with ".." components that still resolve underneath src
	}
	for _, tt := range tests {
		if got := Overlaps(tt.src, tt.dst); got != tt.want {
			t.Errorf("Overlaps(%q, %q) = %v, want %v", tt.src, tt.dst, got, tt.want)
		}
	}
}

func TestCopyFile(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.txt")
	if err := os.WriteFile(src, []byte("hello"), 0o640); err != nil {
		t.Fatal(err)
	}

	dst := filepath.Join(dir, "dst.txt")
	if err := Copy(src, dst, CopyOptions{}); err != nil {
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

	if err := Copy(src, dst, CopyOptions{}); err == nil {
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

	if err := Copy(src, dst, CopyOptions{Force: true}); err != nil {
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

// TestCopyMergeIntoKeepsExtraFilesNotInSource pins MergeInto's own
// documented behavior, unchanged from Copy's original, only-ever
// behavior before OverwriteMode existed: an existing dst file src
// doesn't also have survives untouched.
func TestCopyMergeIntoKeepsExtraFilesNotInSource(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	if err := os.MkdirAll(src, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "a.txt"), []byte("clean"), 0o640); err != nil {
		t.Fatal(err)
	}

	dst := filepath.Join(dir, "dst")
	if err := os.MkdirAll(dst, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dst, "a.txt"), []byte("old"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dst, "planted.txt"), []byte("not from source"), 0o640); err != nil {
		t.Fatal(err)
	}

	if err := Copy(src, dst, CopyOptions{Force: true, Mode: MergeInto}); err != nil {
		t.Fatalf("Copy: %v", err)
	}

	if got, err := os.ReadFile(filepath.Join(dst, "a.txt")); err != nil || string(got) != "clean" {
		t.Errorf("dst/a.txt = %q, %v, want %q", got, err, "clean")
	}
	if got, err := os.ReadFile(filepath.Join(dst, "planted.txt")); err != nil || string(got) != "not from source" {
		t.Errorf("dst/planted.txt = %q, %v, want it left untouched by a merge", got, err)
	}
}

// TestCopyReplaceEntirelyRemovesExtraFilesNotInSource pins the actual
// user-reported gap ReplaceEntirely exists to close: a merge-only
// "overwrite" left a destination file with no matching source entry
// (e.g. a WordPress wp-admin file planted by a compromise, being
// replaced from a known-clean source) sitting there completely
// untouched — meaningless for a security-motivated "make this
// identical to the clean copy" replace. ReplaceEntirely must remove it.
func TestCopyReplaceEntirelyRemovesExtraFilesNotInSource(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	if err := os.MkdirAll(src, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "a.txt"), []byte("clean"), 0o640); err != nil {
		t.Fatal(err)
	}

	dst := filepath.Join(dir, "dst")
	if err := os.MkdirAll(dst, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dst, "a.txt"), []byte("old"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dst, "planted.txt"), []byte("malicious"), 0o640); err != nil {
		t.Fatal(err)
	}

	if err := Copy(src, dst, CopyOptions{Force: true, Mode: ReplaceEntirely}); err != nil {
		t.Fatalf("Copy: %v", err)
	}

	if got, err := os.ReadFile(filepath.Join(dst, "a.txt")); err != nil || string(got) != "clean" {
		t.Errorf("dst/a.txt = %q, %v, want %q", got, err, "clean")
	}
	if _, err := os.Stat(filepath.Join(dst, "planted.txt")); !os.IsNotExist(err) {
		t.Errorf("dst/planted.txt should be gone after ReplaceEntirely, stat err = %v", err)
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
	if err := Copy(src, dst, CopyOptions{}); err != nil {
		t.Fatalf("Copy: %v", err)
	}

	if got, err := os.ReadFile(filepath.Join(dst, "a.txt")); err != nil || string(got) != "a" {
		t.Errorf("dst/a.txt = %q, %v, want %q, nil", got, err, "a")
	}
	if got, err := os.ReadFile(filepath.Join(dst, "sub", "b.txt")); err != nil || string(got) != "b" {
		t.Errorf("dst/sub/b.txt = %q, %v, want %q, nil", got, err, "b")
	}
}

// TestCopyReportsEveryFileViaOnFile pins onFile's own documented
// contract: called once per real file or symlink actually copied,
// recursively for a directory, never for a directory itself (MkdirAll
// is instant — nothing to report progress on).
func TestCopyReportsEveryFileViaOnFile(t *testing.T) {
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

	var reported []string
	dst := filepath.Join(dir, "dst")
	if err := Copy(src, dst, CopyOptions{OnFile: func(path string) { reported = append(reported, path) }}); err != nil {
		t.Fatalf("Copy: %v", err)
	}

	want := []string{filepath.Join(src, "a.txt"), filepath.Join(src, "sub", "b.txt")}
	if len(reported) != len(want) {
		t.Fatalf("onFile reported %v, want %v", reported, want)
	}
	sort.Strings(reported)
	sort.Strings(want)
	for i := range want {
		if reported[i] != want[i] {
			t.Errorf("onFile reported %v, want %v", reported, want)
			break
		}
	}
}

// TestCopyReportsRunningTotalViaOnBytes pins OnBytes' own contract: a
// cumulative running total *per file* (not across the whole Copy call
// — see its own doc comment), reaching that file's exact full size by
// the time the last chunk lands, and never called at all for a
// directory itself or for a symlink recreated as a symlink (nothing
// streamed either way).
func TestCopyReportsRunningTotalViaOnBytes(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	content := strings.Repeat("x", 5000) // several read buffers' worth, not just one
	if err := os.WriteFile(src, []byte(content), 0o640); err != nil {
		t.Fatal(err)
	}

	var reported []int64
	dst := filepath.Join(dir, "dst")
	if err := Copy(src, dst, CopyOptions{OnBytes: func(n int64) { reported = append(reported, n) }}); err != nil {
		t.Fatalf("Copy: %v", err)
	}

	if len(reported) == 0 {
		t.Fatal("OnBytes was never called")
	}
	for i := 1; i < len(reported); i++ {
		if reported[i] < reported[i-1] {
			t.Fatalf("OnBytes reported a non-monotonic sequence: %v", reported)
		}
	}
	if last := reported[len(reported)-1]; last != int64(len(content)) {
		t.Errorf("OnBytes' last report = %d, want the file's full size %d", last, len(content))
	}
}

// TestCopyNeverCallsOnBytesForASymlinkRecreatedAsALink pins the other
// half of OnBytes' own contract: recreating a link (the default,
// FollowSymlinks false) is a single name-and-target write, never a
// byte stream, so there's nothing to report a running total of.
func TestCopyNeverCallsOnBytesForASymlinkRecreatedAsALink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target.txt")
	if err := os.WriteFile(target, []byte("hi"), 0o640); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}

	called := false
	dst := filepath.Join(dir, "dst")
	if err := Copy(link, dst, CopyOptions{OnBytes: func(int64) { called = true }}); err != nil {
		t.Fatalf("Copy: %v", err)
	}
	if called {
		t.Error("OnBytes was called for a symlink recreated as a symlink — nothing was actually streamed")
	}
}

// TestCopyRefusesWhenDestinationIsSourceItself pins the actual data-loss
// bug this guards against: before Overlaps existed, force=true's own
// "remove dst first, then open src to recreate it" sequence in
// copyFile — harmless when dst is a genuinely different path — silently
// destroyed the file when dst *was* src, since removing "dst" removed
// the only copy there ever was, and the subsequent open of "src" then
// failed against something no longer there. Checked with force=true
// specifically, the exact path that used to overwrite/delete instead of
// simply refusing to overwrite.
func TestCopyRefusesWhenDestinationIsSourceItself(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "file.txt")
	if err := os.WriteFile(src, []byte("irreplaceable"), 0o640); err != nil {
		t.Fatal(err)
	}

	if err := Copy(src, src, CopyOptions{Force: true}); err == nil {
		t.Fatal("Copy(src, src, ...) should refuse, not silently succeed")
	}

	got, err := os.ReadFile(src)
	if err != nil {
		t.Fatalf("the file is gone after the refused Copy: %v", err)
	}
	if string(got) != "irreplaceable" {
		t.Errorf("file content = %q, want it untouched: %q", got, "irreplaceable")
	}
}

// TestCopyRefusesCopyingDirectoryIntoOwnSubdirectory pins the other
// failure mode Overlaps guards against: copying a directory into one of
// its own subdirectories used to recurse without any bound at all — the
// destination copyDir's own MkdirAll creates physically underneath src
// becomes something a still-pending recursive call over that same
// subtree then discovers on its own next os.ReadDir and copies again,
// on and on, the same "cp: cannot copy a directory into itself" shape
// cp(1) itself refuses outright. Also pins that refusing happens before
// anything is written at all — the destination subdirectory named in
// dst must not even exist afterward, not just "stopped partway".
func TestCopyRefusesCopyingDirectoryIntoOwnSubdirectory(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "foo")
	sub := filepath.Join(src, "bar")
	if err := os.MkdirAll(sub, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "file.txt"), []byte("x"), 0o640); err != nil {
		t.Fatal(err)
	}

	dst := filepath.Join(sub, "foo") // foo/bar/foo — a subdirectory of foo itself
	if err := Copy(src, dst, CopyOptions{}); err == nil {
		t.Fatal("Copy(foo, foo/bar/foo, ...) should refuse, not recurse")
	}

	if _, err := os.Stat(dst); !os.IsNotExist(err) {
		t.Errorf("dst should never have been created at all, stat err = %v", err)
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
	if err := Copy(link, dst, CopyOptions{}); err != nil {
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

// TestCopyFollowSymlinksCopiesTargetFileContent pins the user's own
// explicit request: an at-least option to dereference a symlink instead
// of always recreating it — dst here must be a real, independent file
// with the target's own content, not a link at all.
func TestCopyFollowSymlinksCopiesTargetFileContent(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target.txt")
	if err := os.WriteFile(target, []byte("real content"), 0o640); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link.txt")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}

	dst := filepath.Join(dir, "dereferenced.txt")
	if err := Copy(link, dst, CopyOptions{FollowSymlinks: true}); err != nil {
		t.Fatalf("Copy: %v", err)
	}

	if fi, err := os.Lstat(dst); err != nil || fi.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("dst should be a real file, not a symlink (mode %v, err %v)", fi.Mode(), err)
	}
	got, err := os.ReadFile(dst)
	if err != nil || string(got) != "real content" {
		t.Errorf("dst content = %q, %v, want %q", got, err, "real content")
	}

	// Modifying the target afterward must not affect dst — it's a real,
	// independent copy now, not still linked to the source in any way.
	if err := os.WriteFile(target, []byte("changed"), 0o640); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(dst); string(got) != "real content" {
		t.Errorf("dst changed after editing target — it should be independent, got %q", got)
	}
}

// TestCopyFollowSymlinksCopiesTargetDirectoryRecursively pins the same
// contract for a symlink to a directory: the whole tree it resolves to
// is copied, recursively, as a real directory — including a further
// symlink found underneath it, still resolved the same way rather than
// only the top-level link.
func TestCopyFollowSymlinksCopiesTargetDirectoryRecursively(t *testing.T) {
	dir := t.TempDir()
	realDir := filepath.Join(dir, "real")
	if err := os.MkdirAll(realDir, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(realDir, "a.txt"), []byte("a"), 0o640); err != nil {
		t.Fatal(err)
	}
	innerTarget := filepath.Join(dir, "inner-target.txt")
	if err := os.WriteFile(innerTarget, []byte("inner"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(innerTarget, filepath.Join(realDir, "inner-link.txt")); err != nil {
		t.Fatal(err)
	}

	link := filepath.Join(dir, "dir-link")
	if err := os.Symlink(realDir, link); err != nil {
		t.Fatal(err)
	}

	dst := filepath.Join(dir, "dst")
	if err := Copy(link, dst, CopyOptions{FollowSymlinks: true}); err != nil {
		t.Fatalf("Copy: %v", err)
	}

	if fi, err := os.Lstat(dst); err != nil || !fi.IsDir() {
		t.Fatalf("dst should be a real directory (mode %v, err %v)", fi.Mode(), err)
	}
	if got, err := os.ReadFile(filepath.Join(dst, "a.txt")); err != nil || string(got) != "a" {
		t.Errorf("dst/a.txt = %q, %v, want %q", got, err, "a")
	}
	if fi, err := os.Lstat(filepath.Join(dst, "inner-link.txt")); err != nil || fi.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("dst/inner-link.txt should also be dereferenced into a real file, not a symlink (mode %v, err %v)", fi.Mode(), err)
	}
	if got, err := os.ReadFile(filepath.Join(dst, "inner-link.txt")); err != nil || string(got) != "inner" {
		t.Errorf("dst/inner-link.txt = %q, %v, want %q", got, err, "inner")
	}
}

// TestCopyFollowSymlinksRefusesWhenTargetOverlapsDestination pins
// Overlaps' own second check inside copySymlink: a symlink whose own
// raw path doesn't overlap dst can still resolve to something that
// does once actually followed — checked against the resolved target,
// not just the link's own literal path.
func TestCopyFollowSymlinksRefusesWhenTargetOverlapsDestination(t *testing.T) {
	dir := t.TempDir()
	realDir := filepath.Join(dir, "real")
	if err := os.MkdirAll(realDir, 0o750); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link") // link's own path doesn't overlap dst below at all
	if err := os.Symlink(realDir, link); err != nil {
		t.Fatal(err)
	}

	dst := filepath.Join(realDir, "sub") // but the link resolves into realDir, and dst is inside *that*
	if err := Copy(link, dst, CopyOptions{FollowSymlinks: true}); err == nil {
		t.Fatal("Copy should refuse — the resolved target overlaps dst")
	}
	if _, err := os.Stat(dst); !os.IsNotExist(err) {
		t.Errorf("dst should never have been created, stat err = %v", err)
	}
}

// TestCopyFollowSymlinksRefusesWhenTargetOverlapsDestinationThroughADifferentAlias
// pins resolveExistingAncestor's own reason for existing: dst run
// through EvalSymlinks-following-src's own resolved path only actually
// catches an overlap if dst is normalized through symlinks the same
// way — otherwise the identical directory on disk can look like two
// unrelated paths purely because one route to it happens to pass
// through a symlink and the other doesn't. Built portably (no reliance
// on any particular OS's own temp-directory layout) by constructing
// exactly that shape by hand: "alias" is a symlink to "actual", and dst
// is expressed through "alias" while the followed link resolves fully
// to "actual" — the same class of mismatch a failing macOS CI run
// caught for real, where /tmp itself sits behind /var -> /private/var.
func TestCopyFollowSymlinksRefusesWhenTargetOverlapsDestinationThroughADifferentAlias(t *testing.T) {
	base := t.TempDir()
	actual := filepath.Join(base, "actual")
	if err := os.MkdirAll(filepath.Join(actual, "real"), 0o750); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(base, "alias")
	if err := os.Symlink(actual, alias); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(base, "link")
	if err := os.Symlink(filepath.Join(alias, "real"), link); err != nil {
		t.Fatal(err)
	}

	// Expressed through the alias, not through actual — the same
	// directory on disk under a differently-spelled path.
	dst := filepath.Join(alias, "real", "sub")
	if err := Copy(link, dst, CopyOptions{FollowSymlinks: true}); err == nil {
		t.Fatal("Copy should refuse — dst is the resolved target's own subdirectory, just spelled through a different alias")
	}
	if _, err := os.Stat(dst); !os.IsNotExist(err) {
		t.Errorf("dst should never have been created, stat err = %v", err)
	}
}
