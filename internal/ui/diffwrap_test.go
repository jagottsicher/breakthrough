package ui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// drawTextViewRow renders one line into a TextView on a simulation
// screen and returns the row it actually produced, trailing blanks
// trimmed — the only way to check a claim about what tview *draws*
// rather than about what this package computes.
func drawTextViewRow(t *testing.T, text string, width int) string {
	t.Helper()
	screen := tcell.NewSimulationScreen("")
	if err := screen.Init(); err != nil {
		t.Fatalf("screen.Init: %v", err)
	}
	t.Cleanup(screen.Fini)
	screen.SetSize(width, 1)

	view := tview.NewTextView()
	view.SetWrap(false)
	view.SetText(text)
	view.SetRect(0, 0, width, 1)
	view.Draw(screen)

	var row strings.Builder
	for x := 0; x < width; x++ {
		ch, _, _, _ := screen.GetContent(x, 0)
		row.WriteRune(ch)
	}
	return strings.TrimRight(row.String(), " ")
}

// TestExpandTabsDrawsTheSameAsTviewsOwnTabHandling is the empirical
// half of expandTabs' own doc comment: the whole point of expanding
// tabs ourselves is that the result has to look exactly like what
// tview would have drawn from the tabs themselves, or soft wrap would
// silently shift tab-indented code sideways the moment it's switched
// on. Checked against a real draw, not against the tab-stop formula
// this package would just be restating.
func TestExpandTabsDrawsTheSameAsTviewsOwnTabHandling(t *testing.T) {
	for _, line := range []string{
		"a\tb",
		"\tindented",
		"abcd\tx",
		"abc\tdef\tghi",
		"\t\tdouble",
		"no tabs here",
	} {
		withTabs := drawTextViewRow(t, line, 60)
		expanded := drawTextViewRow(t, expandTabs(line, 0), 60)
		if withTabs != expanded {
			t.Errorf("expandTabs(%q) drew %q, tview drew %q from the tabs themselves", line, expanded, withTabs)
		}
	}
}

func TestExpandTabsCountsFromTheGivenStartColumn(t *testing.T) {
	// A gutter five columns wide puts the content's first tab stop at
	// column 8, not at column 4 — the case both side-by-side views are
	// in, where the line number column precedes every content column.
	for _, tc := range []struct {
		line     string
		startCol int
		want     string
	}{
		{"\tx", 0, "    x"},
		{"\tx", 5, "   x"},
		{"\tx", 4, "    x"},
		// Starting at 5, "ab" lands the tab on column 7, one short of
		// the stop at 8 — so a single space, not a full TabSize worth.
		{"ab\tx", 5, "ab x"},
	} {
		if got := expandTabs(tc.line, tc.startCol); got != tc.want {
			t.Errorf("expandTabs(%q, %d) = %q, want %q", tc.line, tc.startCol, got, tc.want)
		}
	}
}

func TestExpandTabsLeavesTabFreeTextAlone(t *testing.T) {
	const line = "nothing to expand here"
	if got := expandTabs(line, 3); got != line {
		t.Errorf("expandTabs(%q) = %q, want it unchanged", line, got)
	}
}

// TestExpandTabsTaggedCountsStyleTagsAsNoWidth pins the difference
// between the two expanders: a style tag occupies no columns, so a tab
// after one has to land on the same stop it would have landed on with
// the tag absent.
func TestExpandTabsTaggedCountsStyleTagsAsNoWidth(t *testing.T) {
	plain := expandTabs("ab\tx", 0)
	tagged := expandTabsTagged("[red]ab[-]\tx", 0)
	if want := "[red]ab[-]" + strings.TrimPrefix(plain, "ab"); tagged != want {
		t.Errorf("expandTabsTagged = %q, want %q (same stop as the untagged %q)", tagged, want, plain)
	}
}

// TestWrapTaggedKeepsEverySegmentWithinWidth is the property the whole
// feature rests on: once wrapped, nothing is wider than the pane, which
// is also what lets the horizontal scrollbar hide itself.
func TestWrapTaggedKeepsEverySegmentWithinWidth(t *testing.T) {
	const width = 20
	for _, line := range []string{
		"a short one",
		strings.Repeat("word ", 40),
		strings.Repeat("x", 97),
		"[red]tagged[-] " + strings.Repeat("content ", 20),
	} {
		for i, segment := range wrapTagged(line, width) {
			if got := tview.TaggedStringWidth(segment); got > width {
				t.Errorf("segment %d of %.20q… is %d columns wide, want at most %d", i, line, got, width)
			}
		}
	}
}

func TestWrapTaggedWithoutAWidthReturnsTheLineUnchanged(t *testing.T) {
	const line = "[red]left alone[-]"
	for _, width := range []int{0, -1} {
		got := wrapTagged(line, width)
		if len(got) != 1 || got[0] != line {
			t.Errorf("wrapTagged(%q, %d) = %q, want exactly [%q]", line, width, got, line)
		}
	}
}

// TestWrapRenderedTextWithoutAWidthIsTheIdentity is what keeps every
// caller that reads a scroll offset as a line number correct while soft
// wrap is off, through the same code path rather than a second one.
func TestWrapRenderedTextWithoutAWidthIsTheIdentity(t *testing.T) {
	const text = "first\nsecond\nthird"
	got, rows := wrapRenderedText(text, 0, 2)
	if got != text {
		t.Errorf("text = %q, want it unchanged as %q", got, text)
	}
	for i, line := range rows {
		if line != i {
			t.Errorf("row %d maps to line %d, want the identity", i, line)
		}
	}
	if len(rows) != 3 {
		t.Errorf("row map has %d entries, want 3", len(rows))
	}
}

// TestWrapRenderedTextMapsEveryRowToItsSourceLine pins the map hunk
// staging reads: whichever row the view is scrolled to has to resolve
// back to the diff line it came from, or "s" stages the wrong hunk.
func TestWrapRenderedTextMapsEveryRowToItsSourceLine(t *testing.T) {
	text := strings.Join([]string{
		"short",
		strings.Repeat("long ", 20),
		"short again",
	}, "\n")

	wrapped, rows := wrapRenderedText(text, 20, 2)
	produced := strings.Split(wrapped, "\n")
	if len(produced) != len(rows) {
		t.Fatalf("produced %d rows but %d map entries", len(produced), len(rows))
	}
	if len(produced) <= 3 {
		t.Fatalf("produced %d rows, want the middle line to have wrapped into several", len(produced))
	}

	if rows[0] != 0 {
		t.Errorf("first row maps to line %d, want 0", rows[0])
	}
	if last := rows[len(rows)-1]; last != 2 {
		t.Errorf("last row maps to line %d, want 2", last)
	}
	for i := 1; i < len(rows); i++ {
		if rows[i] < rows[i-1] {
			t.Fatalf("row map is not non-decreasing at %d: %v", i, rows)
		}
		if rows[i] > rows[i-1]+1 {
			t.Errorf("row map skips line %d: %v", rows[i-1]+1, rows)
		}
	}
}

// TestWrapRenderedTextIndentsContinuationRowsOnly checks the one thing
// that makes a wrapped diff readable: a line's own first row starts
// where it always did, every further row is pushed in.
func TestWrapRenderedTextIndentsContinuationRowsOnly(t *testing.T) {
	wrapped, rows := wrapRenderedText(strings.Repeat("word ", 20), 20, gitBreachDiffWrapIndent)
	produced := strings.Split(wrapped, "\n")
	if len(produced) < 2 {
		t.Fatalf("produced %d rows, want several", len(produced))
	}
	indent := strings.Repeat(" ", gitBreachDiffWrapIndent)
	if strings.HasPrefix(produced[0], indent) {
		t.Errorf("first row %q is indented, want it flush left", produced[0])
	}
	for i := 1; i < len(produced); i++ {
		if !strings.HasPrefix(produced[i], indent) {
			t.Errorf("continuation row %d = %q, want it to start with %q", i, produced[i], indent)
		}
		if rows[i] != 0 {
			t.Errorf("continuation row %d maps to line %d, want 0", i, rows[i])
		}
	}
}

// TestSourceLineAtRowAndBackRoundTrips is what carrying the reading
// position across a re-render depends on.
func TestSourceLineAtRowAndBackRoundTrips(t *testing.T) {
	rows := []int{0, 1, 1, 1, 2, 3, 3}
	for row, line := range rows {
		if got := sourceLineAtRow(rows, row); got != line {
			t.Errorf("sourceLineAtRow(row %d) = %d, want %d", row, got, line)
		}
	}
	for line, wantRow := range map[int]int{0: 0, 1: 1, 2: 4, 3: 5} {
		if got := rowForSourceLine(rows, line); got != wantRow {
			t.Errorf("rowForSourceLine(line %d) = %d, want its first row %d", line, got, wantRow)
		}
	}
}

// TestSourceLineAtRowFallsBackToTheRowItself covers the case every
// caller would otherwise need its own guard for: a view that has never
// rendered wrapped has no map at all, and there row and line are the
// same thing.
func TestSourceLineAtRowFallsBackToTheRowItself(t *testing.T) {
	if got := sourceLineAtRow(nil, 7); got != 7 {
		t.Errorf("sourceLineAtRow(nil, 7) = %d, want 7", got)
	}
	if got := sourceLineAtRow([]int{0, 0}, 9); got != 9 {
		t.Errorf("sourceLineAtRow(short map, 9) = %d, want 9", got)
	}
}

// TestWriteDiffPairRowsPadsTheShorterSide pins the invariant both
// two-pane diff views read as "row N on the left is row N on the
// right" — without the padding, one unevenly-wrapping pair would push
// everything below it permanently out of step.
func TestWriteDiffPairRowsPadsTheShorterSide(t *testing.T) {
	var left, right strings.Builder
	rows := writeDiffPairRows(&left, &right, []string{"a", "b", "c"}, []string{"x"})
	if rows != 3 {
		t.Errorf("rows = %d, want 3", rows)
	}
	gotLeft := strings.Split(strings.TrimSuffix(left.String(), "\n"), "\n")
	gotRight := strings.Split(strings.TrimSuffix(right.String(), "\n"), "\n")
	if len(gotLeft) != len(gotRight) {
		t.Fatalf("left has %d rows, right has %d — they must match", len(gotLeft), len(gotRight))
	}
	if gotRight[0] != "x" || gotRight[1] != "" || gotRight[2] != "" {
		t.Errorf("right = %q, want the one real row then two blanks", gotRight)
	}
}

// TestCompareDiffLineRowsWithoutWrapIsUnchanged pins that switching
// soft wrap off leaves both side-by-side views rendering exactly what
// they rendered before this existed: one row, gutter, content, and the
// full "[-:-:-]" reset at the end.
func TestCompareDiffLineRowsWithoutWrapIsUnchanged(t *testing.T) {
	palette := paletteFor(tcell.ColorBlack)
	rows := compareDiffLineRows(12, "content", "x.txt", palette, gitBreachDiffAddedBackground, true, 0)
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want exactly 1 without wrapping", len(rows))
	}
	wantPrefix := fmt.Sprintf("%*d [:#%06x:]", compareDiffGutterWidth, 12, uint32(gitBreachDiffAddedBackground.Hex())&0xffffff)
	if !strings.HasPrefix(rows[0], wantPrefix) {
		t.Errorf("row = %q, want it to start with %q", rows[0], wantPrefix)
	}
	if !strings.HasSuffix(rows[0], "[-:-:-]") {
		t.Errorf("row = %q, want it to end with the full reset %q", rows[0], "[-:-:-]")
	}
}

func TestCompareDiffLineRowsBlanksTheGutterOnContinuationRows(t *testing.T) {
	palette := paletteFor(tcell.ColorBlack)
	rows := compareDiffLineRows(7, strings.Repeat("word ", 20), "x.txt", palette, tcell.ColorDefault, false, 20)
	if len(rows) < 2 {
		t.Fatalf("rows = %d, want the line to have wrapped", len(rows))
	}
	if want := fmt.Sprintf("%*d ", compareDiffGutterWidth, 7); !strings.HasPrefix(rows[0], want) {
		t.Errorf("first row = %q, want it to start with the line number %q", rows[0], want)
	}
	blank := strings.Repeat(" ", compareDiffGutterWidth+1)
	for i := 1; i < len(rows); i++ {
		if !strings.HasPrefix(rows[i], blank) {
			t.Errorf("continuation row %d = %q, want a blank gutter", i, rows[i])
		}
		if strings.TrimSpace(rows[i][:compareDiffGutterWidth+1]) != "" {
			t.Errorf("continuation row %d repeats the line number: %q", i, rows[i])
		}
	}
}

// TestCompareDiffLineRowsTintsEveryRow pins the per-segment tag scheme:
// every row of a changed line carries the background, and only the last
// one resets it in full — the rows in between reset the background
// alone ("[:-:]"), so a syntax color opened before the break survives
// into the next row.
func TestCompareDiffLineRowsTintsEveryRow(t *testing.T) {
	palette := paletteFor(tcell.ColorBlack)
	rows := compareDiffLineRows(1, strings.Repeat("word ", 20), "x.txt", palette, gitBreachDiffRemovedBackground, true, 20)
	if len(rows) < 2 {
		t.Fatalf("rows = %d, want the line to have wrapped", len(rows))
	}
	tint := fmt.Sprintf("[:#%06x:]", uint32(gitBreachDiffRemovedBackground.Hex())&0xffffff)
	for i, row := range rows {
		if !strings.Contains(row, tint) {
			t.Errorf("row %d = %q, want it to carry the background tint %q", i, row, tint)
		}
		if i == len(rows)-1 {
			if !strings.HasSuffix(row, "[-:-:-]") {
				t.Errorf("last row = %q, want the full reset", row)
			}
			continue
		}
		if !strings.HasSuffix(row, "[:-:]") {
			t.Errorf("row %d = %q, want it to close with the background-only reset %q", i, row, "[:-:]")
		}
	}
}
