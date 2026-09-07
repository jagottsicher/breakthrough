package ui

import (
	"fmt"
	"strconv"

	"github.com/rivo/tview"
)

// Split view: two of the open tabs on screen at once, side by side or
// stacked, instead of one at a time.
//
// Deliberately built out of the tabs that already exist rather than as a
// second, parallel notion of "a pane". A tab is already a complete,
// independent browsing context — its own directory, history, filter,
// sort order, selection and cursor (see tabs.go's own doc comment on why
// each tab is a whole real Panel) — which is exactly what a pane in a
// two-pane file manager is. Introducing a separate pane concept beside
// it would have meant two kinds of browsing context to keep in sync, and
// two answers to every question the rest of this package asks about
// "the panel".
//
// So: splitPanes names two tabs, panelHost mounts both instead of one,
// and r.panel keeps meaning "the one with keyboard focus" throughout.
// Every existing feature — the context menu, Copy/Paste, the Details
// sidebar, every shortcut — goes on acting on the focused pane without
// knowing any of this happened.
//
// The two panes are drawn in a fixed screen order that does not change
// when focus moves between them: a layout that reshuffled itself every
// time you tabbed across would be unusable. splitPanes[0] is always the
// left (or top) one.

// splitPaneCount is how many panes split view shows. Named rather than a
// bare 2 in the loops below — not because it's ever going to be three
// (splitPanes is a fixed-size array precisely because two is the whole
// design), but because "2" appearing in a bounds check reads as a magic
// number at every call site.
const splitPaneCount = 2

// splitDirection is the tview Flex direction the current orientation
// preference asks for: stacked panes divide the window into rows, side-
// by-side ones into columns.
func (r *Root) splitDirection() int {
	if r.settings.SplitStacked {
		return tview.FlexRow
	}
	return tview.FlexColumn
}

// remountPanels rebuilds panelHost to show whatever should currently be
// on screen: both split panes, or just the active tab.
//
// Rebuilt wholesale on every change rather than mounting every tab once
// and hiding all but the visible ones (which is what an earlier
// tview.Pages-based version did): a Flex only ever draws, focuses and
// routes mouse events to the items actually in it, so "not mounted" is a
// stronger and simpler guarantee than "mounted but hidden" — and it
// removes the positional page-name bookkeeping that made closing a tab
// from the middle of the list a rebuild anyway.
func (r *Root) remountPanels() {
	r.panelHost.Clear()

	if !r.splitActive {
		r.panelHost.SetDirection(tview.FlexColumn) // irrelevant for one item; keeps the state predictable
		r.panelHost.AddItem(r.panel, 0, 1, true)
		return
	}

	r.panelHost.SetDirection(r.splitDirection())
	for _, tab := range r.splitPanes {
		if tab < 0 || tab >= len(r.tabs) {
			continue // defensive: splitPanesValid should already have ruled this out
		}
		// Equal proportions, no fixed size: an even split is the only
		// division that needs no explanation, and a draggable divider
		// would need mouse-drag plumbing this first version deliberately
		// leaves out.
		r.panelHost.AddItem(r.tabs[tab], 0, 1, tab == r.activeTab)
	}
}

// splitPanesValid reports whether splitPanes currently names two
// distinct, in-range tabs — the precondition for split view meaning
// anything at all. Checked rather than assumed at every entry point,
// since closing tabs shifts every index above the closed one (see
// adjustSplitForClosedTab).
func (r *Root) splitPanesValid() bool {
	if r.splitPanes[0] == r.splitPanes[1] {
		return false
	}
	for _, tab := range r.splitPanes {
		if tab < 0 || tab >= len(r.tabs) {
			return false
		}
	}
	return true
}

// splitPartner is the tab index of the pane that isn't the focused one,
// and whether there is one at all.
func (r *Root) splitPartner() (int, bool) {
	if !r.splitActive {
		return 0, false
	}
	for _, tab := range r.splitPanes {
		if tab != r.activeTab {
			return tab, true
		}
	}
	return 0, false
}

// splitSlotOf is which pane slot (0 = left/top, 1 = right/bottom) tab
// currently occupies, and whether it occupies one at all.
func (r *Root) splitSlotOf(tab int) (int, bool) {
	for slot, t := range r.splitPanes {
		if t == tab {
			return slot, true
		}
	}
	return 0, false
}

// enterSplit shows the active tab alongside partner.
//
// The active tab keeps the first (left/top) slot, so the pane you were
// already looking at stays where your eyes are and the new one appears
// beside it — rather than the layout deciding for itself which of the
// two is "first" and possibly moving your own pane out from under you.
func (r *Root) enterSplit(partner int) {
	if partner < 0 || partner >= len(r.tabs) || partner == r.activeTab {
		return
	}
	r.splitPanes = [2]int{r.activeTab, partner}
	r.splitActive = true
	r.remountPanels()
	r.refreshTabStrips()
	r.syncSplitMenuLabels()
	r.refreshButtonBar()
	r.app.SetFocus(r.panel.table)
}

// exitSplit returns to a single pane, keeping whichever one had focus.
//
// splitPanes is deliberately left as it is rather than cleared: turning
// split view straight back on should return to the same pair, which is
// what makes a single toggle key (see ToggleSplitShortcut) usable as a
// quick "show me both / hide the second one" rather than a setup ritual
// every time.
func (r *Root) exitSplit() {
	if !r.splitActive {
		return
	}
	r.splitActive = false
	r.remountPanels()
	r.refreshTabStrips()
	r.syncSplitMenuLabels()
	r.refreshButtonBar()
	r.app.SetFocus(r.panel.table)
}

// splitPartnerForToggle picks which tab to open split view with when the
// user asked for a split without naming one — the toggle key's own case.
//
// In order of preference: the pair last used (so toggling off and on
// returns to it), then the next tab along, and failing both a brand-new
// tab on the current directory. That last case is the one that makes the
// toggle useful on its own: with a single tab open, one keypress gives
// you the same directory in two panes, which is the classic starting
// point for copying between two places in one tree.
//
// Returns false only when a new tab was needed and couldn't be created
// (newTab reports that failure itself, see maxTabs).
func (r *Root) splitPartnerForToggle() (int, bool) {
	for _, tab := range r.splitPanes {
		if tab >= 0 && tab < len(r.tabs) && tab != r.activeTab {
			return tab, true
		}
	}
	if len(r.tabs) > 1 {
		return (r.activeTab + 1) % len(r.tabs), true
	}
	return r.newTabForSplit()
}

// newTabForSplit opens another tab on the current directory and returns
// its index, leaving the tab it was opened from active.
//
// That last part is the whole reason this isn't just a newTabHere call:
// newTabHere focuses what it creates, which for a split would move the
// user into the new empty-handed pane and demote the one they were
// actually working in to the partner. Coming back first means "split"
// consistently reads as "keep me where I am, put the other one beside
// me", however the split was asked for.
func (r *Root) newTabForSplit() (int, bool) {
	origin := r.activeTab
	before := len(r.tabs)

	r.newTabHere()
	if len(r.tabs) == before {
		return 0, false // refused (see maxTabs) — newTab already said so
	}

	created := len(r.tabs) - 1
	r.switchToTab(origin)
	return created, true
}

// adjustSplitForClosedTab keeps splitPanes pointing at the right tabs
// after the one at index closed has been removed from r.tabs.
//
// Every index above the closed one shifts down by one — the same reason
// closeTab itself has to renumber. A pane whose own tab was the one
// closed has nothing left to show, so split view ends rather than
// silently pairing the survivor with some unrelated neighbour that
// happened to slide into the same index.
func (r *Root) adjustSplitForClosedTab(closed int) {
	for slot, tab := range r.splitPanes {
		switch {
		case tab == closed:
			r.splitActive = false
			r.splitPanes = [2]int{-1, -1}
			return
		case tab > closed:
			r.splitPanes[slot] = tab - 1
		}
	}
	if r.splitActive && !r.splitPanesValid() {
		r.splitActive = false
	}
}

// splitToggleLabel renders the context menu's split entry as the action
// selecting it performs next, not the current state — the same
// convention hiddenToggleLabel already established for the "Globals"
// toggles (see its own doc comment).
func splitToggleLabel(active bool) string {
	if active {
		return "Close split view"
	}
	return "Split view"
}

// splitButtonLabel is the button bar's own shorter version of the same
// toggle (see buildButtonBar) — "Split" / "Unsplit", matching that row's
// own terse vocabulary rather than the context menu's fuller wording,
// exactly as hideUnhideLabel already does beside the menu's "Show/Hide
// hidden files".
func splitButtonLabel(active bool) string {
	if active {
		return "Unsplit"
	}
	return "Split"
}

// splitOrientationLabel is the same for the orientation entry: it names
// the arrangement selecting it would switch *to*, not the one currently
// in force.
func splitOrientationLabel(stacked bool) string {
	if stacked {
		return "Split side by side"
	}
	return "Split above/below"
}

// syncSplitMenuLabels re-renders both split entries from the current
// state — called wherever either can change (see setSplitStacked,
// enterSplit/exitSplit, and switchToTab, which can end a split by
// implication).
func (r *Root) syncSplitMenuLabels() {
	if r.menu == nil {
		return
	}
	r.menu.SetItemText(r.splitToggleIdx, splitToggleLabel(r.splitActive), "")
	r.menu.SetItemText(r.splitOrientationIdx, splitOrientationLabel(r.settings.SplitStacked), "")
}

// --- Actions reachable from the UI -----------------------------------

// ToggleSplitShortcut guards toggleSplit the way every other global
// shortcut in this package does (see acceptsGlobalShortcut) — show a
// second pane, or go back to one.
//
// Split view's own real keyboard path is the plain-letter layer's "s"
// (see keymap.go), which calls toggleSplit directly and needs no such
// guard (acceptsPlainKeyCommand already covers the same ground more
// precisely) — so nothing in cmd/breakthrough currently calls this.
// Kept as an exported building block, the same shape every shortcut
// here already has, for whatever future caller wants the guarded form.
func (r *Root) ToggleSplitShortcut() {
	if !r.acceptsGlobalShortcut() {
		return
	}
	r.toggleSplit()
}

// toggleSplit is ToggleSplitShortcut's own body, without the shortcut
// guard — also the context menu's "Split view" entry, which is already
// only reachable from a state where acting is fine.
func (r *Root) toggleSplit() {
	if r.splitActive {
		r.exitSplit()
		return
	}
	partner, ok := r.splitPartnerForToggle()
	if !ok {
		return
	}
	r.enterSplit(partner)
}

// SplitOrientationShortcut guards toggleSplitStacked the way
// ToggleSplitShortcut guards toggleSplit — see its own doc comment on
// why nothing currently calls this either (the real keyboard path is
// the "z" chord's own "o" member, see keymap.go).
//
// Works whether or not split view is currently showing — setting the
// orientation you want before turning the split on is a reasonable thing
// to do, and refusing the key outside split view would just be a rule to
// remember. The change is persisted like every other setting the user
// can flip live (see toggleHidden and friends), so it survives a restart
// and can equally be set from the Options screen or a config file.
func (r *Root) SplitOrientationShortcut() {
	if !r.acceptsGlobalShortcut() {
		return
	}
	r.toggleSplitStacked()
}

// toggleSplitStacked flips the stacked/side-by-side preference and, if
// split view is currently showing, re-lays it out immediately.
func (r *Root) toggleSplitStacked() {
	r.setSplitStacked(!r.settings.SplitStacked)
}

// setSplitStacked applies a specific orientation — toggleSplitStacked's
// own body with the value passed in rather than derived by flipping, so
// the Options screen can set one directly through exactly the same path
// (the same split setShowHidden already makes for the hidden-files
// toggle, and for the same reason: one place that knows what applying
// this actually entails).
func (r *Root) setSplitStacked(stacked bool) {
	r.settings.SplitStacked = stacked
	r.persistSetting("split_stacked", strconv.FormatBool(stacked))
	if r.splitActive {
		r.remountPanels()
	}
	r.syncSplitMenuLabels()
}

// splitWithTab is the tab switcher's own "split with this one" action
// (see tabswitcher.go): show tab i beside the current one.
//
// Choosing the same tab that's already the other pane is treated as
// "yes, that one" and simply leaves the split as it is, rather than
// refusing — from the switcher's point of view the user asked for a
// state that is already true.
func (r *Root) splitWithTab(i int) {
	if i < 0 || i >= len(r.tabs) {
		return
	}
	if i == r.activeTab {
		// Splitting a tab with itself isn't a layout: give the user the
		// second pane they clearly wanted, on a new tab showing the same
		// place — the same answer splitPartnerForToggle gives when
		// there's only one tab to work with.
		r.toggleSplit()
		return
	}
	r.enterSplit(i)
}

// splitWithNewTab is the switcher's own "new tab, split with it" action:
// open another tab on the current directory and put it in the second
// pane.
//
// The single most common way to want a split — two views of the same
// tree, one to copy from and one to copy to — which is why it has its
// own row rather than being two separate steps.
func (r *Root) splitWithNewTab() {
	created, ok := r.newTabForSplit()
	if !ok {
		return
	}
	r.enterSplit(created)
}

// swapPanes exchanges the two panes' positions on screen: what was on
// the left is now on the right, and vice versa.
//
// Only splitPanes changes — activeTab is left alone, so the pane you
// were working in stays the one with the keyboard and simply moves to
// the other side. Swapping focus as well would make this two actions in
// one and leave no way to ask for just this one; moving between panes
// is already Tab's own job (see CycleFocusShortcut).
//
// This is the one operation the fixed screen order (see this file's own
// doc comment) deliberately doesn't do on its own: panes never reorder
// themselves as a side effect of anything, so the only way they change
// sides is because someone asked.
func (r *Root) swapPanes() bool {
	if !r.splitActive || !r.splitPanesValid() {
		return false
	}
	r.splitPanes[0], r.splitPanes[1] = r.splitPanes[1], r.splitPanes[0]
	r.remountPanels()
	r.refreshTabStrips()
	return true
}

// swapPanesOrExplain is the "S" key's own action: swap the two panes, or
// say why there's nothing to swap.
//
// Saying so matters more here than for most verbs. Split view is the
// precondition, it is not obvious from a single pane that the key even
// needs one, and a key that silently does nothing reads as broken
// rather than as inapplicable — the same reason an unrecognized chord
// second key names itself instead of failing quietly (see resolveChord).
func (r *Root) swapPanesOrExplain() {
	if r.swapPanes() {
		return
	}
	r.showError(fmt.Errorf(`there are no panes to swap — press "s" to split the window first`))
}
