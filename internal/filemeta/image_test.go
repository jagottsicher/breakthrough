package filemeta

import (
	"bytes"
	"errors"
	"image"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/evanoberholster/imagemeta/meta"
	"github.com/evanoberholster/imagemeta/meta/exif"
)

func TestFieldsFromExifIncludesEverySetField(t *testing.T) {
	var x exif.Exif
	x.IFD0.Make = "ACME"
	x.IFD0.Model = "X100"
	x.ExifIFD.ExposureTime = meta.ExposureTime(1.0 / 250.0)
	x.ExifIFD.FNumber = meta.NewAperture(28, 10)
	x.ExifIFD.ISOSpeedRatings = 400
	x.ExifIFD.FocalLength = meta.NewFocalLength(50, 1)
	x.ExifIFD.PixelXDimension = 4000
	x.ExifIFD.PixelYDimension = 3000

	got := fieldsFromExif(x)

	want := []Field{
		{Label: "Camera", Value: "ACME X100"},
		{Label: "Exposure", Value: "1/250s"},
		{Label: "Aperture", Value: "f/2.80"},
		{Label: "ISO", Value: "400"},
		{Label: "Focal length", Value: "50.00mm"},
		{Label: "Dimensions", Value: "4000 × 3000 px"},
	}
	if len(got) != len(want) {
		t.Fatalf("fieldsFromExif() = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("field %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestFieldsFromExifOmitsUnsetFields(t *testing.T) {
	var x exif.Exif
	x.IFD0.Make = "ACME"
	// Everything else left at its zero value.

	got := fieldsFromExif(x)
	want := []Field{{Label: "Camera", Value: "ACME"}}
	if len(got) != 1 || got[0] != want[0] {
		t.Errorf("fieldsFromExif() = %+v, want %+v", got, want)
	}
}

func TestFieldsFromExifZeroValueIsEmpty(t *testing.T) {
	if got := fieldsFromExif(exif.Exif{}); len(got) != 0 {
		t.Errorf("fieldsFromExif(zero value) = %+v, want empty", got)
	}
}

func TestFieldsFromExifFallsBackToIFD0Dimensions(t *testing.T) {
	var x exif.Exif
	x.IFD0.ImageWidth = 640
	x.IFD0.ImageHeight = 480
	// ExifIFD's own PixelXDimension/PixelYDimension left unset.

	got := fieldsFromExif(x)
	want := Field{Label: "Dimensions", Value: "640 × 480 px"}
	if len(got) != 1 || got[0] != want {
		t.Errorf("fieldsFromExif() = %+v, want [%+v]", got, want)
	}
}

func TestForImageMissingFile(t *testing.T) {
	_, err := ForImage(filepath.Join(t.TempDir(), "does-not-exist.jpg"))
	if err == nil {
		t.Fatal("ForImage() on a missing file: want an error, got nil")
	}
}

func TestForImageUnsupportedFormatReturnsErrNotSupported(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "not-an-image.txt")
	if err := os.WriteFile(path, []byte("plain text, not an image at all"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := ForImage(path)
	if !errors.Is(err, ErrNotSupported) {
		t.Errorf("ForImage() error = %v, want ErrNotSupported", err)
	}
}

func TestForImagePNGWithoutExifReturnsErrNoMetadata(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "plain.png")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(f, image.NewNRGBA(image.Rect(0, 0, 4, 4))); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	_, err = ForImage(path)
	if !errors.Is(err, ErrNoMetadata) {
		t.Errorf("ForImage() error = %v, want ErrNoMetadata", err)
	}
}

// buildExifJPEG returns a real, decodable JPEG (via image/jpeg's own
// encoder) with a hand-built minimal EXIF APP1 segment spliced in right
// after the SOI marker — Make/Model only, both short enough to fit
// inline in a TIFF IFD entry's own 4-byte value field (no external
// offset bookkeeping needed, see the IFD0 entry layout below). Built at
// test time rather than checked in as a binary fixture, the same
// "generate it, don't ship it" approach this package's own sibling
// tests (internal/viewer, internal/ui) already use for synthetic
// images.
func buildExifJPEG(t *testing.T, make_, model string) []byte {
	t.Helper()
	if len(make_)+1 > 4 || len(model)+1 > 4 {
		t.Fatalf("buildExifJPEG: make/model must each fit inline (<=3 chars): %q %q", make_, model)
	}

	var raw bytes.Buffer
	if err := jpeg.Encode(&raw, image.NewNRGBA(image.Rect(0, 0, 4, 4)), nil); err != nil {
		t.Fatal(err)
	}
	rawBytes := raw.Bytes()
	if len(rawBytes) < 2 || rawBytes[0] != 0xFF || rawBytes[1] != 0xD8 {
		t.Fatal("buildExifJPEG: image/jpeg didn't produce a JPEG starting with SOI")
	}

	inlineASCII := func(s string) [4]byte {
		var b [4]byte
		copy(b[:], s)
		return b // trailing zero byte(s) are the ASCII null terminator(s) IFD entries require
	}
	makeVal := inlineASCII(make_)
	modelVal := inlineASCII(model)

	var tiff bytes.Buffer
	tiff.WriteString("II")                     // little-endian byte order
	tiff.Write([]byte{0x2A, 0x00})             // TIFF magic
	tiff.Write([]byte{0x08, 0x00, 0x00, 0x00}) // offset to IFD0, right after this header
	tiff.Write([]byte{0x02, 0x00})             // IFD0 entry count
	tiff.Write([]byte{0x0F, 0x01})             // tag 0x010F Make
	tiff.Write([]byte{0x02, 0x00})             // type 2 = ASCII
	tiff.Write([]byte{0x04, 0x00, 0x00, 0x00}) // count, including the null terminator
	tiff.Write(makeVal[:])
	tiff.Write([]byte{0x10, 0x01})             // tag 0x0110 Model
	tiff.Write([]byte{0x02, 0x00})             // type 2 = ASCII
	tiff.Write([]byte{0x04, 0x00, 0x00, 0x00}) // count, including the null terminator
	tiff.Write(modelVal[:])
	tiff.Write([]byte{0x00, 0x00, 0x00, 0x00}) // next IFD offset: none

	var app1 bytes.Buffer
	app1.WriteString("Exif\x00\x00")
	app1.Write(tiff.Bytes())
	payload := app1.Bytes()

	var out bytes.Buffer
	out.Write(rawBytes[:2]) // SOI
	out.Write([]byte{0xFF, 0xE1})
	length := len(payload) + 2 // length field counts itself
	out.Write([]byte{byte(length >> 8), byte(length)})
	out.Write(payload)
	out.Write(rawBytes[2:])
	return out.Bytes()
}

func TestForImageRealJPEGWithExif(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "photo.jpg")
	if err := os.WriteFile(path, buildExifJPEG(t, "ACM", "X10"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := ForImage(path)
	if err != nil {
		t.Fatalf("ForImage() error = %v, want nil", err)
	}
	// "acm" lowercase, not "ACM": imagemeta's own camera-make
	// identification (meta/exif/makernote.IdentifyCameraMake)
	// normalizes the raw Make bytes in place while checking them
	// against its known-manufacturer table, even when — as here, for a
	// made-up manufacturer — nothing actually matches. Model goes
	// through no such lookup, so it survives untouched. Confirmed by
	// reading imagemeta's own source, not guessed: not a bug to fix,
	// just real upstream behavior to not be surprised by.
	want := Field{Label: "Camera", Value: "acm X10"}
	if len(got) != 1 || got[0] != want {
		t.Errorf("ForImage() = %+v, want [%+v]", got, want)
	}
}
