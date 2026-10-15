package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// openCompareDiffWithManyLines opens Compare's own side-by-side diff
// view on two files with enough identical-but-independent content to
// overflow a small screen on both sides — real overflow on both panes,
// not just one, since compareDiffPaneContent reads each pane's own
// geometry independently (unlike Git Breach's own Pair-guaranteed-
// equal-length diff split).
func openCompareDiffWithManyLines(t *testing.T, width, height int) *Root {
	t.Helper()
	r, dir := newCompareRoot(t)
	var left, right strings.Builder
	for i := 0; i < 60; i++ {
		fmt.Fprintf(&left, "left line %d\n", i)
		fmt.Fprintf(&right, "right line %d\n", i)
	}
	a, b := filepath.Join(dir, "a.txt"), filepath.Join(dir, "b.txt")
	if err := os.WriteFile(a, []byte(left.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(b, []byte(right.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	r.openCompareFile(a, b)
	r.openCompareDiff()
	if r.activePage != compareDiffPage {
		t.Fatalf("activePage = %q, want %q", r.activePage, compareDiffPage)
	}

	r.SetRect(0, 0, width, height)
	r.compareDiffLayout.SetRect(0, 0, width, height)
	return r
}

func drawCompareDiff(t *testing.T, r *Root, width, height int) {
	t.Helper()
	screen := tcell.NewSimulationScreen("")
	if err := screen.Init(); err != nil {
		t.Fatalf("screen.Init: %v", err)
	}
	t.Cleanup(screen.Fini)
	screen.SetSize(width, height)
	r.compareDiffLayout.Draw(screen)
}

// TestCompareDiffRightPaneClickFocusesIt is a real, reproduced
// regression test: a MouseLeftDown anywhere in the whole screen was
// swallowed before ever reaching compareDiffRight, by two different
// widgets in turn — compareDiffSeparator's own gitBreachBlockFocusSteal
// capture (fixed with an explicit InRect check, same shape as the
// catalog table headers' own fix), and then compareDiffTitleBar's own
// capture, whose fallback branch called gitBreachBlockFocusSteal with
// no position check at all. Both had to be fixed before a click on the
// right pane actually focused it.
func TestCompareDiffRightPaneClickFocusesIt(t *testing.T) {
	r := openCompareDiffWithManyLines(t, 100, 20)
	drawCompareDiff(t, r, 100, 20)

	r.app.SetFocus(r.compareDiffLeft)
	rx, ry, _, _ := r.compareDiffRight.GetRect()

	setFocus := func(p tview.Primitive) { r.app.SetFocus(p) }
	handler := r.compareDiffLayout.MouseHandler()
	handler(tview.MouseLeftDown, tcell.NewEventMouse(rx+1, ry, tcell.Button1, 0), setFocus)

	if !r.compareDiffRight.HasFocus() {
		t.Error("clicking inside compareDiffRight's own rect should focus it")
	}
}

// TestCompareDiffScrollbarsMoveBothPanesWhenLinked pins the default,
// linked state (openCompareDiff sets compareDiffLinked = true): a click
// on either pane's own down arrow must scroll both panes together, the
// same lockstep scrollCompareDiff itself already enforces for the
// keyboard and mouse wheel.
func TestCompareDiffScrollbarsMoveBothPanesWhenLinked(t *testing.T) {
	r := openCompareDiffWithManyLines(t, 100, 20)
	drawCompareDiff(t, r, 100, 20)

	if !r.compareDiffLinked {
		t.Fatal("setup: compareDiffLinked = false, want true (openCompareDiff's own default)")
	}
	sb := r.compareDiffLeftScrollbar
	if sb.maxOffset() <= 0 {
		t.Fatalf("maxOffset = %d, want real overflow for 60 lines on a 20-row screen", sb.maxOffset())
	}
	sbx, sby, _, sbh := sb.GetRect()

	setFocus := func(p tview.Primitive) { r.app.SetFocus(p) }
	handler := r.compareDiffLayout.MouseHandler()
	handler(tview.MouseLeftDown, tcell.NewEventMouse(sbx, sby+sbh-1, tcell.Button1, 0), setFocus)
	sb.stopRepeat()
	handler(tview.MouseLeftUp, tcell.NewEventMouse(sbx, sby+sbh-1, tcell.ButtonNone, 0), setFocus)

	leftRow, _ := r.compareDiffLeft.GetScrollOffset()
	rightRow, _ := r.compareDiffRight.GetScrollOffset()
	if leftRow != 1 {
		t.Errorf("left pane offset = %d, want 1", leftRow)
	}
	if rightRow != leftRow {
		t.Errorf("right pane offset = %d, want it to match the left pane (%d) while linked", rightRow, leftRow)
	}
}

// TestCompareDiffScrollbarsMoveOnlyOwnPaneWhenUnlinked pins the other
// half: once unlinked, clicking one pane's own scrollbar must leave the
// other pane's own scroll position untouched.
func TestCompareDiffScrollbarsMoveOnlyOwnPaneWhenUnlinked(t *testing.T) {
	r := openCompareDiffWithManyLines(t, 100, 20)
	r.toggleCompareDiffLinked()
	if r.compareDiffLinked {
		t.Fatal("setup: toggleCompareDiffLinked should have turned linking off")
	}
	drawCompareDiff(t, r, 100, 20)

	sb := r.compareDiffLeftScrollbar
	if sb.maxOffset() <= 0 {
		t.Fatalf("maxOffset = %d, want real overflow for 60 lines on a 20-row screen", sb.maxOffset())
	}
	sbx, sby, _, sbh := sb.GetRect()
	rightBefore, _ := r.compareDiffRight.GetScrollOffset()

	setFocus := func(p tview.Primitive) { r.app.SetFocus(p) }
	handler := r.compareDiffLayout.MouseHandler()
	handler(tview.MouseLeftDown, tcell.NewEventMouse(sbx, sby+sbh-1, tcell.Button1, 0), setFocus)
	sb.stopRepeat()
	handler(tview.MouseLeftUp, tcell.NewEventMouse(sbx, sby+sbh-1, tcell.ButtonNone, 0), setFocus)

	leftRow, _ := r.compareDiffLeft.GetScrollOffset()
	rightAfter, _ := r.compareDiffRight.GetScrollOffset()
	if leftRow != 1 {
		t.Errorf("left pane offset = %d, want 1", leftRow)
	}
	if rightAfter != rightBefore {
		t.Errorf("right pane offset changed from %d to %d while unlinked, want it untouched", rightBefore, rightAfter)
	}
}

// openCompareDiffWithWideLines mirrors openCompareDiffWithManyLines but
// for horizontal overflow: one long line per side, wider than any
// reasonable test screen, rather than many short ones.
func openCompareDiffWithWideLines(t *testing.T, width, height int) *Root {
	t.Helper()
	r, dir := newCompareRoot(t)
	left := "left " + strings.Repeat("x", 200) + "\n"
	right := "right " + strings.Repeat("y", 200) + "\n"
	a, b := filepath.Join(dir, "a.txt"), filepath.Join(dir, "b.txt")
	if err := os.WriteFile(a, []byte(left), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(b, []byte(right), 0o644); err != nil {
		t.Fatal(err)
	}
	r.openCompareFile(a, b)
	r.openCompareDiff()
	if r.activePage != compareDiffPage {
		t.Fatalf("activePage = %q, want %q", r.activePage, compareDiffPage)
	}

	r.SetRect(0, 0, width, height)
	r.compareDiffLayout.SetRect(0, 0, width, height)
	return r
}

// TestCompareDiffHScrollbarsMoveBothPanesWhenLinked mirrors
// TestCompareDiffScrollbarsMoveBothPanesWhenLinked for the horizontal
// bar: a click on either pane's own right arrow must scroll both panes
// sideways together while linked.
func TestCompareDiffHScrollbarsMoveBothPanesWhenLinked(t *testing.T) {
	r := openCompareDiffWithWideLines(t, 60, 20)
	drawCompareDiff(t, r, 60, 20)

	if !r.compareDiffLinked {
		t.Fatal("setup: compareDiffLinked = false, want true (openCompareDiff's own default)")
	}
	sb := r.compareDiffLeftHScrollbar
	if sb.maxOffset() <= 0 {
		t.Fatalf("maxOffset = %d, want real overflow for a 200-column line on a 60-column screen", sb.maxOffset())
	}
	sbx, sby, _, _ := sb.GetRect()

	setFocus := func(p tview.Primitive) { r.app.SetFocus(p) }
	handler := r.compareDiffLayout.MouseHandler()
	handler(tview.MouseLeftDown, tcell.NewEventMouse(sbx+1, sby, tcell.Button1, 0), setFocus)
	sb.stopRepeat()
	handler(tview.MouseLeftUp, tcell.NewEventMouse(sbx+1, sby, tcell.ButtonNone, 0), setFocus)

	_, leftCol := r.compareDiffLeft.GetScrollOffset()
	_, rightCol := r.compareDiffRight.GetScrollOffset()
	if leftCol != 1 {
		t.Errorf("left pane column offset = %d, want 1", leftCol)
	}
	if rightCol != leftCol {
		t.Errorf("right pane column offset = %d, want it to match the left pane (%d) while linked", rightCol, leftCol)
	}
}

// TestCompareDiffHScrollbarsMoveOnlyOwnPaneWhenUnlinked mirrors
// TestCompareDiffScrollbarsMoveOnlyOwnPaneWhenUnlinked for the
// horizontal bar.
func TestCompareDiffHScrollbarsMoveOnlyOwnPaneWhenUnlinked(t *testing.T) {
	r := openCompareDiffWithWideLines(t, 60, 20)
	r.toggleCompareDiffLinked()
	if r.compareDiffLinked {
		t.Fatal("setup: toggleCompareDiffLinked should have turned linking off")
	}
	drawCompareDiff(t, r, 60, 20)

	sb := r.compareDiffLeftHScrollbar
	if sb.maxOffset() <= 0 {
		t.Fatalf("maxOffset = %d, want real overflow for a 200-column line on a 60-column screen", sb.maxOffset())
	}
	sbx, sby, _, _ := sb.GetRect()
	_, rightBefore := r.compareDiffRight.GetScrollOffset()

	setFocus := func(p tview.Primitive) { r.app.SetFocus(p) }
	handler := r.compareDiffLayout.MouseHandler()
	handler(tview.MouseLeftDown, tcell.NewEventMouse(sbx+1, sby, tcell.Button1, 0), setFocus)
	sb.stopRepeat()
	handler(tview.MouseLeftUp, tcell.NewEventMouse(sbx+1, sby, tcell.ButtonNone, 0), setFocus)

	_, leftCol := r.compareDiffLeft.GetScrollOffset()
	_, rightAfter := r.compareDiffRight.GetScrollOffset()
	if leftCol != 1 {
		t.Errorf("left pane column offset = %d, want 1", leftCol)
	}
	if rightAfter != rightBefore {
		t.Errorf("right pane column offset changed from %d to %d while unlinked, want it untouched", rightBefore, rightAfter)
	}
}

// TestCompareDiffScrollbarsDoNotShareTheCornerCell pins the user's own
// explicit requirement that the vertical and horizontal bars never
// overlap — the vertical bar's own column must stay entirely to the
// right of the horizontal bar's own width.
func TestCompareDiffScrollbarsDoNotShareTheCornerCell(t *testing.T) {
	r := openCompareDiffWithManyLines(t, 100, 20)
	drawCompareDiff(t, r, 100, 20)

	vx, _, _, _ := r.compareDiffLeftScrollbar.GetRect()
	hx, _, hw, _ := r.compareDiffLeftHScrollbar.GetRect()
	if hx+hw > vx {
		t.Errorf("left pane's horizontal bar (x=%d width=%d) reaches into the vertical bar's own column (x=%d)", hx, hw, vx)
	}
}
