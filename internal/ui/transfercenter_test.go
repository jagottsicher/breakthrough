package ui

import (
	"context"
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/jagottsicher/breakthrough/internal/notify"
	"github.com/jagottsicher/breakthrough/internal/operations"
)

func TestGCReachesAIConsigliereFromTransferCenter(t *testing.T) {
	r, err := NewRoot(tview.NewApplication(), fixtureDir(t))
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.openTransferCenter()

	if got := r.captureTransferCenterKey(tcell.NewEventKey(tcell.KeyRune, 'g', tcell.ModNone)); got != nil {
		t.Fatalf("capture('g') = %v, want nil (consumed, chord started)", got)
	}
	if r.pendingChord != 'g' {
		t.Fatalf("pendingChord = %q, want 'g'", string(r.pendingChord))
	}

	if !r.HandlePlainKey(tcell.NewEventKey(tcell.KeyRune, 'C', tcell.ModNone)) {
		t.Fatal("the resolving 'C' should have been consumed")
	}
	if !r.consigliere.Visible {
		t.Error("\"gC\" should have opened the AI Consigliere sidebar")
	}
}

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

	if len(r.transfer.Ops) != 2 {
		t.Fatalf("transfer.Ops has %d entries, want 2", len(r.transfer.Ops))
	}
	// Newest first (see reloadTransferCenter's own doc comment) — doneID
	// started after runningID, so it comes first.
	if r.transfer.Ops[0].ID != doneID {
		t.Errorf("first entry = %+v, want ID %d (newest first)", r.transfer.Ops[0], doneID)
	}
	if r.transfer.Ops[1].ID != runningID {
		t.Errorf("second entry = %+v, want ID %d", r.transfer.Ops[1], runningID)
	}

	text := r.transfer.Table.GetCell(1, transferCenterColResult).Text
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

	cell := r.transfer.Table.GetCell(1, 0)
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
	if len(r.transfer.Ops) != 0 {
		t.Fatalf("setup: expected no operations yet, got %d", len(r.transfer.Ops))
	}

	r.operations.Start(string(notify.CategoryPaste), "/src", "/dst")

	if len(r.transfer.Ops) != 1 {
		t.Errorf("transfer.Ops should have updated live via Subscribe while the screen is open, got %d entries", len(r.transfer.Ops))
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

	if r.transfer.Ops != nil {
		t.Errorf("transfer.Ops should stay unset while the screen was never opened, got %+v", r.transfer.Ops)
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
	r.transfer.Table.Select(1, 0)

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
	r.transfer.Table.Select(1, 0)

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
	r.transfer.Table.Select(1, 0)

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
	r.rsync.Job = job
	r.openTransferCenter()
	r.transfer.Table.Select(1, 0)

	r.pauseResumeSelectedTransferCenterOperation() // must be a no-op, not a panic

	if got := r.operations.List()[0].State; got != operations.StateRunning {
		t.Errorf("State = %v, want unchanged StateRunning (Rsync has no pause)", got)
	}
}
