package fsops

import (
	"os"
	"path/filepath"
	"testing"
)

func mustWriteFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile(%s): %v", path, err)
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
