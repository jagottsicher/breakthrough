package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/jagottsicher/breakthrough/internal/fsops"
)

func TestRenderFileIconBoxHasOneCellMarginOnEverySide(t *testing.T) {
	bg := tcell.NewRGBColor(224, 224, 224)
	fg := tcell.NewRGBColor(40, 40, 40)
	got := renderFileIconBox(bg, fg, "X", fileIconBoxCols, false, false)

	lines := strings.Split(got, "\n")
	if len(lines) != fileIconBoxRows {
		t.Fatalf("renderFileIconBox produced %d lines, want %d", len(lines), fileIconBoxRows)
	}
	centerRow := fileIconBoxRows / 2
	for i, line := range lines {
		if i == centerRow {
			if !strings.Contains(line, "X") {
				t.Errorf("center row %q should contain the glyph", line)
			}
			continue
		}
		if strings.Contains(line, "X") {
			t.Errorf("non-center row %q should not contain the glyph", line)
		}
	}
}

func TestRenderFileIconBoxCentersWithinAvailableWidth(t *testing.T) {
	bg := tcell.NewRGBColor(224, 224, 224)
	fg := tcell.NewRGBColor(40, 40, 40)
	narrow := renderFileIconBox(bg, fg, "X", fileIconBoxCols, false, false)
	wide := renderFileIconBox(bg, fg, "X", fileIconBoxCols+10, false, false)

	if len(wide) <= len(narrow) {
		t.Errorf("a wider availableWidth should produce more leading padding, narrow=%d wide=%d bytes", len(narrow), len(wide))
	}
}

func TestRenderFileIconBoxDecoratesCornersWhenRequested(t *testing.T) {
	bg := tcell.NewRGBColor(224, 224, 224)
	fg := tcell.NewRGBColor(40, 40, 40)
	got := renderFileIconBox(bg, fg, "X", fileIconBoxCols, true, true)
	lines := strings.Split(got, "\n")
	if len(lines) != fileIconBoxRows {
		t.Fatalf("renderFileIconBox produced %d lines, want %d", len(lines), fileIconBoxRows)
	}

	// Top-right: the fold, confined to the top fileIconFoldRows rows
	// (3) — each has a solid light-gray band (fileIconBoxFoldBackground)
	// that grows by one column per row, rows 0 and 2 (not 1) also get
	// one "▄" transition cell (bottom light gray, top background) right
	// after that band, per the user's own explicit "Zeile 1 und 3"
	// (1-indexed) spec.
	foldTag := colorTag(fileIconBoxFoldBackground)
	for row := 0; row < fileIconFoldRows; row++ {
		if !strings.Contains(lines[row], foldTag) {
			t.Errorf("row %d should reference the fold shade %s, got %q", row, foldTag, lines[row])
		}
	}
	// Row 0 has two ▄ glyphs: the top-left rounded corner and the
	// fold's own transition cell.
	if got := strings.Count(lines[0], "▄"); got != 2 {
		t.Errorf("row 0 should contain 2 ▄ glyphs (top-left corner + fold transition), got %d in %q", got, lines[0])
	}
	if strings.Contains(lines[1], "▄") {
		t.Errorf("row 1 should have a hard edge (no ▄ transition), got %q", lines[1])
	}
	last := lines[len(lines)-1]
	if strings.Contains(last, foldTag) {
		t.Errorf("bottom row should not reference the fold shade %s, the fold should be confined to the top %d rows, got %q", foldTag, fileIconFoldRows, last)
	}
	if strings.Contains(lines[fileIconFoldRows], foldTag) {
		t.Errorf("row %d (just past the fold) should not reference the fold shade %s, got %q", fileIconFoldRows, foldTag, lines[fileIconFoldRows])
	}

	// The other three corners: rounded, via a half-block glyph each —
	// deliberately NOT a quadrant glyph (see renderFileIconBox's own
	// doc comment on why that read as "zu stark").
	if !strings.Contains(lines[0], "▄") {
		t.Errorf("top row should contain the top-left rounded-corner glyph ▄, got %q", lines[0])
	}
	if !strings.Contains(last, "▀") {
		t.Errorf("bottom row should contain the rounded-corner glyph ▀ (both bottom corners use it), got %q", last)
	}
}

func TestRenderFileIconBoxSkipsCornerDecorationWhenNotRequested(t *testing.T) {
	bg := tcell.NewRGBColor(224, 224, 224)
	fg := tcell.NewRGBColor(40, 40, 40)
	got := renderFileIconBox(bg, fg, "X", fileIconBoxCols, false, false)

	for _, glyph := range []string{"▄", "▀", "▐"} {
		if strings.Contains(got, glyph) {
			t.Errorf("roundCorners=false, foldTopRight=false should produce a plain rectangle, found corner/fold glyph %q in:\n%s", glyph, got)
		}
	}
	if strings.Contains(got, colorTag(fileIconBoxFoldBackground)) {
		t.Errorf("foldTopRight=false should never reference the fold shade, got:\n%s", got)
	}
}

// TestRenderFileIconBoxRoundsAllFourCornersWithoutFold pins the
// folder box's own look, per the user's own follow-up request: "die 4
// Eckzeichen durch Halbblöcke ersetzt werden" (all four corners, not
// just three) — roundCorners alone, no foldTopRight, rounds the
// top-right corner the same simple way as the other three instead of
// folding it.
func TestRenderFileIconBoxRoundsAllFourCornersWithoutFold(t *testing.T) {
	bg := tcell.NewRGBColor(224, 224, 224)
	fg := tcell.NewRGBColor(40, 40, 40)
	got := renderFileIconBox(bg, fg, "X", fileIconBoxCols, true, false)
	lines := strings.Split(got, "\n")
	if len(lines) != fileIconBoxRows {
		t.Fatalf("renderFileIconBox produced %d lines, want %d", len(lines), fileIconBoxRows)
	}

	top := lines[0]
	bottom := lines[len(lines)-1]
	// Top-left corner (1) plus the top-right taper (folderTopRightTaperCols)
	// leading up to the fully-removed top-right corner cell itself.
	wantTopGlyphs := 1 + folderTopRightTaperCols
	if got := strings.Count(top, "▄"); got != wantTopGlyphs {
		t.Errorf("top row should contain %d ▄ glyphs (top-left + the top-right taper), got %d in %q", wantTopGlyphs, got, top)
	}
	if !strings.Contains(top, "[-:-:] ") {
		t.Errorf("top row's own rightmost cell should be fully background (\"[-:-:] \"), got %q", top)
	}
	if strings.Count(bottom, "▀") != 2 {
		t.Errorf("bottom row should contain exactly 2 rounded-corner glyphs ▀ (bottom-left and bottom-right), got %q", bottom)
	}
	if strings.Contains(got, colorTag(fileIconBoxFoldBackground)) {
		t.Errorf("foldTopRight=false should never reference the fold shade, even with roundCorners=true, got:\n%s", got)
	}
}

func TestDetailsEntryTypeMirrorsClassifyKindPrecedence(t *testing.T) {
	cases := []struct {
		name string
		info fsops.Info
		want fsops.EntryType
	}{
		{"plain file", fsops.Info{}, fsops.TypeFile},
		{"directory", fsops.Info{IsDir: true}, fsops.TypeDir},
		{"broken symlink wins over dir", fsops.Info{IsSymlink: true, LinkBroken: true, IsDir: true}, fsops.TypeSymlinkBroken},
		{"symlink to dir", fsops.Info{IsSymlink: true, LinkIsDir: true}, fsops.TypeSymlinkDir},
		{"symlink to file", fsops.Info{IsSymlink: true}, fsops.TypeSymlinkFile},
		{"socket", fsops.Info{Mode: os.ModeSocket}, fsops.TypeSocket},
		{"fifo", fsops.Info{Mode: os.ModeNamedPipe}, fsops.TypeFIFO},
		{"char device", fsops.Info{Mode: os.ModeDevice | os.ModeCharDevice}, fsops.TypeCharDevice},
		{"block device", fsops.Info{Mode: os.ModeDevice}, fsops.TypeBlockDevice},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := detailsEntryType(c.info); got != c.want {
				t.Errorf("detailsEntryType(%+v) = %v, want %v", c.info, got, c.want)
			}
		})
	}
}

func TestDetailsSidebarShowsFileIconBoxForPlainFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(path, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}

	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.SetRect(0, 0, 100, 40)
	r.panel.focusRow(1)
	r.showDetailsSidebar()
	r.loadDetailsTarget(path)

	text := r.detailsSidebar.GetText(true)
	if !strings.Contains(text, "≡") {
		t.Errorf("details sidebar text should show the plain-text fallback glyph (≡), got:\n%s", text)
	}
}

func TestDetailsSidebarShowsFolderIconBoxForDirectory(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "subdir"), 0o755); err != nil {
		t.Fatal(err)
	}

	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.SetRect(0, 0, 100, 40)
	r.panel.focusRow(1)
	r.showDetailsSidebar()
	r.loadDetailsTarget(filepath.Join(dir, "subdir"))

	text := r.detailsSidebar.GetText(true)
	if !strings.Contains(text, "/") {
		t.Errorf("details sidebar text should show the folder fallback glyph (/), got:\n%s", text)
	}
}

func TestDetailsTypeGlyphUsesNerdFontIconWhenEnabled(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(path, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}

	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.SetRect(0, 0, 100, 40)
	r.panel.focusRow(1)
	r.loadDetailsTarget(path)

	r.panel.fileIcons = false
	fallback := r.detailsTypeGlyph()
	r.panel.fileIcons = true
	icon := r.detailsTypeGlyph()

	if fallback == icon {
		t.Errorf("detailsTypeGlyph() should differ between icon mode on/off for a plain text file, got the same glyph %q both times", fallback)
	}
	if fallback != "≡" {
		t.Errorf("fallback glyph = %q, want the plain-text fallback (≡)", fallback)
	}
}
