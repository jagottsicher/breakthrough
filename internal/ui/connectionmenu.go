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
	"github.com/jagottsicher/breakthrough/internal/filterexpr"
	"github.com/jagottsicher/breakthrough/internal/rclone"
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

// connectionMenuCloudTargetSegments is how many trailing path
// segments of a cloud mount's own Target a row shows (see
// shortenPathLastSegments) — aligned to path separators, never
// truncated mid-segment the way shortenPathLeft's own character-count
// truncation can, per the user's own explicit request after a real
// example ("…hare/breakthrough/mounts/jens/mydropbox") cut off
// mid-word. 3 covers the default MountsDir()-derived path's own last
// three segments exactly ("mounts", "<user>", "<remote name>") — by
// the user's own math, that leaves roughly 25 of
// connectionMenuColumnWidths' own available room for the remote
// name itself even at the default base path's own length, comfortably
// enough for a real-world remote name.
const connectionMenuCloudTargetSegments = 3

// shortenPathLastSegments returns path's own last n slash-separated
// segments, joined back with "/" and prefixed with "…/" if any
// earlier segments were cut off — unlike shortenPathLeft, this never
// lands mid-segment, since it only ever cuts at a "/" boundary.
// Returns path unchanged if it has n or fewer segments to begin with
// (nothing to cut).
func shortenPathLastSegments(path string, n int) string {
	segments := strings.Split(path, "/")
	// A leading "/" (every cloud mount's own Target is absolute)
	// produces a leading "" element from Split — drop it so "/a/b/c"
	// counts as 3 segments, not 4, the same way a human reading the
	// path would count them.
	if len(segments) > 0 && segments[0] == "" {
		segments = segments[1:]
	}
	if len(segments) <= n {
		return path
	}
	return "…/" + strings.Join(segments[len(segments)-n:], "/")
}

// Column indices within the connection menu table. User | @ | Host/IP
// are fixed, in both collapsed and expanded mode
// (r.settings.ConnectionMenuExpanded); Identifier is fixed too, but
// only present at all once expanded. Type, Tags, Eject and Remove all
// move depending on that flag — resolved through the functions below
// rather than more iota constants, because Type's own position isn't
// fixed the way the others are: per the user's own explicit request,
// it sits right after Identifier when expanded, but right after
// Host/IP when collapsed, since there's no Identifier column there
// for it to sit behind. Per the user's own explicit request, collapsed
// is a real column table too now, not a single merged "user@host"
// string — just missing Identifier/Tags, the two columns expanded mode
// adds.
//
// Type is always present, in both modes — unlike Identifier/Tags: it's
// what tells an SFTP connection row and a cloud-mount row apart at a
// glance ("SFTP" vs "S3"/"WebDAV"/...), which has to work whether or
// not the dropdown happens to be expanded right now, the same reason
// User/@/Host-IP themselves are never hidden either. Deliberately
// never sortable (connectionMenuSortNone, same as Tags) — see
// connectionMenuSortHistory's own doc comment for why a sort key only
// makes sense for a column every row can meaningfully compare on; the
// same reason User/Host-IP themselves don't try to mean anything for a
// cloud row either.
//
// Only connectionMenuColUser is ever selectable — @/Host-IP/Identifier/
// Type/Tags each still carry their own SetClickedFunc (see
// renderConnectionMenu), so clicking anywhere across the whole label
// group still connects/toggles the mount (tview's own Table.
// MouseHandler calls a cell's Clicked() regardless of its own
// Selectable flag, verified against table.go, not guessed), but
// Left/Right keyboard navigation skips straight over them to the
// row's other two real stops, ✎/⏏/mount-unmount and ✕ — exactly the
// "the whole label group is one stop, I don't need to navigate its own
// pieces separately" behavior the user asked for, since every one of
// those cells does the exact same thing regardless of which one was
// actually activated.
const (
	connectionMenuColUser = iota
	connectionMenuColAt
	connectionMenuColHost
	connectionMenuColIdentifier // present only when expanded
)

// connectionMenuTypeColumn resolves Type's own index for the given
// expand state — right after Identifier when expanded, right after
// Host/IP when collapsed (see the column constants' own doc comment
// on why its position, unlike every other column here, actually
// depends on expanded rather than Type simply being skipped).
func connectionMenuTypeColumn(expanded bool) int {
	if expanded {
		return connectionMenuColIdentifier + 1
	}
	return connectionMenuColHost + 1
}

// connectionMenuTagsColumn resolves Tags' own index — present only
// when expanded, right after Type.
func connectionMenuTagsColumn(expanded bool) int {
	return connectionMenuTypeColumn(expanded) + 1
}

// connectionMenuEjectColumn/connectionMenuRemoveColumn resolve the
// eject/edit and remove columns' own index for the given expand state
// — the one place activateConnectionMenuCell and renderConnectionMenu
// both go through, so a click on either glyph is routed by the layout
// actually on screen.
func connectionMenuEjectColumn(expanded bool) int {
	if expanded {
		return connectionMenuTagsColumn(expanded) + 1
	}
	return connectionMenuTypeColumn(expanded) + 1
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

// connectionMenuFilterPlaceholder names, for the search field's own
// placeholder text, exactly which fields a query actually searches —
// collapsed/expanded differ (see connectionMenuFilterHistory's own doc
// comment), so this has to track that split rather than being a fixed
// string.
func connectionMenuFilterPlaceholder(expanded bool) string {
	if expanded {
		return "user, host/ip, type, identifier, tags"
	}
	return "user, host/ip, type"
}

// connectionMenuFilterHistory returns the subset of history whose
// currently visible fields (User+Host-IP always; Identifier and Tags
// too, but only once expanded — collapsed has neither column for a
// query to plausibly mean) match query, reusing filterexpr.QueryFilter
// (internal/filterexpr/query.go) — the same AND/OR/NAND/NOR/NOT
// boolean grammar Log Holepunch's own keyword field already uses, per
// the user's own explicit request for the two to feel the same rather
// than inventing a second parser for a dropdown with at most
// historyMaxEntries (20) rows to search in the first place.
//
// Identifier and Tags both land in the same message argument —
// QueryFilter.Match takes exactly two strings, source and message, and
// Identifier already occupied the second one before Tags existed, so
// there's no third slot to give Tags its own. A query term matches if
// it's a substring of either field, same as always: searching "web"
// matches a Tags value of "web server" too, the identical substring
// behavior User/Host-IP already have (and that a real, investigated
// user report confirmed is accepted, not surprising, in this
// dropdown).
//
// An empty or unparseable query (query midway through being typed —
// an unterminated "(" or trailing "AND", say) means "no filter yet",
// the same convention this package's own doc comment already
// documents for its own callers — never an error surfaced to the user,
// since every character typed is itself a transient, possibly-
// incomplete expression until they stop.
func connectionMenuFilterHistory(history []remotefs.HistoryEntry, query string, expanded bool) []remotefs.HistoryEntry {
	query = strings.TrimSpace(query)
	if query == "" {
		return history
	}
	filter, err := filterexpr.ParseQuery(query)
	if err != nil {
		return history
	}
	filtered := make([]remotefs.HistoryEntry, 0, len(history))
	for _, entry := range history {
		// "SFTP" is always appended — Type is always present, in both
		// modes (see the column constant's own doc comment), so it has
		// to always be searchable too, not only once expanded the way
		// Identifier/Tags below are.
		source := entry.User + " " + entry.HostPort() + " SFTP"
		message := ""
		if expanded {
			message = entry.Identifier + " " + strings.Join(entry.Tags, " ")
		}
		if filter.Match(source, message) {
			filtered = append(filtered, entry)
		}
	}
	return filtered
}

// connectionMenuFilterCloudMounts mirrors connectionMenuFilterHistory
// exactly, for the other kind of row connectionMenuTable now carries
// (see this file's own package doc comment on why both live in one
// table): the same query, against the same two always-vs-expanded-only
// halves, just with RemoteName/Target/the resolved backend type (see
// renderConnectionMenu's own typeByName) standing in for
// User/HostPort/"SFTP". typeByName's own absence for a given entry
// (ListRemotes itself failed, or rclone no longer knows that name) is
// not an error here — it just searches as "", the same "degrade, don't
// crash" contract renderConnectionMenu's own caller already accepts.
func connectionMenuFilterCloudMounts(entries []rclone.MountEntry, typeByName map[string]string, query string, expanded bool) []rclone.MountEntry {
	query = strings.TrimSpace(query)
	if query == "" {
		return entries
	}
	filter, err := filterexpr.ParseQuery(query)
	if err != nil {
		return entries
	}
	filtered := make([]rclone.MountEntry, 0, len(entries))
	for _, entry := range entries {
		source := entry.RemoteName + " " + entry.Target + " " + typeByName[entry.RemoteName]
		message := ""
		if expanded {
			message = entry.Identifier + " " + strings.Join(entry.Tags, " ")
		}
		if filter.Match(source, message) {
			filtered = append(filtered, entry)
		}
	}
	return filtered
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
	// The search query resets the same way and for the same reason: a
	// stale filter silently hiding entries the next time this opens
	// would read as "my connection history is gone", not "I forgot to
	// clear a search".
	r.connectionMenuSortKey = connectionMenuSortNone
	r.connectionMenuSortDescending = false
	r.connectionMenuSearchField.SetText("")
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
	r.renderConnectionMenuTitleBar()
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
	// connectionMenuSortNone deliberately, and unconditionally (not
	// inside `if expanded`) — see the column constants' own doc
	// comment: Type is always present, and never sortable, in either
	// mode, just at a different index depending on whether Identifier
	// is there to sit behind.
	r.connectionMenuSetHeaderCell(connectionMenuTypeColumn(expanded), "Type", connectionMenuSortNone)
	if expanded {
		// connectionMenuSortNone deliberately: Tags is filterable (see
		// connectionMenuFilterHistory), not sortable — the plan never
		// asked for a fourth mnemonic/sort key alongside U/H/I, and a
		// set-valued column has no single natural sort order the way a
		// plain string column does anyway.
		r.connectionMenuSetHeaderCell(connectionMenuTagsColumn(expanded), "Tags", connectionMenuSortNone)
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
	history = connectionMenuFilterHistory(history, r.connectionMenuSearchField.GetText(), expanded)
	r.connectionMenuSearchField.SetPlaceholder(connectionMenuFilterPlaceholder(expanded))
	row := 1
	for i, entry := range history {
		if i >= connectionMenuMaxHistoryRows {
			break
		}
		entry := entry
		thisRow := row
		isActive := panel.remote != nil && panel.remoteConn.Equal(entry.Connection)
		color := connectionHistoryColor(r.theme, isActive, entry.LastFailed, entry.Untested)

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
			// Not run through truncateMiddle, deliberately — same as
			// Identifier right next to it (see renderConnectionMenu's
			// own loop above, which never truncates it either): "columns
			// use as much horizontal space as they need" is tview's own
			// stated default (table.go's own doc comment), and expanded
			// mode already spans the full terminal width specifically so
			// a wide entry has somewhere to actually show. An earlier
			// version capped this at a fixed width regardless of
			// available room — a real, user-reported regression (tags
			// cut with "…" even on a wide terminal with plenty of free
			// space) — removed rather than tuned, since any fixed cap
			// reintroduces the same problem the moment a terminal is
			// wider than that guess.
			table.SetCell(thisRow, connectionMenuTagsColumn(expanded),
				tview.NewTableCell(strings.Join(entry.Tags, ", ")).
					SetTextColor(color).
					SetSelectable(false).
					SetClickedFunc(r.clickConnectionMenuCell(thisRow, connectionMenuTagsColumn(expanded))))
		}
		// Same color as every other cell in this row's own label
		// group (color, derived above from isActive/LastFailed/
		// Untested) — never theme.MutedTextColor on its own: Type is
		// part of that same "at a glance, is this reachable" signal,
		// not a secondary, always-muted descriptor. Position depends
		// on expanded (see connectionMenuTypeColumn's own doc
		// comment), so this has to run after the Identifier block
		// above, not alongside Host/IP the way it did before that
		// position became conditional.
		table.SetCell(thisRow, connectionMenuTypeColumn(expanded),
			tview.NewTableCell("SFTP").
				SetTextColor(color).
				SetSelectable(false).
				SetClickedFunc(r.clickConnectionMenuCell(thisRow, connectionMenuTypeColumn(expanded))))

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

	// Cloud-mount rows come next, after every SFTP history row and
	// before "+ New" — per the user's own explicit request that both
	// read as one list (SFTP first, since it's the one actually used
	// most often, same reasoning renderConnectionMenu's own doc comment
	// already gives for history-before-"+ New"), not two separate menus
	// reached through two different doorways. typeByName is resolved
	// once per render (one ListRemotes call), not per row — this
	// dropdown has at most historyMaxEntries-ish rows total, so a
	// second subprocess call per open is already the expensive part;
	// a third call per row would make opening this dropdown
	// perceptibly slower for no reason. A ListRemotes failure (rclone
	// missing, or erroring) just means every cloud row's own Type cell
	// reads "" — degrade, don't crash, the same contract LoadHistory's
	// own failure already gets a few lines up.
	typeByName := map[string]string{}
	if remotes, err := rclone.ListRemotes(); err == nil {
		for _, rem := range remotes {
			typeByName[rem.Name] = rem.Type
		}
	}
	cloudEntries, _ := rclone.LoadMountEntries()
	cloudEntries = connectionMenuFilterCloudMounts(cloudEntries, typeByName, r.connectionMenuSearchField.GetText(), expanded)
	r.connectionMenuCloudMountRows = map[int]rclone.MountEntry{}
	for _, entry := range cloudEntries {
		entry := entry
		thisRow := row
		mounted, mountErr := rclone.IsMounted(entry.Target)
		_, pending := r.cloudMountPending[entry.Target]
		color := cloudMountRowColor(r.theme, mounted, mountErr != nil)

		// RemoteName alone, never "RemoteName (Identifier)" — a real,
		// reported duplication: Identifier already gets its own column
		// (connectionMenuColIdentifier, expanded only), the same plain
		// "just entry.User" shape the SFTP branch just above already
		// uses for its own label cell.
		table.SetCell(thisRow, connectionMenuColUser,
			tview.NewTableCell(entry.RemoteName).
				SetTextColor(color).
				SetSelectable(true).
				SetClickedFunc(r.clickConnectionMenuCell(thisRow, connectionMenuColUser)))
		table.SetCell(thisRow, connectionMenuColAt, blankConnectionMenuCell())
		table.SetCell(thisRow, connectionMenuColHost,
			tview.NewTableCell(shortenPathLastSegments(entry.Target, connectionMenuCloudTargetSegments)).
				SetTextColor(color).
				SetSelectable(false).
				SetClickedFunc(r.clickConnectionMenuCell(thisRow, connectionMenuColHost)))
		if expanded {
			table.SetCell(thisRow, connectionMenuColIdentifier,
				tview.NewTableCell(entry.Identifier).
					SetTextColor(color).
					SetSelectable(false).
					SetClickedFunc(r.clickConnectionMenuCell(thisRow, connectionMenuColIdentifier)))
			table.SetCell(thisRow, connectionMenuTagsColumn(expanded),
				tview.NewTableCell(strings.Join(entry.Tags, ", ")).
					SetTextColor(color).
					SetSelectable(false).
					SetClickedFunc(r.clickConnectionMenuCell(thisRow, connectionMenuTagsColumn(expanded))))
		}
		// Same color as the rest of this row's own label group — see
		// the matching comment on the SFTP loop above for why never
		// theme.MutedTextColor on its own.
		table.SetCell(thisRow, connectionMenuTypeColumn(expanded),
			tview.NewTableCell(typeByName[entry.RemoteName]).
				SetTextColor(color).
				SetSelectable(false).
				SetClickedFunc(r.clickConnectionMenuCell(thisRow, connectionMenuTypeColumn(expanded))))

		// Eject column mirrors SFTP's own Eject/Edit shape exactly now
		// — per the user's own explicit, later report that the earlier
		// "mount"/"unmount" words were themselves superfluous: mounting
		// already happens from the label group (see
		// activateConnectionMenuCell's own cloud-mount branch,
		// toggleCloudMount goes both directions), so this column only
		// ever needs to offer whichever action the label group doesn't
		// already cover for the *other* state — eject to unmount while
		// mounted (the label group still also unmounts, per the user's
		// own explicit "the name itself is a toggle too"), edit while
		// not mounted (editCloudMountRow — mounting from here would
		// just duplicate the label group, a real way in to actually
		// change an entry didn't exist at all before this). pending's
		// own transient "mounting…"/"unmounting…" label still wins over
		// either glyph — toggleCloudMount's whole reason for setting it
		// is so a slow attempt never looks like a stale, clickable
		// action instead.
		actionText := connectionHistoryEditGlyph
		if mounted {
			actionText = connectionHistoryEjectGlyph
		}
		if pending {
			actionText = r.cloudMountPending[entry.Target]
		}
		table.SetCell(thisRow, ejectCol,
			tview.NewTableCell("   "+actionText+" ").
				SetTextColor(r.theme.MutedTextColor).
				SetSelectable(true).
				SetClickedFunc(r.clickConnectionMenuCell(thisRow, ejectCol)))
		table.SetCell(thisRow, removeCol,
			tview.NewTableCell(" "+cloudMountRemoveGlyph+" ").
				SetTextColor(r.theme.MutedTextColor).
				SetSelectable(true).
				SetClickedFunc(r.clickConnectionMenuCell(thisRow, removeCol)))

		r.connectionMenuCloudMountRows[thisRow] = entry
		r.connectionMenuRowColors[thisRow] = color
		row++
	}

	newRow := row
	// Remembered for openNewConnectionChooser's own positioning below
	// — it opens well after this render call returns, so it can't just
	// read the local newRow here.
	r.connectionMenuNewRow = newRow
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
	// Every column gets an explicit blank cell, Type included — a
	// cell tview never had SetCell called on for this row at all
	// renders with its own un-themed default background (black),
	// never the table's own SurfaceBackground, which is exactly the
	// real, reported bug an earlier version of this row had: Type's
	// own position depends on expanded (see
	// connectionMenuTypeColumn's own doc comment), so it has to be
	// blanked explicitly here too, the same as every other column in
	// this row already is.
	table.SetCell(newRow, connectionMenuTypeColumn(expanded), blankConnectionMenuCell())
	if expanded {
		table.SetCell(newRow, connectionMenuTagsColumn(expanded), blankConnectionMenuCell())
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

// newConnectionMenuSearchField builds the dropdown's own always-present
// search box — see newConnectionMenuTitleRow's own doc comment on why
// it replaces the old static "Connections" label entirely rather than
// the title bar transforming into one only once activated. The label
// itself carries the "/" mnemonic the same way a column header's own
// U/H/I do (connectionMenuSetHeaderCell's own ButtonBackground chip,
// here on InputField.SetLabel instead of a TableCell — verified
// against tview's own textarea.go that TextArea.Draw's label half goes
// through the same tag-aware printWithStyle a Table cell's own text
// does, so the markup renders identically). The field is never focused
// by default (SetFocus) — "/", Tab/Backtab, or a direct click (see
// captureConnectionMenuKey) are the only ways in; typing without one of
// these first would otherwise be ambiguous with the table's own
// single-letter shortcuts (U/H/I/e/x/</>).
// styleConnectionMenuSearchField applies styleInput (theme.go) — this
// app's own shared InputBackground/InputFocusedBackground "look like a
// real input" convention, the same one logAuditKeywordField already
// uses for an identical kind of boolean-query field — and then
// overrides the label's own base style back to SurfaceBackground/
// MutedTextColor, so "Search: " reads as ordinary dropdown chrome
// instead of also picking up the lighter input-field background
// styleInput's own SetLabelStyle call would otherwise give it (the
// user's own explicit, later report: only the typed/placeholder area
// itself — never "Search: " — should look different from the rest of
// the dropdown). The "/" mnemonic keeps its own distinct
// ButtonBackground regardless, since that's an inline color tag inside
// the label's own text (see newConnectionMenuSearchField's own
// SetLabel call), never touched by SetLabelStyle's base at all.
func (r *Root) styleConnectionMenuSearchField(focused bool) {
	styleInput(r.connectionMenuSearchField, r.theme, focused)
	r.connectionMenuSearchField.SetLabelStyle(tcell.StyleDefault.
		Background(r.theme.SurfaceBackground).
		Foreground(r.theme.MutedTextColor))
}

func (r *Root) newConnectionMenuSearchField() *tview.InputField {
	f := tview.NewInputField()
	// "Filter:", not "Search:" — this app's own established term for
	// exactly this behavior (narrows a list live as you type, see
	// Panel.filterField/filter_persistent) rather than something that
	// implies jumping to or finding a single match.
	f.SetLabel(fmt.Sprintf("[:%s:] / [-:-:-]Filter: ", colorTag(r.theme.ButtonBackground)))
	// styleConnectionMenuSearchField (not styleInput directly — see its
	// own doc comment) wired via Focus/Blur rather than set once: a
	// fixed background regardless of focus state is what made this
	// field unrecognizable as something you can type into at all (the
	// user's own first explicit report) — applyTheme's own call picks
	// the right one for whatever the actual focus state is at the
	// moment a theme gets (re)applied.
	f.SetFocusFunc(func() { r.styleConnectionMenuSearchField(true) })
	f.SetBlurFunc(func() { r.styleConnectionMenuSearchField(false) })
	f.SetChangedFunc(func(text string) {
		if r.activePage == connectionMenuPage {
			r.renderConnectionMenu()
		}
	})
	// Enter and Escape both just return focus to the table — neither
	// clears the typed query (a filter that silently vanishes the
	// instant you stop editing it would be more surprising than
	// useful) nor closes the dropdown (that's still Escape on the
	// table itself, via captureConnectionMenuKey, a deliberate second
	// stage: the first Escape leaves the field, the second closes the
	// whole dropdown).
	f.SetDoneFunc(func(key tcell.Key) {
		r.app.SetFocus(r.connectionMenuTable)
	})
	return f
}

// connectionMenuToggleWidth is connectionMenuTitleBar's own fixed
// width within connectionMenuTitleRow — just the "<"/">" glyph plus
// one column of padding either side, now that it's its own Flex item
// rather than the right-hand end of a wide "Connections" label (see
// newConnectionMenuTitleRow's own doc comment on why the label went
// away entirely).
const connectionMenuToggleWidth = 3

// newConnectionMenuTitleRow replaces the old single-widget title bar
// ("Connections" label, with the expand/collapse glyph right-aligned
// into the same TextView) with a search field filling the whole row
// instead — per the user's own explicit request: a search box that's
// always there, not a static label that transforms into one, with the
// toggle staying a fixed-width item on the right. connectionMenuTitleBar
// keeps its existing name but is now just that glyph, nothing else —
// see renderConnectionMenuTitleBar's own doc comment for how much
// simpler that made it.
func (r *Root) newConnectionMenuTitleRow() *tview.Flex {
	row := tview.NewFlex().SetDirection(tview.FlexColumn).
		AddItem(r.connectionMenuSearchField, 0, 1, false).
		AddItem(r.connectionMenuTitleBar, connectionMenuToggleWidth, 0, false)
	return row
}

func (r *Root) newConnectionMenuLayout() *tview.Flex {
	return tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(r.connectionMenuTitleRow, 1, 0, false).
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
	expanded := r.settings.ConnectionMenuExpanded
	if entry, ok := r.connectionMenuCloudMountRows[row]; ok {
		switch {
		case column == connectionMenuRemoveColumn(expanded):
			r.removeCloudMount(entry)
		case column == connectionMenuEjectColumn(expanded):
			// Mirrors the SFTP branch just below: eject (unmount) while
			// mounted, edit while not — see renderConnectionMenu's own
			// comment on this column's cloud-mount shape for why only
			// the "other" action belongs here.
			mounted, err := rclone.IsMounted(entry.Target)
			if err != nil {
				r.showError(err)
				return
			}
			if mounted {
				r.toggleCloudMount(entry)
			} else {
				r.editCloudMountRow(entry)
			}
		default:
			// The label group (User/Host/Identifier/Tags/Type) — mounts
			// if not mounted, unmounts if already mounted, same as
			// clicking an SFTP row's own label dials it. toggleCloudMount
			// itself decides which direction via its own IsMounted check.
			r.toggleCloudMount(entry)
		}
		return
	}
	conn, ok := r.connectionMenuHistoryRows[row]
	if !ok {
		// Neither a history row nor a cloud-mount row: the one other
		// kind this table has, "+ New" — see openNewConnectionChooser's
		// own doc comment for why this opens a small chooser now
		// rather than going straight to openConnectDialog the way it
		// used to, back when there was only ever one kind of thing to
		// create.
		r.openNewConnectionChooser()
		return
	}
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
		r.openConnectDialog(conn, connectDialogModeNew)
		r.runConnect()
	}
}

// openNewConnectionChooser is the trailing "+ New" row's own action —
// a small two-item picker (reusing r.picker, the same shared generic
// chooser the owner/group picker, label menu, rsync presets, and the
// ssh-key-copy target already use), not a direct jump to the Connect
// dialog: SFTP connections and cloud mounts need two genuinely
// different creation dialogs (see cloudmountdialog.go's own package
// doc comment on why one of them can't just be another field set on
// the same form), and "+ New" now has to mean "pick which kind" first.
//
// Deliberately pushOverlay, not showOverlay: this chooser sits ON TOP
// of the still-open connection menu rather than replacing it, so
// Escape (the picker's own SetDoneFunc below) pops back to the menu
// instead of closing everything — the exact back-navigation dead end
// an earlier, now-reverted design (a second, separate dropdown opened
// via showOverlay) had, per a real, reported complaint. Picking either
// item still ends up closing both layers anyway, because
// openConnectDialog/openCloudMountCreateDialog each call showOverlay
// themselves, which clears the whole stack — the same "a dialog
// replaces whatever menu led to it" behavior "+ New" already had.
func (r *Root) openNewConnectionChooser() {
	r.picker.Clear()
	r.picker.AddItem("SFTP connection", "", 0, func() {
		r.openConnectDialog(remotefs.Connection{}, connectDialogModeNew)
	})
	r.picker.AddItem("Cloud mount", "", 0, func() {
		r.openCloudMountCreateDialog("", nil)
	})
	r.picker.SetDoneFunc(func() { r.hideOverlay() })

	// Anchored at the "+ New" row's own screen position, per the
	// user's own explicit request — the column right after User (@'s
	// own column; connectionMenuColAt exists at the same index in both
	// collapsed and expanded mode, so no expanded-mode branch is
	// needed here), the row connectionMenuNewRow itself. GetInnerRect
	// already accounts for the table's own left/right border padding
	// (SetBorderPadding(0, 0, 1, 1)); rowOffset is never anything but 0
	// here in practice, since positionConnectionMenu always sizes the
	// table's own height to fit every row at once (see
	// connectionMenuSize), so row index already is screen row offset.
	tableX, tableY, _, _ := r.connectionMenuTable.GetInnerRect()
	columnWidths := connectionMenuColumnWidths(r.connectionMenuTable)
	startX := tableX + connectionMenuColumnStartX(columnWidths, connectionMenuColAt)
	startY := tableY + r.connectionMenuNewRow

	width, height := listSize(r.picker)
	x, y, width, height := r.clampToPanel(startX, startY, width, height)
	r.picker.SetRect(x, y, width, height)
	r.picker.SetCurrentItem(0)

	r.pushOverlay(pickerPage, r.picker, nil)
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
	r.openConnectDialog(conn, connectDialogModeEdit)
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

	// Up at the first real row (1 — row 0 is the inert header) wraps to
	// the last; Down at the last wraps to the first — the same
	// wraparound filtermenu.go's own moveFocus already gives its own
	// dropdown ((i + delta + len(order)) % len(order)), per a real,
	// reported complaint that this table was the one dropdown in the
	// app that didn't. tview's own Table.InputHandler clamps instead of
	// wrapping for every other Up/Down press, which is exactly right
	// there — this only overrides the two boundary cases, never
	// touching ordinary navigation in between (no case matches, so
	// execution falls through to this function's own final "return
	// event" below, the same as today).
	lastRow := r.connectionMenuTable.GetRowCount() - 1
	switch {
	case event.Key() == tcell.KeyUp && row <= 1:
		r.connectionMenuTable.Select(lastRow, column)
		return nil
	case event.Key() == tcell.KeyDown && row >= lastRow:
		r.connectionMenuTable.Select(1, column)
		return nil
	case event.Key() == tcell.KeyEscape:
		r.hideOverlay()
		return nil
	case event.Key() == tcell.KeyRune && event.Rune() == ' ':
		r.activateConnectionMenuCell(row, column)
		return nil
	case event.Key() == tcell.KeyTab, event.Key() == tcell.KeyBacktab:
		// tview.Table's own InputHandler claims both of these outright
		// (table.go's own InputHandler checks key == KeyTab ||
		// KeyBacktab and returns unconditionally, calling t.done only if
		// actually set — verified directly against tview's own source,
		// not guessed) and connectionMenuTable never sets a DoneFunc, so
		// without this, either key was a silent no-op. The dropdown only
		// has two real keyboard stops — this table and the search field
		// (see newConnectionMenuSearchField) — so both directions lead
		// to the same other stop; the search field's own SetDoneFunc
		// already returns here for Tab, Backtab, Enter, and Escape
		// alike, so the round trip works both ways without any further
		// change there.
		r.app.SetFocus(r.connectionMenuSearchField)
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

	// "/" focuses the search field — alongside Tab/Backtab above and a
	// direct click, now three ways in (see newConnectionMenuSearchField's
	// own doc comment on why "just start typing" would be ambiguous with
	// every other single-letter shortcut this table already has, "/"
	// included).
	if event.Key() == tcell.KeyRune && event.Rune() == '/' {
		r.app.SetFocus(r.connectionMenuSearchField)
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

	columnWidths := connectionMenuColumnWidths(table)
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

// connectionMenuColumnWidths measures table's own widest cell per
// column, collapsed-mode columns only (connectionMenuRemoveColumn(
// false)+1 of them) — split out of connectionMenuSize so
// openNewConnectionChooser's own positioning below can reuse the exact
// same measurement rather than a second, possibly-drifting copy of it.
func connectionMenuColumnWidths(table *tview.Table) []int {
	rows := table.GetRowCount()
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
	return columnWidths
}

// connectionMenuColumnStartX sums columnWidths' own per-column widths
// and inter-column gaps up to (not including) column — the same
// "widest cell plus the single-column gap tview leaves between cells"
// arithmetic connectionMenuSize already uses for the table's own total
// width, just stopped partway through to answer "where does this
// column's own cell actually start" instead of "how wide is the whole
// table."
func connectionMenuColumnStartX(columnWidths []int, column int) int {
	x := 0
	for i := 0; i < column && i < len(columnWidths); i++ {
		if i > 0 {
			x++ // the single column tview leaves between cells
		}
		x += columnWidths[i]
	}
	if column > 0 {
		x++ // the gap between the last counted column and this one
	}
	return x
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
// last attempt to reach it failed, theme.Text (plain, uncolored — the
// same neutral color the "+ New" row itself already uses) if it was
// only ever saved via the Connect dialog's own "OK" button and never
// actually dialed (see remotefs.HistoryEntry.Untested's own doc
// comment), a dimmer green (see connectionHistorySuccessBlend)
// otherwise — per the user's own explicit request that the current
// connection, any failed one, any never-tried one, and any
// merely-inactive-but-working one all be visually distinguishable from
// each other at a glance. isActive is checked first and wins outright:
// a connection can be both Untested (saved, never dialed through this
// exact history entry before) and the panel's own current one at the
// very same time, if the user saved it with OK and the panel happened
// to already be connected to that same endpoint by some other means —
// "demonstrably connected right now" is a stronger, more useful signal
// than "never tried," so bright green wins that combination, not
// white. A real tcell.Color set directly on the cell
// (TableCell.SetTextColor), not a markup tag embedded in its own text
// — the same "the widget's own API carries color, not the string"
// shape tabSwitcherRowLabel's plain dim/undimmed split already uses,
// now needed here for a four-way split instead of two.
func connectionHistoryColor(theme config.ResolvedTheme, isActive, lastFailed, untested bool) tcell.Color {
	switch {
	case isActive:
		return theme.EntryExecutable
	case lastFailed:
		return theme.CriticalText
	case untested:
		return theme.Text
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
// renderConnectionMenuTitleBar sets connectionMenuTitleBar's own text
// to just the expand/collapse glyph — no padding math needed anymore
// (unlike the old single-widget title bar, which right-aligned this
// glyph within a wide "Connections" label): the glyph is its own fixed
// connectionMenuToggleWidth-wide Flex item now (see
// newConnectionMenuTitleRow), so its own rect already is exactly the
// glyph's own column.
func (r *Root) renderConnectionMenuTitleBar() {
	glyph := connectionMenuExpandGlyph
	if r.settings.ConnectionMenuExpanded {
		glyph = connectionMenuCollapseGlyph
	}
	r.connectionMenuTitleBar.SetText(" " + glyph + " ")
}

// captureConnectionMenuTitleBarMouse toggles expand/collapse on any
// click inside connectionMenuTitleBar's own rect — no exact-column hit
// test needed anymore (unlike when this bar was wide and the glyph was
// just its right-hand end): the whole widget is the glyph now, nothing
// else sits inside its rect to miss.
func (r *Root) captureConnectionMenuTitleBarMouse(action tview.MouseAction, event *tcell.EventMouse) (tview.MouseAction, *tcell.EventMouse) {
	if !r.connectionMenuTitleBar.InRect(event.Position()) {
		return action, event
	}
	if action != tview.MouseLeftClick {
		return tview.MouseConsumed, nil
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
	groupCols = append(groupCols, connectionMenuTypeColumn(expanded))
	if expanded {
		groupCols = append(groupCols, connectionMenuTagsColumn(expanded))
	}
	rows := table.GetRowCount()
	// row 0 is the header, skipped entirely — not just exempted from
	// the highlight condition below, which used to still leave this
	// loop free to overwrite its own Host-IP/Identifier cells with
	// SetBackgroundColor/SetTextColor on every single selection change
	// (this function runs on every one, including the table.Select at
	// the very end of renderConnectionMenu). That silently erased
	// connectionMenuSetHeaderCell's own embedded "[:ButtonBackground:]"
	// mnemonic markup on those two cells specifically — User never
	// showed the bug because it isn't in groupCols at all, which is
	// exactly what made this easy to miss (a real, user-reported
	// regression: only User's own "U" stayed highlighted).
	for row := 1; row < rows; row++ {
		bg := r.theme.SurfaceBackground
		fg := r.connectionMenuRowColors[row]
		if row == selectedRow && selectedColumn == connectionMenuColUser {
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
