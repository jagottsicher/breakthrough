package ui

import (
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// TestNextToolWindowPositionCascades pins that successive spawn points
// step down-right rather than always landing in the same spot (see
// nextToolWindowPosition's own doc comment), wrapping back around after
// a few instead of drifting off-screen forever.
func TestNextToolWindowPositionCascades(t *testing.T) {
	r, err := NewRoot(tview.NewApplication(), fixtureDir(t))
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}

	x0, y0 := r.nextToolWindowPosition()
	r.toolWindows = append(r.toolWindows, &toolWindow{})
	x1, y1 := r.nextToolWindowPosition()
	if x1 <= x0 || y1 <= y0 {
		t.Errorf("second position (%d,%d) should be further down-right than the first (%d,%d)", x1, y1, x0, y0)
	}

	for i := 0; i < 5; i++ { // the first plus these 5 reaches the wrap point (6) exactly
		r.toolWindows = append(r.toolWindows, &toolWindow{})
	}
	xWrap, yWrap := r.nextToolWindowPosition()
	if xWrap != x0 || yWrap != y0 {
		t.Errorf("position after wrapping = (%d,%d), want it back at the first spot (%d,%d)", xWrap, yWrap, x0, y0)
	}
}

// TestToolWindowAppendLineEscapesContent pins that real command output
// is escaped before being written (tview.Escape — see appendLine's own
// doc comment): a literal "[" in a command's own output must not be
// misread as one of tview's style tags.
func TestToolWindowAppendLineEscapesContent(t *testing.T) {
	r, err := NewRoot(tview.NewApplication(), fixtureDir(t))
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	tw := newToolWindow(r, "tw", "test")

	tw.appendLine("[ERROR] something broke")

	if got := tw.content.GetText(true); !strings.Contains(got, "[ERROR] something broke") {
		t.Errorf("content = %q, want the literal line preserved (escaped, not swallowed as a style tag)", got)
	}
}

// TestToolWindowAppendAfterCloseIsNoop pins the closed guard both
// appendLine and appendStatus start with: a stray, already-in-flight
// QueueUpdateDraw callback from a process that's still shutting down
// must not write into a window whose page has already been removed.
func TestToolWindowAppendAfterCloseIsNoop(t *testing.T) {
	r, err := NewRoot(tview.NewApplication(), fixtureDir(t))
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	tw := newToolWindow(r, "tw", "test")
	tw.cancel = func() {}
	r.toolWindows = append(r.toolWindows, tw)
	r.AddPage("tw", tw, false, true)

	tw.close()
	tw.appendLine("too late")
	tw.appendStatus("also too late")

	if got := tw.content.GetText(true); strings.Contains(got, "too late") {
		t.Errorf("content = %q, appendLine/appendStatus should no-op once closed", got)
	}
	if containsToolWindow(r, tw) {
		t.Error("close should remove the window from Root.toolWindows")
	}
}

// containsToolWindow reports whether tw is currently one of r's own open
// tool windows — a small test helper standing in for the map membership
// check a plain map[string]*toolWindow would have given for free, back
// when toolWindows was one; it's an ordered []*toolWindow instead (see
// its own doc comment in root.go on why the order itself matters, for
// CycleFocusShortcut).
func containsToolWindow(r *Root, tw *toolWindow) bool {
	for _, w := range r.toolWindows {
		if w == tw {
			return true
		}
	}
	return false
}

// TestToolWindowTitleBarDragMovesWindow drives toolWindow.MouseHandler
// directly with the same three-event sequence a real drag produces
// (MouseLeftDown, then MouseMove with the button still held, then
// MouseLeftUp — see table_click_test.go's own TestRightDragLiveFocusFollowsMouseMove
// for the precedent this mirrors): pressing the title bar (the border's
// own top row) and moving repositions the window by the same offset the
// drag started with.
func TestToolWindowTitleBarDragMovesWindow(t *testing.T) {
	r, err := NewRoot(tview.NewApplication(), fixtureDir(t))
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.SetRect(0, 0, 100, 40) // realistic screen size — clampToScreen (see moveTo) needs real bounds to not itself distort this test's own math
	tw := newToolWindow(r, "tw", "test")
	tw.SetRect(10, 5, 40, 10)

	handler := tw.MouseHandler()
	// Press on the title bar: row 5 is the window's own top (border) row.
	handler(tview.MouseLeftDown, tcell.NewEventMouse(15, 5, tcell.Button1, 0), func(tview.Primitive) {})
	if !tw.dragging {
		t.Fatal("pressing the title bar should start a drag")
	}

	// Move 4 columns right, 3 rows down, button still held.
	handler(tview.MouseMove, tcell.NewEventMouse(19, 8, tcell.Button1, 0), func(tview.Primitive) {})
	x, y, _, _ := tw.GetRect()
	if x != 14 || y != 8 {
		t.Errorf("rect after drag move = (%d,%d), want (14,8)", x, y)
	}

	handler(tview.MouseLeftUp, tcell.NewEventMouse(19, 8, tcell.ButtonNone, 0), func(tview.Primitive) {})
	if tw.dragging {
		t.Error("MouseLeftUp should end the drag")
	}

	// Further movement after release must not keep moving the window.
	handler(tview.MouseMove, tcell.NewEventMouse(30, 20, tcell.ButtonNone, 0), func(tview.Primitive) {})
	if x2, y2, _, _ := tw.GetRect(); x2 != x || y2 != y {
		t.Errorf("rect moved after release to (%d,%d), want it to stay at (%d,%d)", x2, y2, x, y)
	}
}

// TestToolWindowContentClickDoesNotStartDrag pins that only the title
// bar itself starts a drag — a press inside the content area (one row
// below the window's own top) must not move the window when the mouse
// moves afterward, or a user trying to select/scroll inside a tool
// window would find it sliding around underneath them instead.
func TestToolWindowContentClickDoesNotStartDrag(t *testing.T) {
	r, err := NewRoot(tview.NewApplication(), fixtureDir(t))
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.SetRect(0, 0, 100, 40)
	tw := newToolWindow(r, "tw", "test")
	tw.SetRect(10, 5, 40, 10)

	handler := tw.MouseHandler()
	handler(tview.MouseLeftDown, tcell.NewEventMouse(15, 8, tcell.Button1, 0), func(tview.Primitive) {})
	if tw.dragging {
		t.Fatal("pressing inside the content area should not start a drag")
	}

	handler(tview.MouseMove, tcell.NewEventMouse(25, 15, tcell.Button1, 0), func(tview.Primitive) {})
	if x, y, _, _ := tw.GetRect(); x != 10 || y != 5 {
		t.Errorf("rect = (%d,%d), want unchanged (10,5) — a content-area press should never move the window", x, y)
	}
}

// TestToolWindowAltArrowMovesWindow pins the keyboard side of moving a
// tool window, per the user's own explicit request: Alt+arrow keys
// reposition it; a plain arrow (no Alt) must not, since that's needed
// for scrolling the content instead (see InputHandler's own doc
// comment).
func TestToolWindowAltArrowMovesWindow(t *testing.T) {
	r, err := NewRoot(tview.NewApplication(), fixtureDir(t))
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.SetRect(0, 0, 100, 40)
	tw := newToolWindow(r, "tw", "test")
	tw.cancel = func() {}
	tw.SetRect(10, 5, 40, 10)

	handler := tw.InputHandler()
	handler(tcell.NewEventKey(tcell.KeyRight, 0, tcell.ModAlt), func(tview.Primitive) {})
	handler(tcell.NewEventKey(tcell.KeyDown, 0, tcell.ModAlt), func(tview.Primitive) {})
	if x, y, _, _ := tw.GetRect(); x != 11 || y != 6 {
		t.Errorf("rect after Alt+Right, Alt+Down = (%d,%d), want (11,6)", x, y)
	}

	handler(tcell.NewEventKey(tcell.KeyLeft, 0, tcell.ModNone), func(tview.Primitive) {})
	if x, y, _, _ := tw.GetRect(); x != 11 || y != 6 {
		t.Errorf("rect after a plain (non-Alt) arrow = (%d,%d), want unchanged (11,6)", x, y)
	}
}

// TestToolWindowEscapeClosesWindow pins Escape as this window's own
// close gesture (the same key every other overlay in this app already
// uses to dismiss itself), which must both stop the underlying process
// (cancel) and remove the window from Root's own bookkeeping.
func TestToolWindowEscapeClosesWindow(t *testing.T) {
	r, err := NewRoot(tview.NewApplication(), fixtureDir(t))
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	tw := newToolWindow(r, "tw", "test")
	cancelled := false
	tw.cancel = func() { cancelled = true }
	r.toolWindows = append(r.toolWindows, tw)
	r.AddPage("tw", tw, false, true)

	handler := tw.InputHandler()
	handler(tcell.NewEventKey(tcell.KeyEscape, 0, tcell.ModNone), func(tview.Primitive) {})

	if !cancelled {
		t.Error("Escape should cancel the underlying process")
	}
	if !tw.closed {
		t.Error("Escape should mark the window closed")
	}
	if containsToolWindow(r, tw) {
		t.Error("Escape should remove the window from Root.toolWindows")
	}
	if r.HasPage("tw") {
		t.Error("Escape should remove the window's own page")
	}
}

// TestOpenToolCommandRegistersRunningWindow pins openToolCommand's own
// synchronous setup: a real, fast process (echo — not ping, so this
// stays fast, deterministic and network-independent) is started, its
// window is registered, added as its own page, sent to the front and
// given real keyboard focus, all before this function even returns —
// everything past that point (the streamed output, the final status
// line) arrives asynchronously via r.app.QueueUpdateDraw, which nothing
// here drains, the same convention every other async flow in this
// package's own tests already follows (see e.g.
// TestComputeHashesUpdatesPropertiesText's own doc comment) rather than
// relying on a background goroutine actually completing during a test.
func TestOpenToolCommandRegistersRunningWindow(t *testing.T) {
	r, err := NewRoot(tview.NewApplication(), fixtureDir(t))
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}

	tw := r.openToolCommand("echo test", "echo", []string{"hello"})
	if tw == nil {
		t.Fatal("openToolCommand returned nil for a real, valid command")
	}

	if len(r.toolWindows) != 1 {
		t.Errorf("len(toolWindows) = %d, want 1", len(r.toolWindows))
	}
	if !r.HasPage(tw.id) {
		t.Error("openToolCommand should add its window as its own page")
	}
	if r.app.GetFocus() != tw {
		t.Error("openToolCommand should give the new window real keyboard focus")
	}
	tw.close() // stop the process rather than leaving it running past the test
}

// TestOpenToolCommandReportsStartFailure pins the synchronous failure
// path: a command that doesn't even exist reports through the existing
// error overlay (the same one every other action already uses) instead
// of silently registering a window for a process that never started.
func TestOpenToolCommandReportsStartFailure(t *testing.T) {
	r, err := NewRoot(tview.NewApplication(), fixtureDir(t))
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}

	tw := r.openToolCommand("nope", "definitely-not-a-real-binary-xyz", nil)
	if tw != nil {
		t.Error("openToolCommand should return nil when the command fails to start")
	}
	if len(r.toolWindows) != 0 {
		t.Errorf("len(toolWindows) = %d, want 0 after a failed start", len(r.toolWindows))
	}
	if r.activePage != errorPage {
		t.Errorf("activePage = %q, want the error overlay after a failed start", r.activePage)
	}
}

// TestOpenPingTestWindowPromptsForHost pins that Ping, this first
// slice's own placeholder entry point (see openPingTestWindow's own doc
// comment), asks for a host through the existing generic prompt overlay
// rather than running against a hardcoded target — real ping execution
// itself is exercised by hand (see this PR's own tmux verification),
// not here: a real ICMP ping depends on raw-socket permissions this
// sandbox (or a CI runner) may not have, which openToolCommand's own
// tests above already avoid entirely by using echo instead.
func TestOpenPingTestWindowPromptsForHost(t *testing.T) {
	r, err := NewRoot(tview.NewApplication(), fixtureDir(t))
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}

	r.openPingTestWindow()

	if r.activePage != promptPage {
		t.Fatalf("activePage = %q, want the prompt overlay", r.activePage)
	}
	if got := r.prompt.GetLabel(); got != "Ping host: " {
		t.Errorf("prompt label = %q, want %q", got, "Ping host: ")
	}
}

// TestCycleFocusShortcutReachesToolWindow pins the whole reason
// CycleFocusShortcut was generalized past a plain panel/Details toggle
// in the first place: without it, a tool window could only ever be
// focused by clicking it (or in the instant it's first opened) — there
// was no keyboard-only way in at all. Panel -> tool window -> back to
// panel, with no Details sidebar open at all here (see
// TestCycleFocusShortcutOrdersDetailsBeforeToolWindows for all three
// together).
func TestCycleFocusShortcutReachesToolWindow(t *testing.T) {
	r, err := NewRoot(tview.NewApplication(), fixtureDir(t))
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.SetRect(0, 0, 100, 40)
	r.app.SetFocus(r.panel.table)

	tw := newToolWindow(r, "tw", "test")
	tw.cancel = func() {}
	r.toolWindows = append(r.toolWindows, tw)
	r.AddPage("tw", tw, false, true)

	if !r.CycleFocusShortcut() {
		t.Fatal("Tab from the panel with a tool window open should report true")
	}
	if got := r.app.GetFocus(); got != tw {
		t.Errorf("focus after Tab = %v, want the tool window", got)
	}

	if !r.CycleFocusShortcut() {
		t.Fatal("Tab from the tool window should report true")
	}
	if got := r.app.GetFocus(); got != r.panel.table {
		t.Errorf("focus after second Tab = %v, want back on the panel", got)
	}
}

// TestCycleFocusShortcutOrdersDetailsBeforeToolWindows pins the fixed
// stop order all three together follow (see focusCycleStops' own doc
// comment): panel, then Details, then every open tool window in the
// order they were opened.
func TestCycleFocusShortcutOrdersDetailsBeforeToolWindows(t *testing.T) {
	r, err := NewRoot(tview.NewApplication(), fixtureDir(t))
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.SetRect(0, 0, 100, 40)
	r.app.SetFocus(r.panel.table)
	r.showDetailsSidebar()

	tw1 := newToolWindow(r, "tw1", "first")
	tw1.cancel = func() {}
	r.toolWindows = append(r.toolWindows, tw1)
	r.AddPage("tw1", tw1, false, true)

	tw2 := newToolWindow(r, "tw2", "second")
	tw2.cancel = func() {}
	r.toolWindows = append(r.toolWindows, tw2)
	r.AddPage("tw2", tw2, false, true)

	wantOrder := []tview.Primitive{r.panel.table, r.detailsSidebar, tw1, tw2}
	for i := 1; i < len(wantOrder)+1; i++ {
		if !r.CycleFocusShortcut() {
			t.Fatalf("Tab #%d should report true", i)
		}
		want := wantOrder[i%len(wantOrder)]
		if got := r.app.GetFocus(); got != want {
			t.Errorf("focus after Tab #%d = %v, want %v", i, got, want)
		}
	}
}

// TestCycleFocusShortcutSkipsClosedToolWindow pins that closing a tool
// window (Escape — see toolWindow.close) removes it from this cycle too,
// not just from the screen: cycling afterwards must not get stuck trying
// to focus a Primitive that's already torn down.
func TestCycleFocusShortcutSkipsClosedToolWindow(t *testing.T) {
	r, err := NewRoot(tview.NewApplication(), fixtureDir(t))
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.SetRect(0, 0, 100, 40)
	r.app.SetFocus(r.panel.table)

	tw := newToolWindow(r, "tw", "test")
	tw.cancel = func() {}
	r.toolWindows = append(r.toolWindows, tw)
	r.AddPage("tw", tw, false, true)
	tw.close()

	if !r.CycleFocusShortcut() {
		t.Fatal("setup: still expect focus to move somewhere")
	}
	if got := r.app.GetFocus(); got == tview.Primitive(tw) {
		t.Error("focus should never land on a closed tool window")
	}
}

// TestToolWindowBackgroundMatchesDetailsSidebar pins the user's own
// explicit request that every panel floating over the main one share
// one look: the content area is always AccentBackground, the same
// "normal panel background" Details' own content area has (see
// detailssidebar.go's newDetailsSidebarView), independent of the title
// bar's own separate two-state pair — EditableBackground while
// unfocused, FocusedBackground (a dark cyan/"petrol" tone) while
// focused — the same scheme Details' own title bar (newDetailsTitleBar)
// uses.
func TestToolWindowBackgroundMatchesDetailsSidebar(t *testing.T) {
	r, err := NewRoot(tview.NewApplication(), fixtureDir(t))
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	tw := newToolWindow(r, "tw", "test")

	if got, want := tw.content.GetBackgroundColor(), r.theme.AccentBackground; got != want {
		t.Errorf("content background = %v, want AccentBackground %v", got, want)
	}
	if got, want := tw.titleBar.GetBackgroundColor(), r.theme.EditableBackground; got != want {
		t.Errorf("title bar background before focus = %v, want EditableBackground %v", got, want)
	}

	tw.Focus(func(tview.Primitive) {})
	if got, want := tw.titleBar.GetBackgroundColor(), r.theme.FocusedBackground; got != want {
		t.Errorf("title bar background while focused = %v, want FocusedBackground %v", got, want)
	}
	if got, want := tw.content.GetBackgroundColor(), r.theme.AccentBackground; got != want {
		t.Errorf("content background while focused = %v, want still AccentBackground %v", got, want)
	}

	tw.Blur()
	if got, want := tw.titleBar.GetBackgroundColor(), r.theme.EditableBackground; got != want {
		t.Errorf("title bar background after losing focus again = %v, want EditableBackground %v", got, want)
	}
}
