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
