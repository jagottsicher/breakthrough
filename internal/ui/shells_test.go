package ui

import (
	"errors"
	"os/exec"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"

	"github.com/rivo/tview"

	"github.com/jagottsicher/breakthrough/internal/remotefs"
	"github.com/jagottsicher/breakthrough/internal/sshshell"
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

// TestExitedTerminalLeaveKeysReturnToFiles pins the full, wired-up
// path a real exit goes through — not just termtabview.go's own
// isolated InputHandler tests: watchShellExit's own callback body
// (reproduced directly, see this file's own doc comment on why) sets
// tab.view.exited, and openNewShellEmbedded wired tab.view.onLeave to
// ExitTerminalViewShortcut — so once exited, Ctrl+C/Escape/Tab on the
// actual mounted view really do leave Terminal view, the user's own
// explicit request ("mit Aktivierung des äußeren breakthrough").
func TestExitedTerminalLeaveKeysReturnToFiles(t *testing.T) {
	r := newTestRootForShells(t)
	stubShellSession(t, func() *exec.Cmd { return exec.Command("sh", "-c", "sleep 5") })

	r.openNewShellEmbedded(testShellConn("one"))
	tab := r.shells[0]

	tab.exited = true
	tab.view.exited = true
	refreshShellHeader(tab)

	tab.view.InputHandler()(tcell.NewEventKey(tcell.KeyCtrlC, 0, tcell.ModNone), func(tview.Primitive) {})

	if r.viewingShells {
		t.Error("viewingShells still true after Ctrl+C on an exited terminal's own view")
	}
	if r.panel == nil || r.app.GetFocus() != r.panel.table {
		t.Error("focus didn't return to the panel after leaving an exited terminal")
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

// The tests below pin the user's own explicit request: a Terminal's
// own connection is remembered in the shared, disk-persisted history
// (internal/remotefs) — the same one the Connect dialog and "gs"
// dropdown already use — not just for the lifetime of r.shells, so it
// can be reopened later, including after a real restart.

func TestOpenNewShellEmbeddedSavesToHistoryAsUntested(t *testing.T) {
	r := newTestRootForShells(t)
	stubShellSession(t, func() *exec.Cmd { return exec.Command("sh", "-c", "sleep 5") })

	conn := testShellConn("histone")
	r.openNewShellEmbedded(conn)
	defer func() { _ = r.shells[0].session.Close() }()

	history, err := remotefs.LoadHistory()
	if err != nil {
		t.Fatalf("LoadHistory: %v", err)
	}
	if len(history) != 1 {
		t.Fatalf("len(history) = %d, want 1", len(history))
	}
	if !history[0].Equal(conn) {
		t.Errorf("history[0].Connection = %+v, want %+v", history[0].Connection, conn)
	}
	if !history[0].Untested {
		t.Error("history[0].Untested = false, want true — the outcome isn't known yet")
	}
}

func TestWatchShellExitRecordsASuccessfulConnection(t *testing.T) {
	r := newTestRootForShells(t)
	stubShellSession(t, func() *exec.Cmd { return exec.Command("sh", "-c", "exit 0") })

	conn := testShellConn("histsuccess")
	r.openNewShellEmbedded(conn)
	tab := r.shells[0]

	// watchShellExit's own callback body, reproduced directly — see
	// this file's own earlier doc comment on why (QueueUpdateDraw
	// needs a real running event loop no test here starts).
	<-tab.session.Done()
	_, exitErr := tab.session.Exited()
	failed := false
	var ee *exec.ExitError
	if errors.As(exitErr, &ee) {
		failed = ee.ExitCode() == sshshell.ConnectionFailedExitCode
	}
	_ = remotefs.RecordAttempt(tab.conn, failed)

	history, err := remotefs.LoadHistory()
	if err != nil {
		t.Fatalf("LoadHistory: %v", err)
	}
	if len(history) != 1 {
		t.Fatalf("len(history) = %d, want 1", len(history))
	}
	if history[0].Untested {
		t.Error("history[0].Untested = true, want false — a real outcome is now known")
	}
	if history[0].LastFailed {
		t.Error("history[0].LastFailed = true, want false — an ordinary exit isn't a connection failure")
	}
}

func TestWatchShellExitRecordsAFailedConnectionOnExitCode255(t *testing.T) {
	r := newTestRootForShells(t)
	stubShellSession(t, func() *exec.Cmd { return exec.Command("sh", "-c", "exit 255") })

	conn := testShellConn("histfailed")
	r.openNewShellEmbedded(conn)
	tab := r.shells[0]

	<-tab.session.Done()
	_, exitErr := tab.session.Exited()
	failed := false
	var ee *exec.ExitError
	if errors.As(exitErr, &ee) {
		failed = ee.ExitCode() == sshshell.ConnectionFailedExitCode
	}
	_ = remotefs.RecordAttempt(tab.conn, failed)

	history, err := remotefs.LoadHistory()
	if err != nil {
		t.Fatalf("LoadHistory: %v", err)
	}
	if len(history) != 1 || !history[0].LastFailed {
		t.Fatalf("history = %+v, want exactly 1 entry marked LastFailed", history)
	}
}

// TestTerminalHistorySurvivesACleanRestart pins the actual, user-facing
// point of all this: a second, independent Root sharing the same
// config directory (standing in for "the app restarted") sees the
// connection a first run already made.
func TestTerminalHistorySurvivesACleanRestart(t *testing.T) {
	withTestConfigHome(t) // deliberately once — both Roots below share it
	dir := t.TempDir()

	r1, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot (first run): %v", err)
	}
	orig := startShellSession
	startShellSession = func(conn remotefs.Connection, opts termtab.StartOptions) (*termtab.Session, error) {
		opts.Command = func() *exec.Cmd { return exec.Command("sh", "-c", "sleep 5") }
		return termtab.Start(conn, opts)
	}
	t.Cleanup(func() { startShellSession = orig })

	conn := testShellConn("restarted")
	r1.openNewShellEmbedded(conn)
	_ = r1.shells[0].session.Close()

	r2, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot (second run): %v", err)
	}
	if len(r2.shells) != 0 {
		t.Fatalf("the second run's own r.shells = %v, want empty — it never opened anything itself", r2.shells)
	}

	history, err := remotefs.LoadHistory()
	if err != nil {
		t.Fatalf("LoadHistory: %v", err)
	}
	if len(history) != 1 || !history[0].Equal(conn) {
		t.Fatalf("history after \"restart\" = %+v, want the first run's own connection still there", history)
	}
}
