package ui

import (
	"os"
	"path/filepath"
	"testing"

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

// TestCaptureGitBreachFilesTableKeyRightMeansDiffOnlyOnChanges pins
// the one real behavior change this patch makes: Right still means
// "view diff" on Changes (unchanged), but means "next tab" on
// Worktrees/Submodules instead — see captureGitBreachFilesTableKey's
// own doc comment on why the two meanings never apply to the same
// sub-tab at once.
func TestCaptureGitBreachFilesTableKeyRightMeansDiffOnlyOnChanges(t *testing.T) {
	r := gitBreachFilesTabFixture(t)

	r.captureGitBreachFilesTableKey(tcell.NewEventKey(tcell.KeyRight, 0, tcell.ModNone))

	if r.gitBreach.MainOwner != gitBreachMainOwnerFiles {
		t.Error("Right on Changes did not activate Main — existing diff behavior broke")
	}
	if r.gitBreach.FilesTab != gitBreachFilesTabChanges {
		t.Error("Right on Changes changed FilesTab, want it to stay on Changes")
	}

	r.deactivateGitBreachMain()
	r.cycleGitBreachFilesTab(1) // -> Worktrees

	r.captureGitBreachFilesTableKey(tcell.NewEventKey(tcell.KeyRight, 0, tcell.ModNone))

	if r.gitBreach.FilesTab != gitBreachFilesTabSubmodules {
		t.Errorf("FilesTab = %v after Right on Worktrees, want it to have cycled to Submodules", r.gitBreach.FilesTab)
	}
}

func TestCaptureGitBreachFilesTableKeyLeftCyclesTabsOnChangesToo(t *testing.T) {
	r := gitBreachFilesTabFixture(t)

	r.captureGitBreachFilesTableKey(tcell.NewEventKey(tcell.KeyLeft, 0, tcell.ModNone))

	if r.gitBreach.FilesTab != gitBreachFilesTabSubmodules {
		t.Errorf("FilesTab = %v after Left on Changes, want it wrapped to Submodules", r.gitBreach.FilesTab)
	}
}

func TestRenderGitBreachFilesOnWorktreesShowsAPlaceholder(t *testing.T) {
	r := gitBreachFilesTabFixture(t)

	r.setGitBreachFilesTab(gitBreachFilesTabWorktrees)

	if got, want := r.gitBreach.FilesTable.GetCell(1, 0).Text, "Worktrees aren't wired up yet."; got != want {
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
