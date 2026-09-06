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

// mountedPanels is which tab panels are actually in panelHost right now
// — the ground truth for "what is on screen", read back off the real
// widget rather than inferred from r.splitActive, so a test can catch
// state and layout disagreeing with each other.
func mountedPanels(r *Root) []*Panel {
	var mounted []*Panel
	for i := 0; i < r.panelHost.GetItemCount(); i++ {
		if p, ok := r.panelHost.GetItem(i).(*Panel); ok {
			mounted = append(mounted, p)
		}
	}
	return mounted
}

// newSplitRoot is a Root with two tabs open on two different real
// directories, tab 0 active — the starting point for everything below.
func newSplitRoot(t *testing.T) (r *Root, first, second string) {
	t.Helper()
	first = t.TempDir()
	second = t.TempDir()
	if err := os.WriteFile(filepath.Join(second, "other.txt"), nil, 0o644); err != nil {
		t.Fatal(err)
	}

	isolateUserConfigFile(t)
	r, err := NewRoot(tview.NewApplication(), first)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.newTab(second)
	r.switchToTab(0)
	return r, first, second
}

func TestSingleTabMountsExactlyOnePanel(t *testing.T) {
	r, _, _ := newSplitRoot(t)

	mounted := mountedPanels(r)
	if len(mounted) != 1 || mounted[0] != r.panel {
		t.Errorf("mounted = %v, want only the active panel", mounted)
	}
}

func TestEnterSplitMountsBothPanes(t *testing.T) {
	r, _, _ := newSplitRoot(t)

	r.enterSplit(1)

	mounted := mountedPanels(r)
	if len(mounted) != 2 {
		t.Fatalf("mounted %d panels, want 2", len(mounted))
	}
	if mounted[0] != r.tabs[0] || mounted[1] != r.tabs[1] {
		t.Error("panes are not mounted in splitPanes' own screen order")
	}
	if !r.splitActive {
		t.Error("splitActive should be true")
	}
}

func TestEnterSplitKeepsTheActiveTabInTheFirstSlot(t *testing.T) {
	r, _, _ := newSplitRoot(t)
	r.switchToTab(1)

	r.enterSplit(0)

	if r.splitPanes[0] != 1 {
		t.Errorf("splitPanes = %v, want the active tab (1) in the first slot", r.splitPanes)
	}
	if r.activeTab != 1 {
		t.Errorf("activeTab = %d, want the tab we split *from* to stay focused", r.activeTab)
	}
}

func TestExitSplitReturnsToTheFocusedPaneAlone(t *testing.T) {
	r, _, _ := newSplitRoot(t)
	r.enterSplit(1)

	r.exitSplit()

	mounted := mountedPanels(r)
	if len(mounted) != 1 || mounted[0] != r.tabs[0] {
		t.Errorf("mounted = %v, want only the tab that had focus", mounted)
	}
	if r.splitActive {
		t.Error("splitActive should be false")
	}
}

func TestExitSplitRemembersThePairForNextTime(t *testing.T) {
	r, _, _ := newSplitRoot(t)
	r.enterSplit(1)
	r.exitSplit()

	r.toggleSplit()

	if !r.splitActive || r.splitPanes != [2]int{0, 1} {
		t.Errorf("splitActive=%v panes=%v, want the same pair back", r.splitActive, r.splitPanes)
	}
	if r.tabCount() != 2 {
		t.Errorf("tab count = %d, want no extra tab created for a pair we already had", r.tabCount())
	}
}

func TestToggleSplitWithOneTabOpensASecondOnTheSameDirectory(t *testing.T) {
	isolateUserConfigFile(t)
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}

	r.toggleSplit()

	if r.tabCount() != 2 {
		t.Fatalf("tab count = %d, want a second tab created", r.tabCount())
	}
	if !r.splitActive {
		t.Fatal("splitActive should be true")
	}
	if r.tabs[1].path != dir {
		t.Errorf("new pane shows %q, want the same directory %q", r.tabs[1].path, dir)
	}
	// The user stays in the pane they were already in — the new one joins
	// them, rather than moving them into it.
	if r.activeTab != 0 {
		t.Errorf("activeTab = %d, want to stay on the original tab", r.activeTab)
	}
}

func TestSplitOrientationDrivesTheFlexDirection(t *testing.T) {
	r, _, _ := newSplitRoot(t)
	r.enterSplit(1)

	if got := r.splitDirection(); got != tview.FlexColumn {
		t.Errorf("direction = %v, want side by side (FlexColumn) by default", got)
	}
	r.toggleSplitStacked()
	if got := r.splitDirection(); got != tview.FlexRow {
		t.Errorf("direction = %v, want stacked (FlexRow) after the flip", got)
	}
	if !r.settings.SplitStacked {
		t.Error("the flip should have updated the stored setting too")
	}
}

func TestSplitOrientationSurvivesIntoTheConfigFile(t *testing.T) {
	first := t.TempDir()
	configPath := isolateUserConfigFile(t)
	r, err := NewRoot(tview.NewApplication(), first)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}

	r.toggleSplitStacked()

	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("reading the config back: %v", err)
	}
	if got := string(data); !strings.Contains(got, "split_stacked = true") {
		t.Errorf("config = %q, want it to record split_stacked", got)
	}
}

func TestSwitchingToTheOtherPaneOnlyMovesFocus(t *testing.T) {
	r, _, _ := newSplitRoot(t)
	r.enterSplit(1)
	before := r.splitPanes

	r.switchToTab(1)

	if r.splitPanes != before {
		t.Errorf("splitPanes = %v, want the panes to stay put at %v", r.splitPanes, before)
	}
	if r.activeTab != 1 {
		t.Errorf("activeTab = %d, want focus on the other pane", r.activeTab)
	}
	if r.panel != r.tabs[1] {
		t.Error("r.panel should follow the focused pane")
	}
}

func TestSwitchingToAThirdTabReplacesOnlyTheFocusedPane(t *testing.T) {
	r, _, _ := newSplitRoot(t)
	third := t.TempDir()
	r.newTab(third) // this switches to it
	r.switchToTab(0)
	r.enterSplit(1) // panes: [0, 1], focused 0

	r.switchToTab(2)

	if r.splitPanes != [2]int{2, 1} {
		t.Errorf("splitPanes = %v, want the focused slot replaced and the other left alone", r.splitPanes)
	}
	if r.activeTab != 2 {
		t.Errorf("activeTab = %d, want 2", r.activeTab)
	}
}

func TestClosingAPaneTabEndsSplitView(t *testing.T) {
	r, _, _ := newSplitRoot(t)
	r.enterSplit(1)

	r.closeTab(1)

	if r.splitActive {
		t.Error("closing one of the two panes' own tabs should end split view")
	}
	if got := mountedPanels(r); len(got) != 1 {
		t.Errorf("mounted %d panels, want 1", len(got))
	}
}

func TestClosingAnUnrelatedTabKeepsSplitViewAndFixesTheIndices(t *testing.T) {
	r, _, _ := newSplitRoot(t)
	third := t.TempDir()
	fourth := t.TempDir()
	r.newTab(third)  // tab 2
	r.newTab(fourth) // tab 3
	r.switchToTab(2)
	r.enterSplit(3) // panes: [2, 3]

	r.closeTab(0) // an unrelated tab below both panes

	if !r.splitActive {
		t.Fatal("closing an unrelated tab should leave split view alone")
	}
	if r.splitPanes != [2]int{1, 2} {
		t.Errorf("splitPanes = %v, want both indices shifted down to {1, 2}", r.splitPanes)
	}
	// The panels themselves must still be the same two objects, not
	// whatever now happens to sit at those indices.
	mounted := mountedPanels(r)
	if len(mounted) != 2 || mounted[0] != r.tabs[1] || mounted[1] != r.tabs[2] {
		t.Error("the mounted panes are not the ones the shifted indices name")
	}
}

func TestTabKeyMovesBetweenThePanes(t *testing.T) {
	r, _, _ := newSplitRoot(t)
	r.enterSplit(1)
	r.app.SetFocus(r.tabs[0].table)

	if !r.CycleFocusShortcut() {
		t.Fatal("Tab should be consumed while a pane has focus")
	}
	if r.activeTab != 1 {
		t.Errorf("activeTab = %d, want Tab to cross into the other pane", r.activeTab)
	}
	if !r.tabs[1].table.HasFocus() {
		t.Error("the other pane's table should have keyboard focus")
	}
}

func TestClickingTheOtherPaneMakesItActive(t *testing.T) {
	r, _, _ := newSplitRoot(t)
	r.enterSplit(1)

	r.captureMouseOnPanel(r.tabs[1], tview.MouseLeftClick, tcell.NewEventMouse(0, 0, tcell.Button1, tcell.ModNone))

	if r.activeTab != 1 {
		t.Errorf("activeTab = %d, want the clicked pane to become active", r.activeTab)
	}
}

// TestMerelyPointingAtTheOtherPaneDoesNotActivateIt is a regression test
// for a real report: the active pane used to follow *any* mouse event,
// so a pointer left resting over one pane would yank the keyboard back
// to it seconds after the user had deliberately selected something in
// the other — with nothing on screen to explain why.
//
// This test previously asserted the opposite, having been written with
// MouseMove on the assumption that any event meant a click. It pinned
// the bug in place instead of catching it.
func TestMerelyPointingAtTheOtherPaneDoesNotActivateIt(t *testing.T) {
	r, _, _ := newSplitRoot(t)
	r.enterSplit(1)
	before := r.activeTab

	for _, action := range []tview.MouseAction{
		tview.MouseMove,
		tview.MouseScrollUp,
		tview.MouseScrollDown,
	} {
		r.captureMouseOnPanel(r.tabs[1], action, tcell.NewEventMouse(0, 0, tcell.ButtonNone, tcell.ModNone))
		if r.activeTab != before {
			t.Errorf("action %v moved the active pane to %d — only a deliberate press should", action, r.activeTab)
			r.switchToTab(before)
		}
	}
}

func TestEachPaneHighlightsItsOwnTabNumber(t *testing.T) {
	r, _, _ := newSplitRoot(t)
	r.enterSplit(1)

	if got := r.tabs[0].tabActive; got != 0 {
		t.Errorf("first pane highlights tab %d, want its own (0)", got)
	}
	if got := r.tabs[1].tabActive; got != 1 {
		t.Errorf("second pane highlights tab %d, want its own (1)", got)
	}
}

func TestSplitMenuLabelsDescribeTheNextAction(t *testing.T) {
	if got := splitToggleLabel(false); got != "Split view" {
		t.Errorf("label = %q, want the action that turns it on", got)
	}
	if got := splitToggleLabel(true); got != "Close split view" {
		t.Errorf("label = %q, want the action that turns it off", got)
	}
	if got := splitOrientationLabel(false); got != "Split above/below" {
		t.Errorf("label = %q, want the arrangement it would switch to", got)
	}
	if got := splitOrientationLabel(true); got != "Split side by side" {
		t.Errorf("label = %q, want the arrangement it would switch to", got)
	}
}

func TestSplitMenuLabelsFollowTheState(t *testing.T) {
	r, _, _ := newSplitRoot(t)

	r.toggleSplit()
	if main, _ := r.menu.GetItemText(r.splitToggleIdx); main != "Close split view" {
		t.Errorf("menu label = %q, want it to offer closing the split", main)
	}
	r.toggleSplit()
	if main, _ := r.menu.GetItemText(r.splitToggleIdx); main != "Split view" {
		t.Errorf("menu label = %q, want it to offer opening the split", main)
	}
}

func TestSplitWithTabOnTheCurrentTabOpensAFreshPaneInstead(t *testing.T) {
	r, _, _ := newSplitRoot(t)

	// Asking to split tab 0 with itself is not a layout — it should still
	// give the second pane the user plainly wanted.
	r.splitWithTab(0)

	if !r.splitActive {
		t.Fatal("splitActive should be true")
	}
	if partner, ok := r.splitPartner(); !ok || partner == r.activeTab {
		t.Errorf("partner = %d (ok=%v), want a different tab", partner, ok)
	}
}

func TestSplitWithNewTabAddsAPaneAndKeepsYouPut(t *testing.T) {
	r, first, _ := newSplitRoot(t)

	r.splitWithNewTab()

	if r.tabCount() != 3 {
		t.Fatalf("tab count = %d, want a third tab created", r.tabCount())
	}
	if r.activeTab != 0 {
		t.Errorf("activeTab = %d, want to stay on the tab we split from", r.activeTab)
	}
	if r.tabs[2].path != first {
		t.Errorf("new pane shows %q, want the current directory %q", r.tabs[2].path, first)
	}
	if partner, _ := r.splitPartner(); partner != 2 {
		t.Errorf("partner = %d, want the newly created tab", partner)
	}
}

// TestSplitSurvivesSaveAndRestore is the round trip: quit with two panes
// showing, start again, and get the same two panes back — in the same
// screen order, which is the part a plain "was it split" flag couldn't
// give back on its own.
func TestSplitSurvivesSaveAndRestore(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	isolateUserConfigFile(t)
	first := fixtureDir(t)
	second := t.TempDir()

	before, err := NewRoot(tview.NewApplication(), first)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	before.newTab(second)
	before.switchToTab(1)
	before.enterSplit(0) // panes: [1, 0] — the focused one first
	before.saveTabs()

	after, err := NewRoot(tview.NewApplication(), first)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	after.RestoreSavedTabs()

	if !after.splitActive {
		t.Fatal("split view was not restored")
	}
	if after.splitPanes != [2]int{1, 0} {
		t.Errorf("splitPanes = %v, want the saved screen order {1, 0}", after.splitPanes)
	}
	if got := mountedPanels(after); len(got) != 2 {
		t.Errorf("mounted %d panels, want both panes back", len(got))
	}
	// The invariant the rest of split.go leans on: the focused tab is
	// always one of the two panes.
	if _, ok := after.splitSlotOf(after.activeTab); !ok {
		t.Errorf("activeTab %d is not one of the panes %v", after.activeTab, after.splitPanes)
	}
}

// TestSplitIsDroppedWhenAPaneDirectoryVanished pins the degrade-don't-
// fail rule for the split specifically: if one pane's own directory is
// gone by the next start, opening single-pane is the right answer, not
// half a layout.
func TestSplitIsDroppedWhenAPaneDirectoryVanished(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	isolateUserConfigFile(t)
	dir := fixtureDir(t)
	gone := filepath.Join(t.TempDir(), "no-such-directory")

	if err := session.SaveTabs(session.TabsPath(), session.TabState{
		Paths:      []string{dir, gone},
		Active:     0,
		Split:      true,
		SplitPanes: []int{0, 1},
	}); err != nil {
		t.Fatalf("SaveTabs: %v", err)
	}

	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.RestoreSavedTabs()

	if r.splitActive {
		t.Error("split should have been dropped — one of its panes never came back")
	}
	if got := mountedPanels(r); len(got) != 1 {
		t.Errorf("mounted %d panels, want 1", len(got))
	}
}
