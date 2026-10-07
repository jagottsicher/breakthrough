package ui

import (
	"fmt"
	"path/filepath"
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/jagottsicher/breakthrough/internal/config"
)

// TestChordColorLabelOpensPicker pins the "zl" chord's own dispatch:
// both keys resolve to openLabelMenu, which shows r.picker.
func TestChordColorLabelOpensPicker(t *testing.T) {
	root := newPlainKeyRoot(t)

	root.HandlePlainKey(runeEvent('z'))
	root.HandlePlainKey(runeEvent('l'))

	if root.activePage != pickerPage {
		t.Errorf("activePage = %q, want the picker overlay", root.activePage)
	}
	if got, want := root.picker.GetItemCount(), config.MaxLabelID+1; got != want {
		t.Errorf("picker item count = %d, want %d (0..9)", got, want)
	}
}

// TestLabelMenuRowsUsesSettingsNames pins that the picker's own rows
// read live from settings — a renamed label shows its new name, not a
// hardcoded "Label N".
func TestLabelMenuRowsUsesSettingsNames(t *testing.T) {
	theme := config.DefaultTheme().Resolve()
	settings := config.DefaultSettings()
	settings.Label3Name = "Urgent"

	rows := labelMenuRows(theme, settings)
	if len(rows) != config.MaxLabelID+1 {
		t.Fatalf("len(rows) = %d, want %d", len(rows), config.MaxLabelID+1)
	}
	if rows[0].id != 0 || rows[0].text != " 0   no label" {
		t.Errorf("rows[0] = %+v, want id 0 \" 0   no label\"", rows[0])
	}
	wantSwatch := fmt.Sprintf("[:%s:] 3 [-:-:-]", colorTag(theme.LabelBackground(3)))
	want3 := wantSwatch + " Urgent"
	if rows[3].id != 3 || rows[3].text != want3 {
		t.Errorf("rows[3] = %+v, want id 3 %q", rows[3], want3)
	}
}

// TestOpenLabelMenuPickingARowSetsTheLabel pins the end-to-end path:
// opening the picker and selecting row id 5 actually sets label 5 on
// the focused row's own path.
func TestOpenLabelMenuPickingARowSetsTheLabel(t *testing.T) {
	root := newPlainKeyRoot(t)
	root.panel.focusRow(1) // apple.txt (row 0 is "..")
	_, path, ok := root.panel.CurrentRowPath()
	if !ok {
		t.Fatal("setup: no current row")
	}

	root.openLabelMenu()
	if root.activePage != pickerPage {
		t.Fatalf("activePage = %q, want the picker overlay", root.activePage)
	}
	root.picker.SetCurrentItem(5) // row index 5 = label id 5 (see labelMenuRows)
	root.picker.InputHandler()(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone), func(tview.Primitive) {})

	if got := root.labels.Get(path); got != 5 {
		t.Errorf("Get(%q) = %d, want 5", path, got)
	}
}

// TestOpenLabelMenuPickingNoLabelClears pins that picking row 0 ("no
// label") clears an existing assignment.
func TestOpenLabelMenuPickingNoLabelClears(t *testing.T) {
	root := newPlainKeyRoot(t)
	root.panel.focusRow(1)
	_, path, ok := root.panel.CurrentRowPath()
	if !ok {
		t.Fatal("setup: no current row")
	}
	if err := root.labels.Set(path, 8); err != nil {
		t.Fatalf("Set: %v", err)
	}

	root.openLabelMenu()
	root.picker.SetCurrentItem(0) // "0  no label"
	root.picker.InputHandler()(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone), func(tview.Primitive) {})

	if got := root.labels.Get(path); got != 0 {
		t.Errorf("Get(%q) = %d, want 0 (cleared)", path, got)
	}
}

// TestOpenLabelMenuAppliesToEveryCheckedRow pins the "acts on the
// checked selection if there is one" convention every other chord
// action already follows (see selectedOrCurrentPaths).
func TestOpenLabelMenuAppliesToEveryCheckedRow(t *testing.T) {
	root := newPlainKeyRoot(t)
	dir := root.panel.path
	apple := filepath.Join(dir, "apple.txt")
	apricot := filepath.Join(dir, "apricot.txt")
	var appleRow int
	for _, path := range []string{apple, apricot} {
		row, ok := rowForPath(root.panel, path)
		if !ok {
			t.Fatalf("setup: row for %q not found", path)
		}
		root.panel.setChecked(row, true)
		if path == apple {
			appleRow = row
		}
	}
	root.panel.focusRow(appleRow) // cursor on one of the checked rows — see labelTargets' own doc comment

	root.openLabelMenu()
	root.picker.SetCurrentItem(1) // label id 1
	root.picker.InputHandler()(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone), func(tview.Primitive) {})

	if got := root.labels.Get(apple); got != 1 {
		t.Errorf("Get(apple) = %d, want 1", got)
	}
	if got := root.labels.Get(apricot); got != 1 {
		t.Errorf("Get(apricot) = %d, want 1", got)
	}
}

// TestOpenLabelMenuTargetOutsideSelectionLabelsOnlyTarget pins the
// user's own explicit, live-tested correction: with several rows
// already checked from an earlier, unrelated action, labeling a
// *different*, unchecked row must label only that one row — never the
// checked selection it has nothing to do with (see labelTargets' own
// doc comment).
func TestOpenLabelMenuTargetOutsideSelectionLabelsOnlyTarget(t *testing.T) {
	root := newPlainKeyRoot(t)
	dir := root.panel.path
	apple := filepath.Join(dir, "apple.txt")
	apricot := filepath.Join(dir, "apricot.txt")
	banana := filepath.Join(dir, "banana.txt")

	for _, path := range []string{apple, apricot} {
		row, ok := rowForPath(root.panel, path)
		if !ok {
			t.Fatalf("setup: row for %q not found", path)
		}
		root.panel.setChecked(row, true)
	}
	bananaRow, ok := rowForPath(root.panel, banana)
	if !ok {
		t.Fatal("setup: row for banana.txt not found")
	}
	root.panel.focusRow(bananaRow) // cursor on the unchecked row

	root.openLabelMenu()
	root.picker.SetCurrentItem(2) // label id 2
	root.picker.InputHandler()(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone), func(tview.Primitive) {})

	if got := root.labels.Get(banana); got != 2 {
		t.Errorf("Get(banana) = %d, want 2", got)
	}
	if got := root.labels.Get(apple); got != 0 {
		t.Errorf("Get(apple) = %d, want 0 — must stay untouched, it was never the target", got)
	}
	if got := root.labels.Get(apricot); got != 0 {
		t.Errorf("Get(apricot) = %d, want 0 — must stay untouched, it was never the target", got)
	}
}

// TestOpenLabelMenuExcludedWhileRemote pins that the chord reports the
// same remote-not-supported error every other mutating action does,
// rather than silently doing nothing or opening the picker anyway.
func TestOpenLabelMenuExcludedWhileRemote(t *testing.T) {
	root := newPlainKeyRoot(t)
	root.panel.remote = newTestFakeRemote("/remote")

	root.openLabelMenu()

	if root.activePage != errorPage {
		t.Errorf("activePage = %q, want the error overlay", root.activePage)
	}
}

// TestOpenLabelMenuShowsAllTenRowsWithoutScrolling pins the live-tested
// fix: the picker's own rect must be tall enough for every one of the
// ten rows, never capped the way the much longer owner/group picker's
// own pickerHeight is.
func TestOpenLabelMenuShowsAllTenRowsWithoutScrolling(t *testing.T) {
	root := newPlainKeyRoot(t)
	root.SetRect(0, 0, 100, 40)

	root.openLabelMenu()

	_, _, _, height := root.picker.GetRect()
	if height != config.MaxLabelID+1 {
		t.Errorf("picker height = %d, want %d (every row visible, no scrolling)", height, config.MaxLabelID+1)
	}
}

// TestPickerOpenYTopHalfOpensDownward pins the ordinary, unchanged
// case: a row in the screen's own top half opens exactly where
// menuAnchorForCurrentRow already puts it, growing downward.
func TestPickerOpenYTopHalfOpensDownward(t *testing.T) {
	root := newPlainKeyRoot(t)
	root.SetRect(0, 0, 100, 40)

	belowY := 5 // row itself at y=4, well within the top half of a 40-row screen
	if got := root.pickerOpenY(belowY, 10); got != belowY {
		t.Errorf("pickerOpenY(%d, 10) = %d, want %d (open downward, unchanged)", belowY, got, belowY)
	}
}

// TestPickerOpenYBottomHalfOpensUpward pins the user's own explicit
// request: a row in the screen's own bottom half opens upward instead,
// its own bottom edge landing at the row's own top — so a tall picker
// never needs the terminal to scroll to show every row.
func TestPickerOpenYBottomHalfOpensUpward(t *testing.T) {
	root := newPlainKeyRoot(t)
	root.SetRect(0, 0, 100, 40)

	belowY := 35 // row itself at y=34, in the bottom half of a 40-row screen
	height := 10
	want := 34 - height // the row's own top edge, minus the picker's own height
	if got := root.pickerOpenY(belowY, height); got != want {
		t.Errorf("pickerOpenY(%d, %d) = %d, want %d (open upward)", belowY, height, got, want)
	}
}
