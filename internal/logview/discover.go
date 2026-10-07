package logview

import (
	"compress/bzip2"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/klauspost/compress/zstd"
	"github.com/ulikunitz/xz"
)

// FileGroup is one logrotate family — "access.log",
// "access.log.1", "access.log.2.gz" — collapsed to a single entry the
// selection screen shows and the user picks as one unit. Files is
// sorted newest-rotation-first (the plain file, then .1, .2, ...), the
// order a logrotate family is naturally read in.
type FileGroup struct {
	Base  string // the family's own shared name, e.g. "access.log"
	Files []CandidateFile
}

// CandidateFile is one file discovered by Discover.
type CandidateFile struct {
	Path       string
	Compressed bool // true for any of supportedCompressedExts, or a compression Open doesn't recognize at all
	Supported  bool // false only for a compression extension Open doesn't recognize at all — every one it does (gz/xz/zst/bz2) is always Supported
}

// rotationSuffixes strips, in order, the pieces a logrotate'd file name
// is actually built from — compression extension, then a numeric
// rotation index (".1", ".2", ...) or a dateext timestamp
// ("-20260101" or ".20260101") — repeatedly, so e.g.
// "access.log.2.gz" reduces to "access.log" in two passes (gz, then
// ".2").
var (
	reCompressedSuffix = regexp.MustCompile(`(?i)\.(gz|bz2|xz|zst)$`)
	reNumericSuffix    = regexp.MustCompile(`\.\d+$`)
	reDateSuffix       = regexp.MustCompile(`[-.]\d{8}$`)
)

// familyBase reduces name to the shared base name its whole logrotate
// family has in common — see rotationSuffixes' own doc comment.
func familyBase(name string) string {
	for {
		switch {
		case reCompressedSuffix.MatchString(name):
			name = reCompressedSuffix.ReplaceAllString(name, "")
		case reNumericSuffix.MatchString(name):
			name = reNumericSuffix.ReplaceAllString(name, "")
		case reDateSuffix.MatchString(name):
			name = reDateSuffix.ReplaceAllString(name, "")
		default:
			return name
		}
	}
}

// compressedExt reports the lowercase compression extension of name
// ("gz", "xz", "zst", "bz2"), or "" if name has none.
func compressedExt(name string) string {
	m := reCompressedSuffix.FindString(name)
	if m == "" {
		return ""
	}
	return strings.ToLower(strings.TrimPrefix(m, "."))
}

// supportedCompressedExts is what Open can actually decompress — gzip,
// xz, zstd, and bzip2 (Phase 1b), all pure Go, no CGO: gzip and bzip2
// via stdlib, xz via the already-present github.com/ulikunitz/xz
// (internal/viewer's own Look decompression already depends on it),
// zstd via github.com/klauspost/compress/zstd, newly added for this.
var supportedCompressedExts = map[string]bool{"gz": true, "xz": true, "zst": true, "bz2": true}

// Discover lists dir's own regular files (one level, not recursive —
// this mirrors how "jL" is invoked: on whichever directory is open
// right now, the same scope the panel itself shows) and groups them
// into logrotate families by familyBase. Groups are sorted by Base,
// and each group's own Files newest-rotation-first — a file with a
// numeric or dateext suffix sorts after the bare file, lowest suffix
// first, by a plain string comparison of the stripped suffix (good
// enough for both "access.log.1".."access.log.9" and dateext's
// 8-digit stamps, which compare correctly as plain strings either
// way).
func Discover(dir string) ([]FileGroup, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	groups := map[string][]CandidateFile{}
	for _, e := range entries {
		if e.IsDir() || !e.Type().IsRegular() {
			continue
		}
		name := e.Name()
		base := familyBase(name)
		ext := compressedExt(name)
		groups[base] = append(groups[base], CandidateFile{
			Path:       filepath.Join(dir, name),
			Compressed: ext != "",
			Supported:  ext == "" || supportedCompressedExts[ext],
		})
	}

	var bases []string
	for b := range groups {
		bases = append(bases, b)
	}
	sort.Strings(bases)

	out := make([]FileGroup, 0, len(bases))
	for _, b := range bases {
		files := groups[b]
		sort.Slice(files, func(i, j int) bool {
			return rotationSortKey(files[i].Path) < rotationSortKey(files[j].Path)
		})
		out = append(out, FileGroup{Base: b, Files: files})
	}
	return out, nil
}

// rotationSortKey is the suffix familyBase stripped off, so sorting by
// it puts the bare file ("") first, then ".1", ".2", ... in ascending
// order — plain filepath.Base(path) comparison would instead sort
// "access.log" after "access.log.1" lexically ('.' < nothing is not
// how string comparison works here), which is the wrong order.
func rotationSortKey(path string) string {
	name := filepath.Base(path)
	base := familyBase(name)
	return strings.TrimPrefix(name, base)
}

// Open returns a decompressing reader for f — a plain os.File for an
// uncompressed CandidateFile, or the right decoder wrapped around one
// for a compressed extension (see supportedCompressedExts). Callers
// must check f.Supported first; Open itself still refuses an
// unsupported compression (an unrecognized extension sneaking in some
// other way) rather than silently handing back raw compressed bytes.
func Open(f CandidateFile) (io.ReadCloser, error) {
	file, err := os.Open(f.Path)
	if err != nil {
		return nil, err
	}
	if !f.Compressed {
		return file, nil
	}

	switch compressedExt(filepath.Base(f.Path)) {
	case "gz":
		gz, err := gzip.NewReader(file)
		if err != nil {
			_ = file.Close()
			return nil, err
		}
		return &closingReader{Reader: gz, closer: gz, file: file}, nil
	case "xz":
		xr, err := xz.NewReader(file)
		if err != nil {
			_ = file.Close()
			return nil, err
		}
		// xz.Reader owns no resource of its own beyond the file —
		// pure Go decompression, nothing to release but the fd.
		return &closingReader{Reader: xr, file: file}, nil
	case "zst":
		zr, err := zstd.NewReader(file)
		if err != nil {
			_ = file.Close()
			return nil, err
		}
		return &closingReader{Reader: zr, closer: zstdCloser{zr}, file: file}, nil
	case "bz2":
		// bzip2.NewReader returns a plain io.Reader — like xz, the Go
		// standard library only ever implements bzip2 decoding, never
		// encoding, and needs no Close of its own beyond the file.
		return &closingReader{Reader: bzip2.NewReader(file), file: file}, nil
	default:
		_ = file.Close()
		return nil, fmt.Errorf("%s: unsupported compression", f.Path)
	}
}

// closingReader pairs a decompressing io.Reader with the underlying
// file it reads from, closing both — closer is the decoder's own
// Close (gzip.Reader, zstdCloser), left nil for a decoder that has
// none (xz.Reader, bzip2's own Reader), the same "only close what
// actually owns a resource" reasoning gzipReadCloser used to need its
// own dedicated type for.
type closingReader struct {
	io.Reader
	closer io.Closer
	file   *os.File
}

func (c *closingReader) Close() error {
	var closerErr error
	if c.closer != nil {
		closerErr = c.closer.Close()
	}
	fileErr := c.file.Close()
	if closerErr != nil {
		return closerErr
	}
	return fileErr
}

// zstdCloser adapts *zstd.Decoder's own Close (which returns nothing)
// to io.Closer, so closingReader can treat it the same as gzip.Reader.
type zstdCloser struct{ zr *zstd.Decoder }

func (z zstdCloser) Close() error {
	z.zr.Close()
	return nil
}
