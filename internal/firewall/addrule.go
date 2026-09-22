package firewall

import "fmt"

// NewRuleSpec is what the Firewall screen's own rule-builder form will
// collect (see feature_ideas.txt's own "Regel-Baukasten" stage) — the
// same fields an already-read Rule has, minus everything only
// ReadSnapshot itself ever fills in (Order, Backend, Chain, Raw,
// Shadowed*): this is an *input* to AddRuleCommand, never a Rule in its
// own right until the real backend has actually accepted it and this
// package reads it back fresh via ReadSnapshot — the same "never trust
// a locally-built guess over the real tool's own report" principle
// AnnotateShadows' own read-only Rules already rely on.
type NewRuleSpec struct {
	Direction   Direction
	Action      Action
	Protocol    string // "tcp", "udp", or "" for any protocol
	PortFrom    int    // 0 means "any port" (PortTo must also be 0)
	PortTo      int    // equal to PortFrom for a single port
	Source      string // CIDR/IP, or "" for any source
	Destination string // CIDR/IP, or "" for any destination
	Interface   string // "" for any interface
}

// HasAnyPort mirrors Rule's own method of the same name — true once
// PortFrom/PortTo are both left at their zero value.
func (s NewRuleSpec) HasAnyPort() bool {
	return s.PortFrom == 0 && s.PortTo == 0
}

// Validate reports the one thing every backend's own AddRuleCommand
// needs true before it can build a real command line at all: a port
// *range* has to actually be a range (From no greater than To), and a
// single port (From == To) can never be expressed as "0" — 0 already
// means "any port" (see HasAnyPort) — a real rule naming port 0
// specifically is never what a filled-in form actually meant.
func (s NewRuleSpec) Validate() error {
	if s.HasAnyPort() {
		return nil
	}
	if s.PortFrom <= 0 || s.PortTo <= 0 {
		return fmt.Errorf("firewall: port %d-%d: a port must be 1-65535, or both 0 for \"any port\"", s.PortFrom, s.PortTo)
	}
	if s.PortFrom > s.PortTo {
		return fmt.Errorf("firewall: port range %d-%d: the first port must not be greater than the second", s.PortFrom, s.PortTo)
	}
	return nil
}

// isSSHRelevant reports whether spec could plausibly affect an already
// established SSH session to this host — the trigger
// feature_ideas.txt's own "Selbstaussperr-Schutz" exists for: a rule
// naming port 22 specifically, or one with no port restriction at all
// (which blocks port 22 right along with everything else), and whose
// own protocol isn't explicitly something other than tcp (SSH only
// ever runs over tcp, so a rule pinned to "udp" specifically can never
// touch it). Direction/Action aren't checked here — an Allow rule can
// still matter (see the Firewall screen's own default-deny reasoning
// for why not every relevant change is a Deny), and callers layering
// this on top of a live SSH-session check of their own (see
// runningOverSSH) are what actually decides whether to arm the
// rollback timer, not this function in isolation.
func (s NewRuleSpec) isSSHRelevant() bool {
	if s.Protocol != "" && s.Protocol != "tcp" {
		return false
	}
	if s.HasAnyPort() {
		return true
	}
	return s.PortFrom <= 22 && s.PortTo >= 22
}

// portRangeArg renders a port (or range) using sep between From and To
// — "-" for ufw, ":" for iptables, the one place their own port-range
// notations actually differ (see parsePortList/parseIPTablesPortSpec's
// own doc comments for the read-side equivalent).
func portRangeArg(from, to int, sep string) string {
	if from == to {
		return fmt.Sprintf("%d", from)
	}
	return fmt.Sprintf("%d%s%d", from, sep, to)
}
