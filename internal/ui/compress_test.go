package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rivo/tview"
)

// TestArchiveFormatForRecognizesEveryExtension pins every extension
// archiveFormats claims to recognize, both directions of the table:
// the label a real user would see, and the exact suffix Extract has to
// match against an existing file.
func TestArchiveFormatForRecognizesEveryExtension(t *testing.T) {
	cases := map[string]string{
		"a.zip":         "zip (.zip)",
		"a.tar":         "tar (.tar)",
		"a.tar.gz":      "tar.gz (.tar.gz, .tgz)",
		"a.tgz":         "tar.gz (.tar.gz, .tgz)",
		"a.tar.bz2":     "tar.bz2 (.tar.bz2, .tbz2)",
		"a.tbz2":        "tar.bz2 (.tar.bz2, .tbz2)",
		"a.tbz":         "tar.bz2 (.tar.bz2, .tbz2)",
		"a.tar.xz":      "tar.xz (.tar.xz, .txz)",
		"a.txz":         "tar.xz (.tar.xz, .txz)",
		"a.tar.zst":     "tar.zst (.tar.zst, .tzst)",
		"a.tzst":        "tar.zst (.tar.zst, .tzst)",
		"/dir/A.ZIP":    "zip (.zip)", // case-insensitive, same convention internal/archive.Classify uses
		"a.tar.gz.part": "",           // a real suffix, just not one of ours
	}
	for path, wantLabel := range cases {
		format, ok := archiveFormatFor(path)
		if wantLabel == "" {
			if ok {
				t.Errorf("archiveFormatFor(%q) = %q, want no match", path, format.label)
			}
			continue
		}
		if !ok {
			t.Errorf("archiveFormatFor(%q): no match, want %q", path, wantLabel)
			continue
		}
		if format.label != wantLabel {
			t.Errorf("archiveFormatFor(%q) = %q, want %q", path, format.label, wantLabel)
		}
	}
}

// TestArchiveFormatForDoesNotConfuseTarWithACompressedTar pins that a
// bare ".tar" only ever matches the plain tar format — HasSuffix(".tar.gz",
// ".tar") is false (the string ends in ".gz", not ".tar"), but this is
// exactly the kind of off-by-one this table's own ordering could get
// wrong without a direct test.
func TestArchiveFormatForDoesNotConfuseTarWithACompressedTar(t *testing.T) {
	format, ok := archiveFormatFor("project.tar.gz")
	if !ok || format.ext != ".tar.gz" {
		t.Fatalf("archiveFormatFor(project.tar.gz) = %+v, %v, want the tar.gz format", format, ok)
	}
}

// TestZipCompressCommandBuildsTheRealZipInvocation pins the exact shell
// command line Compress hands to a real shell for the simplest format —
// a real, reproducible bug here would silently create garbage archives
// or corrupt filenames, never surfaced by anything else in this package.
func TestZipCompressCommandBuildsTheRealZipInvocation(t *testing.T) {
	format, ok := archiveFormatFor("x.zip")
	if !ok {
		t.Fatal("archiveFormatFor(x.zip) should match")
	}
	got := format.compress("'out.zip'", []string{"'a.txt'", "'b dir'"})
	want := "zip -r 'out.zip' 'a.txt' 'b dir'"
	if got != want {
		t.Errorf("compress command = %q, want %q", got, want)
	}
}

// TestTarGzCompressAndExtractCommandsPipeThroughGzipExplicitly pins the
// one design choice this whole format family depends on: never tar's
// own bundled "-z", always an explicit pipe through the real gzip
// binary — see archiveFormats' own doc comment for why (portable across
// GNU tar and bsdtar alike, and lets checkTools name gzip specifically
// if it's what's missing).
func TestTarGzCompressAndExtractCommandsPipeThroughGzipExplicitly(t *testing.T) {
	format, ok := archiveFormatFor("x.tar.gz")
	if !ok {
		t.Fatal("archiveFormatFor(x.tar.gz) should match")
	}
	gotCompress := format.compress("'out.tar.gz'", []string{"'a.txt'"})
	wantCompress := "tar -cf - 'a.txt' | gzip > 'out.tar.gz'"
	if gotCompress != wantCompress {
		t.Errorf("compress command = %q, want %q", gotCompress, wantCompress)
	}

	gotExtract := format.extract("'x.tar.gz'", "'/dest'")
	wantExtract := "gzip -dc 'x.tar.gz' | tar -x -C '/dest'"
	if gotExtract != wantExtract {
		t.Errorf("extract command = %q, want %q", gotExtract, wantExtract)
	}
}

// TestShellQuoteArgEscapesEmbeddedSingleQuotes pins the one character
// single-quoting can't represent directly — a filename containing a
// literal apostrophe is common enough (contractions, "it's") that this
// has to actually work, not just the easy, quote-free case.
func TestShellQuoteArgEscapesEmbeddedSingleQuotes(t *testing.T) {
	got := shellQuoteArg("it's a file.txt")
	want := `'it'"'"'s a file.txt'`
	if got != want {
		t.Errorf("shellQuoteArg = %q, want %q", got, want)
	}
}

// TestCheckToolsReportsEveryMissingToolByName pins that a real,
// findable binary is never listed as missing, and a made-up one always
// is, by name — the whole point of checkTools over letting a real
// "command not found" from deep inside a shell pipeline speak for
// itself.
func TestCheckToolsReportsEveryMissingToolByName(t *testing.T) {
	if err := checkTools([]string{"sh"}); err != nil {
		t.Errorf("checkTools([sh]) = %v, want nil (sh must exist for the shell this app already depends on elsewhere)", err)
	}

	err := checkTools([]string{"sh", "definitely-not-a-real-binary-xyz"})
	if err == nil {
		t.Fatal("checkTools should report the missing binary")
	}
	if !strings.Contains(err.Error(), "definitely-not-a-real-binary-xyz") {
		t.Errorf("error = %q, want it to name the missing binary", err.Error())
	}
	if strings.Contains(err.Error(), "sh,") || strings.Contains(err.Error(), "sh ") {
		t.Errorf("error = %q, should not also list \"sh\", which does exist", err.Error())
	}
}

// TestDefaultCompressOutputNameSingleTarget pins the common case: one
// selected file or directory names the archive after itself.
func TestDefaultCompressOutputNameSingleTarget(t *testing.T) {
	got := defaultCompressOutputName([]string{"/home/jens/project"}, "/home/jens")
	if got != "project" {
		t.Errorf("got %q, want %q", got, "project")
	}
}

// TestDefaultCompressOutputNameMultipleTargetsUsesCurrentDir pins the
// "compress several items at once" case: named after the directory
// they live in, the same convention a real GUI file manager's own
// "compress N items" already follows.
func TestDefaultCompressOutputNameMultipleTargetsUsesCurrentDir(t *testing.T) {
	got := defaultCompressOutputName([]string{"/home/jens/a.txt", "/home/jens/b.txt"}, "/home/jens/myproject")
	if got != "myproject" {
		t.Errorf("got %q, want %q", got, "myproject")
	}
}

// TestDefaultCompressOutputNameFallsBackAtFilesystemRoot pins the one
// place a directory's own base name isn't meaningful at all.
func TestDefaultCompressOutputNameFallsBackAtFilesystemRoot(t *testing.T) {
	got := defaultCompressOutputName([]string{"/a.txt", "/b.txt"}, "/")
	if got != "archive" {
		t.Errorf("got %q, want %q", got, "archive")
	}
}

// TestOpenCompressPopulatesTargetsAndDefaultName pins openCompress' own
// basic contract against a real fixture: it opens the dialog and seeds
// its own mirrors from the real current selection.
func TestOpenCompressPopulatesTargetsAndDefaultName(t *testing.T) {
	r, dir, file := newTestRootWithFile(t)
	_ = dir

	r.openCompress()

	if r.activePage != compressPage {
		t.Fatalf("activePage = %q, want the Compress dialog", r.activePage)
	}
	if len(r.compressTargets) != 1 || r.compressTargets[0] != file {
		t.Errorf("compressTargets = %v, want [%q]", r.compressTargets, file)
	}
	if r.compressOutputName != filepath.Base(file) {
		t.Errorf("compressOutputName = %q, want %q", r.compressOutputName, filepath.Base(file))
	}
}

// TestOpenCompressRefusedOnRemotePanel pins that Compress, which has no
// remote archive-creation path yet, says so plainly instead of trying
// (and failing confusingly) against a path that only exists on this
// machine, not the connection.
func TestOpenCompressRefusedOnRemotePanel(t *testing.T) {
	r := newTestRemoteRoot(t)

	r.openCompress()

	if r.activePage != errorPage {
		t.Fatalf("activePage = %q, want the error overlay", r.activePage)
	}
	if !strings.Contains(r.errorView.GetText(true), "remote") {
		t.Errorf("error = %q, want it to mention remote panels aren't supported", r.errorView.GetText(true))
	}
}

// TestRunCompressRefusesAnEmptyOutputName pins that Compress never
// silently runs with a blank name (which would otherwise build a
// shell command archiving into a bare extension like ".zip").
func TestRunCompressRefusesAnEmptyOutputName(t *testing.T) {
	r, _, _ := newTestRootWithFile(t)
	r.openCompress()
	r.compressOutputName = "   "

	r.runCompress()

	if r.activePage != errorPage {
		t.Fatalf("activePage = %q, want the error overlay", r.activePage)
	}
}

// TestRunCompressRefusesAnAlreadyExistingOutputFile pins the
// "never silently overwrite" guarantee: a name that already exists in
// the current directory is refused outright, before ever reaching a
// real shell command.
func TestRunCompressRefusesAnAlreadyExistingOutputFile(t *testing.T) {
	r, dir, _ := newTestRootWithFile(t)
	r.openCompress()
	r.compressOutputName = "already-there"
	r.compressFormatIndex = 0 // zip
	if err := os.WriteFile(filepath.Join(dir, "already-there.zip"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	r.runCompress()

	if r.activePage != errorPage {
		t.Fatalf("activePage = %q, want the error overlay", r.activePage)
	}
	// wrapText (see showError) has already broken the message across
	// several lines by the time it reaches errorView — normalized back
	// to a single line before comparing, the same reason
	// TestCutCurrentSelectionRefusedInsideArchive's own identical
	// pattern does.
	got := strings.ReplaceAll(r.errorView.GetText(true), "\n", " ")
	if !strings.Contains(got, "already exists") {
		t.Errorf("error = %q, want it to mention the name already exists", got)
	}
}

// TestExtractCurrentArchiveRefusesAnUnrecognizedFile pins that
// Extract, invoked on a plain file that isn't any recognized archive
// format, reports that plainly rather than trying (and failing) to
// run a real extractor on it.
func TestExtractCurrentArchiveRefusesAnUnrecognizedFile(t *testing.T) {
	r, _, _ := newTestRootWithFile(t) // "a.txt", not an archive

	r.extractCurrentArchive(false)

	if r.activePage != errorPage {
		t.Fatalf("activePage = %q, want the error overlay", r.activePage)
	}
	got := strings.ReplaceAll(r.errorView.GetText(true), "\n", " ")
	if !strings.Contains(got, "not a recognized archive") {
		t.Errorf("error = %q, want it to say so", got)
	}
}

// TestExtractCurrentArchiveRefusedOnRemotePanel mirrors
// TestOpenCompressRefusedOnRemotePanel for Extract.
func TestExtractCurrentArchiveRefusedOnRemotePanel(t *testing.T) {
	r := newTestRemoteRoot(t)

	r.extractCurrentArchive(false)

	if r.activePage != errorPage {
		t.Fatalf("activePage = %q, want the error overlay", r.activePage)
	}
	if !strings.Contains(r.errorView.GetText(true), "remote") {
		t.Errorf("error = %q, want it to mention remote archives aren't supported", r.errorView.GetText(true))
	}
}

// TestMenuTargetIsArchiveMatchesOnlyRealArchiveFiles pins the context
// menu's own visibility gate for "Extract"/"Extract, delete original" —
// hidden for a directory, hidden for a plain file, shown only once the
// cursor is actually on something archiveFormatFor recognizes.
func TestMenuTargetIsArchiveMatchesOnlyRealArchiveFiles(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "plain.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "bundle.zip"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "sub.zip"), 0o755); err != nil { // a directory that merely happens to be named like a zip
		t.Fatal(err)
	}
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}

	setTarget := func(name string) {
		row, ref, found := 0, rowRef{}, false
		for i := 0; ; i++ {
			candidate, ok := r.panel.rowRef(i)
			if !ok {
				break
			}
			if candidate.name == name {
				row, ref, found = i, candidate, true
				break
			}
		}
		if !found {
			t.Fatalf("setup: no row named %q", name)
		}
		r.target, r.targetRow = ref.path, row
	}

	setTarget("plain.txt")
	if menuTargetIsArchive(r) {
		t.Error("a plain, non-archive file should not count as an archive")
	}

	setTarget("sub.zip")
	if menuTargetIsArchive(r) {
		t.Error("a directory should never count as an archive, regardless of its own name")
	}

	setTarget("bundle.zip")
	if !menuTargetIsArchive(r) {
		t.Error("a real .zip file should count as an archive")
	}
}

// TestDeleteExtractedArchiveMovesItToTrashOnSuccess pins the default,
// reversible path: a real archive file, a real, working trash — moved,
// not destroyed.
func TestDeleteExtractedArchiveMovesItToTrashOnSuccess(t *testing.T) {
	r, dir, _ := newTestRootWithFile(t)
	archivePath := filepath.Join(dir, "bundle.zip")
	if err := os.WriteFile(archivePath, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	r.deleteExtractedArchive(archivePath)

	if _, err := os.Stat(archivePath); !os.IsNotExist(err) {
		t.Errorf("Stat(%s) after delete: err = %v, want a not-exist error (moved to Trash)", archivePath, err)
	}
	if r.activePage == errorPage {
		t.Errorf("deleteExtractedArchive reported an error on the success path: %q", r.errorView.GetText(true))
	}
}

// TestDeleteExtractedArchiveAsksBeforeAHardDeleteWhenTrashFails pins
// the user's own explicit design: if moving to Trash fails outright,
// this must never silently leave the archive in place, and never
// silently hard-delete it either — it has to ask first, naming plainly
// that the fallback is permanent. Trash failure is forced the simplest
// possible way: archivePath itself doesn't exist on disk, so
// fsops.MoveToTrash's own initial stat fails before it ever touches
// the trash directory.
func TestDeleteExtractedArchiveAsksBeforeAHardDeleteWhenTrashFails(t *testing.T) {
	r, dir, _ := newTestRootWithFile(t)
	missingArchive := filepath.Join(dir, "gone-already.zip")

	r.deleteExtractedArchive(missingArchive)

	if r.activePage != confirmPage {
		t.Fatalf("activePage = %q, want the confirmation dialog", r.activePage)
	}
	got := r.confirmDialogTitleBar.GetText(true)
	if !strings.Contains(got, "Trash failed") || !strings.Contains(got, "delete it completely") {
		t.Errorf("confirmation message = %q, want it to name the Trash failure and the permanent-delete fallback", got)
	}
}
