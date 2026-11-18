package ui

import (
	"sort"
	"strings"

	"github.com/rivo/tview"
)

// diffWrapMinContentWidth is the narrowest content column soft wrap will
// still wrap into. Below it, a pane is so narrow that wrapping produces
// a column of one- or two-character fragments that reads worse than the
// sideways scroll it replaces — so the affected view falls back to not
// wrapping at all until there's room again, rather than rendering
// something unusable. Deliberately generous enough to cover the usual
// cause (a half-width pane in a narrow terminal, or the first frame
// before a pane has ever been laid out).
const diffWrapMinContentWidth = 12

// diffwrap.go: the two primitives every diff view's own soft-wrap mode
// is built from. Both diff screens (Compare Faultline's own two panes,
// Git Breach's unified pager and its side-by-side view) wrap by
// emitting real "\n"-separated rows themselves rather than by turning
// tview's own SetWrap(true) on.
//
// That choice is not stylistic. tview indexes a wrapped TextView per
// *displayed* row, not per logical line (see textview.go's own
// lineIndex/parseAhead, and ScrollTo, which addresses that same index),
// so with SetWrap(true):
//
//   - two side-by-side panes wrap independently, and from the first
//     line pair that wraps a different number of times, every following
//     row is misaligned against its own counterpart — a still image is
//     already wrong, before any scrolling happens;
//   - scrollTo(row) no longer means the same content in both panes,
//     which is exactly what compareDiffPaneContent's and
//     gitBreachDiffSplitContent's own linked scrolling rests on;
//   - textViewContent.lineWidths (scrollbar.go) stops being meaningful
//     at all — its own doc comment spells out why a wrapped view has no
//     recoverable per-displayed-row text to measure — and the vertical
//     scrollbar's text-density minimap goes with it.
//
// Wrapping in the render layer instead keeps all three properties:
// every emitted row is still one logical line as far as tview is
// concerned, so row index stays screen row, lineWidths stays honest,
// and the horizontal scrollbar hides itself through its own existing
// maxOffset() <= 0 rule once nothing overflows any more.

// expandTabs replaces every tab in s with as many spaces as it would
// have rendered to, counting columns from startCol.
//
// Needed because the two measurements involved disagree about tabs:
// tview.WordWrap and tview.TaggedStringWidth (both built on step())
// have no tab case at all and count a tab as its bare grapheme width,
// while TextView's own parseAhead explicitly advances to the next
// multiple of tview.TabSize (textview.go). Wrapping tab-indented code —
// which is to say, most of a Go diff — against the first measurement
// would produce rows that the second one then draws several columns
// wider than the pane, clipped and unreachable, since the horizontal
// scrollbar is itself hidden in wrap mode.
//
// Always applied to plain text *before* highlighting, never to text
// that already carries tview style tags: a tag's own "[" ... "]" bytes
// occupy no columns, so counting them here would place every later tab
// stop on this line wrong. The resulting rows look identical to what
// tview would have drawn from the tabs themselves, because startCol
// lets the caller account for whatever fixed prefix (a line-number
// gutter, a diff's own +/- marker column) precedes the content on the
// row — the same origin tview counts its own tab stops from.
//
// Returns s unchanged when it holds no tab at all, which is the
// common case and worth not allocating for.
func expandTabs(s string, startCol int) string {
	if !strings.ContainsRune(s, '\t') {
		return s
	}
	if startCol < 0 {
		startCol = 0
	}

	var b strings.Builder
	b.Grow(len(s))
	col := startCol
	for _, r := range s {
		if r != '\t' {
			b.WriteRune(r)
			col++
			continue
		}
		stop := tview.TabSize - col%tview.TabSize
		b.WriteString(strings.Repeat(" ", stop))
		col += stop
	}
	return b.String()
}

// expandTabsTagged is expandTabs for text that already carries tview
// style tags — the one-pane diff views, which wrap text that has
// already been rendered rather than raw content (see
// gitBreachWrapRenderedText).
//
// Splitting on the tab character is safe on tagged text: a tab can
// never occur inside a tview tag, which accepts only letters, digits
// and "#:-" between its brackets (strings.go's own parseTag bails out
// on anything else), so every tab in the string is a real one in the
// content. Each chunk between two tabs is then measured the same way
// tview itself would measure it, tags counting as nothing.
//
// Kept separate from expandTabs rather than replacing it: measuring
// *plain* content with a tag-aware function would get a literal
// "[foo]" in someone's source code wrong, since that only becomes an
// escaped non-tag later, when renderSyntax writes it out.
func expandTabsTagged(s string, startCol int) string {
	if !strings.ContainsRune(s, '\t') {
		return s
	}
	if startCol < 0 {
		startCol = 0
	}

	chunks := strings.Split(s, "\t")
	var b strings.Builder
	b.Grow(len(s))
	col := startCol
	for i, chunk := range chunks {
		b.WriteString(chunk)
		col += tview.TaggedStringWidth(chunk)
		if i == len(chunks)-1 {
			break
		}
		stop := tview.TabSize - col%tview.TabSize
		b.WriteString(strings.Repeat(" ", stop))
		col += stop
	}
	return b.String()
}

// wrapTagged splits one already-tagged line into consecutive segments,
// none of them wider than width columns on screen.
//
// A thin wrapper around tview.WordWrap, which is the right tool for
// exactly two reasons worth not reimplementing: it counts style tags as
// zero width (these views all run SetDynamicColors(true), so a line is
// full of them), and it never splits inside a tag, since a whole tag is
// one step of its own parser. Line breaking follows Unicode UAX #14,
// so a long line breaks at word boundaries where it can and mid-word
// only where it must.
//
// width <= 0 means "don't wrap" and returns the line untouched as a
// single segment — the state every caller is in before its own pane has
// been laid out once (GetInnerRect is 0,0,0,0 until the first Draw) and
// the state they stay in while soft wrap is switched off, so the
// unwrapped output stays byte-for-byte what it was before this file
// existed.
func wrapTagged(text string, width int) []string {
	if width <= 0 {
		return []string{text}
	}
	segments := tview.WordWrap(text, width)
	if len(segments) == 0 {
		// WordWrap returns nothing at all only for an empty input (its
		// own trailing append always contributes one segment
		// otherwise). An empty line is still a row on screen, so it has
		// to survive as one.
		return []string{text}
	}
	return segments
}

// writeDiffPairRows appends one side-by-side line pair's already-wrapped
// rows to both panes at once, padding whichever side produced fewer
// rows with blank ones.
//
// This is what keeps the one invariant both two-pane diff views rest
// on: row N in the left pane is row N's counterpart in the right pane.
// Without wrapping that holds for free (one LinePair is one row on each
// side); with it, a line that wraps three times on the left and once on
// the right would otherwise push everything below it two rows out of
// step, and stay that way for the rest of the file. The padding costs
// two blank rows there and keeps scrollTo(row) meaning the same content
// in both panes — which is what compareDiffPaneContent's and
// gitBreachDiffSplitContent's own linked scrolling, and the shared
// horizontal scrollbar's own per-row minimap, both read it as.
// Returns how many rows the pair occupied on each side, so a caller
// building a row index (see diffPairAtRow) can keep count without
// measuring the builders it just wrote into.
func writeDiffPairRows(left, right *strings.Builder, leftRows, rightRows []string) int {
	rows := len(leftRows)
	if len(rightRows) > rows {
		rows = len(rightRows)
	}
	for i := 0; i < rows; i++ {
		if i < len(leftRows) {
			left.WriteString(leftRows[i])
		}
		left.WriteString("\n")
		if i < len(rightRows) {
			right.WriteString(rightRows[i])
		}
		right.WriteString("\n")
	}
	return rows
}

// wrapRenderedText soft-wraps a whole already-rendered, already-tagged
// block of text — the shape a single-pane diff view holds, as opposed
// to the two-pane views, which wrap each side's content while rendering
// it (see compareDiffLineRows) because their line-number gutter must
// stay out of the wrapped region and out of the background tint.
//
// A single pane has no such gutter, so wrapping afterwards is both
// simpler and better: the renderer that produced the text stays
// completely untouched, whatever it was — a colorized diff, a
// highlighted untracked file, a conflict, a blame, a plain error
// message — and all of them gain soft wrap from this one place.
//
// indent is prepended to every row but a line's first. It deliberately
// goes *outside* any tag of its own, so it inherits whatever style was
// in force at the break (tview carries style across a line break): on a
// tinted diff line the indent is tinted too, which is what makes a
// continuation row read as part of that line rather than as a gap in
// it. Wrapping happens at width minus indent so the indented rows still
// fit the pane.
//
// The second return value maps each produced row back to the index of
// the source line it came from. With width <= 0 nothing is wrapped, the
// text comes back unchanged, and that map is the identity — which is
// what keeps every caller that reads a scroll offset as a line number
// (openGitBreachStageHunk, notably) correct in both modes through one
// code path instead of two.
func wrapRenderedText(text string, width, indent int) (string, []int) {
	lines := strings.Split(text, "\n")
	if width <= 0 {
		rows := make([]int, len(lines))
		for i := range rows {
			rows[i] = i
		}
		return text, rows
	}

	segmentWidth := width - indent
	if segmentWidth < diffWrapMinContentWidth {
		rows := make([]int, len(lines))
		for i := range rows {
			rows[i] = i
		}
		return text, rows
	}
	pad := strings.Repeat(" ", indent)

	var b strings.Builder
	b.Grow(len(text))
	rowToLine := make([]int, 0, len(lines))
	for i, line := range lines {
		for j, segment := range wrapTagged(expandTabsTagged(line, 0), segmentWidth) {
			// The separator goes before every row but the very first,
			// rather than after every row but the last: with one
			// segment per line, which is every line when not wrapping,
			// that reproduces the input exactly.
			if len(rowToLine) > 0 {
				b.WriteString("\n")
			}
			if j > 0 {
				b.WriteString(pad)
			}
			b.WriteString(segment)
			rowToLine = append(rowToLine, i)
		}
	}
	return b.String(), rowToLine
}

// sourceLineAtRow reads one entry out of a wrapRenderedText row map:
// which source line the given rendered row came from.
//
// Tolerates an empty map and an out-of-range row by falling back to the
// row number itself, which is exactly right in the one case that
// produces an empty map — a view that has never rendered wrapped, where
// row and line are the same thing. That keeps callers free of a "has
// this been wrapped yet" check they'd otherwise all need.
func sourceLineAtRow(rowToLine []int, row int) int {
	if row < 0 {
		return 0
	}
	if row >= len(rowToLine) {
		return row
	}
	return rowToLine[row]
}

// rowForSourceLine is sourceLineAtRow's own inverse: the first rendered
// row a given source line occupies. Binary search is valid because a
// row map is non-decreasing by construction — wrapRenderedText emits
// every row of line i before any row of line i+1.
//
// Used to carry a reading position across a re-render at a different
// width: out through sourceLineAtRow against the old map, back in
// through here against the new one.
func rowForSourceLine(rowToLine []int, line int) int {
	if len(rowToLine) == 0 || line <= 0 {
		return 0
	}
	row := sort.SearchInts(rowToLine, line)
	if row >= len(rowToLine) {
		return len(rowToLine) - 1
	}
	return row
}

// diffPairAtRow maps a rendered row back to the index of the line pair
// that produced it, given each pair's own first row in document order.
//
// Needed because turning soft wrap on or off re-renders the whole view
// at a different number of rows: the row the user was looking at before
// means nothing afterward, but the line pair does. Mapping out through
// here and back in through diffRowForPair is what keeps the toggle from
// throwing away the reading position, which on a long diff is the whole
// difference between a display preference and a navigation accident.
//
// Clamps rather than failing: a row past the last pair (possible while
// a stale scroll offset is being restored) answers with the last pair.
func diffPairAtRow(starts []int, row int) int {
	if len(starts) == 0 {
		return 0
	}
	index := sort.SearchInts(starts, row+1) - 1
	if index < 0 {
		return 0
	}
	if index >= len(starts) {
		return len(starts) - 1
	}
	return index
}

// diffRowForPair is diffPairAtRow's own inverse: the first row a given
// line pair occupies. Clamps the same way, for the same reason.
func diffRowForPair(starts []int, pair int) int {
	if len(starts) == 0 {
		return 0
	}
	if pair < 0 {
		pair = 0
	}
	if pair >= len(starts) {
		pair = len(starts) - 1
	}
	return starts[pair]
}
