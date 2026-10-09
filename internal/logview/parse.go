package logview

import (
	"bufio"
	"encoding/json"
	"io"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// ReadSample reads up to detectSampleSize non-blank lines from r,
// without consuming more of it than that — callers that still need the
// rest of the stream (there are none today; ParseAll re-reads from a
// fresh Open instead) would need their own io.MultiReader around the
// already-buffered part.
func ReadSample(r io.Reader) []string {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	var lines []string
	for len(lines) < detectSampleSize && scanner.Scan() {
		if strings.TrimSpace(scanner.Text()) != "" {
			lines = append(lines, scanner.Text())
		}
	}
	return lines
}

// ParseAll reads every line of r, detects its format from the first
// lines, and parses the whole stream against that one format —
// fileName is stamped onto every Entry.Line/Entry.File, fallbackTime
// seeds a line's own Time when the format or line itself carries none
// at all (RFC 3164's own missing year, or FormatPlain's total absence
// of a timestamp) so entries from a timestamp-less file still sort
// close to where they actually belong instead of all collapsing onto
// the zero time.
func ParseAll(r io.Reader, fileName string, fallbackTime time.Time) ([]Entry, Format, error) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	var lines []string
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		return nil, FormatPlain, err
	}

	sampleN := detectSampleSize
	if sampleN > len(lines) {
		sampleN = len(lines)
	}
	format := Detect(lines[:sampleN])

	if format == FormatAptHistory {
		return parseAptHistoryEntries(lines, fileName, fallbackTime), format, nil
	}
	if format == FormatEIPP {
		return parseEIPPEntries(lines, fileName, fallbackTime), format, nil
	}

	entries := make([]Entry, 0, len(lines))
	for i, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		e := parseLine(line, format, fallbackTime)
		e.Raw = line
		e.File = fileName
		e.Line = i + 1
		if e.Time.IsZero() {
			e.Time = fallbackTime
		}
		entries = append(entries, e)
	}
	return entries, format, nil
}

func parseLine(line string, format Format, fallbackTime time.Time) Entry {
	switch format {
	case FormatJSON:
		if e, ok := parseJSONLine(line); ok {
			return e
		}
	case FormatSyslogRFC5424:
		if e, ok := parseRFC5424Line(line); ok {
			return e
		}
	case FormatSyslogRFC3164:
		if e, ok := parseRFC3164Line(line, fallbackTime.Year()); ok {
			return e
		}
	case FormatGeneric:
		if e, ok := parseGenericLine(line); ok {
			return e
		}
	case FormatCLF:
		if e, ok := parseCLFLine(line); ok {
			return e
		}
	}
	return Entry{Message: line}
}

// jsonTimeKeys/jsonLevelKeys/jsonMessageKeys/jsonSourceKeys are, in
// priority order, the field names real structured loggers actually use
// — slog/zap/logrus's own JSON encoders ("time"/"level"/"msg"), and
// Docker's json-file log driver ("time"... "log", no level/source at
// all, which parseJSONLine's own empty-Level/Source result already
// handles plainly).
var (
	jsonTimeKeys    = []string{"time", "ts", "timestamp", "@timestamp"}
	jsonLevelKeys   = []string{"level", "lvl", "severity", "loglevel"}
	jsonMessageKeys = []string{"msg", "message", "log"}
	jsonSourceKeys  = []string{"logger", "service", "source", "container", "stream", "name"}
)

func parseJSONLine(line string) (Entry, bool) {
	var fields map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(line)), &fields); err != nil {
		return Entry{}, false
	}

	e := Entry{}
	if v := firstStringField(fields, jsonTimeKeys); v != "" {
		e.Time = parseAnyTime(v)
	}
	if v := firstStringField(fields, jsonLevelKeys); v != "" {
		e.Level = ParseLevel(v)
	}
	e.Source = firstStringField(fields, jsonSourceKeys)
	e.Message = firstStringField(fields, jsonMessageKeys)
	if e.Message == "" {
		// No recognized message field (an unfamiliar JSON log schema) —
		// the raw line is still far more useful than an empty cell.
		e.Message = line
	}
	return e, true
}

func firstStringField(fields map[string]any, keys []string) string {
	for _, k := range keys {
		if v, ok := fields[k]; ok {
			if s, ok := v.(string); ok {
				return s
			}
		}
	}
	return ""
}

// reRFC5424Fields captures PRI, VERSION, TIMESTAMP, HOSTNAME,
// APP-NAME, PROCID, MSGID, and the rest (structured data + message) —
// RFC 5424 §6.
var reRFC5424Fields = regexp.MustCompile(`^<(\d{1,3})>(\d+)\s+(\S+)\s+(\S+)\s+(\S+)\s+(\S+)\s+(\S+)\s+(.*)$`)

func parseRFC5424Line(line string) (Entry, bool) {
	m := reRFC5424Fields.FindStringSubmatch(line)
	if m == nil {
		return Entry{}, false
	}
	pri, _ := strconv.Atoi(m[1])
	severity := pri % 8
	rest := m[8]
	// Structured data ("[...]" or "-") precedes the actual message —
	// stripped rather than shown, since it's metadata, not the message
	// a keyword search should match against.
	if strings.HasPrefix(rest, "-") {
		rest = strings.TrimSpace(strings.TrimPrefix(rest, "-"))
	} else if strings.HasPrefix(rest, "[") {
		if idx := strings.Index(rest, "] "); idx >= 0 {
			rest = rest[idx+2:]
		}
	}
	return Entry{
		Time:    parseAnyTime(m[3]),
		Level:   rfc5424SeverityLevel(severity),
		Source:  m[5], // APP-NAME
		Message: rest,
	}, true
}

func rfc5424SeverityLevel(severity int) Level {
	switch severity {
	case 0, 1, 2, 3:
		return LevelError
	case 4:
		return LevelWarn
	case 5, 6:
		return LevelInfo
	case 7:
		return LevelDebug
	default:
		return LevelUnknown
	}
}

// reRFC3164Fields captures TIMESTAMP (no year for the classic
// alternative — RFC 3164 §4.1's own "Mon _2 15:04:05 host tag[pid]:
// message" shape — a real year for reISO8601SyslogTimestamp, see its
// own doc comment in detect.go for why a second timestamp shape is
// accepted here too), HOSTNAME, TAG (with optional "[pid]"), and
// MESSAGE.
var reRFC3164Fields = regexp.MustCompile(`^([A-Z][a-z]{2}\s+\d{1,2}\s+\d{2}:\d{2}:\d{2}|` + reISO8601SyslogTimestamp + `)\s+(\S+)\s+([^:\[\s]+)(?:\[\d+\])?:\s*(.*)$`)

func parseRFC3164Line(line string, year int) (Entry, bool) {
	m := reRFC3164Fields.FindStringSubmatch(line)
	if m == nil {
		return Entry{}, false
	}
	// The classic alternative carries no year of its own (fallback
	// year below); the ISO8601 one already has a complete, unambiguous
	// timestamp — parseAnyTime (which Time.Parse's own classic layout
	// can never match, so trying it first costs nothing) handles it
	// directly, including its own real year.
	t, err := time.Parse("Jan _2 15:04:05", m[1])
	if err == nil {
		t = t.AddDate(year, 0, 0)
	} else {
		t = parseAnyTime(m[1])
		if t.IsZero() {
			return Entry{}, false
		}
	}
	return Entry{
		Time:    t,
		Source:  m[3],
		Message: m[4],
	}, true
}

// reGenericFields captures TIMESTAMP, an optional LEVEL, and MESSAGE
// for the "2026-10-07 16:04:23 ERROR something happened" shape — the
// same pattern detect.go's own reGenericTAB already uses to recognize
// the format in the first place, just with the parts split into groups
// here.
// [.,]\d+, not \.\d+ — see reGenericTAB's own doc comment (detect.go)
// for why: Python's logging.Formatter default datefmt uses a comma
// before milliseconds, not a dot.
//
// The level group is wrapped in its own optional "(?:...)?" — a real,
// user-reported gap: a line with a perfectly good, parseable timestamp
// but no level at all (dpkg.log's own "status installed pkg:amd64
// 1.0", one concrete example from the user's own real files) used to
// match neither this nor any other format, falling all the way back to
// FormatPlain — Time lost entirely, not just Level/Source, for the
// sole reason that no level happened to follow. m[2] is "" in that
// case; ParseLevel("") already correctly answers LevelUnknown (see its
// own test), the same honest "no level recognized" this format already
// gives a line whose level word isn't one of the known ones.
// [-/], not just "-" — see reGenericTAB's own doc comment (detect.go)
// for why: TeamViewer's own log format uses slashes in its date.
// genericTimestampAlt/genericLevelToken (detect.go) add PHP-FPM's own
// "[10-Oct-2026 13:55:36]" and Apache's own error log
// "[Thu Oct 09 13:55:36.123456 2026]" timestamps, and broaden the
// level vocabulary to match ParseLevel's own full synonym list
// (notice/crit/alert/emerg/..., not just the original handful) — see
// their own doc comments for the full reasoning (nginx/apache2/
// php-fpm, explicitly asked about).
var reGenericFields = regexp.MustCompile(`(?i)^(\d{4}[-/]\d{2}[-/]\d{2}[ T]\d{2}:\d{2}:\d{2}(?:[.,]\d+)?(?:Z|[+-]\d{2}:?\d{2})?|` + genericTimestampAlt + `)\s+(?:\[?` + genericLevelToken + `\]?:?\s+)?(.*)$`)

func parseGenericLine(line string) (Entry, bool) {
	m := reGenericFields.FindStringSubmatch(line)
	if m == nil {
		return Entry{}, false
	}
	// m[1] carries its own brackets intact for the two bracket-wrapped
	// alternatives (PHP-FPM/Apache) — stripped here rather than from
	// within the regex itself, so the same single capture group still
	// works for the plain ISO-ish alternative, which never has them.
	ts := strings.TrimSuffix(strings.TrimPrefix(m[1], "["), "]")
	return Entry{
		Time:    parseAnyTime(ts),
		Level:   ParseLevel(m[2]),
		Message: m[3],
	}, true
}

// reCLFFields captures HOST and TIMESTAMP from the Apache/CUPS Common/
// Combined Log Format shape (see reCLF's own doc comment, detect.go) —
// IDENT/AUTHUSER (almost always "-") are matched but not captured,
// nothing useful to show. Everything from the quoted request line
// onward (status, size, and whatever else "combined" format tacks on —
// referer, user-agent, or CUPS's own trailing job-status words) stays
// together as Message rather than being split into fields of its own:
// none of them map onto Entry's own Level/Source, and showing the
// whole thing verbatim is more useful than discarding any of it.
var reCLFFields = regexp.MustCompile(`^(\S+)\s+\S+\s+\S+\s+\[(\d{2}/[A-Za-z]{3}/\d{4}:\d{2}:\d{2}:\d{2}\s[+-]\d{4})\]\s+(.*)$`)

// clfTimeLayout is Apache/CUPS's own bracketed access-log timestamp —
// "09/Oct/2026:13:55:36 +0200", the one true fixed shape this format
// ever uses, so a dedicated layout (rather than parseAnyTime's whole
// list) is both correct and slightly cheaper.
const clfTimeLayout = "02/Jan/2006:15:04:05 -0700"

func parseCLFLine(line string) (Entry, bool) {
	m := reCLFFields.FindStringSubmatch(line)
	if m == nil {
		return Entry{}, false
	}
	t, err := time.Parse(clfTimeLayout, m[2])
	if err != nil {
		return Entry{}, false
	}
	return Entry{
		Time:    t,
		Source:  m[1],
		Message: m[3],
	}, true
}

// aptHistoryStartPrefix/aptHistoryEndPrefix are apt history.log's own
// two timestamped field labels — every other field (Commandline,
// Install, Upgrade, Remove, Purge, Downgrade, Reinstall, Requested-By)
// carries no timestamp of its own at all.
const (
	aptHistoryStartPrefix = "Start-Date:"
	aptHistoryEndPrefix   = "End-Date:"
)

// parseAptHistoryEntries is apt history.log's own dedicated parsing
// pass — genuinely different from every other format's parseXLine
// function, which ParseAll calls once per line with no memory of
// what came before: a real apt history.log transaction spans several
// lines (Start-Date, an optional Requested-By, Commandline, one or
// more of Install/Upgrade/Remove/..., End-Date), and only Start-Date/
// End-Date carry a timestamp of their own — "Commandline: apt install
// nodejs" doesn't. Every other line in the same transaction is given
// the most recently seen Start-Date instead (current, carried forward
// across the loop) rather than falling back to the whole file's own
// mtime the way an ordinary timestamp-less line would — still
// approximate (a transaction can span several minutes, see Start-
// Date/End-Date in the user's own real file), but far closer than one
// flat fallback for the entire file, and correctly distinct between
// one transaction and the next. Confirmed against the user's own real
// history.log, including multiple rotated files sharing one family
// (see logaudit.go's own Merge call, which this feeds into exactly
// like every other format's entries).
func parseAptHistoryEntries(lines []string, fileName string, fallbackTime time.Time) []Entry {
	entries := make([]Entry, 0, len(lines))
	current := fallbackTime
	for i, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		t := current
		if start, ok := parseAptHistoryTimestamp(line, aptHistoryStartPrefix); ok {
			current = start
			t = start
		} else if end, ok := parseAptHistoryTimestamp(line, aptHistoryEndPrefix); ok {
			// End-Date's own real timestamp for its own line only —
			// current stays at the transaction's Start-Date for
			// whatever (if anything) still follows before the next
			// Start-Date resets it.
			t = end
		}
		entries = append(entries, Entry{
			Time:    t,
			Message: line,
			Raw:     line,
			File:    fileName,
			Line:    i + 1,
		})
	}
	return entries
}

// parseAptHistoryTimestamp reports line's own timestamp if it starts
// with prefix, false otherwise. strings.Fields/strings.Join collapses
// whatever whitespace actually separates the date and time — the
// user's own real file uses two spaces ("2026-02-25  21:04:11"), but a
// layout hardcoded to that exact count would be brittle for no real
// reason — into exactly one, which "2006-01-02 15:04:05" can then
// parse unconditionally.
func parseAptHistoryTimestamp(line, prefix string) (time.Time, bool) {
	if !strings.HasPrefix(line, prefix) {
		return time.Time{}, false
	}
	normalized := strings.Join(strings.Fields(strings.TrimPrefix(line, prefix)), " ")
	t, err := time.Parse("2006-01-02 15:04:05", normalized)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}

// eippPackagePrefix is eipp.log's own stanza header — the one field
// every other field in the same stanza is grouped under (see
// parseEIPPEntries' own doc comment).
const eippPackagePrefix = "Package:"

// parseEIPPEntries is eipp.log's own dedicated parsing pass — the same
// stateful shape parseAptHistoryEntries already uses, carrying a
// stanza's own "Package: " value forward as Source for every other
// field line in that same stanza, reset at each blank line (a real
// eipp.log always starts a fresh stanza with its own "Package:" line
// right after one — see FormatEIPP's own doc comment, detect.go, for
// why). There is no timestamp anywhere in this format at all, by
// design — a snapshot of package state, not a sequence of timed
// events — so every entry's own Time is simply fallbackTime
// throughout; confirmed against the user's own real eipp.log, and the
// user's own explicit choice to still want this Source grouping once
// told plainly that no real timestamp exists to recover here.
func parseEIPPEntries(lines []string, fileName string, fallbackTime time.Time) []Entry {
	entries := make([]Entry, 0, len(lines))
	currentPackage := ""
	for i, line := range lines {
		if strings.TrimSpace(line) == "" {
			currentPackage = ""
			continue
		}
		if pkg, ok := strings.CutPrefix(line, eippPackagePrefix); ok {
			currentPackage = strings.TrimSpace(pkg)
		}
		entries = append(entries, Entry{
			Time:    fallbackTime,
			Source:  currentPackage,
			Message: line,
			Raw:     line,
			File:    fileName,
			Line:    i + 1,
		})
	}
	return entries
}

// timeLayouts are tried in order by parseAnyTime — RFC3339(Nano) first
// (what every JSON/generic encoder above actually produces in
// practice), then the handful of other shapes a hand-written logger
// might use instead.
var timeLayouts = []string{
	time.RFC3339Nano,
	time.RFC3339,
	"2006-01-02T15:04:05",
	"2006-01-02 15:04:05.999999999",
	// The comma variant of the layout above — Go's time package treats
	// "," exactly like "." as the fractional-seconds separator (verified
	// directly against time.Parse, not assumed) — for Python's own
	// logging.Formatter default datefmt; see reGenericTAB's own doc
	// comment (detect.go) for the full reasoning.
	"2006-01-02 15:04:05,999999999",
	"2006-01-02 15:04:05",
	// TeamViewer's own date separator ("2023/12/31 22:33:22.756") —
	// see reGenericTAB's own doc comment (detect.go) for the full
	// reasoning.
	"2006/01/02 15:04:05.999999999",
	"2006/01/02 15:04:05",
	// PHP-FPM's own date shape ("10-Oct-2026 13:55:36") — see
	// genericTimestampAlt's own doc comment (detect.go).
	"02-Jan-2006 15:04:05",
	// Apache's own error log timestamp (httpd's ErrorLogFormat default
	// %{u}t, "Thu Oct 09 13:55:36.123456 2026") — both zero-padded and
	// space-padded day, with and without fractional seconds, since
	// Go's time.Parse treats "02" and "_2" as genuinely different
	// layouts rather than one lenient one.
	"Mon Jan 02 15:04:05.999999999 2006",
	"Mon Jan 02 15:04:05 2006",
	"Mon Jan _2 15:04:05.999999999 2006",
	"Mon Jan _2 15:04:05 2006",
}

// parseAnyTime tries every timeLayouts entry and returns the zero Time
// on total failure — a line whose timestamp doesn't parse keeps
// everything else about it (see parseLine's own fallbackTime handling
// for what happens next), rather than being discarded outright.
func parseAnyTime(s string) time.Time {
	s = strings.TrimSpace(s)
	for _, layout := range timeLayouts {
		if t, err := time.Parse(layout, s); err == nil {
			return t
		}
	}
	return time.Time{}
}
