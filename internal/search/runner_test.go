package search

import (
	"archive/zip"
	"compress/gzip"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
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

// TestRunFilenameSearchIgnoreDirsPrunesSubdirectory pins IgnoreDirs end
// to end through the real find binary: a pattern that would otherwise
// match cherry.txt (see fixtureTree's own "sub/cherry.txt") finds
// nothing once "sub" is ignored.
func TestRunFilenameSearchIgnoreDirsPrunesSubdirectory(t *testing.T) {
	requireTool(t, "find")
	dir := fixtureTree(t)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	results, errs := Run(ctx, Request{Pattern: "cherry.txt", Scope: dir, Mode: ModeGlob, Engine: EngineFind, IgnoreDirs: []string{"sub"}})

	got := collectResults(t, results, errs)
	if len(got) != 0 {
		t.Errorf("got %v, want no results — \"sub\" should have been pruned", got)
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

// TestRunFilenameSearchShallowFirst pins the user's own explicit
// request: a recursive search reports files directly in Scope before
// anything in a subdirectory, rather than in whatever order find's own
// directory traversal happens to produce — checked on arrival order
// from the results channel itself (unlike this file's other tests,
// which sort before comparing, since order is exactly what's being
// pinned here).
func TestRunFilenameSearchShallowFirst(t *testing.T) {
	requireTool(t, "find")
	dir := t.TempDir()
	write := func(rel string) {
		full := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("a.txt")
	write("b.txt")
	write(filepath.Join("sub", "c.txt"))

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	results, errs := Run(ctx, Request{Pattern: "*.txt", Scope: dir, Mode: ModeGlob, Engine: EngineFind})

	var order []string
	for r := range results {
		order = append(order, r.Path)
	}
	if err := <-errs; err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(order) != 3 {
		t.Fatalf("got %v, want 3 results", order)
	}

	deepIdx := -1
	for i, p := range order {
		if filepath.Dir(p) != dir {
			deepIdx = i
			break
		}
	}
	if deepIdx != 2 {
		t.Errorf("order = %v, want the two Scope-level files (a.txt, b.txt) reported before sub/c.txt", order)
	}
}

// TestRunFilenameSearchShallowFirstStillNonRecursiveWhenAsked pins
// that the shallow-first restructuring doesn't change NonRecursive's
// own meaning: it's still a single -maxdepth 1 pass, never a second,
// deeper one — the two-pass behavior only ever applies to a genuinely
// recursive search.
func TestRunFilenameSearchShallowFirstStillNonRecursiveWhenAsked(t *testing.T) {
	requireTool(t, "find")
	dir := t.TempDir()
	write := func(rel string) {
		full := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("a.txt")
	write(filepath.Join("sub", "c.txt"))

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	results, errs := Run(ctx, Request{Pattern: "*.txt", Scope: dir, Mode: ModeGlob, Engine: EngineFind, NonRecursive: true})

	got := paths(collectResults(t, results, errs))
	want := []string{filepath.Join(dir, "a.txt")}
	if len(got) != 1 || got[0] != want[0] {
		t.Errorf("got %v, want %v (sub/c.txt should stay excluded)", got, want)
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

// TestRunContentSearchGrepAppliesIgnoreDirs pins a real user report: a
// plain content search (no Filename term at all — nothing for
// listThenGrep to narrow the file list with first, so this is the one
// path that ever hands req.IgnoreDirs to GrepArgs directly — see its
// own doc comment) used to walk right through an ignored subdirectory
// as if IgnoreDirs had never been set, since nothing here ever passed
// it to grep. A real subprocess invocation, not just an args-building
// check (see TestGrepArgsIgnoreDirsAddsExcludeDirFlags for that) —
// proving --exclude-dir actually does what its own name says on a
// real grep, not just that this app asked for it.
func TestRunContentSearchGrepAppliesIgnoreDirs(t *testing.T) {
	requireTool(t, "grep")
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "development"), 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(name, content string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("hangman.go", "func main() {}\n")
	write(filepath.Join("development", "hangman.go"), "func main() {}\n")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	results, errs := Run(ctx, Request{
		Pattern:    "func main",
		Scope:      dir,
		Mode:       ModeKeyword,
		Content:    ContentGrep,
		IgnoreDirs: []string{"development"},
	})

	got := collectResults(t, results, errs)
	if len(got) != 1 {
		t.Fatalf("got %d results, want 1 (development/ excluded): %+v", len(got), got)
	}
	if got[0].Path != filepath.Join(dir, "hangman.go") {
		t.Errorf("got %+v, want the top-level hangman.go only, not development/hangman.go", got[0])
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

// TestRunContentSearchWithNamePatternRestrictsFiles pins the user's own
// explicit request: filling in both a name pattern and a content
// pattern narrows the content search to files matching the name first
// (via find, the same as a filename search would use), rather than
// grepping every file under Scope regardless of name.
func TestRunContentSearchWithNamePatternRestrictsFiles(t *testing.T) {
	requireTool(t, "find")
	requireTool(t, "grep")
	dir := t.TempDir()
	write := func(name, content string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("a.txt", "TODO: fix a\n")
	write("a.log", "TODO: fix a\n") // same content, wrong name — must be excluded

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	results, errs := Run(ctx, Request{
		Pattern:     "TODO",
		NamePattern: "*.txt",
		NameMode:    ModeGlob,
		Scope:       dir,
		Mode:        ModeKeyword,
		Content:     ContentGrep,
	})

	got := collectResults(t, results, errs)
	if len(got) != 1 || got[0].Path != filepath.Join(dir, "a.txt") {
		t.Errorf("got %+v, want exactly one match in a.txt (not a.log, despite matching content)", got)
	}
}

// TestRunContentSearchWithoutNamePatternSearchesEverything pins that
// leaving NamePattern blank (its own zero value) still searches every
// file under Scope, the same as before it existed.
func TestRunContentSearchWithoutNamePatternSearchesEverything(t *testing.T) {
	requireTool(t, "grep")
	dir := t.TempDir()
	write := func(name, content string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("a.txt", "TODO: fix a\n")
	write("a.log", "TODO: fix a\n")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	results, errs := Run(ctx, Request{Pattern: "TODO", Scope: dir, Mode: ModeKeyword, Content: ContentGrep})

	got := collectResults(t, results, errs)
	if len(got) != 2 {
		t.Errorf("got %d results, want 2 (both files, no name filter applied): %+v", len(got), got)
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

// TestRunErrsChannelClosesAfterResultsWithNoError pins Run's own
// documented contract (see its doc comment): a caller that drains
// results with a plain "for range" and only then reads errs must never
// block doing so, even when nothing was ever sent — errs is closed no
// later than results, so that receive returns its zero value
// immediately instead. Uses a plain (blocking) receive specifically,
// not a select-with-default, since that's the whole point being
// pinned: a select-with-default would pass even if this contract were
// broken and errs were simply never closed.
func TestRunErrsChannelClosesAfterResultsWithNoError(t *testing.T) {
	requireTool(t, "find")
	dir := fixtureTree(t)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	results, errs := Run(ctx, Request{Pattern: "apple.txt", Scope: dir, Mode: ModeGlob, Engine: EngineFind})

	for range results {
	}

	done := make(chan struct{})
	var err error
	go func() {
		err = <-errs
		close(done)
	}()
	select {
	case <-done:
		if err != nil {
			t.Errorf("err = %v, want nil", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("receiving from errs blocked — it should have been closed once results closed")
	}
}

func TestUnderIgnoredDir(t *testing.T) {
	tests := []struct {
		path       string
		ignoreDirs []string
		want       bool
	}{
		{"/home/jens/project/node_modules/pkg/index.js", []string{"node_modules"}, true},
		{"/home/jens/project/src/index.js", []string{"node_modules"}, false},
		// Exact-component match, not a substring: "node_modules_old"
		// isn't "node_modules".
		{"/home/jens/project/node_modules_old/index.js", []string{"node_modules"}, false},
		{"/home/jens/.git/config", []string{".git", "node_modules"}, true},
		{"/home/jens/project/file.txt", nil, false},
		// A real glob (see Request.IgnoreDirs' own doc comment on why
		// this matches now, not just exact names) — the "Skip hidden"
		// UI toggle's own mechanism.
		{"/home/jens/.config/breakthrough", []string{".*"}, true},
		{"/home/jens/project/file.txt", []string{".*"}, false},
	}
	for _, tt := range tests {
		if got := underIgnoredDir(tt.path, tt.ignoreDirs); got != tt.want {
			t.Errorf("underIgnoredDir(%q, %v) = %v, want %v", tt.path, tt.ignoreDirs, got, tt.want)
		}
	}
}

// TestFilenameCommandDispatchesOnEngine pins filenameCommand's own
// central decision, now that it takes explicit pattern/mode/
// caseSensitive parameters instead of reading them off a Request
// directly (see its own doc comment on why: listThenGrep needs to call
// it for a name-*narrowing* pattern, never the Request's own Pattern/
// Mode, which for a content search means something else entirely) —
// engine alone decides "find" vs "locate", and each carries its own
// real args, not just an empty placeholder for the other's sake. A
// pure, no-subprocess test: real locate/find execution is
// environment-dependent (locate's own index, in particular, can't be
// controlled from a test — see this package's own locate_test.go,
// which is exactly this same "test the args, not a real invocation"
// boundary for LocateArgs itself).
func TestFilenameCommandDispatchesOnEngine(t *testing.T) {
	name, args, ok := filenameCommand(EngineLocate, "/ignored/for/locate", "*.go", ModeGlob, nil, false, false, false)
	if !ok {
		t.Fatal("ok = false, want true")
	}
	if name != "locate" {
		t.Errorf("name = %q, want %q", name, "locate")
	}
	wantArgs, _ := LocateArgs("linux", "*.go", ModeGlob, false)
	if runtime.GOOS == "linux" && !reflect.DeepEqual(args, wantArgs) {
		t.Errorf("args = %v, want %v (matching LocateArgs directly)", args, wantArgs)
	}

	name, args, ok = filenameCommand(EngineFind, "/some/scope", "*.go", ModeGlob, nil, false, false, false)
	if !ok {
		t.Fatal("ok = false, want true")
	}
	if name != "find" {
		t.Errorf("name = %q, want %q", name, "find")
	}
	wantArgs = FindArgs(runtime.GOOS, "/some/scope", "*.go", ModeGlob, nil, false, false, false)
	if !reflect.DeepEqual(args, wantArgs) {
		t.Errorf("args = %v, want %v (matching FindArgs directly)", args, wantArgs)
	}
}

// TestFilenameCommandPropagatesLocateRegexUnavailable pins that
// filenameCommand's own ok=false (see LocateArgs' identical case)
// survives the refactor to explicit parameters — a regex search under
// locate on a platform with no regex support of its own must still be
// refused, not silently build a command that would just fail with a
// usage error.
func TestFilenameCommandPropagatesLocateRegexUnavailable(t *testing.T) {
	if runtime.GOOS == "linux" {
		t.Skip("regex is available for locate on linux — see LocateArgs' own doc comment")
	}
	if _, _, ok := filenameCommand(EngineLocate, "", `.*\.go$`, ModeRegex, nil, false, false, false); ok {
		t.Error("ok = true, want false — this platform's locate has no regex support")
	}
}
