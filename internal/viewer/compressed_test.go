package viewer

import (
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ulikunitz/xz"
)

func TestLooksLikeBareCompressedPath(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{"syslog.1.gz", true},
		{"kern.log.4.gz", true},
		{"SYSLOG.GZ", true}, // case-insensitive
		{"archive.xz", true},
		{"backup.tar.gz", false},
		{"backup.tgz", false},
		{"backup.tar.xz", false},
		{"backup.txz", false},
		{"plain.log", false},
		{"data.bz2", false}, // not asked for, not handled
	}
	for _, tt := range tests {
		if got := LooksLikeBareCompressedPath(tt.path); got != tt.want {
			t.Errorf("LooksLikeBareCompressedPath(%q) = %v, want %v", tt.path, got, tt.want)
		}
	}
}

func writeGzip(t *testing.T, path, content string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	gz := gzip.NewWriter(f)
	if _, err := gz.Write([]byte(content)); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
}

func writeXz(t *testing.T, path, content string) {
	t.Helper()
	var buf bytes.Buffer
	xw, err := xz.NewWriter(&buf)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := xw.Write([]byte(content)); err != nil {
		t.Fatal(err)
	}
	if err := xw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestDecompressForLookGzipText(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "syslog.1.gz")
	content := "Oct  7 10:00:00 host kernel: hello\n"
	writeGzip(t, path, content)

	tmpPath, cleanup, err := DecompressForLook(path, DefaultPreviewLimit)
	defer cleanup()
	if err != nil {
		t.Fatal(err)
	}
	if tmpPath == "" {
		t.Fatal("tmpPath is empty")
	}
	got, err := os.ReadFile(tmpPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != content {
		t.Errorf("decompressed content = %q, want %q", got, content)
	}

	// The temp file must be a plain, independently showable file: Load
	// is Look's own real next step on whatever DecompressForLook
	// handed it, so this pins that the two actually compose.
	result, err := Load(tmpPath, DefaultPreviewLimit)
	if err != nil {
		t.Fatal(err)
	}
	if result.Kind != KindText || result.Content != content {
		t.Errorf("Load(decompressed) = %+v, want KindText with %q", result, content)
	}
}

func TestDecompressForLookXzText(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "kern.log.4.xz")
	content := "Oct  7 10:00:00 host kernel: world\n"
	writeXz(t, path, content)

	tmpPath, cleanup, err := DecompressForLook(path, DefaultPreviewLimit)
	defer cleanup()
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(tmpPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != content {
		t.Errorf("decompressed content = %q, want %q", got, content)
	}
}

// TestDecompressForLookBinary pins the user's own explicit request:
// decompressed content that isn't text must still reach Load/Sniff
// exactly like any other file — DecompressForLook itself never treats
// "not text" as an error, that classification is entirely Load's own
// job on the result.
func TestDecompressForLookBinary(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "data.gz")
	content := "\x00\x01\x02binary\x00stuff"
	writeGzip(t, path, content)

	tmpPath, cleanup, err := DecompressForLook(path, DefaultPreviewLimit)
	defer cleanup()
	if err != nil {
		t.Fatal(err)
	}
	result, err := Load(tmpPath, DefaultPreviewLimit)
	if err != nil {
		t.Fatal(err)
	}
	if result.Kind != KindUnsupported {
		t.Errorf("Kind = %v, want KindUnsupported for binary content", result.Kind)
	}
}

// TestDecompressForLookBoundsOutputSize pins the limit argument against
// a decompressed-size-far-exceeds-compressed-size input — the same
// protection ReadPreview already gives an ordinary large file, just
// applied to the decompressed stream instead of the file on disk.
func TestDecompressForLookBoundsOutputSize(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "big.gz")
	// Highly compressible, so the compressed file itself stays tiny
	// while the decompressed content is well past a small limit.
	writeGzip(t, path, strings.Repeat("a", 10_000))

	tmpPath, cleanup, err := DecompressForLook(path, 100)
	defer cleanup()
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(tmpPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() > 101 { // limit+1, the same probe-byte convention ReadPreview uses
		t.Errorf("decompressed temp file is %d bytes, want at most limit+1 (101)", info.Size())
	}
}

func TestDecompressForLookCleanupRemovesTempFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "syslog.gz")
	writeGzip(t, path, "content")

	tmpPath, cleanup, err := DecompressForLook(path, DefaultPreviewLimit)
	if err != nil {
		t.Fatal(err)
	}
	cleanup()
	if _, err := os.Stat(tmpPath); !os.IsNotExist(err) {
		t.Errorf("temp file still exists after cleanup: %v", err)
	}
}

func TestDecompressForLookNotGzip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "not-actually-gzip.gz")
	if err := os.WriteFile(path, []byte("plain text, not gzip at all"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, cleanup, err := DecompressForLook(path, DefaultPreviewLimit)
	defer cleanup()
	if err == nil {
		t.Error("want an error for a .gz file that isn't actually gzip-compressed")
	}
}
