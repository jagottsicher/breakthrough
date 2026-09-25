package firewall

// Simulate is feature_ideas.txt's own "Test-/Simulationsfunktion" (0b,
// Stufe 3): for a hypothetical request described by spec, finds which
// rule — if any — actually decides it. spec is exactly the same
// NewRuleSpec shape the "Add rule" form already fills in (see
// addrule.go); its own Action field is meaningless here and ignored — a
// hypothetical request has no action of its own, only the rule that
// ends up deciding it does.
//
// This never re-derives its own matching rules: a request is nothing
// more than the most specific possible rule a real packet could ever
// be, so it's compared to every actual rule via covers, the exact same
// relation AnnotateShadows already uses to detect one rule making
// another unreachable, rather than a second, parallel notion of
// "matches" that could quietly drift from it. First match wins, the
// same left-to-right evaluation order every real backend gives its own
// rule list.
//
// ok is false when no rule matches at all — the backend's own implicit
// default policy would decide it instead, which this package has no
// way to read back for every backend (ReadSnapshot never captures one),
// so it's left unstated here rather than guessed.
func Simulate(rules []Rule, spec NewRuleSpec) (Rule, bool) {
	query := Rule{
		Direction:   spec.Direction,
		Protocol:    spec.Protocol,
		PortFrom:    spec.PortFrom,
		PortTo:      spec.PortTo,
		Source:      spec.Source,
		Destination: spec.Destination,
		Interface:   spec.Interface,
	}

	var best Rule
	found := false
	for _, r := range rules {
		if r.Direction != query.Direction {
			continue
		}
		if !covers(r, query) {
			continue
		}
		if !found || r.Order < best.Order {
			best = r
			found = true
		}
	}
	return best, found
}
