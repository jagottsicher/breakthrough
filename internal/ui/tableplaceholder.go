package ui

import (
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// showTablePlaceholder and enableTableSelection are the shared shape
// every full-screen catalog table (Mounts, Firewall, Activity Log, and
// any future one reached by a "j" chord member) uses for its own
// "nothing real to show right now" state — a failed read, no backend,
// no rows/mounts/entries. Row 0 is always that screen's own header;
// row 1 is the one placeholder message row these two functions expect
// to own.
//
// showTablePlaceholder does two things, not just one: it writes the
// message, and it turns row selection off entirely
// (SetSelectable(false, false)) for as long as that message is the
// only thing showing. The second part is not optional cosmetic
// tidiness — it is the actual fix for a real, reported freeze:
// tview's own Table (v0.42.0, verified directly against its own
// table.go, not guessed), on its very first Draw while row selection
// is still on and not one cell anywhere is selectable, silently walks
// its own cursor row one past the last real row. The very next Up/Down
// then asks tview's own forward/backward cell search to walk back to
// that same now-nonexistent row number, which its own row wraparound
// can never produce again — spinning forever at 100% CPU, freezing the
// whole application, not just that screen. No prior large list or
// stale selection is required to hit this; one ordinary redraw between
// opening the screen and the first arrow key is enough, which is
// exactly what a real terminal session always does.
//
// Select(1, 0) still runs afterward, purely so the stored row number
// itself is never left stale (out of range for a much longer previous
// list) in the event selection is ever turned back on — see
// enableTableSelection — while something has gone wrong reading the
// underlying data again later.
func showTablePlaceholder(table *tview.Table, text string, color tcell.Color) {
	table.SetCell(1, 0, tview.NewTableCell(text).SetTextColor(color).SetSelectable(false))
	table.SetSelectable(false, false)
	table.Select(1, 0)
}

// enableTableSelection is showTablePlaceholder's own counterpart: turn
// row selection back on once real, selectable content exists again.
func enableTableSelection(table *tview.Table) {
	table.SetSelectable(true, false)
}
