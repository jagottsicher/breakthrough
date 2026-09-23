package ui

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jagottsicher/breakthrough/internal/activitylog"
	"github.com/jagottsicher/breakthrough/internal/fsops"
)

// createSpyClient wraps fakeRemoteClient to record whether Create was
// actually called — finishRemoteEdit's own "unchanged" test needs this
// rather than just comparing final remote content, since an upload of
// content identical to what's already there would leave that content
// looking exactly as if nothing had been uploaded at all; only a real
// call-was-made/wasn't-made check tells the two apart. The same
// reasoning renameSpyClient's own doc comment gives (remotepaste_test.go)
// for why it spies on Open rather than trusting the end state alone.
type createSpyClient struct {
	*fakeRemoteClient
	createCalled bool
}

func (s *createSpyClient) Create(p string) (io.WriteCloser, error) {
	s.createCalled = true
	return s.fakeRemoteClient.Create(p)
}

func TestDownloadRemoteToTempCopiesContentAndKeepsTheExtension(t *testing.T) {
	client := &fakeRemoteClient{
		entries: map[string][]fsops.Entry{"/remote": {{Name: "notes.txt", Type: fsops.TypeFile}}},
		content: map[string][]byte{"/remote/notes.txt": []byte("hello remote world")},
	}

	localPath, cleanup, err := downloadRemoteToTemp(client, "/remote/notes.txt")
	defer cleanup()

	if err != nil {
		t.Fatalf("downloadRemoteToTemp: %v", err)
	}
	if filepath.Ext(localPath) != ".txt" {
		t.Errorf("localPath = %q, want it to keep the .txt extension", localPath)
	}
	got, err := os.ReadFile(localPath)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != "hello remote world" {
		t.Errorf("content = %q, want %q", got, "hello remote world")
	}
}

func TestDownloadRemoteToTempCleanupRemovesTheFile(t *testing.T) {
	client := &fakeRemoteClient{
		entries: map[string][]fsops.Entry{"/remote": {{Name: "a.txt", Type: fsops.TypeFile}}},
		content: map[string][]byte{"/remote/a.txt": []byte("x")},
	}
	localPath, cleanup, err := downloadRemoteToTemp(client, "/remote/a.txt")
	if err != nil {
		t.Fatalf("downloadRemoteToTemp: %v", err)
	}

	cleanup()

	if _, err := os.Stat(localPath); !os.IsNotExist(err) {
		t.Errorf("Stat(%s) after cleanup: err = %v, want a not-exist error", localPath, err)
	}
}

func TestDownloadRemoteToTempOnAMissingFileReturnsAnError(t *testing.T) {
	client := &fakeRemoteClient{entries: map[string][]fsops.Entry{"/remote": nil}}

	_, cleanup, err := downloadRemoteToTemp(client, "/remote/missing.txt")
	defer cleanup()

	if err == nil {
		t.Error("downloadRemoteToTemp on a missing remote file returned no error")
	}
}

// TestOpenRemoteLookOpensTheBuiltinViewerAndRemovesTheTempFile pins the
// plain text/image case: Look's whole content is already rendered into
// r.viewerView by the time openRemoteLook returns, so the staged temp
// file is disposable immediately afterward — unlike a PDF (see
// TestOpenRemoteLookOnAPDFKeepsTheTempFileForPageTurns).
func TestOpenRemoteLookOpensTheBuiltinViewerAndRemovesTheTempFile(t *testing.T) {
	r := newTestRemoteRoot(t)
	client := r.panel.remote.(*fakeRemoteClient)
	client.content = map[string][]byte{"/remote/b.txt": []byte("remote file content")}

	r.openRemoteLook(client, "/remote/b.txt")

	if r.activePage != viewerPage {
		t.Fatalf("activePage = %q, want %q", r.activePage, viewerPage)
	}
	if got := r.viewerView.GetText(true); got != "remote file content" {
		t.Errorf("viewerView text = %q, want the remote file's own content", got)
	}
	if r.viewerRemoteTempFile != "" {
		t.Errorf("viewerRemoteTempFile = %q, want empty — a plain text Look's own temp file should be removed immediately", r.viewerRemoteTempFile)
	}
}

// TestOpenRemoteLookOnAPDFKeepsTheTempFileForPageTurns pins the one
// exception: turnPDFPage reads r.viewerPDFPath again on every page
// turn, so a remote PDF's own staged copy has to survive at least
// until the Look overlay closes.
func TestOpenRemoteLookOnAPDFKeepsTheTempFileForPageTurns(t *testing.T) {
	r := newTestRemoteRoot(t)
	client := r.panel.remote.(*fakeRemoteClient)
	client.content = map[string][]byte{"/remote/doc.pdf": buildMinimalPDF("Hello remote PDF")}

	r.openRemoteLook(client, "/remote/doc.pdf")

	if r.activePage != viewerPage {
		t.Fatalf("activePage = %q, want %q", r.activePage, viewerPage)
	}
	if r.viewerRemoteTempFile == "" {
		t.Fatal("viewerRemoteTempFile is empty, want the staged PDF's own local path")
	}
	if r.viewerPDFPath != r.viewerRemoteTempFile {
		t.Errorf("viewerPDFPath = %q, want it to match viewerRemoteTempFile %q", r.viewerPDFPath, r.viewerRemoteTempFile)
	}
	if _, err := os.Stat(r.viewerRemoteTempFile); err != nil {
		t.Errorf("the staged PDF's own temp file is missing while Look is still open: %v", err)
	}
}

// TestClosingLookRemovesARemotePDFsOwnTempFile pins the hideOverlay
// cleanup hook: once the user actually closes Look, the temp file a
// remote PDF was staged into must not linger indefinitely.
func TestClosingLookRemovesARemotePDFsOwnTempFile(t *testing.T) {
	r := newTestRemoteRoot(t)
	client := r.panel.remote.(*fakeRemoteClient)
	client.content = map[string][]byte{"/remote/doc.pdf": buildMinimalPDF("Hello remote PDF")}
	r.openRemoteLook(client, "/remote/doc.pdf")
	tempFile := r.viewerRemoteTempFile
	if tempFile == "" {
		t.Fatal("setup: expected a staged PDF temp file")
	}

	r.hideOverlay()

	if r.viewerRemoteTempFile != "" {
		t.Errorf("viewerRemoteTempFile = %q after closing Look, want it cleared", r.viewerRemoteTempFile)
	}
	if _, err := os.Stat(tempFile); !os.IsNotExist(err) {
		t.Errorf("Stat(%s) after closing Look: err = %v, want a not-exist error", tempFile, err)
	}
}

// TestOpeningANewLookRemovesThePreviousRemotePDFsOwnTempFile is the
// other cleanup path: pressing "l" again on a different file, without
// ever closing the previous PDF's own Look overlay first, must not
// silently orphan its temp file either — showBuiltinLook's own
// top-of-function reset is what catches this one.
func TestOpeningANewLookRemovesThePreviousRemotePDFsOwnTempFile(t *testing.T) {
	r := newTestRemoteRoot(t)
	client := r.panel.remote.(*fakeRemoteClient)
	client.content = map[string][]byte{
		"/remote/doc.pdf": buildMinimalPDF("Hello remote PDF"),
		"/remote/b.txt":   []byte("plain text"),
	}
	r.openRemoteLook(client, "/remote/doc.pdf")
	firstTempFile := r.viewerRemoteTempFile
	if firstTempFile == "" {
		t.Fatal("setup: expected a staged PDF temp file")
	}

	r.openRemoteLook(client, "/remote/b.txt")

	if _, err := os.Stat(firstTempFile); !os.IsNotExist(err) {
		t.Errorf("Stat(%s) after opening a new Look: err = %v, want a not-exist error", firstTempFile, err)
	}
}

// TestOpenLookOnARemotePanelDispatchesToOpenRemoteLook pins openLook's
// own top-level dispatch (isRemote takes priority over the external-
// pager setting, exactly the same "remote first" order editCurrentEntry
// now follows too).
func TestOpenLookOnARemotePanelDispatchesToOpenRemoteLook(t *testing.T) {
	r := newTestRemoteRoot(t)
	client := r.panel.remote.(*fakeRemoteClient)
	client.content = map[string][]byte{"/remote/b.txt": []byte("remote content")}

	r.openLook()

	if r.activePage != viewerPage {
		t.Fatalf("activePage = %q, want %q", r.activePage, viewerPage)
	}
	if got := r.viewerView.GetText(true); got != "remote content" {
		t.Errorf("viewerView text = %q, want the remote file's own content", got)
	}
}

// TestEditCurrentEntryOnARemotePanelStagesAndDownloadsButUploadsNothingHere
// pins editCurrentEntry's own remote dispatch reaches editRemoteEntry
// at all — the actual upload-or-not decision is pinned separately by
// finishRemoteEdit's own tests below, since app.Suspend never actually
// runs an editor process in this test environment (see
// runEditorProcess's own doc comment) and so can never itself change
// the downloaded temp file.
func TestEditCurrentEntryOnARemotePanelDoesNotError(t *testing.T) {
	r := newTestRemoteRoot(t)
	client := r.panel.remote.(*fakeRemoteClient)
	client.content = map[string][]byte{"/remote/b.txt": []byte("hello")}

	r.editCurrentEntry()

	if r.activePage == errorPage {
		t.Errorf("editCurrentEntry on a remote panel reported an error: %q", r.errorView.GetText(true))
	}
}

// TestFinishRemoteEditUploadsWhenTheFileChanged pins the "changed"
// half of finishRemoteEdit's own mtime/size compare.
func TestFinishRemoteEditUploadsWhenTheFileChanged(t *testing.T) {
	r := newTestRemoteRoot(t)
	client := r.panel.remote.(*fakeRemoteClient)
	client.content = map[string][]byte{"/remote/b.txt": []byte("original")}

	localPath := filepath.Join(t.TempDir(), "b.txt")
	if err := os.WriteFile(localPath, []byte("original"), 0o644); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(localPath)
	if err != nil {
		t.Fatal(err)
	}

	// Simulate an editor's own save: new content, and a distinctly
	// later mtime than before (os.Chtimes rather than relying on the
	// wall clock actually advancing between the two os.Stat calls,
	// which a fast test run can't guarantee on its own).
	if err := os.WriteFile(localPath, []byte("edited content"), 0o644); err != nil {
		t.Fatal(err)
	}
	later := before.ModTime().Add(time.Second)
	if err := os.Chtimes(localPath, later, later); err != nil {
		t.Fatal(err)
	}

	if err := r.finishRemoteEdit(client, "/remote/b.txt", localPath, before); err != nil {
		t.Fatalf("finishRemoteEdit: %v", err)
	}

	if got := string(client.content["/remote/b.txt"]); got != "edited content" {
		t.Errorf("remote content = %q, want the edited content uploaded back", got)
	}
}

func TestFinishRemoteEditLogsAnActionWhenUploaded(t *testing.T) {
	r := newTestRemoteRoot(t)
	client := r.panel.remote.(*fakeRemoteClient)
	client.content = map[string][]byte{"/remote/b.txt": []byte("original")}

	localPath := filepath.Join(t.TempDir(), "b.txt")
	if err := os.WriteFile(localPath, []byte("original"), 0o644); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(localPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(localPath, []byte("edited content"), 0o644); err != nil {
		t.Fatal(err)
	}
	later := before.ModTime().Add(time.Second)
	if err := os.Chtimes(localPath, later, later); err != nil {
		t.Fatal(err)
	}
	readLog := attachTestActivityLog(t, r)

	if err := r.finishRemoteEdit(client, "/remote/b.txt", localPath, before); err != nil {
		t.Fatalf("finishRemoteEdit: %v", err)
	}

	got := readLog()
	if !strings.Contains(got, string(activitylog.CategoryRemote)) || !strings.Contains(got, "uploaded changes to /remote/b.txt") {
		t.Errorf("log = %q, want a remote entry about the upload", got)
	}
}

// TestFinishRemoteEditUploadsNothingWhenUnchanged pins the "no change"
// half — the whole point of comparing before/after at all, per the
// user's own explicit "don't touch the remote copy if nothing really
// changed" expectation. Uses createSpyClient rather than comparing
// final remote content: an upload of content identical to what's
// already there would leave the exact same bytes behind either way,
// so only a real call-was-made/wasn't-made check actually tells the
// two apart.
func TestFinishRemoteEditUploadsNothingWhenUnchanged(t *testing.T) {
	r := newTestRemoteRoot(t)
	inner := r.panel.remote.(*fakeRemoteClient)
	inner.content = map[string][]byte{"/remote/b.txt": []byte("original")}
	client := &createSpyClient{fakeRemoteClient: inner}

	localPath := filepath.Join(t.TempDir(), "b.txt")
	if err := os.WriteFile(localPath, []byte("original"), 0o644); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(localPath)
	if err != nil {
		t.Fatal(err)
	}

	if err := r.finishRemoteEdit(client, "/remote/b.txt", localPath, before); err != nil {
		t.Fatalf("finishRemoteEdit: %v", err)
	}

	if client.createCalled {
		t.Error("finishRemoteEdit called Create despite the file being unchanged — want the remote copy left completely untouched")
	}
}
