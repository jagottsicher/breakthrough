package ui

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/jagottsicher/breakthrough/internal/git"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

func gitBreachFilesTabFixture(t *testing.T) *Root {
	t.Helper()
	requireGitForBreach(t)
	dir := initGitBreachRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitBreach(t, dir, "add", "a.txt")
	runGitBreach(t, dir, "commit", "-q", "-m", "initial")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a changed"), 0o644); err != nil {
		t.Fatal(err)
	}

	r := newTestRootForGitBreachDir(t, dir)
	r.openGitBreach()
	r.app.SetFocus(r.gitBreach.FilesTable)
	if r.gitBreach.FilesTab != gitBreachFilesTabChanges {
		t.Fatal("setup: FilesTab != Changes, want a fresh \"jg\" to start there")
	}
	return r
}

func TestCycleGitBreachFilesTabWrapsThroughAllThree(t *testing.T) {
	r := gitBreachFilesTabFixture(t)

	r.cycleGitBreachFilesTab(1)
	if r.gitBreach.FilesTab != gitBreachFilesTabWorktrees {
		t.Fatalf("FilesTab = %v after one Right, want Worktrees", r.gitBreach.FilesTab)
	}
	r.cycleGitBreachFilesTab(1)
	if r.gitBreach.FilesTab != gitBreachFilesTabSubmodules {
		t.Fatalf("FilesTab = %v after two Right, want Submodules", r.gitBreach.FilesTab)
	}
	r.cycleGitBreachFilesTab(1)
	if r.gitBreach.FilesTab != gitBreachFilesTabChanges {
		t.Fatalf("FilesTab = %v after three Right, want it wrapped back to Changes", r.gitBreach.FilesTab)
	}
	r.cycleGitBreachFilesTab(-1)
	if r.gitBreach.FilesTab != gitBreachFilesTabSubmodules {
		t.Fatalf("FilesTab = %v after one Left from Changes, want it wrapped to Submodules", r.gitBreach.FilesTab)
	}
}

// TestCaptureGitBreachFilesTableKeyRightAlwaysCyclesTabs pins Right's
// own uniform meaning on every sub-tab: cycle forward, same as Left
// cycles backward, on Changes included. Enter, not Right, is this
// box's own way into Main on Changes (see the test right below) —
// Right used to double as "view diff" on Changes specifically, which
// broke the left/right symmetry every other cycle in this dashboard
// already has (see captureGitBreachFilesTableKey's own doc comment).
func TestCaptureGitBreachFilesTableKeyRightAlwaysCyclesTabs(t *testing.T) {
	r := gitBreachFilesTabFixture(t)

	r.captureGitBreachFilesTableKey(tcell.NewEventKey(tcell.KeyRight, 0, tcell.ModNone))

	if r.gitBreachMainHasFocus() {
		t.Error("Right on Changes activated Main, want it to cycle tabs instead")
	}
	if r.gitBreach.FilesTab != gitBreachFilesTabWorktrees {
		t.Errorf("FilesTab = %v after Right on Changes, want it to have cycled to Worktrees", r.gitBreach.FilesTab)
	}

	r.captureGitBreachFilesTableKey(tcell.NewEventKey(tcell.KeyRight, 0, tcell.ModNone))

	if r.gitBreach.FilesTab != gitBreachFilesTabSubmodules {
		t.Errorf("FilesTab = %v after Right on Worktrees, want it to have cycled to Submodules", r.gitBreach.FilesTab)
	}
}

// TestCaptureGitBreachFilesTableKeyEnterActivatesMainOnChangesOnly
// pins Changes' own way into Main now that Right no longer doubles
// as it — a no-op on Worktrees/Submodules, which have no diff of
// their own to show at all.
func TestCaptureGitBreachFilesTableKeyEnterActivatesMainOnChangesOnly(t *testing.T) {
	r := gitBreachFilesTabFixture(t)

	r.captureGitBreachFilesTableKey(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone))

	if !r.gitBreachMainHasFocus() {
		t.Error("Enter on Changes did not activate Main")
	}

	r.deactivateGitBreachMain()
	r.cycleGitBreachFilesTab(1) // -> Worktrees

	r.captureGitBreachFilesTableKey(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone))

	if r.gitBreachMainHasFocus() {
		t.Error("Enter on Worktrees activated Main, want a no-op (no diff to show)")
	}
}

func TestCaptureGitBreachFilesTableKeyLeftCyclesTabsOnChangesToo(t *testing.T) {
	r := gitBreachFilesTabFixture(t)

	r.captureGitBreachFilesTableKey(tcell.NewEventKey(tcell.KeyLeft, 0, tcell.ModNone))

	if r.gitBreach.FilesTab != gitBreachFilesTabSubmodules {
		t.Errorf("FilesTab = %v after Left on Changes, want it wrapped to Submodules", r.gitBreach.FilesTab)
	}
}

func TestRenderGitBreachFilesOnWorktreesListsThem(t *testing.T) {
	r := gitBreachFilesTabFixture(t)
	r.gitBreach.Worktrees = []git.Worktree{{Path: "/repo", Branch: "main"}}

	r.setGitBreachFilesTab(gitBreachFilesTabWorktrees)

	if got, want := r.gitBreach.FilesTable.GetCell(0, 0).Text, "  /repo  main"; got != want {
		t.Errorf("row text = %q, want %q", got, want)
	}
}

func TestRenderGitBreachFilesOnWorktreesWithNoneShowsAPlaceholder(t *testing.T) {
	r := gitBreachFilesTabFixture(t)
	r.gitBreach.Worktrees = nil

	r.setGitBreachFilesTab(gitBreachFilesTabWorktrees)

	if got, want := r.gitBreach.FilesTable.GetCell(1, 0).Text, "No worktrees."; got != want {
		t.Errorf("placeholder text = %q, want %q", got, want)
	}
}

func TestRenderGitBreachFilesHeaderTracksEachLabelsOwnSpan(t *testing.T) {
	r := gitBreachFilesTabFixture(t)

	spans := r.gitBreach.FilesTabSpans
	if len(spans) != 3 {
		t.Fatalf("FilesTabSpans = %+v, want 3 entries", spans)
	}
	wantOrder := []gitBreachFilesTab{gitBreachFilesTabChanges, gitBreachFilesTabWorktrees, gitBreachFilesTabSubmodules}
	for i, want := range wantOrder {
		if spans[i].tab != want {
			t.Errorf("spans[%d].tab = %v, want %v", i, spans[i].tab, want)
		}
		if spans[i].end <= spans[i].start {
			t.Errorf("spans[%d] = %+v, want a real, non-empty column range", i, spans[i])
		}
	}
}

func TestGitBreachFocusFilesOnClickSwitchesToTheClickedSubTab(t *testing.T) {
	r := gitBreachFilesTabFixture(t)
	r.gitBreach.FilesHeader.SetRect(0, 0, 60, 1)
	r.renderGitBreachFilesHeader()

	var worktreesSpan gitBreachFilesTabSpan
	found := false
	for _, span := range r.gitBreach.FilesTabSpans {
		if span.tab == gitBreachFilesTabWorktrees {
			worktreesSpan, found = span, true
		}
	}
	if !found {
		t.Fatal("no span found for the Worktrees sub-tab")
	}

	rectX, _, _, _ := r.gitBreach.FilesHeader.GetInnerRect()
	x := rectX + (worktreesSpan.start+worktreesSpan.end)/2
	event := tcell.NewEventMouse(x, 0, tcell.Button1, tcell.ModNone)

	action, returnedEvent := r.gitBreachFocusFilesOnClick(tview.MouseLeftDown, event)

	if action != tview.MouseConsumed || returnedEvent != nil {
		t.Errorf("gitBreachFocusFilesOnClick = (%v, %v), want (MouseConsumed, nil)", action, returnedEvent)
	}
	if r.gitBreach.FilesTab != gitBreachFilesTabWorktrees {
		t.Errorf("FilesTab = %v, want Worktrees after clicking its own label", r.gitBreach.FilesTab)
	}
}
