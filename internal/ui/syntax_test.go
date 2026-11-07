package ui

import (
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/jagottsicher/breakthrough/internal/viewer"
)

func TestIsLightColor(t *testing.T) {
	cases := []struct {
		name  string
		color tcell.Color
		want  bool
	}{
		{"black", tcell.ColorBlack, false},
		{"white", tcell.ColorWhite, true},
		{"darkslategray (this app's own default background)", tcell.ColorDarkSlateGray, false},
		{"terminal default (no RGB to weigh)", tcell.ColorDefault, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := isLightColor(c.color); got != c.want {
				t.Errorf("isLightColor(%v) = %v, want %v", c.name, got, c.want)
			}
		})
	}
}

func TestPaletteForPicksDarkOrLightSet(t *testing.T) {
	dark := paletteFor(tcell.ColorBlack)
	if dark[viewer.KindKeyword] != darkSyntaxPalette[viewer.KindKeyword] {
		t.Error("paletteFor(black) didn't return darkSyntaxPalette")
	}
	light := paletteFor(tcell.ColorWhite)
	if light[viewer.KindKeyword] != lightSyntaxPalette[viewer.KindKeyword] {
		t.Error("paletteFor(white) didn't return lightSyntaxPalette")
	}
}

func TestRenderSyntaxEscapesContentAndTagsColoredKinds(t *testing.T) {
	tokens := []viewer.Token{
		{Kind: viewer.KindPlain, Text: "package "},
		{Kind: viewer.KindKeyword, Text: "main"},
		{Kind: viewer.KindPlain, Text: " // [not a tag]"},
	}
	palette := darkSyntaxPalette

	got := renderSyntax(tokens, palette)

	// The plain runs carry no color tag at all — see renderSyntax's own
	// doc comment on why: they should read exactly as their own escaped
	// text, unwrapped.
	if !strings.HasPrefix(got, "package ") {
		t.Errorf("plain leading token wasn't left untagged: %q", got)
	}
	if !strings.Contains(got, tview.Escape("[not a tag]")) {
		t.Errorf("literal brackets in plain text weren't escaped: %q", got)
	}
	wantKeywordTag := "[" + colorTag(palette[viewer.KindKeyword]) + "]main[-]"
	if !strings.Contains(got, wantKeywordTag) {
		t.Errorf("keyword token wasn't wrapped as %q, got %q", wantKeywordTag, got)
	}
}

// TestRenderSyntaxRoundTripsThroughTviewParsing pins the actual
// motivating bug class: content containing tview-tag-shaped brackets
// must render as literal text once tview parses the region, not be
// silently swallowed as a (invalid) style tag.
func TestRenderSyntaxRoundTripsThroughTviewParsing(t *testing.T) {
	tokens := []viewer.Token{{Kind: viewer.KindPlain, Text: "log line with [ERROR] inside"}}

	rendered := renderSyntax(tokens, darkSyntaxPalette)

	v := tview.NewTextView().SetDynamicColors(true)
	v.SetText(rendered)
	if got := v.GetText(true); got != "log line with [ERROR] inside" {
		t.Errorf("GetText(true) = %q, want the literal content unmangled by tag parsing", got)
	}
}

// TestSplitHighlightedTokensByLineLineCountInvariant is the
// discriminating test gitBreachColorizeDiff's own correctness rests
// on: splitHighlightedTokensByLine's result must have exactly one
// element per "\n"-split line of the content Highlight was actually
// run on, or a caller indexing into it by line number silently
// mis-colors a diff instead of merely losing the multi-line-construct
// improvement (see renderReconstructedFiles' own defensive length
// check in gitbreachscreen.go, which exists precisely because this
// invariant is Highlight's own contract, not provable from this
// package alone, across every lexer chroma ships).
func TestSplitHighlightedTokensByLineLineCountInvariant(t *testing.T) {
	cases := []struct {
		name string
		path string
		ls   []string
	}{
		{"Go file with a block comment", "x.go", []string{
			"/* start of comment", "still a comment", "end */", "func x() {}",
		}},
		{"file ending without a trailing newline", "x.go", []string{
			"package main", "func main() {}",
		}},
		{"single-line file", "x.go", []string{"package main"}},
		{"single empty line", "x.go", []string{""}},
		{"no recognized extension", "x", []string{"plain text", "more plain text"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tokens := viewer.Highlight(c.path, strings.Join(c.ls, "\n"))
			got := splitHighlightedTokensByLine(tokens, darkSyntaxPalette)
			if len(got) != len(c.ls) {
				t.Fatalf("splitHighlightedTokensByLine returned %d lines, want %d (input: %q)", len(got), len(c.ls), c.ls)
			}
		})
	}
}

// TestSplitHighlightedTokensByLinePreservesClassificationAcrossLines
// pins the actual motivating improvement over lexing one line at a
// time: a multi-line Go block comment, split back per line, still
// carries its own KindComment color on every one of its lines — which
// Highlight given each line in isolation (no real surrounding
// context) cannot do at all (verified directly: it comes back
// KindPlain line by line).
func TestSplitHighlightedTokensByLinePreservesClassificationAcrossLines(t *testing.T) {
	ls := []string{"/* start of comment", "still a comment", "end */"}
	tokens := viewer.Highlight("x.go", strings.Join(ls, "\n"))

	got := splitHighlightedTokensByLine(tokens, darkSyntaxPalette)
	wantTag := "[" + colorTag(darkSyntaxPalette[viewer.KindComment]) + "]"
	for i, line := range got {
		if !strings.Contains(line, wantTag) {
			t.Errorf("line %d = %q, want it tagged as a comment (%q)", i, line, wantTag)
		}
	}
}
