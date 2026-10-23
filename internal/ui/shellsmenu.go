package ui

import (
	"strconv"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/jagottsicher/breakthrough/internal/remotefs"
)

// shellsMenuColLabel/shellsMenuColClose are the Shells overlay's own two
// columns — the identical shape sshShellMenuCol* already establishes,
// minus the Edit column: a running Shell has nothing to edit, only to
// switch to or close.
const (
	shellsMenuColLabel = iota
	shellsMenuColClose
)

// OpenShellsMenuShortcut is Alt+S (see cmd/breakthrough's own global
// SetInputCapture) — gated the same way every other Shortcut method in
// this package is (see acceptsGlobalShortcut's own doc comment), so it
// stays inert while an overlay is already open or the bash line has
// focus.
func (r *Root) OpenShellsMenuShortcut() {
	if !r.acceptsGlobalShortcut() {
		return
	}
	r.openShellsMenu()
}

// openShellsMenu shows the Shells overlay, centered on screen — the
// identical placement openSSHShellMenu already uses, since there's no
// header button of its own for this either.
func (r *Root) openShellsMenu() {
	r.renderShellsMenu()

	width, height := r.shellsMenuSize()
	x, y := r.centeredOnScreen(width, height)
	x, y, width, height = r.clampToScreen(x, y, width, height)
	r.renderShellsMenuTitleBar(width)
	r.shellsMenuLayout.SetRect(x, y, width, height)
	r.showOverlay(shellsMenuPage, r.shellsMenuTable)
	r.setShellsMenuEmptyStackFocus()
}

// setShellsMenuEmptyStackFocus points the Shells menu's own, currently
// topmost overlay frame's emptyStackFocus at whichever Shell is
// currently active — hideOverlay's own default, once this is the
// last overlay layer to close, is r.app.SetFocus(r.panel), which is
// wrong while a Shell is on screen instead of the panel (r.panel
// isn't even mounted in panelHost then, see remountShellView): a real
// bug caught before this ever shipped, where Escape would leave the
// keyboard going nowhere, not a live report. emptyStackFocus is
// exactly the escape hatch pushOverlayReturningFocusTo already uses
// for the identical "not the panel" case (openCompletionPicker).
//
// Called again (not just once from openShellsMenu) right before every
// path that closes this menu without itself setting focus afterwards
// (Escape, the title bar's own close glyph) — r.activeShell can have
// changed, or every Shell can have closed, while the menu's own "✕"
// column was used to close one without leaving the overlay (see
// activateShellsMenuCell/captureShellsMenuKey), which a value captured
// once at open time would otherwise go stale against.
func (r *Root) setShellsMenuEmptyStackFocus() {
	if len(r.overlayStack) == 0 {
		return
	}
	var focus tview.Primitive
	if r.viewingShells {
		focus = r.shells[r.activeShell].view
	}
	r.overlayStack[len(r.overlayStack)-1].emptyStackFocus = focus
}

// renderShellsMenu rebuilds the overlay's own rows: one per currently
// open Shell, numbered 1..9 then 0 for the tenth — the same numbering
// bare digits jump to directly while this table has focus (see
// captureShellsMenuKey) and the same "…+0 is the tenth" convention
// SwitchToTabShortcut's own doc comment already establishes for the
// file-tab switcher, so the two schemes read the same way even though
// they're otherwise fully independent. Followed by "+ New shell…" and,
// only while a Shell is currently the one on screen, "Files" (to
// return to whatever remountPanels would otherwise show) — a plain
// index-based lookup (shellsMenuRows) rather than encoding the row
// number into the cell's own text and parsing it back out.
func (r *Root) renderShellsMenu() {
	table := r.shellsMenuTable
	table.Clear()
	r.shellsMenuRows = map[int]int{}

	row := 0
	for i, tab := range r.shells {
		i := i
		label := tab.label()
		if i < 10 {
			// Only the first ten get a digit prefix at all — the bare-
			// digit jump this numbering exists for (see
			// captureShellsMenuKey) only ever has ten keys to bind to
			// in the first place; an 11th+ Shell is still listed here
			// and still reachable by Enter/click, just not by a single
			// keystroke.
			digit := (i + 1) % 10 // 10th shell (index 9) shows as "0"
			label = strconv.Itoa(digit) + ". " + label
		}
		if r.viewingShells && r.activeShell == i {
			label += "  (current)"
		}

		table.SetCell(row, shellsMenuColLabel,
			tview.NewTableCell(label).
				SetTextColor(r.theme.Text).
				SetSelectable(true).
				SetClickedFunc(r.clickShellsMenuCell(row, shellsMenuColLabel)))
		table.SetCell(row, shellsMenuColClose,
			tview.NewTableCell(" "+connectionHistoryRemoveGlyph+" ").
				SetTextColor(r.theme.MutedTextColor).
				SetSelectable(true).
				SetClickedFunc(r.clickShellsMenuCell(row, shellsMenuColClose)))

		r.shellsMenuRows[row] = i
		row++
	}

	r.shellsMenuNewRow = row
	table.SetCell(row, shellsMenuColLabel,
		tview.NewTableCell("+ New shell…").
			SetTextColor(r.theme.Text).
			SetSelectable(true).
			SetClickedFunc(r.clickShellsMenuCell(row, shellsMenuColLabel)))
	table.SetCell(row, shellsMenuColClose, blankConnectionMenuCell())
	row++

	r.shellsMenuFilesRow = -1
	if r.viewingShells {
		r.shellsMenuFilesRow = row
		table.SetCell(row, shellsMenuColLabel,
			tview.NewTableCell("Files").
				SetTextColor(r.theme.Text).
				SetSelectable(true).
				SetClickedFunc(r.clickShellsMenuCell(row, shellsMenuColLabel)))
		table.SetCell(row, shellsMenuColClose, blankConnectionMenuCell())
	}

	table.Select(0, shellsMenuColLabel)
}

// newShellsMenuTable builds the overlay's own Table — the identical
// shape newSSHShellMenuTable already establishes.
func (r *Root) newShellsMenuTable() *tview.Table {
	table := tview.NewTable()
	table.SetBorders(false)
	table.SetBorderPadding(0, 0, 1, 1)
	table.SetSelectable(true, true)
	table.SetInputCapture(r.captureShellsMenuKey)
	table.SetSelectedFunc(func(row, column int) { r.activateShellsMenuCell(row, column) })
	return table
}

func (r *Root) newShellsMenuLayout() *tview.Flex {
	return tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(r.shellsMenuTitleBar, 1, 0, false).
		AddItem(r.shellsMenuTable, 0, 1, true)
}

func (r *Root) newShellsMenuTitleBar() *tview.TextView {
	bar := tview.NewTextView()
	bar.SetWrap(false)
	bar.SetMouseCapture(r.captureShellsMenuTitleBarMouse)
	return bar
}

// renderShellsMenuTitleBar sets shellsMenuTitleBar's own text to
// "Shells", padded out to width columns, with the close glyph placed
// the same way renderSSHShellMenuTitleBar's own does — see its doc
// comment.
func (r *Root) renderShellsMenuTitleBar(width int) {
	const label = " Shells "
	closeCol := toolWindowCloseButtonCol(0, width)
	padding := closeCol - len(label)
	if padding < 0 {
		padding = 0
	}
	r.shellsMenuTitleBar.SetText(label + strings.Repeat(" ", padding) + string(toolWindowCloseGlyph) + " ")
}

func (r *Root) captureShellsMenuTitleBarMouse(action tview.MouseAction, event *tcell.EventMouse) (tview.MouseAction, *tcell.EventMouse) {
	if action != tview.MouseLeftClick {
		return action, event
	}
	x, y := event.Position()
	rectX, rectY, width, _ := r.shellsMenuTitleBar.GetRect()
	if y != rectY || x != rectX+toolWindowCloseButtonCol(0, width) {
		return action, event
	}
	r.setShellsMenuEmptyStackFocus()
	r.hideOverlay()
	return tview.MouseConsumed, nil
}

// activateShellsMenuCell is Enter, Space, or a click on one cell — the
// identical dispatch-by-column/by-row shape activateSSHShellMenuCell
// already establishes.
func (r *Root) activateShellsMenuCell(row, column int) {
	if i, ok := r.shellsMenuRows[row]; ok {
		if column == shellsMenuColClose {
			r.closeShell(i)
			r.renderShellsMenu()
			return
		}
		r.hideOverlay()
		r.switchToShell(i)
		return
	}
	if row == r.shellsMenuNewRow {
		r.hideOverlay()
		r.sshShellSubmit = r.openNewShellEmbedded
		r.openSSHShellDialog(remotefs.Connection{})
		return
	}
	if row == r.shellsMenuFilesRow {
		r.hideOverlay()
		r.viewFiles()
	}
}

func (r *Root) clickShellsMenuCell(row, column int) func() bool {
	return func() bool {
		r.activateShellsMenuCell(row, column)
		return true
	}
}

// captureShellsMenuKey adds Escape (close), "x"/Delete (close the
// selected row's own Shell), and — the design this whole overlay
// exists to carry — a bare, unmodified digit 0-9 that jumps straight
// to that numbered Shell without going through Enter at all. Zero
// collision risk: an unmodified digit is always correctly reported by
// every terminal, unlike the Ctrl/Alt+digit combinations already
// spoken for elsewhere in this app (see cmd/breakthrough's own
// SwitchToTabShortcut wiring) — the same prefix-then-digit convention
// tmux's own window-switching already relies on.
func (r *Root) captureShellsMenuKey(event *tcell.EventKey) *tcell.EventKey {
	row, column := r.shellsMenuTable.GetSelection()

	switch {
	case event.Key() == tcell.KeyEscape:
		r.setShellsMenuEmptyStackFocus()
		r.hideOverlay()
		return nil
	case event.Key() == tcell.KeyRune && event.Rune() == ' ':
		r.activateShellsMenuCell(row, column)
		return nil
	}

	if event.Key() == tcell.KeyRune && event.Modifiers() == tcell.ModNone && event.Rune() >= '0' && event.Rune() <= '9' {
		digit := int(event.Rune() - '0')
		index := digit - 1
		if digit == 0 {
			index = 9 // "0" is the tenth shell — see renderShellsMenu
		}
		if index >= 0 && index < len(r.shells) {
			r.hideOverlay()
			r.switchToShell(index)
			return nil
		}
	}

	isCloseKey := (event.Key() == tcell.KeyRune && event.Rune() == 'x') || event.Key() == tcell.KeyDelete
	if isCloseKey {
		if i, ok := r.shellsMenuRows[row]; ok {
			r.closeShell(i)
			r.renderShellsMenu()
			return nil
		}
	}

	return event
}

// shellsMenuSize is the overlay's own size — the identical per-column
// measurement sshShellMenuSize already establishes.
func (r *Root) shellsMenuSize() (width, height int) {
	table := r.shellsMenuTable
	rows := table.GetRowCount()

	columnWidths := make([]int, shellsMenuColClose+1)
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

	const minWidth = 24 // "Shells" title bar plus its own close glyph
	width += 2
	if width < minWidth {
		width = minWidth
	}
	return width, rows + 1 // +1 for the title bar's own row
}
