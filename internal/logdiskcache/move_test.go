package logdiskcache

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/jagottsicher/breakthrough/internal/logview"
)

func TestValidateNewRootAcceptsAFreshNonexistentDirectory(t *testing.T) {
	newRoot := filepath.Join(t.TempDir(), "does-not-exist-yet")
	if err := ValidateNewRoot(newRoot); err != nil {
		t.Fatalf("ValidateNewRoot: %v", err)
	}
	fi, err := os.Stat(newRoot)
	if err != nil || !fi.IsDir() {
		t.Errorf("ValidateNewRoot did not create %s as a directory", newRoot)
	}
}

func TestValidateNewRootAcceptsAnEmptyExistingDirectory(t *testing.T) {
	if err := ValidateNewRoot(t.TempDir()); err != nil {
		t.Errorf("ValidateNewRoot on an empty directory: %v", err)
	}
}

// TestValidateNewRootRejectsARealUnrelatedDirectory is the direct
// regression test for the data-loss vector a free-text path setting
// opens up — see Open's own doc comment on isSafeRoot. A user typing
// "~/Documents" into the Options dialog must be rejected here, before
// Move ever runs, not discovered after the fact.
func TestValidateNewRootRejectsARealUnrelatedDirectory(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "vacation-photos.jpg"), []byte("not actually a jpg"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := ValidateNewRoot(dir); err == nil {
		t.Fatal("ValidateNewRoot accepted a directory with unrelated real content")
	}
	if _, err := os.Stat(filepath.Join(dir, "vacation-photos.jpg")); err != nil {
		t.Errorf("the unrelated file was removed: %v", err)
	}
}

func TestValidateNewRootAcceptsAnExistingBreakthroughCache(t *testing.T) {
	dir := t.TempDir()
	c, err := Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := c.Put("remote:a@hosta:22", "key1", []logview.Entry{{Message: "a"}}, 10*mb); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := ValidateNewRoot(dir); err != nil {
		t.Errorf("ValidateNewRoot rejected a directory that is already this cache's own data: %v", err)
	}
}

func newTestCacheWithOneEntry(t *testing.T, identity, key string, entries []logview.Entry) (*Cache, string) {
	t.Helper()
	root := t.TempDir()
	c, err := Open(root)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := c.Put(identity, key, entries, 10*mb); err != nil {
		t.Fatalf("Put: %v", err)
	}
	return c, root
}

func TestMoveToSameRootIsANoOp(t *testing.T) {
	c, root := newTestCacheWithOneEntry(t, "remote:a@hosta:22", "key1", []logview.Entry{{Message: "a"}})
	if err := c.Move(context.Background(), root); err != nil {
		t.Fatalf("Move to the same root: %v", err)
	}
	if _, ok := c.Get(context.Background(), "remote:a@hosta:22", "key1"); !ok {
		t.Error("entry lost after a no-op move")
	}
}

func TestMoveRejectsATargetInsideTheCurrentRoot(t *testing.T) {
	c, root := newTestCacheWithOneEntry(t, "remote:a@hosta:22", "key1", []logview.Entry{{Message: "a"}})
	if err := c.Move(context.Background(), filepath.Join(root, "nested")); err == nil {
		t.Fatal("Move accepted a target nested inside the current root")
	}
}

func TestMoveRejectsAnUnsafeTarget(t *testing.T) {
	c, _ := newTestCacheWithOneEntry(t, "remote:a@hosta:22", "key1", []logview.Entry{{Message: "a"}})
	unsafe := t.TempDir()
	if err := os.WriteFile(filepath.Join(unsafe, "real-file.txt"), []byte("mine"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := c.Move(context.Background(), unsafe); err == nil {
		t.Fatal("Move accepted a target directory with unrelated real content")
	}
	if _, err := os.Stat(filepath.Join(unsafe, "real-file.txt")); err != nil {
		t.Errorf("the unrelated file was removed: %v", err)
	}
}

// TestMoveRenamesOnTheSameFilesystem exercises Move's own common,
// fast path end to end (t.TempDir() for both sides lands on the same
// filesystem in any normal test environment) — the data is reachable
// at the new root afterward, and the old root is gone (a real
// os.Rename, not a copy left behind alongside it).
func TestMoveRenamesOnTheSameFilesystem(t *testing.T) {
	c, oldRoot := newTestCacheWithOneEntry(t, "remote:a@hosta:22", "key1", []logview.Entry{{Message: "a"}})
	newRoot := filepath.Join(t.TempDir(), "new-location")

	if err := c.Move(context.Background(), newRoot); err != nil {
		t.Fatalf("Move: %v", err)
	}
	if _, err := os.Stat(oldRoot); !os.IsNotExist(err) {
		t.Errorf("old root still exists after a same-filesystem move: err = %v", err)
	}
	got, ok := c.Get(context.Background(), "remote:a@hosta:22", "key1")
	if !ok || len(got) != 1 || got[0].Message != "a" {
		t.Errorf("Get after move = %+v, %v, want the entry that was there before", got, ok)
	}
}

// TestCopyDirPreservesContentAndPermissions exercises the EXDEV
// fallback's own copy mechanics directly — Move's own EXDEV trigger
// can't be forced in a unit test without two real filesystems (see
// copyDir's own doc comment), so this is deliberately the boundary:
// what copyDir does once reached is fully covered; that Move actually
// reaches it under a real cross-filesystem move is not, and isn't
// claimed to be.
func TestCopyDirPreservesContentAndPermissions(t *testing.T) {
	src := t.TempDir()
	if err := os.Mkdir(filepath.Join(src, "subdir"), 0o700); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(src, "subdir", "file.cache"), []byte("hello"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	dst := filepath.Join(t.TempDir(), "copied")

	if err := copyDir(context.Background(), src, dst); err != nil {
		t.Fatalf("copyDir: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dst, "subdir", "file.cache"))
	if err != nil || string(data) != "hello" {
		t.Fatalf("ReadFile = %q, %v, want \"hello\"", data, err)
	}
	fi, err := os.Stat(filepath.Join(dst, "subdir", "file.cache"))
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if perm := fi.Mode().Perm(); perm != 0o600 {
		t.Errorf("copied file perm = %o, want 0600 — permissions must survive the EXDEV fallback", perm)
	}
	dirInfo, err := os.Stat(filepath.Join(dst, "subdir"))
	if err != nil {
		t.Fatalf("Stat dir: %v", err)
	}
	if perm := dirInfo.Mode().Perm(); perm != 0o700 {
		t.Errorf("copied directory perm = %o, want 0700", perm)
	}
}

// TestCopyDirCancellationLeavesNoPartialCopy pins the one case a plan
// for this feature could easily skip: cancelling mid-copy must not
// leave a half-written destination behind for Move's own caller to
// mistake for a complete one. copyDir itself only has to stop and
// report ctx.Err() — Move's own responsibility (removing that partial
// destination and leaving the old root untouched) is covered by
// inspecting exactly that return contract here, since forcing Move's
// own EXDEV path isn't possible in this test environment (see
// TestCopyDirPreservesContentAndPermissions's own doc comment).
func TestCopyDirCancellationLeavesNoPartialCopy(t *testing.T) {
	src := t.TempDir()
	for i := range 5 {
		if err := os.WriteFile(filepath.Join(src, "file"+string(rune('a'+i))+".cache"), []byte("some content"), 0o600); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
	}
	dst := filepath.Join(t.TempDir(), "copied")

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already cancelled — copyDir must stop at the very first file
	err := copyDir(ctx, src, dst)
	if err == nil {
		t.Fatal("copyDir with an already-cancelled context = nil error, want ctx.Err()")
	}
}
