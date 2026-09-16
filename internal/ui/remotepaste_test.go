package ui

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rivo/tview"

	"github.com/jagottsicher/breakthrough/internal/archive"
	"github.com/jagottsicher/breakthrough/internal/fsops"
	"github.com/jagottsicher/breakthrough/internal/remotefs"
)

// isolateRemotePasteIO is isolatePasteIO's own remote counterpart —
// wraps runPasteTransferItem instead of fsCopy/fsMove, the same
// "still call through to the real implementation, just signal once it
// actually returns" shape, for a test that needs to deterministically
// wait for a remote-involving pasteWalk's own background goroutine.
func isolateRemotePasteIO(t *testing.T) <-chan struct{} {
	t.Helper()
	done := make(chan struct{}, 64)
	orig := runPasteTransferItem
	runPasteTransferItem = func(src, dest transferSide, srcPath, destPath string, force bool, mode fsops.OverwriteMode, onFile func(string), onBytes func(int64), skippedSymlinks *[]string) (bool, error) {
		skipped, err := orig(src, dest, srcPath, destPath, force, mode, onFile, onBytes, skippedSymlinks)
		done <- struct{}{}
		return skipped, err
	}
	t.Cleanup(func() { runPasteTransferItem = orig })
	return done
}

// newRemotePasteTestJob mirrors newPasteTestJob (pasteconflict_test.go)
// with srcClient/destClient additionally set — nil for whichever end is
// local, matching every other "nil means local" call site in this
// package.
func newRemotePasteTestJob(r *Root, cut bool, destDir string, total int, srcClient, destClient remotefs.Client) *pasteJob {
	ctx, cancel := context.WithCancel(context.Background())
	job := &pasteJob{
		ctx: ctx, cancel: cancel, cut: cut, destDir: destDir, total: total, remaining: total,
		destDirs:   map[string]bool{destDir: true},
		srcClient:  srcClient,
		destClient: destClient,
	}
	r.pasteJob = job
	return job
}

func TestPasteWalkUploadsALocalFileToARemoteDirectory(t *testing.T) {
	localDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(localDir, "local.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	client := &fakeRemoteClient{entries: map[string][]fsops.Entry{"/remote": nil}}
	job := newRemotePasteTestJob(r, false, "/remote", 1, nil, client)

	done := isolateRemotePasteIO(t)
	r.pasteWalk(job, []string{filepath.Join(localDir, "local.txt")})
	waitPasteIO(t, done, 1)

	if got := string(client.content["/remote/local.txt"]); got != "hello" {
		t.Errorf("uploaded content = %q, want %q", got, "hello")
	}
	if _, err := os.Stat(filepath.Join(localDir, "local.txt")); err != nil {
		t.Error("a plain Copy must leave the local source file in place")
	}
}

// TestPasteOneRemoteReportsTheRealCurrentFileSize pins a real,
// live-reported gap: without a real per-file size lookup, a large
// single file (a video, say) copied to or from a remote connection
// showed a permanently empty progress bar for its entire transfer —
// currentFileSize stuck at 0 read as "stuck", not "still copying",
// since fileFrac (see pasteProgressText) can never be anything but 0
// without a real size to divide by. onFile now looks the real size up
// via a plain Lstat on whichever side src lives on, the same call
// pasteWalk's own conflict check already makes for free elsewhere.
func TestPasteOneRemoteReportsTheRealCurrentFileSize(t *testing.T) {
	localDir := t.TempDir()
	content := bytes.Repeat([]byte("a"), 12345)
	if err := os.WriteFile(filepath.Join(localDir, "big.bin"), content, 0o644); err != nil {
		t.Fatal(err)
	}
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	client := &fakeRemoteClient{entries: map[string][]fsops.Entry{"/remote": nil}}
	job := newRemotePasteTestJob(r, false, "/remote", 1, nil, client)

	done := isolateRemotePasteIO(t)
	r.pasteWalk(job, []string{filepath.Join(localDir, "big.bin")})
	waitPasteIO(t, done, 1)

	if got := job.currentFileSize.Load(); got != int64(len(content)) {
		t.Errorf("currentFileSize = %d, want %d (the file's own real size, not the previous always-0 default)", got, len(content))
	}
	if got := job.currentFileBytes.Load(); got != int64(len(content)) {
		t.Errorf("currentFileBytes = %d, want %d (fully copied)", got, len(content))
	}
}

func TestPasteWalkDownloadsARemoteFileToALocalDirectory(t *testing.T) {
	client := &fakeRemoteClient{
		entries: map[string][]fsops.Entry{"/remote": {{Name: "a.txt", Type: fsops.TypeFile}}},
		content: map[string][]byte{"/remote/a.txt": []byte("hi")},
	}
	localDir := t.TempDir()
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	job := newRemotePasteTestJob(r, false, localDir, 1, client, nil)

	done := isolateRemotePasteIO(t)
	r.pasteWalk(job, []string{"/remote/a.txt"})
	waitPasteIO(t, done, 1)

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

func TestPasteWalkMoveRemovesTheLocalSourceAfterUploading(t *testing.T) {
	localDir := t.TempDir()
	srcPath := filepath.Join(localDir, "local.txt")
	if err := os.WriteFile(srcPath, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	client := &fakeRemoteClient{entries: map[string][]fsops.Entry{"/remote": nil}}
	job := newRemotePasteTestJob(r, true, "/remote", 1, nil, client)

	done := isolateRemotePasteIO(t)
	r.pasteWalk(job, []string{srcPath})
	waitPasteIO(t, done, 1)
	// removeTransferSource runs synchronously, right after
	// runPasteTransferItem returns, inside the very same background
	// goroutine — give it a moment to actually finish before asserting.
	time.Sleep(20 * time.Millisecond)

	if _, err := os.Stat(srcPath); err == nil {
		t.Error("a Cut+Paste must remove the local source once the upload succeeds")
	}
	if _, err := client.Stat("/remote/local.txt"); err != nil {
		t.Error("the uploaded file is missing at the destination")
	}
}

func TestPasteWalkSkipsASymlinkSourceWithoutCopyingItAndLeavesItInPlace(t *testing.T) {
	client := &fakeRemoteClient{entries: map[string][]fsops.Entry{
		"/remote": {{Name: "link", Type: fsops.TypeSymlinkFile}},
	}}
	localDir := t.TempDir()
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	job := newRemotePasteTestJob(r, true, localDir, 1, client, nil)

	done := isolateRemotePasteIO(t)
	r.pasteWalk(job, []string{"/remote/link"})
	waitPasteIO(t, done, 1)
	time.Sleep(20 * time.Millisecond)

	if len(job.skippedSymlinks) != 1 || job.skippedSymlinks[0] != "/remote/link" {
		t.Errorf("skippedSymlinks = %v, want exactly [\"/remote/link\"]", job.skippedSymlinks)
	}
	entries, readErr := os.ReadDir(localDir)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if len(entries) != 0 {
		t.Errorf("local dir = %v, want nothing created for a skipped symlink", entries)
	}
	// A Cut of a symlink that was skipped, not copied, must leave the
	// original in place — see pasteOneRemote's own !skipped guard.
	if _, err := client.Stat("/remote/link"); err != nil {
		t.Error("a skipped symlink's own source must survive a Cut")
	}
}

func TestPasteWalkCopiesADirectoryRecursively(t *testing.T) {
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
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	job := newRemotePasteTestJob(r, false, localDir, 1, client, nil)

	done := isolateRemotePasteIO(t)
	r.pasteWalk(job, []string{"/remote/dir"})
	waitPasteIO(t, done, 1)

	top, err := os.ReadFile(filepath.Join(localDir, "dir", "file.txt"))
	if err != nil || string(top) != "top" {
		t.Errorf("dir/file.txt = %q, %v, want %q", top, err, "top")
	}
	deep, err := os.ReadFile(filepath.Join(localDir, "dir", "sub", "nested.txt"))
	if err != nil || string(deep) != "deep" {
		t.Errorf("dir/sub/nested.txt = %q, %v, want %q", deep, err, "deep")
	}
}

// TestPasteWalkNeverCopiesOverAnExistingRemoteDestination pins the
// actual bug this whole engine unification fixes: pasteWalk used to
// hand a remote-involving item straight to a hard error the moment its
// destination already existed — this proves it never even attempts the
// real copy for one, the same "never touch it, hand off to the dialog
// instead" guarantee TestPasteWalkNeverAttemptsWhenDestinationOverlapsSource
// already pins for the unrelated overlap case. Doesn't assert
// job.current itself (see TestPasteConflictFoundOpensDialogForARemoteDestinationInsteadOfErroring
// for that, called directly — the real hand-off happens on the far side
// of a QueueUpdateDraw hop nothing here drains, the same reason every
// other conflict test in this package calls pasteConflictFound
// directly rather than going through pasteWalk end to end).
func TestPasteWalkNeverCopiesOverAnExistingRemoteDestination(t *testing.T) {
	localDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(localDir, "a.txt"), []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}
	client := &fakeRemoteClient{
		entries: map[string][]fsops.Entry{"/remote": {{Name: "a.txt", Type: fsops.TypeFile}}},
		content: map[string][]byte{"/remote/a.txt": []byte("original")},
	}
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	job := newRemotePasteTestJob(r, false, "/remote", 1, nil, client)

	done := isolateRemotePasteIO(t)
	r.pasteWalk(job, []string{filepath.Join(localDir, "a.txt")})

	select {
	case <-done:
		t.Fatal("runPasteTransferItem was called — an existing remote destination must go to the conflict dialog, never a direct copy")
	case <-time.After(200 * time.Millisecond):
		// Expected: pasteWalk found the conflict and handed it off
		// instead of ever attempting the real copy.
	}
	if got := string(client.content["/remote/a.txt"]); got != "original" {
		t.Errorf("existing remote content = %q, want it left untouched", got)
	}
}

// newRemotePasteTestConflict is newPasteTestConflict's own remote
// counterpart — src/dst can each be local or remote, matching whichever
// of srcClient/destClient is nil (see transferSide's own "nil means
// local" convention).
func newRemotePasteTestConflict(t *testing.T, srcClient, destClient remotefs.Client, src, dst string) pasteConflict {
	t.Helper()
	srcInfo, err := (transferSide{client: srcClient}).lstat(src)
	if err != nil {
		t.Fatalf("lstat(src): %v", err)
	}
	dstInfo, err := (transferSide{client: destClient}).lstat(dst)
	if err != nil {
		t.Fatalf("lstat(dst): %v", err)
	}
	return pasteConflict{src: src, dst: dst, srcInfo: srcInfo, dstInfo: dstInfo}
}

// TestPasteConflictFoundOpensDialogForARemoteDestinationInsteadOfErroring
// pins the actual point of folding a remote-involving Paste into this
// same job type: pasting into an existing remote destination used to
// fail outright, with no way to choose Overwrite/Merge/Skip the way a
// local conflict always could — now it raises the exact same dialog
// (see TestPasteConflictOpensDialogInsteadOfErroring, its identical
// local-only counterpart), sharing every one of its resolution options.
func TestPasteConflictFoundOpensDialogForARemoteDestinationInsteadOfErroring(t *testing.T) {
	localDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(localDir, "a.txt"), []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}
	client := &fakeRemoteClient{
		entries: map[string][]fsops.Entry{"/remote": {{Name: "a.txt", Type: fsops.TypeFile}}},
		content: map[string][]byte{"/remote/a.txt": []byte("original")},
	}
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	job := newRemotePasteTestJob(r, false, "/remote", 1, nil, client)
	conflict := newRemotePasteTestConflict(t, nil, client, filepath.Join(localDir, "a.txt"), "/remote/a.txt")

	r.pasteConflictFound(job, conflict)

	if job.current == nil {
		t.Fatal("expected a conflict dialog to be raised, job.current is nil")
	}
	if job.current.dst != "/remote/a.txt" {
		t.Errorf("conflict.dst = %q, want %q", job.current.dst, "/remote/a.txt")
	}
	if got := string(client.content["/remote/a.txt"]); got != "original" {
		t.Errorf("existing remote content = %q, want it left untouched until the conflict is resolved", got)
	}
}

// TestChooseConflictResolutionOverwriteAppliesToARemoteDestination pins
// that actually choosing "Overwrite" on that same dialog carries
// through and replaces the remote file's content — the dialog isn't
// just cosmetically reachable, its answer really drives
// pasteOneRemote.
func TestChooseConflictResolutionOverwriteAppliesToARemoteDestination(t *testing.T) {
	localDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(localDir, "a.txt"), []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}
	client := &fakeRemoteClient{
		entries: map[string][]fsops.Entry{"/remote": {{Name: "a.txt", Type: fsops.TypeFile}}},
		content: map[string][]byte{"/remote/a.txt": []byte("original")},
	}
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	job := newRemotePasteTestJob(r, false, "/remote", 1, nil, client)
	conflict := newRemotePasteTestConflict(t, nil, client, filepath.Join(localDir, "a.txt"), "/remote/a.txt")
	r.pasteConflictFound(job, conflict)
	if job.current == nil {
		t.Fatal("setup: expected a conflict dialog to be raised")
	}

	done := isolateRemotePasteIO(t)
	r.chooseConflictResolution(resolveOverwrite, false)
	waitPasteIO(t, done, 1)

	if got := string(client.content["/remote/a.txt"]); got != "new" {
		t.Errorf("remote content after Overwrite = %q, want %q", got, "new")
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
// doc comments): once runRemoteArchiveExtraction has actually mutated
// the remote directory, the panel showing it must reflect that without
// a manual reload.
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
