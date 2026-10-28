package ui

import (
	"strings"
	"testing"
)

func TestTruncateDocChunkBodyFitsWithoutTruncation(t *testing.T) {
	body := "short body"
	if got := truncateDocChunkBody(body, 100); got != body {
		t.Errorf("got %q, want the body unchanged", got)
	}
}

func TestTruncateDocChunkBodyWithNoTableFallsBackToPlainTruncation(t *testing.T) {
	body := strings.Repeat("word ", 100)
	got := truncateDocChunkBody(body, 50)
	want := truncateRunes(body, 50)
	if got != want {
		t.Errorf("got %q, want %q (plain truncateRunes)", got, want)
	}
}

// TestTruncateDocChunkBodyKeepsTableIntactOverProse pins the real,
// reported bug: a chunk whose actual reference table sits well after
// a long preamble must still have that whole table survive
// truncation, even if the preamble itself gets cut short to make
// room.
func TestTruncateDocChunkBodyKeepsTableIntactOverProse(t *testing.T) {
	preamble := strings.Repeat("filler prose that explains mechanics in great detail. ", 20)
	table := "| Key | Action |\n|---|---|\n| Time field | accepts `2 hours ago` |"
	body := preamble + "\n" + table

	got := truncateDocChunkBody(body, 120)

	if !strings.Contains(got, "2 hours ago") {
		t.Fatalf("got %q, want the table's own content to survive truncation", got)
	}
	if !strings.HasSuffix(got, table) {
		t.Errorf("got %q, want the full table preserved verbatim at the end", got)
	}
}

// TestTruncateDocChunkBodyFallsBackWhenTableAloneExceedsMax pins the
// edge case: if even the table rows alone don't fit in max, truncate
// plainly instead of risking a cut partway through the table that
// could drop whichever row actually matters.
func TestTruncateDocChunkBodyFallsBackWhenTableAloneExceedsMax(t *testing.T) {
	table := strings.Repeat("| Key | Action — a very long description here |\n", 10)
	got := truncateDocChunkBody(table, 20)
	want := truncateRunes(table, 20)
	if got != want {
		t.Errorf("got %q, want %q (plain truncation when the table itself doesn't fit)", got, want)
	}
}
