package logview

import (
	"regexp"
	"strings"
)

// inferLevel recovers a Level from message free text when the format
// itself gave none — classic RFC 3164 syslog never carries a level at
// all, and FormatGeneric's own level token is optional. Real-world
// evidence for every case below (see the commit this file was added
// in) comes from a production host's actual /var/log/syslog, not
// assumption: AWS's amazon-ssm-agent writes its own
// "timestamp LEVEL component message" line straight into the syslog
// message (nestedStructuredLevel), a Java/Logback app writes
// "elapsed [thread] LEVEL logger - message" (reLogbackLevelToken), and
// a third app simply starts its own message with "Error: ..."
// (reLeadingLevelToken). Checked in that priority order — logfmt and
// the nested full-format parse are both an explicit, named field the
// source itself declared (inferred stays false); the two token-anchor
// regexes are a genuine guess from a message's own shape (inferred is
// true, see Entry.LevelInferred's own doc comment for what that
// controls in the UI). Returns LevelUnknown (inferred/message both
// false/"") when nothing anchored matches — never a bare substring
// search: "errors=remount-ro" (an ext4 mount option, common in real
// kernel.log), "0 errors", "no errors found", "/var/log/error.log",
// and "Terror" all contain "error" as a substring but must not be
// recovered as a Level, exactly the false-positive corpus this file's
// own test pins.
//
// message is the only input — the file's own fallback time, Source,
// etc. are untouched — called from ParseAll only when the format's own
// parseLine already left Level at LevelUnknown, never overriding a
// Level the format declared authoritatively (model.go's own doc
// comment on LevelInferred).
func inferLevel(message string) (level Level, inferred bool, extractedMessage string) {
	// mightContainLevelClue is a cheap, single-pass reject before any
	// of the four regexes below ever run — BenchmarkParseAllLargeSyslogFile
	// (900k classic-RFC-3164 lines, none of which carry a level at all)
	// measured a 2.8x slowdown (1.86s → 5.24s) without this: Go's
	// regexp backtracking engine (confirmed by CPU profile, not
	// assumed — regexp.(*Regexp).tryBacktrack alone was 76% of total
	// time) still costs real time to conclusively reject a line, even
	// one whose very first byte already can't match, once that
	// rejection happens four separate times on every single line of a
	// multi-million-line family — exactly the shape of cost this
	// project's own prior incidents (19e, cited throughout this
	// package) were about. A plain lowercase scan for one of a handful
	// of short, cheap literal fragments is far cheaper, and every real
	// case inferLevel can ever recover from starts with one of them
	// somewhere in the message — if none is even present, no regex
	// below could possibly match either. This alone brings the same
	// benchmark down to 2.13s (+15% over the 1.86s baseline with no
	// inference at all) — the remaining difference is this function's
	// own strings.ToLower allocation plus up to 13 strings.Contains
	// scans, paid once per line regardless of outcome; accepted as the
	// real, now-small cost of the feature itself rather than chased
	// further, since it no longer resembles the regex-driven blowups
	// 19e's own incidents were about.
	if !mightContainLevelClue(message) {
		return LevelUnknown, false, ""
	}
	if lvl, ok := logfmtLevel(message); ok {
		return lvl, false, ""
	}
	if lvl, msg, ok := nestedStructuredLevel(message); ok {
		return lvl, false, msg
	}
	if m := reLeadingLevelToken.FindStringSubmatch(message); m != nil {
		if lvl := ParseLevel(m[1]); lvl != LevelUnknown {
			return lvl, true, ""
		}
	}
	if m := reLogbackLevelToken.FindStringSubmatch(message); m != nil {
		if lvl := ParseLevel(m[1]); lvl != LevelUnknown {
			return lvl, true, ""
		}
	}
	return LevelUnknown, false, ""
}

// levelClueFragments are lowercase substrings of genericLevelToken's
// own full vocabulary (detect.go/model.go — kept in agreement the same
// way every other duplicate of that vocabulary in this package already
// is), short enough that every real level word contains at least one,
// plus the three logfmt key names logfmtLevel looks for. "err" alone
// covers ERROR; "warn" covers WARN/WARNING; "crit" covers
// CRITICAL; "emerg" covers EMERGENCY; the rest are exact already.
var levelClueFragments = []string{
	"trace", "debug", "dbg", "info", "notice", "warn", "err",
	"crit", "alert", "emerg", "fatal", "panic",
	"level=", "lvl=", "severity=",
}

// mightContainLevelClue is inferLevel's own cheap pre-filter — see its
// doc comment for why this exists at all. strings.ToLower allocates
// once per call, but that is still far cheaper than even a single
// failed regex match per BenchmarkParseAllLargeSyslogFile, let alone
// the four this replaces for the common case of a message with no
// level clue anywhere in it.
func mightContainLevelClue(message string) bool {
	lower := strings.ToLower(message)
	for _, frag := range levelClueFragments {
		if strings.Contains(lower, frag) {
			return true
		}
	}
	return false
}

// reLogfmtLevel matches a logfmt-style "level="/"lvl="/"severity="
// pair anywhere in the message — Go's own slog text handler,
// containerd, and Docker all emit this shape, and unlike the two
// token-anchor regexes below, an explicitly named "level=" field is
// exactly as authoritative as a JSON "level" key would be, not a
// guess.
var reLogfmtLevel = regexp.MustCompile(`(?i)(?:^|\s)(?:level|lvl|severity)=('|")?([a-zA-Z]+)`)

func logfmtLevel(message string) (Level, bool) {
	m := reLogfmtLevel.FindStringSubmatch(message)
	if m == nil {
		return LevelUnknown, false
	}
	lvl := ParseLevel(m[2])
	if lvl == LevelUnknown {
		return LevelUnknown, false
	}
	return lvl, true
}

// nestedStructuredLevel tries message itself as a whole second log
// line — real evidence this is worth doing at all: amazon-ssm-agent
// writes syslog lines whose own message is "2026-11-01
// 00:12:37.9973 WARN EC2RoleProvider Failed to connect ...", a
// complete FormatGeneric line nested inside the syslog wrapper, with
// Detect/parseGenericLine already fully equipped to parse it — no new
// parsing logic needed, just running the existing one a second time
// on the part RFC 3164 left as one opaque Message. The cheap shape
// checks (looksLikeJSON's own first-byte check, reGenericTAB's own
// match) run before the real parse attempt, so a message that is
// neither shape (the common case — a plain one-line message with no
// nested structure at all) costs one cheap check, not a wasted parse.
// extractedMessage replaces Entry.Message with whatever the nested
// parse itself extracted (per the user's own explicit request,
// 2026-11-06) — Raw still keeps the full original line untouched.
func nestedStructuredLevel(message string) (level Level, extractedMessage string, ok bool) {
	trimmed := strings.TrimSpace(message)
	if trimmed == "" {
		return LevelUnknown, "", false
	}
	if looksLikeJSON(trimmed) {
		if e, parsed := parseJSONLine(trimmed); parsed && e.Level != LevelUnknown {
			return e.Level, e.Message, true
		}
		return LevelUnknown, "", false
	}
	if reGenericTAB.MatchString(trimmed) {
		if e, parsed := parseGenericLine(trimmed); parsed && e.Level != LevelUnknown {
			return e.Level, e.Message, true
		}
	}
	return LevelUnknown, "", false
}

// reLeadingLevelToken anchors genericLevelToken's own vocabulary
// (detect.go — kept in exact agreement with ParseLevel the same way
// reGenericTAB/reGenericFields already are) at the very start of
// message, optionally bracket/paren-wrapped and followed by ":"/"-"/
// "|" or nothing before the real text — "Error: 404 Not Found",
// "[WARN] disk usage high", "(INFO) starting up". Anchoring at ^
// rather than searching anywhere is what rules out "0 errors",
// "errors=remount-ro", "/var/log/error.log", and "no errors found" —
// none of those have the level word at the very start of the message
// — and the trailing \b (inherited from genericLevelToken) rules out
// "Terror" the same way reGenericFields' own level group already
// does mid-line.
var reLeadingLevelToken = regexp.MustCompile(`(?i)^\s*[\[<(]?\s*` + genericLevelToken + `\b[\]>)]?\s*[:|-]?\s`)

// reLogbackLevelToken anchors the same vocabulary against a second
// real, user-confirmed shape: a Java/Logback-style
// "<elapsed> [<thread>] LEVEL <logger> - message" line — e.g.
// "209860721 [scheduling-1] DEBUG n.j.s.c.DefaultLockingTaskExecutor
// - Locked ...", found directly in the same production syslog
// reLeadingLevelToken's own doc comment cites. Unlike
// reLeadingLevelToken, the level word here follows a numeric prefix
// and a bracketed thread name, never right at the start of the
// message — a genuinely different shape, not a looser version of the
// same regex.
var reLogbackLevelToken = regexp.MustCompile(`(?i)^\d+\s+\[[^\]]*\]\s+` + genericLevelToken + `\b`)
