package ui

import (
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// TestCaptureBashLineKeyEnterVsAltEnter pins captureBashLineKey's own
// central split: plain Enter is consumed here (runs the buffer, see
// runBashCommand) rather than reaching TextArea's own default "insert a
// newline" handling, while Alt+Enter is returned unchanged specifically
// so that default handling still fires — the one way to compose a
// multi-line script before running it (see the function's own doc
// comment). "echo hi" runs through runShellCommandFullScreen — every
// command does now (see newBashConsole's own doc comment) — where
// Suspend is a no-op in this screenless test environment (see
// TestCaptureStatusBarMouseEditClickRunsEditAction's own doc comment in
// bottombar_test.go), so this only pins the wiring and that the buffer
// clears afterwards, not that a command actually ran.
func TestCaptureBashLineKeyEnterVsAltEnter(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}

	r.bashLine.SetText("echo hi", true)
	if got := r.captureBashLineKey(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModAlt)); got == nil {
		t.Error("Alt+Enter should be returned unchanged (to insert a newline), not consumed")
	}
	// Unconsumed by captureBashLineKey — confirm the buffer itself was
	// left alone (runBashCommand was NOT called for this one).
	if got := r.bashLine.GetText(); got != "echo hi" {
		t.Errorf("bashLine text after Alt+Enter (capture layer only) = %q, want unchanged %q", got, "echo hi")
	}

	if got := r.captureBashLineKey(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone)); got != nil {
		t.Error("plain Enter should be consumed (nil returned) — it runs the buffer instead of inserting a newline")
	}
	if got := r.bashLine.GetText(); got != "" {
		t.Errorf("bashLine text after plain Enter = %q, want cleared (runBashCommand ran)", got)
	}
}

// TestExpandCollapseBashConsole pins the resize itself: collapsed to a
// single row until bashLine gains focus, then grown to half the
// terminal's current height (see expandBashConsole/collapseBashConsole)
// — and back to a single row again on blur.
func TestExpandCollapseBashConsole(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}

	screen := tcell.NewSimulationScreen("")
	if err := screen.Init(); err != nil {
		t.Fatalf("screen.Init: %v", err)
	}
	defer screen.Fini()
	screen.SetSize(80, 24)
	r.SetRect(0, 0, 80, 24)
	r.Draw(screen) // cascades layout down through mainLayout to bashLine

	if _, _, _, h := r.bashLine.GetRect(); h != 1 {
		t.Errorf("collapsed bashLine height = %d, want 1", h)
	}

	r.app.SetFocus(r.bashLine) // triggers expandBashConsole via bashLine's own FocusFunc
	r.Draw(screen)

	wantExpanded := 24 / 2
	if _, _, _, h := r.bashLine.GetRect(); h != wantExpanded {
		t.Errorf("expanded bashLine height = %d, want %d (half the 24-row screen)", h, wantExpanded)
	}

	r.app.SetFocus(r.panel.table) // moves focus away — triggers collapseBashConsole via bashLine's own BlurFunc
	r.Draw(screen)

	if _, _, _, h := r.bashLine.GetRect(); h != 1 {
		t.Errorf("bashLine height after collapse = %d, want 1", h)
	}
}

// TestBashLineEscapeReturnsFocusToPanel pins that Escape (the one key,
// alongside Backtab, TextArea itself calls its own FinishedFunc for —
// see newBashConsole's own doc comment) hands focus back to the panel,
// which in turn collapses bashLine via its own BlurFunc.
func TestBashLineEscapeReturnsFocusToPanel(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}

	r.app.SetFocus(r.bashLine)
	if !r.bashLine.HasFocus() {
		t.Fatal("setup: bashLine should have focus")
	}

	r.bashLine.InputHandler()(tcell.NewEventKey(tcell.KeyEscape, 0, tcell.ModNone), func(tview.Primitive) {})

	if !r.panel.table.HasFocus() {
		t.Error("Escape should return focus to the panel's table")
	}
}
