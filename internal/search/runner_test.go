package search

import (
	"archive/zip"
	"compress/gzip"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"testing"
	"time"
)

// requireTool skips t unless name is on $PATH — find/grep are assumed
// present (this app already depends on a real POSIX environment for
// far more basic things, e.g. the bash line itself), but this project
// also targets environments where a given tool genuinely might not be
// installed (zgrep/zipgrep especially), so tests exercising those
// still check rather than assume.
func requireTool(t *testing.T, name string) {
	t.Helper()
	if _, err := exec.LookPath(name); err != nil {
		t.Skipf("%s not available in this environment: %v", name, err)
	}
}

// collectResults drains ch (and errs) into a slice, failing t if
// anything arrives on errs — the common shape most of this file's
// tests want; the few that expect an error, or a cancellation, drain
// the channels themselves instead.
func collectResults(t *testing.T, results <-chan Result, errs <-chan error) []Result {
	t.Helper()
	var got []Result
	for r := range results {
		got = append(got, r)
	}
	select {
	case err := <-errs:
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	default:
	}
	return got
}

func paths(results []Result) []string {
	out := make([]string, len(results))
	for i, r := range results {
		out[i] = r.Path
	}
	sort.Strings(out)
	return out
}

func fixtureTree(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	files := []string{"apple.txt", "apricot.txt", "banana.log", filepath.Join("sub", "cherry.txt")}
	for _, f := range files {
		full := filepath.Join(dir, f)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestRunFilenameSearchGlob(t *testing.T) {
	requireTool(t, "find")
	dir := fixtureTree(t)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	results, errs := Run(ctx, Request{Pattern: "a*.txt", Scope: dir, Mode: ModeGlob, Engine: EngineFind})

	got := paths(collectResults(t, results, errs))
	want := []string{filepath.Join(dir, "apple.txt"), filepath.Join(dir, "apricot.txt")}
	sort.Strings(want)
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("got[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestRunFilenameSearchKeywordMatchesSubdirectories(t *testing.T) {
	requireTool(t, "find")
	dir := fixtureTree(t)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	results, errs := Run(ctx, Request{Pattern: "cherry", Scope: dir, Mode: ModeKeyword, Engine: EngineFind})

	got := paths(collectResults(t, results, errs))
	want := []string{filepath.Join(dir, "sub", "cherry.txt")}
	if len(got) != 1 || got[0] != want[0] {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestRunFilenameSearchRegex(t *testing.T) {
	requireTool(t, "find")
	dir := fixtureTree(t)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	results, errs := Run(ctx, Request{Pattern: `.*\.log$`, Scope: dir, Mode: ModeRegex, Engine: EngineFind})

	got := paths(collectResults(t, results, errs))
	want := []string{filepath.Join(dir, "banana.log")}
	if len(got) != 1 || got[0] != want[0] {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestRunContentSearchGrep(t *testing.T) {
	requireTool(t, "grep")
	dir := t.TempDir()
	write := func(name, content string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("a.txt", "hello world\nTODO: fix this\n")
	write("b.txt", "nothing interesting here\n")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	results, errs := Run(ctx, Request{Pattern: "TODO", Scope: dir, Mode: ModeKeyword, Content: ContentGrep})

	got := collectResults(t, results, errs)
	if len(got) != 1 {
		t.Fatalf("got %d results, want 1: %+v", len(got), got)
	}
	if got[0].Path != filepath.Join(dir, "a.txt") || got[0].Line != 2 || got[0].Text != "TODO: fix this" {
		t.Errorf("got %+v, want {Path: %q, Line: 2, Text: \"TODO: fix this\"}", got[0], filepath.Join(dir, "a.txt"))
	}
}

func TestRunContentSearchGrepRegex(t *testing.T) {
	requireTool(t, "grep")
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "code.go"), []byte("func main() {}\nvar x = 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	results, errs := Run(ctx, Request{Pattern: `^func \w+\(`, Scope: dir, Mode: ModeRegex, Content: ContentGrep})

	got := collectResults(t, results, errs)
	if len(got) != 1 || got[0].Line != 1 {
		t.Errorf("got %+v, want exactly one match on line 1", got)
	}
}

func TestRunContentSearchGzip(t *testing.T) {
	requireTool(t, "find")
	requireTool(t, "zgrep")
	dir := t.TempDir()

	gz, err := os.Create(filepath.Join(dir, "app.log.gz"))
	if err != nil {
		t.Fatal(err)
	}
	w := gzip.NewWriter(gz)
	if _, err := w.Write([]byte("line one\nERROR: boom\nline three\n")); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	results, errs := Run(ctx, Request{Pattern: "ERROR", Scope: dir, Mode: ModeKeyword, Content: ContentGzip})

	got := collectResults(t, results, errs)
	if len(got) != 1 || got[0].Line != 2 || got[0].Text != "ERROR: boom" {
		t.Errorf("got %+v, want exactly one match: line 2, \"ERROR: boom\"", got)
	}
}

func TestRunContentSearchZip(t *testing.T) {
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
	results, errs := Run(ctx, Request{Pattern: "KEYWORD", Scope: dir, Mode: ModeKeyword, Content: ContentZip})

	got := collectResults(t, results, errs)
	if len(got) != 1 || got[0].Line != 2 {
		t.Errorf("got %+v, want exactly one match on line 2", got)
	}
}

// TestRunCancelStopsPromptly pins that cancelling ctx actually stops
// the search — the results channel closes without the caller having to
// drain every result first, and no error is reported (a cancellation
// isn't a failure — see Run's own doc comment).
func TestRunCancelStopsPromptly(t *testing.T) {
	requireTool(t, "find")
	dir := fixtureTree(t)

	ctx, cancel := context.WithCancel(context.Background())
	results, errs := Run(ctx, Request{Pattern: "*", Scope: dir, Mode: ModeGlob, Engine: EngineFind})
	cancel() // cancel immediately, before reading anything

	// A result may already have been in flight when cancel() ran — drain
	// whatever's left, expecting the channel to close promptly either way
	// rather than the search continuing to completion regardless.
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

func TestWithinScope(t *testing.T) {
	tests := []struct {
		path, scope string
		want        bool
	}{
		{"/home/jens/file.txt", "/home/jens", true},
		{"/home/jens", "/home/jens", true},
		{"/etc/passwd", "/home/jens", false},
		// The classic filepath.Rel-vs-strings.HasPrefix trap: "/homefoo"
		// shares a string prefix with "/home" but isn't nested under it.
		{"/homefoo/file.txt", "/home", false},
	}
	for _, tt := range tests {
		if got := withinScope(tt.path, tt.scope); got != tt.want {
			t.Errorf("withinScope(%q, %q) = %v, want %v", tt.path, tt.scope, got, tt.want)
		}
	}
}
