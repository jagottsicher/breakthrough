package ui

import (
	"errors"
	"io/fs"
	"strings"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/jagottsicher/breakthrough/internal/sshkeys"
)

// isolateSSHKeysRead swaps readSSHKeys/readSSHAgent for the duration of
// one test, restoring the originals via t.Cleanup — the same
// "swap the package-level var, restore in Cleanup" idiom
// isolateFirewallRead already establishes, so these tests never touch a
// real ~/.ssh directory or a real running ssh-agent.
func isolateSSHKeysRead(t *testing.T, pairs []sshkeys.KeyPair, err error, agent map[string]bool) {
	t.Helper()
	origKeys, origAgent := readSSHKeys, readSSHAgent
	t.Cleanup(func() {
		readSSHKeys, readSSHAgent = origKeys, origAgent
	})
	readSSHKeys = func(string) ([]sshkeys.KeyPair, error) { return pairs, err }
	readSSHAgent = func() map[string]bool { return agent }
}

func TestOpenSSHKeysShowsTheOverlay(t *testing.T) {
	isolateSSHKeysRead(t, nil, nil, nil)
	r, err := NewRoot(tview.NewApplication(), fixtureDir(t))
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}

	r.openSSHKeys()

	if r.activePage != sshKeysPage {
		t.Errorf("activePage = %q, want %q", r.activePage, sshKeysPage)
	}
}

func TestCaptureSSHKeysKeyEscapeCloses(t *testing.T) {
	isolateSSHKeysRead(t, nil, nil, nil)
	r, err := NewRoot(tview.NewApplication(), fixtureDir(t))
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.openSSHKeys()

	if got := r.captureSSHKeysKey(tcell.NewEventKey(tcell.KeyEscape, 0, tcell.ModNone)); got != nil {
		t.Error("captureSSHKeysKey should consume Escape")
	}
	if r.activePage == sshKeysPage {
		t.Error("Escape should have closed the SSH Keys screen")
	}
}

func TestCaptureSSHKeysKeyRRefreshesWithoutClosing(t *testing.T) {
	isolateSSHKeysRead(t, nil, nil, nil)
	r, err := NewRoot(tview.NewApplication(), fixtureDir(t))
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.openSSHKeys()

	if got := r.captureSSHKeysKey(tcell.NewEventKey(tcell.KeyRune, 'r', tcell.ModNone)); got != nil {
		t.Error("captureSSHKeysKey should consume \"r\"")
	}
	if r.activePage != sshKeysPage {
		t.Error("\"r\" should refresh in place, not close the screen")
	}
}

// TestRenderSSHKeysShowsTheErrorInPlaceOfRows pins that a failed scan
// (~/.ssh unreadable for a reason other than not existing) is reported
// on screen rather than leaving the table looking like an empty, still-
// successful read.
func TestRenderSSHKeysShowsTheErrorInPlaceOfRows(t *testing.T) {
	r, err := NewRoot(tview.NewApplication(), fixtureDir(t))
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.sshKeysErr = errors.New("open /home/x/.ssh: permission denied")
	r.sshKeysPairs = nil

	r.renderSSHKeys()

	got := strings.TrimSpace(r.sshKeysTable.GetCell(1, sshKeysColName).Text)
	if !strings.Contains(got, "permission denied") {
		t.Errorf("row 1 = %q, want it to show the read error", got)
	}
}

// TestRenderSSHKeysShowsAPlaceholderWhenEmpty pins the other no-real-
// content case: a successful scan finding no key pairs at all.
func TestRenderSSHKeysShowsAPlaceholderWhenEmpty(t *testing.T) {
	r, err := NewRoot(tview.NewApplication(), fixtureDir(t))
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.sshKeysErr = nil
	r.sshKeysPairs = nil

	r.renderSSHKeys()

	got := strings.TrimSpace(r.sshKeysTable.GetCell(1, sshKeysColName).Text)
	if !strings.Contains(got, "No SSH key pairs found") {
		t.Errorf("row 1 = %q, want the empty-scan placeholder", got)
	}
}

// TestReloadSSHKeysTreatsAMissingDirAsEmptyNotAnError pins that a
// ~/.ssh that has simply never been created is not this screen's own
// error to report — the same "not every non-existence is a failure"
// reasoning Mounts' own "No real storage mounted." already follows for
// its own genuinely-empty case.
func TestReloadSSHKeysTreatsAMissingDirAsEmptyNotAnError(t *testing.T) {
	isolateSSHKeysRead(t, nil, fs.ErrNotExist, nil)
	r, err := NewRoot(tview.NewApplication(), fixtureDir(t))
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}

	r.reloadSSHKeys()

	if r.sshKeysErr != nil {
		t.Errorf("sshKeysErr = %v, want nil for a simply-missing ~/.ssh", r.sshKeysErr)
	}
	got := strings.TrimSpace(r.sshKeysTable.GetCell(1, sshKeysColName).Text)
	if !strings.Contains(got, "No SSH key pairs found") {
		t.Errorf("row 1 = %q, want the empty-scan placeholder", got)
	}
}

func TestRenderSSHKeysRowValuesAndColors(t *testing.T) {
	r, err := NewRoot(tview.NewApplication(), fixtureDir(t))
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.sshKeysErr = nil
	r.sshKeysAgent = map[string]bool{"SHA256:loaded": true}
	r.sshKeysPairs = []sshkeys.KeyPair{
		{
			Name: "id_ed25519", HasPrivate: true, HasPublic: true,
			Type: "ed25519", Bits: 256, Fingerprint: "SHA256:loaded",
			Comment: "user@host", PrivateMode: fs.FileMode(0o600),
		},
		{
			Name: "id_rsa_old", HasPrivate: true, HasPublic: true,
			Type: "rsa", Bits: 2048, Fingerprint: "SHA256:notloaded",
			PrivateMode: fs.FileMode(0o644), PrivatePermissiveWarning: true,
		},
		{
			Name: "orphan_pub", HasPrivate: false, HasPublic: true,
			Type: "ed25519", Bits: 256, Fingerprint: "SHA256:orphan",
		},
	}

	r.renderSSHKeys()

	// Row 1: fully loaded, unencrypted, strict permissions, agent-loaded.
	if got := r.sshKeysTable.GetCell(1, sshKeysColType).Text; strings.TrimSpace(got) != "ed25519 256" {
		t.Errorf("row 1 Type = %q, want \"ed25519 256\"", got)
	}
	if got := r.sshKeysTable.GetCell(1, sshKeysColAgent).Text; strings.TrimSpace(got) != "✔" {
		t.Errorf("row 1 Agent = %q, want ✔", got)
	}
	if got := cellTextColor(r.sshKeysTable.GetCell(1, sshKeysColAgent)); got != r.theme.EntryExecutable {
		t.Errorf("row 1 Agent color = %v, want EntryExecutable", got)
	}

	// Row 2: permissive private key file, not loaded in the agent.
	if got := r.sshKeysTable.GetCell(2, sshKeysColPerms).Text; strings.TrimSpace(got) != "644" {
		t.Errorf("row 2 Perms = %q, want 644", got)
	}
	if got := cellTextColor(r.sshKeysTable.GetCell(2, sshKeysColPerms)); got != r.theme.WarningText {
		t.Errorf("row 2 Perms color = %v, want WarningText for a permissive private key", got)
	}
	if got := r.sshKeysTable.GetCell(2, sshKeysColAgent).Text; strings.TrimSpace(got) != "✘" {
		t.Errorf("row 2 Agent = %q, want ✘", got)
	}

	// Row 3: no private key at all — Encrypted/Perms both show "–", and
	// the Note column says so.
	if got := r.sshKeysTable.GetCell(3, sshKeysColEncrypted).Text; strings.TrimSpace(got) != "–" {
		t.Errorf("row 3 Encrypted = %q, want –", got)
	}
	if got := r.sshKeysTable.GetCell(3, sshKeysColPerms).Text; strings.TrimSpace(got) != "–" {
		t.Errorf("row 3 Perms = %q, want –", got)
	}
	if got := r.sshKeysTable.GetCell(3, sshKeysColNote).Text; !strings.Contains(got, "no private key file") {
		t.Errorf("row 3 Note = %q, want it to mention the missing private key", got)
	}
}

func TestSSHKeysAgentGlyphUnknownWhenNoAgentReachable(t *testing.T) {
	kp := sshkeys.KeyPair{Fingerprint: "SHA256:x"}
	if got := sshKeysAgentGlyph(kp, nil); got != "–" {
		t.Errorf("sshKeysAgentGlyph() = %q, want – when no agent is reachable", got)
	}
}

func TestSSHKeysAgentGlyphUnknownWhenNoFingerprint(t *testing.T) {
	kp := sshkeys.KeyPair{}
	if got := sshKeysAgentGlyph(kp, map[string]bool{}); got != "–" {
		t.Errorf("sshKeysAgentGlyph() = %q, want – when there is no fingerprint to look up", got)
	}
}

func TestSSHKeysTypeLabel(t *testing.T) {
	cases := []struct {
		kp   sshkeys.KeyPair
		want string
	}{
		{sshkeys.KeyPair{Type: "ed25519", Bits: 256}, "ed25519 256"},
		{sshkeys.KeyPair{Type: "dsa", Bits: 0}, "dsa"},
		{sshkeys.KeyPair{Type: ""}, "unknown"},
	}
	for _, c := range cases {
		if got := sshKeysTypeLabel(c.kp); got != c.want {
			t.Errorf("sshKeysTypeLabel(%+v) = %q, want %q", c.kp, got, c.want)
		}
	}
}

// TestRenderSSHKeysSelectedRowUsesOneUniformHighlightColor pins a real,
// live-confirmed bug: without its own SetSelectedStyle, tview's default
// selected-cell style just reverses each cell's own foreground into a
// background, so the Agent/Comment/Note columns — each carrying its own
// semantic color (green ✔, muted placeholder text, warning orange, the
// last one even on an empty Note) — painted the selected row in
// mismatched color patches instead of one consistent highlight bar. Draws
// the real table via a SimulationScreen (the same mechanism
// TestMountsErrorScreenNeverHangsOnDownAfterARealDraw already uses) and
// checks the Agent/Comment/Note cells' own background at the selected
// row against theme.SelectionBackground.
func TestRenderSSHKeysSelectedRowUsesOneUniformHighlightColor(t *testing.T) {
	r, err := NewRoot(tview.NewApplication(), fixtureDir(t))
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.sshKeysErr = nil
	r.sshKeysAgent = map[string]bool{"SHA256:loaded": true}
	r.sshKeysPairs = []sshkeys.KeyPair{
		{
			Name: "id_ed25519", HasPrivate: true, HasPublic: true,
			Type: "ed25519", Bits: 256, Fingerprint: "SHA256:loaded",
			Comment: "user@host", PrivateMode: fs.FileMode(0o600),
		},
	}
	r.renderSSHKeys()
	r.applySSHKeysTheme(r.theme) // wires up SetSelectedStyle, same as a real theme apply would

	screen := tcell.NewSimulationScreen("")
	if err := screen.Init(); err != nil {
		t.Fatalf("screen.Init: %v", err)
	}
	defer screen.Fini()
	const screenWidth, screenHeight = 200, 20
	screen.SetSize(screenWidth, screenHeight)
	r.sshKeysTable.SetRect(0, 0, screenWidth, screenHeight)
	r.sshKeysTable.Select(1, 0)
	r.sshKeysTable.Draw(screen)

	// Locate the selected data row, and three of its own cells (Name —
	// always plain Text — plus Agent and Comment, the two that used to
	// leak their own semantic color through as a mismatched patch), by
	// their actual rendered text rather than a hand-computed x/y:
	// SetBorderPadding shifts every row and column, and hard-coding that
	// offset here would just make this test as fragile as the bug it's
	// pinning.
	y := findScreenRow(t, screen, screenWidth, screenHeight, "id_ed25519")
	nameX := findScreenCellX(t, screen, screenWidth, y, "id_ed25519")
	agentX := findScreenCellX(t, screen, screenWidth, y, "✔")
	commentX := findScreenCellX(t, screen, screenWidth, y, "user@host")

	_, nameStyle, _ := screen.Get(nameX, y)
	_, agentStyle, _ := screen.Get(agentX, y)
	_, commentStyle, _ := screen.Get(commentX, y)

	_, wantBg, _ := nameStyle.Decompose()
	for cellName, style := range map[string]tcell.Style{"Agent": agentStyle, "Comment": commentStyle} {
		_, gotBg, _ := style.Decompose()
		if gotBg != wantBg {
			t.Errorf("%s cell's own background on the selected row = %v, want the uniform background %v the Name cell right next to it already has", cellName, gotBg, wantBg)
		}
	}
}

// findScreenRow scans a drawn screen for the row containing substr —
// good enough for this test's own single-screen, un-scrolled table.
// Fails the test outright if no line matches, rather than silently
// comparing against row 0.
func findScreenRow(t *testing.T, screen tcell.SimulationScreen, width, height int, substr string) (y int) {
	t.Helper()
	for y := 0; y < height; y++ {
		if findScreenCellX(nil, screen, width, y, substr) >= 0 {
			return y
		}
	}
	t.Fatalf("no rendered row contains %q", substr)
	return 0
}

// findScreenCellX returns the screen column row y's own rendered text
// starts substr at, or -1 if t is nil (findScreenRow's own probing
// call) — otherwise fails the test outright, since every caller past
// findScreenRow itself already knows the row must contain its own
// target text. Cell-by-cell, not a byte offset into one concatenated
// string: this app's own glyphs (✔, ○, ⭯, …) are multi-byte UTF-8, which
// would silently misalign a byte offset against the screen's own,
// one-cell-per-column coordinates.
func findScreenCellX(t *testing.T, screen tcell.SimulationScreen, width, y int, substr string) int {
	for x := 0; x < width; x++ {
		matched := true
		cursor := x
		for _, wantRune := range substr {
			str, _, _ := screen.Get(cursor, y)
			got := []rune(str)
			if len(got) == 0 || got[0] != wantRune {
				matched = false
				break
			}
			cursor++
		}
		if matched {
			return x
		}
	}
	if t == nil {
		return -1
	}
	t.Fatalf("row %d has no cell starting with %q", y, substr)
	return -1
}

// TestSSHKeysReloadIntoAnErrorNeverHangsOnDown pins the same real,
// reported freeze TestFirewallReloadIntoAnErrorNeverHangsOnDown pins for
// the Firewall screen — see that test's own doc comment and
// renderFirewall's own doc comment on its error branch's Select call for
// the mechanism and the fix, followed identically here via
// showTablePlaceholder/enableTableSelection.
func TestSSHKeysReloadIntoAnErrorNeverHangsOnDown(t *testing.T) {
	r, err := NewRoot(tview.NewApplication(), fixtureDir(t))
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.sshKeysErr = errors.New("permission denied")
	r.sshKeysPairs = nil
	r.renderSSHKeys()
	r.sshKeysTable.Select(15, 0) // simulate a stale cursor from a much longer previous list

	r.renderSSHKeys() // still errors — re-renders down to just 2 rows

	done := make(chan struct{})
	go func() {
		r.sshKeysTable.InputHandler()(tcell.NewEventKey(tcell.KeyDown, 0, tcell.ModNone), func(tview.Primitive) {})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Down key handling hung after reloading into an error")
	}
}
