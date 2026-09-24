package ui

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jagottsicher/breakthrough/internal/activitylog"
)

func newTestRootForCompressJob(t *testing.T) *Root {
	t.Helper()
	r, _, _ := newTestRootWithFile(t)
	return r
}

// TestFinishCompressJobReloadsTheDestinationTab pins finishCompressJob's
// own success path — mirrors TestFinishRsyncJobReloadsTheDestinationPanel
// in rsyncjob_test.go, matched by destDir rather than a single active
// panel, since the user is free to have switched tabs while a
// backgrounded Compress/Extract was still running.
func TestFinishCompressJobReloadsTheDestinationTab(t *testing.T) {
	r := newTestRootForCompressJob(t)
	ctx, cancel := context.WithCancel(context.Background())
	job := &compressJob{ctx: ctx, cancel: cancel, destDir: r.panel.path}
	r.compressJob = job

	if err := os.WriteFile(filepath.Join(r.panel.path, "archived.zip"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	r.finishCompressJob(job, nil)

	var found bool
	for row := 1; row < r.panel.table.GetRowCount(); row++ {
		if ref, ok := r.panel.rowRef(row); ok && ref.name == "archived.zip" {
			found = true
		}
	}
	if !found {
		t.Error("panel was not reloaded after the compress job finished — archived.zip is missing")
	}
	if r.compressJob != nil {
		t.Error("compressJob should be cleared once finished")
	}
}

// TestFinishCompressJobReportsARealError mirrors
// TestFinishRsyncJobReportsARealError.
func TestFinishCompressJobReportsARealError(t *testing.T) {
	r := newTestRootForCompressJob(t)
	ctx, cancel := context.WithCancel(context.Background())
	job := &compressJob{ctx: ctx, cancel: cancel, destDir: r.panel.path}
	r.compressJob = job

	r.finishCompressJob(job, errors.New("compress: boom"))

	if r.activePage != errorPage {
		t.Errorf("activePage = %q, want %q for a genuine failure", r.activePage, errorPage)
	}
}

// TestFinishCompressJobLogsAnAction pins finishCompressJob's own
// activity-log instrumentation on success — compressPastTenseVerb turns
// job.verb's own present-continuous spelling into the past tense the
// log line uses.
func TestFinishCompressJobLogsAnAction(t *testing.T) {
	r := newTestRootForCompressJob(t)
	readLog := attachTestActivityLog(t, r)
	ctx, cancel := context.WithCancel(context.Background())
	job := &compressJob{ctx: ctx, cancel: cancel, destDir: r.panel.path, verb: "Compressing", label: "archive.zip"}
	r.compressJob = job

	r.finishCompressJob(job, nil)

	got := readLog()
	if !strings.Contains(got, string(activitylog.CategoryArchive)) || !strings.Contains(got, "compressed archive.zip") {
		t.Errorf("log = %q, want an archive entry about the completed compress", got)
	}
}

// TestFinishCompressJobLogsAnError is the failure counterpart —
// "Extracting" this time, to also pin the verb mapping for Extract.
func TestFinishCompressJobLogsAnError(t *testing.T) {
	r := newTestRootForCompressJob(t)
	readLog := attachTestActivityLog(t, r)
	ctx, cancel := context.WithCancel(context.Background())
	job := &compressJob{ctx: ctx, cancel: cancel, destDir: r.panel.path, verb: "Extracting", label: "archive.zip"}
	r.compressJob = job

	r.finishCompressJob(job, errors.New("boom"))

	got := readLog()
	if !strings.Contains(got, string(activitylog.CategoryArchive)) || !strings.Contains(got, "extracted archive.zip: boom") {
		t.Errorf("log = %q, want an archive error entry mentioning the failure", got)
	}
}

// TestFinishCompressJobDoesNotLogWhenCancelled mirrors
// TestFinishCompressJobSuppressesErrorWhenCancelled: a cancelled job's
// own wait-error is expected, not a real outcome worth logging either.
func TestFinishCompressJobDoesNotLogWhenCancelled(t *testing.T) {
	r := newTestRootForCompressJob(t)
	readLog := attachTestActivityLog(t, r)
	ctx, cancel := context.WithCancel(context.Background())
	job := &compressJob{ctx: ctx, cancel: cancel, destDir: r.panel.path, verb: "Compressing", label: "archive.zip"}
	r.compressJob = job

	cancel() // simulate cancelCompressJob already having run
	r.finishCompressJob(job, errors.New("signal: killed"))

	if got := readLog(); got != "" {
		t.Errorf("log = %q, want nothing logged for a cancelled job", got)
	}
}

// TestFinishCompressJobSuppressesErrorWhenCancelled mirrors
// TestFinishRsyncJobSuppressesErrorWhenCancelled — cmd.Wait returning an
// error because cancelCompressJob just killed the process is expected,
// not a failure worth an error overlay.
func TestFinishCompressJobSuppressesErrorWhenCancelled(t *testing.T) {
	r := newTestRootForCompressJob(t)
	ctx, cancel := context.WithCancel(context.Background())
	job := &compressJob{ctx: ctx, cancel: cancel, destDir: r.panel.path}
	r.compressJob = job

	cancel() // simulate cancelCompressJob already having run
	r.finishCompressJob(job, errors.New("signal: killed"))

	if r.activePage == errorPage {
		t.Error("a cancelled job's own wait-error should not show an error overlay")
	}
}

// TestFinishCompressJobRunsDeleteOriginalOnSuccess pins "jE"'s own
// contract end to end: once a backgrounded Extract actually succeeds,
// deleteExtractedArchive runs and moves the archive to the Trash — never
// for a failed or cancelled job (see the two tests above), which would
// have nothing safe to delete.
func TestFinishCompressJobRunsDeleteOriginalOnSuccess(t *testing.T) {
	r := newTestRootForCompressJob(t)
	archivePath := filepath.Join(r.panel.path, "bundle.zip")
	if err := os.WriteFile(archivePath, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	job := &compressJob{ctx: ctx, cancel: cancel, destDir: r.panel.path, deleteOriginal: archivePath}
	r.compressJob = job

	r.finishCompressJob(job, nil)

	if _, err := os.Stat(archivePath); !os.IsNotExist(err) {
		t.Errorf("Stat(%s) after finish: err = %v, want a not-exist error (moved to Trash)", archivePath, err)
	}
	if r.activePage == errorPage {
		t.Errorf("finishCompressJob reported an error on the success path: %q", r.errorView.GetText(true))
	}
}

// TestFinishCompressJobStartsTheNextQueuedRun mirrors
// TestFinishRsyncJobStartsTheNextQueuedRun.
func TestFinishCompressJobStartsTheNextQueuedRun(t *testing.T) {
	r := newTestRootForCompressJob(t)
	ctx, cancel := context.WithCancel(context.Background())
	job := &compressJob{ctx: ctx, cancel: cancel, destDir: r.panel.path}
	r.compressJob = job
	r.compressQueue = []compressRequest{{
		command:    "true",
		errContext: "compress",
		verb:       "Compressing",
		label:      "queued.zip",
		destDir:    r.panel.path,
	}}

	r.finishCompressJob(job, nil)

	if r.compressJob == nil {
		t.Fatal("expected the queued run to have started")
	}
	if r.compressJob.label != "queued.zip" {
		t.Errorf("started job label = %q, want %q", r.compressJob.label, "queued.zip")
	}
	r.cancelCompressJob() // don't leave a real process running past this test
}

// TestCancelCompressJobStopsTheProcessAndClearsTheQueue mirrors
// TestCancelRsyncJobStopsTheProcessAndClearsTheQueue.
func TestCancelCompressJobStopsTheProcessAndClearsTheQueue(t *testing.T) {
	r := newTestRootForCompressJob(t)
	ctx, cancel := context.WithCancel(context.Background())
	job := &compressJob{ctx: ctx, cancel: cancel}
	r.compressJob = job
	r.compressQueue = []compressRequest{{label: "queued"}}

	r.cancelCompressJob()

	if job.ctx.Err() == nil {
		t.Error("job's own context should be cancelled")
	}
	if len(r.compressQueue) != 0 {
		t.Error("compressQueue should be cleared by a deliberate cancel")
	}
}

// TestStartCompressJobQueuesBehindARunningJob pins startCompressJob's
// own dispatch: a second request while one is already running is
// appended to compressQueue rather than started immediately, or racing
// the one already in flight.
func TestStartCompressJobQueuesBehindARunningJob(t *testing.T) {
	r := newTestRootForCompressJob(t)
	ctx, cancel := context.WithCancel(context.Background())
	running := &compressJob{ctx: ctx, cancel: cancel, label: "first.zip"}
	r.compressJob = running

	r.startCompressJob(compressRequest{label: "second.zip"})

	if r.compressJob != running {
		t.Error("the already-running job should not have been replaced")
	}
	if len(r.compressQueue) != 1 || r.compressQueue[0].label != "second.zip" {
		t.Errorf("compressQueue = %+v, want one entry for %q", r.compressQueue, "second.zip")
	}
}

// TestReallyStartCompressJobRunsInThePanelsOwnDirectory pins
// reallyStartCompressJob's own cmd construction: cmd.Dir is the local
// panel's current directory, the same rule runShellCommandFullScreen's
// own foreground path already follows, so a command built from plain
// base names (see runCompress) resolves against the right place.
func TestReallyStartCompressJobRunsInThePanelsOwnDirectory(t *testing.T) {
	r := newTestRootForCompressJob(t)

	r.reallyStartCompressJob(compressRequest{
		command:    "true",
		errContext: "compress",
		verb:       "Compressing",
		label:      "test.zip",
		destDir:    r.panel.path,
	})
	if r.compressJob == nil {
		t.Fatal("reallyStartCompressJob did not start a job")
	}
	if got := r.compressJob.cmd.Dir; got != r.panel.path {
		t.Errorf("cmd.Dir = %q, want %q", got, r.panel.path)
	}
	r.cancelCompressJob() // don't leave a real process running past this test
}

// TestReallyStartCompressJobSetsSid pins a real, reported bug: without
// Setsid, the child — an *interactive* shell, per fullScreenShellArgs'
// own "-i" — inherits breakthrough's own session and controlling
// terminal, tries its own job-control setup against it, and the
// kernel's answer is SIGTTOU sent to the whole process group,
// including breakthrough itself, which promptly stops (bash's own job
// control then reports it as "[1]+ Stopped", exactly the reported
// "the whole app just seems to quit" symptom) — see this file's own
// package doc comment for the full mechanism, already fixed for Rsync's
// own identical background-job shape in reallyStartRsyncBackground.
func TestReallyStartCompressJobSetsSid(t *testing.T) {
	r := newTestRootForCompressJob(t)

	r.reallyStartCompressJob(compressRequest{
		command:    "true",
		errContext: "compress",
		verb:       "Compressing",
		label:      "test.zip",
		destDir:    r.panel.path,
	})
	if r.compressJob == nil {
		t.Fatal("reallyStartCompressJob did not start a job")
	}
	if r.compressJob.cmd.SysProcAttr == nil || !r.compressJob.cmd.SysProcAttr.Setsid {
		t.Error("cmd.SysProcAttr.Setsid = false, want true")
	}
	r.cancelCompressJob() // don't leave a real process running past this test
}

// TestCompressProgressText pins compressProgressText's own shape: the
// verb, the label, and the queued-count suffix only once there actually
// is one — the same "don't show (+0 queued)" rule
// pasteProgressText/rsyncProgressText already follow.
func TestCompressProgressText(t *testing.T) {
	job := &compressJob{verb: "Compressing", label: "archive.zip", startedAt: time.Now()}

	got := compressProgressText(job, 0)
	if !strings.Contains(got, "Compressing") || !strings.Contains(got, "archive.zip") {
		t.Errorf("compressProgressText = %q, want it to mention the verb and label", got)
	}
	if strings.Contains(got, "queued") {
		t.Errorf("compressProgressText with nothing queued = %q, should not mention queueing", got)
	}

	if got := compressProgressText(job, 2); !strings.Contains(got, "(+2 queued)") {
		t.Errorf("compressProgressText 2 queued = %q, want it to contain %q", got, "(+2 queued)")
	}
}
