// Package archive lets breakthrough browse into zip and tar (plain,
// .gz, .bz2, .xz) files as if they were ordinary directories — read
// only: listing and extracting are all this package does, matching the
// feature's own deliberate scope (see internal/ui's own archive-browse
// doc comments for the "why" behind that limit). Nested archives (a zip
// found inside a tar, say) are never opened automatically; a member
// that happens to itself be a recognized archive format is still just
// listed as an ordinary file, extractable like any other.
package archive

import (
	"archive/tar"
	"archive/zip"
	"compress/bzip2"
	"compress/gzip"
	"io"
	"io/fs"
	"os"
	"path"
	"strings"
	"time"

	"github.com/ulikunitz/xz"
)

// Kind is which archive format a path was classified as (see Classify).
type Kind int

const (
	// KindZip is a .zip file, read via Go's own archive/zip — a real
	// central directory, so List doesn't need to scan the whole file.
	KindZip Kind = iota
	// KindTar covers every member of the tar family: plain .tar, and
	// .tar.gz/.tgz, .tar.bz2/.tbz(2), .tar.xz/.txz — one archive/tar
	// reader underneath in every case, wrapped in whichever
	// decompressor (or none) the extension calls for (see
	// compressionFor). Unlike zip, a tar stream has no index: List and
	// Extract both have to read all the way through it.
	KindTar
)

// archiveExtensions is the suffix table Classify checks against, and
// the compression compressionFor derives for KindTar from the same
// suffix — kept in one table, rather than two separate lists, so a
// suffix and its decompressor can never drift apart from each other.
var archiveExtensions = []struct {
	suffix string
	kind   Kind
	comp   compression
}{
	{".zip", KindZip, compNone},
	{".tar", KindTar, compNone},
	{".tar.gz", KindTar, compGzip},
	{".tgz", KindTar, compGzip},
	{".tar.bz2", KindTar, compBzip2},
	{".tbz2", KindTar, compBzip2},
	{".tbz", KindTar, compBzip2},
	{".tar.xz", KindTar, compXz},
	{".txz", KindTar, compXz},
}

// compression is which decompressor (if any) a KindTar archive's own
// bytes need before archive/tar can read them — Classify/compressionFor
// derive this once, up front, from the extension alone (the same
// convention internal/search's own classifyArchive already uses,
// rather than sniffing magic bytes): every extension this package
// recognizes at all names its compression unambiguously.
type compression int

const (
	compNone compression = iota
	compGzip
	compBzip2
	compXz
)

// Classify reports which Kind path's own extension matches
// (case-insensitively, the same convention internal/search's own
// classifyArchive uses), or ok=false if it matches none of
// archiveExtensions at all.
func Classify(p string) (kind Kind, ok bool) {
	lower := strings.ToLower(p)
	for _, e := range archiveExtensions {
		if strings.HasSuffix(lower, e.suffix) {
			return e.kind, true
		}
	}
	return 0, false
}

// compressionFor is Classify's own compression half — always resolved
// from the very same table entry, so it can never disagree with
// Classify about which extension matched.
func compressionFor(p string) compression {
	lower := strings.ToLower(p)
	for _, e := range archiveExtensions {
		if strings.HasSuffix(lower, e.suffix) {
			return e.comp
		}
	}
	return compNone
}

// Entry is one member of an archive, as List reports it — a flat
// listing of the whole archive, not just one directory level (see
// Children for turning this into a browsable hierarchy).
type Entry struct {
	// Path is the member's own path inside the archive, forward-slash
	// separated regardless of host OS (archive formats are themselves
	// slash-separated, independent of the platform an archive was
	// created — or is being read — on), with no leading slash and no
	// trailing slash even for a directory (IsDir carries that instead —
	// see path.Clean's own doc comment on why a bare trailing slash is
	// never meaningful information Path itself needs to carry).
	Path    string
	Size    int64
	Mode    fs.FileMode
	ModTime time.Time
	IsDir   bool
}

// List returns every entry in the archive at archivePath, in whatever
// order the underlying format's own index (zip) or stream (tar)
// reports them — Children is what turns this into one directory
// level's own, sorted listing; List itself makes no ordering promise.
func List(archivePath string) ([]Entry, error) {
	kind, ok := Classify(archivePath)
	if !ok {
		return nil, &UnsupportedFormatError{Path: archivePath}
	}
	switch kind {
	case KindZip:
		return listZip(archivePath)
	default:
		return listTar(archivePath)
	}
}

// UnsupportedFormatError reports that Path's own extension isn't one
// Classify recognizes at all — the same "this isn't an archive this
// package knows how to open" condition every exported func in this
// package can return, named so callers can tell it apart from a read
// failure on an archive it does recognize (a real I/O error, a
// corrupted file, ...).
type UnsupportedFormatError struct{ Path string }

func (e *UnsupportedFormatError) Error() string {
	return e.Path + ": not a recognized archive format"
}

// listZip reads archivePath's own central directory via archive/zip —
// a real index, so this never has to decompress a single member's
// worth of data just to list them all.
func listZip(archivePath string) ([]Entry, error) {
	r, err := zip.OpenReader(archivePath)
	if err != nil {
		return nil, err
	}
	defer func() { _ = r.Close() }() // read-only; nothing to do if this fails

	entries := make([]Entry, 0, len(r.File))
	for _, f := range r.File {
		fi := f.FileInfo()
		entries = append(entries, Entry{
			Path:    path.Clean(strings.TrimSuffix(f.Name, "/")),
			Size:    fi.Size(),
			Mode:    fi.Mode(),
			ModTime: fi.ModTime(),
			IsDir:   fi.IsDir(),
		})
	}
	return entries, nil
}

// listTar streams all the way through archivePath's own tar body (see
// openTarStream) since, unlike zip, there's no separate index to read
// instead — the same up-front cost List's own doc comment already
// warns about.
func listTar(archivePath string) ([]Entry, error) {
	f, closeAll, err := openTarStream(archivePath)
	if err != nil {
		return nil, err
	}
	defer closeAll()

	var entries []Entry
	tr := tar.NewReader(f)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if hdr.Typeflag != tar.TypeDir && hdr.Typeflag != tar.TypeReg {
			continue // skip symlinks/devices/etc. — nothing this browsable, read-only view can do with one anyway
		}
		entries = append(entries, Entry{
			Path:    path.Clean(strings.TrimSuffix(hdr.Name, "/")),
			Size:    hdr.Size,
			Mode:    hdr.FileInfo().Mode(),
			ModTime: hdr.ModTime,
			IsDir:   hdr.Typeflag == tar.TypeDir,
		})
	}
	return entries, nil
}

// openTarStream opens archivePath and wraps it in whichever
// decompressor compressionFor says its extension calls for, returning a
// reader ready for archive/tar.NewReader and a close func that tears
// down every layer this needed. Every one of these is read-only, so
// every Close error here is deliberately discarded the same way
// listZip's own is — there's nothing left to do about a failure
// closing something nothing more is ever written to.
func openTarStream(archivePath string) (r io.Reader, closeAll func(), err error) {
	f, err := os.Open(archivePath)
	if err != nil {
		return nil, nil, err
	}
	switch compressionFor(archivePath) {
	case compGzip:
		gz, err := gzip.NewReader(f)
		if err != nil {
			_ = f.Close()
			return nil, nil, err
		}
		return gz, func() { _ = gz.Close(); _ = f.Close() }, nil
	case compBzip2:
		return bzip2.NewReader(f), func() { _ = f.Close() }, nil
	case compXz:
		// *xz.Reader has no Close method of its own to call at all
		// (verified directly against its own type, not assumed) — only
		// the underlying file needs closing here.
		xr, err := xz.NewReader(f)
		if err != nil {
			_ = f.Close()
			return nil, nil, err
		}
		return xr, func() { _ = f.Close() }, nil
	default:
		return f, func() { _ = f.Close() }, nil
	}
}
