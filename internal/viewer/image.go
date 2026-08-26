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
