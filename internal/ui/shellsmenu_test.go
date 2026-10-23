package ui

import (
	"os/exec"
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

func TestRenderShellsMenuListsOpenShellsNumberedPlusNewShellAndFilesRows(t *testing.T) {
	r := newTestRootForShells(t)
	stubShellSession(t, func() *exec.Cmd { return exec.Command("sh", "-c", "sleep 5") })

	r.openNewShellEmbedded(testShellConn("alpha"))
	r.openNewShellEmbedded(testShellConn("beta"))
	defer func() {
		for _, tab := range r.shells {
			_ = tab.session.Close()
		}
	}()

	r.openShellsMenu()

	if got := r.shellsMenuTable.GetCell(0, shellsMenuColLabel).Text; got != "1. nobody@alpha" {
		t.Errorf("row 0 label = %q, want %q", got, "1. nobody@alpha")
	}
	if got := r.shellsMenuTable.GetCell(1, shellsMenuColLabel).Text; got != "2. nobody@beta  (current)" {
		t.Errorf("row 1 label = %q, want %q", got, "2. nobody@beta  (current)")
	}
	if got := r.shellsMenuTable.GetCell(2, shellsMenuColLabel).Text; got != "+ New shell…" {
		t.Errorf("row 2 label = %q, want %q", got, "+ New shell…")
	}
	if got := r.shellsMenuTable.GetCell(3, shellsMenuColLabel).Text; got != "Files" {
		t.Errorf("row 3 label = %q, want %q", got, "Files")
	}
}

func TestRenderShellsMenuOmitsFilesRowWhenNotViewingAShell(t *testing.T) {
	r := newTestRootForShells(t)
	stubShellSession(t, func() *exec.Cmd { return exec.Command("sh", "-c", "sleep 5") })

	r.openNewShellEmbedded(testShellConn("alpha"))
	defer func() { _ = r.shells[0].session.Close() }()
	r.viewFiles()

	r.openShellsMenu()

	if got := r.shellsMenuTable.GetCell(1, shellsMenuColLabel).Text; got != "+ New shell…" {
		t.Errorf("row 1 label = %q, want %q", got, "+ New shell…")
	}
	if r.shellsMenuFilesRow != -1 {
		t.Errorf("shellsMenuFilesRow = %d while not viewing a shell, want -1", r.shellsMenuFilesRow)
	}
}

// TestCaptureShellsMenuKeyBareDigitJumpsDirectlyToThatShell pins the
// user's own approved design: an unmodified digit typed while this
// overlay has focus jumps straight to that numbered Shell — the same
// tmux-style prefix-then-digit convention this whole scheme was built
// around (see OpenShellsMenuShortcut's own doc comment).
func TestCaptureShellsMenuKeyBareDigitJumpsDirectlyToThatShell(t *testing.T) {
	r := newTestRootForShells(t)
	stubShellSession(t, func() *exec.Cmd { return exec.Command("sh", "-c", "sleep 5") })

	r.openNewShellEmbedded(testShellConn("alpha"))
	r.openNewShellEmbedded(testShellConn("beta"))
	defer func() {
		for _, tab := range r.shells {
			_ = tab.session.Close()
		}
	}()

	r.openShellsMenu()
	r.captureShellsMenuKey(tcell.NewEventKey(tcell.KeyRune, '1', tcell.ModNone))

	if r.activePage != "" {
		t.Errorf("activePage after jumping = %q, want closed (\"\")", r.activePage)
	}
	if r.activeShell != 0 {
		t.Errorf("activeShell after pressing '1' = %d, want 0", r.activeShell)
	}
}

func TestCaptureShellsMenuKeyDigitWithNoMatchingShellIsIgnored(t *testing.T) {
	r := newTestRootForShells(t)
	stubShellSession(t, func() *exec.Cmd { return exec.Command("sh", "-c", "sleep 5") })

	r.openNewShellEmbedded(testShellConn("alpha"))
	defer func() { _ = r.shells[0].session.Close() }()

	r.openShellsMenu()
	returned := r.captureShellsMenuKey(tcell.NewEventKey(tcell.KeyRune, '9', tcell.ModNone))

	if returned == nil {
		t.Error("an unassigned digit should not be consumed")
	}
	if r.activePage == "" {
		t.Error("the menu shouldn't have closed for an unassigned digit")
	}
}

// TestCaptureShellsMenuKeyXClosesTheSelectedRowsShell pins the "x"/
// Delete close key — the identical shape captureSSHShellMenuKey's own
// equivalent already establishes for the "gs" dropdown's history rows.
func TestCaptureShellsMenuKeyXClosesTheSelectedRowsShell(t *testing.T) {
	r := newTestRootForShells(t)
	stubShellSession(t, func() *exec.Cmd { return exec.Command("sh", "-c", "sleep 5") })

	r.openNewShellEmbedded(testShellConn("alpha"))
	r.openNewShellEmbedded(testShellConn("beta"))
	defer func() {
		for _, tab := range r.shells {
			_ = tab.session.Close()
		}
	}()

	r.openShellsMenu()
	r.shellsMenuTable.Select(0, shellsMenuColLabel)
	r.captureShellsMenuKey(tcell.NewEventKey(tcell.KeyRune, 'x', tcell.ModNone))

	if len(r.shells) != 1 {
		t.Fatalf("len(r.shells) = %d after closing row 0, want 1", len(r.shells))
	}
	if r.shells[0].label() != "nobody@beta" {
		t.Errorf("surviving shell label = %q, want %q", r.shells[0].label(), "nobody@beta")
	}
}

func TestActivateShellsMenuCellNewShellRowOpensTheReusedDialogWithTheEmbeddedSubmitOverride(t *testing.T) {
	r := newTestRootForShells(t)
	stubShellSession(t, func() *exec.Cmd { return exec.Command("sh", "-c", "sleep 5") })

	r.openNewShellEmbedded(testShellConn("alpha"))
	defer func() {
		for _, tab := range r.shells {
			_ = tab.session.Close()
		}
	}()

	r.openShellsMenu()
	r.activateShellsMenuCell(r.shellsMenuNewRow, shellsMenuColLabel)

	if r.activePage != sshShellDialogPage {
		t.Errorf("activePage after the \"+ New shell…\" row = %q, want %q", r.activePage, sshShellDialogPage)
	}
	if r.sshShellSubmit == nil {
		t.Fatal("sshShellSubmit wasn't set — the dialog would fall back to the default ssh launcher")
	}

	r.sshShellHostField.SetText("newhost")
	r.runSSHShellFromDialog()

	if len(r.shells) != 2 {
		t.Fatalf("len(r.shells) = %d after submitting the dialog, want 2", len(r.shells))
	}
	if r.shells[1].conn.Host != "newhost" {
		t.Errorf("new shell's own conn.Host = %q, want %q", r.shells[1].conn.Host, "newhost")
	}
	if r.sshShellSubmit != nil {
		t.Error("sshShellSubmit should be cleared again after it fires once")
	}
}

func TestActivateShellsMenuCellFilesRowLeavesShellView(t *testing.T) {
	r := newTestRootForShells(t)
	stubShellSession(t, func() *exec.Cmd { return exec.Command("sh", "-c", "sleep 5") })

	r.openNewShellEmbedded(testShellConn("alpha"))
	defer func() { _ = r.shells[0].session.Close() }()

	r.openShellsMenu()
	r.activateShellsMenuCell(r.shellsMenuFilesRow, shellsMenuColLabel)

	if r.viewingShells {
		t.Error("viewingShells still true after activating the Files row")
	}
	if r.activePage != "" {
		t.Errorf("activePage after the Files row = %q, want closed (\"\")", r.activePage)
	}
}

// TestApplyThemeColorsTheShellsMenu is a regression test for the
// identical real bug TestApplyThemeColorsTheSSHShellMenuAndDialog
// already pins for the "gs" dropdown: a new widget added to Root is
// invisible to applyTheme until it gets its own explicit line there
// (see theme.go) — nothing paints a widget's colors just by it
// existing.
func TestApplyThemeColorsTheShellsMenu(t *testing.T) {
	r := newTestRootForShells(t)

	custom := solarizedTheme().Resolve()
	r.applyTheme(custom)

	if got := r.shellsMenuTable.GetBackgroundColor(); got != custom.SurfaceBackground {
		t.Errorf("shellsMenuTable background = %v, want %v", got, custom.SurfaceBackground)
	}
	if got := r.shellsMenuTitleBar.GetBackgroundColor(); got != custom.InputFocusedBackground {
		t.Errorf("shellsMenuTitleBar background = %v, want %v", got, custom.InputFocusedBackground)
	}
}

func TestShellsMenuTitleBarClosesOnCloseButtonClick(t *testing.T) {
	r := newTestRootForShells(t)
	r.openShellsMenu()

	x, y, width, _ := r.shellsMenuTitleBar.GetRect()
	closeX := x + toolWindowCloseButtonCol(0, width)
	captured, _ := r.captureShellsMenuTitleBarMouse(tview.MouseLeftClick, tcell.NewEventMouse(closeX, y, tcell.ButtonNone, 0))

	if captured != tview.MouseConsumed {
		t.Error("clicking the close button should consume the click")
	}
	if r.activePage != "" {
		t.Errorf("activePage after clicking the close button = %q, want closed (\"\")", r.activePage)
	}
}
