package ui

import (
	"strings"
	"testing"

	"github.com/jagottsicher/breakthrough/internal/viewer"
)

// TestGitBreachColorizeDiffClassifiesAMultiLineCommentAcrossLines is
// the discriminating test for the whole reconstruct-then-lex approach:
// a block comment that a single-line-at-a-time lexer (the previous
// behavior) cannot recognize at all — verified directly in
// internal/viewer that it falls back to KindPlain line by line — must
// come out colored as a comment on every one of its lines once
// gitBreachColorizeDiff reconstructs the full file and lexes it in
// one piece. Includes an *added* line sitting inside the comment, so
// this also exercises newLines' own reconstruction, not just oldLines.
func TestGitBreachColorizeDiffClassifiesAMultiLineCommentAcrossLines(t *testing.T) {
	r := newTestRootForGitBreachDir(t, t.TempDir())
	diff := strings.Join([]string{
		"diff --git a/x.go b/x.go",
		"index 1111111..2222222 100644",
		"--- a/x.go",
		"+++ b/x.go",
		"@@ -1,3 +1,4 @@",
		" /* start of comment",
		" still a comment",
		"+added line inside comment",
		" end */",
	}, "\n")

	got := gitBreachColorizeDiff(diff, "", r.theme)

	palette := paletteFor(r.theme.SurfaceBackground)
	wantTag := "[" + colorTag(palette[viewer.KindComment]) + "]"
	lines := strings.Split(got, "\n")
	if len(lines) != 9 {
		t.Fatalf("gitBreachColorizeDiff produced %d lines, want 9 (one per input line): %q", len(lines), got)
	}
	// Lines 5-8 (0-indexed) are the four comment-construct lines:
	// "/* start of comment", "still a comment", the added line, and
	// "end */" — every one of them must carry the comment color tag.
	for i := 5; i <= 8; i++ {
		if !strings.Contains(lines[i], wantTag) {
			t.Errorf("line %d = %q, want it tagged as a comment (%q)", i, lines[i], wantTag)
		}
	}
}

// TestGitBreachColorizeDiffKeepsFilesSeparateInAMultiFileDiff pins
// that reconstruction is per-file (keyed by path, reset at each "diff
// --git" header) in a commit's own combined diff spanning several
// files — a bug here would either crash (index out of range, mixing
// one file's line count into another's) or lex the second file's
// content as the first file's language.
func TestGitBreachColorizeDiffKeepsFilesSeparateInAMultiFileDiff(t *testing.T) {
	r := newTestRootForGitBreachDir(t, t.TempDir())
	diff := strings.Join([]string{
		"diff --git a/x.go b/x.go",
		"index 1111111..2222222 100644",
		"--- a/x.go",
		"+++ b/x.go",
		"@@ -1,2 +1,2 @@",
		"-old go line",
		"+new go line",
		" package main",
		"diff --git a/y.py b/y.py",
		"index 3333333..4444444 100644",
		"--- a/y.py",
		"+++ b/y.py",
		"@@ -1,1 +1,1 @@",
		"-old_py = 1",
		"+new_py = 2",
	}, "\n")

	got := gitBreachColorizeDiff(diff, "", r.theme)

	lines := strings.Split(got, "\n")
	if len(lines) != 15 {
		t.Fatalf("gitBreachColorizeDiff produced %d lines, want 15 (one per input line): %q", len(lines), got)
	}
	// Header lines are wrapped whole (wrapColor around the entire raw
	// line, see gitBreachColorizeDiff's gitBreachDiffLineHeader case),
	// so these stay intact, contiguous substrings — unlike content
	// lines, where a recognized keyword/number inside them (Go's own
	// "go", a Python numeral) gets its own nested color tag and so no
	// longer appears as one unbroken run of plain text.
	for _, want := range []string{
		"diff --git a/x.go b/x.go", "diff --git a/y.py b/y.py",
		"old_py", "new_py", // identifiers — not tokenized on their own
	} {
		if !strings.Contains(got, want) {
			t.Errorf("output missing %q entirely:\n%s", want, got)
		}
	}
}
