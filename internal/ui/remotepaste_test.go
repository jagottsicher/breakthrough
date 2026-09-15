package ui

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/jagottsicher/breakthrough/internal/archive"
	"github.com/jagottsicher/breakthrough/internal/fsops"
)

func TestRunRemotePasteUploadsALocalFileToARemoteDirectory(t *testing.T) {
	localDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(localDir, "local.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	client := &fakeRemoteClient{entries: map[string][]fsops.Entry{"/remote": nil}}

	succeeded, skipped, err := runRemotePaste([]string{filepath.Join(localDir, "local.txt")}, nil, false, client, "/remote")

	if err != nil || succeeded != 1 || len(skipped) != 0 {
		t.Fatalf("succeeded, skipped, err = %d, %v, %v", succeeded, skipped, err)
	}
	if got := string(client.content["/remote/local.txt"]); got != "hello" {
		t.Errorf("uploaded content = %q, want %q", got, "hello")
	}
	if _, err := os.Stat(filepath.Join(localDir, "local.txt")); err != nil {
		t.Error("a plain Copy must leave the local source file in place")
	}
}

func TestRunRemotePasteDownloadsARemoteFileToALocalDirectory(t *testing.T) {
	client := &fakeRemoteClient{
		entries: map[string][]fsops.Entry{"/remote": {{Name: "a.txt", Type: fsops.TypeFile}}},
		content: map[string][]byte{"/remote/a.txt": []byte("hi")},
	}
	localDir := t.TempDir()

	succeeded, skipped, err := runRemotePaste([]string{"/remote/a.txt"}, client, false, nil, localDir)

	if err != nil || succeeded != 1 || len(skipped) != 0 {
		t.Fatalf("succeeded, skipped, err = %d, %v, %v", succeeded, skipped, err)
	}
	got, readErr := os.ReadFile(filepath.Join(localDir, "a.txt"))
	if readErr != nil {
		t.Fatalf("ReadFile: %v", readErr)
	}
	if string(got) != "hi" {
		t.Errorf("downloaded content = %q, want %q", got, "hi")
	}
	if _, err := client.Stat("/remote/a.txt"); err != nil {
		t.Error("a plain Copy must leave the remote source file in place")
	}
}

func TestRunRemotePasteMoveRemovesTheLocalSourceAfterUploading(t *testing.T) {
	localDir := t.TempDir()
	srcPath := filepath.Join(localDir, "local.txt")
	if err := os.WriteFile(srcPath, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	client := &fakeRemoteClient{entries: map[string][]fsops.Entry{"/remote": nil}}

	succeeded, _, err := runRemotePaste([]string{srcPath}, nil, true, client, "/remote")

	if err != nil || succeeded != 1 {
		t.Fatalf("succeeded, err = %d, %v", succeeded, err)
	}
	if _, err := os.Stat(srcPath); err == nil {
		t.Error("a Cut+Paste must remove the local source once the upload succeeds")
	}
	if _, err := client.Stat("/remote/local.txt"); err != nil {
		t.Error("the uploaded file is missing at the destination")
	}
}

// renameSpyClient wraps fakeRemoteClient to record whether Open or
// Rename actually got called — runRemotePasteMoveWithinTheSameRemoteClient
// needs to prove the same-connection move optimization (see
// runRemotePaste's own doc comment) really took the single-Rename path
// instead of silently falling back to a stream copy plus a separate
// delete, which would still produce an identical *end result* but defeats
// the entire point of the optimization for a real, possibly large file.
type renameSpyClient struct {
	*fakeRemoteClient
	openCalled             bool
	renamedFrom, renamedTo string
}

func (s *renameSpyClient) Open(p string) (io.ReadCloser, error) {
	s.openCalled = true
	return s.fakeRemoteClient.Open(p)
}

func (s *renameSpyClient) Rename(oldPath, newPath string) error {
	s.renamedFrom, s.renamedTo = oldPath, newPath
	return s.fakeRemoteClient.Rename(oldPath, newPath)
}

func TestRunRemotePasteMoveWithinTheSameRemoteClientUsesRenameNotCopy(t *testing.T) {
	client := &renameSpyClient{fakeRemoteClient: &fakeRemoteClient{
		entries: map[string][]fsops.Entry{
			"/remote":      {{Name: "a.txt", Type: fsops.TypeFile}},
			"/remote/dest": nil,
		},
		content: map[string][]byte{"/remote/a.txt": []byte("hello")},
	}}

	succeeded, _, err := runRemotePaste([]string{"/remote/a.txt"}, client, true, client, "/remote/dest")

	if err != nil || succeeded != 1 {
		t.Fatalf("succeeded, err = %d, %v", succeeded, err)
	}
	if client.openCalled {
		t.Error("a same-connection move called Open — want a single Rename, no streamed copy")
	}
	if client.renamedFrom != "/remote/a.txt" || client.renamedTo != "/remote/dest/a.txt" {
		t.Errorf("renamedFrom, renamedTo = %q, %q", client.renamedFrom, client.renamedTo)
	}
	if _, err := client.Stat("/remote/a.txt"); err == nil {
		t.Error("the old path still exists after the same-connection move")
	}
}

// TestRunRemotePasteMoveAcrossTwoDifferentRemoteClientsStillCopiesAndDeletes
// pins the negative case for the same optimization: two Client values —
// even if they happen to point at the same server, as two separate
// connections would — are never treated as "the same connection", so
// the move still goes through the ordinary copy-then-delete path.
func TestRunRemotePasteMoveAcrossTwoDifferentRemoteClientsStillCopiesAndDeletes(t *testing.T) {
	src := &fakeRemoteClient{
		entries: map[string][]fsops.Entry{"/remote": {{Name: "a.txt", Type: fsops.TypeFile}}},
		content: map[string][]byte{"/remote/a.txt": []byte("hello")},
	}
	dest := &fakeRemoteClient{entries: map[string][]fsops.Entry{"/other": nil}}

	succeeded, _, err := runRemotePaste([]string{"/remote/a.txt"}, src, true, dest, "/other")

	if err != nil || succeeded != 1 {
		t.Fatalf("succeeded, err = %d, %v", succeeded, err)
	}
	if _, err := src.Stat("/remote/a.txt"); err == nil {
		t.Error("the source client still has the file after a completed move")
	}
	if got := string(dest.content["/other/a.txt"]); got != "hello" {
		t.Errorf("destination content = %q, want %q", got, "hello")
	}
}

func TestRunRemotePasteSkipsASymlinkSourceWithoutCopyingIt(t *testing.T) {
	client := &fakeRemoteClient{entries: map[string][]fsops.Entry{
		"/remote": {{Name: "link", Type: fsops.TypeSymlinkFile}},
	}}
	localDir := t.TempDir()

	succeeded, skipped, err := runRemotePaste([]string{"/remote/link"}, client, false, nil, localDir)

	if err != nil || succeeded != 0 {
		t.Fatalf("succeeded, err = %d, %v", succeeded, err)
	}
	if len(skipped) != 1 || skipped[0].path != "/remote/link" {
		t.Errorf("skipped = %+v, want exactly one entry for /remote/link", skipped)
	}
	entries, readErr := os.ReadDir(localDir)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if len(entries) != 0 {
		t.Errorf("local dir = %v, want nothing created for a skipped symlink", entries)
	}
}

func TestRunRemotePasteRefusesToOverwriteAnExistingDestination(t *testing.T) {
	localDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(localDir, "a.txt"), []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}
	client := &fakeRemoteClient{
		entries: map[string][]fsops.Entry{"/remote": {{Name: "a.txt", Type: fsops.TypeFile}}},
		content: map[string][]byte{"/remote/a.txt": []byte("original")},
	}

	succeeded, _, err := runRemotePaste([]string{filepath.Join(localDir, "a.txt")}, nil, false, client, "/remote")

	if succeeded != 0 || err == nil {
		t.Fatalf("succeeded, err = %d, %v, want a refusal and nothing copied", succeeded, err)
	}
	if got := string(client.content["/remote/a.txt"]); got != "original" {
		t.Errorf("existing remote content = %q, want it left untouched", got)
	}
}

func TestRunRemotePasteCopiesADirectoryRecursively(t *testing.T) {
	client := &fakeRemoteClient{entries: map[string][]fsops.Entry{
		// "/remote/dir" needs an entry in its own parent's list too —
		// Lstat looks a path up by finding it as a named child of
		// path.Dir(p), the same gotcha remoteops_test.go's own
		// TestChmodDirsRecursiveRemoteAppliesToDirectoriesOnlyNotFiles
		// documents.
		"/remote":         {{Name: "dir", Type: fsops.TypeDir, IsDir: true}},
		"/remote/dir":     {{Name: "file.txt", Type: fsops.TypeFile}, {Name: "sub", Type: fsops.TypeDir, IsDir: true}},
		"/remote/dir/sub": {{Name: "nested.txt", Type: fsops.TypeFile}},
	}, content: map[string][]byte{
		"/remote/dir/file.txt":       []byte("top"),
		"/remote/dir/sub/nested.txt": []byte("deep"),
	}}
	localDir := t.TempDir()

	succeeded, skipped, err := runRemotePaste([]string{"/remote/dir"}, client, false, nil, localDir)

	if err != nil || succeeded != 1 || len(skipped) != 0 {
		t.Fatalf("succeeded, skipped, err = %d, %v, %v", succeeded, skipped, err)
	}
	top, err := os.ReadFile(filepath.Join(localDir, "dir", "file.txt"))
	if err != nil || string(top) != "top" {
		t.Errorf("dir/file.txt = %q, %v, want %q", top, err, "top")
	}
	deep, err := os.ReadFile(filepath.Join(localDir, "dir", "sub", "nested.txt"))
	if err != nil || string(deep) != "deep" {
		t.Errorf("dir/sub/nested.txt = %q, %v, want %q", deep, err, "deep")
	}
}

// TestRunRemoteArchiveExtractionUploadsAMemberToARemoteDirectory pins
// the destination-is-itself-remote case pasteInto routes here once
// Root.remoteArchiveExtractionFor has already resolved a clipboard
// entry to a real, already-downloaded local archive copy: extract
// into a throwaway local temp directory, then upload the result the
// same way an ordinary local-source Paste to a remote destination
// already does.
func TestRunRemoteArchiveExtractionUploadsAMemberToARemoteDirectory(t *testing.T) {
	zipPath := writeTestZip(t, t.TempDir(), "archive.zip", map[string]string{"member.txt": "hello from the archive"})
	client := &fakeRemoteClient{entries: map[string][]fsops.Entry{"/remote": nil}}

	succeeded, skipped, err := runRemoteArchiveExtraction(zipPath, []archive.Entry{{Path: "member.txt"}}, client, "/remote")

	if err != nil || succeeded != 1 || len(skipped) != 0 {
		t.Fatalf("succeeded, skipped, err = %d, %v, %v", succeeded, skipped, err)
	}
	if got := string(client.content["/remote/member.txt"]); got != "hello from the archive" {
		t.Errorf("uploaded content = %q, want %q", got, "hello from the archive")
	}
}

// TestRunRemoteArchiveExtractionUploadsADirectoryMemberRecursively
// pins that a marked directory member extracts and uploads everything
// nested under it, not just its own top-level entry.
func TestRunRemoteArchiveExtractionUploadsADirectoryMemberRecursively(t *testing.T) {
	zipPath := writeTestZip(t, t.TempDir(), "archive.zip", map[string]string{
		"src/main.go":     "package main\n",
		"src/lib/util.go": "package lib\n",
	})
	client := &fakeRemoteClient{entries: map[string][]fsops.Entry{"/remote": nil}}

	succeeded, skipped, err := runRemoteArchiveExtraction(zipPath, []archive.Entry{{Path: "src", IsDir: true}}, client, "/remote")

	if err != nil || succeeded != 1 || len(skipped) != 0 {
		t.Fatalf("succeeded, skipped, err = %d, %v, %v", succeeded, skipped, err)
	}
	if got := string(client.content["/remote/src/main.go"]); got != "package main\n" {
		t.Errorf("uploaded src/main.go = %q, want %q", got, "package main\n")
	}
	if got := string(client.content["/remote/src/lib/util.go"]); got != "package lib\n" {
		t.Errorf("uploaded src/lib/util.go = %q, want %q", got, "package lib\n")
	}
}

func TestFinishRemotePasteClearsTheClipboardAfterACleanMove(t *testing.T) {
	r := newTestRemoteRoot(t)
	r.clipboardSourceClient = r.panel.remote
	r.clipboard = []string{"/remote/b.txt"}
	r.clipboardCut = true

	r.finishRemotePaste(r.panel, 1, nil, nil, true)

	if len(r.clipboard) != 0 {
		t.Errorf("clipboard = %v, want it cleared after a clean move", r.clipboard)
	}
	if r.clipboardSourceClient != nil {
		t.Error("clipboardSourceClient still set after the clipboard was cleared")
	}
}

// TestFinishRemotePasteKeepsTheClipboardWhenAMoveFails pins the reason
// finishRemotePaste checks firstErr before clearing anything: a failed
// move must leave its own source on the clipboard so the user can fix
// whatever went wrong (a name collision, a dropped connection, ...) and
// retry the same Paste, instead of silently losing track of what was
// being moved.
func TestFinishRemotePasteKeepsTheClipboardWhenAMoveFails(t *testing.T) {
	r := newTestRemoteRoot(t)
	r.clipboardSourceClient = r.panel.remote
	r.clipboard = []string{"/remote/b.txt"}
	r.clipboardCut = true

	r.finishRemotePaste(r.panel, 0, nil, errors.New("boom"), true)

	if len(r.clipboard) == 0 {
		t.Error("clipboard was cleared despite the move failing")
	}
}

// TestFinishRemotePasteReloadsTheDestinationPanel confirms the visible
// result guarantee every other paste path in this project already
// gives (see finishPasteJob/extractClipboardArchive's own identical
// doc comments): once runRemotePaste has actually mutated the remote
// directory, the panel showing it must reflect that without a manual
// reload.
func TestFinishRemotePasteReloadsTheDestinationPanel(t *testing.T) {
	r := newTestRemoteRoot(t)
	client := r.panel.remote.(*fakeRemoteClient)
	client.entries["/remote"] = append(client.entries["/remote"], fsops.Entry{Name: "newfile.txt", Type: fsops.TypeFile})

	r.finishRemotePaste(r.panel, 1, nil, nil, false)

	var found bool
	for row := 1; row < r.panel.table.GetRowCount(); row++ {
		if ref, ok := r.panel.rowRef(row); ok && ref.name == "newfile.txt" {
			found = true
		}
	}
	if !found {
		t.Error("panel was not reloaded after a successful paste — newfile.txt is missing from the table")
	}
}
