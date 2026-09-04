package ui

import (
	"fmt"

	"github.com/jagottsicher/breakthrough/internal/config"
	"github.com/jagottsicher/breakthrough/internal/session"
)

// Panel tabs: several independent browsing contexts in one window, one
// visible at a time, switched by Ctrl+1..Ctrl+0, Ctrl+Tab, the number
// strip in the panel header (see tabstrip.go), or the switcher overlay
// (see tabswitcher.go).
//
// Each tab is a whole real *Panel, not a saved bundle of a live panel's
// fields. That's the central design decision here and it's deliberate: a
// browsing context is far more than its directory — it's also the
// navigation history behind Back/Forward, the filter box's contents and
// mode, the sort column, the checkbox selection, the cursor row, and any
// in-progress search results. Reproducing all of that as an explicit
// save/restore struct would mean enumerating every per-tab field on
// Panel correctly, and re-enumerating it every time a future field is
// added — a silent-drift bug waiting to happen, where a forgotten field
// leaks from one tab into another. Giving each tab its own Panel gets all
// of it right by construction, and costs only the memory of the extra
// tables.
//
// r.panel continues to mean "the panel the user is looking at" throughout
// the rest of this package, so every existing call site keeps working
// untouched; switching a tab repoints it (see switchToTab).

// tabsHost is the Pages primitive holding one page per tab (see
// Root.panelHost). Named as a constant prefix rather than built inline so
// the page name and the lookup can't drift apart.
const tabsPagedPrefix = "tab-"

// tabPageName is the Pages key for tab i.
func tabPageName(i int) string {
	return fmt.Sprintf("%s%d", tabsPagedPrefix, i)
}

// maxTabs caps how many tabs can be open at once.
//
// Not a UI constraint (the strip windows itself past ten — see
// tabStripWindow — and the switcher scrolls), and deliberately far above
// the ten that have their own Ctrl+digit shortcut: the user's own stated
// position is that there should be no artificial ceiling, since a tab
// costs little more than the path it holds. This exists only as a
// backstop against a runaway loop or a corrupted saved layout opening
// tabs without end, not as a limit anyone browsing normally should ever
// meet.
const maxTabs = 64

// tabCount is how many tabs are currently open.
func (r *Root) tabCount() int { return len(r.tabs) }

// newTab opens a new tab showing path and switches to it.
//
// The new panel is built with the same theme and settings-derived
// "Globals" toggles the existing tabs are currently using — read off the
// active panel rather than from r.settings, so a toggle flipped during
// this session (which updates every live panel, see forEachTab) carries
// into a tab opened afterwards instead of reverting to whatever was last
// written to disk.
func (r *Root) newTab(path string) {
	if len(r.tabs) >= maxTabs {
		r.showError(fmt.Errorf("too many tabs open (limit %d)", maxTabs))
		return
	}

	panel, err := NewPanel(r.app, path, r.theme, r.tabSettings())
	if err != nil {
		r.showError(err)
		return
	}
	r.wirePanel(panel)

	r.tabs = append(r.tabs, panel)
	r.panelHost.AddPage(tabPageName(len(r.tabs)-1), panel, true, false)
	r.switchToTab(len(r.tabs) - 1)
}

// tabSettings is the settings snapshot a newly created tab's Panel starts
// from: r.settings with the three "Globals" toggles overridden by
// whatever the currently active panel actually has right now — see
// newTab's own doc comment for why the live value wins over the stored
// one.
func (r *Root) tabSettings() config.Settings {
	s := r.settings
	if r.panel != nil {
		s.ShowHidden = r.panel.showHidden
		s.SizeBytes = r.panel.sizeBytes
		s.MtimeUnix = r.panel.mtimeUnix
	}
	return s
}

// closeTab closes tab i, switching to a neighbour if it was the active
// one.
//
// Closing the last remaining tab is refused outright rather than quietly
// quitting the application or leaving an empty window: "close" and "quit"
// being the same gesture under some conditions is a classic way to lose
// work by accident, and Ctrl+Q already exists (with its own confirmation)
// for actually leaving.
func (r *Root) closeTab(i int) {
	if i < 0 || i >= len(r.tabs) {
		return
	}
	if len(r.tabs) == 1 {
		r.showError(fmt.Errorf("this is the last tab — use Ctrl+Q to quit breakthrough"))
		return
	}

	// Pages keys are positional (tab-0, tab-1, ...), so removing one from
	// the middle would leave every later tab's key pointing at the wrong
	// index. Rather than renaming pages around, every page is dropped and
	// re-added from the new slice — there are at most maxTabs of them and
	// this only runs on an explicit close, so the simplicity is worth
	// more here than avoiding the rebuild.
	for idx := range r.tabs {
		r.panelHost.RemovePage(tabPageName(idx))
	}
	r.tabs = append(r.tabs[:i], r.tabs[i+1:]...)
	for idx, p := range r.tabs {
		r.panelHost.AddPage(tabPageName(idx), p, true, false)
	}

	// Land on the tab that took the closed one's place, or the new last
	// one if it was at the end — the same "stay where you were" behaviour
	// browsers and editors have.
	active := r.activeTab
	switch {
	case i < active:
		active--
	case i == active && active > len(r.tabs)-1:
		active = len(r.tabs) - 1
	}
	r.activeTab = -1 // force switchToTab to do a real switch even to the same index
	r.switchToTab(active)
}

// switchToTab makes tab i the visible one.
//
// Repoints r.panel, which is what the whole rest of this package means by
// "the panel", so no other call site needs to know tabs exist at all.
// Also re-syncs everything that's derived from the active panel and would
// otherwise still be describing the tab we just left: the status bar's
// disk usage, the Details sidebar's current entry, and the context menu's
// own "Globals" labels.
func (r *Root) switchToTab(i int) {
	if i < 0 || i >= len(r.tabs) {
		return
	}
	if i == r.activeTab {
		return
	}

	// Leaving a tab mid-edit would strand its header edit field visible
	// but unreachable — the same reason RequestQuit cancels it too.
	if r.panel != nil {
		r.panel.cancelEdit()
	}

	r.activeTab = i
	r.panel = r.tabs[i]
	r.panelHost.SwitchToPage(tabPageName(i))

	r.refreshTabStrips()
	r.syncGlobalsMenuLabels()
	r.refreshStatusBar()
	r.refreshDetailsSidebar()

	// Focus follows the switch: a tab you just switched to that didn't
	// take keyboard focus with it would leave arrow keys driving the tab
	// you just left, which is still mounted and still focusable.
	r.app.SetFocus(r.panel.table)
}

// refreshTabStrips pushes the current tab count and active index into
// every tab's own strip — not just the visible one.
//
// All of them, because a tab that isn't currently on screen still has to
// be correct the instant it's switched to, and a strip that only updated
// while visible would briefly show a stale count at exactly that moment.
func (r *Root) refreshTabStrips() {
	for _, p := range r.tabs {
		p.setTabs(len(r.tabs), r.activeTab)
	}
}

// forEachTab runs fn against every open tab's panel — the propagation
// path for anything that is global to the application rather than local
// to one browsing context.
//
// The "Globals" toggles (hidden files, size format, time format) and the
// color scheme are exactly that, and every one of them predates tabs, so
// each was written against the single panel that used to exist. Without
// this, flipping one would change only the visible tab and leave the
// others silently disagreeing until they were next switched to.
func (r *Root) forEachTab(fn func(*Panel)) {
	for _, p := range r.tabs {
		fn(p)
	}
}

// tabPaths is every open tab's current directory, in tab order — what
// gets saved for the next run (see saveTabs).
//
// A tab showing search results has no real directory on screen, but
// Panel.path is deliberately left frozen at the directory the search
// started from throughout search mode (see Panel.showSearchResults), so
// this needs no special case: such a tab restores to where it was
// searching from, which is the useful answer anyway.
func (r *Root) tabPaths() []string {
	paths := make([]string, 0, len(r.tabs))
	for _, p := range r.tabs {
		paths = append(paths, p.path)
	}
	return paths
}

// saveTabs writes the current layout for the next run — called on a
// clean exit (see confirmQuit).
//
// Silent on failure, deliberately: this runs while the application is
// already on its way out, where there's no longer a UI to show an error
// in, and a lost tab layout is not worth blocking a quit over.
func (r *Root) saveTabs() {
	_ = session.SaveTabs(session.TabsPath(), session.TabState{
		Paths:  r.tabPaths(),
		Active: r.activeTab,
	})
}

// RestoreSavedTabs reopens the tabs saved by the previous run (see
// session.LoadTabs), if the restore_tabs setting allows it.
//
// Called by cmd/breakthrough only when breakthrough was started without
// an explicit directory argument — see the restore_tabs setting's own
// doc comment in internal/config for why an explicit path suppresses
// this entirely.
//
// The tab that was active is restored last so it ends up focused. A saved
// path that no longer exists (a removed USB drive, a deleted project
// directory) is skipped rather than aborting the whole restore — NewPanel
// itself fails on an unreadable directory, and one stale entry shouldn't
// cost the user every other tab. If nothing at all can be restored, the
// single tab NewRoot already opened is simply left as it is.
func (r *Root) RestoreSavedTabs() {
	if !r.settings.RestoreTabs {
		return
	}
	state, err := session.LoadTabs(session.TabsPath())
	if err != nil || len(state.Paths) == 0 {
		return
	}

	// The first saved tab replaces the one NewRoot opened, rather than
	// being added alongside it — otherwise every restore would leave a
	// stray extra tab at the front showing the working directory.
	restored := 0
	for i, path := range state.Paths {
		if restored >= maxTabs {
			break
		}
		if i == 0 {
			if err := r.tabs[0].navigate(path); err != nil {
				continue // stale path — leave tab 0 where NewRoot put it
			}
			restored++
			continue
		}
		if len(r.tabs) >= maxTabs {
			break
		}
		panel, err := NewPanel(r.app, path, r.theme, r.tabSettings())
		if err != nil {
			continue // see this func's own doc comment: skip, don't abort
		}
		r.wirePanel(panel)
		r.tabs = append(r.tabs, panel)
		r.panelHost.AddPage(tabPageName(len(r.tabs)-1), panel, true, false)
		restored++
	}

	r.refreshTabStrips()

	// Clamped against what actually came back, not against what was
	// saved: skipped stale paths mean the saved index can now point past
	// the end.
	active := state.Active
	if active < 0 || active >= len(r.tabs) {
		active = 0
	}
	r.activeTab = -1 // see closeTab: force a real switch
	r.switchToTab(active)
}

// --- Actions reachable from the UI -----------------------------------

// newTabHere opens a new tab on the same directory the current one is
// showing — the context menu's "New tab" and the strip's own "+" button.
//
// Same directory rather than $HOME or the working directory: opening a
// second view of where you already are is the overwhelmingly common
// reason to want another tab (compare two subdirectories, keep a
// reference point while wandering off), and going somewhere else from
// there is one navigation away.
func (r *Root) newTabHere() {
	r.newTab(r.panel.path)
}

// closeCurrentTab is the context menu's "Close tab".
func (r *Root) closeCurrentTab() {
	r.closeTab(r.activeTab)
}

// SwitchToTabShortcut is Ctrl+1..Ctrl+9 and Ctrl+0's own action (see
// cmd/breakthrough), with n the 1-based tab number the user typed and
// Ctrl+0 meaning 10 — the numbering the strip itself shows.
//
// Silently does nothing when there's no such tab, rather than reporting
// an error: reaching for Ctrl+7 with four tabs open is a miss, not a
// mistake worth an error overlay.
func (r *Root) SwitchToTabShortcut(n int) {
	if !r.acceptsGlobalShortcut() {
		return
	}
	r.switchToTab(n - 1)
}

// NextTabShortcut and PrevTabShortcut are Ctrl+Tab and Ctrl+Shift+Tab
// (see cmd/breakthrough).
//
// Both open the switcher overlay rather than switching immediately, with
// the neighbouring tab preselected — the user's own explicit design for
// this: an overlay listing every tab's real path, showing which one
// you're on and which one you're about to move to, since the strip's
// numbers deliberately don't say what any tab holds. Pressing the same
// key again moves the selection further, Enter commits, Escape leaves
// the current tab alone.
//
// That two-step is a concession to the terminal, not a preference: a
// true hold-Ctrl-and-tab-through switcher needs a key *release* event to
// know when to commit, and terminal input has no such thing — there is
// no way to be told the modifier was let go.
func (r *Root) NextTabShortcut() { r.stepTabSwitcher(1) }

// PrevTabShortcut is NextTabShortcut in the other direction.
func (r *Root) PrevTabShortcut() { r.stepTabSwitcher(-1) }

// TabSwitcherShortcut opens the switcher without moving the selection —
// F4's own action (see cmd/breakthrough), the keyboard path that works
// on every terminal.
//
// Ctrl+Tab and Ctrl+1..Ctrl+0 both depend on the terminal actually
// reporting the modifier, which needs one of the enhanced keyboard
// protocols tcell requests at startup (kitty's CSI-u, xterm's
// modifyOtherKeys). Modern terminals answer; older ones silently don't,
// and there send a bare Tab or a bare digit that this app can't tell
// from the unmodified key. F4 is a plain function key with no such
// dependency, so the feature is always reachable from the keyboard
// regardless — the same reason F1/F2/F3 exist alongside their own
// Ctrl-letter neighbours here.
// Opening and closing tabs deliberately have no global keybinding of
// their own: both live inside the switcher this opens (its "New tab" row
// and Delete — see tabswitcher.go), alongside the context menu's own
// "Tabs" section. Every remaining Ctrl letter is either already claimed
// here or collides with the bash line's own readline editing — Ctrl+W,
// the obvious borrow from browsers for "close tab", deletes a word there
// — and one key that reaches all of tab management is easier to remember
// than three that each do a piece of it.
func (r *Root) TabSwitcherShortcut() {
	if !r.acceptsGlobalShortcut() {
		return
	}
	r.openTabSwitcher(r.activeTab)
}
