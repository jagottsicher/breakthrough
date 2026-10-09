package logview

import (
	"encoding/json"
	"regexp"
	"strings"
)

// Format identifies which parser Parse should use for a whole file —
// decided once, from a small sample of its own lines (see Detect), not
// re-guessed line by line: a file that mixes formats line-to-line
// isn't one this package tries to support.
type Format int

const (
	FormatPlain Format = iota // fallback: no structure recognized
	FormatJSON
	FormatSyslogRFC3164
	FormatSyslogRFC5424
	FormatGeneric // "TIMESTAMP LEVEL message"
)

// String names Format for diagnostics and the selection screen's own
// "detected: ..." hint.
func (f Format) String() string {
	switch f {
	case FormatJSON:
		return "JSON Lines"
	case FormatSyslogRFC3164:
		return "syslog (RFC 3164)"
	case FormatSyslogRFC5424:
		return "syslog (RFC 5424)"
	case FormatGeneric:
		return "timestamp + level"
	default:
		return "plain text"
	}
}

var (
	reRFC5424 = regexp.MustCompile(`^<\d{1,3}>\d+\s+\S+\s+\S+\s+\S+\s+\S+\s+\S+\s+`)
	reRFC3164 = regexp.MustCompile(`^[A-Z][a-z]{2}\s+\d{1,2}\s+\d{2}:\d{2}:\d{2}\s+\S+\s+`)
	// [.,]\d+, not \.\d+: Python's own logging.Formatter default
	// datefmt ("2026-10-08 21:00:00,123 INFO message") uses a comma
	// before the milliseconds, not a dot — one concrete, very common
	// real-world shape this pattern used to miss entirely, falling all
	// the way back to FormatPlain for it: Level/Source always empty,
	// and the timestamp itself showing up as plain message text instead
	// of being recognized at all — matching the user's own report.
	reGenericTAB = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}[ T]\d{2}:\d{2}:\d{2}(?:[.,]\d+)?(?:Z|[+-]\d{2}:?\d{2})?\s+\[?(?i:TRACE|DEBUG|INFO|WARN(?:ING)?|ERROR|FATAL|PANIC|CRITICAL)\]?:?\s`)
)

// detectSampleSize is how many of a file's own leading non-blank lines
// Detect looks at — enough to not be fooled by one odd line (a blank
// separator, a stack-trace continuation line that doesn't match its own
// format), small enough that detection stays effectively instant even
// on a huge file.
const detectSampleSize = 20

// detectThreshold is the fraction of sampled lines that must match a
// candidate format for Detect to commit to it, rather than falling back
// to FormatPlain — a log genuinely is this format even if the odd line
// (a multi-line stack trace's own continuation, say) doesn't match on
// its own.
const detectThreshold = 0.6

// Detect sniffs sample (a file's own first detectSampleSize non-blank
// lines are enough — see ReadSample) and picks the one Format whose own
// line shape most of them match. Order matters: JSON and the two
// syslog shapes are checked first since they're unambiguous once they
// match at all, then the generic timestamp+level shape, so a file that
// doesn't commit to any of those falls back to FormatPlain rather than
// being forced into a bad fit.
func Detect(sample []string) Format {
	var lines []string
	for _, l := range sample {
		if strings.TrimSpace(l) != "" {
			lines = append(lines, l)
		}
	}
	if len(lines) == 0 {
		return FormatPlain
	}

	counts := map[Format]int{}
	for _, l := range lines {
		switch {
		case looksLikeJSON(l):
			counts[FormatJSON]++
		case reRFC5424.MatchString(l):
			counts[FormatSyslogRFC5424]++
		case reRFC3164.MatchString(l):
			counts[FormatSyslogRFC3164]++
		case reGenericTAB.MatchString(l):
			counts[FormatGeneric]++
		}
	}

	best := FormatPlain
	bestCount := 0
	for f, c := range counts {
		if c > bestCount {
			best, bestCount = f, c
		}
	}
	if float64(bestCount)/float64(len(lines)) < detectThreshold {
		return FormatPlain
	}
	return best
}

// looksLikeJSON is a cheap pre-check (first non-space byte is '{')
// before the real json.Unmarshal attempt in parseJSONLine — avoids
// running a full decode on every sampled line just to rule out JSON.
func looksLikeJSON(line string) bool {
	trimmed := strings.TrimSpace(line)
	if !strings.HasPrefix(trimmed, "{") {
		return false
	}
	var v map[string]any
	return json.Unmarshal([]byte(trimmed), &v) == nil
}
