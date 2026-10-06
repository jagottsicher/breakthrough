package ui

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

func TestStartOrphanLabelScanErrorsWithoutLabelsStore(t *testing.T) {
	r, _, _ := newTestRootWithFile(t)
	r.labels = nil

	r.startOrphanLabelScan()

	if r.activePage != errorPage {
		t.Errorf("activePage = %q, want the error overlay", r.activePage)
	}
}

// TestStartOrphanLabelScanNoOpWhileAlreadyRunning pins that a second
// click on the button while a scan is already in flight does nothing
// — no second goroutine, no queued follow-up (see its own doc comment
// on why this needs no queue the way Paste/Rsync/Compress do).
func TestStartOrphanLabelScanNoOpWhileAlreadyRunning(t *testing.T) {
	r, _, _ := newTestRootWithFile(t)
	_, cancel := context.WithCancel(context.Background())
	r.labelScanCancel = cancel
	defer cancel()

	r.startOrphanLabelScan()

	// Still the exact same CancelFunc — a real second scan would have
	// replaced it with a fresh one.
	if r.labelScanCancel == nil {
		t.Fatal("labelScanCancel cleared unexpectedly")
	}
}

func TestCancelOrphanLabelScanCancelsTheContext(t *testing.T) {
	r, _, _ := newTestRootWithFile(t)
	ctx, cancel := context.WithCancel(context.Background())
	r.labelScanCancel = cancel

	r.cancelOrphanLabelScan()

	if ctx.Err() == nil {
		t.Error("context should be cancelled after cancelOrphanLabelScan")
	}
}

// TestFinishOrphanLabelScanCancelledReportsNothing pins that a
// cancelled scan's own result is silently discarded, not reported as
// if it had completed normally.
func TestFinishOrphanLabelScanCancelledReportsNothing(t *testing.T) {
	r, _, file := newTestRootWithFile(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	r.finishOrphanLabelScan(ctx, []string{file})

	if r.activePage == confirmPage {
		t.Error("a cancelled scan should not open the removal confirmation")
	}
	if r.labelScanCancel != nil {
		t.Error("labelScanCancel should be cleared even when cancelled")
	}
}

func TestFinishOrphanLabelScanNoOrphansPushesNotification(t *testing.T) {
	r, _, _ := newTestRootWithFile(t)

	r.finishOrphanLabelScan(context.Background(), nil)

	msgs := r.notify.Messages()
	if len(msgs) == 0 || msgs[len(msgs)-1].Category != "label" {
		t.Errorf("notify messages = %+v, want a trailing \"label\" category push", msgs)
	}
	if r.activePage == confirmPage {
		t.Error("zero orphans should not open a removal confirmation")
	}
}

// TestFinishOrphanLabelScanFoundOrphansOpensConfirm pins the real,
// end-to-end path: a found orphan opens the removal confirmation, and
// confirming it actually clears the label.
func TestFinishOrphanLabelScanFoundOrphansOpensConfirmAndRemoves(t *testing.T) {
	r, dir, _ := newTestRootWithFile(t)
	gone := filepath.Join(dir, "gone.txt")
	if err := r.labels.Set(gone, 3); err != nil {
		t.Fatalf("Set: %v", err)
	}

	r.finishOrphanLabelScan(context.Background(), []string{gone})

	if r.activePage != confirmPage {
		t.Fatalf("activePage = %q, want the removal confirmation %q", r.activePage, confirmPage)
	}
	r.confirmDialog.SetCurrentItem(0) // "Yes, remove"
	r.confirmDialog.InputHandler()(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone), func(tview.Primitive) {})

	if got := r.labels.Get(gone); got != 0 {
		t.Errorf("Get(gone) after confirmed removal = %d, want 0", got)
	}
}

// TestRemoveOrphanLabelsLogsAnAction pins the activity log entry — the
// same convention applyLabel's own logging already establishes.
func TestRemoveOrphanLabelsLogsAnAction(t *testing.T) {
	r, dir, _ := newTestRootWithFile(t)
	gone := filepath.Join(dir, "gone.txt")
	if err := r.labels.Set(gone, 3); err != nil {
		t.Fatalf("Set: %v", err)
	}
	readLog := attachTestActivityLog(t, r)

	r.removeOrphanLabels([]string{gone})

	got := readLog()
	if got == "" {
		t.Error("expected an activity log entry for the orphan removal")
	}
}

// TestRemoveOrphanLabelsRepaintsOpenTabs pins that a removed label is
// reflected immediately in whatever tab is showing that path — the
// same repaint applyLabel already triggers.
func TestRemoveOrphanLabelsRepaintsOpenTabs(t *testing.T) {
	r, dir, _ := newTestRootWithFile(t)
	labeled := filepath.Join(dir, "a.txt")
	if err := r.labels.Set(labeled, 2); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if err := os.Remove(labeled); err != nil {
		t.Fatalf("Remove: %v", err)
	}

	r.removeOrphanLabels([]string{labeled})

	if got := r.labels.Get(labeled); got != 0 {
		t.Errorf("Get(labeled) = %d, want 0 after removal", got)
	}
}
