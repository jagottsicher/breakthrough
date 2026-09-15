package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rivo/tview"

	"github.com/jagottsicher/breakthrough/internal/compare"
)

// newCompareRoot opens a Root against fixtureDir's own fixture files
// (see panel_test.go's own fixtureDir) — real files on a real temp
// directory, the same reasoning newBatchRenameRoot's own doc comment
// gives, since CompareFiles/Walk genuinely stat real paths on disk.
func newCompareRoot(t *testing.T) (*Root, string) {
	t.Helper()
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	return r, dir
}

func markTwo(t *testing.T, r *Root, patterns ...string) {
	t.Helper()
	for _, p := range patterns {
		if _, err := r.panel.selectByPattern(p, true); err != nil {
			t.Fatalf("selectByPattern(%q): %v", p, err)
		}
	}
}

func TestCompareTargetsFromTwoMarkedEntries(t *testing.T) {
	r, dir := newCompareRoot(t)
	markTwo(t, r, "apple.txt", "apricot.txt")

	a, b, ok := r.compareTargets()
	if !ok {
		t.Fatal("compareTargets ok = false, want true for two marked entries")
	}
	want := []string{filepath.Join(dir, "apple.txt"), filepath.Join(dir, "apricot.txt")}
	if a != want[0] || b != want[1] {
		t.Errorf("compareTargets = (%q, %q), want %v in display order", a, b, want)
	}
}

func TestCompareTargetsFailsWithoutTwoMarkedOrSplit(t *testing.T) {
	r, _ := newCompareRoot(t)
	if _, _, ok := r.compareTargets(); ok {
		t.Error("compareTargets ok = true with nothing marked and no split, want false")
	}

	if _, err := r.panel.selectByPattern("apple.txt", true); err != nil {
		t.Fatal(err)
	}
	if _, _, ok := r.compareTargets(); ok {
		t.Error("compareTargets ok = true with only one marked, want false")
	}
}

func TestCompareTargetsFromSplitView(t *testing.T) {
	r, first, second := newSplitRoot(t)
	// newSplitRoot leaves "first" empty (just ".."); give it a real
	// row of its own too, the same way "second" already has
	// "other.txt" -- CurrentRowPath needs a real entry on both sides.
	if err := os.WriteFile(filepath.Join(first, "mine.txt"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	r.panel.reportError(r.panel.load(r.panel.path))
	r.enterSplit(1)
	r.tabs[0].focusRow(1)
	r.tabs[1].focusRow(1)

	a, b, ok := r.compareTargets()
	if !ok {
		t.Fatal("compareTargets ok = false in split view, want true")
	}
	if !strings.HasPrefix(a, first) || !strings.HasPrefix(b, second) {
		t.Errorf("compareTargets = (%q, %q), want one path under each pane's own directory", a, b)
	}
}

func TestOpenCompareShowsGuidanceWhenNothingIsWellDefined(t *testing.T) {
	r, _ := newCompareRoot(t)
	r.openCompare()

	if r.activePage != errorPage {
		t.Fatalf("activePage = %q, want the notice overlay %q", r.activePage, errorPage)
	}
	if got := strings.ReplaceAll(r.errorView.GetText(true), "\n", " "); !strings.Contains(got, "mark exactly two") {
		t.Errorf("error text = %q, want guidance about marking two items", got)
	}
}

func TestOpenCompareRoutesToTheFileOverlayForTwoFiles(t *testing.T) {
	r, _ := newCompareRoot(t)
	markTwo(t, r, "apple.txt", "apricot.txt")
	r.openCompare()

	if r.activePage != comparePage {
		t.Fatalf("activePage = %q, want the file overlay %q", r.activePage, comparePage)
	}
}

func TestOpenCompareRoutesToTheTreeScreenForTwoDirectories(t *testing.T) {
	r, dir := newCompareRoot(t)
	if err := os.Mkdir(filepath.Join(dir, "dirA"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "dirB"), 0o755); err != nil {
		t.Fatal(err)
	}
	r.panel.reportError(r.panel.load(r.panel.path))
	markTwo(t, r, "dirA", "dirB")
	r.openCompare()

	if r.activePage != compareTreePage {
		t.Fatalf("activePage = %q, want the tree screen %q", r.activePage, compareTreePage)
	}
}

func TestOpenCompareRefusesAFileAgainstADirectory(t *testing.T) {
	r, dir := newCompareRoot(t)
	if err := os.Mkdir(filepath.Join(dir, "app-data-2"), 0o755); err != nil {
		t.Fatal(err)
	}
	r.panel.reportError(r.panel.load(r.panel.path))
	markTwo(t, r, "apple.txt", "app-data-2")
	r.openCompare()

	if r.activePage != errorPage {
		t.Fatalf("activePage = %q, want the notice overlay %q", r.activePage, errorPage)
	}
	if got := r.errorView.GetText(true); !strings.Contains(got, "directory") {
		t.Errorf("error text = %q, want it to explain the file/directory mismatch", got)
	}
}

func TestOpenCompareRefusesInsideAnArchiveView(t *testing.T) {
	r, _ := newCompareRoot(t)
	r.panel.archivePath = "/somewhere.zip" // enough for inArchiveView() to report true
	r.openCompare()

	if r.activePage != errorPage {
		t.Fatalf("activePage = %q, want the notice overlay %q", r.activePage, errorPage)
	}
	if got := r.errorView.GetText(true); !strings.Contains(got, "archive") {
		t.Errorf("error text = %q, want the archive-view guard message", got)
	}
}

func TestRenderCompareFileShowsSizeMismatchAsDifferent(t *testing.T) {
	r, dir := newCompareRoot(t)
	a, b := filepath.Join(dir, "a.txt"), filepath.Join(dir, "b.txt")
	if err := os.WriteFile(a, []byte("short"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(b, []byte("a much longer body"), 0o644); err != nil {
		t.Fatal(err)
	}
	r.openCompareFile(a, b)

	if got := r.compareVerdict.GetText(true); !strings.Contains(got, "Different") || !strings.Contains(got, "sizes") {
		t.Errorf("verdict = %q, want it to name the size mismatch", got)
	}
}

func TestRenderCompareFileShowsSameSizeSameTimeAsProbablyIdentical(t *testing.T) {
	r, dir := newCompareRoot(t)
	a, b := filepath.Join(dir, "a.txt"), filepath.Join(dir, "b.txt")
	when := time.Now().Truncate(time.Second)
	for _, p := range []string{a, b} {
		if err := os.WriteFile(p, []byte("same"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(p, when, when); err != nil {
			t.Fatal(err)
		}
	}
	r.openCompareFile(a, b)

	if got := r.compareVerdict.GetText(true); !strings.Contains(got, "Probably identical") {
		t.Errorf("verdict = %q, want the heuristic's own \"probably identical\"", got)
	}
}

func TestRenderCompareFileShowsSameSizeDifferentTimeAsUncertain(t *testing.T) {
	r, dir := newCompareRoot(t)
	a, b := filepath.Join(dir, "a.txt"), filepath.Join(dir, "b.txt")
	if err := os.WriteFile(a, []byte("same"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(b, []byte("same"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(b, time.Now().Add(time.Hour), time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	r.openCompareFile(a, b)

	if got := r.compareVerdict.GetText(true); !strings.Contains(got, "Uncertain") {
		t.Errorf("verdict = %q, want Uncertain for same size, different time", got)
	}
}

func TestRenderCompareFileWithAComputedHashOverridesTheHeuristic(t *testing.T) {
	r, dir := newCompareRoot(t)
	a, b := filepath.Join(dir, "a.txt"), filepath.Join(dir, "b.txt")
	when := time.Now().Truncate(time.Second)
	// Same size, same (truncated-to-the-second) mtime, different
	// content -- the exact false-positive the size+time heuristic is
	// known to miss (see compare.Uncertain's own doc comment).
	if err := os.WriteFile(a, []byte("AAAA"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(b, []byte("BBBB"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{a, b} {
		if err := os.Chtimes(p, when, when); err != nil {
			t.Fatal(err)
		}
	}
	r.openCompareFile(a, b)
	if got := r.compareVerdict.GetText(true); !strings.Contains(got, "Probably identical") {
		t.Fatalf("verdict before hashing = %q, want the heuristic's false-positive \"Probably identical\"", got)
	}

	// Bypass computeCompareHash's own goroutine (see properties_test.go's
	// identical reasoning: QueueUpdateDraw never drains without a
	// running Application) and just set what it would have produced.
	r.compareHashes = map[string]string{a: "hash-a", b: "hash-b"}
	r.renderCompareFile()

	if got := r.compareVerdict.GetText(true); !strings.Contains(got, "Different") || !strings.Contains(got, "SHA-256") {
		t.Errorf("verdict after hashing = %q, want the hash result to override the heuristic", got)
	}
}

func TestComputeCompareHashIgnoresReentryWhileRunning(t *testing.T) {
	r, dir := newCompareRoot(t)
	a, b := filepath.Join(dir, "a.txt"), filepath.Join(dir, "b.txt")
	if err := os.WriteFile(a, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(b, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	r.openCompareFile(a, b)

	r.compareHashRunning = true
	firstCancel := r.compareHashCancel
	r.computeCompareHash()
	if r.compareHashCancel != nil && firstCancel == nil {
		t.Error("computeCompareHash should not start a new computation while one is already running")
	}
	r.cancelCompareHashComputation()
}

func TestOpenCompareDiffReportsNoDifferences(t *testing.T) {
	if !compare.Available() {
		t.Skip("diff(1) not on PATH")
	}
	r, dir := newCompareRoot(t)
	a, b := filepath.Join(dir, "a.txt"), filepath.Join(dir, "b.txt")
	if err := os.WriteFile(a, []byte("same\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(b, []byte("same\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	r.openCompareFile(a, b)
	r.openCompareDiff()

	if r.activePage != errorPage {
		t.Fatalf("activePage = %q, want the notice overlay %q", r.activePage, errorPage)
	}
	if got := strings.ReplaceAll(r.errorView.GetText(true), "\n", " "); !strings.Contains(got, "no differences") {
		t.Errorf("error text = %q, want a no-differences notice", got)
	}
}

func TestOpenCompareDiffOpensTheLookPagerWithColoredLines(t *testing.T) {
	if !compare.Available() {
		t.Skip("diff(1) not on PATH")
	}
	r, dir := newCompareRoot(t)
	a, b := filepath.Join(dir, "a.txt"), filepath.Join(dir, "b.txt")
	if err := os.WriteFile(a, []byte("line one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(b, []byte("line two\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	r.openCompareFile(a, b)
	r.openCompareDiff()

	if r.activePage != viewerPage {
		t.Fatalf("activePage = %q, want the Look pager %q", r.activePage, viewerPage)
	}
	got := r.viewerView.GetText(false)
	if !strings.Contains(got, "line one") || !strings.Contains(got, "line two") {
		t.Errorf("diff pager text = %q, missing one of the two lines", got)
	}
}

func TestCompareDiffButtonDisabledForABinaryPair(t *testing.T) {
	r, dir := newCompareRoot(t)
	a, b := filepath.Join(dir, "a.bin"), filepath.Join(dir, "b.bin")
	if err := os.WriteFile(a, []byte("abc\x00def"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(b, []byte("abc\x00def"), 0o644); err != nil {
		t.Fatal(err)
	}
	r.openCompareFile(a, b)

	if !r.compareDiffBtn.IsDisabled() {
		t.Error("Show diff should be disabled for a binary pair")
	}
}

func TestCloseCompareCancelsAnyRunningHash(t *testing.T) {
	r, dir := newCompareRoot(t)
	a, b := filepath.Join(dir, "a.txt"), filepath.Join(dir, "b.txt")
	if err := os.WriteFile(a, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(b, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	r.openCompareFile(a, b)
	r.compareHashRunning = true

	r.closeCompare()

	if r.compareHashRunning {
		t.Error("closeCompare should cancel a hash computation still in flight")
	}
}
