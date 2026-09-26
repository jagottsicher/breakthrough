package ui

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// TestParseFindmntJSONFlattensNestedChildren pins the flattening this
// screen depends on: findmnt nests every mount under whatever it's
// physically mounted on top of, arbitrarily deep, and the table needs a
// plain list, depth-first, in the same order findmnt itself reports.
func TestParseFindmntJSONFlattensNestedChildren(t *testing.T) {
	const data = `{
		"filesystems": [
			{
				"target": "/", "source": "/dev/md0", "fstype": "ext4", "options": "rw",
				"children": [
					{"target": "/boot", "source": "/dev/md1", "fstype": "ext4", "options": "rw"},
					{
						"target": "/home", "source": "/dev/md2", "fstype": "ext4", "options": "rw",
						"children": [
							{"target": "/home/jens/snap", "source": "/dev/sdd1", "fstype": "ext4", "options": "rw"}
						]
					}
				]
			}
		]
	}`

	got, err := parseFindmntJSON([]byte(data))
	if err != nil {
		t.Fatalf("parseFindmntJSON: %v", err)
	}

	wantTargets := []string{"/", "/boot", "/home", "/home/jens/snap"}
	if len(got) != len(wantTargets) {
		t.Fatalf("got %d nodes, want %d: %+v", len(got), len(wantTargets), got)
	}
	for i, want := range wantTargets {
		if got[i].Target != want {
			t.Errorf("node %d target = %q, want %q", i, got[i].Target, want)
		}
		if got[i].Children != nil {
			t.Errorf("node %d (%q) still carries its own Children after flattening", i, got[i].Target)
		}
	}
}

// TestParseFindmntJSONInvalidReturnsError pins that malformed input is
// reported rather than silently producing an empty (and therefore
// misleadingly "nothing is mounted") list.
func TestParseFindmntJSONInvalidReturnsError(t *testing.T) {
	if _, err := parseFindmntJSON([]byte("not json")); err == nil {
		t.Error("parseFindmntJSON should report an error for invalid JSON")
	}
}

// TestIsBindMountSourceDetectsBracketNotation pins the exact pattern
// confirmed by hand against a real bind mount on a real system:
// "<device>[<subpath>]" means a bind mount of that sub-path, and
// anything else — a plain device, a bare name with no brackets, an
// unterminated bracket — is not one.
func TestIsBindMountSourceDetectsBracketNotation(t *testing.T) {
	cases := []struct {
		source string
		want   bool
	}{
		{"/dev/sdd1[/.bitcoin]", true},
		{"//192.168.1.1/share[/sub/dir]", true},
		{"/dev/sda1", false},
		{"tmpfs", false},
		{"/dev/sda1[unterminated", false},
	}
	for _, c := range cases {
		if got := isBindMountSource(c.source); got != c.want {
			t.Errorf("isBindMountSource(%q) = %v, want %v", c.source, got, c.want)
		}
	}
}

// TestBuildMountEntriesMarksFstabTargetsAsPersistent pins the whole
// point of cross-referencing the two findmnt calls: a live mount whose
// target is also configured in fstab is persistent (survives a
// reboot); one that isn't was mounted some other way.
func TestBuildMountEntriesMarksFstabTargetsAsPersistent(t *testing.T) {
	live := []findmntNode{
		{Target: "/", Source: "/dev/md0", Fstype: "ext4", Options: "rw"},
		{Target: "/mnt/usb", Source: "/dev/sdb1", Fstype: "vfat", Options: "rw"},
	}
	fstab := []findmntNode{
		{Target: "/", Source: "UUID=abc", Fstype: "ext4", Options: "errors=remount-ro"},
		{Target: "none", Source: "/dev/mapper/cryptswap1", Fstype: "swap", Options: "sw"},
	}

	entries := buildMountEntries(live, fstab)
	if len(entries) != 2 {
		t.Fatalf("got %d entries, want 2", len(entries))
	}
	if !entries[0].persistent {
		t.Errorf("%q is in fstab, should be marked persistent", entries[0].target)
	}
	if entries[1].persistent {
		t.Errorf("%q is not in fstab, should not be marked persistent", entries[1].target)
	}
}

// TestBuildMountEntriesNeverMatchesTheSwapPlaceholderTarget pins a
// specific edge case: fstab's own swap entries use the literal target
// "none", which is never a real mountpoint — a naive implementation
// comparing target strings could accidentally treat a live mount that
// somehow also reported "/" as empty, or otherwise degenerate, as a
// false match. This pins that "none" itself is never eligible at all.
func TestBuildMountEntriesNeverMatchesTheSwapPlaceholderTarget(t *testing.T) {
	live := []findmntNode{{Target: "none", Source: "/dev/mapper/cryptswap1", Fstype: "swap", Options: "sw"}}
	fstab := []findmntNode{{Target: "none", Source: "/dev/mapper/cryptswap1", Fstype: "swap", Options: "sw"}}

	entries := buildMountEntries(live, fstab)
	if len(entries) != 1 {
		t.Fatalf("got %d entries, want 1", len(entries))
	}
	if entries[0].persistent {
		t.Error("the swap placeholder target \"none\" should never be treated as a real, persistent mountpoint")
	}
}

// TestBuildMountEntriesDetectsBindMounts pins that bind-mount detection
// (see TestIsBindMountSourceDetectsBracketNotation) actually reaches
// the assembled mountEntry, not just the lower-level helper.
func TestBuildMountEntriesDetectsBindMounts(t *testing.T) {
	live := []findmntNode{
		{Target: "/home/jens/snap/bitcoin-core/common/.bitcoin", Source: "/dev/sdd1[/.bitcoin]", Fstype: "ext4", Options: "rw"},
		{Target: "/", Source: "/dev/md0", Fstype: "ext4", Options: "rw"},
	}

	entries := buildMountEntries(live, nil)
	if !entries[0].bind {
		t.Error("a bracketed source should be detected as a bind mount")
	}
	if entries[1].bind {
		t.Error("a plain device source should not be detected as a bind mount")
	}
}

// TestReadMountsRealSystem exercises the real, live shelling-out path
// end to end — skipped where findmnt itself isn't installed, the same
// requireCommand pattern TestFetchDiskUsageRealFilesystem already
// establishes for df. "/" is always mounted, and a real ext4/xfs/btrfs
// root is never excluded by --real's own pseudo-filesystem filter, so
// it's a safe thing to assert on any real Linux system.
func TestReadMountsRealSystem(t *testing.T) {
	requireCommand(t, "findmnt")

	entries, err := readMounts()
	if err != nil {
		t.Fatalf("readMounts: %v", err)
	}

	found := false
	for _, e := range entries {
		if e.target == "/" {
			found = true
		}
	}
	if !found {
		t.Error("readMounts should always include the real filesystem root \"/\"")
	}
}

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

// TestRenderMountsColorsNonPersistentEntriesWithWarning pins the one
// piece of the user's own explicit request this whole feature exists
// for: a mount that would NOT survive a reboot should visually stand
// out from one that would.
func TestRenderMountsColorsNonPersistentEntriesWithWarning(t *testing.T) {
	r, err := NewRoot(tview.NewApplication(), fixtureDir(t))
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.mountsErr = nil
	r.mountsEntries = []mountEntry{
		{target: "/", source: "/dev/md0", fstype: "ext4", persistent: true},
		{target: "/mnt/usb", source: "/dev/sdb1", fstype: "vfat", persistent: false},
	}

	r.renderMounts()

	persistentColor := cellTextColor(r.mountsTable.GetCell(1, mountsColPersistent))
	manualColor := cellTextColor(r.mountsTable.GetCell(2, mountsColPersistent))

	if persistentColor != r.theme.Text {
		t.Errorf("persistent entry's own color = %v, want the plain Text color %v", persistentColor, r.theme.Text)
	}
	if manualColor != r.theme.WarningText {
		t.Errorf("non-persistent entry's own color = %v, want WarningText %v", manualColor, r.theme.WarningText)
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
	r.mountsEntries = []mountEntry{{target: "/", source: "/dev/md0", fstype: "ext4"}}

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
	r.mountsEntries = []mountEntry{{target: "/definitely-not-a-real-mount-xyz"}}
	r.renderMounts()
	r.mountsTitleBar.SetRect(0, 0, 100, 1) // the same rect a real Draw would have left it at

	_, y, width, _ := r.mountsTitleBar.GetRect()
	col := reloadTitleBarButtonCol(width)
	captured, _ := captureReloadTitleBarMouse(r.mountsTitleBar, r.reloadMounts)(tview.MouseLeftClick, tcell.NewEventMouse(col, y, tcell.ButtonNone, 0))

	if captured != tview.MouseConsumed {
		t.Error("clicking the reload glyph should consume the click")
	}
	for _, e := range r.mountsEntries {
		if e.target == "/definitely-not-a-real-mount-xyz" {
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
	r.mountsEntries = []mountEntry{{target: "/", source: "/dev/md0", fstype: "ext4"}}

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
