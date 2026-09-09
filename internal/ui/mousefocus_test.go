package ui

import (
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// clickAt simulates one genuine mouse click at (x, y) — MouseLeftDown
// followed by MouseLeftClick, both dispatched through root's own real
// MouseHandler chain, the same two events (and only those two — see
// captureMouse's own doc comment on why a plain click never sends
// MouseLeftUp) a real terminal click produces. Every test in this file
// exists specifically because sending only MouseLeftClick (as some
// older tests in this package still do) never exercises the bug class
// here at all: it's MouseLeftDown that tview's own default handlers use
// to steal focus, and a test that never sends it can't catch a capture
// that fails to suppress it.
//
// The setFocus callback passed down to each handler must actually call
// through to Application.SetFocus, not a no-op — a first version of
// this helper used one, which meant every MouseLeftDown's own
// setFocus(t) call (whichever widget's default handler makes it) did
// nothing at all, regardless of whether the capture under test
// correctly suppressed it — every test below passed unconditionally,
// fix or no fix, until this was caught by deliberately reverting each
// fix in turn and finding the tests stayed green (this project's own
// standing rule for a new regression test, not skipped here just
// because the mistake was in the test itself rather than the
// production code).
func clickAt(root *Root, x, y int) {
	handler := root.MouseHandler()
	setFocus := func(p tview.Primitive) { root.app.SetFocus(p) }
	handler(tview.MouseLeftDown, tcell.NewEventMouse(x, y, tcell.Button1, 0), setFocus)
	handler(tview.MouseLeftClick, tcell.NewEventMouse(x, y, tcell.Button1, 0), setFocus)
}

// drawnRootTwice is drawnRoot's own shape, but keeps the screen around
// for a second Draw() call and returns it instead of just a cleanup
// func — needed by every test below that reads a cell's own
// GetLastPosition() (columnHeader's cells, detailsTitleBar's rect): a
// tview.Flex only draws the currently-*focused* item last (deferred,
// verified directly against its own flex.go — draws every other item
// immediately, defers the focused one via defer item.Item.Draw(screen)
// so it paints on top), and p.table's own SetDrawFunc callback — which
// rebuilds columnHeader's cells via relayoutColumns/buildColumnHeader —
// runs *inside* that deferred call, since p.table itself has focus by
// default here. So columnHeader draws first, using whatever cells
// existed before this frame's rebuild; the freshly rebuilt ones (with
// their own zeroed, never-drawn GetLastPosition) replace them in
// content but are never actually painted until a second frame runs. A
// second real Draw() call is what settles them — the same
// "Flex.SetRect alone never cascades a rect to a child, only Draw()
// does" class of gap this project has already run into more than once
// with nested overlays, just via a different mechanism.
func drawnRootTwice(t *testing.T, dir string) (root *Root, screen tcell.SimulationScreen) {
	t.Helper()

	root, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}

	screen = tcell.NewSimulationScreen("")
	if err := screen.Init(); err != nil {
		t.Fatalf("screen.Init: %v", err)
	}
	t.Cleanup(screen.Fini)
	screen.SetSize(80, 24)

	root.SetRect(0, 0, 80, 24)
	root.Draw(screen)
	root.Draw(screen)

	return root, screen
}

// TestClickingColumnHeaderKeepsTableFocused pins the user's own
// explicit report: clicking Name/Size/mtime to sort silently stole real
// keyboard focus onto columnHeader itself (a bare, non-navigable
// tview.Table with no mouse capture of its own to suppress tview's
// default MouseLeftDown->setFocus behavior — see
// captureColumnHeaderMouse's own doc comment), leaving c/x/v/d and
// every other plain-key shortcut dead afterward even though the sort
// itself visibly worked.
func TestClickingColumnHeaderKeepsTableFocused(t *testing.T) {
	dir := fixtureDir(t)
	root, screen := drawnRootTwice(t, dir)

	root.app.SetFocus(root.panel.table)
	root.Draw(screen)
	if !root.panel.table.HasFocus() {
		t.Fatal("setup: table should have focus before the click")
	}

	nameCell := root.panel.columnHeader.GetCell(0, colName)
	x, y, w := nameCell.GetLastPosition()
	if w == 0 {
		t.Fatal("setup: Name header cell has no position — was columnHeader actually drawn?")
	}

	clickAt(root, x, y)

	if root.panel.sortKey != sortByName {
		t.Error("clicking the Name header cell should still sort by name")
	}
	if !root.panel.table.HasFocus() {
		t.Error("clicking the Name header cell stole keyboard focus away from the table")
	}
}

// TestClickingSelectAllCheckboxKeepsTableFocused is the same pin for
// columnHeader's other clickable cell — the header's own select-all
// checkbox (see toggleSelectAllViaHeader) — not just the three sort
// columns.
func TestClickingSelectAllCheckboxKeepsTableFocused(t *testing.T) {
	dir := fixtureDir(t)
	root, screen := drawnRootTwice(t, dir)

	root.app.SetFocus(root.panel.table)
	root.Draw(screen)

	checkboxCell := root.panel.columnHeader.GetCell(0, colCheckbox)
	x, y, w := checkboxCell.GetLastPosition()
	if w == 0 {
		t.Fatal("setup: checkbox header cell has no position — was columnHeader actually drawn?")
	}

	clickAt(root, x, y)

	if !root.panel.table.HasFocus() {
		t.Error("clicking the select-all checkbox stole keyboard focus away from the table")
	}
}

// TestClickingButtonBarKeepsTableFocused pins the same class of bug for
// the bottom button bar: captureButtonBarMouse used to fold its InRect
// check into the same condition as its action-type gate
// ("action != MouseLeftClick || !InRect"), which let a MouseLeftDown
// landing inside the bar's own rect fall through unsuppressed to its
// default TextView.MouseHandler and steal focus, exactly the same way
// columnHeader's own missing capture did.
func TestClickingButtonBarKeepsTableFocused(t *testing.T) {
	dir := fixtureDir(t)
	root, screen := drawnRootTwice(t, dir)

	root.app.SetFocus(root.panel.table)
	root.Draw(screen)

	x, y, w, h := root.buttonBar.GetRect()
	if w == 0 || h == 0 {
		t.Fatal("setup: buttonBar has no rect — was root actually drawn?")
	}

	clickAt(root, x, y)

	if !root.panel.table.HasFocus() {
		t.Error("clicking the button bar stole keyboard focus away from the table")
	}
}

// TestClosingDetailsSidebarByMouseRestoresFocusToTable pins a subtler
// variant of the same bug: captureDetailsTitleBarMouse used to check
// its action-type gate before InRect, so a MouseLeftDown landing on the
// title bar (even off its own close button) fell through unsuppressed
// and stole focus onto detailsTitleBar itself — a *different* widget
// than detailsSidebar. hideDetailsSidebar's own "was the sidebar
// focused a moment ago" check reads detailsSidebar.HasFocus()
// specifically, so that stray focus steal made it read false right
// when it should have read true, skipping the focus restore to the
// table it was about to need — the sidebar closes, but focus is left
// stranded on now-hidden chrome instead of going back to the table.
func TestClosingDetailsSidebarByMouseRestoresFocusToTable(t *testing.T) {
	dir := fixtureDir(t)
	root, screen := drawnRootTwice(t, dir)

	root.showDetailsSidebar()
	root.app.SetFocus(root.detailsSidebar)
	root.Draw(screen)
	if !root.detailsSidebar.HasFocus() {
		t.Fatal("setup: detailsSidebar should have focus before the click")
	}

	x, y, width, _ := root.detailsTitleBar.GetRect()
	if width == 0 {
		t.Fatal("setup: detailsTitleBar has no rect — was the sidebar actually drawn?")
	}
	closeX := x + toolWindowCloseButtonCol(0, width)

	clickAt(root, closeX, y)

	if root.detailsSidebarVisible {
		t.Fatal("clicking the sidebar's own close button should have hidden it")
	}
	if !root.panel.table.HasFocus() {
		t.Error("closing the Details sidebar by mouse left focus stranded off the table")
	}
}
