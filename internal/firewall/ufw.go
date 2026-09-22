package firewall

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// UFWActive reports whether output (the text `ufw status` printed)
// starts with an active status line — UFW itself prints "Status:
// active" or "Status: inactive", the one line DetectBackend actually
// needs.
func UFWActive(output string) bool {
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		return strings.EqualFold(line, "Status: active")
	}
	return false
}

// ufwColumns splits one of ufw status verbose's own table rows into its
// three columns (To, Action, From) — each column padded with runs of
// two or more spaces between it and the next, confirmed against real
// `ufw status verbose` output, not guessed. A plain strings.Fields
// would also split "ALLOW IN" or a "Anywhere on eth0" interface
// qualifier into separate fields, which is exactly what this needs to
// keep together.
var ufwColumnSplit = regexp.MustCompile(`\s{2,}`)

func ufwColumns(line string) []string {
	var cols []string
	for _, c := range ufwColumnSplit.Split(strings.TrimSpace(line), -1) {
		if c != "" {
			cols = append(cols, c)
		}
	}
	return cols
}

// ParseUFWStatusVerbose parses `ufw status verbose`'s own table into
// Rules. Only the "To / Action / From" rule table is used — the
// Status/Logging/Default/New-profiles header lines above it are
// skipped by looking for the table's own "--  ------  ----" separator
// row first, the one fixed anchor every real ufw version prints
// regardless of locale or how many rules follow.
func ParseUFWStatusVerbose(output string) ([]Rule, error) {
	lines := strings.Split(output, "\n")

	start := -1
	for i, line := range lines {
		cols := ufwColumns(line)
		if len(cols) == 3 && strings.Trim(cols[0], "-") == "" && strings.Trim(cols[1], "-") == "" && strings.Trim(cols[2], "-") == "" {
			start = i + 1
			break
		}
	}
	if start < 0 {
		return nil, nil // inactive, or no rules configured at all — not an error
	}

	var rules []Rule
	order := 1
	for _, line := range lines[start:] {
		if strings.TrimSpace(line) == "" {
			continue
		}
		cols := ufwColumns(line)
		if len(cols) != 3 {
			return nil, fmt.Errorf("ufw status verbose: unrecognized rule line %q", line)
		}
		expanded, err := parseUFWRuleLine(cols[0], cols[1], cols[2])
		if err != nil {
			return nil, fmt.Errorf("ufw status verbose: %w (line %q)", err, line)
		}
		for i := range expanded {
			expanded[i].Order = order
			expanded[i].Raw = line
			order++
		}
		rules = append(rules, expanded...)
	}
	return rules, nil
}

// stripV6Suffix removes ufw's own trailing " (v6)" marker — printed on
// both the To and From columns of the IPv6 twin ufw shows right
// alongside every plain rule whenever IPv6 support is enabled (its own
// default). Both twins match real traffic independently; showing the
// v6 one as an ordinary row of its own (rather than dropping it) is the
// accurate answer, "seen once per address family, same as ufw itself
// already prints it" — this just stops that marker leaking into a port
// number or CIDR this package would otherwise fail to parse.
func stripV6Suffix(s string) string {
	return strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(s), "(v6)"))
}

// parseUFWRuleLine reads one already-split (To, Action, From) triple
// and returns one Rule per port in "To" — ufw status verbose groups
// several ports sharing one rule into a single comma-separated line
// ("80,443/tcp"), which this expands back into one row per port so
// each one lines up with its own PortLabel/service name. "To" also
// carries an "on <iface>" suffix for a rule scoped to one NIC; "Action"
// is "ALLOW"/"DENY"/"REJECT"/"LIMIT" optionally followed by "IN"/"OUT"
// (ufw omits the direction word for its own default "IN" rules); "From"
// is the source address/CIDR, "Anywhere" for no restriction.
func parseUFWRuleLine(to, action, from string) ([]Rule, error) {
	var base Rule

	actionFields := strings.Fields(action)
	if len(actionFields) == 0 {
		return nil, fmt.Errorf("empty action column")
	}
	switch strings.ToUpper(actionFields[0]) {
	case "ALLOW":
		base.Action = ActionAllow
	case "DENY":
		base.Action = ActionDeny
	case "REJECT":
		base.Action = ActionReject
	case "LIMIT":
		// LIMIT rate-limits rather than outright blocking, but still lets
		// matching traffic through — closer to an allow than a deny for
		// "does this get in" purposes, until this screen grows a fourth,
		// LIMIT-specific action of its own.
		base.Action = ActionAllow
	default:
		return nil, fmt.Errorf("unrecognized action %q", action)
	}
	base.Direction = DirectionIn
	if len(actionFields) > 1 && strings.EqualFold(actionFields[1], "OUT") {
		base.Direction = DirectionOut
	}

	target := stripV6Suffix(to)
	toFields := strings.Fields(target)
	if len(toFields) >= 3 && strings.EqualFold(toFields[len(toFields)-2], "on") {
		base.Interface = toFields[len(toFields)-1]
		target = strings.Join(toFields[:len(toFields)-2], " ")
	}

	from = stripV6Suffix(from)
	if !strings.EqualFold(from, "Anywhere") {
		base.Source = from
	}

	if strings.EqualFold(target, "Anywhere") {
		return []Rule{base}, nil
	}

	ports, proto, ok := parsePortList(target)
	if !ok {
		base.Destination = target
		return []Rule{base}, nil
	}
	rules := make([]Rule, len(ports))
	for i, p := range ports {
		r := base
		r.PortFrom, r.PortTo, r.Protocol = p, p, proto
		rules[i] = r
	}
	return rules, nil
}

// parsePortList reads ufw's own "22/tcp", bare "22" (any protocol), or
// comma-grouped "80,443/tcp" port notation, common to a single
// "Action"/"From" pair sharing one rule.
func parsePortList(s string) (ports []int, proto string, ok bool) {
	numeric := s
	if i := strings.IndexByte(s, '/'); i >= 0 {
		proto = s[i+1:]
		numeric = s[:i]
	}
	for _, p := range strings.Split(numeric, ",") {
		n, err := strconv.Atoi(p)
		if err != nil {
			return nil, "", false
		}
		ports = append(ports, n)
	}
	return ports, proto, true
}
