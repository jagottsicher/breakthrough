package ui

import (
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

	if panel.remote != nil {
		label := "Disconnect (" + panel.remoteConn.Label() + ")"
		r.connectionMenuList.AddItem(label, "", 0, func() {
			r.hideOverlay()
			r.showError(panel.disconnectRemote())
		})
	}

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
	}

	l := r.connectionMenuList
	l.SetDoneFunc(r.hideOverlay) // Escape
}

// connectionHistoryLabel colors entry's own plain Connection.Label()
// by state: green (theme.EntryExecutable, this app's established
// "healthy/active" color — see bottombar.go/gitstatus.go) if entry is
// the panel's own currently active connection, red
// (theme.CriticalText) if the last attempt to reach it failed,
// otherwise left in the dropdown's own default color. Per the user's
// own explicit request that both the current connection and any failed
// one be visually distinguishable at a glance.
func (r *Root) connectionHistoryLabel(panel *Panel, entry remotefs.HistoryEntry) string {
	label := entry.Label()
	switch {
	case panel.remote != nil && panel.remoteConn.Equal(entry.Connection):
		return "[" + colorTag(r.theme.EntryExecutable) + "]" + label + "[-]"
	case entry.LastFailed:
		return "[" + colorTag(r.theme.CriticalText) + "]" + label + "[-]"
	default:
		return label
	}
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
