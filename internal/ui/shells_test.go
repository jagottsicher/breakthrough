package ui

import (
	"os/exec"
	"testing"
	"time"

	"github.com/rivo/tview"

	"github.com/jagottsicher/breakthrough/internal/remotefs"
	"github.com/jagottsicher/breakthrough/internal/termtab"
)

// newTestRootForShells builds a plain, local Root — the identical
// shape newTestRootForSSHShell already establishes, minus opening any
// overlay: most tests here drive openNewShellEmbedded/switchToShell
// directly instead of going through the Shells menu's own rows.
func newTestRootForShells(t *testing.T) *Root {
	t.Helper()
	withTestConfigHome(t)
	dir := t.TempDir()
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	return r
}

// stubShellSession points startShellSession (shells.go's own test
// seam) at a real termtab.Session running newCmd()'s own process — a
// real pty-backed process, the same "real external process, not a
// mock" preference Phases 1/2's own tests already establish — instead
// of ever actually dialing ssh. newCmd is called fresh for every
// Shell opened against this stub (an *exec.Cmd can only ever be
// started once), exactly as termtab.StartOptions.Command's own doc
// comment already requires of every caller.
func stubShellSession(t *testing.T, newCmd func() *exec.Cmd) {
	t.Helper()
	orig := startShellSession
	startShellSession = func(conn remotefs.Connection, opts termtab.StartOptions) (*termtab.Session, error) {
		opts.Command = newCmd
		return termtab.Start(conn, opts)
	}
	t.Cleanup(func() { startShellSession = orig })
}

func testShellConn(label string) remotefs.Connection {
	return remotefs.Connection{Host: label, User: "nobody"}
}

func TestOpenNewShellEmbeddedAddsAndSwitchesToIt(t *testing.T) {
	r := newTestRootForShells(t)
	stubShellSession(t, func() *exec.Cmd { return exec.Command("sh", "-c", "sleep 5") })

	r.openNewShellEmbedded(testShellConn("one"))
	defer func() {
		for _, tab := range r.shells {
			_ = tab.session.Close()
		}
	}()

	if len(r.shells) != 1 {
		t.Fatalf("len(r.shells) = %d, want 1", len(r.shells))
	}
	if !r.viewingShells {
		t.Error("viewingShells = false after opening a shell, want true")
	}
	if r.activeShell != 0 {
		t.Errorf("activeShell = %d, want 0", r.activeShell)
	}
}

func TestOpenNewShellEmbeddedSurfacesAStartFailureAsAnError(t *testing.T) {
	r := newTestRootForShells(t)
	orig := startShellSession
	startShellSession = func(conn remotefs.Connection, opts termtab.StartOptions) (*termtab.Session, error) {
		opts.Command = func() *exec.Cmd { return exec.Command("/nonexistent-breakthrough-test-binary") }
		return termtab.Start(conn, opts)
	}
	t.Cleanup(func() { startShellSession = orig })

	r.openNewShellEmbedded(testShellConn("broken"))

	if len(r.shells) != 0 {
		t.Errorf("len(r.shells) = %d after a failed start, want 0", len(r.shells))
	}
	if r.viewingShells {
		t.Error("viewingShells = true after a failed start, want false")
	}
}

func TestRemountShellViewCollapsesTheBashConsoleAndShowsTheActiveView(t *testing.T) {
	r := newTestRootForShells(t)
	stubShellSession(t, func() *exec.Cmd { return exec.Command("sh", "-c", "sleep 5") })

	r.openNewShellEmbedded(testShellConn("one"))
	defer func() { _ = r.shells[0].session.Close() }()

	// Three items: the panel's own header row (kept visible — see
	// remountShellView's own doc comment), this Terminal's own
	// one-line header, and the terminal view itself.
	if got, want := r.panelHost.GetItemCount(), 3; got != want {
		t.Fatalf("panelHost.GetItemCount() = %d, want %d", got, want)
	}
	if r.panelHost.GetItem(0) != r.panel.headerRow {
		t.Error("panelHost's first item isn't the panel's own header row")
	}
	if r.panelHost.GetItem(1) != r.shells[0].header {
		t.Error("panelHost's second item isn't the active shell's own header")
	}
	if r.panelHost.GetItem(2) != r.shells[0].view {
		t.Error("panelHost's third item isn't the active shell's own view")
	}
}

func TestViewFilesLeavesShellViewAndRestoresThePanel(t *testing.T) {
	r := newTestRootForShells(t)
	stubShellSession(t, func() *exec.Cmd { return exec.Command("sh", "-c", "sleep 5") })

	r.openNewShellEmbedded(testShellConn("one"))
	defer func() { _ = r.shells[0].session.Close() }()

	r.viewFiles()

	if r.viewingShells {
		t.Error("viewingShells still true after viewFiles")
	}
	if r.panelHost.GetItem(0) != r.panel {
		t.Error("panelHost's item isn't r.panel after viewFiles")
	}
}

func TestSwitchingFileTabsLeavesShellViewAutomatically(t *testing.T) {
	// remountPanels (split.go) is where this is actually implemented —
	// see its own doc comment — this test pins the cross-package
	// contract that every existing caller of it (here: newTab) keeps
	// working correctly now that Shells exist, without newTab itself
	// needing to know that.
	r := newTestRootForShells(t)
	stubShellSession(t, func() *exec.Cmd { return exec.Command("sh", "-c", "sleep 5") })

	r.openNewShellEmbedded(testShellConn("one"))
	defer func() { _ = r.shells[0].session.Close() }()

	r.newTab(r.panel.path)

	if r.viewingShells {
		t.Error("viewingShells still true after opening a new file tab")
	}
}

// removeShell is only ever actually invoked from inside an
// r.app.QueueUpdateDraw callback (watchShellExit) — which, like every
// other background-job finish callback in this package (see
// finishCompressJob/finishRsyncJob and their own tests), needs a real
// running Application event loop to ever execute at all, something no
// test in this package starts (see compressjob_test.go's own doc
// comment on finishCompressJob for the identical reasoning). These
// tests therefore call removeShell directly, the same convention
// finishCompressJob's own tests already establish, rather than
// exercising watchShellExit's goroutine end to end.

func TestRemoveShellFallsBackToTheNextOneWhenTheActiveShellCloses(t *testing.T) {
	r := newTestRootForShells(t)
	stubShellSession(t, func() *exec.Cmd { return exec.Command("sh", "-c", "sleep 5") })

	r.openNewShellEmbedded(testShellConn("one"))
	r.openNewShellEmbedded(testShellConn("two"))
	defer func() {
		for _, tab := range r.shells {
			_ = tab.session.Close()
		}
	}()

	if len(r.shells) != 2 || r.activeShell != 1 {
		t.Fatalf("setup: len=%d activeShell=%d, want 2/1", len(r.shells), r.activeShell)
	}

	closed := r.shells[1]
	r.removeShell(closed)

	if len(r.shells) != 1 {
		t.Fatalf("len(r.shells) = %d, want 1", len(r.shells))
	}
	if r.activeShell != 0 {
		t.Errorf("activeShell = %d after removing the active (last) shell, want 0", r.activeShell)
	}
	if !r.viewingShells {
		t.Error("viewingShells = false after removing one of two shells, want true (one remains)")
	}
}

func TestRemoveShellOnlyAdjustsTheIndexWhenANonActiveShellCloses(t *testing.T) {
	r := newTestRootForShells(t)
	stubShellSession(t, func() *exec.Cmd { return exec.Command("sh", "-c", "sleep 5") })

	r.openNewShellEmbedded(testShellConn("one"))
	r.openNewShellEmbedded(testShellConn("two"))
	defer func() {
		for _, tab := range r.shells {
			_ = tab.session.Close()
		}
	}()

	active := r.shells[1]
	closed := r.shells[0]
	r.removeShell(closed)

	if len(r.shells) != 1 {
		t.Fatalf("len(r.shells) = %d, want 1", len(r.shells))
	}
	if r.activeShell != 0 {
		t.Errorf("activeShell = %d after removing an earlier, non-active shell, want 0", r.activeShell)
	}
	if r.shells[0] != active {
		t.Error("the surviving shell isn't the one that was active before the removal")
	}
}

func TestRemoveLastShellReturnsToFileView(t *testing.T) {
	r := newTestRootForShells(t)
	stubShellSession(t, func() *exec.Cmd { return exec.Command("sh", "-c", "sleep 5") })

	r.openNewShellEmbedded(testShellConn("one"))
	tab := r.shells[0]
	_ = tab.session.Close()

	r.removeShell(tab)

	if len(r.shells) != 0 {
		t.Errorf("len(r.shells) = %d, want 0", len(r.shells))
	}
	if r.viewingShells {
		t.Error("viewingShells = true after removing the only open shell, want false")
	}
	if r.panelHost.GetItem(0) != r.panel {
		t.Error("panelHost's item isn't r.panel after removing the only shell")
	}
}

func TestRemoveShellIsANoOpForAnAlreadyRemovedShell(t *testing.T) {
	r := newTestRootForShells(t)
	stubShellSession(t, func() *exec.Cmd { return exec.Command("sh", "-c", "sleep 5") })

	r.openNewShellEmbedded(testShellConn("one"))
	tab := r.shells[0]
	r.removeShell(tab)
	r.removeShell(tab) // must not panic or go negative

	if len(r.shells) != 0 {
		t.Errorf("len(r.shells) = %d, want 0", len(r.shells))
	}
}

func TestCloseShellClosesTheUnderlyingSession(t *testing.T) {
	r := newTestRootForShells(t)
	stubShellSession(t, func() *exec.Cmd { return exec.Command("sh", "-c", "sleep 5") })

	r.openNewShellEmbedded(testShellConn("one"))
	tab := r.shells[0]

	r.closeShell(0)

	select {
	case <-tab.session.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("session.Done() never fired after closeShell")
	}
}

// The tests below pin the user's own explicit request: a Terminal
// whose own process exits on its own stays exactly where it was — in
// r.shells, in the strip, in the switcher, and on screen if it was the
// one showing — rather than disappearing or falling back to the
// panel. watchShellExit itself only ever runs inside an
// r.app.QueueUpdateDraw callback (see shells.go's own doc comment on
// why that needs a real running Application event loop no test here
// starts — the same reasoning TestRemoveShellFallsBackToTheNextOne...
// already established for removeShell). These tests therefore reproduce
// that callback's own body directly, the same convention
// finishCompressJob's own tests already establish.

func TestExitedTerminalStaysInTheListAndOnScreen(t *testing.T) {
	r := newTestRootForShells(t)
	stubShellSession(t, func() *exec.Cmd { return exec.Command("sh", "-c", "sleep 5") })

	r.openNewShellEmbedded(testShellConn("one"))
	tab := r.shells[0]

	// watchShellExit's own callback body, reproduced directly (see
	// this block's own doc comment).
	tab.exited = true
	refreshShellHeader(tab)

	if len(r.shells) != 1 {
		t.Errorf("len(r.shells) = %d after exit, want 1 (still there)", len(r.shells))
	}
	if !r.viewingShells {
		t.Error("viewingShells = false after exit while it was the one on screen, want true (no auto fallback)")
	}
	if r.panelHost.GetItem(2) != tab.view {
		t.Error("panelHost's view item changed after exit, want the same frozen view still mounted")
	}
}

func TestExitedTerminalLabelAndHeaderShowExited(t *testing.T) {
	r := newTestRootForShells(t)
	stubShellSession(t, func() *exec.Cmd { return exec.Command("sh", "-c", "sleep 5") })

	r.openNewShellEmbedded(testShellConn("one"))
	tab := r.shells[0]

	if got := tab.label(); got == tab.conn.Label()+"  (exited)" {
		t.Fatalf("label already marked exited before the process exited: %q", got)
	}

	tab.exited = true
	refreshShellHeader(tab)

	wantLabel := tab.conn.Label() + "  (exited)"
	if got := tab.label(); got != wantLabel {
		t.Errorf("label() after exit = %q, want %q", got, wantLabel)
	}
	if got, want := tab.header.GetText(true), " "+wantLabel+" "; got != want {
		t.Errorf("header text after exit = %q, want %q", got, want)
	}
}

// TestApplyThemeColorsEveryShellHeader is a regression test for the
// identical real bug TestApplyThemeColorsTheShellsMenu/
// TestApplyThemeColorsTheSSHShellMenuAndDialog already pin for two
// earlier widgets in this same feature: a dynamically created widget
// with no fixed Root field (there can be any number of open
// Terminals) needs its own loop in applyTheme, not just a styling
// call at construction time — otherwise a live scheme switch leaves
// it showing whatever color it was built with.
func TestApplyThemeColorsEveryShellHeader(t *testing.T) {
	r := newTestRootForShells(t)
	stubShellSession(t, func() *exec.Cmd { return exec.Command("sh", "-c", "sleep 5") })

	r.openNewShellEmbedded(testShellConn("one"))
	defer func() { _ = r.shells[0].session.Close() }()

	custom := solarizedTheme().Resolve()
	r.applyTheme(custom)

	if got := r.shells[0].header.GetBackgroundColor(); got != custom.InputFocusedBackground {
		t.Errorf("shell header background = %v, want %v", got, custom.InputFocusedBackground)
	}
}

func TestClosingAnExitedTerminalStillRemovesIt(t *testing.T) {
	r := newTestRootForShells(t)
	stubShellSession(t, func() *exec.Cmd { return exec.Command("sh", "-c", "exit 0") })

	r.openNewShellEmbedded(testShellConn("one"))
	tab := r.shells[0]
	select {
	case <-tab.session.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("the stub process never exited")
	}
	tab.exited = true

	r.closeShell(0)

	if len(r.shells) != 0 {
		t.Errorf("len(r.shells) = %d after explicitly closing an exited terminal, want 0", len(r.shells))
	}
}
