package ui

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/rivo/tview"

	"github.com/jagottsicher/breakthrough/internal/rsync"
)

func TestOpenRsyncCancelDialogIsANoOpWhenNothingRunning(t *testing.T) {
	r := newTestRootForRsyncJob(t)

	r.openRsyncCancelDialog()

	if r.activePage == bgJobCancelPage {
		t.Error("the dialog should not have opened — there's nothing to cancel")
	}
}

func TestOpenRsyncCancelDialogOpensWhenAJobIsRunning(t *testing.T) {
	r := newTestRootForRsyncJob(t)
	ctx, cancel := context.WithCancel(context.Background())
	r.rsyncJob = &rsyncJob{ctx: ctx, cancel: cancel}

	r.openRsyncCancelDialog()

	if r.activePage != bgJobCancelPage {
		t.Errorf("activePage = %q, want the cancel dialog open", r.activePage)
	}
	r.cancelRsyncJob() // don't leave a real rsync process running past this test
}

func TestOpenRsyncCancelDialogOpensWhenOnlyTheQueueIsNonEmpty(t *testing.T) {
	// A defensive case rather than a currently reachable one (see
	// openRsyncCancelDialog's own doc comment) — the queue is never
	// actually non-empty with rsyncJob nil in practice, but the guard
	// checks both, so this pins that it isn't accidentally
	// rsyncJob-only.
	r := newTestRootForRsyncJob(t)
	r.rsyncQueue = []queuedRsync{{label: "queued"}}

	r.openRsyncCancelDialog()

	if r.activePage != bgJobCancelPage {
		t.Errorf("activePage = %q, want the cancel dialog open", r.activePage)
	}
}

// TestRsyncCancelDialogCurrentItemStopsOnlyTheCurrentJob pins the
// dialog's own "Cancel current job, keep queue" answer end to end:
// opening it wires pendingBgJobCancelCurrent to cancelRsyncKeepQueue
// (see openRsyncCancelDialog), and running it must leave the queued
// entry to start, not drop it.
func TestRsyncCancelDialogCurrentItemStopsOnlyTheCurrentJob(t *testing.T) {
	r := newTestRootForRsyncJob(t)
	ctx, cancel := context.WithCancel(context.Background())
	job := &rsyncJob{ctx: ctx, cancel: cancel}
	r.rsyncJob = job
	r.rsyncQueue = []queuedRsync{{
		job:   rsync.Job{Source: rsync.Endpoint{Path: filepath.Join(r.panel.path, "a.txt")}, Destination: rsync.Endpoint{Path: t.TempDir()}},
		label: "queued",
	}}
	r.openRsyncCancelDialog()

	r.runBgJobCancel(r.pendingBgJobCancelCurrent)

	if job.ctx.Err() == nil {
		t.Error("the current job should be cancelled")
	}
	if r.rsyncJob == nil || r.rsyncJob.label != "queued" {
		t.Errorf("r.rsyncJob = %+v, want the queued run started", r.rsyncJob)
	}
	if r.activePage == bgJobCancelPage {
		t.Error("the dialog should have closed")
	}
	r.cancelRsyncJob() // don't leave a real rsync process running past this test
}

// TestRsyncCancelDialogAllItemDropsTheQueueToo pins the dialog's own
// "Cancel everything" answer: wired to cancelRsyncJob (see
// openRsyncCancelDialog), which drops the queue too, unlike the
// current-only answer just above.
func TestRsyncCancelDialogAllItemDropsTheQueueToo(t *testing.T) {
	r := newTestRootForRsyncJob(t)
	ctx, cancel := context.WithCancel(context.Background())
	job := &rsyncJob{ctx: ctx, cancel: cancel}
	r.rsyncJob = job
	r.rsyncQueue = []queuedRsync{{label: "queued"}}
	r.openRsyncCancelDialog()

	r.runBgJobCancel(r.pendingBgJobCancelAll)

	if job.ctx.Err() == nil {
		t.Error("the current job should be cancelled")
	}
	if len(r.rsyncQueue) != 0 {
		t.Error("the whole queue should have been dropped too")
	}
}

// TestRsyncCancelDialogCurrentItemIsANoOpIfTheJobAlreadyFinished pins
// the user's own explicit requirement: by the time a confirmation is
// actually answered, the job it was about to stop may have already
// finished on its own — answering "Cancel current" at that point must
// do nothing rather than error or touch whatever new job (from the
// queue) may have started in the meantime.
func TestRsyncCancelDialogCurrentItemIsANoOpIfTheJobAlreadyFinished(t *testing.T) {
	r := newTestRootForRsyncJob(t)
	ctx, cancel := context.WithCancel(context.Background())
	job := &rsyncJob{ctx: ctx, cancel: cancel}
	r.rsyncJob = job
	r.openRsyncCancelDialog()

	// The job finishes on its own, exactly as finishRsyncJob's own
	// "already superseded" guard describes, before the dialog is ever
	// answered.
	r.rsyncJob = nil

	r.runBgJobCancel(r.pendingBgJobCancelCurrent) // must not panic or touch anything

	if r.rsyncJob != nil {
		t.Error("r.rsyncJob should still be nil")
	}
}

func TestOpenCompressCancelDialogIsANoOpWhenNothingRunning(t *testing.T) {
	r := newTestRootForCompressJob(t)

	r.openCompressCancelDialog()

	if r.activePage == bgJobCancelPage {
		t.Error("the dialog should not have opened — there's nothing to cancel")
	}
}

func TestOpenCompressCancelDialogOpensWhenAJobIsRunning(t *testing.T) {
	r := newTestRootForCompressJob(t)
	ctx, cancel := context.WithCancel(context.Background())
	r.compressJob = &compressJob{ctx: ctx, cancel: cancel}

	r.openCompressCancelDialog()

	if r.activePage != bgJobCancelPage {
		t.Errorf("activePage = %q, want the cancel dialog open", r.activePage)
	}
	r.cancelCompressJob() // don't leave a real process running past this test
}

func TestConfirmCancelCurrentRsyncIsANoOpWhenNothingRunning(t *testing.T) {
	r := newTestRootForRsyncJob(t)

	r.confirmCancelCurrentRsync()

	if r.activePage == confirmPage {
		t.Error("no confirmation should have opened — there's nothing to cancel")
	}
}

func TestConfirmCancelCurrentRsyncOpensAndStopsOnlyTheCurrentJob(t *testing.T) {
	r := newTestRootForRsyncJob(t)
	ctx, cancel := context.WithCancel(context.Background())
	job := &rsyncJob{ctx: ctx, cancel: cancel}
	r.rsyncJob = job
	r.rsyncQueue = []queuedRsync{{
		job:   rsync.Job{Source: rsync.Endpoint{Path: filepath.Join(r.panel.path, "a.txt")}, Destination: rsync.Endpoint{Path: t.TempDir()}},
		label: "queued",
	}}

	r.confirmCancelCurrentRsync()
	if r.activePage != confirmPage {
		t.Fatalf("activePage = %q, want the confirm dialog open", r.activePage)
	}
	r.acceptConfirm()

	if job.ctx.Err() == nil {
		t.Error("the current job should be cancelled")
	}
	if r.rsyncJob == nil || r.rsyncJob.label != "queued" {
		t.Errorf("r.rsyncJob = %+v, want the queued run started, not dropped", r.rsyncJob)
	}
	r.cancelRsyncJob() // don't leave a real rsync process running past this test
}

func TestConfirmCancelCurrentCompressIsANoOpWhenNothingRunning(t *testing.T) {
	r := newTestRootForCompressJob(t)

	r.confirmCancelCurrentCompress()

	if r.activePage == confirmPage {
		t.Error("no confirmation should have opened — there's nothing to cancel")
	}
}

func TestConfirmCancelCurrentPasteIsANoOpWhenNothingRunning(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}

	r.confirmCancelCurrentPaste()

	if r.activePage == confirmPage {
		t.Error("no confirmation should have opened — there's nothing to cancel")
	}
}

func TestConfirmCancelCurrentPasteStopsOnlyTheCurrentJob(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	job := newPasteTestJob(r, false, dir, 3)

	r.confirmCancelCurrentPaste()
	if r.activePage != confirmPage {
		t.Fatalf("activePage = %q, want the confirm dialog open", r.activePage)
	}
	r.acceptConfirm()

	if job.ctx.Err() == nil {
		t.Error("the current job should be cancelled")
	}
	if r.pasteJob == job {
		t.Error("r.pasteJob should no longer be the cancelled job")
	}
}
