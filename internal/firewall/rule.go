package firewall

import "fmt"

// Direction is which way traffic a Rule matches is travelling.
type Direction int

const (
	DirectionIn Direction = iota
	DirectionOut
)

func (d Direction) String() string {
	if d == DirectionOut {
		return "OUT"
	}
	return "IN"
}

// Action is what a Rule does to traffic it matches.
type Action int

const (
	ActionAllow Action = iota
	ActionDeny
	ActionReject
)

func (a Action) String() string {
	switch a {
	case ActionDeny:
		return "DENY"
	case ActionReject:
		return "REJECT"
	default:
		return "ALLOW"
	}
}

// Backend identifies which real firewall tool a Rule was read from.
type Backend string

const (
	BackendNone     Backend = ""
	BackendUFW      Backend = "ufw"
	BackendNFTables Backend = "nftables"
	BackendIPTables Backend = "iptables"
)

// anyCIDR/AnyPort/AnyInterface are Rule's own "unset means any" values —
// spelled out here once so every parser and every comparison uses the
// exact same sentinel rather than each guessing "" vs "any" vs "0.0.0.0/0"
// on its own.
const anyCIDR = ""

// Rule is one firewall rule, normalized from whichever real backend
// (UFW, nftables, iptables) actually reported it, so internal/ui's own
// Firewall screen never needs backend-specific logic. Order is the
// position this rule is actually evaluated in, relative to every other
// rule in the same Direction — first match wins, the same left-to-right
// reading every real backend already gives its own rule list, so a
// smaller Order always takes precedence over a larger one that would
// otherwise also match the same traffic (see AnnotateShadows).
type Rule struct {
	Order       int
	Direction   Direction
	Action      Action
	Protocol    string // "tcp", "udp", or "" for any protocol
	PortFrom    int    // 0 means "any port" (with PortTo also 0)
	PortTo      int    // equal to PortFrom for a single port
	Source      string // CIDR/IP, or "" for any source
	Destination string // CIDR/IP, or "" for any destination
	Interface   string // "" for any interface
	Backend     Backend
	Chain       string // backend-specific chain/table name, shown for traceability
	Raw         string // the exact backend line/fragment this was parsed from

	// ShadowedByOrder/ShadowedByAction are set by AnnotateShadows when an
	// earlier rule (smaller Order, same Direction) already matches every
	// packet this rule would ever see, making this rule unreachable.
	// ShadowedByOrder is 0 (Order is always >= 1) when this rule isn't
	// shadowed.
	ShadowedByOrder  int
	ShadowedByAction Action
}

// HasAnyPort reports whether this rule matches every port rather than a
// specific one or range.
func (r Rule) HasAnyPort() bool {
	return r.PortFrom == 0 && r.PortTo == 0
}

// PortLabel renders the port part of a rule for display: "any" for no
// port restriction, "22" for a single port, "6000-6063" for a range —
// with the port's own well-known service name from lookup appended in
// parentheses where one is known ("22 (ssh)"), same as getent's own
// output already looks up, never a hardcoded, eventually stale table of
// well-known ports baked into this package itself.
func (r Rule) PortLabel(lookup ServiceLookup) string {
	if r.HasAnyPort() {
		return "any"
	}
	label := fmt.Sprintf("%d", r.PortFrom)
	if r.PortTo != r.PortFrom {
		label = fmt.Sprintf("%d-%d", r.PortFrom, r.PortTo)
	}
	if name := lookup.Name(r.PortFrom, r.Protocol); name != "" {
		label += " (" + name + ")"
	}
	return label
}

// SourceLabel/DestinationLabel render "any" for the unrestricted case
// rather than an empty string, so a table cell is never blank in a way
// that could be misread as "not yet loaded".
func (r Rule) SourceLabel() string      { return orAny(r.Source) }
func (r Rule) DestinationLabel() string { return orAny(r.Destination) }
func (r Rule) InterfaceLabel() string   { return orAny(r.Interface) }

func orAny(s string) string {
	if s == anyCIDR {
		return "any"
	}
	return s
}
