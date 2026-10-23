package ui

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
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

	// Two terminals, "+  New terminal", and "Files" (a terminal is on
	// screen) — four rows.
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

	if got, want := r.terminalSwitcher.GetRowCount(), 2; got != want { // terminal + "New terminal", no "Files"
		t.Fatalf("switcher has %d rows, want %d", got, want)
	}
	if r.terminalSwitcherFilesRow != -1 {
		t.Errorf("terminalSwitcherFilesRow = %d, want -1", r.terminalSwitcherFilesRow)
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
