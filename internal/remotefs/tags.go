package remotefs

import (
	"sort"
	"strings"
)

// tagsMaxCount caps how many tags a single Connection can carry — short
// enough that the connection dropdown's own Tags column (internal/ui's
// connectionmenu.go) stays glance-readable, long enough to tag a server
// by environment, role, and project all at once without hitting the
// limit in practice.
const tagsMaxCount = 10

// tagMaxLength caps a single tag's own length — the same
// "glance-readable, not a second description field" reasoning
// connectIdentifierMaxLength already applies to Identifier, here per
// tag rather than for the whole field.
const tagMaxLength = 32

// IsValidTagRune reports whether r is allowed inside a tag: ASCII
// letters, digits, underscore, hyphen, or a space — a superset of
// Identifier's own character set (internal/ui's
// isConnectIdentifierRune), since a tag like "web server" reads more
// naturally with a space than "web-server" would for every tag. Shared
// between ParseTags' own defensive validation below and the Connect
// dialog's live Tags field acceptance func (connectTagsAcceptanceFunc),
// so the two can never silently drift into accepting different
// characters.
func IsValidTagRune(r rune) bool {
	return r == '-' || r == '_' || r == ' ' ||
		(r >= 'a' && r <= 'z') ||
		(r >= 'A' && r <= 'Z') ||
		(r >= '0' && r <= '9')
}

// isValidTag reports whether every rune in tag is allowed — tag itself
// is already non-empty and trimmed by the time ParseTags calls this.
func isValidTag(tag string) bool {
	for _, r := range tag {
		if !IsValidTagRune(r) {
			return false
		}
	}
	return true
}

// ParseTags turns a comma-separated raw string (the Connect dialog's
// own Tags field, see internal/ui's connectdialog.go) into a
// well-formed Tags value — the single place that produces one, so
// every caller (RecordAttempt's inherit rule included) can rely on the
// result already being normalized rather than re-checking it:
//
//   - Split on ",", each piece trimmed of surrounding whitespace — "
//     web , build " and "web,build" both read the same.
//   - Empty pieces (from "web,,build" or a trailing/leading comma)
//     dropped silently, never kept as a blank tag.
//   - A piece containing anything outside IsValidTagRune is dropped
//     entirely rather than partially sanitized — this only matters for
//     a caller other than the Connect dialog's own already-filtering
//     field, which never produces one in the first place.
//   - A piece longer than tagMaxLength is truncated to it, not
//     dropped — losing the tail of an over-long tag is still more
//     useful than losing the tag outright.
//   - Duplicates (exact match, after trimming/truncation) collapse to
//     one.
//   - Capped at tagsMaxCount, first-occurrence order up to the cap.
//   - Sorted alphanumerically — tags are a set, not an ordered list,
//     and a stable, predictable order is what makes the same set look
//     identical regardless of the order they were typed in.
//
// Deliberately NOT done here, on every keystroke: this is the
// commit-time normalization the Connect dialog applies only once the
// Tags field loses focus (normalizeConnectTagsField), never while the
// field still has it — sorting/deduplicating live, mid-keystroke, would
// move the text the user is still typing right under their own cursor.
func ParseTags(raw string) []string {
	parts := strings.Split(raw, ",")
	seen := make(map[string]bool, len(parts))
	tags := make([]string, 0, len(parts))
	for _, part := range parts {
		tag := strings.TrimSpace(part)
		if tag == "" {
			continue
		}
		if len(tag) > tagMaxLength {
			tag = tag[:tagMaxLength]
		}
		if !isValidTag(tag) {
			continue
		}
		if seen[tag] {
			continue
		}
		seen[tag] = true
		tags = append(tags, tag)
		if len(tags) >= tagsMaxCount {
			break
		}
	}
	sort.Strings(tags)
	return tags
}
