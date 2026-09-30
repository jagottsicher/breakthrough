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
		{"archive.7z", KindSevenZip, true},
		{"ARCHIVE.7Z", KindSevenZip, true},
		{"backup.rar", KindRar, true},
		{"BACKUP.RAR", KindRar, true},
	}
	for _, c := range cases {
		kind, ok := Classify(c.path)
		if ok != c.wantOK || (ok && kind != c.wantKind) {
			t.Errorf("Classify(%q) = (%v, %v), want (%v, %v)", c.path, kind, ok, c.wantKind, c.wantOK)
		}
	}
}

// FuzzClassify pins the one property TestClassify's own fixed cases
// can't: suffix matching can never depend on what comes *before* the
// suffix, for any real path at all — prepending arbitrary bytes ahead
// of p must never change whether it classifies as an archive, or which
// Kind it classifies as. Also a plain crash-safety net for the
// case-insensitive strings.ToLower call ahead of the real suffix
// check — arbitrary Unicode input is exactly what a hand-picked test
// corpus is least likely to include on its own.
func FuzzClassify(f *testing.F) {
	for _, seed := range []string{
		"archive.zip", "ARCHIVE.ZIP", "backup.tar.gz", "backup.tar.bz2",
		"backup.tar.xz", "notes.txt", "archive.7z", "", "no-extension-at-all",
		"a.tar.gz.part", "İstanbul.zip", "a.zip/inner.tar",
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, p string) {
		kind, ok := Classify(p)
		prefixed := "random-prefix-" + p
		kindPrefixed, okPrefixed := Classify(prefixed)
		if ok != okPrefixed || (ok && kind != kindPrefixed) {
			t.Errorf("Classify(%q) = (%v, %v), but Classify(%q) = (%v, %v) — suffix matching must be prefix-independent", p, kind, ok, prefixed, kindPrefixed, okPrefixed)
		}
	})
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

// requireTool mirrors internal/viewer's own helper of the same name —
// duplicated locally rather than exported and imported across packages
// for a single small skip-helper, the same convention that helper's
// own doc comment already documents.
func requireTool(t *testing.T, name string) {
	t.Helper()
	if _, err := exec.LookPath(name); err != nil {
		t.Skipf("%s not available in this environment: %v", name, err)
	}
}

// writeSourceTree materializes files (the same path -> content
// convention writeZip/writeTar already use, a "/"-suffixed key with
// empty content meaning an explicit, otherwise-empty directory) as
// real files under a fresh temp directory, returning that directory's
// own path — the staging area writeSevenZip/writeRar both archive
// from, since neither the real `7z` nor `rar` binary can be driven
// from an in-memory byte stream the way archive/zip.Writer/
// archive/tar.Writer can.
func writeSourceTree(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for p, content := range files {
		if content == "" && len(p) > 0 && p[len(p)-1] == '/' {
			if err := os.MkdirAll(filepath.Join(root, p), 0o755); err != nil {
				t.Fatal(err)
			}
			continue
		}
		full := filepath.Join(root, p)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// archiveTopLevelNames lists src's own immediate children — what
// writeSevenZip/writeRar both pass as the real `7z`/`rar` binary's own
// "what to add" arguments, run with Dir=src, so the archive's internal
// paths come out matching files' own keys exactly rather than being
// prefixed with src's own absolute temp-directory path.
func archiveTopLevelNames(t *testing.T, src string) []string {
	t.Helper()
	entries, err := os.ReadDir(src)
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, len(entries))
	for i, e := range entries {
		names[i] = e.Name()
	}
	return names
}

// writeSevenZip builds a real .7z fixture at dir/name via the actual
// `7z` binary — requires it on $PATH (see requireTool); this package
// has no pure-Go 7z writer to build one with instead (see archive.go's
// own package doc comment on why 7z/RAR are read via real binaries at
// all here).
func writeSevenZip(t *testing.T, dir, name string, files map[string]string) string {
	t.Helper()
	requireTool(t, "7z")
	src := writeSourceTree(t, files)
	full := filepath.Join(dir, name)
	args := append([]string{"a", "-y", full}, archiveTopLevelNames(t, src)...)
	cmd := exec.Command("7z", args...)
	cmd.Dir = src
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("7z a: %v: %s", err, out)
	}
	return full
}

// writeRar builds a real .rar fixture at dir/name via the actual `rar`
// binary (the proprietary creator tool — a separate program from
// `unrar`, which this package's own listRar/extractRar use to *read*
// one; see requireTool). Neither this package nor its production code
// ever needs `rar` itself — only these tests do, to have a real
// fixture to read back.
func writeRar(t *testing.T, dir, name string, files map[string]string) string {
	t.Helper()
	requireTool(t, "rar")
	src := writeSourceTree(t, files)
	full := filepath.Join(dir, name)
	args := append([]string{"a", "-y", full}, archiveTopLevelNames(t, src)...)
	cmd := exec.Command("rar", args...)
	cmd.Dir = src
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("rar a: %v: %s", err, out)
	}
	return full
}

func TestListSevenZip(t *testing.T) {
	requireTool(t, "7z")
	dir := t.TempDir()
	p := writeSevenZip(t, dir, "a.7z", map[string]string{
		"README.md":       "hello\n",
		"src/main.go":     "package main\n",
		"src/lib/util.go": "package lib\n",
	})

	entries, err := List(p)
	if err != nil {
		t.Fatal(err)
	}
	got := pathsOf(entries)
	want := []string{"README.md", "src", "src/lib", "src/lib/util.go", "src/main.go"}
	if !equalStrings(got, want) {
		t.Errorf("List(%q) paths = %v, want %v", p, got, want)
	}
	for _, e := range entries {
		switch e.Path {
		case "src", "src/lib":
			if !e.IsDir {
				t.Errorf("entry %q should be reported as a directory", e.Path)
			}
		case "README.md":
			if e.IsDir {
				t.Error(`entry "README.md" should not be reported as a directory`)
			}
			if want := int64(len("hello\n")); e.Size != want {
				t.Errorf("README.md size = %d, want %d", e.Size, want)
			}
		}
	}
}

func TestListSevenZipWithoutSevenZipBinary(t *testing.T) {
	requireTool(t, "7z") // build the fixture with a real 7z first
	dir := t.TempDir()
	p := writeSevenZip(t, dir, "a.7z", map[string]string{"file.txt": "hi\n"})

	t.Setenv("PATH", t.TempDir()) // isolate: no 7z/7za/7zr left to find

	if _, err := List(p); err == nil {
		t.Error("List on a .7z file with no 7-Zip binary on $PATH should fail")
	}
}

func TestListRar(t *testing.T) {
	requireTool(t, "unrar")
	dir := t.TempDir()
	p := writeRar(t, dir, "a.rar", map[string]string{
		"README.md":       "hello\n",
		"src/main.go":     "package main\n",
		"src/lib/util.go": "package lib\n",
	})

	entries, err := List(p)
	if err != nil {
		t.Fatal(err)
	}
	got := pathsOf(entries)
	want := []string{"README.md", "src", "src/lib", "src/lib/util.go", "src/main.go"}
	if !equalStrings(got, want) {
		t.Errorf("List(%q) paths = %v, want %v", p, got, want)
	}
	for _, e := range entries {
		switch e.Path {
		case "src", "src/lib":
			if !e.IsDir {
				t.Errorf("entry %q should be reported as a directory", e.Path)
			}
		case "README.md":
			if e.IsDir {
				t.Error(`entry "README.md" should not be reported as a directory`)
			}
			if want := int64(len("hello\n")); e.Size != want {
				t.Errorf("README.md size = %d, want %d", e.Size, want)
			}
		}
	}
}

func TestListRarWithoutUnrarBinary(t *testing.T) {
	requireTool(t, "unrar") // build the fixture with a real rar/unrar pair first
	dir := t.TempDir()
	p := writeRar(t, dir, "a.rar", map[string]string{"file.txt": "hi\n"})

	t.Setenv("PATH", t.TempDir()) // isolate: no unrar left to find

	if _, err := List(p); err == nil {
		t.Error("List on a .rar file with no unrar on $PATH should fail")
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
