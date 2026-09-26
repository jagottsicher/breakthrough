package ui

import (
	"strings"
	"testing"

	"github.com/rivo/tview"

	"github.com/jagottsicher/breakthrough/internal/firewall"
)

func TestOpenFirewallSimulateShowsTheForm(t *testing.T) {
	r := newFirewallAddRuleTestRoot(t, firewall.BackendUFW)

	r.openFirewallSimulate()

	if r.activePage != firewallSimulatePage {
		t.Fatalf("activePage = %q, want the Simulate form", r.activePage)
	}
}

func TestOpenFirewallSimulateWorksWithoutAnActiveBackend(t *testing.T) {
	// Unlike openFirewallAddRule, Simulate never builds a backend
	// command, so there's nothing here for BackendNone (or nftables) to
	// refuse — see this file's own doc comment.
	r := newFirewallAddRuleTestRoot(t, firewall.BackendNone)

	r.openFirewallSimulate()

	if r.activePage != firewallSimulatePage {
		t.Errorf("activePage = %q, want the Simulate form to open even with no active backend", r.activePage)
	}
}

func TestOpenFirewallSimulateResetsToHarmlessDefaults(t *testing.T) {
	r := newFirewallAddRuleTestRoot(t, firewall.BackendUFW)
	r.firewallSimulatePortText = "9999"
	r.firewallSimulateResult.SetText("stale result from a previous open")

	r.openFirewallSimulate()

	if r.firewallSimulateDirection != firewall.DirectionIn {
		t.Errorf("Direction = %v, want DirectionIn (harmless default)", r.firewallSimulateDirection)
	}
	if r.firewallSimulatePortText != "" {
		t.Errorf("PortText = %q, want blank (reset on every open, never sticky)", r.firewallSimulatePortText)
	}
	if got := r.firewallSimulateResult.GetText(true); got != "" {
		t.Errorf("result = %q, want cleared on every open", got)
	}
}

func TestRunFirewallSimulateReportsTheMatchedRule(t *testing.T) {
	snapshot := firewall.Snapshot{
		Backend: firewall.BackendUFW,
		Rules: []firewall.Rule{
			{Order: 1, Direction: firewall.DirectionIn, Action: firewall.ActionAllow, Protocol: "tcp", PortFrom: 22, PortTo: 22, Backend: firewall.BackendUFW, Chain: "ufw-user-input"},
			{Order: 2, Direction: firewall.DirectionIn, Action: firewall.ActionDeny},
		},
	}
	r, err := NewRoot(tview.NewApplication(), fixtureDir(t))
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	isolateFirewallRead(t, snapshot, nil)
	r.openFirewall()
	r.openFirewallSimulate()
	r.firewallSimulateProtocol = "tcp"
	r.firewallSimulatePortText = "22"

	r.runFirewallSimulate()

	got := r.firewallSimulateResult.GetText(true)
	if !strings.Contains(got, "#1") || !strings.Contains(got, "ALLOW") {
		t.Errorf("result = %q, want it to report rule #1 (ALLOW)", got)
	}
}

func TestRunFirewallSimulateReportsNoMatch(t *testing.T) {
	snapshot := firewall.Snapshot{
		Backend: firewall.BackendUFW,
		Rules: []firewall.Rule{
			{Order: 1, Direction: firewall.DirectionIn, Action: firewall.ActionAllow, Protocol: "tcp", PortFrom: 80, PortTo: 80},
		},
	}
	r, err := NewRoot(tview.NewApplication(), fixtureDir(t))
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	isolateFirewallRead(t, snapshot, nil)
	r.openFirewall()
	r.openFirewallSimulate()
	r.firewallSimulateProtocol = "tcp"
	r.firewallSimulatePortText = "22"

	r.runFirewallSimulate()

	got := r.firewallSimulateResult.GetText(true)
	if !strings.Contains(got, "No rule matches") || !strings.Contains(got, string(firewall.BackendUFW)) {
		t.Errorf("result = %q, want a \"no rule matches\" message naming the active backend", got)
	}
}

func TestRunFirewallSimulateReportsNoActiveBackendDistinctly(t *testing.T) {
	r := newFirewallAddRuleTestRoot(t, firewall.BackendNone)
	r.openFirewallSimulate()

	r.runFirewallSimulate()

	got := r.firewallSimulateResult.GetText(true)
	if !strings.Contains(got, "No active firewall backend") {
		t.Errorf("result = %q, want a message about there being no active backend at all", got)
	}
}

func TestRunFirewallSimulateReportsAMalformedPortInPlace(t *testing.T) {
	r := newFirewallAddRuleTestRoot(t, firewall.BackendUFW)
	r.openFirewallSimulate()
	r.firewallSimulatePortText = "not-a-port"

	r.runFirewallSimulate()

	if r.activePage != firewallSimulatePage {
		t.Errorf("activePage = %q, want to stay on the Simulate form after a validation error", r.activePage)
	}
	if got := r.firewallSimulateResult.GetText(true); got == "" {
		t.Error("result is empty, want the parse error reported in place")
	}
}

func TestRunFirewallSimulateIgnoresQueryDirectionMismatch(t *testing.T) {
	snapshot := firewall.Snapshot{
		Backend: firewall.BackendUFW,
		Rules: []firewall.Rule{
			{Order: 1, Direction: firewall.DirectionOut, Action: firewall.ActionAllow},
		},
	}
	r, err := NewRoot(tview.NewApplication(), fixtureDir(t))
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	isolateFirewallRead(t, snapshot, nil)
	r.openFirewall()
	r.openFirewallSimulate()
	r.firewallSimulateDirection = firewall.DirectionIn // default, but explicit here for clarity

	r.runFirewallSimulate()

	got := r.firewallSimulateResult.GetText(true)
	if !strings.Contains(got, "No rule matches") {
		t.Errorf("result = %q, want no match — the only rule is Outgoing", got)
	}
}

func TestCloseFirewallSimulateReturnsToTheFirewallScreen(t *testing.T) {
	r := newFirewallAddRuleTestRoot(t, firewall.BackendUFW)
	r.openFirewallSimulate()

	r.closeFirewallSimulate()

	if r.activePage != firewallPage {
		t.Errorf("activePage = %q, want back to the Firewall screen underneath", r.activePage)
	}
}
