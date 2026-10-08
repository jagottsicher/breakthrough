package logview

import (
	"compress/gzip"
	"encoding/base64"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/klauspost/compress/zstd"
	"github.com/ulikunitz/xz"
)

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile %s: %v", name, err)
	}
}

func writeGzipFile(t *testing.T, dir, name, content string) {
	t.Helper()
	f, err := os.Create(filepath.Join(dir, name))
	if err != nil {
		t.Fatalf("Create %s: %v", name, err)
	}
	defer func() { _ = f.Close() }()
	gz := gzip.NewWriter(f)
	if _, err := gz.Write([]byte(content)); err != nil {
		t.Fatalf("gzip Write %s: %v", name, err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("gzip Close %s: %v", name, err)
	}
}

func TestDiscoverGroupsLogrotateFamily(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "access.log", "current\n")
	writeFile(t, dir, "access.log.1", "yesterday\n")
	writeGzipFile(t, dir, "access.log.2.gz", "two days ago\n")
	writeFile(t, dir, "error.log", "errors\n")
	if err := os.Mkdir(filepath.Join(dir, "subdir"), 0o755); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}

	groups, err := Discover(dir)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(groups) != 2 {
		t.Fatalf("len(groups) = %d, want 2 (access.log, error.log)", len(groups))
	}

	access := groups[0]
	if access.Base != "access.log" {
		t.Fatalf("groups[0].Base = %q, want access.log", access.Base)
	}
	if len(access.Files) != 3 {
		t.Fatalf("len(access.Files) = %d, want 3", len(access.Files))
	}
	if filepath.Base(access.Files[0].Path) != "access.log" {
		t.Errorf("Files[0] = %q, want access.log first (newest)", access.Files[0].Path)
	}
	if filepath.Base(access.Files[2].Path) != "access.log.2.gz" {
		t.Errorf("Files[2] = %q, want access.log.2.gz last (oldest)", access.Files[2].Path)
	}
	if !access.Files[2].Compressed || !access.Files[2].Supported {
		t.Errorf("Files[2] (gz) Compressed/Supported = %v/%v, want true/true", access.Files[2].Compressed, access.Files[2].Supported)
	}

	if groups[1].Base != "error.log" {
		t.Fatalf("groups[1].Base = %q, want error.log", groups[1].Base)
	}
}

// writeXzFile/writeZstdFile use the same libraries Open itself
// decodes with (github.com/ulikunitz/xz, github.com/klauspost/compress/zstd)
// to encode a fixture — both provide a real Writer, unlike bzip2 (see
// bz2FixtureBase64's own doc comment below).
func writeXzFile(t *testing.T, dir, name, content string) {
	t.Helper()
	f, err := os.Create(filepath.Join(dir, name))
	if err != nil {
		t.Fatalf("Create %s: %v", name, err)
	}
	defer func() { _ = f.Close() }()
	xw, err := xz.NewWriter(f)
	if err != nil {
		t.Fatalf("xz.NewWriter %s: %v", name, err)
	}
	if _, err := xw.Write([]byte(content)); err != nil {
		t.Fatalf("xz Write %s: %v", name, err)
	}
	if err := xw.Close(); err != nil {
		t.Fatalf("xz Close %s: %v", name, err)
	}
}

func writeZstdFile(t *testing.T, dir, name, content string) {
	t.Helper()
	f, err := os.Create(filepath.Join(dir, name))
	if err != nil {
		t.Fatalf("Create %s: %v", name, err)
	}
	defer func() { _ = f.Close() }()
	zw, err := zstd.NewWriter(f)
	if err != nil {
		t.Fatalf("zstd.NewWriter %s: %v", name, err)
	}
	if _, err := zw.Write([]byte(content)); err != nil {
		t.Fatalf("zstd Write %s: %v", name, err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("zstd Close %s: %v", name, err)
	}
}

// bz2Fixture is "hello from bzip2\n" compressed with the real `bzip2`
// command-line tool — compress/bzip2 (Go's standard library) only ever
// implements the decoder, never an encoder, so there is no Go-side way
// to produce this fixture at test time the way writeXzFile/
// writeZstdFile do. Embedded as base64 rather than shelling out to
// `bzip2` from the test itself, so the test stays hermetic regardless
// of whether that binary happens to be installed wherever it runs.
const bz2FixtureBase64 = "QlpoOTFBWSZTWVXpr+UAAAPZgAAQQAAQABNm0BAgACKaMmnpH6hAAA0q9CbgvywBYu5IpwoSCr01/KA="

func writeBz2File(t *testing.T, dir, name string) {
	t.Helper()
	data, err := base64.StdEncoding.DecodeString(bz2FixtureBase64)
	if err != nil {
		t.Fatalf("decode bz2 fixture: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
		t.Fatalf("WriteFile %s: %v", name, err)
	}
}

// TestDiscoverSupportsEveryCompressedExtension pins that Phase 1b
// closed the gap TestDiscoverMarksUnsupportedCompression used to pin
// for .xz specifically — every extension supportedCompressedExts lists
// is now Supported, not just .gz.
func TestDiscoverSupportsEveryCompressedExtension(t *testing.T) {
	dir := t.TempDir()
	writeXzFile(t, dir, "app.log.1.xz", "placeholder\n")
	writeZstdFile(t, dir, "app.log.2.zst", "placeholder\n")
	writeBz2File(t, dir, "app.log.3.bz2")

	groups, err := Discover(dir)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(groups) != 1 || len(groups[0].Files) != 3 {
		t.Fatalf("groups = %+v", groups)
	}
	for _, f := range groups[0].Files {
		if !f.Compressed || !f.Supported {
			t.Errorf("%s: Compressed/Supported = %v/%v, want true/true", f.Path, f.Compressed, f.Supported)
		}
	}
}

func TestOpenDecompressesGzip(t *testing.T) {
	dir := t.TempDir()
	writeGzipFile(t, dir, "access.log.1.gz", "hello from gzip\n")

	groups, err := Discover(dir)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	f := groups[0].Files[0]

	r, err := Open(f)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = r.Close() }()

	data, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if string(data) != "hello from gzip\n" {
		t.Errorf("data = %q", data)
	}
}

func TestOpenDecompressesXz(t *testing.T) {
	dir := t.TempDir()
	writeXzFile(t, dir, "access.log.1.xz", "hello from xz\n")

	groups, err := Discover(dir)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	r, err := Open(groups[0].Files[0])
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = r.Close() }()

	data, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if string(data) != "hello from xz\n" {
		t.Errorf("data = %q", data)
	}
}

func TestOpenDecompressesZstd(t *testing.T) {
	dir := t.TempDir()
	writeZstdFile(t, dir, "access.log.1.zst", "hello from zstd\n")

	groups, err := Discover(dir)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	r, err := Open(groups[0].Files[0])
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = r.Close() }()

	data, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if string(data) != "hello from zstd\n" {
		t.Errorf("data = %q", data)
	}
}

func TestOpenDecompressesBzip2(t *testing.T) {
	dir := t.TempDir()
	writeBz2File(t, dir, "access.log.1.bz2")

	groups, err := Discover(dir)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	r, err := Open(groups[0].Files[0])
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = r.Close() }()

	data, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if string(data) != "hello from bzip2\n" {
		t.Errorf("data = %q", data)
	}
}
