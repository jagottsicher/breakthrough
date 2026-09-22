package ui

import (
	"os"
	"path/filepath"
	"strings"
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
	r.confirmDialog.SetCurrentItem(0)
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
	r.confirmDialog.SetCurrentItem(0)
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

func TestBatchRenameStepMarksFollowTheRules(t *testing.T) {
	r, _ := newBatchRenameRoot(t)

	main, _ := r.batchRenameStepsList.GetItemText(0)
	if main != batchRenameInactiveMark+"Search & Replace" {
		t.Fatalf("fresh Search & Replace item = %q, want it unmarked", main)
	}

	// Typing a Find value through the field's own apply func is what a
	// user does — the mark must follow without a full rebuild.
	f, _ := r.batchRenameFieldAtRow(0)
	f.apply(r, "apple")

	main, _ = r.batchRenameStepsList.GetItemText(0)
	if main != batchRenameActiveMark+"Search & Replace" {
		t.Errorf("Search & Replace item after Find set = %q, want it marked active", main)
	}
	if cur := r.batchRenameStepsList.GetCurrentItem(); cur != 0 {
		t.Errorf("re-marking moved the list selection to %d", cur)
	}

	r.resetBatchRenameSteps()
	main, _ = r.batchRenameStepsList.GetItemText(0)
	if main != batchRenameInactiveMark+"Search & Replace" {
		t.Errorf("Search & Replace item after reset = %q, want it unmarked again", main)
	}
}

func TestBatchRenameFieldHelpFollowsTheSelectedRow(t *testing.T) {
	r, _ := newBatchRenameRoot(t)

	fields := batchRenameSteps()[0].fields
	if got := r.batchRenameFieldHelp.GetText(true); got != fields[0].help {
		t.Errorf("help on open = %q, want the first field's own %q", got, fields[0].help)
	}

	r.batchRenameFieldsTable.Select(2, 0) // Regex
	if got := r.batchRenameFieldHelp.GetText(true); got != fields[2].help {
		t.Errorf("help after selecting row 2 = %q, want %q", got, fields[2].help)
	}

	selectBatchRenameStep(t, r, "Trim")
	trim := batchRenameSteps()[2].fields
	if got := r.batchRenameFieldHelp.GetText(true); got != trim[0].help && got != trim[1].help {
		t.Errorf("help after switching to Trim = %q, want one of Trim's own field helps", got)
	}
}

func TestEveryBatchRenameFieldHasHelpText(t *testing.T) {
	for _, step := range batchRenameSteps() {
		if step.active == nil {
			t.Errorf("step %q has no active func", step.name)
		}
		for _, f := range step.fields {
			if f.help == "" {
				t.Errorf("field %q of step %q has no help text", f.label, step.name)
			}
		}
	}
}

func TestBatchRenamePreviewLeavesAFolderExtensionAloneUnlessAsked(t *testing.T) {
	r, dir := newBatchRenameRoot(t)
	if err := os.Mkdir(filepath.Join(dir, "my.project"), 0o755); err != nil {
		t.Fatal(err)
	}
	r.batchRenameTargets = []string{filepath.Join(dir, "my.project"), filepath.Join(dir, "apple.txt")}
	r.batchRenameRules.ExtensionMode = batchrename.ExtensionUpper
	r.renderBatchRenamePreview()

	if len(r.batchRenamePendingChanges) != 1 || filepath.Base(r.batchRenamePendingChanges[0].To) != "apple.TXT" {
		t.Fatalf("pending = %+v, want only apple.txt -> apple.TXT", r.batchRenamePendingChanges)
	}

	selectBatchRenameStep(t, r, "Extension")
	r.activateBatchRenameFieldRow(2) // "Treat folder names as having extensions too"
	if !r.batchRenameRules.ExtensionOnDirs {
		t.Fatal("ExtensionOnDirs should be on after toggling its row")
	}
	names := make(map[string]bool)
	for _, c := range r.batchRenamePendingChanges {
		names[filepath.Base(c.To)] = true
	}
	if !names["my.PROJECT"] || !names["apple.TXT"] {
		t.Errorf("pending after opting folders in = %v, want both my.PROJECT and apple.TXT", names)
	}
}

// key sends one key event straight into a primitive's input handler,
// the way the tests above already do for the confirm dialog.
func key(p tview.Primitive, k tcell.Key, r rune) {
	p.InputHandler()(tcell.NewEventKey(k, r, tcell.ModNone), func(tview.Primitive) {})
}

func TestBatchRenameTargetsFollowThePanelDisplayOrder(t *testing.T) {
	r, dir := newBatchRenameRoot(t)
	r.closeBatchRename()

	// Descending by name: the display order is then the *reverse* of the
	// order the selection was made in, which no accidental map-iteration
	// order (a rotation of insertion order at best) can reproduce.
	r.panel.setSortKey(sortByName) // already the key: flips to descending (and reloads, dropping the selection)
	if !r.panel.sortDescending {
		t.Fatal("expected the panel to be sorted descending now")
	}
	// Selected bottom-up on purpose, so insertion order into the
	// selection set is the exact reverse of what's on screen.
	for _, pattern := range []string{"apple*", "apricot*", "banana*"} {
		if _, err := r.panel.selectByPattern(pattern, true); err != nil {
			t.Fatal(err)
		}
	}
	r.openBatchRename()

	want := []string{filepath.Join(dir, "banana.txt"), filepath.Join(dir, "apricot.txt"), filepath.Join(dir, "apple.txt")}
	if len(r.batchRenameTargets) != len(want) {
		t.Fatalf("targets = %v, want %v", r.batchRenameTargets, want)
	}
	for i := range want {
		if r.batchRenameTargets[i] != want[i] {
			t.Fatalf("targets = %v, want the panel's own top-to-bottom order %v", r.batchRenameTargets, want)
		}
	}
}

func TestBatchRenamePreviewSpaceSkipsARowAndItsNumber(t *testing.T) {
	r, _ := newBatchRenameRoot(t)
	r.batchRenameRules.NumberPosition = batchrename.NumberPrefix
	r.batchRenameRules.NumberStart = 1
	r.renderBatchRenamePreview()

	r.batchRenamePreviewTable.Select(1, 0) // apple.txt
	key(r.batchRenamePreviewTable, tcell.KeyRune, ' ')

	if len(r.batchRenamePendingChanges) != 1 || filepath.Base(r.batchRenamePendingChanges[0].To) != "1-apricot.txt" {
		t.Errorf("pending after skipping apple = %+v, want only apricot, numbered 1 (not 2)", r.batchRenamePendingChanges)
	}
	if got := r.batchRenamePreviewTable.GetCell(1, 3).Text; got != "(skipped)" {
		t.Errorf("row 1 note = %q, want (skipped)", got)
	}
	if got := r.batchRenameStatus.GetText(false); !strings.Contains(got, "1 skipped") {
		t.Errorf("status = %q, want it to count 1 skipped", got)
	}

	key(r.batchRenamePreviewTable, tcell.KeyRune, ' ')
	if len(r.batchRenamePendingChanges) != 2 {
		t.Errorf("pending after ticking apple back = %+v, want both again", r.batchRenamePendingChanges)
	}
}

func TestBatchRenamePreviewMoveRowFreezesOrderAsListed(t *testing.T) {
	r, dir := newBatchRenameRoot(t)
	r.batchRenameRules.NumberOrder = batchrename.OrderByName
	r.batchRenameRules.NumberReversed = true // shown: apricot, apple
	r.renderBatchRenamePreview()
	if got := r.batchRenamePreviewTable.GetCell(1, 1).Text; got != "apricot.txt" {
		t.Fatalf("row 1 under by-name reversed = %q, want apricot.txt", got)
	}

	r.batchRenamePreviewTable.Select(1, 0)
	key(r.batchRenamePreviewTable, tcell.KeyRune, 'd')

	if r.batchRenameRules.NumberOrder != batchrename.OrderAsListed || r.batchRenameRules.NumberReversed {
		t.Errorf("moving a row should switch to as-listed, unreversed; got %v reversed=%v", r.batchRenameRules.NumberOrder, r.batchRenameRules.NumberReversed)
	}
	want := []string{filepath.Join(dir, "apple.txt"), filepath.Join(dir, "apricot.txt")}
	for i := range want {
		if r.batchRenameTargets[i] != want[i] {
			t.Fatalf("targets after moving apricot down = %v, want %v", r.batchRenameTargets, want)
		}
	}
	if row, _ := r.batchRenamePreviewTable.GetSelection(); row != 2 {
		t.Errorf("cursor should follow the moved row to 2, got %d", row)
	}
}

func TestBatchRenamePreviewJumpKeysFindChangesAndConflicts(t *testing.T) {
	r, _ := newBatchRenameRoot(t)
	r.batchRenameRules.Find = "apricot"
	r.batchRenameRules.Replace = "banana" // collides with the unselected banana.txt on disk
	r.renderBatchRenamePreview()

	r.batchRenamePreviewTable.Select(1, 0) // apple: unchanged
	key(r.batchRenamePreviewTable, tcell.KeyRune, 'c')
	if row, _ := r.batchRenamePreviewTable.GetSelection(); row != 2 {
		t.Errorf("c should jump to the conflict on row 2, got %d", row)
	}

	r.batchRenameRules.Replace = "cherry"
	r.renderBatchRenamePreview()
	r.batchRenamePreviewTable.Select(2, 0)
	key(r.batchRenamePreviewTable, tcell.KeyRune, 'n')
	if row, _ := r.batchRenamePreviewTable.GetSelection(); row != 2 {
		t.Errorf("n from the only change should wrap back to itself (row 2), got %d", row)
	}
	r.batchRenamePreviewTable.Select(1, 0)
	key(r.batchRenamePreviewTable, tcell.KeyRune, 'p')
	if row, _ := r.batchRenamePreviewTable.GetSelection(); row != 2 {
		t.Errorf("p from row 1 should wrap to the change on row 2, got %d", row)
	}
}

func TestBatchRenameSavePresetThenLoadRestoresTheRules(t *testing.T) {
	r, _ := newBatchRenameRoot(t)
	r.batchRenamePresetDir = filepath.Join(t.TempDir(), "rename-presets")
	r.batchRenameRules.Find = "IMG_"
	r.batchRenameRules.Case = batchrename.CaseLower
	r.batchRenameRules.NumberOrder = batchrename.OrderByModTime

	r.openBatchRenameSavePreset()
	if r.activePage != batchRenameInputPage {
		t.Fatalf("activePage = %q, want the name input", r.activePage)
	}
	r.batchRenameInput.SetText("camera")
	key(r.batchRenameInput, tcell.KeyEnter, 0)

	if !batchrename.PresetExists(r.batchRenamePresetDir, "camera") {
		t.Fatal("preset file should exist after saving")
	}
	if got := r.batchRenameStatus.GetText(false); !strings.Contains(got, `"camera" saved`) {
		t.Errorf("status = %q, want a saved notice", got)
	}

	r.resetBatchRenameSteps()
	r.openBatchRenamePresetPicker()
	if r.activePage != batchRenamePresetPage {
		t.Fatalf("activePage = %q, want the preset picker", r.activePage)
	}
	if name, _ := r.batchRenamePresetList.GetItemText(0); name != "camera" {
		t.Fatalf("picker item 0 = %q, want camera", name)
	}
	key(r.batchRenamePresetList, tcell.KeyEnter, 0)

	if r.batchRenameRules.Find != "IMG_" || r.batchRenameRules.Case != batchrename.CaseLower || r.batchRenameRules.NumberOrder != batchrename.OrderByModTime {
		t.Errorf("rules after loading = %+v, want the saved ones back", r.batchRenameRules)
	}
	if r.activePage != batchRenamePage {
		t.Errorf("activePage = %q, want back on the screen", r.activePage)
	}
}

func TestBatchRenameSavePresetOverAnExistingNameAsksFirst(t *testing.T) {
	r, _ := newBatchRenameRoot(t)
	r.batchRenamePresetDir = t.TempDir()
	if err := batchrename.SavePreset(r.batchRenamePresetDir, "old", batchrename.Rules{Find: "before"}); err != nil {
		t.Fatal(err)
	}
	r.batchRenameRules.Find = "after"

	r.openBatchRenameSavePreset()
	r.batchRenameInput.SetText("old")
	key(r.batchRenameInput, tcell.KeyEnter, 0)
	if r.activePage != confirmPage {
		t.Fatalf("activePage = %q, want the replace confirmation", r.activePage)
	}
	presets, _ := batchrename.LoadPresets(r.batchRenamePresetDir)
	if presets[0].Rules.Find != "before" {
		t.Fatal("preset was replaced before the question was answered")
	}

	r.confirmDialog.SetCurrentItem(0)
	key(r.confirmDialog, tcell.KeyEnter, 0)
	presets, _ = batchrename.LoadPresets(r.batchRenamePresetDir)
	if presets[0].Rules.Find != "after" {
		t.Errorf("preset after confirming = %+v, want Find=after", presets[0].Rules)
	}
}

func TestBatchRenamePresetPickerDeletesAfterAsking(t *testing.T) {
	r, _ := newBatchRenameRoot(t)
	r.batchRenamePresetDir = t.TempDir()
	if err := batchrename.SavePreset(r.batchRenamePresetDir, "gone", batchrename.Rules{}); err != nil {
		t.Fatal(err)
	}

	r.openBatchRenamePresetPicker()
	key(r.batchRenamePresetList, tcell.KeyRune, 'd')
	if r.activePage != confirmPage {
		t.Fatalf("activePage = %q, want the delete confirmation", r.activePage)
	}
	r.confirmDialog.SetCurrentItem(0)
	key(r.confirmDialog, tcell.KeyEnter, 0)

	if batchrename.PresetExists(r.batchRenamePresetDir, "gone") {
		t.Error("preset should be deleted after confirming")
	}
	if r.activePage != batchRenamePresetPage {
		t.Errorf("activePage = %q, want the picker still open (rebuilt)", r.activePage)
	}
	if name, _ := r.batchRenamePresetList.GetItemText(0); name != "(no presets saved yet)" {
		t.Errorf("picker item 0 after deleting = %q, want the empty placeholder", name)
	}
}

func TestBatchRenameTemplateStepRebuildsNamesLive(t *testing.T) {
	r, dir := newBatchRenameRoot(t)
	selectBatchRenameStep(t, r, "Template")

	f, _ := r.batchRenameFieldAtRow(0)
	if !strings.Contains(f.help, "{counter}") || !strings.Contains(f.help, "{parent}") {
		t.Errorf("Template help should list the tokens, got %q", f.help)
	}
	f.apply(r, "{parent}_{counter}_{name}")

	if len(r.batchRenamePendingChanges) != 2 {
		t.Fatalf("pending = %+v, want both files rebuilt", r.batchRenamePendingChanges)
	}
	want := filepath.Base(dir) + "_0_apple.txt"
	if got := filepath.Base(r.batchRenamePendingChanges[0].To); got != want {
		t.Errorf("first new name = %q, want %q", got, want)
	}
	if main, _ := r.batchRenameStepsList.GetItemText(3); main != batchRenameActiveMark+"Template" {
		t.Errorf("Template item = %q, want it marked active", main)
	}
}

// TestOpenBatchRenameOnALoneFolderExpandsToItsContents pins the fix for
// feedback: applying Batch Rename to a single folder (nothing else
// selected) used to make that folder itself the one and only target —
// a pointless single-item rename. It should instead act on the files
// (and subfolders) inside it, as if they had been selected directly.
func TestOpenBatchRenameOnALoneFolderExpandsToItsContents(t *testing.T) {
	dir := fixtureDir(t)
	if err := os.WriteFile(filepath.Join(dir, "app-data", "inner.txt"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	selectRow(r, 1) // app-data, the only folder

	r.openBatchRename()

	want := []string{filepath.Join(dir, "app-data", "inner.txt")}
	if len(r.batchRenameTargets) != len(want) || r.batchRenameTargets[0] != want[0] {
		t.Fatalf("targets = %v, want %v (the folder's own contents)", r.batchRenameTargets, want)
	}
}

// TestOpenBatchRenameOnAnEmptyFolderShowsAnError pins that an empty lone
// folder reports an explicit error instead of silently opening the
// screen with no targets at all.
func TestOpenBatchRenameOnAnEmptyFolderShowsAnError(t *testing.T) {
	dir := fixtureDir(t) // app-data starts out empty
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	selectRow(r, 1) // app-data

	r.openBatchRename()

	if r.activePage != errorPage {
		t.Fatalf("activePage = %q, want the error overlay", r.activePage)
	}
	if got := strings.ReplaceAll(r.errorView.GetText(true), "\n", " "); !strings.Contains(got, "no entries to rename") {
		t.Errorf("error text = %q, want it to mention the empty folder", got)
	}
}

// TestOpenBatchRenameOnMultipleFoldersLeavesThemAsTargets pins that
// expansion only applies to a *lone* directory target: two or more
// folders checked at once are still legitimate Batch Rename targets in
// their own right (renaming several folders' own names in one pass),
// so they're left untouched.
func TestOpenBatchRenameOnMultipleFoldersLeavesThemAsTargets(t *testing.T) {
	dir := fixtureDir(t)
	if err := os.Mkdir(filepath.Join(dir, "banana-data"), 0o755); err != nil {
		t.Fatal(err)
	}
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	if _, err := r.panel.selectByPattern("*-data", true); err != nil {
		t.Fatalf("selectByPattern: %v", err)
	}

	r.openBatchRename()

	want := []string{filepath.Join(dir, "app-data"), filepath.Join(dir, "banana-data")}
	if len(r.batchRenameTargets) != len(want) {
		t.Fatalf("targets = %v, want the two folders themselves %v", r.batchRenameTargets, want)
	}
}

// TestOpenBatchRenameWithCursorOnDotDotExpandsCurrentDirectory pins that
// this fix composes with CurrentRowPath's own ".." handling (the cursor
// on ".." reports the panel's current directory, not its parent — see
// feedback_list.txt): running Batch Rename from ".." renames the
// current directory's own contents, not the directory itself.
func TestOpenBatchRenameWithCursorOnDotDotExpandsCurrentDirectory(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	selectRow(r, 0) // ".."

	r.openBatchRename()

	want := []string{
		filepath.Join(dir, "app-data"),
		filepath.Join(dir, "apple.txt"),
		filepath.Join(dir, "apricot.txt"),
		filepath.Join(dir, "banana.txt"),
	}
	if len(r.batchRenameTargets) != len(want) {
		t.Fatalf("targets = %v, want the current directory's own contents %v", r.batchRenameTargets, want)
	}
}

// TestOpenBatchRenameOnAFolderRespectsShowHidden pins that expanding a
// lone folder shows exactly what the panel itself shows: toggling
// hidden files off excludes a dotfile from the expanded targets too.
func TestOpenBatchRenameOnAFolderRespectsShowHidden(t *testing.T) {
	dir := fixtureDir(t)
	if err := os.WriteFile(filepath.Join(dir, "app-data", ".secret"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "app-data", "inner.txt"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.panel.showHidden = false
	selectRow(r, 1) // app-data

	r.openBatchRename()

	want := []string{filepath.Join(dir, "app-data", "inner.txt")}
	if len(r.batchRenameTargets) != len(want) || r.batchRenameTargets[0] != want[0] {
		t.Fatalf("targets = %v, want only the visible entry %v", r.batchRenameTargets, want)
	}
}
