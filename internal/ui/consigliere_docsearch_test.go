package ui

import (
	"strings"
	"testing"

	"github.com/jagottsicher/breakthrough/internal/docsearch"
)

func TestConsigliereDocSnapshotFindsRelevantDocumentation(t *testing.T) {
	root := newPlainKeyRoot(t)

	got := root.consigliereDocSnapshot("how do I search the last 3 hours in a log file")

	if !strings.Contains(got, "Relevant documentation:") {
		t.Fatalf("snapshot = %q, want it to mention it found documentation", got)
	}
	if !strings.Contains(got, "Log Audit") {
		t.Errorf("snapshot = %q, want it to surface the Log Audit section", got)
	}
}

func TestConsigliereDocSnapshotEmptyForAnUnrelatedQuestion(t *testing.T) {
	root := newPlainKeyRoot(t)

	got := root.consigliereDocSnapshot("xenomorph platypus quantum")

	if got != "" {
		t.Errorf("snapshot = %q, want empty for a question matching nothing in the docs", got)
	}
}

func TestConsigliereDocSnapshotEmptyWithoutAnIndex(t *testing.T) {
	root := newPlainKeyRoot(t)
	root.docSearchIndex = nil

	if got := root.consigliereDocSnapshot("anything at all"); got != "" {
		t.Errorf("snapshot = %q, want empty when the index itself is unavailable", got)
	}
}

func TestConsigliereDocSnapshotCapsChunkLength(t *testing.T) {
	root := newPlainKeyRoot(t)
	root.docSearchIndex = docsearch.NewIndex([]docsearch.Chunk{
		{Heading: "Long section", Body: strings.Repeat("word ", 300)},
	})

	got := root.consigliereDocSnapshot("word")

	if !strings.Contains(got, "…") {
		t.Errorf("snapshot = %q, want a truncation marker for an oversized chunk", got)
	}
	if len(got) > 2*consigliereMaxDocChunkChars {
		t.Errorf("snapshot is %d chars long, want it bounded near the %d-char cap", len(got), consigliereMaxDocChunkChars)
	}
}

func TestJoinConsigliereSnapshotPartsOmitsEmptyPartsWithoutStrayBlankLines(t *testing.T) {
	cases := []struct {
		name  string
		parts []string
		want  string
	}{
		{"both set", []string{"view info", "doc info"}, "view info\n\ndoc info"},
		{"first only", []string{"view info", ""}, "view info"},
		{"second only", []string{"", "doc info"}, "doc info"},
		{"neither", []string{"", ""}, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := joinConsigliereSnapshotParts(c.parts...); got != c.want {
				t.Errorf("joinConsigliereSnapshotParts(%v) = %q, want %q", c.parts, got, c.want)
			}
		})
	}
}
