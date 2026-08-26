package ui

import (
	"image"
	"image/color"
	"strings"
	"testing"

	"github.com/rivo/tview"
)

// TestRenderImageHalfBlocksPairsRowsIntoOneLine pins the core half-block
// mapping: a 1×2-pixel image (top red, bottom blue) becomes exactly one
// line, colored top=red/bottom=blue on the same "▀" cell.
func TestRenderImageHalfBlocksPairsRowsIntoOneLine(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 1, 2))
	img.Set(0, 0, color.RGBA{R: 255, A: 255})
	img.Set(0, 1, color.RGBA{B: 255, A: 255})

	got := renderImageHalfBlocks(img)

	lines := strings.Split(strings.TrimRight(got, "\n"), "\n")
	if len(lines) != 1 {
		t.Fatalf("got %d lines, want exactly 1 for a 1x2 source: %q", len(lines), got)
	}
	if !strings.Contains(lines[0], colorTag(rgbColor(255, 0, 0))) {
		t.Errorf("line doesn't mention the top pixel's own red: %q", lines[0])
	}
	if !strings.Contains(lines[0], colorTag(rgbColor(0, 0, 255))) {
		t.Errorf("line doesn't mention the bottom pixel's own blue: %q", lines[0])
	}
}

// TestRenderImageHalfBlocksHandlesOddHeight pins that an odd pixel
// height still produces one row per two pixels (rounded up) rather than
// silently dropping the last row.
func TestRenderImageHalfBlocksHandlesOddHeight(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 1, 3))
	got := renderImageHalfBlocks(img)

	lines := strings.Split(strings.TrimRight(got, "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("got %d lines, want 2 for a 1x3 source (ceil(3/2))", len(lines))
	}
}

// TestRenderImageHalfBlocksProducesValidTviewMarkup pins that the
// output actually parses as tview markup rather than, say, tripping
// over a raw "[" from a color tag gone wrong — rendered through a real
// TextView the same way TestRenderSyntaxRoundTripsThroughTviewParsing
// already does for syntax coloring.
func TestRenderImageHalfBlocksProducesValidTviewMarkup(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 3, 4))
	for y := 0; y < 4; y++ {
		for x := 0; x < 3; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x * 80), G: uint8(y * 60), B: 200, A: 255})
		}
	}

	v := tview.NewTextView().SetDynamicColors(true)
	v.SetText(renderImageHalfBlocks(img))
	// GetText(true) strips tags — if the markup were malformed this
	// wouldn't panic, but a stray unclosed tag would visibly eat into
	// what should have been the "▀" glyphs. Just confirming it runs at
	// all, without panicking, is most of what this test is for.
	if got := v.GetText(true); !strings.Contains(got, "▀") {
		t.Errorf("rendered text lost its own glyphs after tview tag parsing: %q", got)
	}
}

func TestPackageManagerInstallHintUsesWhicheverManagerIsOnPath(t *testing.T) {
	empty := t.TempDir()
	t.Setenv("PATH", empty)
	if got := packageManagerInstallHint("chafa"); got != "install chafa" {
		t.Errorf("packageManagerInstallHint() = %q, want the bare fallback %q when no manager is on PATH", got, "install chafa")
	}

	withApt := t.TempDir()
	writeFakeExecutable(t, withApt, "apt")
	t.Setenv("PATH", withApt)
	if got := packageManagerInstallHint("chafa"); got != "sudo apt install chafa" {
		t.Errorf("packageManagerInstallHint() = %q, want the apt command", got)
	}

	withBrewOnly := t.TempDir()
	writeFakeExecutable(t, withBrewOnly, "brew")
	t.Setenv("PATH", withBrewOnly)
	if got := packageManagerInstallHint("chafa"); got != "brew install chafa" {
		t.Errorf("packageManagerInstallHint() = %q, want the brew command", got)
	}
}

func TestCenteredMessageCentersHorizontallyAndVertically(t *testing.T) {
	got := centeredMessage("hi", 10, 5)
	lines := strings.Split(got, "\n")
	if len(lines) != 5 {
		t.Fatalf("got %d lines, want exactly height=5", len(lines))
	}
	// (5-1)/2 = 2 blank lines above a single-line message.
	for i := 0; i < 2; i++ {
		if lines[i] != "" {
			t.Errorf("line %d = %q, want blank (vertical centering pad)", i, lines[i])
		}
	}
	// (10-2)/2 = 4 leading spaces before "hi".
	if want := strings.Repeat(" ", 4) + "hi"; lines[2] != want {
		t.Errorf("centered line = %q, want %q", lines[2], want)
	}
}

func TestCenteredMessageEscapesContent(t *testing.T) {
	got := centeredMessage("literal [brackets] here", 40, 3)
	if !strings.Contains(got, tview.Escape("[brackets]")) {
		t.Errorf("literal brackets weren't escaped: %q", got)
	}
}
