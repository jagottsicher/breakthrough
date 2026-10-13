package compare

import (
	"strings"

	"github.com/aymanbagabas/go-udiff"
)

// PairKind is what one LinePair represents: an unchanged line present on
// both sides, a changed line present on both but with different content,
// or a line that only exists on one side (the other side renders as a
// blank filler, same as a side-by-side diff view anywhere else).
type PairKind int

const (
	PairSame PairKind = iota
	PairChanged
	PairOnlyLeft
	PairOnlyRight
)

// LinePair is one row of a side-by-side diff: a line from the left file,
// a line from the right file, which of the four PairKinds they form, and
// each side's own 1-based line number (0 on whichever side has no line
// here — an OnlyLeft/OnlyRight row, or the filler half of one).
type LinePair struct {
	Left, Right     string
	LeftNo, RightNo int
	Kind            PairKind
}

// SideBySide computes a full, line-by-line side-by-side comparison of a
// and b: every line of both, in order, paired up — never the
// context-trimmed, multi-hunk shape a unified diff uses for patch(1),
// since a side-by-side view has no "..." gap to collapse an unchanged
// run into: the user scrolls past it exactly like any other line.
//
// Built on udiff.Lines/ToUnifiedDiff (Myers diff) rather than parsing
// diff(1)'s own unified-diff text — see this package's own doc comment
// for why. contextLines is sized to the combined line count of both
// files specifically so toUnified's own hunk-joining logic (anything
// within 2*contextLines of the previous change merges into the same
// hunk) always produces exactly one hunk spanning the whole file,
// without the pathological huge-constant version of the same idea
// spending a billion no-op iterations expanding context past either
// file's actual length.
func SideBySide(a, b string) []LinePair {
	edits := udiff.Lines(a, b)
	if len(edits) == 0 {
		return sameLinePairs(a)
	}

	contextLines := strings.Count(a, "\n") + strings.Count(b, "\n") + 2
	ud, err := udiff.ToUnifiedDiff("", "", a, edits, contextLines)
	if err != nil {
		// Can't happen: edits came from udiff.Lines(a, b) itself, so
		// they're always consistent with a — the same invariant
		// udiff.Unified relies on to treat this as unreachable.
		panic("compare: internal error building side-by-side diff: " + err.Error())
	}

	var pairs []LinePair
	leftNo, rightNo := 0, 0
	for _, h := range ud.Hunks {
		lines := h.Lines
		for i := 0; i < len(lines); {
			switch lines[i].Kind {
			case udiff.Equal:
				leftNo++
				rightNo++
				pairs = append(pairs, LinePair{
					Left: trimEOL(lines[i].Content), LeftNo: leftNo,
					Right: trimEOL(lines[i].Content), RightNo: rightNo,
					Kind: PairSame,
				})
				i++
			case udiff.Delete, udiff.Insert:
				dels := takeRun(lines, i, udiff.Delete)
				i += len(dels)
				ins := takeRun(lines, i, udiff.Insert)
				i += len(ins)
				pairs = append(pairs, pairUpChangedRun(dels, ins, &leftNo, &rightNo)...)
			default:
				i++
			}
		}
	}
	return pairs
}

// sameLinePairs is SideBySide's own no-edits shortcut: a and b are
// identical, so every line of a pairs with itself rather than routing
// an empty edit list through ToUnifiedDiff, which returns zero Hunks for
// zero edits (there's nothing to expand context around) and would leave
// SideBySide reporting no lines at all for two files that are the same
// rather than every line as PairSame.
func sameLinePairs(a string) []LinePair {
	lines := strings.SplitAfter(a, "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	pairs := make([]LinePair, len(lines))
	for i, l := range lines {
		content := trimEOL(l)
		pairs[i] = LinePair{Left: content, LeftNo: i + 1, Right: content, RightNo: i + 1, Kind: PairSame}
	}
	return pairs
}

// takeRun returns the longest run of lines starting at i that all share
// kind — Delete lines for one edit always appear contiguously, followed
// immediately by that same edit's Insert lines (see toUnified in
// go-udiff's own unified.go), so a run never spans two unrelated edits.
func takeRun(lines []udiff.Line, i int, kind udiff.OpKind) []udiff.Line {
	start := i
	for i < len(lines) && lines[i].Kind == kind {
		i++
	}
	return lines[start:i]
}

// pairUpChangedRun aligns one edit's deleted lines against its inserted
// lines position by position — a plain replacement reads most naturally
// as "this line became that line" rather than all deletions stacked
// above all insertions. Where one run is longer, the excess lines render
// as OnlyLeft/OnlyRight (filler blank on the other side), never paired
// with unrelated content from the other run.
func pairUpChangedRun(dels, ins []udiff.Line, leftNo, rightNo *int) []LinePair {
	n := len(dels)
	if len(ins) > n {
		n = len(ins)
	}
	pairs := make([]LinePair, 0, n)
	for i := 0; i < n; i++ {
		switch {
		case i < len(dels) && i < len(ins):
			*leftNo++
			*rightNo++
			pairs = append(pairs, LinePair{
				Left: trimEOL(dels[i].Content), LeftNo: *leftNo,
				Right: trimEOL(ins[i].Content), RightNo: *rightNo,
				Kind: PairChanged,
			})
		case i < len(dels):
			*leftNo++
			pairs = append(pairs, LinePair{Left: trimEOL(dels[i].Content), LeftNo: *leftNo, Kind: PairOnlyLeft})
		default:
			*rightNo++
			pairs = append(pairs, LinePair{Right: trimEOL(ins[i].Content), RightNo: *rightNo, Kind: PairOnlyRight})
		}
	}
	return pairs
}

// trimEOL strips the single trailing newline splitLines (go-udiff's own
// internal line splitter) always leaves on a Line's Content, except for
// a file's last line when it has none — display never wants either.
func trimEOL(s string) string {
	return strings.TrimSuffix(s, "\n")
}
