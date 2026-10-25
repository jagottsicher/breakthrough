package ui

import (
	"fmt"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/jagottsicher/breakthrough/internal/remotefs"
)

// The Terminal switcher: a drop-down list that merges every currently
// open Terminal (live or exited, from r.shells) with every other
// connection the shared, disk-persisted connection history
// (internal/remotefs) remembers but doesn't currently have an open
// Terminal of its own — the user's own explicit request, after the
// first few versions of this only ever listed r.shells: a Terminal
// you've closed, or one from a previous run of the app entirely,
// should still be one click (or one digit) away from reopening, the
// same way the "gs" dropdown and the Connect dialog already remember
// every connection across restarts.
//
// A row's own kind decides what its three columns do:
//   - An open row (r.shells has a tab for it, live or exited): the
//     label switches to it (or does nothing extra if it's already the
//     one on screen); the Action column reads "⏏" (disconnect) and
//     closes it; "✕" closes it too — the same action twice, since
//     there's nothing else "remove" could mean for something still
//     running (see closeShell's own doc comment: closing it still
//     leaves the connection remembered in history, reopenable later).
//   - A history-only row (no open Terminal): the label opens a new
//     Terminal straight to that connection, no dialog in between,
//     since the connection itself is already fully known; the Action
//     column reads "✎" (edit) and opens the existing "Add new ssh
//     connection" dialog prefilled, the same Edit flow the "gs"
//     dropdown's own history already has; "✕" removes the entry from
//     history outright (remotefs.RemoveFromHistory) — this is the one
//     place "✕" actually means delete, not just disconnect.
//
// Differences from the tab switcher, each one deliberate:
//   - No split column: there is no split-view concept for Terminals.
//   - A row can close down to zero, unlike tabs (one tab always stays
//     open) — so there's no "first row has no close button" exception.
//   - A trailing "Go to file view" row, shown only while a Terminal is
//     actually on screen (r.viewingShells), to leave that view — tabs
//     have no equivalent, since a tab IS the file view. Named "Go to"
//     rather than "Back"/"Return": a Terminal isn't reached by leaving
//     some single, particular file view to begin with (see the user's
//     own explicit reasoning), so neither word fits.
//   - A bare, unmodified digit 0-9 jumps straight to that numbered
//     row (open or history-only alike) and closes the switcher
//     immediately — carried over unchanged from the first, centered-
//     dropdown version (see captureTerminalSwitcherKey's own doc
//     comment) — the tab switcher has nothing like it.
//   - No close glyph on the title bar: the tab switcher's own title
//     bar doesn't have one either (Escape/outside-click only), so
//     this doesn't either, to read as "the same kind of dropdown".

const terminalSwitcherPage = "terminal-switcher"

const (
	terminalSwitcherColLabel = iota
	terminalSwitcherColAction
	terminalSwitcherColClose
)

// terminalSwitcherNewRowLabel mirrors tabSwitcherNewRowLabel's own "+"
// convention — the identical glyph the terminal strip's own leading "+"
// uses (see terminalstrip.go), so the two read as the same action.
const terminalSwitcherNewRowLabel = " +  New terminal"

// terminalSwitcherFilesRowLabel is the trailing "leave this view" row,
// only present while r.viewingShells — see this file's own doc comment.
// Named for the destination action ("Go to"), not a direction
// ("Back"/"Return"): per the user's own explicit reasoning, a Terminal
// isn't reached by leaving one particular file view in the first
// place, so framing this as returning to "the" view would be
// misleading.
const terminalSwitcherFilesRowLabel = " Go to file view"

// OpenTerminalSwitcherShortcut is the half of Alt+T's own toggle that
// runs while no Terminal is on screen (see cmd/breakthrough's own
// global SetInputCapture — ExitTerminalViewShortcut, shells.go, is the
// other half) — but also plain "T"'s own target (keymap.go), which
// unlike Alt+T is gated on the panel having focus in the first place
// (acceptsPlainKeyCommand, upstream of this ever being called) and so
// never reaches here while a Terminal is on screen at all. Gated on
// acceptsGlobalShortcutEvenInATerminal, not the stricter
// acceptsGlobalShortcut every other Shortcut method in this package
// uses — see that function's own doc comment for why this one caller
// is the exception.
func (r *Root) OpenTerminalSwitcherShortcut() {
	if !r.acceptsGlobalShortcutEvenInATerminal() {
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
// selected is a row index, which for every open Terminal happens to
// equal its own r.shells index too, since open rows are always built
// first, in r.shells order (see below) — every existing caller already
// passes r.activeShell expecting exactly that.
//
// Builds its rows inline rather than through a separate render
// function (closeTerminalFromSwitcher/removeTerminalHistoryRow re-open
// rather than re-rendering in place, the same as closeTabFromSwitcher).
func (r *Root) openTerminalSwitcher(selected int) {
	history, _ := remotefs.LoadHistory()
	matchedHist := make([]bool, len(history))

	r.terminalSwitcher.Clear()
	r.terminalSwitcherOpenRows = map[int]*shellTab{}
	r.terminalSwitcherHistRows = map[int]remotefs.Connection{}

	row := 0
	for _, tab := range r.shells {
		var hist remotefs.HistoryEntry
		known := false
		for hi, h := range history {
			if !matchedHist[hi] && tab.conn.Equal(h.Connection) {
				matchedHist[hi] = true
				hist = h
				known = true
				break
			}
		}
		r.renderTerminalSwitcherOpenRow(row, tab, hist, known)
		r.terminalSwitcherOpenRows[row] = tab
		row++
	}
	for hi, h := range history {
		if matchedHist[hi] {
			continue
		}
		r.renderTerminalSwitcherHistoryRow(row, h)
		r.terminalSwitcherHistRows[row] = h.Connection
		row++
	}

	r.terminalSwitcherNewRow = row
	r.terminalSwitcher.SetCell(row, terminalSwitcherColLabel,
		tview.NewTableCell(terminalSwitcherNewRowLabel).
			SetTextColor(r.theme.Text).
			SetSelectable(true).
			SetClickedFunc(r.clickTerminalSwitcherCell(r.terminalSwitcherNewRow, terminalSwitcherColLabel)))
	r.terminalSwitcher.SetCell(row, terminalSwitcherColAction, blankConnectionMenuCell())
	r.terminalSwitcher.SetCell(row, terminalSwitcherColClose, blankConnectionMenuCell())
	row++

	r.terminalSwitcherFilesRow = -1
	if r.viewingShells {
		r.terminalSwitcherFilesRow = row
		r.terminalSwitcher.SetCell(row, terminalSwitcherColLabel,
			tview.NewTableCell(terminalSwitcherFilesRowLabel).
				SetTextColor(r.theme.Text).
				SetSelectable(true).
				SetClickedFunc(r.clickTerminalSwitcherCell(r.terminalSwitcherFilesRow, terminalSwitcherColLabel)))
		r.terminalSwitcher.SetCell(row, terminalSwitcherColAction, blankConnectionMenuCell())
		r.terminalSwitcher.SetCell(row, terminalSwitcherColClose, blankConnectionMenuCell())
	}

	if selected < 0 || selected >= r.terminalSwitcherNewRow {
		selected = 0
		if len(r.shells) == 0 && len(r.terminalSwitcherHistRows) == 0 {
			selected = r.terminalSwitcherNewRow
		}
	}
	r.terminalSwitcher.Select(selected, terminalSwitcherColLabel)

	width, height := r.terminalSwitcherSize()
	right, y := r.terminalStripAnchor()
	x := right - width
	x, y, width, height = r.clampToPanel(x, y, width, height)

	r.terminalSwitcherLayout.SetRect(x, y, width, height)
	r.showOverlay(terminalSwitcherPage, r.terminalSwitcher)
	r.setTerminalSwitcherEmptyStackFocus()
}

// renderTerminalSwitcherOpenRow fills row with tab's own open-Terminal
// shape — the label switches to it, the Action column disconnects it
// (the identical action "✕" also performs — see this file's own
// package doc comment on why neither needs a separate meaning here).
// hist/known are the matching shared-history entry, if openTerminalSwitcher
// found one — tab.conn might not have one at all (e.g. SaveConnection
// failed to write when this Terminal opened), in which case known is
// false and this renders the same as a genuinely never-tried
// connection (see connectionHistoryColor's own doc comment).
func (r *Root) renderTerminalSwitcherOpenRow(row int, tab *shellTab, hist remotefs.HistoryEntry, known bool) {
	current := tab == r.shells[r.activeShell] && !tab.exited
	color := connectionHistoryColor(r.theme, current, known && hist.LastFailed, !known || hist.Untested)
	label := terminalSwitcherRowLabel(row, tab.label(), current, r.theme.PlaceholderText)
	r.terminalSwitcher.SetCell(row, terminalSwitcherColLabel,
		tview.NewTableCell(label).
			SetTextColor(color).
			SetSelectable(true).
			SetClickedFunc(r.clickTerminalSwitcherCell(row, terminalSwitcherColLabel)))
	r.terminalSwitcher.SetCell(row, terminalSwitcherColAction,
		tview.NewTableCell(" "+connectionHistoryEjectGlyph+" ").
			SetTextColor(r.theme.MutedTextColor).
			SetSelectable(true).
			SetClickedFunc(r.clickTerminalSwitcherCell(row, terminalSwitcherColAction)))
	r.terminalSwitcher.SetCell(row, terminalSwitcherColClose,
		tview.NewTableCell(" "+connectionHistoryRemoveGlyph+" ").
			SetTextColor(r.theme.MutedTextColor).
			SetSelectable(true).
			SetClickedFunc(r.clickTerminalSwitcherCell(row, terminalSwitcherColClose)))
}

// renderTerminalSwitcherHistoryRow fills row with h's own history-only
// shape — no open Terminal has this connection right now. The label
// connects straight to it (no dialog, the connection is already fully
// known); the Action column edits it instead of disconnecting (there's
// nothing live to disconnect); "✕" removes it from history outright.
// Colored the identical red/green/white connectionHistoryColor already
// gives the "@" connections menu's own history rows — never isActive
// here, since a history-only row has no open Terminal to be the
// currently active one.
func (r *Root) renderTerminalSwitcherHistoryRow(row int, h remotefs.HistoryEntry) {
	color := connectionHistoryColor(r.theme, false, h.LastFailed, h.Untested)
	label := fmt.Sprintf("%2d  %s%s", row+1, h.Label(), shellFlagsSuffix(h.Connection))
	r.terminalSwitcher.SetCell(row, terminalSwitcherColLabel,
		tview.NewTableCell(label).
			SetTextColor(color).
			SetSelectable(true).
			SetClickedFunc(r.clickTerminalSwitcherCell(row, terminalSwitcherColLabel)))
	r.terminalSwitcher.SetCell(row, terminalSwitcherColAction,
		tview.NewTableCell(" "+connectionHistoryEditGlyph+" ").
			SetTextColor(r.theme.MutedTextColor).
			SetSelectable(true).
			SetClickedFunc(r.clickTerminalSwitcherCell(row, terminalSwitcherColAction)))
	r.terminalSwitcher.SetCell(row, terminalSwitcherColClose,
		tview.NewTableCell(" "+connectionHistoryRemoveGlyph+" ").
			SetTextColor(r.theme.MutedTextColor).
			SetSelectable(true).
			SetClickedFunc(r.clickTerminalSwitcherCell(row, terminalSwitcherColClose)))
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

// activateTerminalSwitcherCell is Enter, Space, or a click on one cell
// — dispatched by row *kind* (open Terminal, history-only connection,
// "+ New terminal…", "Files"), not by a fixed row-number range the
// way the original, r.shells-only version of this could get away with
// — see this file's own package doc comment for what each kind's own
// three columns do.
func (r *Root) activateTerminalSwitcherCell(row, column int) {
	if tab, ok := r.terminalSwitcherOpenRows[row]; ok {
		if column == terminalSwitcherColAction || column == terminalSwitcherColClose {
			r.closeTerminalFromSwitcher(row)
			return
		}
		r.hideOverlay()
		if idx := r.shellIndex(tab); idx >= 0 {
			r.switchToShell(idx)
		}
		return
	}
	if conn, ok := r.terminalSwitcherHistRows[row]; ok {
		switch column {
		case terminalSwitcherColAction:
			r.hideOverlay()
			r.openNewTerminalDialog(conn)
		case terminalSwitcherColClose:
			r.removeTerminalHistoryRow(row, conn)
		default:
			r.hideOverlay()
			r.openNewShellEmbedded(conn)
		}
		return
	}
	switch row {
	case r.terminalSwitcherNewRow:
		r.hideOverlay()
		r.openNewTerminalDialog(remotefs.Connection{})
	case r.terminalSwitcherFilesRow:
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

// closeTerminalFromSwitcher closes the open Terminal on row (the
// switcher's own "⏏"/"✕" on an open row — see this file's own package
// doc comment) and leaves the switcher open, reselecting roughly the
// same spot — the identical shape closeTabFromSwitcher already
// establishes, minus its "refuse the first tab" rule: a Terminal can
// close down to zero, there's nothing here that always has to stay
// open. The closed connection stays in history regardless (see
// closeShell's own doc comment), so it simply reappears as a
// history-only row once this reopens.
func (r *Root) closeTerminalFromSwitcher(row int) {
	tab, ok := r.terminalSwitcherOpenRows[row]
	if !ok {
		return
	}
	idx := r.shellIndex(tab)
	if idx < 0 {
		return
	}
	r.closeShell(idx)
	next := row - 1
	if next < 0 {
		next = 0
	}
	r.openTerminalSwitcher(next)
}

// removeTerminalHistoryRow deletes a history-only row's own connection
// from the shared, persisted history outright (see
// remotefs.RemoveFromHistory) — the one path "✕" actually means
// delete rather than disconnect (see this file's own package doc
// comment) — and leaves the switcher open, reselecting roughly the
// same spot, the identical shape closeTerminalFromSwitcher already
// establishes for an open row.
func (r *Root) removeTerminalHistoryRow(row int, conn remotefs.Connection) {
	r.showError(remotefs.RemoveFromHistory(conn))
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
// relies on. Dispatches through activateTerminalSwitcherCell on the
// label column, the same as Enter would on that row, so a digit reaches
// an open row (switches to it) or a history-only one (connects to it)
// identically — only the first ten rows get a digit this way; digit 0
// is the tenth, the identical "...0 is the tenth" convention
// SwitchToTabShortcut already uses for file tabs.
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
		if _, ok := r.terminalSwitcherOpenRows[row]; ok {
			r.closeTerminalFromSwitcher(row)
		} else if conn, ok := r.terminalSwitcherHistRows[row]; ok {
			r.removeTerminalHistoryRow(row, conn)
		}
		return nil
	}

	if event.Key() == tcell.KeyRune && event.Modifiers() == tcell.ModNone && event.Rune() >= '0' && event.Rune() <= '9' {
		digit := int(event.Rune() - '0')
		index := digit - 1
		if digit == 0 {
			index = 9 // "0" is the tenth row
		}
		if index >= 0 && index < r.terminalSwitcherNewRow {
			r.activateTerminalSwitcherCell(index, terminalSwitcherColLabel)
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
// per-column measurement tabSwitcherSize already establishes, with one
// more column than it has (Action, see this file's own package doc
// comment on what it's for).
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

// terminalSwitcherRowLabel mirrors tabSwitcherRowLabel — the row's own
// number, then its connection label, with the active live one dimmed
// and marked "(current)" the identical way the tab switcher already
// marks the tab you're currently on.
func terminalSwitcherRowLabel(row int, label string, current bool, dim tcell.Color) string {
	text := fmt.Sprintf("%2d  %s", row+1, label)
	if current {
		return fmt.Sprintf("[%s]%s  (current)[-]", colorTag(dim), text)
	}
	return text
}
