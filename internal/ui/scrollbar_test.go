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

// openLookWithLines writes a file with n short, fixed-width lines (well
// under width, so wrapping never kicks in and GetWrappedLineCount ==
// n), opens it through the real showBuiltinLook path, sizes the Look
// overlay to width×height, and draws it once on a SimulationScreen —
// the one real draw GetWrappedLineCount's own doc comment says is
// required before it reflects actual wrapping (see
// scrollbar.go/wrappedLineCount's own cache doc comment). Returns the
// scrollbar under test and the viewport row count it ends up with.
func openLookWithLines(t *testing.T, n, width, height int) (*verticalScrollbar, int) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "lines.txt")
	var sb strings.Builder
	for i := 1; i <= n; i++ {
		fmt.Fprintf(&sb, "line %03d\n", i)
	}
	if err := os.WriteFile(path, []byte(sb.String()), 0o644); err != nil {
		t.Fatal(err)
	}

	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.SetRect(0, 0, width, height)

	if !r.showBuiltinLook(path) {
		t.Fatal("showBuiltinLook returned false")
	}

	screen := tcell.NewSimulationScreen("")
	if err := screen.Init(); err != nil {
		t.Fatalf("screen.Init: %v", err)
	}
	defer screen.Fini()
	screen.SetSize(width, height)
	r.viewerLayout.Draw(screen)

	return r.viewerScrollbar, r.viewerScrollbar.viewportRows()
}

// TestVerticalScrollbarThumbFillsTrackWhenContentFits pins the no-scroll
// case: content shorter than the viewport leaves nothing to scroll, so
// the thumb spans the whole track and sits at its very top.
func TestVerticalScrollbarThumbFillsTrackWhenContentFits(t *testing.T) {
	sb, _ := openLookWithLines(t, 5, 60, 20)

	_, _, _, sbh := sb.GetRect()
	trackRows := sbh - 2
	thumbRows, thumbStart := sb.thumbGeometry(trackRows)

	if thumbRows != trackRows {
		t.Errorf("thumbRows = %d, want the full track (%d) since content fits", thumbRows, trackRows)
	}
	if thumbStart != 0 {
		t.Errorf("thumbStart = %d, want 0", thumbStart)
	}
	if got := sb.maxOffset(); got != 0 {
		t.Errorf("maxOffset = %d, want 0 (nothing to scroll)", got)
	}
}

// TestVerticalScrollbarThumbShrinksAndMovesWithContent pins the
// proportional case: a document much longer than the viewport gets a
// thumb well short of the full track, and scrolling to the very end
// moves the thumb to the track's own bottom edge.
func TestVerticalScrollbarThumbShrinksAndMovesWithContent(t *testing.T) {
	sb, viewport := openLookWithLines(t, 200, 60, 20)

	_, _, _, sbh := sb.GetRect()
	trackRows := sbh - 2
	thumbRows, thumbStart := sb.thumbGeometry(trackRows)

	if thumbRows >= trackRows {
		t.Fatalf("thumbRows = %d, want it shorter than the full track (%d) for content this much longer than the viewport", thumbRows, trackRows)
	}
	if thumbStart != 0 {
		t.Errorf("thumbStart = %d, want 0 before any scrolling", thumbStart)
	}

	sb.scrollBy(sb.maxOffset()) // jump to the very end
	_ = viewport
	_, newStart := sb.thumbGeometry(trackRows)
	if newStart != trackRows-thumbRows {
		t.Errorf("thumbStart after scrolling to the end = %d, want the track's own bottom edge (%d)", newStart, trackRows-thumbRows)
	}
}

// TestVerticalScrollbarThumbFloorsAtOneRow pins the user's own explicit
// requirement: however long the document, the thumb never shrinks below
// one character cell.
func TestVerticalScrollbarThumbFloorsAtOneRow(t *testing.T) {
	sb, _ := openLookWithLines(t, 5000, 60, 10)

	_, _, _, sbh := sb.GetRect()
	trackRows := sbh - 2
	thumbRows, _ := sb.thumbGeometry(trackRows)

	if thumbRows != 1 {
		t.Errorf("thumbRows = %d, want 1 for a document this much longer than the track", thumbRows)
	}
}

// TestCaptureViewerScrollbarMouseArrowStepsOneLine pins the up/down
// arrow buttons: one click moves the content by exactly one line.
func TestCaptureViewerScrollbarMouseArrowStepsOneLine(t *testing.T) {
	sb, _ := openLookWithLines(t, 200, 60, 20)
	root := sb.root
	sbx, sby, _, sbh := sb.GetRect()

	root.captureViewerScrollbarMouse(tview.MouseLeftDown, tcell.NewEventMouse(sbx, sby+sbh-1, tcell.Button1, 0))
	sb.stopRepeat() // the click itself already started auto-repeat (see startRepeat) — stop it so the test only observes the single immediate step
	root.captureViewerScrollbarMouse(tview.MouseLeftUp, tcell.NewEventMouse(sbx, sby+sbh-1, tcell.ButtonNone, 0))

	if row, _ := sb.content.GetScrollOffset(); row != 1 {
		t.Errorf("scroll offset after one down-arrow click = %d, want 1", row)
	}
}

// TestCaptureViewerScrollbarMousePageJumpOnTrackClick pins clicking the
// track below the thumb: one click jumps a full viewport height, not
// a single line, and stops at maxOffset rather than overshooting.
func TestCaptureViewerScrollbarMousePageJumpOnTrackClick(t *testing.T) {
	sb, viewport := openLookWithLines(t, 200, 60, 20)
	root := sb.root
	sbx, sby, _, sbh := sb.GetRect()

	root.captureViewerScrollbarMouse(tview.MouseLeftDown, tcell.NewEventMouse(sbx, sby+sbh-2, tcell.Button1, 0))
	sb.stopRepeat()
	root.captureViewerScrollbarMouse(tview.MouseLeftUp, tcell.NewEventMouse(sbx, sby+sbh-2, tcell.ButtonNone, 0))

	row, _ := sb.content.GetScrollOffset()
	if row != viewport {
		t.Errorf("scroll offset after one page-jump click = %d, want one viewport height (%d)", row, viewport)
	}
}

// TestVerticalScrollbarDragMovesContentProportionally pins hold-and-drag
// on the thumb itself: dragging it down moves the content forward, and
// releasing the mouse ends the drag so a later unrelated mouse move
// doesn't keep scrolling.
func TestVerticalScrollbarDragMovesContentProportionally(t *testing.T) {
	sb, _ := openLookWithLines(t, 200, 60, 20)
	root := sb.root
	sbx, sby, _, sbh := sb.GetRect()

	root.captureViewerScrollbarMouse(tview.MouseLeftDown, tcell.NewEventMouse(sbx, sby+1, tcell.Button1, 0))
	if !sb.dragging {
		t.Fatal("clicking the thumb itself should start a drag")
	}

	root.captureViewerScrollbarMouse(tview.MouseMove, tcell.NewEventMouse(sbx, sby+sbh-2, tcell.Button1, 0))
	movedRow, _ := sb.content.GetScrollOffset()
	if movedRow <= 0 {
		t.Errorf("scroll offset after dragging the thumb most of the way down = %d, want it clearly advanced", movedRow)
	}

	root.captureViewerScrollbarMouse(tview.MouseLeftUp, tcell.NewEventMouse(sbx, sby+sbh-2, tcell.ButtonNone, 0))
	if sb.dragging {
		t.Error("MouseLeftUp should end the drag")
	}

	root.captureViewerScrollbarMouse(tview.MouseMove, tcell.NewEventMouse(sbx, sby+1, tcell.ButtonNone, 0))
	if afterRow, _ := sb.content.GetScrollOffset(); afterRow != movedRow {
		t.Errorf("scroll offset moved to %d after the drag already ended, want it to stay at %d", afterRow, movedRow)
	}
}

// TestCaptureViewerScrollbarMousePassesThroughOutsideScrollbar pins
// that a click elsewhere in the Look overlay (e.g. on its own content)
// is left alone — this hook must not swallow mouse events that have
// nothing to do with the scrollbar.
func TestCaptureViewerScrollbarMousePassesThroughOutsideScrollbar(t *testing.T) {
	sb, _ := openLookWithLines(t, 5, 60, 20)
	root := sb.root

	event := tcell.NewEventMouse(0, 0, tcell.Button1, 0)
	action, gotEvent := root.captureViewerScrollbarMouse(tview.MouseLeftDown, event)

	if action != tview.MouseLeftDown || gotEvent != event {
		t.Errorf("captureViewerScrollbarMouse(outside click) = (%v, %v), want the event passed through unchanged", action, gotEvent)
	}
}
