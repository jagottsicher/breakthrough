package ui

import (
	"os"
	"sort"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"golang.org/x/term"
)

// TestCaptureBashLineKeyEnterVsNewline pins captureBashLineKey's own
// central split: plain Enter is consumed here (runs the buffer, see
// runBashCommand) rather than reaching TextArea's own default "insert a
// newline" handling, while Alt+Enter is returned unchanged, and Ctrl+J
// a synthesized plain Enter event, specifically so that default
// handling still fires either way — two ways to compose a multi-line
// script before running it (see the function's own doc comment on why
// there are two: Alt+Enter isn't reliable across terminals, per the
// user's own direct report that it "funktioniert nicht"). "echo hi"
// runs through runShellCommandFullScreen — every command does now (see
// newBashConsole's own doc comment) — where Suspend is a no-op in this
// screenless test environment (see
// TestCaptureStatusBarMouseEditClickRunsEditAction's own doc comment in
// bottombar_test.go), so this only pins the wiring and that the buffer
// clears afterwards, not that a command actually ran.
func TestCaptureBashLineKeyEnterVsNewline(t *testing.T) {
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

	got := r.captureBashLineKey(tcell.NewEventKey(tcell.KeyCtrlJ, 0, tcell.ModNone))
	if got == nil {
		t.Fatal("Ctrl+J should not be consumed as nil — it's turned into a synthesized Enter event for TextArea's own default handling")
	}
	if got.Key() != tcell.KeyEnter {
		t.Errorf("Ctrl+J's synthesized event key = %v, want %v (KeyEnter, so TextArea inserts a newline)", got.Key(), tcell.KeyEnter)
	}
	if got := r.bashLine.GetText(); got != "echo hi" {
		t.Errorf("bashLine text after Ctrl+J (capture layer only) = %q, want unchanged %q", got, "echo hi")
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

// TestBashLineCtrlJInsertsNewlineEndToEnd pins Ctrl+J's own effect all
// the way through — not just that captureBashLineKey hands back a
// synthesized Enter event (see TestCaptureBashLineKeyEnterVsNewline),
// but that routing it through bashLine's real, wrapped InputHandler
// (see Box.WrapInputHandler, which applies SetInputCapture before its
// own default handling — the same mechanism SetInputCapture's own
// documentation describes) actually inserts a newline into the buffer,
// the way TextArea's own default Enter handling does.
func TestBashLineCtrlJInsertsNewlineEndToEnd(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}

	r.bashLine.SetText("echo hi", true) // cursor at the end
	r.bashLine.InputHandler()(tcell.NewEventKey(tcell.KeyCtrlJ, 0, tcell.ModNone), func(tview.Primitive) {})

	if got := r.bashLine.GetText(); got != "echo hi\n" {
		t.Errorf("bashLine text after Ctrl+J = %q, want %q (a newline appended, not run)", got, "echo hi\n")
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

// TestFullScreenShellArgsIncludesInteractiveFlag pins the fix for the
// user's own direct report ("der user sollte auch seine aliase und seine
// bashrc oder sowas vorher sourced bekommen. aliase wie ll funktionieren
// sonst nicht"): "-i" must be present so userShell() starts up the same
// way a real login session would (~/.bashrc sourced, aliases live) — see
// fullScreenShellArgs's own doc comment for why plain "-c" alone isn't
// enough.
func TestFullScreenShellArgsIncludesInteractiveFlag(t *testing.T) {
	got := fullScreenShellArgs("ll")
	want := []string{"-i", "-c", "ll"}
	if len(got) != len(want) {
		t.Fatalf("fullScreenShellArgs(%q) = %v, want %v", "ll", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("fullScreenShellArgs(%q)[%d] = %q, want %q", "ll", i, got[i], want[i])
		}
	}
}

// TestBashLineTabDoesNotCloseConsoleEndToEnd pins the fix for the user's
// own direct report ("der bash prompt editor wird geschlossen, wenn man
// tab drückt"): routed through bashLine's real, wrapped InputHandler (see
// TestBashLineCtrlJInsertsNewlineEndToEnd's own doc comment on why that
// matters — SetInputCapture applies before TextArea's own default
// handling), Tab must never reach TextArea's own default KeyTab case,
// which — like SetDisabled once did, see runShellCommandFullScreen's own
// history — calls SetFinishedFunc and collapses the console. Also
// confirms the word at the cursor was completed along the way, i.e. that
// this exercises completeBashLine itself and not just the interception.
func TestBashLineTabDoesNotCloseConsoleEndToEnd(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}

	r.app.SetFocus(r.bashLine)
	if !r.bashLine.HasFocus() {
		t.Fatal("setup: bashLine should have focus")
	}
	r.bashLine.SetText("ban", true)

	r.bashLine.InputHandler()(tcell.NewEventKey(tcell.KeyTab, 0, tcell.ModNone), func(tview.Primitive) {})

	if !r.bashLine.HasFocus() {
		t.Error("Tab should not return focus to the panel (and so should not collapse the console) — it should complete instead")
	}
	if got := r.bashLine.GetText(); got != "banana.txt" {
		t.Errorf("bashLine text after Tab = %q, want %q (completed, not left alone or run)", got, "banana.txt")
	}
}

// TestCompleteBashLine pins completeBashLine's own matching behavior
// directly against fixtureDir's known entries (apple.txt, apricot.txt,
// banana.txt, app-data/ — see fixtureDir's own doc comment in
// panel_test.go): a single match completes fully, several matches
// complete only as far as they agree (the same completions/
// longestCommonPrefix logic the path header's own Tab already uses, see
// Panel.completePath), and no match leaves the typed text alone. "a"
// completes to "ap" rather than further: apple.txt and app-data/ share
// "app", but apricot.txt (also "a"-prefixed) only shares "ap" with
// either, so "ap" is as far as all three can agree.
func TestCompleteBashLine(t *testing.T) {
	dir := fixtureDir(t)

	tests := []struct {
		name string
		in   string
		want string
	}{
		{"single match completes fully", "cat ban", "cat banana.txt"},
		{"several matches complete to their common prefix", "cat a", "cat ap"},
		{"no match leaves the text alone", "zzz", "zzz"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, err := NewRoot(tview.NewApplication(), dir)
			if err != nil {
				t.Fatalf("NewRoot: %v", err)
			}
			r.bashLine.SetText(tt.in, true) // cursor at the end, i.e. right after the word to complete

			r.completeBashLine()

			if got := r.bashLine.GetText(); got != tt.want {
				t.Errorf("bashLine text after completeBashLine() on %q = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// TestCompleteBashLineMultiLineTouchesOnlyTheCursorsLine pins Replace's
// own absolute-offset addressing (see completeBashLine's own doc comment)
// against a multi-line buffer (composed via Ctrl+J/Alt+Enter, see
// captureBashLineKey): completing a word on a later line must account for
// every earlier line's own length, or it lands in the wrong place — and
// must never touch any line but the cursor's own.
func TestCompleteBashLineMultiLineTouchesOnlyTheCursorsLine(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}

	r.bashLine.SetText("echo hi\ncat ban", true) // cursor at the very end, on the second line

	r.completeBashLine()

	want := "echo hi\ncat banana.txt"
	if got := r.bashLine.GetText(); got != want {
		t.Errorf("bashLine text after completeBashLine() = %q, want %q", got, want)
	}
}

// TestCompleteBashLineAmbiguousOpensCompletionPicker pins the fix for the
// user's own direct report: typing "/etc/" and pressing Tab repeatedly
// did nothing at all, because completions() for "ap" against fixtureDir
// (apple.txt, apricot.txt, banana.txt, app-data/ — see fixtureDir's own
// doc comment in panel_test.go) already comes back with "ap" as its own
// longest common prefix — apple.txt/app-data/ share "app", but
// apricot.txt only shares "ap" with either (see TestCompleteBashLine's
// own doc comment), so there's nothing left to extend to, yet 3 matches
// still remain ambiguous. Instead of a no-op, that now opens the
// completion picker (see openCompletionPicker) with all 3 candidates —
// real bash's own "list every option" response to the same situation —
// rather than requiring an identical second Tab press first.
func TestCompleteBashLineAmbiguousOpensCompletionPicker(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}

	r.bashLine.SetText("ap", true)

	r.completeBashLine()

	if r.activePage != pickerPage {
		t.Fatalf("activePage = %q, want %q (the completion picker should have opened)", r.activePage, pickerPage)
	}
	if got := r.bashLine.GetText(); got != "ap" {
		t.Errorf("bashLine text should stay unchanged until a candidate is actually picked, got %q", got)
	}

	var got []string
	for i := 0; i < r.picker.GetItemCount(); i++ {
		main, _ := r.picker.GetItemText(i)
		got = append(got, main)
	}
	sort.Strings(got)
	want := []string{"app-data/", "apple.txt", "apricot.txt"}
	sort.Strings(want)
	if len(got) != len(want) {
		t.Fatalf("picker items = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("picker items = %v, want %v", got, want)
			break
		}
	}
}

// TestOpenCompletionPickerPickReplacesWordAndReturnsFocusToBashLine pins
// the fix for the second half of the same report, once the picker itself
// is open: picking a candidate (Enter, List's own default behavior)
// replaces exactly the word at start/end with the picked match's full
// text, closes the picker, and — unlike hideOverlay's own ordinary
// fallback to the panel once the overlay stack empties (see
// pushOverlayReturningFocusTo/overlayFrame.emptyStackFocus) — hands focus
// back to bashLine, not the panel, since that's where this opened from.
func TestOpenCompletionPickerPickReplacesWordAndReturnsFocusToBashLine(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.bashLine.SetText("foo", true)
	r.app.SetFocus(r.bashLine)

	r.openCompletionPicker([]string{"foobar", "foobaz"}, 0, 3)

	if r.activePage != pickerPage {
		t.Fatalf("activePage = %q, want %q", r.activePage, pickerPage)
	}
	if got := r.picker.GetItemCount(); got != 2 {
		t.Fatalf("picker item count = %d, want 2", got)
	}
	if !r.picker.HasFocus() {
		t.Error("picker should have keyboard focus while it's open, for arrow-key navigation")
	}

	r.picker.InputHandler()(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone), func(tview.Primitive) {})

	if got := r.bashLine.GetText(); got != "foobar" {
		t.Errorf("bashLine text after picking the first candidate = %q, want %q", got, "foobar")
	}
	if r.activePage != "" {
		t.Errorf("activePage = %q after picking, want closed", r.activePage)
	}
	if !r.bashLine.HasFocus() {
		t.Error("bashLine should have focus back after picking — not the panel, which hideOverlay's own ordinary fallback would otherwise leave it on")
	}
}

// TestOpenCompletionPickerCancelLeavesWordAndReturnsFocusToBashLine pins
// Escape's behavior: same focus-return fix as the pick case above, but
// leaving the word untouched, the same as completeBashLine's own no-op
// paths do.
func TestOpenCompletionPickerCancelLeavesWordAndReturnsFocusToBashLine(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.bashLine.SetText("foo", true)
	r.app.SetFocus(r.bashLine)

	r.openCompletionPicker([]string{"foobar", "foobaz"}, 0, 3)
	r.picker.InputHandler()(tcell.NewEventKey(tcell.KeyEscape, 0, tcell.ModNone), func(tview.Primitive) {})

	if got := r.bashLine.GetText(); got != "foo" {
		t.Errorf("bashLine text after cancelling = %q, want unchanged %q", got, "foo")
	}
	if r.activePage != "" {
		t.Errorf("activePage = %q after cancelling, want closed", r.activePage)
	}
	if !r.bashLine.HasFocus() {
		t.Error("bashLine should have focus back after cancelling — not the panel")
	}
}

// TestBashConsoleStaysExpandedWhileCompletionPickerOpen pins the fix for
// the user's own direct report: opening the completion picker used to
// collapse the console right back down underneath it (moving focus to
// the picker blurs bashLine, which triggers collapseBashConsole, bashLine's
// own BlurFunc) — nonsensical, since the picker exists to keep completing
// bashLine's own buffer, which the user still needs to see. See
// bashLineCompletingPick's own doc comment on the Root struct for the fix.
func TestBashConsoleStaysExpandedWhileCompletionPickerOpen(t *testing.T) {
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

	r.app.SetFocus(r.bashLine) // triggers expandBashConsole via bashLine's own FocusFunc
	r.Draw(screen)

	wantExpanded := 24 / 2
	if _, _, _, h := r.bashConsole.GetRect(); h != wantExpanded {
		t.Fatalf("setup: expanded bashConsole height = %d, want %d", h, wantExpanded)
	}

	r.openCompletionPicker([]string{"foobar", "foobaz"}, 0, 0)
	r.Draw(screen)

	if _, _, _, h := r.bashConsole.GetRect(); h != wantExpanded {
		t.Errorf("bashConsole height while the completion picker is open = %d, want still %d (unchanged, not collapsed)", h, wantExpanded)
	}
	if _, _, _, h := r.bashHint.GetRect(); h != 1 {
		t.Errorf("bashHint height while the completion picker is open = %d, want still 1 (unchanged, not hidden)", h)
	}
}

// TestBashLineCursorScreenPosition pins bashLineCursorScreenPosition
// directly, for the overwhelming common case it's exact for (see its own
// doc comment): a single, unwrapped line with the cursor at the end —
// column equal to the line's own length, row equal to bashLine's own
// (border-less, so inner == outer) rect top, since neither the cursor
// nor the scroll offset ever leaves row 0 for one short line.
func TestBashLineCursorScreenPosition(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.bashLine.SetRect(0, 12, 80, 12)

	line := "ls -halF /etc/"
	r.bashLine.SetText(line, true) // cursor at the end

	gotX, gotY := r.bashLineCursorScreenPosition()
	wantX, wantY := len(line), 12
	if gotX != wantX || gotY != wantY {
		t.Errorf("bashLineCursorScreenPosition() = (%d,%d), want (%d,%d)", gotX, gotY, wantX, wantY)
	}
}

// TestCompletionPickerPositionedAtCursor pins the fix for the user's own
// direct report that the picker appeared "oben links" (top-left,
// effectively bashConsole's own left edge with a tall list pinned to the
// panel's own top by clampToPanel) instead of tracking the word actually
// being completed: its bottom edge now sits exactly on bashLine's own
// current cursor position (see completionPickerPosition/
// bashLineCursorScreenPosition), opening upward from there — the same
// direction the console itself grows on focus.
func TestCompletionPickerPositionedAtCursor(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.bashLine.SetRect(0, 12, 80, 12)

	line := "ls -halF /etc/"
	r.bashLine.SetText(line, true) // cursor at the end, right after the word being completed

	gotX, gotY := r.completionPickerPosition(20, 5)
	wantX, wantY := len(line), 12-5 // the cursor's own column/row, height subtracted to open upward
	if gotX != wantX || gotY != wantY {
		t.Errorf("completionPickerPosition(20, 5) = (%d,%d), want (%d,%d)", gotX, gotY, wantX, wantY)
	}
}

// TestCompletionBasename pins the display-vs-insert split (see
// openCompletionPicker/completionBasename): a directory's own trailing
// "/" (see Panel.completions) survives basename extraction — unlike a
// plain filepath.Base, which would strip it — so a directory stays
// visually distinguishable from a file in the picker; a bare filename
// with no directory component at all (matches from completions() run
// against the panel's own current directory, which prepends nothing) is
// returned unchanged, having no separator to strip in the first place.
func TestCompletionBasename(t *testing.T) {
	tests := []struct{ in, want string }{
		{"/etc/hosts", "hosts"},
		{"/etc/cron.d/", "cron.d/"},
		{"apple.txt", "apple.txt"},
		{"app-data/", "app-data/"},
	}
	for _, tt := range tests {
		if got := completionBasename(tt.in); got != tt.want {
			t.Errorf("completionBasename(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
