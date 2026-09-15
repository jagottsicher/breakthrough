package ui

import (
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

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

// openConnectionMenu shows the connection dropdown for the currently
// active panel — reached via a click on the header's own "@" button
// (see buildHeaderSpans/Panel.onOpenConnectionMenu) or the "gc" chord
// (see keymap.go). Anchored under that button, the same "appears right
// below what it drops down from" placement openFilterMenu already
// establishes for its own dropdown.
func (r *Root) openConnectionMenu() {
	r.renderConnectionMenu()

	right, top := r.connectionMenuAnchor()
	width, height := listSize(r.connectionMenuList)
	x, y, width, height := r.clampToPanel(right-width, top, width, height)
	r.connectionMenuLayout.SetRect(x, y, width, height)
	r.showOverlay(connectionMenuPage, r.connectionMenuLayout)
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

// renderConnectionMenu (re)builds connectionMenuList fresh against
// r.panel — the active panel at the moment this runs — every time it's
// opened, the same "rebuilt fresh, not kept in sync incrementally"
// shape renderFilterMenu already establishes for its own dropdown:
// which panel is "active" can change between opens, and the history
// list on disk can too.
func (r *Root) renderConnectionMenu() {
	panel := r.panel
	r.connectionMenuList.Clear()
	r.connectionMenuHistoryRows = map[int]remotefs.Connection{}
	r.connectionMenuActiveRow = -1

	r.connectionMenuList.AddItem("New connection…", "", 0, func() {
		r.hideOverlay()
		r.openConnectDialog(remotefs.Connection{})
	})

	// A read failure here just means an empty history section, not an
	// error worth surfacing through a dropdown menu — the same
	// "absence isn't an error" contract LoadHistory's own doc comment
	// already promises callers.
	history, _ := remotefs.LoadHistory()
	for i, entry := range history {
		if i >= connectionMenuMaxHistoryRows {
			break
		}
		entry := entry
		r.connectionMenuList.AddItem(r.connectionHistoryLabel(panel, entry), "", 0, func() {
			r.hideOverlay()
			r.openConnectDialog(entry.Connection)
			r.runConnect()
		})
		row := r.connectionMenuList.GetItemCount() - 1
		r.connectionMenuHistoryRows[row] = entry.Connection
		if panel.remote != nil && panel.remoteConn.Equal(entry.Connection) {
			r.connectionMenuActiveRow = row
		}
	}

	l := r.connectionMenuList
	l.SetDoneFunc(r.hideOverlay) // Escape
	l.SetInputCapture(r.captureConnectionMenuKey)
	l.SetMouseCapture(r.captureConnectionMenuMouse)
}

// captureConnectionMenuKey adds "x"/Delete as the keyboard equivalent
// of clicking a history row's own "✕" (see captureConnectionMenuMouse),
// and "e" as the equivalent of clicking its "⏏" — whichever row
// currently has the list's own highlight, per this project's own
// "every mouse action needs a keyboard one" rule. A no-op, not
// consumed, on any row the pressed key doesn't apply to at all (not
// history, or history but not the active connection for "e"), or once
// history is genuinely empty.
func (r *Root) captureConnectionMenuKey(event *tcell.EventKey) *tcell.EventKey {
	current := r.connectionMenuList.GetCurrentItem()

	isRemoveKey := (event.Key() == tcell.KeyRune && event.Rune() == 'x') || event.Key() == tcell.KeyDelete
	if isRemoveKey && r.removeConnectionHistoryRow(current) {
		return nil
	}

	isEjectKey := event.Key() == tcell.KeyRune && event.Rune() == 'e'
	if isEjectKey && r.disconnectConnectionRow(current) {
		return nil
	}

	return event
}

// captureConnectionMenuMouse lets a click land on a history row's own
// trailing "✕" (see connectionHistoryLabel) as "remove this entry", or
// on its "⏏" — present only on the active connection's own row, see
// connectionMenuActiveRow — as "disconnect", instead of the row's own
// default "reconnect" action. Both are checked first, before falling
// through to the list's native click-selects-and-fires handling, the
// same "figure out exactly what was clicked before deciding what it
// means" shape captureHeaderMouse/Panel.filterMenuBtn's own mouse
// captures already use. No scrolling to account for here:
// openConnectionMenu always sizes the dropdown to listSize's own item
// count, so every row is already fully visible and row index == y -
// the list's own inner top, unlike a list that can actually scroll.
func (r *Root) captureConnectionMenuMouse(action tview.MouseAction, event *tcell.EventMouse) (tview.MouseAction, *tcell.EventMouse) {
	if action != tview.MouseLeftClick {
		return action, event
	}
	x, y := event.Position()
	rectX, rectY, _, _ := r.connectionMenuList.GetInnerRect()
	if !r.connectionMenuList.InRect(x, y) {
		return action, event
	}
	row := y - rectY

	if _, ok := r.connectionMenuHistoryRows[row]; !ok {
		return action, event // not a history row at all — let the list handle its own click natively
	}
	mainText, _ := r.connectionMenuList.GetItemText(row)
	textWidth := tview.TaggedStringWidth(mainText)
	removeGlyphWidth := tview.TaggedStringWidth(connectionHistoryRemoveGlyph)
	removeStart := rectX + textWidth - removeGlyphWidth
	if x >= removeStart {
		r.removeConnectionHistoryRow(row)
		return tview.MouseConsumed, nil
	}
	if row == r.connectionMenuActiveRow {
		ejectGlyphWidth := tview.TaggedStringWidth(connectionHistoryEjectGlyph)
		ejectEnd := removeStart - 2 // the "  " gap connectionHistoryLabel puts between the two glyphs
		ejectStart := ejectEnd - ejectGlyphWidth
		if x >= ejectStart && x < ejectEnd {
			r.disconnectConnectionRow(row)
			return tview.MouseConsumed, nil
		}
	}
	return action, event // elsewhere on the row — let it reconnect normally
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

	// The dropdown's own box was sized to the old item count in
	// openConnectionMenu — one fewer row now, so it needs the same
	// resize that a fresh open would give it, or removing an entry
	// would leave dead space where that row used to be.
	right, top := r.connectionMenuAnchor()
	width, height := listSize(r.connectionMenuList)
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
// list (typed while renaming isn't even possible here, but see
// removeConnectionHistoryRow's own "false means nothing to do"
// contract) is left for the list's native handling instead of being
// swallowed.
func (r *Root) disconnectConnectionRow(row int) bool {
	if row < 0 || row != r.connectionMenuActiveRow {
		return false
	}
	r.hideOverlay()
	r.showError(r.panel.disconnectRemote())
	return true
}

// connectionHistorySuccessBlend darkens theme.EntryExecutable for a
// history entry that connected successfully last time but isn't the
// one this panel is attached to right now — still unambiguously
// green (never falling back to the dropdown's own plain, uncolored
// text, which read as "unknown/never tried" rather than "this one
// works"), just a visibly dimmer shade than the currently active
// entry's own full-brightness green, per the user's own explicit
// report that a cleanly closed connection showing in plain white was
// indistinguishable from one that had simply never been tried.
const connectionHistorySuccessBlend = 0.45

// connectionHistoryRemoveGlyph is the small "✕" appended to every
// history row (see renderConnectionMenu), per the user's own explicit
// request for a visible, individually clickable way to drop one entry
// out of history without connecting to it — "x" the character itself
// is reserved for typing into a real name, so a distinct glyph avoids
// any chance of the two being confused, the same reasoning
// connectionButtonGlyph's own doc comment gives for picking one
// character over another.
const connectionHistoryRemoveGlyph = "✕"

// connectionHistoryEjectGlyph appears only on the one history row that
// is the active panel's own current connection (see
// connectionMenuActiveRow) — clicking it, or pressing "e" while it's
// highlighted (see captureConnectionMenuKey), disconnects. Replaces
// what used to be a separate, always-present "Disconnect (...)" list
// item of its own — the user asked for a per-row button in its place
// instead, the international "eject media" symbol reading naturally as
// "detach from this" the same way a USB drive's own eject icon does.
const connectionHistoryEjectGlyph = "⏏"

// connectionHistoryLabel colors entry's own plain Connection.Label()
// by state — green (theme.EntryExecutable, this app's established
// "healthy/active" color — see bottombar.go/gitstatus.go) if entry is
// the panel's own currently active connection, red (theme.CriticalText)
// if the last attempt to reach it failed, a dimmer green (see
// connectionHistorySuccessBlend) otherwise — per the user's own
// explicit request that the current connection, any failed one, and
// any merely-inactive-but-working one all be visually distinguishable
// from each other at a glance. The active row alone also gets a
// leading connectionHistoryEjectGlyph "button" ahead of the
// connectionHistoryRemoveGlyph every row already ends with — both
// muted, not colored by row state, since disconnecting/removing are
// the same action regardless of whether this particular entry
// succeeded or failed last time.
func (r *Root) connectionHistoryLabel(panel *Panel, entry remotefs.HistoryEntry) string {
	label := entry.Label()
	isActive := panel.remote != nil && panel.remoteConn.Equal(entry.Connection)
	var colored string
	switch {
	case isActive:
		colored = "[" + colorTag(r.theme.EntryExecutable) + "]" + label + "[-]"
	case entry.LastFailed:
		colored = "[" + colorTag(r.theme.CriticalText) + "]" + label + "[-]"
	default:
		successColor := blendToward(r.theme.EntryExecutable, colorBlack, connectionHistorySuccessBlend)
		colored = "[" + colorTag(successColor) + "]" + label + "[-]"
	}
	mutedTag := "[" + colorTag(r.theme.MutedTextColor) + "]"
	if isActive {
		colored += "  " + mutedTag + connectionHistoryEjectGlyph + "[-]"
	}
	return colored + "  " + mutedTag + connectionHistoryRemoveGlyph + "[-]"
}

func (r *Root) newConnectionMenuList() *tview.List {
	l := tview.NewList().ShowSecondaryText(false)
	l.SetHighlightFullLine(true)
	l.SetBorderPadding(0, 0, 1, 1)
	return l
}

func (r *Root) newConnectionMenuLayout() *tview.Flex {
	return tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(r.connectionMenuList, 0, 1, true)
}
