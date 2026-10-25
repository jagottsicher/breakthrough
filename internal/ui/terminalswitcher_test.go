package ui

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/jagottsicher/breakthrough/internal/remotefs"
)

// terminalSwitcherLabel is one switcher row's own label text — the
// identical shape tabSwitcherLabel already establishes.
func terminalSwitcherLabel(r *Root, row int) string {
	cell := r.terminalSwitcher.GetCell(row, terminalSwitcherColLabel)
	if cell == nil {
		return ""
	}
	return cell.Text
}

func TestOpenTerminalSwitcherShortcutOpensOnTheActiveTerminal(t *testing.T) {
	r := newTestRootForShells(t)
	stubShellSession(t, func() *exec.Cmd { return exec.Command("sh", "-c", "sleep 5") })

	r.openNewShellEmbedded(testShellConn("one"))
	r.openNewShellEmbedded(testShellConn("two"))
	defer func() {
		for _, tab := range r.shells {
			_ = tab.session.Close()
		}
	}()

	r.OpenTerminalSwitcherShortcut()

	if r.activePage != terminalSwitcherPage {
		t.Fatalf("activePage = %q, want %q", r.activePage, terminalSwitcherPage)
	}
	row, _ := r.terminalSwitcher.GetSelection()
	if row != 1 {
		t.Errorf("switcher preselected row %d, want 1 (the active terminal)", row)
	}
}

func TestTerminalSwitcherListsEveryTerminalPlusNewAndFilesRows(t *testing.T) {
	r := newTestRootForShells(t)
	stubShellSession(t, func() *exec.Cmd { return exec.Command("sh", "-c", "sleep 5") })

	r.openNewShellEmbedded(testShellConn("alpha"))
	r.openNewShellEmbedded(testShellConn("beta"))
	defer func() {
		for _, tab := range r.shells {
			_ = tab.session.Close()
		}
	}()

	r.openTerminalSwitcher(r.activeShell)

	// Two terminals, "+  New terminal", and "Go to file view" (a
	// terminal is on screen) — four rows.
	if got, want := r.terminalSwitcher.GetRowCount(), 4; got != want {
		t.Fatalf("switcher has %d rows, want %d", got, want)
	}
	if got := terminalSwitcherLabel(r, 0); !strings.Contains(got, "nobody@alpha") {
		t.Errorf("row 0 = %q, want it to contain the connection label", got)
	}
	if got := terminalSwitcherLabel(r, 2); got != terminalSwitcherNewRowLabel {
		t.Errorf("row 2 = %q, want %q", got, terminalSwitcherNewRowLabel)
	}
	if got := terminalSwitcherLabel(r, 3); got != terminalSwitcherFilesRowLabel {
		t.Errorf("row 3 = %q, want %q", got, terminalSwitcherFilesRowLabel)
	}
}

func TestTerminalSwitcherOmitsFilesRowWhenNotViewingATerminal(t *testing.T) {
	r := newTestRootForShells(t)
	stubShellSession(t, func() *exec.Cmd { return exec.Command("sh", "-c", "sleep 5") })

	r.openNewShellEmbedded(testShellConn("alpha"))
	defer func() { _ = r.shells[0].session.Close() }()
	r.viewFiles()

	r.openTerminalSwitcher(r.activeShell)

	if got, want := r.terminalSwitcher.GetRowCount(), 2; got != want { // terminal + "New terminal", no "Go to file view"
		t.Fatalf("switcher has %d rows, want %d", got, want)
	}
	if r.terminalSwitcherFilesRow != -1 {
		t.Errorf("terminalSwitcherFilesRow = %d, want -1", r.terminalSwitcherFilesRow)
	}
}

// The tests below pin the same red/green/white split
// TestConnectionHistoryColorForEveryStateCombination already proves
// for connectionHistoryColor itself (connectionmenu_test.go) — here
// end to end, for both an open-Terminal row and a history-only row,
// per the user's own explicit request that the Terminal switcher
// color its rows exactly the way the "@" connections menu already
// does.

func TestTerminalSwitcherColorsTheCurrentTerminalGreenRegardlessOfHistory(t *testing.T) {
	r := newTestRootForShells(t)
	stubShellSession(t, func() *exec.Cmd { return exec.Command("sh", "-c", "sleep 5") })

	conn := testShellConn("alpha")
	r.openNewShellEmbedded(conn)
	defer func() { _ = r.shells[0].session.Close() }()
	if err := remotefs.RecordAttempt(conn, true); err != nil {
		t.Fatalf("RecordAttempt: %v", err)
	}

	r.openTerminalSwitcher(0)

	got := cellTextColor(r.terminalSwitcher.GetCell(0, terminalSwitcherColLabel))
	if got != r.theme.EntryExecutable {
		t.Errorf("current terminal's row color = %v, want %v (theme.EntryExecutable) even with a failed history entry", got, r.theme.EntryExecutable)
	}
}

// TestTerminalSwitcherColorsALiveOpenTerminalGreenEvenIfHistoryIsUntested
// pins the user's own explicit, real-world report: a live, not-yet-
// exited Terminal is demonstrably connected right now, regardless of
// what the shared history says — it only learns a real outcome once
// the session actually ends (watchShellExit's own RecordAttempt call),
// so a Terminal opened moments ago and never recorded as a success
// yet still reads as connected, not "never tried."
func TestTerminalSwitcherColorsALiveOpenTerminalGreenEvenIfHistoryIsUntested(t *testing.T) {
	r := newTestRootForShells(t)
	stubShellSession(t, func() *exec.Cmd { return exec.Command("sh", "-c", "sleep 5") })

	r.openNewShellEmbedded(testShellConn("alpha"))
	r.openNewShellEmbedded(testShellConn("beta")) // not current: alpha is
	defer func() {
		for _, tab := range r.shells {
			_ = tab.session.Close()
		}
	}()

	r.openTerminalSwitcher(1)

	got := cellTextColor(r.terminalSwitcher.GetCell(0, terminalSwitcherColLabel))
	want := blendToward(r.theme.EntryExecutable, r.theme.MutedTextColor, connectionHistorySuccessBlend)
	if got != want {
		t.Errorf("live, non-current terminal's row color = %v, want the muted-green blend %v", got, want)
	}
}

func TestTerminalSwitcherColorsAPreviouslySuccessfulNonCurrentTerminalMutedGreen(t *testing.T) {
	r := newTestRootForShells(t)
	stubShellSession(t, func() *exec.Cmd { return exec.Command("sh", "-c", "sleep 5") })

	alpha := testShellConn("alpha")
	r.openNewShellEmbedded(alpha)
	r.openNewShellEmbedded(testShellConn("beta")) // not current: alpha is
	defer func() {
		for _, tab := range r.shells {
			_ = tab.session.Close()
		}
	}()
	if err := remotefs.RecordAttempt(alpha, false); err != nil {
		t.Fatalf("RecordAttempt: %v", err)
	}

	r.openTerminalSwitcher(1)

	got := cellTextColor(r.terminalSwitcher.GetCell(0, terminalSwitcherColLabel))
	want := blendToward(r.theme.EntryExecutable, r.theme.MutedTextColor, connectionHistorySuccessBlend)
	if got != want {
		t.Errorf("previously-successful, non-current terminal's row color = %v, want the muted-green blend %v", got, want)
	}
}

// TestTerminalSwitcherColorsAnExitedFailedTerminalRed covers the
// realistic failed-connection case: a live tab can't carry a real
// LastFailed outcome in the shared history at all (watchShellExit
// only records one once the session actually ends — see
// renderTerminalSwitcherOpenRow's own doc comment), so this reproduces
// that callback's own body directly, the same convention
// TestExitedTerminalStaysInTheListAndOnScreen (shells_test.go) already
// establishes, rather than asserting red for a still-live tab (which
// would mean trusting a stale history record over what's actually
// happening right now — exactly the bug the user reported, just the
// opposite color).
func TestTerminalSwitcherColorsAnExitedFailedTerminalRed(t *testing.T) {
	r := newTestRootForShells(t)
	stubShellSession(t, func() *exec.Cmd { return exec.Command("sh", "-c", "sleep 5") })

	alpha := testShellConn("alpha")
	r.openNewShellEmbedded(alpha)
	r.openNewShellEmbedded(testShellConn("beta")) // not current: alpha is
	defer func() {
		for _, tab := range r.shells {
			_ = tab.session.Close()
		}
	}()

	tab := r.shells[0]
	tab.exited = true
	r.refreshShellHeader(tab)
	if err := remotefs.RecordAttempt(alpha, true); err != nil {
		t.Fatalf("RecordAttempt: %v", err)
	}

	r.openTerminalSwitcher(1)

	got := cellTextColor(r.terminalSwitcher.GetCell(0, terminalSwitcherColLabel))
	if got != r.theme.CriticalText {
		t.Errorf("exited, failed terminal's row color = %v, want theme.CriticalText %v", got, r.theme.CriticalText)
	}
}

func TestTerminalSwitcherColorsAFailedHistoryRowRed(t *testing.T) {
	r := newTestRootForShells(t)
	conn := remotefs.Connection{Host: "unreachable.example.com", User: "nobody"}
	// RecordAttempt's own "never tried, never added" rule (history.go)
	// drops a brand-new connection's very first failed attempt outright
	// rather than cluttering history with it — a real entry has to
	// exist first, the same way a real "this host used to work, now it
	// doesn't" ever actually happens.
	if err := remotefs.RecordAttempt(conn, false); err != nil {
		t.Fatalf("RecordAttempt (seed): %v", err)
	}
	if err := remotefs.RecordAttempt(conn, true); err != nil {
		t.Fatalf("RecordAttempt: %v", err)
	}

	r.openTerminalSwitcher(-1)

	got := cellTextColor(r.terminalSwitcher.GetCell(0, terminalSwitcherColLabel))
	if got != r.theme.CriticalText {
		t.Errorf("failed history row color = %v, want theme.CriticalText %v", got, r.theme.CriticalText)
	}
}

func TestTerminalSwitcherColorsASuccessfulHistoryRowMutedGreen(t *testing.T) {
	r := newTestRootForShells(t)
	conn := remotefs.Connection{Host: "reachable.example.com", User: "nobody"}
	if err := remotefs.RecordAttempt(conn, false); err != nil {
		t.Fatalf("RecordAttempt: %v", err)
	}

	r.openTerminalSwitcher(-1)

	got := cellTextColor(r.terminalSwitcher.GetCell(0, terminalSwitcherColLabel))
	want := blendToward(r.theme.EntryExecutable, r.theme.MutedTextColor, connectionHistorySuccessBlend)
	if got != want {
		t.Errorf("successful history row color = %v, want the muted-green blend %v", got, want)
	}
}

func TestTerminalSwitcherMarksTheCurrentTerminal(t *testing.T) {
	r := newTestRootForShells(t)
	stubShellSession(t, func() *exec.Cmd { return exec.Command("sh", "-c", "sleep 5") })

	r.openNewShellEmbedded(testShellConn("alpha"))
	r.openNewShellEmbedded(testShellConn("beta"))
	defer func() {
		for _, tab := range r.shells {
			_ = tab.session.Close()
		}
	}()

	r.openTerminalSwitcher(0)

	current := terminalSwitcherLabel(r, 1)
	if !strings.Contains(current, "(current)") {
		t.Errorf("the active terminal's row = %q, want it marked as current", current)
	}
	if !strings.Contains(current, colorTag(r.theme.PlaceholderText)) {
		t.Errorf("the active terminal's row = %q, want it dimmed", current)
	}
	other := terminalSwitcherLabel(r, 0)
	if strings.Contains(other, "(current)") {
		t.Errorf("row 0 = %q, want only the active terminal marked", other)
	}
}

// TestTerminalSwitcherShowsExitedNotCurrentForTheActiveExitedTerminal
// pins the user's own explicit request: once the active Terminal has
// exited, that's the state worth showing — "(current)" on top of it
// read as if a dead Terminal were still the one actually in use.
func TestTerminalSwitcherShowsExitedNotCurrentForTheActiveExitedTerminal(t *testing.T) {
	r := newTestRootForShells(t)
	stubShellSession(t, func() *exec.Cmd { return exec.Command("sh", "-c", "sleep 5") })

	r.openNewShellEmbedded(testShellConn("alpha"))
	tab := r.shells[0]
	defer func() { _ = tab.session.Close() }()

	tab.exited = true
	r.refreshShellHeader(tab)

	r.openTerminalSwitcher(0)

	row := terminalSwitcherLabel(r, 0)
	if !strings.Contains(row, "(exited)") {
		t.Errorf("row 0 = %q, want it marked exited", row)
	}
	if strings.Contains(row, "(current)") {
		t.Errorf("row 0 = %q, want no (current) marker on an exited terminal", row)
	}
}

func TestTerminalSwitcherEnterSwitchesAndCloses(t *testing.T) {
	r := newTestRootForShells(t)
	stubShellSession(t, func() *exec.Cmd { return exec.Command("sh", "-c", "sleep 5") })

	r.openNewShellEmbedded(testShellConn("alpha"))
	r.openNewShellEmbedded(testShellConn("beta"))
	defer func() {
		for _, tab := range r.shells {
			_ = tab.session.Close()
		}
	}()

	r.openTerminalSwitcher(0)
	r.terminalSwitcher.InputHandler()(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone), func(tview.Primitive) {})

	if r.activeShell != 0 {
		t.Errorf("activeShell = %d, want 0", r.activeShell)
	}
	if r.activePage != "" {
		t.Errorf("activePage = %q, want the switcher closed", r.activePage)
	}
}

func TestTerminalSwitcherEscapeLeavesTheActiveTerminalAlone(t *testing.T) {
	r := newTestRootForShells(t)
	stubShellSession(t, func() *exec.Cmd { return exec.Command("sh", "-c", "sleep 5") })

	r.openNewShellEmbedded(testShellConn("alpha"))
	r.openNewShellEmbedded(testShellConn("beta"))
	defer func() {
		for _, tab := range r.shells {
			_ = tab.session.Close()
		}
	}()

	r.openTerminalSwitcher(0) // preselecting a *different* terminal than the active one

	r.closeTerminalSwitcher()

	if r.activeShell != 1 {
		t.Errorf("activeShell = %d, want it left at 1", r.activeShell)
	}
	if r.activePage != "" {
		t.Errorf("activePage = %q, want the switcher closed", r.activePage)
	}
}

func TestTerminalSwitcherNewTerminalRowOpensTheReusedDialog(t *testing.T) {
	r := newTestRootForShells(t)
	stubShellSession(t, func() *exec.Cmd { return exec.Command("sh", "-c", "sleep 5") })

	r.openNewShellEmbedded(testShellConn("alpha"))
	defer func() {
		for _, tab := range r.shells {
			_ = tab.session.Close()
		}
	}()

	r.openTerminalSwitcher(0)
	r.terminalSwitcher.Select(1, terminalSwitcherColLabel) // the "+ New terminal" row
	r.terminalSwitcher.InputHandler()(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone), func(tview.Primitive) {})

	if r.activePage != sshShellDialogPage {
		t.Errorf("activePage = %q, want %q", r.activePage, sshShellDialogPage)
	}
	if r.sshShellSubmit == nil {
		t.Error("sshShellSubmit wasn't set — the dialog would fall back to the default ssh launcher")
	}

	r.sshShellHostField.SetText("newhost")
	r.runSSHShellFromDialog()

	if len(r.shells) != 2 {
		t.Fatalf("len(r.shells) = %d after submitting the dialog, want 2", len(r.shells))
	}
}

func TestTerminalSwitcherFilesRowLeavesTerminalView(t *testing.T) {
	r := newTestRootForShells(t)
	stubShellSession(t, func() *exec.Cmd { return exec.Command("sh", "-c", "sleep 5") })

	r.openNewShellEmbedded(testShellConn("alpha"))
	defer func() { _ = r.shells[0].session.Close() }()

	r.openTerminalSwitcher(0)
	r.activateTerminalSwitcherCell(r.terminalSwitcherFilesRow, terminalSwitcherColLabel)

	if r.viewingShells {
		t.Error("viewingShells still true after activating the Files row")
	}
	if r.activePage != "" {
		t.Errorf("activePage = %q, want closed (\"\")", r.activePage)
	}
}

func TestTerminalSwitcherDeleteClosesTheHighlightedTerminalAndStaysOpen(t *testing.T) {
	r := newTestRootForShells(t)
	stubShellSession(t, func() *exec.Cmd { return exec.Command("sh", "-c", "sleep 5") })

	r.openNewShellEmbedded(testShellConn("alpha"))
	r.openNewShellEmbedded(testShellConn("beta"))
	defer func() {
		for _, tab := range r.shells {
			_ = tab.session.Close()
		}
	}()
	r.openTerminalSwitcher(1)

	r.captureTerminalSwitcherKey(tcell.NewEventKey(tcell.KeyDelete, 0, tcell.ModNone))

	if len(r.shells) != 1 {
		t.Errorf("len(r.shells) = %d, want 1", len(r.shells))
	}
	if r.activePage != terminalSwitcherPage {
		t.Errorf("activePage = %q, want the switcher still open", r.activePage)
	}
}

// TestTerminalSwitcherCanCloseDownToZero pins the one real difference
// from the tab switcher's own Delete handling: a tab always leaves one
// behind, a Terminal doesn't have to.
func TestTerminalSwitcherCanCloseDownToZero(t *testing.T) {
	r := newTestRootForShells(t)
	stubShellSession(t, func() *exec.Cmd { return exec.Command("sh", "-c", "sleep 5") })

	r.openNewShellEmbedded(testShellConn("alpha"))
	r.openTerminalSwitcher(0)

	r.captureTerminalSwitcherKey(tcell.NewEventKey(tcell.KeyDelete, 0, tcell.ModNone))

	if len(r.shells) != 0 {
		t.Errorf("len(r.shells) = %d, want 0", len(r.shells))
	}
}

// TestTerminalSwitcherBareDigitJumpsAndCloses pins the one feature the
// tab switcher doesn't have: an unmodified digit jumps straight to that
// numbered Terminal, carried over unchanged from the first, centered-
// dropdown version of this overlay.
func TestTerminalSwitcherBareDigitJumpsAndCloses(t *testing.T) {
	r := newTestRootForShells(t)
	stubShellSession(t, func() *exec.Cmd { return exec.Command("sh", "-c", "sleep 5") })

	r.openNewShellEmbedded(testShellConn("alpha"))
	r.openNewShellEmbedded(testShellConn("beta"))
	defer func() {
		for _, tab := range r.shells {
			_ = tab.session.Close()
		}
	}()

	r.openTerminalSwitcher(1)
	r.captureTerminalSwitcherKey(tcell.NewEventKey(tcell.KeyRune, '1', tcell.ModNone))

	if r.activeShell != 0 {
		t.Errorf("activeShell after pressing '1' = %d, want 0", r.activeShell)
	}
	if r.activePage != "" {
		t.Errorf("activePage = %q, want closed (\"\")", r.activePage)
	}
}

func TestTerminalSwitcherDigitWithNoMatchingTerminalIsIgnored(t *testing.T) {
	r := newTestRootForShells(t)
	stubShellSession(t, func() *exec.Cmd { return exec.Command("sh", "-c", "sleep 5") })

	r.openNewShellEmbedded(testShellConn("alpha"))
	defer func() { _ = r.shells[0].session.Close() }()
	r.openTerminalSwitcher(0)

	returned := r.captureTerminalSwitcherKey(tcell.NewEventKey(tcell.KeyRune, '9', tcell.ModNone))

	if returned == nil {
		t.Error("an unassigned digit should not be consumed")
	}
	if r.activePage == "" {
		t.Error("the switcher shouldn't have closed for an unassigned digit")
	}
}

// TestTerminalSwitcherEscapeWhileViewingATerminalRestoresItsOwnFocus
// pins the one real bug caught before this ever shipped (see
// setTerminalSwitcherEmptyStackFocus's own doc comment): closing the
// switcher while a Terminal is on screen must not leave the keyboard
// focused on r.panel, which isn't even mounted in panelHost then.
func TestTerminalSwitcherEscapeWhileViewingATerminalRestoresItsOwnFocus(t *testing.T) {
	r := newTestRootForShells(t)
	stubShellSession(t, func() *exec.Cmd { return exec.Command("sh", "-c", "sleep 5") })

	r.openNewShellEmbedded(testShellConn("alpha"))
	defer func() { _ = r.shells[0].session.Close() }()

	r.openTerminalSwitcher(0)
	r.captureTerminalSwitcherKey(tcell.NewEventKey(tcell.KeyEscape, 0, tcell.ModNone))

	if r.app.GetFocus() != r.shells[0].view {
		t.Error("focus after Escape isn't the terminal's own view")
	}
}

func TestExitTerminalViewShortcutClosesTheSwitcherAndReturnsToFiles(t *testing.T) {
	r := newTestRootForShells(t)
	stubShellSession(t, func() *exec.Cmd { return exec.Command("sh", "-c", "sleep 5") })

	r.openNewShellEmbedded(testShellConn("alpha"))
	defer func() { _ = r.shells[0].session.Close() }()
	r.openTerminalSwitcher(0)

	if !r.ExitTerminalViewShortcut() {
		t.Fatal("ExitTerminalViewShortcut returned false while viewing a terminal")
	}

	if r.viewingShells {
		t.Error("viewingShells still true after ExitTerminalViewShortcut")
	}
	if r.activePage != "" {
		t.Errorf("activePage = %q, want everything closed", r.activePage)
	}
}

func TestExitTerminalViewShortcutIsANoOpWhileBrowsingFiles(t *testing.T) {
	r := newTestRootForShells(t)

	if r.ExitTerminalViewShortcut() {
		t.Error("ExitTerminalViewShortcut returned true while not viewing a terminal")
	}
}

// The tests below pin the switcher's own merged row list — open
// Terminals from r.shells plus history-only connections the shared,
// persisted history remembers but has no open Terminal for right now
// (see terminalswitcher.go's own package doc comment for the whole
// design and what each row kind's three columns do).

func TestTerminalSwitcherListsAHistoryOnlyRowAfterClosingATerminal(t *testing.T) {
	r := newTestRootForShells(t)
	stubShellSession(t, func() *exec.Cmd { return exec.Command("sh", "-c", "sleep 5") })

	conn := testShellConn("closedone")
	r.openNewShellEmbedded(conn)
	tab := r.shells[0]
	_ = tab.session.Close()
	r.removeShell(tab) // the real path closeShell would take, reproduced directly

	r.openTerminalSwitcher(0)

	// The history-only row, then "+ New terminal" — no "Go to file
	// view" row: nothing is being viewed any more once the only
	// Terminal closed.
	if got, want := r.terminalSwitcher.GetRowCount(), 2; got != want {
		t.Errorf("row count = %d, want %d", got, want)
	}
	if _, ok := r.terminalSwitcherHistRows[0]; !ok {
		t.Fatalf("row 0 isn't a history-only row: openRows=%v histRows=%v", r.terminalSwitcherOpenRows, r.terminalSwitcherHistRows)
	}
	label := terminalSwitcherLabel(r, 0)
	if !strings.Contains(label, conn.Label()) {
		t.Errorf("row 0 = %q, want it to contain the closed connection's own label", label)
	}
}

func TestTerminalSwitcherDoesNotDuplicateAnOpenTerminalAsHistory(t *testing.T) {
	r := newTestRootForShells(t)
	stubShellSession(t, func() *exec.Cmd { return exec.Command("sh", "-c", "sleep 5") })

	r.openNewShellEmbedded(testShellConn("stillopen"))
	defer func() { _ = r.shells[0].session.Close() }()

	r.openTerminalSwitcher(0)

	if len(r.terminalSwitcherHistRows) != 0 {
		t.Errorf("terminalSwitcherHistRows = %v, want empty — the only history entry is already open", r.terminalSwitcherHistRows)
	}
	if _, ok := r.terminalSwitcherOpenRows[0]; !ok {
		t.Error("row 0 isn't recognized as the open Terminal")
	}
}

func TestTerminalSwitcherLabelClickOnAHistoryRowConnectsDirectly(t *testing.T) {
	r := newTestRootForShells(t)
	stubShellSession(t, func() *exec.Cmd { return exec.Command("sh", "-c", "sleep 5") })

	conn := testShellConn("reconnectme")
	r.openNewShellEmbedded(conn)
	tab := r.shells[0]
	_ = tab.session.Close()
	r.removeShell(tab)

	r.openTerminalSwitcher(0)
	r.activateTerminalSwitcherCell(0, terminalSwitcherColLabel)

	if len(r.shells) != 1 {
		t.Fatalf("len(r.shells) = %d after selecting a history-only row, want 1 (reconnected, no dialog)", len(r.shells))
	}
	defer func() { _ = r.shells[0].session.Close() }()
	if !r.shells[0].conn.Equal(conn) {
		t.Errorf("the reopened terminal's own conn = %+v, want %+v", r.shells[0].conn, conn)
	}
	if r.activePage != "" {
		t.Errorf("activePage = %q, want closed (\"\") — no dialog for a history-only reconnect", r.activePage)
	}
}

func TestTerminalSwitcherEditActionOnAHistoryRowOpensThePrefilledDialog(t *testing.T) {
	r := newTestRootForShells(t)
	stubShellSession(t, func() *exec.Cmd { return exec.Command("sh", "-c", "sleep 5") })

	conn := testShellConn("editme")
	r.openNewShellEmbedded(conn)
	tab := r.shells[0]
	_ = tab.session.Close()
	r.removeShell(tab)

	r.openTerminalSwitcher(0)
	r.activateTerminalSwitcherCell(0, terminalSwitcherColAction)

	if r.activePage != sshShellDialogPage {
		t.Errorf("activePage = %q, want %q", r.activePage, sshShellDialogPage)
	}
	if got := r.sshShellHostField.GetText(); got != conn.Host {
		t.Errorf("Host field = %q, want %q (prefilled)", got, conn.Host)
	}
	if r.sshShellSubmit == nil {
		t.Error("sshShellSubmit wasn't set — submitting would fall back to the default ssh launcher")
	}
}

func TestTerminalSwitcherCloseActionOnAnOpenRowDisconnectsButKeepsHistory(t *testing.T) {
	r := newTestRootForShells(t)
	stubShellSession(t, func() *exec.Cmd { return exec.Command("sh", "-c", "sleep 5") })

	conn := testShellConn("disconnectme")
	r.openNewShellEmbedded(conn)

	r.openTerminalSwitcher(0)
	r.activateTerminalSwitcherCell(0, terminalSwitcherColAction)

	if len(r.shells) != 0 {
		t.Errorf("len(r.shells) = %d after the Action column's own disconnect, want 0", len(r.shells))
	}
	history, err := remotefs.LoadHistory()
	if err != nil {
		t.Fatalf("LoadHistory: %v", err)
	}
	if len(history) != 1 || !history[0].Equal(conn) {
		t.Errorf("history after disconnect = %+v, want the connection still remembered", history)
	}
}

func TestTerminalSwitcherRemoveActionOnAHistoryRowDeletesItOutright(t *testing.T) {
	r := newTestRootForShells(t)
	stubShellSession(t, func() *exec.Cmd { return exec.Command("sh", "-c", "sleep 5") })

	conn := testShellConn("removeme")
	r.openNewShellEmbedded(conn)
	tab := r.shells[0]
	_ = tab.session.Close()
	r.removeShell(tab)

	r.openTerminalSwitcher(0)
	r.activateTerminalSwitcherCell(0, terminalSwitcherColClose)

	history, err := remotefs.LoadHistory()
	if err != nil {
		t.Fatalf("LoadHistory: %v", err)
	}
	if len(history) != 0 {
		t.Errorf("history after Remove = %+v, want empty", history)
	}
}

func TestTerminalSwitcherBareDigitReachesAHistoryOnlyRow(t *testing.T) {
	r := newTestRootForShells(t)
	stubShellSession(t, func() *exec.Cmd { return exec.Command("sh", "-c", "sleep 5") })

	conn := testShellConn("digitreconnect")
	r.openNewShellEmbedded(conn)
	tab := r.shells[0]
	_ = tab.session.Close()
	r.removeShell(tab)

	r.openTerminalSwitcher(0)
	r.captureTerminalSwitcherKey(tcell.NewEventKey(tcell.KeyRune, '1', tcell.ModNone))

	if len(r.shells) != 1 {
		t.Fatalf("len(r.shells) = %d after pressing '1' on a history-only row, want 1", len(r.shells))
	}
	defer func() { _ = r.shells[0].session.Close() }()
	if r.activePage != "" {
		t.Errorf("activePage = %q, want closed (\"\")", r.activePage)
	}
}
