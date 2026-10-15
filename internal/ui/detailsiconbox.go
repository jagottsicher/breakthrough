package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/gdamore/tcell/v2"

	"github.com/jagottsicher/breakthrough/internal/fileicons"
	"github.com/jagottsicher/breakthrough/internal/fsops"
)

// fileIconBoxBackground/fileIconBoxForeground are the generic file
// icon box's own fixed "paper" colors — deliberately NOT theme tokens,
// per the user's own explicit request: this placeholder stays a light,
// neutral page color regardless of which color scheme is active,
// unlike the folder box (see renderDetailsSidebar's own call site),
// which reuses theme.DirectoryBackground/Text instead since that
// already has an existing, theme-aware meaning worth matching rather
// than duplicating.
var (
	fileIconBoxBackground = tcell.NewRGBColor(224, 224, 224)
	fileIconBoxForeground = tcell.NewRGBColor(40, 40, 40)
)

// detailsEntryType maps detailsStat (an fsops.Info, a single-path stat
// result) to the fsops.EntryType internal/fileicons.For and this
// package's own fallbackTypeText both key off — the same precedence
// classifyKind (properties.go) already uses for its own "Type" display
// string, just as an enum instead of prose.
func detailsEntryType(info fsops.Info) fsops.EntryType {
	switch {
	case info.IsSymlink && info.LinkBroken:
		return fsops.TypeSymlinkBroken
	case info.IsSymlink && info.LinkIsDir:
		return fsops.TypeSymlinkDir
	case info.IsSymlink:
		return fsops.TypeSymlinkFile
	case info.IsDir:
		return fsops.TypeDir
	case info.Mode&os.ModeSocket != 0:
		return fsops.TypeSocket
	case info.Mode&os.ModeNamedPipe != 0:
		return fsops.TypeFIFO
	case info.Mode&os.ModeDevice != 0 && info.Mode&os.ModeCharDevice != 0:
		return fsops.TypeCharDevice
	case info.Mode&os.ModeDevice != 0:
		return fsops.TypeBlockDevice
	default:
		return fsops.TypeFile
	}
}

// detailsTypeGlyph returns the single character the icon box shows for
// whatever Details currently targets — internal/fileicons' own Nerd
// Font glyph when icon mode ("zi") is on, the same plain-Unicode
// fallback symbol the main panel's own type column already shows
// otherwise (fallbackTypeText, panel.go) — never forcing one over the
// other: whichever the rest of the UI is already configured to show
// for this entry is what shows here too, per the user's own explicit
// "egal ob Glyphe oder Unicode, je nachdem was genutzt wird" request.
func (r *Root) detailsTypeGlyph() string {
	ref := rowRef{
		name:      filepath.Base(r.detailsTarget),
		entryType: detailsEntryType(r.detailsStat),
		mode:      r.detailsStat.Mode,
	}
	if r.panel.fileIcons {
		return string(fileicons.For(ref.entryType, ref.name, ref.mode))
	}
	return fallbackTypeText(ref)
}

// fileIconBoxCols/fileIconBoxRows are the generic icon box's own fixed
// size: exactly one cell of margin around the glyph in every direction
// — "jeweils 1 Zeichen Margin", per the user's own explicit spec — and
// nothing more. Deliberately NOT detailsImageBoxSize's own much larger
// real-preview footprint (innerHeight/3): that budget exists for an
// actual thumbnail worth that much space, and reusing it here for a
// single decorative character pushed the stat/hash section far enough
// down to fall outside the sidebar's own visible viewport in a real,
// observed regression (two existing click-zone tests started failing
// against a real terminal height, not just a stale assumption) — an
// icon is small by nature, so this box stays small by construction
// regardless of how tall the sidebar itself is.
const (
	fileIconBoxCols = 3
	fileIconBoxRows = 3
)

// renderFileIconBox draws a flat-colored, fileIconBoxCols×fileIconBoxRows
// placeholder — glyph centered, with the one-cell margin
// fileIconBoxCols/fileIconBoxRows's own doc comment describes — centered
// horizontally within availableWidth (the sidebar's own inner width),
// for every target that has no real thumbnail of its own (anything
// that isn't a successfully-decoded image or rasterized PDF page), per
// the user's own explicit request for "two half-block fake images, one
// for files, one for folders".
//
// A flat color needs none of renderImageHalfBlocks' own per-pixel
// half-block trick (top and bottom halves would always be identical
// anyway, so "▀" would render pixel-identically to a plain space): a
// background-tagged run of spaces gets the exact same rendered result,
// without building a synthetic image.Image just to feed it back into a
// renderer meant for real per-pixel data.
func renderFileIconBox(bg, fg tcell.Color, glyph string, availableWidth int) string {
	leftPad := (availableWidth - fileIconBoxCols) / 2
	if leftPad < 0 {
		leftPad = 0
	}
	margin := strings.Repeat(" ", leftPad)
	bgTag := colorTag(bg)
	blankRow := margin + fmt.Sprintf("[-:%s:]%s[-:-]", bgTag, strings.Repeat(" ", fileIconBoxCols))
	glyphRow := margin + fmt.Sprintf("[-:%s:] [%s:%s:]%s[-:%s:] [-:-]", bgTag, colorTag(fg), bgTag, glyph, bgTag)

	centerRow := fileIconBoxRows / 2
	var b strings.Builder
	for row := 0; row < fileIconBoxRows; row++ {
		if row == centerRow {
			b.WriteString(glyphRow)
		} else {
			b.WriteString(blankRow)
		}
		if row < fileIconBoxRows-1 {
			b.WriteByte('\n')
		}
	}
	return b.String()
}
