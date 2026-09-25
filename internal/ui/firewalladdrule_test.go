package ui

import (
	"strings"
	"testing"

	"github.com/rivo/tview"

	"github.com/jagottsicher/breakthrough/internal/activitylog"
	"github.com/jagottsicher/breakthrough/internal/firewall"
)

func TestParseFirewallPortFieldBlankMeansAnyPort(t *testing.T) {
	from, to, err := parseFirewallPortField("")
	if err != nil || from != 0 || to != 0 {
		t.Errorf("parseFirewallPortField(\"\") = (%d, %d, %v), want (0, 0, nil)", from, to, err)
	}
}

func TestParseFirewallPortFieldSinglePort(t *testing.T) {
	from, to, err := parseFirewallPortField(" 22 ")
	if err != nil || from != 22 || to != 22 {
		t.Errorf("parseFirewallPortField(\" 22 \") = (%d, %d, %v), want (22, 22, nil)", from, to, err)
	}
}

func TestParseFirewallPortFieldRange(t *testing.T) {
	from, to, err := parseFirewallPortField("6000-6063")
	if err != nil || from != 6000 || to != 6063 {
		t.Errorf("parseFirewallPortField(\"6000-6063\") = (%d, %d, %v), want (6000, 6063, nil)", from, to, err)
	}
}

func TestParseFirewallPortFieldRefusesGarbage(t *testing.T) {
	if _, _, err := parseFirewallPortField("ssh"); err == nil {
		t.Error("parseFirewallPortField(\"ssh\") = nil error, want a parse error")
	}
	if _, _, err := parseFirewallPortField("80-"); err == nil {
		t.Error("parseFirewallPortField(\"80-\") = nil error, want a parse error")
	}
}

// newFirewallAddRuleTestRoot builds a Root with the Firewall screen's
// own snapshot already set to backend, the same "isolate the real
// system read, feed a fixed Snapshot instead" idiom
// isolateFirewallRead's own doc comment establishes for firewall_test.go.
func newFirewallAddRuleTestRoot(t *testing.T, backend firewall.Backend) *Root {
	t.Helper()
	r, err := NewRoot(tview.NewApplication(), fixtureDir(t))
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	isolateFirewallRead(t, firewall.Snapshot{Backend: backend}, nil)
	r.openFirewall()
	return r
}

func TestOpenFirewallAddRuleRefusesWhenNoBackendActive(t *testing.T) {
	r := newFirewallAddRuleTestRoot(t, firewall.BackendNone)

	r.openFirewallAddRule()

	if r.activePage == firewallAddRulePage {
		t.Error("openFirewallAddRule opened the form with no active backend, want a refusal instead")
	}
	if r.activePage != errorPage {
		t.Errorf("activePage = %q, want the error overlay", r.activePage)
	}
}

func TestOpenFirewallAddRuleRefusesNFTables(t *testing.T) {
	r := newFirewallAddRuleTestRoot(t, firewall.BackendNFTables)

	r.openFirewallAddRule()

	if r.activePage == firewallAddRulePage {
		t.Error("openFirewallAddRule opened the form for nftables, want a refusal instead")
	}
	if r.activePage != errorPage {
		t.Errorf("activePage = %q, want the error overlay", r.activePage)
	}
	if got := r.errorView.GetText(true); !strings.Contains(got, "nftables") {
		t.Errorf("error text = %q, want it to mention nftables", got)
	}
}

func TestOpenFirewallAddRuleResetsToHarmlessDefaults(t *testing.T) {
	r := newFirewallAddRuleTestRoot(t, firewall.BackendUFW)
	r.firewallAddRulePortText = "9999"
	r.firewallAddRuleAction = firewall.ActionDeny

	r.openFirewallAddRule()

	if r.activePage != firewallAddRulePage {
		t.Fatalf("activePage = %q, want the Add rule form", r.activePage)
	}
	if r.firewallAddRuleDirection != firewall.DirectionIn {
		t.Errorf("Direction = %v, want DirectionIn (harmless default)", r.firewallAddRuleDirection)
	}
	if r.firewallAddRuleAction != firewall.ActionAllow {
		t.Errorf("Action = %v, want ActionAllow (harmless default)", r.firewallAddRuleAction)
	}
	if r.firewallAddRulePortText != "" {
		t.Errorf("PortText = %q, want blank (reset on every open, never sticky)", r.firewallAddRulePortText)
	}
}

func TestSubmitFirewallAddRuleShowsConfirmWithTheExactCommand(t *testing.T) {
	r := newFirewallAddRuleTestRoot(t, firewall.BackendUFW)
	r.openFirewallAddRule()
	r.firewallAddRulePortText = "22"
	r.firewallAddRuleProtocol = "tcp"

	r.submitFirewallAddRule()

	if r.activePage != confirmPage {
		t.Fatalf("activePage = %q, want the confirm dialog", r.activePage)
	}
	want, err := firewall.UFWAddRuleCommand(firewall.NewRuleSpec{
		Direction: firewall.DirectionIn, Action: firewall.ActionAllow,
		Protocol: "tcp", PortFrom: 22, PortTo: 22,
	})
	if err != nil {
		t.Fatalf("UFWAddRuleCommand: %v", err)
	}
	if got := r.confirmDialogTitleBar.GetText(true); !strings.Contains(got, want) {
		t.Errorf("confirm text = %q, want it to contain the exact command %q", got, want)
	}
}

func TestSubmitFirewallAddRuleReportsAnInvalidPortInPlace(t *testing.T) {
	r := newFirewallAddRuleTestRoot(t, firewall.BackendUFW)
	r.openFirewallAddRule()
	r.firewallAddRulePortText = "not-a-port"

	r.submitFirewallAddRule()

	if r.activePage != firewallAddRulePage {
		t.Errorf("activePage = %q, want to stay on the Add rule form after a validation error", r.activePage)
	}
	if got := r.firewallAddRuleStatus.GetText(true); got == "" {
		t.Error("firewallAddRuleStatus is empty, want the parse error reported in place")
	}
}

func TestApplyFirewallAddRuleLogsActionAndReloadsSnapshot(t *testing.T) {
	r := newFirewallAddRuleTestRoot(t, firewall.BackendUFW)
	r.openFirewallAddRule()
	readLog := attachTestActivityLog(t, r)

	spec := firewall.NewRuleSpec{Direction: firewall.DirectionIn, Action: firewall.ActionAllow, PortFrom: 80, PortTo: 80}
	command, err := firewall.UFWAddRuleCommand(spec)
	if err != nil {
		t.Fatalf("UFWAddRuleCommand: %v", err)
	}

	// r.app.Suspend is a no-op without a real terminal (Application.Run
	// was never called — see tview's own Application.Suspend, which
	// bails out whenever its screen is still nil), the same acknowledged
	// limitation TestRunBashCommandLogsAnAction's own doc comment
	// already notes for runShellCommandFullScreen — so this exercises
	// applyFirewallAddRule's own outcome handling, never a real sudo
	// invocation.
	r.applyFirewallAddRule(firewall.BackendUFW, spec, command)

	if r.activePage == firewallAddRulePage {
		t.Error("applyFirewallAddRule left the Add rule form open, want it closed")
	}
	got := readLog()
	if !strings.Contains(got, string(activitylog.CategoryFirewall)) || !strings.Contains(got, command) {
		t.Errorf("log = %q, want a firewall entry mentioning %q", got, command)
	}
}

func TestApplyFirewallAddRuleArmsRollbackWhenSSHRelevantOverSSH(t *testing.T) {
	t.Setenv("SSH_CONNECTION", "10.0.0.1 22 10.0.0.2 22")
	t.Setenv("SSH_TTY", "")

	r := newFirewallAddRuleTestRoot(t, firewall.BackendUFW)
	r.openFirewallAddRule()
	attachTestActivityLog(t, r)

	spec := firewall.NewRuleSpec{Direction: firewall.DirectionIn, Action: firewall.ActionDeny, PortFrom: 22, PortTo: 22}
	command, err := firewall.UFWAddRuleCommand(spec)
	if err != nil {
		t.Fatalf("UFWAddRuleCommand: %v", err)
	}

	r.applyFirewallAddRule(firewall.BackendUFW, spec, command)
	t.Cleanup(func() {
		if r.firewallRollbackTimer != nil {
			r.firewallRollbackTimer.Stop()
		}
		if r.firewallRollbackCancel != nil {
			r.firewallRollbackCancel()
		}
	})

	if r.firewallRollbackTimer == nil {
		t.Fatal("firewallRollbackTimer is nil, want an armed rollback for an SSH-relevant Deny rule applied over SSH")
	}
	if r.activePage != firewallRollbackPage {
		t.Errorf("activePage = %q, want the rollback countdown overlay", r.activePage)
	}
}

func TestApplyFirewallAddRuleDoesNotArmRollbackWithoutSSH(t *testing.T) {
	t.Setenv("SSH_CONNECTION", "")
	t.Setenv("SSH_TTY", "")

	r := newFirewallAddRuleTestRoot(t, firewall.BackendUFW)
	r.openFirewallAddRule()
	attachTestActivityLog(t, r)

	spec := firewall.NewRuleSpec{Direction: firewall.DirectionIn, Action: firewall.ActionDeny, PortFrom: 22, PortTo: 22}
	command, err := firewall.UFWAddRuleCommand(spec)
	if err != nil {
		t.Fatalf("UFWAddRuleCommand: %v", err)
	}

	r.applyFirewallAddRule(firewall.BackendUFW, spec, command)

	if r.firewallRollbackTimer != nil {
		t.Error("firewallRollbackTimer armed without an active SSH session, want it left nil")
	}
}

func TestApplyFirewallAddRuleDoesNotArmRollbackForAllow(t *testing.T) {
	t.Setenv("SSH_CONNECTION", "10.0.0.1 22 10.0.0.2 22")
	t.Setenv("SSH_TTY", "")

	r := newFirewallAddRuleTestRoot(t, firewall.BackendUFW)
	r.openFirewallAddRule()
	attachTestActivityLog(t, r)

	// An Allow rule can never itself cut off an existing session — only
	// Deny/Reject can (see applyFirewallAddRule's own Action check).
	spec := firewall.NewRuleSpec{Direction: firewall.DirectionIn, Action: firewall.ActionAllow, PortFrom: 22, PortTo: 22}
	command, err := firewall.UFWAddRuleCommand(spec)
	if err != nil {
		t.Fatalf("UFWAddRuleCommand: %v", err)
	}

	r.applyFirewallAddRule(firewall.BackendUFW, spec, command)

	if r.firewallRollbackTimer != nil {
		t.Error("firewallRollbackTimer armed for an Allow rule, want it left nil")
	}
}

func TestKeepFirewallRuleStopsTheTimerAndLogs(t *testing.T) {
	t.Setenv("SSH_CONNECTION", "10.0.0.1 22 10.0.0.2 22")
	t.Setenv("SSH_TTY", "")

	r := newFirewallAddRuleTestRoot(t, firewall.BackendUFW)
	r.openFirewallAddRule()
	readLog := attachTestActivityLog(t, r)

	spec := firewall.NewRuleSpec{Direction: firewall.DirectionIn, Action: firewall.ActionDeny, PortFrom: 22, PortTo: 22}
	command, _ := firewall.UFWAddRuleCommand(spec)
	r.applyFirewallAddRule(firewall.BackendUFW, spec, command)
	if r.firewallRollbackTimer == nil {
		t.Fatal("rollback not armed, precondition for this test failed")
	}

	r.keepFirewallRule()

	if r.firewallRollbackTimer != nil {
		t.Error("keepFirewallRule left firewallRollbackTimer set, want it cleared")
	}
	if r.activePage == firewallRollbackPage {
		t.Error("keepFirewallRule left the countdown overlay open")
	}
	if got := readLog(); !strings.Contains(got, "kept") {
		t.Errorf("log = %q, want an entry noting the rule was kept", got)
	}
}

func TestRollbackFirewallRuleRunsDeleteAndReportsIt(t *testing.T) {
	t.Setenv("SSH_CONNECTION", "10.0.0.1 22 10.0.0.2 22")
	t.Setenv("SSH_TTY", "")

	r := newFirewallAddRuleTestRoot(t, firewall.BackendUFW)
	r.openFirewallAddRule()
	readLog := attachTestActivityLog(t, r)

	spec := firewall.NewRuleSpec{Direction: firewall.DirectionIn, Action: firewall.ActionDeny, PortFrom: 22, PortTo: 22}
	command, _ := firewall.UFWAddRuleCommand(spec)
	r.applyFirewallAddRule(firewall.BackendUFW, spec, command)
	if r.firewallRollbackTimer == nil {
		t.Fatal("rollback not armed, precondition for this test failed")
	}
	r.firewallRollbackTimer.Stop() // don't race the real timer — call the same action it would fire directly

	r.rollbackFirewallRule()

	want, err := firewall.UFWDeleteRuleCommand(spec)
	if err != nil {
		t.Fatalf("UFWDeleteRuleCommand: %v", err)
	}
	got := readLog()
	if !strings.Contains(got, string(activitylog.CategoryFirewall)) || !strings.Contains(got, want) {
		t.Errorf("log = %q, want a firewall entry mentioning the rollback command %q", got, want)
	}
	if r.activePage != errorPage {
		t.Errorf("activePage = %q, want the reverted-rule notice", r.activePage)
	}
}
