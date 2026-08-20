package ui

import (
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// TestCheckboxClickTogglesDisplay reproduces an actual mouse click on the
// checkbox column through Table's own MouseHandler — the real path a click
// takes — rather than calling toggleCheckbox directly (TestToggleCheckbox
// already covers that). A tcell.SimulationScreen stands in for a real
// terminal so Table.CellAt has real layout to resolve coordinates against
// (per its own doc, it needs the table to have actually been drawn).
func TestCheckboxClickTogglesDisplay(t *testing.T) {
	dir := fixtureDir(t)
	p, err := NewPanel(tview.NewApplication(), dir)
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

// TestRightDragSelectsRange simulates a right-button drag from row 1
// through row 3 (MouseRightDown then MouseRightUp at a different row, the
// same two actions — and nothing else — a genuine drag produces; see
// captureMouse's doc comment for why no MouseRightClick is involved) and
// checks that exactly those rows end up selected, the context menu never
// opens, and the drag state is left clean for the next gesture.
func TestRightDragSelectsRange(t *testing.T) {
	dir := fixtureDir(t) // rows: "..", app-data, apple.txt, apricot.txt, banana.txt
	root, cleanup := drawnRoot(t, dir)
	defer cleanup()

	startX, startY, ok := findRowPos(root.panel.table, 1, 80, 24)
	if !ok {
		t.Fatal("could not locate row 1's screen position")
	}
	endX, endY, ok := findRowPos(root.panel.table, 3, 80, 24)
	if !ok {
		t.Fatal("could not locate row 3's screen position")
	}

	handler := root.MouseHandler()
	handler(tview.MouseRightDown, tcell.NewEventMouse(startX, startY, tcell.ButtonSecondary, 0), func(tview.Primitive) {})
	handler(tview.MouseRightUp, tcell.NewEventMouse(endX, endY, tcell.ButtonNone, 0), func(tview.Primitive) {})

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
