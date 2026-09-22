package firewall

import "testing"

func TestAnnotateShadowsMarksALaterRuleFullyCoveredByAnEarlierOne(t *testing.T) {
	rules := []Rule{
		{Order: 1, Direction: DirectionIn, Action: ActionAllow}, // any/any/any — matches everything
		{Order: 2, Direction: DirectionIn, Action: ActionDeny, PortFrom: 22, PortTo: 22, Protocol: "tcp"},
	}
	out := AnnotateShadows(rules)
	if out[1].ShadowedByOrder != 1 {
		t.Errorf("rule[1].ShadowedByOrder = %d, want 1", out[1].ShadowedByOrder)
	}
	if out[1].ShadowedByAction != ActionAllow {
		t.Errorf("rule[1].ShadowedByAction = %v, want ALLOW", out[1].ShadowedByAction)
	}
	if out[0].ShadowedByOrder != 0 {
		t.Errorf("rule[0] (the first, broadest rule) should never be shadowed, got %d", out[0].ShadowedByOrder)
	}
}

func TestAnnotateShadowsDoesNotFlagAnUnrelatedLaterRule(t *testing.T) {
	rules := []Rule{
		{Order: 1, Direction: DirectionIn, Action: ActionAllow, PortFrom: 22, PortTo: 22, Protocol: "tcp"},
		{Order: 2, Direction: DirectionIn, Action: ActionAllow, PortFrom: 80, PortTo: 80, Protocol: "tcp"},
	}
	out := AnnotateShadows(rules)
	if out[1].ShadowedByOrder != 0 {
		t.Errorf("a rule for a different port should not be marked shadowed, got ShadowedByOrder=%d", out[1].ShadowedByOrder)
	}
}

func TestAnnotateShadowsIgnoresOppositeDirection(t *testing.T) {
	rules := []Rule{
		{Order: 1, Direction: DirectionIn, Action: ActionAllow},
		{Order: 2, Direction: DirectionOut, Action: ActionDeny, PortFrom: 22, PortTo: 22},
	}
	out := AnnotateShadows(rules)
	if out[1].ShadowedByOrder != 0 {
		t.Error("an OUT rule must never be marked shadowed by an IN rule")
	}
}

func TestAnnotateShadowsCIDRContainment(t *testing.T) {
	rules := []Rule{
		{Order: 1, Direction: DirectionIn, Action: ActionAllow, Source: "10.0.0.0/8"},
		{Order: 2, Direction: DirectionIn, Action: ActionDeny, Source: "10.1.2.3"},
		{Order: 3, Direction: DirectionIn, Action: ActionDeny, Source: "192.168.1.0/24"},
	}
	out := AnnotateShadows(rules)
	if out[1].ShadowedByOrder != 1 {
		t.Errorf("a single host inside 10.0.0.0/8 should be shadowed by it, got ShadowedByOrder=%d", out[1].ShadowedByOrder)
	}
	if out[2].ShadowedByOrder != 0 {
		t.Errorf("192.168.1.0/24 is unrelated to 10.0.0.0/8 and should not be shadowed, got %d", out[2].ShadowedByOrder)
	}
}

func TestAnnotateShadowsPortRangeContainment(t *testing.T) {
	rules := []Rule{
		{Order: 1, Direction: DirectionIn, Action: ActionAllow, PortFrom: 6000, PortTo: 6100},
		{Order: 2, Direction: DirectionIn, Action: ActionDeny, PortFrom: 6050, PortTo: 6050},
		{Order: 3, Direction: DirectionIn, Action: ActionDeny, PortFrom: 6050, PortTo: 6200}, // partially outside — not covered
	}
	out := AnnotateShadows(rules)
	if out[1].ShadowedByOrder != 1 {
		t.Errorf("port 6050 inside 6000-6100 should be shadowed, got %d", out[1].ShadowedByOrder)
	}
	if out[2].ShadowedByOrder != 0 {
		t.Errorf("a range extending past 6100 is not fully covered and should not be shadowed, got %d", out[2].ShadowedByOrder)
	}
}

func TestAnnotateShadowsDoesNotMutateInput(t *testing.T) {
	rules := []Rule{
		{Order: 1, Direction: DirectionIn, Action: ActionAllow},
		{Order: 2, Direction: DirectionIn, Action: ActionDeny, PortFrom: 22, PortTo: 22},
	}
	_ = AnnotateShadows(rules)
	if rules[1].ShadowedByOrder != 0 {
		t.Error("AnnotateShadows must return a copy, not mutate its input slice")
	}
}

func TestCidrCovers(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"10.0.0.0/8", "10.1.2.3", true},
		{"10.0.0.0/8", "10.1.2.0/24", true},
		{"10.1.2.3", "10.0.0.0/8", false}, // a single host can't cover a whole /8
		{"192.168.1.0/24", "192.168.2.0/24", false},
		{"10.0.0.0/8", "", false}, // "" (any) is never covered by a's own narrower restriction
		{"not-an-ip", "10.0.0.1", false},
	}
	for _, c := range cases {
		if got := cidrCovers(c.a, c.b); got != c.want {
			t.Errorf("cidrCovers(%q, %q) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}
