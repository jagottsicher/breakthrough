package ui

import (
	"errors"
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/jagottsicher/breakthrough/internal/firewall"
)

// isolateFirewallRead swaps readFirewallSnapshot/loadFirewallServices for
// the duration of one test, restoring the originals via t.Cleanup — the
// same "swap the package-level var, restore in Cleanup" idiom used
// throughout this package and internal/firewall's own detect_test.go, so
// these tests never touch a real system's actual firewall state.
func isolateFirewallRead(t *testing.T, snapshot firewall.Snapshot, err error) {
	t.Helper()
	origSnapshot, origServices := readFirewallSnapshot, loadFirewallServices
	t.Cleanup(func() {
		readFirewallSnapshot, loadFirewallServices = origSnapshot, origServices
	})
	readFirewallSnapshot = func() (firewall.Snapshot, error) { return snapshot, err }
	loadFirewallServices = func() firewall.ServiceLookup { return firewall.ServiceLookup{} }
}

// TestOpenFirewallShowsThePage pins openFirewall's own basic contract.
func TestOpenFirewallShowsThePage(t *testing.T) {
	r, err := NewRoot(tview.NewApplication(), fixtureDir(t))
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	isolateFirewallRead(t, firewall.Snapshot{Backend: firewall.BackendNone}, nil)

	r.openFirewall()

	if r.activePage != firewallPage {
		t.Fatalf("activePage = %q, want the Firewall screen", r.activePage)
	}
}

// TestCaptureFirewallKeyEscapeClosesTheScreen mirrors the Mounts screen's
// own equivalent test.
func TestCaptureFirewallKeyEscapeClosesTheScreen(t *testing.T) {
	r, err := NewRoot(tview.NewApplication(), fixtureDir(t))
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	isolateFirewallRead(t, firewall.Snapshot{Backend: firewall.BackendNone}, nil)
	r.openFirewall()

	if got := r.captureFirewallKey(tcell.NewEventKey(tcell.KeyEscape, 0, tcell.ModNone)); got != nil {
		t.Error("captureFirewallKey should consume Escape")
	}
	if r.activePage == firewallPage {
		t.Error("Escape should have closed the Firewall screen")
	}
}

// TestCaptureFirewallKeyRRefreshesWithoutClosing pins that "r" is
// consumed and re-reads the firewall snapshot (via reloadFirewall)
// without closing the screen.
func TestCaptureFirewallKeyRRefreshesWithoutClosing(t *testing.T) {
	r, err := NewRoot(tview.NewApplication(), fixtureDir(t))
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	isolateFirewallRead(t, firewall.Snapshot{Backend: firewall.BackendNone}, nil)
	r.openFirewall()

	if got := r.captureFirewallKey(tcell.NewEventKey(tcell.KeyRune, 'r', tcell.ModNone)); got != nil {
		t.Error("captureFirewallKey should consume \"r\"")
	}
	if r.activePage != firewallPage {
		t.Error("\"r\" should refresh in place, not close the screen")
	}
}

// TestRenderFirewallShowsTheErrorInPlaceOfRows pins that a failed read is
// reported on screen rather than leaving the table looking like an
// empty, still-successful read.
func TestRenderFirewallShowsTheErrorInPlaceOfRows(t *testing.T) {
	r, err := NewRoot(tview.NewApplication(), fixtureDir(t))
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.firewallErr = errors.New("nft: command not found")
	r.firewallSnapshot = firewall.Snapshot{}

	r.renderFirewall()

	got := strings.TrimSpace(r.firewallTable.GetCell(1, firewallColOrder).Text)
	if !strings.Contains(got, "not found") {
		t.Errorf("row 1 = %q, want it to show the read error", got)
	}
}

// TestRenderFirewallShowsNoBackendMessage pins that having no supported
// backend active at all is reported explicitly rather than looking like
// an ordinary, empty "no rules" read.
func TestRenderFirewallShowsNoBackendMessage(t *testing.T) {
	r, err := NewRoot(tview.NewApplication(), fixtureDir(t))
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.firewallErr = nil
	r.firewallSnapshot = firewall.Snapshot{Backend: firewall.BackendNone}

	r.renderFirewall()

	got := strings.TrimSpace(r.firewallTable.GetCell(1, firewallColOrder).Text)
	if !strings.Contains(got, "No supported firewall backend") {
		t.Errorf("row 1 = %q, want the no-backend message", got)
	}
}

// TestRenderFirewallColorsAllowAndDenyRowsApart pins the one piece of
// this whole screen a quick scan depends on most: an ALLOW rule and a
// DENY/REJECT rule need to look visually distinct from each other.
func TestRenderFirewallColorsAllowAndDenyRowsApart(t *testing.T) {
	r, err := NewRoot(tview.NewApplication(), fixtureDir(t))
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.firewallErr = nil
	r.firewallSnapshot = firewall.Snapshot{
		Backend: firewall.BackendUFW,
		Rules: []firewall.Rule{
			{Order: 1, Direction: firewall.DirectionIn, Action: firewall.ActionAllow, Protocol: "tcp", PortFrom: 22, PortTo: 22},
			{Order: 2, Direction: firewall.DirectionIn, Action: firewall.ActionDeny, Protocol: "tcp", PortFrom: 23, PortTo: 23},
		},
	}

	r.renderFirewall()

	// Row 1 is the "Incoming" section label; the rules start at row 2.
	allowColor := cellTextColor(r.firewallTable.GetCell(2, firewallColAction))
	denyColor := cellTextColor(r.firewallTable.GetCell(3, firewallColAction))

	if allowColor != r.theme.EntryExecutable {
		t.Errorf("ALLOW row's own action color = %v, want EntryExecutable %v", allowColor, r.theme.EntryExecutable)
	}
	if denyColor != r.theme.CriticalText {
		t.Errorf("DENY row's own action color = %v, want CriticalText %v", denyColor, r.theme.CriticalText)
	}
}

// TestRenderFirewallMutesAShadowedRuleAndNotesWhy pins that a rule that
// can never actually fire (see firewall.AnnotateShadows) is shown
// visually distinct from one that does, with a note explaining why.
func TestRenderFirewallMutesAShadowedRuleAndNotesWhy(t *testing.T) {
	r, err := NewRoot(tview.NewApplication(), fixtureDir(t))
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.firewallErr = nil
	r.firewallSnapshot = firewall.Snapshot{
		Backend: firewall.BackendUFW,
		Rules: []firewall.Rule{
			{Order: 1, Direction: firewall.DirectionIn, Action: firewall.ActionAllow},
			{Order: 2, Direction: firewall.DirectionIn, Action: firewall.ActionDeny, Protocol: "tcp", PortFrom: 22, PortTo: 22,
				ShadowedByOrder: 1, ShadowedByAction: firewall.ActionAllow},
		},
	}

	r.renderFirewall()

	// Row 1 is the "Incoming" section label; the shadowed rule is the
	// second rule, at row 3.
	gotColor := cellTextColor(r.firewallTable.GetCell(3, firewallColAction))
	if gotColor != r.theme.MutedTextColor {
		t.Errorf("shadowed row's own action color = %v, want MutedTextColor %v", gotColor, r.theme.MutedTextColor)
	}
	note := r.firewallTable.GetCell(3, firewallColNote).Text
	if !strings.Contains(note, "shadowed by #1") {
		t.Errorf("shadowed row's own note = %q, want it to mention \"shadowed by #1\"", note)
	}
}

// TestRenderFirewallSelectsTheFirstRuleRowOnFirstRender pins that the
// cursor never starts on the header row or a section-label row, neither
// of which Enter/click can meaningfully do anything with.
func TestRenderFirewallSelectsTheFirstRuleRowOnFirstRender(t *testing.T) {
	r, err := NewRoot(tview.NewApplication(), fixtureDir(t))
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.firewallErr = nil
	r.firewallSnapshot = firewall.Snapshot{
		Backend: firewall.BackendUFW,
		Rules: []firewall.Rule{
			{Order: 1, Direction: firewall.DirectionIn, Action: firewall.ActionAllow, Protocol: "tcp", PortFrom: 22, PortTo: 22},
		},
	}

	r.renderFirewall()

	row, _ := r.firewallTable.GetSelection()
	if row != 2 {
		t.Errorf("selected row = %d, want 2 (the first real rule row, past the row-0 header and row-1 section label)", row)
	}
}
