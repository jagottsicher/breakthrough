package ui

import (
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// TestScrollAcknowledgeNoticeOpensWithMessageViewFocused pins the
// default focus: the user should be able to start reading (and
// scrolling, via the TextView's own default key handling) right away,
// without an extra Tab first.
func TestScrollAcknowledgeNoticeOpensWithMessageViewFocused(t *testing.T) {
	r, _ := newOptionsRoot(t)
	r.SetRect(0, 0, 100, 40)

	r.openScrollAcknowledgeNotice("Title", strings.Repeat("line\n", 50), "Yes, enable", func() {})

	if !r.scrollAcknowledgeMessageView.HasFocus() {
		t.Error("the message view should have focus right after opening")
	}
}

// TestCanAcknowledgeScrollNoticeTrueWhenMessageFitsEntirely pins the
// edge case a strict "must scroll" rule would otherwise break: a
// warning short enough to fit the box outright has nothing left to
// scroll to, so it must count as already read rather than permanently
// locking the confirming choice out.
func TestCanAcknowledgeScrollNoticeTrueWhenMessageFitsEntirely(t *testing.T) {
	r, _ := newOptionsRoot(t)
	r.SetRect(0, 0, 100, 40)

	r.openScrollAcknowledgeNotice("Title", "a short message", "Yes, enable", func() {})

	if !r.canAcknowledgeScrollNotice() {
		t.Error("a message that already fits entirely should count as read")
	}
}

// TestCanAcknowledgeScrollNoticeFalseUntilScrolledToEnd is the actual
// gate the user asked for: a long message must be scrolled all the
// way to its own end before the confirming choice does anything.
func TestCanAcknowledgeScrollNoticeFalseUntilScrolledToEnd(t *testing.T) {
	r, _ := newOptionsRoot(t)
	r.SetRect(0, 0, 100, 40)

	r.openScrollAcknowledgeNotice("Title", strings.Repeat("line\n", 50), "Yes, enable", func() {})

	if r.canAcknowledgeScrollNotice() {
		t.Error("a long message should not count as read before scrolling")
	}

	r.scrollAcknowledgeMessageView.ScrollTo(r.scrollAcknowledgeScrollbar.maxOffset(), 0) // ScrollToEnd only resolves on the next real Draw, which tests never trigger

	if !r.canAcknowledgeScrollNotice() {
		t.Error("scrolling to the end should make it count as read")
	}
}

// TestScrollAcknowledgeNoticeClampsToScreen pins the same defensive
// guard openConfirm's own colored-stripes fix added: even on a screen
// smaller than this dialog's own fixed size, it must never end up
// with a negative x or a width past the screen's edge.
func TestScrollAcknowledgeNoticeClampsToScreen(t *testing.T) {
	r, _ := newOptionsRoot(t)
	r.SetRect(0, 0, 20, 10) // smaller than scrollAcknowledgeWidth/Height

	r.openScrollAcknowledgeNotice("Title", "message", "Yes, enable", func() {})

	x, y, width, height := r.scrollAcknowledgeLayout.GetRect()
	if x < 0 || y < 0 {
		t.Errorf("rect = %d,%d,%d,%d, want x/y never negative", x, y, width, height)
	}
	if width > 20 || height > 10 {
		t.Errorf("rect = %d,%d,%d,%d, want it clamped to the 20x10 screen", x, y, width, height)
	}
}

// TestTabFromScrollAcknowledgeMessageReachesTheListAndBack pins the
// dialog's own simple two-stop focus ring.
func TestTabFromScrollAcknowledgeMessageReachesTheListAndBack(t *testing.T) {
	r, _ := newOptionsRoot(t)
	r.SetRect(0, 0, 100, 40)
	r.openScrollAcknowledgeNotice("Title", "message", "Yes, enable", func() {})

	r.scrollAcknowledgeMessageView.InputHandler()(tcell.NewEventKey(tcell.KeyTab, 0, tcell.ModNone), func(p tview.Primitive) { r.app.SetFocus(p) })
	if !r.scrollAcknowledgeList.HasFocus() {
		t.Error("Tab from the message view should reach the list")
	}

	r.scrollAcknowledgeList.InputHandler()(tcell.NewEventKey(tcell.KeyBacktab, 0, tcell.ModNone), func(p tview.Primitive) { r.app.SetFocus(p) })
	if !r.scrollAcknowledgeMessageView.HasFocus() {
		t.Error("Backtab from the list should return to the message view")
	}
}

// TestScrollAcknowledgeTitleBarRectResolvesAfterDraw pins the one
// assumption the close-glyph click handler depends on: that Flex
// actually resolves the title bar's own rect to match
// scrollAcknowledgeLayout's (x, y, width), not a stale or zero one —
// the same "Flex only resolves a child's own rect during Draw" gotcha
// drawConsigliereLayout's own doc comment documents elsewhere. If
// this ever stops holding, the click handler's own math is reading
// the wrong rect regardless of how forgiving its column check is.
func TestScrollAcknowledgeTitleBarRectResolvesAfterDraw(t *testing.T) {
	r, _ := newOptionsRoot(t)
	r.SetRect(0, 0, 100, 40)
	r.openScrollAcknowledgeNotice("Title", "message", "Yes, enable", func() {})
	wantX, wantY, wantWidth, _ := r.scrollAcknowledgeLayout.GetRect()

	screen := tcell.NewSimulationScreen("")
	if err := screen.Init(); err != nil {
		t.Fatalf("screen.Init: %v", err)
	}
	defer screen.Fini()
	screen.SetSize(100, 40)
	r.scrollAcknowledgeLayout.Draw(screen)

	gotX, gotY, gotWidth, gotHeight := r.scrollAcknowledgeTitleBar.GetRect()
	if gotX != wantX || gotY != wantY || gotWidth != wantWidth || gotHeight != 1 {
		t.Errorf("title bar rect after Draw = (%d,%d,%d,%d), want (%d,%d,%d,1)",
			gotX, gotY, gotWidth, gotHeight, wantX, wantY, wantWidth)
	}
}

// TestScrollAcknowledgeTitleBarCloseGlyphClickCancels pins a real,
// reported bug: the title bar used to be plain text with no close
// glyph at all, unlike every sibling dialog's own clickable "✕".
func TestScrollAcknowledgeTitleBarCloseGlyphClickCancels(t *testing.T) {
	r, _ := newOptionsRoot(t)
	r.SetRect(0, 0, 100, 40)
	called := false
	r.openScrollAcknowledgeNotice("Title", "message", "Yes, enable", func() { called = true })

	x, y, width, _ := r.scrollAcknowledgeTitleBar.GetRect()
	closeX := x + toolWindowCloseButtonCol(0, width)
	captured, _ := r.captureScrollAcknowledgeTitleBarMouse(tview.MouseLeftDown, tcell.NewEventMouse(closeX, y, tcell.ButtonNone, 0))

	if captured != tview.MouseConsumed {
		t.Error("clicking the close glyph should consume the click")
	}
	if r.activePage == scrollAcknowledgePage {
		t.Error("clicking the close glyph should have closed the notice")
	}
	if called {
		t.Error("clicking the close glyph must never run onConfirm")
	}
}

// TestScrollAcknowledgeTitleBarCloseGlyphToleratesOneColumnOff pins
// the defensive, one-column-wider hit area added after a real,
// reported miss whose exact cause wasn't pinned down (see
// captureScrollAcknowledgeTitleBarMouse's own doc comment) — a click
// one column past the computed position must still count.
func TestScrollAcknowledgeTitleBarCloseGlyphToleratesOneColumnOff(t *testing.T) {
	r, _ := newOptionsRoot(t)
	r.SetRect(0, 0, 100, 40)
	r.openScrollAcknowledgeNotice("Title", "message", "Yes, enable", func() {})

	x, y, width, _ := r.scrollAcknowledgeTitleBar.GetRect()
	closeX := x + toolWindowCloseButtonCol(0, width) + 1
	captured, _ := r.captureScrollAcknowledgeTitleBarMouse(tview.MouseLeftDown, tcell.NewEventMouse(closeX, y, tcell.ButtonNone, 0))

	if captured != tview.MouseConsumed {
		t.Error("clicking one column past the close glyph should still consume the click")
	}
	if r.activePage == scrollAcknowledgePage {
		t.Error("clicking one column past the close glyph should still have closed the notice")
	}
}

// TestEscapeFromScrollAcknowledgeMessageCancels pins Escape as a
// cancel path from either focus stop, the same as every other
// dialog's convention here.
func TestEscapeFromScrollAcknowledgeMessageCancels(t *testing.T) {
	r, _ := newOptionsRoot(t)
	r.SetRect(0, 0, 100, 40)
	called := false
	r.openScrollAcknowledgeNotice("Title", "message", "Yes, enable", func() { called = true })

	r.scrollAcknowledgeMessageView.InputHandler()(tcell.NewEventKey(tcell.KeyEscape, 0, tcell.ModNone), func(p tview.Primitive) { r.app.SetFocus(p) })

	if r.activePage == scrollAcknowledgePage {
		t.Error("Escape should have closed the notice")
	}
	if called {
		t.Error("Escape must never run onConfirm")
	}
}
