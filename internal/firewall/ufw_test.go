package firewall

import (
	"strings"
	"testing"
)

func TestUFWActive(t *testing.T) {
	if !UFWActive("Status: active\n\nTo  Action  From\n") {
		t.Error("Status: active should report active")
	}
	if UFWActive("Status: inactive\n") {
		t.Error("Status: inactive should not report active")
	}
	if UFWActive("") {
		t.Error("empty output should not report active")
	}
}

// realUFWStatusVerbose is confirmed against real `ufw status verbose`
// output: a header block (Status/Logging/Default/New profiles), then a
// blank line, then the "To / Action / From" table with its own
// "--  ------  ----" separator row, IPv6 twins marked "(v6)", a
// comma-grouped multi-port rule, and an interface-scoped rule.
const realUFWStatusVerbose = `Status: active
Logging: on (low)
Default: deny (incoming), allow (outgoing), disabled (routed)
New profiles: skip

To                         Action      From
--                         ------      ----
22/tcp                     ALLOW IN    Anywhere
22/tcp (v6)                ALLOW IN    Anywhere (v6)
80,443/tcp                 ALLOW IN    Anywhere
192.168.1.0/24             DENY IN     Anywhere
Anywhere on eth0           ALLOW IN    203.0.113.5
Anywhere                   ALLOW OUT   Anywhere
`

func TestParseUFWStatusVerbose(t *testing.T) {
	rules, err := ParseUFWStatusVerbose(realUFWStatusVerbose)
	if err != nil {
		t.Fatalf("ParseUFWStatusVerbose: %v", err)
	}
	// 1 (22/tcp) + 1 (22/tcp v6) + 2 (80,443/tcp) + 1 (192.168.1.0/24 deny)
	// + 1 (eth0-scoped) + 1 (outbound any) = 7
	if len(rules) != 7 {
		t.Fatalf("got %d rules, want 7: %+v", len(rules), rules)
	}

	ssh := rules[0]
	if ssh.PortFrom != 22 || ssh.PortTo != 22 || ssh.Protocol != "tcp" {
		t.Errorf("rule[0] port = %d-%d/%s, want 22-22/tcp", ssh.PortFrom, ssh.PortTo, ssh.Protocol)
	}
	if ssh.Action != ActionAllow || ssh.Direction != DirectionIn {
		t.Errorf("rule[0] action/direction = %v/%v, want ALLOW/IN", ssh.Action, ssh.Direction)
	}
	if ssh.Source != "" {
		t.Errorf("rule[0] source = %q, want empty (Anywhere)", ssh.Source)
	}

	sshV6 := rules[1]
	if sshV6.PortFrom != 22 {
		t.Errorf("rule[1] (v6 twin) port = %d, want 22", sshV6.PortFrom)
	}

	http := rules[2]
	if http.PortFrom != 80 || http.Protocol != "tcp" {
		t.Errorf("rule[2] = %+v, want port 80/tcp", http)
	}
	https := rules[3]
	if https.PortFrom != 443 || https.Protocol != "tcp" {
		t.Errorf("rule[3] = %+v, want port 443/tcp", https)
	}

	deny := rules[4]
	if deny.Action != ActionDeny || deny.Destination != "192.168.1.0/24" {
		t.Errorf("rule[4] = %+v, want DENY to 192.168.1.0/24", deny)
	}

	iface := rules[5]
	if iface.Interface != "eth0" || iface.Source != "203.0.113.5" {
		t.Errorf("rule[5] = %+v, want interface eth0, source 203.0.113.5", iface)
	}

	out := rules[6]
	if out.Direction != DirectionOut {
		t.Errorf("rule[6] direction = %v, want OUT", out.Direction)
	}

	for i, r := range rules {
		if r.Order != i+1 {
			t.Errorf("rule[%d].Order = %d, want %d", i, r.Order, i+1)
		}
	}
}

func TestParseUFWStatusVerboseInactiveHasNoRules(t *testing.T) {
	rules, err := ParseUFWStatusVerbose("Status: inactive\n")
	if err != nil {
		t.Fatalf("ParseUFWStatusVerbose: %v", err)
	}
	if rules != nil {
		t.Errorf("got %v, want nil for an inactive/ruleless status", rules)
	}
}

func TestParsePortList(t *testing.T) {
	cases := []struct {
		in        string
		wantPorts []int
		wantProto string
		wantOK    bool
	}{
		{"22/tcp", []int{22}, "tcp", true},
		{"22", []int{22}, "", true},
		{"80,443/tcp", []int{80, 443}, "tcp", true},
		{"not-a-port", nil, "", false},
	}
	for _, c := range cases {
		ports, proto, ok := parsePortList(c.in)
		if ok != c.wantOK || proto != c.wantProto || !intSliceEqual(ports, c.wantPorts) {
			t.Errorf("parsePortList(%q) = %v, %q, %v; want %v, %q, %v",
				c.in, ports, proto, ok, c.wantPorts, c.wantProto, c.wantOK)
		}
	}
}

func intSliceEqual(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestUFWAddRuleCommandBuildsTheRealInvocation pins the exact command
// line a fully-specified rule turns into — a real, reproducible bug
// here would either silently do nothing (a syntax error `ufw` itself
// rejects) or, worse, apply a rule broader or narrower than what the
// form actually asked for.
func TestUFWAddRuleCommandBuildsTheRealInvocation(t *testing.T) {
	spec := NewRuleSpec{
		Direction: DirectionIn, Action: ActionDeny,
		Protocol: "tcp", PortFrom: 22, PortTo: 22,
		Source: "203.0.113.0/24", Interface: "eth0",
	}
	got, err := UFWAddRuleCommand(spec)
	if err != nil {
		t.Fatalf("UFWAddRuleCommand: %v", err)
	}
	want := "ufw deny in on eth0 from 203.0.113.0/24 to any port 22 proto tcp"
	if got != want {
		t.Errorf("UFWAddRuleCommand = %q, want %q", got, want)
	}
}

// TestUFWAddRuleCommandDefaultsEverythingUnsetToAny pins the minimal
// case: nothing but Direction/Action set, source/destination/interface/
// protocol/port all left at their own zero value.
func TestUFWAddRuleCommandDefaultsEverythingUnsetToAny(t *testing.T) {
	spec := NewRuleSpec{Direction: DirectionOut, Action: ActionAllow}
	got, err := UFWAddRuleCommand(spec)
	if err != nil {
		t.Fatalf("UFWAddRuleCommand: %v", err)
	}
	want := "ufw allow out from any to any"
	if got != want {
		t.Errorf("UFWAddRuleCommand = %q, want %q", got, want)
	}
}

// TestUFWAddRuleCommandRendersAPortRangeWithADash pins the one place
// ufw's own port-range notation differs from iptables' (see
// TestIPTablesAddRuleCommandRendersAPortRangeWithAColon).
func TestUFWAddRuleCommandRendersAPortRangeWithADash(t *testing.T) {
	spec := NewRuleSpec{PortFrom: 6000, PortTo: 6063}
	got, err := UFWAddRuleCommand(spec)
	if err != nil {
		t.Fatalf("UFWAddRuleCommand: %v", err)
	}
	if !strings.Contains(got, "port 6000-6063") {
		t.Errorf("UFWAddRuleCommand = %q, want it to contain %q", got, "port 6000-6063")
	}
}

// TestUFWAddRuleCommandRefusesAnInvalidPortRange pins that this never
// reaches a real shell command for a spec NewRuleSpec.Validate itself
// already rejects.
func TestUFWAddRuleCommandRefusesAnInvalidPortRange(t *testing.T) {
	spec := NewRuleSpec{PortFrom: 100, PortTo: 50}
	if _, err := UFWAddRuleCommand(spec); err == nil {
		t.Error("UFWAddRuleCommand should refuse an inverted port range")
	}
}

// TestUFWDeleteRuleCommandInsertsDeleteRightAfterUFW pins the
// rollback's own exact shape: ufw's real "delete" verb takes the same
// rule specification straight back, so this must be byte-for-byte
// UFWAddRuleCommand's own output with "delete " spliced in right after
// "ufw ", never a separately re-derived command line that could drift
// from what was actually applied.
func TestUFWDeleteRuleCommandInsertsDeleteRightAfterUFW(t *testing.T) {
	spec := NewRuleSpec{Direction: DirectionIn, Action: ActionAllow, PortFrom: 22, PortTo: 22, Protocol: "tcp"}
	add, err := UFWAddRuleCommand(spec)
	if err != nil {
		t.Fatalf("UFWAddRuleCommand: %v", err)
	}
	del, err := UFWDeleteRuleCommand(spec)
	if err != nil {
		t.Fatalf("UFWDeleteRuleCommand: %v", err)
	}
	want := strings.Replace(add, "ufw ", "ufw delete ", 1)
	if del != want {
		t.Errorf("UFWDeleteRuleCommand = %q, want %q", del, want)
	}
}
