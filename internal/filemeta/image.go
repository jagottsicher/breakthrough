// Package filemeta extracts format-specific metadata for the Details
// sidebar's "M" shortcut. Images today (EXIF, via
// github.com/evanoberholster/imagemeta); PDF/Office Open XML/EPUB are
// planned next — see feature_ideas.txt #26 for the decided approach for
// each.
package filemeta

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/evanoberholster/imagemeta"
	"github.com/evanoberholster/imagemeta/meta/exif"
)

// Field is one label/value pair for display, in the fixed order
// fieldsFromExif produces them.
type Field struct {
	Label string
	Value string
}

// ErrNotSupported means path isn't a format imagemeta can parse EXIF
// from at all (e.g. GIF, WebP, BMP — formats this library doesn't cover)
// — distinct from ErrNoMetadata (right format, no EXIF present) and a
// plain read error (corrupt/unreadable file).
var ErrNotSupported = errors.New("metadata extraction not supported for this file type")

// ErrNoMetadata means path decoded fine but carries no EXIF data worth
// showing — most screenshots, any image re-saved by software that
// strips it, or a real photo whose own fields (see fieldsFromExif) all
// happen to be unset.
var ErrNoMetadata = errors.New("no EXIF metadata found")

// ForImage reads path's own EXIF data, if any. Deliberately a small,
// fixed field set rather than a full raw tag dump: renderDetailsSidebar
// tracks this section's row range to keep the hash/stat sections below
// it correctly numbered (see detailsMetaRowStart/End), so an
// unpredictably tall block is a real hazard here, not just a cosmetic
// concern — camera, when it was taken, and the core exposure settings a
// photographer actually looks for is plenty.
func ForImage(path string) ([]Field, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }() // read-only; nothing to do if this fails

	x, err := imagemeta.Decode(f)
	if err != nil {
		if errors.Is(err, imagemeta.ErrImageTypeNotFound) {
			return nil, ErrNotSupported
		}
		if errors.Is(err, imagemeta.ErrNoExif) {
			return nil, ErrNoMetadata
		}
		return nil, err
	}

	fields := fieldsFromExif(x)
	if len(fields) == 0 {
		return nil, ErrNoMetadata
	}
	return fields, nil
}

func fieldsFromExif(x exif.Exif) []Field {
	var fields []Field
	add := func(label, value string) {
		if value == "" {
			return
		}
		fields = append(fields, Field{Label: label, Value: value})
	}

	// x.IFD0.Make itself may already be lowercased by imagemeta's own
	// camera-make identification, even for a manufacturer it doesn't
	// recognize (see meta/exif/makernote.IdentifyCameraMake, which
	// normalizes its input in place before checking it) — nothing this
	// package can or should correct, since a recognized manufacturer's
	// name comes back in its own canonical form the exact same way.
	add("Camera", strings.TrimSpace(strings.TrimSpace(x.IFD0.Make)+" "+strings.TrimSpace(x.IFD0.Model)))

	if taken := x.SelectedDate(); !taken.IsZero() {
		add("Taken", taken.Format("2006-01-02 15:04:05"))
	}

	if x.ExifIFD.ExposureTime != 0 {
		add("Exposure", x.ExifIFD.ExposureTime.String()+"s")
	}
	if x.ExifIFD.FNumber != 0 {
		add("Aperture", "f/"+x.ExifIFD.FNumber.String())
	}
	if x.ExifIFD.ISOSpeedRatings != 0 {
		add("ISO", strconv.FormatUint(uint64(x.ExifIFD.ISOSpeedRatings), 10))
	}
	if x.ExifIFD.FocalLength != 0 {
		add("Focal length", x.ExifIFD.FocalLength.String())
	}

	// ExifIFD's own PixelXDimension/PixelYDimension (the decoded raster
	// size) take priority over IFD0's ImageWidth/ImageHeight (the main
	// TIFF/raw image, which for some raw formats describes a different,
	// often larger sensor-native size than the embedded preview this
	// sidebar actually shows) — fall back to IFD0 only when Exif doesn't
	// have its own.
	width, height := x.ExifIFD.PixelXDimension, x.ExifIFD.PixelYDimension
	if width == 0 || height == 0 {
		width, height = x.IFD0.ImageWidth, x.IFD0.ImageHeight
	}
	if width != 0 && height != 0 {
		add("Dimensions", fmt.Sprintf("%d × %d px", width, height))
	}

	return fields
}
