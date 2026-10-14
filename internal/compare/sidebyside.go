package compare

import (
	"strings"

	"github.com/aymanbagabas/go-udiff"
)

// PairKind is what one LinePair represents: an unchanged line present on
// both sides, a changed line present on both but with different content,
// a line that only exists on one side (the other side renders as a
// blank filler, same as a side-by-side diff view anywhere else), or a
// gap marking a skipped, unchanged region between two hunks of an
// already-hunked unified diff (see ParseUnifiedDiff — SideBySide never
// produces PairGap itself, since it always expands context to cover the
// whole file, leaving nothing to skip).
type PairKind int

const (
	PairSame PairKind = iota
	PairChanged
	PairOnlyLeft
	PairOnlyRight
	PairGap
)

// LinePair is one row of a side-by-side diff: a line from the left file,
// a line from the right file, which of the PairKinds they form, and
// each side's own 1-based line number (0 on whichever side has no line
// here — an OnlyLeft/OnlyRight/Gap row, or the filler half of one).
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
		pairs = append(pairs, pairUpLines(toOpLines(h.Lines), &leftNo, &rightNo)...)
	}
	return pairs
}

// toOpLines converts go-udiff's own []Line into the shared pairing
// engine's own input shape (see pairing.go) — the one place SideBySide
// still touches a go-udiff type directly, isolating the rest of the
// pairing logic from it so ParseUnifiedDiff (which never sees a
// udiff.Line at all) can reuse the exact same engine.
func toOpLines(lines []udiff.Line) []opLine {
	out := make([]opLine, len(lines))
	for i, l := range lines {
		var kind opKind
		switch l.Kind {
		case udiff.Delete:
			kind = opDelete
		case udiff.Insert:
			kind = opInsert
		default:
			kind = opEqual
		}
		out[i] = opLine{kind: kind, text: trimEOL(l.Content)}
	}
	return out
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

// trimEOL strips the single trailing newline splitLines (go-udiff's own
// internal line splitter) always leaves on a Line's Content, except for
// a file's last line when it has none — display never wants either.
func trimEOL(s string) string {
	return strings.TrimSuffix(s, "\n")
}
