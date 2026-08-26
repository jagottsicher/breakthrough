package fsops

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestListDir(t *testing.T) {
	dir := t.TempDir()

	mustCreate := func(name string, isDir bool) {
		t.Helper()
		path := filepath.Join(dir, name)
		if isDir {
			if err := os.Mkdir(path, 0o755); err != nil {
				t.Fatal(err)
			}
			return
		}
		if err := os.WriteFile(path, nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// Created in an order that would NOT already match the expected
	// sorted output, so the test actually exercises the sort.
	mustCreate("zeta.txt", false)
	mustCreate("Alpha", true)
	mustCreate("beta.txt", false)
	mustCreate("Omega", true)

	entries, err := ListDir(dir)
	if err != nil {
		t.Fatalf("ListDir: %v", err)
	}

	// Name/IsDir/Type only — Nlink is left out deliberately: a freshly
	// created empty directory's exact link count (2, from its own "."
	// entry) is a POSIX convention this test shouldn't have to assume
	// holds on every filesystem CI happens to run on. TestListDirNlink
	// covers Nlink specifically, for a case where the exact count
	// actually matters and is unambiguous (a file with one extra hard
	// link).
	want := []struct {
		name  string
		isDir bool
		typ   EntryType
	}{
		{"Alpha", true, TypeDir},
		{"Omega", true, TypeDir},
		{"beta.txt", false, TypeFile},
		{"zeta.txt", false, TypeFile},
	}

	if len(entries) != len(want) {
		t.Fatalf("got %d entries, want %d: %+v", len(entries), len(want), entries)
	}
	for i, w := range want {
		if entries[i].Name != w.name || entries[i].IsDir != w.isDir || entries[i].Type != w.typ {
			t.Errorf("entry %d = %+v, want name=%q isDir=%v type=%v", i, entries[i], w.name, w.isDir, w.typ)
		}
	}
}

// TestListDirSymlinks pins how ListDir classifies the three symlink
// cases: valid to a file, valid to a directory (which also makes IsDir
// true, so it sorts and navigates like a real directory now — see
// Entry.IsDir's doc comment), and broken.
func TestListDirSymlinks(t *testing.T) {
	dir := t.TempDir()

	target := filepath.Join(dir, "target.txt")
	if err := os.WriteFile(target, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	targetDir := filepath.Join(dir, "target-dir")
	if err := os.Mkdir(targetDir, 0o755); err != nil {
		t.Fatal(err)
	}

	links := map[string]string{
		"link-to-file.txt": target,
		"link-to-dir":      targetDir,
		"link-broken":      filepath.Join(dir, "does-not-exist"),
	}
	for name, dst := range links {
		if err := os.Symlink(dst, filepath.Join(dir, name)); err != nil {
			t.Fatal(err)
		}
	}

	entries, err := ListDir(dir)
	if err != nil {
		t.Fatalf("ListDir: %v", err)
	}

	byName := make(map[string]Entry, len(entries))
	for _, e := range entries {
		byName[e.Name] = e
	}

	file, ok := byName["link-to-file.txt"]
	if !ok || file.Type != TypeSymlinkFile || file.IsDir || file.LinkTarget != target {
		t.Errorf("link-to-file.txt = %+v, want Type=TypeSymlinkFile IsDir=false LinkTarget=%q", file, target)
	}

	linkDir, ok := byName["link-to-dir"]
	if !ok || linkDir.Type != TypeSymlinkDir || !linkDir.IsDir || linkDir.LinkTarget != targetDir {
		t.Errorf("link-to-dir = %+v, want Type=TypeSymlinkDir IsDir=true LinkTarget=%q", linkDir, targetDir)
	}

	broken, ok := byName["link-broken"]
	if !ok || broken.Type != TypeSymlinkBroken || broken.IsDir {
		t.Errorf("link-broken = %+v, want Type=TypeSymlinkBroken IsDir=false", broken)
	}
	if broken.LinkTarget == "" {
		t.Error("a broken symlink should still report its (dangling) LinkTarget")
	}
}

// TestListDirNlink pins the hard-link count: a file with one extra link
// (created via os.Link) reports Nlink=2, unlike an ordinary file's 1.
// TestListDirSizeAndModTime pins that Size/ModTime are populated from
// the entry's own Lstat, matching what os.Stat itself reports.
func TestListDirSizeAndModTime(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(path, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}

	want, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}

	entries, err := ListDir(dir)
	if err != nil {
		t.Fatalf("ListDir: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("got %d entries, want 1", len(entries))
	}

	if entries[0].Size != want.Size() {
		t.Errorf("Size = %d, want %d", entries[0].Size, want.Size())
	}
	if !entries[0].ModTime.Equal(want.ModTime()) {
		t.Errorf("ModTime = %v, want %v", entries[0].ModTime, want.ModTime())
	}
}

func TestListDirNlink(t *testing.T) {
	dir := t.TempDir()
	original := filepath.Join(dir, "original.txt")
	if err := os.WriteFile(original, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(original, filepath.Join(dir, "hardlink.txt")); err != nil {
		t.Skipf("hard links not supported here: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "lonely.txt"), nil, 0o644); err != nil {
		t.Fatal(err)
	}

	entries, err := ListDir(dir)
	if err != nil {
		t.Fatalf("ListDir: %v", err)
	}

	byName := make(map[string]Entry, len(entries))
	for _, e := range entries {
		byName[e.Name] = e
	}

	if got := byName["original.txt"].Nlink; got != 2 {
		t.Errorf("original.txt Nlink = %d, want 2", got)
	}
	if got := byName["hardlink.txt"].Nlink; got != 2 {
		t.Errorf("hardlink.txt Nlink = %d, want 2", got)
	}
	if got := byName["lonely.txt"].Nlink; got != 1 {
		t.Errorf("lonely.txt Nlink = %d, want 1", got)
	}
}

// TestListDirMountPointFalseForOrdinaryDir pins the negative case: an
// ordinary subdirectory, on the same filesystem as its parent, must not
// be flagged as a mount point. Actually mounting something (a bind mount,
// tmpfs, ...) to test the positive case needs privileges this test
// environment can't assume it has, so that side is left unverified by an
// automated test — the device comparison itself (mountPointVia) is
// simple enough that this negative case, plus a read, is what matters
// most to catch a regression in.
func TestListDirMountPointFalseForOrdinaryDir(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}

	entries, err := ListDir(dir)
	if err != nil {
		t.Fatalf("ListDir: %v", err)
	}
	if len(entries) != 1 || entries[0].MountPoint {
		t.Errorf("an ordinary subdirectory should not be reported as a mount point, got %+v", entries)
	}
}

func TestListDirNonExistent(t *testing.T) {
	if _, err := ListDir(filepath.Join(t.TempDir(), "does-not-exist")); err == nil {
		t.Fatal("expected an error for a non-existent directory, got nil")
	}
}

// TestListDirLarge is a real-world stress test for the "does this even
// work with tens of thousands of entries?" question: it creates 20,000
// files, calls ListDir, and checks both that all of them come back and
// that the result is actually sorted. It logs how long ListDir itself
// took (file creation dominates the test's total time, not ListDir).
func TestListDirLarge(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping large-directory test in -short mode")
	}

	const n = 20000
	dir := t.TempDir()
	for i := 0; i < n; i++ {
		name := fmt.Sprintf("file-%05d.txt", i)
		if err := os.WriteFile(filepath.Join(dir, name), nil, 0o644); err != nil {
			t.Fatalf("creating %s: %v", name, err)
		}
	}

	start := time.Now()
	entries, err := ListDir(dir)
	elapsed := time.Since(start)
	t.Logf("ListDir on %d entries took %v", n, elapsed)

	if err != nil {
		t.Fatalf("ListDir: %v", err)
	}
	if len(entries) != n {
		t.Fatalf("got %d entries, want %d", len(entries), n)
	}
	for i := 1; i < len(entries); i++ {
		if entries[i-1].Name >= entries[i].Name {
			t.Fatalf("not sorted at index %d: %q >= %q", i, entries[i-1].Name, entries[i].Name)
		}
	}
}

// TestDescribeEntryMatchesListDir pins DescribeEntry's own central
// claim: classifying one path in isolation comes back byte-for-byte
// identical to what ListDir already reports for that same path as one
// of a directory's many children — including LinkTarget/IsDir/MountPoint
// for a directory symlink specifically, the one case that depends on
// DescribeEntry resolving its own parentDev correctly (see its own doc
// comment) rather than being handed one by a caller that already read
// the whole directory.
func TestDescribeEntryMatchesListDir(t *testing.T) {
	dir := t.TempDir()

	target := filepath.Join(dir, "target.txt")
	if err := os.WriteFile(target, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	targetDir := filepath.Join(dir, "target-dir")
	if err := os.Mkdir(targetDir, 0o755); err != nil {
		t.Fatal(err)
	}
	links := map[string]string{
		"link-to-file.txt": target,
		"link-to-dir":      targetDir,
		"link-broken":      filepath.Join(dir, "does-not-exist"),
	}
	for name, dst := range links {
		if err := os.Symlink(dst, filepath.Join(dir, name)); err != nil {
			t.Fatal(err)
		}
	}

	entries, err := ListDir(dir)
	if err != nil {
		t.Fatalf("ListDir: %v", err)
	}

	for _, want := range entries {
		got := DescribeEntry(filepath.Join(dir, want.Name))
		if got != want {
			t.Errorf("DescribeEntry(%q) = %+v, want %+v (from ListDir)", want.Name, got, want)
		}
	}
}

// TestDescribeEntryNonExistent pins the same "vanished/never existed"
// fallback describeEntry already gives ListDir for a race between
// os.ReadDir and Lstat — DescribeEntry has no ReadDir step to race
// with, but a search result can just as easily name a path that's been
// deleted since the search that found it ran, so the same graceful
// fallback (a plain TypeFile, not an error) matters here too.
func TestDescribeEntryNonExistent(t *testing.T) {
	got := DescribeEntry(filepath.Join(t.TempDir(), "does-not-exist"))
	want := Entry{Name: "does-not-exist", Type: TypeFile}
	if got != want {
		t.Errorf("DescribeEntry(nonexistent) = %+v, want %+v", got, want)
	}
}

// TestDescribeEntryUnreadable pins canRead's own real access(2) check
// (see its doc comment on why this isn't just a Mode bit comparison) for
// every case describeEntry sets it for: a plain file, a directory (which
// needs execute as well as read to be listable — checked separately from
// the read-only case below, since a naive R_OK-only check would get this
// one wrong), and a symlink to each. Skipped entirely under root, which
// bypasses every permission check these cases rely on.
func TestDescribeEntryUnreadable(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root: permission checks never fail, this test doesn't apply")
	}

	dir := t.TempDir()

	readableFile := filepath.Join(dir, "readable.txt")
	if err := os.WriteFile(readableFile, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	unreadableFile := filepath.Join(dir, "unreadable.txt")
	if err := os.WriteFile(unreadableFile, nil, 0o000); err != nil {
		t.Fatal(err)
	}
	readableDir := filepath.Join(dir, "readable-dir")
	if err := os.Mkdir(readableDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// A directory with its read bit set but not its execute ("search")
	// bit: readdir(3) alone isn't enough to actually list a directory's
	// entries, so this must still come back Unreadable — the specific
	// case a plain "check R_OK" implementation would get wrong.
	unreadableDirNoExec := filepath.Join(dir, "unreadable-dir-no-exec")
	if err := os.Mkdir(unreadableDirNoExec, 0o600); err != nil {
		t.Fatal(err)
	}
	unreadableDir := filepath.Join(dir, "unreadable-dir")
	if err := os.Mkdir(unreadableDir, 0o000); err != nil {
		t.Fatal(err)
	}

	symlinkToReadableFile := filepath.Join(dir, "link-to-readable-file")
	if err := os.Symlink(readableFile, symlinkToReadableFile); err != nil {
		t.Fatal(err)
	}
	symlinkToUnreadableFile := filepath.Join(dir, "link-to-unreadable-file")
	if err := os.Symlink(unreadableFile, symlinkToUnreadableFile); err != nil {
		t.Fatal(err)
	}
	symlinkToUnreadableDir := filepath.Join(dir, "link-to-unreadable-dir")
	if err := os.Symlink(unreadableDir, symlinkToUnreadableDir); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name string
		path string
		want bool
	}{
		{"readable file", readableFile, false},
		{"unreadable file", unreadableFile, true},
		{"readable dir", readableDir, false},
		{"dir with read but no execute bit", unreadableDirNoExec, true},
		{"dir with no permissions at all", unreadableDir, true},
		{"symlink to a readable file", symlinkToReadableFile, false},
		{"symlink to an unreadable file", symlinkToUnreadableFile, true},
		{"symlink to an unreadable dir", symlinkToUnreadableDir, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DescribeEntry(tt.path)
			if got.Unreadable != tt.want {
				t.Errorf("DescribeEntry(%s).Unreadable = %v, want %v (%+v)", tt.name, got.Unreadable, tt.want, got)
			}
		})
	}
}
