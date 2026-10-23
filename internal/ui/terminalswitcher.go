package ui

import (
	"fmt"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// The Terminal switcher: a drop-down list of every open Terminal (see
// shells.go for the whole background concept, still internally named
// "Shell" throughout that file and termtab — only the user-facing name
// changed, per the user's own explicit request), deliberately given the
// identical look and mechanics tabswitcher.go's own switcher has, right
// down to anchoring under its own strip rather than centering on screen
// — the user's own explicit request, after first shipping this as a
// centered, sshShellMenuTable-style dropdown (see git history).
//
// Differences from the tab switcher, each one deliberate:
//   - No split column: there is no split-view concept for Terminals.
//   - A row can close down to zero, unlike tabs (one tab always stays
//     open) — so there's no "first row has no close button" exception.
//   - A trailing "Files" row, shown only while a Terminal is actually
//     on screen (r.viewingShells), to leave that view — tabs have no
//     equivalent, since a tab IS the file view.
//   - A bare, unmodified digit 0-9 jumps straight to that numbered
//     Terminal and closes the switcher immediately — carried over
//     unchanged from the first, centered-dropdown version (see
//     captureTerminalSwitcherKey's own doc comment) — the tab switcher
//     has nothing like it.
//   - No close glyph on the title bar: the tab switcher's own title bar
//     doesn't have one either (Escape/outside-click only), so this
//     doesn't either, to read as "the same kind of dropdown".

const terminalSwitcherPage = "terminal-switcher"

const (
	terminalSwitcherColLabel = iota
	terminalSwitcherColClose
)

// terminalSwitcherNewRowLabel mirrors tabSwitcherNewRowLabel's own "+"
// convention — the identical glyph the terminal strip's own leading "+"
// uses (see terminalstrip.go), so the two read as the same action.
const terminalSwitcherNewRowLabel = " +  New terminal"

// terminalSwitcherFilesRowLabel is the trailing "leave this view" row,
// only present while r.viewingShells — see this file's own doc comment.
const terminalSwitcherFilesRowLabel = " Files"

// OpenTerminalSwitcherShortcut is Alt+S (see cmd/breakthrough's own
// global SetInputCapture, which intercepts it unconditionally so it
// reaches here even while a Terminal has keyboard focus) — gated the
// same way every other Shortcut method in this package is (see
// acceptsGlobalShortcut's own doc comment).
func (r *Root) OpenTerminalSwitcherShortcut() {
	if !r.acceptsGlobalShortcut() {
		return
	}
	r.openTerminalSwitcher(r.activeShell)
}

// newTerminalSwitcher builds the switcher's Table — the identical shape
// newTabSwitcher already establishes.
func (r *Root) newTerminalSwitcher() *tview.Table {
	table := tview.NewTable()
	table.SetBorders(false)
	table.SetBorderPadding(0, 0, 1, 1)
	table.SetSelectable(true, true)
	table.SetInputCapture(r.captureTerminalSwitcherKey)
	table.SetSelectedFunc(func(row, column int) { r.activateTerminalSwitcherCell(row, column) })
	return table
}

func (r *Root) newTerminalSwitcherLayout() *tview.Flex {
	return tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(r.terminalSwitcherTitleBar, 1, 0, false).
		AddItem(r.terminalSwitcher, 0, 1, true)
}

// newTerminalSwitcherTitleBar mirrors newTabSwitcherTitleBar — plain
// text, no close glyph, no mouse capture (see this file's own doc
// comment on why).
func (r *Root) newTerminalSwitcherTitleBar() *tview.TextView {
	bar := tview.NewTextView()
	bar.SetWrap(false)
	bar.SetText(" Terminals ")
	return bar
}

// openTerminalSwitcher shows the switcher with selected preselected —
// the identical shape openTabSwitcher already establishes, including
// building its rows inline rather than through a separate render
// function (closeTerminalFromSwitcher re-opens rather than re-rendering
// in place, the same as closeTabFromSwitcher).
func (r *Root) openTerminalSwitcher(selected int) {
	if selected < 0 || selected >= len(r.shells) {
		selected = 0
	}

	r.terminalSwitcher.Clear()
	for i, tab := range r.shells {
		row := i
		label := terminalSwitcherRowLabel(i, tab.label(), i == r.activeShell, r.theme.PlaceholderText)
		r.terminalSwitcher.SetCell(i, terminalSwitcherColLabel,
			tview.NewTableCell(label).
				SetTextColor(r.theme.Text).
				SetSelectable(true).
				SetClickedFunc(r.clickTerminalSwitcherCell(row, terminalSwitcherColLabel)))
		r.terminalSwitcher.SetCell(i, terminalSwitcherColClose,
			tview.NewTableCell(" "+tabSwitcherCloseGlyph+" ").
				SetTextColor(r.theme.Text).
				SetSelectable(true).
				SetClickedFunc(r.clickTerminalSwitcherCell(row, terminalSwitcherColClose)))
	}

	newRow := len(r.shells)
	r.terminalSwitcher.SetCell(newRow, terminalSwitcherColLabel,
		tview.NewTableCell(terminalSwitcherNewRowLabel).
			SetTextColor(r.theme.Text).
			SetSelectable(true).
			SetClickedFunc(r.clickTerminalSwitcherCell(newRow, terminalSwitcherColLabel)))
	r.terminalSwitcher.SetCell(newRow, terminalSwitcherColClose,
		tview.NewTableCell(" ").SetSelectable(false))

	filesRow := -1
	if r.viewingShells {
		filesRow = newRow + 1
		r.terminalSwitcher.SetCell(filesRow, terminalSwitcherColLabel,
			tview.NewTableCell(terminalSwitcherFilesRowLabel).
				SetTextColor(r.theme.Text).
				SetSelectable(true).
				SetClickedFunc(r.clickTerminalSwitcherCell(filesRow, terminalSwitcherColLabel)))
		r.terminalSwitcher.SetCell(filesRow, terminalSwitcherColClose,
			tview.NewTableCell(" ").SetSelectable(false))
	}
	r.terminalSwitcherFilesRow = filesRow

	r.terminalSwitcher.Select(selected, terminalSwitcherColLabel)

	width, height := r.terminalSwitcherSize()
	right, y := r.terminalStripAnchor()
	x := right - width
	x, y, width, height = r.clampToPanel(x, y, width, height)

	r.terminalSwitcherLayout.SetRect(x, y, width, height)
	r.showOverlay(terminalSwitcherPage, r.terminalSwitcher)
	r.setTerminalSwitcherEmptyStackFocus()
}

// setTerminalSwitcherEmptyStackFocus points the switcher's own,
// currently topmost overlay frame's emptyStackFocus at whichever
// Terminal is currently active — hideOverlay's own default, once this
// is the last overlay layer to close, is r.app.SetFocus(r.panel), which
// is wrong while a Terminal is on screen instead of the panel (r.panel
// isn't even mounted in panelHost then, see remountShellView): a real
// bug caught before the first version of this ever shipped, where
// Escape would leave the keyboard going nowhere. emptyStackFocus is the
// same escape hatch pushOverlayReturningFocusTo already uses for the
// identical "not the panel" case (openCompletionPicker).
//
// Called again (not just once from openTerminalSwitcher) right before
// every path that closes this switcher without itself setting focus
// afterwards (Escape) — r.activeShell can have changed, or every
// Terminal can have closed, while the switcher's own "✕" column was
// used to close one without leaving the overlay, which a value captured
// once at open time would otherwise go stale against.
func (r *Root) setTerminalSwitcherEmptyStackFocus() {
	if len(r.overlayStack) == 0 {
		return
	}
	var focus tview.Primitive
	if r.viewingShells {
		focus = r.shells[r.activeShell].view
	}
	r.overlayStack[len(r.overlayStack)-1].emptyStackFocus = focus
}

// activateTerminalSwitcherCell is Enter, Space, or a click on one cell —
// the identical dispatch-by-row/by-column shape activateTabSwitcherCell
// already establishes, minus the split case (no split column here).
func (r *Root) activateTerminalSwitcherCell(row, column int) {
	switch {
	case row >= 0 && row < len(r.shells):
		if column == terminalSwitcherColClose {
			r.closeTerminalFromSwitcher(row)
			return
		}
		r.hideOverlay()
		r.switchToShell(row)
	case row == len(r.shells):
		r.hideOverlay()
		r.openNewTerminalDialog()
	case row == r.terminalSwitcherFilesRow:
		r.hideOverlay()
		r.viewFiles()
	}
}

func (r *Root) clickTerminalSwitcherCell(row, column int) func() bool {
	return func() bool {
		r.activateTerminalSwitcherCell(row, column)
		return true
	}
}

// closeTerminalFromSwitcher closes the Terminal on one row and leaves
// the switcher open, reselecting the row above it — the identical
// shape closeTabFromSwitcher already establishes, minus its "refuse the
// first tab" rule: a Terminal can close down to zero, there's nothing
// here that always has to stay open.
func (r *Root) closeTerminalFromSwitcher(row int) {
	if row < 0 || row >= len(r.shells) {
		return
	}
	r.closeShell(row)
	next := row - 1
	if next < 0 {
		next = 0
	}
	r.openTerminalSwitcher(next)
}

// captureTerminalSwitcherKey adds Escape, Space, Delete, and a bare
// digit jump to the switcher's own keys.
//
// The bare-digit jump (0-9, no Ctrl/Alt) is this switcher's own
// distinguishing feature, carried over unchanged from the first,
// centered-dropdown version of this overlay: zero collision risk, since
// an unmodified digit is always correctly reported by every terminal,
// unlike the Ctrl/Alt+digit combinations already spoken for elsewhere in
// this app (see cmd/breakthrough's own SwitchToTabShortcut wiring) — the
// same prefix-then-digit convention tmux's own window-switching already
// relies on. Only the first ten Terminals get a digit this way; digit 0
// is the tenth, matching renderTerminalStrip's own "i+1, no wraparound"
// numbering for anything beyond that, and the identical "...0 is the
// tenth" convention SwitchToTabShortcut already uses for file tabs.
func (r *Root) captureTerminalSwitcherKey(event *tcell.EventKey) *tcell.EventKey {
	row, column := r.terminalSwitcher.GetSelection()

	switch {
	case event.Key() == tcell.KeyEscape:
		r.setTerminalSwitcherEmptyStackFocus()
		r.closeTerminalSwitcher()
		return nil
	case event.Key() == tcell.KeyRune && event.Rune() == ' ':
		r.activateTerminalSwitcherCell(row, column)
		return nil
	case event.Key() == tcell.KeyDelete:
		r.closeTerminalFromSwitcher(row)
		return nil
	}

	if event.Key() == tcell.KeyRune && event.Modifiers() == tcell.ModNone && event.Rune() >= '0' && event.Rune() <= '9' {
		digit := int(event.Rune() - '0')
		index := digit - 1
		if digit == 0 {
			index = 9 // "0" is the tenth Terminal
		}
		if index >= 0 && index < len(r.shells) {
			r.hideOverlay()
			r.switchToShell(index)
			return nil
		}
	}

	return event
}

// closeTerminalSwitcher dismisses the switcher without switching —
// Escape, or a click outside it (see captureOutsideClick, which reaches
// this the same way every other overlay's own close does).
func (r *Root) closeTerminalSwitcher() {
	r.hideOverlay()
}

// terminalSwitcherSize is the overlay's own size — the identical
// per-column measurement tabSwitcherSize already establishes, minus the
// split column.
func (r *Root) terminalSwitcherSize() (width, height int) {
	table := r.terminalSwitcher
	rows := table.GetRowCount()

	columnWidths := make([]int, terminalSwitcherColClose+1)
	for row := 0; row < rows; row++ {
		for column := range columnWidths {
			cell := table.GetCell(row, column)
			if cell == nil {
				continue
			}
			if w := tview.TaggedStringWidth(cell.Text); w > columnWidths[column] {
				columnWidths[column] = w
			}
		}
	}
	for i, w := range columnWidths {
		width += w
		if i > 0 {
			width++
		}
	}

	width += 2             // the table's own left/right border padding
	return width, rows + 1 // +1 for the title bar's own row
}

// terminalStripAnchor mirrors tabStripAnchor — the screen position the
// switcher drops down from, the right edge of the panel's own terminal
// strip, one row below it.
func (r *Root) terminalStripAnchor() (right, y int) {
	sx, sy, sw, _ := r.panel.terminalStrip.GetRect()
	if sw == 0 {
		px, py, pw, _ := r.panel.GetRect()
		return px + pw, py + 1
	}
	return sx + sw, sy + 1
}

// terminalSwitcherRowLabel mirrors tabSwitcherRowLabel — the Terminal's
// own number, then its connection label, with the active one dimmed and
// marked "(current)" the identical way the tab switcher already marks
// the tab you're currently on.
func terminalSwitcherRowLabel(i int, label string, current bool, dim tcell.Color) string {
	row := fmt.Sprintf("%2d  %s", i+1, label)
	if current {
		return fmt.Sprintf("[%s]%s  (current)[-]", colorTag(dim), row)
	}
	return row
}
