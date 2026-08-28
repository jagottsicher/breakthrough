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

// TestMoveSelectionToTrashInsideTrashRedirectsToRemove pins
// moveSelectionToTrash's own redirect (see its doc comment): a second
// "Move to Trash" on something already in the trash has nowhere left to
// go, so it opens the same Remove confirmation Ctrl+R/the context
// menu's own "Remove" would, Cancel preselected the same as any other
// Remove — not a silent no-op, and not an unconfirmed delete either.
func TestMoveSelectionToTrashInsideTrashRedirectsToRemove(t *testing.T) {
	r, _, file := newTestRootWithFile(t)
	r.moveSelectionToTrash()
	r.openTrash()
	r.panel.focusRow(1) // off ".." onto the now-trashed a.txt

	r.moveSelectionToTrash()

	if r.activePage != purgeConfirmPage {
		t.Fatalf("activePage = %q, want %q", r.activePage, purgeConfirmPage)
	}
	if got := r.purgeConfirm.GetCurrentItem(); got != 1 {
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

// TestTrashbinShortcutOpensTrash pins Ctrl+B's own action
// (Root.TrashbinShortcut): the same navigation openTrash itself does.
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
// TestTrashShortcutsNoOpWhileAnOverlayIsOpen above for Ctrl+B.
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
// mtime, and the usual "Modify time (mtime)" column label.
func TestOrdinaryDirectoryUnaffectedByTrashRowDescriptions(t *testing.T) {
	r, _, file := newTestRootWithFile(t)

	if r.panel.inTrashView {
		t.Error("inTrashView = true for an ordinary directory, want false")
	}
	if got := strings.TrimSpace(r.panel.columnHeader.GetCell(0, colModified).Text); got != "Modify time (mtime)" {
		t.Errorf("Modified column header = %q, want the usual %q", got, "Modify time (mtime)")
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
