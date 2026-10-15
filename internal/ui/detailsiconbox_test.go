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
	got := renderFileIconBox(bg, fg, "X", fileIconBoxCols)

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
	narrow := renderFileIconBox(bg, fg, "X", fileIconBoxCols)
	wide := renderFileIconBox(bg, fg, "X", fileIconBoxCols+10)

	if len(wide) <= len(narrow) {
		t.Errorf("a wider availableWidth should produce more leading padding, narrow=%d wide=%d bytes", len(narrow), len(wide))
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
