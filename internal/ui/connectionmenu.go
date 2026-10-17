package ui

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

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

// connectionMenuNewConnectionLabel is row 0's own label in both
// collapsed and expanded mode — short deliberately, not the
// "+  New connection" shape tabswitcher.go's own tabSwitcherNewRowLabel
// uses for "New tab" (sshShellMenuNewConnectionLabel below keeps that
// full phrase, for a dropdown that never had this problem). This one
// sits in the User column real usernames occupy, so anything longer
// than a real username drove that column's own width every single
// time the dropdown opened — a real, user-reported regression three
// times over: the full phrase, an expanded-only SetExpansion attempt,
// and a User/Host-IP split that "looked like Mist" per the user's own
// words. Short and in one cell is what's left once all three
// alternatives were tried and reverted.
const connectionMenuNewConnectionLabel = " +  New"

// sshShellMenuNewConnectionLabel is the "gs" SSH shell dropdown's own
// equivalent row (see sshshell.go) — a single label column there, not
// User/Host/@ split the way the "@"/"gc" dropdown above now is, so the
// full phrase never risked distorting anything and stays as one cell.
const sshShellMenuNewConnectionLabel = " +  New connection"

// Column indices within the connection menu table — the same in both
// collapsed and expanded mode (see r.settings.ConnectionMenuExpanded):
// User | @ | Host/IP in both, expanded mode's only difference is
// Identifier inserted right after Host/IP, shifting Eject/Remove one
// column further right (see connectionMenuEjectColumn/
// connectionMenuRemoveColumn). Per the user's own explicit request,
// collapsed is a real column table too now, not single merged "user@
// host" string — just missing the one column (Identifier) expanded
// mode adds.
//
// Only connectionMenuColUser is ever selectable — @/Host-IP/Identifier
// each still carry their own SetClickedFunc (see renderConnectionMenu),
// so clicking anywhere across the whole label group still connects
// (tview's own Table.MouseHandler calls a cell's Clicked() regardless
// of its own Selectable flag, verified against table.go, not guessed),
// but Left/Right keyboard navigation skips straight over them to the
// row's other two real stops, ✎ and ✕ — exactly the "the whole label
// group is one stop, I don't need to navigate its own pieces
// separately" behavior the user asked for, since every one of those
// cells does the exact same thing (connect) regardless of which one
// was actually activated.
const (
	connectionMenuColUser = iota
	connectionMenuColAt
	connectionMenuColHost
	connectionMenuColIdentifier // present only when expanded
)

// connectionMenuEjectColumn/connectionMenuRemoveColumn resolve the
// eject/edit and remove columns' own index for the given expand state
// — the one place activateConnectionMenuCell and renderConnectionMenu
// both go through, so a click on either glyph is routed by the layout
// actually on screen: expanded mode has one more label column
// (Identifier) ahead of them than collapsed mode does, shifting both
// one column to the right.
func connectionMenuEjectColumn(expanded bool) int {
	if expanded {
		return connectionMenuColIdentifier + 1
	}
	return connectionMenuColHost + 1
}

func connectionMenuRemoveColumn(expanded bool) int {
	return connectionMenuEjectColumn(expanded) + 1
}

// connectionMenuSortKey identifies which column, if any, the dropdown's
// own history rows are currently sorted by — reset to
// connectionMenuSortNone every time the dropdown opens (see
// openConnectionMenu), per the plan's own "transient, not persisted"
// decision (feature_ideas.txt section 9a): sorting resets to
// LoadHistory's own "most recently used first" order on the next open,
// the same way renderConnectionMenu itself is already rebuilt fresh on
// every open rather than kept in sync incrementally.
type connectionMenuSortKey int

const (
	connectionMenuSortNone connectionMenuSortKey = iota
	connectionMenuSortUser
	connectionMenuSortHost
	connectionMenuSortIdentifier
)

// connectionMenuSortHistory sorts history in place by key — stable, so
// entries that compare equal (most often: no User, or no Identifier)
// keep LoadHistory's own most-recently-used order rather than an
// arbitrary one. connectionMenuSortNone leaves history untouched,
// since LoadHistory's own order already is "most recent first".
func connectionMenuSortHistory(history []remotefs.HistoryEntry, key connectionMenuSortKey, descending bool) {
	if key == connectionMenuSortNone {
		return
	}
	less := func(i, j int) bool {
		switch key {
		case connectionMenuSortUser:
			return history[i].User < history[j].User
		case connectionMenuSortHost:
			return history[i].HostPort() < history[j].HostPort()
		case connectionMenuSortIdentifier:
			return history[i].Identifier < history[j].Identifier
		}
		return false
	}
	sort.SliceStable(history, func(i, j int) bool {
		if descending {
			return less(j, i)
		}
		return less(i, j)
	})
}

// setConnectionMenuSortKey is what clicking a column header (or its own
// U/H/I keyboard equivalent — see captureConnectionMenuKey) does:
// switch to sorting by key, ascending — or, if key is already the
// active one, flip direction instead. The same "click a new column:
// ascending; click the active one again: reverse" convention
// Panel.setSortKey already establishes for the file panel's own column
// headers (panel.go:2976). A no-op for Identifier while collapsed:
// there's no Identifier column to sort by at all then (see that
// column's own const block doc comment).
func (r *Root) setConnectionMenuSortKey(key connectionMenuSortKey) {
	if key == connectionMenuSortIdentifier && !r.settings.ConnectionMenuExpanded {
		return
	}
	if r.connectionMenuSortKey == key {
		r.connectionMenuSortDescending = !r.connectionMenuSortDescending
	} else {
		r.connectionMenuSortKey = key
		r.connectionMenuSortDescending = false
	}
	r.renderConnectionMenu()
	r.positionConnectionMenu()
}

// openConnectionMenu shows the connection dropdown for the currently
// active panel — reached via a click on the header's own "@" button
// (see buildHeaderSpans/Panel.onOpenConnectionMenu) or the "gc" chord
// (see keymap.go). Anchored under that button, the same "appears right
// below what it drops down from" placement openFilterMenu establishes
// for its own dropdown, and openTabSwitcher for its own.
func (r *Root) openConnectionMenu() {
	// Sorting is transient per open (see connectionMenuSortKey's own
	// doc comment) — reset here, every time, rather than only once at
	// NewRoot, so a previous open's own sort never leaks into this one.
	r.connectionMenuSortKey = connectionMenuSortNone
	r.connectionMenuSortDescending = false
	r.renderConnectionMenu()
	r.positionConnectionMenu()
	// connectionMenuLayout (title bar + table), not just the table —
	// unlike openTabSwitcher's own identical-looking split, this title
	// bar now has its own clickable expand/collapse glyph (see
	// renderConnectionMenuTitleBar), and r.activeWidget is what
	// captureOutsideClick's own primitiveContains check uses to decide
	// "did this click land on the open overlay" — passing just the
	// table there excludes the title bar's own row entirely, so a
	// click on the toggle glyph read as "outside the overlay" and
	// closed the whole dropdown before the title bar's own
	// captureConnectionMenuTitleBarMouse ever ran (a real, user-
	// reported bug). Keyboard focus still lands on the table either
	// way: tview's own Flex.Focus delegates to whichever item was
	// added with Focus=true (connectionMenuTable, see
	// newConnectionMenuLayout) regardless of which ancestor
	// Application.SetFocus was actually called on — verified directly
	// against flex.go/application.go, not guessed.
	r.showOverlay(connectionMenuPage, r.connectionMenuLayout)
}

// positionConnectionMenu sizes and places connectionMenuLayout against
// its own current collapsed/expanded state — the one place
// openConnectionMenu and removeConnectionHistoryRow both go through
// (see its own doc comment there), so a row disappearing while
// expanded can never snap the dropdown back to collapsed-mode sizing
// just because only one of the two call sites learned about expanded
// mode.
//
// Collapsed: unchanged from before expanded mode existed —
// clampToPanel, anchored under the "@" button, never wider than the
// active panel. Expanded: clampToScreen instead (root.go's own
// "orient against the whole view instead of the active panel" helper,
// not guessed — see its own doc comment), x pinned to 0 and width to
// the full screen rather than right-width, since the point is
// deliberately to span the entire terminal, not just whatever room is
// left to the button's own left. y still comes from
// connectionMenuAnchor (already an absolute screen position via
// GetInnerRect, same in both branches) so the dropdown still opens at
// the same row either way, just wider when expanded.
func (r *Root) positionConnectionMenu() {
	right, top := r.connectionMenuAnchor()
	width, height := r.connectionMenuSize()
	var x, y int
	if r.settings.ConnectionMenuExpanded {
		x, y, width, height = r.clampToScreen(0, top, width, height)
	} else {
		x, y, width, height = r.clampToPanel(right-width, top, width, height)
	}
	r.connectionMenuLayout.SetRect(x, y, width, height)
	r.renderConnectionMenuTitleBar(width)
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
	r.connectionMenuRowColors = map[int]tcell.Color{}
	r.connectionMenuActiveRow = -1
	expanded := r.settings.ConnectionMenuExpanded
	ejectCol, removeCol := connectionMenuEjectColumn(expanded), connectionMenuRemoveColumn(expanded)
	// labelCol is wherever a click/Enter on a non-action cell should
	// land focus by default (table.Select below), and the only
	// selectable column within the User/@/Host-IP/Identifier label
	// group (see the column constants' own doc comment) — the
	// trailing "+ New connection" row's own clickable cell uses it too.
	labelCol := connectionMenuColUser

	// Row 0 is the column header — click (or its own U/H/I keyboard
	// equivalent, see captureConnectionMenuKey) sorts by that column;
	// still never keyboard-*navigable* (SetSelectable(false) on every
	// cell) the same way Panel's own file-listing header
	// (panel.go:908) is mouse-click-only too — sorting through table
	// navigation would reopen the row-kind-discriminator problem a
	// selectable header row would otherwise need. Shifts every history
	// row down by one, which is why the loop below starts at 1, not 0.
	r.connectionMenuSetHeaderCell(connectionMenuColUser, "User", connectionMenuSortUser)
	r.connectionMenuSetHeaderCell(connectionMenuColAt, "", connectionMenuSortNone)
	r.connectionMenuSetHeaderCell(connectionMenuColHost, "Host/IP", connectionMenuSortHost)
	if expanded {
		r.connectionMenuSetHeaderCell(connectionMenuColIdentifier, "Identifier", connectionMenuSortIdentifier)
	}
	r.connectionMenuSetHeaderCell(ejectCol, "", connectionMenuSortNone)
	r.connectionMenuSetHeaderCell(removeCol, "", connectionMenuSortNone)
	r.connectionMenuRowColors[0] = r.theme.MutedTextColor

	// A read failure here just means an empty history section, not an
	// error worth surfacing through a dropdown menu — the same
	// "absence isn't an error" contract LoadHistory's own doc comment
	// already promises callers.
	history, _ := remotefs.LoadHistory()
	connectionMenuSortHistory(history, r.connectionMenuSortKey, r.connectionMenuSortDescending)
	row := 1
	for i, entry := range history {
		if i >= connectionMenuMaxHistoryRows {
			break
		}
		entry := entry
		thisRow := row
		isActive := panel.remote != nil && panel.remoteConn.Equal(entry.Connection)
		color := connectionHistoryColor(r.theme, isActive, entry.LastFailed)

		// User is the label group's own one keyboard-selectable stop;
		// @/Host-IP (and Identifier, expanded only) each still carry
		// their own SetClickedFunc, so a mouse click anywhere across
		// the whole group still connects — see the column constants'
		// own doc comment for why only User is selectable.
		table.SetCell(thisRow, connectionMenuColUser,
			tview.NewTableCell(entry.User).
				SetTextColor(color).
				SetSelectable(true).
				SetClickedFunc(r.clickConnectionMenuCell(thisRow, connectionMenuColUser)))
		table.SetCell(thisRow, connectionMenuColAt,
			tview.NewTableCell(connectionMenuAtColumnText(entry.Connection)).
				SetTextColor(color).
				SetSelectable(false).
				SetClickedFunc(r.clickConnectionMenuCell(thisRow, connectionMenuColAt)))
		table.SetCell(thisRow, connectionMenuColHost,
			tview.NewTableCell(entry.HostPort()).
				SetTextColor(color).
				SetSelectable(false).
				SetClickedFunc(r.clickConnectionMenuCell(thisRow, connectionMenuColHost)))
		if expanded {
			table.SetCell(thisRow, connectionMenuColIdentifier,
				tview.NewTableCell(entry.Identifier).
					SetTextColor(color).
					SetSelectable(false).
					SetClickedFunc(r.clickConnectionMenuCell(thisRow, connectionMenuColIdentifier)))
		}

		if isActive {
			table.SetCell(thisRow, ejectCol,
				// Three leading spaces, not one — the only gap between
				// the label group and the action columns, so it needs
				// to read as a deliberate break, not just the ordinary
				// one-column inter-cell gap every other column pair
				// already gets (the user's own explicit report that it
				// looked like no gap at all).
				tview.NewTableCell("   "+connectionHistoryEjectGlyph+" ").
					SetTextColor(r.theme.MutedTextColor).
					SetSelectable(true).
					SetClickedFunc(r.clickConnectionMenuCell(thisRow, ejectCol)))
			r.connectionMenuActiveRow = thisRow
		} else {
			table.SetCell(thisRow, ejectCol,
				tview.NewTableCell("   "+connectionHistoryEditGlyph+" ").
					SetTextColor(r.theme.MutedTextColor).
					SetSelectable(true).
					SetClickedFunc(r.clickConnectionMenuCell(thisRow, ejectCol)))
		}

		table.SetCell(thisRow, removeCol,
			tview.NewTableCell(" "+connectionHistoryRemoveGlyph+" ").
				SetTextColor(r.theme.MutedTextColor).
				SetSelectable(true).
				SetClickedFunc(r.clickConnectionMenuCell(thisRow, removeCol)))

		r.connectionMenuHistoryRows[thisRow] = entry.Connection
		r.connectionMenuRowColors[thisRow] = color
		row++
	}

	newRow := row
	// Back to one short cell in User ("+  New") — the split across
	// User/Host-IP ("+" / "New connection") was tried and reverted
	// (the user's own explicit report: "sieht Mist aus"). Still short
	// deliberately, not the full "+  New connection" phrase
	// tabswitcher.go's own tabSwitcherNewRowLabel uses: that's what
	// drove the User column's own width every time the dropdown
	// opened, in both collapsed and expanded mode, before this existed
	// (see this row's own earlier history in connectionmenu.go's git
	// log for the two separate regressions this already caused).
	table.SetCell(newRow, connectionMenuColUser,
		tview.NewTableCell(connectionMenuNewConnectionLabel).
			SetTextColor(r.theme.Text).
			SetSelectable(true).
			SetClickedFunc(r.clickConnectionMenuCell(newRow, labelCol)))
	r.connectionMenuRowColors[newRow] = r.theme.Text
	table.SetCell(newRow, connectionMenuColAt, blankConnectionMenuCell())
	table.SetCell(newRow, connectionMenuColHost, blankConnectionMenuCell())
	if expanded {
		table.SetCell(newRow, connectionMenuColIdentifier, blankConnectionMenuCell())
	}
	table.SetCell(newRow, ejectCol, blankConnectionMenuCell())
	table.SetCell(newRow, removeCol, blankConnectionMenuCell())

	// Default selection mirrors openTabSwitcher's own
	// r.openTabSwitcher(r.activeTab): the active connection's own row
	// if this panel has one, row 1 (the first real row, after the
	// inert header at row 0) otherwise — never the trailing "New
	// connection" row by default, the same "land on whatever's already
	// relevant, not on the fallback action" the tab switcher already
	// establishes, per the user's own explicit request that the two
	// dropdowns behave the same way here too.
	initialRow := 1
	if r.connectionMenuActiveRow >= 0 {
		initialRow = r.connectionMenuActiveRow
	}
	table.Select(initialRow, labelCol)
}

// connectionMenuAtColumnText renders the expanded column table's own
// "@" column: a literal "@" when conn has a User to join to its own
// Host column, "" otherwise — mirroring the exact condition
// Connection.Label() already uses for whether to show "@" at all, just
// pulled out into its own column instead of inline punctuation.
func connectionMenuAtColumnText(conn remotefs.Connection) string {
	if conn.User == "" {
		return ""
	}
	return "@"
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
	// SetSelectionChangedFunc is what makes the label group
	// (User/@/Host-IP/Identifier) read as one visually highlighted unit
	// — see connectionMenuRepaintLabelGroups' own doc comment for why
	// this can't just be tview's native per-cell highlight.
	table.SetSelectionChangedFunc(func(row, column int) { r.connectionMenuRepaintLabelGroups(row, column) })
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

// connectionMenuSetHeaderCell writes label into table's own row 0 at
// col — inert for now (see renderConnectionMenu's own doc comment on
// why row 0 doesn't yet participate in keyboard/click dispatch the way
// every row below it does). SetSelectable(false) alone does NOT
// guarantee that: tview's own Table.MouseHandler calls t.Select(row,
// column) on any clicked cell whose own Clicked func is nil,
// completely ignoring NotSelectable (verified directly against
// table.go, not guessed — NotSelectable only gates the keyboard
// forward/backward scan) — a real, user-reported bug where clicking a
// header cell moved the real selection onto the header, and
// connectionMenuRepaintLabelGroups then dutifully highlighted it. The
// explicit SetClickedFunc returning true is what actually prevents
// that (true means "noSelect", skipping tview's own t.Select call
// entirely) — once Phase 3 step 2 makes headers sortable, this same
// handler becomes the sort trigger, still returning true.
// connectionMenuSetHeaderCell writes label into table's own row 0 at
// col, plus sortArrow's own " ↑"/" ↓" suffix (panel.go:2831, reused
// directly rather than duplicated) when key is the currently active
// sort column. SetSelectable(false) always — see renderConnectionMenu's
// own doc comment at its call site for why the header stays
// click-only, never keyboard-navigable. key ==
// connectionMenuSortNone is for the @/Eject/Remove columns, which
// never sort anything: their own SetClickedFunc still has to return
// true (tview's own "noSelect" signal — see this method's own doc
// comment where it was first added) so a stray click there can't move
// the real selection onto the header either.
func (r *Root) connectionMenuSetHeaderCell(col int, label string, key connectionMenuSortKey) {
	text := label
	if key != connectionMenuSortNone {
		// The mnemonic letter (U/H/I — always label's own first rune,
		// since User/Host/Identifier were named with this in mind) gets
		// the same ButtonBackground chip bottombar.go's own
		// highlightKey gives every other keyboard shortcut in this app,
		// per the user's own explicit request that this header read the
		// same way. Colored in place rather than prefixed the way
		// highlightKey itself does it (" U " ahead of the full word):
		// this column is already as narrow as "User"/"Host/IP"/
		// "Identifier" themselves, and a separate prefix chip would
		// both widen it and read as a redundant repeat of the letter
		// right there in the word.
		text = fmt.Sprintf("[:%s:]%c[-:-:-]%s", colorTag(r.theme.ButtonBackground), label[0], label[1:])
		if key == r.connectionMenuSortKey {
			text += sortArrow(r.connectionMenuSortDescending)
		}
	}
	cell := tview.NewTableCell(text).
		SetTextColor(r.theme.MutedTextColor).
		SetSelectable(false)
	if key == connectionMenuSortNone {
		cell.SetClickedFunc(func() bool { return true })
	} else {
		cell.SetClickedFunc(func() bool {
			r.setConnectionMenuSortKey(key)
			return true
		})
	}
	r.connectionMenuTable.SetCell(0, col, cell)
}

// activateConnectionMenuCell is Enter, Space or a click on one cell —
// the identical dispatch-by-column shape activateTabSwitcherCell
// already establishes. The trailing "New connection…" row (see
// renderConnectionMenu) is always whichever row isn't in
// connectionMenuHistoryRows at all — the only other kind of row this
// table ever has — regardless of column: it has nothing in its own
// eject/remove cells to tell apart in the first place.
// connectionMenuEjectColumn's own column carries two different actions
// depending on the row, not one — see connectionHistoryEditGlyph's own
// doc comment for why the same column position works for both without
// ever being ambiguous. Resolved per r.settings.ConnectionMenuExpanded
// rather than compared against the collapsed-mode constants directly:
// expanded mode has more columns before Eject/Remove, so their own
// absolute column index shifts (see connectionMenuEjectColumn's own
// doc comment on the bug this would otherwise be).
func (r *Root) activateConnectionMenuCell(row, column int) {
	conn, ok := r.connectionMenuHistoryRows[row]
	if !ok {
		r.hideOverlay()
		r.openConnectDialog(remotefs.Connection{})
		return
	}
	expanded := r.settings.ConnectionMenuExpanded
	switch column {
	case connectionMenuEjectColumn(expanded):
		if row == r.connectionMenuActiveRow {
			r.disconnectConnectionRow(row)
		} else {
			r.editConnectionHistoryRow(conn)
		}
	case connectionMenuRemoveColumn(expanded):
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

	// "<"/">" are the keyboard equivalent of the title bar's own
	// expand/collapse glyph (see renderConnectionMenuTitleBar) — either
	// one flips it, since there's only one button to mirror, not a pair
	// split across two locations the way Details' own open/close
	// buttons are (see that const block's own doc comment on this
	// dropdown's own, simpler convention). Safe to consume unconditionally
	// here: this table has no free-text entry of its own for either
	// character to otherwise mean something else to.
	isToggleKey := event.Key() == tcell.KeyRune && (event.Rune() == '<' || event.Rune() == '>')
	if isToggleKey {
		r.toggleConnectionMenuExpanded()
		return nil
	}

	// U/H/I are the keyboard equivalent of clicking a column header to
	// sort by it (see connectionMenuSetHeaderCell) — the header itself
	// is deliberately not keyboard-navigable (see renderConnectionMenu's
	// own doc comment), so this is the only way to sort without a
	// mouse, the same reasoning Panel's own "z" then N/S/T gives for
	// its file-listing header (panel.go:580). Capitalized specifically,
	// mirroring Panel's own N/S/T: lowercase u/h/i stay free for Phase
	// 5's filter field, which will want ordinary letters once it
	// exists. setConnectionMenuSortKey itself is a no-op for 'I' while
	// collapsed (no Identifier column to sort by), so nothing special
	// is needed here for that case.
	if event.Key() == tcell.KeyRune {
		switch event.Rune() {
		case 'U':
			r.setConnectionMenuSortKey(connectionMenuSortUser)
			return nil
		case 'H':
			r.setConnectionMenuSortKey(connectionMenuSortHost)
			return nil
		case 'I':
			r.setConnectionMenuSortKey(connectionMenuSortIdentifier)
			return nil
		}
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
	// would leave dead space where that row used to be. Goes through
	// positionConnectionMenu rather than repeating the anchor/size/
	// clamp sequence inline — see its own doc comment for why this
	// must be the one place both callers agree on, expanded mode
	// included.
	r.positionConnectionMenu()
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
	// +1 for the title bar's own row (see connectionMenuLayout), in
	// both branches below.
	height = rows + 1

	// Expanded mode deliberately spans the whole terminal width (see
	// positionConnectionMenu's own doc comment on why) regardless of
	// actual column content, so there's nothing for a per-column
	// measurement to usefully compute here — screenWidth itself is the
	// answer, and clampToScreen downstream still caps it on a genuinely
	// narrow terminal.
	if r.settings.ConnectionMenuExpanded {
		_, _, screenWidth, _ := r.GetRect()
		return screenWidth, height
	}

	columnWidths := make([]int, connectionMenuRemoveColumn(false)+1)
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
	return width, height
}

// connectionMenuIdentifierSuffix appends conn's own Identifier (see
// remotefs.Connection's own doc comment) to its row's label — "" if
// unset, so an entry nobody ever named looks exactly as it does today.
// Appended here, at this one call site, rather than folded into
// Connection.Label() itself: Label() is deliberately shared with
// activity-log messages, compress/duplicate error text, and the panel
// header's own "@" button (see Label's own doc comment, and
// shellFlagsSuffix's identical reasoning for sshshell.go's own history
// rows), none of which should suddenly start showing a per-row
// Identifier just because the connection dropdown wants to.
func connectionMenuIdentifierSuffix(conn remotefs.Connection) string {
	if conn.Identifier == "" {
		return ""
	}
	return "  (" + conn.Identifier + ")"
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

// connectionMenuExpandGlyph/connectionMenuCollapseGlyph are the
// connection dropdown's own title-bar toggle, right-aligned the same
// one-column-in-from-the-edge spacing toolWindowCloseButtonCol already
// establishes for every other close/collapse button in this app
// (toolWindow's own, Help's own, Details sidebar's own — see
// detailssidebar.go). Unlike Details' own single ">"-while-open
// collapse button (paired with a separate "<"-while-closed expand
// button elsewhere in the panel header, two buttons in two different
// locations), this dropdown only ever has one place for a button at
// all, so the glyph names the action a click performs instead of the
// current state: ">" while collapsed (click to expand into the full
// column table), "<" while expanded (click to collapse back to the
// single-label list).
const (
	connectionMenuExpandGlyph   = ">"
	connectionMenuCollapseGlyph = "<"
)

// renderConnectionMenuTitleBar sets connectionMenuTitleBar's own text
// to "Connections", padded out to width columns, with the expand/
// collapse glyph in its own top-right corner (see the two constants'
// own doc comment above). Called from positionConnectionMenu, so the
// glyph's own column stays correct whenever the dropdown is resized or
// toggled, not just once at open time — the same reasoning
// renderDetailsTitleBar's own doc comment gives for being called from
// repositionDetailsSidebar rather than just once at show time.
func (r *Root) renderConnectionMenuTitleBar(width int) {
	label := " Connections "
	glyph := connectionMenuExpandGlyph
	if r.settings.ConnectionMenuExpanded {
		glyph = connectionMenuCollapseGlyph
	}
	closeCol := toolWindowCloseButtonCol(0, width)
	padding := closeCol - len(label)
	if padding < 0 {
		padding = 0
	}
	r.connectionMenuTitleBar.SetText(label + strings.Repeat(" ", padding) + glyph + " ")
}

// captureConnectionMenuTitleBarMouse toggles expand/collapse when the
// click lands exactly on the glyph's own column — the mouse
// counterpart to "<"/">" in captureConnectionMenuKey, the identical
// hit-test shape captureDetailsTitleBarMouse already establishes for
// its own single collapse button.
func (r *Root) captureConnectionMenuTitleBarMouse(action tview.MouseAction, event *tcell.EventMouse) (tview.MouseAction, *tcell.EventMouse) {
	if !r.connectionMenuTitleBar.InRect(event.Position()) {
		return action, event
	}
	if action != tview.MouseLeftClick {
		return tview.MouseConsumed, nil
	}
	x, y := event.Position()
	rectX, rectY, width, _ := r.connectionMenuTitleBar.GetRect()
	if y != rectY || x != rectX+toolWindowCloseButtonCol(0, width) {
		// A "miss" — every other click on the title bar is inert, same
		// as Details' own (see func's own doc comment) — passed
		// through rather than consumed, same as before this existed:
		// TextView's own default MouseLeftClick handling does nothing
		// here regardless (no region tags on this bar), so nothing this
		// would let slip through by mistake.
		return action, event
	}
	r.toggleConnectionMenuExpanded()
	return tview.MouseConsumed, nil
}

// toggleConnectionMenuExpanded flips the collapsed/expanded preference
// and, if the dropdown is currently open, re-renders and re-positions
// it immediately — the same "flip, persist, apply if currently
// relevant" shape toggleSplitStacked/setSplitStacked already establish
// for an identical kind of stored display preference.
func (r *Root) toggleConnectionMenuExpanded() {
	r.setConnectionMenuExpanded(!r.settings.ConnectionMenuExpanded)
}

// setConnectionMenuExpanded applies a specific collapsed/expanded state
// — toggleConnectionMenuExpanded's own body with the value passed in
// rather than derived by flipping, so the Options screen can set one
// directly through exactly the same path (the same split
// setSplitStacked already makes for its own toggle/Options pair).
func (r *Root) setConnectionMenuExpanded(expanded bool) {
	r.settings.ConnectionMenuExpanded = expanded
	r.persistSetting("connection_menu_expanded", strconv.FormatBool(expanded))
	if r.activePage == connectionMenuPage {
		r.renderConnectionMenu()
		r.positionConnectionMenu()
	}
}

// connectionMenuRepaintLabelGroups is why the label group (User/@/
// Host-IP/Identifier) reads as one highlighted unit instead of just
// the single User cell tview's own selection would otherwise paint:
// tview only highlights the literal (selectedRow, selectedColumn)
// pair (verified against table.go's own Draw — SetSelectable(true,
// true) means neither its rowsSelectable-only nor columnsSelectable-
// only whole-row/-column highlight paths apply), and switching to
// rows-only selectability to get that span natively (the way Panel's
// own file table does it — see panel.go's own
// SetSelectable(true, false)) would cost keyboard access to ✎/✕ as
// separate stops, since rows-only selectability turns Left/Right into
// plain horizontal scrolling instead of column navigation.
//
// So this manually paints every row's own @/Host-IP/Identifier cells
// with the exact same background SetSelectedStyle already gives the
// User cell (theme.SelectionBackground, wired in theme.go's own
// applyTheme) whenever that row is the selected one *and* the
// selection is actually on the label group (column == labelCol, the
// only selectable column within it — see that const block's own doc
// comment) — reverting to the table's own normal background
// (theme.SurfaceBackground, matching connectionMenuTable's own
// SetBackgroundColor in theme.go) everywhere else, including the same
// row when the selection has moved on to ✎ or ✕ instead. Runs for
// every row on every selection change rather than tracking and
// clearing just the previously-painted one — simpler, and this
// dropdown never has more than historyMaxEntries+1 rows to begin with.
func (r *Root) connectionMenuRepaintLabelGroups(selectedRow, selectedColumn int) {
	table := r.connectionMenuTable
	expanded := r.settings.ConnectionMenuExpanded
	groupCols := []int{connectionMenuColAt, connectionMenuColHost}
	if expanded {
		groupCols = append(groupCols, connectionMenuColIdentifier)
	}
	rows := table.GetRowCount()
	for row := 0; row < rows; row++ {
		bg := r.theme.SurfaceBackground
		fg := r.connectionMenuRowColors[row]
		// row == 0 (the header) is never a legitimate highlight target
		// even if selectedRow somehow ends up there — defense in depth
		// alongside connectionMenuSetHeaderCell's own SetClickedFunc
		// guard, which is what actually stops selectedRow from landing
		// there via a click in the first place.
		if row != 0 && row == selectedRow && selectedColumn == connectionMenuColUser {
			bg = r.theme.SelectionBackground
			// theme.TextColor, not fg: the exact foreground
			// connectionMenuTable's own SetSelectedStyle already gives
			// the User cell while selected (theme.go's own applyTheme)
			// — matched here so @/Host-IP/Identifier read with the
			// same white-on-highlight look instead of keeping their
			// own green/red/muted color underneath a highlighted
			// background, which is illegible (the user's own explicit
			// report).
			fg = r.theme.TextColor
		}
		for _, col := range groupCols {
			if cell := table.GetCell(row, col); cell != nil {
				cell.SetBackgroundColor(bg)
				cell.SetTextColor(fg)
			}
		}
	}
}
