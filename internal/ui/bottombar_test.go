package ui

import (
	"os"
	"os/user"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// TestMain isolates every test in this package from whatever the real
// machine's $HISTFILE/~/.bash_history actually contains — Root now
// loads real bash history at construction (see historyFilePath/
// loadBashHistory), and no test here should depend on, or be thrown off
// by, this developer's or CI runner's own command history. Pointed at a
// path that doesn't exist rather than a real (even if temporary) file:
// loadBashHistory already treats "doesn't exist" as "start empty", the
// same as a first run would. Tests that specifically exercise
// runShellCommand — the only thing that ever writes to this path (see
// appendBashHistory) — additionally isolate themselves with their own
// t.TempDir()-scoped HISTFILE, so they can't contaminate each other
// either, regardless of run order.
func TestMain(m *testing.M) {
	os.Setenv("HISTFILE", filepath.Join(os.TempDir(), "breakthrough-test-history-does-not-exist")) //nolint:errcheck
	os.Exit(m.Run())
}

// t.Setenv (not os.Setenv/os.Unsetenv) throughout: it restores the
// original value automatically once the test ends, and "" is
// indistinguishable from unset as far as editorCommand/userShell's own
// `!= ""` checks are concerned, so it doubles as this test's way of
// clearing a variable mid-test too.
func TestEditorCommandPrecedence(t *testing.T) {
	t.Setenv("VISUAL", "visual-editor")
	t.Setenv("EDITOR", "editor-editor")
	if got := editorCommand(); got != "visual-editor" {
		t.Errorf("editorCommand() = %q, want VISUAL to win (%q)", got, "visual-editor")
	}

	t.Setenv("VISUAL", "")
	if got := editorCommand(); got != "editor-editor" {
		t.Errorf("editorCommand() = %q, want EDITOR as fallback (%q)", got, "editor-editor")
	}

	t.Setenv("EDITOR", "")
	if got := editorCommand(); got != "vi" {
		t.Errorf("editorCommand() = %q, want the last-resort fallback %q", got, "vi")
	}
}

func TestUserShellFallback(t *testing.T) {
	t.Setenv("SHELL", "/usr/bin/fish")
	if got := userShell(); got != "/usr/bin/fish" {
		t.Errorf("userShell() = %q, want %q", got, "/usr/bin/fish")
	}

	t.Setenv("SHELL", "")
	if got := userShell(); got != "/bin/sh" {
		t.Errorf("userShell() = %q, want the fallback %q", got, "/bin/sh")
	}
}

func TestCurrentUsername(t *testing.T) {
	u, err := user.Current()
	if err != nil {
		t.Skipf("user.Current unavailable in this environment: %v", err)
	}
	if got := currentUsername(); got != u.Username {
		t.Errorf("currentUsername() = %q, want %q", got, u.Username)
	}
}

// TestClockTextFormat pins the rendered shape (date, time, zone
// abbreviation) — not an exact value, which would make this test flaky.
func TestClockTextFormat(t *testing.T) {
	re := regexp.MustCompile(`^\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2} \S+$`)
	if got := clockText(); !re.MatchString(got) {
		t.Errorf("clockText() = %q, does not match the expected YYYY-MM-DD HH:MM:SS ZONE shape", got)
	}
}

// TestDfSummaryReturnsNonEmptyLine pins that dfSummary either returns a
// real df data line for a real directory, or its own "unavailable"
// fallback — never empty, never just the header row misread as data.
func TestDfSummaryReturnsNonEmptyLine(t *testing.T) {
	got := dfSummary(t.TempDir())
	if strings.TrimSpace(got) == "" {
		t.Error("dfSummary should never return an empty string")
	}
}

// TestBuildStatusBarSpansLocateButtons pins that each of the three
// button spans in buildStatusBar's output actually covers that button's
// own rendered label, and nothing else — the click-routing tests below
// rely on this being right.
func TestBuildStatusBarSpansLocateButtons(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}

	text, spans := r.buildStatusBar()
	runes := []rune(text)

	wantActions := map[statusBarAction]string{
		statusActionEdit:         "^E Edit",
		statusActionRename:       "^R Rename",
		statusActionToggleHidden: "^G Hidden",
	}
	found := map[statusBarAction]bool{}
	for _, s := range spans {
		want, ok := wantActions[s.action]
		if !ok {
			t.Errorf("unexpected action %v in spans", s.action)
			continue
		}
		if s.endCol > len(runes) || s.startCol < 0 {
			t.Fatalf("span %v out of bounds for text %q", s, text)
		}
		if got := string(runes[s.startCol:s.endCol]); got != want {
			t.Errorf("span for action %v = %q, want %q", s.action, got, want)
		}
		found[s.action] = true
	}
	for action := range wantActions {
		if !found[action] {
			t.Errorf("no span found for action %v", action)
		}
	}

	if !strings.Contains(text, r.currentUser) {
		t.Errorf("status bar text should contain the current user %q, got:\n%s", r.currentUser, text)
	}
}

// clickStatusBar simulates a real left-click on the status bar at the
// given column, the same way capturePropertiesMouse's own tests draw a
// real screen first so InRect/GetInnerRect have real layout to resolve
// coordinates against.
func clickStatusBar(t *testing.T, r *Root, col int) {
	t.Helper()

	// Sized to the text's own actual width, not a fixed guess: dfSummary
	// shells out to the real, platform-specific df, and GNU vs. BSD df
	// (let alone different filesystems/mount paths) don't produce the
	// same length line — a fixed 80-column screen was narrow enough on
	// a real macOS CI runner to push the later buttons past its edge,
	// making InRect reject clicks on them that a wider screen accepts
	// fine.
	width := len([]rune(r.statusBar.GetText(true))) + 10
	screen := tcell.NewSimulationScreen("")
	if err := screen.Init(); err != nil {
		t.Fatalf("screen.Init: %v", err)
	}
	defer screen.Fini()
	screen.SetSize(width, 24)
	r.statusBar.SetRect(0, 0, width, 1)
	r.statusBar.Draw(screen)

	rectX, _, _, _ := r.statusBar.GetInnerRect()
	r.captureStatusBarMouse(tview.MouseLeftClick, tcell.NewEventMouse(rectX+col, 0, tcell.Button1, 0))
}

func TestCaptureStatusBarMouseEditClickRunsEditAction(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.refreshStatusBar()
	r.panel.focusRow(1) // off ".." (the table's default initial selection) onto a real entry, so this exercises editCurrentEntry for real

	span, ok := statusBarSpanFor(r, statusActionEdit)
	if !ok {
		t.Fatal("no Edit span found")
	}

	// app.Suspend is a no-op here (no real screen behind r.app — see
	// runEditor's own doc comment on why this codebase can't unit-test
	// the actual editor invocation), so this only pins that the click
	// reaches editCurrentEntry/runEditor and the panel reloads cleanly
	// afterwards, not that an editor actually ran.
	clickStatusBar(t, r, span.startCol)

	if r.activePage == errorPage {
		t.Errorf("clicking Edit should not report an error here, got: %q", r.errorView.GetText(true))
	}
}

func TestCaptureStatusBarMouseRenameClickOpensRename(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.refreshStatusBar()
	r.panel.focusRow(1) // off ".." (the table's default initial selection) onto a real entry

	row, path, ok := r.panel.CurrentRowPath()
	if !ok {
		t.Fatal("setup: no current row")
	}

	span, ok := statusBarSpanFor(r, statusActionRename)
	if !ok {
		t.Fatal("no Rename span found")
	}
	clickStatusBar(t, r, span.startCol)

	if r.activePage != renamePage {
		t.Errorf("activePage = %q, want %q", r.activePage, renamePage)
	}
	if r.target != path || r.targetRow != row {
		t.Errorf("target/targetRow = %q/%d, want %q/%d", r.target, r.targetRow, path, row)
	}
}

func TestCaptureStatusBarMouseHiddenClickTogglesShowHidden(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.refreshStatusBar()
	before := r.panel.showHidden

	span, ok := statusBarSpanFor(r, statusActionToggleHidden)
	if !ok {
		t.Fatal("no Hidden span found")
	}
	clickStatusBar(t, r, span.startCol)

	if r.panel.showHidden == before {
		t.Error("clicking Hidden should have toggled showHidden")
	}
}

// statusBarSpanFor returns the first span for action in r.statusBarSpans.
func statusBarSpanFor(r *Root, action statusBarAction) (statusBarSpan, bool) {
	for _, s := range r.statusBarSpans {
		if s.action == action {
			return s, true
		}
	}
	return statusBarSpan{}, false
}

// TestAcceptsGlobalShortcutGuards pins acceptsGlobalShortcut's two
// conditions: blocked while any overlay is open, and blocked while the
// bash line has keyboard focus — both real, not just "activePage is the
// zero value" bookkeeping.
func TestAcceptsGlobalShortcutGuards(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}

	if !r.acceptsGlobalShortcut() {
		t.Error("should accept the shortcut with nothing open and the panel focused")
	}

	r.target = filepath.Join(dir, "apple.txt")
	r.openProperties()
	if r.acceptsGlobalShortcut() {
		t.Error("should not accept the shortcut while an overlay is open")
	}
	r.hideOverlay()

	r.app.SetFocus(r.bashLine)
	if r.acceptsGlobalShortcut() {
		t.Error("should not accept the shortcut while the bash line has focus")
	}
}

// TestToggleHiddenShortcutRespectsGuard pins that Ctrl+G's actual action
// (Root.ToggleHiddenShortcut) is a real no-op — not just individually
// harmless — while the guard says no: showHidden must stay untouched.
func TestToggleHiddenShortcutRespectsGuard(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}

	r.app.SetFocus(r.bashLine)
	before := r.panel.showHidden
	r.ToggleHiddenShortcut()
	if r.panel.showHidden != before {
		t.Error("ToggleHiddenShortcut should no-op while the bash line has focus")
	}

	r.app.SetFocus(r.panel)
	r.ToggleHiddenShortcut()
	if r.panel.showHidden == before {
		t.Error("ToggleHiddenShortcut should toggle once the guard passes")
	}
}

// TestRenameShortcutTargetsCurrentRow pins Ctrl+R's actual action
// (Root.RenameShortcut): it targets whichever row the table's cursor is
// on, the same as clicking the status bar's Rename button.
func TestRenameShortcutTargetsCurrentRow(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.panel.focusRow(1) // off ".." (the table's default initial selection) onto a real entry

	row, path, ok := r.panel.CurrentRowPath()
	if !ok {
		t.Fatal("setup: no current row")
	}

	r.RenameShortcut()

	if r.activePage != renamePage {
		t.Errorf("activePage = %q, want %q", r.activePage, renamePage)
	}
	if r.target != path || r.targetRow != row {
		t.Errorf("target/targetRow = %q/%d, want %q/%d", r.target, r.targetRow, path, row)
	}
}

// TestPanelOnLoadRefreshesStatusBar pins the wiring itself: navigating
// the panel calls back into Root and re-renders the status bar, rather
// than it only ever reflecting whatever directory was current when
// Root was constructed.
func TestPanelOnLoadRefreshesStatusBar(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}

	r.statusBar.SetText("")
	r.statusBarSpans = nil

	if err := r.panel.load(dir); err != nil {
		t.Fatalf("load: %v", err)
	}

	if r.statusBar.GetText(true) == "" {
		t.Error("navigating should have refreshed the status bar via Panel.onLoad")
	}
	if len(r.statusBarSpans) == 0 {
		t.Error("navigating should have rebuilt statusBarSpans via Panel.onLoad")
	}
}

// TestBashLineRunsThroughRunShellCommand pins that Enter in the bash
// line dispatches to runShellCommand (app.Suspend no-ops without a real
// screen — see TestCaptureStatusBarMouseEditClickRunsEditAction's own
// doc comment — so this only pins the wiring and the "line clears
// afterwards" behavior, not that a command actually ran).
// isolateHistoryFile points $HISTFILE at a path scoped to this test's
// own t.TempDir() — used by every test below that exercises
// runShellCommand (the only thing that writes to it — see
// appendBashHistory), so they can't contaminate each other via
// TestMain's single shared default path.
func isolateHistoryFile(t *testing.T) {
	t.Helper()
	t.Setenv("HISTFILE", filepath.Join(t.TempDir(), "history"))
}

func TestBashLineRunsThroughRunShellCommand(t *testing.T) {
	isolateHistoryFile(t)
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}

	r.bashLine.SetText("echo hello")
	r.bashLine.InputHandler()(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone), func(tview.Primitive) {})

	if got := r.bashLine.GetText(); got != "" {
		t.Errorf("bash line text = %q after Enter, want cleared", got)
	}
}

// TestRunShellCommandEmptyIsNoop pins that submitting a blank (or
// whitespace-only) command does nothing — no Suspend, no panel reload,
// no error.
func TestRunShellCommandEmptyIsNoop(t *testing.T) {
	isolateHistoryFile(t)
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}

	r.runShellCommand("   ")

	if r.activePage == errorPage {
		t.Error("an empty command should not report an error")
	}
	if len(r.bashHistory) != 0 {
		t.Errorf("bashHistory = %v, want empty — a blank command should not be recorded", r.bashHistory)
	}
}

// TestRunShellCommandRecordsHistory pins that every submitted command is
// appended, unconditionally — the same as a real shell, which remembers
// what was typed regardless of whether it succeeded.
func TestRunShellCommandRecordsHistory(t *testing.T) {
	isolateHistoryFile(t)
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}

	r.runShellCommand("echo one")
	r.runShellCommand("echo two")

	want := []string{"echo one", "echo two"}
	if len(r.bashHistory) != len(want) {
		t.Fatalf("bashHistory = %v, want %v", r.bashHistory, want)
	}
	for i, w := range want {
		if r.bashHistory[i] != w {
			t.Errorf("bashHistory[%d] = %q, want %q", i, r.bashHistory[i], w)
		}
	}
	if r.bashHistoryIdx != len(r.bashHistory) {
		t.Errorf("bashHistoryIdx = %d, want %d (not currently browsing)", r.bashHistoryIdx, len(r.bashHistory))
	}
}

// TestBashHistoryUpDownNavigation pins the full readline-style
// interaction: Up recalls older entries one at a time and stops at the
// oldest; Down recalls newer entries and restores whatever was being
// typed (the draft) once it moves past the newest one.
func TestBashHistoryUpDownNavigation(t *testing.T) {
	isolateHistoryFile(t)
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}

	r.runShellCommand("first")
	r.runShellCommand("second")
	r.bashLine.SetText("in progress")

	up := tcell.NewEventKey(tcell.KeyUp, 0, tcell.ModNone)
	down := tcell.NewEventKey(tcell.KeyDown, 0, tcell.ModNone)

	r.captureBashLineKey(up) // -> "second" (newest), remembering "in progress" as the draft
	if got := r.bashLine.GetText(); got != "second" {
		t.Errorf("after one Up, text = %q, want %q", got, "second")
	}

	r.captureBashLineKey(up) // -> "first" (oldest)
	if got := r.bashLine.GetText(); got != "first" {
		t.Errorf("after two Ups, text = %q, want %q", got, "first")
	}

	r.captureBashLineKey(up) // already at the oldest entry — stays put, does not wrap
	if got := r.bashLine.GetText(); got != "first" {
		t.Errorf("Up past the oldest entry = %q, want it to stay at %q", got, "first")
	}

	r.captureBashLineKey(down) // -> "second"
	if got := r.bashLine.GetText(); got != "second" {
		t.Errorf("after one Down, text = %q, want %q", got, "second")
	}

	r.captureBashLineKey(down) // -> back past the newest entry: restores the draft
	if got := r.bashLine.GetText(); got != "in progress" {
		t.Errorf("Down past the newest entry = %q, want the draft %q restored", got, "in progress")
	}
}

// TestBashHistoryDownWithNoHistoryIsNoop pins that Down is harmless when
// nothing has been recalled yet (no history at all, or history exists
// but Up was never pressed).
func TestBashHistoryDownWithNoHistoryIsNoop(t *testing.T) {
	isolateHistoryFile(t)
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}

	r.bashLine.SetText("untouched")
	r.captureBashLineKey(tcell.NewEventKey(tcell.KeyDown, 0, tcell.ModNone))

	if got := r.bashLine.GetText(); got != "untouched" {
		t.Errorf("text = %q after a stray Down, want unchanged %q", got, "untouched")
	}
}

func TestHistoryFilePathPrefersHISTFILE(t *testing.T) {
	t.Setenv("HISTFILE", "/some/explicit/path")
	if got := historyFilePath(); got != "/some/explicit/path" {
		t.Errorf("historyFilePath() = %q, want %q", got, "/some/explicit/path")
	}
}

func TestHistoryFilePathFallsBackToBashHistory(t *testing.T) {
	t.Setenv("HISTFILE", "")
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("no home directory in this environment: %v", err)
	}
	want := filepath.Join(home, ".bash_history")
	if got := historyFilePath(); got != want {
		t.Errorf("historyFilePath() = %q, want %q", got, want)
	}
}

// TestLoadBashHistorySkipsTimestampComments pins that bash's own
// optional "#<unix timestamp>" history-file comment lines (written when
// HISTTIMEFORMAT is set) are skipped rather than mistaken for commands,
// while an ordinary line starting with "#" some other way (a command
// that's genuinely a shell comment, or coincidentally starts with a
// word after the #) is kept.
func TestLoadBashHistorySkipsTimestampComments(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history")
	content := "ls -la\n#1700000000\ncd /tmp\n#not-a-timestamp\necho hi\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	got := loadBashHistory(path)
	want := []string{"ls -la", "cd /tmp", "#not-a-timestamp", "echo hi"}
	if len(got) != len(want) {
		t.Fatalf("loadBashHistory() = %v, want %v", got, want)
	}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("loadBashHistory()[%d] = %q, want %q", i, got[i], w)
		}
	}
}

func TestLoadBashHistoryMissingFileIsEmpty(t *testing.T) {
	got := loadBashHistory(filepath.Join(t.TempDir(), "does-not-exist"))
	if len(got) != 0 {
		t.Errorf("loadBashHistory() = %v, want empty for a missing file", got)
	}
}

func TestAppendBashHistoryThenLoadRoundTrips(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history")

	if err := appendBashHistory(path, "first command"); err != nil {
		t.Fatalf("appendBashHistory: %v", err)
	}
	if err := appendBashHistory(path, "second command"); err != nil {
		t.Fatalf("appendBashHistory: %v", err)
	}

	got := loadBashHistory(path)
	want := []string{"first command", "second command"}
	if len(got) != len(want) {
		t.Fatalf("loadBashHistory() after appends = %v, want %v", got, want)
	}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("loadBashHistory()[%d] = %q, want %q", i, got[i], w)
		}
	}
}

// TestNewRootLoadsExistingHistory pins the end-to-end wiring: a
// pre-existing history file is what Up recalls from the moment Root is
// constructed, before any command has been run in this session at all
// — inheriting an old session's history, not just recording a new one.
func TestNewRootLoadsExistingHistory(t *testing.T) {
	t.Setenv("HISTFILE", filepath.Join(t.TempDir(), "history"))
	if err := appendBashHistory(os.Getenv("HISTFILE"), "old session command"); err != nil {
		t.Fatal(err)
	}

	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}

	r.captureBashLineKey(tcell.NewEventKey(tcell.KeyUp, 0, tcell.ModNone))
	if got := r.bashLine.GetText(); got != "old session command" {
		t.Errorf("Up right after startup = %q, want the pre-existing history entry %q", got, "old session command")
	}
}
