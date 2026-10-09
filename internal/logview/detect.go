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
	FormatGeneric // "TIMESTAMP [LEVEL] message" — level is optional
	// FormatCLF is the Apache/CUPS Common/Combined Log Format access
	// log shape — "HOST - - [DD/Mon/YYYY:HH:MM:SS +ZZZZ] \"REQUEST\"
	// STATUS SIZE ...", confirmed against the user's own real CUPS
	// access_log. Checked separately from every format above: its own
	// timestamp isn't at the start of the line at all (host/ident/
	// authuser come first), so it can never collide with any of them.
	FormatCLF
	// FormatAptHistory is apt's own /var/log/apt/history.log — a
	// multi-line record format unlike every other format here: a
	// transaction spans several lines (Start-Date, an optional
	// Requested-By, Commandline, any of Install/Upgrade/Remove/Purge/
	// Downgrade/Reinstall, End-Date), and only Start-Date/End-Date
	// carry their own timestamp at all — "Commandline: apt install
	// nodejs" has none of its own. Confirmed against the user's own
	// real history.log. See parseAptHistoryEntries' own doc comment
	// (parse.go) for why this is the one format ParseAll gives a
	// genuinely different, stateful parsing pass instead of the
	// ordinary line-by-line parseLine loop every other format uses.
	FormatAptHistory
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
		return "timestamp"
	case FormatCLF:
		return "access log (Apache/nginx/CUPS)"
	case FormatAptHistory:
		return "apt history.log"
	default:
		return "plain text"
	}
}

// reISO8601SyslogTimestamp is the timestamp alternative modern rsyslog
// installs actually use by default (its own "high precision"/
// RFC3339-ish RSYSLOG_FileFormat, as opposed to the classic BSD-style
// RSYSLOG_TraditionalFileFormat RFC 3164 §4.1 itself specifies) —
// "2026-10-09T10:48:44.551263+02:00" rather than "Oct  9 10:48:44". A
// real, user-reported gap: every field after it (hostname, tag,
// message) is structurally identical RFC 3164 either way, but this
// timestamp shape matched neither the classic alternative below nor
// reGenericTAB (which requires a recognized level token — bare syslog
// has none at all, by design), so these lines fell all the way back to
// FormatPlain: Level/Source empty, and the real timestamp showing up
// as part of the message instead of being recognized, confirmed across
// several of the user's own real /var/log files (syslog, kern.log) on
// more than one machine. Shared between reRFC3164 (detection) and
// reRFC3164Fields (parse.go) so both stay in exact agreement.
const reISO8601SyslogTimestamp = `\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?(?:Z|[+-]\d{2}:\d{2})`

var (
	reRFC5424 = regexp.MustCompile(`^<\d{1,3}>\d+\s+\S+\s+\S+\s+\S+\s+\S+\s+\S+\s+`)
	// The ISO8601 alternative additionally requires HOSTNAME then
	// TAG[PID]: (the same structure reRFC3164Fields' own stricter
	// parsing regex already requires) rather than just "timestamp,
	// anything" — reGenericTAB's own timestamp shape (below) looks
	// identical up to this point, and without that extra structural
	// requirement a plain "TIMESTAMP LEVEL message" line would
	// wrongly match here first (this case runs before reGenericTAB in
	// Detect's own switch) and never reach its correct format at all.
	// The classic BSD-timestamp alternative carries no such risk (a
	// genuinely different shape from reGenericTAB's own ISO8601-only
	// timestamp), so it's left exactly as loose as before.
	reRFC3164 = regexp.MustCompile(`^(?:[A-Z][a-z]{2}\s+\d{1,2}\s+\d{2}:\d{2}:\d{2}\s+\S+\s+|` + reISO8601SyslogTimestamp + `\s+\S+\s+[^:\[\s]+(?:\[\d+\])?:\s)`)
	// [.,]\d+, not \.\d+: Python's own logging.Formatter default
	// datefmt ("2026-10-08 21:00:00,123 INFO message") uses a comma
	// before the milliseconds, not a dot — one concrete, very common
	// real-world shape this pattern used to miss entirely, falling all
	// the way back to FormatPlain for it: Level/Source always empty,
	// and the timestamp itself showing up as plain message text instead
	// of being recognized at all — matching the user's own report.
	//
	// The level token itself is now optional ("(?:...)?" wraps the
	// whole bracket-level-colon group) — a second, separate real gap:
	// a line with a perfectly good, parseable timestamp but no level at
	// all (dpkg.log's own "2026-09-30 20:05:33 status installed
	// pkg:amd64 1.0", one concrete example from the user's own real
	// files) used to fall all the way back to FormatPlain too, for the
	// sole reason that it has no level — the exact complaint "alles als
	// message zu behandeln, wenn man die Zeit nicht lesen kann, ist
	// sehr dürftig" was raised over. Detect's own switch still checks
	// reRFC3164 first, so a line that actually does have the
	// hostname+tag: structure above is never miscounted as this
	// instead, even though both now accept the exact same timestamp
	// shape.
	// \d{4}[-/]\d{2}[-/]\d{2}, not just dashes: TeamViewer's own log
	// format uses slashes ("2023/12/31 22:33:22.756 11924 11924 S!!
	// message") — a real, user-reported gap, same symptom as the
	// comma-milliseconds and no-level gaps above: the timestamp itself
	// fell back to FormatPlain entirely rather than just losing Level/
	// Source, for the sole reason that the date used "/" instead of
	// "-". TeamViewer's own two numeric PID/TID fields right after the
	// timestamp, and its own "S"/"S!!" severity marker, aren't a
	// recognized level, so the whole "PID TID S!! message" remainder
	// lands in Message, same as dpkg.log's own level-less lines — still
	// a real improvement over losing the timestamp outright.
	// genericTimestampAlt adds two more timestamp shapes, both bracket-
	// wrapped, both real user-reported gaps (nginx/apache2/php-fpm
	// explicitly asked about): PHP-FPM's own "[10-Oct-2026 13:55:36]"
	// and Apache's own error log "[Thu Oct 09 13:55:36.123456 2026]"
	// (httpd's ErrorLogFormat default %{u}t). Nginx's own error log
	// ("2026/10/09 13:55:36 [error] ...") needs no new timestamp
	// alternative at all — its slash-separated date already matches the
	// plain ISO-ish alternative above (see its own "[-/]" doc comment).
	// Shared between reGenericTAB (detection) and reGenericFields
	// (parse.go) so both stay in exact agreement.
	genericTimestampAlt = `\[\d{2}-[A-Za-z]{3}-\d{4} \d{2}:\d{2}:\d{2}\]|\[[A-Za-z]{3} [A-Za-z]{3} \d{1,2} \d{2}:\d{2}:\d{2}(?:\.\d+)? \d{4}\]`
	// genericLevelToken mirrors ParseLevel's own full vocabulary
	// (model.go) exactly, not just the handful reGenericTAB originally
	// checked — nginx's own "notice"/"crit"/"alert"/"emerg" and
	// PHP-FPM's own "NOTICE"/"ALERT" are levels ParseLevel already knew
	// how to map, just never reachable because this regex's own token
	// list never captured them in the first place. An optional
	// "MODULE:" prefix inside the brackets (`(?:[A-Za-z0-9_]+:)?`)
	// covers Apache's own newer "[core:error]" shape, module name
	// first, level second, both inside the one bracket pair.
	// The level word itself is a real capture group (used by
	// reGenericFields, parse.go), harmless extra bookkeeping for
	// reGenericTAB's own purely boolean MatchString use here.
	genericLevelToken = `(?:[A-Za-z0-9_]+:)?(TRACE|DEBUG|DBG|INFO(?:RMATION)?|NOTICE|WARN(?:ING)?|ERR(?:OR)?|CRIT(?:ICAL)?|ALERT|EMERG(?:ENCY)?|FATAL|PANIC)`
	reGenericTAB      = regexp.MustCompile(`(?i)^(?:\d{4}[-/]\d{2}[-/]\d{2}[ T]\d{2}:\d{2}:\d{2}(?:[.,]\d+)?(?:Z|[+-]\d{2}:?\d{2})?|` + genericTimestampAlt + `)\s+(?:\[?` + genericLevelToken + `\]?:?\s+)?\S`)
	// reCLF is the Apache/CUPS Common/Combined Log Format access log
	// shape — "HOST IDENT AUTHUSER [DD/Mon/YYYY:HH:MM:SS +ZZZZ]
	// "REQUEST" STATUS SIZE ...". Confirmed against the user's own real
	// CUPS access_log; the exact same shape Apache's and nginx's own
	// default "combined"/"common" access log formats already use (both
	// explicitly asked about) — CUPS deliberately reuses Apache's own
	// format for this file. No level/timestamp-first structure here at
	// all (host comes first), so unlike every format above, this one
	// never risks colliding with any of them.
	reCLF = regexp.MustCompile(`^\S+\s+\S+\s+\S+\s+\[\d{2}/[A-Za-z]{3}/\d{4}:\d{2}:\d{2}:\d{2}\s[+-]\d{4}\]\s+"`)
	// reAptHistoryField matches any of apt history.log's own fixed
	// field labels — every non-blank line in a real history.log starts
	// with one of these (see FormatAptHistory's own doc comment for
	// the full shape), so this alone is enough to recognize the whole
	// file; parseAptHistoryEntries (parse.go) does the real,
	// stateful per-field work.
	reAptHistoryField = regexp.MustCompile(`^(?:Start-Date|End-Date|Commandline|Requested-By|Install|Upgrade|Remove|Purge|Downgrade|Reinstall|Error):\s`)
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
// match at all, then the generic timestamp (optionally + level) shape, so a file that
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
		case reCLF.MatchString(l):
			counts[FormatCLF]++
		case reAptHistoryField.MatchString(l):
			counts[FormatAptHistory]++
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
