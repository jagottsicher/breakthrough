package archive

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/bzip2"
	"compress/gzip"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"testing"

	"github.com/ulikunitz/xz"
)

func TestClassify(t *testing.T) {
	cases := []struct {
		path     string
		wantKind Kind
		wantOK   bool
	}{
		{"archive.zip", KindZip, true},
		{"ARCHIVE.ZIP", KindZip, true},
		{"backup.tar", KindTar, true},
		{"backup.tar.gz", KindTar, true},
		{"backup.tgz", KindTar, true},
		{"backup.tar.bz2", KindTar, true},
		{"backup.tbz2", KindTar, true},
		{"backup.tbz", KindTar, true},
		{"backup.tar.xz", KindTar, true},
		{"backup.txz", KindTar, true},
		{"notes.txt", 0, false},
		{"archive.7z", 0, false},
	}
	for _, c := range cases {
		kind, ok := Classify(c.path)
		if ok != c.wantOK || (ok && kind != c.wantKind) {
			t.Errorf("Classify(%q) = (%v, %v), want (%v, %v)", c.path, kind, ok, c.wantKind, c.wantOK)
		}
	}
}

// writeZip builds a small zip fixture at dir/name.zip containing files
// (path -> content) and returns its full path. A path ending in "/"
// with an empty content adds an explicit, otherwise-empty directory
// entry — used only where a test specifically needs one; every other
// directory in these fixtures is left implicit, matching how a real
// zip most commonly looks (see Children's own doc comment).
func writeZip(t *testing.T, dir, name string, files map[string]string) string {
	t.Helper()
	full := filepath.Join(dir, name)
	f, err := os.Create(full)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()

	zw := zip.NewWriter(f)
	for path, content := range files {
		if content == "" && len(path) > 0 && path[len(path)-1] == '/' {
			if _, err := zw.Create(path); err != nil {
				t.Fatal(err)
			}
			continue
		}
		w, err := zw.Create(path)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return full
}

// writeTar builds a plain (uncompressed) .tar fixture — the shared body
// behind writeTarGz/writeTarBz2/writeTarXz below, which each just wrap
// this same byte stream in their own compression before writing it out.
func writeTar(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	for path, content := range files {
		if content == "" && len(path) > 0 && path[len(path)-1] == '/' {
			if err := tw.WriteHeader(&tar.Header{Name: path, Typeflag: tar.TypeDir, Mode: 0o755}); err != nil {
				t.Fatal(err)
			}
			continue
		}
		if err := tw.WriteHeader(&tar.Header{Name: path, Typeflag: tar.TypeReg, Mode: 0o644, Size: int64(len(content))}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func writeTarPlain(t *testing.T, dir, name string, files map[string]string) string {
	t.Helper()
	full := filepath.Join(dir, name)
	if err := os.WriteFile(full, writeTar(t, files), 0o644); err != nil {
		t.Fatal(err)
	}
	return full
}

func writeTarGz(t *testing.T, dir, name string, files map[string]string) string {
	t.Helper()
	full := filepath.Join(dir, name)
	f, err := os.Create(full)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	gw := gzip.NewWriter(f)
	if _, err := gw.Write(writeTar(t, files)); err != nil {
		t.Fatal(err)
	}
	if err := gw.Close(); err != nil {
		t.Fatal(err)
	}
	return full
}

func writeTarXz(t *testing.T, dir, name string, files map[string]string) string {
	t.Helper()
	full := filepath.Join(dir, name)
	f, err := os.Create(full)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	xw, err := xz.NewWriter(f)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := xw.Write(writeTar(t, files)); err != nil {
		t.Fatal(err)
	}
	if err := xw.Close(); err != nil {
		t.Fatal(err)
	}
	return full
}

// pathsOf sorts entries' own Path fields for an order-independent
// comparison — List makes no ordering promise of its own (see its doc
// comment), so every test here checks the *set* of paths, not a
// specific sequence.
func pathsOf(entries []Entry) []string {
	names := make([]string, len(entries))
	for i, e := range entries {
		names[i] = e.Path
	}
	sort.Strings(names)
	return names
}

func TestListZip(t *testing.T) {
	dir := t.TempDir()
	p := writeZip(t, dir, "a.zip", map[string]string{
		"README.md":       "hello\n",
		"src/main.go":     "package main\n",
		"src/lib/util.go": "package lib\n",
	})

	entries, err := List(p)
	if err != nil {
		t.Fatal(err)
	}
	got := pathsOf(entries)
	want := []string{"README.md", "src/lib/util.go", "src/main.go"}
	if !equalStrings(got, want) {
		t.Errorf("List(%q) paths = %v, want %v", p, got, want)
	}

	for _, e := range entries {
		if e.Path == "README.md" && (e.IsDir || e.Size != int64(len("hello\n"))) {
			t.Errorf("README.md entry = %+v, want a 6-byte file", e)
		}
	}
}

func TestListZipWithExplicitDirectoryEntry(t *testing.T) {
	dir := t.TempDir()
	p := writeZip(t, dir, "a.zip", map[string]string{
		"src/":        "",
		"src/main.go": "package main\n",
	})

	entries, err := List(p)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range entries {
		if e.Path == "src" {
			found = true
			if !e.IsDir {
				t.Errorf("src entry = %+v, want IsDir", e)
			}
		}
	}
	if !found {
		t.Errorf("List(%q) = %v, missing explicit \"src\" directory entry", p, entries)
	}
}

func TestListTarPlain(t *testing.T) {
	dir := t.TempDir()
	p := writeTarPlain(t, dir, "a.tar", map[string]string{
		"file.txt":   "hi\n",
		"dir/nested": "there\n",
	})

	entries, err := List(p)
	if err != nil {
		t.Fatal(err)
	}
	got := pathsOf(entries)
	want := []string{"dir/nested", "file.txt"}
	if !equalStrings(got, want) {
		t.Errorf("List(%q) paths = %v, want %v", p, got, want)
	}
}

func TestListTarGz(t *testing.T) {
	dir := t.TempDir()
	p := writeTarGz(t, dir, "a.tar.gz", map[string]string{"file.txt": "hi\n"})

	entries, err := List(p)
	if err != nil {
		t.Fatal(err)
	}
	if got := pathsOf(entries); !equalStrings(got, []string{"file.txt"}) {
		t.Errorf("List(%q) paths = %v, want [file.txt]", p, got)
	}
}

func TestListTarBz2(t *testing.T) {
	dir := t.TempDir()
	full := filepath.Join(dir, "a.tar.bz2")
	// compress/bzip2 is decode-only (verified against its own package
	// doc, not assumed) — bzip2, unlike gzip/xz, has no Go stdlib
	// encoder at all, so this fixture is produced with the bzip2
	// command-line tool instead of Go code the way writeTarGz/writeTarXz
	// build their own. Skips cleanly wherever bzip2(1) isn't installed
	// rather than failing the whole suite over a missing dev tool.
	if _, err := exec.LookPath("bzip2"); err != nil {
		t.Skip("bzip2 not installed, skipping")
	}
	rawTar := writeTar(t, map[string]string{"file.txt": "hi\n"})
	cmd := exec.Command("bzip2", "-c")
	cmd.Stdin = bytes.NewReader(rawTar)
	compressed, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, compressed, 0o644); err != nil {
		t.Fatal(err)
	}

	entries, err := List(full)
	if err != nil {
		t.Fatal(err)
	}
	if got := pathsOf(entries); !equalStrings(got, []string{"file.txt"}) {
		t.Errorf("List(%q) paths = %v, want [file.txt]", full, got)
	}
	// Also confirm compress/bzip2 itself decodes the fixture correctly,
	// independent of List/openTarStream, as a sanity check on the
	// fixture-generation step above.
	decoded, err := io.ReadAll(bzip2.NewReader(bytes.NewReader(compressed)))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(decoded, rawTar) {
		t.Error("bzip2 fixture didn't decode back to the original tar bytes")
	}
}

func TestListTarXz(t *testing.T) {
	dir := t.TempDir()
	p := writeTarXz(t, dir, "a.tar.xz", map[string]string{"file.txt": "hi\n"})

	entries, err := List(p)
	if err != nil {
		t.Fatal(err)
	}
	if got := pathsOf(entries); !equalStrings(got, []string{"file.txt"}) {
		t.Errorf("List(%q) paths = %v, want [file.txt]", p, got)
	}
}

func TestListUnsupportedFormat(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "notes.txt")
	if err := os.WriteFile(p, []byte("hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := List(p); err == nil {
		t.Error("List on a non-archive file should fail")
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
