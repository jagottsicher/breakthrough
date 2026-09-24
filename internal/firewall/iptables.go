package firewall

import (
	"fmt"
	"strconv"
	"strings"
)

// ParseIPTablesSave parses `iptables-save`'s own stable, scriptable
// output — one "-A <chain> <match...> -j <target>" line per rule,
// grouped under "*filter"/"*nat"/... table sections — rather than
// `iptables -L`'s own text table, which is meant for a human at a
// terminal, not this parser (column widths and wrapping depend on
// what's actually configured). Only the "filter" table's INPUT/OUTPUT
// chains are read: INPUT is the accepted proxy for "incoming traffic to
// this host", OUTPUT for outgoing — FORWARD (routed-through traffic)
// and every other table (nat, mangle, raw) are a router/NAT concern
// outside this screen's own "what reaches or leaves this host" scope.
func ParseIPTablesSave(output string) ([]Rule, error) {
	var rules []Rule
	table := ""
	orderIn, orderOut := 1, 1

	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case line == "", strings.HasPrefix(line, "#"):
			continue
		case strings.HasPrefix(line, "*"):
			table = strings.TrimPrefix(line, "*")
			continue
		case strings.HasPrefix(line, ":"):
			continue // chain policy line (":INPUT ACCEPT [0:0]") — not a rule
		case line == "COMMIT":
			table = ""
			continue
		}
		if table != "filter" || !strings.HasPrefix(line, "-A ") {
			continue
		}

		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		chain := fields[1]
		if chain != "INPUT" && chain != "OUTPUT" {
			continue
		}

		rule, err := parseIPTablesRuleFields(fields[2:])
		if err != nil {
			return nil, fmt.Errorf("iptables-save: %w (line %q)", err, line)
		}
		rule.Chain = chain
		rule.Raw = line
		if chain == "INPUT" {
			rule.Direction = DirectionIn
			rule.Order = orderIn
			orderIn++
		} else {
			rule.Direction = DirectionOut
			rule.Order = orderOut
			orderOut++
		}
		rules = append(rules, rule)
	}
	return rules, nil
}

// parseIPTablesRuleFields reads one rule's own already-tokenized match
// flags (everything after "-A <chain>"). Unrecognized flags (an
// extension module's own option this package doesn't model yet, "!"
// negation, ...) are kept out of the normalized Rule fields but never
// dropped from Raw, so the rule as a whole still shows up rather than
// vanishing — this project's own "report it, don't swallow it"
// principle applied to a match type this package simply doesn't
// understand yet, same as an unrecognized nftables expression (see
// nft.go).
func parseIPTablesRuleFields(fields []string) (Rule, error) {
	var r Rule
	r.Action = ActionAllow // set from -j below; ACCEPT is the common case

	for i := 0; i < len(fields); i++ {
		f := fields[i]
		next := func() string {
			i++
			if i < len(fields) {
				return fields[i]
			}
			return ""
		}
		switch f {
		case "-p", "--protocol":
			r.Protocol = next()
		case "-s", "--source":
			r.Source = strings.TrimSuffix(next(), "/32")
		case "-d", "--destination":
			r.Destination = strings.TrimSuffix(next(), "/32")
		case "-i", "--in-interface":
			r.Interface = next()
		case "-o", "--out-interface":
			r.Interface = next()
		case "--dport", "--destination-port":
			from, to, err := parseIPTablesPortSpec(next())
			if err != nil {
				return r, err
			}
			r.PortFrom, r.PortTo = from, to
		case "-j", "--jump":
			switch strings.ToUpper(next()) {
			case "ACCEPT":
				r.Action = ActionAllow
			case "DROP":
				r.Action = ActionDeny
			case "REJECT":
				r.Action = ActionReject
			}
		}
	}
	return r, nil
}

// parseIPTablesPortSpec reads iptables' own "22" or "6000:6063" (colon,
// not dash) port/port-range notation.
func parseIPTablesPortSpec(s string) (from, to int, err error) {
	if i := strings.IndexByte(s, ':'); i >= 0 {
		from, err = strconv.Atoi(s[:i])
		if err != nil {
			return 0, 0, fmt.Errorf("invalid port range %q: %w", s, err)
		}
		to, err = strconv.Atoi(s[i+1:])
		if err != nil {
			return 0, 0, fmt.Errorf("invalid port range %q: %w", s, err)
		}
		return from, to, nil
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0, 0, fmt.Errorf("invalid port %q: %w", s, err)
	}
	return n, n, nil
}

// iptablesActionTarget/iptablesChain/iptablesInterfaceFlag render
// spec's own fields the way a real iptables command line expects them
// — the same vocabulary parseIPTablesRuleFields already reads back
// (ACCEPT/DROP/REJECT, INPUT/OUTPUT, -i for incoming/-o for outgoing),
// so a rule this package adds and one it merely reports can never
// disagree on it.
func iptablesActionTarget(a Action) string {
	switch a {
	case ActionDeny:
		return "DROP"
	case ActionReject:
		return "REJECT"
	default:
		return "ACCEPT"
	}
}

func iptablesChain(d Direction) string {
	if d == DirectionOut {
		return "OUTPUT"
	}
	return "INPUT"
}

func iptablesInterfaceFlag(d Direction) string {
	if d == DirectionOut {
		return "-o"
	}
	return "-i"
}

// iptablesRuleCommand builds one real `iptables` command line for
// spec, under either flag ("-A" to append, "-D" to delete the exact
// same rule again) — the shared core IPTablesAddRuleCommand/
// IPTablesDeleteRuleCommand build on, so the two can never quietly
// disagree about what a given spec actually turns into beyond that one
// flag. Only the "filter" table's INPUT/OUTPUT chains, matching this
// package's own read side (see ParseIPTablesSave's own doc comment for
// why FORWARD and every other table are out of scope).
//
// A port always requires an explicit protocol: `--dport` is a
// tcp/udp-specific match extension iptables refuses outright without
// `-p tcp`/`-p udp` naming which one first — reported here as a clear,
// actionable error rather than a real iptables invocation this package
// would just hand to a shell to fail on with a less legible message.
func iptablesRuleCommand(flag string, spec NewRuleSpec) (string, error) {
	if err := spec.Validate(); err != nil {
		return "", err
	}
	if !spec.HasAnyPort() && spec.Protocol == "" {
		return "", fmt.Errorf("iptables: a port requires an explicit protocol (tcp or udp)")
	}

	var b strings.Builder
	fmt.Fprintf(&b, "iptables %s %s", flag, iptablesChain(spec.Direction))
	if spec.Protocol != "" {
		fmt.Fprintf(&b, " -p %s", spec.Protocol)
	}
	if spec.Source != "" {
		fmt.Fprintf(&b, " -s %s", spec.Source)
	}
	if spec.Destination != "" {
		fmt.Fprintf(&b, " -d %s", spec.Destination)
	}
	if spec.Interface != "" {
		fmt.Fprintf(&b, " %s %s", iptablesInterfaceFlag(spec.Direction), spec.Interface)
	}
	if !spec.HasAnyPort() {
		fmt.Fprintf(&b, " --dport %s", portRangeArg(spec.PortFrom, spec.PortTo, ":"))
	}
	fmt.Fprintf(&b, " -j %s", iptablesActionTarget(spec.Action))
	return b.String(), nil
}

// IPTablesAddRuleCommand builds the real `iptables -A ...` command
// line that would add spec as a new rule.
func IPTablesAddRuleCommand(spec NewRuleSpec) (string, error) {
	return iptablesRuleCommand("-A", spec)
}

// IPTablesDeleteRuleCommand builds the exact command that reverses
// IPTablesAddRuleCommand's own (`-D` in place of `-A`, otherwise byte-
// for-byte identical) — the rollback feature_ideas.txt's own
// "Selbstaussperr-Schutz" calls once a rule it just applied turns out
// to have cut off the very session that applied it.
func IPTablesDeleteRuleCommand(spec NewRuleSpec) (string, error) {
	return iptablesRuleCommand("-D", spec)
}
