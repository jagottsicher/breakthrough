package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/jagottsicher/breakthrough/internal/config"
)

// TestCheckboxClickTogglesDisplay reproduces an actual mouse click on the
// checkbox column through Table's own MouseHandler — the real path a click
// takes — rather than calling toggleCheckbox directly (TestToggleCheckbox
// already covers that). A tcell.SimulationScreen stands in for a real
// terminal so Table.CellAt has real layout to resolve coordinates against
// (per its own doc, it needs the table to have actually been drawn).
func TestCheckboxClickTogglesDisplay(t *testing.T) {
	dir := fixtureDir(t)
	p, err := NewPanel(tview.NewApplication(), dir, config.DefaultTheme().Resolve(), config.DefaultSettings())
	if err != nil {
		t.Fatalf("NewPanel: %v", err)
	}

	screen := tcell.NewSimulationScreen("")
	if err := screen.Init(); err != nil {
		t.Fatalf("screen.Init: %v", err)
	}
	defer screen.Fini()
	screen.SetSize(80, 24)

	p.table.SetRect(0, 0, 80, 24)
	p.table.Draw(screen)

	// Row 1 is "app-data" (checkable, per fixtureDir/TestPanelLoadPopulatesTable).
	// Scan the drawn row for the screen column that maps back to its
	// checkbox cell, rather than assuming one.
	const row = 1
	x := -1
	for tryX := 0; tryX < 80; tryX++ {
		if r, c := p.table.CellAt(tryX, row); r == row && c == colCheckbox {
			x = tryX
			break
		}
	}
	if x < 0 {
		t.Fatal("could not locate the checkbox cell's screen position")
	}

	ref, ok := p.rowRef(row)
	if !ok {
		t.Fatal("row 1: no rowRef")
	}
	if p.selected[ref.path] {
		t.Fatal("setup: entry should not be selected yet")
	}

	handler := p.table.MouseHandler()
	handler(tview.MouseLeftClick, tcell.NewEventMouse(x, row, tcell.Button1, 0), func(tview.Primitive) {})

	if !p.selected[ref.path] {
		t.Error("clicking the checkbox cell did not select the entry")
	}
	if cell := p.table.GetCell(row, colCheckbox); cell.Text != checkboxText(true) {
		t.Errorf("checkbox cell text = %q after click, want %q", cell.Text, checkboxText(true))
	}
}

// TestCheckboxClickThroughRoot is the same click, but dispatched through
// Root's actual mouse handler the way a real click arrives — down through
// Root's own captureOutsideClick, then Panel's Flex (wrapping
// Root.captureMouse), then finally the table — rather than calling
// Table.MouseHandler directly as TestCheckboxClickTogglesDisplay does.
// Since that more isolated test already passes, this is what's left to
// check: something in the layers above the table swallowing the click.
func TestCheckboxClickThroughRoot(t *testing.T) {
	dir := fixtureDir(t)
	app := tview.NewApplication()
	root, err := NewRoot(app, dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}

	screen := tcell.NewSimulationScreen("")
	if err := screen.Init(); err != nil {
		t.Fatalf("screen.Init: %v", err)
	}
	defer screen.Fini()
	screen.SetSize(80, 24)

	root.SetRect(0, 0, 80, 24)
	root.Draw(screen) // cascades layout down through Panel's Flex to the table

	// Unlike TestCheckboxClickTogglesDisplay (which set the table's rect
	// directly), row 1's screen y here isn't just 1: Panel's Flex puts a
	// 1-row header above the table, so everything below is offset by
	// however many screen rows that header actually took up. Search both
	// axes instead of assuming either.
	const row = 1
	x, y := -1, -1
	for tryY := 0; tryY < 24 && x < 0; tryY++ {
		for tryX := 0; tryX < 80; tryX++ {
			if r, c := root.panel.table.CellAt(tryX, tryY); r == row && c == colCheckbox {
				x, y = tryX, tryY
				break
			}
		}
	}
	if x < 0 {
		t.Fatal("could not locate the checkbox cell's screen position")
	}

	ref, ok := root.panel.rowRef(row)
	if !ok {
		t.Fatal("row 1: no rowRef")
	}
	if root.panel.selected[ref.path] {
		t.Fatal("setup: entry should not be selected yet")
	}

	handler := root.MouseHandler()
	handler(tview.MouseLeftClick, tcell.NewEventMouse(x, y, tcell.Button1, 0), func(tview.Primitive) {})

	if !root.panel.selected[ref.path] {
		t.Error("clicking the checkbox cell through Root did not select the entry")
	}
	if cell := root.panel.table.GetCell(row, colCheckbox); cell.Text != checkboxText(true) {
		t.Errorf("checkbox cell text = %q after click, want %q", cell.Text, checkboxText(true))
	}
}

// findRowPos scans a drawn screen for a position CellAt resolves back to
// row row (any column), the setup step every test below needs.
func findRowPos(t *tview.Table, row, width, height int) (x, y int, ok bool) {
	for tryY := 0; tryY < height; tryY++ {
		for tryX := 0; tryX < width; tryX++ {
			if r, _ := t.CellAt(tryX, tryY); r == row {
				return tryX, tryY, true
			}
		}
	}
	return 0, 0, false
}

// drawnRoot builds a Root over dir and draws it into a same-sized
// SimulationScreen, so its table's CellAt has real layout to resolve
// coordinates against. The caller must call the returned cleanup func
// (defer it) to release the screen.
func drawnRoot(t *testing.T, dir string) (root *Root, cleanup func()) {
	t.Helper()

	root, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}

	screen := tcell.NewSimulationScreen("")
	if err := screen.Init(); err != nil {
		t.Fatalf("screen.Init: %v", err)
	}
	screen.SetSize(80, 24)

	root.SetRect(0, 0, 80, 24)
	root.Draw(screen)

	return root, screen.Fini
}

// dragRight performs a right-button drag from row from to row to — the
// same two actions (MouseRightDown then MouseRightUp) and nothing else, a
// genuine drag produces; see captureMouse's doc comment for why no
// MouseRightClick is involved.
func dragRight(t *testing.T, root *Root, from, to int) {
	t.Helper()

	fromX, fromY, ok := findRowPos(root.panel.table, from, 80, 24)
	if !ok {
		t.Fatalf("could not locate row %d's screen position", from)
	}
	toX, toY, ok := findRowPos(root.panel.table, to, 80, 24)
	if !ok {
		t.Fatalf("could not locate row %d's screen position", to)
	}

	handler := root.MouseHandler()
	handler(tview.MouseRightDown, tcell.NewEventMouse(fromX, fromY, tcell.ButtonSecondary, 0), func(tview.Primitive) {})
	handler(tview.MouseRightUp, tcell.NewEventMouse(toX, toY, tcell.ButtonNone, 0), func(tview.Primitive) {})
}

// TestRightDragLiveFocusFollowsMouseMove reproduces what a real drag
// actually generates, not just the down/up pair dragRight uses: a
// MouseRightDown, then a stream of MouseMove events as the mouse crosses
// rows with the button still held (tview only fires MouseRightDown/Up
// again once the button state itself changes — see
// Application.fireMouseActions — so a real terminal reports these as
// plain moves throughout the drag), then MouseRightUp. Checks that the
// highlight tracks the row under the mouse *while still dragging*, not
// only once released — and that nothing gets toggled until release.
func TestRightDragLiveFocusFollowsMouseMove(t *testing.T) {
	dir := fixtureDir(t)
	root, cleanup := drawnRoot(t, dir)
	defer cleanup()

	checked := func(row int) bool {
		t.Helper()
		ref, ok := root.panel.rowRef(row)
		if !ok {
			t.Fatalf("row %d: no rowRef", row)
		}
		return root.panel.selected[ref.path]
	}

	downX, downY, ok := findRowPos(root.panel.table, 1, 80, 24)
	if !ok {
		t.Fatal("could not locate row 1's screen position")
	}
	midX, midY, ok := findRowPos(root.panel.table, 2, 80, 24)
	if !ok {
		t.Fatal("could not locate row 2's screen position")
	}
	upX, upY, ok := findRowPos(root.panel.table, 3, 80, 24)
	if !ok {
		t.Fatal("could not locate row 3's screen position")
	}

	handler := root.MouseHandler()
	handler(tview.MouseRightDown, tcell.NewEventMouse(downX, downY, tcell.ButtonSecondary, 0), func(tview.Primitive) {})
	if got, _ := root.panel.table.GetSelection(); got != 1 {
		t.Errorf("focus after MouseRightDown = %d, want 1", got)
	}
	// Nothing toggles on press alone — only once the drag actually moves
	// (see advanceDrag), so a plain click still toggles nothing.
	if checked(1) {
		t.Error("row 1 should not be checked yet, right after MouseRightDown")
	}

	handler(tview.MouseMove, tcell.NewEventMouse(midX, midY, tcell.ButtonSecondary, 0), func(tview.Primitive) {})
	if got, _ := root.panel.table.GetSelection(); got != 2 {
		t.Errorf("focus after MouseMove to row 2 (still dragging) = %d, want 2 (live tracking)", got)
	}
	// The drag has now genuinely moved: rows 1 and 2 should already be
	// checked live, before the button is ever released.
	if !checked(1) || !checked(2) {
		t.Errorf("rows 1 and 2 should already be checked mid-drag: row1=%v row2=%v", checked(1), checked(2))
	}
	if checked(3) || checked(4) {
		t.Error("rows the drag hasn't reached yet should not be checked")
	}

	handler(tview.MouseRightUp, tcell.NewEventMouse(upX, upY, tcell.ButtonNone, 0), func(tview.Primitive) {})
	if got, _ := root.panel.table.GetSelection(); got != 3 {
		t.Errorf("focus after release = %d, want 3", got)
	}
	for _, row := range []int{1, 2, 3} {
		if !checked(row) {
			t.Errorf("row %d should be checked after release", row)
		}
	}
	if checked(4) {
		t.Error("row 4 (never reached by the drag) should not be checked")
	}
}

// TestRightDragSelectsRange simulates a right-button drag from row 1
// through row 3 and checks that exactly those rows end up selected, the
// context menu never opens, and the drag state is left clean for the
// next gesture.
func TestRightDragSelectsRange(t *testing.T) {
	dir := fixtureDir(t) // rows: "..", app-data, apple.txt, apricot.txt, banana.txt
	root, cleanup := drawnRoot(t, dir)
	defer cleanup()

	dragRight(t, root, 1, 3)

	wantSelected := map[int]bool{1: true, 2: true, 3: true, 4: false}
	for row, want := range wantSelected {
		ref, ok := root.panel.rowRef(row)
		if !ok {
			t.Fatalf("row %d: no rowRef", row)
		}
		if got := root.panel.selected[ref.path]; got != want {
			t.Errorf("row %d (%s): selected = %v, want %v", row, ref.name, got, want)
		}
	}

	if root.activePage != "" {
		t.Errorf("context menu should not open on a drag, activePage = %q", root.activePage)
	}
	if root.dragging {
		t.Error("dragging should be cleared after MouseRightUp")
	}
}

// TestRightDragAgainDeselects repeats the same drag over an
// already-selected range and checks that it clears the selection instead
// of being a no-op: every row in the range toggles its own state, so
// dragging the same uniformly-checked range again flips every one of
// them back off.
func TestRightDragAgainDeselects(t *testing.T) {
	dir := fixtureDir(t)
	root, cleanup := drawnRoot(t, dir)
	defer cleanup()

	dragRight(t, root, 1, 3)
	for _, row := range []int{1, 2, 3} {
		ref, ok := root.panel.rowRef(row)
		if !ok {
			t.Fatalf("row %d: no rowRef", row)
		}
		if !root.panel.selected[ref.path] {
			t.Fatalf("setup: row %d should be selected after the first drag", row)
		}
	}

	dragRight(t, root, 1, 3) // same range again

	for _, row := range []int{1, 2, 3} {
		ref, _ := root.panel.rowRef(row)
		if root.panel.selected[ref.path] {
			t.Errorf("row %d should be unselected after dragging the same range again", row)
		}
		if cell := root.panel.table.GetCell(row, colCheckbox); cell.Text != checkboxText(false) {
			t.Errorf("row %d checkbox cell = %q, want %q", row, cell.Text, checkboxText(false))
		}
	}
}

// TestRightDragTogglesEachRowIndependently drags over a range that starts
// out mixed — row 1 already checked, rows 2 and 3 not — and checks that
// each row flips its own state rather than the whole range being forced
// to match (or invert) the press row's state.
func TestRightDragTogglesEachRowIndependently(t *testing.T) {
	dir := fixtureDir(t)
	root, cleanup := drawnRoot(t, dir)
	defer cleanup()

	root.panel.toggleCheckbox(1)
	if ref, _ := root.panel.rowRef(1); !root.panel.selected[ref.path] {
		t.Fatal("setup: row 1 should be checked")
	}

	dragRight(t, root, 1, 3)

	want := map[int]bool{1: false, 2: true, 3: true}
	for row, wantChecked := range want {
		ref, ok := root.panel.rowRef(row)
		if !ok {
			t.Fatalf("row %d: no rowRef", row)
		}
		if got := root.panel.selected[ref.path]; got != wantChecked {
			t.Errorf("row %d: selected = %v, want %v", row, got, wantChecked)
		}
	}
}

// TestRightDragReversalUntogglesRowsLeftBehind drags 1 -> 3 -> 2, live,
// via MouseMove — checking applyDragDelta's actual point: row 3 was
// briefly inside the dragged range and got checked, then the drag pulled
// back and left it behind, so it should end up unchecked again, while
// rows 1 and 2 (never left behind) stay checked.
func TestRightDragReversalUntogglesRowsLeftBehind(t *testing.T) {
	dir := fixtureDir(t)
	root, cleanup := drawnRoot(t, dir)
	defer cleanup()

	downX, downY, _ := findRowPos(root.panel.table, 1, 80, 24)
	farX, farY, _ := findRowPos(root.panel.table, 3, 80, 24)
	backX, backY, _ := findRowPos(root.panel.table, 2, 80, 24)

	handler := root.MouseHandler()
	handler(tview.MouseRightDown, tcell.NewEventMouse(downX, downY, tcell.ButtonSecondary, 0), func(tview.Primitive) {})
	handler(tview.MouseMove, tcell.NewEventMouse(farX, farY, tcell.ButtonSecondary, 0), func(tview.Primitive) {})

	ref3, _ := root.panel.rowRef(3)
	if !root.panel.selected[ref3.path] {
		t.Fatal("setup: row 3 should be checked after reaching it")
	}

	handler(tview.MouseMove, tcell.NewEventMouse(backX, backY, tcell.ButtonSecondary, 0), func(tview.Primitive) {})
	handler(tview.MouseRightUp, tcell.NewEventMouse(backX, backY, tcell.ButtonNone, 0), func(tview.Primitive) {})

	want := map[int]bool{1: true, 2: true, 3: false}
	for row, wantChecked := range want {
		ref, ok := root.panel.rowRef(row)
		if !ok {
			t.Fatalf("row %d: no rowRef", row)
		}
		if got := root.panel.selected[ref.path]; got != wantChecked {
			t.Errorf("row %d: selected = %v, want %v", row, got, wantChecked)
		}
	}
}

// TestRightClickWithoutDragOpensMenu is the control case for
// TestRightDragSelectsRange: a right press and release at the *same* row
// must still behave like a plain right-click (open the menu, no range
// selection) — down and up landing on the same row is exactly what a
// real, non-dragged click looks like at this level.
func TestRightClickWithoutDragOpensMenu(t *testing.T) {
	dir := fixtureDir(t)
	root, cleanup := drawnRoot(t, dir)
	defer cleanup()

	x, y, ok := findRowPos(root.panel.table, 1, 80, 24)
	if !ok {
		t.Fatal("could not locate row 1's screen position")
	}

	handler := root.MouseHandler()
	handler(tview.MouseRightDown, tcell.NewEventMouse(x, y, tcell.ButtonSecondary, 0), func(tview.Primitive) {})
	handler(tview.MouseRightUp, tcell.NewEventMouse(x, y, tcell.ButtonNone, 0), func(tview.Primitive) {})
	// Same-position release: real tview would now synthesize
	// MouseRightClick itself (see Application.fireMouseActions); this
	// test drives the dispatcher directly, so it fires the one that
	// matters here explicitly.
	handler(tview.MouseRightClick, tcell.NewEventMouse(x, y, tcell.ButtonNone, 0), func(tview.Primitive) {})

	if root.activePage != contextMenuPage {
		t.Errorf("activePage = %q, want %q (context menu should open)", root.activePage, contextMenuPage)
	}

	ref, ok := root.panel.rowRef(1)
	if !ok {
		t.Fatal("row 1: no rowRef")
	}
	if root.panel.selected[ref.path] {
		t.Error("a plain right-click must not select the row")
	}
}

// TestRightClickMovesFocus checks that right-clicking a row that isn't
// already the table's highlighted one moves the highlight there — the
// context menu is clearly about that row, so the highlight should agree,
// not point wherever a previous left-click or arrow key left it.
func TestRightClickMovesFocus(t *testing.T) {
	dir := fixtureDir(t)
	root, cleanup := drawnRoot(t, dir)
	defer cleanup()

	// Row 0 is the table's default focus; right-click a different row.
	const row = 3
	x, y, ok := findRowPos(root.panel.table, row, 80, 24)
	if !ok {
		t.Fatalf("could not locate row %d's screen position", row)
	}
	if got, _ := root.panel.table.GetSelection(); got == row {
		t.Fatalf("setup: row %d should not already be focused", row)
	}

	handler := root.MouseHandler()
	handler(tview.MouseRightDown, tcell.NewEventMouse(x, y, tcell.ButtonSecondary, 0), func(tview.Primitive) {})
	handler(tview.MouseRightUp, tcell.NewEventMouse(x, y, tcell.ButtonNone, 0), func(tview.Primitive) {})
	handler(tview.MouseRightClick, tcell.NewEventMouse(x, y, tcell.ButtonNone, 0), func(tview.Primitive) {})

	if got, _ := root.panel.table.GetSelection(); got != row {
		t.Errorf("focused row = %d after right-clicking row %d, want %d", got, row, row)
	}
}

// TestRenamePositionsOverRightClickedRow is a regression test for a bug
// where Root.targetRow stored the right-click event's raw screen y
// coordinate instead of the table's own row *index* (see captureMouse's
// MouseRightClick case). openRename indexes the table by row index, not
// screen position (Panel.nameCellRect -> Table.GetCell), so the two only
// ever matched by coincidence — off by the header's 1-line offset even
// with no scrolling at all, and drifting further out of sync with
// whatever the table's scroll offset happened to be after navigating
// around. The rename field would then silently open over some other
// row's name instead of the one actually right-clicked — editable-
// looking, but not editing what the user thought.
//
// Row 3 alone is enough to catch it: its screen y (4, from the header's
// 1 line plus rows 0-3) already differs from its own row index before
// any scrolling is involved.
func TestRenamePositionsOverRightClickedRow(t *testing.T) {
	dir := fixtureDir(t)
	root, cleanup := drawnRoot(t, dir)
	defer cleanup()

	const row = 3
	x, y, ok := findRowPos(root.panel.table, row, 80, 24)
	if !ok {
		t.Fatalf("could not locate row %d's screen position", row)
	}

	handler := root.MouseHandler()
	handler(tview.MouseRightDown, tcell.NewEventMouse(x, y, tcell.ButtonSecondary, 0), func(tview.Primitive) {})
	handler(tview.MouseRightUp, tcell.NewEventMouse(x, y, tcell.ButtonNone, 0), func(tview.Primitive) {})
	handler(tview.MouseRightClick, tcell.NewEventMouse(x, y, tcell.ButtonNone, 0), func(tview.Primitive) {})

	if root.targetRow != row {
		t.Fatalf("targetRow = %d, want %d (the table row index, not the screen y %d)", root.targetRow, row, y)
	}

	root.openRename()

	wantX, wantY, wantWidth, ok := root.panel.nameCellRect(row)
	if !ok {
		t.Fatal("nameCellRect(row) failed")
	}
	gotX, gotY, gotWidth, _ := root.rename.GetRect()
	if gotX != wantX || gotY != wantY || gotWidth != wantWidth {
		t.Errorf("rename field rect = (%d,%d,%d), want (%d,%d,%d) — the right-clicked row's own name cell",
			gotX, gotY, gotWidth, wantX, wantY, wantWidth)
	}

	ref, ok := root.panel.rowRef(row)
	if !ok {
		t.Fatal("row has no rowRef")
	}
	if got := root.rename.GetText(); got != ref.name {
		t.Errorf("rename field pre-filled with %q, want %q (the right-clicked row's own name)", got, ref.name)
	}
}

// TestRightDragMovesFocusToEndRow checks that a right-button drag leaves
// the table's highlight on the row the drag ended on (the release row),
// not the row it started from.
func TestRightDragMovesFocusToEndRow(t *testing.T) {
	dir := fixtureDir(t)
	root, cleanup := drawnRoot(t, dir)
	defer cleanup()

	dragRight(t, root, 1, 3)

	if got, _ := root.panel.table.GetSelection(); got != 3 {
		t.Errorf("focused row = %d after dragging 1 -> 3, want 3", got)
	}
}

// manyEntriesDir returns a directory with n plain files — enough that a
// row index computed from a screen position well past a shrunk panel's
// own bottom edge (see TestRightClickUnderExpandedConsoleDoesNotOpenMenu)
// would still land on a *real* entry if it were validated only against
// the table's own data size, not its current on-screen bounds — the
// exact condition the bug that test pins needs to actually reproduce.
func manyEntriesDir(t *testing.T, n int) string {
	t.Helper()
	dir := t.TempDir()
	for i := 0; i < n; i++ {
		name := fmt.Sprintf("file-%03d.txt", i)
		if err := os.WriteFile(filepath.Join(dir, name), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// TestRightClickUnderExpandedConsoleDoesNotOpenMenu pins the fix for the
// user's own report: a right-click over the expanded bash console must
// not open the panel's own context menu. tview's own Box.WrapMouseHandler
// invokes an installed SetMouseCapture (see Root.captureMouse, installed
// on the panel) for *every* mouse event reaching that primitive's own
// MouseHandler, regardless of position — nothing filters by position
// automatically. Before the fix, Panel.rowIndexAt validated a row only
// against the table's own *data* size (via tview's own Table.CellAt,
// which has no notion of its own current rendered height at all — see
// rowIndexAt's own doc comment): with more entries than fit on screen,
// a click well below the panel's own current bottom edge still
// numerically mapped to a real (if invisible) row, and the menu opened
// for it. manyEntriesDir's 40 entries make sure there really are enough
// for that stale-looking mapping to exist, the same condition that let
// the bug reproduce in the first place.
func TestRightClickUnderExpandedConsoleDoesNotOpenMenu(t *testing.T) {
	dir := manyEntriesDir(t, 40)
	root, cleanup := drawnRoot(t, dir)
	defer cleanup()

	root.app.SetFocus(root.bashLine) // expands bashConsole (see expandBashConsole), shrinking the panel
	screen := tcell.NewSimulationScreen("")
	if err := screen.Init(); err != nil {
		t.Fatalf("screen.Init: %v", err)
	}
	defer screen.Fini()
	screen.SetSize(80, 24)
	root.Draw(screen) // re-cascades layout with the new, shrunk panel height

	// The panel's own rect is the trustworthy source for "is this
	// position actually within it" — unlike Table.CellAt (see this
	// test's own doc comment above), Box.GetRect reflects exactly what
	// the last Draw call actually allocated.
	_, panelY, _, panelHeight := root.panel.GetRect()
	if panelHeight >= 20 {
		t.Fatalf("setup: panel height = %d after focusing bashLine, want it meaningfully shrunk (expandBashConsole isn't doing its job in this test)", panelHeight)
	}
	clickY := panelY + panelHeight + 3 // comfortably inside bashConsole, below the panel's own current bottom edge
	const clickX = 5

	handler := root.MouseHandler()
	handler(tview.MouseRightDown, tcell.NewEventMouse(clickX, clickY, tcell.ButtonSecondary, 0), func(tview.Primitive) {})
	handler(tview.MouseRightUp, tcell.NewEventMouse(clickX, clickY, tcell.ButtonNone, 0), func(tview.Primitive) {})
	handler(tview.MouseRightClick, tcell.NewEventMouse(clickX, clickY, tcell.ButtonNone, 0), func(tview.Primitive) {})

	if root.activePage == contextMenuPage {
		t.Error("right-clicking below the panel's own shrunk bounds (over the expanded console) opened its context menu")
	}
}

// TestQuitConfirmBlocksRightDragSelection pins the fix for the user's
// own direct report ("wenn der quit confirm dialog offen ist, kann man
// immer noch rumklicken oder den overlay öffnen. dann muss wirklich alle
// gesperrt sein"): a right-click drag on the panel used to still work —
// moving focus, toggling checkboxes — even with the quit-confirmation
// overlay open on top of it, since captureOutsideClick only ever
// consumed MouseLeftClick/MouseRightClick and scrolling; MouseRightDown,
// MouseMove, and MouseRightUp — what a genuine drag is actually made of
// (see dragRight's own doc comment, no MouseRightClick involved at all)
// — fell through unconsumed straight to Panel.captureMouse underneath.
//
// The drag must now have no effect whatsoever while the dialog is open:
// nothing selected, focus unmoved, dragging never even set. The dialog
// itself stays open too — a drag produces no Left/RightClick for
// captureOutsideClick to close it on (see its own doc comment on why
// only those two actions do).
func TestQuitConfirmBlocksRightDragSelection(t *testing.T) {
	dir := fixtureDir(t) // rows: "..", app-data, apple.txt, apricot.txt, banana.txt
	root, cleanup := drawnRoot(t, dir)
	defer cleanup()

	if got, _ := root.panel.table.GetSelection(); got != 0 {
		t.Fatalf("setup: focused row = %d, want 0", got)
	}

	root.RequestQuit()
	if root.activePage != quitConfirmPage {
		t.Fatalf("setup: activePage = %q, want %q", root.activePage, quitConfirmPage)
	}

	dragRight(t, root, 1, 3)

	for row := 1; row <= 3; row++ {
		ref, ok := root.panel.rowRef(row)
		if !ok {
			t.Fatalf("row %d: no rowRef", row)
		}
		if root.panel.selected[ref.path] {
			t.Errorf("row %d (%s) got selected by a drag while the quit-confirmation overlay was open", row, ref.name)
		}
	}
	if root.dragging {
		t.Error("dragging should never have been set — MouseRightDown should not have reached Panel.captureMouse at all")
	}
	if got, _ := root.panel.table.GetSelection(); got != 0 {
		t.Errorf("focused row = %d after the drag, want still 0 (unmoved)", got)
	}
	if root.activePage != quitConfirmPage {
		t.Errorf("activePage = %q after the drag, want still %q (a drag produces no click to close it on)", root.activePage, quitConfirmPage)
	}
}
