package activitylog

import (
	"fmt"
	"strings"
	"time"
)

// Entry is one recorded line — see this package's own doc comment for
// why a plain, greppable text line rather than JSON or a binary
// format.
type Entry struct {
	Time     time.Time
	Level    Level
	Category Category
	Message  string
}

// Format renders e as the exact line Writer appends, and ParseLine
// reads back: RFC3339 timestamp, level, category, then the message —
// each field space-separated, the message always last so it's free to
// contain its own spaces (but never a newline — see sanitizeMessage's
// own doc comment for why that's guaranteed before an Entry is ever
// built, not re-checked here).
func (e Entry) Format() string {
	return fmt.Sprintf("%s %s %s %s\n", e.Time.Format(time.RFC3339), e.Level, e.Category, e.Message)
}

// ParseLine reads back one line Format produced — the in-app log
// viewer's own way of turning a real log file back into Entry values
// to filter and display, rather than a second, parallel record of
// what was logged. Empty or malformed lines (a truncated write, a line
// a human hand-edited into the file) are reported as ok == false
// rather than a zero Entry that could be mistaken for a real one.
func ParseLine(line string) (Entry, bool) {
	line = strings.TrimRight(line, "\r\n")
	if line == "" {
		return Entry{}, false
	}
	parts := strings.SplitN(line, " ", 4)
	if len(parts) != 4 {
		return Entry{}, false
	}
	t, err := time.Parse(time.RFC3339, parts[0])
	if err != nil {
		return Entry{}, false
	}
	return Entry{Time: t, Level: ParseLevel(parts[1]), Category: Category(parts[2]), Message: parts[3]}, true
}

// sanitizeMessage flattens an embedded newline to a space — Logger.Log's
// own guarantee that every Entry it ever builds is exactly one real
// line, the same reason session.SaveTabs already refuses a path
// containing "\r\n" rather than let it silently corrupt its own
// one-entry-per-line file format.
func sanitizeMessage(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", " "), "\n", " ")
}
