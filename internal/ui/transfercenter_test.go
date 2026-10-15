package ui

import (
	"context"
	"strings"
	"testing"

	"github.com/rivo/tview"

	"github.com/jagottsicher/breakthrough/internal/notify"
	"github.com/jagottsicher/breakthrough/internal/operations"
)

func TestOpenTransferCenterShowsRunningAndFinishedOperations(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}

	runningID := r.operations.Start(string(notify.CategoryPaste), "/src", "/dst")
	doneID := r.operations.Start(string(notify.CategoryRsync), "/a", "/b")
	r.operations.Finish(doneID, operations.OutcomeFailed, 0, 1, 0, []string{"permission denied"})

	r.openTransferCenter()

	if len(r.transferCenterOps) != 2 {
		t.Fatalf("transferCenterOps has %d entries, want 2", len(r.transferCenterOps))
	}
	// Newest first (see reloadTransferCenter's own doc comment) — doneID
	// started after runningID, so it comes first.
	if r.transferCenterOps[0].ID != doneID {
		t.Errorf("first entry = %+v, want ID %d (newest first)", r.transferCenterOps[0], doneID)
	}
	if r.transferCenterOps[1].ID != runningID {
		t.Errorf("second entry = %+v, want ID %d", r.transferCenterOps[1], runningID)
	}

	text := r.transferCenterTable.GetCell(1, transferCenterColResult).Text
	if !strings.Contains(text, "permission denied") {
		t.Errorf("Result column for the failed entry = %q, want it to include the error", text)
	}
}

func TestReloadTransferCenterShowsPlaceholderWhenEmpty(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.openTransferCenter()

	cell := r.transferCenterTable.GetCell(1, 0)
	if cell == nil || !strings.Contains(cell.Text, "Nothing running") {
		t.Errorf("empty state should show the placeholder, got %+v", cell)
	}
}

func TestTransferCenterLiveUpdatesWhileOpen(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.openTransferCenter()
	if len(r.transferCenterOps) != 0 {
		t.Fatalf("setup: expected no operations yet, got %d", len(r.transferCenterOps))
	}

	r.operations.Start(string(notify.CategoryPaste), "/src", "/dst")

	if len(r.transferCenterOps) != 1 {
		t.Errorf("transferCenterOps should have updated live via Subscribe while the screen is open, got %d entries", len(r.transferCenterOps))
	}
}

func TestTransferCenterDoesNotUpdateWhileClosed(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	// Never opened — activePage isn't transferCenterPage.
	r.operations.Start(string(notify.CategoryPaste), "/src", "/dst")

	if r.transferCenterOps != nil {
		t.Errorf("transferCenterOps should stay unset while the screen was never opened, got %+v", r.transferCenterOps)
	}
}

func TestCancelSelectedTransferCenterOperationCancelsTheLivePasteJob(t *testing.T) {
	destDir := t.TempDir()
	r, err := NewRoot(tview.NewApplication(), destDir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	job := newPasteTestJob(r, false, destDir, 1)
	job.opID = r.operations.Start(string(notify.CategoryPaste), "/src", destDir)
	r.openTransferCenter()
	r.transferCenterTable.Select(1, 0)

	r.cancelSelectedTransferCenterOperation()

	if r.pasteJob != nil {
		t.Error("cancelSelectedTransferCenterOperation should have cancelled the live paste job")
	}
	ops := r.operations.List()
	if len(ops) != 1 || ops[0].Outcome != operations.OutcomeCancelled {
		t.Errorf("operations.List() = %+v, want one OutcomeCancelled entry", ops)
	}
}

func TestCancelSelectedTransferCenterOperationIgnoresAFinishedRow(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	id := r.operations.Start(string(notify.CategoryPaste), "/src", "/dst")
	r.operations.Finish(id, operations.OutcomeSucceeded, 1, 0, 0, nil)
	r.openTransferCenter()
	r.transferCenterTable.Select(1, 0)

	r.cancelSelectedTransferCenterOperation() // must not panic on a nil r.pasteJob

	ops := r.operations.List()
	if ops[0].Outcome != operations.OutcomeSucceeded {
		t.Errorf("a finished row's own outcome should be untouched, got %+v", ops[0])
	}
}

func TestPauseResumeSelectedTransferCenterOperationTogglesPasteState(t *testing.T) {
	destDir := t.TempDir()
	r, err := NewRoot(tview.NewApplication(), destDir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	job := newPasteTestJob(r, false, destDir, 1)
	job.opID = r.operations.Start(string(notify.CategoryPaste), "/src", destDir)
	r.openTransferCenter()
	r.transferCenterTable.Select(1, 0)

	r.pauseResumeSelectedTransferCenterOperation()
	if got := r.operations.List()[0].State; got != operations.StatePaused {
		t.Errorf("State after pause = %v, want StatePaused", got)
	}

	r.pauseResumeSelectedTransferCenterOperation()
	if got := r.operations.List()[0].State; got != operations.StateRunning {
		t.Errorf("State after resume = %v, want StateRunning", got)
	}
}

func TestPauseResumeSelectedTransferCenterOperationIgnoresRsync(t *testing.T) {
	r := newTestRootForRsyncJob(t)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	job := &rsyncJob{ctx: ctx, cancel: cancel, destPath: r.panel.path}
	job.opID = r.operations.Start(string(notify.CategoryRsync), "/a", r.panel.path)
	r.rsyncJob = job
	r.openTransferCenter()
	r.transferCenterTable.Select(1, 0)

	r.pauseResumeSelectedTransferCenterOperation() // must be a no-op, not a panic

	if got := r.operations.List()[0].State; got != operations.StateRunning {
		t.Errorf("State = %v, want unchanged StateRunning (Rsync has no pause)", got)
	}
}
