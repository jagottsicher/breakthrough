package docsearch

import "strings"

// Chunk is one retrievable unit of documentation — a single Markdown
// section (from one heading line up to, but not including, the next
// one of any level), kept small and topically narrow on purpose: a
// whole 2,800-line user guide as a single chunk would always "match"
// everything and never let Search actually discriminate between
// questions.
type Chunk struct {
	// Heading is the section's own heading text, with the leading
	// "#"s and surrounding whitespace stripped — "Log Audit screen
	// (\"jL\")", not "### Log Audit screen (\"jL\")".
	Heading string
	// Body is everything between this heading and the next one,
	// trimmed of leading/trailing blank lines. Never truncated here —
	// Search's own caller is what bounds how much of it actually goes
	// into a snapshot (see consigliereMaxDocChunkChars in internal/ui).
	Body string
	// Source names where this chunk came from (e.g. "user-guide.md"),
	// purely for attribution if a caller ever wants to show it —
	// Search itself doesn't filter or weight by it.
	Source string
}

// ParseMarkdownChunks splits markdown into one Chunk per heading line
// (any level, "#" through "######" — this corpus's own headings
// nest up to "###", but nothing here assumes a fixed depth). Text
// before the first heading becomes its own chunk with an empty
// Heading, rather than being dropped, so a document that opens with
// unheaded introductory text isn't silently excluded from search.
func ParseMarkdownChunks(markdown, source string) []Chunk {
	lines := strings.Split(markdown, "\n")

	var chunks []Chunk
	heading := ""
	var body []string

	flush := func() {
		text := strings.TrimSpace(strings.Join(body, "\n"))
		if heading == "" && text == "" {
			return // nothing before the very first heading — not a real chunk
		}
		chunks = append(chunks, Chunk{Heading: heading, Body: text, Source: source})
	}

	for _, line := range lines {
		if isMarkdownHeading(line) {
			flush()
			heading = strings.TrimSpace(strings.TrimLeft(line, "#"))
			body = nil
			continue
		}
		body = append(body, line)
	}
	flush()

	return chunks
}

// isMarkdownHeading reports whether line is an ATX-style Markdown
// heading ("#" through "######" followed by a space or end of line) —
// the only heading syntax this corpus's own docs actually use (never
// Setext-style "Title\n=====" underlines), verified against
// docs/user-guide.md directly rather than handling a syntax it
// doesn't need.
func isMarkdownHeading(line string) bool {
	trimmed := strings.TrimLeft(line, "#")
	n := len(line) - len(trimmed)
	if n == 0 || n > 6 {
		return false
	}
	return trimmed == "" || strings.HasPrefix(trimmed, " ")
}
