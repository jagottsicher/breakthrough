package ui

import (
	"archive/zip"
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jagottsicher/breakthrough/internal/fsops"
)

// buildTestZipBytes is internal/archive's own writeZip fixture builder,
// duplicated here in-memory (bytes rather than a file on disk) since a
// fakeRemoteClient's own content map holds raw bytes, not a real local
// path — the same small per-package test-fixture duplication this
// codebase already accepts elsewhere (see pdf_test.go's own
// buildMinimalPDF doc comment for the identical reasoning).
func buildTestZipBytes(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for path, content := range files {
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
	return buf.Bytes()
}

func TestNeedsRemoteArchiveConfirmAtExactlyTheThresholdReturnsTrue(t *testing.T) {
	// >=, not >: a file exactly at the configured threshold still asks
	// first — see needsRemoteArchiveConfirm's own doc comment.
	if !needsRemoteArchiveConfirm(10, 10) {
		t.Error("needsRemoteArchiveConfirm(10, 10) = false, want true (size exactly at the threshold)")
	}
	if needsRemoteArchiveConfirm(9, 10) {
		t.Error("needsRemoteArchiveConfirm(9, 10) = true, want false (below the threshold)")
	}
	if !needsRemoteArchiveConfirm(11, 10) {
		t.Error("needsRemoteArchiveConfirm(11, 10) = false, want true (above the threshold)")
	}
}

// TestEnterRemoteArchiveAboveThresholdAsksFirst pins the confirm path
// — a large archive must not start downloading before the user has
// actually agreed to it.
func TestEnterRemoteArchiveAboveThresholdAsksFirst(t *testing.T) {
	r := newTestRemoteRoot(t)
	client := r.panel.remote.(*fakeRemoteClient)
	zipBytes := buildTestZipBytes(t, map[string]string{"file.txt": "hello"})
	client.entries["/remote"] = append(client.entries["/remote"], fsops.Entry{Name: "big.zip", Type: fsops.TypeFile, Size: int64(len(zipBytes))})
	client.content = map[string][]byte{"/remote/big.zip": zipBytes}
	r.settings.RemoteArchiveConfirmSize = 1 // anything real is "large" against this
	if err := r.panel.load(r.panel.path); err != nil {
		t.Fatal(err)
	}
	row := rowForName(t, r.panel, "big.zip")
	r.panel.focusRow(row)

	r.enterRemoteArchive()

	if r.activePage != confirmPage {
		t.Fatalf("activePage = %q, want %q — a large archive must ask before downloading", r.activePage, confirmPage)
	}
	if r.panel.archiveLocalPath != "" {
		t.Error("archiveLocalPath is already set despite the confirm dialog never having been answered")
	}
}

// TestFinishRemoteArchiveDownloadEntersTheArchiveAndBrowsesItsContent
// is the end-to-end real behavior finishRemoteArchiveDownload,
// resolveRemoteArchiveState, and loadArchiveEntries together produce
// once a download has actually landed — called directly rather than
// through the real async startRemoteArchiveDownload/safeGo path, which
// needs a real Application event loop this test doesn't have (see
// finishRemoteArchiveDownload's own doc comment).
func TestFinishRemoteArchiveDownloadEntersTheArchiveAndBrowsesItsContent(t *testing.T) {
	r := newTestRemoteRoot(t)
	client := r.panel.remote.(*fakeRemoteClient)
	zipBytes := buildTestZipBytes(t, map[string]string{
		"top.txt":      "top level",
		"sub/deep.txt": "nested",
	})
	client.entries["/remote"] = append(client.entries["/remote"], fsops.Entry{Name: "archive.zip", Type: fsops.TypeFile})
	client.content = map[string][]byte{"/remote/archive.zip": zipBytes}

	localPath, cleanup, err := downloadRemoteToTemp(client, "/remote/archive.zip")
	if err != nil {
		t.Fatalf("downloadRemoteToTemp: %v", err)
	}

	r.finishRemoteArchiveDownload(r.panel, client, "/remote/archive.zip", localPath, cleanup, nil)

	if r.panel.path != "/remote/archive.zip" {
		t.Fatalf("panel.path = %q, want the archive's own remote path", r.panel.path)
	}
	if !r.panel.inArchiveView() {
		t.Fatal("panel is not in archive view after entering a remote archive")
	}
	if r.panel.archiveLocalPath != localPath {
		t.Errorf("archiveLocalPath = %q, want %q", r.panel.archiveLocalPath, localPath)
	}

	var names []string
	for row := 1; row < r.panel.table.GetRowCount(); row++ {
		if ref, ok := r.panel.rowRef(row); ok {
			names = append(names, ref.name)
		}
	}
	if !strings.Contains(strings.Join(names, ","), "top.txt") {
		t.Errorf("archive listing = %v, want it to include top.txt", names)
	}
	if !strings.Contains(strings.Join(names, ","), "sub") {
		t.Errorf("archive listing = %v, want it to include the sub directory", names)
	}

	// Navigate one level deeper inside the archive — resolveRemoteArchiveState's
	// own "already inside, prefix-match" branch.
	if err := r.panel.navigate("/remote/archive.zip/sub"); err != nil {
		t.Fatalf("navigate into sub: %v", err)
	}
	var subNames []string
	for row := 1; row < r.panel.table.GetRowCount(); row++ {
		if ref, ok := r.panel.rowRef(row); ok {
			subNames = append(subNames, ref.name)
		}
	}
	if !strings.Contains(strings.Join(subNames, ","), "deep.txt") {
		t.Errorf("sub listing = %v, want it to include deep.txt", subNames)
	}
}

// TestLeavingARemoteArchiveRemovesItsOwnTempFile pins the cleanup half
// — a remote archive's own staged temp copy must not linger on disk
// once the panel navigates back out of it.
func TestLeavingARemoteArchiveRemovesItsOwnTempFile(t *testing.T) {
	r := newTestRemoteRoot(t)
	client := r.panel.remote.(*fakeRemoteClient)
	zipBytes := buildTestZipBytes(t, map[string]string{"file.txt": "hello"})
	client.entries["/remote"] = append(client.entries["/remote"], fsops.Entry{Name: "archive.zip", Type: fsops.TypeFile})
	client.content = map[string][]byte{"/remote/archive.zip": zipBytes}
	localPath, cleanup, err := downloadRemoteToTemp(client, "/remote/archive.zip")
	if err != nil {
		t.Fatalf("downloadRemoteToTemp: %v", err)
	}
	r.finishRemoteArchiveDownload(r.panel, client, "/remote/archive.zip", localPath, cleanup, nil)
	if !r.panel.inArchiveView() {
		t.Fatal("setup: expected to be inside the archive")
	}

	if err := r.panel.navigate("/remote"); err != nil {
		t.Fatalf("navigate back out: %v", err)
	}

	if r.panel.archiveLocalPath != "" {
		t.Errorf("archiveLocalPath = %q after leaving the archive, want it cleared", r.panel.archiveLocalPath)
	}
	if r.panel.archiveRemoteClient != nil {
		t.Error("archiveRemoteClient still set after leaving the archive")
	}
	if _, err := os.Stat(localPath); !os.IsNotExist(err) {
		t.Errorf("Stat(%s) after leaving the archive: err = %v, want a not-exist error", localPath, err)
	}
}

// TestFinishRemoteArchiveDownloadOnDownloadFailureReportsTheError pins
// the "download itself failed" case — never reaches staging at all.
func TestFinishRemoteArchiveDownloadOnDownloadFailureReportsTheError(t *testing.T) {
	r := newTestRemoteRoot(t)
	client := r.panel.remote.(*fakeRemoteClient)

	r.finishRemoteArchiveDownload(r.panel, client, "/remote/missing.zip", "", func() {}, fmt.Errorf("boom"))

	if r.activePage != errorPage {
		t.Fatalf("activePage = %q, want %q", r.activePage, errorPage)
	}
	if r.panel.archiveLocalPath != "" {
		t.Error("archiveLocalPath is set despite the download itself failing")
	}
}

// TestFinishRemoteArchiveDownloadOnNavigateFailureRollsBackStaging
// pins the corrupted-archive case: entering fails after the download
// already succeeded, and the panel must not be left looking like it's
// still browsing an archive it never actually entered.
func TestFinishRemoteArchiveDownloadOnNavigateFailureRollsBackStaging(t *testing.T) {
	r := newTestRemoteRoot(t)
	client := r.panel.remote.(*fakeRemoteClient)
	// Not a real zip at all — archive.List will fail against it.
	client.entries["/remote"] = append(client.entries["/remote"], fsops.Entry{Name: "corrupt.zip", Type: fsops.TypeFile})
	client.content = map[string][]byte{"/remote/corrupt.zip": []byte("not a real zip file")}
	localPath, cleanup, err := downloadRemoteToTemp(client, "/remote/corrupt.zip")
	if err != nil {
		t.Fatalf("downloadRemoteToTemp: %v", err)
	}

	r.finishRemoteArchiveDownload(r.panel, client, "/remote/corrupt.zip", localPath, cleanup, nil)

	if r.panel.archiveLocalPath != "" {
		t.Errorf("archiveLocalPath = %q after a failed navigate, want it rolled back to empty", r.panel.archiveLocalPath)
	}
	if r.panel.archiveRemoteClient != nil {
		t.Error("archiveRemoteClient still set after a failed navigate")
	}
	if _, err := os.Stat(localPath); !os.IsNotExist(err) {
		t.Errorf("Stat(%s) after a failed navigate: err = %v, want the temp file removed", localPath, err)
	}
	if r.activePage != errorPage {
		t.Errorf("activePage = %q, want %q reporting the navigate failure", r.activePage, errorPage)
	}
}

// TestFinishRemoteArchiveDownloadOnAClosedTabCleansUpWithoutEntering
// pins the "the tab closed while the download was in flight" guard —
// the same shape remotepaste.go's own startRemotePaste already
// establishes for the identical race.
func TestFinishRemoteArchiveDownloadOnAClosedTabCleansUpWithoutEntering(t *testing.T) {
	r := newTestRemoteRoot(t)
	client := r.panel.remote.(*fakeRemoteClient)
	client.content = map[string][]byte{"/remote/b.txt": []byte("hello")}
	orphan, err := NewPanel(r.app, t.TempDir(), r.theme, r.settings)
	if err != nil {
		t.Fatalf("NewPanel: %v", err)
	}
	// Deliberately never added to r.tabs — this is exactly what makes
	// hasTab report false.

	localPath, cleanup, err := downloadRemoteToTemp(client, "/remote/b.txt")
	if err != nil {
		t.Fatalf("downloadRemoteToTemp: %v", err)
	}

	r.finishRemoteArchiveDownload(orphan, client, "/remote/b.txt", localPath, cleanup, nil)

	if orphan.archiveLocalPath != "" {
		t.Error("archiveLocalPath was set on a panel whose own tab had already closed")
	}
	if _, err := os.Stat(localPath); !os.IsNotExist(err) {
		t.Errorf("Stat(%s): err = %v, want the temp file cleaned up even though nothing entered it", localPath, err)
	}
}

// TestPasteRefusesCuttingAMemberFromARemoteArchive pins that Cut is
// refused for a remote archive member exactly the way it already is
// for a local one (see archiveExtractionFor's own identical local
// refusal): there's no writing back into a read-only archive to make
// the "move" half of it real, remote or not.
func TestPasteRefusesCuttingAMemberFromARemoteArchive(t *testing.T) {
	r := newTestRemoteRoot(t)
	client := r.panel.remote.(*fakeRemoteClient)
	zipBytes := buildTestZipBytes(t, map[string]string{"member.txt": "hello"})
	client.entries["/remote"] = append(client.entries["/remote"], fsops.Entry{Name: "archive.zip", Type: fsops.TypeFile})
	client.content = map[string][]byte{"/remote/archive.zip": zipBytes}
	localPath, cleanup, err := downloadRemoteToTemp(client, "/remote/archive.zip")
	if err != nil {
		t.Fatalf("downloadRemoteToTemp: %v", err)
	}
	defer cleanup()
	r.finishRemoteArchiveDownload(r.panel, client, "/remote/archive.zip", localPath, cleanup, nil)
	if !r.panel.inArchiveView() {
		t.Fatal("setup: expected to be inside the archive")
	}

	r.clipboardSourceClient = client
	r.clipboard = []string{"/remote/archive.zip/member.txt"}
	r.clipboardCut = true

	r.pasteInto(r.panel.path, false)

	if r.activePage != errorPage {
		t.Fatalf("activePage = %q, want %q", r.activePage, errorPage)
	}
	got := strings.ReplaceAll(r.errorView.GetText(true), "\n", " ")
	if !strings.Contains(got, "Copy instead") {
		t.Errorf("error text = %q, want it to explain that Cut isn't supported here", got)
	}
}

// TestPasteCopiesAMemberOutOfARemoteArchiveToALocalDestination pins
// the "total easy" case: the archive is already downloaded locally
// (archiveLocalPath), so copying a member back out to a real local
// directory needs nothing more than an ordinary local archive.Extract
// against that temp copy — see remoteArchiveExtractionFor's own doc
// comment.
func TestPasteCopiesAMemberOutOfARemoteArchiveToALocalDestination(t *testing.T) {
	r := newTestRemoteRoot(t)
	client := r.panel.remote.(*fakeRemoteClient)
	zipBytes := buildTestZipBytes(t, map[string]string{"member.txt": "hello from the archive"})
	client.entries["/remote"] = append(client.entries["/remote"], fsops.Entry{Name: "archive.zip", Type: fsops.TypeFile})
	client.content = map[string][]byte{"/remote/archive.zip": zipBytes}
	localPath, cleanup, err := downloadRemoteToTemp(client, "/remote/archive.zip")
	if err != nil {
		t.Fatalf("downloadRemoteToTemp: %v", err)
	}
	defer cleanup()
	r.finishRemoteArchiveDownload(r.panel, client, "/remote/archive.zip", localPath, cleanup, nil)
	if !r.panel.inArchiveView() {
		t.Fatal("setup: expected to be inside the archive")
	}

	r.clipboardSourceClient = client
	r.clipboard = []string{"/remote/archive.zip/member.txt"}
	r.clipboardCut = false

	// Switch to a genuinely local tab before pasting — pasteInto's own
	// dispatch reads r.panel.remote to decide whether the destination
	// itself is remote (see runRemoteArchiveExtraction's own doc
	// comment for the other case), and the clipboard carries across
	// tabs untouched, exactly like an ordinary Copy-then-switch-tabs-
	// then-Paste.
	destDir := t.TempDir()
	r.newTab(destDir)
	if r.panel.remote != nil {
		t.Fatal("setup: the new tab should be a plain local panel")
	}

	r.pasteInto(destDir, false)

	// extractClipboardArchive runs off the UI thread (see its own doc
	// comment) — waitForCondition polls the real disk write it makes
	// rather than asserting immediately after pasteInto returns, the
	// same way TestCopyPasteExtractsMarkedArchiveEntry already has to
	// for the identical local-archive case.
	waitForCondition(t, func() bool {
		_, err := os.Stat(filepath.Join(destDir, "member.txt"))
		return err == nil
	})
	got, err := os.ReadFile(filepath.Join(destDir, "member.txt"))
	if err != nil {
		t.Fatalf("member.txt was not extracted to the local destination: %v", err)
	}
	if string(got) != "hello from the archive" {
		t.Errorf("member.txt content = %q, want %q", got, "hello from the archive")
	}
}

// TestRemoteArchiveExtractionForResolvesTheAlreadyDownloadedLocalCopy
// pins remoteArchiveExtractionFor's own core job directly (no async
// paste involved): given a clipboard path inside a remote-staged
// archive still open in some tab, it must find that tab's own
// archiveLocalPath and resolve the marked member against it — this is
// what lets pasteInto treat the destination-is-itself-remote case (see
// runRemoteArchiveExtraction in remotepaste_test.go) as an ordinary
// local-source upload once this has already done its job.
func TestRemoteArchiveExtractionForResolvesTheAlreadyDownloadedLocalCopy(t *testing.T) {
	r := newTestRemoteRoot(t)
	client := r.panel.remote.(*fakeRemoteClient)
	zipBytes := buildTestZipBytes(t, map[string]string{"member.txt": "hello"})
	client.entries["/remote"] = append(client.entries["/remote"], fsops.Entry{Name: "archive.zip", Type: fsops.TypeFile})
	client.content = map[string][]byte{"/remote/archive.zip": zipBytes}
	localPath, cleanup, err := downloadRemoteToTemp(client, "/remote/archive.zip")
	if err != nil {
		t.Fatalf("downloadRemoteToTemp: %v", err)
	}
	defer cleanup()
	r.finishRemoteArchiveDownload(r.panel, client, "/remote/archive.zip", localPath, cleanup, nil)
	if !r.panel.inArchiveView() {
		t.Fatal("setup: expected to be inside the archive")
	}

	gotPath, members, ok := r.remoteArchiveExtractionFor([]string{"/remote/archive.zip/member.txt"})

	if !ok {
		t.Fatal("remoteArchiveExtractionFor should have recognized the clipboard as a remote archive member")
	}
	if gotPath != localPath {
		t.Errorf("archiveLocalPath = %q, want the already-downloaded temp copy %q", gotPath, localPath)
	}
	if len(members) != 1 || members[0].Path != "member.txt" || members[0].IsDir {
		t.Errorf("members = %v, want exactly one file entry for \"member.txt\"", members)
	}
}

// TestRemoteArchiveExtractionForRefusesTheArchiveFileItself is the
// remote counterpart of TestArchiveExtractionForRefusesTheArchiveFileItself
// (archivepanel_test.go): marking the archive file's own path — not one
// of its members — while some other tab happens to already be browsing
// into that exact archive must not be mistaken for an extraction either,
// the same real bug class, just requiring an already-open archive tab
// to reach here at all.
func TestRemoteArchiveExtractionForRefusesTheArchiveFileItself(t *testing.T) {
	r := newTestRemoteRoot(t)
	client := r.panel.remote.(*fakeRemoteClient)
	zipBytes := buildTestZipBytes(t, map[string]string{"member.txt": "hello"})
	client.entries["/remote"] = append(client.entries["/remote"], fsops.Entry{Name: "archive.zip", Type: fsops.TypeFile})
	client.content = map[string][]byte{"/remote/archive.zip": zipBytes}
	localPath, cleanup, err := downloadRemoteToTemp(client, "/remote/archive.zip")
	if err != nil {
		t.Fatalf("downloadRemoteToTemp: %v", err)
	}
	defer cleanup()
	r.finishRemoteArchiveDownload(r.panel, client, "/remote/archive.zip", localPath, cleanup, nil)

	_, members, ok := r.remoteArchiveExtractionFor([]string{"/remote/archive.zip"})

	if ok {
		t.Errorf("remoteArchiveExtractionFor should refuse the archive file's own path, got ok=true, members=%v", members)
	}
}

// TestRemoteArchiveExtractionForOnAnUnrelatedPathReturnsFalse pins
// the "ordinary remote paste" fallback: a clipboard path that isn't
// under any currently-open remote archive tab must not be mistaken
// for one.
func TestRemoteArchiveExtractionForOnAnUnrelatedPathReturnsFalse(t *testing.T) {
	r := newTestRemoteRoot(t)

	if _, _, ok := r.remoteArchiveExtractionFor([]string{"/remote/b.txt"}); ok {
		t.Error("remoteArchiveExtractionFor should return false for a plain remote path outside any archive")
	}
}

// rowForName returns the row index of the entry named name in panel's
// current listing, failing the test if it isn't there — used wherever
// a test needs a real row index rather than relying on a fixed
// position that would silently break if sort order ever changed.
func rowForName(t *testing.T, panel *Panel, name string) int {
	t.Helper()
	for row := 0; row < panel.table.GetRowCount(); row++ {
		if ref, ok := panel.rowRef(row); ok && ref.name == name {
			return row
		}
	}
	t.Fatalf("no row named %q in the current listing", name)
	return -1
}
