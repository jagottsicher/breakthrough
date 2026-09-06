package ui

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/jagottsicher/breakthrough/internal/batchrename"
)

// newBatchRenameRoot opens the Batch Rename screen against fixtureDir's
// two "ap*" files (apple.txt, apricot.txt) — real files on a real temp
// directory, the same reasoning newOptionsRoot's own doc comment gives,
// since Apply/Undo genuinely touch disk here.
func newBatchRenameRoot(t *testing.T) (*Root, string) {
	t.Helper()
	dir := fixtureDir(t)

	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	if _, err := r.panel.selectByPattern("ap*.txt", true); err != nil {
		t.Fatalf("selectByPattern: %v", err)
	}
	r.openBatchRename()
	return r, dir
}

// selectBatchRenameStep switches the screen to the named step, so a
// test can reach fields outside the default one — the same helper
// selectOptionCategory already is for the Options screen.
func selectBatchRenameStep(t *testing.T, r *Root, name string) {
	t.Helper()
	for i, step := range batchRenameSteps() {
		if step.name == name {
			r.batchRenameStepsList.SetCurrentItem(i)
			return
		}
	}
	t.Fatalf("no batch rename step named %q", name)
}

func TestOpenBatchRenamePopulatesStepsAndTargets(t *testing.T) {
	r, dir := newBatchRenameRoot(t)

	if got, want := r.batchRenameStepsList.GetItemCount(), len(batchRenameSteps()); got != want {
		t.Errorf("steps list has %d items, want %d", got, want)
	}
	want := []string{filepath.Join(dir, "apple.txt"), filepath.Join(dir, "apricot.txt")}
	if len(r.batchRenameTargets) != len(want) {
		t.Fatalf("targets = %v, want %v", r.batchRenameTargets, want)
	}
}

func TestOpenBatchRenameAlwaysStartsFromAFreshRules(t *testing.T) {
	r, _ := newBatchRenameRoot(t)
	r.batchRenameRules.Find = "leftover"

	r.openBatchRename()

	if r.batchRenameRules.Find != "" {
		t.Errorf("Find = %q, want a fresh Rules on every open", r.batchRenameRules.Find)
	}
}

func TestBatchRenameBoolFieldTogglesInPlace(t *testing.T) {
	r, _ := newBatchRenameRoot(t) // default step is "Search & Replace"

	r.activateBatchRenameFieldRow(2) // Find, Replace with, Regex
	if !r.batchRenameRules.Regex {
		t.Error("Regex should be true after activating its row once")
	}
	r.activateBatchRenameFieldRow(2)
	if r.batchRenameRules.Regex {
		t.Error("Regex should be false after activating its row twice")
	}
}

func TestBatchRenameEnumFieldCyclesImmediately(t *testing.T) {
	r, _ := newBatchRenameRoot(t)
	selectBatchRenameStep(t, r, "Case")

	if r.batchRenameRules.Case != batchrename.CaseNone {
		t.Fatalf("expected CaseNone initially, got %v", r.batchRenameRules.Case)
	}
	r.activateBatchRenameFieldRow(0)
	if r.batchRenameRules.Case != batchrename.CaseUpper {
		t.Errorf("expected CaseUpper after one activation, got %v", r.batchRenameRules.Case)
	}
}

func TestBatchRenameIntFieldOpensAnEditorThatCommitsOnEnter(t *testing.T) {
	r, _ := newBatchRenameRoot(t)
	selectBatchRenameStep(t, r, "Trim")

	r.activateBatchRenameFieldRow(0) // "Characters off the front"
	if r.activePage != batchRenameInputPage {
		t.Fatalf("activePage = %q, want the input editor %q", r.activePage, batchRenameInputPage)
	}

	r.batchRenameInput.SetText("3")
	r.batchRenameInput.InputHandler()(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone), func(tview.Primitive) {})

	if r.batchRenameRules.TrimFront != 3 {
		t.Errorf("TrimFront = %d, want 3", r.batchRenameRules.TrimFront)
	}
	if r.activePage != batchRenamePage {
		t.Errorf("activePage = %q, want back to the screen %q", r.activePage, batchRenamePage)
	}
}

func TestBatchRenameIntFieldEditorDiscardsOnEscape(t *testing.T) {
	r, _ := newBatchRenameRoot(t)
	selectBatchRenameStep(t, r, "Trim")

	r.activateBatchRenameFieldRow(0)
	r.batchRenameInput.SetText("9")
	r.batchRenameInput.InputHandler()(tcell.NewEventKey(tcell.KeyEscape, 0, tcell.ModNone), func(tview.Primitive) {})

	if r.batchRenameRules.TrimFront != 0 {
		t.Errorf("TrimFront = %d, want 0 (Escape should discard)", r.batchRenameRules.TrimFront)
	}
}

func TestBatchRenamePreviewShowsChangesAndUnchanged(t *testing.T) {
	r, _ := newBatchRenameRoot(t)

	r.batchRenameRules.Find = "apple"
	r.batchRenameRules.Replace = "pear"
	r.renderBatchRenamePreview()

	if len(r.batchRenamePendingChanges) != 1 {
		t.Fatalf("pending changes = %+v, want exactly one", r.batchRenamePendingChanges)
	}
	if got := filepath.Base(r.batchRenamePendingChanges[0].To); got != "pear.txt" {
		t.Errorf("planned new name = %q, want %q", got, "pear.txt")
	}
}

func TestBatchRenamePreviewFlagsAConflict(t *testing.T) {
	r, dir := newBatchRenameRoot(t)
	// apple.txt -> banana.txt collides with the fixture's own,
	// unselected banana.txt already sitting in dir.
	r.batchRenameRules.Find = "apple"
	r.batchRenameRules.Replace = "banana"
	r.renderBatchRenamePreview()

	result := batchrename.Plan(r.batchRenameTargets, r.batchRenameRules)
	if len(result.Problems) != 1 || filepath.Dir(result.Problems[0].Path) != dir {
		t.Fatalf("expected exactly one conflict under %s, got %+v", dir, result.Problems)
	}
}

func TestBatchRenamePaneArrowsMoveBetweenStepsAndFields(t *testing.T) {
	r, _ := newBatchRenameRoot(t)
	app := tview.NewApplication()
	r.app = app

	app.SetFocus(r.batchRenameStepsList)
	r.captureBatchRenamePaneArrows(tcell.NewEventKey(tcell.KeyRight, 0, tcell.ModNone))
	if !r.batchRenameFieldsTable.HasFocus() {
		t.Error("Right from the steps list should move focus to the fields table")
	}

	r.captureBatchRenamePaneArrows(tcell.NewEventKey(tcell.KeyLeft, 0, tcell.ModNone))
	if !r.batchRenameStepsList.HasFocus() {
		t.Error("Left from the fields table should move focus back to the steps list")
	}
}

func TestClickingABatchRenameStepDoesNotCloseTheScreen(t *testing.T) {
	r, _ := newBatchRenameRoot(t)

	x, y, w, h := r.batchRenameStepsList.GetRect()
	if w == 0 || h == 0 {
		t.Fatal("steps list has no rect yet")
	}
	action, _ := r.captureOutsideClick(tview.MouseLeftClick, tcellMouseEventAt(x, y))
	if action != tview.MouseLeftClick {
		t.Fatalf("expected the click to pass through, got %v", action)
	}
	if r.activePage != batchRenamePage {
		t.Errorf("activePage = %q, want the screen to stay open (%q)", r.activePage, batchRenamePage)
	}
}

func TestResetBatchRenameStepsClearsEveryField(t *testing.T) {
	r, _ := newBatchRenameRoot(t)
	r.batchRenameRules = batchrename.Rules{Find: "x", Case: batchrename.CaseUpper, TrimFront: 2}

	r.resetBatchRenameSteps()

	if r.batchRenameRules != (batchrename.Rules{}) {
		t.Errorf("Rules = %+v, want the zero value after reset", r.batchRenameRules)
	}
}

func TestBatchRenameEscapeClosesWithoutTouchingDisk(t *testing.T) {
	r, dir := newBatchRenameRoot(t)
	r.batchRenameRules.Find = "apple"
	r.batchRenameRules.Replace = "pear"

	r.captureBatchRenameKey(tcell.NewEventKey(tcell.KeyEscape, 0, tcell.ModNone))

	if r.activePage == batchRenamePage {
		t.Error("Escape should have closed the screen")
	}
	if _, err := os.Stat(filepath.Join(dir, "apple.txt")); err != nil {
		t.Errorf("apple.txt should still exist untouched: %v", err)
	}
}

func TestConfirmApplyBatchRenameRenamesAndReloadsThePanel(t *testing.T) {
	r, dir := newBatchRenameRoot(t)
	r.batchRenameRules.Find = "apple"
	r.batchRenameRules.Replace = "pear"

	r.confirmApplyBatchRename()
	if r.activePage != confirmPage {
		t.Fatalf("activePage = %q, want the confirmation dialog %q", r.activePage, confirmPage)
	}
	r.confirmDialog.SetCurrentItem(2)
	r.confirmDialog.InputHandler()(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone), func(tview.Primitive) {})

	if _, err := os.Stat(filepath.Join(dir, "pear.txt")); err != nil {
		t.Errorf("pear.txt should exist: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "apple.txt")); !os.IsNotExist(err) {
		t.Errorf("apple.txt should be gone, stat err = %v", err)
	}
	if len(r.batchRenameUndo) != 1 {
		t.Errorf("batchRenameUndo = %+v, want exactly one recorded change", r.batchRenameUndo)
	}
}

func TestConfirmApplyBatchRenameWithNoChangesDoesNothing(t *testing.T) {
	r, _ := newBatchRenameRoot(t) // zero Rules: nothing would change

	r.confirmApplyBatchRename()

	if r.activePage == confirmPage {
		t.Error("should not ask for confirmation when nothing would change")
	}
}

func TestUndoLastBatchRenameReversesTheLastApply(t *testing.T) {
	r, dir := newBatchRenameRoot(t)
	r.batchRenameRules.Find = "apple"
	r.batchRenameRules.Replace = "pear"
	r.confirmApplyBatchRename()
	r.confirmDialog.SetCurrentItem(2)
	r.confirmDialog.InputHandler()(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone), func(tview.Primitive) {})

	r.undoLastBatchRename()

	if _, err := os.Stat(filepath.Join(dir, "apple.txt")); err != nil {
		t.Errorf("apple.txt should exist again after undo: %v", err)
	}
	if len(r.batchRenameUndo) != 0 {
		t.Errorf("batchRenameUndo should be cleared after use, got %+v", r.batchRenameUndo)
	}
}

func TestUndoLastBatchRenameWithNothingToUndoShowsANotice(t *testing.T) {
	r, err := NewRoot(tview.NewApplication(), fixtureDir(t))
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}

	r.undoLastBatchRename()

	if r.activePage != errorPage {
		t.Fatalf("activePage = %q, want the notice overlay %q", r.activePage, errorPage)
	}
}

// tcellMouseEventAt builds a plain left-click-position mouse event at
// (x, y) — captureOutsideClick only ever reads Position() off it.
func tcellMouseEventAt(x, y int) *tcell.EventMouse {
	return tcell.NewEventMouse(x, y, tcell.Button1, tcell.ModNone)
}
