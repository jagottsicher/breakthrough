package ui

import (
	"strconv"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/jagottsicher/breakthrough/internal/config"
)

// The tab strip is the compact row of tab numbers living in the panel's
// own header row, between the filter box and the Details "<" button (see
// NewPanel) — per the user's own explicit request for exactly that spot.
//
// Numbers only, never directory names, and that's the whole design rather
// than a limitation worked around: a label wide enough to hold a real
// path (or even just a basename) both eats the little header room left
// there and, worse, changes width every time the user navigates,
// making the entire header row jitter on every keystroke. Digits are one
// or two columns, always, so the row's layout is stable. What each tab
// actually holds is answered on demand instead, by the switcher overlay
// (see tabswitcher.go), which has room to show every path in full.
//
// The active tab is the only one painted with a background, using the
// same FocusedBackground the rest of this app already means "this is the
// thing you're on" with — not a tab-strip-specific color invented here.
const (
	// tabStripMaxVisible caps how many tab numbers are drawn at once.
	// Beyond that the strip shows a window around the active tab with
	// "‹"/"›" markers for what's scrolled out of view (see
	// renderTabStrip), rather than growing without limit across a header
	// row that has a path to display too. Ten matches the ten direct
	// Ctrl+1..Ctrl+0 shortcuts (see Root.SwitchToTabShortcut), so every
	// tab that has its own hotkey can be on screen at once.
	tabStripMaxVisible = 10

	// tabStripNewLabel is the trailing "new tab" button drawn after the
	// numbers — a mouse affordance for the same thing the context menu's
	// own "New tab" entry does, since the strip is exactly where someone
	// looking for it would click first.
	tabStripNewLabel = "+"

	// tabStripMoreBefore/tabStripMoreAfter mark tabs scrolled out of the
	// visible window on either side. Single-column glyphs from the same
	// box-drawing/typographic family the rest of this app's chrome uses.
	tabStripMoreBefore = "‹"
	tabStripMoreAfter  = "›"
)

// tabStripSpan locates one clickable region in the rendered strip, the
// same shape (and for the same reason) as the panel header's own
// headerSpan: the drawn text carries color tags, so a click's column can
// only be mapped back to a target by recording the spans as they're
// built, not by measuring the final string afterwards.
//
// tab is the zero-based tab index this span selects, or tabStripSpanNew
// for the trailing "+" button.
type tabStripSpan struct {
	start, end int
	tab        int
}

// tabStripSpanNew is the tab field's own sentinel for the "+" button —
// not a real tab index.
const tabStripSpanNew = -1

// renderTabStrip builds the strip's display text and its click map for
// count tabs with active currently selected.
//
// Pure and independently testable (no widget, no theme lookup beyond the
// two colors handed in) — the click map in particular is the kind of
// off-by-one-prone index arithmetic that's much easier to pin down
// directly than through a rendered screen.
//
// Columns are counted in display cells, not bytes: the "‹"/"›" markers
// are multi-byte runes that still occupy exactly one column each, so
// spans are advanced by rune width throughout.
func renderTabStrip(count, active int, textColor, activeBackground tcell.Color) (string, []tabStripSpan) {
	if count < 2 {
		// Numbering a single tab says nothing useful, but the "+" itself
		// still has to be somewhere — it's the only way to create a
		// second tab at all, and hiding it entirely (an earlier version
		// of this did exactly that) turned out to hide the whole feature
		// along with it: nothing in the header row hinted that tabs
		// existed, per a real user report. One button, not the full
		// strip, is the smallest fix that still leaves an entry point.
		return tabStripNewLabel, []tabStripSpan{{start: 0, end: len([]rune(tabStripNewLabel)), tab: tabStripSpanNew}}
	}
	if active < 0 || active >= count {
		active = 0
	}

	first, last := tabStripWindow(count, active)

	var b strings.Builder
	var spans []tabStripSpan
	col := 0

	write := func(s string) {
		b.WriteString(s)
		col += len([]rune(s))
	}

	if first > 0 {
		write(tabStripMoreBefore)
	}

	for i := first; i <= last; i++ {
		if i > first {
			write(" ")
		}
		label := strconv.Itoa(i + 1)
		start := col
		if i == active {
			// Explicit foreground alongside the background: a tview color
			// tag that sets only the background leaves the foreground at
			// whatever the previous tag left behind, which here would be
			// the dimmer inactive text color.
			b.WriteString("[" + colorTag(textColor) + ":" + colorTag(activeBackground) + "]")
			b.WriteString(label)
			b.WriteString("[-:-]")
			col += len([]rune(label))
		} else {
			write(label)
		}
		spans = append(spans, tabStripSpan{start: start, end: col, tab: i})
	}

	if last < count-1 {
		write(tabStripMoreAfter)
	}

	write(" ")
	start := col
	write(tabStripNewLabel)
	spans = append(spans, tabStripSpan{start: start, end: col, tab: tabStripSpanNew})

	return b.String(), spans
}

// tabStripWindow picks which slice of tabs the strip shows when there are
// more than tabStripMaxVisible of them: a window of that size that always
// contains active, kept flush against whichever end it's near so the
// first and last tabs aren't permanently hidden behind a "‹"/"›" marker.
//
// Returned as an inclusive [first, last] range — both ends are real,
// drawable tab indices, which keeps renderTabStrip's own loop free of
// empty-window edge cases.
func tabStripWindow(count, active int) (first, last int) {
	if count <= tabStripMaxVisible {
		return 0, count - 1
	}
	// Center the window on active, then push it back inside the real
	// range at either end.
	first = active - tabStripMaxVisible/2
	if first < 0 {
		first = 0
	}
	last = first + tabStripMaxVisible - 1
	if last > count-1 {
		last = count - 1
		first = last - tabStripMaxVisible + 1
	}
	return first, last
}

// tabStripWidth is how many columns the strip needs for count tabs — what
// NewPanel's own Flex has to reserve for it (see Panel.refreshTabStrip's
// ResizeItem call). Derived by rendering the real thing and measuring it
// with tview's own tag-aware width function, rather than recomputing the
// layout a second time here: a second implementation is a second thing to
// keep in sync, and this one is guaranteed to agree with what's actually
// drawn.
//
// The active index affects the width (two-digit numbers scroll in and out
// of the window), so it's a parameter here too rather than assumed.
func tabStripWidth(count, active int) int {
	text, _ := renderTabStrip(count, active, tcell.ColorWhite, tcell.ColorBlack)
	return tview.TaggedStringWidth(text)
}

// setTabs tells the panel how many tabs exist and which one is active, so
// its own strip can draw them. Panel deliberately doesn't own the tab
// list itself — Root does (see Root.tabs) — for the same reason it owns
// none of the other cross-panel state it renders: a Panel is one tab, and
// asking one tab to be the authority on all the others inverts that.
//
// Called on every change to the tab set and on every switch, including
// for a panel that's about to become visible (see Root.refreshTabStrips).
func (p *Panel) setTabs(count, active int) {
	p.tabCount = count
	p.tabActive = active
	p.refreshTabStrip()
}

// refreshTabStrip redraws the strip from p.tabCount/p.tabActive and
// resizes its slot in the header row to match.
//
// With a single tab, renderTabStrip draws just the "+" — no numbers (one
// tab has nothing to number against) but not nothing either: an earlier
// version of this hid the whole strip down to zero width whenever there
// was only one tab, on the reasoning that tabs are opt-in and shouldn't
// cost header room nobody's using. In practice that hid the feature's
// only discoverable entry point along with it — a user who doesn't
// already know a keyboard shortcut exists has no way to find it (a real
// report caught this within minutes of testing). One small button is a
// far cheaper cost than that.
func (p *Panel) refreshTabStrip() {
	if p.tabStrip == nil || p.headerRow == nil {
		return // not built yet (a Panel mid-construction) — nothing to draw on
	}

	text, spans := renderTabStrip(p.tabCount, p.tabActive, p.theme.Text, p.theme.FocusedBackground)

	// headerTabStripGap columns of lead-in before the first glyph, drawn
	// as plain spaces in the strip's own text rather than a separate
	// blank Flex item beside it — the widget's own background color
	// (see styleTabStrip) then covers that gap the same as everywhere
	// else in it, instead of leaving a visibly different-colored seam
	// where a colorless spacer item used to sit (a real user report).
	leadIn := strings.Repeat(" ", headerTabStripGap)
	p.tabStrip.SetText(leadIn + text)
	for i := range spans {
		spans[i].start += headerTabStripGap
		spans[i].end += headerTabStripGap
	}
	p.tabStripSpans = spans
	// One trailing column of breathing room before the Details "<"
	// button, so the "+" doesn't sit flush against it.
	p.headerRow.ResizeItem(p.tabStrip, headerTabStripGap+tview.TaggedStringWidth(text)+1, 0)
}

// captureTabStripMouse turns a click on a tab number into a switch, and a
// click on the trailing "+" into a new tab — the mouse half of the same
// two actions Ctrl+1..Ctrl+0 and the context menu's "New tab" reach from
// the keyboard.
//
// A click anywhere else in the strip (the gaps between numbers, the "‹"/
// "›" markers, the trailing padding) opens the switcher overlay, which is
// the user's own stated intent for the strip as a whole: clicking "the
// tab area" should offer the full list with real paths, since the numbers
// alone deliberately don't say what any tab holds.
func (p *Panel) captureTabStripMouse(action tview.MouseAction, event *tcell.EventMouse) (tview.MouseAction, *tcell.EventMouse) {
	if !p.tabStrip.InRect(event.Position()) {
		return action, event
	}
	if action != tview.MouseLeftClick {
		return tview.MouseConsumed, nil
	}

	x, _ := event.Position()
	rectX, _, _, _ := p.tabStrip.GetInnerRect()
	col := x - rectX

	for _, span := range p.tabStripSpans {
		if col < span.start || col >= span.end {
			continue
		}
		if span.tab == tabStripSpanNew {
			if p.onNewTab != nil {
				p.onNewTab()
			}
		} else if p.onSelectTab != nil {
			p.onSelectTab(span.tab)
		}
		return tview.MouseConsumed, nil
	}

	if p.onOpenTabSwitcher != nil {
		p.onOpenTabSwitcher()
	}
	return tview.MouseConsumed, nil
}

// styleTabStrip paints the strip's own static colors — the inactive
// numbers and the background it all sits on. The active number's own
// highlight isn't here: it's a per-render color tag (see renderTabStrip),
// since only one number at a time carries it.
func (p *Panel) styleTabStrip(theme config.ResolvedTheme) {
	if p.tabStrip == nil {
		return
	}
	p.tabStrip.SetTextColor(theme.Text)
	p.tabStrip.SetBackgroundColor(theme.AccentBackground)
}
