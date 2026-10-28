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
	return parseChunks(markdown, source, markdownHeadingText)
}

// ParseTviewBoldHeadingChunks splits text into one Chunk per
// "[::b]...[::-]" line — the Help overlay's own section-heading
// convention (see internal/ui/help.go's helpText), bold tview color
// tags standing in for Markdown's "#" since that text is rendered
// directly by a tview.TextView, never through a Markdown renderer.
// Same splitting rule as ParseMarkdownChunks otherwise, including
// text before the first heading becoming its own empty-Heading chunk.
func ParseTviewBoldHeadingChunks(text, source string) []Chunk {
	return parseChunks(text, source, tviewBoldHeadingText)
}

// parseChunks is ParseMarkdownChunks/ParseTviewBoldHeadingChunks' own
// shared body — only how a single line is recognized as a heading
// (and what text it carries) actually differs between the two
// corpora this package indexes.
func parseChunks(text, source string, headingText func(line string) (string, bool)) []Chunk {
	lines := strings.Split(text, "\n")

	var chunks []Chunk
	heading := ""
	var body []string

	flush := func() {
		bodyText := strings.TrimSpace(strings.Join(body, "\n"))
		if heading == "" && bodyText == "" {
			return // nothing before the very first heading — not a real chunk
		}
		chunks = append(chunks, Chunk{Heading: heading, Body: bodyText, Source: source})
	}

	for _, line := range lines {
		if h, ok := headingText(line); ok {
			flush()
			heading = h
			body = nil
			continue
		}
		body = append(body, line)
	}
	flush()

	return chunks
}

// markdownHeadingText recognizes an ATX-style Markdown heading ("#"
// through "######" followed by a space or end of line) — the only
// heading syntax this corpus's own docs actually use (never
// Setext-style "Title\n=====" underlines), verified against
// docs/user-guide.md directly rather than handling a syntax it
// doesn't need.
func markdownHeadingText(line string) (string, bool) {
	trimmed := strings.TrimLeft(line, "#")
	n := len(line) - len(trimmed)
	if n == 0 || n > 6 {
		return "", false
	}
	if trimmed != "" && !strings.HasPrefix(trimmed, " ") {
		return "", false
	}
	return strings.TrimSpace(trimmed), true
}

// tviewBoldHeadingText recognizes a line that is, after trimming
// surrounding whitespace, exactly "[::b]<text>[::-]" with nothing
// else on it — helpText's own heading convention, verified directly
// against internal/ui/help.go rather than assumed.
func tviewBoldHeadingText(line string) (string, bool) {
	trimmed := strings.TrimSpace(line)
	const open, close = "[::b]", "[::-]"
	if !strings.HasPrefix(trimmed, open) || !strings.HasSuffix(trimmed, close) {
		return "", false
	}
	return trimmed[len(open) : len(trimmed)-len(close)], true
}
