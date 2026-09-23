package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rivo/tview"

	"github.com/jagottsicher/breakthrough/internal/activitylog"
	"github.com/jagottsicher/breakthrough/internal/fsops"
	"github.com/jagottsicher/breakthrough/internal/remotefs"
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

// TestCompressTargetNameUsesDotForTheCurrentDirectoryItself pins a
// real, live-tested regression: compressing the current directory as a
// whole (reached by cursor-on-".." — see Panel.CurrentRowPath) used to
// pass the directory's own base name as the zip/tar target, which
// zip/tar then refused to find *inside* that same directory ("zip
// warning: name not matched"). "." is the same relative name a real
// shell prompt would use here instead.
func TestCompressTargetNameUsesDotForTheCurrentDirectoryItself(t *testing.T) {
	if got := compressTargetName("/home/jens/project", "/home/jens/project"); got != "." {
		t.Errorf("compressTargetName(destDir, destDir) = %q, want %q", got, ".")
	}
}

// TestCompressTargetNameUsesTheBaseNameOtherwise pins the ordinary
// case: an entry actually inside destDir names itself normally.
func TestCompressTargetNameUsesTheBaseNameOtherwise(t *testing.T) {
	if got := compressTargetName("/home/jens/project/report.txt", "/home/jens/project"); got != "report.txt" {
		t.Errorf("compressTargetName = %q, want %q", got, "report.txt")
	}
}

// TestRunCompressOnTheCurrentDirectoryItselfBuildsAWorkingCommand is
// the end-to-end regression test for the bug
// TestCompressTargetNameUsesDotForTheCurrentDirectoryItself pins in
// isolation: focusing ".." (the real, live-tested trigger — see
// Panel.CurrentRowPath) and running Compress must build a command zip
// can actually satisfy — inspected on the started *exec.Cmd's own Args
// rather than waited out to completion, the same restraint
// TestReallyStartCompressJobRunsInThePanelsOwnDirectory already takes
// in compressjob_test.go (calling Wait here too would race the
// background job's own goroutine, which already calls it — see that
// test's own doc comment).
func TestRunCompressOnTheCurrentDirectoryItselfBuildsAWorkingCommand(t *testing.T) {
	r, _, _ := newTestRootWithFile(t)
	r.panel.focusRow(0) // the ".." row
	r.openCompress()
	r.compressOutputName = "whole-dir"
	r.compressFormatIndex = 0 // zip

	r.runCompress()

	job := r.compressJob
	if job == nil {
		t.Fatal("runCompress did not start a background job")
	}
	defer r.cancelCompressJob()

	command := strings.Join(job.cmd.Args, " ")
	if !strings.Contains(command, "zip -r '") || !strings.HasSuffix(command, "whole-dir.zip' '.'") {
		t.Errorf("command = %q, want it to compress '.' into whole-dir.zip rather than the directory's own base name", command)
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

func TestDeleteExtractedArchiveLogsAnActionOnSuccess(t *testing.T) {
	r, dir, _ := newTestRootWithFile(t)
	archivePath := filepath.Join(dir, "bundle.zip")
	if err := os.WriteFile(archivePath, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	readLog := attachTestActivityLog(t, r)

	r.deleteExtractedArchive(archivePath)

	got := readLog()
	if !strings.Contains(got, string(activitylog.CategoryFileOps)) || !strings.Contains(got, "moved extracted archive") {
		t.Errorf("log = %q, want a fileops entry about the archive moving to trash", got)
	}
}

// TestDeleteExtractedArchiveLogsAnActionAfterAConfirmedHardDelete forces
// the Trash step to fail (a plain file sitting where the trash's own
// "trash" directory needs to be created — see ensureTrashSkeleton) while
// leaving the real archive file in place, unlike
// TestDeleteExtractedArchiveAsksBeforeAHardDeleteWhenTrashFails' own
// "archive already gone" trick, which would make the confirmed hard
// delete itself fail too instead of actually succeeding.
func TestDeleteExtractedArchiveLogsAnActionAfterAConfirmedHardDelete(t *testing.T) {
	r, dir, _ := newTestRootWithFile(t)
	r.settings.TrashPersistent = true
	dataHome := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dataHome)
	if err := os.MkdirAll(filepath.Join(dataHome, "breakthrough"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dataHome, "breakthrough", "trash"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	archivePath := filepath.Join(dir, "bundle.zip")
	if err := os.WriteFile(archivePath, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	r.deleteExtractedArchive(archivePath)
	if r.activePage != confirmPage {
		t.Fatalf("setup: activePage = %q, want the confirmation dialog", r.activePage)
	}
	readLog := attachTestActivityLog(t, r)

	r.confirmDialog.SetCurrentItem(0)
	r.resolvePurgeConfirmByCurrentFocus(t)

	got := readLog()
	if !strings.Contains(got, string(activitylog.CategoryFileOps)) || !strings.Contains(got, "permanently deleted extracted archive") {
		t.Errorf("log = %q, want a fileops entry about the confirmed hard delete", got)
	}
}

// newTestFakeRemote builds a fakeRemoteClient with an already-
// initialized entries map — Mkdir/Create both assign into it directly
// (see fakeRemoteClient's own Close/Mkdir), which panics on a nil map,
// unlike the zero-value-friendly local filesystem this package's own
// remote tests otherwise mirror.
func newTestFakeRemote(root string) *fakeRemoteClient {
	return &fakeRemoteClient{root: root, entries: map[string][]fsops.Entry{root: nil}}
}

// TestUploadCompressedFileUploadsAndRemovesTheLocalStagingCopy pins
// uploadCompressedFile's own whole contract: the real bytes land at
// destPath on the fake remote client, and the local staging file is
// gone afterward regardless — its only reason to exist was to get
// uploaded.
func TestUploadCompressedFileUploadsAndRemovesTheLocalStagingCopy(t *testing.T) {
	dir := t.TempDir()
	localPath := filepath.Join(dir, "archive.zip")
	if err := os.WriteFile(localPath, []byte("zip bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	remote := newTestFakeRemote("/remote")

	if err := uploadCompressedFile(localPath, remote, "/remote/archive.zip"); err != nil {
		t.Fatalf("uploadCompressedFile: %v", err)
	}

	if got := string(remote.content["/remote/archive.zip"]); got != "zip bytes" {
		t.Errorf("uploaded content = %q, want %q", got, "zip bytes")
	}
	if _, err := os.Stat(localPath); !os.IsNotExist(err) {
		t.Errorf("Stat(localPath) after upload: err = %v, want a not-exist error", err)
	}
}

// TestUploadCompressedFileRefusesAnExistingRemoteFileAndStillCleansUp
// pins copyTransferItem's own "never silently overwrite" policy
// reaching all the way through this wiring, and that the local staging
// copy is still removed even when the upload itself fails — leaving it
// behind would just be an orphaned temp file nobody will ever act on
// again.
func TestUploadCompressedFileRefusesAnExistingRemoteFileAndStillCleansUp(t *testing.T) {
	dir := t.TempDir()
	localPath := filepath.Join(dir, "archive.zip")
	if err := os.WriteFile(localPath, []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}
	remote := newTestFakeRemote("/remote")
	remote.entries["/remote"] = []fsops.Entry{{Name: "archive.zip", Type: fsops.TypeFile}}
	remote.content = map[string][]byte{"/remote/archive.zip": []byte("already there")}

	err := uploadCompressedFile(localPath, remote, "/remote/archive.zip")

	if err == nil {
		t.Fatal("uploadCompressedFile should refuse an already-existing remote file")
	}
	if got := string(remote.content["/remote/archive.zip"]); got != "already there" {
		t.Errorf("existing remote content = %q, want it untouched", got)
	}
	if _, statErr := os.Stat(localPath); !os.IsNotExist(statErr) {
		t.Errorf("Stat(localPath) after a failed upload: err = %v, want a not-exist error", statErr)
	}
}

// TestUploadExtractedTreeUploadsEveryTopLevelEntryAndRemovesTempDir
// pins uploadExtractedTree's own whole contract for the common,
// no-conflict case: every entry directly inside tempDir lands at the
// matching name under destPanel's own remote directory, and tempDir
// itself is gone afterward.
func TestUploadExtractedTreeUploadsEveryTopLevelEntryAndRemovesTempDir(t *testing.T) {
	tempDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(tempDir, "a.txt"), []byte("A"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(tempDir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tempDir, "sub", "b.txt"), []byte("B"), 0o644); err != nil {
		t.Fatal(err)
	}
	remote := newTestFakeRemote("/remote")
	destPanel := &Panel{path: "/remote/dest", remote: remote, remoteConn: remotefs.Connection{Host: "example.com"}}
	remote.entries["/remote/dest"] = nil

	if err := uploadExtractedTree(tempDir, destPanel); err != nil {
		t.Fatalf("uploadExtractedTree: %v", err)
	}

	if got := string(remote.content["/remote/dest/a.txt"]); got != "A" {
		t.Errorf("a.txt content = %q, want %q", got, "A")
	}
	if got := string(remote.content["/remote/dest/sub/b.txt"]); got != "B" {
		t.Errorf("sub/b.txt content = %q, want %q", got, "B")
	}
	if _, err := os.Stat(tempDir); !os.IsNotExist(err) {
		t.Errorf("Stat(tempDir) after upload: err = %v, want a not-exist error", err)
	}
}

// TestUploadExtractedTreeRefusesBeforeUploadingAnythingOnAnyConflict
// pins the "no partial extraction on the remote end" guarantee: every
// top-level entry is checked for a conflict before any of them are
// actually uploaded, so a collision on the *second* entry must still
// leave the *first* one untouched on the remote side.
func TestUploadExtractedTreeRefusesBeforeUploadingAnythingOnAnyConflict(t *testing.T) {
	tempDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(tempDir, "a.txt"), []byte("A"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tempDir, "b.txt"), []byte("B"), 0o644); err != nil {
		t.Fatal(err)
	}
	remote := newTestFakeRemote("/remote")
	destPanel := &Panel{path: "/remote/dest", remote: remote, remoteConn: remotefs.Connection{Host: "example.com"}}
	// "b.txt" (the second entry ListDir would return — see
	// fakeRemoteClient.ListDir's own case-insensitive sort) already
	// exists remotely; "a.txt" does not.
	remote.entries["/remote/dest"] = []fsops.Entry{{Name: "b.txt", Type: fsops.TypeFile}}
	remote.content = map[string][]byte{"/remote/dest/b.txt": []byte("already there")}

	err := uploadExtractedTree(tempDir, destPanel)

	if err == nil {
		t.Fatal("uploadExtractedTree should refuse when any top-level entry already exists remotely")
	}
	if _, uploaded := remote.content["/remote/dest/a.txt"]; uploaded {
		t.Error("a.txt should never have been uploaded once b.txt was found to conflict")
	}
	if got := string(remote.content["/remote/dest/b.txt"]); got != "already there" {
		t.Errorf("existing remote content = %q, want it untouched", got)
	}
}

// TestNewCompressTempFileReturnsAFreshNonExistentPath pins
// newCompressTempFile's own contract: a real, unique path ending in
// ext, but not itself left behind as an (empty, invalid-as-an-archive)
// file — see its own doc comment for why that matters specifically for
// zip.
func TestNewCompressTempFileReturnsAFreshNonExistentPath(t *testing.T) {
	got, err := newCompressTempFile(".tar.bz2")
	if err != nil {
		t.Fatalf("newCompressTempFile: %v", err)
	}
	if !strings.HasSuffix(got, ".tar.bz2") {
		t.Errorf("newCompressTempFile(.tar.bz2) = %q, want it to end in .tar.bz2", got)
	}
	if _, err := os.Stat(got); !os.IsNotExist(err) {
		t.Errorf("Stat(%s): err = %v, want a not-exist error (nothing left behind)", got, err)
	}
}

// TestRunCompressToRemoteRefusesAnAlreadyExistingRemoteFile pins the
// pre-check that keeps this path consistent with the local one:
// refused before ever running a real compress command, the same
// "never silently overwrite" guarantee TestRunCompressRefusesAn
// AlreadyExistingOutputFile already pins for a local destination.
func TestRunCompressToRemoteRefusesAnAlreadyExistingRemoteFile(t *testing.T) {
	r, dir, _ := newTestRootWithFile(t)
	r.newTabHere()
	remote := newTestFakeRemote("/remote")
	remote.entries["/remote"] = []fsops.Entry{{Name: "a.txt.zip", Type: fsops.TypeFile}}
	remote.content = map[string][]byte{"/remote/a.txt.zip": []byte("already there")}
	if err := r.panel.connectRemote(remote, remotefs.Connection{Host: "example.com"}); err != nil {
		t.Fatalf("connectRemote: %v", err)
	}
	r.switchToTab(0) // back to the original, local tab — the source
	_ = dir
	r.splitWithTab(1) // the remote tab becomes this one's own split partner
	r.openCompress()

	r.runCompress()

	if r.activePage != errorPage {
		t.Fatalf("activePage = %q, want the error overlay", r.activePage)
	}
	if r.compressJob != nil {
		t.Error("no job should have started once the remote destination already had a colliding file")
	}
}

// TestExtractCurrentArchiveToRemoteExtractsLocallyFirst pins
// extractCurrentArchiveToRemote's own dispatch: the real shell command
// still targets a local temp directory (no real shell command can
// write directly to an SFTP path — see its own doc comment), never
// destPanel's own remote-looking path string.
func TestExtractCurrentArchiveToRemoteExtractsLocallyFirst(t *testing.T) {
	r, dir, _ := newTestRootWithFile(t)
	archivePath := filepath.Join(dir, "bundle.zip")
	if err := os.WriteFile(archivePath, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := r.panel.load(dir); err != nil {
		t.Fatal(err)
	}
	focusRowNamed(t, r.panel, "bundle.zip")

	r.newTabHere()
	remote := newTestFakeRemote("/remote")
	if err := r.panel.connectRemote(remote, remotefs.Connection{Host: "example.com"}); err != nil {
		t.Fatalf("connectRemote: %v", err)
	}
	r.switchToTab(0)
	focusRowNamed(t, r.panel, "bundle.zip")
	r.splitWithTab(1)

	r.extractCurrentArchive(false)

	job := r.compressJob
	if job == nil {
		t.Fatal("extractCurrentArchive did not start a background job")
	}
	defer r.cancelCompressJob()
	command := strings.Join(job.cmd.Args, " ")
	if strings.Contains(command, "/remote") {
		t.Errorf("command = %q, should target a local temp directory, never the remote-looking destination path", command)
	}
	if !strings.Contains(command, os.TempDir()) {
		t.Errorf("command = %q, want it to extract into a local temp directory", command)
	}
}

// focusRowNamed moves p's own table cursor onto the row named name —
// t.Fatal if there isn't one, since every caller here treats that as a
// broken test setup rather than something to keep going past.
func focusRowNamed(t *testing.T, p *Panel, name string) {
	t.Helper()
	for row := 0; ; row++ {
		ref, ok := p.rowRef(row)
		if !ok {
			t.Fatalf("%q not found in the panel", name)
		}
		if ref.name == name {
			p.focusRow(row)
			return
		}
	}
}
