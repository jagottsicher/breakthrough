package ui

import (
	"fmt"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// The tab switcher overlay: a drop-down list of every open tab, showing
// each one's real directory alongside its number.
//
// This is the answer to the deliberate tradeoff the tab strip makes (see
// tabstrip.go): the strip stays narrow by showing numbers only, and this
// is where the numbers get their meaning back, with room for full paths.
//
// Two states are visible at once, per the user's own explicit design:
// the tab you are currently on, dimmed, and the tab you would switch to,
// carrying the list's own selection highlight. Those are two genuinely
// different things while the switcher is open — the selection moves as
// you step through, the current tab doesn't — and showing only one of
// them would leave you unable to tell how far you'd moved from where you
// started.
//
// The list is rebuilt on every open rather than kept in sync as tabs
// change, matching how the owner/group picker and the Options overlay
// already work (see r.picker): the contents are cheap to rebuild and
// only ever looked at while open, so there's nothing to gain from
// maintaining them the rest of the time.
//
// A Table rather than a List, because each row carries two independent
// things to act on: the tab itself, and a close button beside it. Cell
// selection gives that its own navigation for free — Up/Down moves
// between tabs, Right steps onto the close button and highlights it,
// Left comes back — which a List, with one selectable thing per row,
// cannot express without hand-rolling both the movement and the
// highlight.

const tabSwitcherPage = "tab-switcher"

// Column indices within the switcher table.
const (
	tabSwitcherColLabel = iota
	tabSwitcherColClose
)

// tabSwitcherCloseGlyph is the per-row close button, the same '✕' every
// panel's own title bar uses (see toolWindowCloseGlyph) so "close this"
// looks identical wherever it appears.
const tabSwitcherCloseGlyph = string(toolWindowCloseGlyph)

// tabSwitcherMaxPathWidth caps how much of a long path a row shows before
// it's shortened from the left (see shortenPathLeft). Chosen so the
// overlay stays comfortably inside a conventional 80-column terminal once
// the number prefix and the list's own padding are accounted for.
const tabSwitcherMaxPathWidth = 60

// tabSwitcherNewRowLabel is the trailing "open another one" row's own
// text — the "+" matching the tab strip's own new-tab button, so the two
// read as the same action in two places.
const tabSwitcherNewRowLabel = " +  New tab"

// newTabSwitcher builds the switcher's Table — no border, one column of
// side padding, and cell selection so the close button in each row is
// its own navigable target (see this file's own doc comment).
func (r *Root) newTabSwitcher() *tview.Table {
	table := tview.NewTable()
	table.SetBorders(false)
	table.SetBorderPadding(0, 0, 1, 1)
	table.SetSelectable(true, true) // individual cells: the tab, or its close button
	table.SetInputCapture(r.captureTabSwitcherKey)
	table.SetSelectedFunc(func(row, column int) { r.activateTabSwitcherCell(row, column) })
	return table
}

// activateTabSwitcherCell is Enter, Space or a click on one cell: the
// label switches to that tab, the close button closes it, and the
// trailing row opens a new one.
func (r *Root) activateTabSwitcherCell(row, column int) {
	// The row past the real tabs is "New tab" — see openTabSwitcher.
	if row < 0 || row >= len(r.tabs) {
		r.hideOverlay()
		r.newTabHere()
		return
	}
	if column == tabSwitcherColClose {
		r.closeTabFromSwitcher(row)
		return
	}
	r.commitTabSwitcher(row)
}

// clickTabSwitcherCell is one cell's own mouse action: the same thing
// Enter and Space do on it.
//
// Per cell rather than a mouse capture over the whole table, because
// tview's Table does not run a row's selected function on a click at all
// — it only moves the selection there (verified directly against its own
// MouseHandler, after a live click on a ✕ did nothing but highlight it).
// A per-cell Clicked func is tview's own answer to that.
//
// Returns true to tell tview not to also move the selection afterwards:
// every one of these actions rebuilds or closes the table underneath,
// so the row and column it would select are about to mean something
// else, or nothing at all.
func (r *Root) clickTabSwitcherCell(row, column int) func() bool {
	return func() bool {
		r.activateTabSwitcherCell(row, column)
		return true
	}
}

// closeTabFromSwitcher closes the tab on one row and leaves the switcher
// open, reselecting the row above it.
//
// Above rather than the one that slid up into its place, per the user's
// own explicit request — and it reads better too: closing several in a
// row then walks steadily upward instead of standing still while the
// list shortens underneath the cursor.
//
// Refuses the first tab outright: it's the one tab that always stays
// open (see closeTab, which enforces the same rule for every other path
// to closing one), so its row has no close button to reach in the first
// place and this is only a backstop.
func (r *Root) closeTabFromSwitcher(row int) {
	if row <= 0 || row >= len(r.tabs) {
		return
	}
	r.closeTab(row)
	r.openTabSwitcher(row - 1)
}

// captureTabSwitcherKey adds Delete to the switcher's own keys: it closes
// the highlighted tab in place, leaving the switcher open and updated.
//
// The counterpart to the "New tab" row (see openTabSwitcher's own doc
// comment on why tab management lives here rather than behind more
// global keybindings) — closing a tab is otherwise reachable only from
// the right-click context menu. Delete specifically because it's what
// this app already binds to the other "remove the thing under the
// cursor" action in the panel itself, and because a tab manager that
// closes entries with Delete needs no explaining.
func (r *Root) captureTabSwitcherKey(event *tcell.EventKey) *tcell.EventKey {
	row, column := r.tabSwitcher.GetSelection()

	switch {
	case event.Key() == tcell.KeyEscape:
		// A Table has no DoneFunc of its own, unlike the List this
		// replaced — Escape has to be handled here or it does nothing.
		r.closeTabSwitcher()
		return nil

	case event.Key() == tcell.KeyRune && event.Rune() == ' ':
		// Space acts on the selected cell exactly as Enter does, the
		// same as everywhere else in this app.
		r.activateTabSwitcherCell(row, column)
		return nil

	case event.Key() == tcell.KeyDelete:
		// Closes the selected tab wherever the cursor is within its row,
		// so it works without first stepping onto the close button.
		if row <= 0 || row >= len(r.tabs) {
			return nil // the first tab never closes, and "New tab" has nothing to close
		}
		r.closeTabFromSwitcher(row)
		return nil
	}
	return event
}

// newTabSwitcherTitleBar is the one-row title above the list, the same
// shape every other overlay in this app has (see menuLayout/
// detailsSidebarLayout).
func (r *Root) newTabSwitcherTitleBar() *tview.TextView {
	bar := tview.NewTextView()
	bar.SetWrap(false)
	bar.SetText(" Tabs ")
	return bar
}

// openTabSwitcher shows the switcher with selected preselected.
//
// selected is an index into r.tabs, not a row number: they happen to
// coincide today, but treating them as the same thing is exactly how a
// later "close" row or section divider in this list would break the
// mapping silently.
func (r *Root) openTabSwitcher(selected int) {
	if len(r.tabs) == 0 {
		return
	}
	if selected < 0 || selected >= len(r.tabs) {
		selected = r.activeTab
	}

	r.tabSwitcher.Clear()
	for i, p := range r.tabs {
		row := i // captured per cell callback below, which outlives this iteration
		label := tabSwitcherRowLabel(i, p.path, i == r.activeTab, r.theme.PlaceholderText)
		r.tabSwitcher.SetCell(i, tabSwitcherColLabel,
			tview.NewTableCell(label).
				SetTextColor(r.theme.Text).
				SetSelectable(true).
				SetClickedFunc(r.clickTabSwitcherCell(row, tabSwitcherColLabel)))

		// No close button on the first tab: one tab always stays open
		// (see closeTab), so offering a control that would only ever
		// refuse is worse than not offering one. A non-selectable blank
		// keeps the column present — tview skips unselectable cells when
		// moving, so Right on this row simply stays put.
		closeCell := tview.NewTableCell(" ").SetSelectable(false)
		if i > 0 {
			closeCell = tview.NewTableCell(" " + tabSwitcherCloseGlyph + " ").
				SetTextColor(r.theme.Text).
				SetSelectable(true).
				SetClickedFunc(r.clickTabSwitcherCell(row, tabSwitcherColClose))
		}
		r.tabSwitcher.SetCell(i, tabSwitcherColClose, closeCell)
	}

	// A trailing "New tab" row, which is what makes the whole feature
	// reachable without a mouse: this app's context menu opens on
	// right-click only, so its own "New tab" entry would otherwise be the
	// single way to create one — and a tab feature that can only be
	// *entered* by mouse isn't much of a keyboard feature. Here rather
	// than as yet another global keybinding because every plausible one
	// is either already claimed or collides with the bash line's own
	// readline editing (Ctrl+W, the obvious borrow from browsers, deletes
	// a word there).
	//
	// No close button of its own either — there is nothing there to
	// close yet.
	newRow := len(r.tabs)
	r.tabSwitcher.SetCell(newRow, tabSwitcherColLabel,
		tview.NewTableCell(tabSwitcherNewRowLabel).
			SetTextColor(r.theme.Text).
			SetSelectable(true).
			SetClickedFunc(r.clickTabSwitcherCell(newRow, tabSwitcherColLabel)))
	r.tabSwitcher.SetCell(newRow, tabSwitcherColClose,
		tview.NewTableCell(" ").SetSelectable(false))

	// Always on the label column: the close button is somewhere you
	// deliberately steer to, never where the cursor lands on its own.
	r.tabSwitcher.Select(selected, tabSwitcherColLabel)

	width, height := r.tabSwitcherSize()

	// Anchored under the tab strip rather than centered on screen: the
	// strip is what this drops down from, so appearing anywhere else
	// would read as an unrelated dialog. Right-aligned to the strip's own
	// right edge, not left-aligned to its left edge: the strip sits hard
	// against the screen's right side (right before the Details "<"
	// button), while the switcher itself is wide enough to hold a full
	// path — left-aligning it there would push most of it off-screen and
	// clampToPanel would then yank the whole thing back to the panel's
	// left edge instead, landing it nowhere near what it opened from
	// (confirmed live: exactly this happened before this alignment
	// existed). Opening up-and-to-the-left from a right-edge control is
	// the same convention an ordinary right-aligned dropdown menu uses.
	// clampToPanel still runs as the backstop for a panel too narrow to
	// fit it even flush against the left edge.
	right, y := r.tabStripAnchor()
	x := right - width
	x, y, width, height = r.clampToPanel(x, y, width, height)

	r.tabSwitcherLayout.SetRect(x, y, width, height)
	r.showOverlay(tabSwitcherPage, r.tabSwitcher)
}

// tabSwitcherSize is the overlay's own size: wide enough for its widest
// row plus the close-button column, tall enough for every row plus the
// title bar.
//
// Measured from the cells actually built rather than from a fixed guess,
// the same reason listSize exists for this package's list overlays —
// which this can't use, being a Table.
func (r *Root) tabSwitcherSize() (width, height int) {
	rows := r.tabSwitcher.GetRowCount()

	// Per column, not per row: tview lays each column out at the widest
	// cell *in that column*, so summing a single row's own cells
	// under-counts whenever the longest label and the close button sit
	// on different rows — which is the normal case, since the row
	// carrying "(current)" is usually the widest and the first tab has
	// no close button at all. A real, observed bug: the ✕ ended up
	// clipped off the right edge.
	columnWidths := make([]int, tabSwitcherColClose+1)
	for row := 0; row < rows; row++ {
		for column := range columnWidths {
			cell := r.tabSwitcher.GetCell(row, column)
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
	// +1 for the title bar's own row (see tabSwitcherLayout).
	return width, rows + 1
}

// tabStripAnchor is the screen position the switcher drops down from —
// the right edge of the panel's own tab strip, one row below it (see
// openTabSwitcher's own doc comment on why the right edge, not the
// left).
//
// Falls back to the panel's own top-right when the strip has no rect yet
// (nothing drawn so far, e.g. in a test), which clampToPanel then keeps
// on screen regardless.
func (r *Root) tabStripAnchor() (right, y int) {
	sx, sy, sw, _ := r.panel.tabStrip.GetRect()
	if sw == 0 {
		px, py, pw, _ := r.panel.GetRect()
		return px + pw, py + 1
	}
	return sx + sw, sy + 1
}

// tabSwitcherRowLabel renders one row: the tab's own number, then its
// directory.
//
// The current tab's row is dimmed (PlaceholderText — this app's existing
// "present but not the point" color, already used for the filter box's
// own placeholder) rather than given a color of its own, and carries a
// trailing marker so the distinction survives for anyone who can't rely
// on color alone. The row the user would switch to needs no treatment
// here at all: the list's own selection highlight is already
// FocusedBackground, the same "this is where you're going" color the rest
// of the app uses.
func tabSwitcherRowLabel(i int, path string, current bool, dim tcell.Color) string {
	// Right-aligned in two columns so the paths line up whether the
	// number is one digit or two.
	label := fmt.Sprintf("%2d  %s", i+1, shortenPathLeft(path, tabSwitcherMaxPathWidth))
	if current {
		return fmt.Sprintf("[%s]%s  (current)[-]", colorTag(dim), label)
	}
	return label
}

// shortenPathLeft trims a path from the left to at most maxWidth columns,
// marking the cut with a leading ellipsis.
//
// From the left specifically: the deepest part of a path is what
// distinguishes one tab from another, while the shared prefix
// ("/home/jens/projects/...") is usually the least informative part of
// it. Measured and cut in runes rather than bytes so a path with
// non-ASCII characters isn't truncated mid-character.
func shortenPathLeft(path string, maxWidth int) string {
	runes := []rune(path)
	if len(runes) <= maxWidth || maxWidth < 2 {
		return path
	}
	return "…" + string(runes[len(runes)-(maxWidth-1):])
}

// stepTabSwitcher is Ctrl+Tab/Ctrl+Shift+Tab's shared action: open the
// switcher one step from the current tab, or — if it's already open —
// move its selection one more step in the same direction.
//
// The "already open" case is why this can't simply defer to
// acceptsGlobalShortcut the way most shortcuts do: by the time the second
// Ctrl+Tab arrives, the switcher itself is the open overlay, which that
// check treats as "something is open, stand down". Every other overlay
// still blocks it — pressing Ctrl+Tab with Properties open shouldn't
// quietly swap the directory out from under it.
func (r *Root) stepTabSwitcher(delta int) {
	switch {
	case r.activePage == tabSwitcherPage:
		// Cycles over the real tabs only, deliberately skipping the
		// trailing "New tab" row: Ctrl+Tab means "the next tab", and
		// stopping on a row that would create one instead is not that.
		// It stays reachable with the arrow keys, which move through the
		// list as a plain list.
		count := len(r.tabs)
		if count == 0 {
			return
		}
		current, _ := r.tabSwitcher.GetSelection()
		if current >= count {
			current = r.activeTab // stepping off the "New tab" row resumes from the real one
		}
		// Wraps in both directions: Go's own % keeps the sign of the
		// dividend, so a step below zero needs the extra += count to
		// land back in range rather than staying negative.
		next := (current + delta) % count
		if next < 0 {
			next += count
		}
		// Back onto the label column: Ctrl+Tab means "the next tab", so
		// it should never leave the cursor parked on a close button.
		r.tabSwitcher.Select(next, tabSwitcherColLabel)
	case r.acceptsGlobalShortcut():
		if len(r.tabs) < 2 {
			return // nothing to switch between; don't open an overlay to say so
		}
		next := (r.activeTab + delta) % len(r.tabs)
		if next < 0 {
			next += len(r.tabs)
		}
		r.openTabSwitcher(next)
	}
}

// commitTabSwitcher closes the switcher and switches to i — Enter on a
// row, or a click on one.
func (r *Root) commitTabSwitcher(i int) {
	r.hideOverlay()
	r.switchToTab(i)
}

// closeTabSwitcher dismisses the switcher without switching — Escape, or
// a click outside it (see captureOutsideClick, which reaches this the
// same way every other overlay's own close does).
func (r *Root) closeTabSwitcher() {
	r.hideOverlay()
}
