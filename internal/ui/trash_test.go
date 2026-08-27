package ui

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/rivo/tview"

	"github.com/jagottsicher/breakthrough/internal/fsops"
)

// newTestRootWithFile creates a directory containing exactly one file
// ("a.txt") and a Root rooted there, with the table cursor already
// focused on that file (row 0 is always ".." — see focusRow's own
// callers elsewhere in this package). $XDG_RUNTIME_DIR is pointed at a
// fresh temp dir so the session-scoped trash (the default —
// r.settings.TrashPersistent starts false) never touches the real one.
func newTestRootWithFile(t *testing.T) (r *Root, dir, file string) {
	t.Helper()
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())

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

func TestOpenRemoveConfirmCancelPreselectedDoesNotDelete(t *testing.T) {
	r, _, file := newTestRootWithFile(t)

	r.openRemoveConfirm()
	if r.activePage != purgeConfirmPage {
		t.Fatalf("activePage = %q, want %q", r.activePage, purgeConfirmPage)
	}
	if got := r.purgeConfirm.GetCurrentItem(); got != 1 {
		t.Fatalf("preselected item = %d, want 1 (Cancel)", got)
	}

	// Enter without ever moving focus - must cancel, never delete.
	r.resolvePurgeConfirmByCurrentFocus(t)

	if _, err := os.Lstat(file); err != nil {
		t.Fatalf("a.txt was removed despite Cancel being preselected: %v", err)
	}
	if r.activePage == purgeConfirmPage {
		t.Fatal("purge confirm overlay is still open after resolving it")
	}
}

// resolvePurgeConfirmByCurrentFocus resolves the currently open
// purgeConfirm exactly the way pressing Enter on the table's current
// selection would: it does not force a particular outcome, unlike
// calling r.confirmPurge()/r.cancelPurge() directly would.
func (r *Root) resolvePurgeConfirmByCurrentFocus(t *testing.T) {
	t.Helper()
	switch r.purgeConfirm.GetCurrentItem() {
	case 1:
		r.cancelPurge()
	case 2:
		r.confirmPurge()
	default:
		t.Fatalf("unexpected purgeConfirm focus %d", r.purgeConfirm.GetCurrentItem())
	}
}

func TestOpenRemoveConfirmConfirmedDeletesPermanently(t *testing.T) {
	r, _, file := newTestRootWithFile(t)

	r.openRemoveConfirm()
	r.purgeConfirm.SetCurrentItem(2) // deliberately move to "Yes, delete permanently"
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
	if r.activePage != purgeConfirmPage {
		t.Fatalf("activePage = %q, want %q", r.activePage, purgeConfirmPage)
	}
	r.purgeConfirm.SetCurrentItem(2) // "Yes, delete permanently"
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
