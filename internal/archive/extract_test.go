package archive

import (
	"archive/zip"
	"os"
	"path/filepath"
	"sort"
	"testing"
)

// listExtractedFiles walks dir and returns every regular file's own
// path relative to dir, sorted — used to check Extract's own output
// shape without caring which order os.ReadDir/filepath.WalkDir happens
// to visit siblings in.
func listExtractedFiles(t *testing.T, dir string) []string {
	t.Helper()
	var files []string
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		files = append(files, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(files)
	return files
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// TestResolveDestRefusesEscapingPaths is resolveDest's own direct unit
// test — belt-and-suspenders for the guard TestExtractNeverEscapesDestDir
// confirms Extract's own callers never actually need to rely on, since
// relativeDest already neutralizes a traversal before resolveDest ever
// sees one that way. Called directly here so the guard itself stays
// covered on its own merits, in case a future caller of destFile/
// ensureDir ever reaches it with less-sanitized input than
// extractZip/extractTar's own always-Clean'd entryPath.
func TestResolveDestRefusesEscapingPaths(t *testing.T) {
	base := t.TempDir()
	if _, err := resolveDest(base, "../../etc/passwd"); err == nil {
		t.Error("resolveDest should refuse a name that climbs above base")
	}
	if got, err := resolveDest(base, "sub/file.txt"); err != nil {
		t.Errorf("resolveDest(sub/file.txt) failed: %v", err)
	} else if want := filepath.Join(base, "sub", "file.txt"); got != want {
		t.Errorf("resolveDest(sub/file.txt) = %q, want %q", got, want)
	}
}

func TestExtractZipFileAndDirectory(t *testing.T) {
	src := t.TempDir()
	zipPath := writeZip(t, src, "a.zip", map[string]string{
		"README.md":       "hello\n",
		"src/main.go":     "package main\n",
		"src/lib/util.go": "package lib\n",
	})

	entries, err := List(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	// Simulate browsing into "src" (Children, the same call load() uses
	// to render that level) and marking both its own children — exactly
	// the shape internal/ui's archiveExtractionFor hands Extract: real
	// Entry values from the level currently on screen, not raw List
	// output (a flat, unleveled listing — see List's own doc comment).
	members := Children(entries, "src")
	if len(members) != 2 {
		t.Fatalf("setup: expected 2 members (lib synthesized + main.go), got %d: %v", len(members), members)
	}

	dest := t.TempDir()
	if err := Extract(zipPath, members, dest); err != nil {
		t.Fatal(err)
	}

	got := listExtractedFiles(t, dest)
	want := []string{"lib/util.go", "main.go"}
	if !equalStrings(got, want) {
		t.Errorf("extracted files = %v, want %v", got, want)
	}
	if got := readFile(t, filepath.Join(dest, "main.go")); got != "package main\n" {
		t.Errorf("main.go content = %q, want %q", got, "package main\n")
	}
	if got := readFile(t, filepath.Join(dest, "lib/util.go")); got != "package lib\n" {
		t.Errorf("lib/util.go content = %q, want %q", got, "package lib\n")
	}
}

// TestExtractOverwritesAPreviousReadOnlyExtractionOfTheSameMember pins
// a real, reported bug: a member whose own stored mode has no owner
// write bit (a license file marked read-only, say — common enough,
// and Extract's own doc comment already documents "silently
// overwritten" as the intended behavior for a name already present)
// used to fail with "permission denied" extracting it a *second* time
// on top of its own previous copy, because os.OpenFile's O_TRUNC was
// asked to reopen an *existing* file for writing, which has to satisfy
// that file's own current permissions — unlike creating a brand new
// one, which doesn't.
func TestExtractOverwritesAPreviousReadOnlyExtractionOfTheSameMember(t *testing.T) {
	src := t.TempDir()
	zipPath := filepath.Join(src, "a.zip")
	f, err := os.Create(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	hdr := &zip.FileHeader{Name: "readonly.txt", Method: zip.Deflate}
	hdr.SetMode(0o444) // no write bit at all
	w, err := zw.CreateHeader(hdr)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte("second\n")); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	entries, err := List(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	members := Children(entries, "")

	dest := t.TempDir()
	// A previous extraction of the same member already left behind a
	// read-only copy — exactly what a first, successful Extract call
	// of this same archive would produce.
	existing := filepath.Join(dest, "readonly.txt")
	if err := os.WriteFile(existing, []byte("first\n"), 0o444); err != nil {
		t.Fatal(err)
	}

	if err := Extract(zipPath, members, dest); err != nil {
		t.Fatalf("Extract: %v, want the pre-existing read-only file replaced, not refused", err)
	}
	if got := readFile(t, existing); got != "second\n" {
		t.Errorf("content = %q, want the archive's own content to have replaced the old file", got)
	}
}

func TestExtractZipWholeArchiveAtRoot(t *testing.T) {
	src := t.TempDir()
	zipPath := writeZip(t, src, "a.zip", map[string]string{
		"README.md":   "hello\n",
		"src/main.go": "package main\n",
	})
	entries, err := List(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	// Extracting from the archive's own root (Children(entries, "")):
	// README.md and the (synthesized) "src" directory.
	members := Children(entries, "")

	dest := t.TempDir()
	if err := Extract(zipPath, members, dest); err != nil {
		t.Fatal(err)
	}
	got := listExtractedFiles(t, dest)
	want := []string{"README.md", "src/main.go"}
	if !equalStrings(got, want) {
		t.Errorf("extracted files = %v, want %v", got, want)
	}
}

func TestExtractTar(t *testing.T) {
	src := t.TempDir()
	tarPath := writeTarPlain(t, src, "a.tar", map[string]string{
		"a/b/c.txt": "deep\n",
	})
	entries, err := List(tarPath)
	if err != nil {
		t.Fatal(err)
	}
	// The archive's own root has exactly one child: the synthesized "a"
	// directory (List itself never synthesizes one — see its own doc
	// comment — only Children does, at whichever level is asked for).
	// Extracting that one member recursively covers everything nested
	// under it.
	members := Children(entries, "")
	dest := t.TempDir()
	if err := Extract(tarPath, members, dest); err != nil {
		t.Fatal(err)
	}
	got := listExtractedFiles(t, dest)
	want := []string{"a/b/c.txt"}
	if !equalStrings(got, want) {
		t.Errorf("extracted files = %v, want %v", got, want)
	}
}

func TestExtractCreatesEmptyDirectory(t *testing.T) {
	src := t.TempDir()
	zipPath := writeZip(t, src, "a.zip", map[string]string{
		"empty/": "",
	})
	entries, err := List(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	dest := t.TempDir()
	if err := Extract(zipPath, entries, dest); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(dest, "empty"))
	if err != nil {
		t.Fatalf("empty directory wasn't created: %v", err)
	}
	if !info.IsDir() {
		t.Error("empty should have been extracted as a directory")
	}
}

// TestExtractNeverEscapesDestDir is the "zip-slip" regression pin: a
// member whose own stored name tries to climb above the destination
// directory — not something a well-formed zip/tar from a trusted tool
// ever produces, but Extract must never let it land outside dest
// regardless. relativeDest already neutralizes this before
// resolveDest's own belt-and-suspenders check would ever even see a
// traversal (see extract.go's own doc comment on how Clean-before-match
// makes that so): a malicious top-level "../../etc/evil.txt" member
// resolves to plain "evil.txt", landing safely inside dest under its
// own base name — the same "destination named after the base name"
// contract every ordinary member already follows (see Extract's own
// doc comment) — rather than erroring out or, worse, silently escaping.
func TestExtractNeverEscapesDestDir(t *testing.T) {
	src := t.TempDir()
	zipPath := writeZip(t, src, "evil.zip", map[string]string{
		"../../etc/evil.txt": "pwned\n",
	})
	entries, err := List(zipPath)
	if err != nil {
		t.Fatal(err)
	}

	dest := t.TempDir()
	if err := Extract(zipPath, entries, dest); err != nil {
		t.Fatalf("Extract of a traversal member should still succeed, safely neutralized: %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(dest, "evil.txt")); statErr != nil {
		t.Errorf("expected the traversal member to land safely at dest/evil.txt: %v", statErr)
	}
	escaped := filepath.Join(filepath.Dir(filepath.Dir(dest)), "evil.txt")
	if _, statErr := os.Stat(escaped); statErr == nil {
		t.Errorf("Extract wrote outside the destination directory, at %s", escaped)
	}
}
