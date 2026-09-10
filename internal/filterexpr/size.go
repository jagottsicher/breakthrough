package filterexpr

import (
	"fmt"
	"strconv"
	"strings"
)

// sizeUnits maps a case-folded unit suffix to its byte multiplier —
// binary (1024-based), matching this app's own existing size
// convention throughout (see humanSize in internal/ui/properties.go
// and the Size column's own human-readable mode), not the decimal
// (1000-based) SI convention some other tools use. An absent suffix
// means plain bytes.
var sizeUnits = map[string]int64{
	"":  1,
	"b": 1,
	"k": 1024,
	"m": 1024 * 1024,
	"g": 1024 * 1024 * 1024,
	"t": 1024 * 1024 * 1024 * 1024,
}

// sizeComparator is one clause's own comparison operator.
type sizeComparator int

const (
	sizeLess sizeComparator = iota
	sizeLessOrEqual
	sizeGreater
	sizeGreaterOrEqual
	sizeEqual
)

type sizeClause struct {
	op    sizeComparator
	bytes int64
}

func (c sizeClause) matches(size int64) bool {
	switch c.op {
	case sizeLess:
		return size < c.bytes
	case sizeLessOrEqual:
		return size <= c.bytes
	case sizeGreater:
		return size > c.bytes
	case sizeGreaterOrEqual:
		return size >= c.bytes
	default:
		return size == c.bytes
	}
}

// SizeFilter is a parsed size expression — see ParseSize.
type SizeFilter struct {
	clauses []sizeClause
}

// Match reports whether size satisfies every clause in the expression
// (clauses combine with an implicit AND — see ParseSize's own doc
// comment on why there's no OR: a range is expressed as two clauses on
// the same field, e.g. "> 1m and < 5m", the same shape the user's own
// request spelled out).
func (f SizeFilter) Match(size int64) bool {
	for _, c := range f.clauses {
		if !c.matches(size) {
			return false
		}
	}
	return true
}

// ParseSize parses a size expression: one or more comparison clauses
// joined by "and" (case-insensitive), each of the form
// "<operator> <number><unit>" — e.g. "> 1m", ">= 1000m",
// "< 1g and > 500m". operator is one of <, <=, >, >=, = (== also
// accepted as a familiar alias for anyone typing it out of programming
// habit). unit is one of b/k/m/g/t (case-insensitive, binary/1024-based
// — see sizeUnits), or omitted entirely for plain bytes. Whitespace
// around the operator and between the number and unit is optional.
//
// An empty expression is rejected rather than treated as "match
// everything" — the caller (internal/ui's filterBySize) already treats
// an empty *field*, and an inactive toggle, as their own separate
// no-op cases before ever calling this, so ParseSize itself only ever
// needs to say whether what it was actually given parses.
func ParseSize(expr string) (SizeFilter, error) {
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return SizeFilter{}, fmt.Errorf("filterexpr: empty size expression")
	}

	parts := splitOnAnd(expr)
	clauses := make([]sizeClause, 0, len(parts))
	for _, part := range parts {
		clause, err := parseSizeClause(part)
		if err != nil {
			return SizeFilter{}, err
		}
		clauses = append(clauses, clause)
	}
	return SizeFilter{clauses: clauses}, nil
}

func parseSizeClause(s string) (sizeClause, error) {
	s = strings.TrimSpace(s)

	op, rest, ok := cutComparator(s)
	if !ok {
		return sizeClause{}, fmt.Errorf("filterexpr: %q doesn't start with a comparison operator (<, <=, >, >=, =)", s)
	}
	rest = strings.TrimSpace(rest)

	numEnd := 0
	seenDot := false
	for numEnd < len(rest) {
		c := rest[numEnd]
		if c >= '0' && c <= '9' {
			numEnd++
			continue
		}
		if c == '.' && !seenDot {
			seenDot = true
			numEnd++
			continue
		}
		break
	}
	if numEnd == 0 {
		return sizeClause{}, fmt.Errorf("filterexpr: %q has no number after the operator", s)
	}
	numText := rest[:numEnd]
	unitText := strings.ToLower(strings.TrimSpace(rest[numEnd:]))

	multiplier, ok := sizeUnits[unitText]
	if !ok {
		return sizeClause{}, fmt.Errorf("filterexpr: unrecognized size unit %q (want b, k, m, g, or t)", unitText)
	}

	n, err := strconv.ParseFloat(numText, 64)
	if err != nil {
		return sizeClause{}, fmt.Errorf("filterexpr: %q is not a valid number", numText)
	}

	return sizeClause{op: op, bytes: int64(n * float64(multiplier))}, nil
}

// cutComparator splits the longest recognized comparator off the front
// of s — checked longest-first (">=" before ">", "==" before "=") so a
// two-character operator is never mistaken for its one-character
// prefix.
func cutComparator(s string) (sizeComparator, string, bool) {
	switch {
	case strings.HasPrefix(s, "<="):
		return sizeLessOrEqual, s[2:], true
	case strings.HasPrefix(s, ">="):
		return sizeGreaterOrEqual, s[2:], true
	case strings.HasPrefix(s, "=="):
		return sizeEqual, s[2:], true
	case strings.HasPrefix(s, "<"):
		return sizeLess, s[1:], true
	case strings.HasPrefix(s, ">"):
		return sizeGreater, s[1:], true
	case strings.HasPrefix(s, "="):
		return sizeEqual, s[1:], true
	default:
		return 0, s, false
	}
}
