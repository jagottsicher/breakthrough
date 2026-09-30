// Package archive lets breakthrough browse into zip, tar (plain, .gz,
// .bz2, .xz), 7z, and RAR files as if they were ordinary directories —
// read only: listing and extracting are all this package does,
// matching the feature's own deliberate scope (see internal/ui's own
// archive-browse doc comments for the "why" behind that limit). Nested
// archives (a zip found inside a tar, say) are never opened
// automatically; a member that happens to itself be a recognized
// archive format is still just listed as an ordinary file, extractable
// like any other.
//
// zip and tar are read entirely in Go (archive/zip, archive/tar, plus
// this package's own decompressors) — no external tool involved, no
// subprocess to spawn per file browsed. 7z and RAR have no comparable
// pure-Go story this project is willing to depend on (7z: no actively
// maintained, complete pure-Go reader; RAR: a proprietary, patent-
// encumbered format with no legally clean Go implementation at all —
// unrar itself is freeware, not open source), so those two instead
// shell out to the real `7z`/`unrar` binaries the same way pdftoppm and
// mpv already are elsewhere in this app (see internal/viewer's own
// pdf.go/video.go) — an already-installed, real tool doing what it
// already does, not a reimplementation.
package archive

import (
	"archive/tar"
	"archive/zip"
	"compress/bzip2"
	"compress/gzip"
	"errors"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"strconv"
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
	// KindSevenZip is a .7z file, read via the real `7z` (or `7za`/
	// `7zr`) binary's own technical listing (`7z l -ba -slt`) and
	// extracted into a temp directory it also produces itself — see
	// listSevenZip/extractSevenZip's own doc comments.
	KindSevenZip
	// KindRar is a .rar file, read and extracted via the real `unrar`
	// binary — see listRar/extractRar's own doc comments.
	KindRar
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
	{".7z", KindSevenZip, compNone},
	{".rar", KindRar, compNone},
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
	case KindSevenZip:
		return listSevenZip(archivePath)
	case KindRar:
		return listRar(archivePath)
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

// sevenZipBinaries is sevenZipBinary's own search order — the full,
// actively-maintained `7z` first, then the older, LZMA-only `7za`/
// `7zr` names some distros still package separately — the same "check
// what's actually there, in priority order" convention
// internal/ui's own batBinary/packageManagerInstallHint already use for
// a different optional external tool.
var sevenZipBinaries = []string{"7z", "7za", "7zr"}

// sevenZipBinary returns whichever of sevenZipBinaries is actually on
// $PATH, or an error naming all three if none is.
func sevenZipBinary() (string, error) {
	for _, name := range sevenZipBinaries {
		if _, err := exec.LookPath(name); err == nil {
			return name, nil
		}
	}
	return "", errors.New("no 7-Zip binary found (7z, 7za, or 7zr)")
}

// listSevenZip runs `7z l -ba -slt` on archivePath — 7-Zip's own
// "technical" listing mode: one blank-line-separated block per member,
// each a set of "Key = Value" lines (verified directly against real
// `7z` output, not assumed from documentation alone — see
// parseSevenZipListing) — rather than its default columnar table,
// which packs Path into a fixed-width column that truncates or wraps a
// long one.
func listSevenZip(archivePath string) ([]Entry, error) {
	bin, err := sevenZipBinary()
	if err != nil {
		return nil, err
	}
	out, err := exec.Command(bin, "l", "-ba", "-slt", archivePath).Output()
	if err != nil {
		return nil, err
	}
	return parseSevenZipListing(string(out)), nil
}

// parseSevenZipListing turns listSevenZip's own raw `-slt` output into
// Entries — see listSevenZip's own doc comment on the block shape this
// expects. A block missing "Path" entirely (shouldn't happen in
// practice, but this package prefers silently skipping an
// unparseable-shaped record over ever raising a nonsensical, empty-
// path Entry) is dropped rather than included.
func parseSevenZipListing(output string) []Entry {
	var entries []Entry
	fields := make(map[string]string)
	flush := func() {
		if p := fields["Path"]; p != "" {
			entries = append(entries, sevenZipEntryFrom(fields))
		}
		fields = make(map[string]string)
	}
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimRight(line, "\r")
		if strings.TrimSpace(line) == "" {
			flush()
			continue
		}
		if key, value, ok := strings.Cut(line, " = "); ok {
			fields[key] = value
		}
	}
	flush()
	return entries
}

// sevenZipEntryFrom turns one already-parsed "Key = Value" block into
// an Entry. Attributes is "D <ls-style-mode>" for a directory or "A
// <ls-style-mode>" for a regular file (real, observed `7z` output —
// see parseLSPermString for the mode string itself); IsDir is read
// from that leading letter rather than from Size (a directory's own
// Size is always 0, but so is a genuinely empty file's).
func sevenZipEntryFrom(fields map[string]string) Entry {
	attrs := fields["Attributes"]
	isDir := strings.HasPrefix(attrs, "D")
	var mode fs.FileMode
	if parts := strings.Fields(attrs); len(parts) > 0 {
		mode = parseLSPermString(parts[len(parts)-1])
	}
	size, _ := strconv.ParseInt(fields["Size"], 10, 64)
	return Entry{
		Path:    path.Clean(strings.ReplaceAll(fields["Path"], "\\", "/")),
		Size:    size,
		Mode:    mode,
		ModTime: parseToolTimestamp(fields["Modified"]),
		IsDir:   isDir,
	}
}

// listRar runs `unrar lt` on archivePath — its own verbose/technical
// listing mode, the same "one block of Key: Value lines per member"
// shape listSevenZip's own `7z l -slt` has, just with unrar's own
// field names and a leading banner/header this package's block parser
// below simply never recognizes a real member in (no "Type" field of
// its own, so flush's own guard drops it) — verified directly against
// real `unrar lt` output, not assumed from documentation alone.
func listRar(archivePath string) ([]Entry, error) {
	out, err := exec.Command("unrar", "lt", archivePath).Output()
	if err != nil {
		return nil, err
	}
	return parseRarListing(string(out)), nil
}

// parseRarListing mirrors parseSevenZipListing's own block-by-block
// shape (see its doc comment), just for unrar's "Key: Value" lines
// (colon, not " = ") and its own explicit "Type: File"/"Type:
// Directory" field rather than a leading letter in Attributes.
func parseRarListing(output string) []Entry {
	var entries []Entry
	fields := make(map[string]string)
	flush := func() {
		switch fields["Type"] {
		case "File":
			entries = append(entries, rarEntryFrom(fields, false))
		case "Directory":
			entries = append(entries, rarEntryFrom(fields, true))
		}
		// Anything else (RAR5's own internal service records, or the
		// banner/"Archive:"/"Details:" preamble before the first real
		// member — neither has a "Type" field at all) is silently
		// dropped, the same as listTar's own skip of a tar member
		// Typeflag that isn't TypeDir/TypeReg.
		fields = make(map[string]string)
	}
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimRight(line, "\r")
		if strings.TrimSpace(line) == "" {
			flush()
			continue
		}
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		key, value = strings.TrimSpace(key), strings.TrimSpace(value)
		if key != "" {
			fields[key] = value
		}
	}
	flush()
	return entries
}

// rarEntryFrom turns one already-parsed unrar "lt" block into an Entry.
// A directory member has no "Size" field of its own at all in real
// unrar output (rather than reporting 0 explicitly) — ParseInt on the
// resulting empty string already fails closed to 0, which is exactly
// right for a directory either way.
func rarEntryFrom(fields map[string]string, isDir bool) Entry {
	size, _ := strconv.ParseInt(fields["Size"], 10, 64)
	return Entry{
		Path:    path.Clean(strings.ReplaceAll(fields["Name"], "\\", "/")),
		Size:    size,
		Mode:    parseLSPermString(fields["Attributes"]),
		ModTime: parseToolTimestamp(fields["mtime"]),
		IsDir:   isDir,
	}
}

// parseLSPermString parses a standard `ls -l`-style 10-character mode
// string ("-rw-r--r--", "drwxr-xr-x", ...) — real `7z`/`unrar` output,
// not something either tool documents as a stable format, so a string
// too short or otherwise not shaped like this is simply parsed as far
// as it goes rather than treated as an error: a member's exact
// permission bits are cosmetic here (extraction restores the real
// ones straight from the archive itself — see extractSevenZip/
// extractRar — this is only ever used for List's own display), never
// worth failing an entire listing over.
func parseLSPermString(s string) fs.FileMode {
	var mode fs.FileMode
	if len(s) > 0 && s[0] == 'd' {
		mode |= fs.ModeDir
	}
	const rwx = "rwxrwxrwx"
	for i := 0; i < len(rwx) && i+1 < len(s); i++ {
		if s[i+1] == rwx[i] {
			mode |= 1 << uint(8-i)
		}
	}
	return mode
}

// parseToolTimestamp parses the "YYYY-MM-DD HH:MM:SS" prefix common to
// both `7z l -slt`'s own "Modified" field ("2026-09-30
// 20:03:41.0708158", dot-separated fractional seconds) and `unrar lt`'s
// own "mtime" field ("2026-09-30 20:03:41,071137256", comma-separated)
// — the fractional part isn't needed for anything this package reports
// (Entry.ModTime's own real callers, e.g. detailsStatLines' formatting,
// only ever show whole seconds), so it's simply never parsed rather
// than handling two different separators for it. Returns the zero
// Time, not an error, for a value shorter than that fixed prefix —
// same "cosmetic, not worth failing a whole listing over" reasoning as
// parseLSPermString.
func parseToolTimestamp(s string) time.Time {
	if len(s) < 19 {
		return time.Time{}
	}
	t, err := time.ParseInLocation("2006-01-02 15:04:05", s[:19], time.Local)
	if err != nil {
		return time.Time{}
	}
	return t
}
