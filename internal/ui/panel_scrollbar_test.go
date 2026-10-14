package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// manyFilesDir is a temp dir with enough files that Panel's table
// overflows a short screen height, the precondition for its scrollbar
// having any visible arrows/thumb to click at all (see
// verticalScrollbar's own "hidden when content fits" doc comment).
func manyFilesDir(t *testing.T, count int) string {
	t.Helper()
	dir := t.TempDir()
	for i := 0; i < count; i++ {
		name := filepath.Join(dir, fmt.Sprintf("file%03d.txt", i))
		if err := os.WriteFile(name, nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// TestPanelScrollbarMouseClickThroughRealDispatch mirrors the same
// regression test shape used for every other scrollbar in this rollout
// (Mounts, Compare Tree, ...): a click on the scrollbar's own down arrow
// must actually scroll the Panel's table, driven through the real
// dispatch chain (Root's own MouseHandler, cascading through panelHost
// and Panel's Flex), not the scrollbar's captureMouse directly.
func TestPanelScrollbarMouseClickThroughRealDispatch(t *testing.T) {
	isolateUserConfigFile(t)
	dir := manyFilesDir(t, 50)
	root, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}

	screen := tcell.NewSimulationScreen("")
	if err := screen.Init(); err != nil {
		t.Fatalf("screen.Init: %v", err)
	}
	defer screen.Fini()
	screen.SetSize(80, 10)
	root.SetRect(0, 0, 80, 10)
	root.Draw(screen) // cascades layout down through panelHost into Panel's own Flex

	sb := root.panel.scrollbar
	if sb.maxOffset() <= 0 {
		t.Fatalf("maxOffset = %d, want real overflow for %d files on a 10-row screen", sb.maxOffset(), 50)
	}
	sbx, sby, _, sbh := sb.GetRect()
	before, _ := root.panel.table.GetOffset()

	handler := root.MouseHandler()
	handler(tview.MouseLeftDown, tcell.NewEventMouse(sbx, sby+sbh-1, tcell.Button1, 0), func(tview.Primitive) {})
	sb.stopRepeat()
	handler(tview.MouseLeftUp, tcell.NewEventMouse(sbx, sby+sbh-1, tcell.ButtonNone, 0), func(tview.Primitive) {})

	after, _ := root.panel.table.GetOffset()
	if after != before+1 {
		t.Errorf("panel table offset after clicking the down arrow = %d, want %d (before=%d)", after, before+1, before)
	}
}

// TestSplitViewPanelsHaveIndependentScrollbars pins the property the user
// explicitly called out as its own deliverable ("die main screen split
// view"): once two tabs are mounted side by side, each keeps its own
// Panel, and so its own scrollbar — clicking one pane's down arrow must
// not move the other pane's table at all.
func TestSplitViewPanelsHaveIndependentScrollbars(t *testing.T) {
	isolateUserConfigFile(t)
	firstDir := manyFilesDir(t, 50)
	secondDir := manyFilesDir(t, 50)

	root, err := NewRoot(tview.NewApplication(), firstDir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	root.newTab(secondDir)
	root.switchToTab(0)
	root.enterSplit(1)

	mounted := mountedPanels(root)
	if len(mounted) != 2 {
		t.Fatalf("mounted %d panels, want 2 for split view", len(mounted))
	}
	left, right := mounted[0], mounted[1]

	screen := tcell.NewSimulationScreen("")
	if err := screen.Init(); err != nil {
		t.Fatalf("screen.Init: %v", err)
	}
	defer screen.Fini()
	screen.SetSize(100, 10)
	root.SetRect(0, 0, 100, 10)
	root.Draw(screen)

	if left.scrollbar.maxOffset() <= 0 || right.scrollbar.maxOffset() <= 0 {
		t.Fatalf("both panes should overflow: left maxOffset=%d right maxOffset=%d", left.scrollbar.maxOffset(), right.scrollbar.maxOffset())
	}

	leftBefore, _ := left.table.GetOffset()
	rightBefore, _ := right.table.GetOffset()

	sbx, sby, _, sbh := left.scrollbar.GetRect()
	handler := root.MouseHandler()
	handler(tview.MouseLeftDown, tcell.NewEventMouse(sbx, sby+sbh-1, tcell.Button1, 0), func(tview.Primitive) {})
	left.scrollbar.stopRepeat()
	handler(tview.MouseLeftUp, tcell.NewEventMouse(sbx, sby+sbh-1, tcell.ButtonNone, 0), func(tview.Primitive) {})

	leftAfter, _ := left.table.GetOffset()
	rightAfter, _ := right.table.GetOffset()

	if leftAfter != leftBefore+1 {
		t.Errorf("left pane offset = %d, want %d (before=%d)", leftAfter, leftBefore+1, leftBefore)
	}
	if rightAfter != rightBefore {
		t.Errorf("right pane offset changed from %d to %d after clicking the left pane's scrollbar", rightBefore, rightAfter)
	}
}
