package viewer

import "image"

// Result is what Load returns: Kind decides how internal/ui presents
// the file (see showBuiltinLook).
//
//   - KindText: Content/Truncated carry the file's own (possibly
//     capped-at-limit) text.
//   - KindImage: Image/ImageFormat carry the decoded picture. Content/
//     Truncated stay at their zero values — a decoded image has no
//     "text" of its own to report, and Truncated here specifically
//     would be misleading: an image Load could actually decode is by
//     definition NOT truncated in any way that matters, regardless of
//     how ImagePreviewLimit compares to DefaultPreviewLimit.
//   - KindUnsupported: nothing else is meaningful. Reason, if non-empty,
//     is a short, human-readable explanation worth showing instead of
//     Look's own generic "no viewer for this file type" message — set
//     today only when an image-shaped file was too large to read
//     within ImagePreviewLimit (see Load); left empty for a genuinely
//     unrecognized format, where the generic message already says
//     everything there is to say.
type Result struct {
	Kind        Kind
	Content     string
	Truncated   bool
	Image       image.Image
	ImageFormat string
	Reason      string
}

// Load reads and classifies path, in one call — internal/ui's
// openLook/showBuiltinLook is the only real caller.
//
// Classification always starts from a small, sniffLen-bounded read
// (see Sniff) — cheap regardless of what path turns out to be. What
// happens next depends on the result: KindText reads up to limit bytes
// total (the caller's own choice — DefaultPreviewLimit from
// showBuiltinLook), reusing the sniffed bytes rather than reading them
// twice. KindImage instead reads up to ImagePreviewLimit — deliberately
// ignoring the caller's limit, which was sized for text, not a real
// photo (see ImagePreviewLimit's own doc comment) — and decodes it (see
// DecodeImage). A failure to decode despite that generous a read frame
// is reported as KindUnsupported with a Reason naming the real cause
// (too large to fully read, or a header this package's own Sniff
// recognized but no registered decoder could actually parse — a
// genuinely corrupt file, or one very rare gap: an unregistered
// sub-format sharing another format's own magic bytes).
func Load(path string, limit int64) (Result, error) {
	sample, sampleTruncated, err := ReadPreview(path, sniffLen)
	if err != nil {
		return Result{}, err
	}

	switch Sniff(sample) {
	case KindText:
		if !sampleTruncated && int64(len(sample)) <= limit {
			// The whole file already fits within both sniffLen and the
			// caller's own limit — no need to read it again just to
			// reconfirm the same bytes. The int64(len(sample)) <= limit
			// half specifically matters for a caller-supplied limit
			// smaller than sniffLen itself (a real case this package's
			// own tests exercise, not just a theoretical one) — without
			// it, a short file would come back whole even when the
			// caller explicitly asked for less than that.
			return Result{Kind: KindText, Content: string(sample)}, nil
		}
		data, truncated, err := ReadPreview(path, limit)
		if err != nil {
			return Result{}, err
		}
		return Result{Kind: KindText, Content: string(data), Truncated: truncated}, nil

	case KindImage:
		data, truncated, err := ReadPreview(path, ImagePreviewLimit)
		if err != nil {
			return Result{}, err
		}
		img, format, err := DecodeImage(data)
		if err != nil {
			if truncated {
				return Result{Kind: KindUnsupported, Reason: "image is larger than Look can preview"}, nil
			}
			return Result{Kind: KindUnsupported, Reason: "recognized as an image, but couldn't actually be decoded"}, nil
		}
		return Result{Kind: KindImage, Image: img, ImageFormat: format}, nil

	default:
		return Result{Kind: KindUnsupported}, nil
	}
}
