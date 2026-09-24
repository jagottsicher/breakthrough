package firewall

import (
	"strings"
	"testing"
)

// realNFTRuleset is confirmed against real `nft -j list ruleset` output
// shape: a metainfo header (ignored), a table, base chains hooked to
// input/output (a forward-hooked chain must be ignored), rules combining a
// payload dport match, a payload saddr match, a meta iifname match, a
// dport range, a dport set (multi-port expansion), and a rule with only a
// counter and no verdict at all (must contribute nothing).
const realNFTRuleset = `{
  "nftables": [
    {"metainfo": {"version": "1.0.6"}},
    {"table": {"family": "inet", "name": "filter", "handle": 1}},
    {"chain": {"family": "inet", "table": "filter", "name": "input", "handle": 1, "type": "filter", "hook": "input", "prio": 0, "policy": "drop"}},
    {"chain": {"family": "inet", "table": "filter", "name": "output", "handle": 2, "type": "filter", "hook": "output", "prio": 0, "policy": "accept"}},
    {"chain": {"family": "inet", "table": "filter", "name": "forward", "handle": 3, "type": "filter", "hook": "forward", "prio": 0, "policy": "drop"}},
    {"rule": {"family": "inet", "table": "filter", "chain": "input", "handle": 10, "expr": [
      {"match": {"op": "==", "left": {"payload": {"protocol": "tcp", "field": "dport"}}, "right": 22}},
      {"accept": null}
    ]}},
    {"rule": {"family": "inet", "table": "filter", "chain": "input", "handle": 11, "expr": [
      {"match": {"op": "==", "left": {"payload": {"protocol": "ip", "field": "saddr"}}, "right": "192.168.1.0/24"}},
      {"match": {"op": "==", "left": {"payload": {"protocol": "tcp", "field": "dport"}}, "right": 80}},
      {"drop": null}
    ]}},
    {"rule": {"family": "inet", "table": "filter", "chain": "input", "handle": 12, "expr": [
      {"match": {"op": "==", "left": {"meta": {"key": "iifname"}}, "right": "eth0"}},
      {"match": {"op": "==", "left": {"payload": {"protocol": "tcp", "field": "dport"}}, "right": {"range": [6000, 6002]}}},
      {"accept": null}
    ]}},
    {"rule": {"family": "inet", "table": "filter", "chain": "input", "handle": 13, "expr": [
      {"match": {"op": "==", "left": {"payload": {"protocol": "tcp", "field": "dport"}}, "right": {"set": [8080, 8443]}}},
      {"reject": {"type": "icmpx", "expr": "port-unreachable"}}
    ]}},
    {"rule": {"family": "inet", "table": "filter", "chain": "input", "handle": 14, "expr": [
      {"counter": {"packets": 0, "bytes": 0}}
    ]}},
    {"rule": {"family": "inet", "table": "filter", "chain": "forward", "handle": 15, "expr": [
      {"accept": null}
    ]}},
    {"rule": {"family": "inet", "table": "filter", "chain": "output", "handle": 20, "expr": [
      {"match": {"op": "==", "left": {"payload": {"protocol": "ip", "field": "daddr"}}, "right": "10.0.0.1"}},
      {"match": {"op": "==", "left": {"payload": {"protocol": "udp", "field": "dport"}}, "right": 53}},
      {"accept": null}
    ]}}
  ]
}`

func TestParseNFTRuleset(t *testing.T) {
	rules, err := ParseNFTRuleset([]byte(realNFTRuleset))
	if err != nil {
		t.Fatalf("ParseNFTRuleset: %v", err)
	}
	// input: 1 (ssh) + 1 (saddr+dport drop) + 1 (dport range 6000-6002, kept
	// as one contiguous rule) + 2 (dport set 8080/8443, expanded one rule
	// per discrete value) = 5; the counter-only rule and the
	// forward-hooked rule contribute nothing. output: 1 (daddr+dport
	// accept)
	wantIn, wantOut := 0, 0
	for _, r := range rules {
		if r.Direction == DirectionIn {
			wantIn++
		} else {
			wantOut++
		}
	}
	if wantIn != 5 {
		t.Errorf("got %d IN rules, want 5: %+v", wantIn, rules)
	}
	if wantOut != 1 {
		t.Errorf("got %d OUT rules, want 1: %+v", wantOut, rules)
	}

	ssh := rules[0]
	if ssh.Protocol != "tcp" || ssh.PortFrom != 22 || ssh.PortTo != 22 || ssh.Action != ActionAllow {
		t.Errorf("rule[0] = %+v, want tcp/22/ALLOW", ssh)
	}
	if ssh.Backend != BackendNFTables {
		t.Errorf("rule[0].Backend = %q, want %q", ssh.Backend, BackendNFTables)
	}

	denied := rules[1]
	if denied.Source != "192.168.1.0/24" || denied.PortFrom != 80 || denied.Action != ActionDeny {
		t.Errorf("rule[1] = %+v, want source 192.168.1.0/24, port 80, DENY", denied)
	}

	rangeRule := rules[2]
	if rangeRule.PortFrom != 6000 || rangeRule.PortTo != 6002 || rangeRule.Interface != "eth0" {
		t.Errorf("range rule = %+v, want ports 6000-6002, interface eth0", rangeRule)
	}

	setRules := rules[3:5]
	gotPorts := map[int]bool{setRules[0].PortFrom: true, setRules[1].PortFrom: true}
	if !gotPorts[8080] || !gotPorts[8443] {
		t.Errorf("set-expanded rules = %+v, want ports 8080 and 8443", setRules)
	}
	for _, r := range setRules {
		if r.Action != ActionReject {
			t.Errorf("set-expanded rule %+v, want REJECT", r)
		}
	}

	out := rules[5]
	if out.Direction != DirectionOut || out.Destination != "10.0.0.1" || out.PortFrom != 53 {
		t.Errorf("OUT rule = %+v, want destination 10.0.0.1, port 53", out)
	}

	for i, r := range rules {
		wantOrder := i + 1
		if r.Direction == DirectionOut {
			wantOrder = 1
		}
		if r.Order != wantOrder {
			t.Errorf("rule[%d].Order = %d, want %d", i, r.Order, wantOrder)
		}
	}
}

func TestParseNFTRulesetEmptyRuleset(t *testing.T) {
	rules, err := ParseNFTRuleset([]byte(`{"nftables": [{"metainfo": {"version": "1.0.6"}}]}`))
	if err != nil {
		t.Fatalf("ParseNFTRuleset: %v", err)
	}
	if rules != nil {
		t.Errorf("got %v, want nil for a ruleset with no tables at all", rules)
	}
}

func TestParseNFTRulesetInvalidJSON(t *testing.T) {
	if _, err := ParseNFTRuleset([]byte("not json")); err == nil {
		t.Error("expected an error for invalid JSON, got nil")
	}
}

// TestNFTAddRuleCommandRefusesRatherThanGuess pins the deliberate
// scope limit: nftables has no well-known chain convention this
// package could safely target, so it explains why instead of building
// a command that might silently add a rule to a chain packets never
// traverse.
func TestNFTAddRuleCommandRefusesRatherThanGuess(t *testing.T) {
	_, err := NFTAddRuleCommand(NewRuleSpec{})
	if err == nil {
		t.Fatal("NFTAddRuleCommand should refuse rather than guess a table/chain")
	}
	if !strings.Contains(err.Error(), "nftables") {
		t.Errorf("error = %q, want it to name nftables specifically", err.Error())
	}
}
