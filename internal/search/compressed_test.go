package search

import (
	"archive/zip"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// makeCompressedFixture writes content to dir/name, compressed via
// tool -c (bzip2 -c / xz -c both write the compressed stream to
// stdout, leaving the original file untouched — the same shape gzip -c
// has, which runner_test.go's own TestRunContentSearchGzip instead
// builds via the compress/gzip stdlib since Go has a writer for that
// one; bzip2/xz have no stdlib writer, only a reader, so this shells
// out to the real tool the same way listArchiveMembers itself does at
// runtime).
func makeCompressedFixture(t *testing.T, dir, name, tool, content string) string {
	t.Helper()
	requireTool(t, tool)
	path := filepath.Join(dir, name)
	cmd := exec.Command(tool, "-c")
	cmd.Stdin = strings.NewReader(content)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("%s -c: %v", tool, err)
	}
	if err := os.WriteFile(path, out, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestRunIncludeCompressedFindsGzipMatch(t *testing.T) {
	requireTool(t, "find")
	requireTool(t, "zgrep")
	dir := t.TempDir()
	makeCompressedFixture(t, dir, "app.log.gz", "gzip", "line one\nERROR: boom\nline three\n")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	results, errs := Run(ctx, Request{
		Pattern: "ERROR", Scope: dir, Mode: ModeKeyword, Content: ContentGrep, IncludeCompressed: true,
	})

	got := collectResults(t, results, errs)
	if len(got) != 1 || got[0].Line != 2 || got[0].Text != "ERROR: boom" {
		t.Errorf("got %+v, want exactly one match: line 2, %q", got, "ERROR: boom")
	}
}

func TestRunIncludeCompressedFindsBzip2Match(t *testing.T) {
	requireTool(t, "find")
	requireTool(t, "bzgrep")
	dir := t.TempDir()
	makeCompressedFixture(t, dir, "app.log.bz2", "bzip2", "line one\nERROR: boom\nline three\n")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	results, errs := Run(ctx, Request{
		Pattern: "ERROR", Scope: dir, Mode: ModeKeyword, Content: ContentGrep, IncludeCompressed: true,
	})

	got := collectResults(t, results, errs)
	if len(got) != 1 || got[0].Line != 2 || got[0].Text != "ERROR: boom" {
		t.Errorf("got %+v, want exactly one match: line 2, %q", got, "ERROR: boom")
	}
}

func TestRunIncludeCompressedFindsXzMatch(t *testing.T) {
	requireTool(t, "find")
	requireTool(t, "xzgrep")
	dir := t.TempDir()
	makeCompressedFixture(t, dir, "app.log.xz", "xz", "line one\nERROR: boom\nline three\n")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	results, errs := Run(ctx, Request{
		Pattern: "ERROR", Scope: dir, Mode: ModeKeyword, Content: ContentGrep, IncludeCompressed: true,
	})

	got := collectResults(t, results, errs)
	if len(got) != 1 || got[0].Line != 2 || got[0].Text != "ERROR: boom" {
		t.Errorf("got %+v, want exactly one match: line 2, %q", got, "ERROR: boom")
	}
}

func TestRunIncludeCompressedFindsZipMatch(t *testing.T) {
	requireTool(t, "find")
	requireTool(t, "zipgrep")
	dir := t.TempDir()

	zf, err := os.Create(filepath.Join(dir, "docs.zip"))
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(zf)
	member, err := zw.Create("notes.txt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := member.Write([]byte("first line\nsecond line with KEYWORD\n")); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := zf.Close(); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	results, errs := Run(ctx, Request{
		Pattern: "KEYWORD", Scope: dir, Mode: ModeKeyword, Content: ContentGrep, IncludeCompressed: true,
	})

	got := collectResults(t, results, errs)
	if len(got) != 1 || got[0].Line != 2 {
		t.Errorf("got %+v, want exactly one match on line 2", got)
	}
}

// TestRunIncludeCompressedCombinesWithPlainGrep pins that IncludeCompressed
// is additive: a plain-text match in a real file and a match inside a
// compressed one both come back from the same search.
func TestRunIncludeCompressedCombinesWithPlainGrep(t *testing.T) {
	requireTool(t, "find")
	requireTool(t, "zgrep")
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "plain.log"), []byte("ERROR: plain\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gzPath := makeCompressedFixture(t, dir, "archived.log.gz", "gzip", "ERROR: compressed\n")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	results, errs := Run(ctx, Request{
		Pattern: "ERROR", Scope: dir, Mode: ModeKeyword, Content: ContentGrep, IncludeCompressed: true,
	})

	got := collectResults(t, results, errs)
	if len(got) != 2 {
		t.Fatalf("got %d results, want 2 (one plain, one compressed): %+v", len(got), got)
	}
	var sawPlain, sawCompressed bool
	for _, r := range got {
		switch r.Path {
		case filepath.Join(dir, "plain.log"):
			sawPlain = true
		case gzPath:
			sawCompressed = true
		}
	}
	if !sawPlain || !sawCompressed {
		t.Errorf("got %+v, want both the plain and the compressed match represented", got)
	}
}

func TestRunIncludeCompressedOffByDefaultLeavesCompressedFilesUnopened(t *testing.T) {
	requireTool(t, "find")
	dir := t.TempDir()
	makeCompressedFixture(t, dir, "app.log.gz", "gzip", "ERROR: boom\n")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	// IncludeCompressed left false (the zero value).
	results, errs := Run(ctx, Request{Pattern: "ERROR", Scope: dir, Mode: ModeKeyword, Content: ContentGrep})

	got := collectResults(t, results, errs)
	if len(got) != 0 {
		t.Errorf("got %+v, want no results — Include Compressed is off", got)
	}
}
