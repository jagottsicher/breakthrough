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

	if r.panelHost.GetItemCount() != 1 {
		t.Fatalf("panelHost.GetItemCount() = %d, want 1", r.panelHost.GetItemCount())
	}
	if r.panelHost.GetItem(0) != r.shells[0].view {
		t.Error("panelHost's only item isn't the active shell's own view")
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
