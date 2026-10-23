package ui

import (
	"strconv"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/jagottsicher/breakthrough/internal/config"
)

// The terminal strip is the compact row of Terminal numbers living in the
// panel's own header row, immediately to the right of the tab strip's own
// "+" (see tabstrip.go) — per the user's own explicit request for exactly
// that spot and, just as explicitly, that order: this strip's own "+"
// comes first, then its numbers, not numbers-then-"+" the way the tab
// strip reads. Mirrors tabstrip.go's whole design (numbers only, a stable
// width, the switcher overlay for anything more — see terminalswitcher.go)
// for the identical reasons given there, with one deliberate difference:
// colors here are fixed, not theme-derived (see renderTerminalStrip's own
// doc comment), per the user's own explicit, literal color spec, meant to
// visually set Terminals apart from Tabs at a glance rather than blend in
// with whichever theme happens to be active.
//
// Unlike the tab strip, which always has at least one tab to draw, a
// session can have zero open Terminals — the "+" alone is still always
// drawn even then (see renderTerminalStrip), the identical "don't hide the
// feature's only discoverable entry point" reasoning tabstrip.go's own
// refreshTabStrip doc comment already gives, caught here before it ever
// shipped rather than from a live report.
const (
	// terminalStripMaxVisible mirrors tabStripMaxVisible — see its own
	// doc comment.
	terminalStripMaxVisible = 10

	terminalStripNewLabel = "+"

	terminalStripMoreBefore = "‹"
	terminalStripMoreAfter  = "›"
)

// terminalStripSpan is a clickable region in the rendered strip — the
// identical shape tabStripSpan already establishes.
//
// shell is the zero-based open-Terminal index this span selects, or
// terminalStripSpanNew for the leading "+" button.
type terminalStripSpan struct {
	start, end int
	shell      int
}

const terminalStripSpanNew = -1

// renderTerminalStrip builds the strip's display text and its click map
// for count open Terminals with active currently selected (-1 if none
// is — unlike a tab, there doesn't have to be one).
//
// Colors are fixed tcell colors, not looked up from theme, per the user's
// own explicit spec: the "+" and every inactive number render gray-on-
// white, the active number teal-on-dim-gray — deliberately the same
// pair of looks regardless of which color scheme is active, so Terminals
// read as a visually distinct strip from the themed tab numbers right
// next to it rather than blending in. styleTerminalStrip (this file)
// still paints the widget's own idle background from theme, the same as
// styleTabStrip does, so the strip's background matches the rest of the
// header row everywhere these fixed colors don't themselves apply (the
// gaps between glyphs, the leading padding).
func renderTerminalStrip(count, active int) (string, []terminalStripSpan) {
	const (
		inactiveFG = tcell.ColorGray
		inactiveBG = tcell.ColorWhite
		activeFG   = tcell.ColorTeal
		activeBG   = tcell.ColorDimGray
	)

	var b strings.Builder
	var spans []terminalStripSpan
	col := 0

	write := func(s string) {
		b.WriteString(s)
		col += len([]rune(s))
	}
	writeTagged := func(s string, fg, bg tcell.Color) {
		b.WriteString("[" + colorTag(fg) + ":" + colorTag(bg) + "]")
		b.WriteString(s)
		b.WriteString("[-:-]")
		col += len([]rune(s))
	}

	start := col
	writeTagged(terminalStripNewLabel, inactiveFG, inactiveBG)
	spans = append(spans, terminalStripSpan{start: start, end: col, shell: terminalStripSpanNew})

	if count == 0 {
		return b.String(), spans
	}
	if active < 0 || active >= count {
		active = -1 // no highlight rather than guessing a wrong one
	}

	first, last := terminalStripWindow(count, active)

	write(" ")
	if first > 0 {
		write(terminalStripMoreBefore)
	}
	for i := first; i <= last; i++ {
		if i > first {
			write(" ")
		}
		label := strconv.Itoa(i + 1)
		start := col
		if i == active {
			writeTagged(label, activeFG, activeBG)
		} else {
			writeTagged(label, inactiveFG, inactiveBG)
		}
		spans = append(spans, terminalStripSpan{start: start, end: col, shell: i})
	}
	if last < count-1 {
		write(terminalStripMoreAfter)
	}

	return b.String(), spans
}

// terminalStripWindow mirrors tabStripWindow — see its own doc comment.
// active of -1 (no highlight) windows around the first Terminal instead,
// since there's nothing else to center on.
func terminalStripWindow(count, active int) (first, last int) {
	if count <= terminalStripMaxVisible {
		return 0, count - 1
	}
	if active < 0 {
		active = 0
	}
	first = active - terminalStripMaxVisible/2
	if first < 0 {
		first = 0
	}
	last = first + terminalStripMaxVisible - 1
	if last > count-1 {
		last = count - 1
		first = last - terminalStripMaxVisible + 1
	}
	return first, last
}

// setTerminals tells the panel how many Terminals are open and which one
// is active, so its own strip can draw them — the identical shape
// Panel.setTabs already establishes, called from Root.refreshTerminalStrips
// on every change to r.shells/r.activeShell.
func (p *Panel) setTerminals(count, active int) {
	p.terminalCount = count
	p.terminalActive = active
	p.refreshTerminalStrip()
}

// refreshTerminalStrip redraws the strip from p.terminalCount/
// p.terminalActive and resizes its slot in the header row to match — the
// identical shape refreshTabStrip already establishes. The trailing
// breathing-room column before the Details "<" button lives here now,
// not on the tab strip any more, since this strip is what sits directly
// before it (see NewPanel's own headerRow.AddItem order).
func (p *Panel) refreshTerminalStrip() {
	if p.terminalStrip == nil || p.headerRow == nil {
		return
	}

	text, spans := renderTerminalStrip(p.terminalCount, p.terminalActive)

	leadIn := strings.Repeat(" ", headerTerminalStripGap)
	p.terminalStrip.SetText(leadIn + text)
	for i := range spans {
		spans[i].start += headerTerminalStripGap
		spans[i].end += headerTerminalStripGap
	}
	p.terminalStripSpans = spans
	p.headerRow.ResizeItem(p.terminalStrip, headerTerminalStripGap+tview.TaggedStringWidth(text)+1, 0)
}

// captureTerminalStripMouse mirrors captureTabStripMouse — a click on a
// number switches to that Terminal, a click on the leading "+" opens
// a new one, anywhere else opens the full switcher.
func (p *Panel) captureTerminalStripMouse(action tview.MouseAction, event *tcell.EventMouse) (tview.MouseAction, *tcell.EventMouse) {
	if !p.terminalStrip.InRect(event.Position()) {
		return action, event
	}
	if action != tview.MouseLeftClick {
		return tview.MouseConsumed, nil
	}

	x, _ := event.Position()
	rectX, _, _, _ := p.terminalStrip.GetInnerRect()
	col := x - rectX

	for _, span := range p.terminalStripSpans {
		if col < span.start || col >= span.end {
			continue
		}
		if span.shell == terminalStripSpanNew {
			if p.onNewTerminal != nil {
				p.onNewTerminal()
			}
		} else if p.onSelectTerminal != nil {
			p.onSelectTerminal(span.shell)
		}
		return tview.MouseConsumed, nil
	}

	if p.onOpenTerminalSwitcher != nil {
		p.onOpenTerminalSwitcher()
	}
	return tview.MouseConsumed, nil
}

// styleTerminalStrip paints the strip's own idle background — the same
// AccentBackground the tab strip sits on, so the two read as one
// continuous row. The fixed gray/white/green glyph colors (see
// renderTerminalStrip) are baked into the text itself and don't come
// from theme at all, so there's nothing more to repaint here when the
// color scheme changes — unlike styleTabStrip, applyTheme still has to
// call refreshTerminalStrip too (not just this), since a live scheme
// switch could change AccentBackground under the "+"/numbers' own
// surrounding gaps.
func (p *Panel) styleTerminalStrip(theme config.ResolvedTheme) {
	if p.terminalStrip == nil {
		return
	}
	p.terminalStrip.SetTextColor(theme.TextColor)
	p.terminalStrip.SetBackgroundColor(theme.AccentBackground)
}
