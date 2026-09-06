package ui

import (
	"strings"
	"unicode/utf8"

	"github.com/rivo/tview"
)

// Column sizing for the file listing.
//
// The rule, per the user's own explicit request, is that the Size and
// Modified columns are never the ones that get cut: a truncated number
// or a half-shown timestamp is not merely ugly, it is wrong — "2026-09"
// and "878." tell you nothing you can act on, and there is no way to
// tell a clipped value from a real one. A truncated *name* stays useful,
// because the parts that identify a file (its beginning and its
// extension) survive at both ends.
//
// So the two data columns take exactly the width their content needs,
// and the name column gets whatever is left, with a middle ellipsis when
// that is not enough. Before this, both were fixed widths (14 and 21)
// and the name column expanded — which worked at full width and fell
// apart the moment a pane was half the screen: tview lays columns out
// left to right and runs out of room at the right-hand end, so Size
// collapsed to "…" and Modified vanished entirely, while an overlong
// filename kept every column it wanted.
//
// Both widths depend on what is actually on screen, not on a worst case:
// a directory of small files in human-readable mode needs six columns
// for Size, not fourteen, and giving the other twelve back to the name
// is the whole point.

// ellipsis marks where a name was shortened. One rune wide, so the
// arithmetic below can treat it as a single column.
const ellipsis = '…'

// nameColumnMinWidth is the floor for the name column. Below this,
// shortening stops being informative — there is no useful "beginning and
// end" left to keep — so the Size and Modified columns give way instead
// and tview's own right-edge clipping takes over, which at that width is
// all anyone can do.
const nameColumnMinWidth = 12

// fixedColumnsWidth is what the three glyph columns and the two
// separators occupy: checkbox, type and modifier are one cell each, and
// each separator is a single "│" (see columnSeparator). tview also puts
// one blank column between every pair of cells, which for eight columns
// is seven gaps.
const fixedColumnsWidth = 3 + 2 + 7

// sizeHeaderVariants and modHeaderVariants are each column's own header
// labels, longest first. The widest one that fits the column's data
// width is used; if none fits, the shortest is, and the column widens to
// hold it (see columnLayout).
//
// Abbreviating the header rather than the data is the same principle as
// shortening the name rather than the numbers: a shortened label still
// says what the column is, where "2026-09-0" says nothing.
//
// The modification column is simply "mtime", per the user's own
// explicit request — the word everyone working at a shell already uses
// for it, and short enough to fit whichever format the column is in, so
// it never costs the name column a single column of room. The variant
// mechanism still earns its keep for the trash's own label, which is
// genuinely too long for a column of Unix timestamps.
func sizeHeaderVariants() []string {
	return []string{"Size"}
}

func modHeaderVariants(trashView bool) []string {
	if trashView {
		// See onDescribeRows: browsing the trash relabels this column.
		// Not "mtime" — what's shown there is when the item was deleted,
		// not when it was last modified, and saying otherwise would be
		// plainly wrong rather than merely terse.
		return []string{"Deletion time", "Deleted", "Del"}
	}
	return []string{"mtime"}
}

// columnLayout is how wide each variable column should be for one
// particular render, and which header label fits in it.
type columnLayout struct {
	size, mod  int
	name       int
	sizeHeader string
	modHeader  string
}

// computeColumnLayout sizes the columns for a panel total columns wide,
// given the widest Size and Modified values it is about to show.
//
// sortArrowWidth is added to each header's own requirement because the
// active sort column carries a trailing arrow (see sortArrow) — leaving
// it out would make the column one column too narrow exactly when it is
// sorted, which is most of the time for at least one of them.
//
// The name column takes the remainder, never below nameColumnMinWidth.
// When even that does not fit, everything is simply as wide as it wants
// to be and the caller lets tview clip: at that width the pane cannot
// show a listing meaningfully anyway, and inventing a different failure
// mode for it would only add a case nobody can test against.
func computeColumnLayout(total, widestSize, widestMod int, trashView, sizeSorted, modSorted bool) columnLayout {
	const sortArrowWidth = 2 // " ↑" / " ↓", see sortArrow

	fit := func(variants []string, dataWidth int, sorted bool) (width int, label string) {
		extra := 0
		if sorted {
			extra = sortArrowWidth
		}
		for _, v := range variants {
			if utf8.RuneCountInString(v)+extra <= dataWidth {
				return dataWidth, v
			}
		}
		// Nothing fits the data width: the column grows to hold the
		// shortest label instead. A header the column cannot show is a
		// column nobody can sort by, since the label is the click target.
		shortest := variants[len(variants)-1]
		return utf8.RuneCountInString(shortest) + extra, shortest
	}

	var layout columnLayout
	layout.size, layout.sizeHeader = fit(sizeHeaderVariants(), widestSize, sizeSorted)
	layout.mod, layout.modHeader = fit(modHeaderVariants(trashView), widestMod, modSorted)

	layout.name = total - fixedColumnsWidth - layout.size - layout.mod
	if layout.name < nameColumnMinWidth {
		layout.name = nameColumnMinWidth
	}
	return layout
}

// truncateMiddle shortens s to at most width display columns, replacing
// the middle with an ellipsis.
//
// The middle, not the end, because both ends of a filename carry the
// information: the start distinguishes it from its neighbours and the
// end carries the extension, which is often the very thing being looked
// for. "annual-report-2026-final.pdf" cut at the end is
// "annual-report-20…", which could be anything; cut in the middle it is
// "annual-re…final.pdf".
//
// Rune-aware throughout, so a multi-byte character is never split in
// half. Width is counted in runes rather than display cells: this
// package's own listing is not double-width-aware anywhere else either
// (see the panel's own columns), and pretending otherwise here alone
// would be a false precision.
func truncateMiddle(s string, width int) string {
	runes := []rune(s)
	if width <= 0 {
		return ""
	}
	if len(runes) <= width {
		return s
	}
	if width == 1 {
		return string(ellipsis)
	}

	// The ellipsis costs one column; the rest is split with the extra
	// column, if any, going to the front — the beginning of a name earns
	// it slightly more often than the end does.
	keep := width - 1
	front := (keep + 1) / 2
	back := keep - front
	return string(runes[:front]) + string(ellipsis) + string(runes[len(runes)-back:])
}

// shortenNameLabel fits one listing row's name into width columns.
//
// suffix is the part that must survive if at all possible — a
// directory's trailing "/" or a symlink's " -> target" — because it says
// what kind of entry this is, which the name alone does not. Only when
// the suffix cannot fit beside any useful amount of name does the whole
// label get shortened as one string instead.
//
// Takes and returns plain, unescaped text: the caller escapes and
// applies style tags afterwards (see addRow), which is the only order
// that works — shortening a string that already contains tview tags
// would cut through a tag and corrupt every style after it.
func shortenNameLabel(name, suffix string, width int) (shortName, shortSuffix string) {
	nameLen := utf8.RuneCountInString(name)
	suffixLen := utf8.RuneCountInString(suffix)

	if nameLen+suffixLen <= width {
		return name, suffix
	}
	// Keep the suffix whole and shorten the name around it, as long as
	// that leaves enough name to be worth reading.
	if room := width - suffixLen; room >= nameColumnMinWidth/2 {
		return truncateMiddle(name, room), suffix
	}
	// The suffix is too long to keep (a symlink to a deep path, say):
	// shorten the two together and let the ellipsis fall wherever it
	// falls.
	return truncateMiddle(name+suffix, width), ""
}

// padLeft right-aligns s in width columns — the Size and Modified cells'
// own formatting, which is right-aligned so digits line up down the
// column.
//
// Never truncates: these are the columns this whole file exists to keep
// intact (see the file's own doc comment). A value wider than width
// means the layout was computed against different data than is being
// rendered, which is a bug worth seeing rather than hiding behind a
// silent cut.
func padLeft(s string, width int) string {
	if pad := width - tview.TaggedStringWidth(s); pad > 0 {
		return strings.Repeat(" ", pad) + s
	}
	return s
}
