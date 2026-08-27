package ui

import (
	"image"
	"os/exec"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// renderImageHalfBlocks turns img (already scaled to fit within a
// boxCols×boxRows character grid — see viewer.ScaleForTerminal, which
// is what guarantees img's own bounds never exceed that box) into
// tview markup using the standard terminal half-block trick: "▀"
// (upper half block) painted with the pair's own top pixel as
// foreground and bottom pixel as background, so one character cell
// shows two vertical pixels at once. Two rows of img are consumed per
// line of output; an image with an odd pixel height repeats its own
// last row as both halves of the final cell row, rendering as one
// solid-colored row rather than an empty or garbage one.
//
// The image is centered within the full box — both horizontally (each
// line gets its own leading spaces) and vertically (blank lines above
// and below) — the same "don't leave it pinned to the top-left corner
// of a full-screen overlay" reasoning centeredMessage already applies
// to the tool-recommendation message; an image whose aspect ratio
// doesn't exactly match the box (the overwhelmingly common case) would
// otherwise always render hugging one edge instead of looking placed.
//
// No escaping needed here the way renderSyntax needs it for file
// content (see its own doc comment): every character this writes is
// either a color tag this function built itself, plain spaces, or the
// fixed glyph "▀" — never anything derived from the image's own bytes.
func renderImageHalfBlocks(img *image.RGBA, boxCols, boxRows int) string {
	bounds := img.Bounds()
	imgCols := bounds.Dx()
	imgRows := (bounds.Dy() + 1) / 2 // ceil: an odd pixel height still occupies one more cell row

	leftPad := (boxCols - imgCols) / 2
	if leftPad < 0 {
		leftPad = 0
	}
	topPad := (boxRows - imgRows) / 2
	if topPad < 0 {
		topPad = 0
	}
	bottomPad := boxRows - imgRows - topPad
	if bottomPad < 0 {
		bottomPad = 0
	}
	leftMargin := strings.Repeat(" ", leftPad)

	var b strings.Builder
	for i := 0; i < topPad; i++ {
		b.WriteByte('\n')
	}
	for y := bounds.Min.Y; y < bounds.Max.Y; y += 2 {
		b.WriteString(leftMargin)
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			top := img.RGBAAt(x, y)
			bottom := top
			if y+1 < bounds.Max.Y {
				bottom = img.RGBAAt(x, y+1)
			}
			b.WriteString("[")
			b.WriteString(colorTag(rgbColor(int32(top.R), int32(top.G), int32(top.B))))
			b.WriteString(":")
			b.WriteString(colorTag(rgbColor(int32(bottom.R), int32(bottom.G), int32(bottom.B))))
			b.WriteString("]▀")
		}
		b.WriteString("[-:-]\n")
	}
	for i := 0; i < bottomPad; i++ {
		b.WriteByte('\n')
	}
	return b.String()
}

// rgbColor is a thin, named wrapper around tcell.NewRGBColor — exists
// so renderImageHalfBlocks' own call reads as "a color from these
// r/g/b values" without repeating tcell's own name at every call site,
// and so tests can build the exact same color a rendered pixel should
// have without duplicating the conversion by hand.
func rgbColor(r, g, b int32) tcell.Color {
	return tcell.NewRGBColor(r, g, b)
}

// imageToolRecommendation is the message showUnsupportedLook shows for
// a file it's confident is an image Look's own built-in decoders just
// don't cover. chafa is recommended first: the best real-world balance
// of "already packaged on virtually every mainstream distro and
// Homebrew" and "wide format and terminal-protocol coverage" — see
// packageManagerInstallHint for how the actual install command is
// chosen. pixterm is offered second, specifically for anyone who'd
// rather not pull in a C dependency at all: a single static Go binary
// via `go install`, even though it isn't nearly as widely pre-packaged
// as chafa.
func imageToolRecommendation() string {
	return packageManagerInstallHint("chafa") + "\nor (Go-native, no system package):\ngo install github.com/eliukblau/pixterm@latest"
}

// packageManagerImageManagers is packageManagerInstallHint's own search
// order — apt/dnf/pacman/zypper/apk (Linux, roughly by real-world
// popularity), then brew (macOS, and Linuxbrew on Linux where none of
// the others are present).
var packageManagerImageManagers = []struct{ bin, cmd string }{
	{"apt", "sudo apt install "},
	{"dnf", "sudo dnf install "},
	{"pacman", "sudo pacman -S "},
	{"zypper", "sudo zypper install "},
	{"apk", "sudo apk add "},
	{"brew", "brew install "},
}

// packageManagerInstallHint returns a one-line "how to install pkg"
// command matched to whichever real package manager this system
// actually has — checked via exec.LookPath, the same "check, don't
// assume" pattern LocateAvailable/batBinary already use elsewhere in
// this app, rather than guessing from runtime.GOOS alone (a Linux box
// could be any of five different package managers). Falls back to a
// bare "install pkg" if none of them are found — an unusual system
// this app doesn't otherwise specially support, but still a usable
// hint rather than nothing at all.
func packageManagerInstallHint(pkg string) string {
	for _, m := range packageManagerImageManagers {
		if _, err := exec.LookPath(m.bin); err == nil {
			return m.cmd + pkg
		}
	}
	return "install " + pkg
}

// centeredMessage lays msg out inside a width×height box, as exactly
// height lines (padded with blank ones, both above and below, when msg
// itself has fewer — never fewer than msg's own line count when it has
// more): each of msg's own lines horizontally centered, the whole
// block vertically centered by splitting the padding evenly above and
// below it — showUnsupportedLook's own way of making a short
// recommendation read as deliberately placed in Look's full-screen
// overlay, rather than pinned to the top-left corner the way file
// content naturally starts. Escaped the same way renderSyntax escapes
// real file content (tview.Escape — see its own doc comment): msg is
// developer-authored constant text today, but pkg's own name flows
// into it via packageManagerInstallHint, so there's still real (if
// remote) content in the mix.
func centeredMessage(msg string, width, height int) string {
	contentLines := strings.Split(msg, "\n")
	topPad := (height - len(contentLines)) / 2
	if topPad < 0 {
		topPad = 0
	}
	bottomPad := height - len(contentLines) - topPad
	if bottomPad < 0 {
		bottomPad = 0
	}

	out := make([]string, 0, topPad+len(contentLines)+bottomPad)
	for i := 0; i < topPad; i++ {
		out = append(out, "")
	}
	for _, line := range contentLines {
		leftPad := (width - len([]rune(line))) / 2
		if leftPad < 0 {
			leftPad = 0
		}
		out = append(out, strings.Repeat(" ", leftPad)+tview.Escape(line))
	}
	for i := 0; i < bottomPad; i++ {
		out = append(out, "")
	}
	return strings.Join(out, "\n")
}
