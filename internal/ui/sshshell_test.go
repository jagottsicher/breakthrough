package ui

import (
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/jagottsicher/breakthrough/internal/activitylog"
	"github.com/jagottsicher/breakthrough/internal/remotefs"
)

// newTestRootForSSHShell builds a plain, local Root with "Add new ssh
// connection" already open — the identical shape newTestRootForConnect
// already establishes for the Connect dialog.
func newTestRootForSSHShell(t *testing.T) *Root {
	t.Helper()
	withTestConfigHome(t)
	dir := t.TempDir()
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.openSSHShellDialog(remotefs.Connection{})
	return r
}

// TestApplyThemeColorsTheSSHShellMenuAndDialog is a regression test for
// a real bug caught live: every one of this file's widgets was added
// to Root and wired up, but never added to applyTheme (theme.go) — so
// they all rendered in tview's own hardcoded default colors instead of
// the app's active theme, both the dropdown and the "Open shell to"
// dialog. Nothing paints a new widget's colors automatically just by
// existing; each one needs its own explicit line in applyTheme, the
// same as TestApplyColorSchemeRepaintsEveryKnownSurface already pins
// for several older widgets.
func TestApplyThemeColorsTheSSHShellMenuAndDialog(t *testing.T) {
	withTestConfigHome(t)
	dir := t.TempDir()
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}

	custom := solarizedTheme().Resolve()
	r.applyTheme(custom)

	if got := r.sshShellMenuTable.GetBackgroundColor(); got != custom.SurfaceBackground {
		t.Errorf("sshShellMenuTable background = %v, want %v", got, custom.SurfaceBackground)
	}
	if got := r.sshShellForm.GetBackgroundColor(); got != custom.SurfaceBackground {
		t.Errorf("sshShellForm background = %v, want %v", got, custom.SurfaceBackground)
	}
	if got := r.sshShellTitleBar.GetBackgroundColor(); got != custom.InputFocusedBackground {
		t.Errorf("sshShellTitleBar background = %v, want %v", got, custom.InputFocusedBackground)
	}
}

// TestApplyThemeGivesTheCheckboxesPlainColorsNotAHighlightedField pins
// a real bug caught live: a bare *tview.Checkbox gets repainted by
// Form.Draw's own generic pass in the same highlighted field-
// background color an InputField's editable area correctly gets,
// every single frame — plainFormCheckbox exists specifically to stop
// that (see its own doc comment, sshshell.go). Rendered to a real
// tcell.SimulationScreen and read back, not just checked against a
// setter having been called — the same "prove it on the actually
// visible style" standard TestDuplicateStrategyFieldUsesThemeColorsWhenFocused
// already holds itself to, since a plausible fix that silently didn't
// reach the real rendered cell is exactly the kind of bug this is
// guarding against.
func TestApplyThemeGivesTheCheckboxesPlainColorsNotAHighlightedField(t *testing.T) {
	withTestConfigHome(t)
	dir := t.TempDir()
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	screen := tcell.NewSimulationScreen("")
	if err := screen.Init(); err != nil {
		t.Fatal(err)
	}
	r.app.SetScreen(screen)
	screen.SetSize(100, 40)
	r.SetRect(0, 0, 100, 40)

	custom := solarizedTheme().Resolve()
	r.applyTheme(custom)
	r.openSSHShellDialog(remotefs.Connection{})
	r.sshShellAgentCheckbox.SetChecked(true)
	screen.Clear()
	r.Draw(screen)

	found := false
	w, h := screen.Size()
	for y := 0; y < h && !found; y++ {
		for x := 0; x < w; x++ {
			c, style, _ := screen.Get(x, y)
			if c != "●" {
				continue
			}
			found = true
			_, bg, _ := style.Decompose()
			if bg == custom.InputFocusedBackground {
				t.Errorf("checked dot background = %v, want anything but theme.InputFocusedBackground (the highlighted-field look)", bg)
			}
			if bg != custom.SurfaceBackground {
				t.Errorf("checked dot background = %v, want %v (the dialog's own plain background)", bg, custom.SurfaceBackground)
			}
		}
	}
	if !found {
		t.Fatal("never found the checked \"●\" glyph on screen")
	}
}

// TestSSHShellMenuTitleBarClosesOnCloseButtonClick/
// TestSSHShellDialogTitleBarClosesOnCloseButtonClick pin the user's
// own explicit request for a mouse-clickable close button on both of
// this feature's own windows — the identical shape
// TestHelpTitleBarClosesOnCloseButtonClick already establishes.
func TestSSHShellMenuTitleBarClosesOnCloseButtonClick(t *testing.T) {
	withTestConfigHome(t)
	dir := t.TempDir()
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.openSSHShellMenu()

	x, y, width, _ := r.sshShellMenuTitleBar.GetRect()
	closeX := x + toolWindowCloseButtonCol(0, width)
	captured, _ := r.captureSSHShellMenuTitleBarMouse(tview.MouseLeftClick, tcell.NewEventMouse(closeX, y, tcell.ButtonNone, 0))

	if captured != tview.MouseConsumed {
		t.Error("clicking the close button should consume the click")
	}
	if r.activePage != "" {
		t.Errorf("activePage after clicking the close button = %q, want closed (\"\")", r.activePage)
	}
}

func TestSSHShellDialogTitleBarClosesOnCloseButtonClick(t *testing.T) {
	r := newTestRootForSSHShell(t)

	x, y, width, _ := r.sshShellTitleBar.GetRect()
	closeX := x + toolWindowCloseButtonCol(0, width)
	captured, _ := r.captureSSHShellTitleBarMouse(tview.MouseLeftClick, tcell.NewEventMouse(closeX, y, tcell.ButtonNone, 0))

	if captured != tview.MouseConsumed {
		t.Error("clicking the close button should consume the click")
	}
	if r.activePage != "" {
		t.Errorf("activePage after clicking the close button = %q, want closed (\"\")", r.activePage)
	}
}

// TestSSHShellTitleBarsNameTheRightWindow pins the user's own explicit
// renaming request: the dropdown reads "Open shell to", the "+ New
// connection…" dialog reads "Add new ssh connection" — not the other
// way around, and not whatever either one used to say before.
func TestSSHShellTitleBarsNameTheRightWindow(t *testing.T) {
	withTestConfigHome(t)
	dir := t.TempDir()
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}

	r.openSSHShellMenu()
	if got := r.sshShellMenuTitleBar.GetText(true); !strings.Contains(got, "Open shell to") {
		t.Errorf("sshShellMenuTitleBar = %q, want it to mention %q", got, "Open shell to")
	}

	r.openSSHShellDialog(remotefs.Connection{})
	if got := r.sshShellTitleBar.GetText(true); !strings.Contains(got, "Add new ssh connection") {
		t.Errorf("sshShellTitleBar = %q, want it to mention %q", got, "Add new ssh connection")
	}
}

func TestRunSSHShellFromDialogRejectsAnEmptyHost(t *testing.T) {
	r := newTestRootForSSHShell(t)
	r.sshShellHostField.SetText("")

	r.runSSHShellFromDialog()

	if r.activePage != sshShellDialogPage {
		t.Fatalf("activePage = %q, want the dialog to stay open", r.activePage)
	}
	if !strings.Contains(r.sshShellStatus.GetText(true), "Host is required") {
		t.Errorf("status = %q, want it to mention the missing host", r.sshShellStatus.GetText(true))
	}
}

func TestRunSSHShellFromDialogRejectsAMalformedPort(t *testing.T) {
	r := newTestRootForSSHShell(t)
	r.sshShellHostField.SetText("example.com")
	r.sshShellPortField.SetText("not-a-number")

	r.runSSHShellFromDialog()

	if !strings.Contains(r.sshShellStatus.GetText(true), "Port must be a number") {
		t.Errorf("status = %q, want it to mention the malformed port", r.sshShellStatus.GetText(true))
	}
}

func TestRunSSHShellFromDialogRejectsAnOutOfRangePort(t *testing.T) {
	r := newTestRootForSSHShell(t)
	r.sshShellHostField.SetText("example.com")
	r.sshShellPortField.SetText("99999")

	r.runSSHShellFromDialog()

	if !strings.Contains(r.sshShellStatus.GetText(true), "Port must be a number") {
		t.Errorf("status = %q, want it to mention the out-of-range port", r.sshShellStatus.GetText(true))
	}
}

// TestRunSSHShellFromDialogRecordsTheFlagsFromTheCheckboxes pins the
// whole point of this dialog over the plain Connect one: the four
// checkboxes actually make it into the Connection that gets recorded —
// app.Suspend is a documented no-op without a real screen (see
// tview.Application.Suspend's own doc comment — the same acknowledged
// limitation TestRunBashCommandLogsAnAction already notes for
// runShellCommandFullScreen), so this never actually execs a real ssh
// process; it only has to prove the right Connection reaches
// RecordAttempt.
func TestRunSSHShellFromDialogRecordsTheFlagsFromTheCheckboxes(t *testing.T) {
	r := newTestRootForSSHShell(t)
	r.sshShellHostField.SetText("example.com")
	r.sshShellUserField.SetText("jens")
	r.sshShellCompressionCheckbox.SetChecked(true)
	r.sshShellVerboseCheckbox.SetChecked(true)

	r.runSSHShellFromDialog()

	entries, err := remotefs.LoadHistory()
	if err != nil {
		t.Fatalf("LoadHistory: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("len(entries) = %d, want 1", len(entries))
	}
	got := entries[0]
	if got.Host != "example.com" || got.User != "jens" {
		t.Errorf("entries[0] = %+v, want Host=example.com User=jens", got)
	}
	if !got.ShellCompression || !got.ShellVerbose {
		t.Errorf("entries[0] = %+v, want ShellCompression and ShellVerbose both true", got)
	}
	if got.ShellAgentForwarding || got.ShellX11Forwarding {
		t.Errorf("entries[0] = %+v, want the two untouched checkboxes to stay false", got)
	}
	if r.activePage == sshShellDialogPage {
		t.Error("activePage is still the dialog, want it closed after a successful submit")
	}
}

// TestLaunchSSHShellRecordsSuccessInHistoryAndLog pins launchSSHShell's
// own success path directly — the identical "Suspend never actually
// runs the child without a real screen" shape
// TestRunBashCommandLogsAnAction already relies on.
func TestLaunchSSHShellRecordsSuccessInHistoryAndLog(t *testing.T) {
	withTestConfigHome(t)
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	readLog := attachTestActivityLog(t, r)

	r.launchSSHShell(remotefs.Connection{Host: "example.com", User: "jens"})

	entries, err := remotefs.LoadHistory()
	if err != nil {
		t.Fatalf("LoadHistory: %v", err)
	}
	if len(entries) != 1 || entries[0].LastFailed {
		t.Errorf("entries = %+v, want one entry recorded as succeeded", entries)
	}

	got := readLog()
	if !strings.Contains(got, string(activitylog.CategoryRemote)) || !strings.Contains(got, "ssh shell to jens@example.com") {
		t.Errorf("log = %q, want an ssh shell entry about jens@example.com", got)
	}
}

func TestSSHShellArgvOmitsPortWhenItIsTheDefault(t *testing.T) {
	got := sshShellArgv(remotefs.Connection{Host: "example.com", User: "jens"})
	want := []string{"jens@example.com"}
	if !equalStrings(got, want) {
		t.Errorf("sshShellArgv = %v, want %v", got, want)
	}
}

func TestSSHShellArgvIncludesANonDefaultPort(t *testing.T) {
	got := sshShellArgv(remotefs.Connection{Host: "example.com", User: "jens", Port: 2222})
	want := []string{"-p", "2222", "jens@example.com"}
	if !equalStrings(got, want) {
		t.Errorf("sshShellArgv = %v, want %v", got, want)
	}
}

func TestSSHShellArgvIncludesEveryCheckedFlag(t *testing.T) {
	conn := remotefs.Connection{
		Host: "example.com", User: "jens",
		ShellAgentForwarding: true, ShellCompression: true, ShellVerbose: true, ShellX11Forwarding: true,
	}
	got := sshShellArgv(conn)
	want := []string{"-A", "-C", "-v", "-X", "jens@example.com"}
	if !equalStrings(got, want) {
		t.Errorf("sshShellArgv = %v, want %v", got, want)
	}
}

func TestSSHShellArgvFallsBackToTheCurrentUsernameWhenNoneIsSet(t *testing.T) {
	got := sshShellArgv(remotefs.Connection{Host: "example.com"})
	want := currentUsername() + "@example.com"
	if len(got) != 1 || got[0] != want {
		t.Errorf("sshShellArgv = %v, want a single arg %q", got, want)
	}
}

func TestShellFlagsSuffixIsEmptyWithNoFlagsSet(t *testing.T) {
	if got := shellFlagsSuffix(remotefs.Connection{Host: "example.com"}); got != "" {
		t.Errorf("shellFlagsSuffix = %q, want empty", got)
	}
}

func TestShellFlagsSuffixListsEveryFlagThatIsSet(t *testing.T) {
	conn := remotefs.Connection{ShellAgentForwarding: true, ShellX11Forwarding: true}
	got := shellFlagsSuffix(conn)
	want := "  [A X]"
	if got != want {
		t.Errorf("shellFlagsSuffix = %q, want %q", got, want)
	}
}

// TestRenderSSHShellMenuDistinguishesFlagVariantsByLabelSuffix pins
// the whole reason shellFlagsSuffix exists: two history entries that
// share a Host/User but differ in their shell flags (see
// remotefs.Connection's own doc comment) must not look identical in
// this dropdown.
func TestRenderSSHShellMenuDistinguishesFlagVariantsByLabelSuffix(t *testing.T) {
	withTestConfigHome(t)
	plain := remotefs.Connection{Host: "example.com", User: "jens"}
	withAgent := remotefs.Connection{Host: "example.com", User: "jens", ShellAgentForwarding: true}
	if err := remotefs.RecordAttempt(plain, false); err != nil {
		t.Fatalf("RecordAttempt: %v", err)
	}
	if err := remotefs.RecordAttempt(withAgent, false); err != nil {
		t.Fatalf("RecordAttempt: %v", err)
	}

	dir := t.TempDir()
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.renderSSHShellMenu()

	var labels []string
	for i := 0; i < r.sshShellMenuTable.GetRowCount(); i++ {
		if cell := r.sshShellMenuTable.GetCell(i, sshShellMenuColLabel); cell != nil {
			labels = append(labels, cell.Text)
		}
	}
	if len(labels) != 3 { // withAgent, plain, "+ New connection"
		t.Fatalf("labels = %v, want 2 history rows plus New connection", labels)
	}
	if !strings.Contains(labels[0], "[A]") {
		t.Errorf("labels[0] = %q, want the agent-forwarding variant's own suffix", labels[0])
	}
	if strings.Contains(labels[1], "[") {
		t.Errorf("labels[1] = %q, want the plain variant to carry no suffix at all", labels[1])
	}
}

// TestActivateSSHShellMenuCellOnNewConnectionRowOpensDialog pins the
// trailing "+ New connection" row's own behavior — the identical shape
// activateConnectionMenuCell's own equivalent case already has.
func TestActivateSSHShellMenuCellOnNewConnectionRowOpensDialog(t *testing.T) {
	withTestConfigHome(t)
	dir := t.TempDir()
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.openSSHShellMenu()

	r.activateSSHShellMenuCell(0, sshShellMenuColLabel)

	if r.activePage != sshShellDialogPage {
		t.Errorf("activePage = %q, want the \"Open shell to\" dialog", r.activePage)
	}
}

// TestRemoveSSHShellHistoryRowDropsEntry pins "x" dropping a row out
// of the shared history — the identical shape
// TestPressingXOnAHistoryRowRemovesItFromHistory already establishes
// for the "@"/"gc" dropdown.
func TestRemoveSSHShellHistoryRowDropsEntry(t *testing.T) {
	withTestConfigHome(t)
	conn := remotefs.Connection{Host: "example.com", User: "jens"}
	if err := remotefs.RecordAttempt(conn, false); err != nil {
		t.Fatalf("RecordAttempt: %v", err)
	}

	dir := t.TempDir()
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.openSSHShellMenu()

	if !r.removeSSHShellHistoryRow(0) {
		t.Fatal("removeSSHShellHistoryRow(0) = false, want true")
	}

	entries, err := remotefs.LoadHistory()
	if err != nil {
		t.Fatalf("LoadHistory: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("entries = %+v, want the one entry removed", entries)
	}
}

// TestGSChordOpensSSHShellMenu pins the "gs" chord binding itself (see
// keymap.go's own "g" family) — a dedicated letter, not a stand-in for
// any of gc/gm/ge, and wired to the right destination.
func TestGSChordOpensSSHShellMenu(t *testing.T) {
	family, ok := chordFamilyFor('g')
	if !ok {
		t.Fatal("no \"g\" chord family")
	}
	var action func(r *Root)
	for _, m := range family.members {
		if m.key == 's' {
			action = m.action
		}
	}
	if action == nil {
		t.Fatal("\"g\" family has no \"s\" member")
	}

	withTestConfigHome(t)
	dir := t.TempDir()
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}

	action(r)

	if r.activePage != sshShellMenuPage {
		t.Errorf("activePage = %q, want %q", r.activePage, sshShellMenuPage)
	}
}
