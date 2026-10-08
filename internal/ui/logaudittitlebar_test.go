package ui

import (
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// TestRenderCloseTitleBarPlacesGlyphAtRightEdge mirrors
// TestRenderReloadTitleBarPlacesGlyphAtRightEdge's own shape for the
// close-button twin.
func TestRenderCloseTitleBarPlacesGlyphAtRightEdge(t *testing.T) {
	bar := tview.NewTextView()
	renderCloseTitleBar(bar, " Log Audit — select files ", 40)

	text := bar.GetText(false)
	got := tview.TaggedStringWidth(text[:strings.IndexRune(text, toolWindowCloseGlyph)])
	if want := closeTitleBarButtonCol(40); got != want {
		t.Errorf("glyph column = %d, want %d (text = %q)", got, want, text)
	}
}

// TestRenderCloseTitleBarNeverPanicsOnZeroWidth mirrors
// TestRenderReloadTitleBarNeverPanicsOnZeroWidth — the same "before
// the first real Draw" fallback.
func TestRenderCloseTitleBarNeverPanicsOnZeroWidth(t *testing.T) {
	bar := tview.NewTextView()
	renderCloseTitleBar(bar, " Log Audit ", 0)
}

// TestCaptureCloseTitleBarMouseClicksTheGlyph mirrors the Mounts
// reload-button mouse test's own shape for the close-button twin.
func TestCaptureCloseTitleBarMouseClicksTheGlyph(t *testing.T) {
	bar := tview.NewTextView()
	renderCloseTitleBar(bar, " Log Audit ", 40)
	bar.SetRect(0, 0, 40, 1)

	closed := false
	_, y, width, _ := bar.GetRect()
	col := closeTitleBarButtonCol(width)
	captured, _ := captureCloseTitleBarMouse(bar, func() { closed = true })(tview.MouseLeftClick, tcell.NewEventMouse(col, y, tcell.ButtonNone, 0))

	if captured != tview.MouseConsumed {
		t.Error("clicking the close glyph should consume the click")
	}
	if !closed {
		t.Error("clicking the close glyph should have run the close func")
	}
}

// TestCaptureCloseTitleBarMouseClickElsewhereDoesNothing mirrors
// TestMountsTitleBarClickElsewhereDoesNothing's own shape.
func TestCaptureCloseTitleBarMouseClickElsewhereDoesNothing(t *testing.T) {
	bar := tview.NewTextView()
	renderCloseTitleBar(bar, " Log Audit ", 40)
	bar.SetRect(0, 0, 40, 1)

	closed := false
	captured, _ := captureCloseTitleBarMouse(bar, func() { closed = true })(tview.MouseLeftClick, tcell.NewEventMouse(0, 0, tcell.ButtonNone, 0))

	if captured == tview.MouseConsumed {
		t.Error("a click away from the close glyph should not be consumed")
	}
	if closed {
		t.Error("a click away from the close glyph should not have closed anything")
	}
}

// TestLogAuditSelectionTitleBarHasCloseButton pins that opening the
// Log Audit selection screen actually wires its own title bar's
// SetMouseCapture up to closeLogAuditSelection (see
// newLogAuditSelectionScreen) — not just that renderCloseTitleBar/
// captureCloseTitleBarMouse work correctly in isolation. Goes through
// MouseHandler() (tview's own real dispatch path for a click), rather
// than calling the capture func directly, so this also catches a
// SetMouseCapture that was never wired up at all.
func TestLogAuditSelectionTitleBarHasCloseButton(t *testing.T) {
	dir := t.TempDir()
	writeLogAuditFixture(t, dir)
	r := newTestRootForLogAudit(t, dir)

	r.logAuditTitleBar.SetRect(0, 0, 40, 1)
	_, y, width, _ := r.logAuditTitleBar.GetRect()
	col := closeTitleBarButtonCol(width)

	consumed, _ := r.logAuditTitleBar.MouseHandler()(tview.MouseLeftClick, tcell.NewEventMouse(col, y, tcell.ButtonNone, 0), func(tview.Primitive) {})
	if !consumed {
		t.Fatal("clicking the selection screen's own close glyph should be consumed")
	}
	if r.activePage != "" {
		t.Errorf("activePage = %q, want \"\" (closed back to the panel)", r.activePage)
	}
}
