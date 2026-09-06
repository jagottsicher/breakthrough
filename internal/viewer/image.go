// Package viewer's own image support: format registration, decoding,
// and scaling a decoded image down (or up) to fit a terminal's own
// character grid — see Load for how a file actually becomes a
// Result{Kind: KindImage}, and internal/ui's own image.go for how the
// scaled pixels become an actual half-block rendering. This file stays
// deliberately free of any terminal/tview concept — same reason
// highlight.go stays free of them for syntax coloring (see its own doc
// comment): "decode and scale pixels" and "turn pixels into terminal
// markup" are different concerns, testable independently.
package viewer

import (
	"bytes"
	"image"
	_ "image/gif"  // side-effect only: registers itself with image.Decode/DecodeConfig
	_ "image/jpeg" // same
	_ "image/png"  // same

	_ "golang.org/x/image/bmp" // same — verified via its own init(), not guessed
	"golang.org/x/image/draw"
	_ "golang.org/x/image/tiff" // same
	"golang.org/x/image/webp"
)

func init() {
	// Unlike image/gif, image/jpeg, image/png, and x/image's own bmp
	// and tiff (all verified by reading their own init() funcs, not
	// guessed), x/image's webp package does not register itself with
	// image.Decode/DecodeConfig at all — done here by hand instead, the
	// documented shape of image.RegisterFormat, so Sniff/Load recognize
	// WebP exactly the same way as every other format this package
	// supports.
	image.RegisterFormat("webp", "RIFF????WEBP", webp.Decode, webp.DecodeConfig)
}

// ImagePreviewLimit is how much of an image file Load will actually
// read before attempting to decode it — separate from, and larger
// than, DefaultPreviewLimit: that number was chosen for *text* (a
// generous cap that still keeps a multi-gigabyte log from loading
// whole), not for a real photo, which can easily run past 8 MiB on a
// modern phone camera. Chosen generous enough for a real photo, still
// bounded against a multi-gigabyte file that merely happens to start
// with bytes that pass an image format's own header check.
const ImagePreviewLimit = 32 << 20 // 32 MiB

// ImagePixelBudget caps how many pixels an image may have before this
// package will decode it at all.
//
// The byte limit above bounds what is *read*; it does nothing about what
// that expands to. A 32 MiB JPEG is routinely a hundred megapixels, and
// decoding one costs four bytes of RGBA per pixel — measured here at 1.8
// seconds and 186 MiB of heap for a 108 MP file, all of it wasted, since
// what it feeds is a preview a few hundred characters across.
//
// 50 megapixels leaves every ordinary photograph well inside the budget
// (a 24 MP phone picture measured at 426 ms) while refusing the sizes
// that are either a scanning artefact or a deliberate decompression
// bomb. Checked from the header via DecodeConfig, which reads a handful
// of bytes rather than the whole picture, so the refusal itself is free.
const ImagePixelBudget = 50 << 20 // 50 megapixels

// ImageTooLarge reports whether data's own header declares more pixels
// than ImagePixelBudget allows, and what it declared.
//
// Returns false for anything whose header can't be read at all: that is
// not this function's decision to make, and DecodeImage will produce a
// proper error for it a moment later.
func ImageTooLarge(data []byte) (tooLarge bool, pixels int) {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return false, 0
	}
	// int64 throughout: on a 32-bit build the multiplication of two
	// plausible dimensions overflows a plain int, which would turn a
	// refusal into an acceptance — the exact case this guards.
	total := int64(cfg.Width) * int64(cfg.Height)
	return total > int64(ImagePixelBudget), int(min(total, int64(^uint(0)>>1)))
}

// DecodeImage decodes data (expected to be a complete image file, or at
// least as much of one as ImagePreviewLimit allowed reading — see
// Load) via the standard image.Decode dispatcher, across every format
// registered above. format is the registered name ("png", "jpeg",
// "gif", "bmp", "tiff", "webp") — Result.ImageFormat's own value,
// mainly useful for a status line or an error message rather than
// anything this package itself branches on.
func DecodeImage(data []byte) (img image.Image, format string, err error) {
	return image.Decode(bytes.NewReader(data))
}

// ScaleForTerminal resizes img to fit within a cols×rows terminal
// character grid, returned as a ready-to-render *image.RGBA sized
// exactly cols×(rows*2) pixels — the *2 is the half-block trick
// internal/ui's own renderer relies on (see its own doc comment): one
// character cell shows two vertical pixels at once, its own foreground
// color painting the top one, its background the bottom, via "▀". A
// terminal character cell's own real aspect ratio (glyphs are roughly
// twice as tall as they are wide) is what makes packing two pixel rows
// into one cell come out close to square, without this needing its own
// separate correction factor.
//
// The image is scaled to fit *within* that box, aspect ratio preserved
// — never stretched or cropped — via CatmullRom, x/image's own
// highest-quality (if slowest) interpolator; a single preview-sized
// image is nowhere near large enough for that cost to matter. Upscaling
// a small image to fill more of the available space is allowed, not
// just downscaling: a tiny icon shown at its own native size in a
// full-screen overlay would otherwise be barely visible, and "make the
// image visible" is the entire point of Look's own image support.
//
// cols/rows below 1 are treated as 1 — GetRect/viewerSize should never
// actually produce that, but a hostile or degenerate caller still gets
// back a real (1×2-pixel) image rather than a division by zero.
func ScaleForTerminal(img image.Image, cols, rows int) *image.RGBA {
	if cols < 1 {
		cols = 1
	}
	if rows < 1 {
		rows = 1
	}
	boxW, boxH := cols, rows*2

	srcBounds := img.Bounds()
	srcW, srcH := srcBounds.Dx(), srcBounds.Dy()
	if srcW < 1 {
		srcW = 1
	}
	if srcH < 1 {
		srcH = 1
	}

	scale := float64(boxW) / float64(srcW)
	if hScale := float64(boxH) / float64(srcH); hScale < scale {
		scale = hScale
	}

	dstW := int(float64(srcW) * scale)
	if dstW < 1 {
		dstW = 1
	}
	dstH := int(float64(srcH) * scale)
	if dstH < 1 {
		dstH = 1
	}

	dst := image.NewRGBA(image.Rect(0, 0, dstW, dstH))
	draw.CatmullRom.Scale(dst, dst.Bounds(), img, srcBounds, draw.Over, nil)
	return dst
}
