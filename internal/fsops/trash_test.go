package fsops

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jagottsicher/breakthrough/internal/config"
)

func mustWriteFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile(%s): %v", path, err)
	}
}

// backdateTrashItem rewrites id's own deleted_at record in trashDir's
// info/ sidecar directly — the same low-level path writeTrashInfo
// itself uses — so a test can simulate an item trashed long ago without
// needing to fake the clock MoveToTrash itself always reads from.
func backdateTrashItem(t *testing.T, trashDir, id string, age time.Duration) {
	t.Helper()
	deletedAt := time.Now().Add(-age).UTC().Format(time.RFC3339Nano)
	if err := config.SetKey(trashInfoPath(trashDir, id), "deleted_at", deletedAt); err != nil {
		t.Fatal(err)
	}
}

func TestMoveToTrashAndListRoundTrip(t *testing.T) {
	srcDir := t.TempDir()
	trashDir := filepath.Join(t.TempDir(), "trash")

	fileA := filepath.Join(srcDir, "a.txt")
	fileB := filepath.Join(srcDir, "b.txt")
	mustWriteFile(t, fileA, "hello")
	mustWriteFile(t, fileB, "world")

	if err := MoveToTrash(fileA, trashDir); err != nil {
		t.Fatalf("MoveToTrash(a): %v", err)
	}
	if err := MoveToTrash(fileB, trashDir); err != nil {
		t.Fatalf("MoveToTrash(b): %v", err)
	}

	if _, err := os.Lstat(fileA); !os.IsNotExist(err) {
		t.Fatalf("a.txt still exists at its original path (err=%v)", err)
	}

	items, err := ListTrash(trashDir)
	if err != nil {
		t.Fatalf("ListTrash: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("ListTrash returned %d items, want 2", len(items))
	}
	// oldest first
	if items[0].OriginalPath != fileA || items[1].OriginalPath != fileB {
		t.Fatalf("ListTrash order/paths = %+v, want [%s, %s] in that order", items, fileA, fileB)
	}
	if !items[0].DeletedAt.Before(items[1].DeletedAt) && items[0].DeletedAt != items[1].DeletedAt {
		t.Fatalf("items not sorted oldest-first: %+v", items)
	}
}

func TestMoveToTrashRecursiveDirectory(t *testing.T) {
	srcDir := t.TempDir()
	trashDir := filepath.Join(t.TempDir(), "trash")

	dir := filepath.Join(srcDir, "project")
	if err := os.MkdirAll(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	mustWriteFile(t, filepath.Join(dir, "top.txt"), "x")
	mustWriteFile(t, filepath.Join(dir, "sub", "nested.txt"), "y")

	if err := MoveToTrash(dir, trashDir); err != nil {
		t.Fatalf("MoveToTrash(dir): %v", err)
	}
	if _, err := os.Lstat(dir); !os.IsNotExist(err) {
		t.Fatalf("directory still exists at its original path")
	}

	items, err := ListTrash(trashDir)
	if err != nil || len(items) != 1 {
		t.Fatalf("ListTrash = %+v, %v, want exactly 1 item", items, err)
	}
	if _, err := os.Lstat(filepath.Join(items[0].Path(trashDir), "sub", "nested.txt")); err != nil {
		t.Fatalf("nested file did not survive the trash move: %v", err)
	}
}

func TestListTrashSelfHealsAfterExternalDeletion(t *testing.T) {
	srcDir := t.TempDir()
	trashDir := filepath.Join(t.TempDir(), "trash")
	fileA := filepath.Join(srcDir, "a.txt")
	mustWriteFile(t, fileA, "hello")

	if err := MoveToTrash(fileA, trashDir); err != nil {
		t.Fatalf("MoveToTrash: %v", err)
	}

	// Simulate deleting the whole trash directory outside breakthrough.
	if err := os.RemoveAll(trashDir); err != nil {
		t.Fatal(err)
	}

	items, err := ListTrash(trashDir)
	if err != nil {
		t.Fatalf("ListTrash after external rm -rf: %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("ListTrash after external rm -rf = %+v, want empty", items)
	}
	for _, dir := range []string{trashDir, trashFilesDir(trashDir), trashInfoDir(trashDir)} {
		if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
			t.Fatalf("expected %s to be recreated as a directory, stat err=%v", dir, err)
		}
	}
}

func TestListTrashDropsOrphanedInfoRecord(t *testing.T) {
	trashDir := filepath.Join(t.TempDir(), "trash")
	if err := ensureTrashSkeleton(trashDir); err != nil {
		t.Fatal(err)
	}
	// An info/ record with no matching files/ entry - e.g. someone
	// deleted just the payload by hand.
	orphanInfo := trashInfoPath(trashDir, "deadbeef_ghost.txt")
	mustWriteFile(t, orphanInfo, "path = /tmp/ghost.txt\ndeleted_at = 2020-01-01T00:00:00Z\n")

	items, err := ListTrash(trashDir)
	if err != nil {
		t.Fatalf("ListTrash: %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("ListTrash = %+v, want the orphan dropped", items)
	}
	if _, err := os.Stat(orphanInfo); !os.IsNotExist(err) {
		t.Fatal("orphaned .trashinfo file was not cleaned up")
	}
}

func TestRestoreFromTrashRoundTrip(t *testing.T) {
	srcDir := t.TempDir()
	trashDir := filepath.Join(t.TempDir(), "trash")
	fileA := filepath.Join(srcDir, "a.txt")
	mustWriteFile(t, fileA, "hello")

	if err := MoveToTrash(fileA, trashDir); err != nil {
		t.Fatalf("MoveToTrash: %v", err)
	}
	items, err := ListTrash(trashDir)
	if err != nil || len(items) != 1 {
		t.Fatalf("ListTrash = %+v, %v", items, err)
	}

	if err := RestoreFromTrash(items[0], trashDir); err != nil {
		t.Fatalf("RestoreFromTrash: %v", err)
	}
	data, err := os.ReadFile(fileA)
	if err != nil || string(data) != "hello" {
		t.Fatalf("restored file content = %q, %v, want \"hello\"", data, err)
	}

	remaining, err := ListTrash(trashDir)
	if err != nil || len(remaining) != 0 {
		t.Fatalf("ListTrash after restore = %+v, %v, want empty", remaining, err)
	}
}

func TestRestoreFromTrashRefusesToOverwrite(t *testing.T) {
	srcDir := t.TempDir()
	trashDir := filepath.Join(t.TempDir(), "trash")
	fileA := filepath.Join(srcDir, "a.txt")
	mustWriteFile(t, fileA, "original")

	if err := MoveToTrash(fileA, trashDir); err != nil {
		t.Fatalf("MoveToTrash: %v", err)
	}
	items, err := ListTrash(trashDir)
	if err != nil || len(items) != 1 {
		t.Fatalf("ListTrash = %+v, %v", items, err)
	}

	// Something new now occupies the original path.
	mustWriteFile(t, fileA, "a different file entirely")

	if err := RestoreFromTrash(items[0], trashDir); err == nil {
		t.Fatal("RestoreFromTrash succeeded despite an existing file at the destination")
	}
	data, _ := os.ReadFile(fileA)
	if string(data) != "a different file entirely" {
		t.Fatalf("the existing file at the destination was overwritten: %q", data)
	}
	// Still restorable later - nothing should have been dropped from trash.
	remaining, err := ListTrash(trashDir)
	if err != nil || len(remaining) != 1 {
		t.Fatalf("ListTrash after refused restore = %+v, %v, want the item still there", remaining, err)
	}
}

func TestEmptyTrashRemovesEverything(t *testing.T) {
	srcDir := t.TempDir()
	trashDir := filepath.Join(t.TempDir(), "trash")
	for _, name := range []string{"a.txt", "b.txt", "c.txt"} {
		mustWriteFile(t, filepath.Join(srcDir, name), name)
	}
	for _, name := range []string{"a.txt", "b.txt", "c.txt"} {
		if err := MoveToTrash(filepath.Join(srcDir, name), trashDir); err != nil {
			t.Fatalf("MoveToTrash(%s): %v", name, err)
		}
	}

	removed, err := EmptyTrash(trashDir)
	if err != nil {
		t.Fatalf("EmptyTrash: %v", err)
	}
	if removed != 3 {
		t.Fatalf("EmptyTrash removed %d items, want 3", removed)
	}

	items, err := ListTrash(trashDir)
	if err != nil || len(items) != 0 {
		t.Fatalf("ListTrash after EmptyTrash = %+v, %v, want empty", items, err)
	}
	entries, err := os.ReadDir(trashFilesDir(trashDir))
	if err != nil || len(entries) != 0 {
		t.Fatalf("files/ after EmptyTrash = %v entries, %v, want none", entries, err)
	}
}

func TestPurgeCompletelyFile(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "a.txt")
	mustWriteFile(t, f, "hello")

	if err := PurgeCompletely(f); err != nil {
		t.Fatalf("PurgeCompletely: %v", err)
	}
	if _, err := os.Lstat(f); !os.IsNotExist(err) {
		t.Fatal("file still exists after PurgeCompletely")
	}
}

func TestPurgeCompletelyNonEmptyDirectory(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "project")
	if err := os.MkdirAll(filepath.Join(target, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	mustWriteFile(t, filepath.Join(target, "sub", "nested.txt"), "x")

	if err := PurgeCompletely(target); err != nil {
		t.Fatalf("PurgeCompletely(non-empty dir): %v", err)
	}
	if _, err := os.Lstat(target); !os.IsNotExist(err) {
		t.Fatal("non-empty directory still exists after PurgeCompletely")
	}
}

func TestCountEntries(t *testing.T) {
	dir := t.TempDir()

	if count, err := CountEntries(filepath.Join(dir, "solo.txt")); err == nil || count != 0 {
		// Nonexistent path: err must be non-nil.
		t.Fatalf("CountEntries(missing) = %d, %v, want an error", count, err)
	}

	f := filepath.Join(dir, "solo.txt")
	mustWriteFile(t, f, "x")
	if count, err := CountEntries(f); err != nil || count != 0 {
		t.Fatalf("CountEntries(plain file) = %d, %v, want 0, nil", count, err)
	}

	target := filepath.Join(dir, "project")
	if err := os.MkdirAll(filepath.Join(target, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	mustWriteFile(t, filepath.Join(target, "top.txt"), "x")
	mustWriteFile(t, filepath.Join(target, "sub", "nested.txt"), "y")

	count, err := CountEntries(target)
	if err != nil {
		t.Fatalf("CountEntries(project): %v", err)
	}
	if count != 3 { // "top.txt", "sub", "sub/nested.txt"
		t.Fatalf("CountEntries(project) = %d, want 3", count)
	}
}

func TestPruneTrashRemovesItemsOlderThanMaxAge(t *testing.T) {
	srcDir := t.TempDir()
	trashDir := filepath.Join(t.TempDir(), "trash")
	oldFile := filepath.Join(srcDir, "old.txt")
	newFile := filepath.Join(srcDir, "new.txt")
	mustWriteFile(t, oldFile, "old")
	mustWriteFile(t, newFile, "new")

	if err := MoveToTrash(oldFile, trashDir); err != nil {
		t.Fatalf("MoveToTrash(old): %v", err)
	}
	if err := MoveToTrash(newFile, trashDir); err != nil {
		t.Fatalf("MoveToTrash(new): %v", err)
	}
	items, err := ListTrash(trashDir)
	if err != nil || len(items) != 2 {
		t.Fatalf("setup ListTrash = %+v, %v, want 2 items", items, err)
	}
	backdateTrashItem(t, trashDir, items[0].ID, 40*24*time.Hour)

	result, err := PruneTrash(trashDir, PruneTrashOptions{MaxAge: 30 * 24 * time.Hour})
	if err != nil {
		t.Fatalf("PruneTrash: %v", err)
	}
	if result.RemovedByAge != 1 || result.RemovedByQuota != 0 {
		t.Fatalf("PruneTrash result = %+v, want {RemovedByAge:1, RemovedByQuota:0}", result)
	}

	remaining, err := ListTrash(trashDir)
	if err != nil || len(remaining) != 1 || remaining[0].OriginalPath != newFile {
		t.Fatalf("ListTrash after PruneTrash = %+v, %v, want only %s left", remaining, err, newFile)
	}
}

func TestPruneTrashZeroMaxAgeDisablesAgePruning(t *testing.T) {
	srcDir := t.TempDir()
	trashDir := filepath.Join(t.TempDir(), "trash")
	f := filepath.Join(srcDir, "ancient.txt")
	mustWriteFile(t, f, "x")
	if err := MoveToTrash(f, trashDir); err != nil {
		t.Fatal(err)
	}
	items, err := ListTrash(trashDir)
	if err != nil || len(items) != 1 {
		t.Fatalf("setup ListTrash = %+v, %v", items, err)
	}
	backdateTrashItem(t, trashDir, items[0].ID, 1000*24*time.Hour)

	result, err := PruneTrash(trashDir, PruneTrashOptions{MaxAge: 0})
	if err != nil {
		t.Fatalf("PruneTrash: %v", err)
	}
	if result.Removed() != 0 {
		t.Fatalf("PruneTrash with MaxAge=0 removed %d item(s), want 0 (age pruning disabled)", result.Removed())
	}
}

func TestPruneTrashQuotaRemovesOldestFirstUntilUnderQuota(t *testing.T) {
	srcDir := t.TempDir()
	trashDir := filepath.Join(t.TempDir(), "trash")

	// Three 10-byte files, trashed in order — ListTrash's own
	// oldest-first ordering makes which two get removed deterministic.
	for _, name := range []string{"a.txt", "b.txt", "c.txt"} {
		f := filepath.Join(srcDir, name)
		mustWriteFile(t, f, "0123456789")
		if err := MoveToTrash(f, trashDir); err != nil {
			t.Fatalf("MoveToTrash(%s): %v", name, err)
		}
	}

	// A simulated 100-byte filesystem: a 10% quota is 10 bytes, one
	// file's worth — 30 bytes of trash needs exactly two removed (a.txt,
	// then b.txt) to get back under it.
	original := fetchDiskUsageForQuota
	fetchDiskUsageForQuota = func(string) (DiskUsage, bool) {
		return DiskUsage{UsedBytes: 90, AvailBytes: 10}, true
	}
	t.Cleanup(func() { fetchDiskUsageForQuota = original })

	result, err := PruneTrash(trashDir, PruneTrashOptions{QuotaPercent: 10})
	if err != nil {
		t.Fatalf("PruneTrash: %v", err)
	}
	if result.RemovedByQuota != 2 || result.RemovedByAge != 0 {
		t.Fatalf("PruneTrash result = %+v, want {RemovedByAge:0, RemovedByQuota:2}", result)
	}

	remaining, err := ListTrash(trashDir)
	if err != nil || len(remaining) != 1 || remaining[0].OriginalPath != filepath.Join(srcDir, "c.txt") {
		t.Fatalf("ListTrash after quota prune = %+v, %v, want only c.txt (newest) left", remaining, err)
	}
}

func TestPruneTrashZeroQuotaPercentDisablesQuotaPruning(t *testing.T) {
	srcDir := t.TempDir()
	trashDir := filepath.Join(t.TempDir(), "trash")
	f := filepath.Join(srcDir, "big.txt")
	mustWriteFile(t, f, "0123456789")
	if err := MoveToTrash(f, trashDir); err != nil {
		t.Fatal(err)
	}

	original := fetchDiskUsageForQuota
	fetchDiskUsageForQuota = func(string) (DiskUsage, bool) {
		return DiskUsage{UsedBytes: 99, AvailBytes: 1}, true // 1% total — the trash alone is already "over" any nonzero quota
	}
	t.Cleanup(func() { fetchDiskUsageForQuota = original })

	result, err := PruneTrash(trashDir, PruneTrashOptions{QuotaPercent: 0})
	if err != nil {
		t.Fatalf("PruneTrash: %v", err)
	}
	if result.Removed() != 0 {
		t.Fatalf("PruneTrash with QuotaPercent=0 removed %d item(s), want 0 (quota pruning disabled)", result.Removed())
	}
}

func TestPruneTrashQuotaSkippedWhenDiskUsageUnavailable(t *testing.T) {
	srcDir := t.TempDir()
	trashDir := filepath.Join(t.TempDir(), "trash")
	f := filepath.Join(srcDir, "a.txt")
	mustWriteFile(t, f, "x")
	if err := MoveToTrash(f, trashDir); err != nil {
		t.Fatal(err)
	}

	original := fetchDiskUsageForQuota
	fetchDiskUsageForQuota = func(string) (DiskUsage, bool) { return DiskUsage{}, false } // e.g. df not on $PATH
	t.Cleanup(func() { fetchDiskUsageForQuota = original })

	result, err := PruneTrash(trashDir, PruneTrashOptions{QuotaPercent: 10})
	if err != nil {
		t.Fatalf("PruneTrash: %v, want nil — an unavailable quota check degrades rather than fails", err)
	}
	if result.Removed() != 0 {
		t.Fatalf("PruneTrash removed %d item(s) despite no way to check quota, want 0", result.Removed())
	}
}
