package filterexpr

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// mtimeUnits maps a case-folded relative unit name (singular, plural,
// and a couple of common abbreviations) to its Duration. Month/year are
// necessarily approximate (30/365 days) — there's no single "correct"
// duration for either once a day stops being a fixed 24 hours, and a
// filter deciding "does this look about right" has no need for the
// calendar-exact precision a billing system would.
var mtimeUnits = map[string]time.Duration{
	"sec": time.Second, "secs": time.Second, "second": time.Second, "seconds": time.Second,
	"min": time.Minute, "mins": time.Minute, "minute": time.Minute, "minutes": time.Minute,
	"hour": time.Hour, "hours": time.Hour, "hr": time.Hour, "hrs": time.Hour,
	"day": 24 * time.Hour, "days": 24 * time.Hour,
	"week": 7 * 24 * time.Hour, "weeks": 7 * 24 * time.Hour,
	"month": 30 * 24 * time.Hour, "months": 30 * 24 * time.Hour,
	"year": 365 * 24 * time.Hour, "years": 365 * 24 * time.Hour,
}

// relativePattern matches a relative moment: an optional leading
// "last", a number, a unit name, and an optional trailing "ago" — all
// three of "7 days", "last 7 days", and "7 days ago" resolve the same
// way (see parseMoment), the extra words are accepted purely so
// whichever one reads naturally in context ("after 2 hours ago") isn't
// rejected on a technicality.
var relativePattern = regexp.MustCompile(`(?i)^(?:last\s+)?([0-9]+(?:\.[0-9]+)?)\s*([a-z]+)\s*(?:ago)?$`)

// absoluteLayouts is every date/time spelling ParseMtime accepts for an
// absolute moment, tried in this order (most specific first, so
// "2026-09-01 14:30:00" is never mistaken for the shorter
// "2026-09-01 14:30" layout's own leftover text getting silently
// dropped). Every layout but RFC3339 carries no zone of its own, so
// those are parsed against time.Local — the same zone a file's own
// ModTime is already expressed in, meaning "2026-09-01" means midnight
// in the system's own local time, not UTC.
var absoluteLayouts = []string{
	time.RFC3339,
	"2006-01-02T15:04:05",
	"2006-01-02 15:04:05",
	"2006-01-02 15:04",
	"2006-01-02",
}

// MtimeFilter is a parsed modified-time expression — see ParseMtime.
type MtimeFilter struct {
	match func(t time.Time) bool
}

// Match reports whether t (an entry's own ModTime) satisfies the
// parsed expression.
func (f MtimeFilter) Match(t time.Time) bool {
	return f.match(t)
}

// ParseMtime parses a modified-time expression against now (the
// instant every relative clause measures itself against — see this
// package's own doc comment on why that's a parameter here, not a call
// to time.Now() inside):
//
//   - "before <moment>" — modified strictly before moment
//   - "after <moment>" — modified strictly after moment
//   - "between <moment> and <moment>" — modified between the two,
//     inclusive, in either order (whichever moment is actually earlier
//     is always treated as the range's own start, regardless of which
//     one was written first)
//   - a bare moment on its own, with no before/after/between keyword —
//     shorthand for "after (now - moment)", i.e. "modified within the
//     last ...": "last 7 days" and "7 days" mean exactly the same thing
//
// moment itself is either an absolute date/time (see absoluteLayouts)
// or a relative one — "<number> <unit>", optionally preceded by "last"
// and/or followed by "ago" (both accepted purely for readability, e.g.
// "after 2 hours ago") — see mtimeUnits for every unit name recognized.
func ParseMtime(expr string, now time.Time) (MtimeFilter, error) {
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return MtimeFilter{}, fmt.Errorf("filterexpr: empty modified-time expression")
	}

	lower := strings.ToLower(expr)
	switch {
	case strings.HasPrefix(lower, "before "):
		moment, err := parseMoment(expr[len("before "):], now)
		if err != nil {
			return MtimeFilter{}, err
		}
		return MtimeFilter{match: func(t time.Time) bool { return t.Before(moment) }}, nil

	case strings.HasPrefix(lower, "after "):
		moment, err := parseMoment(expr[len("after "):], now)
		if err != nil {
			return MtimeFilter{}, err
		}
		return MtimeFilter{match: func(t time.Time) bool { return t.After(moment) }}, nil

	case strings.HasPrefix(lower, "between "):
		parts := splitOnAnd(expr[len("between "):])
		if len(parts) != 2 {
			return MtimeFilter{}, fmt.Errorf("filterexpr: %q needs exactly two moments joined by \"and\" (\"between X and Y\")", expr)
		}
		a, err := parseMoment(parts[0], now)
		if err != nil {
			return MtimeFilter{}, err
		}
		b, err := parseMoment(parts[1], now)
		if err != nil {
			return MtimeFilter{}, err
		}
		start, end := a, b
		if end.Before(start) {
			start, end = end, start
		}
		return MtimeFilter{match: func(t time.Time) bool {
			return !t.Before(start) && !t.After(end)
		}}, nil

	default:
		// A bare relative/absolute moment with no keyword means "at
		// least this recent" — parseMoment already resolves "7 days" to
		// a real instant (now minus 7 days), so this is the same After
		// check "after 7 days" would use explicitly.
		moment, err := parseMoment(expr, now)
		if err != nil {
			return MtimeFilter{}, err
		}
		return MtimeFilter{match: func(t time.Time) bool { return t.After(moment) }}, nil
	}
}

// parseMoment resolves one side of an expression — either relative
// ("7 days", "2 hours ago", "last 30 minutes") or absolute (see
// absoluteLayouts) — into a real instant, measuring any relative
// clause backward from now.
func parseMoment(s string, now time.Time) (time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, fmt.Errorf("filterexpr: missing a date/time")
	}

	if m := relativePattern.FindStringSubmatch(s); m != nil {
		n, err := strconv.ParseFloat(m[1], 64)
		if err != nil {
			return time.Time{}, fmt.Errorf("filterexpr: %q is not a valid number", m[1])
		}
		unit, ok := mtimeUnits[strings.ToLower(m[2])]
		if !ok {
			return time.Time{}, fmt.Errorf("filterexpr: unrecognized time unit %q", m[2])
		}
		return now.Add(-time.Duration(n * float64(unit))), nil
	}

	for _, layout := range absoluteLayouts {
		if layout == time.RFC3339 {
			if t, err := time.Parse(layout, s); err == nil {
				return t, nil
			}
			continue
		}
		if t, err := time.ParseInLocation(layout, s, time.Local); err == nil {
			return t, nil
		}
	}

	return time.Time{}, fmt.Errorf("filterexpr: %q is neither a relative time (\"7 days\", \"2 hours ago\") nor a recognized date/time (\"2026-09-01\", \"2026-09-01 14:30\")", s)
}
