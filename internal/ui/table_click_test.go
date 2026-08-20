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
