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
//
// fileIconBoxFoldBackground is the file box's own folded-corner shade
// (see renderFileIconBox's own doc comment) — a fixed darker gray for
// the same "stays a paper color regardless of theme" reason, not
// derived from fileIconBoxBackground via a generic darken helper: the
// fold is meant to read as a distinctly different shade of the same
// paper, not a subtle tint, so a fixed, deliberately-picked value reads
// more reliably than an automatic formula would across terminals.
var (
	fileIconBoxBackground     = tcell.NewRGBColor(224, 224, 224)
	fileIconBoxForeground     = tcell.NewRGBColor(40, 40, 40)
	fileIconBoxFoldBackground = tcell.NewRGBColor(176, 176, 176)
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
// size — 15×9, per the user's own explicit, iteratively corrected spec
// (an initial 3×3 pass read as "much too small"; later widened 12→13→
// 15). Deliberately NOT detailsImageBoxSize's own real-preview footprint
// (innerHeight/3, which scales with the sidebar's own height): a fixed
// size keeps this decorative box's own footprint predictable and
// independent of terminal height — scaling it with the sidebar once
// already pushed the stat/hash section outside a small terminal's own
// visible viewport, a real regression two existing click-zone tests
// caught.
const (
	fileIconBoxCols = 15
	fileIconBoxRows = 9
)

// fileIconFoldRow is one row of the top-right fold's own shape (see
// fileIconFoldStaircase) — fullCols is how many columns from the right
// edge are entirely fold-colored that row, transition says whether the
// one column immediately left of that block gets a softening "▐"
// instead of a hard edge straight into the box's own background.
type fileIconFoldRow struct {
	fullCols   int
	transition bool
}

// fileIconFoldStaircase is the top-right fold's own shape, one entry
// per row starting at row 0 — each row one column narrower than the
// last, so the shape tapers down and to the left, toward the box's
// interior, the same direction the user's own "nach innen zeigen"
// (point inward) describes.
var fileIconFoldStaircase = []fileIconFoldRow{
	{4, false},
	{3, true},
	{2, true},
	{1, true},
	{0, true},
}

// folderTopRightTaperCols is how many columns, immediately left of the
// folder box's own top-right corner, taper into it via the same
// half-block glyph the other corners already use — per the user's own
// follow-up request specifically for the folder box (no fold, see
// renderFileIconBox's own doc comment): the corner cell itself goes
// fully to the surrounding background (no box color left in it at all,
// "komplett Hintergrundfarbe"), with this many cells immediately before
// it softening the change gradually rather than jumping straight from
// full box color to none in a single cell.
const folderTopRightTaperCols = 8

// renderFileIconBox draws a flat-colored, fileIconBoxCols×fileIconBoxRows
// placeholder — glyph centered in it, box itself centered horizontally
// within availableWidth (the sidebar's own inner width) — for every
// target that has no real thumbnail of its own (anything that isn't a
// successfully-decoded image or rasterized PDF page), per the user's
// own explicit request for "two half-block fake images, one for files,
// one for folders".
//
// decorateCorners (the file box's own look, not the folder box's — see
// renderDetailsSidebar's own call sites) folds the top-right corner and
// gently rounds the other three, per the user's own iterative
// corrections to the first attempt:
//
//   - The three rounded corners first used a quadrant-block glyph
//     (3/4 of the cell cut away) — reported as "zu stark" (too strong).
//     A plain half-block ("▄"/"▀", half the cell cut away) reads as a
//     softer, subtler rounding instead.
//   - The folded corner first used a single quadrant glyph, which read
//     as "misslungen" (botched) rather than a real fold. foldStaircase
//     instead carves out several rows/columns in fileIconBoxFoldBackground's
//     own distinct shade — a real, if coarse, triangle rather than one
//     cell — narrowing toward the box's interior (the "soll nach innen
//     zeigen" the user asked for) one row at a time, each row's own
//     straight edge softened by one "▐" (RIGHT HALF BLOCK) transition
//     cell so the staircase reads less like stair-steps and more like a
//     diagonal at this scale.
//
// A flat color needs none of renderImageHalfBlocks' own per-pixel
// half-block trick for the rest of the box (top and bottom halves
// would always be identical anyway, so "▀" would render pixel-
// identically to a plain space): a background-tagged run of spaces
// gets the exact same rendered result there, without building a
// synthetic image.Image just to feed it back into a renderer meant for
// real per-pixel data — only the corner/fold cells need an actual
// glyph at all.
func renderFileIconBox(bg, fg tcell.Color, glyph string, availableWidth int, roundCorners, foldTopRight bool) string {
	leftPad := (availableWidth - fileIconBoxCols) / 2
	if leftPad < 0 {
		leftPad = 0
	}
	margin := strings.Repeat(" ", leftPad)
	bgTag := colorTag(bg)
	fgTag := colorTag(fg)
	foldTag := colorTag(fileIconBoxFoldBackground)

	centerRow := fileIconBoxRows / 2
	centerCol := (fileIconBoxCols - 1) / 2
	lastRow := fileIconBoxRows - 1
	lastCol := fileIconBoxCols - 1

	foldStaircase := fileIconFoldStaircase

	var b strings.Builder
	for row := 0; row < fileIconBoxRows; row++ {
		b.WriteString(margin)
		for col := 0; col < fileIconBoxCols; col++ {
			var fold *fileIconFoldRow
			if foldTopRight && row < len(foldStaircase) {
				fold = &foldStaircase[row]
			}
			switch {
			case fold != nil && col > lastCol-fold.fullCols:
				fmt.Fprintf(&b, "[-:%s:] ", foldTag)
			case fold != nil && fold.transition && col == lastCol-fold.fullCols:
				fmt.Fprintf(&b, "[%s:%s:]▐", foldTag, bgTag)
			case roundCorners && row == 0 && col == 0:
				// Top-left: rounded.
				fmt.Fprintf(&b, "[%s:-:]▄", bgTag)
			case roundCorners && !foldTopRight && row == 0 && col == lastCol:
				// The corner cell itself: fully background, no box
				// color at all — "practically removed", not just
				// softened (see folderTopRightTaperCols's own doc
				// comment) — only for the folder box's own look (no
				// fold); the file box's folded corner is handled by the
				// staircase case above instead.
				fmt.Fprintf(&b, "[-:-:] ")
			case roundCorners && !foldTopRight && row == 0 && col >= lastCol-folderTopRightTaperCols && col < lastCol:
				// Several columns leading up to that corner, same
				// half-block rounding the other corners already use,
				// widening the cut gradually instead of jumping
				// straight from full box color to none in one cell.
				fmt.Fprintf(&b, "[%s:-:]▄", bgTag)
			case roundCorners && row == lastRow && col == 0:
				// Bottom-left: rounded.
				fmt.Fprintf(&b, "[%s:-:]▀", bgTag)
			case roundCorners && row == lastRow && col == lastCol:
				// Bottom-right: rounded.
				fmt.Fprintf(&b, "[%s:-:]▀", bgTag)
			case row == centerRow && col == centerCol:
				fmt.Fprintf(&b, "[%s:%s:]%s", fgTag, bgTag, glyph)
			default:
				fmt.Fprintf(&b, "[-:%s:] ", bgTag)
			}
		}
		b.WriteString("[-:-]")
		if row < fileIconBoxRows-1 {
			b.WriteByte('\n')
		}
	}
	return b.String()
}
