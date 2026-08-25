package search

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestClassifyArchive(t *testing.T) {
	tests := []struct {
		path     string
		wantKind archiveKind
		wantOK   bool
	}{
		{"/a/b/docs.zip", archiveZip, true},
		{"/a/b/DOCS.ZIP", archiveZip, true}, // case-insensitive, see classifyArchive's own doc comment
		{"/a/b/backup.tar", archiveTar, true},
		{"/a/b/backup.tar.gz", archiveTar, true},
		{"/a/b/backup.tar.bz2", archiveTar, true},
		{"/a/b/backup.tar.xz", archiveTar, true},
		{"/a/b/backup.tgz", 0, false}, // short alias, deliberately not in "Stufe A" — see archiveExtensions' own doc comment
		{"/a/b/notes.txt", 0, false},
		{"/a/b/lone.gz", 0, false}, // a lone .gz isn't in archiveExtensions at all — see its own doc comment
	}
	for _, tt := range tests {
		kind, ok := classifyArchive(tt.path)
		if ok != tt.wantOK || (ok && kind != tt.wantKind) {
			t.Errorf("classifyArchive(%q) = (%v, %v), want (%v, %v)", tt.path, kind, ok, tt.wantKind, tt.wantOK)
		}
	}
}

func TestArchiveMemberMatchesGlob(t *testing.T) {
	if !archiveMemberMatches("dir/abcdefg.txt", "abc*", ModeGlob, false) {
		t.Error("want glob match against the base name")
	}
	if archiveMemberMatches("dir/xyz.txt", "abc*", ModeGlob, false) {
		t.Error("want no match")
	}
}

func TestArchiveMemberMatchesGlobIgnoresDirComponent(t *testing.T) {
	// "abc*" must match "cdeabc123.help"'s own base name, not the "abc"
	// directory component earlier in the path — the same base-name-only
	// scope FindArgs' own -iname already has (see archiveMemberMatches'
	// own doc comment).
	if archiveMemberMatches("dir/abc/furzi/cdeabc123.help", "abc*", ModeGlob, false) {
		t.Error("glob must not match a directory component, only the base name")
	}
}

func TestArchiveMemberMatchesDirectoryTrailingSlash(t *testing.T) {
	// A directory member ("dir/blib/abcdefg/blubber/", trailing slash —
	// see listArchiveMembers) must still match on its own real name,
	// not get defeated by the slash itself.
	if !archiveMemberMatches("dir/blib/abcdefg/blubber/", "blubber", ModeKeyword, false) {
		t.Error("want a directory member's own trailing slash stripped before matching")
	}
}

func TestArchiveMemberMatchesKeywordWrapsWithWildcards(t *testing.T) {
	if !archiveMemberMatches("dir/report-final.txt", "final", ModeKeyword, false) {
		t.Error("want keyword mode to match as a substring")
	}
}

func TestArchiveMemberMatchesRegexWholePath(t *testing.T) {
	if !archiveMemberMatches("dir/sub/abc123.log", `sub/.*\.log$`, ModeRegex, false) {
		t.Error("want regex mode matched against the whole member path, not just the base name")
	}
}

func TestArchiveMemberMatchesCaseSensitivity(t *testing.T) {
	if !archiveMemberMatches("dir/ABC.txt", "abc*", ModeGlob, false) {
		t.Error("want case-insensitive match by default")
	}
	if archiveMemberMatches("dir/ABC.txt", "abc*", ModeGlob, true) {
		t.Error("want no match once CaseSensitive is on")
	}
}

func TestFindArchiveArgsGroupsExtensions(t *testing.T) {
	got := findArchiveArgs("/home/jens", nil, false, false)
	want := []string{
		"/home/jens",
		"(", "-iname", "*.zip",
		"-o", "-iname", "*.tar",
		"-o", "-iname", "*.tar.gz",
		"-o", "-iname", "*.tar.bz2",
		"-o", "-iname", "*.tar.xz",
		")", "-print0",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("findArchiveArgs = %v, want %v", got, want)
	}
}

func TestFindArchiveArgsIgnoreDirsAddsPruneClauseAheadOfExtensions(t *testing.T) {
	got := findArchiveArgs("/home/jens", []string{".git", "node_modules"}, false, false)
	want := []string{
		"/home/jens",
		"(", "-name", ".git", "-o", "-name", "node_modules", ")", "-prune", "-o",
		"(", "-iname", "*.zip",
		"-o", "-iname", "*.tar",
		"-o", "-iname", "*.tar.gz",
		"-o", "-iname", "*.tar.bz2",
		"-o", "-iname", "*.tar.xz",
		")", "-print0",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("findArchiveArgs = %v, want %v", got, want)
	}
}

func TestFindArchiveArgsNonRecursiveAndFollowSymlinks(t *testing.T) {
	got := findArchiveArgs("/home/jens", nil, true, true)
	if got[0] != "-L" {
		t.Errorf("want -L first, got %v", got)
	}
	if got[1] != "/home/jens" || got[2] != "-maxdepth" || got[3] != "1" {
		t.Errorf("want -maxdepth 1 right after scope, got %v", got)
	}
}

func TestUnderScope(t *testing.T) {
	tests := []struct {
		path, scope string
		want        bool
	}{
		{"/home/jens/pictures/x.zip", "/home/jens", true},
		{"/home/jens", "/home/jens", true},         // scope itself
		{"/home/jens2/x.zip", "/home/jens", false}, // sibling with a shared prefix — the real bug plain HasPrefix would have, see underScope's own doc comment
		{"/var/log/x.zip", "/home/jens", false},
	}
	for _, tt := range tests {
		if got := underScope(tt.path, tt.scope); got != tt.want {
			t.Errorf("underScope(%q, %q) = %v, want %v", tt.path, tt.scope, got, tt.want)
		}
	}
}

// makeZipFixture creates dir/name.zip with the given member names
// (each an empty file), for a real, no-CLI-tool-needed test fixture —
// the same archive/zip stdlib approach runner_test.go's own
// TestRunContentSearchZip already uses.
func makeZipFixture(t *testing.T, dir, name string, members ...string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	for _, m := range members {
		if _, err := zw.Create(m); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

// makeTarGzFixture creates dir/name.tar.gz with the given member names
// (each an empty regular-file entry), via the archive/tar + compress/
// gzip stdlib — no real tar binary needed to *create* the fixture, only
// to list it later (see listArchiveMembers), the same split
// runner_test.go's own TestRunContentSearchGzip already relies on for
// gzip.
func makeTarGzFixture(t *testing.T, dir, name string, members ...string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	gw := gzip.NewWriter(f)
	tw := tar.NewWriter(gw)
	for _, m := range members {
		if err := tw.WriteHeader(&tar.Header{Name: m, Mode: 0o644, Size: 0}); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestRunIncludeArchivesFindsZipMember(t *testing.T) {
	requireTool(t, "find")
	requireTool(t, "unzip")
	dir := fixtureTree(t) // also has apple.txt/apricot.txt/banana.log/sub/cherry.txt — real files sharing the same search
	zipPath := makeZipFixture(t, dir, "docs.zip", "readme.txt", "notes/abcdefg.txt", "unrelated.log")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	results, errs := Run(ctx, Request{
		Pattern: "abc*", Scope: dir, Mode: ModeGlob, Engine: EngineFind, IncludeArchives: true,
	})

	got := collectResults(t, results, errs)
	if len(got) != 1 {
		t.Fatalf("got %d results, want exactly 1 (the archive member match): %+v", len(got), got)
	}
	if got[0].Path != zipPath {
		t.Errorf("Path = %q, want the real archive path %q", got[0].Path, zipPath)
	}
	if got[0].ArchiveMember != "notes/abcdefg.txt" {
		t.Errorf("ArchiveMember = %q, want %q", got[0].ArchiveMember, "notes/abcdefg.txt")
	}
}

func TestRunIncludeArchivesFindsTarGzMember(t *testing.T) {
	requireTool(t, "find")
	requireTool(t, "tar")
	dir := t.TempDir()
	tarPath := makeTarGzFixture(t, dir, "backup.tar.gz", "readme.txt", "inner/cdeabc123.help")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	results, errs := Run(ctx, Request{
		Pattern: "*.help", Scope: dir, Mode: ModeGlob, Engine: EngineFind, IncludeArchives: true,
	})

	got := collectResults(t, results, errs)
	if len(got) != 1 || got[0].ArchiveMember != "inner/cdeabc123.help" || got[0].Path != tarPath {
		t.Errorf("got %+v, want exactly one match: Path=%q ArchiveMember=%q", got, tarPath, "inner/cdeabc123.help")
	}
}

func TestRunIncludeArchivesCombinesWithRealFileMatches(t *testing.T) {
	requireTool(t, "find")
	requireTool(t, "unzip")
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "abcreal.txt"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	zipPath := makeZipFixture(t, dir, "docs.zip", "abcinside.txt")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	results, errs := Run(ctx, Request{
		Pattern: "abc*", Scope: dir, Mode: ModeGlob, Engine: EngineFind, IncludeArchives: true,
	})

	got := collectResults(t, results, errs)
	if len(got) != 2 {
		t.Fatalf("got %d results, want 2 (one real file, one archive member): %+v", len(got), got)
	}
	var sawReal, sawArchive bool
	for _, r := range got {
		switch {
		case r.ArchiveMember == "" && r.Path == filepath.Join(dir, "abcreal.txt"):
			sawReal = true
		case r.ArchiveMember == "abcinside.txt" && r.Path == zipPath:
			sawArchive = true
		}
	}
	if !sawReal || !sawArchive {
		t.Errorf("got %+v, want both the real file and the archive member represented", got)
	}
}

func TestRunIncludeArchivesOffByDefaultLeavesArchivesUnopened(t *testing.T) {
	requireTool(t, "find")
	dir := t.TempDir()
	makeZipFixture(t, dir, "docs.zip", "abcdefg.txt")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	// IncludeArchives left false (the zero value) — docs.zip itself
	// doesn't match "abc*" by name, and its own member shouldn't be
	// looked at at all.
	results, errs := Run(ctx, Request{Pattern: "abc*", Scope: dir, Mode: ModeGlob, Engine: EngineFind})

	got := collectResults(t, results, errs)
	if len(got) != 0 {
		t.Errorf("got %+v, want no results — Include Archives is off", got)
	}
}

// TestRunCancelStopsPromptlyWithArchives is TestRunCancelStopsPromptly's
// own counterpart with IncludeArchives on — pins that startArchiveSearch's
// own goroutine (see runner.go) never leaves Run's results channel open,
// or panics from a send on a closed channel, once ctx is cancelled while
// it might still be mid-listing.
func TestRunCancelStopsPromptlyWithArchives(t *testing.T) {
	requireTool(t, "find")
	requireTool(t, "unzip")
	dir := t.TempDir()
	for i := 0; i < 20; i++ {
		makeZipFixture(t, dir, fmt.Sprintf("a%d.zip", i), "abcdefg.txt")
	}

	ctx, cancel := context.WithCancel(context.Background())
	results, errs := Run(ctx, Request{Pattern: "abc*", Scope: dir, Mode: ModeGlob, Engine: EngineFind, IncludeArchives: true})
	cancel() // cancel immediately, before reading anything

	drained := make(chan struct{})
	go func() {
		for range results {
		}
		close(drained)
	}()
	select {
	case <-drained:
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not stop within 5s of ctx being cancelled")
	}
	select {
	case err := <-errs:
		if err != nil {
			t.Errorf("unexpected error after cancellation: %v", err)
		}
	default:
	}
}
