package filterexpr

import (
	"regexp"
	"strings"
)

// andSplitter matches a standalone "and" (word-bounded, so "android"
// is never split on it), case-insensitively — the one connector both
// ParseSize (chaining clauses into a range) and ParseMtime ("between X
// and Y") share.
var andSplitter = regexp.MustCompile(`(?i)\band\b`)

// splitOnAnd splits s on every standalone occurrence of "and",
// trimming whitespace from each resulting piece and dropping any that
// end up empty (a leading/trailing/doubled "and", say).
func splitOnAnd(s string) []string {
	parts := andSplitter.Split(s, -1)
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
