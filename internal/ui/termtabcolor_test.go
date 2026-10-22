package ui

import (
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/hinshun/vt10x"
)

func TestTermColorANSIRange(t *testing.T) {
	// vt10x.Red is 1, vt10x.White is 15 — both well inside the shared
	// [0, 16) ANSI range termColor's own doc comment says needs no
	// separate lookup table, just tcell.PaletteColor at the same index.
	if got := termColor(vt10x.Red); got != tcell.PaletteColor(1) {
		t.Errorf("termColor(Red) = %v, want tcell.PaletteColor(1)", got)
	}
	if got := termColor(vt10x.White); got != tcell.PaletteColor(15) {
		t.Errorf("termColor(White) = %v, want tcell.PaletteColor(15)", got)
	}
}

func TestTermColorXtermExtendedRange(t *testing.T) {
	c := vt10x.Color(200) // arbitrary xterm-256 index outside the base 16
	if got := termColor(c); got != tcell.PaletteColor(200) {
		t.Errorf("termColor(200) = %v, want tcell.PaletteColor(200)", got)
	}
}

func TestTermColorDefaults(t *testing.T) {
	if got := termColor(vt10x.DefaultFG); got != tcell.ColorDefault {
		t.Errorf("termColor(DefaultFG) = %v, want tcell.ColorDefault", got)
	}
	if got := termColor(vt10x.DefaultBG); got != tcell.ColorDefault {
		t.Errorf("termColor(DefaultBG) = %v, want tcell.ColorDefault", got)
	}
}

func TestTermCellStyleColorsCarryThrough(t *testing.T) {
	g := vt10x.Glyph{Char: 'x', FG: vt10x.Green, BG: vt10x.Blue}
	style := termCellStyle(g)
	fg, bg, _ := style.Decompose()
	if fg != tcell.PaletteColor(int(vt10x.Green)) {
		t.Errorf("foreground = %v, want PaletteColor(Green)", fg)
	}
	if bg != tcell.PaletteColor(int(vt10x.Blue)) {
		t.Errorf("background = %v, want PaletteColor(Blue)", bg)
	}
}

func TestTermCellStyleBoldUnderlineReverse(t *testing.T) {
	plain := termCellStyle(vt10x.Glyph{})
	_, _, plainAttrs := plain.Decompose()
	if plainAttrs&tcell.AttrBold != 0 || plainAttrs&tcell.AttrUnderline != 0 || plainAttrs&tcell.AttrReverse != 0 {
		t.Errorf("a plain glyph got attrs %v, want none set", plainAttrs)
	}

	bold := termCellStyle(vt10x.Glyph{Mode: termAttrBold})
	_, _, boldAttrs := bold.Decompose()
	if boldAttrs&tcell.AttrBold == 0 {
		t.Error("termAttrBold didn't produce tcell.AttrBold")
	}

	underline := termCellStyle(vt10x.Glyph{Mode: termAttrUnderline})
	_, _, underlineAttrs := underline.Decompose()
	if underlineAttrs&tcell.AttrUnderline == 0 {
		t.Error("termAttrUnderline didn't produce tcell.AttrUnderline")
	}

	reverse := termCellStyle(vt10x.Glyph{Mode: termAttrReverse})
	_, _, reverseAttrs := reverse.Decompose()
	if reverseAttrs&tcell.AttrReverse == 0 {
		t.Error("termAttrReverse didn't produce tcell.AttrReverse")
	}
}
