package ui

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

func TestNeedsRealTerminal(t *testing.T) {
	tests := []struct {
		command string
		want    bool
	}{
		{"ls -la", false},
		{"echo hello", false},
		{"git status", false},
		{"git log", false},
		{"du -sh .", false},
		{"vim foo.txt", true},
		{"vi foo.txt", true},
		{"nvim foo.txt", true},
		{"sudo apt update", true},
		{"su -", true},
		{"cat file.txt | less", true},
		{"cat file.txt|less", true}, // no spaces around the pipe
		{"cat file.txt | grep foo", false},
		{"ssh example.com", true},
		{"top", true},
		{"watch -n1 date", true},
		// mc itself — regression pin for the user's own direct report:
		// this was missing from interactivePrograms, so "mc" silently
		// ran captured instead of going full-screen.
		{"mc", true},
		{"ranger", true},
		{"tig", true},
		{"", false},
		// A known-interactive name appearing only as another command's own
		// argument (not as a command itself) still matches — see
		// interactivePrograms' own doc comment on deliberately erring
		// toward a false positive (an unnecessary screen flip) over a
		// false negative (a genuinely broken, garbled capture).
		{"echo less", true},
		// A flag *value* containing a program name, not a standalone
		// token, correctly does NOT match — "=" isn't a token separator.
		{"journalctl --pager=less", false},
	}
	for _, tt := range tests {
		if got := needsRealTerminal(tt.command); got != tt.want {
			t.Errorf("needsRealTerminal(%q) = %v, want %v", tt.command, got, tt.want)
		}
	}
}

// TestCaptureBashLineKeyEnterVsAltEnter pins captureBashLineKey's own
// central split: plain Enter is consumed here (runs the buffer, see
// runBashCommand) rather than reaching TextArea's own default "insert a
// newline" handling, while Alt+Enter is returned unchanged specifically
// so that default handling still fires — the one way to compose a
// multi-line script before running it (see the function's own doc
// comment).
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
// terminal's current height — split bashConsoleInputRows for bashLine,
// the rest for bashHistoryView (see expandBashConsole/
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

	r.app.SetFocus(r.bashLine) // triggers expandBashConsole via bashLine's own FocusFunc
	r.Draw(screen)

	wantExpanded := 24 / 2
	if _, _, _, h := r.bashConsole.GetRect(); h != wantExpanded {
		t.Errorf("expanded bashConsole height = %d, want %d (half the 24-row screen)", h, wantExpanded)
	}
	if _, _, _, h := r.bashLine.GetRect(); h != bashConsoleInputRows {
		t.Errorf("expanded bashLine height = %d, want %d", h, bashConsoleInputRows)
	}
	if _, _, _, h := r.bashHistoryView.GetRect(); h != wantExpanded-bashConsoleInputRows {
		t.Errorf("expanded bashHistoryView height = %d, want %d (whatever's left of the expanded region)", h, wantExpanded-bashConsoleInputRows)
	}

	r.app.SetFocus(r.panel.table) // moves focus away — triggers collapseBashConsole via bashLine's own BlurFunc
	r.Draw(screen)

	if _, _, _, h := r.bashConsole.GetRect(); h != 1 {
		t.Errorf("bashConsole height after collapse = %d, want 1", h)
	}
}

// TestBashLineEscapeReturnsFocusToPanel pins that Escape (the one key,
// alongside Backtab, TextArea itself calls its own FinishedFunc for —
// see newBashConsole's own doc comment) hands focus back to the panel,
// which in turn collapses bashConsole via bashLine's BlurFunc.
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

// TestScrollBashHistory pins PageUp/PageDown's own page size (the
// viewport's current height) and that scrolling up stops at row 0
// rather than going negative.
func TestScrollBashHistory(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.bashHistoryView.SetRect(0, 0, 80, 10) // a 10-row viewport

	r.scrollBashHistory(1) // PageDown
	if row, _ := r.bashHistoryView.GetScrollOffset(); row != 10 {
		t.Errorf("scroll offset after PageDown = %d, want 10 (one 10-row page)", row)
	}

	r.scrollBashHistory(-1) // PageUp, back to the top
	if row, _ := r.bashHistoryView.GetScrollOffset(); row != 0 {
		t.Errorf("scroll offset after PageUp = %d, want 0", row)
	}

	r.scrollBashHistory(-1) // already at the top — must not go negative
	if row, _ := r.bashHistoryView.GetScrollOffset(); row != 0 {
		t.Errorf("scroll offset after PageUp at the top = %d, want clamped to 0", row)
	}
}

// TestCaptureBashLineKeyPageUpDown pins the wiring: PageUp/PageDown
// reach scrollBashHistory through captureBashLineKey (and are consumed
// — TextArea's own default page-the-cursor behavior never fires), not
// just that scrollBashHistory itself works in isolation.
func TestCaptureBashLineKeyPageUpDown(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.bashHistoryView.SetRect(0, 0, 80, 5)

	if got := r.captureBashLineKey(tcell.NewEventKey(tcell.KeyPgDn, 0, tcell.ModNone)); got != nil {
		t.Error("PageDown should be consumed by captureBashLineKey")
	}
	if row, _ := r.bashHistoryView.GetScrollOffset(); row != 5 {
		t.Errorf("scroll offset after PageDown via captureBashLineKey = %d, want 5", row)
	}

	if got := r.captureBashLineKey(tcell.NewEventKey(tcell.KeyPgUp, 0, tcell.ModNone)); got != nil {
		t.Error("PageUp should be consumed by captureBashLineKey")
	}
	if row, _ := r.bashHistoryView.GetScrollOffset(); row != 0 {
		t.Errorf("scroll offset after PageUp via captureBashLineKey = %d, want 0", row)
	}
}

// TestFinishCapturedCommand pins runShellCommandCaptured's own
// completion handler directly (see its own doc comment on why: it needs
// a real Application event loop behind QueueUpdateDraw to reach any
// other way) — clearing bashRunningCmd either way, and reporting the
// exit error into bashHistoryView only when there was one.
func TestFinishCapturedCommand(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}

	r.bashRunningCmd = exec.Command("true") // any non-nil sentinel — never started, doesn't matter here
	r.finishCapturedCommand("ok command", nil)
	if r.bashRunningCmd != nil {
		t.Error("finishCapturedCommand should clear bashRunningCmd on a clean exit")
	}
	if got := r.bashHistoryView.GetText(true); strings.Contains(got, "ok command") {
		t.Errorf("bashHistoryView = %q, should not report anything for a clean exit", got)
	}

	r.bashRunningCmd = exec.Command("true")
	r.finishCapturedCommand("failing command", fmt.Errorf("boom"))
	if r.bashRunningCmd != nil {
		t.Error("finishCapturedCommand should clear bashRunningCmd on a failing exit too")
	}
	if got := r.bashHistoryView.GetText(true); !strings.Contains(got, "failing command") || !strings.Contains(got, "boom") {
		t.Errorf("bashHistoryView = %q, want it to report the exit error", got)
	}
}

// TestRunShellCommandCapturedIgnoresSecondWhileRunning pins the guard
// against a second captured command starting while one is already
// running (see runShellCommandCaptured's own doc comment): bashRunningCmd
// must stay pointing at the first command, not be silently overwritten.
func TestRunShellCommandCapturedIgnoresSecondWhileRunning(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}

	first := exec.Command("sleep", "30")
	if err := first.Start(); err != nil {
		t.Skipf("sleep not available in this environment: %v", err)
	}
	defer first.Process.Kill() //nolint:errcheck
	r.bashRunningCmd = first

	r.runShellCommandCaptured("echo should-be-ignored")

	if r.bashRunningCmd != first {
		t.Error("a second captured command should not replace bashRunningCmd while one is already running")
	}
	if got := r.bashHistoryView.GetText(true); strings.Contains(got, "should-be-ignored") {
		t.Errorf("bashHistoryView = %q, the ignored second command should never have been echoed", got)
	}
}

// TestRunShellCommandCapturedDoesNotBlurBashLine pins the fix for the
// user's own report: the console was closing right after every command
// — starting a captured command used to call bashLine.SetDisabled(true),
// and TextArea's own SetDisabled unconditionally re-fires its
// FinishedFunc, which newBashConsole wires to hand focus back to the
// panel — collapsing bashConsole (via bashLine's own BlurFunc) the
// instant a command started, not on Escape/click-away as intended (see
// runShellCommandCaptured's own doc comment on why SetDisabled isn't
// used here anymore).
func TestRunShellCommandCapturedDoesNotBlurBashLine(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}

	r.app.SetFocus(r.bashLine)
	if !r.bashLine.HasFocus() {
		t.Fatal("setup: bashLine should have focus")
	}

	r.runShellCommandCaptured("sleep 30")
	if r.bashRunningCmd == nil {
		t.Skip("sleep not available in this environment (or failed to start)")
	}
	defer r.bashRunningCmd.Process.Kill() //nolint:errcheck

	if !r.bashLine.HasFocus() {
		t.Error("bashLine lost focus merely from starting a captured command — the console would have collapsed out from under the user")
	}
}

// TestInterruptBashCommand pins interruptBashCommand's own two cases —
// false with nothing running, true (and a real SIGINT actually
// delivered) with something running — the same signal a real shell's
// own Ctrl+C sends its foreground job.
func TestInterruptBashCommand(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}

	if r.interruptBashCommand() {
		t.Error("interruptBashCommand() = true with nothing running, want false")
	}

	cmd := exec.Command("sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Skipf("sleep not available in this environment: %v", err)
	}
	r.bashRunningCmd = cmd

	if !r.interruptBashCommand() {
		t.Error("interruptBashCommand() = false with a command running, want true")
	}

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		if err == nil {
			t.Error("sleep 30 exited cleanly after SIGINT, want a signal-terminated error")
		}
	case <-time.After(5 * time.Second):
		cmd.Process.Kill() //nolint:errcheck
		t.Fatal("sleep 30 did not exit within 5s of SIGINT")
	}
}

// TestRequestCancelInterruptsRunningBashCommandFirst pins RequestCancel's
// own priority order: a running captured command is interrupted instead
// of whatever RequestCancel would otherwise do (back out of an overlay,
// cancel a header edit) — checked here via the overlay case, since
// that's the easiest to set up and observe.
func TestRequestCancelInterruptsRunningBashCommandFirst(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}

	r.target = filepath.Join(dir, "apple.txt")
	r.openProperties()
	if r.activePage == "" {
		t.Fatal("setup: expected an overlay to be open")
	}

	cmd := exec.Command("sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Skipf("sleep not available in this environment: %v", err)
	}
	defer cmd.Process.Kill() //nolint:errcheck
	r.bashRunningCmd = cmd

	r.RequestCancel()

	if r.activePage == "" {
		t.Error("RequestCancel should have interrupted the running command instead of closing the overlay")
	}
	if r.bashRunningCmd == nil {
		t.Error("bashRunningCmd should still be set — interruptBashCommand doesn't clear it itself, finishCapturedCommand does")
	}
}
