package ui

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/jagottsicher/breakthrough/internal/logview"
)

func TestFinishLogAuditDiskCacheMoveAppliesPathOnSuccess(t *testing.T) {
	r, _ := newOptionsRoot(t)
	r.log.DiskCacheMoveCancel = func() {}
	r.log.DiskCacheMoveTarget = "/somewhere"
	r.log.DiskCache = nil // pretend a prior Cache instance existed at the old path
	r.log.DiskCacheUnavailable = true

	newPath := t.TempDir()
	r.finishLogAuditDiskCacheMove(newPath, nil)

	if r.settings.LogAuditDiskCachePath != newPath {
		t.Errorf("LogAuditDiskCachePath = %q, want %q", r.settings.LogAuditDiskCachePath, newPath)
	}
	if r.log.DiskCacheMoveCancel != nil {
		t.Error("DiskCacheMoveCancel still set after a finished move")
	}
	if r.log.DiskCacheMoveTarget != "" {
		t.Error("DiskCacheMoveTarget still set after a finished move")
	}
	if r.log.DiskCacheUnavailable {
		t.Error("DiskCacheUnavailable still true after a successful move — must be reset so the next logDiskCache call retries at the new path")
	}
	if r.activePage == errorPage {
		t.Error("a successful move must not show an error")
	}
}

func TestFinishLogAuditDiskCacheMoveCancelledDoesNotShowError(t *testing.T) {
	r, _ := newOptionsRoot(t)
	r.log.DiskCacheMoveCancel = func() {}
	r.log.DiskCacheMoveTarget = "/somewhere"

	r.finishLogAuditDiskCacheMove("/somewhere", context.Canceled)

	if r.activePage == errorPage {
		t.Error("a cancelled move must revert silently, not show an error dialog")
	}
	if r.settings.LogAuditDiskCachePath != "" {
		t.Errorf("LogAuditDiskCachePath = %q, want unchanged (empty) after a cancelled move", r.settings.LogAuditDiskCachePath)
	}
	if r.log.DiskCacheMoveCancel != nil {
		t.Error("DiskCacheMoveCancel still set after a cancelled move")
	}
}

func TestFinishLogAuditDiskCacheMoveRealErrorShowsError(t *testing.T) {
	r, _ := newOptionsRoot(t)
	r.log.DiskCacheMoveCancel = func() {}

	r.finishLogAuditDiskCacheMove("/somewhere", errors.New("disk full"))

	if r.activePage != errorPage {
		t.Error("a real move failure must show an error")
	}
	if r.settings.LogAuditDiskCachePath != "" {
		t.Errorf("LogAuditDiskCachePath = %q, want unchanged after a failed move", r.settings.LogAuditDiskCachePath)
	}
}

func TestActivateLogAuditDiskCacheLocationRowCancelsAnInFlightMove(t *testing.T) {
	r, _ := newOptionsRoot(t)
	called := false
	r.log.DiskCacheMoveCancel = func() { called = true }

	r.activateLogAuditDiskCacheLocationRow()

	if !called {
		t.Error("re-activating the row while a move is in flight did not call its cancel func")
	}
	// Only the move's own goroutine (via finishLogAuditDiskCacheMove)
	// clears DiskCacheMoveCancel once it actually observes
	// cancellation — activate itself must not clear it early, or the
	// row would stop showing "moving" before the move has actually
	// stopped.
	if r.log.DiskCacheMoveCancel == nil {
		t.Error("DiskCacheMoveCancel was cleared by activate itself, not by the move's own completion")
	}
}

func TestActivateLogAuditDiskCacheLocationRowRefusesWhileLoadingMore(t *testing.T) {
	r, _ := newOptionsRoot(t)
	r.log.LoadingMore = true

	r.activateLogAuditDiskCacheLocationRow()

	if r.activePage != errorPage {
		t.Error("activating the row while a progressive load is running must show an error, not open the path prompt")
	}
	if r.log.DiskCacheMoveCancel != nil {
		t.Error("a move must not have started while LoadingMore was true")
	}
}

func TestConfirmLogAuditDiskCacheMoveRejectsAnUnsafeTarget(t *testing.T) {
	r, _ := newOptionsRoot(t)
	unsafe := t.TempDir()
	if err := os.WriteFile(filepath.Join(unsafe, "vacation-photo.jpg"), []byte("not actually a jpg"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	r.confirmLogAuditDiskCacheMove(unsafe)

	if r.activePage != errorPage {
		t.Error("confirming a move into a directory with unrelated real content must show an error")
	}
	if r.settings.LogAuditDiskCachePath != "" {
		t.Error("LogAuditDiskCachePath must stay unchanged after a rejected target")
	}
}

func TestConfirmLogAuditDiskCacheMoveToTheCurrentPathIsANoOp(t *testing.T) {
	r, _ := newOptionsRoot(t)
	current := r.logAuditDiskCacheRoot()

	r.confirmLogAuditDiskCacheMove(current)

	if r.activePage == errorPage || r.activePage == confirmPage {
		t.Error("confirming a move to the already-current path must do nothing, silently")
	}
}

func TestConfirmLogAuditDiskCacheMoveWithNoExistingCacheAppliesPathDirectly(t *testing.T) {
	r, _ := newOptionsRoot(t)
	// Simulate "disk cache unavailable" without touching the real
	// machine's own XDG cache directory at all.
	r.log.DiskCacheUnavailable = true

	newPath := t.TempDir()
	r.confirmLogAuditDiskCacheMove(newPath)

	if r.settings.LogAuditDiskCachePath != newPath {
		t.Errorf("LogAuditDiskCachePath = %q, want %q — adopting a path with nothing cached yet needs no confirmation", r.settings.LogAuditDiskCachePath, newPath)
	}
	if r.activePage == confirmPage {
		t.Error("no existing cache to move — must not have opened a confirm dialog")
	}
}

// TestConfirmLogAuditDiskCacheMoveWithExistingCacheAsksFirst pins the
// one real-data case: once there's something to actually move, this
// must ask before doing it, not apply the new path immediately the
// way the "nothing cached yet" case does.
func TestConfirmLogAuditDiskCacheMoveWithExistingCacheAsksFirst(t *testing.T) {
	r, _ := newOptionsRoot(t)
	oldRoot := t.TempDir()
	r.settings.LogAuditDiskCachePath = oldRoot
	c := r.logDiskCache()
	if c == nil {
		t.Fatal("setup: logDiskCache() = nil, want a real cache at oldRoot")
	}
	if err := c.Put("remote:a@hosta:22", "key1", []logview.Entry{{Message: "a"}}, 10<<20); err != nil {
		t.Fatalf("Put: %v", err)
	}

	newPath := t.TempDir()
	r.confirmLogAuditDiskCacheMove(newPath)

	if r.activePage != confirmPage {
		t.Error("moving existing cached data must ask for confirmation first")
	}
	if r.settings.LogAuditDiskCachePath != oldRoot {
		t.Error("the path must not change until the move is actually confirmed and completed")
	}
}
