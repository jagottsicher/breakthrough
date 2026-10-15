package ui

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/rivo/tview"

	"github.com/jagottsicher/breakthrough/internal/notify"
	"github.com/jagottsicher/breakthrough/internal/operations"
)

// TestReallyStartPasteRecordsARunningOperation pins the real startPaste
// path (not newPasteTestJob's own fake, already-in-flight job) reaches
// r.operations.Start — the same "real path, not the test fake" reason
// TestStartPasteResolvesCopySettingsOnce goes through startPaste too.
func TestReallyStartPasteRecordsARunningOperation(t *testing.T) {
	dir := fixtureDir(t)
	destDir := t.TempDir()
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	done := isolatePasteIO(t)
	r.startPaste([]string{filepath.Join(dir, "apple.txt")}, false, destDir, false, nil, "", nil, nil)

	active := r.operations.Active()
	if len(active) != 1 {
		t.Fatalf("Active() = %d operations, want 1", len(active))
	}
	got := active[0]
	if got.Type != string(notify.CategoryPaste) {
		t.Errorf("Type = %q, want %q", got.Type, notify.CategoryPaste)
	}
	if got.State != operations.StateRunning {
		t.Errorf("State = %v, want StateRunning", got.State)
	}
	if got.Target != destDir {
		t.Errorf("Target = %q, want %q", got.Target, destDir)
	}
	waitPasteIO(t, done, 1)
}

func TestFinishPasteJobRecordsOutcome(t *testing.T) {
	cases := []struct {
		name        string
		succeeded   int
		errs        []error
		wantOutcome operations.Outcome
	}{
		{"all succeeded", 2, nil, operations.OutcomeSucceeded},
		{"some succeeded some failed", 1, []error{errors.New("boom")}, operations.OutcomePartial},
		{"all failed", 0, []error{errors.New("boom")}, operations.OutcomeFailed},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			destDir := t.TempDir()
			r, err := NewRoot(tview.NewApplication(), destDir)
			if err != nil {
				t.Fatalf("NewRoot: %v", err)
			}
			job := newPasteTestJob(r, false, destDir, 1)
			job.opID = r.operations.Start(string(notify.CategoryPaste), "src", destDir)
			job.succeeded = c.succeeded
			job.errors = c.errs

			r.finishPasteJob(job)

			ops := r.operations.List()
			if len(ops) != 1 {
				t.Fatalf("List() = %d operations, want 1", len(ops))
			}
			got := ops[0]
			if got.State != operations.StateDone {
				t.Errorf("State = %v, want StateDone", got.State)
			}
			if got.Outcome != c.wantOutcome {
				t.Errorf("Outcome = %v, want %v", got.Outcome, c.wantOutcome)
			}
			if got.Succeeded != c.succeeded {
				t.Errorf("Succeeded = %d, want %d", got.Succeeded, c.succeeded)
			}
			if got.Failed != len(c.errs) {
				t.Errorf("Failed = %d, want %d", got.Failed, len(c.errs))
			}
		})
	}
}

func TestCancelPasteJobMarksOperationCancelled(t *testing.T) {
	destDir := t.TempDir()
	r, err := NewRoot(tview.NewApplication(), destDir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	job := newPasteTestJob(r, false, destDir, 3)
	job.opID = r.operations.Start(string(notify.CategoryPaste), "src", destDir)

	r.cancelPasteJob()

	ops := r.operations.List()
	if len(ops) != 1 {
		t.Fatalf("List() = %d operations, want 1", len(ops))
	}
	if ops[0].Outcome != operations.OutcomeCancelled {
		t.Errorf("Outcome = %v, want OutcomeCancelled", ops[0].Outcome)
	}
	if ops[0].State != operations.StateDone {
		t.Errorf("State = %v, want StateDone — a cancelled job must not stay stuck as still running", ops[0].State)
	}
}

func TestCancelPastePauseKeepQueueMarksOperationCancelled(t *testing.T) {
	destDir := t.TempDir()
	r, err := NewRoot(tview.NewApplication(), destDir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	job := newPasteTestJob(r, false, destDir, 3)
	job.opID = r.operations.Start(string(notify.CategoryPaste), "src", destDir)

	r.cancelPastePauseKeepQueue()

	ops := r.operations.List()
	if len(ops) != 1 || ops[0].Outcome != operations.OutcomeCancelled {
		t.Errorf("List() = %+v, want one OutcomeCancelled operation", ops)
	}
}

func TestRequestPastePauseAndResumeSetOperationState(t *testing.T) {
	destDir := t.TempDir()
	r, err := NewRoot(tview.NewApplication(), destDir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	job := newPasteTestJob(r, false, destDir, 1)
	job.opID = r.operations.Start(string(notify.CategoryPaste), "src", destDir)

	r.requestPastePause()
	if got := r.operations.List()[0].State; got != operations.StatePaused {
		t.Errorf("State after requestPastePause = %v, want StatePaused", got)
	}

	r.resumePastePause()
	if got := r.operations.List()[0].State; got != operations.StateRunning {
		t.Errorf("State after resumePastePause = %v, want StateRunning", got)
	}
}

func TestFinishRsyncJobRecordsOperation(t *testing.T) {
	cases := []struct {
		name        string
		err         error
		cancel      bool
		wantOutcome operations.Outcome
	}{
		{"succeeded", nil, false, operations.OutcomeSucceeded},
		{"failed", errors.New("rsync: boom"), false, operations.OutcomeFailed},
		{"cancelled", context.Canceled, true, operations.OutcomeCancelled},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := newTestRootForRsyncJob(t)
			ctx, cancel := context.WithCancel(context.Background())
			if c.cancel {
				cancel()
			} else {
				defer cancel()
			}
			job := &rsyncJob{ctx: ctx, cancel: cancel, destPath: r.panel.path, label: "src -> dst"}
			job.opID = r.operations.Start(string(notify.CategoryRsync), "src", r.panel.path)
			r.rsyncJob = job

			r.finishRsyncJob(job, c.err)

			ops := r.operations.List()
			if len(ops) != 1 {
				t.Fatalf("List() = %d operations, want 1", len(ops))
			}
			if ops[0].Outcome != c.wantOutcome {
				t.Errorf("Outcome = %v, want %v", ops[0].Outcome, c.wantOutcome)
			}
			if ops[0].State != operations.StateDone {
				t.Errorf("State = %v, want StateDone", ops[0].State)
			}
		})
	}
}
