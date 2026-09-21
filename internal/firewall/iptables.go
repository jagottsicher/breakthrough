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
