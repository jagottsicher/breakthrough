package ui

import (
	"fmt"
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

// TestToolWindowContentShowsAllLinesThatFit pins a real, hand-found bug
// (see writeContentLine's own doc comment): appendLine/appendStatus
// used to write every line via fmt.Fprintln, leaving the underlying
// text ending in a trailing newline after the *last* line too — which
// TextView's own trackEnd scrolling (ScrollToEnd, see newToolWindow)
// reads as one more, empty trailing line, permanently occupying this
// window's own last visible content row. Every window, at every
// height, was showing exactly one real line fewer than its content
// height should allow, no matter how much real output there actually
// was. Appends exactly as many lines as the window's own content
// height, then renders to a real tcell.SimulationScreen (GetText alone
// can't reveal a scrolling bug like this one, since the text itself is
// perfectly correct — only what actually ends up on screen isn't) and
// checks every one of those rows shows its own real line, with no
// spare row left over for a mystery blank one.
func TestToolWindowContentShowsAllLinesThatFit(t *testing.T) {
	r, err := NewRoot(tview.NewApplication(), fixtureDir(t))
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.SetRect(0, 0, 100, 40)
	tw := newToolWindow(r, "tw", "t")
	const n = 5
	tw.SetRect(5, 5, toolWindowMinWidth, n+2) // contentHeight == n exactly

	for i := 1; i <= n; i++ {
		tw.appendLine(fmt.Sprintf("LINE%d", i))
	}

	screen := tcell.NewSimulationScreen("")
	if err := screen.Init(); err != nil {
		t.Fatalf("screen.Init: %v", err)
	}
	screen.SetSize(100, 40)
	tw.Draw(screen)

	for i := 0; i < n; i++ {
		want := fmt.Sprintf("LINE%d", i+1)
		row := 6 + i // row 5 is the title bar; content starts at row 6
		var got strings.Builder
		for col := 5; col < 5+len(want); col++ {
			ch, _, _ := screen.Get(col, row)
			got.WriteString(ch)
		}
		if got.String() != want {
			t.Errorf("row %d = %q, want %q — a content row this window's own height has room for should never be blank", row, got.String(), want)
		}
	}
}

// TestToolWindowGrowsToFitLongLine pins the user's own explicit
// request that a tool window widen itself to fit its own longest
// visible line, rather than clipping it or requiring a manual resize —
// there's no manual resize here at all (see toolWindowDefaultHeight's
// own doc comment), only this automatic one.
func TestToolWindowGrowsToFitLongLine(t *testing.T) {
	r, err := NewRoot(tview.NewApplication(), fixtureDir(t))
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.SetRect(0, 0, 200, 40)
	tw := newToolWindow(r, "tw", "t")
	tw.SetRect(5, 5, toolWindowMinWidth, 6)

	long := strings.Repeat("x", 80)
	tw.appendLine(long)

	if _, _, width, _ := tw.GetRect(); width != len(long) {
		t.Errorf("width = %d, want %d (grown to fit the one line actually on screen)", width, len(long))
	}
}

// TestToolWindowShrinksWhenVisibleLinesShorten pins the other half of
// the same request: once a long line has scrolled out of view (see
// ScrollToEnd's own doc comment in newToolWindow — new lines push old
// ones off the top), it must stop holding the window open wide, even
// though it's still sitting in the TextView's own scrollback above
// what's currently shown.
func TestToolWindowShrinksWhenVisibleLinesShorten(t *testing.T) {
	r, err := NewRoot(tview.NewApplication(), fixtureDir(t))
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.SetRect(0, 0, 200, 40)
	tw := newToolWindow(r, "tw", "t")
	tw.SetRect(5, 5, toolWindowMinWidth, 4) // contentHeight = 3

	tw.appendLine(strings.Repeat("x", 80))
	if _, _, width, _ := tw.GetRect(); width != 80 {
		t.Fatalf("setup: width = %d, want 80 right after the long line", width)
	}

	tw.appendLine("short1")
	tw.appendLine("short2")
	tw.appendLine("short3") // exactly contentHeight lines now — the long one has scrolled fully out of view

	if _, _, width, _ := tw.GetRect(); width != toolWindowMinWidth {
		t.Errorf("width = %d, want it back down to the floor %d now that only short lines are visible", width, toolWindowMinWidth)
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

// TestToolWindowCloseButtonClosesWindow pins the user's own explicit
// request for a close button one column in from the title bar's own
// top-right corner (the toolWindowCloseGlyph drawn there — see
// Draw/toolWindowCloseButtonCol): clicking exactly that cell closes the
// window the same way Escape already does, rather than starting a
// move-drag the way the rest of the title bar would.
func TestToolWindowCloseButtonClosesWindow(t *testing.T) {
	r, err := NewRoot(tview.NewApplication(), fixtureDir(t))
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.SetRect(0, 0, 100, 40)
	tw := newToolWindow(r, "tw", "test")
	cancelled := false
	tw.cancel = func() { cancelled = true }
	r.toolWindows = append(r.toolWindows, tw)
	r.AddPage("tw", tw, false, true)
	tw.SetRect(10, 5, 40, 10)

	handler := tw.MouseHandler()
	handler(tview.MouseLeftDown, tcell.NewEventMouse(48, 5, tcell.Button1, 0), func(tview.Primitive) {}) // toolWindowCloseButtonCol(10, 40) = 48

	if !cancelled {
		t.Error("clicking the close button should cancel the underlying process")
	}
	if !tw.closed {
		t.Error("clicking the close button should mark the window closed")
	}
	if tw.dragging {
		t.Error("clicking the close button should not also start a move-drag")
	}
	if containsToolWindow(r, tw) {
		t.Error("clicking the close button should remove the window from Root.toolWindows")
	}
}

// TestToolWindowResizeHandleDragResizesWindow pins the user's own
// explicit request for a resize handle in the footer row's own
// bottom-right corner (the toolWindowResizeGlyph drawn there — see
// Draw): dragging it changes width and height together by the drag's
// own offset, while x and y — the opposite corner — stay exactly where
// they were, the same "grab one corner, the other stays put" behavior
// a real resize grip has.
func TestToolWindowResizeHandleDragResizesWindow(t *testing.T) {
	r, err := NewRoot(tview.NewApplication(), fixtureDir(t))
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.SetRect(0, 0, 100, 40)
	tw := newToolWindow(r, "tw", "test")
	tw.SetRect(10, 5, 40, 10)

	handler := tw.MouseHandler()
	// Press on the resize handle: (wx+width-1, wy+height-1) = (49, 14).
	handler(tview.MouseLeftDown, tcell.NewEventMouse(49, 14, tcell.Button1, 0), func(tview.Primitive) {})
	if !tw.resizing {
		t.Fatal("pressing the resize handle should start a resize")
	}

	// Move 6 columns right, 6 rows down, button still held.
	handler(tview.MouseMove, tcell.NewEventMouse(55, 20, tcell.Button1, 0), func(tview.Primitive) {})
	x, y, width, height := tw.GetRect()
	if x != 10 || y != 5 {
		t.Errorf("rect origin after resize = (%d,%d), want it to stay at (10,5) — only the dragged corner should move", x, y)
	}
	if width != 46 || height != 16 {
		t.Errorf("rect size after resize = %dx%d, want 46x16 (grown by the same 6x6 the drag moved)", width, height)
	}
	if !tw.manuallyResized {
		t.Error("dragging the resize handle should set manuallyResized")
	}

	handler(tview.MouseLeftUp, tcell.NewEventMouse(55, 20, tcell.ButtonNone, 0), func(tview.Primitive) {})
	if tw.resizing {
		t.Error("MouseLeftUp should end the resize")
	}
}

// TestToolWindowResizeHandleRespectsMinWidth pins the user's own
// explicit request for a floor on how narrow a manual resize can make
// this window: its own title, one space, and the close button (see
// minWidth's own doc comment) — dragging the handle further left than
// that must stop shrinking there instead of overlapping or clipping
// either one.
func TestToolWindowResizeHandleRespectsMinWidth(t *testing.T) {
	r, err := NewRoot(tview.NewApplication(), fixtureDir(t))
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.SetRect(0, 0, 100, 40)
	tw := newToolWindow(r, "tw", "test")
	tw.SetRect(10, 5, 40, 10)

	handler := tw.MouseHandler()
	handler(tview.MouseLeftDown, tcell.NewEventMouse(49, 14, tcell.Button1, 0), func(tview.Primitive) {})
	handler(tview.MouseMove, tcell.NewEventMouse(0, 14, tcell.Button1, 0), func(tview.Primitive) {}) // far past the left edge

	if _, _, width, _ := tw.GetRect(); width != tw.minWidth() {
		t.Errorf("width = %d, want it floored at minWidth() = %d", width, tw.minWidth())
	}
}

// TestToolWindowResizeHandleRespectsMinHeight is
// TestToolWindowResizeHandleRespectsMinWidth's own height counterpart:
// toolWindowMinHeight (title bar + one content row + the footer row
// itself).
func TestToolWindowResizeHandleRespectsMinHeight(t *testing.T) {
	r, err := NewRoot(tview.NewApplication(), fixtureDir(t))
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.SetRect(0, 0, 100, 40)
	tw := newToolWindow(r, "tw", "test")
	tw.SetRect(10, 5, 40, 10)

	handler := tw.MouseHandler()
	handler(tview.MouseLeftDown, tcell.NewEventMouse(49, 14, tcell.Button1, 0), func(tview.Primitive) {})
	handler(tview.MouseMove, tcell.NewEventMouse(49, 0, tcell.Button1, 0), func(tview.Primitive) {}) // far past the top edge

	if _, _, _, height := tw.GetRect(); height != toolWindowMinHeight {
		t.Errorf("height = %d, want it floored at toolWindowMinHeight = %d", height, toolWindowMinHeight)
	}
}

// TestToolWindowManualResizeDisablesAutoFit pins the user's own
// implicit requirement that a manual resize actually stick: without
// manuallyResized (see its own doc comment), the very next appendLine
// would immediately snap the width back to whatever recalculateWidth's
// own auto-fit logic alone would have chosen, undoing the drag.
func TestToolWindowManualResizeDisablesAutoFit(t *testing.T) {
	r, err := NewRoot(tview.NewApplication(), fixtureDir(t))
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.SetRect(0, 0, 200, 40)
	tw := newToolWindow(r, "tw", "test")
	tw.SetRect(10, 5, 40, 10)

	handler := tw.MouseHandler()
	handler(tview.MouseLeftDown, tcell.NewEventMouse(49, 14, tcell.Button1, 0), func(tview.Primitive) {})
	handler(tview.MouseMove, tcell.NewEventMouse(55, 20, tcell.Button1, 0), func(tview.Primitive) {})
	handler(tview.MouseLeftUp, tcell.NewEventMouse(55, 20, tcell.ButtonNone, 0), func(tview.Primitive) {})

	_, _, wantWidth, wantHeight := tw.GetRect()

	tw.appendLine(strings.Repeat("x", 200)) // would grow the window a lot if auto-fit were still active
	if _, _, width, height := tw.GetRect(); width != wantWidth || height != wantHeight {
		t.Errorf("rect after appendLine = %dx%d, want unchanged %dx%d — a manual resize should disable auto-fit", width, height, wantWidth, wantHeight)
	}
}

// TestToolWindowFooterRowClickDoesNotStartDrag pins that only the exact
// resize-handle cell starts a resize — a press anywhere else on the
// footer row (which otherwise always stays empty — see contentHeight's
// own doc comment) must do nothing at all, not move or resize the
// window.
func TestToolWindowFooterRowClickDoesNotStartDrag(t *testing.T) {
	r, err := NewRoot(tview.NewApplication(), fixtureDir(t))
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.SetRect(0, 0, 100, 40)
	tw := newToolWindow(r, "tw", "test")
	tw.SetRect(10, 5, 40, 10)

	handler := tw.MouseHandler()
	handler(tview.MouseLeftDown, tcell.NewEventMouse(20, 14, tcell.Button1, 0), func(tview.Primitive) {}) // footer row, not its own corner
	if tw.dragging || tw.resizing {
		t.Fatal("pressing the footer row away from its own corner should neither move nor resize the window")
	}

	handler(tview.MouseMove, tcell.NewEventMouse(30, 25, tcell.Button1, 0), func(tview.Primitive) {})
	if x, y, width, height := tw.GetRect(); x != 10 || y != 5 || width != 40 || height != 10 {
		t.Errorf("rect = %d,%d %dx%d, want unchanged 10,5 40x10", x, y, width, height)
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

// TestOpenToolCommandFirstWindowUsesFirstCascadePosition pins a real
// bug found by hand: openToolCommand used to append tw to
// r.toolWindows *before* calling nextToolWindowPosition, so even the
// very first window ever opened already saw itself counted (len == 1),
// landing one cascade step further down-right than
// nextToolWindowPosition's own doc comment promises for a first
// window. Position is computed (and the rect set) before the append
// now, so the very first window must land exactly where
// nextToolWindowPosition(), called with the slice still empty, would
// have put it.
func TestOpenToolCommandFirstWindowUsesFirstCascadePosition(t *testing.T) {
	r, err := NewRoot(tview.NewApplication(), fixtureDir(t))
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.SetRect(0, 0, 100, 40)                   // realistic screen size — clampToScreen (see moveTo) needs real bounds to not itself distort this test's own math
	wantX, wantY := r.nextToolWindowPosition() // computed against the still-empty slice, same as openToolCommand's own first call should see

	tw := r.openToolCommand("echo test", "echo", []string{"hello"})
	if tw == nil {
		t.Fatal("openToolCommand returned nil for a real, valid command")
	}
	defer tw.close()

	if x, y, _, _ := tw.GetRect(); x != wantX || y != wantY {
		t.Errorf("first window position = (%d,%d), want (%d,%d) — nextToolWindowPosition's own first cascade spot", x, y, wantX, wantY)
	}
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
