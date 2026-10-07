package logview

import (
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
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
	Compressed bool // true for .gz (see Open) or an as-yet-unsupported format
	Supported  bool // false for a compression Open can't decompress yet (.xz/.zst/.bz2 — Phase 1b)
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

// supportedCompressedExts is what Open can actually decompress today —
// gzip only, Phase 1; .xz/.zst/.bz2 are still discovered and grouped
// (so the selection screen can show them, clearly marked unsupported)
// but Open refuses them until Phase 1b adds their own decoders.
var supportedCompressedExts = map[string]bool{"gz": true}

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
// uncompressed CandidateFile, or a gzip.Reader wrapped around one for
// a ".gz" (see supportedCompressedExts). Callers must check f.Supported
// first; Open itself still refuses an unsupported compression rather
// than silently handing back raw compressed bytes.
func Open(f CandidateFile) (io.ReadCloser, error) {
	file, err := os.Open(f.Path)
	if err != nil {
		return nil, err
	}
	if !f.Compressed {
		return file, nil
	}
	gz, err := gzip.NewReader(file)
	if err != nil {
		_ = file.Close()
		return nil, err
	}
	return &gzipReadCloser{gz: gz, file: file}, nil
}

// gzipReadCloser closes both the gzip.Reader and the underlying file —
// gzip.Reader.Close only releases the decompressor's own state, not
// the file descriptor it was reading from.
type gzipReadCloser struct {
	gz   *gzip.Reader
	file *os.File
}

func (g *gzipReadCloser) Read(p []byte) (int, error) { return g.gz.Read(p) }

func (g *gzipReadCloser) Close() error {
	gzErr := g.gz.Close()
	fileErr := g.file.Close()
	if gzErr != nil {
		return gzErr
	}
	return fileErr
}
