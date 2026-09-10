package ui

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/rivo/tview"

	"github.com/jagottsicher/breakthrough/internal/config"
	"github.com/jagottsicher/breakthrough/internal/fsops"
)

// backdateTrashItem rewrites id's own deleted_at record directly, the
// same "info/<id>.trashinfo" layout fsops.MoveToTrash itself writes
// (see its own doc comment) — internal/fsops has no exported way to
// simulate an old item, and duplicating these two literals here is
// cheaper than exporting a test-only helper from production code just
// for this.
func backdateTrashItem(t *testing.T, trashDir, id string, age time.Duration) {
	t.Helper()
	path := filepath.Join(trashDir, "info", id+".trashinfo")
	deletedAt := time.Now().Add(-age).UTC().Format(time.RFC3339Nano)
	if err := config.SetKey(path, "deleted_at", deletedAt); err != nil {
		t.Fatal(err)
	}
}

// newTestRootWithFile creates a directory containing exactly one file
// ("a.txt") and a Root rooted there, with the table cursor already
// focused on that file (row 0 is always ".." — see focusRow's own
// callers elsewhere in this package). Both $XDG_RUNTIME_DIR and
// $XDG_DATA_HOME are pointed at their own fresh temp dirs — session-
// scoped and persistent trash resolve to one or the other (see
// session.TrashDir), and isolating only whichever TrashPersistent
// currently defaults to would silently break the moment that default
// changes again; isolating both means this test's own trash is always
// private regardless.
func newTestRootWithFile(t *testing.T) (r *Root, dir, file string) {
	t.Helper()
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	dir = t.TempDir()
	file = filepath.Join(dir, "a.txt")
	if err := os.WriteFile(file, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}

	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.panel.focusRow(1) // off ".." onto the one real entry
	return r, dir, file
}

func TestMoveSelectionToTrashMovesFileAndListsIt(t *testing.T) {
	r, _, file := newTestRootWithFile(t)

	r.moveSelectionToTrash()

	if _, err := os.Lstat(file); !os.IsNotExist(err) {
		t.Fatalf("a.txt still exists at its original path (err=%v)", err)
	}

	trashDir, err := r.trashDir()
	if err != nil {
		t.Fatalf("trashDir: %v", err)
	}
	items, err := fsops.ListTrash(trashDir)
	if err != nil || len(items) != 1 || items[0].OriginalPath != file {
		t.Fatalf("ListTrash = %+v, %v, want exactly one item for %s", items, err, file)
	}
	if r.activePage == errorPage {
		t.Errorf("moveSelectionToTrash reported an error: %q", r.errorView.GetText(true))
	}
}

// TestMoveSelectionToTrashClearsDetailsShowingSameFile pins the user's
// own explicit request extended to Trash: Details, if it's showing the
// very entry that just got trashed, must not keep claiming stale data
// for a file that isn't there any more — cleared to "(nothing
// selected)" (see refreshDetailsIfShowing's own doc comment on why an
// obscure trash-internal path isn't worth following it to instead).
func TestMoveSelectionToTrashClearsDetailsShowingSameFile(t *testing.T) {
	r, _, file := newTestRootWithFile(t)
	r.SetRect(0, 0, 100, 40)
	r.showDetailsSidebar()
	if r.detailsTarget != file {
		t.Fatalf("setup: detailsTarget = %q, want %q", r.detailsTarget, file)
	}

	r.moveSelectionToTrash()

	if r.detailsTarget != "" {
		t.Errorf("detailsTarget after Trash = %q, want \"\" (cleared)", r.detailsTarget)
	}
}

// TestRemoveClearsDetailsShowingSameFile is the same pin for a
// permanent delete (see openRemoveConfirm's own confirm callback).
func TestRemoveClearsDetailsShowingSameFile(t *testing.T) {
	r, _, file := newTestRootWithFile(t)
	r.SetRect(0, 0, 100, 40)
	r.showDetailsSidebar()
	if r.detailsTarget != file {
		t.Fatalf("setup: detailsTarget = %q, want %q", r.detailsTarget, file)
	}

	r.openRemoveConfirm()
	r.confirmDialog.SetCurrentItem(0) // "Yes, delete permanently"
	r.acceptConfirm()

	if r.detailsTarget != "" {
		t.Errorf("detailsTarget after Remove = %q, want \"\" (cleared)", r.detailsTarget)
	}
}

// TestRestoreFromTrashRefreshesDetailsShowingSameFile pins Restore's own
// half: Details following the trashed item (shown while browsing the
// trash itself) back to its own real, original path once it's restored.
//
// Restore now runs through the same asynchronous Paste machinery an
// ordinary Cut+Paste already does (see restoreSelectionFromTrash's own
// doc comment) — applyPasteOneResult is what actually updates
// detailsTarget, but only ever runs via a QueueUpdateDraw hop no test
// here has a live Application.Run() loop to service (see
// applyPasteOneResult's own doc comment on exactly this gap). Calling it
// directly, the same way TestPasteMoveRefreshesDetailsShowingSameFile
// already does for an ordinary Cut+Paste, exercises the one thing this
// test actually pins without needing that loop — the real on-disk move
// itself is performed by the direct fsops.RestoreFromTrash call just
// below instead, exactly the same real effect the async path would
// produce.
func TestRestoreFromTrashRefreshesDetailsShowingSameFile(t *testing.T) {
	r, dir, file := newTestRootWithFile(t)
	r.SetRect(0, 0, 100, 40)

	r.moveSelectionToTrash()
	r.openTrash()
	r.panel.focusRow(1) // the one trashed item
	r.showDetailsSidebar()

	_, trashPath, ok := r.panel.CurrentRowPath()
	if !ok {
		t.Fatal("setup: no current row in the trash listing")
	}
	if r.detailsTarget != trashPath {
		t.Fatalf("setup: detailsTarget = %q, want the trash-internal path %q", r.detailsTarget, trashPath)
	}

	trashDir, err := r.trashDir()
	if err != nil {
		t.Fatalf("trashDir: %v", err)
	}
	trashItems, err := fsops.ListTrash(trashDir)
	if err != nil || len(trashItems) != 1 {
		t.Fatalf("ListTrash = %+v, %v, want exactly one item", trashItems, err)
	}
	if err := fsops.RestoreFromTrash(trashItems[0], trashDir); err != nil {
		t.Fatalf("RestoreFromTrash: %v", err)
	}

	job := newPasteTestJob(r, true, "", 1)
	r.applyPasteOneResult(job, trashPath, file, nil)

	if r.detailsTarget != file {
		t.Errorf("detailsTarget after Restore = %q, want the original path %q", r.detailsTarget, file)
	}
	if _, err := os.Stat(filepath.Join(dir, "a.txt")); err != nil {
		t.Fatalf("setup sanity: restored file missing: %v", err)
	}
}

// TestRestoreConflictOpensTheSameSixOptionDialog pins the whole point of
// this feature: restoring an item whose original path now has something
// else sitting on it (recreated after the original was trashed, say) no
// longer refuses outright with a bare error the way
// fsops.RestoreFromTrash's own simpler contract does — it opens the same
// conflict dialog an ordinary Paste conflict already does (see
// pasteConflictFound), leaving the existing file untouched until a
// decision is made. Calls pasteConflictFound directly, the same
// established reason TestPasteConflictOpensDialogInsteadOfErroring
// already does: pasteWalk only ever reaches this through
// QueueUpdateDraw, which never fires without a live Application.Run()
// loop.
func TestRestoreConflictOpensTheSameSixOptionDialog(t *testing.T) {
	dir := t.TempDir()
	original := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(original, []byte("original"), 0o644); err != nil {
		t.Fatal(err)
	}

	trashDir := t.TempDir()
	if err := fsops.MoveToTrash(original, trashDir); err != nil {
		t.Fatalf("MoveToTrash: %v", err)
	}
	// Something new now sits at the original path — restoring must
	// collide with it, not silently overwrite or refuse outright.
	if err := os.WriteFile(original, []byte("recreated after delete"), 0o644); err != nil {
		t.Fatal(err)
	}

	trashItems, err := fsops.ListTrash(trashDir)
	if err != nil || len(trashItems) != 1 {
		t.Fatalf("ListTrash = %+v, %v, want exactly one item", trashItems, err)
	}
	trashPath := trashItems[0].Path(trashDir)

	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.SetRect(0, 0, 100, 40)

	job := newRestoreTestJob(r, []string{original}, trashDir, 1)
	r.pasteConflictFound(job, newPasteTestConflict(t, trashPath, original))

	if r.activePage != pasteConflictPage {
		t.Errorf("activePage = %q, want %q — a restore conflict should open the same dialog an ordinary Paste conflict does", r.activePage, pasteConflictPage)
	}
	got, err := os.ReadFile(original)
	if err != nil || string(got) != "recreated after delete" {
		t.Errorf("the file at the original path should be untouched until a decision is made, got %q, %v", got, err)
	}
	if _, err := os.Lstat(trashPath); err != nil {
		t.Errorf("the trashed payload should still be there too, untouched until a decision is made: %v", err)
	}
}

// TestRestoreConflictOverwriteRestoresAndRemovesSidecar pins the
// Overwrite path end to end: the real fsMove goes through (see
// isolatePasteIO), the file at the original path becomes the restored
// content, and — the one thing genuinely new to Restore's own path
// through pasteOne, not something an ordinary Paste ever has to do —
// the trashed item's own .trashinfo sidecar is gone afterward too (see
// fsops.RemoveTrashSidecar), the same cleanup fsops.RestoreFromTrash's
// own simpler, no-conflict path already does in one combined call.
func TestRestoreConflictOverwriteRestoresAndRemovesSidecar(t *testing.T) {
	dir := t.TempDir()
	original := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(original, []byte("original"), 0o644); err != nil {
		t.Fatal(err)
	}
	trashDir := t.TempDir()
	if err := fsops.MoveToTrash(original, trashDir); err != nil {
		t.Fatalf("MoveToTrash: %v", err)
	}
	if err := os.WriteFile(original, []byte("recreated after delete"), 0o644); err != nil {
		t.Fatal(err)
	}
	trashItems, err := fsops.ListTrash(trashDir)
	if err != nil || len(trashItems) != 1 {
		t.Fatalf("ListTrash = %+v, %v, want exactly one item", trashItems, err)
	}
	trashPath := trashItems[0].Path(trashDir)

	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.SetRect(0, 0, 100, 40)

	job := newRestoreTestJob(r, []string{original}, trashDir, 1)
	r.pasteConflictFound(job, newPasteTestConflict(t, trashPath, original))

	done := isolatePasteIO(t)
	r.chooseConflictResolution(resolveOverwrite, false)
	waitPasteIO(t, done, 1)
	// chooseConflictResolution's own real move happens on a background
	// goroutine (see resolveConflictAsync) — waitPasteIO only waits for
	// the wrapped fsMove call itself to return, not for the
	// QueueUpdateDraw-gated applyPasteOneResult afterward (see its own
	// doc comment on why nothing here can). The sidecar removal lives in
	// applyPasteOneResult, so it's exercised directly, the same
	// established reason every other detailsTarget/clipboard assertion
	// in this file already does.
	r.applyPasteOneResult(job, trashPath, original, nil)

	got, err := os.ReadFile(original)
	if err != nil || string(got) != "original" {
		t.Errorf("original path content = %q, %v, want the restored %q", got, err, "original")
	}
	// Checked via a second RemoveTrashSidecar call, not fsops.ListTrash:
	// ListTrash silently self-heals an orphaned sidecar itself the
	// moment it notices the payload is already gone (see its own doc
	// comment), which would report "empty" here regardless of whether
	// applyPasteOneResult's own cleanup actually ran at all — this way
	// genuinely distinguishes "already removed" (os.ErrNotExist) from
	// "still there" (nil).
	if err := fsops.RemoveTrashSidecar(trashDir, trashItems[0].ID); !os.IsNotExist(err) {
		t.Errorf("sidecar removal after a confirmed restore = %v, want os.ErrNotExist (already gone)", err)
	}
}

// TestRestoreConflictSkipLeavesEverythingInPlace pins "Skip": the file
// already at the original path is left exactly as it was, and the
// trashed item is neither removed from the trash nor restored — a
// skipped conflict is a genuine no-op, not a partial restore.
func TestRestoreConflictSkipLeavesEverythingInPlace(t *testing.T) {
	dir := t.TempDir()
	original := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(original, []byte("original"), 0o644); err != nil {
		t.Fatal(err)
	}
	trashDir := t.TempDir()
	if err := fsops.MoveToTrash(original, trashDir); err != nil {
		t.Fatalf("MoveToTrash: %v", err)
	}
	if err := os.WriteFile(original, []byte("recreated after delete"), 0o644); err != nil {
		t.Fatal(err)
	}
	trashItems, err := fsops.ListTrash(trashDir)
	if err != nil || len(trashItems) != 1 {
		t.Fatalf("ListTrash = %+v, %v, want exactly one item", trashItems, err)
	}
	trashPath := trashItems[0].Path(trashDir)

	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.SetRect(0, 0, 100, 40)

	job := newRestoreTestJob(r, []string{original}, trashDir, 1)
	r.pasteConflictFound(job, newPasteTestConflict(t, trashPath, original))
	r.chooseConflictResolution(resolveSkip, false)

	got, err := os.ReadFile(original)
	if err != nil || string(got) != "recreated after delete" {
		t.Errorf("original path content = %q, %v, want the untouched %q", got, err, "recreated after delete")
	}
	remaining, err := fsops.ListTrash(trashDir)
	if err != nil || len(remaining) != 1 {
		t.Errorf("ListTrash after Skip = %+v, %v, want the one item still there, unrestored", remaining, err)
	}
	if r.activePage == pasteConflictPage {
		t.Error("the dialog should have closed after Skip")
	}
}

// TestRestoreDoesNotClearUnrelatedClipboard pins the real, easy-to-miss
// hazard reusing the Paste machinery for Restore introduced: Restore
// also sets job.cut (it genuinely is a move), and finishPasteJob's own
// clipboard-clearing was, until this was guarded, keyed on job.cut
// alone — which would silently wipe out a completely unrelated Copy/Cut
// the user still had pending, just because a Restore happened to finish
// while it was sitting there. Restore was never sourced from
// r.clipboard in the first place, so it must never touch it either way.
func TestRestoreDoesNotClearUnrelatedClipboard(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.target = filepath.Join(dir, "apple.txt")
	r.copyToClipboard()
	if len(r.clipboard) == 0 {
		t.Fatal("setup: clipboard should hold something unrelated to the restore below")
	}

	job := newRestoreTestJob(r, []string{filepath.Join(dir, "restored.txt")}, t.TempDir(), 1)
	r.applyPasteOneResult(job, filepath.Join(dir, "trash-payload"), filepath.Join(dir, "restored.txt"), nil)

	if len(r.clipboard) == 0 {
		t.Error("an unrelated pending Copy should survive a Restore finishing — Restore was never sourced from the clipboard")
	}
}

// TestRestoreMultiSelectFromDifferentOriginalDirsRestoresBoth pins the
// actual reason Restore couldn't just reuse startPaste's own single
// shared destDir unchanged: a multi-select restore can pull items whose
// own OriginalPath lived in entirely different directories, unlike an
// ordinary Paste's items, which always share one destination. Both
// items' own real fsMove calls are exercised here (see isolatePasteIO),
// proving pasteWalk's own per-item restoreDests[i] lookup — not a single
// job.destDir — is what actually drives each one's destination.
func TestRestoreMultiSelectFromDifferentOriginalDirsRestoresBoth(t *testing.T) {
	dirA := t.TempDir()
	dirB := t.TempDir()
	fileA := filepath.Join(dirA, "a.txt")
	fileB := filepath.Join(dirB, "b.txt")
	if err := os.WriteFile(fileA, []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fileB, []byte("b"), 0o644); err != nil {
		t.Fatal(err)
	}

	trashDir := t.TempDir()
	if err := fsops.MoveToTrash(fileA, trashDir); err != nil {
		t.Fatalf("MoveToTrash(a): %v", err)
	}
	if err := fsops.MoveToTrash(fileB, trashDir); err != nil {
		t.Fatalf("MoveToTrash(b): %v", err)
	}
	trashItems, err := fsops.ListTrash(trashDir)
	if err != nil || len(trashItems) != 2 {
		t.Fatalf("ListTrash = %+v, %v, want exactly two items", trashItems, err)
	}

	byOriginal := map[string]fsops.TrashItem{}
	for _, item := range trashItems {
		byOriginal[item.OriginalPath] = item
	}
	itemA, itemB := byOriginal[fileA], byOriginal[fileB]

	r, err := NewRoot(tview.NewApplication(), dirA)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}

	items := []string{itemA.Path(trashDir), itemB.Path(trashDir)}
	dests := []string{fileA, fileB}
	done := isolatePasteIO(t)
	r.startPaste(items, true, "", false, dests, trashDir)
	waitPasteIO(t, done, 2)

	if got, err := os.ReadFile(fileA); err != nil || string(got) != "a" {
		t.Errorf("fileA after restore = %q, %v, want %q", got, err, "a")
	}
	if got, err := os.ReadFile(fileB); err != nil || string(got) != "b" {
		t.Errorf("fileB after restore = %q, %v, want %q", got, err, "b")
	}
}

func TestOpenRemoveConfirmCancelPreselectedDoesNotDelete(t *testing.T) {
	r, _, file := newTestRootWithFile(t)

	r.openRemoveConfirm()
	if r.activePage != confirmPage {
		t.Fatalf("activePage = %q, want %q", r.activePage, confirmPage)
	}
	if got := r.confirmDialog.GetCurrentItem(); got != 1 {
		t.Fatalf("preselected item = %d, want 1 (Cancel)", got)
	}

	// Enter without ever moving focus - must cancel, never delete.
	r.resolvePurgeConfirmByCurrentFocus(t)

	if _, err := os.Lstat(file); err != nil {
		t.Fatalf("a.txt was removed despite Cancel being preselected: %v", err)
	}
	if r.activePage == confirmPage {
		t.Fatal("purge confirm overlay is still open after resolving it")
	}
}

// TestOpenRemoveConfirmHasATitleBar pins the fix for a real, user-reported
// gap: the Remove/Empty-Trash confirmation used to be a bare List with
// no heading at all, unlike every other dialog in this app (Properties,
// Menu, Options, ...) — see confirmDialogTitleBar's own doc comment. The
// title bar now carries the actual question rather than a generic
// "Confirm" caption — a later, separately user-requested change (see
// openConfirm's own doc comment).
func TestOpenRemoveConfirmHasATitleBar(t *testing.T) {
	r, _, _ := newTestRootWithFile(t)

	r.openRemoveConfirm()

	if got, want := r.confirmDialogTitleBar.GetText(true), " Permanently delete \"a.txt\"? "; got != want {
		t.Errorf("confirmDialogTitleBar text = %q, want %q", got, want)
	}
	if _, _, w, h := r.confirmDialogLayout.GetRect(); w <= 0 || h <= 0 {
		t.Errorf("confirmDialogLayout rect = %dx%d, want a real, positioned size", w, h)
	}
}

// resolvePurgeConfirmByCurrentFocus resolves the currently open
// purgeConfirm exactly the way pressing Enter on the table's current
// selection would: it does not force a particular outcome, unlike
// calling r.acceptConfirm()/r.cancelConfirm() directly would.
func (r *Root) resolvePurgeConfirmByCurrentFocus(t *testing.T) {
	t.Helper()
	switch r.confirmDialog.GetCurrentItem() {
	case 1:
		r.cancelConfirm()
	case 0:
		r.acceptConfirm()
	default:
		t.Fatalf("unexpected purgeConfirm focus %d", r.confirmDialog.GetCurrentItem())
	}
}

func TestOpenRemoveConfirmConfirmedDeletesPermanently(t *testing.T) {
	r, _, file := newTestRootWithFile(t)

	r.openRemoveConfirm()
	r.confirmDialog.SetCurrentItem(0) // deliberately move to "Yes, delete permanently"
	r.resolvePurgeConfirmByCurrentFocus(t)

	if _, err := os.Lstat(file); !os.IsNotExist(err) {
		t.Fatalf("a.txt still exists after a confirmed Remove (err=%v)", err)
	}
	trashDir, err := r.trashDir()
	if err != nil {
		t.Fatalf("trashDir: %v", err)
	}
	items, err := fsops.ListTrash(trashDir)
	if err != nil || len(items) != 0 {
		t.Fatalf("ListTrash = %+v, %v, want empty - Remove must bypass the trash entirely", items, err)
	}
}

func TestRestoreOnlyWorksWhileViewingTrash(t *testing.T) {
	r, _, _ := newTestRootWithFile(t)

	r.restoreSelectionFromTrash()

	if r.activePage != errorPage {
		t.Fatal("restoreSelectionFromTrash outside the trash directory should report an error")
	}
}

// TestRestoreDoesNotWorkFromTrashDirItself pins that browsing to the bare
// trash root (as opposed to its files/ subdirectory) also doesn't count
// as "viewing the trash" for Restore - trashDir only ever contains
// files/ and info/, never a trashed item directly, so a row focused
// there could never resolve to a real TrashItem anyway.
func TestRestoreDoesNotWorkFromTrashDirItself(t *testing.T) {
	r, _, _ := newTestRootWithFile(t)
	r.moveSelectionToTrash()

	trashDir, err := r.trashDir()
	if err != nil {
		t.Fatalf("trashDir: %v", err)
	}
	if err := r.panel.load(trashDir); err != nil {
		t.Fatalf("navigating to the trash root: %v", err)
	}
	r.panel.focusRow(1)

	r.restoreSelectionFromTrash()

	if r.activePage != errorPage {
		t.Fatal("restoreSelectionFromTrash from the bare trash root should report an error")
	}
}

func TestMoveToTrashThenRestoreRoundTrip(t *testing.T) {
	r, _, file := newTestRootWithFile(t)

	r.moveSelectionToTrash()
	if _, err := os.Lstat(file); !os.IsNotExist(err) {
		t.Fatalf("setup: a.txt still exists after moveSelectionToTrash")
	}

	trashDir, err := r.trashDir()
	if err != nil {
		t.Fatalf("trashDir: %v", err)
	}
	// Restore only works from trash/files/, not trashDir itself (which
	// only ever contains files/ and info/, never a trashed item
	// directly) - see restoreSelectionFromTrash's own doc comment.
	if err := r.panel.load(fsops.FilesDir(trashDir)); err != nil {
		t.Fatalf("navigating into the trash's files/ dir: %v", err)
	}
	r.panel.focusRow(1) // off ".." onto the one trashed entry

	r.restoreSelectionFromTrash()

	data, err := os.ReadFile(file)
	if err != nil || string(data) != "hello" {
		t.Fatalf("restored file content = %q, %v, want \"hello\"", data, err)
	}
	items, err := fsops.ListTrash(trashDir)
	if err != nil || len(items) != 0 {
		t.Fatalf("ListTrash after restore = %+v, %v, want empty", items, err)
	}
}

func TestOpenTrashNavigatesToFilesDir(t *testing.T) {
	r, _, _ := newTestRootWithFile(t)
	r.moveSelectionToTrash()

	r.openTrash()

	trashDir, err := r.trashDir()
	if err != nil {
		t.Fatalf("trashDir: %v", err)
	}
	if got, want := filepath.Clean(r.panel.path), filepath.Clean(fsops.FilesDir(trashDir)); got != want {
		t.Fatalf("panel.path after openTrash = %q, want %q", got, want)
	}
	if r.activePage == errorPage {
		t.Errorf("openTrash reported an error: %q", r.errorView.GetText(true))
	}

	// Restore should now work without the user ever having typed a path.
	r.panel.focusRow(1)
	r.restoreSelectionFromTrash()
	if r.activePage == errorPage {
		t.Errorf("restore right after openTrash failed: %q", r.errorView.GetText(true))
	}
}

func TestOpenEmptyTrashConfirmRemovesEverything(t *testing.T) {
	r, _, file := newTestRootWithFile(t)
	r.moveSelectionToTrash()
	if _, err := os.Lstat(file); !os.IsNotExist(err) {
		t.Fatalf("setup: a.txt still exists after moveSelectionToTrash")
	}

	r.openEmptyTrashConfirm()
	if r.activePage != confirmPage {
		t.Fatalf("activePage = %q, want %q", r.activePage, confirmPage)
	}
	r.confirmDialog.SetCurrentItem(0) // "Yes, delete permanently"
	r.resolvePurgeConfirmByCurrentFocus(t)

	trashDir, err := r.trashDir()
	if err != nil {
		t.Fatalf("trashDir: %v", err)
	}
	items, err := fsops.ListTrash(trashDir)
	if err != nil || len(items) != 0 {
		t.Fatalf("ListTrash after Empty Trash = %+v, %v, want empty", items, err)
	}
}

func TestTrashShortcutsNoOpWhileAnOverlayIsOpen(t *testing.T) {
	r, _, file := newTestRootWithFile(t)

	r.openOptions() // any overlay; makes acceptsGlobalShortcut false
	r.TrashShortcut()

	if _, err := os.Lstat(file); err != nil {
		t.Fatalf("TrashShortcut acted while an overlay was open (a.txt gone: %v)", err)
	}
}

// TestMoveSelectionToTrashInsideTrashRedirectsToRemove pins
// moveSelectionToTrash's own redirect (see its doc comment): a second
// "Move to Trash" on something already in the trash has nowhere left to
// go, so it opens the same Remove confirmation "D"/the context menu's
// own "Remove" would, Cancel preselected the same as any other Remove —
// not a silent no-op, and not an unconfirmed delete either.
func TestMoveSelectionToTrashInsideTrashRedirectsToRemove(t *testing.T) {
	r, _, file := newTestRootWithFile(t)
	r.moveSelectionToTrash()
	r.openTrash()
	r.panel.focusRow(1) // off ".." onto the now-trashed a.txt

	r.moveSelectionToTrash()

	if r.activePage != confirmPage {
		t.Fatalf("activePage = %q, want %q", r.activePage, confirmPage)
	}
	if got := r.confirmDialog.GetCurrentItem(); got != 1 {
		t.Fatalf("preselected item = %d, want 1 (Cancel)", got)
	}

	// Cancel must still actually cancel — nothing removed by this redirect alone.
	r.resolvePurgeConfirmByCurrentFocus(t)
	trashDir, err := r.trashDir()
	if err != nil {
		t.Fatalf("trashDir: %v", err)
	}
	items, err := fsops.ListTrash(trashDir)
	if err != nil || len(items) != 1 || items[0].OriginalPath != file {
		t.Fatalf("ListTrash after Cancel = %+v, %v, want the one item still there", items, err)
	}
}

// TestTrashbinShortcutOpensTrash pins TrashbinShortcut's own guarded
// action (see its doc comment on why it's kept despite no longer being
// wired to Ctrl+B): the same navigation openTrash itself does.
func TestTrashbinShortcutOpensTrash(t *testing.T) {
	r, _, _ := newTestRootWithFile(t)
	r.moveSelectionToTrash()

	r.TrashbinShortcut()

	trashDir, err := r.trashDir()
	if err != nil {
		t.Fatalf("trashDir: %v", err)
	}
	if got, want := filepath.Clean(r.panel.path), filepath.Clean(fsops.FilesDir(trashDir)); got != want {
		t.Fatalf("panel.path after TrashbinShortcut = %q, want %q", got, want)
	}
}

// TestTrashbinShortcutNoOpsWhileAnOverlayIsOpen mirrors
// TestTrashShortcutsNoOpWhileAnOverlayIsOpen above for TrashbinShortcut.
func TestTrashbinShortcutNoOpsWhileAnOverlayIsOpen(t *testing.T) {
	r, dir, _ := newTestRootWithFile(t)

	r.openOptions() // any overlay; makes acceptsGlobalShortcut false
	r.TrashbinShortcut()

	if got, want := filepath.Clean(r.panel.path), filepath.Clean(dir); got != want {
		t.Errorf("TrashbinShortcut navigated while an overlay was open: panel.path = %q, want unchanged %q", got, want)
	}
}

// TestPruneTrashAtStartupRemovesOldItemAndReturnsNotice pins the actual
// integration, not just fsops.PruneTrash's own already-tested logic: an
// item older than r.settings.TrashMaxAgeDays is gone from the trash
// afterward, and the returned notice names how many.
func TestPruneTrashAtStartupRemovesOldItemAndReturnsNotice(t *testing.T) {
	r, _, file := newTestRootWithFile(t)
	r.moveSelectionToTrash()

	dir, err := r.trashDir()
	if err != nil {
		t.Fatalf("trashDir: %v", err)
	}
	items, err := fsops.ListTrash(dir)
	if err != nil || len(items) != 1 {
		t.Fatalf("setup ListTrash = %+v, %v, want 1 item", items, err)
	}
	backdateTrashItem(t, dir, items[0].ID, time.Duration(r.settings.TrashMaxAgeDays+10)*24*time.Hour)

	notice := r.pruneTrashAtStartup()

	if notice == "" {
		t.Fatal("pruneTrashAtStartup returned no notice despite an item old enough to prune")
	}
	remaining, err := fsops.ListTrash(dir)
	if err != nil || len(remaining) != 0 {
		t.Fatalf("ListTrash after pruneTrashAtStartup = %+v, %v, want empty", remaining, err)
	}
	if _, err := os.Lstat(file); !os.IsNotExist(err) {
		t.Fatalf("original file reappeared at %s after the aged trash item was pruned", file)
	}
}

// TestPruneTrashAtStartupNoOpWithEmptyTrash is a regression guard for
// NewRoot's own end-of-construction wiring (see its startupNotices):
// an ordinary fresh start, with nothing ever trashed, must not pop an
// error overlay of its own accord.
func TestPruneTrashAtStartupNoOpWithEmptyTrash(t *testing.T) {
	r, _, _ := newTestRootWithFile(t)

	if r.activePage == errorPage {
		t.Fatalf("NewRoot already shows an error overlay with an empty trash: %q", r.errorView.GetText(true))
	}
	if notice := r.pruneTrashAtStartup(); notice != "" {
		t.Errorf("pruneTrashAtStartup() = %q, want \"\" with nothing in the trash", notice)
	}
}

// TestTrashPruneMessageMentionsBothAgeAndQuota pins trashPruneMessage's
// own formatting directly — a pure function, no filesystem needed.
func TestTrashPruneMessageMentionsBothAgeAndQuota(t *testing.T) {
	got := trashPruneMessage(fsops.PruneTrashResult{RemovedByAge: 2, RemovedByQuota: 3})
	for _, want := range []string{"5", "2", "age", "3", "quota"} {
		if !strings.Contains(got, want) {
			t.Errorf("trashPruneMessage(...) = %q, missing %q", got, want)
		}
	}
}

// TestGoToTrashShowsOriginalPathAndDeletionTimeLabel pins
// Root.describeTrashRows' own point (see its doc comment and Panel's
// own onDescribeRows): browsing the trash shows each item's real
// original path as its row name — not the raw, hash-prefixed on-disk
// name nobody could tell apart at a glance — and labels the Modified
// column "Deletion time" instead of its usual "Modify time (mtime)",
// per the user's own explicit report.
func TestGoToTrashShowsOriginalPathAndDeletionTimeLabel(t *testing.T) {
	r, _, file := newTestRootWithFile(t)
	r.moveSelectionToTrash()
	r.openTrash()

	if !r.panel.inTrashView {
		t.Fatal("panel.inTrashView = false after Go to Trash, want true")
	}
	if got := strings.TrimSpace(r.panel.columnHeader.GetCell(0, colModified).Text); got != "Deletion time" {
		t.Errorf("Modified column header = %q, want %q", got, "Deletion time")
	}

	r.panel.focusRow(1) // off ".." onto the one trashed item
	row, _, ok := r.panel.CurrentRowPath()
	if !ok {
		t.Fatal("no current row after Go to Trash")
	}
	ref, ok := r.panel.rowRef(row)
	if !ok {
		t.Fatal("no rowRef for the current row")
	}
	if ref.name != file {
		t.Errorf("row name = %q, want the original path %q, not the raw on-disk name", ref.name, file)
	}
}

// TestGoToTrashSortByModifiedUsesDeletionTime pins that sorting the
// trash's own Modified/"Deletion time" column, and the time actually
// rendered in it, both reflect deleted_at — not the trashed file's own
// real, unrelated last-edit time, which load() would otherwise use.
func TestGoToTrashSortByModifiedUsesDeletionTime(t *testing.T) {
	srcDir := t.TempDir()
	trashRoot := t.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", trashRoot)
	t.Setenv("XDG_DATA_HOME", trashRoot)

	older := filepath.Join(srcDir, "older.txt")
	newer := filepath.Join(srcDir, "newer.txt")
	if err := os.WriteFile(older, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(newer, nil, 0o644); err != nil {
		t.Fatal(err)
	}

	r, err := NewRoot(tview.NewApplication(), srcDir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.panel.focusRow(1)
	r.moveSelectionToTrash() // older.txt
	r.panel.focusRow(1)
	r.moveSelectionToTrash() // newer.txt (now the only entry left in srcDir)

	dir, err := r.trashDir()
	if err != nil {
		t.Fatalf("trashDir: %v", err)
	}
	items, err := fsops.ListTrash(dir)
	if err != nil || len(items) != 2 {
		t.Fatalf("setup ListTrash = %+v, %v, want 2 items", items, err)
	}
	var olderID, newerID string
	for _, item := range items {
		switch item.OriginalPath {
		case older:
			olderID = item.ID
		case newer:
			newerID = item.ID
		}
	}
	if olderID == "" || newerID == "" {
		t.Fatalf("setup: could not find both items by original path in %+v", items)
	}
	// Both items were actually trashed moments apart just now — force a
	// real, unambiguous gap so sort order can't come down to timing luck.
	backdateTrashItem(t, dir, olderID, 48*time.Hour)
	backdateTrashItem(t, dir, newerID, 24*time.Hour)

	r.openTrash()
	r.panel.setSortKey(sortByModified) // ascending: oldest deletion first

	ref1, ok1 := r.panel.rowRef(1)
	ref2, ok2 := r.panel.rowRef(2)
	if !ok1 || !ok2 {
		t.Fatalf("expected two real rows after row 0 (\"..\"), got ok=%v/%v", ok1, ok2)
	}
	if ref1.name != older || ref2.name != newer {
		t.Errorf("sortByModified order = [%q, %q], want [%q, %q] (oldest deletion first)", ref1.name, ref2.name, older, newer)
	}

	cellText := strings.TrimSpace(r.panel.table.GetCell(1, colModified).Text)
	wantYear := strconv.Itoa(time.Now().Add(-48 * time.Hour).Year())
	if !strings.Contains(cellText, wantYear) {
		t.Errorf("rendered Modified cell = %q, want it to reflect the backdated deletion time (year %s), not the file's own real mtime (today)", cellText, wantYear)
	}
}

// TestOrdinaryDirectoryUnaffectedByTrashRowDescriptions is a regression
// guard for describeTrashRows: browsing a perfectly ordinary directory
// (never the trash) must still show real names, the file's own real
// mtime, and the ordinary "mtime" column label rather than the trash's
// own "Deletion time".
func TestOrdinaryDirectoryUnaffectedByTrashRowDescriptions(t *testing.T) {
	r, _, file := newTestRootWithFile(t)

	if r.panel.inTrashView {
		t.Error("inTrashView = true for an ordinary directory, want false")
	}
	if got := strings.TrimSpace(r.panel.columnHeader.GetCell(0, colModified).Text); got != "mtime" {
		t.Errorf("Modified column header = %q, want the usual %q", got, "mtime")
	}

	r.panel.focusRow(1)
	ref, ok := r.panel.rowRef(1)
	if !ok {
		t.Fatal("no rowRef for row 1")
	}
	if want := filepath.Base(file); ref.name != want {
		t.Errorf("row name = %q, want the real basename %q, not an original-path override", ref.name, want)
	}
}
