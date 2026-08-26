package ui

import (
	"os"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"golang.org/x/term"
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
// single row (bashHint hidden) until bashLine gains focus, then grown
// to half the terminal's current height — one row for bashHint's own
// legend, the rest for bashLine (see expandBashConsole/
// collapseBashConsole) — and back to a single row again on blur.
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
	r.Draw(screen) // cascades layout down through mainLayout to bashConsole

	if _, _, _, h := r.bashConsole.GetRect(); h != 1 {
		t.Errorf("collapsed bashConsole height = %d, want 1", h)
	}
	if _, _, _, h := r.bashHint.GetRect(); h != 0 {
		t.Errorf("collapsed bashHint height = %d, want 0 (hidden)", h)
	}

	r.app.SetFocus(r.bashLine) // triggers expandBashConsole via bashLine's own FocusFunc
	r.Draw(screen)

	wantExpanded := 24 / 2
	if _, _, _, h := r.bashConsole.GetRect(); h != wantExpanded {
		t.Errorf("expanded bashConsole height = %d, want %d (half the 24-row screen)", h, wantExpanded)
	}
	if _, _, _, h := r.bashHint.GetRect(); h != 1 {
		t.Errorf("expanded bashHint height = %d, want 1", h)
	}
	if _, _, _, h := r.bashLine.GetRect(); h != wantExpanded-1 {
		t.Errorf("expanded bashLine height = %d, want %d (whatever's left of the expanded region)", h, wantExpanded-1)
	}

	r.app.SetFocus(r.panel.table) // moves focus away — triggers collapseBashConsole via bashLine's own BlurFunc
	r.Draw(screen)

	if _, _, _, h := r.bashConsole.GetRect(); h != 1 {
		t.Errorf("bashConsole height after collapse = %d, want 1", h)
	}
	if _, _, _, h := r.bashHint.GetRect(); h != 0 {
		t.Errorf("bashHint height after collapse = %d, want 0 (hidden)", h)
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

// TestBashLineAtFirstLastLine pins bashLineAtFirstLine/bashLineAtLastLine
// directly: a single-line buffer's only line is always both; a
// multi-line buffer's cursor is only one or the other, depending on
// where TextArea.SetText's own cursorAtTheEnd left it.
func TestBashLineAtFirstLastLine(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}

	r.bashLine.SetText("single line", true)
	if !r.bashLineAtFirstLine() {
		t.Error("a single-line buffer's cursor should count as being on the first line")
	}
	if !r.bashLineAtLastLine() {
		t.Error("a single-line buffer's cursor should count as being on the last line")
	}

	r.bashLine.SetText("line one\nline two\nline three", false) // cursor at the very start
	if !r.bashLineAtFirstLine() {
		t.Error("cursor at the start of a multi-line buffer should count as being on the first line")
	}
	if r.bashLineAtLastLine() {
		t.Error("cursor at the start of a multi-line buffer should NOT count as being on the last line")
	}

	r.bashLine.SetText("line one\nline two\nline three", true) // cursor at the very end
	if r.bashLineAtFirstLine() {
		t.Error("cursor at the end of a multi-line buffer should NOT count as being on the first line")
	}
	if !r.bashLineAtLastLine() {
		t.Error("cursor at the end of a multi-line buffer should count as being on the last line")
	}
}

// TestCaptureBashLineKeyUpDownRecallHistoryAtBoundaries pins the smart
// Up/Down behavior (see captureBashLineKey's own doc comment): a
// single-line buffer's Up/Down recall history directly, the same as
// Ctrl+P/Ctrl+N, since the only line is always both the first and the
// last; a multi-line buffer only recalls history at the actual
// boundary, leaving Up/Down at any other line unconsumed for TextArea's
// own default cursor-movement handling.
func TestCaptureBashLineKeyUpDownRecallHistoryAtBoundaries(t *testing.T) {
	isolateHistoryFile(t)
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}

	r.runBashCommand("first")
	r.runBashCommand("second")

	r.bashLine.SetText("in progress", true)
	if got := r.captureBashLineKey(tcell.NewEventKey(tcell.KeyUp, 0, tcell.ModNone)); got != nil {
		t.Error("Up on a single-line buffer should be consumed (history recall)")
	}
	if got := r.bashLine.GetText(); got != "second" {
		t.Errorf("after Up on a single-line buffer, text = %q, want %q (newest history entry)", got, "second")
	}

	r.bashLine.SetText("line one\nline two", false) // cursor at the first line
	if got := r.captureBashLineKey(tcell.NewEventKey(tcell.KeyDown, 0, tcell.ModNone)); got == nil {
		t.Error("Down from the first line of a two-line buffer should be returned unchanged (cursor movement, not history)")
	}

	r.bashLine.SetText("line one\nline two", true) // cursor at the last line
	if got := r.captureBashLineKey(tcell.NewEventKey(tcell.KeyUp, 0, tcell.ModNone)); got == nil {
		t.Error("Up from the last line of a two-line buffer should be returned unchanged (cursor movement, not history)")
	}
}

// TestWaitForEscapeReturnsImmediatelyWithoutARealTerminal pins
// waitForEscape's own best-effort short-circuit: with no real terminal
// on stdin (the ordinary case under `go test`), it must return promptly
// rather than block on a keypress that will never come. Skips itself if
// stdin genuinely is a terminal in whatever environment this runs in —
// waitForEscape would then (correctly) wait for a real keypress, which
// this test isn't set up to provide.
func TestWaitForEscapeReturnsImmediatelyWithoutARealTerminal(t *testing.T) {
	if term.IsTerminal(int(os.Stdin.Fd())) {
		t.Skip("stdin is a real terminal in this environment — waitForEscape would correctly wait for a keypress here")
	}

	done := make(chan struct{})
	go func() {
		waitForEscape()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("waitForEscape did not return promptly without a real terminal")
	}
}
