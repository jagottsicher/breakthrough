package ui

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/jagottsicher/breakthrough/internal/fsops"
)

// TestFinishRenameRehomesLabel pins the lifecycle rule the feature spec
// settled on: a plain rename carries a path's color label along to its
// new name — see root.go's own finishRename.
func TestFinishRenameRehomesLabel(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.SetRect(0, 0, 100, 40)

	path := filepath.Join(dir, "apple.txt")
	if err := r.labels.Set(path, 4); err != nil {
		t.Fatalf("Set: %v", err)
	}

	r.panel.focusRow(2) // apple.txt
	r.renameRow(2)
	r.rename.SetText("renamed.txt")
	r.finishRename(tcell.KeyEnter)

	want := filepath.Join(dir, "renamed.txt")
	if got := r.labels.Get(path); got != 0 {
		t.Errorf("Get(old path) = %d, want 0 (rehomed away)", got)
	}
	if got := r.labels.Get(want); got != 4 {
		t.Errorf("Get(new path) = %d, want 4", got)
	}
}

// TestReallyMoveToTrashRehomesLabel pins that a color label follows a
// trashed file into the trash directory's own real on-disk location
// (see internal/fsops.MoveToTrash's own returned target and
// reallyMoveToTrash's use of it), rather than being lost or left
// pointing at a path that no longer exists.
func TestReallyMoveToTrashRehomesLabel(t *testing.T) {
	r, _, file := newTestRootWithFile(t)
	if err := r.labels.Set(file, 7); err != nil {
		t.Fatalf("Set: %v", err)
	}

	r.reallyMoveToTrash([]string{file})

	if got := r.labels.Get(file); got != 0 {
		t.Errorf("Get(original path) = %d, want 0 (rehomed away)", got)
	}

	trashDir, err := r.trashDir()
	if err != nil {
		t.Fatalf("trashDir: %v", err)
	}
	items, err := fsops.ListTrash(trashDir)
	if err != nil || len(items) != 1 {
		t.Fatalf("ListTrash = %+v, %v, want exactly one item", items, err)
	}
	trashedPath := items[0].Path(trashDir)
	if got := r.labels.Get(trashedPath); got != 7 {
		t.Errorf("Get(trashed path %q) = %d, want 7", trashedPath, got)
	}
}

// TestApplyPasteOneResultRehomesLabelOnLocalCutMove pins that an
// ordinary local Cut+Paste move — and, by the same code path (see
// applyPasteOneResult's own doc comment on why), a Restore-from-Trash —
// carries a color label along to the destination path.
func TestApplyPasteOneResultRehomesLabelOnLocalCutMove(t *testing.T) {
	r, dir, file := newTestRootWithFile(t)
	if err := r.labels.Set(file, 2); err != nil {
		t.Fatalf("Set: %v", err)
	}

	dst := filepath.Join(dir, "moved.txt")
	job := newPasteTestJob(r, true, dir, 1)
	r.applyPasteOneResult(job, file, dst, nil)

	if got := r.labels.Get(file); got != 0 {
		t.Errorf("Get(src) = %d, want 0 (rehomed away)", got)
	}
	if got := r.labels.Get(dst); got != 2 {
		t.Errorf("Get(dst) = %d, want 2", got)
	}
}

// TestApplyPasteOneResultDoesNotRehomeLabelForRemoteMove pins the one
// exclusion applyPasteOneResult's own Rehome call makes: when either
// end of the job is a remote connection, src/dst aren't real local
// filesystem paths filelabels' own store was ever meant to track (the
// same exclusion Panel.labelablePath already applies while browsing),
// so nothing here should be written for them at all.
func TestApplyPasteOneResultDoesNotRehomeLabelForRemoteMove(t *testing.T) {
	r, dir, file := newTestRootWithFile(t)
	// A label happens to already exist under the destination's own
	// path, from some earlier, unrelated local use — a remote-involving
	// move must leave it completely untouched, not overwrite or clear
	// it.
	dst := filepath.Join(dir, "moved.txt")
	if err := r.labels.Set(dst, 9); err != nil {
		t.Fatalf("Set: %v", err)
	}

	job := newPasteTestJob(r, true, dir, 1)
	job.destClient = newTestFakeRemote(dir)
	r.applyPasteOneResult(job, file, dst, nil)

	if got := r.labels.Get(dst); got != 9 {
		t.Errorf("Get(dst) = %d, want 9 (untouched by a remote-involving move)", got)
	}
}

// TestOpenRemoveConfirmConfirmedDeletesLabelPermanently pins that a
// permanently deleted path's color label is gone too (Delete, not
// Rehome to anywhere) — see openRemoveConfirm.
func TestOpenRemoveConfirmConfirmedDeletesLabelPermanently(t *testing.T) {
	r, _, file := newTestRootWithFile(t)
	if err := r.labels.Set(file, 6); err != nil {
		t.Fatalf("Set: %v", err)
	}

	r.openRemoveConfirm()
	// openRemoveConfirm shows a confirmation dialog rather than acting
	// immediately (see newPurgeConfirm) — this test's own concern is
	// only the label bookkeeping inside the confirmed callback, reached
	// the same way TestOpenRemoveConfirmConfirmedDeletesPermanently
	// already reaches it for the filesystem side of this same action.
	r.confirmDialog.SetCurrentItem(0) // "Yes, delete permanently"
	r.resolvePurgeConfirmByCurrentFocus(t)

	if _, err := os.Stat(file); !os.IsNotExist(err) {
		t.Fatalf("setup sanity: file still exists after confirmed Remove: %v", err)
	}
	if got := r.labels.Get(file); got != 0 {
		t.Errorf("Get(file) = %d, want 0 (deleted, not rehomed)", got)
	}
}
