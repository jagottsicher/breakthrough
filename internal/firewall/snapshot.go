package firewall

import "fmt"

// Snapshot is the full picture ReadSnapshot builds: which Backend actually
// governs traffic right now, and its Rules, each already checked for being
// shadowed by an earlier one (see AnnotateShadows).
type Snapshot struct {
	Backend Backend
	Rules   []Rule
}

// ReadSnapshot detects which single backend actually governs this host's
// traffic right now (see DetectBackend) and reads its Rules. Backend ==
// BackendNone with a nil error is not a failure — no firewall this package
// understands is simply active right now; the only error case is a
// detected backend whose own output this package failed to read or parse.
func ReadSnapshot() (Snapshot, error) {
	backend := DetectBackend()

	var rules []Rule
	var err error
	switch backend {
	case BackendUFW:
		var out string
		if out, err = runUFWStatus(); err == nil {
			rules, err = ParseUFWStatusVerbose(out)
		}
	case BackendNFTables:
		var out []byte
		if out, err = runNFTRuleset(); err == nil {
			rules, err = ParseNFTRuleset(out)
		}
	case BackendIPTables:
		var out string
		if out, err = runIPTablesSave(); err == nil {
			rules, err = ParseIPTablesSave(out)
		}
	}
	if err != nil {
		return Snapshot{}, fmt.Errorf("firewall (%s): %w", backend, err)
	}
	return Snapshot{Backend: backend, Rules: AnnotateShadows(rules)}, nil
}
