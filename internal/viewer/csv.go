package viewer

import (
	"encoding/csv"
	"strings"
	"unicode/utf8"
)

// CSVDelimiterFor reports the field delimiter Look uses to render path
// as a table — comma for .csv, tab for .tsv/.tab — and ok=false for
// anything else. Extension-only, the same convention
// LooksLikeVideoPath already uses: nothing about a CSV/TSV file's own
// content distinguishes it from any other line-oriented text file
// (Sniff already classifies it KindText, correctly — this is purely a
// "how should Look format text it already read" decision, not a
// different Kind of its own).
func CSVDelimiterFor(path string) (delim rune, ok bool) {
	lower := strings.ToLower(path)
	switch {
	case strings.HasSuffix(lower, ".csv"):
		return ',', true
	case strings.HasSuffix(lower, ".tsv"), strings.HasSuffix(lower, ".tab"):
		return '\t', true
	default:
		return 0, false
	}
}

// FormatCSVTable parses content as delim-separated records and
// re-renders it as a plain, fixed-width-aligned table — each column
// padded to its own widest cell across every row, separated by two
// spaces, the same shape the POSIX `column -t` tool already produces
// for exactly this job.
//
// encoding/csv is deliberately run in its most lenient mode:
// LazyQuotes (a stray quote mid-field doesn't abort the whole parse)
// and FieldsPerRecord = -1 (a ragged row — one with a different field
// count than others — is accepted rather than rejected outright). A
// preview should show its best effort at a real, possibly imperfectly
// formatted, real-world CSV rather than refusing it the way a strict
// validator would; Look's own caller falls back to plain syntax-
// highlighted text if even this fails (see internal/ui's own doc
// comment on that fallback).
//
// A row with fewer fields than the table's own column count is padded
// with empty cells rather than kept at its own shorter length — every
// rendered row always has exactly as many columns as the widest one
// did, so nothing downstream needs to special-case a short line.
func FormatCSVTable(content string, delim rune) (string, error) {
	r := csv.NewReader(strings.NewReader(content))
	r.Comma = delim
	r.LazyQuotes = true
	r.FieldsPerRecord = -1
	// A human-formatted CSV/TSV often has a space after each delimiter
	// for readability — without this, that space becomes part of the
	// next cell's own content, defeating the column alignment this
	// function exists to add back.
	r.TrimLeadingSpace = true

	records, err := r.ReadAll()
	if err != nil {
		return "", err
	}
	if len(records) == 0 {
		return "", nil
	}

	cols := 0
	for _, row := range records {
		if len(row) > cols {
			cols = len(row)
		}
	}
	widths := make([]int, cols)
	for _, row := range records {
		for i, cell := range row {
			if w := utf8.RuneCountInString(cell); w > widths[i] {
				widths[i] = w
			}
		}
	}

	var b strings.Builder
	for _, row := range records {
		for i := 0; i < cols; i++ {
			var cell string
			if i < len(row) {
				cell = row[i]
			}
			b.WriteString(cell)
			if i < cols-1 {
				b.WriteString(strings.Repeat(" ", widths[i]-utf8.RuneCountInString(cell)+2))
			}
		}
		b.WriteString("\n")
	}
	return b.String(), nil
}
