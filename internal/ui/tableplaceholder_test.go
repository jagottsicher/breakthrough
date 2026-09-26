package ui

import (
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// TestShowTablePlaceholderWritesTheMessageAndDisablesSelection pins
// showTablePlaceholder's own two-part contract: the message lands at
// (1, 0), and row selection is turned off entirely — not just the one
// cell marked unselectable.
func TestShowTablePlaceholderWritesTheMessageAndDisablesSelection(t *testing.T) {
	table := tview.NewTable()
	table.SetSelectable(true, false)

	showTablePlaceholder(table, "nothing to show", tcell.ColorRed)

	if got := table.GetCell(1, 0).Text; got != "nothing to show" {
		t.Errorf("cell (1,0) text = %q, want %q", got, "nothing to show")
	}
	row, _ := table.GetSelection()
	if row != 1 {
		t.Errorf("selected row = %d, want 1", row)
	}
}

// TestShowTablePlaceholderNeverHangsOnDownAfterARealDraw is the shared
// helper's own regression coverage for the real freeze it exists to fix
// — see this package's own per-screen equivalents (e.g.
// TestFirewallErrorScreenNeverHangsOnDownAfterARealDraw) for the full
// mechanism. Proven once here, directly against the helper itself,
// rather than only indirectly through every caller.
func TestShowTablePlaceholderNeverHangsOnDownAfterARealDraw(t *testing.T) {
	table := tview.NewTable()
	table.SetSelectable(true, false)
	for c := 0; c < 9; c++ {
		table.SetCell(0, c, tview.NewTableCell("h").SetSelectable(false))
	}

	showTablePlaceholder(table, "nothing to show", tcell.ColorRed)

	screen := tcell.NewSimulationScreen("")
	if err := screen.Init(); err != nil {
		t.Fatalf("screen.Init: %v", err)
	}
	defer screen.Fini()
	screen.SetSize(100, 40)
	table.SetRect(0, 0, 100, 40)
	table.Draw(screen) // the real app's own ordinary redraw

	callWithTimeout(t, 2*time.Second, func() {
		table.InputHandler()(tcell.NewEventKey(tcell.KeyDown, 0, tcell.ModNone), func(tview.Primitive) {})
	})
}

// TestEnableTableSelectionTurnsSelectionBackOn pins the counterpart:
// real row selection actually works again afterward — not just that
// Select() accepts a number, which it would regardless of
// SetSelectable's own state.
func TestEnableTableSelectionTurnsSelectionBackOn(t *testing.T) {
	table := tview.NewTable()
	showTablePlaceholder(table, "nothing to show", tcell.ColorRed)

	table.Clear()
	table.SetCell(0, 0, tview.NewTableCell("row 0").SetSelectable(true))
	table.SetCell(1, 0, tview.NewTableCell("row 1").SetSelectable(true))
	enableTableSelection(table)
	table.Select(0, 0)

	table.InputHandler()(tcell.NewEventKey(tcell.KeyDown, 0, tcell.ModNone), func(tview.Primitive) {})

	if row, _ := table.GetSelection(); row != 1 {
		t.Errorf("selected row after Down = %d, want 1 (selection should be active again)", row)
	}
}
