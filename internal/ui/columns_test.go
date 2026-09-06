package ui

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestTruncateMiddleKeepsBothEnds(t *testing.T) {
	got := truncateMiddle("annual-report-2026-final.pdf", 20)

	if utf8.RuneCountInString(got) != 20 {
		t.Errorf("%q is %d runes, want exactly 20", got, utf8.RuneCountInString(got))
	}
	if !strings.HasPrefix(got, "annual") {
		t.Errorf("%q lost its beginning", got)
	}
	if !strings.HasSuffix(got, ".pdf") {
		t.Errorf("%q lost its extension — the half most often being looked for", got)
	}
	if !strings.ContainsRune(got, ellipsis) {
		t.Errorf("%q doesn't mark where it was cut", got)
	}
}

func TestTruncateMiddleLeavesShortNamesAlone(t *testing.T) {
	for _, s := range []string{"", "a", "kurz.txt", "exactly-20-chars.abc"} {
		if got := truncateMiddle(s, 20); got != s {
			t.Errorf("truncateMiddle(%q, 20) = %q, want it untouched", s, got)
		}
	}
}

func TestTruncateMiddleIsRuneAware(t *testing.T) {
	// Every character is two bytes, so a byte-based split would land
	// mid-character and produce replacement characters.
	got := truncateMiddle("äöüäöüäöüäöüäöü", 7)

	if utf8.RuneCountInString(got) != 7 {
		t.Errorf("%q is %d runes, want 7", got, utf8.RuneCountInString(got))
	}
	if !utf8.ValidString(got) {
		t.Errorf("%q is not valid UTF-8 — a rune got split", got)
	}
}

func TestTruncateMiddleDegradesGracefullyAtTinyWidths(t *testing.T) {
	if got := truncateMiddle("something", 0); got != "" {
		t.Errorf("width 0 = %q, want empty", got)
	}
	if got := truncateMiddle("something", 1); got != string(ellipsis) {
		t.Errorf("width 1 = %q, want just the ellipsis", got)
	}
	if got := utf8.RuneCountInString(truncateMiddle("something", 2)); got != 2 {
		t.Errorf("width 2 produced %d runes, want 2", got)
	}
}

func TestShortenNameLabelKeepsTheSuffix(t *testing.T) {
	name, suffix := shortenNameLabel("a-very-long-directory-name-here", "/", 20)

	if suffix != "/" {
		t.Errorf("suffix = %q, want the trailing slash kept — it says what kind of entry this is", suffix)
	}
	if got := utf8.RuneCountInString(name + suffix); got != 20 {
		t.Errorf("%q+%q is %d columns, want 20", name, suffix, got)
	}
}

func TestShortenNameLabelDropsAnOversizedSuffix(t *testing.T) {
	// A symlink whose target is far longer than the space available:
	// keeping it whole would leave no name at all.
	name, suffix := shortenNameLabel("link", " -> /very/deep/path/to/somewhere/far/away", 16)

	if suffix != "" {
		t.Errorf("suffix = %q, want it dropped rather than crowding out the name", suffix)
	}
	if got := utf8.RuneCountInString(name); got > 16 {
		t.Errorf("%q is %d columns, want at most 16", name, got)
	}
}

func TestShortenNameLabelLeavesShortOnesAlone(t *testing.T) {
	name, suffix := shortenNameLabel("kurz.txt", "", 40)

	if name != "kurz.txt" || suffix != "" {
		t.Errorf("got %q+%q, want it untouched", name, suffix)
	}
}

// TestComputeColumnLayoutGivesDataColumnsWhatTheyNeed is the core
// promise: Size and Modified are sized from their content, and the name
// column absorbs the difference.
func TestComputeColumnLayoutGivesDataColumnsWhatTheyNeed(t *testing.T) {
	// Human-readable sizes ("878.9K" = 6) and formatted timestamps
	// ("2026-09-06 15:43:25" = 19).
	layout := computeColumnLayout(120, 6, 19, false, false, false)

	if layout.size != 6 {
		t.Errorf("size column = %d, want exactly what the data needs (6)", layout.size)
	}
	if layout.mod != 19 {
		t.Errorf("mod column = %d, want exactly what the data needs (19)", layout.mod)
	}
	if want := 120 - fixedColumnsWidth - 6 - 19; layout.name != want {
		t.Errorf("name column = %d, want the remainder %d", layout.name, want)
	}
}

// TestComputeColumnLayoutShrinksWithTheFormat pins the actual request:
// switching to Unix timestamps (10 columns) must hand the nine columns
// it no longer needs to the name, not keep them reserved.
func TestComputeColumnLayoutShrinksWithTheFormat(t *testing.T) {
	formatted := computeColumnLayout(120, 6, 19, false, false, false)
	timestamps := computeColumnLayout(120, 6, 10, false, false, false)

	if timestamps.mod >= formatted.mod {
		t.Errorf("timestamp mode kept %d columns, formatted used %d — it should need fewer", timestamps.mod, formatted.mod)
	}
	if timestamps.name <= formatted.name {
		t.Errorf("name column didn't grow: %d vs %d", timestamps.name, formatted.name)
	}
}

// TestComputeColumnLayoutModHeaderIsAlwaysMtime pins the label itself:
// short enough that it fits every format the column can be in, so it
// never costs the name column room.
func TestComputeColumnLayoutModHeaderIsAlwaysMtime(t *testing.T) {
	for _, widestMod := range []int{10, 19} { // timestamp, formatted
		layout := computeColumnLayout(120, 6, widestMod, false, false, false)
		if layout.modHeader != "mtime" {
			t.Errorf("data width %d: header = %q, want %q", widestMod, layout.modHeader, "mtime")
		}
		if layout.mod != widestMod {
			t.Errorf("data width %d: column = %d, want the data's own width — the label is never the constraint",
				widestMod, layout.mod)
		}
	}
}

// TestComputeColumnLayoutAbbreviatesTheHeaderRatherThanTheData pins that
// the variant mechanism still works where it is actually needed: the
// trash's own "Deletion time" is genuinely too long for a column of
// Unix timestamps, and it is the *label* that gives way, never the data.
func TestComputeColumnLayoutAbbreviatesTheHeaderRatherThanTheData(t *testing.T) {
	layout := computeColumnLayout(120, 6, 10, true, false, false)

	if layout.mod < 10 {
		t.Errorf("mod column = %d, want at least the 10 columns a timestamp needs", layout.mod)
	}
	if layout.modHeader == "Deletion time" {
		t.Error("the full label can't fit 10 columns — a shorter variant should have been picked")
	}
	if utf8.RuneCountInString(layout.modHeader) > layout.mod {
		t.Errorf("header %q (%d) doesn't fit its own column (%d)",
			layout.modHeader, utf8.RuneCountInString(layout.modHeader), layout.mod)
	}
}

// TestComputeColumnLayoutLeavesRoomForTheSortArrow guards an off-by-two
// that would bite exactly when a column is sorted, which is most of the
// time for one of them.
func TestComputeColumnLayoutLeavesRoomForTheSortArrow(t *testing.T) {
	unsorted := computeColumnLayout(120, 6, 10, false, false, false)
	sorted := computeColumnLayout(120, 6, 10, false, false, true)

	if utf8.RuneCountInString(sorted.modHeader)+2 > sorted.mod {
		t.Errorf("sorted header %q plus its arrow doesn't fit in %d columns", sorted.modHeader, sorted.mod)
	}
	_ = unsorted
}

// TestComputeColumnLayoutInTheTrashUsesTheDeletionLabel pins that
// browsing the trash keeps its own column name through the abbreviation
// logic rather than reverting to the modification-time label, which
// would be plainly wrong there.
func TestComputeColumnLayoutInTheTrashUsesTheDeletionLabel(t *testing.T) {
	wide := computeColumnLayout(120, 6, 19, true, false, false)
	if wide.modHeader != "Deletion time" {
		t.Errorf("header = %q, want the trash's own label", wide.modHeader)
	}

	narrow := computeColumnLayout(120, 6, 10, true, false, false)
	if strings.HasPrefix(narrow.modHeader, "Modif") {
		t.Errorf("header = %q, want a shortened *deletion* label, not a modification one", narrow.modHeader)
	}
}

// TestComputeColumnLayoutNeverStarvesTheNameColumn pins the floor: in a
// very narrow pane the name stops shrinking rather than going to zero.
func TestComputeColumnLayoutNeverStarvesTheNameColumn(t *testing.T) {
	layout := computeColumnLayout(30, 12, 19, false, false, false)

	if layout.name < nameColumnMinWidth {
		t.Errorf("name column = %d, want at least %d", layout.name, nameColumnMinWidth)
	}
}

func TestPadLeftRightAlignsAndNeverTruncates(t *testing.T) {
	if got := padLeft("878.9K", 10); got != "    878.9K" {
		t.Errorf("got %q, want it right-aligned in 10 columns", got)
	}
	// Wider than the column: kept whole, deliberately (see padLeft).
	if got := padLeft("2026-09-06 15:43:25", 10); got != "2026-09-06 15:43:25" {
		t.Errorf("got %q, want the value kept intact rather than cut", got)
	}
}
