package viewer

import (
	"bytes"
	"image"
)

// Kind is what Load decided a file's content actually is — see Load and
// Sniff.
type Kind int

const (
	// KindUnsupported is anything this package doesn't yet know how to
	// preview — binary content Sniff doesn't recognize as an image
	// either (see below), or one Load reads but can't actually decode
	// (a corrupt file, a real image format none of this package's
	// registered decoders cover — see image.go). Later phases add their
	// own Kind values (PDF, archive listings) rather than teaching
	// Sniff to recognize them by content alone.
	KindUnsupported Kind = iota
	// KindText is plain, previewable text content — source code, config
	// files, diffs/patches, logs, anything else Sniff didn't flag as
	// binary.
	KindText
	// KindImage is content Sniff recognized as a decodable raster image
	// (see image.go's own format registration) — PNG, JPEG, GIF, BMP,
	// TIFF, WebP. Result.Image carries the decoded image.Image itself;
	// Content/Truncated stay at their zero values, the same as
	// KindUnsupported (see Result's own doc comment).
	KindImage
	// KindPDF is content Sniff recognized by the PDF file signature
	// ("%PDF-", checked before the NUL-byte text/binary split below —
	// a real PDF's own mixed binary/text structure would otherwise
	// often land on KindUnsupported before ever getting this far). Load
	// itself only ever reports the Kind for a PDF — see its own doc
	// comment on why the actual page content is a separate, explicit
	// call (LoadPDFPage — see pdf.go) rather than something Load does
	// automatically the way it does for text and images.
	KindPDF
)

// sniffLen is how much of a file's content Sniff actually inspects.
// git's own xdiff-interface.c buffer_is_binary uses 8000 bytes for
// exactly the same text-vs-binary NUL check below — generous enough not
// to be fooled by a text file that happens to start with a long run of
// plain content before anything binary-looking — but that number turned
// out to be too small for the image.DecodeConfig check just below it:
// a real, user-reported JPEG (a GIMP-authored photo with a sizeable
// embedded EXIF block) put its own SOF marker — the thing
// DecodeConfig actually needs to reach — at byte 12752, past the old
// 8000-byte window, so Sniff reported KindUnsupported for a perfectly
// valid, normally-decodable image; both Look and the Details sidebar
// silently showed nothing for it. 64 KiB is comfortable headroom over
// that (and over an even larger embedded thumbnail/XMP block a
// different camera or editor might produce) while staying nowhere near
// the cost of the real, full pixel decode this is deliberately meant to
// stay cheaper than (see Load).
const sniffLen = 64 << 10 // 64 KiB

// Sniff decides Kind from sample, a prefix of a file's real content (see
// Load, which always passes the file's own first sniffLen bytes — never
// a shorter, truncated caller-supplied slice). A NUL byte anywhere in it
// is treated as the tell that this is binary content, not text — Sniff
// never actually attempts to validate UTF-8 or count printable-vs-control
// bytes, on the theory that a real NUL is the one byte no ordinary text
// file (any encoding, any language) legitimately contains, while "mostly
// printable" heuristics are far easier to fool one way or the other.
//
// Binary content is then checked against image.DecodeConfig (every
// registered format — see image.go's own init) before giving up as
// KindUnsupported: DecodeConfig only ever reads a format's own header,
// never the full pixel data, so this stays a cheap, sniffLen-bounded
// check exactly like the text one above it, not a real decode (see
// Load for the actual pixel decode, run only once Sniff has already
// said KindImage).
//
// Known limitations, accepted rather than solved here:
//   - UTF-16 text (a real, if uncommon, encoding for a Windows-authored
//     file) legitimately contains a NUL byte after every plain-ASCII
//     character, and would be misclassified as binary by the same rule
//     that makes KindText work at all — the identical limitation git's
//     own binary detection has.
//   - TIFF stores its own image directory (IFD) at a byte offset named
//     in the header, not necessarily near the start of the file —
//     DecodeConfig needs to actually reach that offset to succeed, and
//     a TIFF whose IFD sits past sniffLen bytes in won't be recognized
//     here. Likewise a JPEG whose own SOF marker sits past sniffLen —
//     sniffLen's own doc comment above already covers a real,
//     user-reported case that used to fail this way; a pathological
//     one with even more embedded metadata than that still could.
func Sniff(sample []byte) Kind {
	if bytes.HasPrefix(sample, []byte("%PDF-")) {
		return KindPDF
	}
	if bytes.IndexByte(sample, 0) == -1 {
		return KindText
	}
	if _, _, err := image.DecodeConfig(bytes.NewReader(sample)); err == nil {
		return KindImage
	}
	return KindUnsupported
}
