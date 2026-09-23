package ui

import (
	"fmt"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/jagottsicher/breakthrough/internal/activitylog"
	"github.com/jagottsicher/breakthrough/internal/config"
	"github.com/jagottsicher/breakthrough/internal/remotefs"
)

// connectionMenuMaxHistoryRows caps how many of remotefs.LoadHistory's
// own entries the dropdown actually lists — long enough to show
// "everywhere recent" without the dropdown itself growing past a
// screenful (see openConnectionMenu's own screen-height clamp);
// history.go's own historyMaxEntries already caps how many are ever
// persisted at all, this is just the display-side half of the same
// idea.
const connectionMenuMaxHistoryRows = 10

// connectionMenuNewConnectionLabel is row 0's own label — the exact
// "space, +, two spaces, label" shape tabswitcher.go's own
// tabSwitcherNewRowLabel already establishes for "New tab", per the
// user's own explicit request that the two read as the same kind of
// row wherever they appear.
const connectionMenuNewConnectionLabel = " +  New connection"

// Column indices within the connection menu table — the same
// label/action-cell/action-cell shape tabswitcher.go's own
// tabSwitcherCol* constants already establish for an identical reason:
// each row carries more than one independent thing to act on.
const (
	connectionMenuColLabel = iota
	connectionMenuColEject
	connectionMenuColRemove
)

// openConnectionMenu shows the connection dropdown for the currently
// active panel — reached via a click on the header's own "@" button
// (see buildHeaderSpans/Panel.onOpenConnectionMenu) or the "gc" chord
// (see keymap.go). Anchored under that button, the same "appears right
// below what it drops down from" placement openFilterMenu establishes
// for its own dropdown, and openTabSwitcher for its own.
func (r *Root) openConnectionMenu() {
	r.renderConnectionMenu()

	right, top := r.connectionMenuAnchor()
	width, height := r.connectionMenuSize()
	x, y, width, height := r.clampToPanel(right-width, top, width, height)
	r.connectionMenuLayout.SetRect(x, y, width, height)
	// connectionMenuTable is the real focus target, connectionMenuLayout
	// (title bar + table) what Pages actually shows — the identical split
	// openTabSwitcher's own doc comment explains for the tab switcher.
	r.showOverlay(connectionMenuPage, r.connectionMenuTable)
}

// connectionMenuAnchor returns the screen position the header's own
// connection button ends at — the same GetInnerRect-plus-span-column
// arithmetic captureHeaderMouse itself uses to turn a click position
// into a column, just run in reverse (span column -> screen position)
// since there's no separate tview widget for this one button to call
// GetRect on directly (see buildHeaderSpans' own doc comment: it's one
// colored span within the header TextView, the same as the seven nav
// buttons beside it).
func (r *Root) connectionMenuAnchor() (right, y int) {
	hx, hy, _, _ := r.panel.header.GetInnerRect()
	for _, span := range r.panel.headerSpans {
		if span.action == actionOpenConnectionMenu {
			return hx + span.end, hy + 1
		}
	}
	return hx, hy + 1
}

// renderConnectionMenu (re)builds connectionMenuTable fresh against
// r.panel — the active panel at the moment this runs — every time it's
// opened, the same "rebuilt fresh, not kept in sync incrementally"
// shape renderFilterMenu/openTabSwitcher already establish for their
// own dropdowns: which panel is "active", and what history says, can
// both have changed since last time.
//
// "+ New connection" is a trailing row, after every history entry —
// the same position tabswitcher.go's own "New tab" row occupies at the
// bottom of that dropdown (see tabSwitcherNewRowLabel's own doc
// comment), per the user's own explicit request that the two read as
// the same kind of row in the same kind of place, not just the same
// label shape. History (the thing actually used most often, and what
// this dropdown mainly exists to show) reads first as a result,
// "New connection" last as the deliberate fallback it is.
func (r *Root) renderConnectionMenu() {
	panel := r.panel
	table := r.connectionMenuTable
	table.Clear()
	r.connectionMenuHistoryRows = map[int]remotefs.Connection{}
	r.connectionMenuActiveRow = -1

	// A read failure here just means an empty history section, not an
	// error worth surfacing through a dropdown menu — the same
	// "absence isn't an error" contract LoadHistory's own doc comment
	// already promises callers.
	history, _ := remotefs.LoadHistory()
	row := 0
	for i, entry := range history {
		if i >= connectionMenuMaxHistoryRows {
			break
		}
		entry := entry
		thisRow := row
		isActive := panel.remote != nil && panel.remoteConn.Equal(entry.Connection)

		table.SetCell(thisRow, connectionMenuColLabel,
			tview.NewTableCell(entry.Label()).
				SetTextColor(connectionHistoryColor(r.theme, isActive, entry.LastFailed)).
				SetSelectable(true).
				SetClickedFunc(r.clickConnectionMenuCell(thisRow, connectionMenuColLabel)))

		if isActive {
			table.SetCell(thisRow, connectionMenuColEject,
				tview.NewTableCell(" "+connectionHistoryEjectGlyph+" ").
					SetTextColor(r.theme.MutedTextColor).
					SetSelectable(true).
					SetClickedFunc(r.clickConnectionMenuCell(thisRow, connectionMenuColEject)))
			r.connectionMenuActiveRow = thisRow
		} else {
			table.SetCell(thisRow, connectionMenuColEject,
				tview.NewTableCell(" "+connectionHistoryEditGlyph+" ").
					SetTextColor(r.theme.MutedTextColor).
					SetSelectable(true).
					SetClickedFunc(r.clickConnectionMenuCell(thisRow, connectionMenuColEject)))
		}

		table.SetCell(thisRow, connectionMenuColRemove,
			tview.NewTableCell(" "+connectionHistoryRemoveGlyph+" ").
				SetTextColor(r.theme.MutedTextColor).
				SetSelectable(true).
				SetClickedFunc(r.clickConnectionMenuCell(thisRow, connectionMenuColRemove)))

		r.connectionMenuHistoryRows[thisRow] = entry.Connection
		row++
	}

	newRow := row
	table.SetCell(newRow, connectionMenuColLabel,
		tview.NewTableCell(connectionMenuNewConnectionLabel).
			SetTextColor(r.theme.Text).
			SetSelectable(true).
			SetClickedFunc(r.clickConnectionMenuCell(newRow, connectionMenuColLabel)))
	table.SetCell(newRow, connectionMenuColEject, blankConnectionMenuCell())
	table.SetCell(newRow, connectionMenuColRemove, blankConnectionMenuCell())

	// Default selection mirrors openTabSwitcher's own
	// r.openTabSwitcher(r.activeTab): the active connection's own row
	// if this panel has one, row 0 otherwise — never the trailing "New
	// connection" row by default, the same "land on whatever's already
	// relevant, not on the fallback action" the tab switcher already
	// establishes, per the user's own explicit request that the two
	// dropdowns behave the same way here too.
	initialRow := 0
	if r.connectionMenuActiveRow >= 0 {
		initialRow = r.connectionMenuActiveRow
	}
	table.Select(initialRow, connectionMenuColLabel)
}

// newConnectionMenuTable builds the dropdown's own Table — no border,
// one column of side padding, cell selection so the eject/remove
// buttons in a row are their own navigable target, and
// SetSelectedFunc/SetInputCapture wired once here rather than on every
// renderConnectionMenu call. The identical shape newTabSwitcher already
// establishes, for the identical reason (see this file's own doc
// comment on the column constants).
func (r *Root) newConnectionMenuTable() *tview.Table {
	table := tview.NewTable()
	table.SetBorders(false)
	table.SetBorderPadding(0, 0, 1, 1)
	table.SetSelectable(true, true)
	table.SetInputCapture(r.captureConnectionMenuKey)
	table.SetSelectedFunc(func(row, column int) { r.activateConnectionMenuCell(row, column) })
	return table
}

func (r *Root) newConnectionMenuLayout() *tview.Flex {
	return tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(r.connectionMenuTitleBar, 1, 0, false).
		AddItem(r.connectionMenuTable, 0, 1, true)
}

// blankConnectionMenuCell fills a column that this particular row has
// nothing for (the "New connection…" row's own eject/remove cells, or
// an ordinary history row's eject cell) — a single non-selectable
// space, the same pattern openTabSwitcher uses for the first tab's own
// missing close button, so the column stays present and tview simply
// steps over it when moving the selection.
func blankConnectionMenuCell() *tview.TableCell {
	return tview.NewTableCell(" ").SetSelectable(false)
}

// activateConnectionMenuCell is Enter, Space or a click on one cell —
// the identical dispatch-by-column shape activateTabSwitcherCell
// already establishes. The trailing "New connection…" row (see
// renderConnectionMenu) is always whichever row isn't in
// connectionMenuHistoryRows at all — the only other kind of row this
// table ever has — regardless of column: it has nothing in its own
// eject/remove cells to tell apart in the first place.
// connectionMenuColEject carries two different actions depending on
// the row, not one — see connectionHistoryEditGlyph's own doc comment
// for why the same column position works for both without ever being
// ambiguous.
func (r *Root) activateConnectionMenuCell(row, column int) {
	conn, ok := r.connectionMenuHistoryRows[row]
	if !ok {
		r.hideOverlay()
		r.openConnectDialog(remotefs.Connection{})
		return
	}
	switch column {
	case connectionMenuColEject:
		if row == r.connectionMenuActiveRow {
			r.disconnectConnectionRow(row)
		} else {
			r.editConnectionHistoryRow(conn)
		}
	case connectionMenuColRemove:
		r.removeConnectionHistoryRow(row)
	default:
		r.hideOverlay()
		r.openConnectDialog(conn)
		r.runConnect()
	}
}

// editConnectionHistoryRow opens the Connect dialog prefilled from
// conn, the same as reconnecting to a history row does, but without
// immediately dialing it the way clicking the row's own label does —
// lets the user review or fix a saved entry's Host/Port/User (a typo,
// a since-changed port) before actually attempting a connection with
// it, rather than only ever being able to retype it from scratch as a
// brand-new "New connection…" or fire off an attempt with whatever the
// history already has.
func (r *Root) editConnectionHistoryRow(conn remotefs.Connection) {
	r.hideOverlay()
	r.openConnectDialog(conn)
}

// clickConnectionMenuCell is one cell's own mouse action — the same
// thing Enter and Space do on it. Per cell rather than a table-wide
// mouse capture, because tview's Table does not run a row's selected
// function on a click at all — it only moves the selection there (see
// clickTabSwitcherCell's own doc comment, which found and documented
// this exact tview behavior first). Returns true so tview does not
// also move the selection afterwards: every one of these actions
// rebuilds or closes the table underneath, so the row/column it would
// select next means something else, or nothing at all.
func (r *Root) clickConnectionMenuCell(row, column int) func() bool {
	return func() bool {
		r.activateConnectionMenuCell(row, column)
		return true
	}
}

// captureConnectionMenuKey adds "x"/Delete as a from-anywhere-in-the-row
// keyboard equivalent of clicking a history row's own "✕" (see
// captureTabSwitcherKey's identical Delete handling for the tab
// switcher's own close button), and "e" as the same for whichever of
// "⏏"/"✎" the currently selected row actually carries — eject on the
// one active row, edit on every other history row (see
// connectionHistoryEditGlyph's own doc comment). Escape closes the
// dropdown outright: a Table has no DoneFunc of its own, unlike the
// List this replaced (see captureTabSwitcherKey's own doc comment on
// the identical gap), and Space activates whichever cell currently has
// the selection, since tview's Table only wires that natively to
// Enter.
func (r *Root) captureConnectionMenuKey(event *tcell.EventKey) *tcell.EventKey {
	row, column := r.connectionMenuTable.GetSelection()

	switch {
	case event.Key() == tcell.KeyEscape:
		r.hideOverlay()
		return nil
	case event.Key() == tcell.KeyRune && event.Rune() == ' ':
		r.activateConnectionMenuCell(row, column)
		return nil
	}

	isRemoveKey := (event.Key() == tcell.KeyRune && event.Rune() == 'x') || event.Key() == tcell.KeyDelete
	if isRemoveKey && r.removeConnectionHistoryRow(row) {
		return nil
	}

	isEjectKey := event.Key() == tcell.KeyRune && event.Rune() == 'e'
	if isEjectKey {
		if r.disconnectConnectionRow(row) {
			return nil
		}
		if conn, ok := r.connectionMenuHistoryRows[row]; ok {
			r.editConnectionHistoryRow(conn)
			return nil
		}
	}

	return event
}

// removeConnectionHistoryRow drops row's own Connection out of the
// persisted history (see remotefs.RemoveFromHistory) and re-renders
// the dropdown in place, still open, so removing several entries in a
// row doesn't mean reopening the menu each time. Reports whether row
// actually was a history row at all — false for "New connection…" or
// an out-of-range index, so callers can tell "nothing to remove" apart
// from "removed, nothing more to do".
func (r *Root) removeConnectionHistoryRow(row int) bool {
	conn, ok := r.connectionMenuHistoryRows[row]
	if !ok {
		return false
	}
	r.showError(remotefs.RemoveFromHistory(conn))
	r.renderConnectionMenu()

	// The dropdown's own box was sized to the old row count in
	// openConnectionMenu — one fewer row now, so it needs the same
	// resize that a fresh open would give it, or removing an entry
	// would leave dead space where that row used to be.
	right, top := r.connectionMenuAnchor()
	width, height := r.connectionMenuSize()
	x, y, width, height := r.clampToPanel(right-width, top, width, height)
	r.connectionMenuLayout.SetRect(x, y, width, height)
	return true
}

// disconnectConnectionRow closes the active panel's own remote
// connection and dismisses the whole dropdown, mirroring what used to
// be a dedicated, always-present "Disconnect (...)" list item — the
// user asked for a per-row glyph in its place instead, the same "⏏"
// shape connectionHistoryRemoveGlyph's own "✕" already established,
// rather than a row of its own that's only ever relevant for exactly
// one entry. Reports whether row actually was the active connection's
// own row at all — false otherwise, so a stray "e" elsewhere in the
// table is left for its native handling instead of being swallowed.
func (r *Root) disconnectConnectionRow(row int) bool {
	if row < 0 || row != r.connectionMenuActiveRow {
		return false
	}
	label := r.panel.remoteConn.Label()
	r.hideOverlay()
	if err := r.panel.disconnectRemote(); err != nil {
		r.activityLog.Error(activitylog.CategoryRemote, fmt.Sprintf("disconnect from %s: %v", label, err))
		r.showError(err)
	} else {
		r.activityLog.Action(activitylog.CategoryRemote, fmt.Sprintf("disconnected from %s", label))
	}
	return true
}

// connectionMenuSize is the dropdown's own size: wide enough for its
// widest row plus both action columns, tall enough for every row plus
// the title bar. The identical per-column measurement
// tabSwitcherSize's own doc comment explains (summing a single row's
// own cells under-counts whenever the longest label and an action
// glyph sit on different rows, which is the normal case here too).
func (r *Root) connectionMenuSize() (width, height int) {
	table := r.connectionMenuTable
	rows := table.GetRowCount()

	columnWidths := make([]int, connectionMenuColRemove+1)
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
			width++ // the single column tview leaves between cells
		}
	}

	// +2 for the table's own left/right border padding.
	width += 2
	// +1 for the title bar's own row (see connectionMenuLayout).
	return width, rows + 1
}

// connectionHistorySuccessBlend blends theme.EntryExecutable toward
// theme.MutedTextColor for a history entry that connected successfully
// last time but isn't the one this panel is attached to right now —
// still unambiguously green (never falling back to the dropdown's own
// plain, uncolored text, which read as "unknown/never tried" rather
// than "this one works"), just a matte, muted shade next to the
// currently active entry's own full-brightness green, per the user's
// own explicit report that a cleanly closed connection showing in
// plain white was indistinguishable from one that had simply never
// been tried.
//
// Toward the theme's own muted gray, not toward black: two earlier
// attempts (0.45, then 0.22, both toward black) still read as too
// dark rather than matte — darkening a color and desaturating it are
// different operations, and "matte" specifically asked for the
// second, which blending toward a mid-brightness gray delivers without
// also dimming it the way black inevitably does.
const connectionHistorySuccessBlend = 0.5

// connectionHistoryRemoveGlyph is the small "✕" every history row's
// own remove cell carries (see renderConnectionMenu), per the user's
// own explicit request for a visible, individually clickable way to
// drop one entry out of history without connecting to it — "x" the
// character itself is reserved for typing into a real name, so a
// distinct glyph avoids any chance of the two being confused, the same
// reasoning connectionButtonGlyph's own doc comment gives for picking
// one character over another.
const connectionHistoryRemoveGlyph = "✕"

// connectionHistoryEjectGlyph fills the eject cell only on the one
// history row that is the active panel's own current connection (see
// connectionMenuActiveRow) — clicking it, or pressing "e" while that
// row is highlighted (see captureConnectionMenuKey), disconnects.
// Replaces what used to be a separate, always-present "Disconnect
// (...)" list item of its own — the user asked for a per-row button in
// its place instead, the international "eject media" symbol reading
// naturally as "detach from this" the same way a USB drive's own eject
// icon does.
const connectionHistoryEjectGlyph = "⏏"

// connectionHistoryEditGlyph fills the same column position as
// connectionHistoryEjectGlyph, on every history row that is *not* the
// active connection — the two are mutually exclusive per row (see
// renderConnectionMenu), so one column comfortably carries both
// without ever needing a column of its own. Clicking it, or pressing
// "e" while that row is highlighted (see captureConnectionMenuKey and
// editConnectionHistoryRow), opens the Connect dialog prefilled from
// that entry instead of dialing it immediately the way clicking the
// row's own label does — per the user's own explicit request for a
// way to fix a saved entry's Host/Port/User before reconnecting,
// rather than only being able to retype it from scratch. A thin
// pencil outline, matching the eject glyph's own hollow-line weight
// rather than a filled one, and unambiguous next to "✕": the two read
// as clearly different actions at a glance even though both use a
// single stroke-based symbol.
const connectionHistoryEditGlyph = "✎"

// connectionHistoryColor picks entry's own label color by state —
// green (theme.EntryExecutable, this app's established "healthy/
// active" color — see bottombar.go/gitstatus.go) if it's the panel's
// own currently active connection, red (theme.CriticalText) if the
// last attempt to reach it failed, a dimmer green (see
// connectionHistorySuccessBlend) otherwise — per the user's own
// explicit request that the current connection, any failed one, and
// any merely-inactive-but-working one all be visually distinguishable
// from each other at a glance. A real tcell.Color set directly on the
// cell (TableCell.SetTextColor), not a markup tag embedded in its own
// text — the same "the widget's own API carries color, not the
// string" shape tabSwitcherRowLabel's plain dim/undimmed split already
// uses, now needed here for a three-way split instead of two.
func connectionHistoryColor(theme config.ResolvedTheme, isActive, lastFailed bool) tcell.Color {
	switch {
	case isActive:
		return theme.EntryExecutable
	case lastFailed:
		return theme.CriticalText
	default:
		return blendToward(theme.EntryExecutable, theme.MutedTextColor, connectionHistorySuccessBlend)
	}
}
