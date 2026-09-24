package firewall

import (
	"encoding/json"
	"fmt"
)

// nftDocument mirrors `nft -j list ruleset`'s own top-level shape: a
// flat list of heterogeneous items — tables, chains, rules, sets, and
// so on — distinguished by which single field is actually present on
// each one. Only "chain" and "rule" are read here; everything else
// (tables themselves, sets, counters as their own top-level objects)
// is silently skipped, the same "not every item is a rule" shape
// ParseUFWStatusVerbose's own header-skipping already has to deal with.
type nftDocument struct {
	Nftables []nftItem `json:"nftables"`
}

type nftItem struct {
	Chain *nftChain    `json:"chain,omitempty"`
	Rule  *nftRuleJSON `json:"rule,omitempty"`
}

// nftChain is only read for its own hook: "input"/"output" mark a base
// chain actually attached to the kernel's own netfilter hooks for
// traffic to/from this host, the nftables equivalent of iptables'
// INPUT/OUTPUT built-in chains (see ParseIPTablesSave's own doc comment
// on why FORWARD and every non-base, jump-only chain are out of this
// screen's scope). Table/Name together identify which chain each rule
// below belongs to, since a rule only ever names its own chain, not the
// chain's hook.
type nftChain struct {
	Table string `json:"table"`
	Name  string `json:"name"`
	Hook  string `json:"hook"`
}

type nftRuleJSON struct {
	Table string            `json:"table"`
	Chain string            `json:"chain"`
	Expr  []json.RawMessage `json:"expr"`
}

type nftExprEnvelope struct {
	Match  *nftMatch       `json:"match,omitempty"`
	Accept json.RawMessage `json:"accept,omitempty"`
	Drop   json.RawMessage `json:"drop,omitempty"`
	Reject json.RawMessage `json:"reject,omitempty"`
}

type nftMatch struct {
	Left  nftLeft         `json:"left"`
	Right json.RawMessage `json:"right"`
}

type nftLeft struct {
	Payload *nftPayload `json:"payload,omitempty"`
	Meta    *nftMeta    `json:"meta,omitempty"`
}

type nftPayload struct {
	Protocol string `json:"protocol"`
	Field    string `json:"field"`
}

type nftMeta struct {
	Key string `json:"key"`
}

// ParseNFTRuleset parses `nft -j list ruleset`'s own JSON output into
// Rules, reading only base chains hooked to "input"/"output". Every
// match this package doesn't recognize (connection-tracking state,
// packet/byte counters, logging, an address family or match type not
// covered below) is simply not reflected in that Rule's own
// Source/Destination/Protocol/Port fields rather than rejecting the
// whole rule — Raw always keeps the complete expression list, so
// nothing is ever silently hidden even where this parser's own
// understanding of it is incomplete.
func ParseNFTRuleset(data []byte) ([]Rule, error) {
	var doc nftDocument
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, err
	}

	hooks := make(map[string]string) // "table/chain" -> hook
	for _, item := range doc.Nftables {
		if item.Chain != nil && item.Chain.Hook != "" {
			hooks[item.Chain.Table+"/"+item.Chain.Name] = item.Chain.Hook
		}
	}

	orderIn, orderOut := 1, 1
	var rules []Rule
	for _, item := range doc.Nftables {
		if item.Rule == nil {
			continue
		}
		hook := hooks[item.Rule.Table+"/"+item.Rule.Chain]
		var direction Direction
		switch hook {
		case "input":
			direction = DirectionIn
		case "output":
			direction = DirectionOut
		default:
			continue
		}

		expanded, err := parseNFTRule(item.Rule, direction)
		if err != nil {
			return nil, fmt.Errorf("nft ruleset: %w (chain %q)", err, item.Rule.Chain)
		}
		if expanded == nil {
			continue // no verdict in this rule (a counter/log-only helper) — nothing to show
		}
		for i := range expanded {
			expanded[i].Chain = item.Rule.Chain
			if direction == DirectionIn {
				expanded[i].Order = orderIn
				orderIn++
			} else {
				expanded[i].Order = orderOut
				orderOut++
			}
		}
		rules = append(rules, expanded...)
	}
	return rules, nil
}

// parseNFTRule reads one rule's own expr list. Returns nil, nil (not an
// error) when no verdict statement (accept/drop/reject) is present at
// all — a rule that only counts or logs decides nothing on its own.
func parseNFTRule(rule *nftRuleJSON, direction Direction) ([]Rule, error) {
	raw, err := json.Marshal(rule.Expr)
	if err != nil {
		return nil, err
	}

	base := Rule{Direction: direction, Backend: BackendNFTables, Raw: string(raw)}
	var portSet []int
	haveVerdict := false

	for _, exprRaw := range rule.Expr {
		var env nftExprEnvelope
		if err := json.Unmarshal(exprRaw, &env); err != nil {
			return nil, err
		}
		switch {
		case len(env.Accept) > 0:
			base.Action, haveVerdict = ActionAllow, true
		case len(env.Drop) > 0:
			base.Action, haveVerdict = ActionDeny, true
		case len(env.Reject) > 0:
			base.Action, haveVerdict = ActionReject, true
		case env.Match != nil:
			set, err := applyNFTMatch(&base, env.Match)
			if err != nil {
				return nil, err
			}
			if set != nil {
				portSet = set
			}
		}
	}
	if !haveVerdict {
		return nil, nil
	}
	if len(portSet) == 0 {
		return []Rule{base}, nil
	}
	out := make([]Rule, len(portSet))
	for i, p := range portSet {
		r := base
		r.PortFrom, r.PortTo = p, p
		out[i] = r
	}
	return out, nil
}

// applyNFTMatch reads one "match" expression, filling in whichever of
// base's fields it identifies. A dport/sport match against an explicit
// {"set": [...]} of discrete ports (nft's own `tcp dport {80, 443}`
// syntax) is reported back as portSet rather than written straight into
// base, so the caller can expand it into that many separate Rules, the
// same way ParseUFWStatusVerbose expands a comma-grouped port list — a
// single port or a genuine contiguous {"range": [from, to]} is written
// directly into base.PortFrom/PortTo instead, since Rule.PortLabel
// already renders a range as one "from-to" span.
func applyNFTMatch(base *Rule, m *nftMatch) (portSet []int, err error) {
	switch {
	case m.Left.Payload != nil:
		switch m.Left.Payload.Field {
		case "dport", "sport":
			base.Protocol = m.Left.Payload.Protocol
			spec, err := nftPortSpec(m.Right)
			if err != nil {
				return nil, err
			}
			if len(spec.set) > 0 {
				return spec.set, nil
			}
			base.PortFrom, base.PortTo = spec.from, spec.to
			return nil, nil
		case "saddr":
			base.Source, err = nftAddrValue(m.Right)
			return nil, err
		case "daddr":
			base.Destination, err = nftAddrValue(m.Right)
			return nil, err
		}
	case m.Left.Meta != nil:
		switch m.Left.Meta.Key {
		case "l4proto":
			if base.Protocol == "" {
				base.Protocol, err = nftStringValue(m.Right)
			}
			return nil, err
		case "iifname":
			if base.Direction == DirectionIn {
				base.Interface, err = nftStringValue(m.Right)
			}
			return nil, err
		case "oifname":
			if base.Direction == DirectionOut {
				base.Interface, err = nftStringValue(m.Right)
			}
			return nil, err
		}
	}
	return nil, nil
}

func nftStringValue(right json.RawMessage) (string, error) {
	var s string
	if err := json.Unmarshal(right, &s); err != nil {
		return "", fmt.Errorf("expected a string, got %s: %w", right, err)
	}
	return s, nil
}

// nftAddrValue reads a plain "192.168.1.0/24" string, or nftables' own
// alternative {"prefix": {"addr": "...", "len": N}} shape for a CIDR
// some nft versions emit instead.
func nftAddrValue(right json.RawMessage) (string, error) {
	var s string
	if err := json.Unmarshal(right, &s); err == nil {
		return s, nil
	}
	var prefix struct {
		Prefix struct {
			Addr string `json:"addr"`
			Len  int    `json:"len"`
		} `json:"prefix"`
	}
	if err := json.Unmarshal(right, &prefix); err != nil {
		return "", fmt.Errorf("expected an address or prefix, got %s: %w", right, err)
	}
	return fmt.Sprintf("%s/%d", prefix.Prefix.Addr, prefix.Prefix.Len), nil
}

// nftPortSpecResult is a plain port or a genuine contiguous range as
// from/to (to == from for a plain port), or — mutually exclusive with
// from/to — an explicit set of discrete port values.
type nftPortSpecResult struct {
	from, to int
	set      []int
}

// nftPortSpec reads a plain port number, a {"range": [from, to]}, or a
// {"set": [...]} of individual ports (nft's own `tcp dport {80, 443}`
// syntax).
func nftPortSpec(right json.RawMessage) (nftPortSpecResult, error) {
	var n float64
	if err := json.Unmarshal(right, &n); err == nil {
		return nftPortSpecResult{from: int(n), to: int(n)}, nil
	}

	var obj struct {
		Range []float64 `json:"range"`
		Set   []float64 `json:"set"`
	}
	if err := json.Unmarshal(right, &obj); err != nil {
		return nftPortSpecResult{}, fmt.Errorf("expected a port, range, or set, got %s: %w", right, err)
	}
	if len(obj.Range) == 2 {
		return nftPortSpecResult{from: int(obj.Range[0]), to: int(obj.Range[1])}, nil
	}
	if len(obj.Set) > 0 {
		set := make([]int, len(obj.Set))
		for i, p := range obj.Set {
			set[i] = int(p)
		}
		return nftPortSpecResult{set: set}, nil
	}
	return nftPortSpecResult{}, fmt.Errorf("expected a port, range, or set, got %s", right)
}

// NFTAddRuleCommand always refuses — unlike ufw's own implicit default
// or iptables' conventional INPUT/OUTPUT chains, a real nftables
// ruleset's table and chain names are entirely up to whoever set it
// up, with no convention this package could safely guess at without a
// real risk of adding a rule to a chain packets never actually
// traverse — which would look like it worked while quietly doing
// nothing. Reported plainly rather than attempted, the same "explain
// rather than guess wrong" principle checkTools already follows for a
// missing archive tool elsewhere in this app.
func NFTAddRuleCommand(NewRuleSpec) (string, error) {
	return "", fmt.Errorf("nftables: adding rules through this screen isn't supported yet — its own table/chain layout varies per host with no safe default to guess; use \"nft\" by hand, or switch this host to ufw or iptables for the rule builder")
}
