package viewer

import (
	"bytes"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/image/bmp"
	"golang.org/x/image/tiff"
)

// testImage builds a small, real image.Image — a solid-ish gradient
// rather than one flat color, so a lossy encoder (JPEG) still has
// something non-trivial to round-trip.
func testImage(w, h int) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.NRGBA{R: uint8(x * 255 / max(w-1, 1)), G: uint8(y * 255 / max(h-1, 1)), B: 128, A: 255})
		}
	}
	return img
}

// TestImageFormatsRoundTrip pins that every format this package
// registers (see image.go's own init and blank imports) is actually
// reachable through the shared image.Decode/DecodeConfig dispatcher —
// not just imported, but really wired up. WebP has no encoder in
// x/image (decode-only package — verified by reading its own source),
// so it's excluded here; its registration is verified by inspection
// instead (see image.go's own init doc comment) the same way bmp/tiff's
// own self-registration was verified by reading their init() funcs
// rather than guessed.
func TestImageFormatsRoundTrip(t *testing.T) {
	src := testImage(12, 8)

	cases := []struct {
		format string
		encode func(w *bytes.Buffer) error
	}{
		{"png", func(w *bytes.Buffer) error { return png.Encode(w, src) }},
		{"jpeg", func(w *bytes.Buffer) error { return jpeg.Encode(w, src, nil) }},
		{"gif", func(w *bytes.Buffer) error { return gif.Encode(w, src, nil) }},
		{"bmp", func(w *bytes.Buffer) error { return bmp.Encode(w, src) }},
		{"tiff", func(w *bytes.Buffer) error { return tiff.Encode(w, src, nil) }},
	}
	for _, c := range cases {
		t.Run(c.format, func(t *testing.T) {
			var buf bytes.Buffer
			if err := c.encode(&buf); err != nil {
				t.Fatalf("setup: encoding %s: %v", c.format, err)
			}
			data := buf.Bytes()

			if kind := Sniff(data); kind != KindImage {
				t.Errorf("Sniff(%s) = %v, want KindImage", c.format, kind)
			}

			img, format, err := DecodeImage(data)
			if err != nil {
				t.Fatalf("DecodeImage(%s): %v", c.format, err)
			}
			if format != c.format {
				t.Errorf("DecodeImage reported format %q, want %q", format, c.format)
			}
			if got := img.Bounds(); got.Dx() != 12 || got.Dy() != 8 {
				t.Errorf("decoded bounds = %v, want 12x8", got)
			}
		})
	}
}

func TestLoadDecodesRealImageFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "photo.png")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(f, testImage(20, 10)); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	result, err := Load(path, DefaultPreviewLimit)
	if err != nil {
		t.Fatal(err)
	}
	if result.Kind != KindImage {
		t.Fatalf("Kind = %v, want KindImage", result.Kind)
	}
	if result.Image == nil {
		t.Fatal("Image is nil")
	}
	if result.ImageFormat != "png" {
		t.Errorf("ImageFormat = %q, want %q", result.ImageFormat, "png")
	}
	if got := result.Image.Bounds(); got.Dx() != 20 || got.Dy() != 10 {
		t.Errorf("decoded bounds = %v, want 20x10", got)
	}
	if result.Content != "" || result.Truncated {
		t.Error("Content/Truncated should stay at their zero values for KindImage")
	}
}

// TestLoadDecodesJPEGWithLargeMetadataBeforeSOF is a regression test for
// a real, user-reported bug: a JPEG with a sizeable embedded metadata
// block (EXIF with a thumbnail, XMP, an ICC profile, ...) can legitimately
// put its own SOF marker — the thing image.DecodeConfig actually needs
// to reach — tens of kilobytes into the file, well past what sniffLen
// used to cover (8000 bytes; see its own doc comment for the concrete
// file that first exposed this). Sniff misclassified such a file as
// KindUnsupported despite it being perfectly decodable, so neither Look
// nor the Details sidebar showed anything for it at all — no image, no
// error either.
//
// Builds a real, valid JPEG (jpeg.Encode), then hand-inserts an oversized
// APP1 marker segment (the same marker family EXIF actually uses) right
// after the SOI, pushing everything else — SOF marker included — well
// past both the old and the new sniffLen, to prove this isn't just
// papering over the one specific file that was first reported: 20000
// bytes of filler comfortably exceeds the old 8000-byte window while
// still fitting the new 64 KiB one with room to spare.
func TestLoadDecodesJPEGWithLargeMetadataBeforeSOF(t *testing.T) {
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, testImage(10, 10), nil); err != nil {
		t.Fatal(err)
	}
	data := buf.Bytes()
	if len(data) < 4 || data[0] != 0xFF || data[1] != 0xD8 {
		t.Fatalf("encoded JPEG doesn't start with a plain SOI marker: %x", data[:4])
	}

	filler := make([]byte, 20000)
	segLen := len(filler) + 2 // JPEG's own marker-length field counts itself
	segment := append([]byte{0xFF, 0xE1, byte(segLen >> 8), byte(segLen)}, filler...)
	injected := append(append(append([]byte{}, data[:2]...), segment...), data[2:]...)
	const oldSniffLen = 8000 // sniffLen's own value before this fix — see its own doc comment
	if len(injected) <= oldSniffLen {
		t.Fatalf("setup: injected file (%d bytes) should exceed the old sniffLen (%d), or this wouldn't actually be exercising the bug this test guards against", len(injected), oldSniffLen)
	}
	if len(injected) > sniffLen {
		t.Fatalf("setup: injected file (%d bytes) should still fit within the current sniffLen (%d), or this test wouldn't prove the fix actually works", len(injected), sniffLen)
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "photo.jpg")
	if err := os.WriteFile(path, injected, 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := Load(path, DefaultPreviewLimit)
	if err != nil {
		t.Fatal(err)
	}
	if result.Kind != KindImage {
		t.Fatalf("Kind = %v, want KindImage — a decodable JPEG shouldn't be misclassified just because its own SOF marker sits deep past a large metadata block", result.Kind)
	}
	if got := result.Image.Bounds(); got.Dx() != 10 || got.Dy() != 10 {
		t.Errorf("decoded bounds = %v, want 10x10", got)
	}
}

// TestLoadReportsTooLargeImageAsUnsupportedWithReason pins the one real
// failure mode Load itself can produce for image-shaped content: a file
// whose header passes Sniff but whose full data runs past
// ImagePreviewLimit before Load ever reads far enough to finish
// decoding it. NoCompression keeps the encoded size == raw pixel size,
// exactly predictable, and the encode itself fast (a compressing
// encoder over this much data would make the test slow for no benefit
// here).
func TestLoadReportsTooLargeImageAsUnsupportedWithReason(t *testing.T) {
	// 4000x4000, uncompressed PNG: this test's own testImage has A==255
	// everywhere, which Go's png encoder detects and drops to 3
	// bytes/pixel (no alpha channel needed) rather than NRGBA's own 4 —
	// side chosen with enough margin over ImagePreviewLimit (32 MiB) to
	// comfortably clear that either way (4000² × 3 ≈ 45.8 MiB).
	const side = 4000
	dir := t.TempDir()
	path := filepath.Join(dir, "huge.png")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	enc := png.Encoder{CompressionLevel: png.NoCompression}
	if err := enc.Encode(f, testImage(side, side)); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() <= ImagePreviewLimit {
		t.Fatalf("setup: fixture is only %d bytes, want more than ImagePreviewLimit (%d)", info.Size(), int64(ImagePreviewLimit))
	}

	result, err := Load(path, DefaultPreviewLimit)
	if err != nil {
		t.Fatal(err)
	}
	if result.Kind != KindUnsupported {
		t.Fatalf("Kind = %v, want KindUnsupported", result.Kind)
	}
	if result.Reason == "" {
		t.Error("Reason is empty, want an explanation naming the file as too large")
	}
}

func TestScaleForTerminalFitsWithinBoxPreservingAspectRatio(t *testing.T) {
	// A 200x100 (2:1) source into a 40x15-cell box (80x30 pixels, also
	// 2.67:1) — width is the tighter constraint (80/200 = 0.4 vs.
	// 30/100 = 0.3, so height is actually tighter here); either way the
	// result must still be wholly within the box and keep proportion.
	src := testImage(200, 100)
	dst := ScaleForTerminal(src, 40, 15)

	b := dst.Bounds()
	if b.Dx() > 40 || b.Dy() > 30 {
		t.Fatalf("scaled bounds %v exceed the 40x30-pixel box", b)
	}
	gotRatio := float64(b.Dx()) / float64(b.Dy())
	wantRatio := 200.0 / 100.0
	if diff := gotRatio - wantRatio; diff > 0.05 || diff < -0.05 {
		t.Errorf("aspect ratio = %.3f, want approximately %.3f (source's own)", gotRatio, wantRatio)
	}
}

func TestScaleForTerminalUpscalesSmallImages(t *testing.T) {
	src := testImage(4, 4)
	dst := ScaleForTerminal(src, 40, 20)

	b := dst.Bounds()
	if b.Dx() <= 4 && b.Dy() <= 4 {
		t.Errorf("scaled bounds %v were not enlarged for a tiny source image into a large box", b)
	}
}

func TestScaleForTerminalGuardsDegenerateInputs(t *testing.T) {
	src := testImage(10, 10)
	for _, tc := range []struct{ cols, rows int }{
		{0, 5}, {5, 0}, {-3, -3},
	} {
		dst := ScaleForTerminal(src, tc.cols, tc.rows)
		b := dst.Bounds()
		if b.Dx() < 1 || b.Dy() < 1 {
			t.Errorf("ScaleForTerminal(cols=%d, rows=%d) produced a degenerate %v image", tc.cols, tc.rows, b)
		}
	}
}
