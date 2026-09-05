package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/jagottsicher/breakthrough/internal/session"
)

// newTabbedRoot is the shared setup for the tests here: a Root with an
// isolated $XDG_STATE_HOME (so nothing touches the real saved layout)
// and a second directory to open tabs on.
func newTabbedRoot(t *testing.T) (*Root, string, string) {
	t.Helper()
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	dir := fixtureDir(t)
	other := t.TempDir()
	if err := os.WriteFile(filepath.Join(other, "elsewhere.txt"), []byte("x"), 0o600); err != nil {
		t.Fatalf("writing fixture: %v", err)
	}

	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	return r, dir, other
}

// TestNewRootStartsWithExactlyOneTab pins the baseline: tabs are opt-in,
// and a fresh Root behaves exactly like the single-panel one it replaced.
func TestNewRootStartsWithExactlyOneTab(t *testing.T) {
	r, dir, _ := newTabbedRoot(t)

	if got := r.tabCount(); got != 1 {
		t.Errorf("tab count = %d, want 1", got)
	}
	if r.activeTab != 0 {
		t.Errorf("activeTab = %d, want 0", r.activeTab)
	}
	if r.panel != r.tabs[0] {
		t.Error("r.panel is not tabs[0]")
	}
	if r.panel.path != dir {
		t.Errorf("panel path = %q, want %q", r.panel.path, dir)
	}
}

// TestNewTabOpensAndSwitchesToIt pins that opening a tab also makes it
// the visible one — the behaviour every tabbed application has.
func TestNewTabOpensAndSwitchesToIt(t *testing.T) {
	r, _, other := newTabbedRoot(t)

	r.newTab(other)

	if got := r.tabCount(); got != 2 {
		t.Fatalf("tab count = %d, want 2", got)
	}
	if r.activeTab != 1 {
		t.Errorf("activeTab = %d, want 1", r.activeTab)
	}
	if r.panel != r.tabs[1] {
		t.Error("r.panel was not repointed to the new tab")
	}
	if r.panel.path != other {
		t.Errorf("new tab's path = %q, want %q", r.panel.path, other)
	}
}

// TestTabsKeepIndependentDirectories is the core promise of the whole
// feature: each tab is its own browsing context, and navigating one
// leaves the other exactly where it was.
func TestTabsKeepIndependentDirectories(t *testing.T) {
	r, dir, other := newTabbedRoot(t)
	r.newTab(other)

	// Navigate the second tab somewhere else entirely.
	if err := r.panel.navigate(dir); err != nil {
		t.Fatalf("navigate: %v", err)
	}

	r.switchToTab(0)
	if r.panel.path != dir {
		t.Errorf("tab 1 path = %q, want %q", r.panel.path, dir)
	}
	r.switchToTab(1)
	if r.panel.path != dir {
		t.Errorf("tab 2 path = %q, want %q", r.panel.path, dir)
	}

	// And the reverse: moving tab 1 doesn't disturb tab 2.
	r.switchToTab(0)
	if err := r.panel.navigate(other); err != nil {
		t.Fatalf("navigate: %v", err)
	}
	r.switchToTab(1)
	if r.panel.path != dir {
		t.Errorf("tab 2 path after moving tab 1 = %q, want it untouched at %q", r.panel.path, dir)
	}
}

// TestTabsKeepIndependentFilters pins that per-tab state genuinely means
// everything a browsing context carries, not just the directory — the
// filter box is the most visible of the rest, and the reason each tab is
// a whole Panel rather than a saved path (see tabs.go's own doc comment).
func TestTabsKeepIndependentFilters(t *testing.T) {
	r, _, other := newTabbedRoot(t)

	r.panel.filterText = "apple"
	r.newTab(other)

	if got := r.panel.filterText; got != "" {
		t.Errorf("new tab's filter = %q, want empty — it must not inherit the other tab's", got)
	}
	r.switchToTab(0)
	if got := r.panel.filterText; got != "apple" {
		t.Errorf("original tab's filter = %q, want %q still set", got, "apple")
	}
}

// TestSwitchToTabIgnoresOutOfRange pins that a stray Ctrl+7 with three
// tabs open is simply inert, rather than panicking or landing somewhere
// arbitrary.
func TestSwitchToTabIgnoresOutOfRange(t *testing.T) {
	r, _, other := newTabbedRoot(t)
	r.newTab(other)

	for _, i := range []int{-1, 2, 99} {
		before := r.activeTab
		r.switchToTab(i)
		if r.activeTab != before {
			t.Errorf("switchToTab(%d) moved activeTab to %d, want it left at %d", i, r.activeTab, before)
		}
	}
}

// TestCloseTabRefusesTheLastOne pins the deliberate guard: closing the
// only tab must never quietly quit the application or leave an empty
// window (see closeTab's own doc comment).
func TestCloseTabRefusesTheLastOne(t *testing.T) {
	r, _, _ := newTabbedRoot(t)

	r.closeTab(0)

	if got := r.tabCount(); got != 1 {
		t.Errorf("tab count = %d, want the last tab kept", got)
	}
	if r.activePage != errorPage {
		t.Errorf("activePage = %q, want the error overlay explaining why", r.activePage)
	}
}

// TestCloseTabKeepsTheRemainingOnesInOrder pins that closing from the
// middle renumbers cleanly — the Pages keys are positional, so this is
// exactly where an off-by-one would strand a panel on the wrong page.
func TestCloseTabKeepsTheRemainingOnesInOrder(t *testing.T) {
	r, dir, other := newTabbedRoot(t)
	r.newTab(other)
	r.newTab(dir)
	first, third := r.tabs[0], r.tabs[2]

	r.closeTab(1) // the middle one

	if got := r.tabCount(); got != 2 {
		t.Fatalf("tab count = %d, want 2", got)
	}
	if r.tabs[0] != first || r.tabs[1] != third {
		t.Error("the surviving tabs are not in their original relative order")
	}
	// The visible page must be the active tab's own, not a stale key.
	name, _ := r.panelHost.GetFrontPage()
	if want := tabPageName(r.activeTab); name != want {
		t.Errorf("front page = %q, want %q", name, want)
	}
	if r.panel != r.tabs[r.activeTab] {
		t.Error("r.panel is not the active tab after a close")
	}
}

// TestCloseActiveTabLandsOnANeighbour pins where focus goes when the tab
// you're on disappears — the "stay where you were" behaviour browsers and
// editors have, rather than jumping to the first tab.
func TestCloseActiveTabLandsOnANeighbour(t *testing.T) {
	r, dir, other := newTabbedRoot(t)
	r.newTab(other)
	r.newTab(dir)

	r.switchToTab(1)
	r.closeTab(1)
	if r.activeTab != 1 {
		t.Errorf("activeTab = %d, want 1 (the tab that took its place)", r.activeTab)
	}

	// Closing the last tab in the row falls back to the new last one.
	r.switchToTab(1)
	r.closeTab(1)
	if r.activeTab != 0 {
		t.Errorf("activeTab = %d, want 0 (the new last tab)", r.activeTab)
	}
}

// TestCloseTabBeforeActiveShiftsTheIndex pins the other renumbering
// direction: closing a tab to the left of the active one must keep the
// same tab visible, at its new index.
func TestCloseTabBeforeActiveShiftsTheIndex(t *testing.T) {
	r, dir, other := newTabbedRoot(t)
	r.newTab(other)
	r.newTab(dir)
	r.switchToTab(2)
	stillWant := r.tabs[2]

	r.closeTab(0)

	if r.activeTab != 1 {
		t.Errorf("activeTab = %d, want 1 (shifted down by the close)", r.activeTab)
	}
	if r.panel != stillWant {
		t.Error("a different tab became active — the same one should have stayed visible")
	}
}

// TestGlobalTogglesApplyToEveryTab pins that the "Globals" section of the
// context menu means what it says: a toggle reaches every open tab, not
// just the visible one (see forEachTab's own doc comment).
func TestGlobalTogglesApplyToEveryTab(t *testing.T) {
	r, _, other := newTabbedRoot(t)
	r.newTab(other)
	before := r.panel.showHidden

	r.toggleHidden()

	for i, p := range r.tabs {
		if p.showHidden == before {
			t.Errorf("tab %d still has showHidden = %v after the toggle", i+1, before)
		}
	}
}

// TestNewTabInheritsLiveGlobalToggles pins that a tab opened after a
// toggle was flipped starts out agreeing with the others, rather than
// reverting to whatever was last written to disk (see tabSettings).
func TestNewTabInheritsLiveGlobalToggles(t *testing.T) {
	r, _, other := newTabbedRoot(t)
	r.toggleHidden()
	want := r.panel.showHidden

	r.newTab(other)

	if got := r.panel.showHidden; got != want {
		t.Errorf("new tab's showHidden = %v, want %v (matching the live setting)", got, want)
	}
}

// TestTabStripsStayInSyncAcrossEveryTab pins that a background tab's own
// strip is already correct before it's switched to — otherwise it would
// visibly flicker to the right value at exactly the wrong moment.
func TestTabStripsStayInSyncAcrossEveryTab(t *testing.T) {
	r, dir, other := newTabbedRoot(t)
	r.newTab(other)
	r.newTab(dir)

	for i, p := range r.tabs {
		if p.tabCount != 3 {
			t.Errorf("tab %d's strip shows %d tabs, want 3", i+1, p.tabCount)
		}
		if p.tabActive != r.activeTab {
			t.Errorf("tab %d's strip marks %d active, want %d", i+1, p.tabActive, r.activeTab)
		}
	}
}

// TestMenuSelectAllTargetsTheActiveTab pins a real trap the tab work
// introduced: the context menu's Select all/Deselect all used to be bound
// method values, capturing the first tab's panel forever. They must
// follow r.panel instead.
func TestMenuSelectAllTargetsTheActiveTab(t *testing.T) {
	r, _, other := newTabbedRoot(t)
	r.newTab(other)

	idx := -1
	for i := 0; i < r.menu.GetItemCount(); i++ {
		if main, _ := r.menu.GetItemText(i); main == "Select all" {
			idx = i
			break
		}
	}
	if idx < 0 {
		t.Fatal(`no "Select all" item in the context menu`)
	}
	r.menu.SetCurrentItem(idx)
	r.menu.InputHandler()(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone), func(tview.Primitive) {})

	if len(r.tabs[1].selected) == 0 {
		t.Error("Select all did nothing to the active (second) tab")
	}
	if len(r.tabs[0].selected) != 0 {
		t.Error("Select all reached the first tab — it must target whichever tab is active")
	}
}

// --- Shortcuts --------------------------------------------------------

// TestSwitchToTabShortcutUsesOneBasedNumbers pins the Ctrl+1..Ctrl+0
// mapping against the numbering the strip actually shows.
func TestSwitchToTabShortcutUsesOneBasedNumbers(t *testing.T) {
	r, dir, other := newTabbedRoot(t)
	r.newTab(other)
	r.newTab(dir)

	r.SwitchToTabShortcut(1)
	if r.activeTab != 0 {
		t.Errorf("Ctrl+1 landed on tab index %d, want 0", r.activeTab)
	}
	r.SwitchToTabShortcut(3)
	if r.activeTab != 2 {
		t.Errorf("Ctrl+3 landed on tab index %d, want 2", r.activeTab)
	}
	// A number with no tab behind it is a miss, not an error.
	r.SwitchToTabShortcut(9)
	if r.activeTab != 2 {
		t.Errorf("Ctrl+9 with three tabs moved to %d, want it left at 2", r.activeTab)
	}
	if r.activePage != "" {
		t.Errorf("activePage = %q, want nothing opened by a missed shortcut", r.activePage)
	}
}

// TestSwitchToTabShortcutBlockedWhileAnOverlayIsOpen pins the rule agreed
// for every tab shortcut: switching the directory out from under an open
// dialog (Properties, say, which is showing one specific file) would be
// incoherent, so the shortcut stands down instead.
func TestSwitchToTabShortcutBlockedWhileAnOverlayIsOpen(t *testing.T) {
	r, dir, other := newTabbedRoot(t)
	r.newTab(other)
	r.target = filepath.Join(dir, "apple.txt")
	r.openProperties()

	r.SwitchToTabShortcut(1)

	if r.activeTab != 1 {
		t.Errorf("activeTab = %d, want it left at 1 while Properties is open", r.activeTab)
	}
	if r.activePage != propertiesPage {
		t.Errorf("activePage = %q, want %q still open", r.activePage, propertiesPage)
	}
}

// TestNextTabShortcutOpensTheSwitcherOnTheNeighbour pins the user's own
// design for Ctrl+Tab: it opens the switcher preselected on the next tab
// rather than switching immediately, so you can see where you're going
// before committing.
func TestNextTabShortcutOpensTheSwitcherOnTheNeighbour(t *testing.T) {
	r, dir, other := newTabbedRoot(t)
	r.newTab(other)
	r.newTab(dir)
	r.switchToTab(0)

	r.NextTabShortcut()

	if r.activePage != tabSwitcherPage {
		t.Fatalf("activePage = %q, want %q", r.activePage, tabSwitcherPage)
	}
	if got := r.tabSwitcher.GetCurrentItem(); got != 1 {
		t.Errorf("switcher preselected row %d, want 1 (the next tab)", got)
	}
	if r.activeTab != 0 {
		t.Errorf("activeTab = %d, want 0 — opening the switcher must not switch yet", r.activeTab)
	}
}

// TestNextTabShortcutSteppedRepeatedlyKeepsMoving pins the Alt-Tab-style
// behaviour: with the switcher already open, the same key moves the
// selection further instead of being blocked as "an overlay is open".
func TestNextTabShortcutSteppedRepeatedlyKeepsMoving(t *testing.T) {
	r, dir, other := newTabbedRoot(t)
	r.newTab(other)
	r.newTab(dir)
	r.switchToTab(0)

	r.NextTabShortcut() // opens on 1
	r.NextTabShortcut() // -> 2
	if got := r.tabSwitcher.GetCurrentItem(); got != 2 {
		t.Errorf("selection after two steps = %d, want 2", got)
	}
	r.NextTabShortcut() // wraps back to 0
	if got := r.tabSwitcher.GetCurrentItem(); got != 0 {
		t.Errorf("selection after wrapping = %d, want 0", got)
	}
}

// TestPrevTabShortcutStepsBackwards pins the other direction, including
// the wrap below zero — where Go's own signed modulo needs the explicit
// correction stepTabSwitcher makes.
func TestPrevTabShortcutStepsBackwards(t *testing.T) {
	r, dir, other := newTabbedRoot(t)
	r.newTab(other)
	r.newTab(dir)
	r.switchToTab(0)

	r.PrevTabShortcut() // from tab 0, wrapping to the last

	if r.activePage != tabSwitcherPage {
		t.Fatalf("activePage = %q, want %q", r.activePage, tabSwitcherPage)
	}
	if got := r.tabSwitcher.GetCurrentItem(); got != 2 {
		t.Errorf("selection = %d, want 2 (wrapped to the last tab)", got)
	}
}

// TestTabCycleShortcutsDoNothingWithASingleTab pins that Ctrl+Tab with
// nothing to switch between doesn't open an overlay just to say so.
func TestTabCycleShortcutsDoNothingWithASingleTab(t *testing.T) {
	r, _, _ := newTabbedRoot(t)

	r.NextTabShortcut()

	if r.activePage != "" {
		t.Errorf("activePage = %q, want nothing opened with only one tab", r.activePage)
	}
}

// TestTabSwitcherShortcutOpensOnTheCurrentTab pins F4 — the always-
// available keyboard path (see TabSwitcherShortcut's own doc comment),
// which unlike Ctrl+Tab doesn't preselect a neighbour.
func TestTabSwitcherShortcutOpensOnTheCurrentTab(t *testing.T) {
	r, dir, other := newTabbedRoot(t)
	r.newTab(other)
	r.newTab(dir)
	r.switchToTab(1)

	r.TabSwitcherShortcut()

	if r.activePage != tabSwitcherPage {
		t.Fatalf("activePage = %q, want %q", r.activePage, tabSwitcherPage)
	}
	if got := r.tabSwitcher.GetCurrentItem(); got != 1 {
		t.Errorf("switcher preselected row %d, want 1 (the current tab)", got)
	}
}

// --- Switcher overlay -------------------------------------------------

// TestTabSwitcherListsEveryTabWithItsPath pins what the overlay is for:
// the numbers in the strip deliberately don't say what any tab holds, and
// this is where that information lives.
func TestTabSwitcherListsEveryTabWithItsPath(t *testing.T) {
	r, dir, other := newTabbedRoot(t)
	r.newTab(other)
	r.openTabSwitcher(r.activeTab)

	// Two tabs plus the trailing "New tab" row.
	if got, want := r.tabSwitcher.GetItemCount(), 3; got != want {
		t.Fatalf("switcher has %d rows, want %d", got, want)
	}
	if last, _ := r.tabSwitcher.GetItemText(2); last != tabSwitcherNewRowLabel {
		t.Errorf("last row = %q, want %q", last, tabSwitcherNewRowLabel)
	}
	for i, wantPath := range []string{dir, other} {
		main, _ := r.tabSwitcher.GetItemText(i)
		// Compared against the tail: a long path is shortened from the
		// left for display (see shortenPathLeft).
		tail := wantPath
		if len(tail) > tabSwitcherMaxPathWidth-1 {
			tail = tail[len(tail)-(tabSwitcherMaxPathWidth-1):]
		}
		if !strings.Contains(main, tail) {
			t.Errorf("row %d = %q, want it to contain %q", i, main, tail)
		}
	}
}

// TestTabSwitcherMarksTheCurrentTab pins the two-state display the user
// asked for: the tab you're on is dimmed and labelled, distinct from the
// row the selection highlight is sitting on.
func TestTabSwitcherMarksTheCurrentTab(t *testing.T) {
	r, _, other := newTabbedRoot(t)
	r.newTab(other) // active is now tab 2
	r.openTabSwitcher(0)

	current, _ := r.tabSwitcher.GetItemText(1)
	if !strings.Contains(current, "(current)") {
		t.Errorf("the active tab's row = %q, want it marked as current", current)
	}
	if !strings.Contains(current, colorTag(r.theme.PlaceholderText)) {
		t.Errorf("the active tab's row = %q, want it dimmed", current)
	}

	other0, _ := r.tabSwitcher.GetItemText(0)
	if strings.Contains(other0, "(current)") {
		t.Errorf("row 0 = %q, want only the active tab marked", other0)
	}
}

// TestTabSwitcherEnterCommitsTheSelection pins that activating a row
// closes the overlay and actually switches.
func TestTabSwitcherEnterCommitsTheSelection(t *testing.T) {
	r, _, other := newTabbedRoot(t)
	r.newTab(other)
	r.openTabSwitcher(0)

	r.tabSwitcher.InputHandler()(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone), func(tview.Primitive) {})

	if r.activeTab != 0 {
		t.Errorf("activeTab = %d, want 0", r.activeTab)
	}
	if r.activePage != "" {
		t.Errorf("activePage = %q, want the switcher closed", r.activePage)
	}
}

// TestTabSwitcherEscapeLeavesTheTabAlone pins the cancel path — the whole
// reason the switcher shows the current tab separately from the selected
// one is that backing out has to be possible.
func TestTabSwitcherEscapeLeavesTheTabAlone(t *testing.T) {
	r, _, other := newTabbedRoot(t)
	r.newTab(other)
	r.openTabSwitcher(0) // preselecting a *different* tab than the active one

	r.closeTabSwitcher() // Escape's own action

	if r.activeTab != 1 {
		t.Errorf("activeTab = %d, want it left at 1", r.activeTab)
	}
	if r.activePage != "" {
		t.Errorf("activePage = %q, want the switcher closed", r.activePage)
	}
}

// TestTabSwitcherNewTabRowOpensATab pins the keyboard path to creating a
// tab at all: the context menu opens on right-click only, so without this
// row the feature couldn't be entered without a mouse (see
// openTabSwitcher's own doc comment).
func TestTabSwitcherNewTabRowOpensATab(t *testing.T) {
	r, _, other := newTabbedRoot(t)
	r.newTab(other)
	r.openTabSwitcher(0)

	r.tabSwitcher.SetCurrentItem(2) // the "New tab" row
	r.tabSwitcher.InputHandler()(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone), func(tview.Primitive) {})

	if got := r.tabCount(); got != 3 {
		t.Errorf("tab count = %d, want 3", got)
	}
	if r.activePage != "" {
		t.Errorf("activePage = %q, want the switcher closed after opening a tab", r.activePage)
	}
}

// TestTabSwitcherDeleteClosesTheHighlightedTab pins Delete's own action —
// the counterpart to the "New tab" row, and likewise the only way to
// close a tab without a mouse.
func TestTabSwitcherDeleteClosesTheHighlightedTab(t *testing.T) {
	r, dir, other := newTabbedRoot(t)
	r.newTab(other)
	r.newTab(dir)
	r.openTabSwitcher(0)

	r.captureTabSwitcherKey(tcell.NewEventKey(tcell.KeyDelete, 0, tcell.ModNone))

	if got := r.tabCount(); got != 2 {
		t.Errorf("tab count = %d, want 2", got)
	}
	if r.activePage != tabSwitcherPage {
		t.Errorf("activePage = %q, want the switcher still open", r.activePage)
	}
	if got := r.tabSwitcher.GetItemCount(); got != 3 { // two tabs plus "New tab"
		t.Errorf("switcher has %d rows after the close, want 3 — it should have rebuilt", got)
	}
}

// TestTabSwitcherDeleteOnTheLastTabIsInert pins that Delete can't empty
// the window, and doesn't stack an error overlay over the switcher to
// say so either.
func TestTabSwitcherDeleteOnTheLastTabIsInert(t *testing.T) {
	r, _, _ := newTabbedRoot(t)
	r.openTabSwitcher(0)

	r.captureTabSwitcherKey(tcell.NewEventKey(tcell.KeyDelete, 0, tcell.ModNone))

	if got := r.tabCount(); got != 1 {
		t.Errorf("tab count = %d, want the last tab kept", got)
	}
	if r.activePage != tabSwitcherPage {
		t.Errorf("activePage = %q, want the switcher still open, no error overlay", r.activePage)
	}
}

// TestTabCycleSkipsTheNewTabRow pins that Ctrl+Tab steps between real
// tabs only — landing on a row that would create one isn't what "next
// tab" means.
func TestTabCycleSkipsTheNewTabRow(t *testing.T) {
	r, _, other := newTabbedRoot(t)
	r.newTab(other)
	r.switchToTab(0)

	r.NextTabShortcut() // opens on tab 2 (row 1)
	r.NextTabShortcut() // must wrap to tab 1 (row 0), not the "New tab" row 2

	if got := r.tabSwitcher.GetCurrentItem(); got != 0 {
		t.Errorf("selection = %d, want 0 — cycling must skip the \"New tab\" row", got)
	}
}

// TestTabSwitcherOpensNearTheStripNotAtThePanelsLeftEdge pins a real
// reported bug: the switcher used to anchor its own left edge to the tab
// strip's left edge, which sits hard against the screen's right side —
// with a switcher wide enough to hold a full path, that pushed most of
// it off-screen, and clampToPanel then yanked the whole thing back to
// the panel's far left edge, landing nowhere near what it opened from.
// Anchoring the switcher's own right edge to the strip's right edge
// keeps it opening up-and-to-the-left from the control, regardless of
// how wide the panel is.
func TestTabSwitcherOpensNearTheStripNotAtThePanelsLeftEdge(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}

	// A real screen, not a nil one: tview's Flex only lays out its
	// children during Draw, and Draw itself touches the screen for
	// styling — so a genuine (if simulated) screen is needed to get
	// tabStrip's own real rect the way a live draw cycle would.
	screen := tcell.NewSimulationScreen("")
	if err := screen.Init(); err != nil {
		t.Fatalf("screen.Init: %v", err)
	}
	defer screen.Fini()
	// Wide and short — the tab strip sits far to the right; the
	// switcher's own width, set by a full path up to
	// tabSwitcherMaxPathWidth, comfortably exceeds the room to the right
	// of it, which is exactly what reproduces the bug.
	screen.SetSize(100, 24)
	r.SetRect(0, 0, 100, 24)
	r.Draw(screen)

	r.newTab(dir)
	// A second draw: the tab newTab just created is a brand-new Panel
	// that has never been through a layout pass, so its own rect (and
	// tabStrip's) is still tview's own construction-time default (15x10)
	// until one happens — exactly what a live Application.Run() loop's
	// own continuous redraws do for free, and exactly what's needed here
	// to make tabStrip's rect (what tabStripAnchor actually reads) real.
	r.Draw(screen)
	stripX, _, stripWidth, _ := r.panel.tabStrip.GetRect()
	stripRight := stripX + stripWidth

	r.openTabSwitcher(0)
	x, _, width, _ := r.tabSwitcherLayout.GetRect()

	// The switcher's own width here (driven by fixtureDir's short path,
	// well under tabSwitcherMaxPathWidth) comfortably fits without
	// clampToPanel needing to intervene at all, so the fix's own
	// invariant — right edge flush with the strip's right edge — should
	// hold exactly, not just approximately.
	//
	// This is what actually distinguishes the fix from the old, buggy
	// left-edge anchor: the old code's clampToPanel fallback used the
	// *panel's* own right edge once it needed to intervene, which sits a
	// few columns past the strip's own right edge (the Details "<"
	// button occupies exactly that gap) — a real, if easy to
	// under-measure, difference that a looser "is it near enough" check
	// let slip through once already.
	if got, want := x+width, stripRight; got != want {
		t.Errorf("switcher's right edge = %d, want %d (flush with the tab strip's own right edge)", got, want)
	}
	if x < 0 {
		t.Errorf("switcher x = %d, want it kept on screen", x)
	}
}

// TestShortenPathLeftKeepsTheDistinctiveEnd pins the truncation
// direction: the deepest part of a path is what tells two tabs apart, so
// that's the half that survives.
func TestShortenPathLeftKeepsTheDistinctiveEnd(t *testing.T) {
	long := "/home/jens/development/some/very/deep/project/src/internal/thing"
	got := shortenPathLeft(long, 20)

	if len([]rune(got)) != 20 {
		t.Errorf("shortened to %d columns, want 20 — %q", len([]rune(got)), got)
	}
	if !strings.HasSuffix(got, "internal/thing") {
		t.Errorf("shortened = %q, want the tail kept", got)
	}
	if !strings.HasPrefix(got, "…") {
		t.Errorf("shortened = %q, want the cut marked", got)
	}
	if short := "/tmp"; shortenPathLeft(short, 20) != short {
		t.Errorf("a path that already fits was altered")
	}
}

// --- Persistence ------------------------------------------------------

// TestSaveTabsWritesEveryOpenTab pins what gets persisted on quit.
func TestSaveTabsWritesEveryOpenTab(t *testing.T) {
	r, dir, other := newTabbedRoot(t)
	r.newTab(other)

	r.saveTabs()

	state, err := session.LoadTabs(session.TabsPath())
	if err != nil {
		t.Fatalf("LoadTabs: %v", err)
	}
	if want := []string{dir, other}; !equalStrings(state.Paths, want) {
		t.Errorf("saved paths = %v, want %v", state.Paths, want)
	}
	if state.Active != 1 {
		t.Errorf("saved active = %d, want 1", state.Active)
	}
}

// TestRestoreSavedTabsReopensTheLayout is the round trip the user asked
// for: every tab back where it was, with the same one active.
func TestRestoreSavedTabsReopensTheLayout(t *testing.T) {
	state := t.TempDir()
	t.Setenv("XDG_STATE_HOME", state)
	dir := fixtureDir(t)
	other := t.TempDir()

	if err := session.SaveTabs(session.TabsPath(), session.TabState{
		Paths:  []string{dir, other},
		Active: 1,
	}); err != nil {
		t.Fatalf("SaveTabs: %v", err)
	}

	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.RestoreSavedTabs()

	if got := r.tabCount(); got != 2 {
		t.Fatalf("tab count = %d, want 2", got)
	}
	if r.tabs[0].path != dir || r.tabs[1].path != other {
		t.Errorf("restored paths = %q/%q, want %q/%q", r.tabs[0].path, r.tabs[1].path, dir, other)
	}
	if r.activeTab != 1 {
		t.Errorf("activeTab = %d, want 1", r.activeTab)
	}
}

// TestRestoreSavedTabsSkipsVanishedDirectories pins the degrade-don't-
// fail rule: a saved path that no longer exists (an unmounted drive, a
// deleted project) costs that one tab, not the whole restore.
func TestRestoreSavedTabsSkipsVanishedDirectories(t *testing.T) {
	state := t.TempDir()
	t.Setenv("XDG_STATE_HOME", state)
	dir := fixtureDir(t)
	gone := filepath.Join(t.TempDir(), "no-such-directory")

	if err := session.SaveTabs(session.TabsPath(), session.TabState{
		Paths:  []string{dir, gone, dir},
		Active: 2,
	}); err != nil {
		t.Fatalf("SaveTabs: %v", err)
	}

	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.RestoreSavedTabs()

	if got := r.tabCount(); got != 2 {
		t.Errorf("tab count = %d, want 2 (the vanished one skipped)", got)
	}
	if r.activeTab < 0 || r.activeTab >= r.tabCount() {
		t.Errorf("activeTab = %d, out of range for %d tabs", r.activeTab, r.tabCount())
	}
}

// TestRestoreSavedTabsHonoursTheSetting pins the restore_tabs opt-out.
func TestRestoreSavedTabsHonoursTheSetting(t *testing.T) {
	state := t.TempDir()
	t.Setenv("XDG_STATE_HOME", state)
	dir := fixtureDir(t)

	if err := session.SaveTabs(session.TabsPath(), session.TabState{
		Paths: []string{dir, dir, dir},
	}); err != nil {
		t.Fatalf("SaveTabs: %v", err)
	}

	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.settings.RestoreTabs = false
	r.RestoreSavedTabs()

	if got := r.tabCount(); got != 1 {
		t.Errorf("tab count = %d, want 1 — restore_tabs = false must suppress it", got)
	}
}

// TestRestoreSavedTabsWithNothingSavedIsANoop pins the first-run case.
func TestRestoreSavedTabsWithNothingSavedIsANoop(t *testing.T) {
	r, dir, _ := newTabbedRoot(t)

	r.RestoreSavedTabs()

	if got := r.tabCount(); got != 1 {
		t.Errorf("tab count = %d, want the single startup tab", got)
	}
	if r.panel.path != dir {
		t.Errorf("panel path = %q, want %q", r.panel.path, dir)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
