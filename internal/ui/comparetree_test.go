package ui

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/jagottsicher/breakthrough/internal/compare"
)

// finishCompareTreeWalk is the tree-screen equivalent of the hash-
// override test's own bypass in compare_test.go: runCompareTreeWalk's
// own result lands via r.app.QueueUpdateDraw, which nothing drains
// without a running Application (see properties_test.go's identical
// reasoning on isolateHashFile/computeHashes) — so a test that wants to
// see rendered results runs compare.Walk directly instead, and sets
// the fields runCompareTreeWalk would have.
func finishCompareTreeWalk(t *testing.T, r *Root) {
	t.Helper()
	// The same hash wrapper runCompareTreeWalk itself builds -- real
	// fsops.Hash under the hood, not a fake, since Walk's own ModeHash
	// contract requires a working HashFunc (nil panics, correctly, the
	// same way Walk's own doc comment says it will).
	hash := func(ctx context.Context, path string) (string, error) {
		h, err := hashFile(ctx, path, nil)
		return h.SHA256, err
	}
	entries, stats, err := compare.Walk(context.Background(), r.compareTreeA, r.compareTreeB, r.compareTreeMode, hash, nil)
	if err != nil {
		t.Fatalf("compare.Walk: %v", err)
	}
	r.cancelCompareTreeWalk()
	r.compareTreeEntries, r.compareTreeStats = entries, stats
	r.renderCompareTree()
}

// newCompareTreePair builds two real directory trees under one
// temporary root, wired exactly like the sysadmin backup-check scenario
// this feature is for: one file identical on both sides, one that
// exists only in A, one only in B, and one that differs.
func newCompareTreePair(t *testing.T) (r *Root, dirA, dirB string) {
	t.Helper()
	root := t.TempDir()
	dirA, dirB = filepath.Join(root, "A"), filepath.Join(root, "B")
	for _, d := range []string{dirA, dirB, filepath.Join(dirA, "sub"), filepath.Join(dirB, "sub")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	when := time.Now().Truncate(time.Second)
	write := func(path, content string) {
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(path, when, when); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(dirA, "common.txt"), "same")
	write(filepath.Join(dirB, "common.txt"), "same")
	write(filepath.Join(dirA, "onlyA.txt"), "a-side")
	write(filepath.Join(dirB, "onlyB.txt"), "b-side")
	write(filepath.Join(dirA, "sub", "nested.txt"), "old text\n")
	write(filepath.Join(dirB, "sub", "nested.txt"), "new text\n")

	r, _ = newCompareRoot(t)
	return r, dirA, dirB
}

func TestOpenCompareTreeStartsFreshEveryTime(t *testing.T) {
	r, dirA, dirB := newCompareTreePair(t)
	r.compareTreeMode = compare.ModeHash // stale state from a previous open
	r.compareTreeShowIdentical = true

	r.openCompareTree(dirA, dirB)

	if r.compareTreeMode != compare.ModeQuick {
		t.Errorf("mode = %v, want a fresh open to reset to ModeQuick", r.compareTreeMode)
	}
	if r.compareTreeShowIdentical {
		t.Error("showIdentical should reset to false on a fresh open")
	}
	if r.activePage != compareTreePage {
		t.Fatalf("activePage = %q, want %q", r.activePage, compareTreePage)
	}
	if !r.compareTreeRunning {
		t.Error("opening should start a walk immediately (compareTreeRunning)")
	}
}

func TestRenderCompareTreeHidesIdenticalByDefault(t *testing.T) {
	r, dirA, dirB := newCompareTreePair(t)
	r.openCompareTree(dirA, dirB)
	finishCompareTreeWalk(t, r)

	names := compareTreeVisibleRelPaths(r)
	for _, want := range []string{"onlyA.txt", "onlyB.txt"} {
		if !contains(names, want) {
			t.Errorf("visible rows = %v, missing %q", names, want)
		}
	}
	if contains(names, "common.txt") {
		t.Errorf("visible rows = %v, common.txt (Identical) should be hidden by default", names)
	}
	// sub/nested.txt has the same size and the same truncated-to-the-
	// second mtime on both sides (see newCompareTreePair) -- exactly
	// the quick-check heuristic's own known blind spot, so it reads as
	// Identical under ModeQuick and is hidden right alongside common.txt.
	if contains(names, "sub/nested.txt") {
		t.Errorf("visible rows = %v, sub/nested.txt should read Identical (and so be hidden) under ModeQuick's same-size/same-time heuristic", names)
	}
}

func TestToggleCompareTreeShowIdenticalRevealsThem(t *testing.T) {
	r, dirA, dirB := newCompareTreePair(t)
	r.openCompareTree(dirA, dirB)
	finishCompareTreeWalk(t, r)

	r.toggleCompareTreeShowIdentical()

	names := compareTreeVisibleRelPaths(r)
	if !contains(names, "common.txt") {
		t.Errorf("visible rows = %v, want common.txt once identical rows are shown", names)
	}
}

func TestToggleCompareTreeModeResolvesTheHeuristicsBlindSpot(t *testing.T) {
	r, dirA, dirB := newCompareTreePair(t)
	r.openCompareTree(dirA, dirB)
	finishCompareTreeWalk(t, r)
	if contains(compareTreeVisibleRelPaths(r), "sub/nested.txt") {
		t.Fatal("sub/nested.txt should not be visible yet under ModeQuick")
	}

	r.toggleCompareTreeMode()
	if r.compareTreeMode != compare.ModeHash {
		t.Fatalf("mode after toggling = %v, want ModeHash", r.compareTreeMode)
	}
	finishCompareTreeWalk(t, r)

	names := compareTreeVisibleRelPaths(r)
	if !contains(names, "sub/nested.txt") {
		t.Errorf("visible rows = %v, want sub/nested.txt once ModeHash resolves the size+time coincidence as Differs", names)
	}
}

func TestToggleCompareTreeModeIgnoredWhileAWalkIsRunning(t *testing.T) {
	r, dirA, dirB := newCompareTreePair(t)
	r.openCompareTree(dirA, dirB) // leaves compareTreeRunning true (see the test above)

	r.toggleCompareTreeMode()

	if r.compareTreeMode != compare.ModeQuick {
		t.Error("toggling mode while a walk is running should be a no-op")
	}
}

func TestActivateCompareTreeRowOpensTheDiffForADifferingPair(t *testing.T) {
	r, dirA, dirB := newCompareTreePair(t)
	r.openCompareTree(dirA, dirB)
	finishCompareTreeWalk(t, r) // settle the initial ModeQuick walk first -- toggleCompareTreeMode ignores a still-"running" one
	r.toggleCompareTreeMode()   // ModeHash, so sub/nested.txt actually shows as Differs
	finishCompareTreeWalk(t, r)

	row := compareTreeRowOf(t, r, "sub/nested.txt")
	r.activateCompareTreeRow(row)

	if r.activePage != viewerPage {
		t.Fatalf("activePage = %q, want the Look pager %q", r.activePage, viewerPage)
	}
	got := r.viewerView.GetText(false)
	if !strings.Contains(got, "old text") || !strings.Contains(got, "new text") {
		t.Errorf("diff pager text = %q, missing one of the two versions", got)
	}
}

func TestActivateCompareTreeRowDoesNothingForAOneSidedEntry(t *testing.T) {
	r, dirA, dirB := newCompareTreePair(t)
	r.openCompareTree(dirA, dirB)
	finishCompareTreeWalk(t, r)

	row := compareTreeRowOf(t, r, "onlyA.txt")
	r.activateCompareTreeRow(row)

	if r.activePage != compareTreePage {
		t.Errorf("activePage = %q, want to stay on the tree screen for a one-sided row", r.activePage)
	}
}

func TestCopyCompareTreeRowAsksThenCopiesAndRewalks(t *testing.T) {
	r, dirA, dirB := newCompareTreePair(t)
	r.openCompareTree(dirA, dirB)
	finishCompareTreeWalk(t, r)

	row := compareTreeRowOf(t, r, "onlyA.txt")
	r.copyCompareTreeRow(row)

	if r.activePage != confirmPage {
		t.Fatalf("activePage = %q, want the confirmation dialog %q", r.activePage, confirmPage)
	}
	if _, err := os.Stat(filepath.Join(dirB, "onlyA.txt")); !os.IsNotExist(err) {
		t.Fatal("onlyA.txt should not exist under B before the confirmation is answered")
	}

	r.confirmDialog.SetCurrentItem(0) // "Yes, copy"
	r.confirmDialog.InputHandler()(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone), func(tview.Primitive) {})

	if _, err := os.Stat(filepath.Join(dirB, "onlyA.txt")); err != nil {
		t.Errorf("onlyA.txt should exist under B after confirming, stat err = %v", err)
	}
	if contains(compareTreeVisibleRelPaths(r), "onlyA.txt") {
		t.Error("onlyA.txt should no longer be a one-sided row after the copy and re-walk")
	}
}

func TestCompareTreeStatusTextTalliesEveryVerdict(t *testing.T) {
	got := compareTreeStatusText(compare.Stats{Differs: 1, OnlyInA: 2, OnlyInB: 3, Uncertain: 4, Identical: 5, Errored: 6})
	for _, want := range []string{"1 differ", "2 only in A", "3 only in B", "4 uncertain", "5 identical", "6 error(s)"} {
		if !strings.Contains(got, want) {
			t.Errorf("status text %q missing %q", got, want)
		}
	}
}

// compareTreeVisibleRelPaths reads back every row renderCompareTree
// actually put in the table, by its own stored Entry (see
// compareTreeEntryAt) -- the ground truth for "what's on screen",
// rather than re-deriving it from compareTreeEntries/showIdentical
// separately, which would only test the test's own logic against
// itself.
func compareTreeVisibleRelPaths(r *Root) []string {
	var names []string
	for row := 1; row < r.compareTreeTable.GetRowCount(); row++ {
		if e, ok := r.compareTreeEntryAt(row); ok {
			names = append(names, e.RelPath)
		}
	}
	return names
}

func compareTreeRowOf(t *testing.T, r *Root, relPath string) int {
	t.Helper()
	for row := 1; row < r.compareTreeTable.GetRowCount(); row++ {
		if e, ok := r.compareTreeEntryAt(row); ok && e.RelPath == relPath {
			return row
		}
	}
	t.Fatalf("no visible row for %q", relPath)
	return -1
}

func contains(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}
