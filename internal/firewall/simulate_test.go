package firewall

import "testing"

func TestSimulateFindsTheFirstMatchingRuleByOrder(t *testing.T) {
	rules := []Rule{
		{Order: 1, Direction: DirectionIn, Action: ActionAllow, Protocol: "tcp", PortFrom: 22, PortTo: 22},
		{Order: 2, Direction: DirectionIn, Action: ActionDeny},
	}
	spec := NewRuleSpec{Direction: DirectionIn, Protocol: "tcp", PortFrom: 22, PortTo: 22}

	got, ok := Simulate(rules, spec)
	if !ok {
		t.Fatal("Simulate should have matched a rule")
	}
	if got.Order != 1 || got.Action != ActionAllow {
		t.Errorf("got rule #%d (%v), want #1 (ALLOW)", got.Order, got.Action)
	}
}

func TestSimulateSkipsAnEarlierRuleThatDoesNotMatch(t *testing.T) {
	rules := []Rule{
		{Order: 1, Direction: DirectionIn, Action: ActionAllow, Protocol: "tcp", PortFrom: 80, PortTo: 80},
		{Order: 2, Direction: DirectionIn, Action: ActionDeny, Protocol: "tcp", PortFrom: 22, PortTo: 22},
	}
	spec := NewRuleSpec{Direction: DirectionIn, Protocol: "tcp", PortFrom: 22, PortTo: 22}

	got, ok := Simulate(rules, spec)
	if !ok {
		t.Fatal("Simulate should have matched rule #2")
	}
	if got.Order != 2 {
		t.Errorf("got rule #%d, want #2 (the only one that actually restricts port 22)", got.Order)
	}
}

func TestSimulateReturnsNotOkWhenNoRuleMatches(t *testing.T) {
	rules := []Rule{
		{Order: 1, Direction: DirectionIn, Action: ActionAllow, Protocol: "tcp", PortFrom: 80, PortTo: 80},
	}
	spec := NewRuleSpec{Direction: DirectionIn, Protocol: "tcp", PortFrom: 22, PortTo: 22}

	_, ok := Simulate(rules, spec)
	if ok {
		t.Error("Simulate should report no match — no rule restricts port 22")
	}
}

func TestSimulateIgnoresTheOppositeDirection(t *testing.T) {
	rules := []Rule{
		{Order: 1, Direction: DirectionOut, Action: ActionAllow},
	}
	spec := NewRuleSpec{Direction: DirectionIn, Protocol: "tcp", PortFrom: 22, PortTo: 22}

	_, ok := Simulate(rules, spec)
	if ok {
		t.Error("an OUT rule must never decide an IN request")
	}
}

func TestSimulateAnUnrestrictedRuleMatchesAnyRequest(t *testing.T) {
	rules := []Rule{
		{Order: 1, Direction: DirectionIn, Action: ActionDeny}, // any/any/any
	}
	spec := NewRuleSpec{Direction: DirectionIn, Protocol: "udp", PortFrom: 6000, PortTo: 6000, Source: "203.0.113.5"}

	got, ok := Simulate(rules, spec)
	if !ok || got.Order != 1 {
		t.Errorf("Simulate = (%v, %v), want the catch-all rule #1", got, ok)
	}
}

func TestSimulateMatchesByCIDRSource(t *testing.T) {
	rules := []Rule{
		{Order: 1, Direction: DirectionIn, Action: ActionAllow, Source: "10.0.0.0/8"},
		{Order: 2, Direction: DirectionIn, Action: ActionDeny},
	}

	insideSubnet := NewRuleSpec{Direction: DirectionIn, Source: "10.1.2.3"}
	got, ok := Simulate(rules, insideSubnet)
	if !ok || got.Order != 1 {
		t.Errorf("a source inside 10.0.0.0/8 should match rule #1, got (%v, %v)", got, ok)
	}

	outsideSubnet := NewRuleSpec{Direction: DirectionIn, Source: "203.0.113.5"}
	got, ok = Simulate(rules, outsideSubnet)
	if !ok || got.Order != 2 {
		t.Errorf("a source outside 10.0.0.0/8 should fall through to rule #2, got (%v, %v)", got, ok)
	}
}

func TestSimulateAnUnspecifiedSourceOnlyMatchesUnrestrictedRules(t *testing.T) {
	rules := []Rule{
		{Order: 1, Direction: DirectionIn, Action: ActionAllow, Source: "10.0.0.0/8"},
		{Order: 2, Direction: DirectionIn, Action: ActionDeny},
	}
	// A blank Source means "could come from anywhere" — that can never be
	// said to be fully covered by a rule restricted to one subnet, since
	// the hypothetical traffic might come from outside it.
	spec := NewRuleSpec{Direction: DirectionIn}

	got, ok := Simulate(rules, spec)
	if !ok || got.Order != 2 {
		t.Errorf("an unspecified source should skip the subnet-restricted rule and land on #2, got (%v, %v)", got, ok)
	}
}

func TestSimulateHonorsProtocolMismatch(t *testing.T) {
	rules := []Rule{
		{Order: 1, Direction: DirectionIn, Action: ActionDeny, Protocol: "udp"},
	}
	spec := NewRuleSpec{Direction: DirectionIn, Protocol: "tcp", PortFrom: 22, PortTo: 22}

	_, ok := Simulate(rules, spec)
	if ok {
		t.Error("a udp-only rule must never decide a tcp request")
	}
}

func TestSimulateIgnoresSpecOwnAction(t *testing.T) {
	rules := []Rule{
		{Order: 1, Direction: DirectionIn, Action: ActionDeny, Protocol: "tcp", PortFrom: 22, PortTo: 22},
	}
	// spec.Action is meaningless for a hypothetical request — Simulate
	// must not use it to filter, only Direction/Protocol/Port/Source/
	// Destination/Interface.
	spec := NewRuleSpec{Direction: DirectionIn, Action: ActionAllow, Protocol: "tcp", PortFrom: 22, PortTo: 22}

	got, ok := Simulate(rules, spec)
	if !ok || got.Action != ActionDeny {
		t.Errorf("Simulate should report the real rule's own DENY, not be swayed by spec's own Action, got (%v, %v)", got, ok)
	}
}
