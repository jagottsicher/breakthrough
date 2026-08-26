package search

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// tarMember is one name/content pair for the fixture builders below.
type tarMember struct {
	name    string
	content string
}

// writeTarMembers writes members to w in tar format (archive/tar
// stdlib) — the shared body behind every *ContentFixture helper below,
// factored out once rather than repeated per compression. Unlike
// archive_test.go's own makeTarGzFixture (member names only, Size: 0),
// these carry real content: a content search needs something to
// actually match against.
func writeTarMembers(t *testing.T, w io.Writer, members []tarMember) {
	t.Helper()
	tw := tar.NewWriter(w)
	for _, m := range members {
		if err := tw.WriteHeader(&tar.Header{Name: m.name, Mode: 0o644, Size: int64(len(m.content))}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(m.content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
}

// makePlainTarContentFixture writes dir/name as an uncompressed tar
// with real member content (see writeTarMembers) — via the archive/tar
// stdlib, no real tar binary needed to build the fixture, only to read
// it back later (see searchOneTarContent/listArchiveMembers), the same
// split makeTarGzFixture's own doc comment (archive_test.go) already
// explains.
func makePlainTarContentFixture(t *testing.T, dir, name string, members []tarMember) string {
	t.Helper()
	path := filepath.Join(dir, name)
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	writeTarMembers(t, f, members)
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

// makeTarGzContentFixture is makePlainTarContentFixture's own
// gzip-compressed counterpart, via compress/gzip's stdlib writer.
func makeTarGzContentFixture(t *testing.T, dir, name string, members []tarMember) string {
	t.Helper()
	path := filepath.Join(dir, name)
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	gw := gzip.NewWriter(f)
	writeTarMembers(t, gw, members)
	if err := gw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

// compressTarFixture builds a plain tar in memory (see writeTarMembers)
// and shells out to tool -c to compress it — Go's stdlib has no writer
// for bzip2 or xz, only compress/bzip2's reader, the same gap
// makeCompressedFixture's own doc comment (compressed_test.go) already
// explains for a plain (non-tar) compressed fixture.
func compressTarFixture(t *testing.T, dir, name, tool string, members []tarMember) string {
	t.Helper()
	requireTool(t, tool)
	var buf bytes.Buffer
	writeTarMembers(t, &buf, members)

	cmd := exec.Command(tool, "-c")
	cmd.Stdin = &buf
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("%s -c: %v", tool, err)
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, out, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestRunIncludeCompressedFindsMatchInsidePlainTarMember(t *testing.T) {
	requireTool(t, "find")
	requireTool(t, "tar")
	requireTool(t, "grep")
	dir := t.TempDir()
	tarPath := makePlainTarContentFixture(t, dir, "backup.tar", []tarMember{
		{"readme.txt", "nothing interesting here\n"},
		{"etc/fstab", "line one\nLeere Zeile folgt\nline three\n"},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	results, errs := Run(ctx, Request{
		Pattern: "leere", Scope: dir, Mode: ModeKeyword, Content: ContentGrep, IncludeCompressed: true,
	})

	got := collectResults(t, results, errs)
	if len(got) != 1 {
		t.Fatalf("got %d results, want exactly 1: %+v", len(got), got)
	}
	if got[0].Path != tarPath || got[0].ArchiveMember != "etc/fstab" || got[0].Line != 2 || got[0].Text != "Leere Zeile folgt" {
		t.Errorf("got %+v, want Path=%q ArchiveMember=%q Line=2 Text=%q", got[0], tarPath, "etc/fstab", "Leere Zeile folgt")
	}
}

func TestRunIncludeCompressedFindsMatchInsideTarGzMember(t *testing.T) {
	requireTool(t, "find")
	requireTool(t, "tar")
	requireTool(t, "gzip")
	requireTool(t, "grep")
	dir := t.TempDir()
	tarPath := makeTarGzContentFixture(t, dir, "etc.tar.gz", []tarMember{
		{"etc/fstab", "UUID=abc / ext4 defaults 0 1\nLeere Zeile folgt\n"},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	results, errs := Run(ctx, Request{
		Pattern: "leere", Scope: dir, Mode: ModeKeyword, Content: ContentGrep, IncludeCompressed: true,
	})

	got := collectResults(t, results, errs)
	if len(got) != 1 {
		t.Fatalf("got %d results, want exactly 1: %+v", len(got), got)
	}
	if got[0].Path != tarPath || got[0].ArchiveMember != "etc/fstab" || got[0].Line != 2 {
		t.Errorf("got %+v, want Path=%q ArchiveMember=%q Line=2", got[0], tarPath, "etc/fstab")
	}
}

func TestRunIncludeCompressedFindsMatchInsideTarBz2Member(t *testing.T) {
	requireTool(t, "find")
	requireTool(t, "tar")
	requireTool(t, "bzip2")
	requireTool(t, "grep")
	dir := t.TempDir()
	tarPath := compressTarFixture(t, dir, "etc.tar.bz2", "bzip2", []tarMember{
		{"etc/fstab", "line one\nLeere Zeile folgt\n"},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	results, errs := Run(ctx, Request{
		Pattern: "leere", Scope: dir, Mode: ModeKeyword, Content: ContentGrep, IncludeCompressed: true,
	})

	got := collectResults(t, results, errs)
	if len(got) != 1 || got[0].Path != tarPath || got[0].ArchiveMember != "etc/fstab" {
		t.Errorf("got %+v, want exactly one match: Path=%q ArchiveMember=%q", got, tarPath, "etc/fstab")
	}
}

func TestRunIncludeCompressedFindsMatchInsideTarXzMember(t *testing.T) {
	requireTool(t, "find")
	requireTool(t, "tar")
	requireTool(t, "xz")
	requireTool(t, "grep")
	dir := t.TempDir()
	tarPath := compressTarFixture(t, dir, "etc.tar.xz", "xz", []tarMember{
		{"etc/fstab", "line one\nLeere Zeile folgt\n"},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	results, errs := Run(ctx, Request{
		Pattern: "leere", Scope: dir, Mode: ModeKeyword, Content: ContentGrep, IncludeCompressed: true,
	})

	got := collectResults(t, results, errs)
	if len(got) != 1 || got[0].Path != tarPath || got[0].ArchiveMember != "etc/fstab" {
		t.Errorf("got %+v, want exactly one match: Path=%q ArchiveMember=%q", got, tarPath, "etc/fstab")
	}
}

// TestRunIncludeCompressedTarSkipsNonMatchingMembers pins that only the
// member actually containing the pattern is reported — not every
// member in the archive.
func TestRunIncludeCompressedTarSkipsNonMatchingMembers(t *testing.T) {
	requireTool(t, "find")
	requireTool(t, "tar")
	requireTool(t, "grep")
	dir := t.TempDir()
	makePlainTarContentFixture(t, dir, "backup.tar", []tarMember{
		{"a.txt", "nothing here\n"},
		{"b.txt", "match: ERROR\n"},
		{"c.txt", "also nothing\n"},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	results, errs := Run(ctx, Request{
		Pattern: "ERROR", Scope: dir, Mode: ModeKeyword, Content: ContentGrep, IncludeCompressed: true,
	})

	got := collectResults(t, results, errs)
	if len(got) != 1 || got[0].ArchiveMember != "b.txt" {
		t.Errorf("got %+v, want exactly one match, in b.txt", got)
	}
}

// TestRunIncludeCompressedOffLeavesTarUnopened mirrors
// TestRunIncludeCompressedOffByDefaultLeavesCompressedFilesUnopened
// (compressed_test.go) for the tar-family path: IncludeCompressed:
// false must never open (or even attempt to decompress) a matching
// tar archive at all.
func TestRunIncludeCompressedOffLeavesTarUnopened(t *testing.T) {
	requireTool(t, "find")
	requireTool(t, "tar")
	dir := t.TempDir()
	makePlainTarContentFixture(t, dir, "backup.tar", []tarMember{
		{"a.txt", "ERROR: boom\n"},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	results, errs := Run(ctx, Request{
		Pattern: "ERROR", Scope: dir, Mode: ModeKeyword, Content: ContentGrep, IncludeCompressed: false,
	})

	got := collectResults(t, results, errs)
	if len(got) != 0 {
		t.Errorf("got %+v, want no results with IncludeCompressed off", got)
	}
}

// TestRunIncludeCompressedDoesNotDuplicateZipContentMatches pins that a
// .zip candidate is never handed to the tar path too (see
// searchTarMembersContent's own doc comment on why: ContentZip/zipgrep
// already covers it) — a directory with both a matching .zip and a
// matching .tar.gz should report exactly one match per archive, not an
// extra duplicate for the zip.
func TestRunIncludeCompressedDoesNotDuplicateZipContentMatches(t *testing.T) {
	requireTool(t, "find")
	requireTool(t, "zipgrep")
	requireTool(t, "tar")
	requireTool(t, "gzip")
	dir := t.TempDir()
	makeZipContentFixture(t, dir, "docs.zip", "notes.txt", "ERROR: from zip\n")
	makeTarGzContentFixture(t, dir, "backup.tar.gz", []tarMember{
		{"notes.txt", "ERROR: from tar\n"},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	results, errs := Run(ctx, Request{
		Pattern: "ERROR", Scope: dir, Mode: ModeKeyword, Content: ContentGrep, IncludeCompressed: true,
	})

	got := collectResults(t, results, errs)
	if len(got) != 2 {
		t.Fatalf("got %d results, want exactly 2 (one per archive, no zip duplicate): %+v", len(got), got)
	}
}

// makeZipContentFixture writes dir/name as a zip with one member
// holding real content — TestRunIncludeCompressedDoesNotDuplicateZipContentMatches's
// own fixture, via archive/zip's stdlib writer (the same package
// archive_test.go's own makeZipFixture already uses, just with real
// content instead of an empty member).
func makeZipContentFixture(t *testing.T, dir, name, member, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	w, err := zw.Create(member)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte(content)); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestMaterializePlainTarReturnsPathUnchangedForPlainTar(t *testing.T) {
	dir := t.TempDir()
	tarPath := makePlainTarContentFixture(t, dir, "backup.tar", []tarMember{{"a.txt", "hi\n"}})

	plainPath, cleanup, err := materializePlainTar(context.Background(), tarPath)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()

	if plainPath != tarPath {
		t.Errorf("plainPath = %q, want the original path %q unchanged (no decompression needed for plain .tar)", plainPath, tarPath)
	}
	// cleanup on the "already plain" path must never remove the real
	// archive itself.
	cleanup()
	if _, err := os.Stat(tarPath); err != nil {
		t.Errorf("original archive should still exist after cleanup: %v", err)
	}
}

func TestMaterializePlainTarDecompressesGzipToTempFile(t *testing.T) {
	requireTool(t, "gzip")
	dir := t.TempDir()
	members := []tarMember{{"a.txt", "hello from a member\n"}}
	tarPath := makeTarGzContentFixture(t, dir, "backup.tar.gz", members)

	plainPath, cleanup, err := materializePlainTar(context.Background(), tarPath)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()

	if plainPath == tarPath {
		t.Fatal("plainPath should be a different (temp) file for a compressed archive, not the .tar.gz itself")
	}
	data, err := os.ReadFile(plainPath)
	if err != nil {
		t.Fatalf("temp file should be readable: %v", err)
	}
	if !bytes.Contains(data, []byte("hello from a member")) {
		t.Error("decompressed temp file doesn't contain the member's own real content")
	}

	cleanup()
	if _, err := os.Stat(plainPath); !os.IsNotExist(err) {
		t.Errorf("cleanup should have removed the temp file, stat err = %v", err)
	}
}

func TestParseGrepStdinLine(t *testing.T) {
	tests := []struct {
		line     string
		wantLine int
		wantText string
		wantOK   bool
	}{
		{"3:some matching text", 3, "some matching text", true},
		{"1:text with: an extra colon", 1, "text with: an extra colon", true},
		{"no colon at all", 0, "", false},
		{"not-a-number:text", 0, "", false},
	}
	for _, tt := range tests {
		line, text, ok := parseGrepStdinLine(tt.line)
		if ok != tt.wantOK || (ok && (line != tt.wantLine || text != tt.wantText)) {
			t.Errorf("parseGrepStdinLine(%q) = (%d, %q, %v), want (%d, %q, %v)", tt.line, line, text, ok, tt.wantLine, tt.wantText, tt.wantOK)
		}
	}
}
