package ui

import (
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/jagottsicher/breakthrough/internal/viewer"
)

// syntaxPalette is one complete set of Look's own syntax colors — one
// entry per viewer.TokenKind that gets a color at all (KindPlain
// deliberately has none: plain text keeps whatever the view's own
// configured text color already is, so it always matches the active
// color scheme rather than fighting it).
type syntaxPalette map[viewer.TokenKind]tcell.Color

// darkSyntaxPalette and lightSyntaxPalette are the built-in syntax
// colors, picked per scheme rather than per file (see paletteFor).
// Two palettes rather than one because a single set genuinely cannot
// work on both: a yellow readable on near-black is invisible on
// near-white, and vice versa.
//
// The colors themselves are the widely-recognizable terminal-syntax
// conventions (comments muted, strings warm, keywords cool) rather than
// any one editor theme's own palette — a user coming from vim, nano, or
// bat should find them unsurprising.
var darkSyntaxPalette = syntaxPalette{
	viewer.KindKeyword:    tcell.ColorCornflowerBlue,
	viewer.KindString:     tcell.ColorDarkSalmon,
	viewer.KindComment:    tcell.ColorGray,
	viewer.KindNumber:     tcell.ColorMediumPurple,
	viewer.KindName:       tcell.ColorPaleTurquoise,
	viewer.KindLiteral:    tcell.ColorGold,
	viewer.KindDiffAdd:    tcell.ColorMediumSeaGreen,
	viewer.KindDiffDelete: tcell.ColorIndianRed,
}

var lightSyntaxPalette = syntaxPalette{
	viewer.KindKeyword:    tcell.ColorDarkBlue,
	viewer.KindString:     tcell.ColorSaddleBrown,
	viewer.KindComment:    tcell.ColorDimGray,
	viewer.KindNumber:     tcell.ColorDarkMagenta,
	viewer.KindName:       tcell.ColorTeal,
	viewer.KindLiteral:    tcell.ColorDarkGoldenrod,
	viewer.KindDiffAdd:    tcell.ColorDarkGreen,
	viewer.KindDiffDelete: tcell.ColorDarkRed,
}

// paletteFor picks the palette that will actually be readable against
// background — the color the Look overlay is painted in (see
// Root.applyTheme, which gives it theme.AccentBackground). This is what
// "built-in colors that follow the active scheme" actually means here:
// the syntax colors aren't read from the scheme file, but which of the
// two sets is used is decided by the scheme's own background, so
// switching to a light scheme doesn't leave unreadable dark-on-light
// text behind.
func paletteFor(background tcell.Color) syntaxPalette {
	if isLightColor(background) {
		return lightSyntaxPalette
	}
	return darkSyntaxPalette
}

// isLightColor reports whether c is light enough that dark text reads
// better on it than light text — via the ITU-R BT.601 luma weights
// (0.299 R, 0.587 G, 0.114 B), the standard "perceived brightness"
// approximation: green contributes most to how bright a color looks,
// blue least, which a plain (R+G+B)/3 average gets visibly wrong for
// exactly the saturated colors a terminal scheme is most likely to use.
// The 50% threshold is the conventional split point.
//
// tcell.ColorDefault (a scheme that never set a background at all, so
// the terminal's own applies) has no RGB to weigh and reports false —
// dark is the safer assumption, since a terminal left at its own
// default is overwhelmingly more likely to be dark than light.
func isLightColor(c tcell.Color) bool {
	if c == tcell.ColorDefault {
		return false
	}
	r, g, b := c.RGB()
	luma := (299*r + 587*g + 114*b) / 1000
	return luma > 127
}

// renderSyntax turns tokens into one tview-markup string ready for a
// SetDynamicColors(true) TextView: every token's own text is escaped
// (see tview.Escape — file content routinely contains "[", which would
// otherwise be swallowed as a style tag; the same reason
// showBuiltinLook escaped the whole body before syntax coloring
// existed), then wrapped in its kind's own color tag.
//
// A KindPlain token — and any kind the palette has no entry for — is
// written with no tag at all rather than an explicit "default color"
// tag: leaving it alone means it keeps whatever color the TextView
// itself is set to (see Root.applyTheme), so plain text always matches
// the active scheme exactly.
//
// "[-]" resets only the foreground, never the background, so the
// overlay's own background stays uniform underneath colored text.
func renderSyntax(tokens []viewer.Token, palette syntaxPalette) string {
	var b strings.Builder
	for _, t := range tokens {
		escaped := tview.Escape(t.Text)
		color, ok := palette[t.Kind]
		if !ok {
			b.WriteString(escaped)
			continue
		}
		b.WriteString("[")
		b.WriteString(colorTag(color))
		b.WriteString("]")
		b.WriteString(escaped)
		b.WriteString("[-]")
	}
	return b.String()
}
