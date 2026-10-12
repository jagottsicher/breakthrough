package ui

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/jagottsicher/breakthrough/internal/mounts"
)

// Parsing/fstab-cross-referencing logic (findmnt JSON flattening, bind
// detection, persistence matching) is tested in internal/mounts itself —
// this file only covers the UI/rendering contract on top of it.

// TestOpenMountsShowsThePage pins openMounts' own basic contract.
func TestOpenMountsShowsThePage(t *testing.T) {
	r, err := NewRoot(tview.NewApplication(), fixtureDir(t))
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}

	r.openMounts()

	if r.activePage != mountsPage {
		t.Fatalf("activePage = %q, want the Mounts screen", r.activePage)
	}
}

// TestCaptureMountsKeyEscapeClosesTheScreen mirrors the Toolbox
// screen's own equivalent test.
func TestCaptureMountsKeyEscapeClosesTheScreen(t *testing.T) {
	r, err := NewRoot(tview.NewApplication(), fixtureDir(t))
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.openMounts()

	if got := r.captureMountsKey(tcell.NewEventKey(tcell.KeyEscape, 0, tcell.ModNone)); got != nil {
		t.Error("captureMountsKey should consume Escape")
	}
	if r.activePage == mountsPage {
		t.Error("Escape should have closed the Mounts screen")
	}
}

// TestCaptureMountsKeyRRefreshesWithoutClosing pins that "r" is
// consumed and re-reads the mount table (via reloadMounts) without
// closing the screen — even when findmnt itself fails (a real
// possibility on a non-Linux build target), reloadMounts reports that
// through mountsErr rather than leaving the screen in an inconsistent
// state, so this doesn't need requireCommand to hold either way.
func TestCaptureMountsKeyRRefreshesWithoutClosing(t *testing.T) {
	r, err := NewRoot(tview.NewApplication(), fixtureDir(t))
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.openMounts()

	if got := r.captureMountsKey(tcell.NewEventKey(tcell.KeyRune, 'r', tcell.ModNone)); got != nil {
		t.Error("captureMountsKey should consume \"r\"")
	}
	if r.activePage != mountsPage {
		t.Error("\"r\" should refresh in place, not close the screen")
	}
}

// TestRenderMountsShowsTheErrorInPlaceOfRows pins that a failed read
// (findmnt missing or erroring outright) is reported on screen rather
// than leaving the table looking like an empty, still-successful read.
func TestRenderMountsShowsTheErrorInPlaceOfRows(t *testing.T) {
	r, err := NewRoot(tview.NewApplication(), fixtureDir(t))
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.mountsErr = errors.New("findmnt: command not found")
	r.mountsEntries = nil

	r.renderMounts()

	got := strings.TrimSpace(r.mountsTable.GetCell(1, mountsColTarget).Text)
	if !strings.Contains(got, "not found") {
		t.Errorf("row 1 = %q, want it to show the read error", got)
	}
}

// TestRenderMountsShowsAPlaceholderWhenGenuinelyEmpty pins the other
// no-real-content case (a successful read finding no real storage
// mounted at all — vanishingly unlikely in practice, but not
// impossible): reported explicitly, the same as a failed read, rather
// than just leaving the table looking like an empty header.
func TestRenderMountsShowsAPlaceholderWhenGenuinelyEmpty(t *testing.T) {
	r, err := NewRoot(tview.NewApplication(), fixtureDir(t))
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.mountsErr = nil
	r.mountsEntries = nil

	r.renderMounts()

	got := strings.TrimSpace(r.mountsTable.GetCell(1, mountsColTarget).Text)
	if !strings.Contains(got, "No real storage mounted") {
		t.Errorf("row 1 = %q, want the empty-read placeholder", got)
	}
}

// TestMountsReloadIntoAnErrorNeverHangsOnDown pins the same real,
// reported freeze TestFirewallReloadIntoAnErrorNeverHangsOnDown pins for
// the Firewall screen — see that test's own doc comment and
// renderMounts' own doc comment on its error branch's Select call for
// the mechanism and the fix.
func TestMountsReloadIntoAnErrorNeverHangsOnDown(t *testing.T) {
	r, err := NewRoot(tview.NewApplication(), fixtureDir(t))
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.mountsErr = errors.New("findmnt: command not found")
	r.mountsEntries = nil
	r.renderMounts()
	r.mountsTable.Select(15, 0) // simulate a stale cursor from a much longer previous list

	r.renderMounts() // still errors — re-renders down to just 2 rows

	callWithTimeout(t, 2*time.Second, func() {
		r.mountsTable.InputHandler()(tcell.NewEventKey(tcell.KeyDown, 0, tcell.ModNone), func(tview.Primitive) {})
	})
}

// TestMountsErrorScreenNeverHangsOnDownAfterARealDraw pins the actual
// mechanism behind TestMountsReloadIntoAnErrorNeverHangsOnDown — see
// TestFirewallErrorScreenNeverHangsOnDownAfterARealDraw's own doc
// comment for the full explanation. No prior big list or stale Select()
// needed at all here either: just one ordinary redraw between opening
// the screen and the very first arrow key.
func TestMountsErrorScreenNeverHangsOnDownAfterARealDraw(t *testing.T) {
	r, err := NewRoot(tview.NewApplication(), fixtureDir(t))
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.mountsErr = errors.New("findmnt: command not found")
	r.mountsEntries = nil
	r.renderMounts()

	screen := tcell.NewSimulationScreen("")
	if err := screen.Init(); err != nil {
		t.Fatalf("screen.Init: %v", err)
	}
	defer screen.Fini()
	screen.SetSize(100, 40)
	r.mountsTable.SetRect(0, 0, 100, 40)
	r.mountsTable.Draw(screen) // the real app's own ordinary redraw

	callWithTimeout(t, 2*time.Second, func() {
		r.mountsTable.InputHandler()(tcell.NewEventKey(tcell.KeyDown, 0, tcell.ModNone), func(tview.Primitive) {})
	})
}

// TestRenderMountsPersistentColumnUsesTheSamePlainColorRegardless pins
// the user's own explicit follow-up request: a non-persistent mount's
// own circle used to stand out in WarningText (orange) — reported back
// as confusing rather than informative once actually seen in daily use
// ("das macht eigentlich keinen Sinn") — so both persistent and
// non-persistent entries now render in the same plain, unremarkable
// color as the Bind column right next to it, not a warning color that
// singles either one out.
func TestRenderMountsPersistentColumnUsesTheSamePlainColorRegardless(t *testing.T) {
	r, err := NewRoot(tview.NewApplication(), fixtureDir(t))
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.mountsErr = nil
	r.mountsEntries = []mounts.Entry{
		{Target: "/", Source: "/dev/md0", Fstype: "ext4", Persistent: true},
		{Target: "/mnt/usb", Source: "/dev/sdb1", Fstype: "vfat", Persistent: false},
	}

	r.renderMounts()

	persistentColor := cellTextColor(r.mountsTable.GetCell(1, mountsColPersistent))
	manualColor := cellTextColor(r.mountsTable.GetCell(2, mountsColPersistent))

	if persistentColor != r.theme.PlaceholderText {
		t.Errorf("persistent entry's own color = %v, want the plain PlaceholderText color %v", persistentColor, r.theme.PlaceholderText)
	}
	if manualColor != r.theme.PlaceholderText {
		t.Errorf("non-persistent entry's own color = %v, want the same plain PlaceholderText color %v, not a warning color", manualColor, r.theme.PlaceholderText)
	}
}

// TestRenderMountsSelectsTheFirstDataRowOnFirstRender pins that the
// cursor never starts on the header row (row 0), which Enter/click
// can't meaningfully do anything with.
func TestRenderMountsSelectsTheFirstDataRowOnFirstRender(t *testing.T) {
	r, err := NewRoot(tview.NewApplication(), fixtureDir(t))
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.mountsEntries = []mounts.Entry{{Target: "/", Source: "/dev/md0", Fstype: "ext4"}}

	r.renderMounts()

	row, _ := r.mountsTable.GetSelection()
	if row != 1 {
		t.Errorf("selected row = %d, want 1 (the first real data row, not the row-0 header)", row)
	}
}

// TestMountsTitleBarReloadButtonClickReloads pins the user's own
// explicit request for a mouse-reachable reload button on the Mounts
// screen's own title bar, top right: clicking the exact glyph cell must
// re-read the live mount table, the same as pressing "r". Runs against
// the real findmnt (see TestReadMountsRealSystem's own identical
// guard/reasoning), since readMounts has no swappable var of its own —
// a placeholder entry findmnt could never itself report is used to
// detect that a real re-read actually happened, not just a re-render of
// whatever was already there.
func TestMountsTitleBarReloadButtonClickReloads(t *testing.T) {
	requireCommand(t, "findmnt")

	r, err := NewRoot(tview.NewApplication(), fixtureDir(t))
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.openMounts()

	screen := simulationScreen(t, 100, 30)
	r.handleBeforeDraw(screen) // establishes lastScreenWidth, which renderReloadTitleBar's own column math needs
	r.mountsEntries = []mounts.Entry{{Target: "/definitely-not-a-real-mount-xyz"}}
	r.renderMounts()
	r.mountsTitleBar.SetRect(0, 0, 100, 1) // the same rect a real Draw would have left it at

	_, y, width, _ := r.mountsTitleBar.GetRect()
	col := reloadTitleBarButtonCol(width)
	captured, _ := captureReloadTitleBarMouse(r.mountsTitleBar, r.reloadMounts)(tview.MouseLeftClick, tcell.NewEventMouse(col, y, tcell.ButtonNone, 0))

	if captured != tview.MouseConsumed {
		t.Error("clicking the reload glyph should consume the click")
	}
	for _, e := range r.mountsEntries {
		if e.Target == "/definitely-not-a-real-mount-xyz" {
			t.Error("the placeholder entry survived the click, want it replaced by a real re-read")
		}
	}
}

// TestMountsTitleBarClickElsewhereDoesNothing pins that only the exact
// reload-glyph cell does anything — the same "no drag, no other action"
// scope Help's own title bar already has (see
// TestHelpTitleBarClickElsewhereDoesNothing).
func TestMountsTitleBarClickElsewhereDoesNothing(t *testing.T) {
	r, err := NewRoot(tview.NewApplication(), fixtureDir(t))
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.mountsEntries = []mounts.Entry{{Target: "/", Source: "/dev/md0", Fstype: "ext4"}}

	screen := simulationScreen(t, 100, 30)
	r.handleBeforeDraw(screen)
	r.renderMounts()
	r.mountsTitleBar.SetRect(0, 0, 100, 1)

	reloaded := false
	captured, _ := captureReloadTitleBarMouse(r.mountsTitleBar, func() { reloaded = true })(tview.MouseLeftClick, tcell.NewEventMouse(0, 0, tcell.ButtonNone, 0))

	if captured == tview.MouseConsumed {
		t.Error("a click away from the reload glyph should not be consumed")
	}
	if reloaded {
		t.Error("a click away from the reload glyph should not have reloaded")
	}
}
