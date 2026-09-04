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
// already work (see r.picker/r.optionsList): the contents are cheap to
// rebuild and only ever looked at while open, so there's nothing to gain
// from maintaining them the rest of the time.

const tabSwitcherPage = "tab-switcher"

// tabSwitcherMaxPathWidth caps how much of a long path a row shows before
// it's shortened from the left (see shortenPathLeft). Chosen so the
// overlay stays comfortably inside a conventional 80-column terminal once
// the number prefix and the list's own padding are accounted for.
const tabSwitcherMaxPathWidth = 60

// tabSwitcherNewRowLabel is the trailing "open another one" row's own
// text — the "+" matching the tab strip's own new-tab button, so the two
// read as the same action in two places.
const tabSwitcherNewRowLabel = " +  New tab"

// newTabSwitcher builds the switcher's List — same construction as every
// other list overlay in this package (no border, full-line highlight, one
// column of side padding).
func (r *Root) newTabSwitcher() *tview.List {
	list := tview.NewList().ShowSecondaryText(false)
	list.SetHighlightFullLine(true)
	list.SetBorderPadding(0, 0, 1, 1)
	list.SetDoneFunc(r.closeTabSwitcher) // Escape
	list.SetInputCapture(r.captureTabSwitcherKey)
	return list
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
	if event.Key() != tcell.KeyDelete {
		return event
	}
	i := r.tabSwitcher.GetCurrentItem()
	if i < 0 || i >= len(r.tabs) {
		return nil // the "New tab" row: nothing to close
	}
	if len(r.tabs) == 1 {
		return nil // closeTab would refuse anyway; don't stack an error overlay over the switcher
	}

	r.closeTab(i)
	// Rebuild in place so the list reflects the close immediately,
	// keeping the cursor near where it was rather than jumping home.
	r.openTabSwitcher(min(i, len(r.tabs)-1))
	return nil
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
		label := tabSwitcherRowLabel(i, p.path, i == r.activeTab, r.theme.PlaceholderText)
		// A captured copy, not the loop variable: the callback outlives
		// this iteration. (Go 1.22+ scopes loop variables per iteration,
		// making this safe either way — kept explicit because the intent
		// is easier to see than the language version it relies on.)
		target := i
		r.tabSwitcher.AddItem(label, "", 0, func() { r.commitTabSwitcher(target) })
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
	r.tabSwitcher.AddItem(tabSwitcherNewRowLabel, "", 0, func() {
		r.hideOverlay()
		r.newTabHere()
	})

	r.tabSwitcher.SetCurrentItem(selected)

	width, height := listSize(r.tabSwitcher)
	height++ // the title bar's own row (see tabSwitcherLayout)

	// Anchored under the tab strip rather than centered on screen: the
	// strip is what this drops down from, so appearing anywhere else
	// would read as an unrelated dialog. clampToPanel keeps it on screen
	// when the panel is too narrow or too short for it there.
	x, y := r.tabStripAnchor()
	x, y, width, height = r.clampToPanel(x, y, width, height)

	r.tabSwitcherLayout.SetRect(x, y, width, height)
	r.showOverlay(tabSwitcherPage, r.tabSwitcher)
}

// tabStripAnchor is the screen position the switcher drops down from —
// the left edge of the panel's own tab strip, one row below it.
//
// Falls back to the panel's own top-left when the strip has no rect yet
// (nothing drawn so far, e.g. in a test), which clampToPanel then keeps
// on screen regardless.
func (r *Root) tabStripAnchor() (x, y int) {
	sx, sy, sw, _ := r.panel.tabStrip.GetRect()
	if sw == 0 {
		px, py, _, _ := r.panel.GetRect()
		return px, py + 1
	}
	return sx, sy + 1
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
		current := r.tabSwitcher.GetCurrentItem()
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
		r.tabSwitcher.SetCurrentItem(next)
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
