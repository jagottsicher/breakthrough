package ui

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// TestRenderGitBreachBranchesHeaderTracksEachLabelsOwnSpan pins the
// pure data renderGitBreachBranchesHeader produces for
// gitBreachFocusBranchesOnClick to act on — three real, non-empty,
// non-overlapping column ranges, in the fixed Branches/Tags/Remotes
// order regardless of which one is currently active.
func TestRenderGitBreachBranchesHeaderTracksEachLabelsOwnSpan(t *testing.T) {
	requireGitForBreach(t)
	dir := initGitBreachRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitBreach(t, dir, "add", "a.txt")
	runGitBreach(t, dir, "commit", "-q", "-m", "initial")

	r := newTestRootForGitBreachDir(t, dir)
	r.openGitBreach()

	spans := r.gitBreach.BranchesTabSpans
	if len(spans) != 3 {
		t.Fatalf("BranchesTabSpans = %+v, want 3 entries", spans)
	}
	wantOrder := []gitBreachBranchesTab{gitBreachBranchesTabBranches, gitBreachBranchesTabTags, gitBreachBranchesTabRemotes}
	for i, want := range wantOrder {
		if spans[i].tab != want {
			t.Errorf("spans[%d].tab = %v, want %v", i, spans[i].tab, want)
		}
		if spans[i].end <= spans[i].start {
			t.Errorf("spans[%d] = %+v, want a real, non-empty column range", i, spans[i])
		}
	}
	for i := 1; i < len(spans); i++ {
		if spans[i].start < spans[i-1].end {
			t.Errorf("spans[%d] (%+v) overlaps spans[%d] (%+v)", i, spans[i], i-1, spans[i-1])
		}
	}
}

// TestGitBreachFocusBranchesOnClickSwitchesToTheClickedSubTab mirrors
// TestCaptureStatusBarMouseClickOnGitStatusOpensGitBreach's own shape
// exactly: a real rect, a real span read back off it, a synthetic
// click at the midpoint of that span's own columns.
func TestGitBreachFocusBranchesOnClickSwitchesToTheClickedSubTab(t *testing.T) {
	requireGitForBreach(t)
	dir := initGitBreachRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitBreach(t, dir, "add", "a.txt")
	runGitBreach(t, dir, "commit", "-q", "-m", "initial")

	r := newTestRootForGitBreachDir(t, dir)
	r.openGitBreach()
	r.gitBreach.BranchesHeader.SetRect(0, 0, 60, 1)
	r.renderGitBreachBranchesHeader() // re-populate spans against this rect's own width

	var tagsSpan gitBreachBranchesTabSpan
	found := false
	for _, span := range r.gitBreach.BranchesTabSpans {
		if span.tab == gitBreachBranchesTabTags {
			tagsSpan, found = span, true
		}
	}
	if !found {
		t.Fatal("no span found for the Tags sub-tab")
	}

	rectX, _, _, _ := r.gitBreach.BranchesHeader.GetInnerRect()
	x := rectX + (tagsSpan.start+tagsSpan.end)/2
	event := tcell.NewEventMouse(x, 0, tcell.Button1, tcell.ModNone)

	action, returnedEvent := r.gitBreachFocusBranchesOnClick(tview.MouseLeftDown, event)

	if action != tview.MouseConsumed || returnedEvent != nil {
		t.Errorf("gitBreachFocusBranchesOnClick = (%v, %v), want (MouseConsumed, nil)", action, returnedEvent)
	}
	if r.gitBreach.BranchesTab != gitBreachBranchesTabTags {
		t.Errorf("BranchesTab = %v, want Tags after clicking its own label", r.gitBreach.BranchesTab)
	}
}

// TestGitBreachFocusBranchesOnClickOutsideAnyLabelStillFocusesTheTable
// pins the fallback: a click inside the header but not on any
// specific label (the leading/trailing padding) still focuses the
// table, same as before this click handler could switch tabs at all.
func TestGitBreachFocusBranchesOnClickOutsideAnyLabelStillFocusesTheTable(t *testing.T) {
	requireGitForBreach(t)
	dir := initGitBreachRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitBreach(t, dir, "add", "a.txt")
	runGitBreach(t, dir, "commit", "-q", "-m", "initial")

	r := newTestRootForGitBreachDir(t, dir)
	r.openGitBreach()
	r.gitBreach.BranchesHeader.SetRect(0, 0, 60, 1)
	r.renderGitBreachBranchesHeader()

	rectX, _, _, _ := r.gitBreach.BranchesHeader.GetInnerRect()
	event := tcell.NewEventMouse(rectX, 0, tcell.Button1, tcell.ModNone) // column 0: the leading space, before any label

	before := r.gitBreach.BranchesTab
	r.gitBreachFocusBranchesOnClick(tview.MouseLeftDown, event)

	if r.gitBreach.BranchesTab != before {
		t.Errorf("BranchesTab changed to %v, want it unchanged by a click outside any label", r.gitBreach.BranchesTab)
	}
}
