package viewer

import (
	"compress/gzip"
	"io"
	"os"
	"strings"

	"github.com/ulikunitz/xz"
)

// tarContainerSuffixes are the extensions LooksLikeBareCompressedPath
// must decline — a .tar.gz/.tgz/.tar.xz/.txz is a real multi-file
// container (the same distinction internal/archive's own Classify
// makes, kept here as its own small, independent copy rather than a
// new cross-package dependency — the same shape internal/ui's own
// archiveHighlightExtensions already keeps apart from internal/
// fileicons' own archiveExtensions), already browsable by entering it
// like a directory. Decompressing just its own outer gzip/xz layer
// here would only ever hand Look a tar stream's raw bytes, still
// binary and still unsupported — so this is purely about not even
// trying on one of these, per the user's own explicit request that
// ".tar.gz kann man 'rein' ... das soll auch so bleiben."
var tarContainerSuffixes = []string{".tar.gz", ".tgz", ".tar.xz", ".txz"}

// LooksLikeBareCompressedPath reports whether path is a single-stream
// .gz or .xz file DecompressForLook can actually handle — never one of
// tarContainerSuffixes, and never any other compression format (.bz2,
// .zst, ...) this package doesn't decompress for Look today (not asked
// for; easy to add here later the same way if it is).
func LooksLikeBareCompressedPath(path string) bool {
	lower := strings.ToLower(path)
	for _, s := range tarContainerSuffixes {
		if strings.HasSuffix(lower, s) {
			return false
		}
	}
	return strings.HasSuffix(lower, ".gz") || strings.HasSuffix(lower, ".xz")
}

// DecompressForLook decompresses a bare .gz/.xz file (see
// LooksLikeBareCompressedPath — callers check that first) into a fresh
// temp file, bounded by limit the same way ReadPreview already is —
// decompressed content can be far larger than its own compressed
// source, so this is also what keeps a pathological "small file,
// enormous decompressed size" input from being read without any bound
// at all. The temp file is handed to Load exactly like any other
// local path from there, so Look's own ordinary text/image/PDF
// classification decides what's actually shown — a decompressed
// binary (or anything else Load can't show) reaches Look's own
// regular "no viewer for this file type" response from there, per the
// user's own explicit request: "wenn dann in der gz datei ein Binary
// ... steckt, geht das eben nicht."
//
// cleanup removes the temp file and is always non-nil, even on error —
// the same "always safe to call, no nil-check needed" contract
// downloadRemoteToTemp's own identically-shaped cleanup already
// follows (internal/ui/remotestage.go).
func DecompressForLook(path string, limit int64) (tmpPath string, cleanup func(), err error) {
	noop := func() {}

	f, err := os.Open(path)
	if err != nil {
		return "", noop, err
	}
	defer func() { _ = f.Close() }()

	var r io.Reader
	if strings.HasSuffix(strings.ToLower(path), ".xz") {
		r, err = xz.NewReader(f)
	} else {
		var gz *gzip.Reader
		gz, err = gzip.NewReader(f)
		if gz != nil {
			defer func() { _ = gz.Close() }()
		}
		r = gz
	}
	if err != nil {
		return "", noop, err
	}

	tmp, err := os.CreateTemp("", "breakthrough-look-*")
	if err != nil {
		return "", noop, err
	}
	cleanup = func() { _ = os.Remove(tmp.Name()) }

	if _, err := io.Copy(tmp, io.LimitReader(r, limit+1)); err != nil {
		_ = tmp.Close()
		cleanup()
		return "", noop, err
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return "", noop, err
	}
	return tmp.Name(), cleanup, nil
}
