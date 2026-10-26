package ui

import (
	"strings"
	"testing"
)

// TestConsigliereSplashGridRowsAreAllTheSameWidth guards
// consigliereSplashGrid's own basic shape: a row shorter or longer
// than consigliereSplashWidth would misalign every column to its
// right for the rest of that row in consigliereSplashArt's own
// half-block rendering — easy to get wrong by hand when a row is built
// from several strings.Repeat calls added up (see the grid's own doc
// comment).
func TestConsigliereSplashGridRowsAreAllTheSameWidth(t *testing.T) {
	for i, row := range consigliereSplashGrid {
		if len(row) != consigliereSplashWidth {
			t.Errorf("row %d has length %d, want %d (%q)", i, len(row), consigliereSplashWidth, row)
		}
	}
}

// TestConsigliereSplashGridHasAnEvenRowCount guards
// consigliereSplashArt's own pairing loop (two source rows per
// terminal row) — an odd count would silently drop the last row
// instead of rendering it.
func TestConsigliereSplashGridHasAnEvenRowCount(t *testing.T) {
	if len(consigliereSplashGrid)%2 != 0 {
		t.Errorf("consigliereSplashGrid has %d rows, want an even number", len(consigliereSplashGrid))
	}
}

// TestConsigliereSplashArtUsesOnlyKnownColors guards
// consigliereSplashColors against a typo'd letter in
// consigliereSplashGrid: a missing entry for anything other than "."
// (deliberately unmapped — see consigliereSplashColors' own doc
// comment on why it stays transparent) would silently render as an
// empty tview color tag ("[:...]"), not a build failure.
func TestConsigliereSplashArtUsesOnlyKnownColors(t *testing.T) {
	for i, row := range consigliereSplashGrid {
		for col := 0; col < len(row); col++ {
			if row[col] == '.' {
				continue
			}
			if _, ok := consigliereSplashColors[row[col]]; !ok {
				t.Errorf("row %d, col %d: %q has no entry in consigliereSplashColors", i, col, row[col])
			}
		}
	}
}

// TestConsigliereSplashArtIncludesTheCaption pins
// consigliereSplashArt's own basic output shape — at a generous width,
// wide enough that wrapText (see consigliereSplashArt's own doc
// comment on wrapping the caption before centering it) leaves the
// caption on a single, unbroken line.
func TestConsigliereSplashArtIncludesTheCaption(t *testing.T) {
	if got := consigliereSplashArt(len(consigliereSplashCaption) + 10); !strings.Contains(got, consigliereSplashCaption) {
		t.Errorf("consigliereSplashArt() = %q, want it to include the caption %q", got, consigliereSplashCaption)
	}
}

// TestConsigliereSplashArtCentersEveryWrappedCaptionLine pins the real
// bug the user reported ("weder Bild noch Text mittig"): at a width
// too narrow for consigliereSplashCaption to fit on one line, every
// line it wraps into (via wrapText, not consigliereHistory's own
// SetWrap) must be centered on its own, not just the first one.
func TestConsigliereSplashArtCentersEveryWrappedCaptionLine(t *testing.T) {
	width := len(consigliereSplashCaption) / 2
	got := consigliereSplashArt(width)
	lines := strings.Split(strings.TrimRight(got, "\n"), "\n")

	captionLines := lines[len(lines)-2:] // the caption must have wrapped into (at least) two lines
	for _, line := range captionLines {
		trimmed := strings.TrimLeft(line, " ")
		wantPad := (width - len(trimmed)) / 2
		if wantPad < 0 {
			wantPad = 0
		}
		gotPad := len(line) - len(trimmed)
		if gotPad != wantPad {
			t.Errorf("wrapped caption line %q has %d leading spaces, want %d", line, gotPad, wantPad)
		}
	}
}

// TestConsigliereSplashArtCentersWithinAWiderWidth pins the user's own
// explicit request ("sollte es mittes des panels sein"): a width
// wider than the portrait's own consigliereSplashWidth must pad every
// line with leading spaces, not leave it flush against the left edge.
func TestConsigliereSplashArtCentersWithinAWiderWidth(t *testing.T) {
	narrow := consigliereSplashArt(consigliereSplashWidth)
	wide := consigliereSplashArt(consigliereSplashWidth * 3)

	firstLineWide := strings.SplitN(wide, "\n", 2)[0]
	firstLineNarrow := strings.SplitN(narrow, "\n", 2)[0]
	if !strings.HasPrefix(firstLineWide, "  ") {
		t.Errorf("first line at 3x width = %q, want it padded with leading spaces", firstLineWide)
	}
	if strings.HasPrefix(firstLineNarrow, " ") {
		t.Errorf("first line at exactly consigliereSplashWidth = %q, want no padding", firstLineNarrow)
	}
}
