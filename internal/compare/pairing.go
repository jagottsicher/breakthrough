package compare

// opKind is a single diff line's own operation — equal, deleted, or
// inserted — independent of go-udiff's own udiff.OpKind: this engine is
// shared between SideBySide (which does start from go-udiff's own
// Hunks) and ParseUnifiedDiff (which parses existing unified-diff text
// git itself already produced, never touching go-udiff at all), so it
// can't depend on either source's own types.
type opKind int

const (
	opEqual opKind = iota
	opDelete
	opInsert
)

// opLine is one line of a diff hunk, already classified — the shared
// input shape pairUpLines works from, regardless of which of the two
// sources above produced it.
type opLine struct {
	kind opKind
	text string
}

// pairUpLines turns a flat run of opLines (as one hunk's worth, in
// order) into LinePairs, threading leftNo/rightNo through so line
// numbers keep counting correctly across multiple calls spanning one
// file's several hunks (see ParseUnifiedDiff, which resets them to each
// hunk's own header-reported starting numbers instead of trusting a
// running count alone, since a hunk can skip arbitrarily far ahead).
func pairUpLines(lines []opLine, leftNo, rightNo *int) []LinePair {
	var pairs []LinePair
	for i := 0; i < len(lines); {
		switch lines[i].kind {
		case opEqual:
			*leftNo++
			*rightNo++
			pairs = append(pairs, LinePair{
				Left: lines[i].text, LeftNo: *leftNo,
				Right: lines[i].text, RightNo: *rightNo,
				Kind: PairSame,
			})
			i++
		default:
			dels := takeRun(lines, i, opDelete)
			i += len(dels)
			ins := takeRun(lines, i, opInsert)
			i += len(ins)
			pairs = append(pairs, pairUpChangedRun(dels, ins, leftNo, rightNo)...)
		}
	}
	return pairs
}

// takeRun returns the longest run of lines starting at i that all share
// kind — a hunk's own deleted lines for one edit always appear
// contiguously, followed immediately by that same edit's inserted
// lines (true of go-udiff's own Hunks, and of a real unified diff's own
// "-" lines followed by its own "+" lines for the same change), so a
// run never spans two unrelated edits.
func takeRun(lines []opLine, i int, kind opKind) []opLine {
	start := i
	for i < len(lines) && lines[i].kind == kind {
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
func pairUpChangedRun(dels, ins []opLine, leftNo, rightNo *int) []LinePair {
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
				Left: dels[i].text, LeftNo: *leftNo,
				Right: ins[i].text, RightNo: *rightNo,
				Kind: PairChanged,
			})
		case i < len(dels):
			*leftNo++
			pairs = append(pairs, LinePair{Left: dels[i].text, LeftNo: *leftNo, Kind: PairOnlyLeft})
		default:
			*rightNo++
			pairs = append(pairs, LinePair{Right: ins[i].text, RightNo: *rightNo, Kind: PairOnlyRight})
		}
	}
	return pairs
}
