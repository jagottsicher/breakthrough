package ui

import (
	"github.com/gdamore/tcell/v2"
	"github.com/hinshun/vt10x"
)

// termAttrReverse/termAttrUnderline/termAttrBold mirror vt10x's own
// attribute bits (state.go, unexported there) — verified directly
// against the pinned dependency version
// (github.com/hinshun/vt10x v0.0.0-20220301184237-5011da428d02,
// state.go's own "attrReverse = 1 << iota" block), not guessed.
// attrItalic/attrBlink exist in that same block too but aren't
// rendered here yet — see feature_ideas.txt #11's own Phase 2 notes,
// a purely additive follow-up, not an architectural gap. Hardcoding
// these bit positions is only safe because the dependency version is
// pinned in go.mod, never "@latest" — re-verify against the real
// source first if that version ever changes.
const (
	termAttrReverse   = 1 << 0
	termAttrUnderline = 1 << 1
	termAttrBold      = 1 << 2
)

// termColor maps a vt10x.Color — ANSI [0, 16) or xterm [16, 256), see
// vt10x.Color's own doc comment — onto tcell's own identical
// xterm-256 palette index convention (tcell.PaletteColor): the two
// color spaces agree index for index, so no separate 16-entry lookup
// table is needed for the ANSI range either. DefaultFG/DefaultBG map
// to tcell.ColorDefault — the terminal's own ambient color, the same
// thing an absent/cleared color attribute already means everywhere
// else in this app.
func termColor(c vt10x.Color) tcell.Color {
	switch c {
	case vt10x.DefaultFG, vt10x.DefaultBG:
		return tcell.ColorDefault
	default:
		return tcell.PaletteColor(int(c))
	}
}

// termCellStyle renders one vt10x.Glyph as a tcell.Style — foreground/
// background via termColor, bold/underline/reverse via the verified
// bit positions above.
func termCellStyle(g vt10x.Glyph) tcell.Style {
	style := tcell.StyleDefault.
		Foreground(termColor(g.FG)).
		Background(termColor(g.BG)).
		Bold(g.Mode&termAttrBold != 0).
		Underline(g.Mode&termAttrUnderline != 0)
	if g.Mode&termAttrReverse != 0 {
		style = style.Reverse(true)
	}
	return style
}
